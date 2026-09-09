# M3c external subtitle verification

Date: 2026-09-09. This increment delivers indexed external SRT/WebVTT and records
WebSocket research. WebSocket implementation and the complete M3 acceptance gate
remain open. Local verification consisted only of the authorized `go build ./...`;
all tests, reference exchanges, and deployed checks ran through `ssh test-env`.

## Delivered behavior

- Stable external subtitle indexes, tombstones, bounded sidecar discovery and
  snapshots stored separately from primary media-probe data.
- Strict UTF-8/BOM and UTF-16 text parsing, SRT/WebVTT serialization/conversion,
  source preservation, and reference-aligned window/timestamp handling.
- Item and source stream descriptors with current-user delivery URLs; explicit
  external-format selection in PlaybackInfo without claiming media transcoding.
- GET/HEAD through Videos/Items and path/query start forms, current permissions
  and source checks before conditionals, content ETags and effective modification
  dates, and bounded request/storage work.

The [subtitle guide](external-subtitles.md) documents supported names, encodings,
limits, time behavior, and explicit access-policy differences. In particular,
Goby requires authentication where the reference served sampled subtitles
publicly, and it preserves existing indexes where the reference reordered them.
These differences are not presented as exact wire parity.

## Reference records

The [HTTP reference report](../research/reference-server.md) adds 36 SRT delivery,
conversion, negotiation, and time-matrix records plus three native VTT records.
Native VTT was observed as Codec=vtt, Language=en, and IsForced=true for the
language/forced filename. Its insertion changed the reference's stream indexes;
old records were preserved without rewriting them.

The separate [WebSocket report](../research/websocket-reference.md) adds 46 records:
19 connection transcripts and 27 supporting HTTP exchanges. It establishes
accepted upgrade paths, token-scoped UserDataChanged events with MessageId,
Ping/Pong, and carefully bounded negative observations for incoming progress and
session-subscription messages. No Goby event endpoint is claimed by this report.
The combined reference directory contains 317 JSON records. Each investigation
audited its fixed pre-existing records and kept credentials outside the repository.

## Compilation and complete Linux suite

Local `go build ./...` passed. The final Go source, scripts, and reference fixtures
were transferred to `/opt/goby-test/verify-m3c-20260909`. Fixtures are required for
the subtitle package's 16 byte-exact reference-body/MIME comparisons; missing
fixtures fail the test rather than skip it.

The complete remote command was `go test -race -count=1 -v ./...`, with the protected
PostgreSQL/media environment and dedicated Goby caches and executable tmpfs.
All ten test-bearing packages passed, with no skipped tests. The retained remote
log is `/opt/goby-test/m3c-race.log`.

| Package | Result | Reported time |
| --- | --- | --- |
| artwork | PASS | 2.986 s |
| config | PASS | 1.010 s |
| database | PASS | 3.008 s |
| identity | PASS | 25.653 s |
| library | PASS | 22.205 s |
| media | PASS | 2.526 s |
| metadata | PASS | 1.636 s |
| playback | PASS | 1.040 s |
| server | PASS | 142.804 s |
| subtitle | PASS | 1.162 s |

Coverage includes text/resource boundaries, overlapping cues and exact output
spelling, source-start versus transformed-end filtering, query precedence,
native/converted profile selection and rejection boundaries, cached scans,
directory ownership, stable indexes and retirement/reappearance, embedded-index
collisions, same-snapshot batched projection, file/hash/ctime replacement checks,
symlink/FIFO/oversize rejection, no primary-media content read, cancellation with
retained worker slots, and HTTP authentication before conditional responses.
Existing identity, playback, catalog, metadata, artwork, and migration tests passed.

## Deployed subtitle workflow

The maintained deployment script installed the prepared source. The active service
runs as `goby` on `127.0.0.1:18096`; read-only inspection confirmed schema version 9.
The [deployed verifier](../../scripts/test-env/verify-subtitles.py) passed using the
real 600-second H.264/AAC movie and its owned SRT/VTT sidecars:

| Check | Observed result |
| --- | --- |
| Scan and descriptors | One movie; two external tracks; srt/vtt, language en, native VTT forced |
| Top-level and source MediaStreams | Matching descriptors and authenticated delivery URLs |
| Native GET/HEAD | 200; original SRT/VTT bytes preserved; text/plain and text/vtt; matching lengths |
| VTT-only External profile selecting SRT | Native Codec stays srt, URL selects vtt, DirectPlay/DirectStream true, Transcoding false; DefaultSubtitleStreamIndex omitted |
| Full SRT-to-VTT output | Matches the reference body exactly |
| Start 10 seconds / End 20 seconds | CopyTimestamps=false and true each match their distinct reference body |
| Conditional request with valid token | 304 |
| URL stripped of all query parameters, or invalid token | 401, including with the cached validator |
| Cleanup | Viewer/admin sessions revoked; no cleanup errors; video, both subtitles, and ownership marker unchanged |

Only a separately recorded Goby library and its reusable synthetic viewer were
used. Reference media were read-only. No credential-bearing URL, password, or
notification token is included in the verification output or this report.

## Remaining scope

Embedded subtitle extraction, ASS/SSA and bitmap rendering, HLS subtitles,
fonts/attachments, richer encoding and style support, WebSocket/events,
conversion/hardware pipelines, broad administration, and consumer-client release
evidence remain open. Global NextUp reference parity is still unresolved. The
React/MUI administrator frontend was unchanged and has no consumer player.
No hardware decoding/encoding execution or new browser test is claimed here.
