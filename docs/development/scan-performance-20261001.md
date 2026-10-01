# Media library scan performance review, 2026-10-01

This change reduces scan overhead by combining exact-path catalog lookup,
reusing repeated source-less television hierarchy publications, avoiding
unchanged subtitle row writes, and batching directory-evidence entry writes.
The largest measured improvement is for television files stored directly in a
library root: cached scans fell from a median 1.221902 seconds to 0.243168
seconds, with SQL statements falling from 8,143 to 1,729 in the measured
fixture. The measurements isolate scanner overhead with a cheap descriptor
prober; they do not establish representative NAS or full-content throughput.

Structured results are recorded in
[scan-performance-results-20261001.json](scan-performance-results-20261001.json).
Raw evidence is retained locally under `.artifacts/scan-performance-20261001`.
The verification environment is `ssh test-env`; no tests, builds, or runtime
probes were run on the local workstation for this task.

## Architecture review

`Store` has two scan workers. A library walk is sequential, while independent
libraries can use the two workers. Probes and sidecar inspection occur outside
the catalog-owner mutex. Catalog mutations use short transactions on the
reserved PostgreSQL session that holds the ownership advisory lock. Ownership
loss fences writes instead of reconnecting an old worker to a successor's
catalog.

The ordinary file path opens and identifies a descriptor, persists the entry
progress checkpoint, finds the existing catalog identity, and either reuses a
valid probe or probes the descriptor. It then reads local metadata, establishes
the hierarchy, and compares accepted automatic facts with the stored facts.
Changed primary items publish metadata and catalog notifications in an owned
transaction. Cached primary items still inspect subtitles, images, and embedded
artwork and record accepted identity membership for final reconciliation.

The review identified unnecessary work at four boundaries:

| Boundary | Previous work | Optimized work |
| --- | --- | --- |
| Existing file at an exact path | Separate role-conflict and metadata queries | One role-aware exact-path query |
| Repeated virtual series or season | Repeat the folder publication transaction for each episode | Reuse the latest identical accepted input within the root's scan |
| Unchanged subtitle sidecar | Rewrite an already accepted row and compare catalog projections | Compare validated source facts with the owned snapshot and skip unchanged writes |
| Spool entry persistence | One 384-byte `WriteAt` per directory member | One bounded write for up to 64 entries |

The single owner session is a correctness boundary. Increasing worker count or
moving catalog writes to unrelated pool sessions would not address these
redundant operations and would require a separate ownership design review.

## Implemented changes

### One exact-path lookup

`findStoredFileForRole` now selects the stored-file columns and role
compatibility from the unique `(root_id, relative_path)` row in one query.
Compatibility is checked before claim acceptance or JSON decoding. An inactive
theme or extra identity retains its permanent role protection, even when its
stored metadata is malformed.

The exact-path query remains independent of the number of already claimed
identities. The rename fallback retains its existing identity, size, timestamp,
claim-exclusion, candidate-limit, and disappeared-old-path checks. It can still
reuse a same-library identity across registered roots when the required
evidence exists.

### Bounded virtual hierarchy reuse

Only folders with a `//` relative path and no filesystem metadata source are
eligible for the scan-local cache. Each root retains at most 4,096 entries,
keyed by relative path. An entry stores the latest successfully accepted ID and
the complete input tuple: path, name, type, parent, and index number.

Repeated identical inputs reuse the ID after checking cancellation and store
availability and recording the identity as seen. A different input or a visit
with a filesystem metadata source invalidates the old entry before the ordinary
publication path runs. This preserves both `Show -> SHOW -> Show` publication
and a source-less visit followed by `tvshow.nfo` followed by another source-less
visit. A new scan starts with a new cache.

Physical folders retain their existing NFO, image, and directory-identity
inspection. The optimization does not cache those source-backed publications.

### Unchanged subtitle publications

Subtitle bytes are still freshly inspected, hashed, and checked against their
current filesystem identities before publication. The owned transaction still
locks the primary item, validates its source, and checks the combined subtitle
capacity. The active subtitle snapshot now includes both public fields and
private source facts.

If there are no retirements and all successfully inspected sources match that
snapshot, the transaction commits without rewriting subtitle rows or reading
the before/after catalog projection. The comparison includes path, root,
identity, change time, hash, size, modification time, codec, language, title,
flags, and MIME type. A content change with a restored modification timestamp
or a private-source change still publishes.

Mixed updates retain an `IS DISTINCT FROM` guard on the upsert, so adding or
changing one sidecar does not rewrite another unchanged sidecar. Invalid
sidecars, capacity limits, retirement rules, and stable stream identities keep
their existing behavior.

### Bounded spool writes

Directory-evidence entries keep the existing sorted order, fixed 384-byte
format, individual checksums, and read offsets. A fixed 24 KiB buffer writes up
to 64 entries at a time. For a directory with `E` members, entry-region write
calls fall from `E` to `ceil(E / 64)`; header writes are unchanged.

Each entry still checks cancellation and obtains its own current stat and
version before encoding. The tail is flushed before directory proof. A write
error or short write immediately disables the evidence pass; the accepted
offset advances only after the complete write succeeds. Resource charging,
cleanup ownership, completion marking, and subsequent proof remain unchanged.

## Measurement method

The baseline is commit
`83e1b7f31c0f60e29fa781d078695f1ecccc6c3a`. The candidate contains the four
changes above. Both sources used the same profiling test file with an identical
SHA-256, the same fixture construction, and three fresh fixture runs per source.

Measurements ran in an isolated environment on `test-env` with PostgreSQL
17.11, Go 1.27.1, `GOMAXPROCS=4`, and a dedicated 512 MiB ext4 loop filesystem.
The fixture contained 400 media leaves:

| Layout | Leaves | Purpose |
| --- | ---: | --- |
| Flat movies | 160 | Ordinary exact-path lookup and sidecar overhead |
| Flat television | 192 | Repeated virtual series/season hierarchy work |
| Physical television | 48 | Existing physical folder hierarchy control |

Each fresh run performed a cold scan, two cached scans, an incremental scan
with add/rename/delete operations, a cached scan after the incremental changes,
and a `ForceProbe` scan. The cached timing medians below combine the first two
cached scans from all three runs, giving six samples per source. Cold,
incremental, and forced timings each have three samples per source. The
post-incremental cached phase is retained in the structured evidence but is
not pooled into those cached medians.

The descriptor prober is intentionally cheap. This removes representative
`ffprobe` and media-content costs from the timing and makes catalog, hierarchy,
sidecar, and evidence overhead visible. This profile is not a real-media
capacity test. Separate regression tests cover real FFmpeg media fixtures.

## Results

The timing metric is `job_elapsed`, in seconds.

| Flat television phase | Baseline median | Candidate median | Reduction | Baseline SQL statements | Candidate SQL statements |
| --- | ---: | ---: | ---: | ---: | ---: |
| Cold | 2.069836 | 1.062237 | 48.7% | 13,381 | 6,967 |
| Cached | 1.221902 | 0.243168 | 80.1% | 8,143 | 1,729 |
| Incremental | 1.235338 | 0.278981 | 77.4% | 8,223 | 1,809 |
| ForceProbe | 1.851126 | 0.881989 | 52.4% | 12,175 | 5,761 |

The other layouts provide useful controls for the scope of the improvement:

| Cached layout | Baseline median | Candidate median | Observed reduction | Baseline SQL statements | Candidate SQL statements |
| --- | ---: | ---: | ---: | ---: | ---: |
| Flat movies | 0.189260 | 0.168796 | 10.8% | 1,360 | 1,200 |
| Physical television | 0.115395 | 0.113102 | 2.0% | 739 | 691 |

The flat television results are consistent with eliminating repeated virtual
folder transactions in addition to reducing exact-path queries. The movie
statement reduction matches one fewer lookup per leaf. The small physical
television timing difference is not strong evidence of a runtime improvement.
These end-to-end profile results measure the combined candidate; they do not
independently attribute a percentage to subtitle or spool batching changes.

No representative NAS workload, production `ffprobe` cost, HTTP throughput,
or long-running multi-user load was measured. The small synthetic fixture and
sample counts should not be extrapolated into a production capacity claim.

## Verification

All selected commands completed successfully on `test-env`:

| Scope | Passed top-level results | Passed results including subtests | Skips |
| --- | ---: | ---: | ---: |
| Targeted library regression with `-race` | 39 | 92 | 0 |
| Expanded scan-related library regression with `-race` | 386 | 1,101 | 5 |
| Entire `internal/tasks` package with `-race` | 112 | 242 | 0 |
| Baseline performance profile | 3 fresh runs | 3 | 0 |
| Candidate performance profile | 3 fresh runs | 3 | 0 |

The targeted library checks are a subset of the expanded run and must not be
added to it as unique coverage. The expanded run selected scan, claim,
ForceProbe, subtitle, root-binding, theme, extra, and NFO tests. Its five skips
were the nonroot provider-subtitle permission check, an explicitly supplied
read-only provider-subtitle mount, the separately opted-in full-scan mount
helper, the performance profile (executed separately above), and the
separately opted-in high-directory-fanout proof. Those conditions were not
claimed as covered by this task.

The expanded run includes the existing real-media capacity test with 10,000
media leaves and 10,688 catalog items. It passed cold scanning, reopening the
Store, cached rescanning with one replaced source, library ACL projections,
catalog identity and hierarchy checks, and eight saved UserData rows. It
completed four scan jobs with 10,001 total real probe calls: 10,000 cold, zero
on reopen, and one for the replacement during cached rescan. The observed
cold phase took 137.020 seconds and the cached phase 22.525 seconds with
`-race`. These are candidate correctness observations with no matched
real-media baseline in this task; they are not additional speedup claims.

The targeted coverage includes role and claim
boundaries, malformed protected metadata, virtual hierarchy input changes and
NFO transitions, unchanged and changed subtitle source facts, batch boundaries,
short writes, write errors, and actual spool lookup/revalidation/absence proof.

## Environment closure

All logs, source archives, source hashes, and test summaries were downloaded
before retiring the dedicated environment. The owned PostgreSQL cluster is
shut down, its private socket is absent, and the task's ext4 mount and loop
image association are released. Independent readback confirmed that existing
PostgreSQL processes and mounts were preserved.

The first cleanup helper returned a failed postcondition after successfully
detaching the loop image: listing the loop device name still returns the
unattached block-device name. The retained receipt records that failure and
the corrected image-association/sysfs check. No stop, unmount, or detach command
was repeated to resolve it. This was a cleanup-observer error, not a product
test failure. The disposable workspace was removed after evidence download.

## Preserved contracts

There is no schema or API change, and the worker count remains two. Per-file
progress checkpoints remain in place. Independent primary-item counters retain
their item-transaction publication, and task-owned scans retain atomic scan-job
and child progress snapshots. Cancellation, ownership loss, root binding, and
catalog-notification behavior remain required regression boundaries.

Final missing-item reconciliation still requires complete root and directory
evidence, sealed accepted-identity staging, fresh absence proof, bounded
candidate/closure admission, and one final deletion transaction. Incomplete or
changed evidence cannot authorize deletion. The optimizations retain manual
metadata controls, stable identities, permanent auxiliary roles, and user data.

## Remaining performance work

These are follow-up measurement and design topics, not changes in this patch:

- Spool verification performs `O(E log E)` small reads because every enumerated
  directory member uses binary lookup against immutable records. Any read
  batching or index cache must retain checksum, ordering, exact-membership,
  cancellation, and bounded-scratch guarantees.
- Per-file progress persistence remains an owned transaction. Reducing its
  frequency needs a design that preserves cancellation detection and atomic
  task-child snapshots, rather than a timer-only throttle.
- Reconciliation pages filter by library and order by item ID without a
  dedicated `(library_id, id)` index. Remote query plans on a larger multi-library
  catalog should determine whether an index and separate first/range query
  shapes are worthwhile.
- Auxiliary owners can repeat filesystem publication witnesses. Reuse would
  need explicit source, lifetime, and publication fences; an unchanged catalog
  projection alone is insufficient authority to skip those checks.
