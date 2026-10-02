# Media performance closeout and Git delivery - October 2, 2026

Subsequent selected scope: the user resumed the first follow-up on October3.
[Source integration, metadata concurrency and analysis cleanup](media-performance-integration-20261003.md)
are now verified at source6d681bc. The remaining work families below retain their
scope and are not accepted by that selected integration. This document preserves
the original October2 interruption, delivery and resource state.

The user requested session closeout, an updated handoff, and publication of code
that is ready to merge. Further development and verification were explicitly
stopped. No new test, build, implementation campaign, feature enablement or
deployment is authorized by this closeout.

## Delivered source

The previously verified scan improvement is on main and was pushed to origin:
`e37b04f6cfeac989e21483780f21c8b21fc2c487`.
Exact remote readback returned that same commit. Its source and original
verification are documented in [scan performance](scan-performance-20261001.md)
and the corresponding result manifest. This publication moved origin/main from
`83e1b7f` to `e37b04f`; no uncommitted working-tree code entered that push.

Later performance working-tree increments are not included in this source
delivery. The October 2 integration candidate has unresolved failures and an
interrupted verification run. A separate attempt to reconstruct the earlier
77-file follow-up overlay also could not establish an exact final source match:
two files differ from the initial full manifest and five differ from the final
manifest. No additional implementation was selected from that candidate.

The original workspace `D:/Code/goby` retains the later source and unrelated
drafts. Temporary publication worktrees were archived as recoverable snapshots.
Prepared peer, metadata-comparison and analysis-close fixes are retained in
`.artifacts/performance-publication-20261002`, with their verification boundaries;
they are not claims about code published on main.

## Final execution and resource state

The verification wrapper PID2593935/start50851257 and its actual Go/server test
tree were stopped through captured pidfds at the user's explicit request. No
additional test package was started after that request. The stop receipt records
zero remaining captured live processes. The partial run remains interrupted,
not passing or complete.

Completed package observations from that unpublished candidate were:

| Package | Top-level PASS | FAIL | SKIP | Status |
| --- | ---: | ---: | ---: | --- |
| database | 76 | 0 | 0 | Completed |
| identity | 201 | 0 | 0 | Completed |
| library | 1217 | 1 | 11 | Completed, failed |
| media | 636 | 4 | 8 | Completed, failed |
| server | Partial | Unknown final | Partial | Interrupted |
| Remaining planned packages and builds | Unrun | Unrun | Unrun | Canceled |

The library failure is a valid concurrent metadata edit rejected by the new
cold-probe whole-row comparison. The four media failures share a repeated stderr
Close that replaces successful EOF with cleanup cancellation. Proposed fixes
were preserved without executing their follow-up checks. These diagnostics do
not alter acceptance of the older delivered scan commit.

The exact restarted private PostgreSQL17 instance PID2592733/start50794543 was
then stopped normally with smart shutdown, exit0, after observing zero other
client backends. The final receipt records no task-owned process references,
reserved-UID65534 processes, mounts, loops or native cgroups. Shared PostgreSQL
PID898/start689 and protected Docker PID1452/1455/1467 remained unchanged.

Remote source, database data, fixtures, cache, logs and original failures remain
at `/opt/goby-performance-roadmap-20261002-01`. There is no active worker or
private database to poll. Do not restart controllers, initialization or tests
until the user explicitly selects further work.

Local evidence:

- Interrupted evidence archive SHA256:
  `de391c3eeae30d28b3c458d34f11a9c90d8c8c7864db68e18eca7bbbedb42e1d`.
- Final closure receipt SHA256:
  `25eed0c20bfe484257a249002e9afccd1f4af0002b36cd4060e3d6b7d26b20f7`.
- Source/provenance report:
  `.artifacts/performance-closeout-20261002-session/followup-provenance-reconstruction1/e37-plus-followup77-report.json`.
- Proposed fix snapshots, earlier failures, execution state and receipts:
  `.artifacts/performance-publication-20261002`.

## Work remaining for a later scope

1. Integrate the later working-tree increments from coherent frozen source,
   including the metadata/source comparator and idempotent analysis stderr
   Close fixes. Verify source provenance before publishing those increments.
2. Complete Stop recovery when early-intent capacity is exhausted, diagnose
   delayed-input retirement, prevent task resurrection, and qualify complete
   existing Emby-client A/V, fMP4, subtitles and native/source clocks.
3. Finish analysis dual streams, actual RLIMIT/kernel collector proof under
   unchanged whole physical/inode budgets, and writer/broker/finalizer/Manager
   storage and process-resource integration.
4. Repair scan-read ownership batching for the retained64 versus auxiliary256
   contract; finish HLS/transcode, subtitle/provider/artwork and directory/walk
   read consumers, then verify actual mixed-reader workloads.
5. Complete reconciliation candidate hints, larger-page and final filesystem
   work outside long catalog ownership; measure complete cold/warm/force scans
   with concurrent GET/Stop, tail latency and whole-service resource closure.

P1/P2 observations retain their recorded costs and tradeoffs; P7 acceptance is
parser-resource scope only. Neither the seven-priority program nor full client,
GPU/encoding throughput, whole-service capacity or a replacement Docker image
is accepted by this closeout.
