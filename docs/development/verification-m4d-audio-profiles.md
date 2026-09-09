# M4d: audio profile negotiation and input timing

Verification date: 2026-09-09. Status: **passed for this increment**.

This increment connects audio `PlaybackInfo` profile selection to progressive
HTTP output while preserving the existing HLS path. It does not establish full
Emby client compatibility or complete the conversion milestone.

## Verification boundary

Local compilation is authorized. All functional tests, PostgreSQL integration,
FFmpeg probes, decoding, deployed requests, and source-preservation checks run
only through `ssh test-env` on Linux.

The test host uses Go 1.27.1 and FFmpeg/ffprobe 9.0.1. The deployed service runs
as the unprivileged `goby` user against the persistent PostgreSQL database on
port 5432. The full integration suite uses the separate owned PostgreSQL
cluster on port 15432 and isolated temporary schemas. These roles must not be
interchanged during deployment.

The source snapshot is `/opt/goby-test/verify-m4d-20260909`, a checked alias for
the ownership-marked tmpfs directory `/dev/shm/goby-verify-m4d-20260909`.
Go caches use the existing dedicated tmpfs locations. Both `GOTMPDIR` and
`TMPDIR` point to `/opt/goby-test/exec-scratch`; the shared `/dev/shm` mount is
not made executable. Unrelated host workloads and files remain untouched.

## Required checks

| Check | Current result |
| --- | --- |
| Local compilation | `go build ./...` passed with the final production changes |
| Ordered progressive/HLS profiles, projected output constraints, request immutability | Final targeted Linux race run passed |
| Standard audio URL round trip and independent channel ceilings | Initial Linux race run passed |
| Real HTTP negotiation, HEAD, media decoding, seeks, permissions and unchanged user data | Both new HTTP tests passed in the initial Linux race run |
| Exact supported Ogg timing and rejected unproven timelines | Complete media package and new progressive/HLS conversion regressions passed |
| New Python capture/deployment/upgrade scripts | Remote `py_compile` passed |
| Complete repository race suite | Passed: every package completed; no skipped tests |
| Deployed profile workflow and existing-library probe upgrade | Passed after correcting two verifier expectations |

The initial run used the complete `internal/media` and `internal/playback`
packages and the new server URL/channel/profile tests. Package durations were
4.555 s, 1.057 s, and 11.004 s respectively, with no skips. The two real HTTP
profile tests took 5.12 s and 4.87 s. Logs are
`/opt/goby-test/m4d-targeted-media-playback.log` and
`/opt/goby-test/m4d-targeted-server.log`. Later planner refinements and the Ogg
conversion fix require the final complete suite below before deployment.

## Ogg seek regression

Independent FFmpeg controls found two cases in which output sample counts alone
concealed incorrect media positions. A short Vorbis source had 4282 presented
samples at 44.1 kHz, beginning at timestamp 128/44100; a nonzero input seek
returned the expected count but content 128 samples too early. An Ogg FLAC
source of 2.013 s, requested from 1.2378912 s, returned content 11264 samples too
early after a demuxer timestamp-seek failure.

The selected fix uses private `AudioSampleSeek` plans for measured Ogg audio
encoding. Both progressive and HLS workers decode the prefix, trim by source
sample indices, reset timestamps, then resample and limit the selected output
window. This avoids relying on an unproven page-seek heuristic. Nonzero Ogg copy
conversions are declined; permitted encoding can satisfy a normal codec request.
Complete copies and original-file byte ranges remain separate delivery paths.
Prefix decoding is subject to the existing startup/job deadlines and resource
limits. The new Go runner regression passed all 18 complete/seek combinations
across Vorbis, Opus and FLAC at 100 ms and 2.013 s. WAV output matched the
corresponding complete-source PCM slice byte for byte, with exact sample counts.

Sample-based plans also omit output `-t`: retaining it made FFmpeg apply an
additional duration rounding after the exact sample filter. A 44101-sample
44.1 kHz source resampled to 48 kHz lost its last sample with that option.
The corrected WAV and FLAC outputs both preserve all 48002 samples and match
an independent decoded reference, including a nonzero last sample. A remote
read-rate control confirmed that the trim endpoint terminates the pipeline
without reading through the rest of a five-second source.

HLS regression covers three sources and seven producer windows: complete VOD,
later global segments, a bounded middle window, and an explicitly fractional
short-source worker window. Segment numbering, first PTS, decoded sample counts
and content-position checks passed. The fractional worker case is not described
as a canonical API segment boundary.

The first targeted run had one test-reference failure: a 100 ms Vorbis full
conversion's AAC output had normalized MSE 0.04167696 against uncompressed PCM,
above the 0.02 threshold. Direct MPEG-TS and independently decoded raw-float PCM
encoded to AAC produced the same decoded SHA-256 as Goby's output:
`51eb122ca5809015ef9b271bc5830c8167593dccaa3f865daf7b2fabbff500fe`.
The difference was local AAC encoding distortion, not a source-position error.
Only that complete-source case now uses the independent codec-matched reference;
the threshold and all six other window comparisons remain unchanged. The final
HLS test passed in 1.585 s including race instrumentation. Logs are
`/opt/goby-test/m4d-final-targeted.log` and
`/opt/goby-test/m4d-hls-ogg-final.log`.

## Complete Linux suite

The final source passed `go test -race -count=1 -v ./...` against the separate
PostgreSQL verification cluster. The [machine-readable summary](m4d-full-race-summary.json)
records every package and the full log SHA-256. The command package has no tests;
all twelve packages with tests passed, and no test was skipped.

| Package | Duration |
| --- | ---: |
| artwork | 1.546 s |
| config | 1.021 s |
| database | 3.094 s |
| events | 1.078 s |
| identity | 29.612 s |
| library | 24.926 s |
| media | 4.965 s |
| metadata | 1.586 s |
| playback | 1.185 s |
| server | 239.311 s |
| subtitle | 1.159 s |
| transcode | 48.905 s |

The Go process returned zero. Its PowerShell-to-SSH wrapper subsequently returned
2 because a trailing carriage return turned the final shell argument into
`0\r`. The log and expected package inventory were independently checked; this
was not a Go test failure. Subsequent inline SSH scripts normalize CRLF. The
audit summary retains both exit statuses instead of hiding the wrapper error.

All 999 selected Go/module, test-fixture and test-script files matched the source
archive at the pre-deployment comparison. The later deployed-verifier correction
changed no Go production code or Go test. Its final Python syntax and actual
deployed run were checked remotely.

## Deployed workflow and probe upgrade

The test service was rebuilt and started as `goby`, reporting schema 12, probe
version 4 and FFmpeg 9.0.1. The prior binary was preserved in an ownership-marked
private tmpfs directory. The persistent application database remains on port
5432; it was not replaced by the integration-test database.

The [deployment evidence](m4d-deployment-evidence.json) confirms active/running
state, PID 3288722 with real/effective UID 995, equality of the on-disk and running
executable, and equality of all 238 Go/module files between the deployed source
and the verified snapshot. The deployed binary SHA-256 is
`5d9a802ca35f3ddcd3ba07a21238639ddaf5844fe021d2e952f44b2b95d856a3`.

The [first deployed attempt](m4d-deployed-audio-profiles-attempt-1.json) completed
the actual version 3 to version 4 upgrade of all eleven media records in five
existing fixture libraries. Twenty fixture files, twenty-one catalog items,
all fifteen user-state rows and seven ownership records remained unchanged
apart from the intended probe facts and scan bookkeeping.

That attempt then exposed two incorrect verifier expectations. With transcoding
explicitly disabled, this single-track ADTS fixture uses the established complete
original-file fallback rather than an M4A remux URL. Goby's established Emby
logout response is 200, not the verifier's assumed 204; the session had actually
been revoked. The script now checks those actual contracts. No server behavior
was weakened to make the script pass.

The [final deployed result](m4d-deployed-audio-profiles.json) passed with empty
cleanup errors and 700876 received HTTP bytes:

| Check | Observed result |
| --- | --- |
| FLAC to MP3 profile | Mono, 24 kHz, 96 kbit/s; standard authenticated URL |
| Complete MP3 | 145152 decoded samples, 6.048 s |
| Same URL changed to two-second start | 97344 decoded samples, 4.056 s |
| Persisted seek plans | Starts 0 and 20000000 ticks; other plan/source/scope fields unchanged |
| ADTS to WAV via PlaybackInfo | Exactly 289792 mono samples at 48 kHz |
| Mixed HTTP/HLS profiles | First supported protocol selected in both orders |
| Progressive HEAD | No encoding job added and no invented length |
| Enabled conversion delivery with AAC copy | M4A `TranscodingUrl` retains `AudioCodec=copy` |
| Explicit transcoding-disabled fallback | Original ADTS `DirectStreamUrl`, full length and byte-range support |
| Repeated upgrade scans | Eleven current version 4 records reused; zero additions or updates |
| Source DTOs, media files and user state | Preserved |
| Owned authentication, encoders and temporary files | Revoked/inactive/removed; no cleanup errors |

MP3 durations above include actual codec padding; they are not relabeled as
exact six- and four-second responses. The deployed fixture is periodic, so its
sample count and persisted plan alone do not independently prove audible source
position. The separate Ogg content-position and exact-PCM runner regressions
provide that stronger evidence for their own fixtures.

The host root filesystem remains constrained. A read-only Go package-inventory
audit accidentally populated its default module cache; only those identified,
rebuildable Goby dependency/compiler caches were reclaimed. Build and test work
uses the dedicated tmpfs caches. The successful deployed workflow ran after
space was restored; unrelated workload files and services were preserved.

## Acceptance limits

Output promises depend on constructible media plans and measured input timing.
A successful API status or a container's estimated duration does not prove a
complete playable response. Lossy output padding must be reported as measured;
lossless output tests use integer decoded samples.

The reference study records successful and failed server behavior separately.
Goby does not reproduce inaccurate lengths, ignored required output conditions,
truncated responses, or unusable advertised profiles. Its progressive
`TranscodingSubProtocol` is normalized to `http`, including profiles whose
protocol was omitted or empty.

Actual GPU execution, progressive video, broader subtitle and metadata support,
and real-client acceptance remain outside this increment's completion claim.
