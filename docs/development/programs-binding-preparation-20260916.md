# Programs binding preparation, 2026-09-16

Status: the final consumer/tooling revision passed 44 runtime, 28 successor, and 70 closer checks in one new remote scope, with zero failures or skips and a passed independent saved-evidence review. The initial 138-check scope remains separate. The parent independently published the real observation and reviewed runtime envelope; these consumers bind that exact envelope and keep the Programs transition admission hold in place. This is a tooling checkpoint, not a product release, transition, client acceptance, or main merge.

The isolated branch is `codex/programs-artifact-runtime-binding`, based on `371158f576d47f50ef99ea611bf23db66998019f`. The changes affect the existing runtime reader, Programs closer, and their component tests. Product Go code, the transition operator, historical receipts, and frozen remote sources are unchanged.

## Fixed recovery edges

The existing current-runtime schema stays at versions 1 and 2. The original version 1 path and first version 2 edge retain their checks. A second version 2 edge is limited to the accepted `aed2914bc75663b51a9f4d923acc137cfe6a0926a901dd7fa0a7e64f42183de4` predecessor, whose own predecessor must be the fixed version 1 `38b906d090cf1ae6cf1e6679d68e92772e60e4e5f396ef085b232983a35de60c` record. Unknown ancestors, another version 2 parent, and cycles are rejected.

The second edge binds the Sep16 restart execution `cccb62597eb67dc5f54685e2f72ce1b1a0e48bd1632e5f85972d071838fce0dd`, review `a2036bace081fbe10a78ae1d507710b25cc2b2ddd55a5ee37f66c0e84a5a701a`, selected start/result, and retained configuration projection. The new review is read in its actual `applications`, `candidateEvidence`, `closure`, `counts`, and `protected` shape; old review flags are neither invented nor made optional.

The configuration projection keeps its Sep15 SQL provenance. Its exact bytes must match the already validated predecessor, while the new execution must preserve all eight candidate configuration hash/metadata entries, all five protected file identities, and the postmasters. Environment contents are not decoded. The separately reviewed post-start diagnostics remains separate evidence; the restart review does not claim that observation.

The original TV closeout retains its version 1 runtime pin. The startup-review path accepts that precise ancestor only after the caller has loaded and verified the fixed second recovery chain. It cannot substitute the new runtime pin into the old closeout or accept an arbitrary ancestor.

## Complete-source product

The old artifact profile remains available with its original exact fields. The new profile is selected only by the fixed complete-source archive pin and binds seven complete path/hash/byte descriptors: source archive, source manifest, source bridge, embedded build manifest, embedded binary, package manifest, and package archive.

The complete source contains 5,405 frozen files: 5,348 tracked project inputs and 57 retained assets, totaling 120,705,443 bytes, from commit `3d6b79b36b6a5050e174f7a152f214b9356608c8`. The bridge must retain its explicit `trackedInputCountScope` statement. The build inventory contains 861 files and 12,109,709 bytes in `cmd/`, `internal/`, and `web/admin/embedded.go`; this is not a compiled dependency graph. Frozen modes remain `0644`, with the recorded `0077` umask projection to `0600`.

The embedded binary is `ead67c8faaf4cde88f7fe1bf57ffed43705259aba7b29f59c3473747afd732fb`. The ordinary Linux binary is a separate artifact. The existing version 1 artifact aggregation and independent-product-review schemas remain strict; the two completed full/build and closure reviews cannot simply be renamed to satisfy them.

The actual retained closure records its adapter as a two-field path/hash descriptor. Only the complete-source profile projects the canonical review's three-field descriptor to that exact shape for comparison, and independently reads the adapter source to verify its byte length and hash. Extra fields, another path/hash, an incorrect byte count, or altered source bytes are rejected. The old profile retains its original full-descriptor equality rule, and the old closure is not modified.

## Verification and remaining binding

Added checks cover saved second-recovery records with explicitly synthetic observations, ancestry rejection, actual review/configuration provenance, unchanged hash-only access, retained TV-closeout ancestry, source-profile mixing, the real saved bridge, and closer inventory/mode/pin boundaries. Synthetic timestamps and observations must never be published as current facts.

The affected suites ran once on `ssh test-env` in `/opt/goby-test/programs-binding-final-component-checks-20260916-c365ddb98652`, after the prior M5 task had closed. The 44/28/70 results and 12 frozen source files were independently reviewed. The five changed repository code/test files match the tested bytes; the seven paired dependencies, including the transition operator, were unchanged. Exact pins are in [the component checkpoint](programs-final-binding-components-20260916.json). No unrelated 74/55/5 component groups, Go tests/builds, frontend build, reference client, SQL, HTTP, or application service action ran in this scope.

The unchanged unit limits were 256 MiB memory, swap disabled, CPU quota 25%, TasksMax 32, a 240-second runtime and 8 MiB per-output-file limit, inside a 300-second outer bound. Plan r02 explicitly selected 1 GiB MemAvailable and 2 GiB root free for this component job only, superseding the copied 4 GiB manifest admission field without rewriting it. This was a budget decision, not a measured minimum; M5 full's separate 4 GiB admission was unchanged. Admission observed 4,340,092,928 bytes available memory and 45,964,713,984 bytes root free. Peak unit memory was 65,093,632 bytes. The runner finished in 14.699 seconds and the outer dispatch in 15.441 seconds. All recorded test process groups, PIDs, the owned unit/cgroup and scratch were closed; source pins stayed unchanged. The independent review checked 52 raw streams totaling 38,888 bytes.

The completed 138-check scope retains its original source pins and results. The actual observer used that frozen runtime helper (`5d66cb04c5b48d8a0faea38fde05aceac67f7dcd2ba04096330d345c182e1d0e`) as an independent producer. The 142-check consumer revision is separately bound and reviewed; it does not rewrite the producer or reuse the 138 scores as its result.

The Programs authority constants bind `/opt/goby-test/candidate-oom-exit-recovery-20260916/current-runtime-01/private/current-runtime-binding.json`, SHA-256 `db22e3d954febd07c422d143027876a94782d3937e0ab374f80d566ba283c4ef`. The separate actual observation supplied the cluster identity and deployment-lease result, including `backendStart`; the restart timestamp was not substituted. The old `aed2914...` and `38b906d...` ancestor pins and frozen producer remain unchanged.

The parent published byte-identical runtime and transition CLI sources under `resumed-delivery-20260913-4cd0f29a0c14/programs-final-consumer-20260916-c365ddb98652/code/`, satisfying the existing historical-root bootstrap path boundary. Its 2,032-byte `private/source-publication.json` has SHA-256 `9d9f9821d251a3f7c11e8cdb7de09901dbfd5ad9a6d04257ca32ae01798f9b14`. Those published copies were not imported or executed, and publication did not create capture or transition acceptance.

Component checks do not establish an actual product-loader result. The Python loader retains about 250 MiB of raw archives before other allocations; its later actual saved-file invocation needs the separately proposed 1 GiB / 600-second budget and a real strict input. Do not substitute synthetic future fields. The complete JS loader additionally requires real post-transition version 4 records. State capture, retention/recovery admission, actual loader verification, and the direct-A transition remain separately gated; this preparation does not remove the transition hold.
