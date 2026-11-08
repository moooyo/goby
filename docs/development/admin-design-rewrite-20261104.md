# Administrator design rewrite

This is the November 4 baseline record. The
[November 6 capability implementation](admin-capabilities-20261106.md) supersedes
its capability-decision table and records the subsequent verification scope.
The [core API follow-up](admin-core-api-20261106.md) records the later first-run,
update-check, playback-activity, configuration, and restart work.

## Reference and scope

The November 4, 2026 downloaded administrator prototype selects direction 1a:
a 232px sidebar, a 56px header, and right-side editing sheets. Alternative
directions in the reference are not additional product requirements.

The rewrite removes MUI and Emotion, packages the four reference font families,
and introduces native accessible controls with static CSS and semantic tokens.
It preserves the nineteen existing canonical routes and their flat URL aliases
while exposing fifteen primary navigation destinations. Hardware settings are
part of Transcoding; online providers are part of Metadata & subtitles. Library
detail combines items, images, directories, analysis, and scan history.

The interface defaults to Chinese and dark appearance. Language and appearance
are independently persisted without storing authentication information. Existing
CSRF handling, exact revision values, shared settings drafts, navigation guards,
destructive confirmations, and recovery from unconfirmed mutations remain in
the underlying workflows.

## Verification protocol

The reproducible loop is documented in
[the administrator review checklist](../../web/admin/e2e/design-review-checklist.md).
Each final round binds the frontend, shared tokens, tests, lockfile, build inputs,
and compiled assets to a fingerprint, verifies that the HTTP entry matches the
current production build, runs browser checks, captures screenshots and geometry, and
requires a separate recorded visual review. Two consecutive passing rounds
with no new recommendations for the same source hash are required to finish.
Earlier rounds with findings remain part of the evidence.

Browser checks use explicit synthetic API fixtures and never send a real
mutation. They establish frontend behavior, including request payloads and
security state, rather than database-backed end-to-end behavior. Go resource
handler checks use actual `httptest` requests and an actual embedded asset tree.
The application entry point is Linux-only; compilation on the Windows host
targets Linux amd64, and Linux handler tests run in the existing local WSL
Debian environment.

Raw screenshots, test reports, review journals, command logs, and build receipts
are retained outside the repository. The task closeout records the final round
numbers, test counts, source hash, and binary hash in that evidence directory.

## Recorded outcome

The loop completed on November 4, 2026 after eleven retained iterations.
Rounds `round-10` and `round-11` each passed all 116 browser checks with zero
skipped, unexpected, flaky, or retried results. Each round independently reviewed
all 148 newly captured PNG files and recorded zero new recommendations. Two
independent visual reviewers agreed on both final rounds. The final status is
`consecutiveClearRounds: 2` and `exitConditionMet: true`.

Both final rounds reviewed the actual production build, with an unchanged HTTP
entry and identical source-and-asset fingerprint:

- Source and production fingerprint: `ee76b6299e4f05a51996d856cbfc9e28f8f234ff85934ed20cb21c0e5c5dc39c`.
- Production entry SHA256: `31fc73d600b52a8f74a552648879c731e41db881ae689dd8e7ec89727a002ffe`.
- Linux amd64 embedded binary SHA256: `9d1ed17e73bc5c3a5f690cea184a316d0038b4a14d2ce7e18c4c771543ad93db`.

Local production TypeScript/Vite compilation passed. The Windows-host Linux
amd64 embedded cross-build passed, followed by five parent resource tests and
nineteen subtests in local WSL. All 270 production assets were reread and matched
the embedded-build manifest during closeout. The six existing schedule unit
tests also passed. Vite retains a non-blocking main-chunk size advisory; its
warning threshold was not increased to hide it.

The retained local evidence directory is
`C:\Users\moooyo\.codex\tmp\goby-admin-review-20261104`. It includes `loop.json`,
`verification-closeout.json`, `go-final-verification.json`, the final binary,
per-round reports, both reviewers' submissions, screenshots, and diagnostics.
Earlier failed and non-clear rounds were kept.

The regression suite preserves real keyboard activation and browser FileChooser
events. A bubbling file-input cancellation is no longer treated as cancellation
of its containing sheet. FileChooser interception is subscribed before navigation
so immediate keyboard activation does not race the browser automation subscription.
Queued native close events from an earlier command-palette opening cannot close a
later opening. Raw names and sort words remain literal while UI count nouns use
locale-aware plural rules. The 0/1/many count matrix is DOM-assertion evidence;
its states are not claimed as additional screenshots.

## Capability decisions

These are differences in the current management contracts, not simulated
features or claims that the underlying server has no related functionality.

| Area | Current behavior and supported functionality | Decision required to match the remaining prototype behavior |
| --- | --- | --- |
| Playback monitoring and control | Native administrator sessions expose sign-in records. The Emby token surface already exposes current playback and supported remote commands. The dashboard does not repurpose its administrator cookie as an Emby token. | Add a cookie/CSRF administrator bridge for current playback, control acknowledgements, messages, and a durable recently-ended playback history. |
| Notifications | Administrators configure one HTTPS receiver and see supported invalidation events and pending deliveries. Personal Emby registrations separately support event selection, testing, and the last result. | Add administrator-owned multiple destinations, SMTP/ntfy transports, an event subscription matrix, test operations, and a delivery history list. |
| Provider configuration | Provider status and per-item metadata, image, and subtitle search/application are available. Global credentials are supplied by deployment. | Add administrator credential updates and an explicit connectivity-test contract. Do not equate per-item candidate search with credential editing. |
| Scheduled tasks | Existing triggers, timezone, next-run information, run/stop actions, and recorded results remain available. Task `Enabled` is read-only. | Add an explicit task enable/disable mutation if the prototype switches are required. |
| Backups | Encrypted creation, import, download, restore planning, validation/staging, application, and rollback are implemented. Managed images stored in the database are included. | Add automatic schedule/retention policies, an optional image-inclusion policy, and a before/after content diff. The existing restore plan must not be described as missing. |
| General server settings | Server identity and desired/active HTTP listeners are editable; listener changes explicitly require a restart. Browser language and appearance are immediate local preferences. | Add global remote-access/LAN policies, base-path configuration, default interface language, service restart, and update/channel management where required. |
| Hardware information | Software and VA-API choices, device availability, actual diagnostics, and existing encoding limits are retained. | Add per-codec device capability summaries or additional backend modes before exposing a detailed acceleration matrix. |
| Directory permissions and volume grouping | Allowed-root availability, aggregate storage usage, and exact-path matched volume capacities are available. Aggregate totals count each volume once; they are not directory quotas. | Add read-only/writable descriptors and explicit root-to-volume associations before displaying those prototype permission labels or grouping different directory paths under one volume. |
| Images | Upload, removal, reorder, reset to automatic images, and per-item online candidates/preview/application are supported. Managed changes preserve their image-type override. Image writes are acknowledged individually. | Add explicit image-lock state, online Logo candidates, atomic multi-image draft submission, or server-managed missing-image automation. Single-item online image download is already supported. |
| Episode ordering | Existing expected episode rosters, missing-episode information, and playback marker editing remain available. | Add a DVD-versus-aired display-order setting and its rematching semantics. |
| List and technical summaries | Library totals are read through existing paginated queries. Devices and API keys retain their actual address and activity fields. User and item list DTOs expose fewer aggregate/technical columns than the prototype; the file and marker tabs retain the details their APIs report. | Add reliable user last-login/playback summaries, bulk item duration/specification/artwork/analysis summaries, full chapter/file-size/bitrate details, and database/schema version information where required. Some underlying details exist; the corresponding management summary contracts are missing. |
| First-run setup | The bootstrap contract creates the administrator and returns to a cleared login form. Libraries can then be created through authenticated management APIs; media roots remain deployment-controlled. | Extend the bootstrap workflow if media-root configuration and first-library creation must be completed before the initial sign-in. |

## Verification limit

The local WSL environment has no PostgreSQL `initdb`, `pg_ctl`, `postgres`, or
`psql` installation. An additional disposable database-backed browser smoke run
could not be started there. No software was installed and no existing database
or credentials were reused. This does not invalidate the production frontend
build, synthetic browser checks, Linux cross-compilation, or local resource
handler checks; it limits the end-to-end claim to those recorded scopes.
