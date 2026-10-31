GO ?= ~/.local/go/bin/go

.PHONY: build vet test tidy check-licenses

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

check-licenses:
	@bash ./scripts/check-licenses.sh
