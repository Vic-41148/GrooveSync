#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors

# Snapshot the Navidrome data directory, the syncd database and the compose
# inputs into a restic repository. syncd is stopped for the copy so SQLite is
# consistent, then started again before restic runs.

set -euo pipefail

: "${RESTIC_REPOSITORY:?set RESTIC_REPOSITORY}"
: "${RESTIC_PASSWORD_FILE:?set RESTIC_PASSWORD_FILE}"
export RESTIC_REPOSITORY RESTIC_PASSWORD_FILE

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
COMPOSE_FILE="$ROOT/docker-compose.yml"
NAVIDROME_DATA=${NAVIDROME_DATA:-$ROOT/data/navidrome}
SYNCD_SERVICE=${SYNCD_SERVICE:-syncd}

if [ ! -d "$NAVIDROME_DATA" ]; then
  echo "backup: $NAVIDROME_DATA is missing" >&2
  exit 1
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

container=$(docker compose -f "$COMPOSE_FILE" ps -q "$SYNCD_SERVICE")
if [ -z "$container" ]; then
  echo "backup: $SYNCD_SERVICE has no container, start the stack once first" >&2
  exit 1
fi

docker compose -f "$COMPOSE_FILE" stop "$SYNCD_SERVICE" >/dev/null
docker cp "$container:/data" "$STAGE/syncd" >/dev/null
docker compose -f "$COMPOSE_FILE" start "$SYNCD_SERVICE" >/dev/null

paths=("$NAVIDROME_DATA" "$STAGE/syncd" "$COMPOSE_FILE")
if [ -f "$ROOT/.env" ]; then
  paths+=("$ROOT/.env")
fi

restic backup --tag groovesync "${paths[@]}"
restic forget --tag groovesync \
  --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune
restic check
