# M5 combined-source frontend verification

The frontend for combined M5 source `138522b707d0c3ba6012b119e38a18d38cf530c0`
passed one real typecheck and production build on September 16. The
[record](m5-final-frontend-verification-20260916.json) pins the command, source,
artifacts and separate execution/resource and artifact/source reviews.

The single command was the pinned Node executable running the pinned npm CLI
with `--prefix web/admin run build`. Its actual script ran
`tsc --noEmit && vite build` and exited 0. The sidecar reported Node 22.23.2,
Vite 8.2.2 and Rolldown 1.2.7. No dependency installation or old Programs/E11
frontend rebuild was performed.

The original S0 archive retains all 5400 committed source records. The complete
S1 archive preserves those canonical rows exactly and adds only the 59 actual
`web/admin/dist/` files, totaling 1,141,685 bytes. S1 contains 5459 files and
121,846,736 uncompressed bytes. Its Git HEAD identifies S0; S1 also contains
generated files and is not a pure Git archive. The source-binding record joins
both archives/manifests, the unchanged source rows, original build root and
sidecar build ID.

The original 1,918,049-byte contribution sidecar is retained at
`web/admin/.artifacts/frontend-contributions.json` in the build workspace.
Its 48 chunks cover all 48 JavaScript assets; the explicit
`javascriptWithoutChunkModules` list is empty. The actual dist inventory and five HTML entry references
were independently matched. The sidecar retains `buildSuccessClaimed:false`;
the separate successful command receipt supplies build completion. Bundler
module observations are not a complete compiled dependency graph, proof of
original-byte consumption or legal completeness.

The worker used a 2 GiB memory limit, zero swap, private network and strict
filesystem protection. Recorded memory peak was 721,768,448 bytes, with zero
recorded OOM events. All 35 controller children were reaped; their 70 streams
were read. The build unit was collected, recorded PIDs and cgroup were absent,
and the owned temporary directory was empty. Source, dependencies, dist,
sidecar and evidence intentionally remain in the workspace. Tool/dependency
identity checks and six protected process projections remained exact; no
business-database or global-configuration observation is inferred.

The retained 345-byte stderr warns that the extensionless import of
`./build/frontend-contributions` is unsupported by Vite's planned future
native config loader. The warning was not suppressed and did not fail this
build.

The subsequent full build must consume the complete S1 archive and invoke
`scripts/build-release.mjs` with `--frontend-contributions`. It must preserve
the original sidecar and require an exact final asset/JavaScript partition
match after source relocation. The sidecar stays outside the systemd payload.
The fixed consumer's saved-data shape was reviewed, but the consumer was not
executed in this frontend scope. Combined Go/DB/HTTP/browser behavior, codec
execution and final M5 builds remain pending. The first storage preparation
attempt failed before any volume or payload; its independently reviewed closure
is recorded in [combined-source preparation](m5-combined-source-preparation-20260916.json).
