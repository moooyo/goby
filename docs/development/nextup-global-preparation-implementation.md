# Global NextUp preparation producer implementation

Status: **140 remote guards and two compile checks passed after the Devices
repair. The earlier live preparation stopped before library/account creation.** The source is
[prepare-nextup-global-reference.py](../../scripts/test-env/prepare-nextup-global-reference.py).
The remote-only guards are
[test-prepare-nextup-global-reference.py](../../scripts/test-env/test-prepare-nextup-global-reference.py).
This producer creates neither a reference result nor a client acceptance claim
until a separately authorized live run actually supplies those observations.

## Execution and publication boundary

The producer uses five distinct boundaries:

1. `validate_manifest` and `frozen_plan` reject missing structural authority
   and freeze the source-bound phase budget. They perform no HTTP or writes.
2. `Authority` reads only explicitly pinned owned Python sources, private JSON
   inputs, historical JSON evidence, and the approved synthetic MP4/manifest.
   It validates the complete retained public baseline before creating any
   output. Original implementation/data roots are explicit exclusions.
3. The existing reference lock is acquired without replacement. Current
   metadata binds the original application and its separate existing host
   proxy. The released v4 controller/worker units are checked through current
   systemd properties and their exact empty cgroups.
4. `PreparationRunner` creates an exclusive journal/media root and follows the
   bounded public workflow. Each HTTP attempt requires an fsynced intent,
   reserved ordinal, private pending state, and another authority check.
   Resource-creation responses retain a separate ownershipPending transaction
   until complete identity registration and its final commit are durable.
5. Successful preparation stops at `awaiting_independent_attestation`. All
   three exact tokens have logout and same-token 401 proofs, but
   `matrixInputsUsable` remains false. Draft matrix/execution documents set
   `binding.fixtureReleased=false`; the published consumer rejects them.

The independent root task owns the outer systemd runtime, broader quiescence
and preservation observations, and final attestation/publication. It must
review actual private responses and final files before issuing an attested
manifest. Changing a boolean in a draft without that independent work is not
the intended publication process.

Use the isolated interpreter entry point, including for plan mode:

```text
/usr/bin/python3 -I -B /absolute/owned/prepare-nextup-global-reference.py plan --manifest /absolute/private/input.json --manifest-sha256 EXACT_SHA256
/usr/bin/python3 -I -B /absolute/owned/prepare-nextup-global-reference.py prepare --manifest /absolute/private/input.json --manifest-sha256 EXACT_SHA256 --plan-sha256 EXACT_PLAN_SHA256
```

The script rejects a direct entry without `-I -B` before importing other
standard-library modules. Prepare also requires Linux, root, and an SSH
environment. There is no automatic resume, existing-root adoption, generic
retry, service restart, proxy creation, bridge setup, or namespace entry.
No historical operator `preconditions()` is called.

## Manifest contract

The complete manifest has exactly these top-level fields:

```text
schemaVersion, runId, target, ownerUid, server, endpoint, process, lock,
sources, inputs, scope, sealedRoots, forbiddenOriginalRoots, media, actors,
libraries, preservation, budgets, matrixBudgets, lifecycleSeparationSeconds
```

`schemaVersion` is integer 1, `target` is `reference`, and `ownerUid` is integer
0. `server` contains `id` and `version`. `endpoint` is exactly HTTP loopback
port 18197. `process` uses the published transport's current `application`,
`endpoint`, and `workerNetworkNamespace` shape. Original executable identity
is metadata only: path, device/inode, PID/start ticks, boot, UID, command,
cgroup, and namespace. No original executable hash is accepted or read.

`sources` contains descriptors `{path, sha256}` for `preparation`, `transport`,
`matrix`, and `proxy`. The first three filenames are fixed. The transport must
have reviewed SHA-256
`d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1`.
The existing proxy Python path must appear in its actual command line beside
the bound `--reference-pid` and `--reference-start-ticks` values.

`scope` contains `fixtureRoot`, `inputRoot`, `sourceRoot`, `proxySourceRoot`,
`evidenceParent`, `outputRoot`, and `matrixEvidenceRoot`. All paths are explicit
normalized Linux paths. The lock remains inside fixtureRoot. The future
matrix root is a fresh direct child of evidenceParent; it is disjoint from
the preparation output. `sealedRoots` excludes all completed historical roots
from new output/media creation. `forbiddenOriginalRoots` excludes original
implementation and original server data from byte reads and output. It must
cover the original executable and any `-programdata` root in its command.

`actors` contains admin/P/Q. Admin fields are userId, username, credentialRef,
and deviceId. P/Q contain username, credentialRef, deviceId, and matrixDeviceId.
The five device IDs are distinct and absent from the retained device registry.
P/Q usernames are absent from the retained roster. `libraries` contains LA/LB,
each with distinct stable `name` and `seriesName` strings. Actual new account,
library, view, root, series, season, and episode IDs are discovered only from
completed public responses.

`preservation` contains exactly six old userIds, eight old libraryIds, and six
detailRoutes. Each detail route has group/userId/itemId, identifying the
already retained old full-detail witnesses. Its group/item mapping must match
the retained snapshot. A changed historical population requires a new reviewed
plan rather than dynamic expansion.

## Actual input receipts required from the root task

`inputs` contains four owner-only JSON descriptors: `release`,
`publicBaseline`, `credentials`, and `mediaApproval`. They must be beneath
inputRoot, except that publicBaseline may refer directly to an explicitly
sealed historical root. These descriptors must be real current files; a
symbolic path, copied boolean, or missing receipt prevents prepare admission.

The publicBaseline input is a complete retained public snapshot, initially the
v4 `after-public.json`, with
exactly marker/version/captured_at/server/roster/configuration/libraries/
catalog_by_library/items_by_user/preferences/details/devices/credential_context.
All identities, all six detail witnesses, capture time, and the original
administrator-token fingerprint are checked before output or HTTP. The new
snapshot retains its own fresh token fingerprint. These two fingerprints are
compared as provenance, not treated as a business-state equality requirement.

After the stopped preparation described below, the next run must use a newly
observed complete snapshot. The input schema accepts a complete six-user,
eight-library snapshot with the larger retained device population; it has no
hard-coded 85-device requirement. All five proposed new preparation/matrix
device IDs must remain absent from that actual baseline.

The private credentials input has this shape:

```json
{
  "schemaVersion": 1,
  "runId": "{same runId}",
  "accounts": {
    "admin": {"userId": "{existing admin}", "username": "{name}", "credentialRef": "{ref}", "password": "{private value}"},
    "P": {"username": "{new name}", "credentialRef": "{ref}", "password": "{private value}"},
    "Q": {"username": "{new name}", "credentialRef": "{ref}", "password": "{private value}"}
  }
}
```

Passwords are distinct and at least 32 characters. The manifest never embeds
them. Login sends actual UTF-8 URL-encoded form bytes with
`Content-Type: application/x-www-form-urlencoded; charset=utf-8`.

The release input contains exactly schemaVersion/kind/runId/process/lock/
sealedRoots/releasedAt/sealed/units/closedAuthentication. Its kind is
`nextup-global-preparation-release`; process and lock exactly match the
manifest. `sealed` contains one `{kind: terminal, record: {path, sha256}}` and
one `{kind: inventory, record: {path, sha256}}`.

The actual terminal must report
`status: protocol_observation_complete_independently_confirmed`, a capture time
no later than releasedAt, true recursive_cgroups_empty,
full_target_restored_except_etag,
administrator_logout204_same_token401_verified,
viewer_logout204_same_token401_verified, and media_unchanged, plus false
reference_database_read and original_implementation_bytes_read. Its
scope_inventory descriptor must equal the supplied inventory descriptor. The
inventory must parse as a nonempty JSON object or array. The original sealed
bytes remain unmodified.

`units` contains exactly the two unit names in that terminal's systemd map.
Each row is `{name, invocationId, properties, cgroupPath}`. Properties contains
exactly ActiveState/SubState/MainPID/Result/ControlGroup. The allowed terminal
state pairs are active/exited and inactive/dead, with MainPID `"0"` and Result
`"success"`. The invocation and terminal properties match the sealed record.
The current systemd read must match again. ControlGroup is empty or
`/system.slice/{name}`; cgroupPath is exactly
`/sys/fs/cgroup/system.slice/{name}`. If it still exists, its bounded recursive
cgroup.procs population must be empty. Unrelated Node processes do not enter
this decision.

Each optional prior closed-authentication row contains userId,
reportedDeviceId, tokenSha256, from, through, logout, and rejection. The
reported device and user must be present together in the retained baseline.
Each logout/rejection value contains record, intent, status, tokenSha256, and
completedAt. Record and intent are descriptors of actual retained
controller_api `*-result.json` and `*-intent.json` files:

- The result must have complete=true, actual status 204/401, matching
  token_sha256, and matching completed_at.
- The actual intent must have the controller_api channel and POST
  `/emby/Sessions/Logout` for logout; rejection is GET `/emby/System/Info` or
  `/emby/Sessions`.
- Logout precedes rejection, both are inside from/through, and through does
  not exceed releasedAt. The two response receipt digests are distinct.

These verified windows permit only the prior closed device's DateLastActivity
to advance, retaining its structural fields and ownership. They do not permit
arbitrary old account, policy, preference, catalog, or media drift. A different
browser-only evidence schema is an explicit integration gap until its actual
retained fields receive a separately reviewed adapter; do not manufacture a
controller response from a summary.

## Approved media binding and output

`media` contains source/ownedRoot/approvedReceipt/approvedReceiptSha256/roots.
The source fields are path/sha256/sizeBytes/device/inode/uid/gid/mode/nlink/
mtimeNs/ctimeNs. The actual source is a bounded owned MP4 within ownedRoot;
its current identity and bytes must match. approvedReceipt is a real owned
JSON manifest descriptor within ownedRoot, and its digest must equal
approvedReceiptSha256. Its marker is `goby-client-media-m3e-v1`, movieProfile is
600 seconds/30 fps/H.264/AAC, and files[source-relative-path] equals the source
digest. The mediaApproval input repeats these exact bindings, plus integer
schemaVersion 1, kind `nextup-global-preparation-media-approval`,
durationSeconds 600, frameRate 30, and originalImplementationBytesRead=false.

The destination roots are exactly outputRoot/media/LA and outputRoot/media/LB.
The producer creates six independent media files, two tvshow NFOs, six episode
NFOs, and two ownership markers. Each copied file is re-read, hashed, and
checked for independent inode ownership; the original source is rechecked.
No source link is removed or replaced. The actual public scan must establish
one source at 600 seconds and 30 fps per episode, with no alternate version or
auxiliary playable media.

## HTTP phases, cleanup, and retained outputs

The separate preparation limit is 320 requests: 240 normal plus 80 reserved
for cleanup. The normal phase maxima are 32 before, 42 library creation/scan,
5 root/view mapping, 8 account setup, 36 own-token baseline, 44 calibration,
28 final zero proofs, and 37 final snapshot, totaling 232. Successful logout
adds six requests, so the success maximum is 238. Early stable scan completion
uses fewer requests; unused capacity authorizes no additional hypothesis.

Each library creation is followed immediately by one exact
Library/VirtualFolders/Query reconciliation. Until that response establishes
the unique new library ID and the registration is durable, it is the only
permitted follow-up request. The second library is created only after the
first identity commits. This adds one bounded inventory GET compared with the
earlier shared lookup, within the unchanged 240-normal/80-cleanup allowance.

Each new library receives one scoped Items/{id}/Refresh. The scan has at most
twelve three-request observation rounds and a 100-second deadline, requiring
two equal complete ready states. No global Library/Refresh route is allowed.
Both accounts have exact two-library policies and independently read their
Views, catalogs, twelve episode details, and twelve summaries before playback.

Calibrations are P/A1 partial, P/A1 complete, Q/B1 partial, Q/B1 complete. Each
uses its own token and real acknowledged source/session identities. The full
before-zero, before-DELETE, DELETE, and after-DELETE responses provide sixteen
ordered observations; the two actual logins produce eighteen distinct private
response receipts. The resulting contractVersion 2 cleanup receipt is consumed
by the published transport's actual validator. Playback is a protocol control,
not proof of actual media delivery or client playback.

Login, account creation, library creation, and PlaybackInfo responses first
persist ownershipPending with their actual response receipt digest and
uncertain=true, before response decoding or nested identity consumption can
complete. Shape, identity, and uniqueness checks are inside this durable
uncertainty interval. Complete JSON with null/list in User, Policy,
SessionInfo, or MediaSources cannot fall through to known-responsibility
cleanup. The producer also retains all four acknowledged PlaySessionIds, so a
session ID from an earlier completed lifecycle cannot be reused unnoticed.

Ownership commit writes the complete actor/account/library/playback context
with stage owner-registered while uncertainty remains true. Only a subsequent
durable state commit clears ownershipPending and uncertainty. If either write
fails, the retained state and terminal continue to identify unresolved
ownership; no later HTTP, including cleanup HTTP, is authorized. The original
raw response remains independently available for a separate recovery review.

Known-responsibility cleanup has at most 77 requests: two stop slots, two
reconciliation details, two conditional episode DELETEs, twenty-four full zero
and summary proofs, four profile/preferences reads, a 37-request public
snapshot, and six exact logout/rejection requests. Lost responses, unverified
logins/negotiations, or persistence failures stop further dispatch and retain
recovery responsibility. A normal observation failure may close known
responsibilities once, but remains a failed preparation outcome.

Private journals retain actual headers, form/JSON payload bytes, bounded raw
response bytes, response headers, completion times, and pending state. Safe
exports are separate files. Passwords, tokens, session/source/device IDs, and
native paths are removed from exports. Duplicate JSON keys, invalid UTF-8,
nonfinite values, conflicting response framing, and invalid status types
cannot establish an acknowledged preparation state.

Successful pending outputs include eight `draft-{receipt}.json` files,
matrix-credentials.json, draft-matrix.json, draft-execution.json, and
calibration-history-disposition.json. The eight receipt names match the
transport exactly. All accounts, catalog, copied media, authentication records,
and protocol audit history remain declared artifacts. Exposed UserData returns
to zero; the producer never claims audit deletion or whole-database equality.

## Verification record

The guard entry requires explicit owned preparation/transport/matrix source
paths and writes an exclusive report to a fresh remote tool directory. Every
case uses temporary synthetic files and fake HTTP/process/clock observations.
Network and business process access are blocked by the harness. Actual
Authority cases additionally exercise owned-file admission, real temporary
flock contention, and six independent temporary media copies; they never read
the real synthetic source or connect to a business service.

All **140 guards passed**, with zero failures, errors, or skips, through
`/usr/bin/python3 -I -B` in
`/opt/goby-test/exec-work-m3e/nextup-global-preparation-tool-06`. Both sources
also passed remote `py_compile` checks. The exact
[guard report](nextup-global-preparation-verification-03.json) has SHA-256
`75e69bf98593ea0e711bbe6f0098f15b1c8bd6e30a122b47a1e26cf50475e243`.

| Frozen source | SHA-256 |
| --- | --- |
| Preparation producer | `2c1d4d31fd2dfaf01fac0969d89acc881774aa2005b97b647442e625e634c98d` |
| Preparation guards | `dcafaf8ff644ab4cccb2686a0f15c808b095de62672ba2449942757a9ab1dc70` |
| Transport consumer | `d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1` |
| Matrix planner | `da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259` |

Earlier tool scopes remain retained. [Tool-01](nextup-global-preparation-tool01-guards.json) exposed a direct-dispatch clock
harness issue; the clock contract and harness were corrected. Tool-02 passed
the [64-case pipeline suite](nextup-global-preparation-tool02-guards.json).
[Tool-03](nextup-global-preparation-tool03-guards.json) added actual-Authority cases and identified
a missing capture timestamp in the synthetic terminal fixture; tool-04 includes
that fixture correction. Its completed
[83-case receipt](nextup-global-preparation-verification-01.json), SHA-256
`2f8aad13308cb29b619ce9afdb65862e9147795ee58a2fc1d685414fbb99a103`, and the
entire remote tool-04 scope remain unchanged.

Independent review then identified ownership-consumption failures after complete
HTTP 200 responses: a null nested policy/source could raise after HTTP pending
had cleared, and a reused PlaySessionId could fail before the new responsibility
was registered. Tool-05 adds 23 malformed-resource/duplicate-session cases and
eight persistence-boundary cases for login, account, library, and playback
ownership. They prove actual raw-response retention, a durable unresolved
ownership record, recovery_required, cleanupComplete=false, and zero HTTP after
the failure. Its [114-case receipt](nextup-global-preparation-verification-02.json),
SHA-256 `418e9083ed1dfc380d0f029489af6554d32a458602bed4ef6db79d7159b70f16`,
and the tool-05 source/evidence scope remain unchanged. Tool-06 adds seven pure
Devices decoder cases and nineteen complete-snapshot cases. Every normal fake
pipeline now retains an initial old device, inserts each acknowledged login's
device into an independent registry, and returns the observed zero sentinel.
The 140-case receipt binds the current repaired sources above.

The private input schema is unchanged by this repair. A new reviewed plan must
bind the repaired preparation source digest and its revised 232-normal/238-success
maximums; an old plan/source pin cannot authorize this version.

No local verification was performed. These guard runs issued no business HTTP
or real process probe and never read the real source media. Separate
[authority](nextup-global-authority-observation.json) and
[Goby preservation](nextup-global-goby-preservation.json) observations inform
the root task's actual input assembly; they are not preparation admission.
Current real reference preparation inputs remain the independent root task's
responsibility. These guards establish the
operator contract under synthetic responses, not reference compatibility.

## Stopped live preparation and the Devices count sentinel

The separately authorized live tool-05 producer ran in
`/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-02` and stopped
with `stopped_with_known_cleanup`: 31 normal requests and two cleanup requests.
It created only its administrator session, then proved logout 204 and rejection
401 for that exact token. No new ordinary account, library API mutation, or
playback request occurred. Six copied synthetic MP4s were staged and remain as
declared artifacts. The root task independently sealed the completed scope;
its execution record `independent-terminal.json` has SHA-256
`588c4f09fc817ed555d7f2711f692f94e35f1f41bd288748626fffca12546111`.

The last normal response was actual `GET /emby/Devices`, HTTP 200 and complete,
with exactly the keys Items/TotalRecordCount, 86 unique device IDs, 86 unique
ReportedDeviceIds, and TotalRecordCount=0. Its raw response record is
`private/0031-before-devices-response.json`, SHA-256
`9b06672040960b1c8d4bdc466d7ad93c83dc62fcff405e836c27263d4e62498e`.
The earlier actual v4 `after-devices-result.json`, SHA-256
`431be8903ae39e93eeb68627bf0197b4adbf428486338246299b87e0b8631f8f`,
also returned TotalRecordCount=0 with 85 unique device and reported IDs. A
read-only comparison of these retained responses found all old 85 DTOs exactly
equal and one new device matching the stopped run's actual administrator
device/user/name and request client metadata.

`devices_page` therefore handles this exact route separately. It requires the
two observed top-level fields, a bounded Items array, an actual integer count
equal to zero or the measured array length, and unique nonempty Id and
ReportedDeviceId values. It does not apply catalog pagination semantics to the
zero sentinel. The generic `page` decoder is unchanged and still rejects a
nonempty catalog with TotalRecordCount=0.

The runner adds population checks before accepting Devices: every baseline
device ID and ReportedDeviceId must remain; each actual acknowledged
preparation login must have exactly one new device with its exact user and
client metadata; and there may be no other new devices. Missing old/owned
devices, repeated identities, substituted owners, or an unowned new device
are rejected. Existing device DTO business fields and receipted closure-time
changes remain subject to the separate preservation comparison.

## Honest baseline renewal after the stopped attempt

The stopped attempt observed the server, roster, libraries, catalogs, per-user
projections, preferences, six explicit details, and Devices. It did not reach
the following System/Configuration read. These records cannot be labeled a
new complete snapshot, nor may an old configuration document be silently
inserted under the stopped run's token/time attribution.

The minimum new complete-observation allowance is one new independent admin
login, the full 31-GET snapshot sequence, one logout, and one exact-token
rejection: **34 HTTP requests**. Authentication and device history remain
retained; it issues no catalog, media, user-creation or playback mutations. It
needs its own exclusive evidence scope and current process/lock bindings. The new snapshot
must include the stopped preparation's retained administrator device and the
observer's own device. A snapshot captured before observer logout retains its
own token attribution and is accompanied by that observer's actual closure
window, intent, and response receipts.

The [separate observer02](nextup-global-baseline-observer-02.md) completed all
34 requests and independent attestation, including configuration, 88 device rows
and exact-token closure. Its baseline is now available for fresh preparation.
The root task checks the stopped preparation and observer units are closed in
the new outer admission record. The producer's release contract may continue to
bind the genuine sealed v4 terminal/inventory anchor, with the new observer's
actual controller_api logout/401 evidence supplied to closedAuthentication.
No release-schema expansion or fabricated composite snapshot is required.
