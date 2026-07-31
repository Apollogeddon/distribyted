#-include .env

VERSION := $(shell git describe --tags 2>/dev/null || echo "dev")
BUILD := $(shell git rev-parse --short HEAD)
BIN_OUTPUT ?= bin/distribyted-$(VERSION)-`go env GOOS`-`go env GOARCH``go env GOEXE`
PROJECTNAME := $(shell basename "$(PWD)")

# Use linker flags to provide version/build settings
LDFLAGS=-X=main.Version=$(VERSION) -X=main.Build=$(BUILD)

# Make is verbose in Linux. Make it silent.
MAKEFLAGS += --silent

all: build

## run: run from code.
run:
	go run cmd/distribyted/main.go examples/conf_example.yaml

## build: build binary.
build: go-generate go-build

## test-race: execute all tests with race enabled.
test-race:
	go test --race -coverprofile=coverage.out ./...

## test-short: execute unit tests only (skips integration tests) with race enabled.
test-short:
	go test --short --race -coverprofile=coverage.out ./...

## test: execute all tests
test:
	go test -coverprofile=coverage.out ./...

## bench: run performance/latency benchmarks (streaming start latency, mount
## and route lookup scaling, HTTP API/WebDAV serving latency, dashboard
## polling costs, archive access). Compare a saved run against a later one
## with benchstat to catch regressions: go install
## golang.org/x/perf/cmd/benchstat@latest; make bench > old.txt; (change
## code); make bench > new.txt; benchstat old.txt new.txt
bench:
	go test -bench=. -benchmem -run='^$$' ./internal/fs/... ./internal/http/... ./internal/testenv/... ./internal/torrent/... ./internal/webdav/...

go-build:
	@echo "  >  Building binary on $(BIN_OUTPUT)..."
	go build -o $(BIN_OUTPUT) -tags "release" -ldflags='$(LDFLAGS)' cmd/distribyted/main.go

go-generate:
	@echo "  >  Generating code files..."
	go generate ./...

.PHONY: help
all: help
help: Makefile
	@echo
	@echo " Choose a command run in "$(PROJECTNAME)":"
	@echo
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'
	@echo
