GO ?= ~/.local/go/bin/go

.PHONY: build vet test tidy check-licenses

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
