# TagService

Browse facets for tags, codecs, containers, item types, languages, years, and prefixes; tag changes.

**14 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Artists/Prefixes`](#operation-getartistsprefixes)
- [`GET /AudioCodecs`](#operation-getaudiocodecs)
- [`GET /AudioLayouts`](#operation-getaudiolayouts)
- [`GET /Containers`](#operation-getcontainers)
- [`GET /ExtendedVideoTypes`](#operation-getextendedvideotypes)
- [`POST /Items/{Id}/Tags/Add`](#operation-postitemsbyidtagsadd)
- [`POST /Items/{Id}/Tags/Delete`](#operation-postitemsbyidtagsdelete)
- [`GET /Items/Prefixes`](#operation-getitemsprefixes)
- [`GET /ItemTypes`](#operation-getitemtypes)
- [`GET /StreamLanguages`](#operation-getstreamlanguages)
- [`GET /SubtitleCodecs`](#operation-getsubtitlecodecs)
- [`GET /Tags`](#operation-gettags)
- [`GET /VideoCodecs`](#operation-getvideocodecs)
- [`GET /Years`](#operation-getyears)

<a id="operation-getartistsprefixes"></a>

## GET /Artists/Prefixes

- Operation ID: `getArtistsPrefixes`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Artists~1Prefixes/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getArtistsPrefixes.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[NameValuePair](../models.md#model-namevaluepair)&gt; | Operation successful. Returning a NameValuePair[] object. | `#/paths/~1Artists~1Prefixes/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1Prefixes/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1Prefixes/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1Prefixes/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1Prefixes/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1Prefixes/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getaudiocodecs"></a>

## GET /AudioCodecs

- Operation ID: `getAudiocodecs`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1AudioCodecs/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getAudiocodecs.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1AudioCodecs/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioCodecs/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioCodecs/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioCodecs/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioCodecs/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioCodecs/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getaudiolayouts"></a>

## GET /AudioLayouts

- Operation ID: `getAudiolayouts`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1AudioLayouts/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getAudiolayouts.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1AudioLayouts/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioLayouts/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioLayouts/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioLayouts/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioLayouts/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1AudioLayouts/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getcontainers"></a>

## GET /Containers

- Operation ID: `getContainers`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Containers/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getContainers.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1Containers/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Containers/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Containers/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Containers/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Containers/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Containers/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getextendedvideotypes"></a>

## GET /ExtendedVideoTypes

- Operation ID: `getExtendedvideotypes`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1ExtendedVideoTypes/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getExtendedvideotypes.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1ExtendedVideoTypes/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ExtendedVideoTypes/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ExtendedVideoTypes/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ExtendedVideoTypes/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ExtendedVideoTypes/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ExtendedVideoTypes/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidtagsadd"></a>

## POST /Items/{Id}/Tags/Add

- Operation ID: `postItemsByIdTagsAdd`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Tags~1Add/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/postItemsByIdTagsAdd.html).
- Consumes: `application/json`, `application/xml`.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [UserLibrary.AddTags](../models.md#model-userlibrary-addtags) | not declared | not declared |

### Request body and model references

- `body`: [UserLibrary.AddTags](../models.md#model-userlibrary-addtags).
  Source pointer: `#/paths/~1Items~1{Id}~1Tags~1Add/post/parameters/1`.

Direct schema references in all request parameters:

- [UserLibrary.AddTags](../models.md#model-userlibrary-addtags).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Tags~1Add/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Add/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Add/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Add/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Add/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Add/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidtagsdelete"></a>

## POST /Items/{Id}/Tags/Delete

- Operation ID: `postItemsByIdTagsDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Tags~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/postItemsByIdTagsDelete.html).
- Consumes: `application/json`, `application/xml`.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [UserLibrary.RemoveTags](../models.md#model-userlibrary-removetags) | not declared | not declared |

### Request body and model references

- `body`: [UserLibrary.RemoveTags](../models.md#model-userlibrary-removetags).
  Source pointer: `#/paths/~1Items~1{Id}~1Tags~1Delete/post/parameters/1`.

Direct schema references in all request parameters:

- [UserLibrary.RemoveTags](../models.md#model-userlibrary-removetags).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Tags~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Tags~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsprefixes"></a>

## GET /Items/Prefixes

- Operation ID: `getItemsPrefixes`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1Prefixes/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getItemsPrefixes.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[NameValuePair](../models.md#model-namevaluepair)&gt; | Operation successful. Returning a NameValuePair[] object. | `#/paths/~1Items~1Prefixes/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Prefixes/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Prefixes/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Prefixes/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Prefixes/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Prefixes/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemtypes"></a>

## GET /ItemTypes

- Operation ID: `getItemtypes`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1ItemTypes/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getItemtypes.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1ItemTypes/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ItemTypes/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ItemTypes/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ItemTypes/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ItemTypes/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1ItemTypes/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getstreamlanguages"></a>

## GET /StreamLanguages

- Operation ID: `getStreamlanguages`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1StreamLanguages/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getStreamlanguages.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1StreamLanguages/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1StreamLanguages/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1StreamLanguages/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1StreamLanguages/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1StreamLanguages/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1StreamLanguages/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsubtitlecodecs"></a>

## GET /SubtitleCodecs

- Operation ID: `getSubtitlecodecs`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1SubtitleCodecs/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getSubtitlecodecs.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1SubtitleCodecs/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1SubtitleCodecs/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1SubtitleCodecs/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1SubtitleCodecs/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1SubtitleCodecs/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1SubtitleCodecs/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-gettags"></a>

## GET /Tags

- Operation ID: `getTags`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Tags/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getTags.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1Tags/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Tags/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Tags/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Tags/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Tags/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Tags/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideocodecs"></a>

## GET /VideoCodecs

- Operation ID: `getVideocodecs`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1VideoCodecs/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getVideocodecs.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1VideoCodecs/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1VideoCodecs/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1VideoCodecs/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1VideoCodecs/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1VideoCodecs/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1VideoCodecs/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getyears"></a>

## GET /Years

- Operation ID: `getYears`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Years/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/TagService/getYears.html).
- Consumes: not declared.
- Produces: `application/json`, `application/xml`.

### Source authentication declarations

```json
{
  "securityDeclared": true,
  "security": [
    {
      "apikeyauth": []
    },
    {
      "embyauth": []
    }
  ],
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserLibrary.TagItem](../models.md#model-queryresult_userlibrary-tagitem) | Operation successful. Returning a QueryResult<TagItem> object. | `#/paths/~1Years/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Years/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Years/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Years/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Years/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Years/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
