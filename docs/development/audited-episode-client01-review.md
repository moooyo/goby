# Episode01 closeout and next diagnostic decision

Reviewed on 2026-09-14. Status: **owned state closed; episode and TV browse
acceptance open**. The input is consumed. The selected product, candidate,
media, hosting and network boundary remain unchanged.

## Result and preserved state

The episode run completed TV library/series/season/detail navigation and video
playback: advancing time/decoded frames, pause/hold, seeks to 30% and 10%,
resumed progress, stop and logout. Browser and gateway exited with code 0. Offline closeout
correctly rejected `client_ui_or_cleanup_incomplete` because three page errors
remain unresolved. The original observation and failed closeout are unchanged.

The [owned closeout](audited-episode-client01-closeout.json) records 349 physical
exchanges, one counted Stopped lifecycle and one unstarted Prepared row. Started
319, Progress 321/322/325/328/329/330 and Stop 331 bind the counted play; userdata
has count 1 and position 591579620 ticks. Media exchanges 317/324 delivered
10,780,672 and 10,425,472 body bytes before browser cancellation; 327 completed.
The production physical checker accounts for both interruptions within the
owned chain. Neither partial response is labeled complete.

After-stop PlaybackInfo 335/337/338 explains the new uncounted Prepared row.
Logout 345 and same-token rejection 347 bind revocation. Twenty sessions are
revoked; eleven plays, five userdata rows, two retained audio references and no
encoding jobs remain. Thirty tables are unchanged and all prior rows preserved.
Browser PID 491875 and gateway PID 491867 are absent, their cgroups empty and
transient units unloaded. Saved terminal identities establish exit 0; unloaded
unit defaults are not exit evidence. Fresh checks confirm candidate PID 486706
and PostgreSQL process continuity.

Saved-only reconciliation used the unchanged production lineage and physical/
durable validators. It reproduced the original UI rejection on the unchanged
observation. Its receipt SHA-256 is
`00f8e110eb847b3105ac00047d9b79d71e07d74d4f8e325359742f58a88f7225`;
the independent checkpoint SHA-256 is
`72b24756bcd3e986c4bc3e7a128aebe2236d58e1020c00253b7cb80c4c6c0414`.
Both remain under the existing delivery root. These checks made no new business
HTTP request, SQL query, browser start or service mutation.

## What the error evidence establishes

| Observation | Established fact | Missing evidence |
| --- | --- | --- |
| One `Response` page error near browser 262 / physical 264 | Goby returned complete 404 `not_implemented` JSON for `/emby/LiveTv/Programs` | No direct rejection-to-response binding |
| Two `undefined` errors near external registration requests | Corresponding CONNECT attempts were rejected by the fixed gateway boundary, without upstream connection or written bytes | No shared request identity or source location establishes causality |
| An earlier registration request failed without a nearby page error | The same request failure does not always have the same adjacent error pattern | Timing cannot classify later errors as caused by that request or harmless |

The historical source15 TV observation used an older Goby backend, not an Emby
reference. Its errors cannot establish equivalent reference behavior. These
facts support neither Live TV implementation nor a network/license workaround.
MP3/FLAC acceptance remains valid; movie, episode and TV browse remain open.

## One minimal diagnostic increment

Hypothesis: pageerror serialization loses useful native rejection reason
information. A passive `unhandledrejection` listener may distinguish a native
Response, primitive undefined and the string `"undefined"`, and may associate a
Response with an already recorded exchange through a unique request ID.

Implement this in the existing adapter before freezing the first subtitle
input. Preserve pageerror, its count and failure rule. Read only safe primitive
types and captured native Response getters for URL/status/type/redirected, plus
the native Headers getter for `X-Request-Id`. Do not read bodies, enumerate
arbitrary objects or invoke their getters/string conversions. Do not intercept
fetch/Promise or call `preventDefault`.

Reuse the final credential set for delayed redaction. Publish sanitized
locations without query, fragment or userinfo. Retain at most sixteen native
events and 32 KiB serialized diagnostics; cap individual strings. Overflow,
observer failure or incomplete authority makes diagnostics explicitly incomplete.
It cannot overwrite the original error or block Stop/Logout. Collection starts
before navigation and ends at page disposal. Bridge/setup/teardown completion
must be bounded independently of UI cleanup.

Native and pageerror events keep independent identities. Associate a Response
only when its request ID and exact URL/status uniquely match recorded response
metadata. URL/status without a unique ID remains ambiguous. Carrying a response
does not alone prove its effect on playback. Primitive undefined stays
unattributed. Defer Debugger, async stacks and Network initiator collection.

Run targeted checks on `test-env`:

- A self-owned synthetic page rejects a native Response and primitive undefined;
  metadata is retained and pageerror still propagates. Include a handled failed
  request to reject time-based association.
- Projection cases cover duplicate/mismatched IDs, redirect/opaque metadata,
  late-known credentials, hostile objects, limits and overflow. Cleanup fault
  cases show the observer cannot prevent existing Stop/Logout handling.

Reuse relevant adapter/lifecycle checks for changed integration. Product bytes
are unchanged, so no full Go suite is needed. Limit implementation/verification
to one focused work session; reassess after 60 minutes of investigation. Do not
chain diagnostic expansions automatically.

## Next business scope

The next proposed run is the unconsumed subtitle scenario, primarily to establish
supported SRT/VTT playback. After diagnostics pass, freeze source, unused actor
facts and the twenty-session closed state, check resources, and keep existing
browser/request/cleanup budgets. This review does not create that input.

A directly bound Response supplies new evidence for a focused decision. Only
undefined, or no recurrence, retains the attribution gap. A clean subtitle run
does not repair historical episode/movie errors. Close all new owned state
before selecting another action. No automatic episode/movie replay, actor reset,
external egress change or synthetic vendor registration reply follows.
