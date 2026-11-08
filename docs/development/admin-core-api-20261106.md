# Administrator core API follow-up

Date: November 6, 2026.

## Scope and evidence

This follow-up implements capabilities verified as Emby core behavior after the
administrator design rewrite. Plugin-only parity is outside the requested scope.
The earlier native notification and backup implementations remain usable.

The official SDK baseline is 4.10.2.0 at commit
`45e73012eddf985dcfe878ca031b500085e16496`. A route exported by that SDK is not
automatically a core service: its generated inventory also contains plugin
services. Conversely, hidden routes can implement a core product workflow.

| Capability | Primary evidence | Implemented result |
| --- | --- | --- |
| Initial setup | Official [installation workflow](https://emby.media/support/articles/Installation.html), [library setup](https://emby.media/support/articles/Library-Setup.html), and the repository's recorded Emby 4.9.5.0 startup user, remote-access, and completion responses | A complete native setup workflow with the existing operator token and authenticated library authority. |
| System update observation | Core [Packages/Updates](https://dev.emby.media/reference/RestAPI/PackageService/getPackagesUpdates.html) distinguishes System packages from UserInstalled plugins | A real Goby release-feed checker, cached observations, administrator UI, and the System package projection. |
| Recent playback events | Core [ActivityLog](https://dev.emby.media/reference/RestAPI/ActivityLogService/getSystemActivitylogEntries.html) and the official [historical core implementation](https://github.com/MediaBrowser/Emby/blob/63ab71357117a97adf5789d31e4261ea934fb0fa/Emby.Server.Implementations/Activity/ActivityLogEntryPoint.cs#L149-L238) | Committed start/stop observations and recent stops using the existing activity retention policy. |
| Configuration policy | Core [ServerConfiguration](https://dev.emby.media/reference/RestAPI/ConfigurationService/getSystemConfiguration.html) exposes EnableRemoteAccess, LocalNetworkSubnets, and UICulture | Full and Partial configuration reads/writes share the native server policy and its real enforcement. |
| Restart | Core [System/Restart](https://dev.emby.media/reference/RestAPI/SystemService/postSystemRestart.html) and CanSelfRestart | The compatibility API invokes the same graceful generation owner as native administration. |

The historical source establishes separate AudioPlayback, AudioPlaybackStopped,
VideoPlayback, and VideoPlaybackStopped types. The current SDK declares Type as
a string; it does not independently prove every implementation detail of the
current proprietary server.

## Behavior

The first-run sequence creates the administrator, authenticates once, offers an
initial library, configures remote access, then logs out and displays a clean
login form. Library creation can be skipped. Paths remain within deployment
authorized roots. The original bootstrap request still creates no session.
Unknown write outcomes require readback or authentication recovery before any
new attempt. Network settings that would disconnect the wizard are deferred
until normal administration. No anonymous directory or library API is added.

Update checks use an optional deployment-controlled `GOBY_UPDATE_FEED_URL`.
There is no fabricated public Goby release service. Unconfigured, unchecked,
failed, current, available, and incomparable-version states remain distinct.
The source is HTTPS, public-address constrained, bounded, and redirect-free.
The checker compares the Goby product version; Emby's compatibility version is
not a release identifier. The System package projection reports only actual
observations. Docker replacement remains a deployment operation and
CanSelfUpdate remains false. See [the API contract](../api/server-updates.md).

Configuration policy and managed settings commit in one owned transaction and
publish together after success. Both native revision contracts are preserved.
The latest locked access policy is rechecked even for a request without policy
fields. A permitted request can finish its own disconnect response; a request
that became forbidden while waiting cannot reopen access. Full objects from
older clients preserve omitted policy fields. Named sections cannot bypass the
typed configuration boundary. See [configuration](../api/configuration.md).

Core restart requires a current Emby administrator or an owned application key.
It returns HTTP 200 without an invented success DTO. Callback absence and a
previously accepted request return explicit Goby conflicts. Native and core
requests share one pending latch. CanSelfRestart reflects the real generation
owner. Acceptance does not prove that the replacement generation is ready.

Playback activity is written only with the committed playback transition.
Duplicate reports, denied actors, expiry cleanup, and accepted remote commands
do not manufacture additional events. Current user and item names enrich reads;
deleted entities retain their original event identifiers. Known current media
types select the corresponding audio/video compatibility type. An unavailable
classification uses a neutral action name. Recent stops show real user, item,
and timestamp data, without invented viewing time or device history.

Migration 0076 extends existing typed activity constraints and adds a unique
observation index. Its trusted backup catalog was exported from a newly created,
empty PostgreSQL 17 database after applying every embedded migration. The
catalog was not derived by editing a previous baseline.

Broader migration regression also found that the newly introduced schema71
backup migration assumed a terminal CHECK name that schema28 explicitly allowed
to vary. The unreleased schema71 migration now discovers the unique exact
referenced-column set, preserves the real name and complete predicate, and
rejects absent or ambiguous checks. Published migrations 1 through 68 are
unchanged. Catalogs 71 through 76 were then regenerated from fresh PostgreSQL
schemas. Historical data, repeated migration, direct invalid inserts, and
rollback remain explicitly checked.

## Remaining core contract boundaries

These precise operations were not established as public core contracts. This
does not establish that no private implementation can provide related behavior.

| Requested distinction | Verified boundary | Product decision if desired |
| --- | --- | --- |
| Separate image-type locks and an atomic multi-image draft | General metadata locks and individual image writes expose neither exact operation | Define lock ownership, reset behavior, and multi-image transaction semantics. |
| Independent task enable with preserved triggers | TaskInfo and TriggerInfo have no writable independent Enabled field | Define disable, manual-run, and trigger restoration semantics. |
| Stable directory-to-volume grouping and directory quotas | Environment returns path entries and writeability checks | Define supported platform identity and quota mechanisms. |
| Internal database/schema diagnosis | SystemInfo has application/OS/path information; SQLite tuning fields are not PostgreSQL diagnostics | Select the Goby-specific diagnostic fields to expose. |
| Writable URL base path | The inspected ServerConfiguration has no BaseUrl/BasePath field; DashboardSourcePath is a filesystem location | Define proxy, asset, cookie, streaming, and redirect prefix behavior. |

Long-term reporting plugins, plugin delivery transports, plugin backup-diff
parity, and plugin update management are excluded. Supplying a Goby release
source and replacing a Docker image require deployment configuration; the
implemented checker exposes this state directly.

Hardware inventory remains limited to the already qualified software and
VA-API delivery scope. Additional hardware backends require actual engine and
device qualification and a supported image profile. A compiled interface or an
upstream configuration field is not evidence that a device can perform the
requested encoding operation.

## Verification

Local verification is authorized by the original task. Windows ran both
TypeScript/Vite production builds, the sixteen frontend unit tests, and the
browser suites. Local WSL Debian supplied Go 1.27.1, PostgreSQL 17.11, FFmpeg
9.0.1, and ext4 media fixtures. All repository Go packages compiled for the
declared Linux target.

The final selected backend result is 335 parent tests and 1760 subtests passed,
with no unresolved failures. It includes the full updates, config, database,
activity, and settings packages plus affected library, server, and actual
PostgreSQL backup integration tests. A real HLS configuration test was rerun
with the explicit FFmpeg paths after its initial environment skip. Three
pre-existing opt-in index-performance observations were not enabled; their
selectors and skip reasons remain in the raw evidence.

An exploratory native Windows Go build exposed existing Linux-only host-status
functions. Windows native binaries are outside Goby's declared Docker/Linux
delivery boundary. This attempt is recorded separately and is not reported as
a supported-target build success.

The final real three-application flow passed all 24 checks in one entirely fresh
nonprivileged database. A separate fresh environment passed five complete
first-run UI checks: UI administrator creation and automatic authentication,
the actual authorized-directory/library drawer and scan, real language preview
and save from revision1 to revision2, confirmed logout with cleared credentials,
and a new login that read back the retained library, scan, and policy. These 29
checks used the final embedded binary and production assets, with seven actual
playback commands, two committed playback lifecycles, the core restart, nine
screenshots, and zero browser errors. Both fresh databases had schema76 with
76 migration records. All four task application/proxy processes were stopped.

Earlier failed and resumed attempts remain as historical evidence. The final
fresh result does not depend on chaining a partially successful setup attempt.

The final embedded Linux binary was rebuilt from captured unchanged production
inputs. Its backend production inputs match the verified Go suites, and both
real environments served the exact new production administrator entry:

- Binary SHA256: `2a156ff7455d7b6504164e07fb0c51494aab5249635d7de26ba9aeb6518254d6`.
- Administrator entry SHA256: `95e77bd3c8f118fabb524281516ca2426afdac78039afb8246c6fcb5fae1c69d`.
- Production input fingerprint: `9c06fade93e28e4048a66f253a0da62a2c26185f13be9dd7fb2b7522088a0ad3`.

Five embedded-resource tests with nineteen subtests additionally passed against
the final assets. The original verified binary and its source receipts remain
available alongside the final build.

Evidence is retained outside the repository under the task directory
`C:\Users\moooyo\.codex\tmp\goby-admin-core-api-20261106`. Earlier failures,
the superseded catalog exports, the prior live database, source, and raw
evidence remain available. Shared compiler/module caches were reused.

The earlier completed browser loop applies only to its historical fingerprint.
The first follow-up round passed all 233 browser cases but required two
verification corrections. Round2 passed and actually reviewed all 237 images.
Round3 also passed its browser cases but independent image inspection found
an embedded preference footer exposing body text underneath and a fourth
server-name mode hidden on a phone. Both findings reset the clear-round count.

The embedded preferences now have one bounded body scrollport and a separate
opaque footer, with native keyboard scrolling kept clear of the footer. The
phone name-source selector shows all four choices in two rows while desktop
retains its single row. Thirteen new behavior and geometry checks cover the
fixes. Rounds4 and 5 each passed all 246 browser cases with zero skipped,
unexpected, or flaky cases. Each round actually reviewed all 254 newly captured
images through eight disjoint inspection partitions, including original-size
checks of the corrections. Both recorded zero new modification recommendations.
The final loop result is consecutiveClearRounds=2 and exitConditionMet=true.
Its source-and-production fingerprint is
`73a9c4efb3eb67493ed0437900770e89a22ffbceb13213a56b1766f743969ebf`.

The final receipt is `verification-closeout-final.json` in the external evidence
directory. It checks the complete screenshot coverage, live-served index,
binary and runner hashes, real verification, backend results, and loop status.
The task PostgreSQL cluster and production preview were stopped after the last
capture; all six task ports have no remaining listener. Source, data, raw
evidence, ext4 fixtures, and shared caches remain available.
