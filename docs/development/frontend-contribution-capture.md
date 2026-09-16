# Frontend contribution capture preparation

Status: **synthetic producer and consumer contract tests passed; type checking
and real frontend builds remain pending**. The
[saved verification](https://github.com/moooyo/goby/blob/a83644d6aa289fa8a587bf8831bf3df21a4f27ac/docs/development/frontend-contribution-verification.md)
binds the unchanged tooling at `43a9b76a93615b7f872f48dae4e9b3a73ca050b0`.
This branch prepares evidence capture for the next independently required
frontend build. It does not schedule a rebuild of unchanged E11 or change the
held Programs verification and candidate-transition order.

## Capture and consumption

`web/admin/build/frontend-contributions.ts` is a build-only Vite plugin. A
one-shot written client build removes its previous private report at build
start, observes parsed modules, and writes
`web/admin/.artifacts/frontend-contributions.json` during its `writeBundle`
hook with `order: 'post'`. The report stays outside `dist`, uses mode 0600 and
is excluded by the existing `.artifacts/` ignore rule. It contains hashes and
metadata, not source bodies or environment values.

The report records:

- The bundler's chunk-to-module map, chunk imports and observed loaded-module
  imports, retaining virtual/generated or unobserved modules explicitly.
- Associated physical file snapshots and their nearest named/versioned package
  manifests, separately from parsed-code and pre-final-render code hashes.
- The actual Vite package, Rolldown resolved from that Vite entry, and the React
  plugin package; the lockfile, configuration dependencies and Node/V8 versions.
- All files actually present in the output directory, with lengths and SHA256;
  each output-bundle entry must match its corresponding file.
- An explicit `javascriptWithoutChunkModules` list for `.js`, `.mjs` and `.cjs`
  assets, case-insensitively, that have no output-chunk module map. This includes
  copied public files and scripts emitted as assets without guessing ownership.

Files observed during parsing/configuration must still match before publication.
An incomplete named/versioned package manifest stops package attribution at that
boundary instead of borrowing an enclosing package's version. A type-only
subdirectory manifest may still resolve to its enclosing package.
Input files are bounded to 16 MiB each and 256 MiB in aggregate; the report is
bounded to 16 MiB. This is additional build work whose cost remains unmeasured.
The output and report directory checks reject symbolic-link substitution at
their observation points. They are not a general adversarial filesystem
isolation mechanism; the build retains its existing owned-workspace prerequisite.

The existing release script accepts an optional explicit input:

```text
node scripts/build-release.mjs --arch amd64 --output-dir <fresh-output> --frontend-contributions <saved-report>
```

Paths are resolved from the repository root. The consumer requires the report's
complete asset inventory to equal the selected `dist` inventory and checks the
report identity across the Go build and packaging. It copies the report to the
release output as mode 0600 and records its hash/build ID in the build manifest.
Chunk names and the explicit unknown-JavaScript list must form an exact,
non-overlapping partition of those JavaScript asset filenames. This is an
extension-based inventory boundary, not a JavaScript content detector. The
unexecuted version-1 draft has been refined in place; no actual earlier report
or accepted historical input is being reinterpreted.
Before reporting completion it rechecks both the source and retained-copy
identities, bytes, hashes and modes, including the no-package path.
The sidecar is not added to the existing five-member systemd package. Omitting
the option preserves the previous manifest shape and does not claim source
contribution evidence.

## Interpretation boundaries

The pinned [Rolldown output types](https://github.com/rolldown/rolldown/blob/v1.2.7/packages/rolldown/src/types/rolldown-output.ts)
provide `modules`, `renderedExports`, nullable `code` and `renderedLength`.
No experimental capture flag or sourcemap is required. The
[rendered-module adapter](https://github.com/rolldown/rolldown/blob/v1.2.7/packages/rolldown/src/utils/transform-rendered-module.ts)
uses JavaScript string length, so the report names it `renderedLengthUtf16`.
Hashes use UTF-8 bytes instead. Neither quantity is a module's final minified
byte share: module rendering precedes the
[final render/minify stages](https://github.com/rolldown/rolldown/blob/v1.2.7/crates/rolldown/src/stages/generate_stage/render_chunk_to_assets.rs).

Associated disk files and parsed code remain distinct. Resource queries and
plugins may transform file contents. Import edges describe the loaded graph,
not proof that every imported module survives tree shaking. A virtual or
unobserved ID receives no guessed package owner, and null code is preserved.
Inline generated code inside another module is not independently attributed.

Vite's [plugin order](https://github.com/vitejs/vite/blob/v8.2.2/packages/vite/src/node/plugins/index.ts)
places build plugins after ordinary user plugins. `writeBundle` runs after the
bundle is written, but other post hooks can still follow. The report therefore
claims only `phase: bundle_written` and `buildSuccessClaimed: false`. The actual
frontend command must succeed, and the later selected package assets must still
match. A report left by a failed later hook is not a successful build receipt.

This is a new build's contribution observation, not proof of historical E11
inputs, npm installation integrity, every generated helper's original source,
font provenance or legal completeness. Those claims retain their own evidence.

## Verification still required

The prepared producer Node test source covers physical/virtual/unknown modules, UTF-8
versus UTF-16 lengths, nested Rolldown resolution, changed/missing assets, changed
source inputs, stale reports, package identity boundaries and directory/file
substitution, plus explicit unmapped public/emitted JavaScript files. The
synthetic entry points resolve package fixtures; they do not
invoke Vite, Rolldown or a production build. The Node test command, requiring a
Node version with TypeScript stripping, is:

```text
node --experimental-strip-types --test web/admin/build/frontend-contributions.test.ts
```

`scripts/build-release-contributions.test.mjs` adds Linux CLI contract cases
using a private synthetic Go substitute. Its isolated PATH has no real Go
fallback; it creates only a marked synthetic binary and metadata. Cases cover
explicit private-copy binding, unchanged no-option manifest shape, rejected
asset/JavaScript partitions before Go dispatch, source mutation during the
metadata command, and copy mutation after manifest writing. The latter uses
a synchronous preload restricted to that fixture's exact output paths, without
a watcher or background process. These tests do not exercise a real compiler,
systemd packaging or application runtime:

```text
node --test scripts/build-release-contributions.test.mjs
```

The saved `test-env` producer run passed 15 TAP tests (7 top-level tests and
8 subtests); the consumer run passed 14 (5 top-level tests and 9 subtests).
Each suite ran once with exit 0, no failures, cancellations, skips or TODOs,
and unchanged source/runtime identities. Both units and their recorded process
lifetimes were closed. The
[machine-readable verification](https://github.com/moooyo/goby/blob/a83644d6aa289fa8a587bf8831bf3df21a4f27ac/docs/development/frontend-contribution-verification.json)
records the source, runtime, TAP, result and closure pins. Its saved summary is
`/opt/goby-test/frontend-contribution-tests-20260916-9d8e9b6baa61/private/summary.json`
(1848 bytes, SHA256
`7293b53dd5d6d032b4e11d2fe3e88cc04ca33e56f8b6fc3c4f6ee1d344fa8bdc`).
The earlier nanosecond-encoding preflight rejection created no test unit and is
retained separately; neither suite was retried.

These checks used synthetic plugin hooks and a Node-based fake Go executable.
They did not run Vite, Rolldown, a real Go compiler, type checking, systemd
packaging, a browser or an application. Reuse these results while the tested
sources remain unchanged. Run the administrator type check and the next
independently required frontend build only in the coordinated `test-env` window.
Verify the actual
Rolldown callback data, complete asset equality, absent sidecar from `dist` and
the systemd package, and successful explicit consumption by the release script.
Measure capture overhead within that build's existing resource budget.
Keep the frontend exit/result and private report with the same build evidence.
Do not merge this preparation into the frozen Programs artifact or count it as
frontend, package or M6 acceptance before those checks complete.
