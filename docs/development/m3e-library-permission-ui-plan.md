# Original-client library permission changes

Status: **the declared candidate Home permission-change UI gate passed after explicit
reloads in one actual run.** The [verification record](verification-m3e-library-permission-ui.md)
reports `permission_ui_acceptance=true`, `client_acceptance=false` and outcome
`permission_observation_after_explicit_reload`. One new B token spanned baseline,
restriction and restoration. Both ten-second no-action windows had zero fresh
frame/physical Views requests and retained the previous visible state. Both are
`not_observed_within_window`, not proof that automatic refresh never happens.
One explicit reload per changed phase produced a fresh complete frame/physical
Views200 pair and the correct three/four ID-scoped library cards.

Nine native HTTP exchanges completed the two B updates and exact restoration
of its existing eight-key raw Policy, revision 3 to 5. UI and administrator
logout204/exact-token401, three WebSocket closures and complete owned cleanup
passed with no errors or fallback. Three blocked external requests and three
console warnings are retained as totals. The admitted delta was two auth rows,
one device and six audits; other old rows, expected sequences, private state,
media, 26 play rows and seven UserData rows were preserved.

Latest authority is
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd`:
73 global auth rows, 62 devices, 163 audits, 63 selected A/B auth rows, four
libraries and 22 items; B is revision 5. The unchanged coordination contract
below records the requirements followed by this run. Continue remaining event,
subtitle and client gaps; automatic notification behavior and complete
M3/M4/M5/M6 acceptance remain open.

The passed Home v3 prerequisite remains a separate baseline-only observation. The
[v3 report](m3e-library-home-v3.json) remains explicitly
`baseline_observation`, with `client_acceptance=false` and
`permission_ui_acceptance=false`.

One login response with HTTP 200 led to four correct-ID Home cards and one complete
frame/physical Views200 pair without service-worker delivery. Each library had two global title
matches but one visible card for its correct ID. One explicit `page.reload()`
completed document200, then a new complete frame/physical Views200 pair returned
the same four libraries under the same B token. There was no Movie navigation,
PlaybackInfo, media playback, Policy write or UserData write.

UI logout204/same-token401 and full browser/context/proxy closure passed, with
two WebSockets opened/closed, zero pending work, no page/cleanup errors and no fallback.
Two external requests were blocked and two console warnings were retained.
Controller/worker exited normally with code 0. Final stdout evidence matches
the actual completed log.

The verified delta is one new B session, one device and two audits, with old
rows, expected sequence state, users/Policy, private state, media, 26 play rows
and seven UserData rows preserved. Counts at that prerequisite were 71 global auth rows,
61 devices, 157 audits and 62 selected A/B auth rows, four libraries and 22 items;
B was revision 3. Its historical complete after snapshot is
`8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a`.
JS04 passed seven remote syntax checks, 35 Home/114 cross-user guards; Python03
passed two syntax checks and 33 guards, and check-only passed with zero HTTP.
Exact report/source/terminal pins are in [Home verification](verification-m3e-library-home.md).

The permission run subsequently consumed the v3 authority and followed the
coordination contract with a new B token across all phases. Neither the revoked
v3 token nor v1/v2 was reused. Continue future work from the latest permission-run state;
the explicit-reload pass does not prove automatic permission-change refresh.

Historical v2: **it failed the global-title Home predicate, while login, UI logout,
cleanup and the complete expected state delta passed. No reload ran.** The
[v2 report](m3e-library-home-v2-failed.json) retains the failure. Its initial
Views200 completed with four libraries; each library had two global title
matches but exactly one visible card with the correct `data-id`. Requiring
global title uniqueness was too strict when a card ID was already available.
The repaired login proof succeeded, the WebSocket opened/closed once and UI
logout204/exact401 completed with no page errors, cleanup failures or fallback.
V2 needs no native-session recovery.

V2 admitted one new B session, one device and two audits. Old-row and sequence
checks passed, with Policy, 26 play rows, seven UserData rows, private state and media preserved.
Its historical complete after snapshot is
`27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26`,
with 70 global auth rows, 60 devices, 155 audits and 61 selected A/B auth rows;
B remains revision 3. The worker exited normally with code 1 and an empty cgroup.
The report's `node.stdout` descriptor was captured at empty-file creation;
its actual final digest is recorded separately in
[Home verification](verification-m3e-library-home.md). Preserve the old report
and use terminal evidence, not that descriptor, to prove exit.

Node tool03 passed seven remote syntax checks, 34 Home guards and 114 cross-user
guards; Python tool02 passed two syntax checks and 29 guards. The later v3
implementation used the ID-scoped predicate, retained the unique-title fallback
only when no ID exists, and ran with a new root/unit and terminal log digests.
The newer JS04/Python03 gates and actual v3 pass are recorded above. V1/v2 remain
failed; only the preparatory Home/reload gate has passed, not permission-change UI.

The earlier v1 [recovery report](m3e-library-home-session-recovery.json)
records one successful attempt with four complete HTTP exchanges: new native
administrator login 200, revocation 200 of only B session
`00fb0b833884308ab946e1c40ff6abdd`, administrator logout 204 and 401 for that same administrator token.
B's token was lost, so no B exact-token 401 result is claimed. Its persisted
session changed only `revoked_at`, to `2026-09-11T21:05:29.180099+00:00`.

The historical recovered after-snapshot SHA-256 was
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0`.
Counts at that recovered checkpoint were 69 auth rows, 59 devices, 153 audits, 26 play rows, seven
UserData rows, four libraries and 22 items; B remains revision 3. Recovery added
one native administrator session and three native audit entries, with no
device, play or UserData change. The failed evidence tree and complete media
inventory were preserved. V2 subsequently consumed this recovered authority.
V3 and the later permission run used new output roots/units; both old failed
runs stay failed. Current authority is the permission run's 12278 after snapshot.

The [Home verification record](verification-m3e-library-home.md) separates the
actual successful login/Views responses from the incomplete observer, DOM and
cleanup gates. Tool01 observed one complete login200 and a frame-owned,
physically forwarded Views200 containing exactly four libraries, then failed
`home_login_proof_incomplete`. No DOM result or reload completed.

The Home observer matched the login path case-sensitively and missed the actual
`/emby/Users/authenticatebyname` request already recorded by the core harness.
The resulting unproven-login gate blocked UI logout, producing 502; the exact
token check returned 200. No private proven-login receipt was available for
ordinary cleanup. The later independent native-administrator recovery proved
the exact persisted revocation described above; it did not prove B-token401
or turn the failed UI run into a pass. The uncancelled `Promise.race` polling also kept Node alive, leaving
the controller's 360-second worker limit to bound process cleanup. Process termination
does not establish authentication cleanup.

The actual controller has now terminated with exit code 1 and
`unacknowledged_state_delta`. RuntimeMax terminated the worker; its unit has
`MainPID=0` and an empty cgroup. The complete after snapshot contains one new B
session, one device and one login audit: 68 auth rows, 59 devices and 150 audits.
B remains revision 3; 26 play rows, seven UserData rows and all media are unchanged.
Worker process cleanup was proved separately; exact native session recovery
subsequently passed with its own receipt.

The minimal Node correction accepts the observed route casing and bounds and
cancels waits. Four new guards extend the Home selection from 27 to 31;
[tool02 verification](m3e-library-home-js02-verification.json) passed seven
remote syntax checks, 31 Home guards and 114 cross-user guards. This is corrected
tool evidence, not a successful Home/reload rerun. Preserve tool01, its before/
after/failed trees and original reports. The independent recovery tool passed
[two remote syntax checks and 12 pure guards](m3e-library-home-session-recovery-verification.json),
zero-HTTP check-only and its actual four-exchange recovery. The later corrected
v2 run is recorded above and remains UI-failed with complete cleanup. It has
superseded the recovered snapshot at that stage. V3 and then the permission run
established later after snapshots; 12278 is current. Do not replay either failed tree or treat the old tool02 checks
as the later JS04/Python03 and actual v3 evidence.

The [candidate API gate](verification-m3e-library-restriction.md) passed, including
exact restoration and independent persisted-state inspection. It was a
prerequisite for the later Home and permission UI runs. Further work must bind
the latest permission-run after state without replaying an earlier workflow.

## Known behavior and unresolved questions

Folder-only native user updates commit Policy and a `user.updated` audit fact.
They do not publish a library/user refresh event or invalidate client caches.
Every subsequent authorized HTTP request resolves current user Policy. The
WebSocket revalidation loop also reloads current authority, but it does not
send that Policy to the client. Existing UserDataChanged delivery filters
items by the current library visibility. These facts come from
[native updates](../../internal/server/admin_users.go),
[managed-user transactions](../../internal/identity/managed_users.go), and
[WebSocket handling](../../internal/server/websocket.go).

The retained [positive-client report](m3e-positive-original-client.json),
SHA-256 `b061a2ae791b77c5db30959ec70d7bff710f13d92fe1bddf4eba901b3ebbcca4`,
contains one actual `/emby/Users/{actor}/Views` GET for each of A and B. Both
were observed during `login_form_closed`, returned 200, finished without
failure, and were not served by a service worker. Each also has one completed
physical-proxy200 observation. Matching the report's path digest to the known
actor-specific route establishes that inventory request; the old report does
not retain its item-membership body. New Views observers therefore must be
installed before the initial page is created, not after `open()` returns.

The backend alone cannot establish whether the client refreshes a Home view,
reuses cached library data or issues PlaybackInfo during another navigation.
Both GET and POST PlaybackInfo may create preparation state. Keep both blocked
in the new Home-only scope and retain any attempted request as an observation
failure. Do not inspect or decompile the original Emby/client implementation.

## New Home and reload observation

Use one fresh browser context for B, preserving service-worker support and
the existing fixed-target forwarding guard. No saved browser storage is loaded.
Perform one original UI login, observe Home and four library cards, perform at
most one explicit normal browser reload, then sign out through the UI. No
administrator login, Policy write, Movie-detail navigation, playback, scan,
preference write or UserData write belongs to this preparatory scope.

For both initial Home and the reload:

- Capture actual frame-owned Views requests, current actor/token fingerprint,
  complete200 JSON responses, bounded Id/Name membership and browser request
  completion. Keep service-worker provenance explicit.
- Bind those responses to the physical forwarding request and successful
  downstream completion. Passive observation must not change entity bytes.
- Require the exact four current library IDs and names, and corresponding
  visible library cards. With a DOM ID, require exactly one visible card for
  that correct ID; other same-title page text does not invalidate it. Without
  an ID, require a unique title match and record that weaker evidence without
  claiming an observed DOM identifier.
- Record visible navigation controls and inactive media. Preserve page errors
  and console warnings with the established redaction and bounds.
- Require the same B token after reload and no second login. A reload is an
  explicit action, not evidence of an automatic update notification. If Views
  is not observed, retain that result rather than generating an API request
  and labeling it as client traffic.

Login observation must accept the actual route casing while retaining the
fixed origin, method, actor and physical-forwarding bindings. Every polling
wait must have a finite deadline and cancel its outstanding poll/timer work on
success or failure; winning a `Promise.race` does not cancel its losing branch.

UI logout204 must be followed by rejection of that exact token with401 and
closure of owned WebSocket/browser resources. A private, proven login receipt
may support exact API cleanup after UI failure; such cleanup does not promote
a failed UI observation to passed. It cannot authorize bulk revocation or
cleanup of a pre-existing credential.

If that private proven-login receipt is missing, do not claim ordinary cleanup
or silently widen its authority. A separate native-administrator recovery must
bind the exact newly created B session and retained failure/database evidence.
Its own authentication, revocation, audits and final persisted-state proof are
separate from the failed browser run and cannot turn that run into a pass.

The controller must take a fresh complete database/media baseline under the
existing fixture lock and compare all old state afterward. Only one new B
authentication row, one new device and two session audit facts are expected.
Capabilities/Touch changes are confined to that new session/device. Existing
users, Policy, play rows, UserData, references, encoding state, catalog and
unrelated sequences must remain exact. This Home/reload observation does not
complete the permission-change UI gate.

## Historical authority and latest permission-run baseline

The last closed authority before the failed Home run was source32/schema27,
PID748513/start ticks6996875, binary
SHA-256 `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
Its recorded fixture-state SHA-256 was
`5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1`.
B was revision 3 with the six supported Policy defaults and two role flags.
That pre-attempt complete state had 67 global sessions, 58 devices, 149 audits,
26 play rows, seven UserData rows, four libraries and 22 items.

Bind the successful API report
`193a5cc5ddaa806575d630a03de1268abed2418347b4e57eb5cc57610a04e8eb`,
its after-snapshot
`a09e42a404b9aa0251e2341e7ffa85c93528b7b141d81e11df6791802d6e92fd`,
the independent inspection
`e3dcbc0c21edbc484baab89762e11857a7cbc46889b78701af8dd309f12c3441`
and its current-snapshot
`1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3`.
The historical positive-client after-snapshot and raw Policy `{}` are not the
baseline of that API checkpoint. The failed Home attempt's actual before/after
snapshots have SHA-256
`8534805055cdb18ba1585c7637879ddea9b4d4b5cbd38e6b21ca324b7b43a948` and
`6d00f9cd99313762702c212ffdcc612cf88a426da67cfbcb0d1b403e8ef2d4d8`.
The latter remains the historical 68-auth/59-device/150-audit failure observation,
not a cleanup success. The independent recovery bound that state and completed
using recovery before/authenticated/after snapshots
`829204605a64614aa388022b5cda32e539cf769899ea87a755fa1c1d3d91e511`,
`f3a03707804521ffa57935a73d115bf35f6d54304e491eec67cf0159936da33c` and
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0`.
The last value was the recovered 69-auth/59-device/153-audit authority consumed
by v2. V2's fresh before snapshot is
`e2223db45db0d5bd4ae52833ad639d436f0122f334f08f21115db720bae3ec5f`;
its preserved after snapshot is
`27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26`.
The latter was the 70-auth/60-device/155-audit input consumed by v3. V3's actual
before snapshot is
`84738aa74749a017e7394498d40e58a330df031203d269cc599d5adeae514fb7`;
its latest verified after snapshot is
`8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a`.
That 71-auth/61-device/157-audit state was consumed by the completed permission
run. Its before/authenticated/after snapshot SHA-256 values are
`f81a699d20893edf05326825638bc9b312e8f17d5aaef106fa547667b29f2d13`,
`add58cebc4289e3c957548e4e9eff76cec7a124b3c3ce7e08cec294a5f2a5664` and
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd`.
Only the last is current: 73 auth rows, 62 devices, 163 audits and B revision 5.
Use that latest state for future independently scoped work.
Neither the failed root nor the old `1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3` API authority may be
replayed. Private snapshots and credentials remain on test-env.

## Subsequent permission-change gate

The [completed run](verification-m3e-library-permission-ui.md) supplied the
post-reload observations required here. These requirements describe that
declared scope, not a request to repeat the finished operation.

Use the actual Home/reload observations to freeze the next workflow. Keep one
B UI token across baseline, restriction and restoration; a separate owned
administrator session performs only the original Movies restriction and
conditional full restoration with fresh revisions and owned audit proof.
Observe whether a new Views response and matching DOM update happen without
action. Any explicit refresh must be recorded separately. After each successful
write, bind the actual post-write frame request, token fingerprint, response
membership, completion and visible state. Absence of a request is not success.

Restore Policy before closing the ordinary browser, including on observation
failure. Verify the new complete database difference and exact owned cleanup.
Neither the API gate nor the preparatory Home observation establishes full M3,
the original client's automatic refresh behavior or complete M4/M5/M6 work.

## Permission-run coordination contract

After the corrected Home observation supplies real DOM and reload evidence,
the next independent permission run must keep one new B browser session open
through baseline, restriction and restoration. One separately owned native
administrator session performs the two account updates. The controller and
browser exchange bounded, exclusively created stage records tied to their
input digest and live process identities; a file's existence alone cannot
authorize an update or a browser action.

| Stage | Controller condition | Original-client observation |
| --- | --- | --- |
| Baseline | New B login and complete four-library Home proof | Four library cards and a completed frame/physical Views response |
| Restriction | Fresh B revision, exact original Movies restriction, acknowledged owned update | Observe up to 10 seconds without an action; then one explicit browser reload and a fresh Views response with exactly the three retained libraries |
| Restoration | Fresh revision and restoration of all supported original account/Policy values | Observe up to 10 seconds without an action; then one explicit browser reload and a fresh Views response with the original four libraries |
| Closure | Restoration proved, including after observation failure | UI logout, exact-token rejection and complete owned browser/WebSocket/process closure |

The ten-second intervals are bounded observations, not a server timing contract.
Record an absent request as `not_observed_within_window`; it cannot prove that
the client never refreshes automatically. Preserve the visible state and any
actual request during each interval, separately from the explicit reload.
Fresh response membership must agree with the visible cards after each reload,
and all phases must retain the same B token. The original Movies card must be
absent while restricted; Extras Movies, Music and TV must remain visible.

The controller must finish or reconcile restoration before releasing the
ordinary browser, even if DOM observation or stage coordination fails. Bind
each acknowledged update to its administrator, audit event and revision, and
never overwrite an unowned intervening change. Complete snapshots must account
for the two new sessions, the browser's new device, the two owned user updates
and their authentication audits. All other old rows and media stay exact;
PlaybackInfo, playback, scans, preferences and UserData writes remain outside
this Home-only permission run. These are implementation requirements, not
evidence that the permission run has executed or passed.
