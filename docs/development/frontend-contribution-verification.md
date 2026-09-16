# Frontend contribution contract verification

The saved producer and consumer contract tests passed on `test-env` for commit `43a9b76a93615b7f872f48dae4e9b3a73ca050b0`. Each test file ran once in a separate bounded unit. These are synthetic tooling results, not an actual frontend build or product-acceptance result.

| Test group | Top-level tests | Subtests | TAP passed | Attempts | Exit | Peak cgroup memory |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Producer | 7 | 8 | 15 | 1 | 0 | 38,203,392 B |
| Consumer CLI | 5 | 9 | 14 | 1 | 0 | 42,721,280 B |

Both TAP reports have zero failures, cancellations, skips, and TODOs. The producer covers physical/virtual/unknown contributions, UTF-16 versus UTF-8 sizes, final-asset hashes, package identity boundaries, unattributed JavaScript, changed inputs, and symbolic-link rejection. The consumer covers explicit sidecar binding, legacy manifest compatibility, complete JavaScript partitioning, and report/copy mutation before final success.

The selected runtime was observed as Node `v24.20.0`:
`/root/.cache/agentic-review-toolchain/node-v24.20.0-linux-x64/bin/node`.
Its executable is 126458664 bytes with SHA256 `89af8424dd53e560b1933f87ba650d8bf57c83ca5a04600eefb31f416aabbae7`. The pinned runtime record also contains seven loaded shared-library file identities and the virtual `linux-vdso.so.1` entry. Source and runtime identities were checked before and after each group.

Each unit used 128 MiB MemoryMax, zero swap, 25% CPU quota, a 120-second runtime limit, PrivateNetwork, ProtectSystem=strict, and writes restricted to the new test scope. Source files and the selected runtime were read-only. The test runner used `--test-isolation=none` and a 32 MiB old-space limit; child helpers remained subject to the unit aggregate cgroup limit. Both units were unloaded after completion; their cgroup paths and recorded PIDs were absent, and their temporary directories were empty. Seven observed consumer fake-Go helper processes used the pinned Node executable; the observations are not a complete syscall trace.

A preflight attempt was rejected before creating any test unit because eight nanosecond identity fields lost precision when transported as JavaScript JSON numbers. The original rejected record was retained. File hashes and device/inode identities were unchanged; a separate identity record uses exact decimal strings for nanoseconds. This was not a failed test execution, and neither suite was retried.

The [machine-readable record](frontend-contribution-verification.json) contains all source, dispatch, runtime, TAP, result, process-observation, and closure pins. The fixed evidence scope is:
`/opt/goby-test/frontend-contribution-tests-20260916-9d8e9b6baa61`.

| Primary saved record | Bytes | SHA256 |
| --- | ---: | --- |
| `private/summary.json` | 1848 | `7293b53dd5d6d032b4e11d2fe3e88cc04ca33e56f8b6fc3c4f6ee1d344fa8bdc` |
| `private/producer.result.json` | 2650 | `dd6abbb53c6343a7fb8e06d34c17c01d4677b266013d76eace82c0b185456715` |
| `private/consumer.result.json` | 2652 | `4e3174efd228fa56b3fa9f707999c31cffcf8b8c16ba25e6c4b6a9cd39bca559` |
| `private/preflight-rejection.json` | 1893 | `a40839482e237172eb44046c36fcd956c6e40ca9585a51f5f78111ce26e62d1d` |

Document preparation only reread and hash-checked saved records; it did not rerun tests. No real Vite/application build, Go compilation, browser, Docker/Compose operation, database operation, or application-runtime verification occurred. Actual frontend graph coverage, legal/notice completeness, successful real build receipts, and final product-asset binding remain separate requirements.
