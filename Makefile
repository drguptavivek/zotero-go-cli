.PHONY: build zotero-go-cli zot zotero-cli test test-unit test-integration test-write-integration check clean

build: zotero-go-cli

zotero-go-cli:
	go build -trimpath -o bin/zotero-go-cli ./cmd/zotero-go-cli

zot:
	go build -trimpath -o bin/zot ./cmd/zot

zotero-cli:
	go build -trimpath -o bin/zotero-cli ./cmd/zotero-cli

test test-unit:
	go test ./...

check:
	go vet ./...
	go test -race ./...

# Explicitly opt in; credentials are read from the environment.
test-integration:
	go test -tags integration ./tests

# Use only a disposable library: these tests create and delete records.
test-write-integration:
	go test -tags integration_write ./tests

clean:
	rm -rf bin
