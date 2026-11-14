#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors
set -euo pipefail
HERE=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$HERE"

output=benchmark/build/outputs/connected_android_test_additional_output
profile=$(find "$output" -name 'BaselineProfileGenerator_generate-baseline-prof.txt' -printf '%T@ %p\n' 2>/dev/null |
  sort -nr | head -1 | cut -d' ' -f2-)
if [ -z "$profile" ]; then
  echo "no generated profile in $output; run the generator test first" >&2
  exit 1
fi

cp "$profile" android/src/main/baseline-prof.txt
echo "copied $profile to android/src/main/baseline-prof.txt"
