# Global NextUp transport adapter

Status: **The frozen playback-contract bundle passed all 379 remote guards
(53 matrix, 100 transport, 161 preparation, and 65 operator) and eight compile
checks. Cleanup requires version 3, complete stopped lifecycles, shared strict
percentage validation, and at most one DELETE per lifecycle. No live matrix or
client acceptance is established.**

The adapter in [nextup-global-transport.py](../../scripts/test-env/nextup-global-transport.py)
connects the existing pure `Matrix` API to explicit private authority, bounded
HTTP, and durable receipts. The current adapter and planner share the
[percentage contract](nextup-playback-percentage-contract.md); the adapter does
not prepare a fixture. Its command-line entry always refuses live execution.
The [preparation producer](nextup-global-preparation-implementation.md) and
[reference operator](nextup-global-reference-operator.md) require independently
accepted real inputs before their live paths are available. Goby
execution is also rejected until source, schema, and fixture observation receipts
have their own concrete transport contract.

## Public interfaces

`Authority(execution, probe=process_identity)` validates the exact version-one
execution object, acquires the existing fixture lock without creating or
replacing it, and checks concrete inputs before every dispatch and after a
completed response. `TransportRunner(execution, transport=..., probe=...,
monotonic=..., sleeper=...)` creates one fresh evidence root and exposes `run()`.
It has no resume or retry method. Injected probe, clock, and transport objects
are intended for fake guards. The default `HTTPTransport` performs only the
single exact request passed by the runner.

`HTTPTransport.send(request, headers, payload, timeout_seconds=..., max_bytes=...)`
returns a `WireResponse` containing actual status, ordered header pairs, raw
bytes, framing completeness, completion timestamp, and failure type. It follows
no redirects, retains no cookies, and performs no automatic retry. The bounded
socket timeout is supplemented by a process timer. Response bodies must be
complete, bounded, and strict UTF-8 JSON without duplicate keys or nonfinite
numbers. An empty body remains `None`. Only logout and token-rejection HTTP 401
responses may retain a non-JSON UTF-8 body when their media type does not claim
JSON; their matrix contract depends on the actual status.

## Private execution contract

The exact top-level fields are `schemaVersion`, `ownerUid`, `matrix`, `scope`, `lock`,
`process`, `endpoint`, `sources`, `receipts`, `credentials`, and `budgets`.
`matrix` is the source-bound planner preparation manifest. Do not use a guard
fixture as a preparation input.

| Field | Required concrete evidence |
| --- | --- |
| `scope` | Existing absolute `fixtureRoot`, `receiptRoot`, `sourceRoot`, `proxySourceRoot`, `credentialRoot`, and `evidenceParent`; all directory identities are pinned. Receipt and credential roots must be owner-only. |
| `lock` | Existing path under `fixtureRoot`, filesystem device number, and inode. An exclusive nonblocking flock is held for the run and checked against the same open descriptor. |
| `process` | Separate `application` and `endpoint` identities: PID, start ticks, boot ID, UID, executable path/device/inode, command line, cgroup, and network namespace. Endpoint also has `listener: {port, socketInode}`. `workerNetworkNamespace` matches the existing proxy's namespace; the application may be in a different namespace. Executable content is never read by this adapter. |
| `endpoint` | Exactly HTTP, `127.0.0.1`, and the existing owned proxy's bounded port. The default observer proves the listener socket belongs to the endpoint PID. No new proxy, bridge, or namespace route is created. |
| `sources` | Exact matrix and current transport paths/digests under `sourceRoot`, plus the existing owned Python proxy path/digest under `proxySourceRoot`. The proxy command must contain this script and exactly matching `--reference-pid`/`--reference-start-ticks` arguments. |
| `credentials` | Exact owner-only store path and SHA-256 under `credentialRoot`. Its two actor entries contain credentialRef, userId, username, and independent private passwords. No input token is accepted. |
| `receipts` | Exact private path/digest descriptors for preparation, coordination, catalog, cleanup, P/Q policy, and LA/LB media receipts. Every file is reread and hashed during authority checks. |
| `budgets` | Finite integer request timeout, normal/cleanup deadlines, request body limit, response body limit, total response-body allowance, and an eighty-response cleanup byte reserve. The planner independently enforces its 300/220/80 request limits. |

Response headers have separate fixed limits of 100 fields and 32 KiB. The
request route and headers have a separate 32 KiB limit. These metadata limits
are independent of the manifest's explicitly named response-body budget. The
HTTP reader accumulates bounded chunks until actual EOF, declared completion,
or the capture bound, including close-delimited nonempty responses.

Each receipt uses `schemaVersion: 1`, `kind: nextup-global-{name}`, the same
`runId` and `target`, and a `facts` object. Preparation binds the exact process,
endpoint, version, server ID, credential/catalog digests, P/Q IDs, retained
artifact disposition, and unique `evidenceRoot`. Reusing those receipts with a
different evidence root fails before HTTP. Reopening the original evidence root
also fails rather than resuming.

Coordination retains the release timestamp and exact lock, fixture root, and
both process identities. The planner binding refers to the application, while
HTTP uses the existing proxy. Login's actual ServerId/user/session response is
the final public application identity check. The adapter does not alter or
claim to remove any executable checks inside the already existing proxy.
Catalog retains the exact semantic library/item maps. Policy receipts
retain prior user IDs, the newly created user response, and the complete observed
ordinary profile. Media receipts bind the source-root item ID and native root to
the exact three files, sizes, and media hashes for each library. Cleanup uses
the version-three four-calibration contract described below.
No boolean preparation attestation replaces these receipt or runtime checks.

The adapter only verifies retained preparation evidence and immutable source
bytes. It does not independently discover a catalog or demonstrate that a future
preparation producer captured those facts correctly. That producer remains a
separate live integration gate.

## Version-three four-calibration cleanup receipt

The receipt wrapper remains schemaVersion 1 and `kind: nextup-global-cleanup`.
Its exact `facts` keys are `contractVersion: 3`, `process`, `serverId`, `actors`,
and `calibrations`. The process and server bindings match the execution inputs.
The matrix binds the SHA-256 of this complete receipt. Historical version-two
and single actor/item proof shapes are rejected by the current adapter.

`actors` contains exactly P and Q. Each value contains `credentialRef`, the
actor's preparation `deviceId`, and `login`. `login` contains actual HTTP
`status: 200`, the complete original login `body`, and
`responseReceiptSha256`. The login body must acknowledge the bound ServerId,
ordinary User.Id/Name/Policy, AccessToken, and SessionInfo.Id/UserId/DeviceId.
P/Q tokens, session IDs, and preparation device IDs must be independent. These
are preparation sessions, not the future matrix sessions; their tokens remain
only in the private receipt and may already have been revoked.

`calibrations` contains exactly four objects in this fixed order:

| Actor | Item | Mode |
| --- | --- | --- |
| P | A1 | partial |
| P | A1 | complete |
| Q | B1 | partial |
| Q | B1 | complete |

Each object has exactly `actor`, `item`, `mode`, and these eight events in order:
`beforeZero`, `playbackInfo`, `started`, `progress`, `stopped`, `beforeDelete`,
`delete`, and `afterDelete`. Each event uses this envelope:

```json
{
  "ordinal": 1,
  "completedAt": "{actual timezone-qualified completion timestamp}",
  "responseReceiptSha256": "{actual private response receipt digest}",
  "request": {
    "method": "{GET, POST, or DELETE}",
    "route": "{exact owned actor/item or playback route}",
    "headers": [["{actual header name}", "{actual header value}"]],
    "body": "{null for GET/DELETE; exact object for playback POST}"
  },
  "response": {
    "status": 200,
    "body": "{actual full DTO, playback response, DELETE value, or empty body}"
  }
}
```

This is a shape illustration with an HTTP 200 response; Started, Progress, and
Stopped require integer status 204. It is not a live or guard fixture. Request
ordinals increase across all 32 events, and completion timestamps
cannot move backwards. The two logins and 32 events reference
34 distinct private response records, each retaining its own request and
completion context; identical response bodies do not justify reusing a record.
Each actual X-Emby-Token is fingerprinted and matched
to that actor's actual acknowledged preparation token. The standard Emby
Authorization metadata must contain Client, Device, DeviceId, and Version,
with the same preparation DeviceId. Duplicate authentication headers,
alternate token/cookie sources, additional authorization token fields, and
another actor's token or device are rejected. Session ownership follows the
token's acknowledged SessionInfo; no fictional SessionId request header is
introduced. Only the reviewed ordinary HTTP headers plus Authorization and
X-Emby-Token are allowed. Parallel X-Emby-Authorization and device headers are
not alternative identity sources.

The GET routes are exact full details for the fixed actor/item; DELETE is the
same actor's individual PlayedItems route, with no query or request body.
PlaybackInfo is POST to the exact item with the actual user ID and IsPlayback.
Its response must acknowledge one source with the bound Episode runtime and a
new PlaySessionId. All four PlaySessionIds must be distinct. Started, Progress,
and Stopped must each return 204 with request bodies binding that source,
PlaySessionId, acknowledged login SessionId, and item. Started reports position
zero; Progress and Stopped report 120 seconds for partial or the full runtime
for complete. Progress uses TimeUpdate; Stopped uses Failed=false and
IsAutomated=false. These retained acknowledgements precede the changed-state
detail and the single DELETE.
Every detail must identify the mapped Episode, parent, series, numbering, and
authoritative runtime. The adapter consumes the published planner's
`userdata_fact`, `zero_state`, playback-date, `require_episode_percentage`, and
`require_playback_userdata_change` contracts directly:

- Each `beforeZero` and `afterDelete` must prove zero history and have exactly
  equal complete UserData, including field presence and primitive types.
- The partial and complete calibrations for an actor must share that same
  baseline. An absent LastPlayedDate cannot silently become explicit null.
- Partial state requires boolean Played=false, integer position 1200000000,
  positive integer PlayCount, and an actual timezone-qualified LastPlayedDate.
- Complete state requires boolean Played=true, positive integer PlayCount,
  and an actual timezone-qualified LastPlayedDate. Position remains bounded by
  the planner's zero-to-runtime contract; this increment adds no unobserved
  requirement that the completion position must be zero.
- The shared change check permits Played, PlayCount, PlaybackPositionTicks,
  LastPlayedDate, and a separately validated PlayedPercentage. Every other
  full UserData value and type remains equal.

A missing PlayedPercentage stays missing. A present value must be a finite
integer or float in [0, 100], with Played=false, and must exactly equal
100 * PlaybackPositionTicks / the actual bound runtime. The check uses integer
ratios, with no tolerance or normalization; booleans, null, strings, nonfinite
numbers, and inconsistent values fail. Played=true with a present percentage
remains unproved and is rejected. The helper does not authorize DELETE or
replace the separate full-baseline restoration equality check.

The DELETE response body is retained as observed; no particular JSON shape is
guessed. The adapter validates these bound event values and unique digests; it
does not independently dereference all 34 raw-response files. Raw wire receipt
paths/bytes and the preparation producer's separate
success terminal remain independently reviewable producer evidence. All three
preparation sessions must be retired before that producer publishes success;
the adapter does not become a preparation state machine or infer logout from
the four cleanup calibrations. This schema introduces no receipt digest cycle:
the producer captures logins and calibrations, writes cleanup, then writes the
matrix/execution manifests that bind the cleanup digest.

## Attempt and response ordering

For each planner request the runner checks authority, computes the actual actor
token fingerprint, and fsyncs an exclusive intent containing the exact request,
source/plan/execution digests, ordinal, and private matrix state. It then calls
`authorize`, fsyncs the reserved state, rechecks all authority and the remaining deadline, and
makes at most one transport attempt. Slow persistence cannot extend the request
past the frozen phase deadline.

The raw response receipt is private and independent of the sanitized export.
It includes actual request headers/payload and raw response bytes, which can
contain credentials. The response receipt file's real digest is passed to
`accept` only after durable capture, strict decoding, and export. Actual login
tokens are retained privately before consumer validation. Only a fully verified
P/Q login enables its token for subsequent dispatch.

This is an **at-most-once dispatch contract**, not a promise of exactly-once
server effects after network loss. A lost response retains pending intent and
permanently blocks that runner. No late response, repeated `run`, replayed label,
new login, or automatic recovery is permitted. Consumer exceptions are handled
even when the planner has already cleared pending or raised an exception outside
its normal observation-failure handler. Unverified login/playback responses
remain explicit recovery responsibilities.

The planner records each DELETE attempt during authorization, before the
transport call, against the latest acknowledged stopped actor/item/PlaySessionId
lifecycle. A normal DELETE and a cleanup DELETE cannot both consume that
lifecycle. Complete non-200 DELETE responses retain their actual response
digest, pending intent, and blocking failure; they cannot enter known cleanup.
A DELETE 200 followed by failed exact-zero verification marks restoration
failure and also requires separate recovery instead of a second cleanup DELETE.
Different acknowledged PlaySessionIds remain separate permitted lifecycles.

Private snapshots use atomic replacement and fsync. Root/private/export
directories have pinned identities and held directory descriptors; journal
creation and replacement use those descriptors. Files are exclusive and
owner-only. Persistence failures stop further dispatch and are reported as
incomplete evidence even if another private snapshot cannot be written.

## Cleanup and evidence boundary

The runner never prefilters cleanup routes. A retained ordinary observation
failure may enter the planner's single cleanup branch when all ownership is
known. The planner stops known active lifecycles, observes touched episodes
again, and only then selects or removes its already enumerated episode DELETE.
The STOP body uses the last acknowledged position and the original negotiated
context. A failed cleanup or unknown pending request needs separate recovery.

Each recorder token remains available after logout solely for that same token's
HTTP 401 rejection proof. Complete closure requires the planner's closed mode,
both rejection proofs, and no unverified login responsibility. An empty queue
alone is insufficient. Original observation failures and subsequent blocking
failures are both retained.

Exports redact credentials, cookies, session/play/source IDs, device IDs, native
paths, and secret URL values before writing. `SessionInfo.Id` and
`MediaSources[].Id` are collected by context because their literal key is only
`Id`. Private matrix snapshots and raw wire receipts are never exported.

This transport increment establishes no new public reference response, positive global rule,
ordering behavior, media delivery, original-client acceptance, cleanup of a live
fixture, or Goby compatibility result. The reference UI fixture and its receipts
are not consumed or changed by fake-transport verification.

The current producer population and release schema remain the eight-user,
ten-library, twelve-detail release-v2 development contract. They support only
synthetic integration in this bundle. Actual preparation05 is prohibited until
the fresh observer's ten-user/twelve-library/24-detail evidence and release-v3
integration are reviewed and available. Matrix request limits remain
300 total, 220 normal, and 80 cleanup; the producer's separate limits do not
increase them.

## Verification

The dedicated [guard entry](../../scripts/test-env/nextup-global-transport-guards.py)
requires a remote Linux SSH environment. It creates temporary synthetic receipt
trees and injects fake process observations, fake response bodies, clocks, and
persistence failures. It makes no HTTP request and does not inspect an original
client implementation, reference database, or live fixture. Local verification
is not allowed.

### Current playback-contract bundle

The frozen remote scope is
`/opt/goby-test/exec-work-m3e/nextup-playback-contract-tool-01/revision-01`.
The [matrix report](nextup-playback-contract-matrix-verification-01.json)
records 53 passing guards, the
[transport report](nextup-playback-contract-transport-verification-01.json)
records 100, the
[preparation report](nextup-playback-contract-preparation-verification-01.json)
records 161, and the
[operator report](nextup-playback-contract-operator-verification-01.json)
records 65. All 379 guards and eight remote compile checks passed. The shared
predicate also accepted the three pinned actual partial-state DTOs. The
[bundle report](nextup-playback-contract-verification-01.json), SHA-256
`3f79f264037d657fd4f2dbae0834caf73d049debc077b232a84684145bc85039`,
records zero actual business HTTP and process probes.

| Current source | SHA-256 |
| --- | --- |
| Transport | `4134c66a58a1542fc3c7dc9007bcd9ae289d094bb7db28a95d59a3557be4ceb8` |
| Matrix | `a69ff17c26934abbf09375a7832d7e11cd898d5e696c4ba5ce8a718efd19e65d` |
| Preparation | `206d989702a22c6bb4034e479567b6cb9d45d8dfe8b153576216de5419263574` |
| Operator | `e97667ac01541d2115d0df3d4062239895662d41140637e8030b410b1a05475a` |

These are synthetic contract and persistence checks plus retained-DTO checks,
not new live calibration, fixture release, matrix results, or client acceptance.
The historical report sections below retain their own source pins and results.

### Historical initial transport verification

On 2026-09-13, the initial frozen sources were copied through `ssh test-env` into the
new `nextup-global-transport-tool-01` scope under
`/opt/goby-test/exec-work-m3e`. That increment's `verification-02` run passed all
**68 guards**, with zero failures, errors, or skips. Two `py_compile` checks of
the new adapter and guard sources also passed. The unchanged planner was only
used as the fake runner's dependency; its previously published guard suite was
not repeated.

| Retained artifact | SHA-256 |
| --- | --- |
| Transport source | `e40c9b072354b186ae34f9dd424b6df835670269f0c2af3cf9bda00af8a7a2e3` |
| Guard source | `c5fc75681c3677faa2c81728cd61e5c4f70338bcb1ba61cc0bb254c608667b81` |
| Unchanged planner dependency | `da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259` |
| `verification-02/report.json` | `f8d56f4865c823ba283b65f998b2c3cf0457a57366474c024980e4195d7b3261` |
| `verification-02/guards.log` | `d68c4ee23fd3f8540890dfdf08ad5239ea727086bdeee88a92e4d245dc5a8674` |
| `verification-02/compile.log` (empty successful output) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

The first `verification-01` report and its original sources remain intact.
That run executed 67 guards with four failures and zero errors. The failures
identified one real adapter closure bug: after successful cleanup with a
retained observation failure, another `prepare_next()` call encountered the
planner's failure guard. The adapter now recognizes and verifies a closed
planner before asking for another request. A separate added guard verifies
ordered duplicate response headers and per-value cookie redaction in exports.

Both verification rounds used fake transport and filesystem fixtures only.
The final report records `actualBusinessHttpRequests: 0`,
`actualProcessProbes: 0`, and `liveAcceptanceClaim: false`. No local import,
syntax check, test, build, or runtime probe was performed. That historical
verification created no live preparation receipt. The command-line live entry
remains disabled.

### Historical version-two four-calibration integration verification

Version 2 retained beforeZero, beforeDelete, delete, and afterDelete for each
calibration: sixteen events and eighteen distinct response digests including
the two logins. Version 3 adds the acknowledged playback lifecycle events and
replaces that historical receipt shape.

The four-calibration increment used the entirely new
`/opt/goby-test/exec-work-m3e/nextup-global-transport-tool-02/verification-01`
scope. All **91 guards** passed on 2026-09-13, with zero failures, errors, or
skips. Two compile checks of the changed adapter and guard sources also passed.
The additional 23 calibration tests include missing actor/mode, reordered
events, wrong account/token/session/device, parallel authentication headers,
full DTO identity, zero-state restoration, absent/null preservation, numeric
type substitutions in both playback modes, unrelated UserData drift, reused
response records, and rejection of the historical single-proof shape before
runner dispatch. Positive controls preserve a bounded nonzero completion
position in accordance with the unchanged planner contract.

| Historical version-two artifact | SHA-256 |
| --- | --- |
| Transport source | `d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1` |
| Guard source | `7d227c23ca11e34e61e55576d0b4e51a876c6f6935c5091af9e0f9e57e2b3c18` |
| `tool-02/verification-01/report.json` | `599645acbb3d50edb43211eef12c06a8da280d54ad4ce5d8f57d1d4dc6e30ee6` |
| `tool-02/verification-01/guards.log` | `91de3b638166b0edb1c7df73830298cf8343a50447d92ba511d3ba048fefd6b3` |
| `tool-02/verification-01/compile.log` (empty successful output) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

No tool-01 source, report, or log was overwritten. That increment's planner
dependency remained at SHA-256
`da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259`.
The new report again records zero actual business HTTP requests and zero actual
process probes. This was adapter integration verification with fake transport
and private file fixtures, not a product test run, live calibration, or receipt
producer execution. The live CLI remains disabled.
