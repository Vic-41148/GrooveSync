#!/usr/bin/env python3
"""Check a Macrobenchmark report against the UX-4 budgets.

The handbook proposal is cold start p50 at most 1.2 s, p90 at most 2.0 s, no
run above 3.0 s, and at most 5% janky frames. An emulator cannot sign any of
that off, so by default this refuses to judge one; pass --allow-emulator to
evaluate the numbers anyway.
"""

import argparse
import json
import math
import sys

COLD_P50_MS = 1200.0
COLD_P90_MS = 2000.0
COLD_MAX_MS = 3000.0
JANK_SHARE = 0.05


def percentile(values, percent):
    ordered = sorted(values)
    rank = math.ceil(percent / 100 * len(ordered))
    return ordered[max(rank - 1, 0)]


def is_emulator(context):
    build = context.get("build", {})
    return "emu" in build.get("fingerprint", "") or build.get("model", "").startswith("sdk_")


def benchmark(data, name):
    for entry in data.get("benchmarks", []):
        if entry.get("name") == name:
            return entry
    raise ValueError(f"benchmark {name!r} is missing from the report")


def cold_start_runs(data):
    metric = benchmark(data, "coldStartup").get("metrics", {}).get("timeToInitialDisplayMs")
    if not metric or not metric.get("runs"):
        raise ValueError("cold startup has no timeToInitialDisplayMs runs")
    return metric["runs"]


def jank_frames(data):
    metric = benchmark(data, "libraryScroll").get("sampledMetrics", {}).get("frameOverrunMs")
    if not metric or not metric.get("runs"):
        raise ValueError("library scroll has no frameOverrunMs runs")
    frames = [frame for run in metric["runs"] for frame in run]
    if not frames:
        raise ValueError("library scroll recorded no frames")
    return frames


def check(data, allow_emulator):
    failures = []
    emulator = is_emulator(data.get("context", {}))

    runs = cold_start_runs(data)
    p50 = percentile(runs, 50)
    p90 = percentile(runs, 90)
    worst = max(runs)
    print(
        f"cold start: p50 {p50:.1f} ms (budget {COLD_P50_MS:.0f}), "
        f"p90 {p90:.1f} ms (budget {COLD_P90_MS:.0f}), "
        f"worst {worst:.1f} ms (fail above {COLD_MAX_MS:.0f})"
    )
    if p50 > COLD_P50_MS:
        failures.append(f"cold start p50 {p50:.1f} ms over {COLD_P50_MS:.0f} ms")
    if p90 > COLD_P90_MS:
        failures.append(f"cold start p90 {p90:.1f} ms over {COLD_P90_MS:.0f} ms")
    if worst > COLD_MAX_MS:
        failures.append(f"cold start run {worst:.1f} ms over {COLD_MAX_MS:.0f} ms")

    frames = jank_frames(data)
    janky = sum(1 for frame in frames if frame > 0)
    share = janky / len(frames)
    print(
        f"jank: {share:.1%} of {len(frames)} frames missed the deadline "
        f"(budget {JANK_SHARE:.0%})"
    )
    if share > JANK_SHARE:
        failures.append(f"janky frames {share:.1%} over {JANK_SHARE:.0%}")

    if emulator and not allow_emulator:
        print("refusing to sign off on an emulator; rerun on a reference device or pass --allow-emulator")
        return 3
    if failures:
        for failure in failures:
            print(f"budget violation: {failure}")
        return 1
    print("benchmark gate: within budget")
    return 0


def main(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("report", help="benchmarkData.json from a connected benchmark run")
    parser.add_argument(
        "--allow-emulator",
        action="store_true",
        help="evaluate an emulator report instead of refusing it",
    )
    args = parser.parse_args(argv)
    try:
        with open(args.report, encoding="utf-8") as handle:
            data = json.load(handle)
        return check(data, args.allow_emulator)
    except (OSError, ValueError) as error:
        print(f"benchmark gate: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
