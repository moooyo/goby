# Third-party notices

Status: **INCOMPLETE; external distribution is not approved**. Prepared on
2026-09-14 for the currently locked dependencies and retained administrator
assets. Goby's own project license has not been selected. This file neither
creates a project license nor grants rights to distribute Goby.

The original third-party legal texts are preserved under
[third_party/licenses](third_party/licenses/). Retain those files with this
index. Their original copyright statements and terms are authoritative; the
short identifiers below are evidence labels, not replacements for the texts.
The [machine-readable inventory](docs/development/third-party-notices-inventory.json)
records exact versions, lock checksums, cache paths, legal-file hashes and sizes,
copyright-source references, font mappings and unresolved entries.

## Collected scope

- All 11 current go.mod requirements have exact-version legal texts. The
  inventory also retains 12 additional module/version entries from go.sum,
  distinguishing full module checksums from historical go.mod-only entries.
- All 154 npm lock paths are listed, including production, development and
  platform-optional entries. All 87 production-classified entries have legal
  texts; 110 entries in total have matching cached package metadata and texts.
  Classification follows the lockfile and does not determine which code is
  present in a generated JavaScript bundle.
- Go 1.27.1 build-toolchain notices are included with its LICENSE and PATENTS.
  This broad collection includes toolchain commands and test data; it does not
  claim that every included component is linked into the Goby executable.
- Nine actual WOFF2 assets are bound by SHA256 and byte length to the retained
  frontend build and the exact Fontsource package files. The two font-family
  licenses and copyright notices are retained below.

Collection read existing test-env metadata, legal files and saved build evidence.
It installed nothing and ran no builds, browsers, services or application code.
No reference-client source or database was read. Go h1 values are recorded and,
where available, compared with cache ziphash metadata; full module h1 values and
npm tarball integrity were not recomputed. This is not a complete binary SBOM.

## Go module notices

The requirement column separates direct/indirect go.mod requirements from
additional go.sum entries. A missing or subtree-only license remains an open
scope; the license of another version is not substituted.

| Module | Version | Requirement | Text-evidenced license identifiers | Preserved legal text |
| --- | --- | --- | --- | --- |
| c2sp.org/CCTV/age | v0.0.0-20260829155415-4448f2097b2d | go.sum-only | BSD-3-Clause | [internal/LICENSE](third_party/licenses/go/c2sp.org/CCTV/age@v0.0.0-20260829155415-4448f2097b2d/internal/LICENSE) — scope incomplete |
| filippo.io/age | v1.3.2 | direct | BSD-3-Clause | [.github/workflows/LICENSE.suffix.txt](third_party/licenses/go/filippo.io/age@v1.3.2/.github/workflows/LICENSE.suffix.txt); [LICENSE](third_party/licenses/go/filippo.io/age@v1.3.2/LICENSE) |
| filippo.io/hpke | v0.4.0 | indirect | BSD-3-Clause | [LICENSE](third_party/licenses/go/filippo.io/hpke@v0.4.0/LICENSE) |
| github.com/coder/websocket | v1.8.15 | direct | ISC | [LICENSE.txt](third_party/licenses/go/github.com/coder/websocket@v1.8.15/LICENSE.txt) |
| github.com/davecgh/go-spew | v1.1.0 | go.sum-only | Unresolved | Not present in inspected cache — scope incomplete |
| github.com/davecgh/go-spew | v1.1.1 | go.sum-only | ISC | [LICENSE](third_party/licenses/go/github.com/davecgh/go-spew@v1.1.1/LICENSE) |
| github.com/jackc/pgpassfile | v1.0.0 | indirect | MIT | [LICENSE](third_party/licenses/go/github.com/jackc/pgpassfile@v1.0.0/LICENSE) |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | indirect | MIT | [LICENSE](third_party/licenses/go/github.com/jackc/pgservicefile@v0.0.0-20240606120523-5a60cdf6a761/LICENSE) |
| github.com/jackc/pgx/v5 | v5.11.0 | direct | MIT | [LICENSE](third_party/licenses/go/github.com/jackc/pgx/v5@v5.11.0/LICENSE) |
| github.com/jackc/puddle/v2 | v2.2.2 | indirect | MIT | [LICENSE](third_party/licenses/go/github.com/jackc/puddle/v2@v2.2.2/LICENSE) |
| github.com/pmezard/go-difflib | v1.0.0 | go.sum-only | BSD-3-Clause | [LICENSE](third_party/licenses/go/github.com/pmezard/go-difflib@v1.0.0/LICENSE) |
| github.com/stretchr/objx | v0.1.0 | go.sum-only | Unresolved | Not present in inspected cache — scope incomplete |
| github.com/stretchr/testify | v1.11.1 | go.sum-only | MIT | [LICENSE](third_party/licenses/go/github.com/stretchr/testify@v1.11.1/LICENSE) |
| github.com/stretchr/testify | v1.3.0 | go.sum-only | Unresolved | Not present in inspected cache — scope incomplete |
| github.com/stretchr/testify | v1.7.0 | go.sum-only | Unresolved | Not present in inspected cache — scope incomplete |
| golang.org/x/crypto | v0.57.0 | direct | BSD-3-Clause | [LICENSE](third_party/licenses/go/golang.org/x/crypto@v0.57.0/LICENSE); [PATENTS](third_party/licenses/go/golang.org/x/crypto@v0.57.0/PATENTS) |
| golang.org/x/sync | v0.23.0 | indirect | BSD-3-Clause | [LICENSE](third_party/licenses/go/golang.org/x/sync@v0.23.0/LICENSE); [PATENTS](third_party/licenses/go/golang.org/x/sync@v0.23.0/PATENTS) |
| golang.org/x/sys | v0.48.0 | direct | BSD-3-Clause | [LICENSE](third_party/licenses/go/golang.org/x/sys@v0.48.0/LICENSE); [PATENTS](third_party/licenses/go/golang.org/x/sys@v0.48.0/PATENTS) |
| golang.org/x/term | v0.46.0 | go.sum-only | BSD-3-Clause | [LICENSE](third_party/licenses/go/golang.org/x/term@v0.46.0/LICENSE); [PATENTS](third_party/licenses/go/golang.org/x/term@v0.46.0/PATENTS) |
| golang.org/x/text | v0.42.0 | indirect | BSD-3-Clause | [LICENSE](third_party/licenses/go/golang.org/x/text@v0.42.0/LICENSE); [PATENTS](third_party/licenses/go/golang.org/x/text@v0.42.0/PATENTS) |
| gopkg.in/check.v1 | v0.0.0-20161208181325-20d25e280405 | go.sum-only | Unresolved | Not present in inspected cache — scope incomplete |
| gopkg.in/yaml.v3 | v3.0.0-20200313102051-9f266ea9e77c | go.sum-only | Unresolved | Not present in inspected cache — scope incomplete |
| gopkg.in/yaml.v3 | v3.0.1 | go.sum-only | MIT AND Apache-2.0 | [LICENSE](third_party/licenses/go/gopkg.in/yaml.v3@v3.0.1/LICENSE); [NOTICE](third_party/licenses/go/gopkg.in/yaml.v3@v3.0.1/NOTICE) |

The YAML v3 LICENSE assigns MIT to its named ported files and Apache-2.0 to
remaining files; this is a mixed-license distribution, not an OR choice. Its
original LICENSE and NOTICE are preserved. The complete
[Apache-2.0 terms](third_party/licenses/go/runtime/go1.27.1.linux-amd64/src/cmd/vendor/github.com/google/pprof/LICENSE)
are supplied as a separate standard license text already present in the exact
Go toolchain. This does not attribute pprof code or copyright to YAML.

## Go toolchain and runtime

The saved selected-build record identifies Go 1.27.1 and application binary
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`.
Toolchain VERSION and all 38 collected legal files were compared with the
matching existing cache. See the
[Go LICENSE](third_party/licenses/go/runtime/go1.27.1.linux-amd64/LICENSE),
[PATENTS](third_party/licenses/go/runtime/go1.27.1.linux-amd64/PATENTS),
[VERSION](third_party/licenses/go/runtime/go1.27.1.linux-amd64/VERSION) and the
complete file/source inventory in the machine-readable document. This records
build provenance, not the identity or acceptance of a current service.

## Administrator JavaScript and build dependencies

The following entries have original legal files from a cached package matching
both locked name and version. Package license declarations remain separate from
bundled third-party notices: for example, tool packages may contain several
licenses in one original NOTICE or license collection. Follow all linked texts,
not only the package's short declaration.

| Package | Version | Lock class | Declared license | Preserved legal text |
| --- | --- | --- | --- | --- |
| @babel/code-frame | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/code-frame@7.29.7/LICENSE) |
| @babel/generator | 7.29.8 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/generator@7.29.8/LICENSE) |
| @babel/helper-globals | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/helper-globals@7.29.7/LICENSE) |
| @babel/helper-module-imports | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/helper-module-imports@7.29.7/LICENSE) |
| @babel/helper-string-parser | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/helper-string-parser@7.29.7/LICENSE) |
| @babel/helper-validator-identifier | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/helper-validator-identifier@7.29.7/LICENSE) |
| @babel/parser | 7.29.8 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/parser@7.29.8/LICENSE) |
| @babel/runtime | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/runtime@7.29.7/LICENSE) |
| @babel/template | 7.29.7 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/template@7.29.7/LICENSE) |
| @babel/traverse | 7.29.8 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/traverse@7.29.8/LICENSE) |
| @babel/types | 7.29.8 | production | MIT | [LICENSE](third_party/licenses/npm/@babel/types@7.29.8/LICENSE) |
| @emotion/babel-plugin | 11.13.5 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/babel-plugin@11.13.5/LICENSE) |
| @emotion/cache | 11.14.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/cache@11.14.0/LICENSE) |
| @emotion/hash | 0.9.2 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/hash@0.9.2/LICENSE) |
| @emotion/is-prop-valid | 1.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/is-prop-valid@1.4.0/LICENSE) |
| @emotion/memoize | 0.9.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/memoize@0.9.0/LICENSE) |
| @emotion/react | 11.14.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/react@11.14.0/LICENSE) |
| @emotion/serialize | 1.3.3 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/serialize@1.3.3/LICENSE) |
| @emotion/sheet | 1.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/sheet@1.4.0/LICENSE) |
| @emotion/styled | 11.14.1 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/styled@11.14.1/LICENSE) |
| @emotion/unitless | 0.10.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/unitless@0.10.0/LICENSE) |
| @emotion/use-insertion-effect-with-fallbacks | 1.2.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/use-insertion-effect-with-fallbacks@1.2.0/LICENSE) |
| @emotion/utils | 1.4.2 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/utils@1.4.2/LICENSE) |
| @emotion/weak-memoize | 0.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@emotion/weak-memoize@0.4.0/LICENSE) |
| @fontsource-variable/manrope | 5.3.0 | production | OFL-1.1 | [LICENSE](third_party/licenses/npm/@fontsource-variable/manrope@5.3.0/LICENSE) |
| @fontsource-variable/public-sans | 5.3.0 | production | OFL-1.1 | [LICENSE](third_party/licenses/npm/@fontsource-variable/public-sans@5.3.0/LICENSE) |
| @jridgewell/gen-mapping | 0.3.13 | production | MIT | [LICENSE](third_party/licenses/npm/@jridgewell/gen-mapping@0.3.13/LICENSE) |
| @jridgewell/resolve-uri | 3.1.2 | production | MIT | [LICENSE](third_party/licenses/npm/@jridgewell/resolve-uri@3.1.2/LICENSE) |
| @jridgewell/sourcemap-codec | 1.6.0 | production | MIT | [LICENSE](third_party/licenses/npm/@jridgewell/sourcemap-codec@1.6.0/LICENSE) |
| @jridgewell/trace-mapping | 0.3.31 | production | MIT | [LICENSE](third_party/licenses/npm/@jridgewell/trace-mapping@0.3.31/LICENSE) |
| @mui/core-downloads-tracker | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/core-downloads-tracker@9.4.0/LICENSE) |
| @mui/icons-material | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/icons-material@9.4.0/LICENSE) |
| @mui/material | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/material@9.4.0/LICENSE) |
| @mui/private-theming | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/private-theming@9.4.0/LICENSE) |
| @mui/styled-engine | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/styled-engine@9.4.0/LICENSE) |
| @mui/system | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/system@9.4.0/LICENSE) |
| @mui/types | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/types@9.4.0/LICENSE) |
| @mui/utils | 9.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/@mui/utils@9.4.0/LICENSE) |
| @oxc-project/types | 0.148.0 | development | MIT | [LICENSE](third_party/licenses/npm/@oxc-project/types@0.148.0/LICENSE) |
| @playwright/test | 1.63.0 | development | Apache-2.0 | [LICENSE](third_party/licenses/npm/@playwright/test@1.63.0/LICENSE); [NOTICE](third_party/licenses/npm/@playwright/test@1.63.0/NOTICE) |
| @popperjs/core | 2.11.8 | production | MIT | [LICENSE.md](third_party/licenses/npm/@popperjs/core@2.11.8/LICENSE.md) |
| @rolldown/pluginutils | 1.0.1 | development | MIT | [LICENSE](third_party/licenses/npm/@rolldown/pluginutils@1.0.1/LICENSE) |
| @types/node | 26.5.0 | development | MIT | [LICENSE](third_party/licenses/npm/@types/node@26.5.0/LICENSE) |
| @types/parse-json | 4.0.2 | production | MIT | [LICENSE](third_party/licenses/npm/@types/parse-json@4.0.2/LICENSE) |
| @types/prop-types | 15.7.15 | production | MIT | [LICENSE](third_party/licenses/npm/@types/prop-types@15.7.15/LICENSE) |
| @types/react | 19.2.18 | production | MIT | [LICENSE](third_party/licenses/npm/@types/react@19.2.18/LICENSE) |
| @types/react-dom | 19.2.7 | development | MIT | [LICENSE](third_party/licenses/npm/@types/react-dom@19.2.7/LICENSE) |
| @types/react-transition-group | 4.4.12 | production | MIT | [LICENSE](third_party/licenses/npm/@types/react-transition-group@4.4.12/LICENSE) |
| @typescript/typescript-linux-x64 | 7.0.2 | development | Apache-2.0 | [LICENSE](third_party/licenses/npm/@typescript/typescript-linux-x64@7.0.2/LICENSE); [NOTICE.txt](third_party/licenses/npm/@typescript/typescript-linux-x64@7.0.2/NOTICE.txt) |
| @vitejs/plugin-react | 6.1.1 | development | MIT | [LICENSE](third_party/licenses/npm/@vitejs/plugin-react@6.1.1/LICENSE) |
| babel-plugin-macros | 3.1.0 | production | MIT | [LICENSE](third_party/licenses/npm/babel-plugin-macros@3.1.0/LICENSE) |
| callsites | 3.1.0 | production | MIT | [license](third_party/licenses/npm/callsites@3.1.0/license) |
| clsx | 2.1.1 | production | MIT | [license](third_party/licenses/npm/clsx@2.1.1/license) |
| convert-source-map | 1.9.0 | production | MIT | [LICENSE](third_party/licenses/npm/convert-source-map@1.9.0/LICENSE) |
| cosmiconfig | 7.1.0 | production | MIT | [LICENSE](third_party/licenses/npm/cosmiconfig@7.1.0/LICENSE) |
| yaml | 1.10.3 | production | ISC | [LICENSE](third_party/licenses/npm/cosmiconfig/nested/yaml@1.10.3/LICENSE) |
| csstype | 3.2.3 | production | MIT | [LICENSE](third_party/licenses/npm/csstype@3.2.3/LICENSE) |
| debug | 4.4.3 | production | MIT | [LICENSE](third_party/licenses/npm/debug@4.4.3/LICENSE) |
| detect-libc | 2.1.2 | development | Apache-2.0 | [LICENSE](third_party/licenses/npm/detect-libc@2.1.2/LICENSE) |
| dom-helpers | 5.2.1 | production | MIT | [LICENSE](third_party/licenses/npm/dom-helpers@5.2.1/LICENSE) |
| error-ex | 1.3.4 | production | MIT | [LICENSE](third_party/licenses/npm/error-ex@1.3.4/LICENSE) |
| es-errors | 1.3.0 | production | MIT | [LICENSE](third_party/licenses/npm/es-errors@1.3.0/LICENSE) |
| escape-string-regexp | 4.0.0 | production | MIT | [license](third_party/licenses/npm/escape-string-regexp@4.0.0/license) |
| fdir | 6.5.0 | development | MIT | [LICENSE](third_party/licenses/npm/fdir@6.5.0/LICENSE) |
| find-root | 1.1.0 | production | MIT | [LICENSE.md](third_party/licenses/npm/find-root@1.1.0/LICENSE.md) |
| function-bind | 1.1.2 | production | MIT | [LICENSE](third_party/licenses/npm/function-bind@1.1.2/LICENSE) |
| hasown | 2.0.4 | production | MIT | [LICENSE](third_party/licenses/npm/hasown@2.0.4/LICENSE) |
| hoist-non-react-statics | 3.3.2 | production | BSD-3-Clause | [LICENSE.md](third_party/licenses/npm/hoist-non-react-statics@3.3.2/LICENSE.md) |
| react-is | 16.13.1 | production | MIT | [LICENSE](third_party/licenses/npm/hoist-non-react-statics/nested/react-is@16.13.1/LICENSE) |
| import-fresh | 3.3.1 | production | MIT | [license](third_party/licenses/npm/import-fresh@3.3.1/license) |
| is-arrayish | 0.2.1 | production | MIT | [LICENSE](third_party/licenses/npm/is-arrayish@0.2.1/LICENSE) |
| is-core-module | 2.16.2 | production | MIT | [LICENSE](third_party/licenses/npm/is-core-module@2.16.2/LICENSE) |
| js-tokens | 4.0.0 | production | MIT | [LICENSE](third_party/licenses/npm/js-tokens@4.0.0/LICENSE) |
| jsesc | 3.1.0 | production | MIT | [LICENSE-MIT.txt](third_party/licenses/npm/jsesc@3.1.0/LICENSE-MIT.txt) |
| json-parse-even-better-errors | 2.3.1 | production | MIT | [LICENSE.md](third_party/licenses/npm/json-parse-even-better-errors@2.3.1/LICENSE.md) |
| lightningcss | 1.33.0 | development | MPL-2.0 | [LICENSE](third_party/licenses/npm/lightningcss@1.33.0/LICENSE) |
| lightningcss-linux-x64-gnu | 1.33.0 | development | MPL-2.0 | [LICENSE](third_party/licenses/npm/lightningcss-linux-x64-gnu@1.33.0/LICENSE) |
| lightningcss-linux-x64-musl | 1.33.0 | development | MPL-2.0 | [LICENSE](third_party/licenses/npm/lightningcss-linux-x64-musl@1.33.0/LICENSE) |
| lines-and-columns | 1.2.4 | production | MIT | [LICENSE](third_party/licenses/npm/lines-and-columns@1.2.4/LICENSE) |
| loose-envify | 1.4.0 | production | MIT | [LICENSE](third_party/licenses/npm/loose-envify@1.4.0/LICENSE) |
| ms | 2.1.3 | production | MIT | [license.md](third_party/licenses/npm/ms@2.1.3/license.md) |
| nanoid | 3.3.18 | development | MIT | [LICENSE](third_party/licenses/npm/nanoid@3.3.18/LICENSE) |
| object-assign | 4.1.1 | production | MIT | [license](third_party/licenses/npm/object-assign@4.1.1/license) |
| parent-module | 1.0.1 | production | MIT | [license](third_party/licenses/npm/parent-module@1.0.1/license) |
| parse-json | 5.2.0 | production | MIT | [license](third_party/licenses/npm/parse-json@5.2.0/license) |
| path-parse | 1.0.7 | production | MIT | [LICENSE](third_party/licenses/npm/path-parse@1.0.7/LICENSE) |
| path-type | 4.0.0 | production | MIT | [license](third_party/licenses/npm/path-type@4.0.0/license) |
| picocolors | 1.1.1 | production | ISC | [LICENSE](third_party/licenses/npm/picocolors@1.1.1/LICENSE) |
| picomatch | 4.0.7 | development | MIT | [LICENSE](third_party/licenses/npm/picomatch@4.0.7/LICENSE) |
| playwright | 1.63.0 | development | Apache-2.0 | [LICENSE](third_party/licenses/npm/playwright@1.63.0/LICENSE); [NOTICE](third_party/licenses/npm/playwright@1.63.0/NOTICE); [ThirdPartyNotices.txt](third_party/licenses/npm/playwright@1.63.0/ThirdPartyNotices.txt) |
| playwright-core | 1.63.0 | development | Apache-2.0 | [LICENSE](third_party/licenses/npm/playwright-core@1.63.0/LICENSE); [NOTICE](third_party/licenses/npm/playwright-core@1.63.0/NOTICE); [ThirdPartyNotices.txt](third_party/licenses/npm/playwright-core@1.63.0/ThirdPartyNotices.txt) |
| postcss | 8.5.28 | development | MIT | [LICENSE](third_party/licenses/npm/postcss@8.5.28/LICENSE) |
| prop-types | 15.8.1 | production | MIT | [LICENSE](third_party/licenses/npm/prop-types@15.8.1/LICENSE) |
| react-is | 16.13.1 | production | MIT | [LICENSE](third_party/licenses/npm/prop-types/nested/react-is@16.13.1/LICENSE) |
| react | 19.2.8 | production | MIT | [LICENSE](third_party/licenses/npm/react@19.2.8/LICENSE) |
| react-dom | 19.2.8 | production | MIT | [LICENSE](third_party/licenses/npm/react-dom@19.2.8/LICENSE) |
| react-is | 19.2.8 | production | MIT | [LICENSE](third_party/licenses/npm/react-is@19.2.8/LICENSE) |
| react-transition-group | 4.4.5 | production | BSD-3-Clause | [LICENSE](third_party/licenses/npm/react-transition-group@4.4.5/LICENSE) |
| resolve | 1.22.12 | production | MIT | [LICENSE](third_party/licenses/npm/resolve@1.22.12/LICENSE) |
| resolve-from | 4.0.0 | production | MIT | [license](third_party/licenses/npm/resolve-from@4.0.0/license) |
| rolldown | 1.2.7 | development | MIT | [LICENSE](third_party/licenses/npm/rolldown@1.2.7/LICENSE); [THIRD-PARTY-LICENSE](third_party/licenses/npm/rolldown@1.2.7/THIRD-PARTY-LICENSE) |
| scheduler | 0.27.0 | production | MIT | [LICENSE](third_party/licenses/npm/scheduler@0.27.0/LICENSE) |
| source-map | 0.5.7 | production | BSD-3-Clause | [LICENSE](third_party/licenses/npm/source-map@0.5.7/LICENSE) |
| source-map-js | 1.2.1 | development | BSD-3-Clause | [LICENSE](third_party/licenses/npm/source-map-js@1.2.1/LICENSE) |
| stylis | 4.2.0 | production | MIT | [LICENSE](third_party/licenses/npm/stylis@4.2.0/LICENSE) |
| supports-preserve-symlinks-flag | 1.0.0 | production | MIT | [LICENSE](third_party/licenses/npm/supports-preserve-symlinks-flag@1.0.0/LICENSE) |
| tinyglobby | 0.2.17 | development | MIT | [LICENSE](third_party/licenses/npm/tinyglobby@0.2.17/LICENSE) |
| typescript | 7.0.2 | development | Apache-2.0 | [LICENSE](third_party/licenses/npm/typescript@7.0.2/LICENSE); [NOTICE.txt](third_party/licenses/npm/typescript@7.0.2/NOTICE.txt) |
| undici-types | 8.9.0 | development | MIT | [LICENSE](third_party/licenses/npm/undici-types@8.9.0/LICENSE) |
| vite | 8.2.2 | development | MIT | [LICENSE.md](third_party/licenses/npm/vite@8.2.2/LICENSE.md) |

## Actual font assets

| Family / exact package | Built assets | License and original copyright notice |
| --- | --- | --- |
| Manrope / @fontsource-variable/manrope 5.3.0 | Six normal variable-weight WOFF2 subsets | [SIL Open Font License 1.1 and copyright](third_party/licenses/npm/font-evidence/manrope-5.3.0-LICENSE): Copyright 2019 The Manrope Project Authors (https://github.com/sharanda/manrope) |
| Public Sans / @fontsource-variable/public-sans 5.3.0 | Three normal variable-weight WOFF2 subsets | [SIL Open Font License 1.1 and copyright](third_party/licenses/npm/font-evidence/public-sans-5.3.0-LICENSE): Copyright 2015 The Public Sans Project Authors (https://github.com/uswds/public-sans); retain the complete package notice, including its additional italic-font attribution |

These are self-hosted assets from the retained 57-file administrator build, not
remote font-service requests. The inventory preserves each actual source/dist
hash and byte length. Package upstream URLs are recorded declarations; no wider
font release history or naming-rights conclusion is inferred.

## Explicit unresolved scope

1. **Goby project license:** no project license is selected or created. The
   project owner's licensing decision is required before external distribution.
2. **Go historical entries:** six go.sum versions have only cached go.mod
   metadata and no exact-version legal text. CCTV/age has only internal/LICENSE;
   the subtree's BSD text is not assigned to its entire module.
3. **Optional npm entries:** 42 platform-optional packages are absent from the
   inspected caches. Two installed @rolldown/binding-linux-x64 packages at
   version 1.2.7 declare MIT but contain no legal file under the recorded search
   policy. Their names, versions and declarations remain in the inventory;
   original legal-text evidence is incomplete.
4. **Material Icons origin:** @mui/icons-material 9.4.0 includes the retained
   package MIT notice. Its README points to Google Material Icons, but a
   separately identified upstream glyph license and release provenance were
   not established from inspected files. Do not infer that the package notice
   alone resolves every upstream-content obligation.
5. **Actual distribution content:** a future package must identify linked and
   bundled components and resolve the relevant open rows. PostgreSQL,
   FFmpeg/ffprobe, Node.js and browser-engine executables are not copied by this
   increment; packaging any of them requires their exact-build notices and
   licensing review. No general codec, patent or redistribution conclusion is
   made here.

No missing notice was invented or taken from an unverified alternate version.
This increment supplies traceable original texts and a bounded inventory; it
does not close the project-license, packaging, or complete M6 release gate.
