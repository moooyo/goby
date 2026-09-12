# Storage binding live administrator UI acceptance

Current checkpoint: `2026-09-12T22:16:17.694015+08:00`; original-client v7 is independently sealed as failed.

Status: accepted. Run5, `20260912_083703_5a77d18d2ae0`, completed all
[15 browser checks and 10 IPC stages](storage-binding-live-ui-accepted-browser.json)
against the real schema28 backend. Playwright passed one test in 16.133s with zero
skips or retries. UI logout and exact-cookie401, the controller's separate
session cleanup, all four worker cleanup checks and all ten controller cleanup
checks passed. The database/role pair and runtime were removed; HBA, global
catalog, host, candidate/primary, history and frozen inputs were preserved.
The [independent terminal](storage-binding-live-ui-accepted-terminal.json) is the
final acceptance authority. The [controller report](storage-binding-live-ui-accepted.json)
retains its original `awaiting_outer_attestation` status by design.

TOOL05 passed [90 remote memory guards and two syntax checks](storage-binding-live-ui-tool05-guards.json),
[single-test discovery](storage-binding-live-ui-tool05-web-list.json) and
[complete input preflight](storage-binding-live-ui-tool05-preflight.json) before
the accepted run. Visual review of the [desktop](storage-binding-live-ui-desktop.png)
and [narrow](storage-binding-live-ui-narrow.png) captures confirmed wrapped text,
visible controls and no horizontal overflow in the narrow view. The accepted
backend prerequisite remains source54's 2,173
full-suite passes across 25 packages, Linux build, and subsequent real private
mount acceptance. The existing 68 frontend checks use mocked binding APIs.
The broader M2-M6 acceptance obligations and existing M7 deferred scope remain
unchanged. Source55 verification, product publication and the independently
[attested candidate schema28 deployment](client-schema28-accepted-attestation.json)
are complete. Fresh original-client acceptance remains pending.
V7 passed discovery and native login. The matching LibraryChanged frame reached
the page, but neither complete request channel recorded HTTP in the forward
window. Restoration and closed sessions are
[independently sealed](client-library-changed-source55-v7-terminal.json).
LibraryChanged acceptance remains false.
The accepted UI gate retains its actual source54 frozen inputs below.

| Scope | Observed outcome | Current disposition |
| --- | --- | --- |
| [Attempt1](storage-binding-live-ui-attempt1.json), `20260912_074733_ca8294cb9f47` | Runtime mode `0710` failed the directory reads required by `openDirectory`. Zero browser/UI stages; empty database. | [Failed terminal](storage-binding-live-ui-attempt1-terminal.json) and [dedicated ordinary disposal](storage-binding-live-ui-attempt1-disposal.json) retained; pair/runtime absent. |
| [Attempt2](storage-binding-live-ui-attempt2.json), `20260912_075346_bf9011cca3e0` | Runtime mode `0750` reached ready, but `to_jsonb(s)` on a PostgreSQL17 sequence lacked a composite type. Zero browser/UI stages; schema28 initialized. | [Failed terminal](storage-binding-live-ui-attempt2-terminal.json) and [dedicated ordinary disposal](storage-binding-live-ui-attempt2-disposal.json) retained; pair/runtime absent. The reader now selects `last_value`, `log_cnt` and `is_called` explicitly. |
| [Attempt3](storage-binding-live-ui-attempt3-browser.json), `20260912_080512_4d076f299a7a` | The tmpfs fixture was actually supported and did not produce the required unbound baseline. Only `browser_ready` completed. | [Failed terminal](storage-binding-live-ui-attempt3-terminal.json) and [ordinary disposal](storage-binding-live-ui-attempt3-disposal.json) retained; pair/runtime absent. The unsupported fixture was changed to proven ramfs. |
| [Attempt4](storage-binding-live-ui-attempt4-browser.json), `20260912_081819_2ede994eef07` | The broad `Unavailable` text locator matched multiple elements and was replaced with an Alert-scoped locator. The catch did not preserve the original stack, so this is not claimed as the sole cause of the failed assertion. | [Failed terminal](storage-binding-live-ui-attempt4-terminal.json) and [ordinary disposal](storage-binding-live-ui-attempt4-disposal.json) retained; pair/runtime absent. |
| [Run5](storage-binding-live-ui-accepted-browser.json), `20260912_083703_5a77d18d2ae0` | All 15 checks and 10 IPC stages passed; final bindings were A3, B1 and U2. | [Independent acceptance](storage-binding-live-ui-accepted-terminal.json) passed, including pair/runtime absence. This scope is consumed. |

| Accepted evidence | SHA256 |
| --- | --- |
| [Independent terminal](storage-binding-live-ui-accepted-terminal.json) | `7d5755df17cd4f10b869628897dce0a496cd99df51a5ffc3c842faa8b5b49b38` |
| [Controller report](storage-binding-live-ui-accepted.json) | `a28c62e56630df9a7bff0dcfac0b00ecd57450f235d7f9e2fbdd5b69901c2d95` |
| [Browser result](storage-binding-live-ui-accepted-browser.json) | `a812dbdfe16dfd4183e5319182ed1c3ee3371beb2538e2ab6914e62ae625e479` |

The accepted scope and all four disposed failed scopes remain immutable.
Its original `78cb...` HISTORY record is independently retained in
`history-before.json` in the source55 target execution evidence. Legitimate
later source55 updates to shared HISTORY do not invalidate this completed UI
acceptance. Do not rerun the old `attest` against the changed shared record.
The frozen inputs and procedure below describe this consumed gate, not a new
execution authorization.

## Frozen product inputs

Use the existing source54 binary without rebuilding it:

```text
/opt/goby-test/exec-work-m3e/client-backup-run-20260912_053517_9cb0074fc731/tmp/goby-linux-amd64
SHA256 5f88c432d96825d5c3f8c2faccc64be673dab85849670707571c20fddd71594e
```

Use the already built frontend directory:

```text
/opt/goby-test/exec-work-m3e/storage-binding-workflow-web-03/dist
```

Its complete file inventory and hashes are in
[the frontend acceptance report](storage-binding-workflow-web-accessibility-verification.json).
The report pins source manifest
`e4ff472e027ff87e50b6e1a4f97b2d2d34ff2cdc31a29fb6e5cf6ba120882d48`
and web input hash
`9d14831513e4005bc0dfe5b9a7dcd5b7e995d1213b206fa029a26477a4ddf058`.
For example, `index.html` is
`dd980aa1c9f4c0504aff4006b1adcd074659e2998535e071274b5471ed28a514`.
Pin the entire inventory, including absence of extra files, rather than only
these examples. Pair the two product artifacts explicitly in the new manifest.

The [accepted TOOL05 closure](storage-binding-live-ui-tool05-guards.json) pins:

| Tool input | SHA256 |
| --- | --- |
| `verify-storage-binding-live-ui.py` | `87c401d9acabc383e074c4c5f9fd493c63222b5d545d73f9cf8c5f11e797a685` |
| `root-binding-live.spec.ts` | `57b3e4ba16072690931a9d861964fea9ecae73e9d3e25c48560a2a6ca8912945` |

The application does not embed this frontend. In
[`dashboard.go`](../../internal/server/dashboard.go), `GOBY_WEB_DIR` supplies
`/admin/`, `/admin/assets/*`, and extensionless routes such as
`/admin/libraries`. The same Go listener serves `/admin/v1/*`. No Vite server,
reverse proxy, compatibility client, or modification of the product artifacts
is needed.

## Existing implementation to reuse

| Existing source | Reusable part | Limitation |
| --- | --- | --- |
| [`root-binding-workflow.spec.ts`](../../web/admin/e2e/root-binding-workflow.spec.ts) | Accessible selectors, expected states, exact PUT body, consent reset and responsive assertions | It fulfills `/admin/v1/**` from `RootBindingMock`; running it against a new base URL is still mocked acceptance. |
| [`RootBindingDialog.tsx`](../../web/admin/src/RootBindingDialog.tsx) | Real dialog and status/error contract | The new test must observe it without replacing fetch or binding responses. |
| [`metadata-management.spec.ts`](../../web/admin/e2e/metadata-management.spec.ts) | Real login, library creation and response-before-click pattern | Use a new fixture contract and only the binding gate's permitted mutations. |
| [`backup-recovery-runtime.spec.ts`](../../web/admin/e2e/backup-recovery-runtime.spec.ts) | Private fixture/result files, bounded real HTTP observation and screenshots | Its recovery actions and schema assumptions do not belong in this gate. |
| [`verify-backup-recovery.py`](../../scripts/test-env/verify-backup-recovery.py) | `RuntimeRunner`, `Worker.prepare`, `start_app`, `browser_phase`: executable descriptor pinning, uid995 child, separate runtime directories and single-worker browser | Its two-database recovery fixture and old table inventory are not reusable authority. Do not run the old entrypoint. |
| [`run-client-backup-tests.py`](../../scripts/test-env/run-client-backup-tests.py) | Current workspace cluster checks, shared lock, exact database/role ownership, schema28 catalog verification, HBA restoration and ordinary disposal | Its fixed `goby_backup_m3e_source/target` names and `client-backup-pair.json` receipt must not be reused. |
| [`verify-bound-scan-root-mount.py`](../../scripts/test-env/verify-bound-scan-root-mount.py) | Reviewed `unshare --mount --fork --kill-child --propagation private`, namespace witnesses and independent terminal proof | Its exact source54 test selection, pair and consumed request/receipt remain immutable. |

Do not use deployed-service wrappers, historical `/dev/shm/goby-pg-m4c`
assumptions, or old cleanup that terminates arbitrary PostgreSQL backends.
[`playwright.config.ts`](../../web/admin/playwright.config.ts) already selects
one worker, zero retries, no trace and no video. Set the new origin explicitly;
its fallback port18096 is outside this gate.

## Fresh isolated scope

The scope contract requires one fresh run with an unpredictable suffix and no
reuse of a prior UI or database receipt. The accepted tool and run namespace
templates are:

```text
WORK       /opt/goby-test/exec-work-m3e
TOOL       WORK/storage-binding-live-ui-tool-05
EVIDENCE   WORK/storage-binding-live-ui-<run>
RUNTIME    /opt/goby-binding-ui-runtime-<run>
DB         goby_binding_ui_<run_without_separators>
ROLE       goby_binding_ui_r_<run_without_separators>
CONTROLLER goby-storage-binding-live-ui-controller-<run>.service
WORKER     goby-storage-binding-live-ui-worker-<run>.service
ORIGIN     http://127.0.0.1:18288
```

The TOOL05 path and accepted run above are pinned by the input preflight. The
consumed intent is not reusable. These templates do not establish that any new
destination or port is available. Require nonexistence and an unoccupied port before creating resources;
fail on a collision without choosing a different destination silently. Also reject
ports5432,15432,18096,18097,18196,18197,18198 as application destinations.

Use the existing owned PostgreSQL17 workspace on port15432, directory
`/var/lib/postgresql/goby-workspace-v1`, and socket directory
`/var/lib/postgresql/goby-workspace-v1/socket`. Recheck its current identity
under its existing lock. Give the new ordinary login role only its new database;
record database/role/public-schema OIDs, owner tags, grants, role flags and exact
temporary HBA bytes. One database is sufficient because this gate does not
restore a backup. The controller's maintenance connection is separate from the
application URL and must never leak into its environment.

Keep `EVIDENCE` and credentials root-owned0700/0600. Both `/opt/goby-test` and
`WORK` are root:root0700; no runtime below either directory is traversable by
uid995 without changing an existing ancestor. Do not change either directory
or any source ancestor's permissions. The new runtime is directly below `/opt`,
whose observed ownership/mode is root:root0755. Recheck that parent identity
before creating the fresh runtime root as root:goby0750. The earlier
root:goby0710 proposal is a failed assumption: `openDirectory` opens each path
component with `O_RDONLY`, so execute-only group traversal was insufficient in
Attempt1. Copy verified bytes there, checking both source and copy against the
frozen inventory. Keep the frozen executable root:goby0550, web directories
root:goby0550, and web files
root:goby0440; retain group read/traversal without application write permission.
Create the separate `RUNTIME/app` state directories as uid995-owned0700. Retain
a verified executable descriptor before starting the exact child. Pin
the new tool's complete static source closure, Playwright/Chromium identities,
and lockfile versions separately from the product artifact manifest.

Run a root controller in its new bounded unit. Its namespace child owns all
fixture mounts; Goby runs in that same private mount namespace as uid995 with
no supplementary groups, no capabilities and NoNewPrivileges. Use the host
network namespace so the isolated backend can reach port15432. The browser
needs only the new loopback origin. Do not join an existing service's mount
namespace. Record host namespace and mountinfo before and after; never allocate
loop devices or operate on shared media paths.

The operator must set all state paths explicitly. A production `config.Load`
starts recovery storage even when tests using a zero-valued config do not.

| Setting | Value for the new runtime |
| --- | --- |
| `GOBY_LISTEN`, `GOBY_PUBLIC_URL` | `127.0.0.1:18288`, `http://127.0.0.1:18288` |
| `GOBY_DATABASE_URL` | New role and database on `127.0.0.1:15432`, explicit `sslmode=disable` |
| `GOBY_RECOVERY_DATABASE_URL` | Empty |
| `GOBY_WEB_DIR` | Verified private copy of the accepted `dist` |
| `GOBY_MEDIA_ROOTS` | One new owned supported-filesystem directory, `RUNTIME/app/media` |
| `GOBY_SETUP_TOKEN` | Fresh private token of at least24 bytes |
| `GOBY_SERVER_NAME` | A unique bounded name for this binding UI run |
| `GOBY_COOKIE_SECURE` | `false`, for the isolated HTTP origin |
| `GOBY_TRUSTED_PROXIES` | Empty |
| `GOBY_API_KEY_MASTER_KEY_FILE` | `RUNTIME/app/master.key` |
| `GOBY_RECOVERY_STATE_DIR`, `GOBY_RECOVERY_OPERATIONS_DIR` | Separate `RUNTIME/app/recovery` and `RUNTIME/app/operations` |
| `GOBY_BACKUP_DIR`, `GOBY_LOG_DIR` | Separate `RUNTIME/app/backups` and `RUNTIME/app/diagnostics` |
| `GOBY_TRANSCODING_ENABLED`, `GOBY_TRANSCODE_CACHE` | `false`, `RUNTIME/app/cache` |
| `GOBY_FFMPEG`, `GOBY_FFPROBE` | Existing pinned `/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg` and `ffprobe` |
| `GOBY_PG_DUMP`, `GOBY_PG_RESTORE` | Existing pinned `/usr/lib/postgresql/17/bin/pg_dump` and `pg_restore` |
| `GOBY_STARTUP_TIMEOUT`, `TMPDIR` | `30s`, `RUNTIME/app/tmp` |

Carry over reviewed small diagnostic/backup byte budgets from the isolated
runtime pattern. Remove inherited `PG*` options and unlisted `GOBY_*` settings.
Use explicit browser, HTTP, IPC, SQL and overall deadlines; a proposed upper
bound is240s for the serial browser workflow and600s for the worker. Pin
PID/start ticks/boot ID/cgroup/executable hash and listening socket ownership
before the first bootstrap or credential request, then recheck at each stage.

## Public initialization and browser actions

Start directly at `/admin/libraries` so the application remains on the libraries
page after authentication. Block service workers. Allow only same-origin static
assets from the pinned inventory and the exact native routes below; do not
fulfill allowed application responses, inject cookies into application code,
or install a mock fetch implementation.

| Action | Real public API contract |
| --- | --- |
| Initial state | `GET /admin/v1/bootstrap` returns `Initialized:false`. |
| Create administrator in the UI | Fill `Setup token`, `Administrator username`, `Password`; click `Create administrator`. `POST /admin/v1/bootstrap` with `{SetupToken,Name,Password}` returns201 and `{User}`. Bootstrap does not create a login session. |
| Sign in in the UI | Fill `Username` and `Password`, click `Sign in`. `POST /admin/v1/session` returns200, `{User,CSRFToken}` and the native HttpOnly `goby_session` cookie. |
| Inspect allowed paths | `GET /admin/v1/storage/roots` and `GET /admin/v1/libraries`; verify only the new runtime's configured media root is exposed. |
| Create fixture library in the UI | Fill `Library name`, `Content type` = `Movies`, and the three exact `Media directories`. Explicitly uncheck the default-on `Scan after creating`. The sole `POST /admin/v1/libraries` has `Scan:false` and returns201 `{Library}`. |
| Open binding dialog | Click exact `Storage bindings for <library name>`; inspect dialog `Storage bindings` and combobox `Registered root`. |
| Observe root | `GET /admin/v1/libraries/{id}/roots`, then `GET /admin/v1/libraries/{id}/roots/{rootId}/binding`; use returned IDs, never guessed IDs. |
| Approve root | Check `I understand that a later complete scan...`, then click `Bind storage` or `Accept replacement`; only the selected root's PUT is allowed. |
| End session | Click `Sign out`, require `DELETE /admin/v1/session`204, then independently require401 using the retained exact cookie. |

Each PUT must carry the exact observed string revision, lowercase64-character
fingerprint, and `AcknowledgeMissingRemoval:true`, with native CSRF and Origin.
No query parameters are accepted by the binding routes. Observe real request
and response bytes with `waitForResponse` armed before the click, strict bounded
DTO decoding, and a redacted request ledger. Never put session cookies, CSRF,
passwords or setup tokens in reports, screenshots, logs or traces.

Create a second native session for the controller only when the stale-write
phase needs it. Its login must be independently tied to the new administrator,
new session row and owned login audit. It has no authority in any other database
or application. The browser and controller must use separate cookie jars.

## Real filesystem fixture and ordered assertions

Use three non-overlapping registered roots beneath the same configured media
anchor: `A`, `B`, and `U`. Choose owned ext4 locations only after the running
uid995 backend demonstrates the supported identity profile. `B` is the unchanged
control. `A/archive` is a private bind mount from an owned backing directory;
three empty backing directories produce distinct strong directory identities.
All directories and mount sources have a fixed manifest and ownership witness.
No real user media or generated audiovisual files are needed.

Fresh supported registrations bind automatically. The accepted unbound fixture
uses a small owned ramfs at `U` inside the private namespace. The independent
[ramfs capability observation](storage-binding-live-ui-ramfs-capability.json)
recorded uid995 with no supplementary groups, UUID ioctl `ENOTTY` and file-handle
`EOPNOTSUPP`. Attempt3 showed that tmpfs was supported on this host and therefore
did not satisfy this fixture contract. Require registration success and a
read-only row showing revision1 with null binding/actor/time. If a fresh kernel
observation does not produce that result, stop and report the missing fixture
capability. Do not clear a binding using SQL or patch production code. Remove
only the owned ramfs to expose the underlying supported `U` directory.

| Phase | Filesystem or actor operation | Required browser and persistence proof |
| --- | --- | --- |
| Baseline | Register A with `archive` bound to source1, B unchanged, U on ramfs; `Scan:false`. | A/B verified at revision1 and attributed to the new administrator. U's stored binding is null at revision1; its API status may be unavailable while unsupported storage is still mounted. No scan job or binding-update audit is created by merely opening/refreshing the dialog. |
| Unbound observation | Unmount only U's ramfs, exposing its supported directory. | U remains unbound with an observed topology/fingerprint. Approval is disabled before consent; refreshing clears consent and creates no PUT. |
| Real conflict | Replace A's `archive` bind mount with source2. Browser observes mismatch at revision1. Controller separately observes and approves that exact root/fingerprint, obtaining revision2. Browser then explicitly submits its stale revision1. | Browser receives real409 `root_binding_conflict`, clears/disables consent and requires explicit refresh. The failed request creates no revision increment or audit. Refresh returns verified revision2; no automatic retry occurs. |
| Browser rebind | Replace A's `archive` mount with source3 and refresh. | `Storage changed` and the `archive` boundary marked `Changed`; review approved/current identities. One consented `Accept replacement` PUT succeeds. UI says `Storage binding saved.`, `Verified`, revision3, real BoundBy and BoundAt. A fresh independent GET and row/audit read match exactly. B remains byte-for-byte at its approved revision1. |
| Unavailable | Temporarily remove uid995 access to the exact owned A directory, retaining its original mode for restoration. | Require an actual unavailable observation before asserting UI behavior. Prior approved topology stays visible; its boundary is `Not observed`, never `Removed`. There is no consent control or permitted approval and no persisted change. Restore the original mode and require verified revision3 again. |
| Browser initial bind | Select U again, refresh and consent. | One `Bind storage` PUT commits revision1 to2. UI, independent GET and audit agree. A remains revision3 and B remains revision1. |
| Narrow view and selection | Set viewport390x844; reopen the dialog and select each actual root by returned ID. | Full path/ID can be inspected, primary actions are reachable, no horizontal page overflow, switching roots never carries consent, and no additional PUT occurs. |

The baseline must verify A/B's stored snapshots independently; a library creation
response alone does not expose their binding status. Do not change the configured
anchor itself when swapping A's nested mount. This preserves the intended
root-specific control while exercising a real topology difference.

IPC between browser and fixture controller must be an exact, monotonic stage
protocol: private create-once request/ACK files or inherited pipes, a run nonce,
sequence, fixed action, exact root/mount IDs and bounded bytes. Only the controller
can change the fixture. An ACK is published after its independent observation;
the browser reports observed real responses. Missing ACKs are failures, not
permission to repeat an approval or mount operation. A lost PUT response requires
a new read and audit inspection; it must never trigger an automatic second PUT.

The expected successful update count is three: controller A1-to2, browser A2-to3,
and browser U1-to2. There is one intentional rejected stale PUT and no update for
B. Assert counts by exact root/action/revision and stage, rather than assuming a
fixed total of all bootstrap/session audit rows. The relevant audit action is
`library.root_binding.updated`, with library-root resource, previous/revision,
observation fingerprint and administrator identity.

This minimal live gate does not start scans. Require no scan jobs, media metadata
edits, backup/recovery actions, ordinary compatibility sessions or application
keys. Source54's accepted real mount scanner gate supplies separate scan/media
descriptor-lifetime evidence. Timing-dependent `Saving` controls, fabricated
malformed responses, cancelled requests and `scan_busy` remain explicitly covered
by the existing mocked/Go tests; they are not newly claimed as live cases here.
Adding live active-scan exclusion would require a separate reviewed deterministic
scan fixture and budget, not an unbounded large tree or injected scan row.

## Evidence, cleanup and terminal acceptance

Before the first mutation retain a new immutable intent containing every owned
path, product/tool hash, cluster witness, database/role OID and tag, exact units,
resource budgets, port, allowed routes, mount plan and expected row transitions.
Use bounded duplicate-free JSON and owned no-follow regular files for all inputs,
IPC and reports. Pin ancestor identities as well as file hashes; matching a
basename is not authority.

Retain per-phase binding DTOs, redacted method/path/status/body-shape records,
desktop/narrow screenshots, native actor/session IDs, mount/namespace witnesses,
and read-only snapshots of all35 schema28 tables, sequences and exact catalog.
The accepted schema28 catalog SHA256 is
`8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b`.
Snapshots containing session hashes remain private. Final proof must show the
specified revision/audit deltas and absence of unowned mutations, not just a
passing Playwright assertion or screenshot. Recheck product copies and media
file contents against their original inventories.

On success, revoke both new native sessions and prove exact-cookie401 before
discarding credentials, with the controller session last. On failure, stop the
browser first and retain any provisionally received cookie privately; determine
whether a new session or binding committed from independently owned rows/audits.
An uncertain response keeps the run failed even if cleanup succeeds. Any fallback
revocation is limited to the exact newly proven session. Never infer a usable
token from an arbitrary new database row or replay a login/approval automatically.

Restore owned permissions, capture final verified bindings while mounts remain
present, then stop the exact Goby child gracefully and close all observers.
Unmount only the recorded owned mounts in reverse order and finish the namespace
child. Verify browser/backend termination, empty owned cgroups, port closure,
absence of owned mounts and unchanged host mountinfo. Preserve the private
evidence even if cleanup fails.

Dispose only the new database/role after verifying their recorded OIDs/tags,
exact catalog and expected fixture rows, absence of active/prepared transactions,
replication slots and external role dependencies, and preserved global catalog
state. Use ordinary DROP without FORCE, CASCADE or terminating other backends.
Restore the exact prior HBA bytes and prove the new pair is absent. A failed or
ambiguous disposal retains the pair and gets a separate reviewed disposal plan;
the original intent is not reusable. Never feed this new gate's records into an
old fixed-pair runner by imitating its marker.

Record independent observations that source44 candidate/primary process identities
and product files remain unchanged. Do not call their HTTP APIs, modify their DBs,
restart services, or read any original/reference client process or asset. Normal
M3 business activity is not a reason to demand an unchanged source44 business-row
snapshot; this gate owns only its new database.

The worker produces a result proposal and exits. A separate outer observer must
then verify the exact unit invocation, exit status, MainPID0, empty cgroup,
cleanup receipts and current identities before creating terminal acceptance.
The worker cannot certify its own terminal state. No skipped required phase,
missing ACK, forced shutdown, residual session/pair/mount, or unverified artifact
can be converted into a pass.

## Next work

Source55's [two real PostgreSQL HTTP tests](collection-folder-source55-target.json)
passed with zero failures/skips and all six cleanup checks true; its
[independent targeted terminal](collection-folder-source55-target-terminal.json)
also passed. The [source55 full run](collection-folder-source55-full.json) and
[independent full terminal](collection-folder-source55-full-terminal.json) passed
2,173 tests across 25 packages with zero failures/skips, the Linux build and all
six cleanup checks. Its binary is 29,337,989 bytes with SHA256
`6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81`.
The full scope is terminal and consumed, and shared HISTORY is finished; do not
rerun that scope or this UI gate's old `attest`.

Product publication to `origin/main` is complete: the 96-file storage/scanner
change is commit `13b21d60c8bdc4caf8d59abdddc9e2b96dc52775`, followed by the
two-file Subviews change `16d75c38064008680fa60839c637efee2f12f2ae`.
The [Git reconciliation](collection-folder-source55-git-reconciliation.json)
checked 849 closure files: 848 matched the frozen raw bytes, and the sole
difference was CRLF in the historical
`internal/server/testdata/emby-4.9.5.0-playback-video-index-zero.json` versus LF
in Git. The [separate EOL verification](collection-folder-source55-git-eol.json)
passed `TestPlaybackInfoRetainsRecordedVideoIndexZeroExtensionCompatibility`
on an 807-input remote Go source copy without database access. This independent
single test is not added to the 2,173 full-suite count.

The candidate is now source55/schema28. Upgrade run
`20260912_100845_47bff13c329b` passed its
[independent attestation](client-schema28-accepted-attestation.json); the
[upgrade report](client-schema28-accepted.json) retains
`awaiting_outer_attestation` by design. Its final TOOL05 closure passed
[94 Python guards](client-schema28-tool05-guards.json),
[seven Go guards](client-schema28-tool05-go-guards.json), the
[helper build](client-schema28-tool05-helper-build.json) and
[preflight](client-schema28-tool05-preflight.json).
The four TOOL05 upgrade sources and 13 safe evidence files were published as
commit `212dc387581d9fffddfc7337bbfee1e77c2d04f4`; this 17-file tools/evidence
publication leaves product authority at `16d75c38064008680fa60839c637efee2f12f2ae`.

The executed sequence was a [real backup](client-schema28-accepted-backup.json),
an [independent schema27 to schema28 rehearsal](client-schema28-accepted-rehearsal.json)
and ordinary rehearsal disposal, baseline revalidation before shutdown, one
stop, [transactional migration](client-schema28-accepted-migration.json), an
independent 58-asset web copy at `/opt/goby-client-m3e/admin`, one start and five
anonymous GETs before attestation. All old columns in 35 tables, five sequences,
ACLs, credentials and recovery state were preserved. At upgrade completion,
counts were 75 sessions, 64 devices and 167 audits. The four roots remain at revision 1 with the new
binding fields NULL; historical audit fields have their migration defaults.
The absent master key remains absent with no application keys. The primary
remains source32/schema27 at PID `762090`; shared web and proxy state are unchanged.

| Candidate authority and last sealed baseline | Value |
| --- | --- |
| Process | PID `1458051`, start ticks `13067360`, invocation `d29ea64c63274346a79c7b3a7938f36e` |
| Upgrade controller invocation | `3821d3052fc24aa0ad23a3194300cd6b` |
| STATE SHA256 | `bb78a846d2d4b69b7e2550ed9e367549cbe2d0b763e2b69270ea98bed6570d82` |
| Runtime SHA256 | `1f245cd8f8c19dbe0b96b803dc8c60dd0cd4b7b7e2a8d99ccc2541e9430c4a7b` |
| Upgrade authority `after-full.json` SHA256 | `7b61f5c94c440e5f6fefc58f0ef04ee2bd3c8f118510ea727f6741c46488412d` |
| Run-v2 `after-full.json` SHA256 | `a13f976b7097e33337527ef2cf10ad9203d755e0fd2cec43307edfa6efbfe8bc` |
| Sealed v2 independent full snapshot SHA256 | `6dfe6cbbf2288b21a66466719067b2c0d063bb588e693e06479d20da4feb9cd4` |
| [Independent v2 failed terminal](client-library-changed-source55-v2-terminal.json) SHA256 | `b23a1a156e781c771e3bb4b1ba31bfb048e29e77504569d129397c79445265d6` |
| Run-v3 `after-full.json` SHA256 | `a84e5e480a70b7d84e49001aaae791a522cd38c6b1055dcb503f0312f933c2dd` |
| Sealed v3 independent full snapshot SHA256 | `668975cd51c66888fb363ba273fe15dec70646d87e9549adf0fbe79c20b4f3f0` |
| [Independent v3 failed terminal](client-library-changed-source55-v3-terminal.json) SHA256 | `e53e9777337c8c0b33a0cc86dad2e22c79d5e7f2601aa64dd68c6ed91cd9ecf1` |
| Run-v4 `after-full.json` SHA256 | `bd992669d5c9ade22768f210009e983b46384c2611a8d8d91986cb729f41236f` |
| Sealed v4 independent full snapshot SHA256 | `5ceeb7916191ceaa223a27062ed399614e906481528a9038400eed3c64c882f1` |
| [Independent v4 failed terminal](client-library-changed-source55-v4-terminal.json) SHA256 | `021a36cc9e6311107acf751b1bba7ca6b2bf1e6618c4a32b6b48c17866404012` |
| V4 complete scope inventory SHA256 | `2618849ab3caa0d8756f372e0a3c9022f6d3bc7b2ef325a8e623bcd27172e407` |
| Run-v5 `after-full.json` SHA256 | `a3d633f0d351001eff02d013c2ccc224adec9c5f5a659cab7bad3f4e9a4a20d6` |
| Sealed v5 independent full snapshot SHA256 | `6724f398e0aa0303005b2f77c629491b1444c21f78deb021849b67eb7a4d1fe5` |
| [Independent v5 failed terminal](client-library-changed-source55-v5-terminal.json) SHA256 | `348be31960bb19a9905c4cef2b6f19df9af25466e28959f00d1bd5e0a6a2183d` |
| V5 complete scope inventory SHA256 | `190b6ea5aaad2d4cb7ab8184623b5abfd42dc5493a6c640e5de4836399ae3970` |
| Run-v6 `after-full.json` SHA256 | `185ea8df81cc228c329f108354cfaa098824a863a4db1a24a7e2acacdf376fa3` |
| Sealed pre-v7 independent full snapshot SHA256 | `b5d792af3b3f8dc854b58bdabc7cfaec0ec87ee4382c0d85250b1b38af693c93` |
| [Independent v6 failed terminal](client-library-changed-source55-v6-terminal.json) SHA256 | `7ea63700f72924318e21e84c061fe1cda3f6d7f04373eaddf78a7f5abea1012d` |
| Sealed pre-v7 counts; not current live totals | 81 sessions, 69 devices, 179 audits |
| Run-v7 `after-full.json` SHA256 | `d89dd1a4446a35364d760fe5a303b05d7113375980e5bed067a27d49f071a0bd` |
| Current independent post-v7 full snapshot SHA256 | `31d08d24047da101774db727f4ea6f0ec0e67ce15b0ce3df40ec7ea819555364` |
| [Independent v7 failed terminal](client-library-changed-source55-v7-terminal.json) SHA256 | `daec6f02a1a611553a7312a4affb892c5defdbae760205bdd31431827f12118f` |
| V7 complete scope inventory SHA256 | `32e3cdd5dc838114513eb2b117884b5d8522a25b5d734c951e5f83fba576d55c` |
| Current independently verified post-v7 counts | 83 sessions, 70 devices, 185 audits |
| [Upgrade report](client-schema28-accepted.json) SHA256 | `328bc6abbf4d2fe0559a34ed62d9e7d9cf7b77c075c583f9070d7aa9761c8832` |
| [Independent attestation](client-schema28-accepted-attestation.json) SHA256 | `ec20d286a1998f0b27667603819853129e88b91579a97a25141bd6258e450032` |

The old `2659...` baseline is historical pre-upgrade evidence only. The three
failed upgrade scopes remain retained with their independent
[first](client-schema28-attempt1-terminal.json),
[second](client-schema28-attempt2-terminal.json) and
[third](client-schema28-attempt3-terminal.json) terminals. The third had completed
backup and rehearsal before a `RawMessage` whitespace difference caused a false
mismatch. Final TOOL05 compares a canonical, exact ten-field `RoleProperties`
structure without discarding values or normalizing unrelated state.

Original-client v1 completed the final TOOL05 upgrade-authority binding and
passed [315 loader, 52 driver and 59 controller guards](client-library-changed-source55-v1-guards.json).
The 127 actor guards remain earlier verification. Its
[Python preflight](client-library-changed-source55-v1-preflight.json) also passed.
The [actual v1 attempt](client-library-changed-source55-v1.json) then failed at
discovery before browser creation or login. Its complete ledger records zero
new sessions, devices and audits, zero metadata revision change, preserved old
state, and restoration `not_required`. The
[independent failed terminal](client-library-changed-source55-v1-terminal.json)
now records `failed_scope_sealed`: both exact unit invocations exited with status 1 with
MainPID0 and empty recursive cgroups, the browser directory is absent, and
cleanup was `not_required`. Old rows, sequences, private files, media, candidate
and primary were preserved. Its independent snapshot `569bb377...` has the same
logical state apart from `captured_at`; `7b61f5...` remains the upgrade authority.
V1 is consumed and must not be replayed. It preserved the candidate and primary,
and established no original-client acceptance.

The [setup diagnosis](client-library-changed-source55-v1-setup-diagnosis.json)
identified loader assumptions that rejected frozen catalog/manifest files with
mode `0600` while expecting `0644`, and misclassified legitimate index/sequence
columns as ordinary table columns. The v1 driver's `checkedHomeFile` path
used ordinary `JSON.parse`, which can round large integers. A separate pure
diagnostic using lossless JSON exposed an incompatible BigInt clone path; that
is not an actual v1 BigInt exception.

TOOL02/v2 passed [348 loader and 54 driver guards](client-library-changed-source55-v2-guards.json),
[127 actor guards](client-library-changed-source55-v2-actor-guards.json) and
[59 controller guards](client-library-changed-source55-v2-controller-guards.json).
Its [seven real-document checks](client-library-changed-source55-v2-real-documents.json)
used retained immutable records, the narrow current reader and mode `0600` inputs.
Only the v2 scope envelope was rebound for those checks; they do not constitute
an actual fresh before-snapshot. The separate
[fresh Python preflight](client-library-changed-source55-v2-preflight.json) passed.
Eight client tools and six safe evidence files were published in commit
`93e88dc2250d8ccfc094cee6a24e2e5e0da25740`; product authority remains
`16d75c38064008680fa60839c637efee2f12f2ae`.

The [actual v2 controller report](client-library-changed-source55-v2.json) and
[browser report](client-library-changed-source55-v2-browser.json) record successful
setup and B login, followed by Movies navigation and two physical/two frame
catalog observations. The target card was not observed within 25 seconds,
producing `library_changed_target_card_not_observed`. The report lacks the last
discovery DOM and corresponding API projections, so the cause was unknown from
v2 evidence alone. No native administrator operation or
metadata write was dispatched. The ledger records +1 session, +1 device,
+2 audits and revision delta 0, preserving old rows, sequences and private data;
restoration was not required. The
[independent v2 failed terminal](client-library-changed-source55-v2-terminal.json)
now records `failed_scope_sealed` and reconfirms B revocation, UI logout and token
rejection, capability registration and browser/context/proxy closure. Complete
v2 root/tool files and the old v1 inventory are unchanged; candidate, primary,
media, old rows, sequences and private data are preserved. No further cleanup
was required. The failed scope is independently closed, with client acceptance
still false.

V2's independent state was 76 sessions, 65 devices and 169 audits at
`6dfe6cbb...`; the run-v2 snapshot `a13f976b...` differs only in `captured_at`.
`7b61f5...` remains upgrade authority only and cannot
serve as a current zero-delta database baseline.

TOOL03/v3 passed [449 loader and 58 driver guards](client-library-changed-source55-v3-guards.json),
[127 actor guards](client-library-changed-source55-v3-actor-guards.json),
[64 controller guards](client-library-changed-source55-v3-controller-guards.json),
[two actual prior-authority checks](client-library-changed-source55-v3-prior-authority.json),
the [fresh Python preflight](client-library-changed-source55-v3-preflight.json)
and [six JavaScript actual-document checks](client-library-changed-source55-v3-real-documents.json).
Its eight frozen tools and ten safe evidence files were published in commit
`01bd683a9ea32ce5d6ff8acd02e432883a55fc15`; product authority remains `16d75c3...`.

The [actual v3 discovery record](client-library-changed-source55-v3-discovery.json)
still reports `library_changed_target_card_not_observed`. CollectionFolder
returned 200 with the correct `Subviews`, and Items returned 200 with one target with the
correct `Id`, `Name` and `Type:Movie`; both frame/physical pairs were complete
and unambiguous on the expected `videos` route. The page had zero nodes whose
`data-id` matched the target ID, rather than duplicate target siblings, and the API type
case matched. V3 evidence alone did not establish a cause or whether a visual
card appeared. The screenshot was omitted with the sole recorded reason
`visible_content_not_safe`; no more specific filter cause is inferred.

The [independent v3 terminal](client-library-changed-source55-v3-terminal.json)
sealed the failed scope with B closed, no native operation, no required cleanup
and a per-run ledger of +1 session/+1 device/+2 audits/revision delta 0. V3 ended
at 77 sessions, 66 devices and 171 audits. Its independent full
snapshot is `668975cd...`; run-v3 `a84e5e48...` differs only in `captured_at`.
V1/v2 and their old tools/roots, candidate, primary, media, old rows, sequences
and private data were preserved. V3 is consumed and must not be replayed.

V4 passed [513 loader, 61 driver and 127 actor guards](client-library-changed-source55-v4-guards.json),
[67 controller guards](client-library-changed-source55-v4-controller-guards.json),
[Python preflight](client-library-changed-source55-v4-preflight.json) and
[six actual-document checks](client-library-changed-source55-v4-real-documents.json).
Its eight frozen tools and eight safe evidence files were published as 16 files
in commit `f9ab33d0d125aeb5f16aa5a6071da6b8bdd126f9`; that publication does not
include the later independent terminal and leaves product authority at `16d75c3...`.
The [actual v4 report](client-library-changed-source55-v4.json),
[DOM diagnostic](client-library-changed-source55-v4-discovery.json) and
[safe screenshot](client-library-changed-source55-v4-discovery.png) resolve the
discovery failure: the Movies page normally displays exactly one
`M3e Client Movie`, but the card has no `data-id`. The matcher incorrectly
required that attribute. The visual card is proven; this failure is not evidence
of a DTO defect. LibraryChanged windows and native metadata operations did not
run, so no LibraryChanged pass is claimed.

V4 records B closed and +1 session/+1 device/+2 audits/revision delta 0,
bringing the candidate to 78 sessions, 67 devices and 173 audits. Its
[independent terminal](client-library-changed-source55-v4-terminal.json) now
records `failed_scope_sealed`: both exact v4 invocations exited with status 1,
with `MainPID=0` and empty cgroups; complete v1-v3 inventories and their six units were
unchanged before and after. Candidate, main, media, old rows, sequences and
private data were preserved, with no cleanup required. The independent seal
made no HTTP requests, business SQL writes or service writes.
V4's independent full snapshot is
`WORK/client-library-changed-source55-execution-04/independent-after-full.json`,
SHA256 `5ceeb7916191ceaa223a27062ed399614e906481528a9038400eed3c64c882f1`;
it differs from run-v4 `bd992669...` only in `captured_at`. All v1-v4 scopes are
consumed, independently sealed and not reusable.

The [initial v5 guard report](client-library-changed-source55-v5-initial-guards.json)
retains 559 loader, 64 driver and 127 actor passes, but its 74 controller cases
included one error from a contradictory old fixture. That failed report remains
preserved at SHA256 `ce6de3f403e40d5408e06a1b813c3b0886ba398ca3698b972587cbfcc5d0df9d`.
An [independent 05b guard repair](client-library-changed-source55-v5-controller-guards.json)
corrected the fixture and added a contradictory-fixture rejection case; all 75
cases passed. It used an unchanged controller copy, SHA256
`9a699ccf5fce8b73d0d31a32fb030cf57d24a078041c7af32298be21d7ac2c32`.
The old failed guard and TOOL05 were retained; this did not change the production
controller. The [v5 preflight](client-library-changed-source55-v5-preflight.json)
and [actual-document checks](client-library-changed-source55-v5-real-documents.json) passed.
Its eight frozen tools and nine safe evidence files were published as 17 files
in commit `e96e771166ab5afd68639dc3836632838bb6dfd9`, using the passed 05b guard
with the actual v5 runtime sources unchanged. That publication did not include
the later independent terminal, and product authority remains `16d75c3...`.

The [actual v5 report](client-library-changed-source55-v5.json),
[browser report](client-library-changed-source55-v5-browser.json) and
[discovery diagnostic](client-library-changed-source55-v5-discovery.json) record
another tool failure before native editing. Three `itemsContainer` elements were
geometrically visible, but the collector enumerated cards only when that total
was exactly one, incorrectly recording zero cards. The target button, card and
owning container were visibly present, and the complete HTTP response contained
one correct Movie with status 200. The [safe screenshot](client-library-changed-source55-v5-discovery.png)
is identical to v4 (`07cab956...`, 38,695 bytes). This is a collector assumption
error, not a missing visual target or an established DTO defect.
The ledger records B closed and +1 session/+1 device/+2 audits/revision delta 0,
bringing the candidate to 79 sessions, 68 devices and 175 audits. Both owned units
reported exit status 1, `MainPID=0` and empty cgroups. The
[independent v5 terminal](client-library-changed-source55-v5-terminal.json) now
records `failed_scope_sealed`, with the exact closed 1/1/2/+0 ledger and no
required cleanup. Complete v1-v4 root/tool inventories and their eight units
were preserved, as were four additional guard/prerequisite directories under
strong before/after comparison. Candidate, primary, media, old rows, sequences
and private data were preserved. V5's independent full snapshot is
`WORK/client-library-changed-source55-execution-05/independent-after-full.json`,
SHA256 `6724f398e0aa0303005b2f77c629491b1444c21f78deb021849b67eb7a4d1fe5`;
it differs from run-v5 `a3d633f0...` only in `captured_at`. All v1-v5 attempts
are consumed and must not be replayed. No native edit occurred, and
LibraryChanged acceptance remains incomplete.

The initial TOOL06 [pure guard failure](client-library-changed-source55-tool06-guards-failed.json),
[DOM failure](client-library-changed-source55-tool06-dom-failed.json),
[failed DOM terminal](client-library-changed-source55-tool06-dom-failed-terminal.json)
and [diagnosis](client-library-changed-source55-tool06-diagnosis.json) remain preserved.
The separate [06b verification](client-library-changed-source55-v6-guards.json)
passed 877 guards: 127 actor, 66 driver, 605 loader and 79 controller cases.
All [13 real browser cases using owned HTML](client-library-changed-source55-v6-dom.json)
passed with an [independent DOM terminal](client-library-changed-source55-v6-dom-terminal.json),
along with [fresh preflight](client-library-changed-source55-v6-preflight.json)
and [actual-document checks](client-library-changed-source55-v6-setup-diagnosis.json).
The final 06b nine frozen sources and 16 safe evidence files were published as
25 files in commit `a4c4ab8e9b68be87d9e5e505dcc6de64e79f7651`, preserving the
initial TOOL06 failures and v7 working edits; product authority remains `16d75c3...`.

The [actual v6 discovery](client-library-changed-source55-v6-discovery.json)
passed: three geometrically visible containers, one card-owning container,
one card and one title button matched the complete correct Movie wire response.
The [run](client-library-changed-source55-v6.json) then failed when native login's
public `CreatedAt` used a valid `+08:00` offset and the validator demanded UTC
spelling. The [read-only time diagnosis](client-library-changed-source55-v6-time-diagnosis.json)
confirmed that the returned representation denotes the same UTC instant.
No rename or metadata request was sent, and LibraryChanged acceptance remains false.
The [independent v6 terminal](client-library-changed-source55-v6-terminal.json)
sealed two closed sessions, one new device, four audits and revision delta0.
The sealed pre-v7 totals are 81 sessions, 69 devices and 179 audits. That independent
snapshot is
`WORK/client-library-changed-source55-execution-06/independent-after-full.json`,
SHA256 `b5d792af3b3f8dc854b58bdabc7cfaec0ec87ee4382c0d85250b1b38af693c93`;
run-v6 `185ea8df...` differs only in `captured_at`. Earlier scopes, candidate,
primary, media, old rows, sequences and private data were preserved.

V7 compares only public `CreatedAt` by instant, accepting
the valid offset while keeping generic UTC validation and `LastEditedAt` strict.
It binds H6's complete two-session failed history rather than reusing the
earlier one-session allowance. Read-only
[native identity input preparation from sealed v6 material](client-library-changed-source55-v6-native-identity.json)
was completed before the run. Its nine-source closure was frozen and passed
[865 JavaScript guards](client-library-changed-source55-v7-js-guards.json)
(127 actor, 66 driver, 672 loader) and
[87 Python guards plus two compile checks](client-library-changed-source55-v7-python-guards.json),
for 952 guards in total. All [13 owned-HTML browser cases](client-library-changed-source55-v7-dom.json)
passed with an [independent DOM terminal](client-library-changed-source55-v7-dom-terminal.json),
along with [fresh read-only preflight](client-library-changed-source55-v7-preflight.json)
and [six actual-history document checks](client-library-changed-source55-v7-setup-diagnosis.json).
Eight changed sources and six safe records were published as 14 files in commit
`6eca52537d104e0108b97218339b2b4701f15c60`; the unchanged DOM guard remained in
the frozen closure and was not counted as a changed file. Product authority
remains `16d75c3...`.

The [actual v7 controller report](client-library-changed-source55-v7.json) and
[browser report](client-library-changed-source55-v7-browser.json), bound to the
[frozen input](client-library-changed-source55-v7-input.json), record failure with
`automatic_http_not_observed_within_window` at `restore-armed`. Real discovery
and armed evidence, native login 200 validation and the forward PUT 200/readback
had succeeded, but the first automatic refresh window did not capture client
automatic HTTP. The controller then reported native-restore 200 complete,
restoration confirmed and both sessions closed, with a 2/1/6/+2 ledger.
Both units exited with status 1 and `MainPID=0`. The
[independent H7 terminal](client-library-changed-source55-v7-terminal.json) now
records `failed_scope_sealed` at `restore-armed`, with the exact closed 2/1/6/+2
ledger and restoration of the original sparse/effective metadata confirmed.
The current independent snapshot is
`WORK/client-library-changed-source55-execution-07/independent-after-full.json`,
SHA256 `31d08d24047da101774db727f4ea6f0ec0e67ce15b0ce3df40ec7ea819555364`,
at 83 sessions, 70 devices and 185 audits. It differs from run-v7 `d89dd1a4...`
only in `captured_at`; `b5d792af...` and 81/69/179 are historical pre-v7 evidence.
The seal preserves 38 roots, 12 prior failed units, two old DOM units and the
current DOM unit, along with candidate, main, media, old rows, sequences and
private data. It confirms native validation, four safe native-identity fields,
nine native requests and the two metadata PUTs with reconciliations. No further
cleanup was needed or performed, and sealing made no new HTTP requests or business SQL
writes. V1-v7 are consumed and must not be replayed; LibraryChanged acceptance
remains false.

The [read-only refresh diagnosis](client-library-changed-source55-v7-refresh-diagnosis.json)
binds six original public artifacts. Physical WebSocket and page-frame evidence
received the same MessageId, with `ItemsUpdated` containing only the target and
the other item/folder arrays empty. Across all 252 actor and 251 proxy requests,
both complete channels contain zero requests within the forward window; all 237
DOM samples retain the old title. Page errors, overflow, observer errors and
proxy rejects/failures are zero. This excludes an already recorded request being
missed by the catalog matcher. Frame reception proves delivery to the page,
not execution of its internal event handler. The protocol/implementation cause
is not established, and no LibraryChanged pass is claimed.

The retained reference Name+Overview event uses the same field shape, but there
is no retained positive proof that original-client Movies metadata updates
trigger automatic HTTP and UI refresh. The Name-only control remains missing.
The new bounded reference UI control has six sources under implementation and
has not been validated or executed. It does not change Goby payloads or the
acceptance target, replay v7, or establish a new accepted client run. The pure
guard contract forbids external effects. A business-read preflight may log in and out
and produce owned authentication records; those records and HTTP requests must
be bounded, retained and cleaned up. That preflight is not a zero-HTTP claim.
Complete wire and MessageId correlation remain required.
Observation windows retain the original same-library Items route or the exact
target `/Users/{UserId}/Items/{TargetId}` route, bound to MessageId, route,
document and token. This adds no detail-page navigation, PlaybackInfo, media or
Playing requests. The normal allowance remains 2/1/6/+2: two new sessions,
one new device, six audits and two metadata revisions. No M3 acceptance is
established.

The reviewed [main schema28 upgrade plan](main-schema28-upgrade-plan.md) was
published in commit `933257c913cd36a57c26c72bb324f73b57441ecd`. The two Go helper
sources passed [15 pure tests with `-race` and a build](main-schema28-helper-verification.json),
with the [owned helper mode recorded](main-schema28-helper-mode.json). The plan,
two readiness reports, two Go files and two helper reports were published as
seven files in commit `ee7ea2c3816616f6b954c9a92596c24b9e3f594f`.
The [preliminary main read](main-schema28-readiness.json) observed PostgreSQL on
port 5432, schema27, 102 sessions, 14 devices, 19 audits, five libraries/roots,
four application keys and an existing 32-byte master key. This is not a complete main baseline.
Use both `EnvironmentFiles` entries from the
[service addendum](main-schema28-readiness-service-addendum.json):
`/opt/goby-test/runtime.env` and `/opt/goby-test/recovery-m5j.env`.
Retain the existing `WORK/main-deployment-schema25.lock`; the absent historical
M5j lock is not to be created. The TOOL01 Python controller passed
[51 remote guards and two compile checks](main-schema28-python-verification.json), with report SHA256
`50e43ce547acda73c6e1e33b72ed026bd50771efeb66636eecc9d5e6b467cf23`.
The two Python sources and three safe verification records were published in
commit `ee076930ccc7303e07f39c4a6dd322da051e30e9`, with all five frozen files
matching their exact bytes.
The [first read-only main prepare](main-schema28-prepare-attempt1.json) returned
`guard_rejected`. It produced no
baseline or deployment directory and made no business SQL, HBA or service
writes; the main process and three protected process identities were unchanged.
Its safe report SHA256 is
`ddf8fcf791e01a32e3393ebafb81080c126875d822e076950bd6131a49f32154`.
The [remote catalog diagnosis](main-schema28-prepare-catalog-diagnosis.json)
confirmed a representation bug: the outer value was a string while the trusted
objects were a list. Exact decoding produced the same 1,109 objects and canonical
SHA256 `e9d77790b352a739876e621b7a435e0716e7ea62436e7341748da73e1af90850`,
with no catalog drift. Only the catalog was read; no business rows were read or
written, and the first prepare input was not retried.
TOOL02 corrects the two Python sources using explicit `::jsonb` and strict type
guards. Its [54 remote guards and two compile checks](main-schema28-tool02-python-verification.json)
passed with report SHA256
`2fd4edfd4ed0012e0ed3811522dae1b01eddba6c7f71d649d11a6f5d037beb8b`.
Independent review passed. The ten-file publication
`2e83a7c38dc21435495b6f7c40d11f4543838064` includes the v4 terminal/docs and
TOOL02 corrections/verification. The original receipts
for 15 Go tests and the build are reused without changing their recorded argv;
TOOL02 verifies the actual TOOL01 build
provenance and binary copy.
The [second fresh read-only prepare](main-schema28-prepare-passed.json) passed
with CLI exit 0 and status `prepared` for `20260912_121742_14953df92aa5`.
Two complete captures passed strong comparison. The main PID `762090`, three
protected processes, TOOL02, old TOOL01 and the first prepare scope were
preserved, with zero business SQL, HBA or service writes.
The private baseline is
`WORK/main-schema28-source55-prepared-20260912_121742_14953df92aa5/baseline.json`,
SHA256 `4390cd27b77a28b732438a0a63d7f26b3e610e96d976ccb2f106f26918fbe2aa`.
The prepare input SHA256 is
`343a283642b754648e148aef1020faec7c1003948c54f279dae53b9809d29a44`.
Its `main_baseline` and `client_gate` inputs were null, `client_gate_checked`
remains false, and no deployment output exists. Its deadline was
`2026-09-12T13:17:42Z`, with at least 900 seconds required to remain at admission.
The earlier 13:35 UTC checkpoint already confirmed that scope had expired. A new scope, input and
read-only prepare are required; do not modify or reuse the expired input. Main remains
source32/schema27 with no source55 deployment or schema28 upgrade.

Main TOOL03 now keeps the controller's strict five-key authority separate from
the browser/input authority that additionally carries `before_snapshot`, binding
that value to the scope's original `before-full.json` bytes. Its
[55 Python guards and two compile checks](main-schema28-tool03-python-verification.json)
passed, and independent review found no blocker. The three files were published
in commit `45ebb226a1b4315e5f51855ea6e6d797df76469b`. Old TOOL02 and its expired
prepare remain preserved; TOOL03 verification did not execute prepare or upgrade.

SSH access has been restored and confirmed with a fresh connection. The
[GPU capability inventory](m4-gpu-test-env-capabilities.md), published in
commit `8604901`, records no exposed GPU suitable for hardware acceptance on
`test-env`. The user has not supplied a GPU environment, so actual GPU acceptance
remains outstanding.

The [global NextUp acceptance plan](nextup-global-acceptance-plan.md) was
published in commit `37d2257`. It is planned and unexecuted, and requires a
separate fresh scope after the active LibraryChanged work releases its fixture.

1. Bind new control preparation to the complete H7 terminal, current independent
   83/70/185 snapshot, exact restored 2/1/6/+2 ledger and preserved inventories.
   Keep all consumed v1-v7 scopes immutable and do not reuse the pre-v7 baseline.
2. Prepare the bounded original-client UI control and its pure guards plus
   business-read preflight to fill the missing metadata-refresh positive and Name-only control.
   Account for permitted owned login/logout records and HTTP during preflight;
   only the pure guards make a zero-external-effects claim.
   Preserve MessageId/document/token correlation, Goby payloads and the existing
   acceptance target; do not replay the consumed v7 attempt.
3. Obtain actual navigation and both automatic-refresh window proofs in a fresh
   admitted client scope, with independent cleanup/terminal acceptance. The
   successful PUT/readback and controlled restoration alone do not prove the
   client outcome.
4. Wait for an independent terminal proving actual Movies navigation and both
   LibraryChanged windows. The old main prepare has expired: create a fresh
   scope/input/prepare and recheck current state before backup/restore rehearsal,
   the single upgrade and independent attestation. Preserve the deadline and
   remaining-time admission bounds; successful prepare alone does not establish
   the client gate or deployment admission.
5. Keep broader M2-M6 acceptance open and retain the existing M7 deferred scope;
   this accepted storage-binding gate does not reduce either scope.
