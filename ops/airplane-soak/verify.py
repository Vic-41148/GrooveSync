#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
"""Compare recorded outbox ids with the server event log, exactly once."""

import argparse
import collections
import datetime as dt
import json
import sys
import urllib.parse
import urllib.request


def recorded_ids(path):
    ids = set()
    counts = collections.Counter()
    if not path:
        return ids, counts
    with open(path, encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            event = json.loads(line)
            ids.add(event["id"])
            counts[event.get("type", "unknown")] += 1
    return ids, counts


def fetch_events(server, token, device_id):
    events = []
    since = ""
    while True:
        query = {"limit": "500"}
        if since:
            query["since"] = since
        url = server.rstrip("/") + "/sync/v1/events?" + urllib.parse.urlencode(query)
        request = urllib.request.Request(
            url,
            headers={
                "Authorization": f"Bearer {token}",
                "X-Device-Id": device_id,
            },
        )
        with urllib.request.urlopen(request, timeout=30) as response:
            page = json.load(response)
        events.extend(event for event in page.get("events", []) if event.get("device_id") == device_id)
        cursor = page.get("next", "")
        if not page.get("has_more") or not cursor or cursor == since:
            break
        since = cursor
    return events


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--state", required=True)
    parser.add_argument("--log", required=True)
    parser.add_argument("--server", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    with open(args.state, encoding="utf-8") as handle:
        state = json.load(handle)
    token = state.get("token", "")
    device_id = state.get("deviceId", "")
    if not token or not device_id:
        raise SystemExit("state has no device token or device id")

    enqueued, by_type = recorded_ids(args.log)
    server_events = fetch_events(args.server, token, device_id)
    server_ids = [event["id"] for event in server_events]
    server_set = set(server_ids)

    report = {
        "checked_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "device_id": device_id,
        "enqueued": len(enqueued),
        "server_events": len(server_events),
        "server_unique": len(server_set),
        "duplicates": len(server_ids) - len(server_set),
        "missing": sorted(enqueued - server_set),
        "unexpected": sorted(server_set - enqueued),
        "outbox": len(state.get("outbox", [])),
        "by_type": dict(sorted(by_type.items())),
    }
    report["ok"] = (
        report["outbox"] == 0
        and report["duplicates"] == 0
        and not report["missing"]
        and not report["unexpected"]
    )
    with open(args.out, "w", encoding="utf-8") as handle:
        json.dump(report, handle, indent=2, sort_keys=True)
        handle.write("\n")

    print(
        f"enqueued={report['enqueued']} server={report['server_unique']} "
        f"duplicates={report['duplicates']} outbox={report['outbox']} "
        f"missing={len(report['missing'])} unexpected={len(report['unexpected'])}"
    )
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
