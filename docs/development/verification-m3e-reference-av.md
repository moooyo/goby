# M3e original-client audio and external subtitles on the reference

Status: **reference-only checkpoint**. These results do not establish Goby audio
or subtitle client acceptance. The corresponding Goby journeys remain to be run
against a frozen candidate with an independently owned account.

The [safe machine-readable summary](m3e-reference-av-summary.json) records the
exact report, driver, shared runtime, session-proof helper, setup operator, and
media-manifest SHA-256 values. Full private runtime evidence remains under
`/opt/goby-test/exec-work-m3e/`; passwords, tokens, query values, and browser
storage state are not included in the committed summary.

## Environment and ownership

All validation ran through `ssh test-env` on Linux. The client was the unmodified
Emby Web 4.9.5.0 served by the isolated Emby Server 4.9.5.0 reference. Its process
was PID `332054`, start ticks `357218`, with executable SHA-256
`c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2`.
The browser was Chromium `153.0.8010.12`, driven by Playwright `1.63.0`.

The operator first proved that `m3e-reference-av-client` did not exist, created
that ordinary account, and retained it with marker
`goby-reference-av-client-v1`. Its ID is
`7d777e79b35541889f738bb5f5a8da23`. Initial item-detail reads showed zero position,
zero play count, unplayed state, and no LastPlayedDate for the three media leaves.
Existing account policy/configuration snapshots were unchanged at setup
completion. Setup administrator sessions were logged out and independently
rejected with HTTP 401.

The reference instance contains only the three owned synthetic libraries. This
account uses the established all-folders fixture policy; the drivers separately
require the exact owned movie, MP3, and FLAC item IDs, names, and source paths.
This setup does not verify a fine-grained Emby folder-policy mapping. It does not
change the original viewer's movie06 history or viewer2's research state.

## Observed profiles

| Profile | Evidence | Result |
| --- | --- | --- |
| MP3, 180 seconds | `reference-av-mp3-02/observation.json` | Actual audio element advanced by more than two seconds with readyState 4; UI pause held position; UI seeks reached 116.898 and 53.953 seconds; UI resume advanced; Stop reported 204 |
| FLAC, 180 seconds | `reference-av-flac-01/observation.json` | The same actual audio-element and UI lifecycle checks passed for the separately selected FLAC track |
| External SRT and VTT | `reference-av-subtitles-02/observation.json` | Both tracks were selected through the original Subtitles menu; the opening cue and the 118–128 second cue were visible; UI seeking reached 121.242 seconds; both tracks were returned to Off before stopping |

Both audio profiles used real `GET /emby/Audio/{Id}/universal` requests with
`Range: bytes=0-`, HTTP 206, and the respective `audio/mpeg` or `audio/flac`
response type. The selected track title was independently observed in the
visible player; a retained audio element supplied the clock, pause, duration,
ready-state, and network-state samples. Physical audible output was not measured.

The subtitle requests used the existing documented path with a start-position
segment:

```text
/emby/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/0/Stream.vtt
```

Index 2 (SRT source) returned HTTP 200 `text/vtt`, 187 bytes, SHA-256
`6639a605af385edeaac96cd2aab8a5ee744c60a041e7d4ecadb5f7a86980a67b`.
Index 3 (VTT source) returned HTTP 200 `text/vtt`, 195 bytes, SHA-256
`a658c7564b07cac7e69750a61f7c9a71f83f660ec172adf85417cad5a18cf5ad`.
Both delivered all four known synthetic cues. The client used hidden native
text tracks together with visible rendered caption DOM; the checks do not
incorrectly require native text-track mode `showing` for that renderer.

The final three runs had no local HTTP error responses. The audio runs had no
page errors. The subtitle run retained two `Error: undefined` observations and
three explicitly blocked external requests; those observations were not hidden
or reclassified as successful traffic. Audio runs each blocked one external
request. The network policy permits only the selected loopback origin and its
corresponding WebSocket origin, while allowing data/blob and the original
service worker. External requests are aborted or denied without replacement
success responses.

## State and logout evidence

The dedicated account's legitimate playback history remains in place. Each run
records its actual starting and ending item-detail UserData; no historical
state is reconstructed or cleared to make the result pass. User Configuration,
Policy, and UserSettings hashes were unchanged across each final run. The
subtitle choice was restored to Off.

Each final run ended with an actual UI Sign Out, HTTP 204, and the visible login
screen. The shared session-proof helper bound the exact observed logout token
in memory and separately requested `GET /emby/System/Info`, which returned 401.
That independent request is explicitly labeled as cleanup verification, not as
a client UI action. All three final runs removed their temporary private
storage-state recovery candidate after successful proof.

Earlier `reference-av-mp3-01` and `reference-av-subtitles-01` observations remain
as failed harness-development evidence. They are not counted as passed
profiles. Their UI logout tokens were also independently rejected with 401;
any retained private recovery files must remain private.

## Entry points and reproducibility

The account setup evidence and private alias are:

```text
/opt/goby-test/exec-work-m3e/av-user-setup/completion-report.json
/opt/goby-test/exec-work-m3e/av-user-setup/browser.json
```

The private alias contains only the explicitly owned fixture credentials. It is
root-owned mode 0600 and is not committed. The operator preserves the new
account and its history. A failed initial library-policy calibration is retained
in `av-user-setup/setup-report.json`; the separate completion report is the
successful setup gate.

The executed PowerShell entry commands were:

```powershell
ssh test-env 'env PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright node /opt/goby-test/exec-work-m3e/client-browser-av.mjs --check mp3 --output /opt/goby-test/exec-work-m3e/reference-av-mp3-02'
ssh test-env 'env PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright node /opt/goby-test/exec-work-m3e/client-browser-av.mjs --check flac --output /opt/goby-test/exec-work-m3e/reference-av-flac-01'
ssh test-env 'env PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright node /opt/goby-test/exec-work-m3e/client-browser-av.mjs --check subtitles --output /opt/goby-test/exec-work-m3e/reference-av-subtitles-02'
```

Those output paths are immutable evidence and must not be reused for a rerun.
Choose a new private output directory. The entry point pins the fresh reference
process and reads the acknowledged account setup; it is not yet a generic Goby
fixture operator.

Before adding the independent TV workflow, the six source files in the summary
were copied into `verified-reference-av-source/` under the same remote work
directory, with every SHA-256 checked against the summary. That archive preserves
the exact AV inputs even when the working entry point subsequently gains modes.
Use its `client-browser-av.mjs` to replay the frozen reference AV driver.

The reusable UI exports are `runAudioUI({page, report, snapshot, track})` for
`M3e MP3` or `M3e FLAC`, and
`runSubtitleUI({page, context, report, snapshot, target, movieId})`. The isolated
runtime export is `runGuardedAV({url, credentialsPath, output, workflow})`.
These modules do not modify the main observer or the historical movie06 sources.
The latter remain frozen under
`/opt/goby-test/exec-work-m3e/verified-movie06-source/`.

For a later Goby run, create and verify its own ordinary fixture account and
read the actual library/album/track layout. The observed Goby album label
`Music` differs from the reference label `M3e Synthetic Album`; selector changes
must follow that visible layout and preserve an exact selected-track identity.
Do not substitute API playback or rewrite a response to conceal that difference.
