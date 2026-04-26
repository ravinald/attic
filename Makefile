BIN     := attic
PKG     := github.com/ravinald/attic
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/cmd.version=$(VERSION) \
	-X $(PKG)/internal/cmd.commit=$(COMMIT) \
	-X $(PKG)/internal/cmd.date=$(DATE)

.PHONY: build install test lint vuln check clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/$(BIN)

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BIN)

test:
	go test ./...

lint:
	golangci-lint run

vuln:
	govulncheck ./...

check: lint test vuln

clean:
	rm -rf bin
