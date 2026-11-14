#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
"""Record every event still in the client outbox, using its HLC wall time."""

import argparse
import json
import os
import sys


def load_ids(path):
    ids = set()
    if not os.path.exists(path):
        return ids
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                ids.add(json.loads(line)["id"])
            except (KeyError, ValueError):
                continue
    return ids


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--state", required=True)
    parser.add_argument("--log", required=True)
    args = parser.parse_args()

    with open(args.state, encoding="utf-8") as handle:
        state = json.load(handle)

    seen = load_ids(args.log)
    added = 0
    with open(args.log, "a", encoding="utf-8") as out:
        for event in state.get("outbox", []):
            event_id = event.get("id")
            if not event_id or event_id in seen:
                continue
            hlc = event.get("hlc") or {}
            out.write(json.dumps({
                "id": event_id,
                "type": event.get("type", ""),
                "canonical_id": event.get("canonicalId", ""),
                "recorded_at": hlc.get("wall", ""),
            }, separators=(",", ":")) + "\n")
            seen.add(event_id)
            added += 1

    print(added)
    return 0


if __name__ == "__main__":
    sys.exit(main())
