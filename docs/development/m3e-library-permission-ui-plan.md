# Original-client library permission changes

Status: **the first Home-only run remains failed; independent exact-session
recovery passed. A corrected Home/reload run and permission-change UI acceptance
remain open.** The [recovery report](m3e-library-home-session-recovery.json)
records one successful attempt with four complete HTTP exchanges: new native
administrator login 200, revocation 200 of only B session
`00fb0b833884308ab946e1c40ff6abdd`, administrator logout 204 and 401 for that same administrator token.
B's token was lost, so no B exact-token 401 result is claimed. Its persisted
session changed only `revoked_at`, to `2026-09-11T21:05:29.180099+00:00`.

The latest recovered after-snapshot SHA-256 is
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0`.
Current counts are 69 auth rows, 59 devices, 153 audits, 26 play rows, seven
UserData rows, four libraries and 22 items; B remains revision 3. Recovery added
one native administrator session and three native audit entries, with no
device, play or UserData change. The failed evidence tree and complete media
inventory were preserved. The next Home/reload attempt must use this latest
recovered authority and a new output root; the old failed run stays failed.

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
zero-HTTP check-only and its actual four-exchange recovery. No corrected Home
run has executed. Bind the recovery's latest after snapshot and completed
tool02 verification before the next fresh run; do not replay the failed tree.

The [candidate API gate](verification-m3e-library-restriction.md) passed, including
exact restoration and independent persisted-state inspection. The next work
must use a new bounded original-client observation after recovery, without
replaying any previous UI or API workflow.

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
  visible library cards. Match a DOM ID when present; when absent, record
  unique-title evidence without claiming an observed DOM identifier.
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

## Historical authority and latest recovered baseline

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
The last value is the latest accepted recovered authority for a corrected
Home run in a new output root. It has69 auth rows, 59 devices and153 audits.
Neither the failed root nor the old `1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3` API authority may be
replayed. Private snapshots and credentials remain on test-env.

## Subsequent permission-change gate

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
