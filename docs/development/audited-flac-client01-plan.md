# First FLAC client increment on the TV successor

Status: [FLAC passed and owned state is closed](audited-flac-client01-closeout.json).
The selected source, runtime and client bundle are unchanged from the passed
[MP3 workflow](audited-mp3-client01-plan.md). Reuse their verified tools and
the existing FLAC actor and 180-second fixture.

The visible journey is login, Music/album/selected FLAC track, play, advancing
playback, pause, seek forward to65%, backward to30%, resume, stop and logout.
Require owned media delivery, progressing playback, correct durable counted
Stopped state and userdata, isolated user changes, no unresolved page errors,
same-token rejection and closed owned workers. MIME and media-element evidence
do not prove audible output or an independently measured output codec.

Bind all35 source tables and sequences to the MP3 `source-after.json`, SHA-256
`9e634223dd44c6e5f5d30a8deb0bf1642a22381467ed6efa3a2de737c1aec515`.
It contains eighteen revoked sessions, eight plays, three userdata rows and
one retained universal-audio reference. Preserve that foreign reference; it is
not an active worker. The FLAC actor has no prior play/reference/userdata row.
No account reset, rescan or media preparation is permitted.

The input `candidate-flac-client01-entry-review-01/input.json` beneath the
retained delivery root has SHA-256
`6611e05990ab3508cd02ef246b3480f8239764bbd0ad6ef920e1e377654d87fc`.
Its review SHA-256 is
`f6e59f015ca721afbd54143458b7824595ce36894ab9ee4afe62da8797eb6d9c`.
Fresh review at `2026-09-13T16:13:50.240039Z` confirms the exact source, actor,
candidate PID486706, PostgreSQL PID363520, lease and hosting; output and worker
units are absent. The review performed no business HTTP or service operation.
The input was then executed once and is consumed.

The original client completed the workflow with zero page errors. Formal
closeout reconciles287 physical exchanges, one login and one counted Stopped
lifecycle. Media260 completed HTTP206 with3,127,772 FLAC body bytes forwarded.
Started264, Progress270-275 and Stop276 match durable state; logout283 and
same-token401 at285 close the credential. Userdata count is1 and final position
is553423150 ticks (55.342315 seconds).

Browser PID490906 and gateway PID490898 are absent, both cgroups empty, and
their units inactive with exit0. Candidate and PostgreSQL remained continuous.
Nineteen sessions are revoked. Twenty-nine tables remain exact; the six changed
tables each add one owned session/device/play/userdata/reference row, with two
audit entries. All older rows, including the MP3 audio reference, remain exact.
The final snapshot is
`94bedf2d19f42bf80f87df27237914dd71dd210c1dbf049d389faa7be6342ba4`.
It contains nine plays, four userdata rows, two retained audio references and
no encoding job. Device/audit sequences advance by1/2; other sequences are exact.

Keep the existing 1,200-second controller/240-second cleanup budgets,
600-second browser limit and unchanged gateway bounds. Execute only this one
new `flac-01` input on `test-env`, then close its actual owned state before the
next scenario. Do not rerun a consumed input automatically. Success closes only
the FLAC row; episode, subtitles and retained movie/TV gates remain separate.
