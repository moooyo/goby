# M4a conversion engine verification

Date: 2026-09-09. This is an engine and reference-evidence increment within M4.
The Emby HLS HTTP adapter, full-duration seek scheduling, policy/configuration
integration, and real-client acceptance remain open. No completed transcoding API
or actual GPU execution is claimed by these results.

## Build and complete Linux suite

The authorized local `go build ./...` passed. All tests and media execution ran
through `ssh test-env`. The final source was transferred to
`/opt/goby-test/verify-m4a-20260909`, which points to its owned tmpfs source copy to
reduce pressure on the test host's root filesystem. GOTMPDIR and TMPDIR used the
separate owned executable scratch mount. Only duplicate Goby build artifacts and
this new owned source snapshot were moved or removed; unrelated data was retained.

The complete command was `go test -race -count=1 -v ./...`. All twelve test-bearing
packages passed with no skipped tests. The retained complete log is
`/opt/goby-test/m4a-race.log`; the earlier targeted log is
`/opt/goby-test/m4a-targeted-race.log`.

| Package | Result | Reported time |
| --- | --- | --- |
| artwork | PASS | 3.062 s |
| config | PASS | 1.012 s |
| database | PASS | 3.119 s |
| events | PASS | 1.102 s |
| identity | PASS | 29.255 s |
| library | PASS | 23.546 s |
| media | PASS | 2.617 s |
| metadata | PASS | 1.750 s |
| playback | PASS | 1.079 s |
| server | PASS | 168.808 s |
| subtitle | PASS | 1.172 s |
| transcode | PASS | 11.611 s |

The [engine guide](transcode-engine.md) documents APIs, scope, limits, hardware
choices, and remaining integration. New tests cover profile-conditioned output,
copy/permission restrictions, selected tracks, bitrate/dimensions/channels,
unknown required facts, container aliases and changed applicability, cache
ownership/symlinks, concurrent deduplication and admission, descriptor ownership,
timeouts, cancellation, reader-aware deletion, storage limits, persistence
failure, and shutdown races. Existing backend tests passed in the same run.

## Real media and PostgreSQL chain

`TestManagerPersistsAndDecodesPlannedMedia` generates an owned eight-second
H.264/AAC source and probes its actual facts. Each mode passes through the real
conversion planner, PostgreSQL admission/repository, manager, FFmpeg process,
measured playlist parser, and leased output reader:

| Mode | Checked output |
| --- | --- |
| Remux | Original H.264 and AAC copied into complete MPEG-TS segments |
| Audio conversion | H.264 copied, audio converted to MP3 |
| Video conversion | H.264 encoded at 96x54, AAC retained |

Every listed segment is independently probed and fully decoded, including both
video and audio. Each completed playlist has several segments, preserves measured
durations within 0.2 seconds of the source, and contains ENDLIST. The PostgreSQL
record reaches completed with positive output bytes and no error code. A repeated
identical plan reuses the completed job and closes the duplicate input descriptor.
The original media SHA-256 and the pre-existing watched/progress data are unchanged.

The separate runner media test also covers video encoding, streamcopy remuxing,
and audio-only MP3 output, with independent full-segment decode. Process tests
cover cancellation of a parent/child/grandchild group and cleanup of surviving
children after a successful parent exit. Environment tests verify that database
and setup credentials are absent from the conversion child environment. No shell
is used for production FFmpeg execution.

PostgreSQL tests use isolated random schemas. They prove fixed scope/source/plan,
current account/auth/playback checks, expiry during a lock wait, irreversible
terminal transitions, concurrent terminal writers, startup interruption, and
existing parent-row retention. Migration concurrency/idempotency tests now expect
schema version 10 and the new encoding_jobs table.

## Unprivileged execution and deployment

The [non-root verifier](../../scripts/test-env/verify-transcode-engine.sh) compiles
the race-instrumented package test binary and executes selected real-media and
process-lifecycle tests as the service's `goby` account. The final execution passed:

- Parent/child/grandchild cancellation and surviving-child retirement.
- Real video encoding, remuxing, and audio-only conversion.
- The complete planner/PostgreSQL/manager/decoder chain for all three modes.

The output is retained at `/opt/goby-test/m4a-nonroot.log`. The verifier's first
attempt stopped before executing tests because the configured PATH omitted
runuser; the wrapper was corrected to use its absolute path and leave the scratch
mount before unmounting. The final run passed, and read-only inspection confirmed
that the owned temporary mount was removed. No production-code fix was needed
after the complete suite.

The maintained deployment script installed the tested source and restarted only
the dedicated Goby service. Readiness passed, PostgreSQL reported schema version
10, and systemd reported User=goby with active/running state. The new engine is
not yet registered as a consumer HLS HTTP service; existing playback capability
flags remain truthful. No new frontend/browser verification is claimed.

## Reference-server evidence

The [HLS study](../research/hls-reference.md) adds 94 independently sanitized
records, bringing the combined reference directory to 436 JSON records. These
are 84 HTTP captures, four media probes, and six runtime observations. Original
684-file and later 790-file raw/export baselines retained their hashes, and each
sanitized export was compared with its private original, including M3U8 bodies.

Four normal-source H.264/AAC segments returned 200 and passed ffprobe plus complete
decode. Main playlists described the complete source with stable segment numbers
and EXT-X-START seek hints. Reference remux and sparse-source segment requests
returned 500, including cases where output files existed. Those failures are
preserved without inventing a root cause or treating them as a desired Goby result.
The source, successful outputs, authentication behavior, timestamp measurements,
and cleanup results are recorded in the research report.

These reference controls and the Goby engine fixtures are different experiments.
They do not constitute a same-input differential HLS pass or a third-party-player
test. The current host has no GPU. VAAPI/QSV/CUDA decode and VAAPI/QSV/NVENC encode
have command/source-level coverage only; real hardware execution remains required.
