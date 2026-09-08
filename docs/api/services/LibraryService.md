# LibraryService

Library queries, file downloads, metadata, deletion, and refresh control.

**31 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Albums/{Id}/Similar`](#operation-getalbumsbyidsimilar)
- [`GET /Artists/{Id}/Similar`](#operation-getartistsbyidsimilar)
- [`GET /Games/{Id}/Similar`](#operation-getgamesbyidsimilar)
- [`DELETE /Items`](#operation-deleteitems)
- [`DELETE /Items/{Id}`](#operation-deleteitemsbyid)
- [`GET /Items/{Id}/Ancestors`](#operation-getitemsbyidancestors)
- [`GET /Items/{Id}/CriticReviews`](#operation-getitemsbyidcriticreviews)
- [`POST /Items/{Id}/Delete`](#operation-postitemsbyiddelete)
- [`GET /Items/{Id}/DeleteInfo`](#operation-getitemsbyiddeleteinfo)
- [`GET /Items/{Id}/Download`](#operation-getitemsbyiddownload)
- [`GET /Items/{Id}/File`](#operation-getitemsbyidfile)
- [`GET /Items/{Id}/Similar`](#operation-getitemsbyidsimilar)
- [`GET /Items/{Id}/ThemeMedia`](#operation-getitemsbyidthememedia)
- [`GET /Items/{Id}/ThemeSongs`](#operation-getitemsbyidthemesongs)
- [`GET /Items/{Id}/ThemeVideos`](#operation-getitemsbyidthemevideos)
- [`GET /Items/Counts`](#operation-getitemscounts)
- [`POST /Items/Delete`](#operation-postitemsdelete)
- [`GET /Items/Intros`](#operation-getitemsintros)
- [`GET /Libraries/AvailableOptions`](#operation-getlibrariesavailableoptions)
- [`POST /Library/Media/Updated`](#operation-postlibrarymediaupdated)
- [`GET /Library/MediaFolders`](#operation-getlibrarymediafolders)
- [`POST /Library/Movies/Added`](#operation-postlibrarymoviesadded)
- [`POST /Library/Movies/Updated`](#operation-postlibrarymoviesupdated)
- [`GET /Library/PhysicalPaths`](#operation-getlibraryphysicalpaths)
- [`POST /Library/Refresh`](#operation-postlibraryrefresh)
- [`GET /Library/SelectableMediaFolders`](#operation-getlibraryselectablemediafolders)
- [`POST /Library/Series/Added`](#operation-postlibraryseriesadded)
- [`POST /Library/Series/Updated`](#operation-postlibraryseriesupdated)
- [`GET /Movies/{Id}/Similar`](#operation-getmoviesbyidsimilar)
- [`GET /Shows/{Id}/Similar`](#operation-getshowsbyidsimilar)
- [`GET /Trailers/{Id}/Similar`](#operation-gettrailersbyidsimilar)

<a id="operation-getalbumsbyidsimilar"></a>

## GET /Albums/{Id}/Similar

- Operation ID: `getAlbumsByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Albums~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getAlbumsByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Albums~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Albums~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Albums~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Albums~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Albums~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Albums~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getartistsbyidsimilar"></a>

## GET /Artists/{Id}/Similar

- Operation ID: `getArtistsByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Artists~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getArtistsByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Artists~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getgamesbyidsimilar"></a>

## GET /Games/{Id}/Similar

- Operation ID: `getGamesByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Games~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getGamesByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Games~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Games~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Games~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Games~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Games~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Games~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteitems"></a>

## DELETE /Items

- Operation ID: `deleteItems`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/deleteItems.html).
- Consumes: not declared.
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
| `Ids` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteitemsbyid"></a>

## DELETE /Items/{Id}

- Operation ID: `deleteItemsById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/deleteItemsById.html).
- Consumes: not declared.
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidancestors"></a>

## GET /Items/{Id}/Ancestors

- Operation ID: `getItemsByIdAncestors`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Ancestors/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdAncestors.html).
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
| `UserId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[BaseItemDto](../models.md#model-baseitemdto)&gt; | Operation successful. Returning a BaseItemDto[] object. | `#/paths/~1Items~1{Id}~1Ancestors/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Ancestors/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Ancestors/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Ancestors/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Ancestors/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Ancestors/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidcriticreviews"></a>

## GET /Items/{Id}/CriticReviews

- Operation ID: `getItemsByIdCriticreviews`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1CriticReviews/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdCriticreviews.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Items~1{Id}~1CriticReviews/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1CriticReviews/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1CriticReviews/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1CriticReviews/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1CriticReviews/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1CriticReviews/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyiddelete"></a>

## POST /Items/{Id}/Delete

- Operation ID: `postItemsByIdDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postItemsByIdDelete.html).
- Consumes: not declared.
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyiddeleteinfo"></a>

## GET /Items/{Id}/DeleteInfo

- Operation ID: `getItemsByIdDeleteinfo`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1DeleteInfo/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdDeleteinfo.html).
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
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [Library.DeleteInfo](../models.md#model-library-deleteinfo) | Operation successful. Returning a DeleteInfo object. | `#/paths/~1Items~1{Id}~1DeleteInfo/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1DeleteInfo/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1DeleteInfo/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1DeleteInfo/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1DeleteInfo/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1DeleteInfo/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyiddownload"></a>

## GET /Items/{Id}/Download

- Operation ID: `getItemsByIdDownload`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Download/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdDownload.html).
- Consumes: not declared.
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Download/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Download/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Download/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Download/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Download/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Download/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidfile"></a>

## GET /Items/{Id}/File

- Operation ID: `getItemsByIdFile`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1File/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdFile.html).
- Consumes: not declared.
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1File/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1File/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1File/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1File/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1File/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1File/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidsimilar"></a>

## GET /Items/{Id}/Similar

- Operation ID: `getItemsByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Items~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidthememedia"></a>

## GET /Items/{Id}/ThemeMedia

- Operation ID: `getItemsByIdThememedia`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1ThemeMedia/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdThememedia.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `InheritFromParent` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
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
| `200` | [AllThemeMediaResult](../models.md#model-allthememediaresult) | Operation successful. Returning a AllThemeMediaResult object. | `#/paths/~1Items~1{Id}~1ThemeMedia/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeMedia/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeMedia/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeMedia/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeMedia/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeMedia/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidthemesongs"></a>

## GET /Items/{Id}/ThemeSongs

- Operation ID: `getItemsByIdThemesongs`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1ThemeSongs/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdThemesongs.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `InheritFromParent` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
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
| `200` | [ThemeMediaResult](../models.md#model-thememediaresult) | Operation successful. Returning a ThemeMediaResult object. | `#/paths/~1Items~1{Id}~1ThemeSongs/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeSongs/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeSongs/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeSongs/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeSongs/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeSongs/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidthemevideos"></a>

## GET /Items/{Id}/ThemeVideos

- Operation ID: `getItemsByIdThemevideos`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1ThemeVideos/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdThemevideos.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `InheritFromParent` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
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
| `200` | [ThemeMediaResult](../models.md#model-thememediaresult) | Operation successful. Returning a ThemeMediaResult object. | `#/paths/~1Items~1{Id}~1ThemeVideos/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeVideos/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeVideos/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeVideos/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeVideos/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThemeVideos/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemscounts"></a>

## GET /Items/Counts

- Operation ID: `getItemsCounts`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1Counts/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsCounts.html).
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
| `UserId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [ItemCounts](../models.md#model-itemcounts) | Operation successful. Returning a ItemCounts object. | `#/paths/~1Items~1Counts/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Counts/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Counts/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Counts/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Counts/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Counts/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsdelete"></a>

## POST /Items/Delete

- Operation ID: `postItemsDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postItemsDelete.html).
- Consumes: not declared.
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
| `Ids` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsintros"></a>

## GET /Items/Intros

- Operation ID: `getItemsIntros`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1Intros/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsIntros.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[Persistence.IntroDebugInfo](../models.md#model-persistence-introdebuginfo)&gt; | Operation successful. Returning a List<IntroDebugInfo> object. | `#/paths/~1Items~1Intros/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Intros/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Intros/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Intros/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Intros/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Intros/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlibrariesavailableoptions"></a>

## GET /Libraries/AvailableOptions

- Operation ID: `getLibrariesAvailableoptions`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Libraries~1AvailableOptions/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getLibrariesAvailableoptions.html).
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [LibraryOptionsResult](../models.md#model-libraryoptionsresult) | Operation successful. Returning a LibraryOptionsResult object. | `#/paths/~1Libraries~1AvailableOptions/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Libraries~1AvailableOptions/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Libraries~1AvailableOptions/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Libraries~1AvailableOptions/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Libraries~1AvailableOptions/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Libraries~1AvailableOptions/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibrarymediaupdated"></a>

## POST /Library/Media/Updated

- Operation ID: `postLibraryMediaUpdated`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1Media~1Updated/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postLibraryMediaUpdated.html).
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
| `body` | `body` | true | [Library.PostUpdatedMedia](../models.md#model-library-postupdatedmedia) | not declared | not declared |

### Request body and model references

- `body`: [Library.PostUpdatedMedia](../models.md#model-library-postupdatedmedia).
  Source pointer: `#/paths/~1Library~1Media~1Updated/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.PostUpdatedMedia](../models.md#model-library-postupdatedmedia).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1Media~1Updated/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Media~1Updated/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Media~1Updated/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Media~1Updated/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Media~1Updated/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Media~1Updated/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlibrarymediafolders"></a>

## GET /Library/MediaFolders

- Operation ID: `getLibraryMediafolders`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1MediaFolders/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getLibraryMediafolders.html).
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
| `IsHidden` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Library~1MediaFolders/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1MediaFolders/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1MediaFolders/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1MediaFolders/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1MediaFolders/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1MediaFolders/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibrarymoviesadded"></a>

## POST /Library/Movies/Added

- Operation ID: `postLibraryMoviesAdded`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1Movies~1Added/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postLibraryMoviesAdded.html).
- Consumes: not declared.
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1Movies~1Added/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Added/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Added/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Added/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Added/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Added/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibrarymoviesupdated"></a>

## POST /Library/Movies/Updated

- Operation ID: `postLibraryMoviesUpdated`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1Movies~1Updated/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postLibraryMoviesUpdated.html).
- Consumes: not declared.
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1Movies~1Updated/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Updated/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Updated/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Updated/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Updated/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Movies~1Updated/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlibraryphysicalpaths"></a>

## GET /Library/PhysicalPaths

- Operation ID: `getLibraryPhysicalpaths`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1PhysicalPaths/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getLibraryPhysicalpaths.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;string&gt; | Operation successful. Returning a List<String> object. | `#/paths/~1Library~1PhysicalPaths/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1PhysicalPaths/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1PhysicalPaths/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1PhysicalPaths/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1PhysicalPaths/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1PhysicalPaths/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryrefresh"></a>

## POST /Library/Refresh

- Operation ID: `postLibraryRefresh`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1Refresh/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postLibraryRefresh.html).
- Consumes: not declared.
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1Refresh/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Refresh/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Refresh/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Refresh/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Refresh/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Refresh/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlibraryselectablemediafolders"></a>

## GET /Library/SelectableMediaFolders

- Operation ID: `getLibrarySelectablemediafolders`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1SelectableMediaFolders/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getLibrarySelectablemediafolders.html).
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[Library.MediaFolder](../models.md#model-library-mediafolder)&gt; | Operation successful. Returning a MediaFolder[] object. | `#/paths/~1Library~1SelectableMediaFolders/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1SelectableMediaFolders/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1SelectableMediaFolders/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1SelectableMediaFolders/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1SelectableMediaFolders/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1SelectableMediaFolders/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryseriesadded"></a>

## POST /Library/Series/Added

- Operation ID: `postLibrarySeriesAdded`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1Series~1Added/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postLibrarySeriesAdded.html).
- Consumes: not declared.
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1Series~1Added/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Added/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Added/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Added/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Added/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Added/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryseriesupdated"></a>

## POST /Library/Series/Updated

- Operation ID: `postLibrarySeriesUpdated`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1Series~1Updated/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/postLibrarySeriesUpdated.html).
- Consumes: not declared.
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1Series~1Updated/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Updated/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Updated/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Updated/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Updated/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1Series~1Updated/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getmoviesbyidsimilar"></a>

## GET /Movies/{Id}/Similar

- Operation ID: `getMoviesByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Movies~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getMoviesByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Movies~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Movies~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Movies~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Movies~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Movies~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Movies~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getshowsbyidsimilar"></a>

## GET /Shows/{Id}/Similar

- Operation ID: `getShowsByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Shows~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getShowsByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Shows~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Shows~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Shows~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Shows~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Shows~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Shows~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-gettrailersbyidsimilar"></a>

## GET /Trailers/{Id}/Similar

- Operation ID: `getTrailersByIdSimilar`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Trailers~1{Id}~1Similar/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryService/getTrailersByIdSimilar.html).
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
| `Id` | `path` | true | string | not declared | not declared |
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1Trailers~1{Id}~1Similar/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Trailers~1{Id}~1Similar/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Trailers~1{Id}~1Similar/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Trailers~1{Id}~1Similar/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Trailers~1{Id}~1Similar/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Trailers~1{Id}~1Similar/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
