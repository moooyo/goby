# Selected compatibility phase 1 execution record

Status: **implementation complete; consolidated verification and browser repairs in progress**.

The user authorized execution of the [four-phase plan](../planning/selected-compatibility-plan-20260920.md),
with all delivery code implemented before each phase's consolidated verification.
Local compilation and unit tests are authorized; real integration/media/browser
E2E uses `test-env`. Actual local E2E is prohibited. All four completed phases
must be handed off, merged to `main` and pushed.

## Source and ownership

- Baseline: `main`/`origin/main` at `f6f2d163eb913e4eaa545eea92980dfa4819589e`,
  fetched and compared before implementation; product baseline `80198b6`.
- Development branch: `codex/selected-client-compatibility`, isolated from the
  original checkout's unrelated OCI, packaging and historical draft changes.
- The six selected planning/status documents were carried into this checkout.
- Schema 42 is assigned to account local credentials; schema 43 to intro markers.
  Fresh catalog exports and backup/recovery coverage are required before closure.

## Delivery ledger

| Requirement | Implementation | Evidence |
| --- | --- | --- |
| A1 PIN and local password | Implemented | Identity and HTTP scopes passed; original-client profile journey pending |
| A2 Source-bound intro intervals | Implemented | Source/HTTP repairs passed; real native administrator journey passed |
| A3 Actual intro skip behavior | Implemented | Item/PlaybackInfo and explicit-start contracts passed; original-client seek journey pending |
| A4 Next-episode preference and consumer | Implemented | Authorized complete-queue contracts passed; original-client transition journey pending |
| A5 Administration, migrations and recovery | Implemented | Historical migration repairs, real encrypted PIN archive/restore and mocked UI passed; full browser closeout pending |

Contract research uses the pinned SDK and retained official client distribution.
Prepared harnesses and code review are not actual client acceptance. The phase
cannot close until its final integrated source passes the required account,
movie/episode playback, administrator, persistence and recovery journeys.

## Recorded implementation decisions

- The user disabled the ui-ux-pro-max skill for this task. UI changes use existing
  repository components and interaction patterns directly.
- Retained official Emby Web reads Configuration.ProfilePin and compares four
  entered digits locally; Configuration/Partial supplies edits. The user chose
  compatibility: encrypt PIN at rest, expose it only to the authenticated owner,
  and treat it as a profile lock while password authentication remains separate.
  Administrator/public/other-user projections do not disclose it.
- The client owns intro seeks and next-episode transitions. Source-bound
  IntroStart/IntroEnd chapters and persisted preferences must reach its actual
  item/playback responses; the server must not also perform the same seek.
- Client episode queue requests omit pagination. The implementation must handle
  series longer than the existing default page without losing the current or
  subsequent episodes, and must retain current authorization and explicit paging.
- The user selected a free-license policy for Goby: its feature-registration
  compatibility queries return valid/registered status without a Premiere
  requirement. This does not grant user/device/library access or advertise
  excluded features. A separate adapter implements this policy. Static research
  found that the retained original Web intro control calls a hardcoded external
  device-registration service; the local adapter does not redirect that request.
  Actual consumer evidence must distinguish those paths.

## First consolidated verification and repairs

Development commits are `bdadd88` (implementation), `a5a56b7` (fresh schema43
catalog), `43b0fdf` and `5c0fef6` (test fixture corrections), and `a99988f`
(administrator dialog accessibility/layout correction). Later repairs must
record their own actual source rather than reclassifying these attempts.

Local frontend typecheck/build and ordinary/embedded Linux cross-builds passed.
Local identity/database unit packages passed. The attempted Windows library/server
scope failed existing Linux-fixture and descriptor assumptions; it is not a
passing platform result. The Linux package scope below is authoritative for those
packages. A fresh owned PostgreSQL 17 cluster generated schema43 catalog SHA-256
`1dc5115fd00947b8beac7384bc16e0d393f9fca70e031a71f9ccec5846ad511c`.

| Remote scope | Original source/result | Separate repair result |
| --- | --- | --- |
| Identity | a5a56b7: 182 parent passes, no failures/skips | Not repeated |
| Database | a5a56b7: 51 parent passes; historical whole-row projections/table inventory failed | 61 parent passes, including explicit legacy plaintext-PIN cleanup; no failures/skips |
| Library | a5a56b7: 709 parent passes, three failures and the existing mount-namespace helper skip | Three affected parents passed with current probe fixtures and exact historical-field/default checks |
| Server | a5a56b7: 822 parent passes, one intro-fixture failure and the explicit AMD profile skip | The affected intro HTTP parent passed |
| PostgreSQL backup | a5a56b7: 98 parent passes, no failures/skips | Two selected account/intro archive parents passed, including strengthened sequence equality |
| Recovery database | a5a56b7: 12 parent passes, no failures/skips | Not repeated |
| Recovery manager/engine | a5a56b7: 21 parent passes; historical identity seed failed, retaining its pair and causing four subsequent empty-target refusals | Six selected parents passed on a fresh pair, including the real encrypted-PIN archive/finalizer/new-login journey |
| Mocked administrator browser | a5a56b7: one pass, nine failures from duplicate dialog title IDs | a99988f: 10 passes, no skips/flaky cases; owned static server closed |

Counts from overlapping original and repair scopes are not added. Runtime
receipts record the exact test binary hashes. The first package batch closed
all eight process groups; peak worker memory was 712.7 MiB with no swap. The
recovery failure's databases were retained, and the repair used a separately
created pair. The owned PostgreSQL service remains running for pending browser
verification and has not been claimed closed.

The first real-browser attempt stopped before launching a browser because the
selected Node executable was not root-owned. A new owned byte-identical Node
copy satisfied that prerequisite without changing the original installation.
The next attempt passed three native administrator stages and their database
acknowledgements, with zero page errors/foreign requests, then failed before any
original-client authentication request: the fixture incorrectly treated
WebDirectory as a `/web` host. Its media/schema/context cleanup passed.
The corrected fixture must transparently serve complete official `/web`
responses, including dynamically generated apphost.js, from the pinned retained
host. A raw distribution directory is insufficient. Business APIs and WebSockets
must still reach the real Goby application; reference-host mutations, client
patches and synthetic license success are not acceptance evidence.

Private execution material is under
`D:/Code/goby/.git/selected-compatibility-20260920/phase1/` and the remote root
`/opt/goby-selected-compatibility-20260920-p1a`. The local mutable checkpoint is
`D:/Code/goby/.git/selected-compatibility-20260920/checkpoint.json`. These are
private operator/evidence paths, not portable clone prerequisites. The current
phase is not complete and phases 2-4 have not started implementation.

## Original-client fixture repairs

These attempts preserve the preceding failures and do not count as completed
playback acceptance. The pinned host serves only official `/web/` assets;
authentication, catalog, playback and WebSockets still target Goby.

| Attempt | Source | Observed result |
| --- | --- | --- |
| r03 | Go and driver `2de476d` | Native administration passed. Official assets were served and hashed, but blocking service workers prevented the unmodified client from reaching authentication. |
| r04 | Driver `38e67dc`; unchanged Go fixture `2de476d` | Native administration passed. The real service worker reached `activating`; the fixture incorrectly required immediate activation. |
| r05 | Driver `9470400`; unchanged Go fixture `2de476d` | Native administration and actual original-client local-password login passed with database acknowledgements. The worker reached `activated`. The PIN journey then failed because same-tab reload retained the client's `sessionStorage` validation state. |

The driver now allows the original client's real service worker behind a
deny-only egress proxy and waits for its actual activation. It neither changes
third-party JavaScript nor manufactures feature entitlement. r05 observed zero
page errors and two blocked foreign requests; their destinations were not
recorded in that scope and remain unclassified. New diagnostics must not
retroactively classify those requests. The next repair uses the actual profile
lock lifecycle: accept the original client's PIN opt-in, close that tab, and
open a new tab without an opener in the same browser context. The browser
naturally retains authentication while starting fresh tab-local validation;
the fixture must not write client storage.

The r03-r05 workers terminated and closed their process groups. These are
fixture repair results only; next-episode, intro-skip and final restart/revocation
journeys are still pending.
