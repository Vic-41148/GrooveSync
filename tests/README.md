# tests

Executable evidence for the completion claims in the planning documents.

The planning checklists drifted away from the code: phase 6 was fully ticked and
entirely unbuilt, phase 3 ticked a why view that returned an empty list, and the
cross-cutting file ticked CI and security jobs that did not exist. This directory
makes that class of drift fail a test.

`tests/claims` checks the durable claims. `tests/docs` is a local guard for the
planning files and project prose; it skips when `docs/todo` is absent.

## Run

```sh
go test ./tests/claims
go test ./tests/docs
go test -v ./tests/claims
```

The claims suite needs no services, no network and no local `docs/` checkout.
The docs suite needs the local `docs/` checkout and skips without it.

## How it works

`tests/claims/claims.json` is a list of claims. Each claim has an id, the
document it came from, a status, a severity and a list of evidence strings.
`tests/claims/claims_test.go` checks every evidence string against the working
tree.

Statuses:

- `verified`: the evidence supports the claim.
- `false`: the evidence proves the claim wrong.
- `overstated`: a weaker version exists.
- `stale`: the status text is out of date.
- `unverifiable`: a human run, owner decision or external setting. The subtest
  is skipped.
- `open`: known work that is not done.

Only `unverifiable` may have no evidence. Every other claim must name at least
one piece of evidence.

## Evidence grammar

| Prefix | Meaning |
|---|---|
| `file:<path>` | The file exists. |
| `absent:<path>` | The path does not exist. |
| `workflow:<name>` | `.github/workflows/<name>` exists. |
| `contains:<path>::<token>` | The file contains the exact token. |
| `lacks:<path>::<token>` | The file does not contain the token. |
| `test:<name>` | A Go test function `func <name>(` exists. |
| `nosource:<token>` | No non-test `.go` file under `server/` contains the token. |
| `nokotlin:<token>` | No `.kt` file under `android/`, `desktop/` or `common/` contains the token. |
| `nodep:<token>` | No dependency file declares the token. |

The separator is `::`. If the token itself starts with `//`, write a doubled
colon before it:

```text
contains:server/internal/library/scanner.go:://go:build cgo
```

`test:` only matches Go tests. For Kotlin use:

```text
contains:desktop/src/test/kotlin/com/groovesync/client/SyncRunnerTest.kt::fun protectsLikedTracks()
```

## Adding or changing a claim

1. Add the claim with a unique, sorted id.
2. Pick the status that describes the document, not the code.
3. Add evidence that a reader can check with one command.
4. Run `go test ./tests/claims`.

When a gap is fixed, the contradiction evidence stops holding, so the test
fails. Move the claim to `verified`, point it at the new test or file, and update
the document in the same commit. This is deliberate.

## What this does not do

It does not prove the product works. It proves the documentation and the tree
agree. Product behaviour belongs in the package tests, the contract suite and
the provisioner integration suite described in the plan.

## Plan

The full suite plan, including the contract, integration, packaging and local
documentation suites, is in `docs/audit/test-plan.md`. `docs/` is git-ignored, so
that file is local only. The audit that produced it is
`docs/audit/completion-claims-audit.md`.
