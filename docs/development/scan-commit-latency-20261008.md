# Intermittent COMMIT latency attribution, 2026-10-08

Three isolated cold COMMIT spikes include repeated, strictly matched PostgreSQL
IO/WalSync observations. The historical sustained 4-7 ms COMMIT-median episodes
and their large tails were not reproduced or fully explained. This task establishes
a server wait domain for three specific events; it does not establish their
physical-device trigger, resolve every historical regression or repair production.

Published source remains `c5b5b7011c4b7c3a5d6fe38c69b92a5b74d0145d`. All three
diagnostic blocks reuse the same retained final lookup binary `64703882`; there
is no production source change, build, transaction/authorization relaxation,
PG/VM configuration change or durability change. Plan-memory/image-noop work is
not selected. Original dirty-workspace files are excluded from source truth.

## Historical evidence and client timer

The retained twelve earlier performance cases cover sixty phases and 205 large
COMMIT tails. Their slow shape is many remaining COMMITs becoming slower, not
one seconds-long callback. Many BEGIN/retained SELECT callbacks stay short;
Stage1 pre2 also has a slow cold episode without named Prepare and recovers
inside that same process. Those facts remain, without a new version speedup claim.

No logged checkpoint overlaps Stage1. All four cases within Stage3 share one
checkpoint, as do all four Stage4 cases; those overlaps cannot isolate their last
baseline's larger tail. Checkpoint write duration includes pacing, and its file-sync
summary is not an individual transaction WAL-sync duration. Historical logs and
cumulative counters cannot reconstruct the missing per-COMMIT waits or host state.

The recorded COMMIT interval is a Go monotonic client-call duration, including
pgx cache maintenance, protocol/transport, server processing/waits and possible
client/OS/VM descheduling. Owner-lock acquisition and pre-Commit journal work are
outside it, as are post-Commit notification/unlock/pool-release work. CommandTag
was not retained, so callback Err=nil alone is not a complete transaction-success
certificate. Neither callback nor SQL union is exclusive server CPU or WAL time.

## Three separate diagnostic acquisitions

| Block | Actual native | Qualified | Complete phases | Retained outcome |
| --- | ---: | ---: | ---: | --- |
| Spaced plain1/observed1/observed2/plain2 | 4 | 4 | 20 | No historical sustained burst; material launch gaps |
| First continuous attempt | 3 | 2 | 10 | Case3 observer JSONDecodeError/exit -15; case4 not started |
| Repaired continuous fixed four | 4 | 4 | 20 | Three isolated cold spikes; no sustained historical burst |

The total ledger is zero builds, eleven actual native invocations, ten qualified
and one collector interruption, with fifty complete phases. One missing-parent
preparation error started zero native processes and remains separately recorded.
Blocks are not pooled into a source-performance estimate and no failed/partial
case is filled with zero or replaced in its original evidence.

The spaced block has 2,552 COMMITs below 10 ms and phase medians 1.045193-1.207666
ms. Actual launch gaps are 81.017996/21.905600/33.904725 seconds, so it is not a
continuous reproduction. Its active-only collector has 36 full-bracket WalSync
points and 252 point-only observations; idle/nonactive coverage is unavailable.
The partial continuous block has 1,276 sub-10-ms callbacks over two qualified
cases and a 68.192588 ms inter-case gap. It misses the historical late positions;
its 42 full-bracket normal active WalSync points cannot explain the old burst.

## Positive evidence in the repaired continuous block

Atomic publication removes the demonstrated state-file race. The old failure
timing strongly supports this explanation, but lacks an operation-specific trace
that would confirm the original JSONDecodeError's cause. All tagged backend
states are captured. Four same-binary processes and twenty phases
qualify, with inter-process gaps 73.489328/73.269391/72.566146 ms. Phase COMMIT
medians are 1.036247-1.317885 ms; case4's maximum is 6.399275 ms. The old sustained
4-7 ms regime is absent, but three isolated cold callbacks exceed 10 ms:

| Case | Callback ms | PID / ordinal / xid | Strict WalSync points | PG query-start lag us | Last strict request start to callback end ms |
| --- | ---: | --- | ---: | ---: | ---: |
| 1 | 28.794359 | 3824237 / 561 / 1437483 | 6 | 37.568 | 1.228842 |
| 2 | 24.220142 | 3824763 / 4196 / 1438296 | 5 | 44.426 | 0.968008 |
| 3 | 29.107171 | 3825309 / 6460 / 1439046 | 5 | 32.301 | 4.823811 |

Strict association requires the exact application tag, PID, unique backend_start,
active/is_commit state, query_start and xid of that same query generation, with
the complete observer request/response bracket inside its client callback and
persisted job. Across the block, 69 strict points cover 56 callbacks, all WalSync.
PostgreSQL defines this as waiting for a WAL file to reach durable storage.
[PostgreSQL 17 monitoring documentation](https://www.postgresql.org/docs/17/monitoring-stats.html#WAIT-EVENT-IO-TABLE).

The three starts occur only 32-44 us after client callback start, supporting an
observed server-side wait rather than a large pre-send delay for these events.
Their last-strict-request suffixes are unclassified: continued server wait,
protocol processing and client execution remain possible. They do not measure
server completion or a precise client tail. The sixteen repeated strict points
in the three spikes are sampled states; count times a nominal 5 ms cadence is
not a wait-duration measurement.

Another 505 active observations are server-point-only, and five rows with
query_start 1-13 us after server_observed remain temporally ambiguous. There are
33 same-generation idle-last-COMMIT points, zero fully contained idle brackets,
and no idle point inside the three spikes. Missing/point-only support is not
upgraded into continuous occupancy. Actual PG RTT p99/max is 2.424285/4.667384 ms;
maximum request interval is 5.983204 ms. Full distributions and endpoint scopes
remain in the external report and machine data.

## What the background/host data do not establish

First observed dedicated-database autovacuum and checkpointer activity appears
after all four cases. There is no contemporaneous maintenance support in these
observations. Statistics refresh and coarse process-IO intervals cannot exclude
every checkpoint or scheduling effect. Disabled WAL/IO timing fields are
unmeasured, rather than zero-cost evidence.

The final read-only PG log check covers the repaired native interval
17:51:20.108849726-17:51:55.178845265 UTC on 2026-10-07. The preceding checkpoint
runs 17:42:06.692-17:44:26.985; the next starts at 17:52:06.286, 11.107 seconds
after the final case. There is no logged active-checkpoint overlap with this
block. This does not exclude other IO or scheduling effects. Exact original log
context is bound below.

Guest IO bins around 203 ms contain many operations; flush counts/times cannot
be assigned to one COMMIT or added to callback time. `owner_cgroup` actually
samples the observer's own group, so its zero throttling does not exclude PG/native
scheduling delay. PVE has no new guest-host clock anchor or QMP observation. This
does not directly identify an NVMe operation or exclude client/host scheduling.

The three observed wait-domain events are a mechanism clue for the historical
205 tails, not retrospective unique-cause attribution. Further selected work
would need per-backend synchronization/scheduling evidence and a controlled view
of the original preceding write/setup pressure. No tracing tool installation,
PG/VM setting experiment, extra runtime or production optimization is implemented
or implicitly authorized by this conclusion.

## Actual closeout and delivery scope

All three blocks are actually closed. Repaired-block evidence is independently
exported and SHA matched; all owned workers/observer tags are gone. Its closeout
reclaims 85,409,792 allocated bytes of verified temporary copies/scratch; task RAM
is zero. Original corpus/source/local binary/raw, all earlier attempts and shared
caches/data remain. Final persistent availability is 724,320,256 bytes; shared
build cache remains 6,158,499,840 bytes. PG/Goby/QEMU identities and ext4 reserve
403374 blocks are unchanged. This is evidence closeout, not source cleanup.

Only this report and a handoff preface are delivered. SourcePaths is empty;
exact final main identity and fresh WIP/index preservation are recorded in the
external publication receipt. Historical protected bytes are not restored over
newer legitimate changes.

## Evidence bindings

Paths below are relative to `.artifacts/scan-commit-latency-20261008`.

| Evidence | SHA256 |
| --- | --- |
| [Repaired report](../../.artifacts/scan-commit-latency-20261008/runtime-contiguous-repaired/analysis/repaired-report.md) | `59ae0f1e12db5c580317b40766b867a1e5795df2ee5ca28b8e56670039ce8300` |
| [Independent PG/background review](../../.artifacts/scan-commit-latency-20261008/runtime-contiguous-repaired/analysis/pg-background-review.md) | `ef88c7b562e631c08d63a11f67e49162de52f8faac00b6282b4b272fee7efb8c` |
| [Repaired checkpoint log context](../../.artifacts/scan-commit-latency-20261008/repaired-checkpoint-context.json) | `5682dc1ff8a72de9d8633be0bdf30448199dfc3276004b955154b36fe9590960` |
| [Runtime ledger](../../.artifacts/scan-commit-latency-20261008/runtime-summary.json) | `3910375b2a3d864084972ca00d58b001b98755c66d022bf0dec89c2e1b361f2c` |
| [Guest closure](../../.artifacts/scan-commit-latency-20261008/runtime-contiguous-repaired/actual-closure.json) | `84c6fba53fc2c72723449c3bfbba8ed0af1425daf7525f1987aece24751b2af9` |
| [PVE closure](../../.artifacts/scan-commit-latency-20261008/runtime-contiguous-repaired/evidence/pve/actual-closure.json) | `b23e0765d51d51dae2ca056e1eed8cab0c34394e00df2f8467075cc3fda2e752` |
| [Closure export verification](../../.artifacts/scan-commit-latency-20261008/runtime-contiguous-repaired/closure-export-verification.json) | `0494431fe3cad8a6c4f1f342c48144d6ba2d7655757a527cb4607b3087b5f7eb` |

[Historical timing](../../.artifacts/scan-commit-latency-20261008/historical-timing-report.md),
[callback boundary](../../.artifacts/scan-commit-latency-20261008/commit-boundary-review.md),
[first spaced report](../../.artifacts/scan-commit-latency-20261008/runtime/analysis/first-diagnostic-report.md)
and [partial continuous report](../../.artifacts/scan-commit-latency-20261008/runtime-contiguous/analysis/completed-two-report.md)
retain their separate intervals, distributions and failed/partial labels.

