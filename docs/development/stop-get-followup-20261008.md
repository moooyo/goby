# Stop and cached GET optimization follow-up, 2026-10-08

## Accepted implementation

Main code commit `82dbcd0540017852fac39747b5b1caa80e89fe00`, a child of
`c6f50ec2384c6cd746d73c81b5945bf9ebeb92e1`, contains the accepted combined
candidate and its regression/diagnostic tests. All six delivered file hashes
match the remotely tested frozen inputs. The earlier read-only identity
completion experiment `063736d` remains excluded.

Two local changes retain the original authorization and data checks:

- `lockStateItem` sends its two original statements as one batch. SHARE still
  precedes a separate statement snapshot for direct visibility. Complete media
  decoding, lock order, final authority/time checks and the business COMMIT
  remain. Returned result errors retain precedence over batch Close errors;
  batch preparation errors can occur before any result is available.
- The playback source reader uses a cached plan only for its complex projection,
  when the actual connection uses CacheDescribe and has a positive statement
  cache capacity. The preceding SHARE query keeps its default mode. Capacity
  zero, connection hooks that disable caching and other execution modes retain
  the original batch. The existing bounded driver cache manages plans; no
  result cache, manual prepared-statement registry or retry was introduced.

With fully warmed description and statement caches, the source strategy adds
one query round trip per authorization stage. Cold or evicted entries can require
additional prepare/describe exchanges. It also copies the physical connection
configuration, so its benefit must be evaluated on complete requests. Both fresh
authorization stages, source/publication/root
checks, final playback clock checks and transaction endings remain. A named plan
does not imply that PostgreSQL never creates a custom plan. First-request HTTP
latency and retained backend memory are not measured by this cached-GET result.

## Correctness and matched HTTP comparison

Verification ran only on `test-env`. One diagnostic baseline build and two
candidate builds completed. The selected functional runs passed 27 top-level
tests and 72 subtests with zero failures or skips. Coverage includes actual
connection cache/mode hooks, capacity-one plan reuse, parameter/media freshness,
complete decoding, visibility changes during lock waits, authority expiration,
publication/binding changes, cancellation, failed Stop commits and real owned
producer retirement.

Four initial diagnostic processes ran against c6 production with enhanced
test-only observations. The formal comparison then ran twelve new processes in
fixed BC/CB/BC order, separately selecting normal1 and key8. Each process retained
the original 256-request steady GET wave, Stop overlap, authority and whole-fixture
closure requirements. All twelve formal runs qualified on their first attempt.
The two formal sources use byte-identical diagnostic inputs. Initial diagnostic
timings and older observer versions are not included in these pairs.

| Metric | normal1 paired median change | key8 paired median change |
| --- | ---: | ---: |
| GET p50 | -28.226952% | -18.439924% |
| GET p95 | -25.298032% | -14.687575% |
| GET p99 | -23.570368% | -12.560201% |
| GET elapsed | -27.424659% | -21.639210% |
| GET allocated bytes | -0.342338% | -0.884119% |
| GET mallocs | -1.662591% | -2.767787% |
| Stop response | -7.410005% | -5.933187% |
| Handler-drain observation upper bound | -7.451299% | -5.894217% |
| Lifetime/output-lease observation upper bound | -3.341201% | -2.924548% |

These are medians of three within-block relative changes, not ratios of source
medians. All six GET metrics decrease in all six group/block pairs. Normal1's
three Stop windows decrease in every pair. Key8 block 3 is adverse: Stop response
rises from 22.862118 to 28.342586 ms, an increase of 5.480468 ms (+23.9718%);
drain and lifetime bounds increase by 5.471258 and 5.170645 ms.

Normal1 block 2 baseline Stop is 262.430422 ms and remains in every calculation.
Its COMMIT client callback occupies 247.512481 ms. In key8 block 3, total COMMIT
callback time rises from 4.813393 to 11.122071 ms, with three COMMITs in both
sources. These intervals locate observed time; they do not identify a PostgreSQL,
storage or scheduling cause. The paused NVMe investigation was not resumed.

This is a bounded combined-source engineering result. It does not isolate the
individual changes, establish stable tail latency, repair the cause of every
historical negative sample or prove a whole-service capacity improvement. The
cached performance fixture uses completed producers; the separate functional
producer-retirement tests do not turn its lifetime bound into encoder-reap time.

## Tail association and fixture-close diagnosis

The test wrapper now returns a bounded request ordinal, and each client duration
is stored beside that ordinal. Every steady wave verifies an exact permutation
of 1 through 256. Response-header parsing occurs after the original client timing
window. The wrapper also counts outer HTTP handlers, including unmarked requests.

All twelve formal client p99 samples can be associated with retained server
details. The retained set is still the sixteen slowest server handlers, not the
complete client tail. All matched details retain the original event bounds and
error checks. Source-stage analysis uses the exact adjacent lock/projection pair,
excluding the two unrelated projection queries with the same broad category.

Batch and direct-query timing windows are not identical: the original whole
source batch includes result consumption and JSON decoding before Close, whereas
the sequential query spans end when Scan closes its result. Prepare is nested
inside query time. These differences prevent treating a target-window reduction
as isolated planning or database execution savings. Overall HTTP/handler results
and all adverse samples remain the acceptance evidence.

The close record now retains each original guard's first observed value:
source-stat error, source FD count, both pools' acquired/idle/constructing/total
counts, stream/HLS slots, outer handlers, library availability and HLS completion.
The original failure condition is unchanged; no wait or retry replaces an initial
failure with a later clean sample. The observations are sequential snapshots.

All four diagnostics and twelve formal runs show zero acquired connections,
slots, source FDs and outer handlers at the first close observation. The earlier
baseline close failure did not recur. Its specific failed operand remains
unidentified; this task improves future evidence without declaring that historical
failure repaired or reclassifying its raw result.

## Source, resources and evidence

The combined freeze is
`c32944f2373be47925c8e9b6a2b6457414b9ee08bd0fa615b8b70572cafc8d5b`.
Exact binaries are:

- c6 diagnostic server: `1176288e2d8c6cfc2339165a6ed1a6b3702c55cb80a729dc536b6b6654931f8c`.
- Final server: `404ada888340b006e8d16621e9a5391b969b702416b210d7f80f1aaecd524b1c`.
- Final library: `e9d8c89e73fae59d855f7749c270a97be8843a15eb3d3a1e48841ec80cf5fdf9`.

The library binary includes the same two frozen scan drivers as the historical
binary for the subsequent direct final-version comparison. No second build of
that source is needed. The code-delivery receipt confirms preservation of all
200 original main-worktree WIP paths and their 4,360,439 bytes, status and staged
content. Measured source excludes that WIP. No push or PR was performed.

After the diagnostic stage, a single explicitly selected idle shared-cache
maintenance reclaimed 6547193856 allocated bytes. Modules, source, binaries and
raw evidence were preserved. The wrapper incorrectly required the pre-clean
256-directory layout after Go had removed it; its post-check failed and the Go
exit code was not recorded. This metadata gap is retained as unknown, not
reconstructed as exit zero. Fresh readback verified the exact cache root,
normal empty layout, unchanged services and sufficient cold-build capacity.
There was no second clean. The two candidate builds used separate owned ext4
compiler scratch, each reclaimed only after actual worker exit; native GOTMPDIR
and TMPDIR both selected ext4 media fixtures.

Evidence root: `.artifacts/stop-get-followup-20261008`.

- `stage1` retains all four diagnostics and maintenance/readback records.
- `candidate-stage` retains both builds, the two functional products and all
  twelve formal raw/observation records; 108 exported files matched their hashes.
- `analysis/formal-http-summary.md` and its JSON retain every paired value,
  allocation and negative sample.
- `analysis/formal-http-mechanisms-final.md` and its JSON retain exact ordinal
  associations, target windows, transaction counts and all close observations.
- `delivery/delivery-receipt.json` binds tested bytes to main commit `82dbcd05`.

The [final scan-version comparison](final-scan-version-comparison-20261008.md)
records the subsequent task and final resource closeout separately.
