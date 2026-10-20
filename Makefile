BINARY  := bin/gomodeltui
PKG     := ./cmd/gomodeltui
GO      ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: all build install test test-race vet fmt fmt-check tidy run snapshot clean macos macos-test macos-dmg

all: build

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

tidy:
	$(GO) mod tidy

run:
	$(GO) run $(PKG)

snapshot:
	goreleaser release --snapshot --clean

macos:
	VERSION="$(VERSION)" sh macos/build-app.sh

macos-test:
	swift test --package-path macos

macos-dmg: macos
	mkdir -p dist
	hdiutil create -volname "GoModel $(VERSION)" -srcfolder bin/GoModel.app -ov -format UDZO "dist/GoModel_$(VERSION)_macOS.dmg"

clean:
	rm -rf bin dist
