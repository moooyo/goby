# Backend continuation implementation, 2026-10-10

## Scope

This change implements V01-V07 from the
[continuation review](backend-design-review-continuation-20261010.md) on
`d921441768e7f1de420b9a47c5ce3d014ae1fb5e`. Work was isolated from the original
checkout's unrelated uncommitted source and experiments. The earlier
[U01-U07 increment](backend-design-review-increment-20261010.md) is a separate
set of recommendations.

| Finding | Implementation | Retained boundary |
| --- | --- | --- |
| V01 | Run the session-existence SELECT only when it acquires a credential lock. | Account-before-credential ordering, separate fresh-clock liveness, final checks, error precedence and application-key authority. |
| V02 | Retain successful nonempty credits tail fingerprints in a runtime-owned FIFO, keyed by run, current complete-content hash, source revision, stream, exact tail and tool/algorithm/options. | Every full-content digest, active current-tool proof, child/root authorization, Force bypass and actual resource retirement. |
| V03 | Skip a matching retained text subtitle within a mixed-change scan. | Complete private source facts, retired identities, deterministic indices, capacity, public notification comparison and final physical proof of every inspected track. |
| V04 | Decode one locked accepted-music snapshot and pass its typed value to private hash/name/merge helpers. | Independent raw-input validation, unextracted byte preservation, extracted-empty semantics, canonical hashes, NFO precedence and manual overrides. |
| V05 | Return a private verified record/reference internally; copy payloads when constructing actual public results. | Full current/proof/file validation, uncertain publication, revision advancement and independent public Read/CAS payloads. |
| V06 | Distinguish deferring one analysis run from exhausting global capacity. | Nonwaiting-child invariant checks, actual examined-child budget/cursor, final RefreshRun, cancellation and authoritative claims. |
| V07 | Rotate through the six fixed media task keys and advance only after a successful claim and worker registration. | One active run per definition, existing candidate filters, one analysis worker, per-child execution approval, failed-worker accounting and reaping. |

The [analysis task contract](analysis-task-contract.md) now describes the
six-key rotation, continuous arrivals, failed claims and restart state. Selection
does not confer execution authority. A post-claim authorization or executor
failure still consumes its scheduling turn, while an unsuccessful claim does
not advance the cursor.

The credits cache has at most 128 entries and a 2 MiB budget for fixed-size keys
and raw uint32 payloads, in addition to its bounded map/ring bookkeeping.
Returned payloads are detached copies. Empty output, errors and Force runs do
not populate the cache. Publication occurs only after the helper has returned
successfully, including its deferred source, I/O-owner and executable closure.

## Implementation review

Independent reviewers checked the source changes and their production callers.
The first remote media regression exposed an error-classification mistake:
joining a final cancellation/source/close error with the completed-empty
fingerprint sentinel let callers still recognize the result as a normal empty
extraction. The implementation now removes that direct parser outcome before
combining real failures. Existing assertions remain intact, and a regression
covers simultaneous close, source and deadline failures.

Two existing HTTP schedule tests initially raced asynchronous task-manager
readiness. Baseline and candidate isolated controls passed. The fixture now
uses its existing bounded readiness helper after the read-only preview and
before the first PUT. Production readiness rejection and shutdown behavior
remain unchanged. The final HTTP suite is rerun with this explicit prerequisite.

An initial measurement fixture wrapped FFmpeg to count invocations. That remains
useful for controlled extraction/probe-count assertions, but would omit hashing
the real binary from its timing. The decision measurement therefore selects
the real executable directly, records its size and digest, and distinguishes
verified source/cache facts from expected process counts.

## Verification

The final `candidate-r3` source manifest has SHA256
`8415aab319a5f3cf0287a443ae02955d9677c3a5075da8492617012835a919a3`.
All final ordinary and race checks below passed on that source.

| Package/scope | Ordinary top-level tests | Race top-level tests |
| --- | ---: | ---: |
| Recovery control, full package | 23 | 23 |
| Library, affected authority/scan/music/subtitle paths | 55 | 8 |
| Tasks, full ordinary package and selected concurrent lifecycle/rotation paths | 142 | 22 |
| Media, credits extraction/proof/retirement | 13 | 13 |
| Server, affected HTTP and cache paths | 30 | 8 |
| Total | 263 | 74 |

The actual Chromaprint tests ran for short and 450-second tail windows.
`cmd/goby` and `cmd/goby-command-launcher` both built successfully as Linux
amd64 ELF executables. These are component build checks, not a new Docker image
delivery. All 15 final test/benchmark phases passed with no skipped tests. The
two HTTP readiness cases also passed ten repetitions each. Initial failed
attempts and isolated baseline controls remain in the raw evidence.

## Bounded performance observations

The direct native measurement used a generated FFV1/PCM Matroska fixture of
30,506,919 bytes, copied to each admitted source. The two repetitions reversed
Force/reuse order. The table reports mean fingerprint-stage time, including
source opening, every complete-content digest and current real-tool proofs;
cohort matching and target visual analysis are excluded.

| Sources | Children | Full digests per run | Expected tail extractions, Force/reuse | Force mean | Reuse mean |
| --- | ---: | ---: | --- | ---: | ---: |
| 16 | 1 | 16 | 16 / 16 | 5,880.856 ms | 5,793.723 ms |
| 24 | 2 | 48 | 48 / 24 | 17,916.256 ms | 13,009.361 ms |
| 32 | 2 | 64 | 64 / 32 | 23,596.304 ms | 17,459.859 ms |

The overlapping cases improved this stage by about 27.4% and 26.0%. The
single-child case has no reuse hits and showed similar timing. Complete hashes
and cache hits were checked; extraction/probe counts are explicitly expected
counts in direct timing, with separate instrumented regression coverage.
Three active capability probes remain per source in both modes. This supports
retaining the bounded cache for overlap, not a general playback throughput or
credits-detection accuracy claim. Larger originals can shift the cost toward
the retained whole-file digest.

Three 500 ms allocation samples compare the old helper composition with one
decode in the same library binary:

| Music input | Median bytes/op, separate decodes | Median bytes/op, one decode | Allocations/op, before/after |
| --- | ---: | ---: | --- |
| Unextracted | 1,232 | 664 | 17 / 9 |
| Extracted empty facts | 6,916 | 4,595 | 122 / 92 |
| Populated sample | 55,844 | 22,611 | 1,090 / 420 |

An identical current-record benchmark file was added to an otherwise unchanged
baseline for V05. With a 1 MiB payload and a parsed-record cache hit, median
allocation fell from 2,109,968 to 1,060,545 bytes per verification operation.
Both variants still read/hash the actual current file and check its complete
publication proof. The 1 KiB case fell from 5,387 to 4,363 bytes. Parsing-cache
miss samples and timings are retained; no general end-to-end CAS latency claim
is made from these component measurements.

## Environment and retention

The capacity policy was read before provisioning. All compilation, tests,
formatting, runtime probes and benchmarks run on `test-env`. The local Windows
host is used for source editing, Git and transferring source/evidence only.

- Task root: `/opt/goby-backend-continuation-fixes-20261010` on ext4.
- Go: `/opt/goby-toolchains/go1.27.1/bin/go`, version 1.27.1; ordinary builds use
  `CGO_ENABLED=0`, `GOMAXPROCS=2` and package compile concurrency one. Race builds
  use `CGO_ENABLED=1` and gcc.
- Shared `GOCACHE=/root/.cache/go-build` and `GOMODCACHE=/root/go/pkg/mod`; no
  private cache or copied dependency cache is selected.
- Compiler `GOTMPDIR` and `TMPDIR` both use the task's `compiler` directory.
  Runtime values both use its separate ext4 `fixtures` directory.
- Ordinary media FFmpeg/ffprobe: 9.0.1 under
  `/opt/goby-toolchains/ffmpeg-9.0.1/bin`.
- Credits raw-fingerprint tool: `/usr/bin/ffmpeg`, Debian 7.1.5, with Chromaprint;
  SHA256 `e8a8d46f5225f3062cec7c07fb145d58ae73c603cb740dcd5bad34bfb54e455a`.
- Three independent non-superuser test databases/owners:
  `goby_cont10_general`, `goby_backup_cont10_source`, and
  `goby_backup_cont10_target`. Credentials remain in remote 0600 environment
  files and are excluded from evidence exports.

Initial shared build-cache allocation was 657,793,024 bytes. Preparation
observed about 29.97 GB of persistent availability and 5.43 GB of available
memory. Admission budgets are 6 GiB for task source/binaries/scratch/fixtures,
3 GiB of shared-cache growth and 2 GiB for owned databases, with availability
floors of 10 GiB persistent space and 2 GiB memory. These are current task
budgets, not historical reservations or general deployment limits.

Raw logs, commands, manifests, failed attempts and source variants remain under
the task root. Local preserved evidence belongs to
`.artifacts/backend-continuation-implementation-20261010/` in the original
checkout. Closeout found no task workers or owned database sessions. Compiler
scratch had no data files; its 4,096-byte empty directory was removed. Shared
build-cache allocation was 1,998,471,168 bytes, a 1,340,678,144-byte increase
within budget, and was retained. Owned databases occupied 148,789,969 bytes;
source variants, binaries, raw evidence, databases and the empty ext4 fixture
root were retained separately. Persistent availability at that closeout was
27,551,973,376 bytes. No shared cache, module cache or unrelated resource was
cleaned.

## Integration

Only the selected implementation, tests and review records are included in the
delivery. The staged/committed code archive is checked against the final tested
source manifest before main integration. Delivery uses a fast-forward and a
normal push; the final Git identities and remote confirmation are recorded in
the local task evidence. The original checkout's unrelated file bytes and
deletions have an independent preservation record.
