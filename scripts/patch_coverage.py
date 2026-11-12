#!/usr/bin/env python3
"""Report Go patch coverage for the changes between a base revision and HEAD.

The gate is deliberately about changed lines, not whole packages: existing
untested code must not block a small change, but a change that adds uncovered
statements must.
"""

import argparse
import os
import re
import subprocess
import sys

HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")


def run(cmd):
    return subprocess.run(cmd, check=True, text=True, capture_output=True).stdout


def module_path():
    with open("go.mod", encoding="utf-8") as handle:
        for line in handle:
            if line.startswith("module "):
                return line.split()[1]
    raise SystemExit("go.mod has no module line")


def changed_lines(base):
    """Map repo-relative Go paths to the set of added or modified line numbers."""
    diff = run(["git", "diff", "--unified=0", "--no-color", f"{base}...HEAD", "--", "*.go"])
    lines = {}
    path = None
    new_line = 0
    for raw in diff.splitlines():
        if raw.startswith("+++ b/"):
            path = raw[6:]
            lines.setdefault(path, set())
            continue
        if raw.startswith("+++ /dev/null"):
            path = None
            continue
        match = HUNK.match(raw)
        if match:
            new_line = int(match.group(1))
            continue
        if path is None:
            continue
        if raw.startswith("+") and not raw.startswith("+++"):
            lines[path].add(new_line)
            new_line += 1
        elif raw.startswith(" "):
            new_line += 1
    return {path: found for path, found in lines.items() if found}


def coverage(base):
    profile = "coverage.out"
    subprocess.run(
        ["go", "test", "-coverprofile", profile, "./..."],
        check=True,
        text=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env={**os.environ, "CGO_ENABLED": "0"},
    )
    prefix = module_path() + "/"
    blocks = []
    with open(profile, encoding="utf-8") as handle:
        next(handle)
        for line in handle:
            file_part, rest = line.rsplit(":", 1)
            span, statements, count = rest.split()
            start, end = span.split(",")
            start_line = int(start.split(".")[0])
            end_line = int(end.split(".")[0])
            path = file_part[len(prefix):] if file_part.startswith(prefix) else file_part
            blocks.append((path, start_line, end_line, int(statements), int(count)))
    return blocks


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base", nargs="?", default="origin/main")
    parser.add_argument("--min", type=float, default=80.0)
    args = parser.parse_args()

    changed = changed_lines(args.base)
    if not changed:
        print(f"no Go changes against {args.base}")
        return 0

    blocks = coverage(args.base)
    per_file = {}
    total_statements = 0
    covered_statements = 0
    for path, start, end, statements, count in blocks:
        touched = changed.get(path)
        if not touched:
            continue
        if not any(start <= line <= end for line in touched):
            continue
        entry = per_file.setdefault(path, [0, 0])
        entry[0] += statements
        total_statements += statements
        if count > 0:
            entry[1] += statements
            covered_statements += statements

    for path in sorted(per_file):
        covered, total = per_file[path]
        print(f"{path}: {covered}/{total} statements")

    if total_statements == 0:
        print("no covered statements in the patch")
        return 0

    percent = covered_statements * 100.0 / total_statements
    print(f"patch coverage: {percent:.1f}% ({covered_statements}/{total_statements})")
    if percent < args.min:
        print(f"patch coverage below {args.min:.1f}%", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
