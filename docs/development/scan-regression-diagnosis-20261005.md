# Remaining scan regression diagnosis - October 5, 2026

The fixed diagnosis locates the dominant excess time in client COMMIT socket
waiting, with per-file progress persistence a major amplifier. It does not identify
PostgreSQL's internal WAL/fsync or host cause, and implements no production repair.
All five selected processes passed, with 105 phases, 4,015 probes and nine complete
job captures. Original formal regressions remain valid observations in their own
batch; the larger new n=1 diagnostic gaps do not replace them.

## Frozen scope and comparability

Production sources are A `7e959c136f7ed1402513a61841153d505c9cc3b6`, image-only
I `924d2f1db719f1adf174401a90b01cb608c2a18c`, and full
F `42cb91f66b6542c2b7dc5c6d7aca1f800d765983`. Current main before this record is
`d4cf2b5db8d6656ca38052f0324563d9486c3e74`, whose production source is F.
All sources use the same two-file diagnostic overlay retained only in artifacts;
no overlay, product source or new test code enters the repository.

The fixed order was reference A/F with TRACE0 and capture disabled, then diagnostic
A/I/F with TRACE1 and SQL_TIMING=0. Each descriptor400 process preserved all 21
phases and 803 probes, including predecessor state. The three selected targets
were flat_movies incremental, directory_episodes incremental, and
directory_episodes force_probe. Nine CPU/runtime-trace/SQL capture bundles completed.
This is n=1 reference/diagnosis work, not a new stable performance matrix. Race,
profiling, diagnostic SQL and observer windows are not pooled with old formal jobs.

## SQL attribution in the current diagnostic samples

Times below are milliseconds. COMMIT is measured client-call duration, not an
isolated server flush measurement. Shares use the current diagnostic job delta.
Each SQL span is clipped to the persisted job interval: pre-admission work and
post-FinishedAt tails are excluded. Runtime trace intervals use the same job cut.
SQL and trace durations overlap and are not added together.

| Target / contrast | Earlier formal result | New job delta | All COMMIT delta | Share |
| --- | --- | ---: | ---: | ---: |
| flat_movies incremental F/A | +15.5-24.7 ms; 3/3 slower, median +18.506 ms | +732.474 | +700.575249 | 95.65% |
| directory incremental F/A | 2/3 slower in the fixed ablation | +260.837 | +259.874769 | 99.63% |
| directory force_probe F/I | +28-44 ms; 3/3 slower, median +31.434 ms | +547.054 | +535.631865 | 97.91% |

SQL work counts did not increase to explain these gaps. The image change removes
one empty image transaction for the incremental addition. The independent root
transaction audit separates pure persistProgress transactions from other commits:

| Target / contrast | Pure progress COMMIT count, numerator/denominator | Denominator total | Numerator total | Difference |
| --- | --- | ---: | ---: | ---: |
| flat_movies incremental F/A | 160 / 160 | 162.592412 | 820.506127 | +657.913715 |
| directory incremental F/A | 48 / 48 | 42.306960 | 244.479542 | +202.172582 |
| directory force_probe F/I | 48 / 48 | 47.358829 | 268.890335 | +221.531506 |

Not every COMMIT is progress persistence. Force probing also commits substantial
primary-content publication work. Owner/safety locks remain required; their
existence is not evidence that removing them would address this wait.

## Independent runtime-trace cross-check

All nine parsed traces were cut by capture/job anchors, with transition mismatches
zero. Attribution uses the real job worker, excluding other Store workers' idle
channel waits. For movie incremental, A worker G30 versus F G14 spent
169.261/869.432 ms in COMMIT socket Waiting: +700.171 ms, 95.59 percent of the job
delta. Its 160 prepareScannedMedia -> persistProgress -> ownedTx.Commit waits total
156.575/814.425 ms, a +657.850 ms difference, matching the SQL component closely.
Directory incremental COMMIT network Waiting was A/F 72.786/332.338 ms;
directory force_probe was I/F 141.386/676.379 ms.

Movie Ping was A/F 6.255/6.878 ms; it bypasses the SQL tracer and remains a small
coverage blind spot. Runnable was 0.711/1.229 ms and Running 66.887/75.346 ms.
Running is not exact CPU time. These worker observations provide no new client
lock-contention evidence; sparse whole-capture CPU samples are not used for precise
job CPU attribution. They do not exclude an unobserved server/host contribution.

The existing 250 ms terminal polling cadence can amplify observed completion
windows. Keep that observer effect separate from persisted job_elapsed_ns and
SQL/runtime job intervals; it does not redefine the original formal job deltas.
PostgreSQL internal WAL/fsync and host behavior were not measured. Bounded existing
PG logs contained no matching event, which is not proof of no wait. No strace/perf
installation or broader experiment was added, and these results do not establish
stable tails or transfer an n=1 magnitude to previous three-block measurements.

Later correction: the optional log reader's UTC-suffix filter did not cover the
server's Asia/Shanghai timestamps, so the retained no-match result is an
incomplete search. The [force diagnosis](scan-force-regression-20261005.md#log-timezone-correction)
documents the corrected reader and events recovered for the later progress-batching
formal window; those events are not reassigned to this earlier A/I/F window.

The time shape is retained across all five runs. Reference F cold scans remained
faster, but its large slow suffix began at ordinal 9. Diagnostic A's first six
phases were slow and then recovered; diagnostic F's first eleven were broadly
normal, with slowdown from ordinal 12. This shape proves neither a host cause nor
a source cause. All 105 phase rows remain in external sql-analysis-report.md and
sql-analysis.json, without hiding the earlier phases or selecting only targets.

## Closeout and next decision

Each A/I/F final source manifest matches its staged identity. All 252 final evidence
files and five binaries were exported and hash-matched; all 27 original profile
files are retained locally. Owned RAM profiles and empty compiler scratch/fixture
temporary paths were removed with no live references. Persistent sources,
archives, binaries, raw and derived evidence remain. Shared build/module caches
were not cleaned. Closing availability was 11,899,817,984 persistent bytes and
6,262,153,216 /dev/shm bytes; PG/Goby and reserve 403,374 stayed unchanged.

One unrelated player container changed identity after preflight; five other
container identities matched. The owner recorded this external change without
intervening. Do not describe all Docker identities as unchanged or attribute the
COMMIT waits to that change.

If a repair is selected, prioritize batching/time-throttling progress persistence
with a final flush while retaining cancellation, token/owner fencing, terminal
state and actual retirement. This report selects no product change or lock removal.
A separate bounded observation is needed to distinguish any PG internal cause.

## Retained evidence

Full data remains outside the repository at
`D:/Code/goby/.artifacts/scan-regression-diagnosis-20261005`. Only this report and
the complete historical handoff are delivery documents. Final SQL/time classification
and independent job-scoped trace attribution are frozen in the records below.

| Record | SHA-256 |
| --- | --- |
| source-freeze.json | 74fee6f0d3f2e0f17da2ecf62afe380d38c6bfe95a89bac6852f7f4ae517641a |
| selection.json | a503340a1949e78a6d42506075b1c26c22412b35fbe6e20e9b45675d8651f0c7 |
| evidence/diagnosis-qualification-receipt.json | 52fe36a1ac7fe8439d6241d0b59200f63639b72278b06439e5bae81970c44b03 |
| analysis/root-commit-audit.json | dfaf834cd5c2e2545a8dc7a7f2fc3d96a80c17cb625f76162e7967edbbcf6bca |
| evidence/closure-receipt.json | c00678e5dff83a005257ca9153c738a0f6863c1b19f83b54c681d59d4abe140a |
| analysis/sql-analysis-report.md | ab5a51cb5d57d70f4fcf7e349f42b3c135d1f61c8edac873a4741e790549fb40 |
| analysis/sql-analysis.json | ac34a4b42fd9d629549dec3626f5710b70417b9ef0b8cf03f4b6b9e92905cfb2 |
| analysis/trace-attribution.md | 68cc7cdb9992766406dea304fa01f8a5af23d20030790244a13b9d8a424730fb |
| analysis/trace-attribution.json | 1d8ee88940041639c2619473774eebb8affa1f035a3e21b1c5615882c7e4539a |

The external publication receipt records subsequent main/origin readback and
preservation of the original 199 WIP byte states and 56 prior distinct hashes.
