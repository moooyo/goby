# Actual components in the retained systemd package

This inventory covers the E11 `goby-linux-amd64-systemd.tar.gz` artifact,
14,072,899 bytes, SHA256
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`.
The [machine-readable inventory](systemd-package-component-inventory.json)
contains the exact member, executable, module, asset, font and evidence pins.
Its status is a bounded package inventory with explicit distribution gaps.
Project licensing remains awaiting the owner's decision; external distribution
and the complete M6 release gate remain open.

## Outer archive

An independent read of the retained archive matched all five regular members
to its pinned package manifest, including bytes, SHA256 and modes. The archive
also has one directory, `goby-linux-amd64-systemd/`, with mode 0755.

| Member inside the top directory | Bytes | Mode | Role |
| --- | ---: | --- | --- |
| `INSTALL.md` | 8,287 | 0644 | Installation instructions |
| `goby` | 30,691,123 | 0755 | Go executable with embedded administrator assets |
| `goby.env.example` | 2,292 | 0644 | Configuration template |
| `goby.service` | 625 | 0644 | Systemd service unit |
| `manifest.json` | 173,346 | 0644 | Build and asset provenance |

The outer `package-manifest.json` is 5,997 bytes, SHA256
`54639571120a36988ca7a91d1af45da44ff8c6b663db63c027c0411486fd5d2f`;
it is retained beside the archive rather than inside it. The archived
`manifest.json` is 173,346 bytes, SHA256
`851cf3b02372fb99b42f75f4548556ca3eb693ddf3290055e2363c16fb91f43a`.
The package retains its original `internal_candidate_only` label and
`projectLicenseResolved=false`.

No PostgreSQL, FFmpeg/ffprobe, Node.js or browser-engine executable is a payload
member. PostgreSQL 17 and FFmpeg/ffprobe remain separately installed prerequisites
described by the included INSTALL document. Go, Node, tar and gzip identities
in build provenance identify build tools; those executables are not payloads.

## Executable and recorded Go modules

The inspected executable is 30,691,123 bytes, SHA256
`7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`.
After matching the Go reader's metadata and SHA256 to E11's original tool pins,
the reader executed only `go version -m` against this file. Fresh build-info
records match the packaged manifest after accounting for the relocated output
pathname. The result records Go 1.27.1, Linux/amd64, `CGO_ENABLED=0`,
`GOAMD64=v1`, `-trimpath=true` and `-tags=goby_embed_admin`. ELF metadata is
64-bit x86-64 with no interpreter program header. The main module is
`github.com/moooyo/goby`, version `(devel)`; no replacement module is recorded.

All eleven dependency module/version/h1 records match the existing notice
inventory. The license identifiers below reproduce that inventory's
text-backed labels; they do not select a license for Goby.

| Recorded dependency module | Version | Existing license labels | Retained legal references |
| --- | --- | --- | --- |
| `filippo.io/age` | `v1.3.2` | BSD-3-Clause | [LICENSE](../../third_party/licenses/go/filippo.io/age@v1.3.2/LICENSE) |
| `filippo.io/hpke` | `v0.4.0` | BSD-3-Clause | [LICENSE](../../third_party/licenses/go/filippo.io/hpke@v0.4.0/LICENSE) |
| `github.com/coder/websocket` | `v1.8.15` | ISC | [LICENSE.txt](../../third_party/licenses/go/github.com/coder/websocket@v1.8.15/LICENSE.txt) |
| `github.com/jackc/pgpassfile` | `v1.0.0` | MIT | [LICENSE](../../third_party/licenses/go/github.com/jackc/pgpassfile@v1.0.0/LICENSE) |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | MIT | [LICENSE](../../third_party/licenses/go/github.com/jackc/pgservicefile@v0.0.0-20240606120523-5a60cdf6a761/LICENSE) |
| `github.com/jackc/pgx/v5` | `v5.11.0` | MIT | [LICENSE](../../third_party/licenses/go/github.com/jackc/pgx/v5@v5.11.0/LICENSE) |
| `github.com/jackc/puddle/v2` | `v2.2.2` | MIT | [LICENSE](../../third_party/licenses/go/github.com/jackc/puddle/v2@v2.2.2/LICENSE) |
| `golang.org/x/crypto` | `v0.57.0` | BSD-3-Clause | [LICENSE](../../third_party/licenses/go/golang.org/x/crypto@v0.57.0/LICENSE), [PATENTS](../../third_party/licenses/go/golang.org/x/crypto@v0.57.0/PATENTS) |
| `golang.org/x/sync` | `v0.23.0` | BSD-3-Clause | [LICENSE](../../third_party/licenses/go/golang.org/x/sync@v0.23.0/LICENSE), [PATENTS](../../third_party/licenses/go/golang.org/x/sync@v0.23.0/PATENTS) |
| `golang.org/x/sys` | `v0.48.0` | BSD-3-Clause | [LICENSE](../../third_party/licenses/go/golang.org/x/sys@v0.48.0/LICENSE), [PATENTS](../../third_party/licenses/go/golang.org/x/sys@v0.48.0/PATENTS) |
| `golang.org/x/text` | `v0.42.0` | BSD-3-Clause | [LICENSE](../../third_party/licenses/go/golang.org/x/text@v0.42.0/LICENSE), [PATENTS](../../third_party/licenses/go/golang.org/x/text@v0.42.0/PATENTS) |

The Go runtime and standard-library component is tied to Go 1.27.1. Its existing
[LICENSE](../../third_party/licenses/go/runtime/go1.27.1.linux-amd64/LICENSE),
[PATENTS](../../third_party/licenses/go/runtime/go1.27.1.linux-amd64/PATENTS)
and [VERSION](../../third_party/licenses/go/runtime/go1.27.1.linux-amd64/VERSION)
are retained and byte-checked. The broader toolchain reference collection has
38 legal files and one VERSION file, including tool and test-data notices.
This inventory does not assert that every component in that broad collection
is linked into this application. Source-file and symbol-level attribution
remain outside the proven granularity.

## Embedded administrator assets and fonts

The exact bundle contains **57 assets / 1,106,130 bytes**: 46 JavaScript chunks,
one CSS file, one HTML file and nine WOFF2 fonts. Each asset was read from E11's
pinned source archive, matched to the package build manifest by name, size and
SHA256, and located as an exact byte sequence in the inspected executable.
The source archive's tagged `web/admin/embedded.go` matches the build input
and contains the complete `all:dist` embed directive. All five recorded HTML
entry references match the retained HTML and their asset records.

The asset set is identical to the earlier embedded build manifest
`a732425002323e0e29a4ca7cf9e0fd517d773c1de027d810488173b75957e683`.
E11 reused that bundle and did not claim a new frontend build. Byte presence
and build-input binding do not test live HTTP asset serving or service startup.

| Included font family | Exact package | WOFF2 assets | Existing legal reference |
| --- | --- | ---: | --- |
| Manrope | `@fontsource-variable/manrope@5.3.0` | 6 | [OFL-1.1 and copyright](../../third_party/licenses/npm/font-evidence/manrope-5.3.0-LICENSE) |
| Public Sans | `@fontsource-variable/public-sans@5.3.0` | 3 | [OFL-1.1 and copyright](../../third_party/licenses/npm/font-evidence/public-sans-5.3.0-LICENSE) |

All nine package-font/source-font/build-asset hashes and sizes match the existing
font evidence; all nine fonts have references in the retained CSS. Per-font
subset, path, hash, size and legal-text pins are in the JSON inventory.

The 46 JavaScript chunk identities and retained bundle provenance are now
package-specific. A complete npm package/version contribution map is still
unproven: no source maps or artifact-specific bundler component graph are
retained with this bundle. The existing 87 production-classified npm entries
are not asserted to be its exact included set. Icon-like chunk names also do
not establish upstream glyph ownership or release provenance. The collected
`@mui/icons-material@9.4.0` package notice leaves the separately recorded
Material Icons upstream gap open.

## Collected legal material and missing payload

The later [JavaScript/glyph readback](systemd-package-javascript-attribution.md)
adds the emitted import graph for all 46 JavaScript chunks and 73 exact
single-path glyph correspondences: 56 to identified icons-material modules and
17 to Material UI internal SVG modules. It binds the exact retained assets and
current matching package files. Complete source-module contribution, historical
Google glyph inputs and coverage beyond that expression form remain open.

All **182 existing legal texts plus one VERSION file** were read from the
already retained notice collection and matched to its original hashes and
lengths. No new dependency, historical version, optional platform package or
legal text was collected. The existing
[notices](../../THIRD_PARTY_NOTICES.md) and
[inventory](third-party-notices-inventory.json) remain unchanged.

The five-member archive contains no standalone project license,
`THIRD_PARTY_NOTICES.md` or `third_party/licenses/` payload. The repository
collection must therefore remain distinguished from what this actual package
ships. The unresolved distribution work is precise:

1. Record the owner's pending project-license decision.
2. Define and carry the approved legal payload for the intended external package.
3. Establish the included JavaScript component/version map and resolve the
   relevant upstream icon glyph provenance.
4. Retain the distinction between module/toolchain-level evidence and any
   stronger source-level SBOM claim.

Unlinked historical go.sum-only records, platform-optional npm entries without
established bundle inclusion and every toolchain test-data notice are not
automatically added as components of this package.

## Read-only evidence

Raw read-only evidence remains in
`/opt/goby-test/m6-systemd-package-20260915/private/component-inventory-01/`
under directory mode 0700 and file mode 0600. It includes the original
`go version -m` stdout/stderr, exact manifest copies, member readback,
the corrected asset/legal reader source, per-asset byte evidence and the
183-file legal-copy readback. The JSON inventory gives every evidence descriptor.

The main component/notice binding receipt is SHA256
`9dc86f635ea01425ef0e9726ec251c2b61ac9a2f75cdeba65d5cbb5f0a5c6d62`.
The asset readback is
`2841b1d172c65d94cb8ccc15809587841118e063e38e7d93d08952480de828a5`;
the original legal-copy readback is
`72615d73cb87cc407df84992612bb194197defd22237e57d4880d0764e60e883`.

One initial asset-reader submission failed at Python parsing before execution.
Its retained note points to the original task diagnostic; the corrected reader
and successful readback are separate evidence. The phase-one reader was supplied
through SSH standard input, with command, tool and output receipts retained.

There was one Go metadata invocation and no Goby, PostgreSQL, FFmpeg, browser,
dependency installation, build, service or package-install action. Existing
artifacts were not modified. This result is not a complete SBOM, a legal
distribution approval, or installation/runtime acceptance.
