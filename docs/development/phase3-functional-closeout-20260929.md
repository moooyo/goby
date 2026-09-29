# Phase 3 functional closeout - September 29, 2026

Status: **COMPLETE for the revised Phase 3 functional scope**.
Final verification, affected repeats, both builds, and owned-resource closure
are complete. No additional product change was needed in this final increment.

The final verification source is committed revision `1bc71f1`. The last product
repair is `242d6d8`; subsequent work adds verification and documentation.
The clean source archive SHA-256 is
`93eb537b9cafe92d9bde7c5948cd85078979b2b9931e4af2278a7a16b7f6ee28`.
Unrelated local packaging, notices, and other dirty changes are excluded.

## Revised acceptance agreement

The user selected functional correctness first, with local compilation allowed
and tests/runtime execution on `test-env`. Historical performance targets and
constrained capacity profiles are not gates for this revised functional scope.
The old authority-publisher, one-shot controller, and final54 orchestration chain
are not prerequisites for these isolated development checks.

Correctness requirements remain: exact catalog membership and identities,
preserved acknowledged user/settings state, safe deletion and rollback,
actual media execution, truthful interrupted work, and release of owned resources.
An HTTP timeout or cancellation response does not prove a blocked worker exited.

## Completed functional milestones

The records below retain their original source revisions, fixtures, limits,
failures, and evidence. Later milestones close earlier outstanding functional
items; their historical remaining-work sections are not cumulative new gates.

| Milestone | Accepted evidence retained for final composition |
| --- | --- |
| [Functional repair](phase3-functional-repair-20260929.md) | Six focused Go tests and 73 workload tests; 20-media and 10,000-real-media HTTP journeys across two roots, 4,200 directories, and 336,000 nonmedia entries. Cold/cached/incremental/settled scans, exact paginated identities, add/delete/cross-root movement, UserData preservation, and direct media bytes passed. |
| [Concurrency and catalog scale](phase3-concurrency-recovery-20260929.md) | 100k SQL-seeded catalog isolation, counts/deep pages/ACL/UserData, real PG owner termination and lock-wait rollback, seven real mount scans, and compound production media workers. The combined scenario used 100 real Movies, three generated Episodes, and 100k additional synthetic leaves. Remux/transcode outputs and generated HTTP BIF frames were decoded. |
| [Process and storage recovery](phase3-process-storage-recovery-20260929.md) | Actual application SIGKILL and dedicated PG immediate restart with explicit app restart; real owner-initialization and preview-payload ENOSPC, old preview/state retention, successful retry, and the cache package race suite. |
| [Filesystem recovery](phase3-filesystem-recovery-20260929.md) | Real nonroot EACCES with healthy-root service and selective deletion after recovery; cold metadata lookup blocked in the kernel, caller timeout with observation retention, exact syscall return after device resume, and binding recovery. |
| [Media read and CLI recovery](phase3-read-cli-recovery-20260929.md) | Cold-page payload read with client cancellation while the handler/lease remained active; healthy delivery, syscall/descriptor retirement, and exact Range retry. Actual `cmd/goby` under systemd passed normal TERM, SIGKILL recovery, and automatic restart after private PG lease loss. |
| [Blocked scan and guest recovery](phase3-scan-guest-recovery-20260929.md) | Cancellation persisted while the scan stayed Running in a real blocked syscall; healthy-library scan/query/media, evidence retirement, and successful retry. An isolated Linux guest passed normal reboot, QMP reset, and absent-media boot followed by original-volume late mount. |

These observations establish complementary functional coverage. They are not
summed into a new count of unique tests or a historical fault-matrix score.
The 100k catalog consists of synthetic database rows, not 100k decoded files.
Generated intro media proved real extraction and truthful no-result handling;
it does not extend the previously accepted labeled-corpus accuracy claim.
CLI/guest BIF checks prove new generation and delivery/header. The compound
scenario supplies frame-decoding evidence; ENOSPC verifies retention and retry.

## Product repairs included

| Revision | Resulting behavior |
| --- | --- |
| `5444369` | Missing-item reconciliation has a bounded aggregate proof allowance while retaining per-observation limits, rollback reserve, identity checks, and live-context commit requirements. Workload assertions distinguish transitional totals from exact settled results. |
| `518307c` | Failed exclusive cache-control writes clean up only the writer's verified incomplete file, preserve the original error, and allow recovery after real ENOSPC. Unknown or replaced ownership records remain rejected. |
| `242d6d8` | Pre-migration recovery scope inspection tolerates another session's native temporary relations, including crash remnants, only when both temporary persistence and PostgreSQL's temporary-namespace predicate match. Persistent foreign scope remains rejected; no cleanup DDL is performed. |

The cache repair does not retroactively repair malformed ownership markers
left by an older build. Original failed cache and CLI fixtures remain evidence.

## Final verification results

The final run follows the [consolidated verification entry](phase3-final-verification-plan-20260929.md).
The [machine-readable result](phase3-functional-closeout-results-20260929.json)
contains per-package counts, required integration results, every skip reason,
command exit statuses, and evidence/build hashes.

| Check | Current result |
| --- | --- |
| Frontend build, including TypeScript checking | PASS |
| Selected Node test groups | PASS: 28 and 14 tests in separate groups |
| Phase 3 workload Python suite | PASS: 73 tests |
| CLI and guest driver Python syntax | PASS |
| Ordinary Go, composed package evidence | PASS: 4,339 parent tests, 12,162 subtests, zero unresolved failures; 30 parent skips and one subtest skip |
| Package coverage | 33 tested packages passed; two additional packages have no test files |
| Complete affected-package repeat | PASS: media, server, and transcode; 1,899 parents and 5,210 subtests, reported separately rather than added again |
| Embedded-admin `cmd/goby` suite | PASS: 24 parents and 37 subtests, zero failures/skips |
| Ordinary application build | PASS: Linux amd64, CGO enabled, `-trimpath` |
| Embedded-admin application build | PASS: Linux amd64, CGO enabled, `-trimpath`, `goby_embed_admin` |
| Final owned-resource closure | PASS: no owned test/build workers or database clients; private PostgreSQL stopped, socket and TCP port released |

The configured ordinary suite includes migration/rollback/timeout checks,
real dump/restore, historical schema restoration, temporary-relation handling,
whole-database recovery binding, and encrypted recovery apply/restart/rollback.
The required database/media/backup/recovery cases executed and passed, including
the new temporary-relation regression and schema 23/24 encrypted archive
apply/restart/rollback. None was credited through a missing-configuration skip.
The ordinary suite also passed its 10,000-real-media / 10,688-item regression:
four completed cold/cached scans, exactly one replacement probe during the
cached repeat, preserved unrelated identities, and a closed Store. This is its
own fixture, not a repeat or relabeling of the earlier HTTP capacity campaign.

The first full Go command exited 1. Its FFmpeg 9.0.1 build lacked `libx265`,
`libaom-av1`, and `zscale`; 10 parents and 38 subtests failed in the server and
transcode packages. That raw attempt is retained as failed. The already recorded
complete `9.0.1-goby-cb8b6d298456` prefix has matching executable hashes and the
required features. No product code was changed. Media, server, and transcode
packages were repeated in full and passed. Final composition replaces those
three package results while retaining the unaffected package results. Parent
test sets were checked for equality, and every original failed parent/subtest
has a matching explicit PASS in the repeat. The original full-command failure
is not rewritten as a first-attempt success.

The initial full attempt recorded 4,329 passed parents, 10 failed parents, and
30 skipped parents. The repeat is a replacement, not 1,899 additional unique
tests. The ordinary and embedded counts likewise remain separate. There are no
run events without terminal results. The no-test-file packages are
`cmd/goby-notification-receiver` and `internal/systemevents`.

The 30 parent skips comprise 11 Phase 3 opt-in/diagnostic/process-helper cases,
two mount/fanout cases, two live HTTP-binding cases, and 15 specialized hardware,
Dolby Vision, or OCR cases. One Vulkan/VAAPI subcase also skipped. These are not
reported as newly executed acceptance; the earlier fault records retain their
own actual evidence. Browser fixtures excluded by build tags are not test passes
or runtime skips in this run.

The complete FFmpeg prefix is
`/opt/goby-amd-media-20260919-50f45177f297/toolchains/ffmpeg-9.0.1-goby-cb8b6d298456`.
Its ffmpeg SHA-256 is `c8887f1a2b5a6777c1285fd7514049ec175947048d82e4b3700e44115da34843`;
ffprobe is `fddc50127245a7a836b5fb6789e1606ebe9e12bcf3c9e2fab5b32bfb7a0b703f`.
Both match the earlier toolchain record. Go 1.27.1, Node 22.23.2, npm 10.9.8,
PostgreSQL 17.11, and the previously verified Chromaprint 1.6.1 helper were used.

| Final Linux artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `goby` | 48,981,764 | `c17348cbbff18b08eca15c9bacfb08cffe8c524c9d7062e003d9318bb8acc2c0` |
| `goby-embedded` | 50,416,088 | `6fe23e32d39865e4585d9de32ff2638b1334f0b6839c9fa19b4b009e2f9bcb77` |

No tracked `web/admin` input changed between `5444369` and `1bc71f1` in the
source comparison. Earlier browser results remain evidence only for their
original revisions and provisioned scenarios. This source equivalence and the
current frontend build do not constitute a browser E2E rerun at `1bc71f1`.

## Evidence and closure

Current raw results are retained locally under `.git/phase3-final-20260929/`
and remotely under `/opt/goby-phase3-final-20260929-01/artifacts/`.
The result JSON binds raw log hashes and binary identities. Both binaries are
also retained locally under the private evidence directory and remotely in the
verified source directory's `artifacts/` folder.

A fresh dedicated PostgreSQL cluster used loopback port 55991, three independent
non-administrative database owners, 128 MiB shared buffers, and normal durability
settings. Backup/recovery packages ran in the order backuppg, recovery, recoverydb.
The last deliberately retains its restored target, so its database is preserved
rather than falsely described as empty. Before shutdown there were zero other
client sessions and no owned test/build workers. PostgreSQL stopped normally;
its socket was absent and the port was available. Shared services and the
previous CLI/guest evidence databases were not changed.

This completes the **revised Phase 3 functional scope**.
No additional functional campaign is required solely to recreate old controller
labels or to rerun unchanged, already accepted milestone evidence.

## Claims outside this closeout

The historical strict 10k/100k resource/SLO profiles, sustained overload,
100k-real-media cold/cached/incremental workload, and exact two-tier 28-case
fault matrix remain unaccepted. Small functional cases are not reassigned to
unexecuted matrix cells. The guest used a direct kernel/initrd boot path; it
does not establish firmware/bootloader recovery or physical power-loss durability.

Dual application builds do not establish release installer/package acceptance,
complete distribution assembly, OCI or additional-platform qualification, or
production deployment. These historical performance and packaging limits remain
explicit, without becoming new gates for the revised functional agreement.
