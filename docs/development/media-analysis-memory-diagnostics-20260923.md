# Phase 3 backend memory diagnostics

These measurements narrow the scope11 PostgreSQL OOM investigation. They do not
establish 10k/100k capacity, playback latency, overload, or fault/recovery acceptance.
The failed scope10 and scope11 database clusters remain unstarted and unchanged.

## Source and affected regression

`99fcb37` avoids recursive folder/collection UserData derivation for leaf-only
results. Its first affected regression passed 67 parent tests and failed four
notification tests, with no skips. The notification projection omitted
`is_folder`, so the optimization incorrectly omitted ancestor and collection
summaries. `fcefb82266bda117c627cb0c35901c795e726f26` adds that field to the existing
query and scanner. All other production callers already carried the folder fact.

The same affected regression on `fcefb82` passed **71 parent tests, zero failures,
zero skips**, covering UserData, Latest, Resume, catalog query policy, collections,
notification state, and the new projection/snapshot tests. The test process group
and worker were independently confirmed absent. This is affected regression,
not a replacement for the final complete regression and builds.

## Method and boundaries

All execution used the owned VM106 and a fresh `repair99-diag01` PostgreSQL 17
cluster. The diagnostic database had a 1 GiB cgroup cap; the capacity acceptance
cap remains **512 MiB**. The diagnostic pools had one physical connection so that
PID checks could bind each before/after observation to the same backend. Production
pool concurrency and memory settings were not changed by these tests.

The library diagnostic measured migration, catalog ownership, 32,768 staging IDs,
TEMP-table deletion, deallocation, connection closure, and repeated leaf/folder/page
projections. The startup diagnostic called the real
`backuppg.InspectRecoveryTransaction` on a separate, initially empty owned database,
then demonstrated the same backend being acquired by `library.New`. It did not
start an HTTP server or execute the complete generation binding/marker lifecycle.

Snapshots included `pg_backend_memory_contexts`, prepared-statement counts, actual
session settings, and same-guest `/proc` memory. Observation queries used explicit
`Exec` mode. Only diagnostic roles received `pg_read_all_stats`. Missing metrics
remain unknown. RSS includes shared pages and must not be summed across backends.

## Observations

- Migration retained a roughly 16 MiB `CacheMemoryContext` allocation, mostly
  free inside PostgreSQL's allocator. This is not the prepared-statement cache.
- Staging added an 8,425,808-byte `LocalBufferContext`. Dropping the TEMP tables
  and running `DeallocateAll` did not release that allocation. Closing the dirty
  owner connection and acquiring a new backend reduced anonymous memory from
  28,106,752 to 3,620,864 bytes in the first diagnostic.
- Real startup catalog inspection retained additional plans and catalog state.
  After migration, inspection and ownership transfer, the observed backend held
  26,730,496 anonymous bytes. Clearing named statements left that value unchanged.
- Type-aware description caching avoided retained named plans while preserving
  the tested query results. The following are end-of-sequence observations from
  fresh, separate backends, not peak-memory or latency measurements:

| Query mode | Prepared statements | Context allocation, bytes | Anonymous memory, bytes |
|---|---:|---:|---:|
| CacheStatement, library projections | 14 | 8,467,096 | 21,094,400 |
| CacheDescribe, library projections | 0 | 2,330,248 | 14,376,960 |
| Exec, library projections | 0 | 2,330,248 | 16,146,432 |
| CacheStatement, second real startup inspection | 40 | 11,248,408 | 17,797,120 |
| CacheDescribe, second real startup inspection | 0 | 6,042,872 | 12,849,152 |
| Exec, second real startup inspection | 0 | 6,042,872 | 12,865,536 |

These results support evaluating type-aware description caching. They do not
prove that changing query mode alone prevents the original OOM. Global `Exec`
also has a known parameter-encoding risk: existing JSON writes pass `[]byte`,
which requires PostgreSQL type information to distinguish JSON from bytea.

## Retained evidence

Private guest evidence is under the owned campaign's
`private/scope11repair01/tests03` and `tests04` directories. Raw runtime contexts
and authenticated database URLs remain on the guest. Safe summaries and independent
worker closures are retained in the local private execution ledger.

| Evidence | SHA-256 |
|---|---|
| Affected regression and first diagnostic result | `9e80f4b3b5f14c2fd1b8345f898d27d73962c8849dad20c7fbdfba3a80a64a35` |
| First diagnostic summary, 38 observations | `e2876d16e912bc172a6e616aa7465dd7ffb6862f1f69e4eb7258c27fcaf7cae8` |
| First worker independent closure | `d488e9a940539782604c6cc6404b919a60916b99a9769f8fbac5e8abd0869dba` |
| Three-mode library and real-startup diagnostic result | `1f750a0f7bca3e009cf84b4c6abfc8549f12acdb9f75219affb42573d563ac7b` |
| Three-mode summary, 52 library and 32 startup observations | `915bdf5c16ad6b6e909bd9a95eb1bfbf6eb87f7c7c55d6f261cea4af902e6cba` |
| Second worker independent closure | `ef06ea87f279f6155dec2e3aa1368c517a353f6af9fdc44f43e26db74f0d7da0` |

The diagnostic workers are closed. PostgreSQL remains intentionally live for the
next affected checks; its runtime is not a capacity fixture. The two opt-in Go
diagnostics are separately pinned temporary test inputs, outside the frozen
production source. Earlier failed tests and runner/preflight failures are retained.
