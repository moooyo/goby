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

## Read-only response identity review (2026-09-14)

A bounded review of the saved observation, gateway records and server log found
a response identity distinction that the Range comparison alone does not show:

| Record | Response `X-Request-Id` |
| --- | --- |
| Browser context request 291 | `9bc31586fdf4e0dd3762da11cdee3978` |
| Physical exchange 293 | `07fabfc745fc6f305c9d5b694616a6f7` |
| Physical exchange 294 | `9bc31586fdf4e0dd3762da11cdee3978` |

The response observed by context request 291 therefore identifies exchange 294,
not 293. The two physical requests have byte-identical original request heads:
931 bytes each, SHA-256
`1ef7a2dd69a21101b2e50089c226f6080abcda06ab3c7a8176fa4103baf5d602`.
Exchange 294 started 1.816665 ms after exchange 293 ended, using their recorded
monotonic timestamps. These facts raise one narrower question: was 293 an
intermediate physical attempt whose response was not exposed through the saved
BrowserContext event? They do not establish an internal retry or its cause.
The end signal of context request 291 cannot directly explain the termination
of exchange 293.

The saved server log at line 714 independently binds request ID
`07fabfc745fc6f305c9d5b694616a6f7` to a 206 response with `duration_ms=11`,
`outcome="aborted"` and `bytes=3438891`. Goby's request logger uses `aborted`
for escaped panics and writer failures; the original stream can also raise an
abort after its work context is cancelled. That outcome does not identify which
endpoint or operation initiated cancellation. The logged byte count measures
bytes accepted by the server's response writer. It is distinct from the
gateway's 229376 response body bytes read and 163840 body bytes delivered to the
client, and does not establish complete delivery.

Static inspection of the existing `matchesContext` and `mediaContextEvidence`
functions confirms that they form the 291-to-293 candidate from matching method,
URL, token, Range and request-start timing without comparing response request
IDs. The later `explainMediaPartial` timing check rejects that candidate. Thus
the saved association is a Range candidate, not proof of response identity or a
passing cancellation interpretation. This review changes no checker code and
establishes no product defect; it does not reinterpret the original failed
closeout as successful.

The following saved files were read with their SHA-256 pins verified. Every
relative path in the table is under
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-core-client-subtitles-01/`.
The request-head hash above is for the decoded head bytes, distinct from the
containing intent-file hashes below.

| Saved evidence | SHA-256 |
| --- | --- |
| `browser/observation.json` | `33421acc17a8994da3b4d0798a48c75d81154875745793337f84ef11f5115f32` |
| `gateway/private/request-000293-intent.json` | `bce2f34a8df7c7d437ea6dfcd7bda7512041fda019e7a010e4cb8d73ffe77aca` |
| `gateway/private/request-000293-result.json` | `a18939de2f6ed695817dbf6ebb3f93d64f5d2b28477c056b35780849e7e6bff9` |
| `gateway/private/request-000294-intent.json` | `4e12b0bfcfb93c43a3c45c7559c42f5edd5e8531a7e5cf98cd0eda31d9fc90a0` |
| `gateway/private/request-000294-result.json` | `70da8e5a1b12f9fb9a21da96c4138ce3d0180e0215a32a4cbd6154d718f1fec1` |
| `private/server-log.json` | `6722116123aed86cb15e38065a47f649f3336b361241a33641bfec6d6c5d8651` |
| `private/server-stdout-after.raw` | `2c93e59e3c050d51e906a3a52d78e962c6c10be49e6ff2c6f33a2945836eb486` |

Both original page errors remain unattributed. Exchange 293 still lacks a
complete cancellation interpretation, its original timing failure is retained,
and formal subtitle acceptance remains open. This review made no business
request, browser run, database access or service change and does not authorize
another observation layer or execution.
