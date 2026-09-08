# ItemLookupService

External metadata search and identification.

**14 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Items/{Id}/ExternalIdInfos`](#operation-getitemsbyidexternalidinfos)
- [`POST /Items/Metadata/Reset`](#operation-postitemsmetadatareset)
- [`POST /Items/RemoteSearch/Apply/{Id}`](#operation-postitemsremotesearchapplybyid)
- [`POST /Items/RemoteSearch/Book`](#operation-postitemsremotesearchbook)
- [`POST /Items/RemoteSearch/BoxSet`](#operation-postitemsremotesearchboxset)
- [`POST /Items/RemoteSearch/Game`](#operation-postitemsremotesearchgame)
- [`GET /Items/RemoteSearch/Image`](#operation-getitemsremotesearchimage)
- [`POST /Items/RemoteSearch/Movie`](#operation-postitemsremotesearchmovie)
- [`POST /Items/RemoteSearch/MusicAlbum`](#operation-postitemsremotesearchmusicalbum)
- [`POST /Items/RemoteSearch/MusicArtist`](#operation-postitemsremotesearchmusicartist)
- [`POST /Items/RemoteSearch/MusicVideo`](#operation-postitemsremotesearchmusicvideo)
- [`POST /Items/RemoteSearch/Person`](#operation-postitemsremotesearchperson)
- [`POST /Items/RemoteSearch/Series`](#operation-postitemsremotesearchseries)
- [`POST /Items/RemoteSearch/Trailer`](#operation-postitemsremotesearchtrailer)

<a id="operation-getitemsbyidexternalidinfos"></a>

## GET /Items/{Id}/ExternalIdInfos

- Operation ID: `getItemsByIdExternalidinfos`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1ExternalIdInfos/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/getItemsByIdExternalidinfos.html).
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

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[ExternalIdInfo](../models.md#model-externalidinfo)&gt; | Operation successful. Returning a List<ExternalIdInfo> object. | `#/paths/~1Items~1{Id}~1ExternalIdInfos/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ExternalIdInfos/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ExternalIdInfos/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ExternalIdInfos/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ExternalIdInfos/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ExternalIdInfos/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsmetadatareset"></a>

## POST /Items/Metadata/Reset

- Operation ID: `postItemsMetadataReset`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1Metadata~1Reset/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsMetadataReset.html).
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

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ItemIds` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1Metadata~1Reset/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Metadata~1Reset/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Metadata~1Reset/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Metadata~1Reset/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Metadata~1Reset/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1Metadata~1Reset/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchapplybyid"></a>

## POST /Items/RemoteSearch/Apply/{Id}

- Operation ID: `postItemsRemotesearchApplyById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchApplyById.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `path` | true | string | not declared | not declared |
| `ReplaceAllImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `body` | `body` | true | [RemoteSearchResult](../models.md#model-remotesearchresult) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchResult](../models.md#model-remotesearchresult).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/parameters/2`.

Direct schema references in all request parameters:

- [RemoteSearchResult](../models.md#model-remotesearchresult).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Apply~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchbook"></a>

## POST /Items/RemoteSearch/Book

- Operation ID: `postItemsRemotesearchBook`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Book/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchBook.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_BookInfo](../models.md#model-remotesearchquery_bookinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_BookInfo](../models.md#model-remotesearchquery_bookinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Book/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_BookInfo](../models.md#model-remotesearchquery_bookinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1Book/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Book/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Book/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Book/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Book/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Book/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchboxset"></a>

## POST /Items/RemoteSearch/BoxSet

- Operation ID: `postItemsRemotesearchBoxset`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1BoxSet/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchBoxset.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_ItemLookupInfo](../models.md#model-remotesearchquery_itemlookupinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_ItemLookupInfo](../models.md#model-remotesearchquery_itemlookupinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1BoxSet/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_ItemLookupInfo](../models.md#model-remotesearchquery_itemlookupinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1BoxSet/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1BoxSet/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1BoxSet/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1BoxSet/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1BoxSet/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1BoxSet/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchgame"></a>

## POST /Items/RemoteSearch/Game

- Operation ID: `postItemsRemotesearchGame`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Game/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchGame.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_GameInfo](../models.md#model-remotesearchquery_gameinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_GameInfo](../models.md#model-remotesearchquery_gameinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Game/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_GameInfo](../models.md#model-remotesearchquery_gameinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1Game/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Game/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Game/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Game/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Game/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Game/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsremotesearchimage"></a>

## GET /Items/RemoteSearch/Image

- Operation ID: `getItemsRemotesearchImage`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Image/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/getItemsRemotesearchImage.html).
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

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ImageUrl` | `query` | true | string | not declared | not declared |
| `ProviderName` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1RemoteSearch~1Image/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Image/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Image/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Image/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Image/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Image/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchmovie"></a>

## POST /Items/RemoteSearch/Movie

- Operation ID: `postItemsRemotesearchMovie`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Movie/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchMovie.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_MovieInfo](../models.md#model-remotesearchquery_movieinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_MovieInfo](../models.md#model-remotesearchquery_movieinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Movie/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_MovieInfo](../models.md#model-remotesearchquery_movieinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1Movie/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Movie/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Movie/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Movie/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Movie/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Movie/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchmusicalbum"></a>

## POST /Items/RemoteSearch/MusicAlbum

- Operation ID: `postItemsRemotesearchMusicalbum`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchMusicalbum.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_AlbumInfo](../models.md#model-remotesearchquery_albuminfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_AlbumInfo](../models.md#model-remotesearchquery_albuminfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_AlbumInfo](../models.md#model-remotesearchquery_albuminfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicAlbum/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchmusicartist"></a>

## POST /Items/RemoteSearch/MusicArtist

- Operation ID: `postItemsRemotesearchMusicartist`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1MusicArtist/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchMusicartist.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_ArtistInfo](../models.md#model-remotesearchquery_artistinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_ArtistInfo](../models.md#model-remotesearchquery_artistinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_ArtistInfo](../models.md#model-remotesearchquery_artistinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicArtist/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchmusicvideo"></a>

## POST /Items/RemoteSearch/MusicVideo

- Operation ID: `postItemsRemotesearchMusicvideo`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1MusicVideo/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchMusicvideo.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_MusicVideoInfo](../models.md#model-remotesearchquery_musicvideoinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_MusicVideoInfo](../models.md#model-remotesearchquery_musicvideoinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_MusicVideoInfo](../models.md#model-remotesearchquery_musicvideoinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1MusicVideo/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchperson"></a>

## POST /Items/RemoteSearch/Person

- Operation ID: `postItemsRemotesearchPerson`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Person/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchPerson.html).
- Consumes: `application/json`, `application/xml`.
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

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `body` | `body` | true | [RemoteSearchQuery_PersonLookupInfo](../models.md#model-remotesearchquery_personlookupinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_PersonLookupInfo](../models.md#model-remotesearchquery_personlookupinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Person/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_PersonLookupInfo](../models.md#model-remotesearchquery_personlookupinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1Person/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Person/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Person/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Person/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Person/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Person/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchseries"></a>

## POST /Items/RemoteSearch/Series

- Operation ID: `postItemsRemotesearchSeries`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Series/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchSeries.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_SeriesInfo](../models.md#model-remotesearchquery_seriesinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_SeriesInfo](../models.md#model-remotesearchquery_seriesinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Series/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_SeriesInfo](../models.md#model-remotesearchquery_seriesinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1Series/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Series/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Series/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Series/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Series/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Series/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsremotesearchtrailer"></a>

## POST /Items/RemoteSearch/Trailer

- Operation ID: `postItemsRemotesearchTrailer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1RemoteSearch~1Trailer/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemLookupService/postItemsRemotesearchTrailer.html).
- Consumes: `application/json`, `application/xml`.
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
| `body` | `body` | true | [RemoteSearchQuery_TrailerInfo](../models.md#model-remotesearchquery_trailerinfo) | not declared | not declared |

### Request body and model references

- `body`: [RemoteSearchQuery_TrailerInfo](../models.md#model-remotesearchquery_trailerinfo).
  Source pointer: `#/paths/~1Items~1RemoteSearch~1Trailer/post/parameters/0`.

Direct schema references in all request parameters:

- [RemoteSearchQuery_TrailerInfo](../models.md#model-remotesearchquery_trailerinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSearchResult](../models.md#model-remotesearchresult)&gt; | Operation successful. Returning a List<RemoteSearchResult> object. | `#/paths/~1Items~1RemoteSearch~1Trailer/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Trailer/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Trailer/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Trailer/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Trailer/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1RemoteSearch~1Trailer/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
