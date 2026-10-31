Licensing setup checklist

This document explains the steps to apply AGPL-3.0-or-later to this repository
and the small follow-up tasks to ensure consistency across packaging and
metadata.

Steps
1. LICENSE
   - Place the full AGPL-3.0-or-later text in a file named LICENSE at repo root.

2. SPDX headers
   - Add an SPDX header to source files. Example: "SPDX-License-Identifier: AGPL-3.0-or-later".
   - Optionally add a copyright holder line: "Copyright (C) YEAR Your Name".
   - Use the provided add_spdx_headers.py script to apply headers safely.

3. Packaging metadata
   - Update project manifests where applicable (Dockerfile, pyproject.toml,
     package.json, go.mod, etc.) to include license: AGPL-3.0-or-later.

4. Contribution policy
   - Do not require a CLA. Use DCO sign-offs on commits (git commit -s) and
     document the DCO policy in CONTRIBUTING.md.

5. Source availability for network users
   - AGPL requires offering source to users of a networked service. Document
     where source is available (eg. a public Git URL) and provide an easy link
     in the service UI or API if applicable.

6. Dependency license check
   - Run a license scan for dependencies and confirm compatibility. Fail the
     CI build if dependencies are incompatible with AGPL requirements.

7. Docs and README
   - Add a short licensing blurb to README.md describing AGPL-3.0-or-later and
     what it means for contributors and operators.

Applying defaults
- Sync-mode default (D10)
  - Update internal/devices/devices.go to keep ModeApp as the default for new
    registrations (code already defaults to ModeApp). Add tests asserting the
    default remains ModeApp and document the choice in LICENSING_SETUP.md.

Notes
- Use AGPL-3.0-or-later in SPDX and repository metadata to allow future minor
  updates of the license text.
