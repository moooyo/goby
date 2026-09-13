# TV parent metadata candidate transition

Status: preparation; the [final product verification](tv-parent-metadata-full-verification.json)
passed all 2,270 tests/25 packages and the Linux build. This plan
does not claim a candidate update or fresh admission. It extends the existing
same-schema transition tool for the current configuration epoch and retained
state. Preserve the historical transition inputs and their validators.

## Fixed predecessor and observed state

The predecessor is the admitted configuration epoch
`72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e`,
with seed binding
`92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f`.
The current binary remains
`477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f`.
The selected replacement must come from the completed full verification of
archive `b363afdcf707471c3a95288d04441bb7be89699010b09ca89c4c783e10436177`;
its source manifest is
`bce4d22a4c51dacca4660a6c8e8e3fac816141cd612a7b32b87367799e495cff`.
The completed full report SHA-256 is
`2dc580db7e44bc2b01f6dc843f147d70588fa381bfd706560fbec2f68b228bfc`,
and its worker report is
`4d65de4801e261de49ed41024a0506d23a521957ee4b312494a12b5031bf99f5`.
The binary at that full scope's `bin/goby-linux-amd64` is 29,561,402 bytes,
SHA-256 `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`.
Independent artifact reconciliation is complete. The transition tool changes,
their focused verification and final execution input remain pending.

The one read-only state capture is retained at
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-tv-parent-transition-state-review-01/`.
Its safe `summary.json` SHA-256 is
`be2bd9a3081c9f84fb71ed617d2d3a025415ee2faf762d89e512e26bcd0dff18`;
private `state.json` SHA-256 is
`82cabde1a8d8dcf73a0e19da5b7c680fd977cace56d0d597bac0c59513b170a6`.
The source snapshot at `2026-09-13T14:31:07.209006Z` matches the TV browse01
closeout across all 35 tables and sequences. It retains fifteen revoked
sessions, seven plays and two userdata rows. Two Prepared plays are unstarted
and uncounted. The inactive schema28 recovery stage matches admission04 across
all tables and sequences, with its completed staged generation preserved.

The saved startup review found three authorized terminal operations: failed
create, completed create and cancelled restore, all in `finished`. No apply,
delete, publication, activation journal or transition remains pending. The two
backup catalog rows are the retained failed empty entry and ready 290,550-byte
object. There are no queued/running scans, tasks, triggers or encoding jobs;
the registered cache has no job. The 58 activity rows remain below even the
minimum one-day retention age over the bounded transition window. Diagnostics,
unit logs, fourteen media files, 57 assets, six owned trees and fixed private
file metadata are captured separately. No HTTP, business write or service
action was performed by this review.

## Required changes to existing tools

Add a bounded binary-successor input/epoch branch that names this predecessor,
the reviewed current-state descriptors and the completed new product proof.
Do not replace old product constants in a way that changes the meaning of old
epochs. Preserve the runtime environment, including the 64 MiB object and
256 MiB total backup limits, and distinguish product lineage from inherited
configuration lineage. The initial seed, catalog/media identities and original
bindings remain history, not a request to seed again.

The old transition's empty playback, three-session and empty recovery assertions
cannot admit this state. Its new branch must bind the actual reviewed source,
control documents, staged recovery, files and startup conditions instead.
`cleanupPlayback` is not called by startup, so both retained Prepared rows must
remain exact; do not grant the expiration exception used by later PlaybackInfo
requests. SQL state and all sequences must match before and after restart.

Reuse the existing stop, atomic binary replacement, start, process/listener,
lease, diagnostics and preservation operations. Update both outer and internal
reader process context when the controlled process changes. Regression checks
must retain old epoch behavior and reject source, configuration, ownership,
state and parent-lineage mismatches before executing a transition.

The original-client hosting initialization receipt remains historical evidence.
Validate it against its own original epoch/binding/helper, then independently
pin the same unchanged hosting process and the new candidate runtime. Do not
reinitialize hosting or require the old candidate PID to be live. Downstream
admission, gateway, adapter and closeout must consume the new lineage explicitly;
old admission04 is reusable evidence for unchanged contracts, not a relabeled
fresh pass for a different binary. Define the affected live-admission checks
separately before resuming clients.

## Execution and completion gates

Keep the existing 900-second transition budget, 60-second stop/readiness limits,
at most ten bounded public requests, and exactly one stop, replacement and
start. Freeze the final new input, tool hashes, source/state proof and new output
before execution. Repeat identity, full source, recovery/control, file and
startup gates immediately before stopping. The earlier capture is not a waiver
for drift, retention deadlines or newly pending work.

The change affects only the installed candidate binary and the normal bounded
diagnostic/unit-log append and process/lease transition. PostgreSQL, environment,
media, assets, accounts, backup history and both database slots remain exact.
Preserve the old binary privately and record staged-file responsibility before
replacement. Publish the new epoch only after readiness and exact preservation
pass. Any failure retains its actual process/file responsibility and original
evidence; no automatic business retry or unreviewed rollback is implied.

This remains an isolated candidate step. It does not update main, complete the
core client gate, explain historical page errors or remove remaining M2-M6 work.
