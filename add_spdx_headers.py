# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 GrooveSync Authors

#!/usr/bin/env python3
"""
Add SPDX license headers to source files.

Usage: add_spdx_headers.py --holder "Your Name" [--apply]

By default the script does a dry run and prints files it would change.
"""
import argparse
import os
import sys

TYPES = {
    '.go': '// SPDX-License-Identifier: AGPL-3.0-or-later\n// Copyright (C) {year} {holder}\n',
    '.py': '# SPDX-License-Identifier: AGPL-3.0-or-later\n# Copyright (C) {year} {holder}\n',
    '.js': '/* SPDX-License-Identifier: AGPL-3.0-or-later */\n/* Copyright (C) {year} {holder} */\n',
    '.rs': '// SPDX-License-Identifier: AGPL-3.0-or-later\n// Copyright (C) {year} {holder}\n',
}


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--holder', required=True)
    p.add_argument('--apply', action='store_true')
    p.add_argument('--year', type=str, help='Year to use in headers (default: current year)')
    args = p.parse_args()
    year = args.year or str(__import__('datetime').date.today().year)
    changes = []
    SKIP_DIRS = {'node_modules', '.git', 'vendor', 'third_party'}
    for root, dirs, files in os.walk('.'):
        # skip known generated/vendor dirs
        if any(sd in root for sd in SKIP_DIRS):
            continue
        for f in files:
            _, ext = os.path.splitext(f)
            if ext not in TYPES:
                continue
            path = os.path.join(root, f)
            # skip generated files by marker or protobuf generated files
            if f.endswith('.pb.go'):
                continue
            with open(path, 'rb') as fh:
                data = fh.read()
            try:
                text = data.decode('utf-8')
            except Exception:
                continue
            header = TYPES[ext].format(year=year, holder=args.holder)
            first_lines = '\n'.join(text.splitlines()[:5])
            if 'DO NOT EDIT' in first_lines or 'Code generated' in first_lines:
                continue
            if header.strip() in first_lines:
                continue
            changes.append(path)
            if args.apply:
                new = header + '\n' + text
                with open(path, 'wb') as fh:
                    fh.write(new.encode('utf-8'))
    if args.apply:
        print('Applied headers to', len(changes), 'files')
    else:
        for p in changes:
            print('Would add header to', p)


if __name__ == '__main__':
    main()
