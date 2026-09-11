# M3e source15 external subtitles and ordinary TV

Status: **the original-client SRT/VTT journey and ordinary TV browse journey
completed**. The [safe summary](m3e-source15-subtitle-tv-summary.json) preserves
exact candidate and driver hashes, UI observations, response bytes, state, and
token-rejection proof. These runs took place before the separately controlled
music scan; no music UI was opened and no audio playback was attempted.

The schema25 candidate was PID 438666, start ticks 2005412, executable SHA-256
`99669e962493ef64f1315d3347b07859d1c376151c9712f48d8629d7e756d60a`.
The loader accepted schema25 only through the completed upgrade and its bound
catalog, migration, source manifest, and current process. An independent static
review and remote syntax/read-only guard checks preceded the actual UI runs.
All verification ran through `ssh test-env`.

## External SRT and VTT

`goby-av-source15-subtitles-01` used unmodified Emby Web 4.9.5.0 in Chromium
153.0.8010.12 with Playwright 1.63.0. Actual UI actions opened the owned movie,
played and paused it, selected English (SRT), sought forward, selected English
(VTT), restored Off, stopped, and signed out.

The first paused observation was at 0.979525 seconds, readyState 4, 320x180, with
35 decoded frames and no dropped frames. The SRT opening cue appeared. The UI
position slider reached 121.242 seconds, where the 118–128 second cue was active
and visibly rendered. The VTT selection rendered the same expected cue. Both
native tracks were disabled and the rendered cue disappeared after the actual
Off action; no video element remained after stopping.

| Selected source | Actual delivery | Body evidence |
| --- | --- | --- |
| Index 2, SRT | Stream.vtt, HTTP 200 text/vtt | 187 bytes, SHA-256 `6639a605af385edeaac96cd2aab8a5ee744c60a041e7d4ecadb5f7a86980a67b` |
| Index 3, VTT | Stream.vtt, HTTP 200 text/vtt | 220 bytes, SHA-256 `efbce0e543ce0acef4a0ab9f979f2a4b5386658d4a96222ec95bdf917139dd6b` |

The SRT conversion matches the earlier reference entity hash. The VTT response
retains the owned VTT file and differs from the reference's earlier 195-byte
normalization; that byte-level difference is preserved, while actual caption
rendering and timing passed. The browser driver did not alter URLs or responses
to obtain these results. The late item-detail native URL did not block this
specific observed flow; no universal claim about client cache-update ordering
is made.

The dedicated account's legitimate movie history was retained at 1212420000
position ticks and PlayCount 4. Its audio UserData remained unchanged and
unplayed. Configuration, Policy, and UserSettings hashes matched before and
after the run.

## Ordinary TV and remaining scope

`goby-av-source15-tv-01` completed the same normal Series, two seasons, three
episodes, detail page, and Home navigation as the
[source12 TV checkpoint](verification-m3e-goby-source12-tv.md). The original
client retained its cross-season list while changing the selected season label.
No playback report or media stream was observed. All nine UserData snapshots
and all preference hashes matched.

This normal fixture does not verify positive specials, standalone specials, or
every special-content combination. It also does not establish MP3/FLAC
acceptance after the pending music scan. Source12 failures and diagnostic inputs
remain unchanged rather than being relabeled as source15 success.

The subtitle run retained six auxiliary HTTP failures and six page errors; the
TV run retained five of each. The subtitle failures included SpecialFeatures,
Similar, ThemeMedia, Intros, and the now explicitly identified ThumbnailSet
request. These gaps were not hidden by the passed subtitle/browse assertions.

Both runs ended with actual UI logout 204, the visible login page, and an
independent HTTP 401 using the exact observed logout token. All instance checks
passed through shutdown. No recovery state or unrevoked session remained from
these two runs. The final nine driver inputs were individually hash-checked and
archived at
`/opt/goby-test/exec-work-m3e/verified-source15-subtitle-tv-source/`.
