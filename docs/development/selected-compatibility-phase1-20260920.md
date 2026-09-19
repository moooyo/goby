# Selected compatibility phase 1 execution record

Status: **implementation in progress; consolidated verification not started**.

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
| A1 PIN and local password | In progress | No verification claim |
| A2 Source-bound intro intervals | In progress | No verification claim |
| A3 Actual intro skip behavior | In progress | No verification claim |
| A4 Next-episode preference and consumer | In progress | No verification claim |
| A5 Administration, migrations and recovery | In progress | No verification claim |

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
