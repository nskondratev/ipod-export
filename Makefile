.PHONY: lint test

GOLANGCI_LINT_CACHE ?= $(CURDIR)/.golangci-lint-cache

lint:
	GOLANGCI_LINT_CACHE=$(GOLANGCI_LINT_CACHE) go tool -modfile=tools/go.mod golangci-lint run ./...

test:
	go test ./...
