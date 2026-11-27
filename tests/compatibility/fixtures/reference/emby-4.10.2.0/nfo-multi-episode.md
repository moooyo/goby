# Multi-episode NFO observation: Emby Server 4.10.2.0

[`nfo-multi-episode.json`](nfo-multi-episode.json) preserves twelve synthetic cases captured on 2026-11-27 from the official Linux amd64 Emby Server 4.10.2.0 package with Nfo Metadata 1.0.88.0. It supplements the [single-document NFO capture](nfo-differential.md) without replacing that reference. These are observations, not instructions to reproduce every upstream fallback.

## Boundaries and observations

- Each case was a separate synthetic series with one physical video under `Season 01`, no embedded descriptive metadata, and a matching NFO unless the case was the no-sidecar control. The service saw media read-only in a private loopback-only namespace. The only local metadata reader was NFO; online metadata, real-time monitoring, automatic refresh, and local writes were disabled.
- The recursive item listing, the series episode endpoint, and item detail showed **one Episode identity per file**, including both `S01E03E04` and `S01E03-E04`. Its parent and source were the original physical season and file. Initial `PlaybackInfo` also returned one source for that item and path; watch state was unchanged across the information request. No streams were requested. This is a descriptor observation, not a playback test.
- The ordered pair returned `IndexNumber=3`, `IndexNumberEnd=4`, both titles joined with `",  "` (two spaces after the comma), both plots joined with two line feeds, and only the first block's TMDB ID. No sidecar or just one block still gave an end number of `4`. The single-file controls without a range filename omitted `IndexNumberEnd`, even when two titles and plots were joined.
- Other cases preserve the observed outcomes for reversed blocks, a second number outside the filename range, missing or duplicate numbers, a truncated second block, and an explicit `<episodes>` wrapper. These remain reference cases even where Goby deliberately requires a stricter input. There was no test of arbitrary root counts or cross-season ranges.
- `initialNfo`, `warmNfo`, and `targetedNfo` hold the exact text after the UTF-8 BOM. The replayer adds the BOM; line endings, block order, and a deliberately missing end tag stay as recorded. An absent initial value means no sidecar; an absent replacement means no change. Both ordinary scans completed. The two warm NFO replacements did not alter the selected official output, so warm expectations reuse `first`. A further replacement followed by the recorded full item refresh (`204`) changed the ordered pair's title, plot and ID without changing the Episode identity or number range. `targeted` is evidence of that specific Emby refresh API, not a Goby replay phase or a promise about normal rescans.

## Offline regression

Run on Linux with a disposable PostgreSQL 17 database (`GOBY_TEST_DATABASE_URL`):

```sh
go test ./internal/server -run '^TestHTTPMultiEpisodeNFOReferenceReplay$' -count=1 -v
```

The replay runs Goby's real scanner, queries, and item projection with synthetic probe facts. It checks the single-record invariant in both listings (including pathless candidates), consistent numbering, physical parent and source, and stable Episode identity after a warm scan. It checks the MediaSources descriptor from the item detail without preparing a playback session; it requires no Emby binary or network. Neither pagination artifacts nor responses with unknown field values are treated as parity. Separate Goby-only assertions require complete rejection of invalid pairs and a warm rescan to take the valid replacement; the official warm result remains unchanged in the fixture.

[Goby's known field-level differences](../../../../../internal/server/testdata/nfo-multi-episode-4.10.2.0-differences.json) are separate from the official inputs and expectations. An exact observed difference is logged, not reported as compatibility; unexpected outcomes fail. A null difference entry records an omitted Goby property, not JSON `null` in its API response. When behavior is aligned, remove the corresponding difference entry rather than changing the official fixture.
