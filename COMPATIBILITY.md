# Compatibility target

The target is the installed pyzotero-cli 1.0.0 command surface: 50 leaf commands in nine groups. The machine-readable inventory records names, arguments, flags, defaults, and output choices. It is a target contract, not evidence that every behavior has passed validation.

The new executable is `zotero-go-cli`; the inherited `zotero-cli` entrypoint remains separately buildable. No Python process is called at runtime.

| Group | Commands |
|---|---|
| items | `list`, `get`, `children`, `count`, `versions`, `create`, `update`, `delete`, `add-tags`, `add-doi`, `bib`, `citation`, `deleted` |
| collections | `list`, `get`, `subcollections`, `all`, `items`, `item-count`, `versions`, `create`, `update`, `delete`, `add-item`, `remove-item`, `tags` |
| tags | `list`, `list-for-item`, `delete` |
| files | `download`, `upload`, `upload-batch` |
| search | `list`, `create`, `delete` |
| fulltext | `get`, `list-new`, `set` |
| groups | `list` |
| util | `key-info`, `last-modified-version`, `item-types`, `item-fields`, `item-type-fields`, `item-template` |
| configure | `setup`, `set`, `get`, `list-profiles`, `current-profile` |

## Validation boundaries

Offline tests use mock HTTP servers. Default `go test ./...` excludes the inherited live integration tests, even if Zotero credentials happen to be set. `integration` enables live read tests; `integration_write` enables tests that mutate a disposable library.

Read-only local benchmarks use user-provided citation lists kept outside this repository. Never commit library metadata, attachment files, credentials, or private configuration.

Command help coverage alone does not prove behavioral equivalence. Endpoint, body, concurrency header, output, and error handling tests are required for implementation validation. Exact Click help/error typography is not a compatibility objective.

## Python helper migration

| Existing skill helper | Go command |
|---|---|
| `zotseek_mcp.py` | `zotero-go-cli zotseek tools` / `zotseek call` |
| `validate_zotero_docx.py` | `zotero-go-cli docx validate` |
| `selftest.py` | `zotero-go-cli doctor` |
| `check_updates.py` | `zotero-go-cli skill check-updates --root SKILL_ROOT` |
| `check_package.py` | `zotero-go-cli skill check-package --root SKILL_ROOT` |

Helper differences: timeouts accept Go duration strings (`15s`); standalone skill maintenance commands receive an explicit root instead of inferring a Python script directory. Package validation checks metadata and manifests, not Python bytecode compilation. Readiness checks the built-in Go runtime and direct API/MCP access rather than requiring the retired Python environment or Python CLI. Existing Python files may coexist during migration; none is invoked by this executable.
