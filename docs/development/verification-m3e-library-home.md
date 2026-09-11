# Original-client Home and library inventory verification

Status: **tool01 and its controller remain failed; independent exact-session
recovery passed once. Corrected Home/reload verification remains incomplete.**
The latest accepted recovered after snapshot is
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0`.
Next, bind that authority and the passed tool02 checks to a new Home/reload
output root. The original UI failure is not relabeled as passed. This is preparatory
evidence for the [permission-change UI plan](m3e-library-permission-ui-plan.md),
not acceptance of original-client permission changes or the complete M3/M4/M5/M6
milestones. The previously completed [API restriction/restore matrix](verification-m3e-library-restriction.md)
and its independent persisted-state inspection retain their separate scope.

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
The last is the latest accepted recovered baseline.

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

Next, bind the successful independent recovery and completed tool02 verification,
then freeze a new bounded Home/reload attempt in a new root against the latest
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0` baseline.
Do not reuse the failed root or replay the earlier API-inspection authority.
The original-client permission-change
workflow still needs separate baseline/restricted/restored UI observations.
Neither successful API restriction nor the partial Views response closes those
gates or the broader M3/M4/M5/M6 requirements.
