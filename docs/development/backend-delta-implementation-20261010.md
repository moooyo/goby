# Backend delta implementation, 2026-10-10

## Scope

This implements X01-X05 from
[the delta review](backend-design-review-delta-20261010.md). The user authorized
implementation, remote verification, integration into main, and push.
The implementation starts from `d192ec55` on `codex/backend-delta-fixes` in a
separate managed checkout. The original checkout's unrelated scan, native
media, playback, release and research changes are preserved and excluded.

All Go formatting, tests, media execution, catalog generation and builds run
on Linux `test-env`. Local work is source editing and static Git/file work.
No additional Windows server implementation remained to remove.

## Changes

| Finding | Implementation | Preserved behavior |
| --- | --- | --- |
| X01 | Migration 68 resolves and SHARE-locks exactly matching entities, then inserts only missing dimensions. | PostgreSQL normalization, complete hash-collision rejection, stable spelling/IDs, credits/groups, concurrent conflict results, isolation, empty-metadata association deletion and rollback. |
| X02 | A private target-revision query shares the full query's fixed authority predicate. | Every enabled/binding/revocation/expiry condition, error classification, polling point and later identity/resource check. |
| X03 | Remove the temporary RawMessage copy immediately before synchronous event encoding. | Independent final JSON, scope metadata ownership, recipient copies, validation and queue budgets. |
| X04 | A callback-scoped preview build shares one completed source-frame proof across up to three distinct widths. | Each output's complete source audit, current source/tool checks, geometry, per-variant limits/admission/timeouts, common build deadline and atomic publication. |
| X05 | Remove the redundant removed-ID predicate and argument from ancestor queries. | Frontier filtering, descendant exclusion, exact pre-deletion membership, identifier/depth/memory bounds, cancellation and commit boundaries. |

X01 preserves the original upsert branch for missing identities that race with
another insertion. It does not use a snapshot-only `DO NOTHING` fallback or
cache entity authority. Existing rows remain protected against concurrent
rename/delete, and stronger transaction isolation retains serialization
failures. The original association reconstruction remains. Historical SQL
bytes and their manifest entries are unchanged.

The schema-68 PostgreSQL 17 catalog was exported by
`scripts/test-env/generate-backuppg-catalog.go` from a newly created, empty
task database after applying the embedded migrations. The catalog regression
proves that only `sync_catalog_item_entities` changes relative to schema 67;
table, sequence and constraint shapes and the migration prefix remain stable.
Current backup/restore and schema-25 offline upgrade paths passed.

X04 keeps proof retention separate from process admission. Every width
reacquires and releases the analysis slot; BIF assembly and temporary cleanup
between widths do not hold it. Reuse reopens/checks executable bytes and
capability, source identity and geometry. Each output still reaches genuine
EOF and matches the full source-metadata trace, selected timestamps, frame and
packet counts. It does not treat that trace as a pixel-content checksum.

Each variant's logical frame/packet/diagnostic budget includes the already
completed source audit. The actual-work counters add only executed passes.
Since there are at most three variants, each bounded by the original
source-plus-output limit, actual source-plus-all-outputs is bounded by three
times that limit without another admission mechanism. Existing per-variant
timeouts remain distinct from the build deadline.

The callback extraction capability is private to the build and is invalidated
on failure or callback completion. Final callback cancellation is checked even
when a callback returns nil. The server discards any provisional publication
on helper failure. Task execution approval and subsequent publication fences
retain their existing contracts; this change does not add repeated account
authorization to an already approved worker.

## Observed behavior and performance scope

X01's database tests observe actual attempted writes with a trigger and inspect
the identity sequence. Seven new dimensions produce seven INSERTs and no
UPDATEs. Three later synchronizations using those same dimensions produce no
entity INSERT/UPDATE and no new identity values, while rebuilding all expected
associations. A mixed set inserts only its new dimension. SHARE row locking
can still produce WAL; these results do not claim zero WAL or a measured
whole-import speedup. The added existing-row lookup also has a cost for
entirely new dimensions.

X05's real PostgreSQL fixture compares the previous exclusion predicate with
the narrowed query. Both return the same ancestor results with two batches
and 66 member queries. Bound ancestor argument content drops from 3,758 to
1,390 bytes. Descendant exclusion is independently required by the fixture
and remains in the implementation. These counts describe argument content,
not network framing or end-to-end latency.

X04 compares independent extraction of all three widths with shared extraction
on the same source descriptor. Every JPEG byte, nominal/actual timestamp and
summary matches. The actual shared source-frame and packet counters establish
four complete preview passes; the independent three two-pass calls use six.
The source descriptor offset remains unchanged and no proof or analysis slot
is retained after completion.

The following are single-run descriptive observations from the final ordinary
test, including executable and geometry checks but excluding fixture creation
and initial probing. They are not production latency estimates or a universal
speedup. Independent mode retains copied reference JPEGs and shared mode checks
against them; this is a correctness-oriented comparison, not an isolated
encoder benchmark.

| Fixture | Independent elapsed | Shared elapsed | Full preview passes |
| --- | ---: | ---: | --- |
| Short CFR, 30 frames | 212.5 ms | 199.6 ms | 6 to 4 |
| 60-second 720p CFR, 1,500 frames | 6.037 s | 4.068 s | 6 to 4 |
| VFR with first/last hold | 224.1 ms | 202.0 ms | 6 to 4 |
| Nonzero source origin | 217.4 ms | 196.8 ms | 6 to 4 |
| Rotation and non-square SAR | 265.1 ms | 221.5 ms | 6 to 4 |

The 720p example records child CPU of 6.587 s versus 4.438 s and process CPU
of 1.282 s versus 1.011 s. Physical block-I/O counters were zero in both modes,
so the run establishes no physical-disk saving. Source/tool changes,
unchanged-tool capability failure, later callback failure/cancellation,
per-variant timeout, whole-build deadline, duplicate width and interval changes
all fail without a successful later summary or retained proof/admission.

X02 and X03 are narrow data/copy reductions. No separate end-to-end speedup is
claimed for them.

## Verification

The task root is `/opt/goby-backend-delta-20261010` on ext4. The selected Go
executable is `/opt/goby-toolchains/go1.27.1/bin/go`, with FFmpeg/ffprobe from
`/opt/goby-toolchains/ffmpeg-9.0.1/bin` and PostgreSQL 17. Ordinary runs reuse
`/root/.cache/go-build` and `/root/go/pkg/mod`, with `GOMAXPROCS=2` and package
parallelism one. Both GOTMPDIR and TMPDIR point to task-owned ext4 `fixtures`
for combined test compile/execution. Release builds and catalog generation use
separate `compiler` scratch. Source, binaries, databases and evidence are
retained separately.

The admitted persistent budget is 6 GiB with a 12 GiB free-space floor, including
source/archive, shared-cache growth, fixtures/databases, binaries/logs/scratch
and reserve. The root initially had about 22 GiB free. No private cache copy,
module-cache cleanup, shared Docker cleanup or unrelated data deletion occurs.
The backup databases have distinct task-owned non-superuser roles and the
required disposable marker. Their credentials remain only in a remote
mode-0600 environment file.

Final results are deduplicated by package, top-level test name and mode.
Repeated runs and subtests are not added again; race cases overlap ordinary
cases. The ordinary database package includes three unrelated optional index
observation tests that were skipped without their explicit measurement flags.
They are not required for these changes. No required selected test is skipped,
and there are no unresolved selected failures.

| Package | Ordinary passed | Race passed |
| --- | ---: | ---: |
| database | 97 | 0 |
| events | 25 | 5 |
| notifications | 20 | 2 |
| library | 15 | 0 |
| media | 26 | 16 |
| server | 39 | 0 |
| backuppg | 4 | 0 |
| Total | 226 | 23 |

The ordinary matrix includes full database/events/notifications packages and
selected production-call, scan/metadata, preview/geometry/notifier and backup
regressions. Migration tests cover concurrent insert/rename/delete under Read
Committed, Repeatable Read and Serializable, row-lock retirement, collisions,
credits, schema-67 upgrade and caller rollback. Media tests include generated
real FFmpeg fixtures and independent reference outputs. The race matrix covers
event sharing, notification revision authority and preview budgets/lifetimes.

`npm ci`, the administration frontend production build, the server build with
`goby_embed_admin`, and the command-launcher build all passed remotely.
No Windows build or local runtime verification was performed.

## Implementation review loop

| Round | Result | Consecutive rounds without a new issue |
| --- | --- | ---: |
| 1 | Identified and fixed missing whole-callback cancellation in the new preview helper; added focused regressions. | 0 |
| 2 | Rechecked cancellation fix, SQL conflict/lock tests, budgets, catalog identity and publication cleanup; no new issue. | 1 |
| 3 | Rechecked concurrent state changes, ignored extraction errors, geometry/tool replacement, automatic notification disable and changing parent rows; no new issue. | 2 |
| 4 | Final production-call, resource/permission, migration/catalog and verified-result review; no new issue. | 3 - stop |

Independent reviewers and the integrating review completed three consecutive
rounds without a new issue after the cancellation fix.

## Resource closeout and Git delivery

Fresh process inspection confirmed that task Go/compiler/linker/test/media/Node
workers had exited. The empty compiler directory was removed. Inspection of
the remaining fixture directory found only task-owned Node compiler cache;
its 3,825,664 allocated bytes were reclaimed after a further liveness and path
check, followed by removal of the empty fixture directory. Shared Go caches,
modules, source, binaries, frontend dependencies, databases and evidence remain.
Persistent availability was about 20.87 GB after closeout; concurrent activity
can also affect that observation.

The commit archive is compared against the remotely tested source before
integration. Main is advanced without force, preserving original working-tree
file contents. Commit/remote equality and original-file preservation receipts
are kept outside Git to avoid self-referential commit hashes.

Raw commands, logs, measurements, environment, source correspondence and
delivery receipts are retained in the task evidence directory and the local
`.artifacts/backend-delta-implementation-20261010/` directory. No private
credential file is copied into the repository or these exported logs.
