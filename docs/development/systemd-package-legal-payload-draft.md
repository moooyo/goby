# E11 legal payload draft

Status: **incomplete draft; no package was created and no distribution approval is claimed**.

This [member manifest](systemd-package-legal-payload-draft.json) gives concrete
proposed destinations for existing original legal texts. It is bound only to
the retained E11 internal Linux amd64 systemd artifact. It does not cover a
future Programs successor or either independent M5 branch.

## Artifact and evidence scope

- Archive: `2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`,
  14,072,899 bytes.
- Executable: `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`,
  30,691,123 bytes.
- Build manifest: `851cf3b02372fb99b42f75f4548556ca3eb693ddf3290055e2363c16fb91f43a`.
  Its 57 assets total 1,106,130 bytes: 46 JavaScript chunks, one CSS file,
  one HTML file and nine WOFF2 fonts. The JSON draft references the complete
  asset pins, source archive, previous embedded manifest and retained readback.

These bindings come from the tracked
[package component inventory](systemd-package-component-inventory.json).
The [notice inventory](third-party-notices-inventory.json) supplies existing
legal-file hashes and lengths. This document copied those recorded values;
it did not rehash files or rerun any verification. The original five-member
archive still contains no standalone NOTICE or legal-text directory and is
unchanged.

## Proposed member selection

The first selection contains **19 original legal texts**: 15 root legal files
for eleven recorded Go modules, the Go runtime's root LICENSE and PATENTS, and
one complete existing notice for each of two font families. Every entry in the
JSON records the source path, existing SHA256/length, proposed destination and
planned mode 0644.

Destinations preserve the repository suffix below
`goby-linux-amd64-systemd/third_party/licenses/`. Copying and package integration
have not occurred. Go VERSION remains provenance evidence, not a selected legal
payload file. The additional age workflow license suffix remains a separately
pinned collection reference; this draft follows the root references in the
existing component table without making a legal-sufficiency determination.

| Recorded Go module | Version | Selected root files |
| --- | --- | --- |
| `filippo.io/age` | `v1.3.2` | `LICENSE` |
| `filippo.io/hpke` | `v0.4.0` | `LICENSE` |
| `github.com/coder/websocket` | `v1.8.15` | `LICENSE.txt` |
| `github.com/jackc/pgpassfile` | `v1.0.0` | `LICENSE` |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | `LICENSE` |
| `github.com/jackc/pgx/v5` | `v5.11.0` | `LICENSE` |
| `github.com/jackc/puddle/v2` | `v2.2.2` | `LICENSE` |
| `golang.org/x/crypto` | `v0.57.0` | `LICENSE`, `PATENTS` |
| `golang.org/x/sync` | `v0.23.0` | `LICENSE`, `PATENTS` |
| `golang.org/x/sys` | `v0.48.0` | `LICENSE`, `PATENTS` |
| `golang.org/x/text` | `v0.42.0` | `LICENSE`, `PATENTS` |

The runtime references are
`third_party/licenses/go/runtime/go1.27.1.linux-amd64/LICENSE` and `PATENTS`.
This selection does not claim that all collected Go toolchain command,
generator or test-data components are linked into Goby.

The font rows bind six Manrope and three Public Sans WOFF2 assets to their
recorded `@fontsource-variable/manrope@5.3.0` and
`@fontsource-variable/public-sans@5.3.0` package versions. Their complete retained
texts, including their original copyright notices, have proposed destinations:

- `goby-linux-amd64-systemd/third_party/licenses/npm/font-evidence/manrope-5.3.0-LICENSE`
- `goby-linux-amd64-systemd/third_party/licenses/npm/font-evidence/public-sans-5.3.0-LICENSE`

## Notice text draft

The following is proposed explanatory text for a future package-specific
`THIRD_PARTY_NOTICES.md`. It is not the contents of a shipped file, and the
JavaScript/glyph section is deliberately unresolved:

> This E11 material index identifies the recorded Go dependency modules, Go
> runtime references and embedded font assets listed in the accompanying member
> manifest. Their original legal texts are preserved at the listed
> `third_party/licenses/` paths and must remain complete when this material is
> assembled. Short labels do not replace those texts. The included JavaScript
> contribution map and relevant glyph material remain pending. The broader
> repository collection is supporting evidence, not a declaration that every
> collected dependency is present in this package. Goby's project-license
> decision remains pending; this draft neither supplies that license nor
> establishes distribution approval.

The module/version table above and the two font-family rows are the concrete
initial index entries. Add the resolved JavaScript/glyph entries before treating
this as a finalized package notice. Do not simply copy the broad repository
[THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) as an exact included-set
declaration.

## Finite remaining material work

1. Add artifact-specific JavaScript package/version contribution rows and their
   existing legal-file pins as they become supported. The two inspected saved
   build reports do not supply a source graph; capture it with the next required
   frontend build rather than rebuilding unchanged E11 to invent history.
2. Reuse the completed [fixed-commit MUI lookup](systemd-package-mui-source-correspondence.md)
   for all 73 recorded glyph paths. The 56 raw SVG candidates do not establish
   historical transformation or Google download versions; the relevant upstream
   material and final legal-text selection remain open.
3. Keep the existing project-license decision pending. No license is selected
   or new permission question introduced by this draft.
4. Finalize the short notice and connect the explicit member list to the
   existing packager and installation instructions in a separate change.
   Any materialized notice, copied text or new package needs its own member
   identity; the retained E11 archive hash cannot describe that new payload.

The existing 182-text collection and one VERSION file are reusable source
material, not 182 included components. Missing historical go.sum-only entries,
platform-optional npm packages and all 87 production-classified lock entries
are not automatically added to this draft's required set. PostgreSQL,
FFmpeg/ffprobe, Node.js and browser engines are not E11 executable payloads.

The initial 19-text selection wrote only these two draft files and ran no
validator, test, build or SSH operation. The later linked MUI source study has
its own remote correspondence report and scope. No legal text or package was
copied into a distribution, and no packager changed. Independent static review
of this draft found no material issue within its selected scope; the review
did not rehash source files. The draft does not complete M6, a source-level
SBOM or a legal review.
