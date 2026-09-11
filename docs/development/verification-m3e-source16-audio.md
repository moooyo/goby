# M3e source16 audio controls and rejected playback reports

Status: **MP3 and FLAC media playback and controls were observed, but both full
audio workflows remain failed because playback-state reports return 404**.
The [safe summary](m3e-source16-audio-summary.json) preserves the two original
runs and their completed session cleanup. The earlier
[source15 failure](verification-m3e-source15-scanned-music.md) is unchanged.

The frozen source16 candidate was PID 472843, start ticks 2393606, boot
`6bdfc486-7bc8-412f-82b5-70095a09dde7`, executable SHA-256
`ce87ea993696ab107837ba61475977aab1e2d8c56a9a2a287350b11d9d34feb6`.
It retained schema25 and the already scanned owned Music fixture. The browser
used the original Emby Web 4.9.5.0 resources, Chromium 153.0.8010.12, and
Playwright 1.63.0. All execution and checks ran through `ssh test-env`.

## Observed media behavior

Both runs selected the exact tagged title in the uniquely matching owned
`data-id` row and retained the actual audio element. Its `currentSrc` matched
the same selected item and target origin throughout the measured controls.
Each actual universal request sent `Range: bytes=0-` and received HTTP 206.

| Observation | MP3 | FLAC |
| --- | --- | --- |
| Run | goby-av-source16-mp3-01 | goby-av-source16-flac-01 |
| Response Content-Type | audio/mpeg | audio/flac |
| Duration / readyState | 180 seconds / 4 | 180 seconds / 4 |
| Measured initial advancement | 2.033423 seconds | 2.037838 seconds |
| Paused position, unchanged over one second | 3.117781 seconds | 3.121529 seconds |
| Actual forward slider seek | 117.093 seconds | 117.093 seconds |
| Actual backward slider seek | 53.042 seconds | 53.042 seconds |
| Position after resumed advancement | 55.252691 seconds | 55.245341 seconds |

The browser controls performed these actions; the driver did not assign media
time, call an internal client API, synthesize a media request, or alter a
response. MIME is a server declaration, and the evidence does not measure the
decoder's output codec or physical audible output. No successful response body
was read. The same original Container selector list that failed in source15
was now accepted, including its `container|codec` entries.

## Remaining core failure

In each run, the client's Playing request, six Progress requests, and Stopped
request all returned 404. The actual Stop control paused/reset the media
element, but the driver correctly stopped at `stopped_report_rejected` rather
than accepting that local state as a server-accepted stop.

All dedicated-account AV UserData snapshots remained unchanged. In particular,
the played audio did not acquire the expected saved history. This is consistent
with the rejected playback-state reports and is not a persistence success.
Configuration, Policy, and UserSettings hashes also remained unchanged.

Similar and ThemeMedia each returned an additional 404; these are auxiliary
requests and are reported separately from the playback-state failure. Each run
retained two page errors. Successful delivery and functioning media controls do
not override these remaining failures or establish the full M3e audio journey.

The frozen observer recorded playback-report field names only. It did not
retain the PlaySessionId, MediaSourceId, ItemId, token, or device values, nor
their cross-request equality. It also did not read playback-report 404 error
bodies. Those missing observations cannot be reconstructed from the original
reports. Any subsequently authorized minimal diagnostic must use a new run and
new frozen inputs instead of changing these records.

## Upgrade binding, checks, and cleanup

The loader's explicit single-upgrade path connected the completed scan to the
current candidate without repeating the scan. The raw `before-state.json`
SHA-256 was
`4321825c58793a7ca94129feb87a7dcb29298316e7dd6777c2d06c8837117e4f`,
exactly the state hash in the old scan receipt. The old scan still reports the
source15 binary and process. The separately supplied completed-upgrade and
report hashes bind source16:

- Evidence directory: `client-upgrade-b7bc1d14528135c429ec86de66a62d65`.
- Completed SHA-256: `cffa700f67bccfa917077813cd34646cba686cb063d9996fc8721f8fd054c9b0`.
- Report: `client-fixture-report-ce8225800770de91fd8b5f6b.json`.
- Report SHA-256: `ad0bdc1358aeab57621a6bb9bdfc3ebec253c65bcc9eaf3c652bc0928ecd5ea3`.

The loader checks the exact completed object, current-state projection, stable
fixture identities, fixed operator hash, and the new and old process relation.
It does not search upgrade history or read full snapshots containing unrelated
credentials. The fixed operator's completed record follows its full
preservation comparison; equality of row counts alone is not used as that
proof, and database digests containing capture timestamps are not incorrectly
required to match.

Independent static review, remote syntax checks of the three new/changed
files, and all seven read-only lineage guards passed before either UI run.
All five instance checks per run passed through browser shutdown. These guards
are harness checks, not client acceptance results.

Both attempts completed actual UI Sign Out with HTTP 204, displayed the login
page, and independently received HTTP 401 using the exact observed logout
token. Their retained private storage states were inspected read-only and
contained zero stored user credentials. No effective session remained, and no
cleanup API call was needed for these two attempts.

The ten frozen driver/guard inputs are retained in
`/opt/goby-test/exec-work-m3e/av-source16-lineage-input-01/`; its
`frozen-inputs.json` SHA-256 is
`bd3e6ad559710c776e5db7017aff1b4cef9e94be0b5ab639ebfbcb2c35986514`.
Each run's `completed-evidence.json` records that all nine driver hashes match
that archive, together with the signed-out private-state check. Original run
reports, screenshots, source15 evidence, and prior input archives are preserved.

The new explicit CLI selectors are `--music-upgrade-directory`,
`--music-upgrade-completed-sha256`, `--music-upgrade-report`, and
`--music-upgrade-report-sha256`, in addition to the exact candidate SHA, original
scan receipt SHA, profile, and new private output directory. No service or
fixture mutation occurs in the loader or its guard script.
