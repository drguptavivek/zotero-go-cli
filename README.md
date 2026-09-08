# zotero-go-cli

A standalone Go executable for Zotero library operations, reference resolution, PDF attachment checks, ZOTseek MCP access, and Zotero skill utilities. It is built on the MIT-licensed [Epistemic Technology Zotero client](https://github.com/Epistemic-Technology/zotero), with changes for local file retrieval, request handling, and CLI workflows.

The compatibility target is all 50 command paths in pyzotero-cli 1.0.0. See [COMPATIBILITY.md](COMPATIBILITY.md) for the command inventory, migration boundaries, and validation scope. The project is under active development; command presence alone is not a parity guarantee.

## Build

```sh
go build -trimpath -o bin/zotero-go-cli ./cmd/zotero-go-cli
./bin/zotero-go-cli --help
```

Go is needed only to build. The resulting executable does not invoke Python, `zot`, `uv`, Node.js, or another package environment. Use the matching binary for macOS, Linux, or Windows and the target processor architecture. Cross-build CI targets Apple-silicon macOS, Windows amd64, and Linux amd64/arm64.

Tagged releases publish installable bundles for Apple-silicon macOS, Windows
amd64, and Linux amd64/arm64. Each bundle contains the matching executable and
the complete `zotero-use` skill under `zotero-use/`; copy that directory into
your agent's skills directory. The binary is at `zotero-use/bin/zotero-go-cli`
(`.exe` on Windows). Release assets include a `SHA256SUMS` file for integrity
checking, and GitHub build-provenance attestations can be verified with
`gh attestation verify <archive> --repo drguptavivek/zotero-go-cli`.
`BUNDLE-MANIFEST.json` records the CLI and skill versions, source commits,
target platform, and binary path.

The Skills CLI can install the skill repository, but it does not support a
post-install hook for downloading a native companion executable. Use these
platform release bundles when the installation must contain both the skill and
the executable.

The release workflow does not currently code-sign or notarize binaries. macOS
Gatekeeper and Windows SmartScreen may therefore warn on direct downloads; see
the release notes for the verification status before distributing broadly.
Release candidates use the same unsigned artifacts and are intended for
controlled testing. Verify `SHA256SUMS` and, where available, the GitHub
build-provenance attestation before extraction. For broad macOS distribution,
the executable needs an Apple Developer ID signature and notarization; Windows
distribution should use Authenticode signing. Neither checksum verification nor
an attestation changes the operating system's trust warning by itself.

Local access requires Zotero Desktop with its local API enabled. Cloud access uses Zotero API credentials and library configuration. Local library operations are read-only. Cloud write commands modify records only when explicitly invoked.

## Reference workflows

```sh
zotero-go-cli --local --library-id 0 --library-type user items list \
  --query "FULL ARTICLE TITLE" --qmode titleCreatorYear --output json

zotero-go-cli --local --library-id 0 workflow resolve-references references.txt \
  --output results.json --pdf verify

zotero-go-cli --local --library-id 0 workflow collection-resolve \
  "Glaucoma > Burden > India"

zotero-go-cli --local --library-id 0 workflow resolve-references references.txt \
  --collection-path "Glaucoma > Burden > India" --include-subcollections \
  --output scoped-results.json

zotero-go-cli --local --library-id 0 workflow pdf-status PARENT_ITEM_KEY
```

Search workflows strip punctuation before sending title or semantic queries, replacing dashes and other separators with spaces and collapsing whitespace. Original citation titles are preserved. Collection breadcrumbs are parsed before query cleanup.

Reference input can be a numbered Vancouver bibliography or a JSON array with explicit `title`, `doi`, `authors`, `year`, and optional `number` fields. Explicit JSON avoids ambiguity in unusual citation styles. Input order and duplicate entries are preserved. Matches require identifier or bibliographic evidence; semantic relevance alone does not establish identity. PDF verification checks file access and the PDF signature, not the paper's scientific content.

## Semantic search within a collection

```sh
zotero-go-cli --local --library-id 0 workflow semantic-search \
  "search for India burden articles in 3 Glaucoma collection"

zotero-go-cli --local --library-id 0 workflow semantic-search \
  "India disease burden" --collection-path "3 Glaucoma"
```

The natural-language form separates the topic from the collection scope. The command resolves the collection breadcrumb, includes descendant collections by default, searches ZOTseek in hybrid mode, and verifies result library identities and item keys against collection membership. Ambiguous collection names return candidate breadcrumbs; use the explicit path to disambiguate.

ZOTseek currently returns at most 100 ranked candidates without a collection-filter argument or pagination. Filtering those candidates by collection can miss relevant papers below the global candidate cutoff. Reports disclose this limit; results are discovery aids, not an exhaustive collection review. For a supplied reference list, use `workflow resolve-references` to verify each reference's identity.

## ZOTseek MCP

```sh
zotero-go-cli zotseek tools
zotero-go-cli zotseek tools --names-only
zotero-go-cli zotseek call search --arguments '{"query":"glaucoma screening","mode":"hybrid","max_results":20}'
```

Always discover the live schema first: tool names and arguments are server capabilities. The default endpoint is `http://localhost:23119/zotseek/mcp`. `ZOTSEEK_MCP_URL` or `--endpoint` overrides it. The protocol defaults to `2025-03-26`; the stateless `2026-07-28` mode is explicit and does not silently replace a failed handshake.

## Skill helpers

```sh
zotero-go-cli docx validate chapter.docx --minimum-fields 1 --json
zotero-go-cli docx validate edited.docx --baseline original.docx --preserve-baseline-citations --json
zotero-go-cli --local --library-id 0 doctor --strict --require-zotseek --json
zotero-go-cli skill check-package --root /path/to/zotero-use --json
zotero-go-cli skill check-updates --root /path/to/zotero-use --no-write --json
```

DOCX validation is read-only. Update checks report availability and never install an update. Skill-package checks can compare a mirror with `--compare-root`.

## Validation

```sh
go test ./...
go test -race ./...
go vet ./...
```

These commands run offline tests only. The inherited live read tests require `-tags integration`; write tests require `-tags integration_write` and a disposable library. Do not run write integration tests against a personal research library.

Private citation fixtures and PDFs remain outside the repository. Preserve the original upstream license and attribution when redistributing.
