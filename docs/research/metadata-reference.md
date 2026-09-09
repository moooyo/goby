# Metadata Update, Sorting, and Locks

This bounded M5b study adds **130 `metadata-m5b-*` records**: **106 complete HTTP exchanges**, **24 supporting observations**, and **zero incomplete HTTP exchanges**. The total HTTP response body size was **312,355 bytes**. The preceding **828 records**, represented by **1,656 private raw/export files**, remained byte-for-byte unchanged. SHA-256 checks also preserved **237 existing media paths**, including hard links. The additive reference corpus is therefore **958 records** after this extension. Counts describe reference evidence, not implemented Goby operations. [Audit record](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-audit.json).

The important implementation distinction is that the SDK declares both `SortName` and `ForcedSortName`, but the observed writer applied a custom sorting value only when `LockedFields` contained `SortName` and the request supplied a nonempty `ForcedSortName`. Locking, local NFO import, forced refresh, and reset are separate operations with different observed effects.

## Pinned SDK contract

The machine-readable baseline is the official Emby SDK specification at commit [`bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json), retained in [the local snapshot](../sources/emby-sdk-openapi.snapshot.json). The SDK release association is 4.9.5.0; the running reference separately reported 4.9.5.0. Schema declarations do not establish field persistence, authorization behavior, validation limits, or patch semantics.

The routes below are API-relative; captured requests used `/emby` as the base.

| Method and route | Declared request | Declared response | Source authentication text |
| --- | --- | --- | --- |
| `POST /Items/{ItemId}` | Required path `ItemId`; required `BaseItemDto` body | `200`, empty | User |
| `GET /Items/{ItemId}/MetadataEditor` | Required path `ItemId` | `200`, `MetadataEditorInfo` | User |
| `POST /Items/{Id}/Refresh` | Required path `Id`; query `Recursive`, `MetadataRefreshMode`, `ImageRefreshMode`, `ReplaceAllMetadata`, `ReplaceAllImages`; required `BaseRefreshRequest` body | `200`, empty | User |
| `POST /Items/Metadata/Reset` | Required string query `ItemIds`; no body declared | `200`, empty | Administrator |

The update/editor operations belong to `ItemUpdateService`, refresh to `ItemRefreshService`, and reset to `ItemLookupService`. The snapshot does not define a separate `MetadataService` tag for these operations. The broad `400/401/403/404/500` response lists are schema declarations rather than observed negative cases. All successful mutations in this study returned **204**, not the declared 200. [Update service](../api/services/ItemUpdateService.md), [refresh service](../api/services/ItemRefreshService.md), [first update capture](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-full-fields-and-forced-sort-post.json).

`BaseRefreshRequest` declares only `ReplaceThumbnailImages: boolean`. The `MetadataRefreshMode` definition enumerates `ValidationOnly`, `Default`, and `FullRefresh`, but the route's refresh-mode query parameters have no explicit schema reference or type. The capture used `MetadataRefreshMode=FullRefresh`, `ImageRefreshMode=ValidationOnly`, explicit replacement booleans, and a JSON body containing `ReplaceThumbnailImages:false`. Only the newly created library received a recursive initial refresh; every item-level refresh explicitly used `Recursive=false`.

### Editable-looking fields in BaseItemDto

These properties have no declared per-field required status, null policy, length limit, numeric range, or replacement/merge rule in the pinned model. A server implementation must define such constraints independently.

| Property | Pinned schema | Runtime evidence in this study |
| --- | --- | --- |
| `Name` | string | Explicit names persisted. Direct administrator updates could change a name while `Name` remained locked. |
| `SortName` | string | Alone did not apply the submitted custom value, including under a `SortName` lock. |
| `ForcedSortName` | string | A nonempty value applied under a `SortName` lock. Missing/empty values preserved an existing locked custom value. |
| `OriginalTitle` | string | Explicit value persisted. |
| `Overview` | string | Explicit value persisted; later NFO refresh and reset could replace/clear it. |
| `ProductionYear` | integer, int32 | `2012` persisted; NFO could replace it; reset removed it. |
| `PremiereDate` | string, date-time | `2012-03-04T00:00:00.0000000Z` persisted. |
| `EndDate` | string, date-time | Declared; no meaningful Movie end-date control was performed. |
| `CommunityRating` | number, float | `7.25` persisted. No range or rounding boundary was tested. |
| `CriticRating` | number, float | `83` persisted. No range boundary was tested. |
| `OfficialRating` | string | `PG-13` persisted. |
| `CustomRating` | string | An owned arbitrary string persisted. |
| `ProviderIds` | object with string-valued additional properties | An owned custom provider key/value persisted. |
| `Genres` | array of strings | An owned genre persisted. |
| `Tags` | array of strings | An owned string tag persisted, but the GET projection used `TagItems`. |
| `TagItems` | array of `NameLongIdPair` | A name with `Id:0` resolved to a returned entity ID. It won the one conflicting `Tags`/`TagItems` control. |
| `Studios` | array of `NameLongIdPair` | A name with `Id:0` resolved to a returned entity ID. |
| `People` | array of `BaseItemPerson` | Owned Actor and Director entries persisted; the Actor role persisted. |
| `LockedFields` | array of `MetadataFields` strings | `SortName` and `Name` were tested. NFO import could replace the stored lock list. |
| `LockData` | boolean | Tested independently from field locks. It did not provide unconditional protection against forced replacement. |

`NameLongIdPair` is `{Name: string, Id: integer/int64}`. `BaseItemPerson` declares `Name`, `Id`, `Role`, and `PrimaryImageTag` as strings; `Type` is `PersonType`. Its enum is `Actor`, `Director`, `Writer`, `Producer`, `GuestStar`, `Composer`, `Conductor`, and `Lyricist`. Only Actor/Director creation was observed. In particular, a Studio/Tag item's numeric `Id` and a Person's string `Id` are different schema types.

The pinned `LockData` description refers to an “enable internet providers” value. That wording is not a reliable definition of the observed lock/refresh behavior. `MetadataEditorInfo` declares `ParentalRatingOptions`, `Countries`, `Cultures`, `ExternalIdInfos`, and `PersonExternalIdInfos`; its GET is supporting editor configuration, not a declaration that every `BaseItemDto` field is writable. [Editor capture](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-metadata-editor.json).

The complete `MetadataFields` enum is:

```text
Cast, Genres, ProductionLocations, Studios, Tags, Name, Overview, Runtime,
OfficialRating, Collections, ChannelNumber, SortName, OriginalTitle,
SortIndexNumber, SortParentIndexNumber, CommunityRating, CriticRating,
Tagline, Composers, Artists, AlbumArtists
```

It does **not** enumerate `People`, `ProductionYear`, `PremiereDate`, `EndDate`, or `ProviderIds`. A native administrator API may support locks for its entire editable field set, but those native guarantees must not be represented as already verified Emby mutation compatibility. `Cast` should not be renamed to `People` in an Emby-compatible wire enum without an explicit adapter decision.

## Isolation and evidence

Every runtime action executed through `ssh test-env` in the private network namespace of `goby-emby-reference.service`. The recorder read `MainPID` and `PrivateNetwork` from systemd at runtime and verified that its namespace matched the service rather than the host. The service remained PID `3131777` with `PrivateNetwork=yes` throughout the study. No host-facing listener, server configuration, old library option, user policy, or existing credential file was changed.

The new `Metadata M5b Owned Fixture` library points only to a marked directory under execution scratch. Its single synthetic 64x36 H.264 MP4 lasts 0.6 seconds and is **1,555 bytes**. The final source directory, including its NFO and ownership marker, is **1,723 bytes**. The library disables real-time monitoring, automatic refresh, metadata saving, and online metadata/image fetchers; the private network namespace also prevents provider network access. [Source provenance](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-source-provenance.json).

Before every item mutation, a fresh read required the exact item ID and exact new source path to occur within the new library. Library creation required a new name/path and an ID distinct from all existing libraries. The recorder allowlist rejected global refresh, old-item mutation, user-policy changes, and other writes. Authentication used a new recorder-device session for the existing reference administrator, and that session was explicitly logged out afterward.

Private unsanitized request/response records and credentials remain in a remote directory. Responses retain complete parsed JSON or text and the measured wire-byte count/hash; this recorder does not claim byte-exact preservation of JSON whitespace. Only structurally audited, sanitized `metadata-m5b-*` exports were copied into the repository. Redaction preserved object keys, array lengths, numbers, booleans, nulls, and nonsensitive strings. The final audit compared all preceding raw/export hashes and old media hashes, and compared old library names, locations, and options. The new library and tiny owned source were retained: the pinned delete operation does not declare its targeting parameters, so the recorder did not guess an undocumented deletion request. New metadata entity names were uniquely prefixed; resetting the owned item removed its links, but this study did not claim orphan-entity deletion.

## Sorting controls

| Control | Observed result |
| --- | --- |
| Set a new Name, a different SortName, and a different ForcedSortName, with no sort lock | Both returned sorting fields followed the new Name. |
| Change Name with both sort fields missing, without a sort lock | Both returned sorting fields followed the new Name. |
| Supply only SortName, without a sort lock | The submitted SortName did not become the returned sorting value. |
| Set `LockedFields:["SortName"]` and nonempty ForcedSortName | Both returned sorting fields became the supplied ForcedSortName, not the conflicting SortName. |
| Keep the sort lock; change Name; omit both sorting fields | The previous custom sorting value remained. |
| Keep the sort lock; change Name; send ForcedSortName as an empty string | The previous custom sorting value remained. |
| Supply only SortName with a sort lock | The prior sorting value remained; the submitted SortName did not apply. |
| Remove the sort lock in a later Name update | The returned sorting fields again followed Name. |

The relevant complete exchanges are [unlocked conflicting sort values](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-full-fields-and-forced-sort-after.json), [locked ForcedSortName](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-locked-sort-and-tagitems-after.json), [locked fields missing](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-locked-sort-fields-missing-after.json), [locked empty ForcedSortName](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-locked-sort-forced-empty-after.json), and [SortName alone under a lock](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-sortname-only-under-lock-after.json).

This establishes a coupling between the sort lock and the writable override for these Movie updates. It does not establish locale collation, article removal, arbitrary Unicode normalization, null semantics, or behavior for all item types. A native API can expose an optional sorting override with a clear “follow Name” default, but that is an explicit native contract rather than a direct alias of every observed wire field.

## Tags and other metadata

The initial `Tags:["M5b Owned Tag"]` update succeeded. The returned full item omitted the legacy `Tags` property and included `TagItems:[{Name:"M5b Owned Tag",Id:59}]`. Later, a request supplied conflicting `Tags` and `TagItems`; the returned `TagItems` used the submitted TagItems entry with a newly resolved ID. Therefore, absence of the `Tags` key was **not** evidence that the write was ignored. The initial supporting state observations selected only `Tags`; the complete HTTP bodies retain the authoritative `TagItems` projection. [Initial tag projection](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-full-fields-and-forced-sort-after.json), [conflicting tag representations](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-locked-sort-and-tagitems-after.json).

The scalar, provider, genre, studio, and person values in the initial full update were read back successfully. Studio and Person names resolved to newly created entity IDs. Subsequent NFO refreshes cleared collection fields absent from the NFO, while the observed OriginalTitle and custom ProviderIds survived those refreshes until reset. These samples do not establish a universal “replace all absent fields” rule or collection merge/deduplication semantics.

## Field locks, LockData, and refresh

The flat library initially ignored a generic `movie.nfo`. Renaming only that owned file to the media's matching basename, followed by a targeted refresh, imported the NFO title, overview, and year. All later refresh controls used that proven named sidecar. [Initial detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-initial-detail.json), [named-sidecar control](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-named-nfo-control-after-2.json).

The NFO deliberately contained title, plot, and year, with **no lock declarations**. This omission matters to the outcomes:

| Starting state and action | Observed result after bounded refresh polling |
| --- | --- |
| `LockedFields:["Name"]`, `LockData:false`; change NFO title/overview/year; FullRefresh with ReplaceAllMetadata=true | Manual Name survived, while overview/year changed to NFO values. SortName/ForcedSortName followed the NFO title. The returned LockedFields became empty. |
| Reestablish `LockedFields:["Name"]`; immediately POST a different Name while preserving the lock | The explicit administrator edit changed Name; LockedFields still contained Name. |
| `LockData:true`, no field locks; change NFO; FullRefresh with ReplaceAllMetadata=false | Manual Name/Overview survived, but the returned LockData became false. |
| Reestablish `LockData:true`; FullRefresh with ReplaceAllMetadata=true | NFO Name/Overview/year replaced the manual values and LockData became false. |

[Field-lock refresh](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-name-lock-full-refresh-after-2.json), [manual edit with a confirmed active name lock](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-manual-edit-confirmed-name-lock-after.json), [non-replacing LockData refresh](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-lockdata-refresh-after-2.json), [reestablished LockData forced replacement](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-confirmed-lockdata-replace-all-after-2.json).

An earlier record named `manual-edit-while-name-locked` occurred after NFO import had already cleared that lock; it is **not** the evidence for manual override while locked. Likewise, the first `lockdata-replace-all-refresh` followed a prior refresh that had already cleared LockData. The later reestablished-lock controls above resolve both sequencing ambiguities. Each refresh was observed through three complete detail reads over 2.5 seconds. The samples show the bounded observed outcomes, not a timing guarantee for other providers or files.

These results do not support treating `LockData` as an unconditional write prohibition. Field locks did not block explicit administrator edits. NFO import could itself replace lock state, and forced metadata replacement could override the global LockData setting. Native administrator locks can be made more durable and predictable, but that must be documented as native behavior.

## Reset

`POST /Items/Metadata/Reset?ItemIds=<owned item>` returned 204. The observed item then used the filename stem, including its `(2026)` suffix, as Name and both sorting fields. Overview, production year, original title, and provider IDs were cleared; genre/tag/studio/person links became empty; LockedFields was empty and LockData false. The reset did not automatically restore the named NFO values during the bounded observation. Source media bytes remained unchanged. [Reset result](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/metadata-m5b-reset-after-2.json).

No multi-item reset, concurrent writer conflict, malformed value/range matrix, non-administrator mutation, filesystem NFO saver, or online provider refresh was tested. The SDK's user-authentication text for update/refresh therefore remains distinct from actual write-permission evidence: every mutation here used the authorized synthetic reference administrator.

## Recorder

[reference-metadata.py](../../scripts/test-env/reference-metadata.py) contains the bounded one-time recorder. Its stages are `prepare`, `setup`, `nfo_control`, `sort`, `sort_locked`, `locks`, `confirm`, and `finish`. It refuses to overwrite its evidence or reuse an existing owned capture directory. A replay must use an explicitly reviewed new capture identity; it must not rerun the setup stage against retained state.

The native M5b editing implementation, validation, revision handling, durable field locks, and reset behavior require their own tests. This reference study is evidence for design decisions, not a declaration that the Emby mutation endpoints or all their lock semantics have been implemented in Goby.
