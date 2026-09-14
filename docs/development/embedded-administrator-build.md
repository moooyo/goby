# Embedded administrator build

Historical checkpoint: **focused remote checks, actual amd64 embedded build,
arm64 cross-build and artifact/resource closure passed**, 2026-09-14.
This is partial M6 evidence.
At that build checkpoint neither binary had run as a service or been deployed.
The later [native catalog restart](native-catalog-restart-verification.json)
passed two amd64 `goby serve` invocations in an isolated fixture. Both served
the exact 800-byte embedded index with no external dashboard directory override,
and stopped normally. This is a root application profile with seven media files;
it is not a deployment or a native arm64 result. Full regression
and candidate admission for the changed version remain separate requirements;
the selected baseline's earlier 2,270-test result is not inherited. Native
arm64, OCI, GPU, license and distribution obligations remain open.

The current [internal systemd package increment](systemd-package-plan.md)
follows the accepted M5 checkpoint `5faf854`. Its optional `--package systemd`
implementation, embedded-compatible environment example and manual installation
instructions have passed [current package build verification](systemd-package-build-verification.json):
three actual builds, seven focused top-level tests (26 including subtests),
26 package guards, independent review and artifact/resource closure. The
package and legacy amd64 binaries are byte-identical; arm64 remains a cross-build.
The existing unit is unchanged. Actual nonroot installation and normal stop/start
acceptance remain pending. The historical results below retain their old scope.

## Recorded remote results

The [verification checkpoint](embedded-administrator-verification.json) binds
the prepared 911-file source workspace, 57 actual production assets totaling
1,106,130 bytes, and 2,119 copied module-cache files totaling 61,499,652 bytes.
Remote `gofmt` results were returned to the working tree. The existing selected
frontend bundle was reused with its provenance; packaging it does not claim a
new TypeScript build.

The focused checks receipt SHA256 is
`b184b142d57cff7f7b27d5c088f90c28c2f297a6cd72176d69d4f3b662ca9cfe`.
It records 22 handler tests/subtests, two ordinary provider tests and two tagged
embedded provider tests, all with race instrumentation, no failures and no
skips. The actual Linux amd64 embedded binary is 30,678,868 bytes, SHA256
`59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312`.
Its manifest records an 848-file source inventory, SHA256
`11a529344454fa059ed38d850b12fda750ecbcdf264166be61e291edfed27aca`.
All 57 assets and five HTML entry references were independently checked.

The guards receipt SHA256 is
`579e9cf11385b06f96a75b20b531d6f598a42247ca6a89fb1a2f99e24c85d612`.
All 11 fixed negative cases passed: seven reject before invoking Go; four use
synthetic Go executables to change source/assets and prove that completion
manifests are refused. Separately, both ordinary provider race tests passed
without `dist`, and an actual tagged compile without `dist` failed as expected.
The synthetic cases are guard evidence, not additional product builds.

The actual Linux arm64 cross-build produced 28,598,324 bytes, SHA256
`11e6e4c0bfdfb1cb4ed9c6debf6f2b7cdf272e2e4abefdd6a35b32ee8870aa3d`;
its manifest SHA256 is
`b2cf0936c12d676951dd005f9a2e780961914428adb6663de1c3bf38ca97dffd`.
ELF machine 183 was checked. No native arm64 runtime was executed.

Finalization at
`/opt/goby-test/m6-embedded-20260914/finalization.json`, SHA256
`7de4a1f729c1f1199b19f5daa44dbae69aab5e8dc02ad4d95810743490382abf`,
records `artifacts_preserved_and_owned_build_tmpfs_closed`. Both binaries and
manifests are retained under `artifacts/linux-{arch}` in that scope. The full
911-file source archive was reread member by member: `source.tar.gz` is
2,710,656 bytes, SHA256
`10d88202cc2a06071b3a216330f70e92512f292f51c46764a4230aa136639dff`.
RAM command logs were saved under `retained-command-logs`; ordinary unmount
closed the owned M6 tmpfs and left its empty underlying directory. Protected
state and every frozen source module-cache file remained unchanged.

## Filesystem selection

Ordinary source builds keep the existing external directory behavior and need
no generated frontend files to compile. `Config.Load` is unchanged: its default
`GOBY_WEB_DIR` remains `web/admin/dist`, resolved to an absolute path.

Builds with `-tags=goby_embed_admin` include the production bundle through
`web/admin/embedded.go`. Without a nonempty `GOBY_WEB_DIR`, the process entry
point passes that embedded `fs.FS` to `server.WithDashboardAssets`. The binary
can then serve its administrator pages without a runtime frontend directory.

A nonempty explicit `GOBY_WEB_DIR` selects the existing resolved external path
in both build modes. An unavailable explicit override reports the existing
dashboard failure; it does not silently fall back to another bundle. An empty
environment value keeps the existing configuration fallback semantics and does
not select an override. Existing systemd environments that explicitly set
`GOBY_WEB_DIR=/usr/share/goby/admin` keep using that directory. To use the
embedded bundle, omit that setting.

Direct `server.New` callers retain the configured directory unless they pass
`WithDashboardAssets`. The filesystem choice does not change `/admin/v1`
authentication, API routing, SPA fallback, CSP, HTML no-store behavior, or
immutable caching for assets. The interface serves administration only.

## Build a candidate

All project builds and verification run through `ssh test-env` unless the
current task explicitly authorizes local verification. Use a separately frozen
source workspace and an independently budgeted build/cache location. Do not
reuse a live installation or infer current space from historical capacity
records. The entry point performs no dependency installation and does not
start Goby, PostgreSQL, FFmpeg, a browser or a container runtime.

The frontend dependency tree must already have been installed from its lockfile
in the prepared workspace. Build its actual production output there, then run:

```text
npm --prefix web/admin run build
node scripts/build-release.mjs --arch amd64 --output-dir bin/release-linux-amd64
node scripts/build-release.mjs --arch arm64 --output-dir bin/release-linux-arm64
```

For the internal amd64 systemd package, use a different fresh output directory:

```text
node scripts/build-release.mjs --arch amd64 --package systemd --output-dir bin/release-linux-amd64-systemd
```

This optional mode adds `goby-linux-amd64-systemd.tar.gz`, its staging directory
and a separate `package-manifest.json`. The archive contains the embedded binary,
the original build manifest, the unchanged systemd unit, an environment example
and [manual installation instructions](../../deploy/linux/INSTALL.md). The
external package manifest binds every member's hash, size and mode, the complete
archive, deployment inputs and the GNU tar/gzip tools. The builder reads back the
saved archive before writing that completion manifest. Systemd packaging is
currently restricted to amd64; arm64 retains the existing no-package mode.

The no-package mode creates only `goby` and `manifest.json`. Existing output
directories are refused and failed build output is retained. The manifest
records the target, binary SHA256/size, Go build metadata and every input asset's
relative path, SHA256 and size. Its `sourceInventory` also records every regular
file below `cmd/` and `internal/`, including SQL migrations, embedded catalogs and
test data, plus `web/admin/embedded.go`. It includes the complete sorted file
list, file count, total bytes and a SHA256 over the UTF-8 `JSON.stringify(files)`
encoding. `go.mod` and `go.sum` remain separately recorded as `moduleInputs`.
This explicit local snapshot works without `.git` and includes uncommitted file
contents; it does not claim that every inventoried file is a compiled dependency.

Assets, local sources and module files are observed before and after compilation.
Symbolic links and other non-regular source files are refused. A changed file
set or content fails the build without writing the completion `manifest.json`;
partial output remains available for inspection. Output directories must be
outside the asset and inventoried source trees. Keep the frontend build's source
and toolchain receipt as well: packaging an existing `dist` does not independently
prove which TypeScript inputs produced it.

The packaging check reads the actual `index.html` and binds its external
`type="module"` script entries, `rel="stylesheet"` links and
`rel="modulepreload"` links to nonempty regular files in the asset inventory.
The current Vite profile uses quoted attribute values and canonical
`/admin/assets/...js` or `/admin/assets/...css` URLs. Boolean attributes such as
`crossorigin` and self-closing link tags are accepted. Duplicate attributes,
inline/classic script forms, ambiguous relevant link relations, encoded or
nonlocal asset URLs and missing referenced files are refused. At least one
actual module entry must be referenced by the HTML; an unrelated remaining
JavaScript chunk cannot satisfy that check. The manifest records the checked
URLs and their file hashes in `administratorEntryReferences`.

This check targets the generated Vite entry markup. It does not build a general
HTML document model, traverse JavaScript static/dynamic imports, or resolve
CSS `url()` references, and it does not establish browser startup or complete
dependency-graph validity. The asset inventory and retained frontend build
receipt remain necessary evidence for the rest of the bundle.

The script sets `GOOS=linux`, the requested `GOARCH`, `CGO_ENABLED=0` and the
embedding tag. It uses read-only module mode, disables module fetching and the
per-user Go environment file, clears `GOFLAGS`, disables workspace selection
and automatic toolchain downloads, and
retains caller-provided cache/scratch locations. Go dependencies must already
be cached; the module files are observed unchanged and recorded in the manifest.
Use the project-pinned Go toolchain. A missing HTML entry or one of its checked
JS/CSS references fails before the Go build with a frontend build instruction.
Direct tagged Go builds also fail at
compile time if `dist/index.html` is missing; no placeholder is bundled.
HTML reference validation belongs to `build-release.mjs`; a direct tagged Go
build does not perform that check.

## Verification boundary

The new handler tests use `httptest.ResponseRecorder` with in-memory or temporary
asset files and the real route registration. They cover index and SPA delivery,
JS/CSS GET and HEAD, missing files, cache/CSP headers, unknown administrator APIs,
missing-cookie rejection and external filesystem behavior. They create no
database, application worker or HTTP listener. Tagged provider tests additionally
read and serve the actual embedded production index/chunks with an absent
external directory, and verify an explicit external override.

Run the focused checks in the prepared remote workspace after its real frontend
build:

```text
go test ./internal/server -run '^TestDashboard'
go test ./cmd/goby -run '^TestDashboardAssets'
go test -tags=goby_embed_admin ./cmd/goby -run '^TestDashboardAssets'
```

A cross-compiled arm64 artifact is build evidence only. Native arm64 installation,
startup, filesystem and media behavior require their own actual platform results.
OCI image identity and runtime checks, supported distribution/architecture rows,
hardware profiles, license selection and exact bundled-component notices remain
open. PostgreSQL and FFmpeg/ffprobe remain separately provisioned dependencies.
The complete M2-M6 delivery scope is unchanged.
