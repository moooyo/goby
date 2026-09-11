# M3e source18 audio playback, reporting, and Home navigation

Status: **MP3 core playback and reporting passed, while its original harness
result remains failed; the subsequent FLAC workflow completed in full.** The
[safe summary](m3e-source18-audio-summary.json) keeps these results separate.
Neither original report was rewritten, and MP3 was not replayed after its Home
selector failure.

The observed source18 candidate was PID 506532, start ticks 2681316, executable
SHA-256 `665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b`.
Its explicitly selected upgrade chain was source15 -> source16 -> source18;
source17 was not installed. The existing completed Music scan was inherited
through that chain. Six lineage guards and syntax checks of three files passed
before the MP3 attempt. These are harness checks, not playback observations.

## Media and server-state evidence

Both attempts used actual original-client controls and bound the selected
track to its owned row `data-id` and the retained audio element's `currentSrc`.
Each observed pause, forward and backward slider seeks, resumed advancement,
and Stop. No media time was assigned by the driver.

| Observation | MP3 | FLAC |
| --- | --- | --- |
| Run | `goby-av-source18-mp3-01` | `goby-av-source18-flac-01` |
| Universal response | 206, `audio/mpeg` | 206, `audio/flac` |
| Duration / readyState | 180 seconds / 4 | 180 seconds / 4 |
| Measured initial advancement | 2.037096 seconds | 2.026551 seconds |
| Playing / Progress / Stopped | 1 / 6 / 1, all 204 | 1 / 6 / 1, all 204 |
| Selected track PlayCount | 0 -> 1 | 0 -> 1 |
| Saved PositionTicks | 553559930 | 553650090 |
| LastPlayedDate | `2026-09-11T05:22:08.477343Z` | `2026-09-11T05:32:51.591226Z` |
| Original workflow outcome | `blocked_at_observed_ui_step` | `audio_ui_flow_completed` |

MP3 left the movie, FLAC UserData, and preferences unchanged. FLAC preserved the
new legitimate MP3 history and the movie state; Configuration, Policy, and
UserSettings hashes remained unchanged. The Music library was not scanned
again, and progress or history was not reset.

## MP3 harness failure and the bounded Home correction

The original MP3 report stopped at `return_home` with
`album_card_title_control_ambiguous`. Its core media, controls, eight accepted
playback reports, and saved history remain valid evidence independently of
that original harness failure.

The retained DOM showed the actual Home page: `Goby Client Reference M3e`,
Home/Favorites navigation, and three libraries under My Media. Newly created
Continue Listening history contained `M3e Synthetic Album` and `M3e MP3`, while
Latest M3e Client Music also contained the album link. The driver's whole-page
uniqueness requirement therefore rejected legitimate duplicate links. The MP3
attempt's SPA hash is unknown and is not reconstructed here.

Only the audio driver and main entry were adjusted using this observed DOM.
Two syntax checks passed before the single FLAC attempt with new frozen inputs.
FLAC then completed Home navigation at `/web/index.html#!/home`; the Music
library card matched the receipted `data-id`
`6383d20008836e137559698c29b10395`. Album selection used the Latest M3e Client
Music section to distinguish it from history links. The album card itself did
not expose a `data-id`, so this is not proof of an album DOM ID. The subsequent
track row and `currentSrc` independently proved the selected audio item ID.

## Cleanup, retained evidence, and limits

Both attempts completed actual UI Sign Out with HTTP 204, displayed the login
view, and received HTTP 401 for the exact observed logout token. All five
instance pins passed in each attempt.

Each run still had Similar and ThemeMedia 404 responses and two page errors.
These auxiliary gaps remain separate from the accepted core audio reports and
the Home selector result. MIME is only an HTTP declaration; these observations
do not prove the decoded output codec, physical audible output, or keepalive
behavior. This report makes no claim about the independently run full Go suite.

The original observation files remain under
`/opt/goby-test/exec-work-m3e/`:

- `goby-av-source18-mp3-01/observation.json`: SHA-256
  `da4a6e1fcb3c5c4a37df9c11530ccda196ff9858bf55a3670accd3e202078034`.
- `goby-av-source18-flac-01/observation.json`: SHA-256
  `6ea0de751a92c252d7a844583e5ffeb5d111ebe66826fa231c6baef6a157fd7a`.

The MP3 frozen input archive is `av-source17-chain-input-01/`, manifest SHA-256
`a493b296c427ad3df8c211e57548e4cafb0c19d726d4785437349d2a3e927411`.
That archive name does not identify an installed source17 candidate. The FLAC
archive is `av-source18-home-input-01/`, manifest SHA-256
`8282c964d849b4443f0582f88bdb4a84f5fbe37229b573d03c6574f8eee37dff`.
Both archives are retained under the same remote workspace. This documentation
records the supplied completed-run evidence; writing it performed no HTTP,
live-state inspection, or validation run.
