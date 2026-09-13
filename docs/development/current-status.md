# Current implementation and delivery status

Reviewed on 2026-09-13 after TV browse01. The [execution plan](../planning/current-execution-plan.md)
is the active queue. Complete M2-M6 delivery remains in scope; M7 is deferred.
Historical handoffs, PIDs, experiment inputs and verification receipts retain
their original meanings and are not fresh deployment observations.

## Current gates

| Gate | Accepted result | Next required result |
| --- | --- | --- |
| Product correctness | R01-R21, diagnostics, cancellation fixes and TV parent metadata are verified; the latest full run passed 2,270 tests/25 packages and a Linux build | Complete affected live admission of the installed TV metadata successor |
| Audited candidate | Prior binary/configuration and live admission04 passed; the verified TV successor is installed with exact retained state | Check changed TV projections and authorization before resuming client work |
| Core original client | Movie05 playback and TV browse01 navigation observed; both formal results failed and owned states closed | Complete strict movie/TV acceptance, episode, MP3, FLAC and external-subtitle scenarios |
| Main deployment | Old source32/schema27 installation retained; new upgrade contract is a draft | Fresh recovery point, isolated forward and old-binary restoration, core acceptance and bounded promotion |
| Complete release | Implemented foundations and historical scoped controls | Remaining M2-M6 capacity, operations, media, hardware, packaging, license and feature evidence |

## Selected product and candidate

The [audit remediation](audit-remediation-20260913.md) is published as `a623375`.
The [exit diagnostic increment](exit-diagnostics-20260913.md) preserves known
lease, panic and listener failure classes; it does not identify the historical
unexpected exits. The later [restore cancellation fix](restore-cancellation-race.md)
is included in the selected binary. Its [full remote verification](restore-cancellation-full-verification.json)
passed 2,264 tests across 25 packages with race instrumentation and a Linux amd64
build, with zero failures or skips. The 57 frontend assets reuse 41 mocked-API
cases from 64 unchanged source files.

The isolated candidate is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`, binary SHA-256
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`.
Its earlier [binary transition](audited-candidate-cancellation-transition.json) and
[backup-limit correction](audited-candidate-backup-capacity.md) preserved all
35 source tables. The [seed closeout](audited-candidate-seed-closeout.json)
binds eight accounts, three libraries, fourteen media files, thirteen stored
items and ten public catalog entries. Seed, scans and hosting were not rebuilt
for later checker failures.

[Live admission04](audited-candidate-live-admission-closeout.json) passed a full
ten-minute window, 79 complete responses, a 290,550-byte backup download,
ready-plan cancellation and exact owned cleanup. The inactive staged recovery
database remains retained; it is not an empty slot. This is admission evidence
for the preceding `477d26ad...` binary. The [original-client host initialization](audited-original-client-host-startup-closeout.json)
is also closed, with an empty hosting library and its setup credential revoked.

The [TV successor transition](tv-parent-candidate-transition-closeout.json)
completed exactly one stop, replacement and start after 103 remote tool checks.
New epoch `76d7cc71...` runs PID 486706 and awaits affected live admission.
All source and inactive tables/sequences, fifteen revoked sessions, seven plays,
two userdata rows, controls and media remained exact. The PostgreSQL process,
configuration and original-client hosting remained unchanged. Old admission04
does not independently admit the new binary.

## Movie checkpoints

The [v3 input contract](audited-client-v3-input.md) integrates occupied movie05
history and request-ID-bound server cancellation evidence into the existing
controller/closeout. [Verification](audited-client-v3-verification.json) passed
71 new remote checks plus an actual read-only stdout capture. The unchanged
adapter and movie-lifecycle tests were reused for that exact revision. The
saved movie05 replay now reconciles physical media and durable state, including
an explicitly cancelled zero-byte response that contributes no media delivery.
Its original failed result and four errors with unrecoverable causes remain.

Movie06 consumed its [one diagnostic execution decision](audited-movie06-execution-decision.md).
It failed before playback at `movie_detail_title`: the item URL had changed,
but two movie titles from Continue Watching and Latest Movies remained visible
on Home. The helper incorrectly required a unique title. The new stdout capture
worked even with browser exit 1. No Playing or media request occurred, so zero
page errors in this run does not resolve movie05's errors.

The [movie06 owned-state closeout](audited-core-movie06-owned-state-closeout.json)
passed using saved evidence only. All fourteen sessions are revoked and both
workers are closed. Of 35 tables, 31 are exact; the four changed tables contain
one new session/device, two audit rows, one new unstarted Prepared play and the
explained expiration of the previous unstarted play. All older plays remain,
including two exact counted Stopped rows. The complete userdata row is unchanged:
play count 2 and resume position 1,217,878,390 ticks. No playback reference or
encoding job remains. The 263-row physical ledger includes 261 complete observed
HTTP exchanges, one CONNECT handshake and one rejection before upstream.

The transition readiness correction now [passes 19 remote tests](audited-movie06-readiness-verification.json),
including a regression that reproduces the original failure against unchanged
source. It preserves real action uniqueness, delayed readiness, separate play
lifecycles and owned logout. The live controller's frozen source selection has
not changed. This is a tooling checkpoint only.

Movie execution stays paused under the [plan review](audited-movie06-plan-review.md).
Before another movie attempt, its per-attempt baseline constants must become an
explicitly reviewed closed-state input with matching saved-state regression.
The current v3 controller remains bound to movie05 and cannot admit movie06's
changed state. No movie07 input or run is claimed. The independent existing
actors do not depend on that movie-only adjustment. TV browse subsequently
consumed its own decision, as recorded below; MP3, FLAC, episode and subtitles
remain later increments after the shared prerequisites. Each needs a separate
frozen decision, serialized execution and complete state closure. Their success
does not resolve movie errors or authorize main promotion.

Earlier failures remain consumed and linked through their closeouts:
[movie01](audited-core-movie01-prelogin-closeout.json),
[movie02](audited-core-movie02-prelogin-closeout.json),
[movie03](audited-core-movie03-failure-closeout.json),
[movie04 alignment](audited-movie-offline-alignment.json),
[movie05 state](audited-core-movie05-owned-state-closeout.json) and
[movie05 evidence correction](audited-movie05-evidence-contract.md).
They establish neither passing client acceptance nor a reason for automatic
retries. Product verification remains reusable because the binary is unchanged.

## Latest increment: TV browse

[TV browse01](audited-tv-browse01-execution-decision.md) completed the visible
library-to-series, two season selections, Episode 2-1 detail, Home return and
logout workflow in the original client. Its observed cross-season list does not
prove strict season filtering. The browser then failed the adapter's
`candidate_item_detail_binding_changed` assertion; one page error remains
unclassified. The [evidence review](audited-tv-browse01-evidence-review.md)
identifies a missing `SeriesId`: seed mapping derived it from parent edges,
while the checker treated it as a required observed response field. Other
identity, parent, index and duration fields matched.

The [owned-state closeout](audited-core-tv-browse01-owned-state-closeout.json)
passed from saved evidence. Of 35 tables, 30 are exact. The new TV session/device,
two audit rows, one unstarted Prepared play and one zero-history userdata row
are fully explained. All fifteen sessions are revoked; both workers are closed;
all six movie plays and its complete userdata are exact. There are no playback
references, encoding jobs, Playing reports or media requests. Candidate,
PostgreSQL, lease and log prefix remain exact as recorded.

The [adapter correction](audited-tv-browse01-adapter-verification.json) passed
55 remote adapter tests and seven saved-response/failure checks. It preserves
requested-item and parent identity, rejects conflicting SeriesId when present,
and permits redacted diagnostics only for the precisely classified
non-credential assertion. The original failed result and unknown page error
remain. The controller still pins the prior adapter; no new browser run has
used the correction.

An existing reference Episode detail also proves a distinct product difference:
SeriesId, SeasonId and their names were absent from Goby's mapper. The
[authorized parent metadata increment](tv-parent-metadata.md) now implements
the observed default Episode and Season fields through the existing read
transaction, with same-library ordinary-parent constraints. Its 21 effective
[focused checks](tv-parent-metadata-targeted-verification.json) passed: twenty
unchanged checks plus a corrected HTTP fixture rerun. The first fixture failure
remains retained. [Full verification](tv-parent-metadata-full-verification.json)
passed 2,270 tests/25 packages with race instrumentation and a Linux build,
without failures or skips. Raw test events, source/archive/manifest and binary
were independently reconciled; the isolated worker/PG are closed. Binary
`b0d6769c...` from archive `b363afdc...` is installed through the completed
[current-state transition](tv-parent-candidate-transition-plan.md) and awaits
affected live admission. This is not a Live TV feature expansion;
the nearby `/LiveTv/Programs` 404 is only a timing lead for the unresolved error.
Affected client runs stay paused until the new admission and client inputs pass.
The consumed TV actor now needs retained-state admission before any future
rerun, just as movie does. No main service change or complete core acceptance
is claimed.

## Main and parked investigations

The host rebooted at `2026-09-13T06:18:45Z`. The [reboot baseline](resumed-delivery-reboot-baseline.json)
found source55 and source32 main inactive with their installed bytes retained,
and PostgreSQL 17 main active. It made no database preservation attestation.
No main/source55 restart or upgrade occurred in this client increment. Old PIDs
and the [pre-reboot source55 recovery](candidate-source55-restart-closeout.md)
are historical, not current live identities.

The [replacement main upgrade contract](audited-main-upgrade-plan.md) requires
one fresh native archive and two distinct isolated restorations: new binary to
schema28, and actual source32 binary to schema27. Its source review identifies
an existing native HTTP backup-creation path; fresh authority, startup effects
and bounded ownership must be reviewed before using it. No main operation is
admitted by this draft or by candidate admission alone.

Global NextUp and automatic-refresh research remain parked. The
[matrix07 record](nextup-global-reference-matrix-07.md) contains 158 requests and
ten empty global results. Two [original-client discoveries](nextup-client-discovery-closeout.md)
issued no physical NextUp request. Existing preservation closures remain valid
for their original scopes; another run does not freshly reverify all 198 roots.
The [Goby comparison contract](nextup-goby-comparison-contract.md) requires new
discriminating evidence or a concrete product decision before reopening.

## Remaining release work

| Area | Remaining acceptance obligation |
| --- | --- |
| Foundation and recovery | Main migration, forward restore, actual old-binary restoration and bounded post-upgrade workflow |
| Catalog and operations | Representative capacity, blocked storage, reboot and filesystem measurements |
| Playback | Complete pinned original-client journeys, broader direct-play/transcode formats, seeks and subtitle cases |
| Administration | Selected policy, executor and provider extensions from the delivery plan |
| NextUp and refresh | Positive selector/ordering/client behavior and automatic-refresh evidence for those feature claims |
| Hardware | Actual GPU decode, encode and combined-path profiles |
| Packaging | arm64, OCI, embedded assets, support rows, project license and dependency notices |
| M7 | Deferred until explicit feature selection and separate acceptance |

A partial internal deployment does not close M2-M6 or claim broad compatibility.
Missing hardware blocks that profile; it does not block unrelated software work.
License and notices must be resolved before external distribution.

## Verification policy

All compilation, formatting tools, tests, browser checks, media probes and runtime
verification use `ssh test-env`. Local verification requires explicit permission
in the current task. An unavailable test environment blocks verification; it
does not authorize a local fallback. Reuse unchanged verification and limit new
checks to the changed risk. No product-wide rerun is needed for these tool edits.

## Supported capacity boundaries

Missing-item reconciliation currently requires a complete proof within 4096
directory handles, 262144 entries, 131072 seen IDs and 64 MiB of observation
storage. Its SQL candidate/closure budget is separately 32768 rows and 32 MiB,
with per-row overhead that can reach the byte limit first. These budgets cover
the library, not only the deleted files. Exhaustion retains missing records and
reports a skipped cleanup while additions and updates may still complete.

Global NextUp query cost and concurrent scan/homepage latency have not been
accepted at a representative large-catalog scale. Neither a small API page size
nor the passing correctness suite establishes that capacity. Existing events
are not durable; supported clients must refetch state after reconnect.
