# M4c audio delivery verification

Date: 2026-09-09. This increment adds Universal and legacy progressive audio,
client-generated playback references, exact audio timing facts, and audio HLS
tail handling. The scope and remaining limits are documented in
[audio playback](audio-playback.md) and
[client playback references](client-playback-references.md). It does not complete
all Emby APIs, real-client acceptance, or GPU verification.

## Environment and isolated PostgreSQL

All functional, race, media, protocol and deployment checks ran through
`ssh test-env`. Only authorized compilation ran locally; `go build ./...` passed.
Go 1.27.1, FFmpeg/ffprobe 9.0.1 and PostgreSQL 17.11 remained the pinned tools.

The large reference-capacity test writes 65,536 real tombstones. To avoid placing
that temporary data and WAL on the constrained root filesystem, an independent
PostgreSQL cluster runs on `127.0.0.1:15432` inside its own 1 GiB tmpfs. Its
`goby_test` role authenticates using SCRAM and has no superuser, createdb,
createrole, replication or bypassrls privileges. The protected environment
override is `/opt/goby-test/m4c-test.env`, mode 0600. The runner loads the original
`test.env` first, then this override. The deployed application continues using
the existing persistent PostgreSQL instance on 5432.

[prepare-postgres-scratch.sh](../../scripts/test-env/prepare-postgres-scratch.sh)
passed remote shell syntax and repeated preparation checks. Repetition preserved
the process/system identity and credential file, and did not alter the main
database service. Its optional stop path received static review; no stop result
is claimed for the cluster while it remains in use.

The root filesystem later reached zero unreserved free space. Inspection showed
unrelated temporary project trees; these were retained. Removing the matching
duplicate Goby binary and clearing the rebuildable root Go cache recovered space.
The main PostgreSQL instance contained only Goby's test database and standard
databases. Its test-host WAL settings were changed to 128 MiB maximum and 32 MiB
minimum, with reload/checkpoint; these are not production defaults or hard WAL
size guarantees. Root availability recovered to approximately 66 MiB before the
deployment build. Large verification files remained in owned scratch locations.

## Complete final regression

The final source snapshot is `/opt/goby-test/verify-m4c-20260909`, backed by its
owned tmpfs directory. The complete command was
`go test -race -count=1 -v ./...`, with PostgreSQL pointed at the isolated 15432
cluster and GOTMPDIR/TMPDIR at `/opt/goby-test/exec-scratch`. Every test-bearing
package passed and no test was skipped. The full log is
`/opt/goby-test/m4c-final-race.log`.

| Package | Result | Reported time |
| --- | --- | --- |
| artwork | PASS | 1.729 s |
| config | PASS | 1.023 s |
| database | PASS | 3.230 s |
| events | PASS | 1.093 s |
| identity | PASS | 29.733 s |
| library | PASS | 25.994 s |
| media | PASS | 4.210 s |
| metadata | PASS | 1.721 s |
| playback | PASS | 1.062 s |
| server | PASS | 227.246 s |
| subtitle | PASS | 1.129 s |
| transcode | PASS | 47.484 s |

The prior full invocation found three outdated audio expectations in an
original-stream fixture that disables conversion. They expected the previous
501 response; the new adapter explicitly reports incompatible output or a
contradictory original/Static selector. The assertions were updated, malformed
playback-reference error mapping was covered, and the complete suite above was
rerun. Existing video, original-range, identity, catalog, metadata, subtitle and
WebSocket checks all passed with the final audio implementation.

## Targeted integration and actual media

The first combined database/library/media run passed those three packages:
database 2.969 s, library 20.591 s, and media 3.809 s. That snapshot caught the
progressive planner while its shared integer sample helper was still being
integrated; the playback package's missing-symbol compilation error was resolved
by synchronizing the completed helper. That first combined invocation is not
reported as a complete pass. Its log is `/opt/goby-test/m4c-core-race.log`.

The final focused exact-audio run passed playback 1.021 s, transcode 1.540 s and
server 5.797 s, with no skipped tests. Its log is
`/opt/goby-test/m4c-exact-audio-race.log`. The updated legacy stream/error contract
and progressive nonce workflow passed together in 7.901 s at
`/opt/goby-test/m4c-stream-contract-race.log`.

Coverage includes:

- A real version-11-to-12 migration, old playback/history preservation,
  independent same-nonce authentication scopes, terminal tombstones, reserved
  internal IDs, concurrency, default-session behavior and real binding limits.
- Exact MP3/AAC priming/discard and raw ADTS sample duration, source descriptor
  stability, cancellation, environment isolation and bounded packet/frame parsing.
- Progressive MP3, AAC, FLAC, Ogg and fragmented MP4, native WAV handling,
  copying where proven, seeking, first-payload readiness and append-only headers.
- 24-bit FLAC preservation verified against decoded PCM bytes; precise WAV
  lengths and bounded sample quantization; malformed/short/changed source failure.
- Real HTTP original bytes, HEAD, Range and 304; fresh client nonces; progressive
  MP3 at the 128 kbit/s ceiling; complete and offset playback; shared-cache scope;
  an ADTS-to-WAV sample-exact full output; and a 6.001-second complete audio HLS
  graph without an unproducible extra segment.
- Live, paced FFmpeg playback: Stop after headers must abort the HTTP body,
  the last disconnected consumer cancels unfinished work, and another consumer
  remains valid when only one shared request disconnects. Checks include cache
  reclamation, process disappearance and unchanged user playback history.
- Media activity renews a prepared play's lease without changing playback
  state, position, count or UserData. Deterministic retirement gates prevent a
  replacement request from reusing a producer being cancelled.

An old AAC test compared every later input seek with a byte slice of a decoder
started at zero. Direct FFmpeg controls showed that this assumption was too
strong: AAC overlap reconstruction changes initial samples, and PNS can change
subsequent decoded noise values. Counts remained exact. The final test retains
integer counts for every seek/rate, full-source byte equality and the verified
initial-packet range; it does not claim byte-identical decoder history after every
lossy-codec seek. PCM seek tests retain their stronger sample-boundary checks.

The first audio HTTP tail fixture also omitted `Container` while requesting a
conversion fallback. After the new reference controls confirmed unrestricted
original delivery for bare Universal URLs, the fixture was corrected to declare
its accepted input format. Production behavior was not changed to force encoding
for a request that actually accepted the original bytes.

## Audio HLS tail and duration evidence

Independent testing of the stable M4b engine used 32 small source variants and
160 full/tail runs. Sixteen cases declared a tiny final segment for which no
reference packet existed. MP3, FLAC and WAV near six seconds reproduced the issue.
Merging the final unsupported cut preserved duration and decoded samples in all
120 diagnostic controls; the publisher's strict segment-count check was retained.

The same work found a separate raw ADTS error: a 5.798398-second format estimate
represented 289792 decoded samples at 48 kHz. Using the estimate as `-t` lost
11264 samples. Probe version 3 establishes actual timing before conversion;
the final WAV path carries integer source sample counts rather than rounding
an already rounded tick duration again.

The new audio timeline passed real full/last-segment decoding on twelve edge
inputs, plus all twelve supported AAC and nine supported MP3 output sample rates.
Frame lengths were checked with decoded integer sample counts instead of rounded
MPEG-TS packet timestamps. Copy timing permits only the documented 100 ns outward
rounding difference, with an explicit rejection beyond that bound.

## Reference-server evidence

The [audio study](../research/audio-reference.md) adds 174 sanitized JSON records,
including 117 HTTP responses, for a combined reference baseline of 610 records.
It preserves the previous 436 records and source media. Exact raw/export hashes,
four immutable source hashes, bounded response sizes, isolated network state and
cleanup were audited remotely.

Positive reference controls cover all four bare original formats, progressive
MP3, a fresh offset stream and complete AAC/TS HLS. First-request failures,
inaccurate HEAD sizes, wrong ADTS MIME, oversized HLS timelines, ignored limits
and cross-source reuse of an old play ID remain recorded as observations. Goby
does not reproduce those failures or advertise them as desired compatibility.

## Deployed audio workflow

The [deployed verifier](../../scripts/test-env/verify-audio.py) passed on its first
execution after deployment, without a script or production-code repair. Its
[sanitized result](m4c-deployed-audio.json) and
[deployment/source evidence](m4c-deployment-evidence.json) are retained locally
and under `/opt/goby-test/` on the host.

The service reports schema 12, active/running state, configured/effective user
`goby`, and UID 995. It uses the original persistent PostgreSQL port 5432. The
verification cluster on 15432 is not the application's database. Five owned
audio files have probe version 3 and exact timing facts; the four main files
match the reference study's immutable source hashes.

| Deployed check | Result |
| --- | --- |
| Bare MP3, FLAC, ADTS and WAV | Exact source bytes; GET/HEAD 200, Range 206, conditional 304 |
| Missing and invalid token | 401 |
| Progressive HEAD | No output Content-Length; zero matching encoding jobs |
| Progressive MP3 full source | Decode/probe pass, 289152 samples at 48 kHz stereo, 6.024 s |
| Progressive MP3 from two seconds | Decode/probe pass, 193536 samples, 4.032 s |
| Progressive Range | Ignored with 200 and the full selected representation |
| Repeated same scope/plan | Same job reused |
| Same nonce in another authentication session | Independent canonical play/job |
| Same nonce applied to another source in one scope | 404 |
| ADTS to WAV | Exactly 289792 samples at 48 kHz mono; no missing or extra presentation samples |
| 6.001-second audio HLS | Two declared segments, 3 + 3.001 s, both 200; start hint retained |
| Complete audio HLS graph | Probe/decode pass, AAC at 48 kHz stereo, 288768 samples, 6.016 s |
| Nonexistent third HLS segment | 404 |
| Stop, DELETE/POST ActiveEncodings and logout | Owned output retired; independent owner retained |

Lossy output durations include codec-frame padding and are reported as actually
measured; they are not relabeled as six or four exact seconds. Media requests
preserved user playback data and all source hashes. Owned temporary files were
removed and cleanup errors were empty. Captured HTTP response bodies totaled
2580621 bytes. No frontend/browser or GPU acceptance result is inferred from
these backend checks.

## Existing-library upgrade

The [upgrade rescan record](m4c-rescan-upgrade.json) covers the four existing
owned test libraries independently of the new audio verification library.
Their normal administrator scan operations completed without errors:

| Existing library | Media scanned/updated | Added |
| --- | ---: | ---: |
| Direct Playback | 1 / 1 | 0 |
| HLS verification | 1 / 1 | 0 |
| NextUp verification | 3 / 3 | 0 |
| Subtitle verification | 1 / 1 | 0 |

Within those selected libraries, six old-version media snapshots became six
version-3 snapshots, leaving no version-2 media. The selected fourteen catalog
items retained their metadata digest, all ten existing user-data rows were
unchanged, and fourteen fixture files retained their inode, size, mtime, ctime
and mode. The service remained active under the same `goby` process and schema
12. The new audio library was skipped by this upgrade operation, and the
temporary administrator login was revoked after completion.

## Remaining acceptance

The remaining scope includes additional exact-timing/input cases, progressive
PlaybackInfo profiles and video output, packed audio HLS, broader subtitle/profile
behavior, hard worker resource isolation, actual GPU decode/encode, long-form and
third-party-client acceptance, and administrator/backup/recovery completion.
The [implementation progress](progress.md) remains the full project status.
