.DEFAULT_GOAL := help

.PHONY: help run build test integration vet fmt check

help:
	@printf '%s\n' \
	  'run    Run the server (requires DATABASE_URL; DB_MAX_CONNS=10, HTTP_ADDR=:8080, LOG_LEVEL=info)' \
	  'build  Build bin/server' \
	  'test   Run unit tests without Docker or PostgreSQL' \
	  'integration  Run tagged tests (requires TEST_DATABASE_URL of a separate database)' \
	  'vet    Run go vet' \
	  'fmt    Format Go sources' \
	  'check  Check formatting, build, vet and unit tests without changing sources'

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./...

integration:
	@test -n "$$TEST_DATABASE_URL" || { printf '%s\n' 'TEST_DATABASE_URL is required (use a separate test database)' >&2; exit 1; }
	go test -tags=integration -count=1 ./...

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
