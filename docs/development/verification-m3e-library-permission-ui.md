# Original-client library permission UI verification

Status: **the declared candidate Home library-permission gate passed in one actual run
after explicit reloads.** The outcome is
`permission_observation_after_explicit_reload`, with
`permission_ui_acceptance=true` and `client_acceptance=false`. Automatic
notification behavior and complete M3/M4/M5/M6 acceptance remain open.

The [controller report](m3e-library-permission-ui.json), SHA-256
`4755b35026c47b692d5f3e68f57e7f2e6e4846b53643bd714cc92f493fe62f2d`,
and [browser report](m3e-library-permission-ui-browser.json), SHA-256
`ed6de30665561a552a1737af5f7856dfcfe2910bc675c7bcb878e27986d7822d`,
are retained under `/opt/goby-test/exec-work-m3e/client-library-permission-ui-v1`.

## Same-token observations

One original UI login established B's session. The same token was bound across
all three phases, SHA-256
`3730c9388198a612753144118e2c2a053ccf246adbf96e239d6c1b0a9ba7cbd9`.

| Phase | Fresh Views evidence | Visible Home state |
| --- | --- | --- |
| Baseline | One complete frame-owned Views200 and matching physical response | Four correct-ID library cards |
| Restricted, ten seconds without action | Zero frame/physical Views requests; `not_observed_within_window` | Original Movies card still present |
| Restricted, one explicit reload | One fresh complete frame/physical Views200 pair with the same token | Three cards; original Movies global-title, visible-card and ID-card counts all zero; Extras, Music and TV retained |
| Restored, ten seconds without action | Zero frame/physical Views requests; `not_observed_within_window` | Original Movies card still absent |
| Restored, one explicit reload | One fresh complete frame/physical Views200 pair with the same token | Four correct-ID cards restored |

The two windows establish only that no fresh Views request was observed within
those intervals. They do not prove the client never updates automatically.
The successful permission result depends on the two explicit `page.reload()` calls.

## Restoration, cleanup and state

Nine native HTTP exchanges completed: one administrator login, four managed-user
GETs, two PUTs, logout and exact-token rejection. B's revision advanced from 3
to 5, and its existing eight-key raw Policy was restored exactly. This differs
from the earlier API run that first materialized the original empty object.

UI and administrator logout204/exact-token401 both passed. Three WebSockets
opened and three closed; context, browser, proxy and Node closure passed with
no errors, page errors or fallback. Three external requests were blocked and
three console warnings were retained in total; no per-phase allocation is claimed.

The admitted delta was two new auth rows, one device and six audits: four session
events plus two `user.updated` events. Apart from the owned B updates and these
additions, old rows, expected sequence state, private state and media were
preserved. All 26 play rows and seven UserData rows were unchanged; references
and encoding jobs remained zero.

Current counts are 73 global auth rows, 62 devices, 163 audits and 63 selected
A/B auth rows, with four libraries and 22 items. B is revision 5. The private
before/authenticated/after snapshot SHA-256 values are respectively
`f81a699d20893edf05326825638bc9b312e8f17d5aaef106fa547667b29f2d13`,
`add58cebc4289e3c957548e4e9eff76cec7a124b3c3ce7e08cec294a5f2a5664` and
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd`.
The last is the latest authority. Home v3's 71-auth/revision-3 snapshot is a
consumed prerequisite, not current state.

An independent outer unit owned the controller/restoration lifetime separately
from SSH. It exited with code 0 and released an empty cgroup, terminal SHA-256
`f384505e0183830f7c74e2d704a282d954275362259011fa72011e55499bf1a8`.
The Node terminal SHA-256 is
`78731dd4e7469d49dab36ba78b48e3af3c5b00db38c4007bbabaf86e215ba67a`.
This records the executed arrangement and termination, not an additional
SSH-disconnection experiment.

## Supporting gates and remaining work

[Node verification](m3e-library-permission-js01-verification.json) passed nine
remote syntax checks, 30 permission guards, 38 Home guards and 114 cross-user
guards, report SHA-256
`e15bd7f5ce3b072f2b405be7890365b53ff5bc0deded80c4dd2e6ab9aa67ed31`.
[Python verification](m3e-library-permission-python01-verification.json) passed
two syntax checks and 24 guards, report SHA-256
`45c72b75b734aad1add1a1629c8c675acdf98f1ca5f86ad0067a46c7a602c21d`.
The actual check-only preflight passed with zero HTTP.

The [Home v3 prerequisite](verification-m3e-library-home.md), v1/v2 failures and
exact-session recovery retain their original results. Continue remaining
events, subtitle and client-compatibility gaps from the latest after state;
do not replay old runs or promote this declared gate to complete M3/M4/M5/M6 acceptance.
