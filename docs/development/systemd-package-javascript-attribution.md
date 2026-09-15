# JavaScript and glyph attribution for the retained systemd package

This is a bounded addition to the [E11 component inventory](systemd-package-component-inventory.md).
The frozen package contains 46 JavaScript chunks. A read-only inspection now
records their emitted import relationships and binds **73 single-path glyph
expressions** to exact files in two identified MUI 9.4.0 caches: **56** in
`@mui/icons-material` and **17** in `@mui/material/internal/svg-icons`.
The [machine-readable readback](systemd-package-javascript-attribution.json)
contains every chunk hash, import edge, glyph-data hash, matching module path,
module hash and byte length, including the unsuccessful same-name candidates.

This establishes content correspondence for the recorded glyph expressions.
It does not establish the complete JavaScript package/module contribution graph,
the historical integrity of the npm installation, or the original Google
download version for each glyph. Project licensing, external distribution,
legal payload inclusion and the complete M6 release gate remain open.

## Artifact and input bindings

The readback ran only on `ssh test-env`, reading the retained archive and
specifically identified package files. It extracted no files, installed no
dependency and invoked no JavaScript, Goby, browser, SQL, application HTTP,
build or service action. The original package and caches were not modified.
The resulting JSON was copied from SSH standard output; it contains asset and
dependency metadata, not application logs, environment files or user data.

| Frozen input | Bytes | SHA256 |
| --- | ---: | --- |
| E11 `private/source.tar.gz` | 2,708,124 | `e9143f909ac2383aefe4aa53209d7806d31ab74388881d0dce124b0fcfaed510` |
| E11 `artifacts/linux-amd64-systemd/manifest.json` | 173,346 | `851cf3b02372fb99b42f75f4548556ca3eb693ddf3290055e2363c16fb91f43a` |
| Cached `@mui/icons-material/package.json` | 1,416 | `5e2753c53d1f6657f867823d093316937edaf7339125f734a02ebe0b4c2bb5d1` |
| Cached `@mui/material/package.json` | 61,025 | `1d9a03a35343487ba3afefb5e0b9b88d620a9c9799292c251ce585d7fe58bd30` |

E11 denotes `/opt/goby-test/m6-systemd-package-20260915`. Both package-cache
paths are under
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/frontend-build/project/node_modules/`.
Their metadata hashes match the exact-version records already retained in the
[notice inventory](third-party-notices-inventory.json). Both declare version
`9.4.0`; the icon package's `import` export maps `./*` to `./*.mjs`.
The readback does not recompute npm tarball integrity or infer original build
provenance from the current contents of a mutable cache.

All 57 archive assets were freshly matched by path, bytes and SHA256 to the
frozen build manifest before inspecting the JavaScript text. The existing
component inventory separately establishes their exact byte presence in
executable SHA256
`7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1` and
package SHA256
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`.
This inspection reuses that binary/package binding; it does not perform another
binary extraction or runtime asset-serving check.

The resulting readback is 162,981 bytes, SHA256
`4b0210264c47f2d8bc05393d765faadffa2d2f69bc8d3335c8e4d4f641d5ac73`.
The bounded reader source is 7,263 bytes, SHA256
`04ab9698b30453c6cb2d599417aacf45dfa11336d656c5fd5c57eb21face80fb`.
It was supplied through SSH to `python3 -I -B`; no local verification ran.

## Emitted chunk relationships

The readback records **245 distinct static importer/specifier edges** and
**12 distinct dynamic importer/specifier edges** in the observed emitted
syntax. All targets are existing relative JavaScript asset paths. Following
these edges from `assets/index-CrH3sgOZ.js` reaches all 46 JavaScript chunks.
This is a graph of emitted files, not a graph of npm source modules.

The entry contains the 12 literal dynamic imports below. Their transitive
shared dependencies, exact hashes and complete static edges are in
`chunkGraph` in the JSON readback.

| Dynamic target under `assets/` | Dynamic target under `assets/` |
| --- | --- |
| `ApiKeysPage-IAvPPkeL.js` | `AuthPage-DgPGSmXK.js` |
| `BackupsPage-ByPmLtUR.js` | `DevicesPage-BUnyXrpT.js` |
| `LibrariesPage-DzYeQK7N.js` | `MetadataItemsPage-Cstdqs3l.js` |
| `ObservabilityPage-BqhF-E90.js` | `OverviewPage-CTgAhW_M.js` |
| `SessionsPage-Bmjj6BRI.js` | `SettingsPage-CfEXcD8M.js` |
| `TasksPage-DgPwvVhZ.js` | `UsersPage-DwpMS25J.js` |

The reader recognizes the concrete emitted `import {...} from`, side-effect
import and `import(literal)` forms. It does not parse arbitrary JavaScript or
execute imports. Reachability therefore describes these recorded literal
references; it does not prove that a page was visited or code executed.

The frozen source archive has no `.map`, metafile or `.vite/` member. Its only
frontend file outside `dist` is `web/admin/embedded.go`. None of the 46 scripts
contains a `sourceMappingURL`, `#region` or `node_modules/` marker. The current
[Vite configuration](../../web/admin/vite.config.ts) also sets `sourcemap: false`.
This does not establish that no supplemental build metadata exists elsewhere;
no artifact-specific module graph has been established for this retained build.
The E11 manifest explicitly records reuse of existing `web/admin/dist`.

## Matched single-path glyph expressions

For each row, the reader takes the literal `d` value and label from a concrete
single-`path` icon-factory expression in a hash-bound E11 chunk. It compares
the decoded path string, without geometric normalization, to the single path
in a cached `.mjs` module and checks that module's matching label literal.
All 73 recorded expressions have exactly one matching module among the two
specific cache locations checked. A matching path is stronger than a suggestive
chunk name; it is still a content match, not a reconstruction of the compiler's
module resolution or transformation history.

The JSON records each asset expression's offset in Unicode code points after
UTF-8 decoding, its UTF-8 byte length and the path-data SHA256/length. It does
not count arbitrary SVG, multipath, circle, rectangle, CSS or font glyphs.
The separate label check found no unmatched label in the reader's narrowly
defined single-path factory form; it is not a whole-bundle SVG coverage claim.

In the table, an icon-package label denotes
`@mui/icons-material@9.4.0/<Label>.mjs`; an internal label denotes
`@mui/material@9.4.0/internal/svg-icons/<Label>.mjs`. Every emitted asset path
has the `assets/` prefix. Module and asset hashes are recorded individually in
`singlePathGlyphs` and `chunkGraph` in the readback.

| Emitted asset | Icon-package labels | Material internal labels |
| --- | --- | --- |
| `AddRounded-n2_Zzdgq.js` | AddRounded | None |
| `ApiKeysPage-IAvPPkeL.js` | BlockOutlined, SelectAllRounded | None |
| `ArrowBackRounded-BDqMKZEU.js` | ArrowBackRounded | None |
| `ArrowForwardRounded-BR3QGpEb.js` | ArrowForwardRounded | None |
| `BackupsPage-ByPmLtUR.js` | UploadFileRounded, RestoreRounded | None |
| `CheckCircleOutlineRounded-WfE3mUNX.js` | CheckCircleOutlineRounded | None |
| `Checkbox-Rgp1fo9o.js` | None | CheckBoxOutlineBlank, CheckBox, IndeterminateCheckBox |
| `ContentCopyRounded-Cml4wPHD.js` | ContentCopyRounded | None |
| `DeleteOutlineRounded-R7hcH6to.js` | DeleteOutlineRounded | None |
| `DownloadRounded-DiXMpA-2.js` | DownloadRounded | None |
| `EditOutlined-OG4NtXRZ.js` | EditOutlined | None |
| `ExpandMoreRounded-0cfrPIS5.js` | ExpandMoreRounded | None |
| `FilterAltOffOutlined-DZdQGtjF.js` | FilterAltOffOutlined | None |
| `LibrariesPage-DzYeQK7N.js` | FolderOpenOutlined, ListAltRounded | None |
| `LockOutlined-vy-PNgce.js` | LockOutlined | None |
| `MetadataItemsPage-Cstdqs3l.js` | CompareArrowsRounded, KeyboardArrowDownRounded | None |
| `ObservabilityPage-BqhF-E90.js` | CloseRounded, ExpandLessRounded, SubjectRounded | None |
| `OverviewPage-CTgAhW_M.js` | DnsOutlined, RadioButtonUncheckedRounded | None |
| `RestartAltRounded-CRqEzGVl.js` | RestartAltRounded | None |
| `SaveOutlined-C7C87bDt.js` | SaveOutlined | None |
| `SearchRounded-8U4DQMcX.js` | SearchRounded | None |
| `SettingsPage-CfEXcD8M.js` | None | RadioButtonUnchecked, RadioButtonChecked |
| `ShieldOutlined-CNyZMv-R.js` | ShieldOutlined | None |
| `TablePagination-N9KqrRWz.js` | None | FirstPage, LastPage, KeyboardArrowLeft, KeyboardArrowRight |
| `TasksPage-DgPwvVhZ.js` | LibraryBooksOutlined, CancelOutlined, ErrorOutlineRounded, HelpOutlineRounded, HourglassTopRounded, PauseCircleOutlineRounded, PlayArrowRounded, ScheduleOutlined, EventAvailableOutlined | None |
| `TextField--oe-SvUr.js` | None | ArrowDropDown |
| `UsersPage-DwpMS25J.js` | PersonAddAltRounded, ManageAccountsOutlined, LockResetRounded | None |
| `components-CUlufDy-.js` | WavesRounded, RefreshRounded | SuccessOutlined, ReportProblemOutlined, ErrorOutline, InfoOutlined, Close, Cancel |
| `formFields-BsLZQDjO.js` | VisibilityOutlined, VisibilityOffOutlined | None |
| `index-CrH3sgOZ.js` | SpaceDashboardOutlined, PeopleOutlineRounded, VideoLibraryOutlined, SensorsRounded, DevicesOutlined, KeyRounded, PlaylistAddCheckRounded, SettingsOutlined, HistoryRounded, BackupOutlined, MenuRounded, ChevronRightRounded | Person |

The 17 internal matches matter: 15 have a same-name icon-package file with
different path data, and two have no same-name icon-package file. They must
not be attributed to `@mui/icons-material` merely because the rendered
component names resemble its public icons. The readback preserves these
negative candidates. The matching cached modules also record imports of
`react/jsx-runtime` and their relevant `createSvgIcon.mjs` helper. These are
cache-source import edges, not proof of which exact React/helper module bytes
survived bundling.

The existing exact-version MUI legal texts remain
[icons-material LICENSE](../../third_party/licenses/npm/@mui/icons-material@9.4.0/LICENSE)
and [material LICENSE](../../third_party/licenses/npm/@mui/material@9.4.0/LICENSE).
They carry the package MIT notices; the Google glyph source question below
remains a separate attribution item.

## Source intent, fonts and build tools

The current [package.json](../../web/admin/package.json),
[package-lock.json](../../web/admin/package-lock.json) and
[main.tsx](../../web/admin/src/main.tsx) provide source-intent edges, not a
replacement for the missing artifact-specific module graph. The existing
notice inventory binds the retained frontend `package.json` to SHA256
`0ed54d13744257efb8ecdba0004b9daa510e5005fc1e90657e5734456e034a7b`
and lockfile to
`3f68111eb71283903ac90550e340dd261a3bba33eb4ff2f65f21dd41ab0c9307`.
Neither E11's Go source checkpoint nor these lockfile pins alone identify
all modules contributing to each minified chunk.

| Locked package or group | Source or artifact evidence | Remaining boundary |
| --- | --- | --- |
| `@mui/icons-material@9.4.0` | 56 matched glyph expressions and exact cache modules above | Other package code and transformation history are not enumerated |
| `@mui/material@9.4.0` | 17 matched internal glyph expressions; current source imports UI components and styles | Complete component/helper module contribution is not enumerated |
| `@fontsource-variable/manrope@5.3.0` | Six WOFF2 assets exactly bound to package files and CSS references in the existing component inventory | No additional upstream font-release reconstruction is claimed |
| `@fontsource-variable/public-sans@5.3.0` | Three WOFF2 assets exactly bound to package files and CSS references in the existing component inventory | No additional upstream font-release reconstruction is claimed |
| `react@19.2.8`, `react-dom@19.2.8` | Direct dependencies; source imports `react` and `react-dom/client` | No source-module-to-emitted-byte mapping is established here |
| `@emotion/react@11.14.0`, `@emotion/styled@11.14.1` | Direct dependency declarations and existing exact-version notices | Declaration is not proof of the precise surviving modules |
| Vite 8.2.2, plugin-react 6.1.1, TypeScript 7.0.2, Playwright 1.63.0 | Build or test configuration and locked development dependencies | Tool role does not automatically prove that no generated helper code ships |
| Rolldown 1.2.7 | Locked tool dependency; E11 contains `rolldown-runtime-CbXtAM7H.js` (589 bytes, SHA256 `bbf7c5086ae414dc3f33777e3eebf16ea8631325b8bb4690d25091c03567f31a`) | The helper is payload; its filename does not establish the complete Rolldown source/version contribution |

The nine font source paths, hashes and OFL texts remain in the
[component inventory](systemd-package-component-inventory.json), with
[Manrope's retained notice](../../third_party/licenses/npm/font-evidence/manrope-5.3.0-LICENSE)
and [Public Sans's retained notice](../../third_party/licenses/npm/font-evidence/public-sans-5.3.0-LICENSE).
No font file or license was recollected in this pass. Likewise, the 87
production-classified npm lock entries are not relabeled as the included set,
and the 67 development-classified entries are not all declared absent from
generated assets.

## Official upstream glyph evidence

The official MUI [v9.4.0 package manifest](https://github.com/mui/material-ui/blob/v9.4.0/packages/mui-icons-material/package.json)
declares MIT and copies the repository license during publishing. Its
[v9.4.0 README](https://github.com/mui/material-ui/blob/v9.4.0/packages/mui-icons-material/README.md)
identifies the icons as converted Google Material Icons. This corroborates the
origin declaration already saved with the cached package; it does not identify
the exact upstream SVG file for each E11 expression.

The official [v9.4.0 download script](https://github.com/mui/material-ui/blob/v9.4.0/packages/mui-icons-material/scripts/download.mjs)
reads dynamic metadata from `fonts.google.com/metadata/icons` and uses each
returned icon version to form a `fonts.gstatic.com/s/i/materialicons.../v.../24px.svg`
URL. It writes named SVG files under `material-icons/`. The inspected script
does not pin a Google Git revision, metadata snapshot or complete per-glyph
version manifest. A MUI version tag therefore does not by itself recover those
Google input versions.

The [v9.4.0 builder](https://github.com/mui/material-ui/blob/v9.4.0/packages/mui-icons-material/builder.mjs)
optimizes SVG data, generates components and also copies `legacy/` and `custom/`
material. The download script has explicit overrides. A description that all
package glyphs are unmodified Google SVGs would be unsupported. The 17 internal
Material UI glyph modules also need their own source-path trace; they must not
inherit the icon package's downloader history without evidence.

Google's official [Material Icons licensing section](https://developers.google.com/fonts/docs/material_icons#licensing)
identifies Apache 2.0 for Material Icons. The inspected Google repository
[LICENSE at commit 40a7a292a79d9394157e1ea24f83d52d5e17c556](https://github.com/google/material-design-icons/blob/40a7a292a79d9394157e1ea24f83d52d5e17c556/LICENSE)
is an external source snapshot, **not** an established E11 upstream revision.
These official pages were read through the browser after the web provider was
unavailable. No new legal text was downloaded or added to the package, and this
source research does not resolve project licensing or approve distribution.

## Specific remaining material

1. **Artifact-specific module contribution.** Locate any already saved module
   graph for the original frontend build and bind it to these exact 57 assets.
   It must identify source module paths, package versions, input hashes and
   emitted chunk relationships. If that evidence was never retained, keep
   E11's complete graph unproven and capture it during the next independently
   required frontend build. Rebuilding unchanged E11 solely to invent a
   historical graph is not part of this work.
2. **The recorded glyphs' upstream inputs.** Starting with these 73 concrete
   module paths and path-data hashes, retain their exact-version MUI source
   counterparts and distinguish generated, legacy, custom and internal paths.
   For generated glyphs, bind the actual SVG or its content hash and original
   download version/metadata where available. A current Google SVG or the
   current repository license is not a substitute for a missing historical
   content relation.
3. **Scope beyond the matched form.** Before claiming complete glyph coverage,
   account for any SVG expression outside the single-path factory form and
   bind surviving helper/runtime modules. The present count remains a precise
   lower-bound contribution inventory.
4. **Legal payload.** Preserve the pending owner decision for Goby's license,
   resolve the applicable upstream material against the actual contribution
   set and carry the approved notices/texts in a future distribution package.
   The existing five-member E11 archive still contains no standalone project
   license, notices index or legal-text directory.

These are remaining evidence and packaging tasks. This document does not
trigger a new installation attempt, frontend build or service promotion.
