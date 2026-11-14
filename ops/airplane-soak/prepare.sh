#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
#
# Register the soak device once, choose a real track, then point the state at
# a dead URL. Run this before enabling the service.

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd -P)
SOAK_DIR=${SOAK_DIR:-/srv/airplane-soak}
SOAK_BIN=${SOAK_BIN:-/opt/groovesync-soak/gsclient/bin/desktop}
SOAK_SERVER_URL=${SOAK_SERVER_URL:-http://127.0.0.1:8080}
SOAK_DEAD_URL=${SOAK_DEAD_URL:-http://127.0.0.1:9}
SOAK_DEVICE_ID=${SOAK_DEVICE_ID:-airplane-soak-$(date -u +%Y%m%d%H%M%S)}

if [[ -e "$SOAK_DIR/state.json" ]]; then
    echo "state already exists in $SOAK_DIR; remove the directory for a fresh run" >&2
    exit 1
fi
if [[ -z "${SYNCD_ADMIN_TOKEN:-}" ]]; then
    echo "SYNCD_ADMIN_TOKEN is required" >&2
    exit 1
fi
if [[ ! -x "$SOAK_BIN" ]]; then
    echo "soak binary not found: $SOAK_BIN" >&2
    exit 1
fi

mkdir -p "$SOAK_DIR"
chmod 700 "$SOAK_DIR"

"$SOAK_BIN" register \
    --server "$SOAK_SERVER_URL" \
    --admin-env SYNCD_ADMIN_TOKEN \
    --id "$SOAK_DEVICE_ID" \
    --name "Airplane soak" \
    --platform linux \
    --data "$SOAK_DIR" \
    --urls "$SOAK_SERVER_URL" \
    --mode off \
    --scope off \
    --network any

if [[ -n "${SOAK_TRACK:-}" ]]; then
    printf '%s\n' "$SOAK_TRACK" > "$SOAK_DIR/track"
else
    python3 - "$SOAK_DIR/state.json" "$SOAK_SERVER_URL" > "$SOAK_DIR/track" <<'PY'
import json
import sys
import urllib.request

state_path, server = sys.argv[1], sys.argv[2].rstrip("/")
with open(state_path, encoding="utf-8") as handle:
    state = json.load(handle)
token, device_id = state["token"], state["deviceId"]
request = urllib.request.Request(
    server + "/sync/v1/library?limit=1",
    headers={
        "Authorization": f"Bearer {token}",
        "X-Device-Id": device_id,
    },
)
with urllib.request.urlopen(request, timeout=30) as response:
    page = json.load(response)
tracks = page.get("tracks", [])
if not tracks:
    raise SystemExit("library is empty; set SOAK_TRACK")
print(tracks[0]["canonical_id"])
PY
fi

if [[ ! -s "$SOAK_DIR/track" ]]; then
    echo "no track selected; set SOAK_TRACK or add a library track" >&2
    exit 1
fi

printf '%s\n' "$SOAK_SERVER_URL" > "$SOAK_DIR/live-url"
python3 "$HERE/set-url.py" --state "$SOAK_DIR/state.json" --url "$SOAK_DEAD_URL"
chmod 600 "$SOAK_DIR/state.json" "$SOAK_DIR/track" "$SOAK_DIR/live-url"

echo "registered $SOAK_DEVICE_ID"
echo "track $(cat "$SOAK_DIR/track")"
echo "live URL $SOAK_SERVER_URL"
echo "offline URL $SOAK_DEAD_URL"
echo "start the service to begin the soak"
