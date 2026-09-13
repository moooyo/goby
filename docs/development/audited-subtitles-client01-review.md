# Subtitles01 result and remaining acceptance gaps

Status: [owned state and workers are closed](audited-subtitles-client01-closeout.json);
formal client acceptance and complete media interpretation remain open.
The single reviewed input is consumed. The original client completed SRT/VTT
selection, cue visibility, seek alignment, return to Off, stop and logout.
The browser and gateway exited with code 0. Formal closeout remains
`client_ui_or_cleanup_incomplete`; this is not a passing subtitle checkpoint.

## New observation and its limit

The native channel captured two original rejection reasons of primitive
undefined at 17001 and 17030 ms. Collection is complete for its one installed
document context, with no overflow or observer failure. Neither event contains
a Response or request association. They remain unattributed.

Nearby browser requests 280/282 attempted the external device-validation URL.
Physical CONNECT 282/284 was rejected by the existing gateway boundary without
an upstream connection or written bytes; no remote HTTP status was received.
An earlier equivalent external request also failed without a nearby pageerror.
Timing does not prove causality or harmlessness. The minimal native observation
has answered its type question; adding another diagnostic layer or repeating
video is not an automatic next step.

## Subtitle and state evidence

The recorded opening SRT cue is visible before 5 seconds. After seeking,
both SRT and VTT show the forward cue at 121.242 seconds within the 118–128-second
fixture window. Restoring Off removes visible cues and disables both tracks.
Physical subtitle exchanges 289/297 completed 200 `text/vtt` responses with 187
and 220 delivered body bytes. Browser-observed body hashes and cue inspection
are retained separately: the physical gateway intentionally excluded these
media bodies, so no independent physical body hash reconstruction is claimed.

The snapshots retain every previous row. New auth
`c31c22797d087807b4c6753502941dbc` is revoked; one counted Stopped play and one
uncounted Prepared play share that auth and the declared movie. Userdata count
is 1 with position 1212420000 ticks. Twenty-one sessions are revoked; thirteen
plays, six userdata rows, two retained audio references and no encoding jobs
remain. The Prepared row comes from later detail PlaybackInfo and stays retained.

## Separate unresolved media timing

Three partial video responses are owned by the recorded playback chain. The
existing abort interpretation accepts 281 and 294. Exchange 293 delivered 163840
body bytes, but its candidate browser end signal is 3507.211510 ms later than
the physical completion, outside the unchanged 2000 ms limit. Browser ordinal 291
matches the range of both 293 and 294, and its end signal is close to 294. These
facts do not establish why 293 ended or prove an automatic retry.

The first saved-only reconciliation stopped at this timing mismatch. Preserve
that failed receipt and the original client failure. The second saved-only
reconciliation independently passed persistent-state cleanup and physical
request ownership, while retaining the failed cancellation interpretation.
The checkpoint keeps `mediaInterpretationComplete=false` and `clientAcceptance=false`.
Its SHA-256 is `2951644a4492e0e32f2c0b3b0e18025ec65f21f7114ce7c95b63350bcda9f3b8`.

The permitted [main read-only identity preparation](audited-main-readonly-identity.json)
has now observed the inactive main installation and matching source32 binary.
Video and subtitle acceptance remain open,
and main promotion still requires the core gate plus fresh recovery/restoration
proofs. No new browser input, network change, main start or automatic diagnostic
expansion follows from this result.
