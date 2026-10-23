# Official Bitbucket Data Center REST API spec. It is Atlassian's and is not
# committed: `make generate` downloads it, filters it to the pull request API
# and regenerates the (committed) Go client from it.
SPEC_VERSION ?= 9.4
SPEC_URL     ?= https://dac-static.atlassian.com/server/bitbucket/$(SPEC_VERSION).swagger.v3.json

FULL_SPEC     := api/bitbucket.swagger.v3.json
FILTERED_SPEC := api/pullrequests.openapi.json

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build test generate spec filter codegen clean

all: build test

## build: compile ./bin/bitbucket-mcp
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/bitbucket-mcp .

test:
	go vet ./...
	go test ./...

## generate: download the spec, filter it and regenerate internal/bitbucket/client.gen.go
generate: spec filter codegen

## spec: download the full official spec
spec:
	mkdir -p api
	curl -sSfL -o $(FULL_SPEC) $(SPEC_URL)

## filter: reduce the full spec to the pull request API
filter:
	go run ./internal/tools/specfilter -in $(FULL_SPEC) -out $(FILTERED_SPEC)

## codegen: run oapi-codegen on the filtered spec
codegen:
	go generate ./...

clean:
	rm -rf bin api
