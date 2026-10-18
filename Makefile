.DEFAULT_GOAL := help

.PHONY: help run build test vet fmt check

help:
	@printf '%s\n' \
	  'run    Run the HTTP server (HTTP_ADDR=:8080, LOG_LEVEL=info)' \
	  'build  Build bin/server' \
	  'test   Run unit tests without Docker or PostgreSQL' \
	  'vet    Run go vet' \
	  'fmt    Format Go sources' \
	  'check  Check formatting, build, vet and unit tests without changing sources'

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

check:
	@files=$$(gofmt -l .) || exit $$?; \
	if [ -n "$$files" ]; then \
	  printf 'Go files need formatting (run make fmt):\n%s\n' "$$files" >&2; \
	  exit 1; \
	fi
	go build ./...
	$(MAKE) vet
	$(MAKE) test
