# Architecture audit remediation

Status: R01-R21 implemented and remotely verified; not deployed. This record
tracks the audit of commit `97a4b6f35c01dd9436dd4c108cb1e4c496fa586e` and binds the
completed changes to [the verification receipt](audit-remediation-20260913-verification.json).
Verification closed at `2026-09-13T03:57:22Z`. It does not alter historical run
outcomes or authorize replay of consumed reference/client operations.

| Findings | Remediation scope | Implementation and regression entry points | Status |
| --- | --- | --- | --- |
| R01, R04 | Backup reserve availability and generated-dump restore preflight | [Reserve handling](../../internal/backupstore/store_linux.go), [dump preflight](../../internal/backuppg/validate_dump.go) | Verified in the composed record |
| R02 | Descriptor ownership and bounded filesystem observations without global admission stalls | [Descriptor ownership](../../internal/library/root_binding_paths.go), [bounded observations](../../internal/library/storage_observation.go) | Verified in the composed record |
| R03 | Byte-aware recovery control capacity, compaction and explicit capacity refusal | [Control persistence](../../internal/recovery/control.go), [capacity regressions](../../internal/recovery/control_budget_linux_test.go) | Verified in the composed record |
| R05 | Preserve unknown copied-stream bitrate facts in HLS planning | [Planner regressions](../../internal/playback/conversion_bitrate_test.go) | Verified in the composed record |
| R06, R07 | Bounded HTTP bodies and stalled output, per-subject stream fairness | [HTTP ownership](../../internal/server/http_bounds.go), [media regressions](../../internal/server/media_bounds_integration_test.go) | Verified in the composed record |
| R08, R09 | Transcode admission fairness and runtime health | [Admission and health regressions](../../internal/transcode/manager_admission_health_test.go) | Verified in the composed record |
| R10, R11 | Consistent credential query carriers and shared-device transactional audit | [Query carriers](../../internal/server/compatibility_query.go), [device audit](../../internal/identity/application_key_devices_audit_test.go) | Verified in the composed record |
| R12, R16 | Subtitle owner admission and final theme source validation | [Subtitle index](../../internal/library/subtitles_index.go), [theme publication regressions](../../internal/library/theme_publication_locks_integration_test.go) | Verified in the composed record |
| R13, R14, R15 | Derived-name notifications, snapshot locks and final administrator authorization | [Metadata persistence](../../internal/library/metadata_store.go), [final authority regressions](../../internal/library/metadata_authorization_integration_test.go) | Verified in the composed record |
| R17 | Preserve committed library creation success when optional scan admission fails | [Library adapter and regression](../../internal/server/reference_library_integration_test.go) | Verified in the composed record |
| R18 | Published migration-content baseline and contiguous applied history | [Published manifest](../../internal/database/migration_manifest.go), [integrity regressions](../../internal/database/migration_integrity_integration_test.go) | Verified in the composed record |
| R19, R20 | Session-scoped non-secret recovery receipts and pending binding navigation guards | [Recovery receipts](../../web/admin/src/backupReceipts.ts), [navigation regression](../../web/admin/e2e/root-binding-workflow.spec.ts) | Verified in the composed record |
| R21 | Registered safe task retry diagnostics | [Diagnostic regressions](../../internal/diagnostics/task_retry_test.go) | Verified in the composed record |

The migration remediation preserves schema28: a published, reviewed SQL manifest
can reject same-name source drift without inventing checksums for historical
database executions. It is not retrospective proof of the bytes an old database
executed. A future schema change requires its own versioned migration, content
baseline and backup catalog acceptance.

The optional scan on compatibility library creation follows the committed
creation outcome: return the established empty 204 response after creation has
committed, even when separate scan admission fails. A safe diagnostic identifies
the created library and the scan failure; it does not claim scanning succeeded.
Operators can scan the existing library without retrying creation. The native
API retains its existing created-library/ScanError response contract.

Unconfirmed forced-shutdown COMMIT timing, measured large-catalog performance,
actual GPU execution and broader client acceptance remain separate work.
Current deployment and capacity boundaries are listed in [current status](current-status.md).

## Runtime bounds introduced by this increment

Ordinary small API request bodies receive a 15-second receiving budget beginning
at their first read, after route admission. The existing backup upload/JSON body
owners retain their own bounds. Completed bodies retire their timers before a
connection can be reused; rejected unread bodies do not force an unbounded drain.

Image, subtitle and original-media output uses a sliding 30-second write-idle
deadline. Successful progress renews it, so a long film has no fixed response
duration cap. Original files also have an eight-response allowance per user or
parent application credential, within the existing global limit. Images release
their four processing slots before transfer and separately cap transfers at
eight responses and 128 MiB of retained image bytes.

Transcode admission counts accepted unfinished work, including persistence and
scheduling races. Each user and parent credential receives its execution
allowance plus a queue allowance defaulting to half the global queue (at least
one). Existing-spec reuse remains ahead of quota checks. Global retained-job and
cache limits remain applicable; this does not promise unlimited admission when
completed history or output storage reaches its independent bound.

The native overview and capabilities responses include
`Transcoding={Configured,Available,Reason}`. Readiness rejects a configured but
unavailable engine with a specific safe reason. Temporary admission pressure
does not mark the engine unhealthy; permanent cache failure and shutdown do.
Hardware configuration remains separate from actual verified hardware support.

Storage observations retain complete identity/absence checks in filesystem-only
workers with two slots and a five-second limit. Final missing-item reconciliation
shares one five-second observation budget inside its owned transaction. A caller
can roll back on timeout while a blocked observation retains only its own
descriptors until it returns. This bounds shared waiting; it does not pretend
that operating-system filesystem calls can always be interrupted.

## Remote verification

All execution used `ssh test-env` with new isolated Linux units and fresh
PostgreSQL fixtures. The accepted, deduplicated set contains **2,255 top-level Go
tests across all 25 packages**, with race instrumentation and zero skipped tests.
The 24 complete passing package runs account for 1,712 tests. Server coverage is
542 passing tests from its full unfiltered run and the remaining test's successful
independent retry. This is composed evidence, not a claim that any single full
repository run or that original server package run exited successfully.

The server interruption happened during fixture scanning, before profile loading,
PlaybackInfo or Range assertions. It overlapped 32 host memory-pressure messages
and SSH handshake failures; the verification unit had no OOM or memory-limit
events. The exact test passed in a new process and fresh database fixture with
the same source and unchanged time limits. No responsible workload is attributed.
The original failed run and its setup/cleanup errors remain intact.

Earlier verification found two library tests that counted media preparation
against their database-lock wait, and an HTTP request-body regression affecting
diagnostic downloads. The test synchronization and HTTP ownership fixes were
verified; the original failures remain in their own reports. The final server
run also passed the new HTTP/1 unread-body, HTTP/2 connection-reuse and unchanged
three-second diagnostic-download regressions.

The frontend typecheck/build and all **41 browser tests** for backup recovery
receipts and pending root-binding navigation passed with mocked APIs. All 64
frontend source/configuration files match those verified inputs. This does not
establish a new original-client result. Linux amd64 compilation also passed.

The final code/test archive is identified by SHA256
`23cb51f31f16b28af1ad853d0ff6d9522d9eca3d578252c3052488f130c52022`.
All 100 changed workspace code, test and script files are byte-identical to the
archive, and all 92 changed Go files passed remote formatting inspection. Final
status documentation was updated after the source freeze. A pre-existing
whole-tree formatting difference outside those changed files remains recorded.

Reuse of the earlier 16 complete package results is supported by exact source
comparison and a fresh dependency graph covering 357 build variants. It excludes
the later server changes and the changed library test from those package builds.
The receipt records each source archive, run status, package result and log hash.

Linux binary SHA256:
`b644295d2eb7d58c1351f12396a46e621d5819d77ee653e575c81cff6d7b5665`.
All verification PostgreSQL processes stopped, private mounts were removed and
verification cgroups were empty. Owned source, stopped database data, logs and
reports remain as evidence. No existing service, primary/candidate database,
reference proxy or consumed historical business scope was changed or replayed.

The containing Git commit records this verified increment on main; deployment
remains pending.
Schema28 is unchanged. Actual NextUp matrix work, positive original-client gates,
large-catalog and blocked-NAS measurements, and actual GPU acceptance retain the boundaries
listed in [current status](current-status.md).
