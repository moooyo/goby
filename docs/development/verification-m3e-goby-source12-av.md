# M3e source12 AV checkpoint and contract diagnostics

Status: **ordinary TV browsing verified; audio and SRT playback remain
blocked**. The [safe AV summary](m3e-source12-av-summary.json) records the exact
source12 candidate and each retained run. Execution and syntax checks were
performed only through `ssh test-env`; production media and other users were
not modified.

Source12 was PID 403472, start ticks 1597941, schema 24, executable SHA-256
`eb9ac18e7adc7d9738c5bb9e37f01269b121027527cbe1a082efc082cf61cf81`.
The original client remained Emby Web 4.9.5.0 on Chromium 153.0.8010.12 and
Playwright 1.63.0. All instance checks passed while the candidate and proxy were
explicitly frozen.

| Journey | Result |
| --- | --- |
| MP3 | The Music album page stayed visibly blank for 15 seconds. No track-row identity or audio decoding was reached. |
| FLAC | Not repeated after the shared album page blocked MP3. FLAC decoding and the new row adapter are not verified on source12. |
| SRT/VTT | SRT could now be selected by its correct display label, but its native SRT response did not render a cue. The subsequent VTT portion of the full journey was not reached. |
| Ordinary TV | Two normal seasons, three normal episodes, details, and Home completed, with unchanged UserData and successful logout proof. See the [scoped TV checkpoint](verification-m3e-goby-source12-tv.md). |

## Album diagnosis

The source12 album response now includes ArtistItems, AlbumArtists, Artists, and
Composers as arrays, and ChildCount is 2. All four arrays are empty. The safe
captured exception changed from source11's undefined index access to:

```text
TypeError: Cannot read properties of undefined (reading 'Name')
```

`goby-av-source12-album-diagnostic-01` captured the exact owned album GET with
HTTP 200 and 575 decoded bytes, SHA-256
`981855feaa34ba57626604b0a0f2f925fb8a51cf617b36c59a8ad8c8d8459f64`.
Both its DOM text and screenshot confirmed that the content area remained
blank. This is not inferred solely from a restricted track selector. The run
was diagnostic-only and ended with actual UI logout and token rejection.

## SRT delivery comparison

The two additional original-client journeys are captured in
[the subtitle contract report](m3e-source12-subtitle-contracts.json). They used
one fresh owned browser session per backend and the same visible sequence:
movie, play, pause, English (SRT), Off, Back/stop, and logout. These short
diagnostic journeys do not replace complete playback acceptance.

The actual requests on both backends supplied the same six SubtitleProfiles,
including `vtt` with `Method=External` and `AllowChunkedResponse=true`. Three
PlaybackInfo requests preceded SRT selection: a non-playback request without a
subtitle index, a playback request with query `SubtitleStreamIndex=-1`, and
another non-playback request without an index. Request-body indexes were
absent. Each request and response has its own recorded time and phase.

| Response or action | Goby source12 | Reference |
| --- | --- | --- |
| SRT stream Codec / DeliveryMethod | srt / External | srt / External |
| PlaybackInfo SRT DeliveryUrl pathname | `/Videos/{item}/{source}/Subtitles/2/0/Stream.srt` | `/Videos/{item}/{source}/Subtitles/2/0/Stream.vtt` |
| Last pre-selection PlaybackInfo response | 285.715 ms | 289.495 ms |
| SRT click invocation | 1624.374 ms | 1688.810 ms |
| Actual subtitle GET begins | 1969.298 ms, Stream.srt | 2034.034 ms, Stream.vtt |
| Delivered response | 200 text/plain | 200 text/vtt |
| Opening cue | Not observed | Visible in the original UI |

There was no intervening PlaybackInfo request between the last pre-selection
response and the subtitle GET. A later item-detail response before selection
also differed: Goby returned native subtitle URLs and MediaSources, whereas
the reference projection omitted MediaSources and MediaStreams. The evidence
therefore preserves both response sources; it does not infer which in-memory
client object won an update.

The earlier full subtitle attempt additionally hashed the returned 212-byte SRT
as `aff96165b6479ebc0710e56faa52df525b13ea7dbc8f44c6fbb6d91e75f9838d`,
matching the owned source file. The collector never changed that URL or body to
force WebVTT. The actual native track had no active cue at the paused opening
position despite successful HTTP delivery.

## Reference music navigation contract

The Reference diagnostic continued in the same login session through actual
Home, Music library, Albums, the owned album, and Home controls. The
[music contract report](m3e-reference-music-navigation-contract.json) contains
only approved parameter values, safe DTO fields, and the exact request phases.
It confirmed artist Name `M3e Synthetic Artist` with resource Id `12` in both
ArtistItems and AlbumArtists; the album resource Id was `13`.

| Actual Items query purpose | Captured values |
| --- | --- |
| Album children | ParentId=13 |
| Other albums for the album artist | AlbumArtistIds=12, ExcludeItemIds=13, IncludeItemTypes=MusicAlbum |
| Containers containing this album | ListItemIds=13, IncludeItemTypes=Playlist,BoxSet, SortBy=SortName |
| Album music videos | AlbumIds=13, IncludeItemTypes=MusicVideo |

The other-album and music-video queries both used the ordered tuple
`ProductionYear,PremiereDate,SortName` with
`Descending,Descending,Ascending`. These combinations and ListItemIds were
observed directly, not derived from parameter names or guessed IDs. Missing,
empty, invalid, and valid values remain distinguishable. Other query values,
including authentication metadata, were not retained.

## Evidence and cleanup

An independent static review preceded the final remote syntax checks and the
two authorized diagnostic runs. JSON capture was restricted to the exact owned
item-detail and PlaybackInfo routes: at most 12 bodies, 256 KiB parsed per body,
and 1 MiB total. The limits bound parsing and retained projections; they do not
claim a hard allocation cap inside Playwright. Both captures converged with
complete request-profile and response projections and no observer overflow.

Both diagnostics performed real Off clicks and observed no showing native track
or known visible cue, then Back with a Stopped 204 response. The report does not
infer the menu's selected state. Both ended with UI logout 204 and independent
401 rejection of the exact logout token. All source12 sessions in this run set
were cleaned up, and legitimate history in the dedicated AV accounts remains.

The final nine diagnostic source files were hash-checked and archived under
`/opt/goby-test/exec-work-m3e/verified-source12-contract-diagnostic-source/`.
All earlier output directories and their hashes remain unchanged. The TV result
is limited to the ordinary fixture; no positive-special or Standalone behavior
is asserted.
