GO ?= ~/.local/go/bin/go

.PHONY: build vet test tidy

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy
