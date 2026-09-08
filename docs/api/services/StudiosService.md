# StudiosService

Studio browsing and studio metadata.

**2 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Studios`](#operation-getstudios)
- [`GET /Studios/{Name}`](#operation-getstudiosbyname)

<a id="operation-getstudios"></a>

## GET /Studios

- Operation ID: `getStudios`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Studios/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/StudiosService/getStudios.html).
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Studios/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getstudiosbyname"></a>

## GET /Studios/{Name}

- Operation ID: `getStudiosByName`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Studios~1{Name}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/StudiosService/getStudiosByName.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [BaseItemDto](../models.md#model-baseitemdto) | Operation successful. Returning a BaseItemDto object. | `#/paths/~1Studios~1{Name}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
