# Repository guidance

This fork builds `zotero-go-cli`, a Go replacement for the pyzotero-cli command surface and the Zotero-use skill helpers. See README.md for usage and COMPATIBILITY.md for the migration contract and validation boundaries. Preserve the upstream MIT license and attribution.

## Code ownership and architecture

- `zotero/`: HTTP client, API operations, pagination, downloads, retries, and response models.
- `internal/cli`, `internal/config`, `internal/output`: command contracts, profiles, and output handling.
- `internal/workflow`: reference parsing, identity resolution, collection scoping, and PDF checks.
- `internal/semantic`: ZOTseek MCP discovery and invocation.
- `internal/docx`: read-only OOXML citation validation.
- `internal/maintenance`, `internal/diagnostic`: skill package/update checks and readiness diagnostics.
- `cmd/zotero-go-cli`: primary entrypoint. The inherited `cmd/zotero-cli` remains a separate older entrypoint.

## Build and verification

```sh
make build
go test ./...
go test -race ./...
go vet ./...
```

Default tests are offline. Live read tests require `-tags integration`; write tests require `-tags integration_write` and a disposable library. Never use a personal library for write tests. Local Zotero access uses `http://localhost:23119/api` and is read-only.

Preserve dirty worktrees and concurrent edits. Keep credentials, private bibliographies, library metadata, and PDFs outside the repository. Verify command behavior with mock HTTP request/response tests; command names and help output alone do not establish parity. Do not introduce runtime Python dependencies.
