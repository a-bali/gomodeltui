.PHONY: build test vet fmt run clean

BINARY := bin/gomodeltui

build:
	mkdir -p bin
	nix develop --command go build -o $(BINARY) ./cmd/gomodeltui

test:
	nix develop --command go test ./...

vet:
	nix develop --command go vet ./...

fmt:
	nix develop --command gofmt -w cmd internal

run:
	nix develop --command go run ./cmd/gomodeltui

clean:
	rm -f $(BINARY)
