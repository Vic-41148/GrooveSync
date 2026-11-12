Contributor guidelines

This project uses the Developer Certificate of Origin (DCO). Do not submit a
Contributor License Agreement (CLA). Sign your commits using "git commit -s".

By contributing you certify that you have the right to submit the changes and
that your contribution can be distributed under the project's AGPL-3.0-or-later
license.

Please keep changes small and well-scoped. Tests are expected for new behavior
and must pass locally before submitting.

New source files need an SPDX license header, for example:

    // SPDX-License-Identifier: AGPL-3.0-or-later
    // Copyright (C) 2026 GrooveSync Authors

Use the helper to add them to every supported file type. It is a dry run by
default and prints the files it would change:

    python3 add_spdx_headers.py --holder "GrooveSync Authors"

Re-run it with `--apply` to write the headers:

    python3 add_spdx_headers.py --holder "GrooveSync Authors" --apply

Generated files are skipped. In SQL migrations the header goes above the
`-- +goose` annotations, and in shell scripts it goes below the shebang; the
helper handles both.

Run `make check-licenses` before opening a PR to ensure third-party licenses
conform to the project's allowed list. A documented exception exists for
modernc.org/mathutil.

Flaky tests are treated as defects. PR gates never retry automatically: a red
run stays red until someone reruns it after understanding the failure. An
end-to-end run gets at most one retry, and the retry is logged with the failing
job and the reason. A test that flakes twice within 14 days is quarantined,
skipped behind an explicit guard and tracked in `docs/todo/`, until the
underlying defect is fixed.

Commit subjects follow Conventional Commits so release-please can build
`CHANGELOG.md`. Keep the description imperative and in the existing style, for
example `feat: cache acquired renditions` or `fix: keep the outbox on a
transport error`. The release workflow only sees commits with a valid prefix.
