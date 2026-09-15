# MUI source correspondence for the retained E11 glyphs

The [recorded 73 E11 single-path expressions](systemd-package-javascript-attribution.md)
now have matching path literals and component labels in the fixed MUI source
commit `809a7717b4c050ba3f69b75300689f07c050a16e`. The
[source correspondence report](systemd-package-mui-source-correspondence.json)
retains every selected source path, URL, length and hash, together with the
existing asset expression and cached-module bindings.

This closes the exact-version MUI source lookup for those 73 recorded rows.
It does not recover the original frontend module graph, the historical Google
download versions, or complete glyph coverage beyond the recorded expression form.

## Source identity and method

The official GitHub [tag reference](https://api.github.com/repos/mui/material-ui/git/ref/tags/v9.4.0)
resolved to annotated tag object `ce3c596185a73b3c85b7702c0297163096b2e6c4`.
That [tag object](https://api.github.com/repos/mui/material-ui/git/tags/ce3c596185a73b3c85b7702c0297163096b2e6c4)
resolved to the commit above. The saved reference observation records these API
results and URLs; it is not a retained byte-for-byte API response or a verified
tag signature. Raw source URLs use the resolved commit, not a mutable branch.

The captured files were transferred to `test-env` as a 442,368-byte archive,
SHA256 `afc58839085788068cfeba80733922f21046d88b9f5a0bdc6b9df44a29cf4787`.
The remote reader checked that binding and the existing 162,981-byte parent
attribution, SHA256
`4b0210264c47f2d8bc05393d765faadffa2d2f69bc8d3335c8e4d4f641d5ac73`.
It read the archive in memory, extracted JavaScript/JSX string literals and XML
path attributes, and compared their UTF-8 bytes with the retained path hashes.
It did not execute the MUI builder, JavaScript, package installation or SVG
optimization, and did not access application services or databases.

The original remote report is 205,826 bytes, SHA256
`c54085df474981d42d282dbf2a057d3fd2af41c4c3a3e7df5356fc5e074417ae`, at
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/mui-glyph-source-20260916-01/source-correspondence.json`.
The source archive is retained as `source-capture.tar` in that same directory.
The JSON in this repository is a copy of the remote report. Its Git blob SHA-1
fields describe the fetched content; they do not claim an independently
reconstructed Git object-membership proof.

## Results and limits

| Recorded group | Result | Meaning |
| --- | --- | --- |
| 56 `@mui/icons-material@9.4.0` rows | All 56 fixed-commit `lib/*.mjs` files match their recorded cached modules byte for byte; all 56 labels and path literals match E11 | Exact module-content correspondence; historical npm installation integrity and original build participation are not reconstructed |
| 17 `@mui/material@9.4.0` internal rows | All 17 fixed-commit `src/internal/svg-icons/*.js` labels and path literals match E11 | Source-literal correspondence; these JSX sources are not byte-identical to the compiled cache modules |
| 56 generated SVG candidates | All 56 filenames correspond to the recorded component labels under the inspected renaming rule; the files have retained hashes | Candidate input association from names and source layout, not proof of the historical transformation |
| 56 raw SVG candidate path comparisons | Zero have an exact path-literal match | SVG optimization was not rerun; literal differences do not decide geometric equivalence or historical derivation |

The complete, non-truncated `legacy` and `custom` filename inventories are
retained in the report. None contains a same-name component for these 56 public
rows. The report consequently uses `generated_candidate`, not an assertion
that the historical downloader and builder actually generated the observed file.
The 17 internal rows remain a separate category; their
[README](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-material/src/internal/svg-icons/README.md)
describes their internal use without supplying Google download versions.

For example, the fixed-commit
[AddRounded module](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-icons-material/lib/AddRounded.mjs)
has the same 130-byte path literal as its E11 expression. The corresponding
[SVG candidate](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-icons-material/material-icons/add_rounded_24px.svg)
has a 131-byte visible path ending in an additional `z`, plus a separate
transparent path. This observation explains one literal mismatch; no geometry
normalization or equivalence test was performed.

The pinned [package script](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-icons-material/package.json)
copies `lib/` for publication. Its source-generation command uses the
[builder](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-icons-material/builder.mjs)
and [renamer](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-icons-material/renameFilters/material-design-icons.mjs).
The builder optimizes SVGs and also incorporates legacy/custom sources. The
[download script](https://github.com/mui/material-ui/blob/809a7717b4c050ba3f69b75300689f07c050a16e/packages/mui-icons-material/scripts/download.mjs)
uses dynamic Google metadata. These source facts explain the candidate search,
but do not supply a missing per-glyph historical metadata snapshot.

## Bounded historical build-report lookup

A separate read-only SSH inspection covered these two exact saved JSON reports:

| Report | Bytes | SHA256 |
| --- | ---: | --- |
| `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/frontend-build.json` | 10,293 | `8a2716ac0fd55c20572bc85ee574b62b9bd23b268e58e1bd759bfad1506fc38f` |
| `/opt/goby-test/audit-fixes-upload-bd962cdf0722/frontend-02-report.json` | 736 | `2462ba8cdaffcf2f00939f784f21b867ad1a8fad6cdf99924dec2d9f60926c42` |

The first has the expected 57 asset records. Neither inspection found a field
identifying a retained source-module graph, sourcemap or metafile. This is a
bounded result about those two reports, not a search of the entire host or
proof that no other evidence exists. The original E11 archive inspection also
retains its empty map/metafile inventory. Its emitted chunk-import graph
remains distinct from a source-module contribution graph.

## Next material work

Preserve this completed lookup and the
[initial 19-text payload draft](systemd-package-legal-payload-draft.md).
The draft gives concrete destinations for recorded Go module/runtime and font
texts. JavaScript/helper contribution, the relevant upstream glyph material,
the existing owner license decision and actual package assembly remain open.

The next independently required frontend build should retain its actual source,
lock and configuration identities; bundler-produced chunk-to-module relations;
each participating module's path, package/version and source hash; separately
identified virtual/generated helpers; and all final asset hashes. Bind that
sidecar to the same build and keep private sourcemaps out of the package unless
explicitly selected. Do not rebuild unchanged E11 to manufacture historical
provenance or repeat this lookup without a concrete new source of evidence.

No Programs regression, frontend build, browser/media execution, candidate
transition, package modification or distribution occurred in this source study.
The shared heavy-work hold and the remaining M2-M6 gates are unchanged.
