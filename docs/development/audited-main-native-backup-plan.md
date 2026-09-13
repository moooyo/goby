# One source32 native backup

Status: **consumed; startup rejection and owned cleanup closed; no backup created**.
This records the bounded action under the [execution plan](../planning/current-execution-plan.md)
and [upgrade contract](audited-main-upgrade-plan.md). The scope is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-native-backup-01`.
Its admitted goal was one schema27 recovery point with the existing source32
executable, followed by main returning inactive. The goal was not achieved.
No migration, restoration, new-product installation or client replay occurred.
This consumed input must not be executed again.

## Actual result and required plan correction

The first controller rejected its reserved-prefix check before acquiring the
deployment lock: it mistook the retained, hash-pinned read-only
`controller-draft.py` for an execution artifact. The original controller,
admission and failure remain preserved. The corrected check accepts only that
exact pinned helper and still rejects every execution artifact. Eleven remote
controller guard groups passed, including the new prefix regression.

The corrected controller then installed the permanent capacity profile and
temporary restart fence, and submitted exactly one source32 start. All original
configured environment values matched. Systemd supplied two additional values,
`MEMORY_PRESSURE_WATCH` and `MEMORY_PRESSURE_WRITE`, which the adapter had not
included. Full environment verification rejected the process before HTTP. The
original controller had not yet assigned its invocation identity, so its
automatic stop correctly refused an unbound process and retained responsibility.

Independent review admitted only closure of this already-started invocation.
The closure rechecked the original binary, configuration, deployment lock,
start-intent window and exact PID510771/startTicks5697079/invocation
`b8029e326a34460b97848cfb0dacbbb5`. The two extra values were accepted only as the
owned cgroup's `memory.pressure` path and the exact encoded
`some 200000 2000000` plus NUL notification setting. One owned stop completed;
the process and cgroup disappeared. The deployment lock was released unchanged.

The [actual post-stop observation](audited-main-native-backup-closeout.json)
closed one read-only database transaction and the private-file comparison:

- All 35 tables, 402 rows and five sequence values remained exact. There were
  zero logins, HTTP requests, new sessions, new activity rows or new backups.
- Lifecycle, operations, the existing backup, master metadata, candidate
  processes and PostgreSQL configuration remained unchanged.
- The cache now contains its two native marker/lock files. Ten old diagnostic
  log bodies remained exact; the old 413-byte entry closed and one new 451-byte
  log closed. No pending object or encoding job was introduced.
- Main and source55 are inactive. The permanent capacity profile and exact
  temporary restart fence remain installed. Fence removal was not attempted.

The backup experiment is paused after two controller failures, with the actual
startup and cleanup fully retained. No third attempt, create or restart is
admitted. Native key authentication, a fresh schema27 archive and both isolated
restoration proofs remain open. The [restore draft](audited-main-isolated-restore-plan.md)
remains unadmitted and cannot run without that archive.

The next admission must separate stop authority from full environment/readiness
acceptance: bind the process to this exact start before checks that can reject a
validly owned process. Keep configuration checks strict and preserve the actual
pressure-variable evidence. Reuse this post-stop state, including the installed
capacity/fence, cache markers and eleven closed logs. Do not rebuild a generic
runner, erase the failed input or restore the obsolete pre-start file baseline.

| Retained remote evidence | SHA256 |
| --- | --- |
| `controller-preflight-failure.json` | `e247d886f82d4ab6dce6bf5ed01fe1c3ebee05496f82552497a7cce5b781ddd8` |
| `admission-corrected.json` | `4910118caa050eb44d5785f3d22ce38350ea8e072cd4a90965a1cf55178aa7db` |
| `execution.json` | `5967957bf7279390f96d54f95304393baa080d5f37893b81f1e0567f0c0d2fce` |
| `owned-start-closeout.json` | `4d4ccfa2af144ee53a214acd9397315d18a08f86473f0e80a7dd2f9271d00b27` |
| `post-stop-observation.json` | `427180ad7e66a26c91cdcf28e482faa6150b11aad557b80854309dd91fc0b55f` |

The remaining sections preserve the original prepared inputs and planned
successful deltas; they do not describe achieved backup outputs or authorize
another execution.

## Accepted preparation

The main binary remains SHA256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`,
source `b9bb7b1cf11e07e011a6e1726ddb9d53ef8e1fe9`. Its preserved installation
is recorded in the [material checkpoint](audited-main-recovery-materials.md).
Main and source55 remain inactive. Main PostgreSQL is still PID893 under the
accepted boot, with system identifier 7683277964552005578, database16385 and
owner16384. The selected lifecycle remains primary/revision0/default master.

The first operational reader rejected a catalog projection before connecting:
the trusted catalog includes index and sequence column objects as well as table
columns. The correction admits only the 35 declared table projections, while
checking the excluded relations against the same trusted catalog. Nine remote
guards passed. One actual read-only repeatable-read transaction then captured
all 35 tables, 402 rows and five non-MVCC sequence observations. The complete
private JSON snapshot is 245,271 bytes. Its frontend/backend closed and the
existing deployment lock was released unchanged. The first failed receipt and
its 15 ms budget remain included in the correction; no business input was replayed.

The frozen browser credential identifies one existing, enabled administrator
with a password. Authentication has not occurred. Four application keys are
present. The earliest retained activity is from 2026-09-10, with no expiry in the
reviewed 2700-second window; both stored task triggers are retired. Fresh data,
clock and ownership checks are still required immediately before mutation.

The HTTP adapter passed 12 remote synthetic guards. The filesystem adapter
passed 10 guards with 21 negative variants, using saved native documents and
synthetic post-workflow state. These checks make no actual HTTP request and do
not authenticate a key or archive. Nine old closed logs still require their
actual content hashes and newline checks in the pre-start file observation.

The final service adapter passed 12 remote guard groups. Its first launcher
omitted the SSH marker required by the unchanged guard and exited during setup;
the failed receipt is retained. The corrected launcher supplied that marker and
retained complete diagnostics. The controller then passed 10 synthetic guard
groups with 27 negative variants. Each successful guard frontend closed with exit code0.
These component checks supplement the nine catalog guards; they are not live
backup or restoration results. Independent integration review accepted the
HTTP/data/file interfaces, snapshot deltas and owned service cleanup order.

The original admitted controller SHA256 was
`960e3a03a202ad83bd798db9caeb7d23a1334622da4b42b9bb7905605e1c6d0d`.
Its external `admission.json` binds every prepared input and exact guard receipt,
including controller guard
`ef263f0ecbbb4ade921389b907a98cdaa28508783b3307636025500cba1df6d9`.
The corrected controller was
`87b4d3a8c2c452789c7765aaee76b37bd1da270caaff93c9944aac9cf7a805c7`;
its eleven-group guard receipt is
`1177ae516aec8c88c495732cd2d4073163bdd8d7a9a81e5c1b9efc0eb14f53cd`.
The execution scope has no reusable business retry slot.

## Allowed configuration and service actions

The prepared capacity remains 64 MiB per object, 256 MiB total and 128 MiB minimum
free. Install its three exact values through the new root-owned mode0600
`/opt/goby-test/main-backup-capacity.env`, referenced by the new permanent
`50-backup-capacity.conf` main drop-in. Existing unit and environment files are
not edited. Require 318,832,640 available bytes: the 302,055,424-byte backup floor
plus 16 MiB for auxiliary evidence. Recheck immediately before publication.

The new runtime `90-audited-backup-once.conf` sets `Restart=no`,
`RuntimeMaxSec=40min` and `LimitCORE=0`. Keep the existing 90-second stop policy
and control-group kill mode. The controller also disables its own core dumps.
Freeze one start, a 300-second startup bound, an 1800-second HTTP operation window,
up to 120 HTTP attempts with four reserved for cleanup, and a 2700-second outer
budget. No automatic start, login or create retry is permitted.

The controller must distinguish `Type=simple` pre-exec startup from failed
ownership, and successful inactive state from a retained failed invocation.
Normal inactive state may have an empty `InvocationID`; ownership must still
follow the originally bound PID, start, command and cgroup evidence. Account
for the unit's automatic `LOGS_DIRECTORY=/var/log/goby-test` environment.
Hold the existing verified deployment lock throughout. Only stop the proven
invocation; preserve uncertainty instead of adopting a different process.

On accepted completion, remove only this scope's runtime fence and owned empty
runtime drop-in directory, reload the manager and retain the capacity profile.
A failed or uncertain workflow keeps its evidence and restart fence pending
review. Fence removal errors must enter the exact-file recovery path.

## One HTTP workflow and its outputs

Use native administrator login, one new 32-character request ID and the new
private passphrase. Read the initial operation/object inventories, submit one
create, observe its actual terminal, read the ready object and download it once
in full. Do not issue HEAD or Range: each download authorization writes audit.
Revoke the original cookie and verify 401 using that same cookie. A lost create
response permits one read of the operation inventory to identify the same
request; it does not permit another create request.

The native `Engine.Create` must authenticate the existing master against the
sealed history in the same snapshot used for SourceFacts and pg_dump. The
standalone key observer remains retired. Success requires the actual completed
operation, published ready object, complete download and matching object bytes,
size and SHA256. Source32 predates the later `ValidateDump` call; the two distinct
isolated restoration proofs remain mandatory after creation.

Expected row changes are one finally revoked native administrator session and
five activity rows, in order: login, requested, finished, downloaded, revoked.
All old rows remain exact, including JSON value types. Devices are unchanged.
The activity sequence and its five new IDs advance together; the other four
sequences remain exact. Server-ID startup may execute its same-value update
without changing logical row values.

The archive snapshot should contain 405 rows: baseline 402 plus the new session,
login audit and requested audit. Its activity count is 21 and application-key
count remains 4. Completion, download and revocation add three later audit rows,
yielding 408 operational rows. These points are compared through their declared
deltas, not through an incorrect whole-table equality claim.

Expected private-file changes are control revision5 to revision9 with one new revision4
completed operation; one new ready backup object; two empty-cache marker/lock
files; closure of the old 413-byte diagnostic entry and one new finally closed
log. Keep lifecycle/master identity, old operation/object and old log bytes.
Reject leftover pending/partial files or encoding jobs. The final CAS pair
proves revision8 to revision9 and its identities; intermediate revision6/7 file bodies are not
claimed as observed. Verify the candidate processes/unit bytes and PostgreSQL
configuration boundaries without replaying unrelated fixture work.

Final admission must bind the exact controller, component guards and prepared
inputs. A successful HTTP result alone cannot close SQL, native stores, worker
resources, the service invocation or the temporary policy. Publish those
results separately, and retain all failures.
