# Rules

Binding for every agent and contributor working in this repo.

## Commits

- GPG-signed only. `commit.gpgsign` is on, do not disable it.
- DCO sign-off on every commit: `git commit -s`.
- No `Co-Authored-By`, no second `Signed-off-by`, no AI attribution in messages, code or docs.
- Commit in logical chunks as work completes. Push only when asked.
- Imperative subject, no trailing period, no emojis, no em dashes.

## Text

- No emojis. No em dashes. Use a comma or a plain hyphen.
- No AI attribution anywhere. `AI-POLICY.md` is the single place where AI
  involvement is disclosed, because the licence and the stores require it.

## Code

- Comment only non-obvious intent. Do not restate code, do not narrate the change.
- No section banners, no commented-out code, no placeholder TODOs.
- Small packages, no globals, dependencies injected through a struct.
- Tests are table-driven where the cases share a shape.

## Repo

- `docs/` is local only and never committed.
- Every GitHub Action is pinned by full commit SHA with the tag in a comment.
- The Flatpak manifest is human-written, always.
- No Spotify or YouTube audio, no in-app APK updater, no proprietary SDKs.
