# Official Bitbucket Data Center REST API spec.
SPEC_VERSION ?= 9.4
SPEC_URL     ?= https://dac-static.atlassian.com/server/bitbucket/$(SPEC_VERSION).swagger.v3.json

FULL_SPEC     := api/bitbucket.swagger.v3.json
FILTERED_SPEC := api/pullrequests.openapi.json

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build test generate spec filter regenerate clean

all: generate build test

## build: compile ./bin/bitbucket-mcp
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/bitbucket-mcp ./cmd/bitbucket-mcp

test:
	go vet ./...
	go test ./...

## spec: download the full official spec (not committed)
spec:
	curl -sSfL -o $(FULL_SPEC) $(SPEC_URL)

## filter: reduce the full spec to the pull request API
filter: $(FULL_SPEC)
	go run ./internal/tools/specfilter -in $(FULL_SPEC) -out $(FILTERED_SPEC)

$(FULL_SPEC):
	$(MAKE) spec

## generate: run oapi-codegen on the filtered spec
generate:
	go generate ./...

## regenerate: download, filter and generate from scratch
regenerate: spec filter generate

clean:
	rm -rf bin
