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
