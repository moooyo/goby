# Prepared-plan retained memory, 2026-10-08

## Result and scope

The existing 512-entry statement cache bounds entries, not retained bytes. In
the selected single-backend workload, five repeated representative statements
had 1,120,496 reported `CachedPlan*` context total bytes. A bounded diversity
workload then retained 512 named statements after eviction draining, with
344,401,792 total and 250,784,184 used context bytes (328.45 and 239.17 MiB).
Those are measurements of this SQL population, not a general memory ceiling.

The separately selected topology held 12 Data connections (one designated owner
and eleven borrowers) and four Control connections. Its 68 representative named
copies had 32,069,456 total and 22,464,832 used `CachedPlan*` context bytes
(30.58 and 21.42 MiB). This was not sixteen connections each holding 512 plans.

All four fixed native invocations passed once, without retry, using one build.
Clearing both pgx caches returned named inventories and the measured context
prefix to zero in all four groups. Subsequent connection closure and reference
release behaved as expected for the selected workload. No long-running soak,
stable timing result, service-wide capacity bound or ongoing-leak conclusion is
claimed. Production source and cache settings were not changed by this task.

## Inputs and measurement protocol

The measured test commit is `4df73181523d871522e566aa19921632853aae50`, containing
only `prepared_plan_memory_capture_test.go` and
`prepared_plan_memory_integration_test.go`. Production remains the accepted
Stop/GET implementation `82dbcd0540017852fac39747b5b1caa80e89fe00`. Measurement
ran on `test-env` with PostgreSQL 17.11, pgx 5.11.0, Go 1.27.1 and GOMAXPROCS 4.
Selected target sessions were configured with `jit=off`; the server default
remained `jit=on`. Separate settings readback reported `plan_cache_mode=auto`.
The harness did not force that setting or emit a GUC readback for every target
backend, so the defaults evidence is not a per-target observation.

Five real typed SELECT families were captured: media, unchanged images,
ordinary lookup, bitmap subtitles and playback source. Dedicated target
connections replayed these templates. Each replay was code-guarded to return
one row with the same per-family result digest established during metadata
warm-up. Successful result bytes and digests were not exported for independent
rehashing. This is captured-query replay, not complete HTTP, scan, Store or
Server execution. The extra deployment lease is excluded from the target
population.

| Group | Target connections | Statement capacity | Description capacity | Selected workload |
| --- | ---: | ---: | ---: | --- |
| single-512 | 1 | 512 | 512 | Five representative templates, then 513 distinct shapes |
| single-0 | 1 | 0 | 512 | The same bounded shape sequence through CacheDescribe |
| single-1 | 1 | 1 | 512 | The same bounded shape sequence with statement eviction |
| topology-16 | 16 | 512 | 512 | Owner: two templates; each borrower: six; Control: none |

The physical default execution mode was CacheDescribe. Positive statement-cache
cases explicitly selected CacheStatement for target replay; single-0 selected
CacheDescribe. The statement and description caches are independent. These
cases did not fill both caches simultaneously. Single-0 therefore means zero
statement-cache capacity, not all caching disabled.

Each single-backend case used four fixed non-source shapes plus 509 valid
synthetic source shapes with bounded policy/user literal diversity. Six
executions per diverse shape and one selected eviction probe followed the
representative phase. The literal distribution is not a measured production
user or policy distribution. Each single case executed 3,627 target queries;
the topology case executed 544. Metadata and the fixed observer were warmed
before an explicit empty baseline. The representative phase executed each
assigned template once and then five additional times.

The topology used real pools and held all sixteen target connections. Owner and
borrower labels are harness role assignments; no complete Store ownership
lifecycle ran in this replay. Its
MinConns and MinIdleConns were zero to avoid replacement connections during
identity-based closure; production MinConns is one. It represents the selected
maximum connection population and role assignments, not the exact production
idle-pool lifecycle or a sixteen-way saturation experiment.

Earlier work left backend-memory observation unavailable. This task's direct
memory-read probe retained SQLSTATE `42501` for `goby_test`: denied observation,
not a zero-byte result. The older unavailable records did not retain a SQLSTATE,
so this probe does not establish their individual failure causes. A temporary
task-owned SECURITY DEFINER helper filled the observation gap and exposed only the
fixed current-backend context query, guarded by the selected database, session
role and application tag with `search_path=pg_catalog`. The only added access
for `goby_test` was schema USAGE and helper EXECUTE; no `pg_monitor` or
`pg_read_all_stats` membership or broad monitoring grant was added.

The PostgreSQL observer then ran on the backend being measured through that
helper. Simple-protocol observation replaced unnamed
statements and avoided adding named observer statements. The raw evidence
retains statement inventories, backend PID/start identities and flat memory
context rows. `CachedPlan*` below sums rows whose name starts with `CachedPlan`;
such rows can include helper/observer-related contexts and are not exclusive
per-query costs. The measured empty baseline prefix was zero in every group.

## PostgreSQL retention and eviction

All values below are bytes reported by the context observer. Total and used
remain separate accounting fields; neither is process RSS.

| Group and phase | Named statements | CachedPlan* total | CachedPlan* used |
| --- | ---: | ---: | ---: |
| single-512, representative repeat | 5 | 1,120,496 | 716,120 |
| single-512, after normal execution drain | 512 | 344,401,792 | 250,784,184 |
| single-0, after normal execution drain | 0 | 0 | 0 |
| single-1, after normal execution drain | 1 | 49,152 | 30,440 |
| topology-16, representative repeat | 68 | 32,069,456 | 22,464,832 |

The single-512 inventory moved through 513 names before draining, 512 after one
normal execution, zero after `DeallocateAll`, and five after representative
rebuilding. The additional pre-drain name was pending eviction cleanup, not a
new permanent cache entry beyond the selected capacity.

Single-1 moved through two names before draining, one after draining, zero after
clear and two after representative rebuilding, including one pending eviction.
Its drained survivor was the media query. Its 49,152-byte total is consequently
not a fair same-content byte comparison against single-512's complex source
population. Capacity changes altered which SQL remained resident.

With `plan_cache_mode=auto` in the separate defaults readback, a stable named
count must not be treated as a stable byte count or as proof that no custom plan
is built. The single-512 representative prefix grew from 667,264 total / 438,280
used bytes after one pass to 1,120,496 / 716,120 after repetition, while retaining
five names. Generic/custom counters describe uses of the currently inventoried
statement generations; they do not count simultaneously retained plan trees.

The actual topology repeat-phase breakdown is:

| Role | Connections | Names per connection | CachedPlan* total per connection | CachedPlan* used per connection |
| --- | ---: | ---: | ---: | ---: |
| Designated Data owner | 1 | 2 | 249,008 | 166,600 |
| Data borrower | 11 | 6 | 2,892,768 | 2,027,112 |
| Control | 4 | 0 | 0 | 0 |

The owner replayed media and unchanged-image reads. Each borrower replayed
lookup, bitmap and four source variants. The 68 named copies cover eight
distinct SQL forms. Across all sixteen backends, all
reported memory contexts totaled 65,551,536 bytes with 48,742,592 used bytes.
That broader sum includes baseline and observer state and is not another
estimate of prepared-plan memory. It must not be added to the prefix sum.

## Client heap, clear and close boundaries

Go HeapAlloc was recorded after GC for the entire test process, including the
fixed fixture, observer and recorder. It is not a per-connection cache size.
HeapInuse, HeapSys, HeapReleased, object counts and cumulative TotalAlloc remain
in the raw data. TotalAlloc is not retained heap, and these fields are not RSS.

| Group | Empty baseline HeapAlloc | After drain HeapAlloc | After clearing both caches | Clear minus baseline |
| --- | ---: | ---: | ---: | ---: |
| single-512 | 1,666,216 | 7,057,736 | 1,667,552 | +1,336 |
| single-0 | 1,546,208 | 6,878,688 | 1,547,928 | +1,720 |
| single-1 | 1,546,312 | 1,557,152 | 1,545,616 | -696 |
| topology-16 | 2,909,352 | 3,293,992 | 2,911,368 | +2,016 |

The topology representative-repeat HeapAlloc was 3,292,032 bytes. Single-0 had
zero named statements and zero measured prefix bytes while its process heap
rose by 5,332,480 bytes at the drain phase. Its description cache was still
enabled. The clear-phase reduction is consistent with release in this fixture;
it does not exclusively attribute every byte to description-cache entries.

Zero prefix bytes after clear does not mean that all backend memory returned to
the empty baseline. Single-512 still reported 2,415,400 all-context total bytes,
131,072 above its 2,284,328-byte baseline; used bytes were 1,909,336 versus
1,866,696. The raw context rows retain this residual without assigning it to an
individual cache or query.

Connection Close happened only after both caches were cleared and a new
representative workload was rebuilt: five target executions per single case
and 68 for the topology. Those executions produced five names for single-512,
zero for single-0, two including one pending eviction for single-1, and 68 for
topology-16. Close was not tested directly against the saturated 512 state.

| Group | Rebuilt HeapAlloc | Closed, references reachable | Target references dropped |
| --- | ---: | ---: | ---: |
| single-512 | 1,669,344 | 1,669,152 | 1,582,784 |
| single-0 | 1,549,824 | 1,549,840 | 1,548,744 |
| single-1 | 1,548,568 | 1,547,392 | 1,489,568 |
| topology-16 | 3,292,472 | 3,189,144 | 1,582,672 |

All nineteen original target PID/backend_start pairs disappeared. No replacement
backend was substituted for an old baseline. Backend memory cannot be queried
after that backend has exited; the post-close value is unavailable, not zero.
The earlier clear reduction must not be credited again to Close or to dropping
Go references.

## Interpretation and next boundary

The main engineering concern is the number and complexity of distinct source
SQL forms retained per connection. The 512-entry capacity is not a byte cap,
and this experiment's 328.45 MiB prefix total is not a universal worst-case
bound. It does not cover simultaneous full statement/description caches, every
production policy shape or sixteen independently saturated backends.

The next candidate should reduce unnecessary source SQL variants by
parameterizing or canonicalizing policy/user literals while preserving current
authorization, snapshot freshness, error ordering and complete results. Any such
change needs its own correctness checks and independently measured performance
and retained-memory comparison. This task does not change production capacities,
connection counts, execution modes or PostgreSQL settings to obtain a smaller
number, and it does not select an additional experiment.

Exact attribution of context bytes to one query is not supported: name, parent
and level are not unique tree keys, identifiers are view-truncated, and the
evidence exports only a bounded identifier prefix and hash. No duplicate-name
deduplication or exclusive tree accounting is used. The observation protocol's
effect on unnamed statements and its retained context state also limit
inference beyond these saved snapshots.

## Evidence and actual closeout

Evidence root: `.artifacts/prepared-plan-memory-20261008`.

- `evidence/native/{single-512,single-0,single-1,topology-16}.jsonl` retains every
  selected phase, inventory, memory row and original-backend exit observation.
- `analysis/memory-summary.json` and `analysis/memory-summary.md` independently
  qualify all four saved groups and retain the complete phase tables and limits.
  Their final SHA-256 values are respectively
  `e220a6ea1caa57ed8c778594e67971ebf66323065232d6b25403de10c6cd25ea`
  and `815109a457cd9f365193938b7da1a16dd12201d1a04389363737b6d58dc0d756`.
  The offline qualifier `analysis/qualify_memory.py` is bound by
  `171cbe42da1ba5d30124e65aff90c0a83e85b15e43011ffeb37929f1a0d7a7d5`.
- `source-freeze.json`, effective manifest, overlay and build/native receipts
  bind the measured test source and runtime. The first group retains its original
  `memory-runtime-single512.py` binding; later groups retain their own final
  runner binding. Historical receipts were not rewritten to one runner version.
- Retained native binary `evidence/binaries/library.test` has SHA-256
  `47e8078d5e3bfc8e413aeb95f3923882dfdc35d21bf8e7b0f63e06e1628eef7f`.
- `export-final.json` lists 49 independently hash-matched files and has SHA-256
  `0c6ef0e8620da1c0073a233675db151cb3d36aa327d131dd645e6b053f517db7`.
- `closeout-receipt.json` has SHA-256
  `20480003c909fcbdc70424cbb4c2f4fd89f57066e9527fcc51012ac19322be2d`.

Actual closeout checked the helper's schema OID 12083782, function OID 12083783
and exact definition hash before removing only that function and schema without
CASCADE; subsequent reads confirmed absence. Role attributes and memberships
were unchanged. Workers were absent and protected service identities stable.
The task reclaimed 47,357,952 allocated RAM bytes and its empty ext4 fixture;
compiler scratch had already been removed after build exit. Source, native
binary, raw evidence, shared build/module caches and the original corpus were
preserved. The ledger is one build, four passing native invocations and zero
retries. No push or PR was performed.

The measured source excludes the original main-worktree WIP. Its separate
delivery snapshot records all 200 protected paths, their bytes and staged state.
Local delivery must preserve that recorded WIP independently of these measured
inputs.
