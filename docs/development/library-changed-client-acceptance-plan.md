# Source44 original-client automatic refresh acceptance

Status: the first candidate-only scope was executed and is now consumed.
It failed during Movies navigation before any native login or metadata write.
The original client raised a TypeError after a successful CollectionFolder
detail response. Normal UI logout, exact-token rejection, complete state
preservation and both process terminal checks passed. See the
[attempt report](m3e-library-changed-client-attempt1.json) and
[independent terminal](m3e-library-changed-client-attempt1-terminal.json).
This document retains that scope's design; it is not permission to replay it.

Any subsequent scope needs a new identity and an explicit lineage from
`client-library-changed-ui-source44-v1/after-full.json`, SHA-256
`1277a8034d738ed451cf99e71b88c53e31c9731576bca7fbae16085e573ce870`.
That state has 74 sessions, 63 devices and 165 audits. It preserves all old
business state and adds only this run's closed B session, device and two audits.
First obtain direct public CollectionFolder DTO evidence to diagnose the
navigation failure. Neither this failure nor passing pure guards establishes
automatic-refresh acceptance.

The required observation is one real native metadata commit, its unmodified
`LibraryChanged` message received by the existing original Web Client, a new
client-initiated HTTP read, and the corresponding visible card update without
navigation or reload. The smallest scope uses existing viewer B, one separate
native administrator session, the original Movies library list, and one existing
ordinary Movie. It creates no library, user, media file, scan, or playback session.

## Current authority and evidence boundary

Use the current head of [handoff.md](handoff.md), not historical deployment
claims farther down that file or in earlier notification documents.

| Binding | Required value |
| --- | --- |
| Candidate service | `goby-client-m3e.service`, source44, schema27 |
| Candidate process | PID `1264063`, start ticks `11104222`, boot ID `6bdfc486-7bc8-412f-82b5-70095a09dde7` |
| Candidate invocation | `c0a5244ae25646c8bd92c3e3c1636575` |
| Candidate binary SHA-256 | `cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d` |
| Candidate source manifest | `source-attempt-44`, SHA-256 `c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b` |
| Published commit | `35ae3d000f812fa18d234921cedb3c33d88190e0` |
| Fixture state | ready/complete; SHA-256 `d6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4` |
| Public client origin / direct candidate | `http://127.0.0.1:18196` / `http://127.0.0.1:18198` |
| Server ID | `c7cfd76b1dee728b2bad523793a37ccb` |
| Protected primary | `goby-foundation-test.service`, source32/schema27, PID `762090`, start ticks `7637121`, invocation `bb94d74b475f4382a6ec6f6df181dd74` |

The accepted [candidate continuation](m3e-source44-candidate-continuation.json)
and [terminal receipt](m3e-source44-candidate-continuation-terminal.json) prove
deployment, preservation, and two readiness GETs only. Its complete candidate
after-snapshot is
`/opt/goby-test/exec-work-m3e/client-notifications-source44-continuation-v1/after-full.json`,
SHA-256 `738eb9717506bec997456830e70d56f0867f9cf8159939ea3733e039cadb9811`.
Take a new complete before-snapshot and compare it with this authority before
authentication. Do not use the old source32 process or fixture-state hashes as
current pins.

The continuation preserved the preceding authority's 73 authentication rows,
62 devices, 163 audits, 26 play rows, seven UserData rows, four libraries, 22
items, and B management revision 5. These are preflight expectations, not a
replacement for complete row/sequence comparison. Any unexplained drift stops
this scope before login or a metadata write.

The [permission UI report](m3e-library-permission-ui.json) and
[browser report](m3e-library-permission-ui-browser.json) proved four/three/four
Home library cards only after explicit reloads. Both action-free ten-second
windows had no Views request. Policy writes do not produce this catalog event.
Do not replay that scope, use its tokens, or count its result as automatic refresh.
The [notification implementation record](library-change-notifications.md) and
existing public protocol transcripts explain message fields, but do not supply
new original-client evidence.

All compatibility actions and observations use public UI/API/network surfaces.
The existing owned Goby candidate's read-only full-state ledger is an integrity
guard, not a source of client behavior or a write mechanism. Never access the
reference instance or its database. Never inspect original Emby/client source,
bundles, handlers, offline asset contents, application globals, or stored client
state. Do not touch `scripts/test-env/upgrade-main-schema25.py`.

## Fixed actors, target, and page

* Browser: existing ordinary viewer B, ID
  `ecbbe4cb82403879bc4b4f78894c5738`, one fresh browser context and one UI login.
  Retain the exact existing Policy, configuration, preferences, UserData, and
  management revision. No second ordinary account is needed for this gate.
* Controller: one fresh native administrator authentication for the existing
  fixture administrator, ID `0dd576d477e8acea871cb4b06cb11153`. Resolve its
  existing credentials through the pinned fixture input. Never send native
  credentials or its cookie to the browser.
* Library: original `M3e Client Movies`, ID
  `a9993591e72f0f2e7babcbf8b9c50790`.
* Target: existing ordinary Movie `268051d3ca734aefcf94e245fb25ad55`, identified
  as `original_movie` in [the positive client record](m3e-positive-original-client.json).
  Bind its current public name, parent, root, physical path, type, and metadata
  revision from the new preflight. Do not guess a current name or revision.
* Page: the Movies library's normal item-list page with this Movie's title card
  visibly rendered. Remain on that page for both change windows. Do not enter
  Movie detail, which previously caused automatic PlaybackInfo and required a
  larger preparation/UserData allowance in other scopes.

Before arming, require the target to be an ordinary Movie in this exact library,
with no theme/extra ownership or resource role, no active scan, and no `Name`
lock. Preserve every existing override and lock. For the minimal ledger below,
require its effective entity-bearing collections to be empty and its existing
`item_entities` population to be empty. A name-only update still invokes
`sync_catalog_item_entities`; existing entities could otherwise consume identity
sequence values on conflict. If this prerequisite fails, stop before mutation
and prepare a separately reviewed sequence allowance; do not silently choose a
different target or broaden this run.

## UI discovery before any catalog mutation

Current evidence supplies these selectors in Goby's own harness:

* `client-browser-cross-user.mjs` uses the visible password form, the visible
  text/password inputs, and the exact `Sign In` button for normal UI login.
* `observeHomeDOM` in `client-browser-library-home.mjs` finds a library by exact
  visible title and its clickable `data-action="link"` ancestor inside `.card`
  or `.cardBox`, then reads the rendered card's `data-id`.
* Existing Home and signout controls have visible accessible-name evidence.

There is no current source44 receipt proving the Movies-list route, its target
card selector, or the automatic request shape. Do not invent a URL hash,
`data-id` location, or list-container selector from client implementation code.

The new driver's discovery stage must therefore:

1. Start network/WebSocket/page-error observers before opening the ordinary
   client entry page. Complete exactly one normal B login with frame/physical
   request agreement and independent new-session ownership proof.
2. Read the current visible Home controls and the original Movies card using
   the evidenced selector. Require exactly one correct-ID card before clicking
   it once. This is preparatory navigation, outside either observation window.
3. After the resulting page settles, record only bounded visible DOM facts:
   current same-origin route, visible heading/control names, target-card text,
   rendered identity attributes, card/container relationships, and inactive
   media state. Correlate the actual completed list response containing the
   target ID and current name. Limit DOM discovery to the rendered UI; do not
   read scripts, hidden application objects, browser storage, or asset bodies.
4. Publish `stage-discovery.json` containing the observed route, selector
   evidence, exact query allowlist, target response projection, and their hashes.
   The controller can arm only a unique card with the target ID. A title-only
   guess is insufficient for this mutation gate.

If the list does not expose a unique target card, requests PlaybackInfo, redirects
to detail, or needs an unevidenced control, stop with `prerequisite_unavailable`.
Preserve discovery and close the new session. Any revised driver then uses a new
scope and its predecessor's final state; discovery failure is not retried here.

## Real trigger and exact restoration

Use the implemented public native endpoint
`GET /admin/v1/items/268051d3ca734aefcf94e245fb25ad55/metadata` to retain revision
`R`, the complete sparse `Overrides` object, `LockedFields`, `LockedValues`,
automatic/effective values, and last-edit facts. Native login is
`POST /admin/v1/session`; mutation uses the returned cookie and `X-CSRF-Token`.

Reserve one forward PUT and one conditional restoration PUT before dispatching
either. Select a unique bounded plain-text name, for example the current name
plus ` [LC source44 v1]`; validate length, absence from the initial page, and
inequality with the current effective name. The exact chosen value and both
canonical request bodies belong to the private input and restoration journal.

The forward request replaces the sparse override layer, so copy the whole
original object and change only `Name`:

```json
{
  "Revision": "R",
  "Overrides": { "<all existing override fields>": "<unchanged>", "Name": "<reserved marker name>" },
  "LockedFields": ["<the exact existing lock set>"]
}
```

The placeholders describe construction, not a literal request payload. Dispatch
one `PUT` to the exact target metadata endpoint after the browser's armed-stage
receipt. Require a complete 200 response with revision `R+1`, unchanged identity
and controls except Name, and the marker in `Effective.Name`. A fresh native
GET independently confirms the committed public result. This real commit, not
a test publisher or a fabricated WebSocket frame, must produce the event.

Restoration sends revision `R+1`, the exact original `Overrides`, and the exact
original `LockedFields`. If Name was originally absent, remove it rather than
installing an override equal to the old displayed title. Confirm revision `R+2`
and equality of original effective metadata, overrides, locked values, and
locks. Last-edit actor/timestamps and revision legitimately retain this history;
restoration does not mean byte-identical metadata rows.

A missing or incomplete PUT acknowledgement is an unresolved write outcome.
Source44 has no complete request-duration bound: its owned transaction timeout
starts after acquiring the owner mutex. A later GET still showing revision R,
the original value and no new audit row does not prove that the earlier request
cannot commit afterward. Never classify that observation as not committed or
send a blind replacement PUT. Retain restoration_required until exact owned
revision and audit facts resolve the outcome. Revision R+1 with the expected
owned forward audit permits the single reserved conditional restoration;
revision R+2 with both exact owned audits can establish completed restoration.
Otherwise preserve the unresolved boundary and all evidence.

## Ordered execution and observation windows

Use a fresh root
`/opt/goby-test/exec-work-m3e/client-library-changed-ui-source44-v1`, a separate
fresh tool directory, and new controller/worker service names. Refuse existing
output paths or active owners. Pin every owned source dependency before launch.
All stage/control records use the existing exclusive pending-file, fsync,
non-overwriting publication pattern, with controller/worker process identity,
input hash, source-closure hash, previous-record hash, and one-shot phase name.
Do not inherit historical authority merely because a helper filename matches.

1. Perform the zero-HTTP source/process/fixture/ledger preflight; publish the new
   before-state, media/private-file inventory, actor/target binding, cleanup
   reservation, and exact permitted deltas. Launch the independent worker.
2. Complete browser discovery. Start the native administrator only after the
   B login and target-page receipt are proven. Read and reserve the metadata
   mutation/restoration. Take an authenticated-state checkpoint.
3. Drain relevant browser/proxy requests, then observe 20 seconds of quiet target
   UI with no actions. Require the original title, one continuously authenticated
   WebSocket, no relevant in-flight reads, and no LibraryChanged event. Unexpected
   catalog activity stops the run; do not wait and retry until it becomes quiet.
4. Browser publishes `stage-armed.json` before the controller dispatches the
   forward PUT. Start recording continuously at this barrier. The event may
   arrive before the PUT's HTTP response because publication follows commit;
   requiring it to follow the 200 response would lose a legitimate observation.
5. Observe for 120 seconds after the forward response completes, retaining any
   earlier event after the armed barrier. During this whole interval perform no
   clicks, keys, scrolls, reload, explicit navigation, focus changes, UI refresh,
   script-triggered request, application callback, or custom event dispatch.
   Read-only DOM samples every 500 ms are allowed. Publish the window result.
6. Independently re-read and classify the owned metadata state. Arm the same page
   for restoration, then execute the one reserved conditional restore. Observe
   another 120-second action-free window for the original title. If the forward
   gate failed, restore promptly as cleanup; do not manufacture a second success
   attempt from the restoration window.
7. Take the restored public/ledger checkpoint, then allow the normal UI signout
   sequence. Require its 204 and exact observed-token 401; log out the native
   administrator with `DELETE /admin/v1/session` and prove that exact cookie gets
   401 from `GET /admin/v1/session`. Close the browser, proxy, sockets, and worker.
   Publish final state and independent terminal/cgroup evidence.

Keep the same B token, document, route, and authenticated WebSocket throughout
both armed windows. A reconnect, full document navigation, history transition,
reload, service-worker update/restart intervention, or loss of observer coverage
invalidates automatic-refresh acceptance even if the final page is correct.

## The evidence chain that must pass

For both forward and normal restoration windows retain separate evidence for:

1. **Commit:** exact native request intent/result and independent public readback,
   target ID, metadata revision, and marker/original value.
2. **Delivery:** one actual original-client WebSocket receive observation with
   nonempty `MessageId`, `MessageType="LibraryChanged"`,
   `ItemsUpdated=[target]`, `IsEmpty=false`, and the other five known ID arrays
   empty. Require an unchanged-wire upstream/forwarded observation and matching
   browser receive projection, bound to B's handshake and token fingerprint.
   A separate observer socket or a proxy counter named `other` is insufficient.
3. **Automatic HTTP:** a new original-client request starting after that browser
   receive event on the same monotonic recorder. It must use B's same authority,
   match a target-item or currently scoped list query observed during discovery,
   and complete with a real candidate 200 body containing the target ID and
   expected name. Pair browser/worker initiation with physical forwarding and
   completion; disambiguate repeated identical URLs using exchange IDs and time,
   not just a request hash. Pre-event in-flight responses do not qualify.
4. **Visible result:** the same target-ID card changes to the expected title
   after that response completes, has no simultaneous old-title card, remains
   correct for at least two samples and the end of the window, and retains the
   same route with media inactive. Record the first matching sample and a final
   screenshot/DOM projection without credentials or unrelated personal content.

Service workers remain enabled as in prior accepted client runs. All worker and
page HTTP must traverse the bounded proxy. A worker-originated physical request
can qualify only when its observed browser-context/worker identity and B token
are unambiguously bound; a cache-only response cannot satisfy this specific
automatic-HTTP gate. Do not disable caching or force a revalidation to obtain a
pass. Preserve that limitation if the client updates through a different path.

Use a bounded passive message decoder in Goby's own transport/observer code.
Expose only the known envelope fields, six ID arrays, IsEmpty, sizes, hashes,
timings, and connection identity. Keep wire bytes unchanged. Do not monkeypatch
the client's WebSocket, fetch, history, listeners, or DOM. Existing proxy guards
must continue rejecting remote-control messages and unauthorized endpoints.

A name-only ordinary-item event is expected to name this target alone, as in
`TestHTTPLibraryNotifierPublishesOnlyCommittedMetadataAndClosesWithServer`.
An extra library event, unexpected target ID, or duplicate action window is
evidence to retain and classify, not permission to broaden the expected payload.

## Persistent-state allowance

The controller's read-only candidate ledger must compare complete rows,
sequences, schema/catalog inventory, credentials/recovery inputs, protected
primary state, and all three receipted media groups. It must never inspect a
reference database. UI/API observations remain the compatibility proof.

| Population or fields | Maximum successful-run change |
| --- | --- |
| Authentication rows | Exactly two new rows: one B `emby` session and one native administrator session; both revoked at completion |
| Devices | Exactly one new B browser device, new reported identity, revision 1; only its observed Touch timestamps may advance |
| Session audits | Exactly four: owned login/revoke pairs, with the correct native/emby source and credential IDs |
| Metadata audits | Exactly two `metadata.updated` rows for the target, revisions `R+1` and `R+2`, this administrator credential, Name and Overrides changed fields |
| `items` | No row-count change; target Name and any resulting effective SortName during the marker window, plus target `updated_at`; final descriptive values equal the original |
| `item_metadata_state` | Existing target row only: override/effective Name during the marker window, revision +2, last-edit actor/time and `updated_at`; automatic/source/music facts unchanged, original controls/effective metadata restored |
| Entity/resource relations | Exact row equality; no added/deleted catalog entities, item entities, theme/extra/image/subtitle rows under the empty-entity prerequisite |
| Session capabilities | Only the new B session, exactly a successfully observed bounded client request; native capabilities remain empty |
| Sequences | `devices_id_seq` advances for exactly one inserted device and `activity_entries_id_seq` for exactly six audits, respecting the previous `is_called`; every other sequence identical |
| Users/Policy/preferences/configuration/UserData | Exact equality, including B revision 5 and all existing UserData rows |
| Playback/references/encodings/scans/libraries/roots | Exact equality and no new rows |
| Services/binary/runtime/fixture/recovery/media | No changes; no restart, deployment, restore, or fixture-state rewrite |

With the confirmed 73/62/163 baseline, the successful final counts are
75 sessions, 63 devices, and 169 audits; play/UserData/library/item counts remain
26/7/4/22. Derive row identities and timestamps from the fresh run, not those
totals alone. Native LastSeen and browser presence changes may affect only the
two new sessions and the one new device. Do not permit modifications to old
session/device rows or removal of historical audit rows.

## Restoration and stop boundaries

The controller must reserve recovery before the first PUT and keep it usable
after browser abort or journal failure. Do not retry login, PUT, scan, navigation,
or an entire consumed scope automatically.

If a PUT acknowledgement is missing, allow at most two bounded public native
readbacks and corresponding owned candidate ledger checks:

* Revision `R` with original controls and no owned update audit means the forward
  change was not applied; restoration is unnecessary.
* Revision `R+1`, the reserved marker/controls, unchanged automatic facts, and
  this new administrator credential's matching metadata audit authorize exactly
  one restoration PUT.
* Revision `R+2` with the exact original controls/effective values and both owned
  audits proves an already completed restoration; never send it again.
* Any foreign revision, changed target identity/source/locks, missing ownership
  proof, lost candidate process identity, or conflicting row fails closed.
  Retain `restoration_required` and exact evidence; do not overwrite another
  actor's work or issue SQL repairs.

The normal browser logout/exact401 barrier is required for acceptance. A
separately reserved fallback may revoke only this run's independently proven new
B token, then check exact401. Using that fallback makes acceptance false but
does not prevent safe cleanup. Never revoke old sessions, create replacement
users, change Policy, restore a database, restart either service, or erase a
failed evidence tree to make cleanup appear successful.

Stop immediately for failed process/source pins, unexpected writes or playback,
second login, scope drift, external network escape, ambiguous request pairing,
observer overflow, page errors, or unsafe IPC ownership. Restore the owned edit
only while its stated conditions remain true, then perform bounded owned-session
cleanup and retain the failure. A timeout is `not_observed_within_window`, not
proof that the client can never refresh.

## New driver and guard work

Create these in a fresh frozen source closure before remote verification:

* `scripts/test-env/client-library-changed-source44-fixture.mjs`: validate the
  new source44 continuation, current after-state, existing fixture lineage and
  unchanged origin/server/actors. Do not weaken the source32-only validators in
  consumed Home/permission scopes or monkeypatch their constants.
* `scripts/test-env/client-browser-library-changed.mjs`: discovery, one normal B
  login, phase records, passive WebSocket/HTTP/DOM correlation, action lockout,
  exact UI logout and bounded diagnostics. Reuse pure `observeHomeDOM` and
  existing session-proof/transport helpers where they have no stale authority.
* `scripts/test-env/observe-client-library-changed.py`: fresh ownership/pin gate,
  complete before/authenticated/restored/after ledgers, native metadata requests,
  one-shot restoration, independent worker lifetime, and public redacted report.
  It must not import an old controller as executable authority.
* Add narrowly opt-in observer callbacks to Goby's own shared transport only if
  required to capture complete decoded LibraryChanged messages and physical
  reads. Preserve old default scope limits and unchanged bytes. Existing
  `inspectWebSocketFrames` currently groups these messages as `other`, which
  cannot prove this new gate by itself.
* `scripts/test-env/test-client-browser-library-changed.mjs` and
  `scripts/test-env/test-observe-client-library-changed.py`: pure guard cases for
  wrong source/process/state, old authority, ambiguous selectors, duplicate
  credentials/actions, message fragmentation/limits, frame/physical mismatch,
  pre-event reads, worker cache-only responses, implicit/explicit navigation,
  fake DOM-only success, late events, cancellation, missing acknowledgements,
  stale restoration revision, foreign audits, and exact permitted row/sequence
  deltas. Add shared-core regressions if its opt-in callbacks change.

Suggested fixed limits: 390 seconds of browser work; 90 seconds of cleanup;
600-second worker runtime and 650-second controller terminal bound. Cap each
observation window at 120 seconds, DOM sampling at 600 total samples, relevant
catalog requests at 20 per window, native exchanges at 12, and metadata PUTs at
two total. Retain existing proxy caps of 2,000 requests, eight concurrent
requests, 128 MiB total response bytes, 2 MiB inspected JSON bodies, 64 KiB
WebSocket messages, one active WebSocket, and at most two bootstrap/cleanup
handshakes; allow no handshake change while armed. The new scope needs a bounded
WebSocket lifetime long enough for both windows, up to 480 seconds, without
altering old scope limits. IPC records remain at most 512 KiB; the separate final
browser report is bounded at 4 MiB. Exported diagnostics are bounded/redacted.
All loops must actually stop at their deadlines.

Only after the new remote pure guards, syntax checks, source-closure inspection,
and zero-HTTP candidate preflight pass should the root task dispatch the single
live run. This document does not execute or certify those prerequisites.

Final outputs include pinned input/source closure, discovery, login ownership,
armed stages, native intents/results, both event/request/DOM timelines, public
metadata restoration, complete private integrity checkpoints, sanitized browser
diagnostics, logout proofs, and independent worker/controller terminal records.
Keep private tokens/cookies/passwords out of exported logs, HARs, traces,
screenshots, and raw payload archives.

Set `library_changed_client_acceptance=true` only when both automatic chains,
semantic restoration, exact persistent-state allowance, and all cleanup gates
pass. Keep full M3/M4/M5/M6 and general client compatibility explicitly open.
Otherwise report the narrow reached boundary: trigger, WebSocket delivery,
automatic HTTP, visible DOM, restoration, or cleanup, with no reload-based
substitute for a missing link.
