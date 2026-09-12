# Storage binding live administrator UI acceptance

Status: proposed operator and browser contract; no operation in this plan has
been executed. The current accepted backend prerequisite is source54's 2,173
full-suite passes across 25 packages, Linux build, and subsequent real private
mount acceptance. The existing 68 frontend checks use mocked binding APIs.
This gate must connect the built administrator UI to the real schema28 backend.
Publication and deployment remain separate work.

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

The reviewed operator should create one fresh run with an unpredictable suffix,
not resume any prior UI or database receipt. Suggested namespaces are:

```text
WORK       /opt/goby-test/exec-work-m3e
TOOL       WORK/storage-binding-live-ui-tool-01
EVIDENCE   WORK/storage-binding-live-ui-<run>
RUNTIME    /opt/goby-binding-ui-runtime-<run>
DB         goby_binding_ui_<run_without_separators>
ROLE       goby_binding_ui_r_<run_without_separators>
CONTROLLER goby-storage-binding-live-ui-controller-<run>.service
WORKER     goby-storage-binding-live-ui-worker-<run>.service
ORIGIN     http://127.0.0.1:18288
```

These are proposed names, not claims that the paths or port are available.
Require nonexistence and an unoccupied port before creating resources; fail on
a collision without choosing a different destination silently. Also reject
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
before creating the fresh runtime root as root:goby0710. Copy verified bytes
there, checking both source and copy against the frozen inventory. Keep the
frozen executable root:goby0550, web directories root:goby0550, and web files
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

Fresh supported registrations bind automatically. To reach a real unbound row
through public APIs, initially place a small owned tmpfs at `U` inside the private
namespace. Registration's explicit unsupported-capability path can leave its
binding null while retaining its authorized mapping. This is a runtime fixture
precondition, not a claimed tmpfs result: require registration success and a
read-only row showing revision1 with null binding/actor/time. If the current
kernel does not produce that result, stop this scenario and report the missing
fixture capability. Do not clear a binding using SQL or patch production code.
Remove only that owned tmpfs to expose the underlying supported `U` directory.

| Phase | Filesystem or actor operation | Required browser and persistence proof |
| --- | --- | --- |
| Baseline | Register A with `archive` bound to source1, B unchanged, U on tmpfs; `Scan:false`. | A/B verified at revision1 and attributed to the new administrator. U's stored binding is null at revision1; its API status may be unavailable while unsupported storage is still mounted. No scan job or binding-update audit is created by merely opening/refreshing the dialog. |
| Unbound observation | Unmount only U's tmpfs, exposing its supported directory. | U remains unbound with an observed topology/fingerprint. Approval is disabled before consent; refreshing clears consent and creates no PUT. |
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

## Work required before dispatch

1. Implement a new narrow operator plus memory-only guards for this exact fresh
   scope, based on the reusable patterns above. Review it independently before
   execution; do not modify or invoke historical operators as a shortcut.
2. Add a separate live Playwright spec, private fixture loader and exact stage
   protocol. Keep `root-binding-workflow.spec.ts` as the existing mocked suite.
3. Freeze the tool closure and complete input manifest, verify the unsupported
   fixture/preflight contract, and prepare independently checked terminal and
   disposal logic. No runtime authority is established by this document alone.
4. Run reviewed verification only on `test-env`; preserve the new run's reports
   and screenshots. Update the milestone evidence after successful independent
   terminal acceptance. Publication/deployment is a subsequent scoped operation.
