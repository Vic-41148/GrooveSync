#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
"""Point the client state at one server URL without printing the token."""

import argparse
import json
import os
import tempfile


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--state", required=True)
    parser.add_argument("--url", required=True)
    args = parser.parse_args()

    with open(args.state, encoding="utf-8") as handle:
        state = json.load(handle)
    state["serverUrls"] = [args.url]

    directory = os.path.dirname(os.path.abspath(args.state))
    fd, temp = tempfile.mkstemp(dir=directory, prefix=".state-", text=True)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            json.dump(state, out, indent=2)
            out.write("\n")
        os.chmod(temp, 0o600)
        os.replace(temp, args.state)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
