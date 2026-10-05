# Player release integration into main

Date: 2026-10-06.

This integration combines main `6250d4345f24cef56672adb24607303dceaa0008`
with player release `325870d45582c054ea46a8b37d01f5f47cf1a6e5`.
The common ancestor is `b30e78c8924380cd404588890f20dd0949394dde`.
It retains the later main scan and operation-authorization optimizations while
adding the already accepted player and persistent media-analysis features.

## Integration changes

- Analysis queries retain operation-scoped root bindings and select the correct
  intro or credits marker revisions. Single-pass admission decoding still
  validates the detector against the actual task kind.
- Background, waveform and subtitle-timeline readers pass their child identity
  to the updated source reader without changing their existing queue revocation
  policy. Bitmap sidecar scans use the started scan's root approval.
- Workers retain main's authorization-at-start behavior, with live cancellation
  and durable execution ownership fences. Later workers require fresh approval.
  Generic scheduled executors use system identity, including the new media jobs.
- All six media task definitions share the analysis slot and bounded deferral
  capacity. Cross-feature tests cover credits marker isolation, cohort changes,
  immutable task identity and bitmap scan authority.

## Remote verification

Verification ran only on `test-env` with Go 1.27.1 and a dedicated PostgreSQL
database. The library, task, server and application packages compiled.

| Scope | Top-level passes | Subtests | Skips |
| --- | ---: | ---: | ---: |
| Complete tasks package | 131 | 155 | 0 |
| Selected library and persistent-media regressions | 87 | 82 | 0 |
| Server contract and policy tests | 25 | 69 | 0 |
| Real HTTP/database integration | 27 | 13 | 0 |
| Total | 270 | 319 | 0 |

One newly added test initially attempted to rewrite an immutable admission
snapshot. PostgreSQL correctly rejected that setup. The corrected test verifies
the rejection and confirms that the original credits task remains readable;
no database trigger or production guard was disabled. The initial log remains
in the evidence alongside the passing final library run.

The ordinary shared Go build/module caches were reused. Task compiler scratch
and empty test-fixture directories were removed after worker exit. The reused
verification PostgreSQL container was stopped, while source, data and raw
evidence were retained under
`/opt/goby-test/player-main-merge-20261006-165c`.
The local evidence is `.artifacts/main-merge-20261006/artifacts`.

This is a source integration, not another image build. The existing
`2026-10-05-player-media` catalog and Docker receipts retain their exact accepted
application revision and archive hashes. Uncommitted work in the primary
checkout is separate from the integration and is not part of this merge.
