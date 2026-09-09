# M4b HLS VOD verification

Date: 2026-09-09. This increment connects the conversion engine to authenticated
Emby playback negotiation, complete VOD playlists, globally numbered MPEG-TS
segments, seek production, current user permissions, and owned encoding cleanup.
It does not establish compatibility with every third-party client or actual GPU
execution. The administrator dashboard remains an administration interface.

## Complete Linux suite

All tests and media execution ran through `ssh test-env`. The final complete
command was `go test -race -count=1 -v ./...` in
`/opt/goby-test/verify-m4b-20260909`, an owned source snapshot backed by tmpfs.
The protected PostgreSQL test configuration was loaded without printing secrets.
GOTMPDIR and TMPDIR used `/opt/goby-test/exec-scratch`; the shared `/dev/shm`
execution policy was unchanged. The complete log is
`/opt/goby-test/m4b-final-race.log`. All twelve test-bearing packages passed with no
skipped tests:

| Package | Result | Reported time |
| --- | --- | --- |
| artwork | PASS | 2.247 s |
| config | PASS | 1.037 s |
| database | PASS | 3.142 s |
| events | PASS | 1.105 s |
| identity | PASS | 29.214 s |
| library | PASS | 25.604 s |
| media | PASS | 2.699 s |
| metadata | PASS | 1.777 s |
| playback | PASS | 1.064 s |
| server | PASS | 178.548 s |
| subtitle | PASS | 1.130 s |
| transcode | PASS | 17.029 s |

The authorized local `go build ./...` also passed after the transport and manual
start-hint corrections. No local functional tests or media probes were run.

The earlier targeted HTTP/race run also passed; its retained log is
`/opt/goby-test/m4b-http-race.log`; the later manual-start regression run is
`/opt/goby-test/m4b-http-final-race.log`. They cover actual HTTP media output and
focused runtime regressions. Existing original-file, user-state, subtitle, catalog,
identity and WebSocket tests remain part of the complete suite.

## Protocol and lifecycle coverage

The HTTP integration fixture is a generated twelve-second, 24 fps, H.264/AAC
source with four distinct three-second color regions. The tests use the actual
scanner/prober, PostgreSQL, playback negotiation, HLS runtime, manager, FFmpeg,
HTTP reader, ffprobe and decoder. The output is H.264 at 96x54 with audio.

- Master and media playlists retain the complete VOD timeline and stable global
  numbering, including a six-second initial position and a later explicit reset
  to zero. Manual master requests pass through the bounded query adapter;
  omitting a later start resets a reused revision to the source beginning.
- Requests for segments `2 -> 3 -> 0 -> 2` return the expected scene, dimensions,
  independently decodable audio/video, and source-global transport timestamps.
- Cache hits, HEAD and 304 retain current authorization. Missing/invalid tokens,
  another user or authentication session, administrator substitution, source
  changes and revoked conversion permissions cannot reuse the owner's output.
- A registered output revision cannot be changed with transform query options.
  ActiveEncodings DELETE/POST aliases, stopped reports and logout retire owned
  output without inventing user playback progress or watched-state updates.
- Deterministic runtime regressions cover out-of-order `0 -> 2 -> 1` prefetch
  sharing one producer, consumed input descriptors, an exhausted maintenance
  cycle preserving unverified sessions, and precise retirement after a confirmed
  permission denial. Transient verification errors do not permanently retire
  otherwise valid revisions.

The tests distinguish a confirmed source snapshot change from temporary I/O
failure. A confirmed change preserves the existing 503 source-error contract
while retiring stale output. An old disabled-conversion fixture now expects 503
from the dedicated HLS handler instead of its previous wildcard-route 404.

## Timeline and worker coverage

Real-media runner tests cover MP4 and MPEG-TS sources, stream copy, H.264 encoding,
AAC and MP3, complete-source final segments, nonzero global segment numbers,
later-start production, and bounded EndTicks windows. Segment cuts use explicit
source timestamps; copied video uses normalized packet seekpoints and preserves
source audio lead-in.

Every produced segment in the media matrix is independently probed and fully
decoded. The same global segment keeps its video PTS across zero-start and
later-start producers. Measured audio differences remain within packet-boundary
tolerances: up to 85.333 ms for copied audio and 21.333 ms for encoded audio in
the tested matrix. These are fixture observations, not a universal guarantee of
sample-exact gapless playback. The separate runner logs are
`/dev/shm/goby-vod-runner-20260909/runner.log` and
`/dev/shm/goby-vod-runner-20260909/av-timeline-fixed.log`.

The worker publishes finalized TS files only after observing the next temporary
segment or process exit, validates packet alignment, and publishes its internal
playlist atomically. Internal lists, publishing files and unfinished segments
are accounted for but unavailable through output readers. Manager tests cover
nonblocking cache lookup, immutable snapshots, scope isolation, cancellation,
reader retention, limits and shutdown races.

The deployed HLS demuxer exposed a gap that single-segment checks missed: fresh
MPEG-TS muxers reset continuity counters at each segment, causing strict FFmpeg
playback to fail with a corrupt-packet error. Both a fresh sequential graph and
a graph with previously fetched nonsequential segments reproduced the failure.
The correction adds transport discontinuity indicators and declares each real
non-first segment boundary with `EXT-X-DISCONTINUITY` in the public playlist.
This follows [RFC 8216 section 3](https://www.rfc-editor.org/rfc/rfc8216.html#section-3)
rather than relying only on a permissive demuxer. The verifier retains `-xerror`;
it does not suppress corruption to obtain a pass.

The focused race run at
`/dev/shm/goby-vod-runner-20260909/continuity-regression.log` passes all eight
MP4/MPEG-TS and copy/encode mode combinations. It checks every PID's initial
transport indicator and strictly decodes both the publisher's actual complete
playlist and a playlist combining different producer windows. The same coverage
passes in the final complete suite above.

## Configuration and PostgreSQL

Configuration tests cover explicit enable/disable, Linux cache paths, concurrency
and byte limits, output bounds, independent decode/encode selection, malformed
settings and incompatible hardware combinations. These checks validate settings
and command generation; they do not execute a GPU.

Migration `0011` preserves `0010` and expands immutable encoding-plan JSON to
128 KiB for VOD cut points. Tests perform a real version-10-to-11 upgrade,
preserve existing job data/history, check the old 8192/8193-byte and new
131072/131073-byte boundaries, reject non-object plans, and rerun migrations
idempotently. Repository tests verify durable larger VOD plans and immutable
start number, end boundary and source cut points.

## Deployed service and continuous playback

The maintained deployment script built and installed the final source and
restarted only `goby-foundation-test.service` at `127.0.0.1:18096`. Readiness
passed. PostgreSQL reports eleven migrations with latest version `0011`,
`0011_encoding_vod_plans.sql`. systemd reports active/running, configured and
effective user `goby`, and effective UID 995. The owned transcode cache is mode
0700 and belongs to `goby`. [Deployment evidence](m4b-deployment-evidence.json)
contains no credentials. No frontend changes or new browser result are claimed.

The deployed [verifier](../../scripts/test-env/verify-hls.py) uses the existing
immutable normal-frame-rate reference source: fifteen seconds, 320x180, 24 fps,
H.264/AAC, 967651 bytes, SHA-256
`332bce27f1e97d71d3ef7cffa59d8cf70cbcc51380a6b600e05048107432de4d`.
It requests H.264/AAC output at 160x90 in five three-second segments. This is the
same source used by the HLS reference study, but the present checks are Goby's
delivery tests rather than a complete automated differential/client comparison.

Both the [nonsequential-first result](m4b-deployed-hls.json) and the independent
[whole-first result](m4b-deployed-hls-whole-first.json) passed. The corresponding
files remain on the host under `/opt/goby-test/`. FFmpeg reads a private master
file whose child resolves to the actual authenticated HTTP media playlist and
segments; credentials never appear in subprocess arguments or the public result.
Strict `-xerror` and both video/audio mappings remain enabled.

| Deployed workflow | Result |
| --- | --- |
| Complete VOD, six-second start hint, stable global segments 0 through 4 | PASS; fifteen-second timeline with four explicit segment discontinuities |
| Request order `2 -> 3 -> 0 -> 4` | PASS; every response 200, 72 video frames, full audio/video decode |
| Video PTS for those requested segments | 7, 10, 1 and 13 seconds; source position plus the Goby transport offset |
| Continuous graph from the beginning | PASS in both request orders; 360 video frames, 15.018667 decoded seconds |
| Continuous graph with `-ss 6` | PASS in both request orders; 216 video frames, 9.018667 decoded seconds |
| HEAD and conditional GET | 200 and 304 after authorization |
| Missing/invalid token and another authentication session of the same user/device | 401 and 404 respectively |
| DELETE/POST ActiveEncodings | 204; retired children return 404 |
| Stopped and logout | Stopped returns 204 and retires children with 404; logout returns 200 and old-token children return 401 |

Both runs preserve the source, ownership marker and viewer's UserData DTO. The
owned temporary private files are removed and cleanup errors are empty. An
ownership-recorded test library remains available for repeatable future checks.
The final HTTP body totals are 1803519 and 1805775 bytes respectively. Earlier
failed captures remain separately on the host; they are not relabeled as passes.

## Remaining acceptance

Full third-party-client acceptance, progressive conversion, universal audio,
additional subtitle/profile/output support, long-form and large-catalog cases,
hard worker resource isolation, recovery/distribution coverage, and actual
hardware decode/encode remain required. The host has no GPU; a configured
hardware path does not change `Hardware.Verified=false` into a verification
claim. See [HLS behavior](hls-playback.md), [configuration](transcoding-configuration.md)
and [implementation progress](progress.md) for the active scope.
