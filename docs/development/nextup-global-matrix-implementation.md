# Global NextUp matrix implementation

Status: **33 remote guards and two compile checks passed; no live API execution**. This is the
pure planning increment of the [acceptance plan](nextup-global-acceptance-plan.md).
It creates no account, library, media, login, HTTP request, or acceptance result.
The current LibraryChanged fixture remains outside this increment.

The source is [nextup-global-matrix.py](../../scripts/test-env/nextup-global-matrix.py).
The remote-only, standard-library guard entry is
[nextup-global-matrix-guards.py](../../scripts/test-env/nextup-global-matrix-guards.py).
All **33 guard cases and two compile checks passed** on `test-env`, with zero
failures, errors, or skips. Synthetic guard responses are planner test inputs
and are never public reference evidence.

## Scope and frozen branches

`Matrix(manifest)` validates a new preparation-owned semantic map, constructs the
request templates, and exposes `frozen_plan` and `plan_sha256`. Construction has
no transport, clock, filesystem, or process side effects. `prepare_next` and
`authorize` reject a changed manifest, changed frozen plan, or reordered request
queue. Preserve the manifest and exact template document before dispatch.

| Branch | HTTP requests | Ordered contents |
| --- | ---: | --- |
| PRE | 18 | For P, then Q: one login, full profile, preferences, and six complete series/season details. |
| R0 | 18 | Core P, then core Q; each core is six full episode details, global NextUp, SeriesId A, SeriesId B. |
| R1 | 17 | P completes A1; core P; global Q and Q A1 detail. |
| R2 | 17 | P completes B1; core P; global Q and Q B1 detail. |
| R3 | 16 | P completes A2; core P; global Q. |
| R4 | 30 | Q completes B1, then A1; core Q, then core P. |
| First positive extension | 14 | Five pagination variants, four ParentId variants, two combined scopes, two projection variants, and the other actor's global read. |
| R5, positive only | 39 | Six current P details; DELETE P A1/A2/B1; six zero proofs and six summary proofs; only A2 at 120 seconds; core P; global Q and Q A1/B1 details. |
| R6, positive only | 24 | Only P B1 at 120 seconds; core P, then core Q. |
| Maximum enumerated cleanup | 49 | Seven possible owned stop routes, five reconciliation details, five episode DELETE routes, twelve episode proofs, twelve summary proofs, four profile/preferences reads, and four logout/rejection requests. |

R0-R4 total **116** requests. A positive branch has at most **193** normal
requests and **242** enumerated requests including the maximum cleanup set.
The target limit remains **300**, with **80** reserved for cleanup. Normal
dispatch stops at request 220. Unused capacity authorizes no extra route, retry,
mutation, flag hypothesis, or timestamp adjustment.

Every lifecycle consists of a full target detail, PlaybackInfo, Started,
Progress, Stopped, and another full target detail. The six normal core details
also verify all untouched episodes against their last independently observed
full UserData. R4's P core must equal P's retained R3 state. All twelve initial
episode details must have Played false, count zero, position zero, and an absent
or null playback date before any negotiation is authorized. Complete UserData
values, field presence, and primitive types are preserved.

The first nonempty owned global response inserts the 14-request extension
immediately before the next queued request. No later mutation can precede that
extension. Global item identity/order and total are retained as observations;
there is no expected first candidate, one-per-series assumption, or ordering
assertion. The existing SeriesId control sequences are exact assertions.
Timestamp comparisons use persisted full LastPlayedDate values. A tie or
non-increasing date produces an inconclusive ranking fact and no retry.

If R0-R4 remains globally empty, the queue closes at
`reference_global_positive_unresolved`. It cannot enter EXT, R5, or R6.
`client_discovery_plan()` returns the separate original-client allowance of at
most two playback attempts and 1,200 seconds. A client discovery receipt can be
attached only after API cleanup and both exact token rejection proofs. A later
client positive result requires a separate frozen public control; it does not
reopen this completed recorder or retroactively authorize R5/R6.

## Inputs still required from preparation

The manifest is private. Supply actual values from the newly completed
preparation gate, rather than copying the synthetic guard map or historical
process/account IDs. `fixture_manifest()` in the guard is an executable schema
example only and must never be used for a live run.

| Input | Required binding |
| --- | --- |
| Run | `schemaVersion: 1`, distinct `runId`, `target`, `serverId`, and SHA-256 digests of the preparation, coordination, and catalog receipts. |
| Process | Current `processIdentity`, `version`, `endpoint`, `isolationIdentity`, and fresh `evidenceRoot`; positive fixture-release and unused-root attestations. The real operator must recheck these against the process and existing lock before each dispatch. |
| Goby-only process | Current source manifest digest, executable digest, schema version, and latest fixture observation digest. A historical verification receipt is insufficient. |
| Actors | Exactly P and Q, each with independent `userId`, `username`, `deviceId`, and private `credentialRef`; a new ordinary-account attestation, policy receipt digest, and exactly two `allowedFolderIds`. No password or token belongs in this manifest. |
| Libraries | Exactly LA and LB, with separate raw `libraryId`, `viewId`, `sourceRootId`, `policyFolderId`, and `scopeItemId`. This control explicitly freezes `scopeItemId` to the verified view ID. Record management/view/root distinctions instead of substituting IDs. |
| Catalog | A/B series; AS1/AS2/BS1/BS2 seasons; A1/A2/A3/B1/B2/B3 episodes. Preserve IDs, types, library affiliation, parent/series relations, episode/season numbering, stable distinct series names, runtime, 30 fps, and immutable media digests. |
| Cleanup | `mode: episode-delete-played-items`, `zeroBaselineRequired: true`, `verifiedForBoundTarget: true`, and the digest of the reviewed cleanup contract for the bound target. |
| Timing | `lifecycleSeparationSeconds` of 2, 3, 4, or 5; the transport supplies elapsed monotonic time and honors `WaitRequired`. Persisted dates, rather than this delay, determine whether ranking evidence is usable. |

The preparation phase must establish exactly one study series in each library
and its actual public catalog relations. The pure matrix does not scan or create
that catalog. The library `sourceRootId` is an item identity; native root paths
and hashes remain in the referenced private media-root receipt. The current
ordinary profile is rechecked for `EnableAllFolders: false` and exact
`EnabledFolders` before playback.

The retained cleanup evidence supports individual episode
`DELETE /emby/Users/{actor}/PlayedItems/{episode}` followed by full item details
for measured zero-history episodes. The module never emits whole-series reset
or UserData POST. Historical `LastPlayedDate:null` did not clear the date, so it
is not an alternative restore body. If preparation cannot bind a sufficient
cleanup contract to the fresh target, construction must remain blocked.

## Recorder integration

The current owned
[fresh recorder](../../scripts/test-env/reference-nextup-fresh.py) has
`request(label, method, route, body=None, login=False)`, a process identity check,
bounded HTTP decoding, private intent/response journals, and a sanitizer.
`Request.recorder_arguments()` returns that same positional and keyword shape.
The root operator can select the dedicated recorder instance by `request.actor`
and directly consume each planned request. The historical recorder itself has
a single-user, three-episode, fixed-PID route guard and must not be run or reused
as the new execution driver without a new manifest-bound implementation.

Use this dispatch order in that new driver:

1. Recheck the existing fixture lock, current process/source binding, fresh
   evidence root, exact actor credential ownership, request limit, and transport
   timeout/body limit.
2. Call `prepare_next(elapsed_seconds)`. If it returns `WaitRequired`, wait its
   bounded duration remotely and ask again. A wait is not an HTTP request.
3. Persist and fsync the exact request intent, including the source/plan digest,
   next ordinal, actual token fingerprint, and current private matrix state.
   Login credential bytes belong only in the recorder's private credential
   storage and encoded login payload.
4. Call `authorize(request, intent_sha256, elapsed_seconds,
   actor_token_sha256=actual_token_sha256)`. Then persist the returned reserved
   ordinal and `private_state()` before making the one HTTP attempt. The token
   fingerprint must equal the actor's acknowledged login, including for logout
   rejection. Login has no token fingerprint yet.
5. Dispatch with the exact request through the selected recorder, then retain
   its raw private response and sanitized export independently. Call
   `accept(status, decoded_body, response_timestamp, response_receipt_sha256,
   elapsed_seconds)` only with those actual response facts, and persist state
   again. The login consumer verifies the server, ordinary user, device,
   SessionInfo user ID, and distinct P/Q token/session bindings.
6. On a lost response, persist `lost_response(reason)` and its pending intent.
   There is no generic retry or resume API. Unknown token/session ownership or
   uncertain mutation effects need a separately retained recovery manifest.

Playback request bodies use the previously captured public shapes. Negotiation
supplies only UserId and IsPlayback. Reports bind ItemId, MediaSourceId,
PlaySessionId, and the actor's SessionId from actual acknowledgements. Started
uses zero position, runtime, CanSeek, pause/mute state, DirectStream, and rate;
Progress adds the requested position and TimeUpdate; Stopped supplies position,
Failed false, and IsAutomated false. Only the subsequent full detail establishes
the persisted played/count/date/position facts. These protocol reports never
claim media delivery or actual client playback.

`begin_cleanup()` selects a subset of the pre-enumerated cleanup routes. It
stops known pending lifecycles first, freshly reconciles every touched episode,
and only deletes histories that still differ from the measured baseline.
An acknowledged stop can advance the four playback history fields before the
next full detail. That detail reconciles the owned stop before selecting or
omitting its already enumerated DELETE; a stale pre-stop observation cannot
remove the reset. Unrelated UserData changes remain rejected, and absent versus
null playback dates remain distinct in the final baseline proof.
Reconciliation cannot silently accept unexplained state changes. Complete
episode details, series/season summaries, profile, preferences, and each exact
token rejection are required. An observation failure remains an explicit
failure outcome even when cleanup of known responsibilities finishes. Cleanup
is single-use; a failed cleanup requires a separately retained recovery manifest
and cannot restart the same route sequence.

`private_state()` can contain session/source IDs and complete profiles. Pass
its export through the owned sanitizer; do not copy it directly into repository
receipts. Retain the catalog/media/accounts and declared protocol audit history
according to the preparation manifest. This module has no automatic deletion
or claim of whole-database equality.

## Remote verification and pending evidence

The root task transferred the two frozen Python sources to the remote
`nextup-global-matrix-tool-02` directory and ran the guard suite and compile
checks through `ssh test-env`. The
[verification receipt](nextup-global-matrix-verification-02.json) records
33 guards and two compile checks passed, with zero failures, errors, or skips.
Its remote report SHA-256 is
`37a785c7b1f67852330fd1ed03d4323b5279aa242af06ed4e100bfbc68163696`.

The [earlier 30-case receipt](nextup-global-matrix-verification-01.json) remains
retained. Independent review then found that cleanup selected resets before
its own stop could change durable history. Three added cases cover a cleanup
stop after a rejected progress report, a normal stop followed by a failed detail
read, and rejection of an unrelated favorite change during reconciliation.

The verified matrix source SHA-256 is
`da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259`;
the guard source SHA-256 is
`20aae9d8bc6ebd66cf22f492a2b38f6a13d9f497f8142d72fa9abc054a21a250`.
No import, syntax check, build, test, or runtime probe was executed locally.
This verification made no actual HTTP request and establishes neither a public
reference result nor original-client acceptance. The source has no direct
execution entry point that could accidentally perform HTTP.

The remaining real dependencies are the fresh two-library preparation receipt,
full target/process/fixture bindings, a verified zero-history cleanup contract,
a new two-actor recorder transport with the existing identity/sanitizer/journal
controls, and actual reference responses. Global positive selection, ordering,
pagination/count semantics, ParentId behavior, EnableResumable,
EnableRewatching, and original-client behavior remain unestablished. The matrix
and its guards alone resolve none of those compatibility questions.
