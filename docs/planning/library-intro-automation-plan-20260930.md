# Automatic episode intro workflow

Status: **COMPLETE within the selected automatic library intro scope**.
Baseline: `40e02bb`. The
[result record](../development/library-intro-automation-20260930.md) binds the
accepted source, scoped checks, actual Docker workflow, image identities and
successful owned-resource closure through its machine-readable receipt.
Git integration is separate; no merge or push is claimed here.

The user selected one normal workflow: enable intro detection in a TV library,
analyze eligible episodes in the background, and expose qualified results to
playback. Progress and failures remain visible. Human candidate approval,
rejection and manual correction are not part of the product workflow. An
uncertain or absent match produces no automatic intro marker.

## Finite scope

- Add the TV-library `EnableIntroDetection` option, defaulting to false. Existing
  library rows retain their settings. Expose the option in library creation and
  editing, with a supported Emby library-option mapping.
- Enabling detection requests background work. Completed independent and
  task-owned scans request another pass through a dedicated durable event.
  Install a default daily task and event trigger without overwriting existing
  administrator schedule choices. Automatic runs select only enabled TV libraries.
  Normal shutdown and crash recovery of automatic work persist fresh requests;
  old execution claims are not replayed, and explicit user stops do not auto-retry.
- Publish qualified results automatically. Disabling detection withdraws the
  library's effective detected markers; existing explicit chapters and historical
  manual/imported markers remain compatible. Keep old API and audit data, but
  remove human correction controls from the administration workflow.
  An explicit false-to-true library transition retires legacy rejection flags
  while advancing decision revisions and preserving decision/audit history.
- Retain the existing matcher, thresholds and first-600-second extraction policy.
  This increment does not change the three-independent-episode support threshold
  or claim broader accuracy across new series.
- Retain background progress, stop and failure reporting. Keep preview generation
  available without coupling it to the TV intro option.

## Verification and delivery

The selected backend composition passed 135 parents: database 10, library 64,
tasks 30, server 10 and backuppg 21. The UI campaign passed 27 checks with one
historical full-Phase-3 skip; Node passed 11 checks with zero skips.
Overlapping or replaced executions are not added as new tests. Original failures
remain retained, with affected scopes rerun separately.

Actual Docker `runtime01` passed using only the library option to start intro
work: three reused positive episodes qualified and PlaybackInfo matched their
intervals; two singleton sources abstained without markers; a disabled control
library never published. Rescan caused a second system-event run. Disabling
withdrew markers immediately, and container recreation preserved that state.
No analysis-start or candidate-decision request was used. These old samples are
not a new holdout population.

Updated software/AMD application archives passed export/import and were downloaded
with inherited base layers unchanged. The AMD image also passed application and
actual UI checks using software axes; there was no new GPU campaign. The first
two UI locator failures remain recorded; only the fixture needed correction.

Final closure left zero owned containers/Compose networks, stopped the dedicated
PostgreSQL on port 55996 normally after zero clients, closed HTTP port 38963, and
retained private data/evidence. The three protected services retained their
original IDs, PIDs and start times.

Delivery is under `D:/Code/goby/.artifacts/intro-automation-20260930`: the software
and AMD archives occupy their profile directories, with
`goby-docker-operations.zip` at the root. The result manifest binds final
toolkit/catalog identities. Git publication is recorded separately. Online
scraping and historical capacity/fault campaigns stay outside this increment.
