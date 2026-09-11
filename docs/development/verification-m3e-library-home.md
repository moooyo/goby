# Original-client Home and library inventory verification

Status: **Home v3 remains a passed preparatory prerequisite. The subsequent
candidate permission-change UI run also passed its declared gate after explicit reloads.**
See [permission UI verification](verification-m3e-library-permission-ui.md) for
`permission_ui_acceptance=true`, `client_acceptance=false` and outcome
`permission_observation_after_explicit_reload`. Both ten-second no-action
windows had no fresh Views request; this does not prove automatic refresh
never occurs. Two explicit reloads produced the correct restricted/restored
library membership with the same new B token.

Current authority is
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd`:
73 global auth rows, 62 devices, 163 audits, 63 selected A/B auth rows, 26 play
rows, seven UserData rows, four libraries and 22 items; B is revision 5.
Continue remaining event, subtitle and client gaps from that state. Automatic
notifications and the complete M3/M4/M5/M6 milestones remain open.

The historical Home v3 result and evidence below retain their baseline-only scope.
The report retains `client_acceptance=false` and `permission_ui_acceptance=false`:
this is only `baseline_observation`. Its then-complete after snapshot was
`8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a`.
Neither v1 nor v2 is relabeled or replayed. This is preparatory
evidence for the [permission-change UI plan](m3e-library-permission-ui-plan.md),
not acceptance of original-client permission changes or the complete M3/M4/M5/M6
milestones. The previously completed [API restriction/restore matrix](verification-m3e-library-restriction.md)
and its independent persisted-state inspection retain their separate scope.

## Actual v3 Home/reload result

The [v3 controller report](m3e-library-home-v3.json) passed, SHA-256
`a8117846d80eeed8e714b8951103632acfc05105d8a14c362e99f0f9d9e62270`.
The [browser report](m3e-library-home-v3-browser.json) has SHA-256
`a5e4cd3a16c625c85b459c20c2349375a22bf63d889d7ac2e89f0e9237a91e28`.
The attempt is `/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v3`.
Controller and worker exited with code 0,
the worker became inactive with an empty cgroup, and no page/cleanup error or fallback
cleanup occurred.

One original UI login completed with 200. Initial Home showed all four correct-ID
library cards. Each library still had two global title matches but exactly one
visible card for its ID, which satisfied the corrected ID-scoped predicate.
One fresh frame-owned Views response and its matching physical forwarding
response completed 200 with all four libraries, without service-worker delivery.

Exactly one explicit `page.reload()` completed with document HTTP 200. Reload produced
one new frame-owned and one physical Views response, both complete with HTTP 200 and the
same B token and all four correct-ID cards. There was no second login, Movie
navigation, PlaybackInfo, media playback, Policy write or UserData write.
The explicit reload result is not evidence of automatic permission-change refresh.

UI logout returned 204 and the same token returned 401. Two WebSockets opened
and both closed; browser/context/proxy closure completed with zero pending work.
Unexpected network failures were zero. Two blocked external requests and two
console warnings remain recorded; the result does
not claim a console free of warnings.

The expected delta was one new B session, one device and two audit entries.
Old-row and sequence checks passed, preserving users/Policy, private state,
media, 26 play rows and seven UserData rows. The new session
`83be430b189b6ef0a4f685381dc493b8` is revoked, token SHA-256
`51965e61bf44dfa417dd5d2f545847aed9f0aba4636e0733fcd3e545639c7feb`.
Counts at the Home v3 checkpoint were 71 global auth rows, 61 devices, 157 audits
and 62 selected A/B auth rows, with four libraries and 22 items; B was revision 3.

V3 before/after snapshot SHA-256 values are
`84738aa74749a017e7394498d40e58a330df031203d269cc599d5adeae514fb7` and
`8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a`.
The latter was consumed by the later permission run; it is no longer current.
The worker-terminal SHA-256 is
`77a7e542193cdc4dccb918e731b10680aeec4bad6314ef7b6c2de6d63eb56fb7`.
The final stdout descriptor now matches the actual completed log, SHA-256
`6fb8ae39aa54e2663f464f1c4c2811b6c4f6bab9b3410b5ea932e574725075aa`.
The v2 creation-time descriptor remains unchanged historical evidence.

## V3 preparatory tool and source gates

[JS04 verification](m3e-library-home-js04-verification.json) passed seven remote
syntax checks, 35 Home guards and 114 cross-user guards, report SHA-256
`0dac68bcd2143d40331dbc19694abc76f63fb23344b8318be442b62d1aef54f4`.
[Python03 verification](m3e-library-home-python03-verification.json) passed two
syntax checks and 33 guards, report SHA-256
`b74e8c1e3e7906fa59270f0747979e26897698e5ad3277c7f0c7efaa0e790ab9`.
The actual check-only preflight passed with zero HTTP. Source-closure SHA-256:
`d10bc2210b8557c5fcac1aa661cff183803e4e4ed3226ce19fc248783e223acb`.
These tool gates remain distinct from the actual v3 browser and state evidence.

## Retained v2 failure

The [v2 controller report](m3e-library-home-v2-failed.json) remains failed,
SHA-256 `54c02dbc41273a5043853d31126dec7455641d5efbf73ee20b97b13d8826ec23`.
The [browser report](m3e-library-home-v2-browser-failed.json) has SHA-256
`900c1ba1279bdd2c32eeb5f1a81a261d8302fb019b2dc912d5a0f495a74a33a3`.
Both belong to `/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v2`.

The repaired login proof succeeded. The initial real Views response returned 200,
completed and contained all four expected libraries. The DOM observation found
two global title matches for each library but exactly one visible card with the
correct `data-id`. Requiring global title uniqueness even when a correct card
ID was available made the Home predicate too strict. The UI gate failed before
reload. This is an observer predicate defect, not an incorrect Views inventory;
the original failed report remains unchanged.

The owned WebSocket opened once and closed once. UI logout returned 204 and the
exact token returned 401, with zero page errors, cleanup failures or fallback
cleanup attempts. V2 therefore does not need the independent native recovery
used for v1. Exactly one new B authentication row, one new device and two audit
entries were admitted. All old rows, users/Policy, 26 play rows, seven UserData
rows, private state and media were preserved; the sequence checks also passed
within the expected state delta. This complete comparison passed independently
of the failed UI outcome.

V2 before/after snapshot SHA-256 values are
`e2223db45db0d5bd4ae52833ad639d436f0122f334f08f21115db720bae3ec5f` and
`27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26`.
The latter was the authority consumed by v3: 70 global auth rows, 60 devices, 155 audits,
61 selected A/B auth rows, 26 play rows and seven UserData rows; B remains
revision 3. The preceding recovery snapshot `5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0` is historical
input to v2, not the next run's baseline.

The worker exited normally with code 1. The independent terminal record has
SHA-256 `032de1e26bc4cfc325ee2054644fb5502b48f3462004810d68cd92fe4409a4d5`
and reports `MainPID=0`, `ExecMainCode=1`, `ExecMainStatus=1` and an empty cgroup.
This is a normal process exit with a failed UI result, not the v1 timeout path.

The immutable controller report's `node.stdout` descriptor contains the empty
file digest recorded when the log was created. The actual finished stdout
SHA-256 is `e7a18f3e12832a8621e6589f9947eb513545a16203f8934d494e064ef6de0c1d`.
Do not rewrite the old report or use that creation-time descriptor as exit
evidence; the retained worker terminal proves closure. A future controller
must record terminal log size/digest after the worker has ended.

## Historical intermediate tool checks

[Node tool03 verification](m3e-library-home-js03-verification.json) passed seven
remote syntax checks, 34 Home guards and 114 cross-user guards.
Its report SHA-256 is
`68f405571b40f78975ede3dcc0f0b481f360eae55987ce4b718b4eabdeb33702`.
[Python tool02 verification](m3e-library-home-python02-verification.json) passed
two remote syntax checks and 29 guards, report SHA-256
`04f6f5607f0665edb3aea09ebf477eecd7225529f2b87fb080f3cff5149eabb2`.
Those retained tool gates do not prove
the complete v3 inputs or a successful v3 UI run.

The subsequently verified v3 predicate requires one visible card for the correct library
ID, allowing other same-title text outside that card. When a card has no ID,
the fallback still requires a unique matching title. V3 used a fresh output
root/unit, the then-current `27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26`
authority and correct terminal log digests. Its later real pass is recorded
above; the older tool03/Python02 guards alone were not evidence of that pass.

## Frozen input and completed preparatory checks

All verification ran on `test-env`. No local tests, builds, runtime probes or
HTTP requests were performed for this documentation update.

Tool01 passed seven JavaScript syntax checks, 27 Home guards and 114 cross-user
guards, plus two Python syntax checks and 26 Python guards. Those results cover
the frozen tool inputs, not a successful original-client run. The source-closure
SHA-256 is
`7df785b05ee68022b1fa9a1ef9b024421194ea8aebf49826f48a05939040296c`.

The actual check-only preflight passed with zero HTTP calls and confirmed that
its output directory was absent. Its retained stdout SHA-256 is
`30a13df50e74118c0fec313bf3fbf9a150b45fec11b178f0e669197f8a4d6855`.
An absent output directory at preflight is not an after-run cleanup proof.

## Actual tool01 browser result

The actual attempt is retained under
`/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v1`.
Its browser-report SHA-256 is
`447297492853d3c69e8fc0ae042f6386177a112f4e3eda817c93678e433c37df`.
The result is failed with `home_login_proof_incomplete` and must remain failed.

The retained transport evidence proves:

- One genuine login returned 200 and completed its response transfer.
- A real frame-owned Views request returned 200 with exactly the four expected
  library IDs/names. The matching physical forwarding request was also observed.
- The original-client Home DOM/card proof did not complete, and no reload ran.

These are distinct observations. Successful Views membership and physical
forwarding do not prove visible cards, a completed Home flow, reload behavior,
automatic updates or permission-change UI behavior. No result is inferred from
client implementation details, decompilation or a synthetic API substitute.

## Observer and wait defects

The new Home observer matched its login path case-sensitively. The actual
request was `/emby/Users/authenticatebyname`, which the core harness already
recorded with complete login evidence. The Home observer failed to select that
request, so its proven-login gate did not open. This is an observer matching
defect, not a rejected login or an authentication failure in the product.

The same unproven-login gate blocked UI logout forwarding. The observed logout
result was 502, and checking the exact token still returned 200. A private
proven-login receipt was not available. The evidence therefore does not prove
revocation of the new B credential.

The wait implementation also left an unlimited polling branch alive after
`Promise.race`. Node could not exit. RuntimeMax subsequently terminated the
worker at its 360-second bound; systemd recorded `MainPID=0`, `ExecMainCode=2`,
`ExecMainStatus=15`, failed state and an empty cgroup. The worker terminal SHA-256 is
`65fd1b907e1942102da4637ba5cfa5eb6d20e14a17814600f0e40da23f749753`.
These fields describe the owned worker, not the Goby candidate service.
Worker process cleanup is now proved; it is not successful UI logout or
credential revocation.

The controller subsequently exited with code 1 and `unacknowledged_state_delta`. Its
failed report SHA-256 is
`ba5c314aeb8d408e61ebaac10c69ed743c376a89ab542f326acc9ade8b8cdea8`.
The original browser failure and the controller failure remain distinct,
immutable records. Their own cleanup gate remains failed; the independent
native recovery below supplies a separate successful revocation receipt.

## Complete failed-after observation

The actual before-snapshot SHA-256 is
`8534805055cdb18ba1585c7637879ddea9b4d4b5cbd38e6b21ca324b7b43a948`;
the complete after-snapshot SHA-256 is
`6d00f9cd99313762702c212ffdcc612cf88a426da67cfbcb0d1b403e8ef2d4d8`.
The after state contains one new B session, one new device and one login audit,
for totals of 68 auth rows, 59 devices and 150 audits. B's policy revision remains 3,
all 26 play rows and seven UserData rows are unchanged, and the complete media
inventory is identical before/after. These observed facts do not override the
controller's failed `unacknowledged_state_delta` result or establish that the
new B session has been revoked.

## Independent exact-session recovery

The [recovery tool verification](m3e-library-home-session-recovery-verification.json)
passed two remote syntax checks and 12 pure guards, SHA-256
`07724e71204f2707fe41fc4b1aa17bb95bda5d64590f4e1fa6db3873947c61a3`.
Check-only passed with zero HTTP. The independent actual
[recovery report](m3e-library-home-session-recovery.json) then passed in one
attempt at `/opt/goby-test/exec-work-m3e/client-library-ui-session-recovery-01/report.json`,
SHA-256 `09b2fad0de6dcce6e1d6ecb85a700e94673642f3c5641d3d7dff6bab43e66ffb`.

Four HTTP exchanges completed: administrator login 200, revoke 200 for only
target session `00fb0b833884308ab946e1c40ff6abdd`, administrator logout 204 and
401 for the same administrator token. The target's persisted row changed only
`revoked_at`, to `2026-09-11T21:05:29.180099+00:00`. B's token was lost; there
is no B exact-token 401 observation. Native administrative revocation and its
persisted-state proof must not be described as successful original UI logout.

Recovery added one new native administrator session and three native audit
entries, with no device, play or UserData changes. Final counts are 69 auth rows,
59 devices, 153 audits, 26 play rows, seven UserData rows, four libraries and 22
items; B remains revision 3. The recovery before/authenticated/after snapshot
SHA-256 values are respectively
`829204605a64614aa388022b5cda32e539cf769899ea87a755fa1c1d3d91e511`,
`f3a03707804521ffa57935a73d115bf35f6d54304e491eec67cf0159936da33c` and
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0`.
The last was the accepted recovered baseline consumed by v2. V2's after state
was then consumed by v3, whose 8095 after snapshot was consumed by the permission
run. Current authority is the latter's complete
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd` after snapshot.

The failed-tree digest was identical before/after recovery:
`1ecd55bcdfc53e37db023e88e5d61e2c91027cd6956616f8a2dd5d4014812af3`.
The full media digest was also unchanged:
`da593eb64e417260ee97653ea04526429332533b286fc16cda683560bf0ff476`.
All original before, after and failed evidence stays retained. No broad
revocation, reuse of a pre-existing authentication token or rewriting of the failed
browser/controller report was used.

The prior API-inspection 67-session/58-device/149-audit checkpoint and the
failed Home 68-session/59-device/150-audit snapshot are historical. Neither
the failed output root nor the old
`1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3`
authority may be replayed for the next run.

## Minimal correction and remaining gates

The verified tool02 change is confined to case-compatible login-path matching
and finite, cancellable waits. Fixed-target forwarding, physical response
binding, actor scope and byte-preserving observation remain required. Four
additional Home guards cover the new correction, increasing that suite from 27
to 31; the 114 cross-user guards remain a separate regression selection.

[Tool02 verification](m3e-library-home-js02-verification.json) passed seven
remote JavaScript syntax checks, 31 Home guards and 114 cross-user guards.
The verification-report SHA-256 is
`29a72b7ef400e856a3e6929b99eb509e0eb7e53c3e8e190731156881cd94c3a6`.
Its frozen Home source SHA-256 is
`3ceaf6bf043d6c377fd634337fda2d6513778efff056f2b0373803f9d12efda0`,
and its test SHA-256 is
`899abe6951bff479314ff43420424bacb27f84e5ca05ce7f0bf07ca2ba65cb9c`.
These completed tool checks do not prove a new Home/reload flow or credential
cleanup. Old tool01 bytes and its failed report remain unchanged; no corrected
execution may reuse its output directory.

The subsequent permission run followed the coordination contract in the
[UI plan](m3e-library-permission-ui-plan.md) with a fresh B token across baseline,
restriction and restoration. Its [separate verification](verification-m3e-library-permission-ui.md)
supplies the explicit-reload permission evidence; Home v3 is only a prerequisite.
Continue other event, subtitle and client gaps from the latest
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd` authority.
Do not reuse the failed root or replay the earlier API-inspection authority.
Automatic notification behavior and the broader M3/M4/M5/M6 requirements remain
open. Neither this prerequisite nor the declared explicit-reload gate establishes
complete client compatibility.
