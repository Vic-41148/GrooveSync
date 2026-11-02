#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors

set -euo pipefail
HERE=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$HERE"

go run github.com/google/go-licenses@v1.6.0 check ./... \
  --ignore modernc.org/mathutil \
  --allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,ISC,MPL-2.0,LGPL-2.1,LGPL-3.0,GPL-3.0,AGPL-3.0
