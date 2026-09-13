# Movie06 closeout and execution-plan review

Movie06 consumed its [single execution decision](audited-movie06-execution-decision.md).
The [owned-state closeout](audited-core-movie06-owned-state-closeout.json) passed,
but the intended playback diagnostic was not reached. Keep browser execution
paused during this review. No movie07 or other new browser input or execution
is authorized by this review; each subsequent increment needs its own concrete
execution decision.

## Observed result

The version-3 integration passed [71 remote checks](audited-client-v3-verification.json)
and a real read-only stdout capture before movie06. The actual run then retained
its before/after stdout successfully despite browser exit 1. The run failed at
`movie_detail_title` with `movie_control_not_unique`: the item URL had changed,
while the old Home controls were still visible. Its two movie titles belonged
to Continue Watching and Latest Movies. The 59 controls in the failed and cleanup
snapshots exactly matched the earlier Home list; there was no detail Play,
Resume or From Beginning control in those snapshots. This is evidence of a
transition wait bug in the test helper, not duplicate detail-page headings.

The later successful PlaybackInfo exchange did create an unstarted Prepared
row and expire the previous unstarted row. The closeout binds both changes to
physical exchange 254. It verifies all 35 tables and sequences: 31 tables and
the complete userdata row are exact, the five older plays remain present, the
two counted Stopped plays remain exact, and the retained count and position
remain 2 and 1,217,878,390 ticks. The sole new session is revoked; all fourteen
sessions are revoked, no playback reference or encoding job remains, and both
workers are closed. The ledger has 263 rows, including a CONNECT handshake and
a rejection before upstream connection; it is not 263 ordinary HTTP exchanges.

No Playing report or media request occurred. Zero page errors before playback
does not resolve the four movie05 errors. Their original causes remain unknown.
The old failed results, snapshots, logs and frozen inputs remain preserved.

## Plan corrections

The delivery order remains product correctness, candidate admission, core client
acceptance, main recovery/upgrade, and the remaining M2-M6 release matrix.
Product verification and candidate admission are complete. Core acceptance and
the main upgrade are still open. Status documents must distinguish these gates
from tool verification and owned-state closure.

The movie readiness increment is now [verified](audited-movie06-readiness-verification.json).
The new regression reproduces the original `movie_control_not_unique` failure
against the unchanged source; the correction passes all 19 lifecycle tests on
`test-env`, with no browser or business action. The live controller's frozen
source selection has not been changed. The following boundaries separate this
completed correction from the remaining movie-only prerequisite:

1. Treat the read-only movie title as a visibility/count diagnostic. Keep the
   observed item URL binding and actual action-control uniqueness. Wait for the
   actual initial Play/From Beginning or relogin Resume control before arming
   or clicking playback. Reproduce the saved URL-before-DOM transition in the
   complete synthetic workflow, including duplicate Home titles and delayed
   action controls; also retain rejection of duplicate actions and another
   relogin item. Existing history uses From Beginning for the initial lifecycle.
   This correction and its targeted regression are complete.
2. Before any later movie execution, make the existing version-3
   `retainedBaseline` input carry an explicitly
   reviewed closed baseline rather than introducing a code constant for every
   new session/play ID. Preserve full snapshot equality, source epoch and actor
   binding, all revoked credentials, zero references/encoding, older terminal
   rows, exact userdata and pruning bounds. Only an explicitly identified
   unstarted Prepared row owned by a revoked session may expire in its matching
   successful owned preparation window. This contract change is still pending;
   the current v3 implementation remains bound to movie05 and cannot run against
   the movie06 state.

For that movie-only adjustment, use the existing controller and closeout, with
matching negative tests in both languages and the actual saved movie05/movie06
states. No new runner, generic
failure-recovery framework, seed reset, duplicate actor, product-wide test run
or repeated reconstruction of historical roots is needed. All verification
continues through `ssh test-env`.

The remaining movie prerequisite is complete only with a reviewed source diff
and a passing bounded remote regression with saved-state replay. That is a
tooling checkpoint only. Before another movie attempt, record why it can reach
the missing page-error observation, the frozen
source/input/baseline, expected outcomes and the existing request/time/cleanup
bounds. A new identifier alone supplies no reason to retry. If offline replay
cannot establish those prerequisites, stop at the documented gap and reassess
the observation method instead of allocating another browser attempt.

## Independent core work proceeds first

The five other scenarios do not call `runMovieWorkflow`. The existing v3
controller and closeout already support their separate ordinary actors with
`retainedBaseline=null`, protecting all foreign rows. They have no functional
dependency on movie acceptance or its pending baseline adjustment. Therefore
the next concrete core increment is TV browse, followed by MP3 and FLAC; prepare
episode playback after the TV navigation result, then subtitles according to
their own video/detail prerequisites. Reuse the existing runners, media,
credentials and verified relevant source. Before each run, freeze its actual
source/input, fresh actor/state/runtime facts, expected result and existing
request/time/cleanup limits. Serialize all state-changing scenarios.

If shared login, observation or cleanup tooling blocks a new scenario, pause
the affected paths for saved-evidence review; do not use a different scenario
as an automatic retry. Identity drift, unexplained state changes or unresolved
cleanup stop subsequent state-changing work until reconciled.

Movie06's retained state is foreign state in those runs and must remain fully
protected. Close each run's actual state before another scenario. Do not require
new movie-specific baseline code merely to prepare these independent scenarios.
A successful TV/audio result closes only its own gate; it neither explains the
movie page errors nor admits main promotion. This order avoids a fresh movie
actor, which would hide the initial duplicate Home cards while retaining the
same relogin/navigation problem and adding seed/identity work.

Main-upgrade preparation can continue as read-only source/document work; its
execution still needs all core evidence and two isolated recovery proofs from
one fresh archive. Global NextUp and refresh research remain parked; their
feature requirements and all M2-M6 work remain in scope.
