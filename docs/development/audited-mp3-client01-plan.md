# First MP3 client increment on the TV successor

Status: [MP3 core scenario passed and owned state is closed](audited-mp3-client01-closeout.json).
The selected
candidate passed [affected admission05](tv-parent-affected-admission-closeout.json).
The [saved-lineage reader correction](tv-parent-client-lineage-review.md) and
final component/controller selection are verified.

The input at `candidate-mp3-client01-entry-review-01/input.json` beneath the
retained delivery root is frozen, SHA-256
`0efd74a9edb9841ded6580de380a752455be4bc542b1b266288b75d0b7f4ef92`.
The entry review SHA-256 is
`798993b0bdcdf0379cd96fee6e809868627447834be0daf74664d69cfa44afc9`.
At `2026-09-13T16:02:33.734418Z`, all source tables and sequences matched the
admission05 closeout, and the actor remained unused. Candidate PID 486706,
PostgreSQL PID 363520, the lease and hosting remained exact. New output and
worker units were absent. The entry performed no business HTTP or service work.
This input was then executed once and is consumed.

The original client completed the workflow with zero page errors. The physical
closeout reconciles 287 exchanges, one login and one counted Stopped lifecycle.
Media ordinal260 completed an HTTP206 `audio/mpeg` response with 2,880,702 body
bytes forwarded. Started263, Progress270-275 and Stopped276 agree with the
durable record; logout283 and same-token401 at285 complete session closure.
Userdata records count1 and position553468840 ticks (55.346884 seconds).

The browser PID489444 and gateway PID489436 are absent, their cgroups are empty
and both units are inactive with exit0. Candidate PID486706 and PostgreSQL
PID363520 remained continuous. Eighteen sessions are revoked. Twenty-nine
tables are unchanged; the six changed tables contain exactly one new session,
device, play, userdata and playback-reference row, plus two audit rows. All old
rows remain exact. The universal-audio reference binds this stopped play and
revoked credential; retain it as history. The device sequence advanced by one,
the audit sequence by two, and the other three sequences remain exact.
The resulting snapshot SHA-256 is
`9e634223dd44c6e5f5d30a8deb0bf1642a22381467ed6efa3a2de737c1aec515`.

Use the existing `mp3` actor and MP3 item in the original seed binding. Reuse
the pinned original client, audio helper and media fixtures. The actor must
still have no play, playback-reference or userdata row. Bind the whole current
source snapshot to admission05's `source-after.json`, SHA-256
`61a4e525952c959912ab0ae4bb4c253d879a854f8a9ff041f4fe30706c4d438d`,
with seventeen revoked sessions, seven foreign plays and two foreign userdata
rows. No actor reset, new account, scan or media preparation belongs here.

The visible workflow is login, open Music and the selected album/track, start
the 180-second MP3 fixture, observe advancing playback, pause, seek forward to
65%, seek backward to 30%, resume, stop and logout. The existing helper owns
the exact control and timing checks; the controller and closeout retain the
already verified request, credential, media-response and durable-state gates.
Require actual owned media delivery and progressing playback, correct stopped
state and userdata, user isolation, no unresolved page errors and complete
owned worker/session cleanup. HTTP MIME and HTMLMediaElement observations do
not claim a human audible-output check.

Keep the existing controller budget of 1,200 seconds including 240 seconds for
cleanup; the browser remains bounded to 600 seconds plus its existing cleanup
allowance. The existing gateway request/body/time limits remain unchanged.
Execute only on `test-env`. First recheck the selected process, lease, hosting,
whole source state, fresh actor and absent output/worker units. Freeze one new
`mp3-01` input; never rerun a consumed input automatically.

This success closes only the scoped MP3 core-client row. Any later failure retains the
original observations and actual owned state for reconciliation before another
business increment. FLAC, episode playback and subtitles remain independent
later increments. Movie and TV browse retain their consumed actors and open
acceptance gaps; this MP3 run cannot pass those scenarios.
