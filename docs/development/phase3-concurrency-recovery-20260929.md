# Phase 3 compound media, catalog scale, and local recovery - September 29, 2026

Status: **this functional continuation is complete**. It builds on the
[reconciliation repair](phase3-functional-repair-20260929.md) at `5444369`.
Small compound media, 100k catalog isolation, their combined same-server
scenario, and selected actual PostgreSQL/mount recovery checks passed.
The historical strict capacity and full fault matrix are not claimed.

## Execution boundary

The user's functional-first agreement remains in effect: local compilation is
allowed, while tests and runtime work use `test-env`. No old authority-publisher
or one-shot orchestration chain was used. Tests own disposable PostgreSQL
schemas and temporary directories. Mount changes ran only in a private mount
namespace. Shared PostgreSQL and the host were not restarted.

This continuation changes test coverage and fixture preparation; it does not
change product query, media, or recovery implementation. Unrelated checkout
changes remain outside the delivery.

## Completed verification

| Scope | Actual result and limit |
| --- | --- |
| Eight focused server/library tests | All passed without skips: progressive video negotiation/seek, producer stop, real HLS/random access and cancellation, original-stream owner quota/recovery, actual owned PG backend termination, and two real statement-timeout/rollback cases. The quota source is a delivery fixture, not decoded media. |
| Four video/HLS tests with FFmpeg 9.0.1 | Passed again using the analysis-compatible toolchain. These overlap the eight focused tests and are not four additional distinct cases. |
| Native fingerprint helper | Built from the vendored Chromaprint 1.6.1 subset and existing CMake source inventory/defines with C/C++ compilers; seven protocol tests passed. No shared tool installation was needed. |
| Four actual extraction tests | Audio fingerprints/nonzero origin, fractional audio tail, long/VFR preview frames, and visual/VFR streaming previews passed without skips. |
| Actual mount recovery | `TestRootBindingFullScanMountNamespaceHelper` passed seven complete scans with real ffprobe, missing/replacement mounts, Store reconstruction, original remount, exact identities/UserData, and explicit mount cleanup. This is not an OS reboot or physical blocked-I/O case. |
| Default catalog profile | 10,000 SQL-seeded leaves plus 442 folders across two libraries passed in 9.02 seconds. |
| Expanded catalog profile | 100,000 SQL-seeded leaves plus 442 folders passed in 32.28 seconds, including counts, first/middle/last pages, ACL/UserData, independent reads during an actual owned row-lock wait, cancellation rollback, and reuse of the same owner connection. |
| Small compound media | 100 real Movies and three generated Episodes passed in 13.16 seconds, including setup and cleanup. |
| Same-server compound with large catalog | 100 real Movies, three generated Episodes, and 100,000 additional SQL-seeded leaves passed in 47.65 seconds, including setup and cleanup. |

The expanded standalone catalog fixture seeded 217,620,480 allocated schema
bytes. Its 20 measured page requests had a maximum observed duration of 525 ms;
the actual blocked-owner observation lasted 524 ms. These are observations,
not SLO or hardware-capacity claims. Default 10k behavior remains available;
the larger population is explicitly enabled by `GOBY_PHASE3_CATALOG_ITEMS=100000`.

The compound scenario uses production `Server.New`, real TCP HTTP, actual
FFmpeg/ffprobe, the native fingerprint helper, and the production analysis and
transcode workers. It verifies full copy remux and a 4.25-second transcode seek
by decoding the returned MP4, including frame count, geometry, audio and visible
color. Each Episode produced 342 audio and 90 visual feature samples. Their
synthetic sine audio yielded the truthful `no_result / low_audio_entropy`
outcome; this is execution coverage, not intro detection accuracy acceptance.
Each generated HTTP BIF contained 23 frames; first, middle, and last JPEGs were
decoded and checked at width 240.

The final large-catalog run observed three stable-query intervals, two complete
large-catalog page/count groups, one intro-running sample, one preview-running
sample, and playback requests bracketed by an active scan. Both synthetic
libraries retained exactly 50,000 leaves and 50,221 total rows each, with zero
scan jobs: synthetic catalog rows were never treated as real media files.
The active compound interval was 10.92 seconds. This does not establish the old
minimum five-lane CPU-overlap interval or sustained load distribution.

All passing fixtures completed their normal worker, connection, temporary
directory, schema, and applicable mount cleanup. The existing shared services
were preserved.

## Failures resolved within the local verification layer

The system FFmpeg 7.1.5 could not satisfy the existing analysis timestamp parser,
which requires 9.0.1. Its four extraction failures remain recorded. Selecting
the already installed `/opt/goby-toolchains/ffmpeg-9.0.1/bin/` tools passed the
same tests without changing product version checks.

The first compound test incorrectly expected `queued` instead of the HTTP scan
state `pending`. After correcting that assertion, the 20-Movie scenario finished
its work but did not observe preview/scan overlap. Using 100 actually probed
Movies and requiring each analysis kind separately provided the needed overlap.

The first same-server 100k attempt exceeded a 30-second client deadline on a
recursive ParentId deep page before any media tasks started. A focused
diagnostic retained the same endpoint and parameters. Before statistics were
prepared, `items.reltuples=-1` and both analyze timestamps were absent; the
global count took 133 ms, while the ParentId count hit its 10-second diagnostic
deadline. After only `ANALYZE items`, the estimate was 100,442 rows, and the
same ParentId count, first page, and deep page passed in 338, 670, and 1,006 ms
with exact results. The compound bulk-SQL fixture now explicitly analyzes only
its own `items` table. No product SQL was changed; the diagnostic is not counted
as acceptance or as proof of an exclusive execution-plan cause.

A subsequent run completed real work but missed the short intro-running
interval while its synchronous large page requests were in flight. The test
now waits for the actual scan to run before admitting analysis, and observes
intro execution before issuing the expensive page group. It does not delay
production handlers or weaken output/overlap assertions. The final repeated
same-server scenario passed.

## Reproduction and retained evidence

Use the configured remote PostgreSQL URL, Go 1.27.1, FFmpeg/ffprobe 9.0.1,
`GOBY_INTRO_FINGERPRINT` pointing to the separately built helper, and an owned
temporary directory. No local test execution was performed.

```sh
CGO_ENABLED=0 GOBY_PHASE3_CATALOG_ITEMS=100000 \
go test -count=1 -timeout=12m \
  -run '^TestHTTPCatalogCapacityKeepsACLAndUserDataDuringOwnedTransactionBlock$' ./internal/server

CGO_ENABLED=0 GOBY_PHASE3_COMPOUND=1 GOBY_PHASE3_COMPOUND_CATALOG_ITEMS=100000 \
go test -count=1 -timeout=35m \
  -run '^TestHTTPPhase3CompoundFunctionalMedia$' ./internal/server
```

Raw results are retained locally in `.git/phase3-concurrency-20260929/` and
remotely in `/opt/goby-test/codex-scan-proof-20260928-01/concurrency/`.

| Evidence | SHA-256 |
| --- | --- |
| `catalog-100000-20260929-160253.jsonl` | `579c75adbf3bd41f0cf41178e7d89a9e4f177f19d59f65f16f0ded01fc11d602` |
| `compound-20260929-162311.jsonl` | `c46ebf53f9afe5ad03196e9b91fef922950d1cfe4f04955e989f117e55f0aab5` |
| `parent-diagnostic-20260929-161824.jsonl` | `e9335cbb01ab1b19a0932068c9cd5b7ed31d885e379bb561662cf202f5c6b34a` |
| `mount-w4eYamwR/mount.jsonl` (local `mount.jsonl`) | `37b04bfa4cdbe88d02742baed6837a85917256267f502564305f5f6eb7e55b41` |

## Remaining Phase 3 work

Real process/PG crash and restart, guest reboot/reset/late mount, actual blocked
filesystem operations, non-root permission recovery, and isolated derivative
ENOSPC remain open. The seven mount scans and PG checks above do not silently
fill the historical 28-case matrix. Whole-phase final regression, migration/
backup/recovery composition and final delivery remain open. A 100k real-media
cold/cached/incremental workload, sustained overload and the historical strict
resource/SLO profile have not been accepted. Continue with small owned recovery
scenarios before expanding those cases; retain the user's relaxed performance
and simplified orchestration agreement.
