# M4f: verified video restart and linear audio history

Verification date: 2026-09-10 (Asia/Shanghai).
Status: **passed for this increment**.

This increment adds private probe-version-6 restart evidence and connects it to
progressive H.264 MP4 playback. It does not complete the whole conversion,
hardware, administrator, or client-compatibility milestone.

## Source and toolchain

The isolated source is `/opt/goby-test/verify-m4f-20260909`, with 298 Go/module
files. It includes the M5b baseline from `e562f1f` and excludes the concurrent
M5c administrator-session changes. Functional, database, media and browser
verification runs through `ssh test-env`; local compilation was authorized.

The official [Go downloads](https://go.dev/dl/) and
[FFmpeg downloads](https://ffmpeg.org/download.html) were checked again on
2026-09-10: Go 1.27.1 and FFmpeg 9.0.1 remain the current stable versions.
PostgreSQL remains 17.11. Integration tests use port 15432, independently of
the retained application database on port 5432. Schema version remains 14.

The exact staged M4f production source compiled for Linux/amd64. Its initial
build identified an incorrectly capitalized existing stream-limit constant;
the production and test references were corrected before compilation passed.

## Runtime contract

The [operating contract](video-fast-seek.md) records source eligibility, private
candidate validation, software-decoder scope, resource bounds and cache
behavior. Scanning prepares optional bounded evidence. PlaybackInfo, HEAD and
argument previews do not execute an index scan or authorize a restart. Run
performs a fresh proof against its borrowed source and actual decoder thread
limit before creating output. Failed optional proof retains the linear path.

Eligible video seeks use a verified input argument, while selected audio reads
the same held file independently and linearly. The original output presentation
seek and common source clock remain in force. This can reduce prefix video
decoding; it does not guarantee constant-time input I/O or a universal startup
deadline. Hardware decoding retains the preceding linear path.

## Focused verification and the multithreaded oracle

The first focused race run passed media (1.831 seconds), playback (1.031 seconds)
and server (9.968 seconds). The only failure was the new two-thread lossy-output
hash comparison. The one-thread MP4, Matroska and MPEG-TS matrix passed for
encoded AAC, copied AAC and video-only output.

The [independent experiment](../research/video-fast-seek/multithreaded-oracle.md)
and its [measurements](../research/video-fast-seek/multithreaded-oracle-evidence.json)
show that two repetitions of the same linear two-thread command already differ
in 42 of 45 decoded-frame hashes; repeating the fast command differs in 41.
Replaying the actual decoder/filter/seek arguments with raw framehash output
instead of lossy encoding gives byte-identical complete records for all 45
frames, including the header, native times, sizes and full-color image hashes.

The corrected regression retains exact final-frame hash comparison for the
single-thread controls. For multiple threads it requires complete pre-encode
equality from recorded production arguments, every final frame's independent
full-color source match and neighboring-frame rejection, strict complete
decoding, exact audio PCM, and the unchanged final timestamp/duration checks.
The existing content bounds were preserved. No production behavior was changed
to address this test-oracle failure.

The updated conversion target passed with the race detector in 10.332 seconds.
Its positive cases explicitly require actual fast producer arguments and a
matching fresh proof, including two decoder threads. Silent linear fallback
cannot satisfy those cases. Negative evidence still requires linear fallback.

Server regressions exercise real scanning through `Server.New`, private
candidate preservation through negotiation and GET, harmless ignored query
injections, zero tool invocations during PlaybackInfo/HEAD, and a technical
probe-five-to-six rescan preserving complete saved metadata and user state.

## Complete suite and deployment

`go test -race -count=1 -v -p 2 ./...` passed **940 top-level tests** across all
twelve tested packages, with zero skipped tests and no race findings. The
command package has no tests. Go and the SSH wrapper both exited zero. The
[complete summary](m4f-full-race-summary.json) preserves package durations and
the log hash. The 298-file Go/module manifest has canonical JSON SHA-256
`0f5e2a93d1b5719bc693bc99b8ddbb2fb3af34f718d729dff02dad6f62f85e7c`.

The [binary deployment audit](m4f-deployment-evidence.json) verifies executable
SHA-256 `5eaf10c9f7ea80aa97c4d3e7d0750c3b26f998671388702ad8d4d1b9be58445d`,
PID 3412620, UID 995, schema 14 and all 298 installed Go/module files. Every
existing row in all seventeen public tables was unchanged across the restart.
All 111 retained administrator assets remained byte-identical, including the
31 files belonging to the current M5b build. The M5c frontend was not deployed
as part of this checkpoint. A private schema-14/probe-5 dump and preceding M5b
binary remain in `/dev/shm/goby-m4f-deployment-backup`.

The [deployed media workflow](m4f-deployed-video-fast-seek.json) passed its first
exclusive attempt. One normal scan of each of five existing libraries upgrades
all eleven media records from probe 5 to 6, preserving complete saved metadata
state and all user data. A 6.37-second requested presentation seek actually uses
a verified 6-second input seek and a second independent linear audio input;
read-only `/proc` observation captured the real producer and proof. The output
strictly decodes to 207 video frames at 160 by 90 and stereo AAC at 48 kHz.
Negotiation and HEAD create no encoding job and expose no private evidence.

Normal playback maintenance expires one preflighted old abandoned playback
record, changing only its allowed state/stop/update fields. No historical row
is deleted, all other existing business rows are preserved, and both owned
logins are revoked. Cleanup reports no errors. The report explicitly separates
this ordinary maintenance from metadata and user-state preservation. The
concurrent M5c browser evidence does not substitute for this M4f verification.

## Remaining scope

Current same-version scan caching does not force reindexing solely because
FFmpeg changes or an index is absent. Runtime rejects stale proof and falls
back; a dedicated rebuild operation remains work to do. Pixel and per-allocation
limits are not aggregate process-memory isolation. Actual GPU execution,
broader media/client coverage, copied-video nonzero seek and the rest of the
planned administrator and release work remain open.
