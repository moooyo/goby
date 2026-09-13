# First episode playback on the TV successor

Status: **consumed; owned state closed, formal client acceptance failed**.
The [entry review](audited-episode-client01-entry.json) passed before the one
execution. The [closeout](audited-episode-client01-closeout.json) and
[result review](audited-episode-client01-review.md) now control the next action.
Three page errors keep episode and overlapping TV browse acceptance open.
Retain this entry contract as history; do not execute it again.

The run reused the selected product and verified client bundle. At entry the
episode actor was unused. The source baseline was the closed FLAC snapshot,
SHA-256 `94bedf2d19f42bf80f87df27237914dd71dd210c1dbf049d389faa7be6342ba4`:
nineteen revoked sessions, nine plays, four userdata rows and two retained
foreign audio references. Preserve all that history and both database identities.

The existing episode scenario first runs the full TV browse helper: library,
series, both seasons, cross-season list, Episode2-1 detail and return Home. It
then revisits the recorded detail URL and plays that exact episode. Require
advancing video time and decoded frames, pause/hold, seeks to30% and10%, resumed
progress, visible stop/back and logout. Preserve exact actor/item/media-source
identity, actual media delivery and complete bounded response/state evidence.
No unresolved page error or unowned state change is allowed.

The episode scenario explicitly includes the complete TV browse journey. After
successful overall closeout, assess that recorded browse portion separately
against the existing browse checks. Complete overlapping evidence can close
the current browse behavior without replaying the consumed TV-browse01 input;
the original failed result and its unknown historical error remain unchanged.
A failed or incomplete playback run does not automatically pass another gate.
Movie cross-login resume and external subtitles are not covered here.

The input `candidate-episode-client01-entry-review-01/input.json` beneath the
retained delivery root has SHA-256
`147c49b0fa8d9499ce1712300f19493fb9ca56992b443f8576b94fa20ffea27d`.
The review SHA-256 is
`4c57f3f74546cca726b2f5ab04e5915ff4107e46977627177fa200af9dcf415b`.
At `2026-09-13T16:23:35.555436Z`, all35 source tables/sequences match the FLAC
closeout; the actor has no play/reference/userdata. Candidate PID486706,
PostgreSQL PID363520, lease and hosting remain exact; new output/units are absent.
The review issued no business HTTP or service operation.

This one `episode-01` input ran on `test-env` using the unchanged1,200-second
controller/240-second cleanup,600-second browser and existing gateway bounds.
Close all actual owned state before another business scenario. No account
reset, rescan, media preparation or automatic retry is authorized by this input.
