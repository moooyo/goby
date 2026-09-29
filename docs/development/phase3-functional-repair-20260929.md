# Phase 3 local functional repair - September 29, 2026

Status: **this functional repair milestone is complete**. Local compilation,
six focused Go tests, 73 workload tests, and the 20-media and 10,000-media HTTP
journeys passed. Strict capacity and fault acceptance remain separate work.

## Current execution agreement

The user authorized a smaller, functional-first milestone after reviewing the
[previous failure](session-handoff-20260929-phase3-functional-reconciliation.md).
Local compilation is allowed; tests and application execution use `test-env`.
Historical latency targets and constrained capacity profiles do not gate this
milestone. The old authority-publication, one-shot controller, and independent
reader chains are not prerequisites for these isolated development tests.

Each test uses its own media directory and PostgreSQL schema, closes its server,
and removes only its own fixture. Existing unrelated checkout changes and shared
remote services are outside the work. Retain exact final identities, safe
deletion, rollback, and successful job completion as correctness requirements.

## Repair and scope

The missing-item reconciliation proof now has an aggregate allowance of up to
12 seconds, capped by the catalog transaction deadline minus five seconds.
Individual filesystem observations retain their five-second limit. Final
identity checks, cancellation, rollback, and a live proof context before commit
remain required. Structured failure logs identify the job, stage, and error
class without logging raw errors or paths.

The workload actor permits a total-count difference of one during the known
incremental add/delete transition while retaining exact page IDs and ordering.
Settled queries again require exact totals. A short copy-only remux child can
be witnessed during a verified media response without requiring a sampled CPU
tick increase. Seven new tests exercise positive and negative cases; the full
workload test file contains 73 passing tests.

The new opt-in HTTP test uses production `Server.New`, actual FFmpeg/ffprobe,
real TCP/TLS requests, PostgreSQL, and scan-evidence spooling. It generates
independent one-second H.264 MP4 files across two roots and 4,200 directories.
Each directory has 80 zero-byte nonmedia files with 237-byte names: 336,000
entries and 79,632,000 filename bytes. This retains the previous directory-load
shape without requiring the old compound workload and orchestration chain.

The journey verifies forced initial probing, a cached scan, one addition, one
deletion, a cross-root rename, and a final unchanged scan. It checks exact Movie
identities through SQL and paginated HTTP, root/path ownership, nondefault user
state preservation, successful scan jobs, observed query/scan overlap, and
direct MP4 response bytes. Direct byte verification is not a remux/transcode or
productive intro/BIF acceptance result.

## Recorded verification

| Check | Result |
| --- | --- |
| Local Linux/amd64 application compilation, CGO disabled | Passed with Go 1.27.1 on Windows; no local test execution |
| Local Linux library and server test compilation | Passed; no local test execution |
| Six focused remote Go tests | Passed with no missing tests, failures, or skips; package time 6.763 seconds |
| Focused 4,200-directory successful deletion | Passed in 1.659 seconds; separate unsafe-path and cancellation cases also passed |
| Remote workload Python suite | 73 passed, including seven new tests |
| 20-media real HTTP journey | Passed, no skip, 200.77 seconds including fixture and cleanup |
| 10,000-media real HTTP journey | Passed, no skip, 2,099.59 seconds including fixture and cleanup |

The 20-media phase times were 45.51 seconds cold, 35.51 cached, 53.61
incremental, and 59.54 settled. Incremental completion reported exactly
20 scanned, one added, and one updated; the final Movie population remained 20.
The cached, incremental, and settled phases observed 355, 536, and 595 stable
query requests bracketed by running-job observations. The direct response
matched all 2,082 bytes of the generated unchanged MP4.

The larger journey completed with the following results. Each phase's complete
paginated HTTP result and SQL Movie identities agreed on exactly 10,000 media
items across the two roots.

| Phase | Seconds | Scanned | Added | Updated | Observed overlapping queries |
| --- | ---: | ---: | ---: | ---: | ---: |
| Cold, forced probing | 1,817.21 | 10,000 | 10,000 | 0 | 0 |
| Cached | 79.41 | 10,000 | 0 | 0 | 795 |
| Incremental | 71.90 | 10,000 | 1 | 1 | 719 |
| Settled | 92.41 | 10,000 | 0 | 0 | 926 |

The removed Movie ID was absent, the newly added Movie did not reuse it, and
the moved Movie kept its ID and complete saved user state. Direct HTTP returned
the unchanged MP4 bytes during the incremental journey. The test does not
claim a separately bracketed playback/scan overlap interval. Both HTTP runs
completed their server, media-directory, and isolated-schema cleanup without
errors. The shared PostgreSQL service was left running.

Raw logs are retained under local `.git/phase3-functional-20260929/` and remote
`/opt/goby-test/codex-scan-proof-20260928-01/artifacts/`. Their SHA-256 values are:

| Evidence | SHA-256 |
| --- | --- |
| `focused.jsonl` | `310ea527192283964c3c9519894159a58034a76392915ead1d2763c4b1db8c2c` |
| `workload-tests.log` | `bd62c91975723f0580d7fbf48ca03e8232633092677fe105576896b44f016e9d` |
| `http-20-20260929-150113.jsonl` | `74d69c74ba17322fdfe0e6aa55dbcf1c388aa4e01456dcc8f7608b9884d092c0` |
| `http-10000-20260929-150518.jsonl` | `127dabb90432dced960fde809f8b2fd6e651dd1340ab4ace94687fdf8c265129` |

The first attempt at the old overlay verifier stopped before tests because of
its archive-member check. The four reviewed source files were then uploaded
directly and the six tests ran through a small runner. No product failure or
test pass is attributed to that packaging attempt.

## Reproduction and remaining work

On the configured remote test environment, provide `GOBY_TEST_DATABASE_URL`,
`GOBY_FFMPEG`, and `GOBY_FFPROBE`, and use an ext4 temporary directory:

```sh
CGO_ENABLED=0 GOBY_PHASE3_FUNCTIONAL=1 GOBY_PHASE3_FUNCTIONAL_ITEMS=20 \
GOBY_PHASE3_FUNCTIONAL_NONMEDIA_PER_DIRECTORY=80 \
go test -count=1 -timeout=125m \
  -run '^TestHTTPPhase3LocalFunctionalReconciliation$' ./internal/server
```

Set `GOBY_PHASE3_FUNCTIONAL_ITEMS=10000` for the larger confirmation. Movie
counts exclude the additional Folder and library rows; this is not the old
exact-10,000-catalog-item capacity profile. Performance observations from this
fixture and relaxed development profile are not capacity claims. These runs
used Go 1.27.1, FFmpeg/ffprobe 7.1.5, CGO disabled, `GOMAXPROCS=4`, and a 6 GiB
Go memory target. They did not reproduce the old business cgroup limits.

Strict 10k/100k compound capacity, overload, the 28 real fault cases, and the
final 54-stage regression remain deferred and unaccepted. They are not gates
for this local functional repair milestone. The original failed func05 result
remains failed; its five-second timing was evidence for the aggregate-deadline
hypothesis, not a recovered bottom-level error receipt.
