# GrooveSync

GrooveSync is a local-first music app with a Go companion service named `syncd`.

Current scope:
- `main` holds the stable working line
- `dev` holds active work
- `docs/` stays local-only and is ignored by git

## Quick start

```sh
go build ./...
go vet ./...
go test ./...
```

## Service

`cmd/syncd` starts the companion HTTP service.

The service currently exposes:
- `GET /healthz`
- `GET /sync/v1/meta`

## License

This project is licensed under the GNU Affero General Public License v3 or later (AGPL-3.0-or-later). See LICENSE for the full text. By contributing, you agree to sign-off your commits using the Developer Certificate of Origin (DCO). For details on contribution and DCO sign-off see CONTRIBUTING.md.
