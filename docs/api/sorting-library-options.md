# Sorting and dynamic artwork options

Status: source implementation complete; consolidated phase 1 verification is
pending. This document defines the supported behavior, not a claim of complete
Emby configuration parity.

## Sort removal words

`GET /emby/System/Configuration` returns `SortRemoveWords` to an authorized
administrator or application key. Full and Partial server configuration POSTs
accept that field. A supplied array replaces the previous array exactly; `[]`
clears removal rules. Partial omission preserves it. Full server-object omission
resets it to `[]`. Named encoding, subtitles and tasks sections do not own this
field. Existing authority, duplicate field-alias rejection and atomic mixed-body
validation remain applicable.

Native `GET /admin/v1/settings` includes `Sorting` and `SortingDefaults`, each
shaped as `{"SortRemoveWords":[]}`. Native PUT accepts an optional `Sorting`
object alongside its existing required `Revision` and `Overrides`. Omission
preserves sorting. The native reset endpoint accepts `Fields:["Sorting"]`.
The dashboard labels the multiline control **Words removed from sort names**;
one nonempty line supplies one token. Clearing it and saving restores `[]`.

Rules contain at most 32 distinct tokens. Each token must contain 1 through 128
UTF-8 bytes, with no Unicode White_Space or C0/C1 control character. Invalid UTF-8,
null arrays, empty tokens and case-insensitive duplicates are rejected. Spelling
and order are retained; input is not silently trimmed or deduplicated. The
database also requires a one-dimensional, one-based array without null elements.
Empty arrays use PostgreSQL's ordinary canonical empty-array representation.
`settings.ValidateStoredSorting` is the shared strict runtime/archive validator.

Ordinary generated keys use Go 1.27.1 / Unicode 17.0.0 simple lowercase, independent of
PostgreSQL locale. The SQL translation table and browser validation table are
derived from that toolchain's `unicode.CaseRanges`; its `unicode/tables.go`
SHA-256 is `AC0E5D5D1B58ED4B02D7CB4A0085BDEFBDB4F7AAE6FADDDBC9477F8463DA8BD8`.
This is simple lowercase, without normalization, transliteration or full case
folding. A future Unicode-table upgrade must preserve the published migration
and explicitly reconcile stored validation and derivation behavior.

For ordinary items, the default empty rule set preserves Goby's previous `strings.ToLower(Name)`
key, including any retained surrounding whitespace. With rules, matching uses
the trimmed lowercase name. One whole leading token may be removed when it is
followed by Unicode whitespace and a nonempty remainder; the remainder is
trimmed. Tokens are literal strings, not regular expressions. `The Amber` becomes
`amber` for `The`; `Theatre` and a name consisting only of `The` keep their full
lowercase keys. `The The Amber` becomes `the amber`, without repeated stripping.
The original-server fixture establishes array replacement, not this internal
algorithm. The token-removal policy is an explicit Goby contract.

Theme and extra resources have a different historical automatic baseline:
their own filename or embedded song title, with its original case. The same
literal-prefix matcher preserves that baseline and the remaining text's case.
With `The`, a resource named `The Trailer` receives sort key `Trailer`; clearing
rules restores `The Trailer`. An unmatched rule also preserves the original
case. This mode covers permanent active and inactive auxiliary identities, and
the scanner supplies the mode explicitly before a new role association exists.
The auxiliary cache comparison uses that same SQL function, so prefix rules do
not create repeated updates or probes. The owner-derived trailer Name/SortName
wire projection remains read-only and independent of the resource's source
metadata and per-field manual controls.

A settings change rebuilds catalog keys immediately in the same owned database
transaction as the settings CAS, activity record and configuration event. A scan
is not required. Scanning, accepted online refreshes, library creation/rename
and collection creation/rename derive new automatic keys using current rules.
Physical source names remain complete, making a later clear reversible. Manual
SortName overrides and captured locks win. Accepted NFO/online explicit SortName
values remain explicit. Migration conservatively marks an unmatched historical
automatic sort key as explicit until a later source scan/refresh establishes its
current provenance. A Name-only manual edit retains the pre-existing independent
SortName behavior.

For retained auxiliary rows, the provenance comparison uses their own case-
preserving baseline rather than mistaking every uppercase filename for an
explicit custom key. A genuinely different historical key remains protected.

`item_metadata_state.automatic` and `items.sort_name` hold the generated/effective
keys. The sparse `effective` JSON remains the original source projection with
manual controls applied; the rebuild does not invent a source SortName field.
Native metadata detail composes automatic facts and controls and therefore
returns the same effective key used by Items ordering.

The catalog owner serializes settings, scanner and metadata writes. Settings
take the publication lock before the owner transaction, then lock/recheck the
actor and settings row; source work never takes the settings publication lock.
Before each potentially waiting rebuild, scope, journal and library-event SQL
statement, the operation recomputes a PostgreSQL statement timeout of at most
five seconds, keeps any stricter deployment value, and budgets two seconds of
the remaining owner context for rollback. Journal recording is one stored
function statement, so its internal SQL shares that statement deadline. The
library-event flush follows with a fresh budget and cached journal stamp, before
the caller's final authority check; COMMIT does not add another event write.
The original timeout is restored on success or by
transaction rollback. A bounded failure leaves the prior revision and keys
intact and permits retry. The 10,000/100,000-item phase 3 load work must measure
rebuild duration and overlapping scan/edit behavior; no capacity claim is made
before those measurements.

A real rule change emits one catalog resynchronization for the global sorting
policy, rather than item-change claims. Its durable scopes are all existing
physical library root items plus collection items, independent of which hidden
keys happened to change. Current authorization filters each scope on delivery.
The source journal keeps its 4,096-reference and byte/queue bounds; an eligible
notification backlog or scope overflow rolls back the entire settings mutation
and returns retryable `503`. Disabled notifications or no eligible registrations
do not add backlog. With no catalog resources, only the configuration event is
needed. No-op saves emit no new sorting invalidation.

## Embedded audio artwork

The new native `LibraryOptions.EnableEmbeddedArtwork` boolean defaults to true.
It controls the actual embedded-picture extractor for Audio items. Extraction
requires both this option and the existing `EnableLocalImages` master switch.
The native create/edit label is **Extract embedded audio artwork**. An edit
takes effect on subsequent scan visits; existing optional scan admission remains
explicit. No new remote provider, download worker or image synthesis is implied.

Older native create requests containing only `EnableLocalMetadata` and
`EnableLocalImages` keep the new option's true default. Older partial edits
preserve its current value. Disabling extraction does not erase accepted image
bytes, create a synthetic no-picture result, disable directory sidecars, or
change managed/provider/sidecar precedence. Source-revision checks still reject
stale artwork after a media replacement. Re-enabling permits a subsequent scan
to extract missing artwork even when the technical audio probe is cached.

The compatibility adapter advertises the explicit Goby provider identity
`Goby Embedded Artwork`. It does not impersonate an original-server provider.

| Operation | Supported contract |
| --- | --- |
| `GET /emby/Libraries/AvailableOptions` | Authenticated discovery advertises one `TypeOptions` entry for `Audio`, one image fetcher named `Goby Embedded Artwork` with `DefaultEnabled:true`, `SupportedImageTypes:["Primary"]`, and no per-type metadata fetchers or image-size controls. Only authentication query parameters are accepted. |
| `GET /emby/Library/VirtualFolders/Query` | Its library item DTO includes current `LibraryOptions.TypeOptions`, plus the existing `DisabledLocalMetadataReaders` projection and library revision. |
| `POST /emby/Library/VirtualFolders/LibraryOptions` | Administrator/key body `{"Id":"library-id","LibraryOptions":{"TypeOptions":[{"Type":"Audio","ImageFetchers":["Goby Embedded Artwork"]}]}}` enables extraction. The same Audio entry with `ImageFetchers:[]` disables it. Success is empty `204` with the resulting revision in `ETag`. |

Optional `ImageFetcherOrder` is empty or the single advertised name; there is
only one supported dynamic provider. Unknown types, other provider names,
multiple entries, duplicate field aliases and unsupported selector fields reject
the mutation before changing its revision or options. An
explicit `TypeOptions:[]` restores default extraction. Omitted `TypeOptions`
preserves it. A selector write preserves the native image master and NFO reader;
an NFO-only write preserves extraction. Optional `If-Match` uses the same library
revision as the native editor. Without it, the adapter takes a fresh snapshot CAS
and does not retry over a concurrent administrator's changes.

## Primary evidence and migration

The retained [configuration mutation study](../research/configuration-mutation-reference.md)
records two-word, one-word and empty `SortRemoveWords` arrays being replaced
exactly. The pinned SDK declares that server field and the dynamic Name-based
`TypeOptions` / `LibraryTypeOptions` selector DTOs.

Historical first-party [Emby ProviderManager](https://github.com/MediaBrowser/Emby/blob/ed925c3367a47ea32d43e664a7fb3f5ddafbc4b7/MediaBrowser.Providers/Manager/ProviderManager.cs#L384)
applies image-fetcher selection to remote or dynamic image providers; local
directory providers do not take that branch. Its
[AudioImageProvider](https://github.com/MediaBrowser/Emby/blob/ed925c3367a47ea32d43e664a7fb3f5ddafbc4b7/MediaBrowser.Providers/MediaInfo/AudioImageProvider.cs#L22)
is a dynamic embedded-image extractor. The fixed first-party
[Jellyfin BaseItemManager](https://github.com/jellyfin/jellyfin/blob/50866380c95758244f79bd4fbb01b9474e80bd81/MediaBrowser.Controller/BaseItemManager/BaseItemManager.cs#L50)
distinguishes a missing type entry from an explicit empty fetcher selection.
These sources justify selecting the actual embedded consumer; they do not
justify applying this selector to Goby's directory-image importer.

Schema 49 adds `managed_settings.sort_remove_words`, the conservative automatic
sort provenance boolean, and `EnableEmbeddedArtwork:true` to historical library
options without changing either old importer boolean. It extends the existing
activity-field constraint for Sorting and the five separately implemented TV
metadata fields. Historical migrations remain byte-for-byte unchanged. Existing
backups migrate their old two-key library options; schema49 archives retain and
strictly validate all three booleans, exact rule arrays and provenance state.

Source verification targets cover SQL/Go Unicode agreement, token boundaries,
array constraints, metadata precedence and refresh, default/partial/reset/CAS,
rollback and owner survival after statement cancellation, bounded authorized
notifications, actual scanner extraction suppression/re-enable, discovery/DTO
round trips, and historical normal/recovery migration. The composed real browser
journey separately exercises native controls, compatibility selectors, scans,
image bytes and restart. Execution results belong to the phase verification
record.
