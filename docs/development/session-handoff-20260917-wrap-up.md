# Session handoff: September 17, 2026

The user requested a session wrap-up, merging the code into main and preserving
a restartable handoff. Implementation is merged; the complete M2-M6 delivery
goal is **not finished**. M7 remains deferred. Resume execution from this
checkpoint when requested, not from an older pending-transition instruction.

## Repository state

- Repository: `D:/Code/goby`, branch `main`, remote `git@github.com:moooyo/goby.git`.
- `1cd48bd` merges the complete M5 implementation through integration
  `d3c074650f75ce23243c8bc24475d6f098ecf83d` and M5 source
  `137b41bd73ee29b6a8856c63a45a2ca50fdd8bfd`.
- `c504f9e` merges the OCI recipe/checker implementation from
  `18cd4efd117f3314cf5cb46b4736f65795bbdf59`.
- Main retains `3f609b6` finalization support and `b9cfacf`'s subsequent
  six-service Programs admission correction.
- The M5 merge preserved that branch's `cmd`, `internal`, `web`, Go module
  and release-build inputs, plus the newer main delivery tooling. Merge commits
  are not newly tested builds. The OCI merge adds a separate `deploy/oci` tree.
- Frontend contribution, native capacity, Programs binding and earlier admission
  branches already have equivalent changes in main. Do not merge them again
  merely because their original commit IDs are not ancestors.
- Keep the dirty query-plan/reconciliation diagnosis worktrees separate. Their
  temporary instrumentation is not part of the product merge. Worktrees and
  branches have been retained for recovery.

These existing untracked main files were deliberately left untouched:

```text
scripts/test-env/dispose-source41-resource-full-failed-pair.py
scripts/test-env/test-dispose-source41-resource-full-failed-pair.py
scripts/test-env/upgrade-main-schema25.py
```

## Execution rules and current access

Use Chinese for conversation and English for code, comments and documentation.
The local OS is Windows/PowerShell. Run tests, builds and runtime probes only
through `ssh test-env`; local verification has not been authorized. Serialize
actual remote verification jobs. Saved-file reading can proceed independently.
Do not expose credential contents; retained master keys are stat-only, and
protected environment/configuration evidence is hashes and metadata.

The wrap-up SSH read failed before its remote command ran:
`Permission denied (publickey)` with the configured `test-env.pub` identity,
tool `e13255`, exit 255. Earlier executions below have their own completed
evidence. Restore normal `ssh test-env` authentication before fresh verification;
do not substitute local testing or claim a new remote observation.

There is no remaining live exec handle from the product executions below:
finalization handle `51813` completed, and admission handle `97835` completed.
Never restart a consumed scope based on an old handle or an observation timeout.

## Programs: finalization completed, affected admission still pending

Define these exact roots for reading the records below:

```text
R = /opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14
C = /opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b
F = R/candidate-programs-finalization-01/private
S = R/programs-finalization-dispatch-20260917-r01/private
```

The original S2 transition stopped/replaced/started A once, then failed its
diagnostic-file preservation check. It remains failed. The subsequent strict
read-only finalization completed once, generated V4 and passed independent
review and resource closure. It performed 16 read-only SQL and 32 metadata
commands, with zero new stop/replace/start/HTTP calls. See the
[finalization result](programs-finalization-result-20260917.md).

Last independently confirmed running candidate:

| Field | Value |
| --- | --- |
| A PID / start ticks | `1907978` / `34901535` |
| Invocation | `2d8403319f3943dbb3d1da638f33093e` |
| Executable | `C/install/goby`, 30,701,500 bytes |
| SHA-256 | `ead67c8faaf4cde88f7fe1bf57ffed43705259aba7b29f59c3473747afd732fb` |
| Executable device / inode | `2049` / `1580898` |
| Lease backend / PostgreSQL | `1907986` / `363520` |
| Boot ID | `4de83999-7586-4716-83d1-0d81c9343126` |

These intentional running processes are not leaked test resources. Preserve B,
the three expected PostgreSQL postmasters and the hosting/reference service.
The wrap-up authentication failure prevented a newer live observation.

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `F/runtime-epoch.json` | 28883 | `ae728a2a4e7d329b536e7c3a755946dd6eefd2234d60b4ef9c9c1499c32fea86` |
| `F/seed-runtime-binding.json` | 23981 | `5d687e1815b6405abb486cfd428935515d4a2a41e4fc7eb2e74b2d01485787a7` |
| `F/fresh.json` | 1182355 | `37b144cae20b044468c97e62dbec4af87ce038f87b763c3aa0766f0078847a34` |
| `F/antecedent.json` | 20268 | `1443e03f840ff6fac43c5767894da5df2c0de5c560c8f1d158593fb4a368ae27` |
| `S/independent-finalization-review.json` | 133472 | `4fd14846d85e1821947850c651b6f9b4840c38c83be1e27a7af9b083b5623e62` |
| `S/independent-finalization-reader-closure.json` | 2515 | `054c7629ebaf0a96f3a0bf3d5e3fd59c41a73d9715fc55107dee992256949132` |
| `S/independent-transition-closeout.json` | 8471 | `5923b53a6c3c2829d422390578b21bc49e0d08367c3477891edf420400c8d6a9` |

The epoch and binding intentionally retain `candidateAdmissionComplete=false`.
No movie/episode/subtitle client acceptance or main promotion follows from V4
alone. Do not rerun D000, the original transition, or the completed finalizer.

### First affected admission failure and correction

The consumed attempt is `R/programs-admission-dispatch-20260917-r01`, with
output `R/candidate-live-admission-programs-01`. Original PowerShell handle
`97835` ended at `e090cd` with exit 1; native SSH/caller/CLI exited 2.
Preflight failed with `tv_control_configuration_or_hosting_changed` after
50,457 ms, before any HTTP request or login. The report has requests `0/0`,
empty controller sessions and no cleanup failures.

- Report: output `private/report.json`, 10556 bytes /
  `47851db8f85de42e0ef0fb2bcf1c636201838b3e10c79fbda9350d5137b80fe0`.
- Original session closure: dispatch `private/session-closure.json`, 268104
  bytes / `92b37791a759cf2716244c0d4ba8a70b99f25afa57fc5e2f2db28c89b3b47024`.
  Tool `82c3b3` exited 2; resources were observed closed, while evidence acceptance
  remains false because the operation failed. Do not relabel that receipt.
- Original outer protection before/after bytes matched. A later reviewer
  reported 19 saved SQL commands and began comparing their closure and snapshots,
  but no final independent failure-review receipt was received before wrap-up.
  Inspect the saved scope for that receipt after access returns; do not assume
  either its absence or its successful completion.

The definite source defect was a four-service reader compared with a six-service
Programs baseline. `b9cfacf` selects `PROGRAMS_PROTECTED` and rejects a baseline
whose service set differs. It preserves the legacy four-service path and
per-service equality checks. Current admission source is 139557 bytes /
`bf115f454f28f1fa13caac09f057d7ba3cd48cbc5db7f75e363d769add39f9fc`;
runtime remains 245121 bytes /
`9a830b06c3a3425593ace27376036e229ef9958852e83dca07e5148d517de0cf`.

The four affected methods passed once on test-env, tool `a19648` exit 0.
Scope `/opt/goby-test/programs-admission-protected-units-components-20260917-724bad163ef0`
has `private/result.json`, 3205 bytes /
`5a3406163f457348f921ac48be51a7f6d9fabd64de7fdf20c0f39cf1b1ff0d73`.
Root read the raw `Ran 4 / OK` output separately (`5f7334`, exit 0).
These are mocked checks, not a second live admission. Do not repeat them without
an invalidating change.

Next: close the first failure review, then prepare a new admission scope/output
using the corrected helper. The retained r01 binder/publisher/outer still pin
the old 139356-byte `2dbcb...` helper and consumed r01 paths. Regenerate all
affected pins for r02; do not run the saved r01 launcher. Preserve the historical
finalization paths and runtime pins when updating the admission scope.
Keep the existing 16 normal + 4 cleanup requests, 180-second operation,
60-second cleanup and 28/8 request limits. Required lineage/product reads remain
inside the admission; do not add a separate large product replay.

After successful admission, use its actual closeout and `sourceAfter` to run
movie/resume, episode/browse, and SRT/VTT/Off serially. Each completed journey
supplies the next baseline. Then close the audio reuse bridge and proceed to G3.

## M5: merged implementation, full verification remains open

The clean verification source is still `137b41bd73ee29b6a8856c63a45a2ca50fdd8bfd`,
published under `/opt/goby-test/m5-diagnostic-http-header-clean-source-20260917-33f9f1cf1deb`.
It contains 5461 files / 121866142 bytes, with the original 59 frontend assets
and contribution receipt. Do not relabel this archive as a later merge commit.

The last full run, `m5-combined-full-0add3118b99f`, failed one HTTP diagnostic
test after 18 complete packages. Six remaining packages and both builds did not
run. The header correction passed its two targeted tests. JIT targeted checks
also passed, including the original reconciliation cases; their details and
the original failures remain in the linked current-status records.

A new V4-aware metadata guard is prepared and its 18 mocked methods passed
once (`6d8ad4`, exit 0; raw readback `f8f59b`, exit 0). Scope
`/opt/goby-test/m5-programs-v4-guard-components-20260917-5bc246d137a8`
has result 3473 bytes /
`1633e92eaf866dd730ec120db3291c39a1ad8d37e0ce4cdea7ede790e4c0a730`.
Its submitted resource limits are not measured live-property/peak evidence.

The first real metadata capture is prepared for nonce `42dc9a175fb6`:

```text
scope: /opt/goby-test/m5-combined-full-42dc9a175fb6
unit: goby-m5-combined-full-42dc9a175fb6.service
local directory:
D:/Code/goby/.git/m5-full-after-programs-finalization-preparation-20260917-r01/bound-42dc9a175fb6
```

No publication or capture was performed for this nonce in the recorded work.
Check actual scope absence after SSH access returns. Read the local
`capture-and-full-entry.md`, then use its source-only
`publish-metadata-source.ps1` and separate capture command. The capture must
produce a real complete `expectedState`, including systemd fields absent from
V4; never handwrite them. It performs 17 metadata commands and no SQL/HTTP or
application service action. Its guard keeps B, PostgreSQL, hosting, key metadata,
configuration and deployment-lock protection.

Only after that capture and its closure succeed, bind the existing full adapter
and input. The adapter is 70639 bytes /
`65300bc52eece2633afc5f2ed93f3324d804f38799cdf0c71b17a407265ce67b`.
Retain all 25 packages, the original profile skip, two builds and
`--frontend-contributions`. Keep the reviewed 3 GiB/no-swap/150% CPU profile,
4 GiB workspace, 2 GiB compiler volume, 512 MiB fixture volume, 4 GiB memory
admission floor and 6870-second outer envelope. Historical free space is not a
current reservation. Full execution must not overlap live client verification.

After full success, bind its actual artifacts into the existing browser r03
and native software r02 preparations. Run administrator users/deletion/diagnostic
UI acceptance and the software diagnostic/cancel/delete-owner scenarios.

## Remaining complete-goal work

- Programs affected admission, three client journeys, audio bridge and G3 main
  promotion remain open.
- M5 complete full/build, browser and real software diagnostics remain open.
- M2 native scan/concurrent HTTP needs its fresh fixture/artifact/runtime input
  and real overlap measurements; reuse the completed component checks.
- M2 storage fault/host durability, wider declared media/client profiles, native
  arm64 and hardware-specific claims retain their separate requirements.
- OCI source/checker is now in main. Its 12 checker methods passed previously;
  no actual image build, Compose/runtime/upgrade verification or full M6
  acceptance is claimed. See [OCI checker evidence](oci-artifact-checker-verification.md).
- License selection, final notices/payload and the final support matrix remain
  unfinished. Do not infer public-release readiness from code integration.

Use [delivery and verification](../planning/delivery-and-verification.md) and
[support matrix](../planning/support-and-delivery-matrix.md) for the full M2-M6
scope. Older historical pending statements do not authorize replay of completed
restriction, refresh, G2, diagnostic, reference or component runs.

## Local recovery material

The original preparation and mirror directories remain under `D:/Code/goby/.git`.
Eleven critical directories were also copied to:

```text
C:/Users/moooyo/.codex/local-memory/projects/goby/handoffs/20260917-wrap-up
```

Its `inventory.json` records source/backup file counts and byte totals. This
local backup is outside Git and must not be copied into repository documents or
public issues. It includes finalization artifacts/review, consumed admission
launchers, the corrected four-method test preparation, V4/full metadata binding,
clean M5 source publication, browser r03 and native outer r02 preparations.
Keep their original status and paths: a copied launcher is not a fresh run.

Storage cleanup requested earlier is complete, including the 25 GiB root
extension and four approved temporary directories. The 10,000-media-file corpus
was synthetic FFmpeg template data; see the retained
[capacity record](catalog-real-media-capacity-verification.json). Do not clean
`/dev/shm/goby-m2-fullscan-20260914`: the paused mount profile has retained live
views and helpers and needs its own explicit closure.
