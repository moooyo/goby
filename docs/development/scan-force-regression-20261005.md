# Force-scan regression diagnosis - October 5, 2026

The fixed diagnosis locates longer client COMMIT waits despite fewer transactions,
and directly observes PostgreSQL WAL waits during matched active COMMITs. It does
not establish that batching causes a storage regression, or that the previous
adverse formal measurements are resolved. Production remains
`d993454fef258301d3ff967c083fa2b2173be802`, published with documentation at
`9abc7abfa3990bcd7c941b274ea5bdcc5e6eb97d`. This delivery changes documentation only.

## Retained formal results and new scope

The [progress-batching report](scan-progress-batching-20261005.md) retains the
original three TRACE0 pairs. Flat-episode force has a paired C/B median of
1.049313307342171, adverse in all three blocks: B [2565.032, 3209.296, 2569.098]
versus C [3351.579, 3367.557, 2586.560] ms. Directory force has median
1.3174699239223628, adverse in blocks 1/2: B [773.289, 764.820, 736.240] versus
C [1018.785, 1136.604, 698.751] ms. These observations remain acceptance evidence;
new references or instrumented durations do not replace them.

Baseline B is `3585a84d612c01d8eead53a3b593d7ddd2557285`; C is the production
commit above. Exactly four processes ran on test-env: reference B/C with TRACE0,
capture directory unset and sampler off, then diagnostic C/B with TRACE1,
SQL_TIMING=0, SQL spans, CPU/runtime profiles and a PostgreSQL observer. Each
retained the complete 21-phase/803-probe descriptor workload in its original
order. Only flat-episode and directory force at ordinals 20/21 were captured.
All four passed, totaling 84 phases/3,212 probes, without retries or extra samples.
All four captures completed with worker retirement and zero incomplete queries.

The artifact-only overlay keeps the original workload and assertions. No product
source or repository measurement driver changed. No full regression, real-media
performance run, local product verification, or new performance matrix was added.

| New unsampled reference, n=1 | B job ms | C job ms | C/B |
| --- | ---: | ---: | ---: |
| Flat-episode force | 3596.142 | 2492.384 | 0.6931 |
| Directory force | 1632.843 | 827.349 | 0.5067 |

This reference direction reverses the old adverse medians. The diagnostic pair
below has C slower again. These distinct observations demonstrate variability;
they do not identify its cause or establish stable improvement. Instrumented
magnitudes are attribution evidence only.

## Job-clipped SQL evidence

SQL intervals are clipped to persisted StartedAt/FinishedAt. Union duration
avoids double-counting overlapping commands. COMMIT durations are client callback
intervals, not server-only execution time. Admission and post-FinishedAt tails,
250 ms terminal polling and process compilation/wall time are separate.

| Diagnostic, n=1 | Flat B | Flat C | Directory B | Directory C |
| --- | ---: | ---: | ---: | ---: |
| Job ms | 2702.420 | 2795.892 | 800.938 | 982.927 |
| SQL union ms | 2130.061626 | 2187.072338 | 644.421469 | 795.895140 |
| Job-clipped COMMIT count | 411 | 219 | 129 | 81 |
| Job-clipped COMMIT ms | 430.499733 | 513.089476 | 128.606754 | 230.813223 |

C adds 93.472/181.989 ms of job time. Its COMMIT interval increase is
82.589743/102.206469 ms, numerically 88.36%/56.16% of those differences.
The SQL-union increases are 57.010712/151.473671 ms; time outside that union
increases by 36.461288/30.515329 ms. These overlapping measures must not be added
as independent costs or treated as a causal allocation of the old formal gaps.
An independent root audit agrees exactly with all four job/union/COMMIT totals.

Flat COMMIT median increases from 0.981926 to 1.192333 ms and maximum from
5.806486 to 21.081107 ms; directory median increases from 0.981027 to
1.473426 ms and maximum from 2.153111 to 13.246466 ms. These are distributions
within one diagnostic job, not cross-run tail-latency estimates.

Full phase counts, including admission/tail, are SQL 6970 to 6010 and COMMIT
413 to 221 for flat episodes, SQL 2062 to 1822 and COMMIT 131 to 83 for directory
episodes. Probe counts remain 192/48, item writes remain 192/48, and rollback
counts are zero. TRACE0 SQL/write fields are uncollected, not measured zeros.

Pairing BEGIN/COMMIT on each backend reports zero pairing issues. The expected
standalone progress transactions disappear, while surviving transaction classes
have longer COMMIT intervals:

| COMMIT class | Flat B count/ms | Flat C count/ms | Directory B count/ms | Directory C count/ms |
| --- | ---: | ---: | ---: | ---: |
| Standalone progress | 192 / 192.841187 | 0 / 0 | 48 / 46.242172 | 0 / 0 |
| Primary media publication | 192 / 210.104915 | 192 / 399.865185 | 48 / 49.693673 | 48 / 119.061722 |
| Folder publication | 18 / 18.741501 | 18 / 83.322847 | 12 / 11.800969 | 12 / 25.888527 |
| Folder image maintenance | none | none | 12 / 11.616603 | 12 / 50.792566 |

Thus flat episodes save 192.841187 ms of standalone progress COMMIT but add
189.760270 ms in primary and 64.581346 ms in folder COMMIT. Directory episodes
save 46.242172 ms while primary/folder COMMIT add 69.368049/14.087558 ms.
Directory image-maintenance commits add another 39.175963 ms at the same count.
These are client-span sums, not disjoint additions to job wall time.

Directory's SQL-union difference beyond COMMIT is 49.267202 ms. Its slower
non-COMMIT templates include unchanged-count catalog snapshots (108 calls,
+9.984233 ms), input lookup (48, +6.067246 ms), theme cleanup (60, +5.250048 ms),
another input lookup (48, +4.719654 ms), extra cleanup (60, +4.405252 ms), and
cached-media lookup (48, +4.193059 ms). These are slower existing work, not new
per-item queries. Template sums overlap where commands run concurrently.

The final transaction's COMMIT starts after persisted FinishedAt and is excluded
from job totals. Its full B/C duration is 0.853800/5.028144 ms for flat episodes
and 0.930668/5.018174 ms for directory episodes; it cannot explain elapsed time
that ends before that COMMIT begins.

## Independent runtime-trace crosscheck

All four traces were aligned by capture anchors and clipped to the actual scan
worker's job interval, excluding unrelated idle workers; transition mismatches
are zero. Flat COMMIT socket Waiting is B/C 413.725/502.412 ms, a difference
of 88.687 ms (94.88% of the instrumented job gap). Directory is 123.494/226.594 ms,
+103.101 ms (56.65%). This independently supports the SQL wait location; trace
and callback boundaries differ, so the values need not match exactly.

Per-file progress COMMIT socket waits fall from 185.618/44.499 ms to zero for
flat/directory. Surviving COMMIT waits grow from 228.107 to 502.412 ms and from
78.995 to 226.594 ms. The benefit is real, but is offset in this diagnostic by
slower remaining commits.

Flat storage-observation helper waiting adds 23.558 ms and worker Running adds
8.276 ms; runnable time falls 0.199 ms. Directory non-COMMIT network waiting adds
39.244 ms, storage-helper waiting adds 16.361 ms, Running adds 17.866 ms and
runnable adds 0.425 ms. Running is not exact CPU time. These trace categories
overlap the SQL measures and do not form an additional SQL-plus-trace total.

Ping adds only 0.172/0.355 ms; waiting for probe results adds 0.128/0.146 ms.
No scan-worker owner-mutex waiting is observed and the largest per-capture
PrimaryIO-class wait is 29 microseconds. The records do not support new
client ownership/governor contention as the principal regression mechanism.
They do not prove that every physical-I/O or scheduling cost is unchanged.

## PostgreSQL wait evidence and limits

One persistent psql observer per diagnostic filtered the exact case application
tag, database and user, excluding itself. It saved backend PID/start, state,
wait type/event and a COMMIT flag without query text or parameters. Matching
requires backend identity and a timestamp inside the client COMMIT and persisted
job. Only active COMMIT samples support the WAL-wait observations below.

| Matched sample counts | Flat B | Flat C | Directory B | Directory C |
| --- | ---: | ---: | ---: | ---: |
| IO/WalSync | 76 | 85 | 14 | 45 |
| IO/WalWrite | 0 | 4 | 1 | 0 |
| LWLock/WALWrite | 0 | 2 | 0 | 0 |
| Commits with at least one sample | 82/411 | 81/219 | 19/129 | 39/81 |

This establishes server WAL waiting during specific client commits, extending
the earlier client-socket-only evidence. The two LWLock samples are PostgreSQL
internal WAL coordination, not evidence of a Goby ownership lock problem.
Sampling misses many short commits and cannot quantify the continuous WAL wait
or assign the entire C/B difference to it. Idle/ClientRead and active NULL wait
are separate; an active NULL wait is not proof of CPU execution. SQL, runtime
socket waits and server samples overlap.

Actual sampling cadence is about 5.1 ms, with all job-window maximum intervals
at most 6.143 ms and no observed backend-identity ambiguity. A secondary estimate
carries a state only to the next same-backend sample within 10 ms, cropped to the
same COMMIT; unrepresented portions remain unknown. The sampler itself adds load
and is absent from the reference runs. Complete collection is not continuous
wait coverage.

PostgreSQL 17.11 uses synchronous_commit=on, fsync=on and
wal_sync_method=fdatasync. track_io_timing and track_wal_io_timing are off;
zero timing counters mean uncollected. No PostgreSQL setting was changed.
Global WAL/checkpointer counters are cluster context, not per-job attribution.
Diagnostic WAL byte deltas are excluded because profile-stop writes are inside
that observation window.

## Log-timezone correction

The previous optional log reader matched UTC suffixes, while the server logs use
Asia/Shanghai. Its no-match output cannot establish an event-free interval.
Correct IANA conversion recovered two events during the previous progress-batching
formal window, UTC 01:19:29-01:22:17: a checkpoint completion at 01:20:32
(4346 buffers; write 194.198 s, sync 4.511 s, total 198.860 s; 2098 sync files)
and a WAL-requested checkpoint start at 01:20:51. They are temporal background,
not proof that a particular scan waited on that checkpoint. The old raw output
is retained. These timestamps belong to progress batching, not to the earlier
A/I/F diagnosis.

The current four-case window is independently UTC 02:00:56-02:03:09. Its
correctly converted log read, including one-minute margins, found zero selected
events. That does not exclude WAL waiting, which is directly observed above,
or establish an unchanged host/storage state.

## Design decision

Static review finds no extra probe, sidecar, primary write, new schema/index or
heavier progress SQL. Neither target is task-owned, so child snapshot work does
not explain the difference. B normally saves Scanned in an entry transaction and
Updated in the media transaction; C saves both in the existing media transaction.
The owner/row-lock order is unchanged. The main change is submission timing and
overlap, not additional database work.

Do not remove ownership locks or immediately batch multiple media publications
in response to this evidence. A media item's catalog, metadata, associations and
accepted counters require atomic publication. Cross-media batches could preserve
that atomicity, but would delay visible success/notifications, roll back earlier
pending items on a later failure, and hold owner locks and descriptors longer.
That changes current per-item progress semantics and needs its own design.

The remaining question is why equivalent surviving commits wait longer under
some execution conditions. A future selected contrast should keep durability
settings fixed and isolate commit-arrival cadence and concurrent storage latency,
rather than repeat the broad matrix or infer a disk defect from WAL samples.
No such follow-up or production repair is executed by this diagnosis.

## Closed resources and retained evidence

All four product invocations and forty profile-reading tool commands exited.
The twelve original CPU/runtime/SQL files, four independent binaries, PostgreSQL
samples and derived records are local; all 130 final remote evidence entries
match their exported hashes. Final effective B/C source manifests match staging.
No live reference remained to owned temporary paths before closeout. Owned RAM
profiles, compiler scratch and empty physical-fixture TMPDIR were removed.
Persistent sources, archives, binaries and evidence remain; shared build/module
caches and old environments were retained. PG/Goby identities and reserve
403,374 are unchanged. Closing persistent availability is 7,531,958,272 bytes.

Evidence is outside the repository at
`D:/Code/goby/.artifacts/scan-force-regression-20261005`. Only this report, the
handoff update and the earlier report's timezone qualification are published.
The original 199 WIP byte states and 56 historical hashes are preserved by the
separate publication receipt.

| Record | SHA-256 |
| --- | --- |
| source-freeze.json | dc066d6476a7cd454cc4bc4da1b60cbbb4e6049a7d0c619e9dc08ae73da41c03 |
| selection.json | a7270c6009317dc32ff8caf9fac7ed380f8335e9349499e5df57e51d889f9a39 |
| evidence/force-qualification-receipt.json | cbd349e2582b6a77623c281d7abf41d8a932ea32399a52b416e69302d19b7026 |
| analysis/root-sql-audit.json | 3408d734dceb2d24d341a6454654a6ba218965a643c1e3569205265a9fa05eae |
| analysis/force-sql-summary.json | 03e312d44433abdeae6f3869ced4cd1e0a247e10bd34d3e9376f63fae1edd045 |
| analysis/force-sql-report.md | 95d9437137bc6ff1ae4adb2ad5d444133963a81290d5b1846ca68eb378293850 |
| analysis/trace-attribution.json | 76ec1209173054a2a96cd0a12778ebda8ba431ea10b05d62b23670e9cc6c7042 |
| analysis/pgwait-crosscheck.json | 34a297dd0024bd92e32c53aaa97d30f234eb8a5d7394d258dc8d029b1fdb4a98 |
| prior-formal-pg-events.json | 5af4007600e5c8a38fa5e9ea972031be430db1d3ad78286c0efc1345238d06ff |
| evidence/closure-receipt.json | a552798f621fb8a0b6eb5b518ea4a489171d92aa4711aa858e9a0b02ad32025d |
| evidence/final-export-verification.json | b56cd91d970e3f58c1e98e718599c6884a381f2332bece43cd8b438acb18d169 |
