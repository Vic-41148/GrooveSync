#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
#
# Queue plays, skips and likes against a dead server, then reconnect once and
# verify every event arrived exactly once. Safe to restart: the deadline and
# the recorded event log live in SOAK_DIR.

set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd -P)
SOAK_DIR=${SOAK_DIR:-/srv/airplane-soak}
SOAK_BIN=${SOAK_BIN:-/opt/groovesync-soak/gsclient/bin/desktop}
SOAK_DAYS=${SOAK_DAYS:-7}
SOAK_INTERVAL_SECONDS=${SOAK_INTERVAL_SECONDS:-600}
SOAK_RECONNECT_SECONDS=${SOAK_RECONNECT_SECONDS:-30}
SOAK_MAX_SYNC_ATTEMPTS=${SOAK_MAX_SYNC_ATTEMPTS:-240}

STATE="$SOAK_DIR/state.json"
TRACK_FILE="$SOAK_DIR/track"
LIVE_URL_FILE="$SOAK_DIR/live-url"
DEADLINE_FILE="$SOAK_DIR/deadline"
ENQUEUED="$SOAK_DIR/enqueued.jsonl"
DAILY="$SOAK_DIR/daily.csv"
STATUS="$SOAK_DIR/status.json"
VERIFY="$SOAK_DIR/verify.json"
DONE="$SOAK_DIR/done"
LOG="$SOAK_DIR/soak.log"

log() {
    printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >> "$LOG"
}

if [[ ! -x "$SOAK_BIN" ]]; then
    echo "soak binary not found: $SOAK_BIN" >&2
    exit 1
fi
if [[ ! -s "$STATE" || ! -s "$TRACK_FILE" || ! -s "$LIVE_URL_FILE" ]]; then
    echo "run prepare.sh first; state, track or live URL is missing" >&2
    exit 1
fi

TRACK=$(cat "$TRACK_FILE")
LIVE_URL=$(cat "$LIVE_URL_FILE")

if [[ -f "$DONE" ]]; then
    log "soak already finished: $(cat "$DONE")"
    while :; do sleep 3600; done
fi

if [[ ! -f "$DEADLINE_FILE" ]]; then
    now=$(date -u +%s)
    echo $((now + SOAK_DAYS * 86400)) > "$DEADLINE_FILE"
    log "deadline set to $SOAK_DAYS days from first start"
fi

while :; do
    now=$(date -u +%s)
    deadline=$(cat "$DEADLINE_FILE")
    if (( now >= deadline )); then
        break
    fi

    for kind in play skip like; do
        if ! "$SOAK_BIN" "$kind" --data "$SOAK_DIR" --track "$TRACK" \
            >> "$SOAK_DIR/enqueue.log" 2>&1; then
            log "enqueue $kind failed"
        fi
    done

    if ! python3 "$HERE/record.py" --state "$STATE" --log "$ENQUEUED" \
        >> "$LOG" 2>&1; then
        log "recording enqueued events failed"
    fi

    if "$SOAK_BIN" sync --data "$SOAK_DIR" >> "$SOAK_DIR/sync.log" 2>&1; then
        log "sync unexpectedly succeeded against the offline URL"
    fi

    if ! python3 "$HERE/progress.py" --state "$STATE" --log "$ENQUEUED" \
        --daily "$DAILY" --status "$STATUS" --deadline "$DEADLINE_FILE" \
        >> "$LOG" 2>&1; then
        log "writing progress failed"
    fi

    now=$(date -u +%s)
    remaining=$((deadline - now))
    sleep_for=$SOAK_INTERVAL_SECONDS
    if (( sleep_for > remaining )); then
        sleep_for=$remaining
    fi
    if (( sleep_for < 1 )); then
        sleep_for=1
    fi
    sleep "$sleep_for"
done

log "deadline reached; reconnecting to $LIVE_URL"
python3 "$HERE/set-url.py" --state "$STATE" --url "$LIVE_URL" >> "$LOG" 2>&1

attempt=0
while (( attempt < SOAK_MAX_SYNC_ATTEMPTS )); do
    attempt=$((attempt + 1))
    if "$SOAK_BIN" sync --data "$SOAK_DIR" >> "$SOAK_DIR/sync.log" 2>&1; then
        pending=$(python3 - "$STATE" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    print(len(json.load(handle).get("outbox", [])))
PY
)
        log "reconnect sync $attempt succeeded; outbox=$pending"
        if (( pending == 0 )); then
            break
        fi
    else
        log "reconnect sync $attempt failed"
    fi
    sleep "$SOAK_RECONNECT_SECONDS"
done

result=failed
for verify_attempt in 1 2 3 4 5; do
    if python3 "$HERE/verify.py" --state "$STATE" --log "$ENQUEUED" \
        --server "$LIVE_URL" --out "$VERIFY" >> "$LOG" 2>&1; then
        result=ok
        break
    fi
    log "verification attempt $verify_attempt failed"
    sleep 60
done

printf 'result=%s at=%s\n' "$result" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$DONE"
chmod 600 "$DONE" "$VERIFY" 2>/dev/null || true
log "soak finished with $result"

while :; do sleep 3600; done
