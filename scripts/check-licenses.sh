#!/usr/bin/env bash
set -euo pipefail
# Fail if any dependency uses a license outside the allowed set.
#
# modernc.org/mathutil is ignored because go-licenses cannot detect its
# LICENSE from the module cache. It was verified by hand: the upstream text is
# copied byte for byte to third_party_licenses/modernc.org/mathutil/LICENSE.
# See THIRD_PARTY_NOTES.md.

HERE=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$HERE"

go-licenses check ./... \
  --ignore modernc.org/mathutil \
  --allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,ISC,MPL-2.0,LGPL-2.1,LGPL-3.0,GPL-3.0,AGPL-3.0
