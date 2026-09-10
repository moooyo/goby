# M5g settings verification

Status: **M5g native settings increment accepted: complete Go regression,
isolated browser/restarts, protected deployment, and main-service workflow passed**.
All test, validation, media, browser, and service execution takes place on Linux
through `ssh test-env`. Local compilation is authorized separately. The accepted
live deployment now uses M5g native settings, schema 20/probe 6. The Emby
ConfigurationService adapter and the wider administrator milestone remain incomplete.

## Implemented native scope

The [native settings API](../api/settings.md) manages the server name and four
limits for new output plans. Schema 20 adds one `managed_settings` singleton;
the existing 27 tables and the old `server_settings` contents remain intact.
Nullable overrides preserve the distinction between deployment defaults and
explicit database values. A reset clears only the selected overrides.

Native writes use revision checks and current administrator authority inside an
owned transaction. A successful commit publishes an immutable runtime snapshot
before completing the operation. Each incoming request captures one effective
settings snapshot. Existing registered HLS plans retain their concrete output
settings; subsequent progressive requests perform their own planning. See
[settings operation](settings.md) for these boundaries and restart behavior.

The dashboard provides effective values, defaults, source indicators, selective
reset, conflict recovery, lost-response recovery, and an unsaved-change guard.
Its deployment section exposes only the seven documented read-only fields.
It does not expose credentials, connection strings, internal paths, or arbitrary
startup configuration.

## Targeted Linux results

The following are separate test runs, not additive counts of unique tests.

| Run | Result | Evidence |
| --- | --- | --- |
| Configuration, settings domain, database, and library race tests | 284 top-level tests passed | [Core report](m5g-core-tests.json) |
| Native settings DTO, strict parser, authorization, HTTP, and PostgreSQL behavior | 18 top-level tests passed | [HTTP report](m5g-settings-http-tests.json) |
| Real HLS output, server-name/key snapshots, and strict NUL rejection | 3 top-level tests passed | [Media report](m5g-settings-media-tests.json) |

The media run uses an actual native settings update. The old registered HLS
output remains 160 by 90 pixels while the new plan produces 96 by 54 output,
with real FFprobe and decode checks. It proves preservation of registered
output, not that an old producer was still running during the update. A separate
test checks that new key metadata uses the new name while existing key and
default-client rows remain unchanged.

The shared name validator rejects invalid UTF-8 and NUL because PostgreSQL text
cannot store NUL. Rejection leaves both the stored row and published revision
unchanged. Valid text is preserved without trimming or Unicode normalization.

## Isolated browser and restart result

The [browser report](m5g-settings-browser.json) records a successful first attempt
in **6.280795 seconds**, with no unexpected, flaky, or skipped browser tests.
The maintained [runner](../../scripts/test-env/verify-settings.py) and
[Playwright scenario](../../web/admin/e2e/settings.spec.ts) cover exact bitrate
entry, partial reset, stale revisions, a response lost after an actual commit,
explicit reload, dirty navigation, and the mobile layout.

Both restarts compare all **28 tables** before issuing new authentication
requests. The first preserves the existing settings and native sessions. The
second changes defaults only in the owned process environment: database
overrides remain unchanged, while fields without overrides take the new
deployment defaults. Existing cookie and CSRF authority survives the restarts.
Both newly owned credentials are revoked, and the isolated process, database,
role, HBA entry, and private runtime are cleaned up. The main service remains
unchanged.

The reviewed screenshots are [defaults](screenshots/settings-defaults-desktop.png),
[overrides](screenshots/settings-overrides-desktop.png),
[reset](screenshots/settings-reset-desktop.png), and
[mobile](screenshots/settings-mobile.png).

This native candidate is bound to 419 Go/module/SQL inputs, ten internal
test-data inputs, 38 browser source inputs, and 50 current dashboard assets.
The [final source gate](m5g-final-go-source-gate.json) passed. Its Linux binary
SHA-256 is unchanged from the
accepted browser run:
`e1f6b723eb963ad855f465d0798958480615b8b000455fcf26bc7b088b8d2f2f`;
the asset archive SHA-256 is
`0f4b7b0124b71551b31f680714cbf00c0ab8e209b7b3f3c66ceb0048eec573eb`.
These identify the tested and deployed native candidate. They do not certify a
future compatibility implementation.

## Complete regression attempts

The [first complete race run](m5g-native-full-race-attempt-1.json) finished with
1,219 passing and three failing top-level tests, with no data-race warning.
The snapshot packaging omitted seven existing NFO fixtures and one existing
MP4 fixture. `TestParseNFOFixtures`,
`TestProgressiveVideoReadyRealFFmpegFirstFragment`, and
`TestProgressiveObserverDispatchesRealVideoPrefixes` failed when opening those
missing files. This is a failed verification attempt; it is not product
acceptance.

The original snapshot and log are retained. The second snapshot preserves the same
419 Go/module/SQL inputs and adds all ten internal test-data files, including
the fixture provenance document and existing probe JSON. Its 2,051 reference
fixtures are recorded in a separate manifest.

The [second complete race run](m5g-native-full-race.json) passed **1222 top-level
tests across fourteen tested packages**, with zero skipped tests, no race
warning, no failed tests, and Go exit code 0. The package-level skip for
`cmd/goby` means it has no test files; no individual test was skipped. The
retained full log SHA-256 is
`1cc9de27619f27504f2d5174e6726a19aab1e4ef00038dc9e50b660f97d7e7c6`.
Both attempts use the isolated PostgreSQL endpoint at `127.0.0.1:15432`, separate
from the main service database. The passing run supersedes the failed attempt
for native source regression without removing its failure evidence.

## Protected deployment

The [deployment report](m5g-deployment-evidence.json) passed on its first attempt.
The service now runs as PID **3614026**, start ticks **25289276**, UID **995**,
schema **20**, and probe **6**. The executable, all 419 installed source inputs,
and 50 current administrator assets match the accepted native candidate.
Previously retained assets are recorded separately from the current build.

The operator completed and verified the protected backup at
`/opt/goby-test/backups/m5g-20260910` before stopping the old M5f service. Generated
recovery SQL was actually restored into an isolated database; all **27 old
tables** matched their captured rows exactly, and that database was removed.
No live database restore was performed. The deployment then replaced the
service and applied migration 20, preserving all old business rows,
`server_settings`, task history, application-key master, runtime/unit
configuration, and eleven media files. Migration history gains its version-20
entry; `managed_settings` starts with one revision-1 row and five NULL overrides.
No media scan or task execution was issued by deployment. This operational
backup/restore check does not implement product backup/restore management.

## Main-service settings workflow

The [deployed workflow](m5g-deployed-settings.json) passed its first attempt in
**0.701 seconds** with 15 GETs, one POST, two PUTs, one DELETE, zero transport
retries, and 19 verifier SQL queries forced read-only. The application PUTs
perform normal authorized database writes; the SQL restriction applies to the
independent verifier connection.

One newly issued native cookie changes all five overrides to a bounded temporary
set. Public system information, administrator overview, effective values and
source indicators follow the commit. A fixed-revision CAS restores the exact
original nullable override set. Deployment defaults and resource configuration
remain unchanged. The credential is logged out and independently denied with
`401`; a final SQL barrier verifies cleanup. No response was lost, and the
exceptional recovery branch was not needed.

All pre-existing rows in the 27 prior tables remain exact, including old user
login activity. The sole new native session remains as revoked history; no
physical history deletion occurs. The separate
[read-only post-workflow observation](m5g-post-workflow-settings-state.json)
confirms revision **3**, with all five overrides NULL, and binds that result to
the accepted main report. Only revision/update time advanced. Media, private
credentials, service identity, and source evidence remain unchanged.

This workflow reads anonymous `/emby/System/Info/Public` but makes no Emby
configuration or credential operation, application-key request, scan, planning,
playback, or media request. It does not restart the main service. The earlier
isolated workflow supplies restart evidence, and targeted media tests supply
the registered-plan/output evidence; this live settings workflow does not
claim either operation anew.

## Configuration compatibility and remaining scope

The [read-only reference study](../research/configuration-reference.md) is a
completed research increment. The subsequent
[fresh mutation study](../research/configuration-mutation-reference.md) adds
254 records: 17 setup records and 237 capture records, bringing the corpus to
2305. Its 236 complete capture HTTP exchanges finished in 3.238 seconds. A
mixed invalid partial update returned `500` after a name change remained visible
in that process; restart persistence was not tested. Three application-key
write controls returned `204` for complete baseline no-ops, not changed values.
The owned fresh process was stopped and its data removed, preserving old state.
Its preparation guards
[pass nine synthetic tests](m5g-fresh-operator-tests.json); the
[first guard attempt](m5g-fresh-operator-tests-attempt-1.json) is retained after
correcting a test fixture whose sensitive key name conflicted with its public
control-value assertion. No sanitizer behavior was relaxed.

The ConfigurationService compatibility adapter remains unimplemented. Native
acceptance and reference observations do not establish that adapter, full Emby
configuration parity, actual GPU execution, or complete client compatibility.
M5g completes this native increment only; M4, M5, M6, and the full goal remain open.
