# M3e source15 scanned music UI checkpoint

Status: **album browsing and exact track selection were observed; MP3 and FLAC
playback remain blocked**. The [safe report](m3e-source15-scanned-music-summary.json)
records both actual UI attempts, their distinct driver hashes, the failed media
requests, and completed session cleanup. No failed attempt is relabeled as a
successful audio journey.

The candidate remained source15/schema25, PID 438666, start ticks 2005412,
executable SHA-256
`99669e962493ef64f1315d3347b07859d1c376151c9712f48d8629d7e756d60a`.
The original Emby Web 4.9.5.0 resources, reference process, and proxy remained
frozen. Chromium was 153.0.8010.12 with Playwright 1.63.0. All execution and
verification ran through `ssh test-env`.

## Scan binding and actual UI

The independent native Music scan had already completed. Its receipt SHA-256
was `e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b`,
and its report SHA-256 was
`11d6f781476093f14b0f1942037add52d00b08bd7d3f78fb8cec8acbbea9b0a4`.
The browser loader pins that exact receipt, report, and owner document together
with the current fixture state, process, schema, library, and media manifest.
Those three scan files are checked again at the existing instance boundaries.
The scan itself is fixture setup, not client UI acceptance.

The loader supplies only the approved ID/name/path/parent mapping. Independent
catalog and item reads verify it before and after the UI workflow. The actual
client displayed `M3e Synthetic Album`, `M3e Synthetic Artist`, and both tagged
track titles. Each run clicked the selected track's exact visible title inside
the uniquely matching `data-id` row:

| Profile | Actual title | Owned item ID | Result |
| --- | --- | --- | --- |
| MP3 | M3e MP3 | e2da060a0addfafde7f92c291c413f6d | Audio/universal returned 400 |
| FLAC | M3e FLAC | 4d2ec259d9ae5224f46aa6f56ca2b186 | Audio/universal returned 400 |

The physical files remain `Music/M3e Client Audio.mp3` and
`Music/M3e Client Audio.flac`, under the same album ID. The title changes are
therefore bound to the actual scan rather than an unconditional name allowance.
The earlier blank album problem was not reproduced on this scanned candidate.

## Core media failure and bounded diagnostics

Both actual media requests targeted the selected owned item and sent
`Range: bytes=0-`. They received HTTP 400 `application/json`, followed by the
client's Playing, Progress, and Stopped requests receiving HTTP 404. These
automatic reports do not prove that the normal play or stop UI steps passed.
The client displayed a Playback Error dialog saying no compatible streams were
available. Neither workflow reached decoded playback, retained `currentSrc`
proof, time advancement, pause, either seek, resume, or normal UI stop.

The subsequent, independently authorized FLAC run passively captured its
existing request and one 116-byte error response. The response contained
`ResponseStatus.ErrorCode = invalid_audio_request` and
`Message = Check audio parameters and stream identifiers.` Its actual Container
value, reconstructed without losing separators or case, was:

```text
opus,mp3|mp3,mp2,mp3|mp2,aac|aac,m4a|aac,mp4|aac,flac,webma,webm,wav|PCM_S16LE,wav|PCM_S24LE,ogg
```

The remaining admitted values were AudioCodec `aac`, TranscodingContainer `ts`,
TranscodingProtocol `hls`, MaxStreamingBitrate `200000000`, StartTimeTicks `0`,
EnableRedirection `true`, and EnableRemoteMedia `false`. MediaSourceId and the
additional optional numeric fields were absent. All other query values remain
unknown; query keys are retained without credentials or token values.

The collector reads only the exact owned Audio HTTP 400 JSON response with an
explicit Content-Length no greater than 8 KiB. It reserves at most four such
reads and 32 KiB total, uses a two-second deadline, checks the actual byte count
before parsing, and retains only scrubbed error fields. This run used one read
of 116 bytes, with no pending body read at return. No successful media or
authentication response body was read. This is bounded diagnostic collection,
not a claim about a hard Playwright allocation ceiling.

The request demonstrates actual `container|codec` selectors alongside the
reported parser error; the product investigation receives the exact evidence.
It does not establish that every later playback-report failure has an
independent cause. The two Similar/ThemeMedia 404 responses are separate
auxiliary failures. Each run also retains three page errors, rather than hiding
them behind successful album navigation.

## State, cleanup, and repeatability

All three dedicated-account AV UserData snapshots and the Configuration,
Policy, and UserSettings hashes matched before and after each attempt. The
primary viewer and other research users were not used. Every instance check
passed through browser shutdown.

MP3's error dialog blocked the initial UI logout. The exact token from that
run's private saved state was subsequently revoked using the separate cleanup
API, with 204 followed by 401. That is cleanup only, not a UI acceptance pass.
Its saved token fingerprint was then matched read-only to the cleanup proof.

For FLAC, the driver saved the original failure evidence, clicked the unique
observed Got It button within its dialog, and retained the failed workflow
result. Actual UI Sign Out returned 204 and showed the login page; the exact
observed logout token independently returned 401. The retained private state
contains zero stored user credentials. Both failed runs retain their private
state files for audit, and neither has a remaining effective session.

The new loader/main and audio increments received independent static review.
Remote syntax checks and three loader guards passed. The first guard command
completed its assertions but encountered a CRLF shell-heredoc wrapper error;
the preserved second invocation used direct Node standard input and exited
successfully. This transport issue did not touch the candidate or create a
browser session.

Each run preserves nine exact input files:

- `/opt/goby-test/exec-work-m3e/verified-source15-mp3-01-source/`
- `/opt/goby-test/exec-work-m3e/verified-source15-flac-01-source/`

The two run directories are `goby-av-source15-mp3-01` and
`goby-av-source15-flac-01`, below the same private work root. Each contains
`observation.json`; MP3 additionally contains `api-session-cleanup.json`, and
both contain `retained-state-verification.json`. Source12 failures and the
earlier source15 subtitle/TV input archives remain unchanged.

The explicit audio entry parameters now include `--backend goby`,
`--candidate-sha256 99669e962493ef64f1315d3347b07859d1c376151c9712f48d8629d7e756d60a`,
`--music-scan-receipt-sha256 e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b`,
`--check mp3` or `--check flac`, and a new private `--output` directory.
The loader deliberately rejects a different candidate process/state with this
scan binding. A future product upgrade needs an explicitly verified preservation
chain before another audio run; it must not silently adopt the old process as
the new one. No media retry or additional diagnostic UI run was performed after
this FLAC checkpoint.
