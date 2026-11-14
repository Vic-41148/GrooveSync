#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
"""Write daily enqueue counts and a small status file for the soak."""

import argparse
import collections
import datetime as dt
import json
import os


def recorded_events(path):
    if not os.path.exists(path):
        return []
    events = []
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if line:
                try:
                    events.append(json.loads(line))
                except ValueError:
                    continue
    return events


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--state", required=True)
    parser.add_argument("--log", required=True)
    parser.add_argument("--daily", required=True)
    parser.add_argument("--status", required=True)
    parser.add_argument("--deadline", required=True)
    args = parser.parse_args()

    events = recorded_events(args.log)
    by_day = collections.defaultdict(lambda: collections.Counter())
    for event in events:
        stamp = event.get("recorded_at", "")
        day = stamp[:10] if len(stamp) >= 10 else "unknown"
        by_day[day][event.get("type", "unknown")] += 1

    with open(args.daily, "w", encoding="utf-8") as out:
        out.write("date,play,skip,like,total\n")
        for day in sorted(by_day):
            counts = by_day[day]
            total = sum(counts.values())
            out.write(f"{day},{counts['play']},{counts['skip']},{counts['like']},{total}\n")

    with open(args.state, encoding="utf-8") as handle:
        state = json.load(handle)
    with open(args.deadline, encoding="utf-8") as handle:
        deadline = int(handle.read().strip())

    status = {
        "updated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "deadline": dt.datetime.fromtimestamp(deadline, dt.timezone.utc).isoformat(),
        "recorded": len(events),
        "outbox": len(state.get("outbox", [])),
        "last_sync": state.get("lastSync", ""),
        "mode": state.get("syncMode", ""),
    }
    with open(args.status, "w", encoding="utf-8") as out:
        json.dump(status, out, indent=2, sort_keys=True)
        out.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
