# M3e source16 audio report correlation diagnostic

Status: **partial diagnostic**. The first Playing request supplied usable
equality evidence, but error-code acquisition remained incomplete. The
[safe summary](m3e-source16-audio-report-diagnostic-summary.json) records this
single additional authorized MP3 run. The
[original source16 MP3 and FLAC evidence](verification-m3e-source16-audio.md)
remains unchanged.

The run was
`/opt/goby-test/exec-work-m3e/goby-av-source16-audio-report-diagnostic-01/`.
Its `observation.json` SHA-256 is
`4c4930ddc3e1c6f098dda0dc01d0d2269f1d7682bddc99e74a5950805f163eec`.
The source16 candidate remained PID 472843, start ticks 2393606, executable
SHA-256 `ce87ea993696ab107837ba61475977aab1e2d8c56a9a2a287350b11d9d34feb6`,
using the same original Emby Web and Chromium environment as the original runs.

The collector observed one unambiguous preceding universal request. Times are
milliseconds relative to collector installation; request and response order
were recorded independently.

| Event | Time | Order | HTTP status |
| --- | ---: | ---: | --- |
| Owned universal GET request | 380 | 1 | Pending |
| Universal response | 399 | 2 | 206 |
| First Playing request | 417 | 3 | Pending |
| First Playing response | 426 | 4 | 404 |

The first Playing body had the required observable fields. The retained
comparisons contain no token, device, session, or item values or value hashes.

| Observed field | Reference | Result |
| --- | --- | --- |
| Body PlaySessionId, string length 13 | Prior universal query PlaySessionId | Equal |
| Body ItemId, string length 32 | Owned item | Equal |
| Body MediaSourceId, string length 32 | Owned item | Equal |
| Body MediaSourceId | `mediasource_` plus owned item | Not equal |
| Body MediaSourceId | Universal query MediaSourceId | Not comparable: reference missing |
| Query X-Emby-Token | Universal query api_key | Equal |
| Query X-Emby-Device-Id | Universal query DeviceId | Equal |

All other inspected token/device sources and body SessionId were missing.
Missing fields were not converted into false equality. In particular, the
missing universal MediaSourceId was not defaulted to the owned item. These
facts establish the listed comparisons for this request; they do not establish
server acceptance or explain the 404 response.

The first Playing, Progress, and Stopped error responses each declared a
93-byte JSON body. All three body readbacks were unavailable or unfinished:
parsed bytes were 0, observer errors were 3, and pending error-body reads were
3. Primary coverage was false and the collector outcome was
`diagnostic_observations_incomplete`. No ErrorCode was obtained; none is inferred
from the status or substituted from product code. The equality observations
remain usable independently of this incomplete error-body evidence.

Error-body reads were limited to the selected failed report responses, with
at most four attempts, 8 KiB reserved per attempt, a 32 KiB total budget, and a
two-second deadline. Only three attempts occurred. Successful authentication
and media response bodies were not read. Message output was omitted, and
ErrorCode output was restricted to fixed recognized values or an unknown
marker. These diagnostics initiated no additional HTTP requests.

Actual audio media and original UI controls were still observed. The final
Stopped response remained 404, so the original audio workflow failed. UI Sign
Out returned 204, the login view appeared, and the exact observed logout token
was independently rejected with 401. All five instance pins passed; observed
UserData and preferences remained unchanged.

The eleven frozen inputs are archived at
`/opt/goby-test/exec-work-m3e/av-source16-audio-report-input-01/`, with manifest
SHA-256 `f42618039e43845f8dc64a9d2e18cdf5c769b56ce6f0739e73912a617c4a308b`.
The linked summary retains the individual driver hashes. No additional run or
retry was performed to obtain the missing ErrorCode. This partial diagnostic
is not a complete audio acceptance result.
