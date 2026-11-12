GO ?= ~/.local/go/bin/go
BASE ?= origin/main

.PHONY: build vet test tidy check-licenses patch-coverage install-hooks

build:
	CGO_ENABLED=0 $(GO) build ./...

build-cgo:
	$(GO) build ./...

vet:
	CGO_ENABLED=0 $(GO) vet ./...

vet-cgo:
	$(GO) vet ./...

test:
	CGO_ENABLED=0 $(GO) test ./...

test-cgo:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

check-licenses:
	@bash ./scripts/check-licenses.sh

patch-coverage:
	@./scripts/patch_coverage.py $(BASE)

install-hooks:
	@install -m 0755 scripts/pre-commit .git/hooks/pre-commit
