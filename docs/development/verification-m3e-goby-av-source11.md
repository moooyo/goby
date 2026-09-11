# M3e Goby source11 AV and TV client observations

Status: **blocked at the recorded original-client UI steps**. These observations
do not establish Goby audio, external-subtitle, or TV-browse acceptance. The
[safe summary](m3e-goby-av-source11-summary.json) contains per-run source and
report hashes, the exact candidate identity, state observations, and cleanup
evidence. All execution and validation used `ssh test-env`.

The candidate remained PID 365638, start ticks 1203391, executable SHA-256
`67b846016ebd89baae07f052bc6b2a23220c82e5f9be5df84a32763f74d5ab3b`.
The original Emby Web 4.9.5.0 assets came from the pinned fresh reference. The
dedicated ordinary account was `m3e-goby-av-client`, ID
`34b4c24f6568659af7ce17938fae7f81`. Its native operator receipt and private alias
were verified before any browser request. Existing users, their preferences,
and their media histories were not used as test accounts.

| Profile | Retained run | Observed result |
| --- | --- | --- |
| MP3 | `goby-av-mp3-02` | Login succeeded. The original Music album detail request returned 200, but the page remained blank for the full 15-second observation. No track was selected or decoded. |
| FLAC | `goby-av-flac-01` | The same album initialization blocked the separate FLAC attempt. This does not diagnose FLAC decoding support. |
| External subtitles | `goby-av-subtitles-01` | The real movie player reached paused playback. Its menu showed Off, en, en; the validated SRT/VTT controls were unavailable, and no subtitle response was observed. |
| TV | `goby-av-tv-02` | The library and Series page loaded, and the season menu was used. Additional Specials content duplicated the three owned episodes, so the exact episode-set check failed. |

The candidate currently uses album name `Music` and track name
`M3e Client Audio` for both audio files. The reference uses the embedded
`M3e Synthetic Album`, `M3e MP3`, and `M3e FLAC` tags. That visible metadata
difference is retained. The audio driver can use an explicitly owned item
mapping, but requires a unique actually observed row ID and a matching actual
audio source item ID; it never invents a distinct visible track label or starts
playback through an API.

## Album initialization diagnosis

The separate `goby-av-album-diagnostic-03` and
`reference-av-album-diagnostic-02` runs clicked the original album UI and
captured only the exact owned album GET response. They were explicitly marked
`diagnostic_only`; debugger timing is excluded from playback acceptance.

| Contract | Goby | Reference |
| --- | --- | --- |
| Status and decoded body size | 200, 497 bytes | 200, 998 bytes |
| Type / IsFolder | MusicAlbum / true | MusicAlbum / true |
| ArtistItems | Missing | Array, one item |
| AlbumArtists | Missing | Array, one item |
| Artists | Missing | Array, one string |
| Composers | Missing | Empty array |
| ChildCount | Missing | 2 |
| Visible album content | No heading or track after 15 seconds | Album heading and two tracks |

After the Goby album response, the caught exception first line was:

```text
TypeError: Cannot read properties of undefined (reading '0')
```

The reference did not produce that album exception. Both runs retained their
shared startup/blocked-external exceptions. The collector immediately resumed
each exception and stored only sanitized first lines; it did not inspect source,
stacks, scopes, object properties, or runtime values. Both diagnostic controls
completed without debugger-control errors and ended in UI logout followed by
HTTP 401 rejection of the exact observed token.

The client projection omitted Path on both backends. Ownership follows the
exact original GET route, the response Id, and the verified fixture. A projected
Path, when present, must match the owned directory. Earlier diagnostics that
incorrectly required a DOM card ID or a projected Path remain as failed harness
evidence; their bodies and observed exceptions were not edited.

## Subtitle and TV evidence

Existing item-detail state reads also supplied diagnostic subtitle DTOs, without
extra API requests. Goby subtitle indexes 2 and 3 retained codecs `srt` and `vtt`,
but both Title and DisplayTitle were `en`, and DisplayLanguage was absent.
Both were external text streams with false forced/hearing-impaired flags.
The reference supplied `English (SRT)` and `English (VTT)` display titles.

The TV query observer retained only five explicitly permitted boolean literals.
The same original client requested the following on both servers:

| Request family | Actual literals |
| --- | --- |
| Shows/.../Seasons | IsSpecialSeason=false |
| Main episode Items list | IsStandaloneSpecial=false, IsFolder=false, Recursive=true |
| Specials Items list | IsSpecialEpisode=true, IsFolder=false, Recursive=true |
| Shows/.../Episodes after selection | IsStandaloneSpecial=false |

The Reference control `reference-av-tv-10` completed the same TV journey, with
the previously documented cross-season list. Goby displayed additional duplicate
Specials entries. No query values were inferred or changed, and no response was
rewritten. The complete TV event set contained no playback report or media
stream; nine item-detail UserData snapshots and all preference hashes matched.

The subtitle attempt confirmed actual 404 responses for SpecialFeatures,
Similar, ThemeMedia, and Intros. Those missing auxiliary routes remain visible
in the safe report alongside page errors; they were not hidden or converted
into successful empty responses.

## Cleanup and reproducibility

The main listed audio/TV attempts ended in actual UI logout, HTTP 204, and an
independent HTTP 401 using the exact observed logout token. The open subtitle
menu and the earlier TV01 menu prevented UI logout. Each corresponding private
saved token was separately matched to the new account and receipted fixture,
logged out with HTTP 204, and rejected with HTTP 401. These API cleanups are
explicitly excluded from UI acceptance. No unrevoked session remains from the
new-account runs listed in this checkpoint; retained recovery files stay private.

Configuration, Policy, and UserSettings hashes matched across the final runs.
Legitimate media history in the dedicated account is retained. The original
reference AV and movie06 histories were not reset.

The command shape for a future frozen candidate is:

```text
node /opt/goby-test/exec-work-m3e/client-browser-av.mjs \
  --backend goby --candidate-sha256 <explicit-frozen-sha256> \
  --check <mp3|flac|subtitles|tv> --output <new-private-run-directory>
```

`--check diagnose-album` is a separate diagnostic command. Private credentials
come from `goby-av-browser.json`; the original fixture credentials are not
overwritten. The shared runtime checks target ownership before browser network,
login, workflow, and logout, and after shutdown. A failed pin skips further UI
logout and retains recovery state. Final event counts and request overflow are
checked after browser shutdown. The independent token-proof helper still lacks
an atomic cancellation interface if an instance changes; the recorded runs
passed all checks against the continuously frozen candidate.

Final diagnostic source files were hash-checked and archived under
`/opt/goby-test/exec-work-m3e/verified-source11-av-diagnostic-source/`. Historical
output directories and their report hashes must not be reused or relabeled for
a later candidate.
