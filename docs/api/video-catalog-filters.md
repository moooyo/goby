# Video catalog filters

The item query endpoints support `Is4K` and `ExtendedVideoTypes` before counting,
sorting and pagination. They compose with the current user's library and content
policy and with the other supported item selectors.

- `Is4K=true` selects the first non-attached video stream with width at least 3800 pixels.
  `Is4K=false` selects a known positive width below 3800 pixels. Height alone does
  not establish 4K, and missing dimensions do not satisfy either value.
- `ExtendedVideoTypes` accepts a comma-separated list of `None`, `Hdr10`,
  `Hdr10Plus`, `HyperLogGamma`, and `DolbyVision`, with case-insensitive input.
  Entries are alternatives and may match any non-attached video stream. Like the
  original server, a standard query may combine the first stream's resolution
  with a different stream's HDR evidence.
- Dolby Vision facts take precedence over the PQ transfer characteristic. PQ and
  HLG transfer characteristics support `Hdr10` and `HyperLogGamma`. `None` requires
  a known SDR range or a recognized SDR transfer characteristic; unknown color
  metadata is not silently labeled SDR.
- The current prober does not retain a separate HDR10+ dynamic-metadata fact.
  `Hdr10Plus` alone therefore has no proven matches. Its PQ sources still appear
  in the general HDR selection through `Hdr10`.

Malformed booleans, empty selectors, duplicate scalar query keys and unknown
enum values return HTTP 400. This deliberately avoids the original server's
observed invalid-value behavior of returning 500 or ignoring an invalid enum.

## Explicit series and season aggregation

Standard video selectors inspect an item's own streams. A `Series` or `Season`
has no such stream and does not match, for either value of `Is4K`.

`GobyAggregateVideoFilters=true` is an explicit Goby extension for series and
season catalog walls. It selects a folder when at least one ordinary descendant
`Episode` has a single video stream satisfying all video selectors. It does not
combine 4K evidence from one episode or track with HDR evidence from another.

Traversal remains inside the folder's library, requires the episode's registered
root to belong to that library, respects descendant content policy, and excludes
theme media, extras, and their permanent reserved paths. Cyclic parent data
terminates. Other item types continue to use their own streams. Omitting the
extension or setting it to false retains standard behavior.

Example queries:

```text
/emby/Items?Recursive=true&IncludeItemTypes=Movie&Is4K=true
/emby/Items?Recursive=true&IncludeItemTypes=Movie&ExtendedVideoTypes=Hdr10,Hdr10Plus,HyperLogGamma,DolbyVision
/emby/Items?Recursive=true&IncludeItemTypes=Series&Is4K=true&GobyAggregateVideoFilters=true
```

## Original Emby reference

The isolated official `emby/embyserver:4.9.5.0` reference was tested with generated
real video files using `scripts/test-env/player-reference-filters.py`. The script
records results and stream metadata in `artifacts/emby-filter-reference.json`
under its explicitly owned remote acceptance root.

Observed results established that width 3798 does not match `Is4K=true`, width
3800 does, 3840-by-1600 scope video does, and 1920-by-2160 portrait video does
not. Separately encoded PQ and HLG samples matched the corresponding enum;
comma-separated values formed a union. Original Series and Season queries
returned no matches for these video selectors even when descendant episodes
matched. The Goby aggregation flag is not an original Emby API parameter.

Two-track files established the distinction between standard and aggregate
semantics: swapping 4K SDR and 1080p PQ track order changed `Is4K`, while both
files matched `Hdr10`. Marking the second track as default did not change the
resolution result. The standard combined `Is4K=true&ExtendedVideoTypes=Hdr10`
query matched the file whose first track was 4K SDR. The explicit Goby series
aggregation deliberately requires both facts from one matching episode stream.
