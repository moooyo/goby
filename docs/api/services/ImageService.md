# ImageService

Item/user image retrieval and image administration.

**49 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Artists/{Name}/Images/{Type}`](#operation-getartistsbynameimagesbytype)
- [`HEAD /Artists/{Name}/Images/{Type}`](#operation-headartistsbynameimagesbytype)
- [`GET /Artists/{Name}/Images/{Type}/{Index}`](#operation-getartistsbynameimagesbytypebyindex)
- [`HEAD /Artists/{Name}/Images/{Type}/{Index}`](#operation-headartistsbynameimagesbytypebyindex)
- [`GET /GameGenres/{Name}/Images/{Type}`](#operation-getgamegenresbynameimagesbytype)
- [`HEAD /GameGenres/{Name}/Images/{Type}`](#operation-headgamegenresbynameimagesbytype)
- [`GET /GameGenres/{Name}/Images/{Type}/{Index}`](#operation-getgamegenresbynameimagesbytypebyindex)
- [`HEAD /GameGenres/{Name}/Images/{Type}/{Index}`](#operation-headgamegenresbynameimagesbytypebyindex)
- [`GET /Genres/{Name}/Images/{Type}`](#operation-getgenresbynameimagesbytype)
- [`HEAD /Genres/{Name}/Images/{Type}`](#operation-headgenresbynameimagesbytype)
- [`GET /Genres/{Name}/Images/{Type}/{Index}`](#operation-getgenresbynameimagesbytypebyindex)
- [`HEAD /Genres/{Name}/Images/{Type}/{Index}`](#operation-headgenresbynameimagesbytypebyindex)
- [`GET /Items/{Id}/Images`](#operation-getitemsbyidimages)
- [`GET /Items/{Id}/Images/{Type}`](#operation-getitemsbyidimagesbytype)
- [`POST /Items/{Id}/Images/{Type}`](#operation-postitemsbyidimagesbytype)
- [`DELETE /Items/{Id}/Images/{Type}`](#operation-deleteitemsbyidimagesbytype)
- [`HEAD /Items/{Id}/Images/{Type}`](#operation-headitemsbyidimagesbytype)
- [`GET /Items/{Id}/Images/{Type}/{Index}`](#operation-getitemsbyidimagesbytypebyindex)
- [`POST /Items/{Id}/Images/{Type}/{Index}`](#operation-postitemsbyidimagesbytypebyindex)
- [`DELETE /Items/{Id}/Images/{Type}/{Index}`](#operation-deleteitemsbyidimagesbytypebyindex)
- [`HEAD /Items/{Id}/Images/{Type}/{Index}`](#operation-headitemsbyidimagesbytypebyindex)
- [`GET /Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}`](#operation-getitemsbyidimagesbytypebyindexbytagbyformatbymaxwidthbymaxheightbypercentplayedbyunplayedcount)
- [`HEAD /Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}`](#operation-headitemsbyidimagesbytypebyindexbytagbyformatbymaxwidthbymaxheightbypercentplayedbyunplayedcount)
- [`POST /Items/{Id}/Images/{Type}/{Index}/Delete`](#operation-postitemsbyidimagesbytypebyindexdelete)
- [`POST /Items/{Id}/Images/{Type}/{Index}/Index`](#operation-postitemsbyidimagesbytypebyindexindex)
- [`POST /Items/{Id}/Images/{Type}/{Index}/Url`](#operation-postitemsbyidimagesbytypebyindexurl)
- [`POST /Items/{Id}/Images/{Type}/Delete`](#operation-postitemsbyidimagesbytypedelete)
- [`GET /MusicGenres/{Name}/Images/{Type}`](#operation-getmusicgenresbynameimagesbytype)
- [`HEAD /MusicGenres/{Name}/Images/{Type}`](#operation-headmusicgenresbynameimagesbytype)
- [`GET /MusicGenres/{Name}/Images/{Type}/{Index}`](#operation-getmusicgenresbynameimagesbytypebyindex)
- [`HEAD /MusicGenres/{Name}/Images/{Type}/{Index}`](#operation-headmusicgenresbynameimagesbytypebyindex)
- [`GET /Persons/{Name}/Images/{Type}`](#operation-getpersonsbynameimagesbytype)
- [`HEAD /Persons/{Name}/Images/{Type}`](#operation-headpersonsbynameimagesbytype)
- [`GET /Persons/{Name}/Images/{Type}/{Index}`](#operation-getpersonsbynameimagesbytypebyindex)
- [`HEAD /Persons/{Name}/Images/{Type}/{Index}`](#operation-headpersonsbynameimagesbytypebyindex)
- [`GET /Studios/{Name}/Images/{Type}`](#operation-getstudiosbynameimagesbytype)
- [`HEAD /Studios/{Name}/Images/{Type}`](#operation-headstudiosbynameimagesbytype)
- [`GET /Studios/{Name}/Images/{Type}/{Index}`](#operation-getstudiosbynameimagesbytypebyindex)
- [`HEAD /Studios/{Name}/Images/{Type}/{Index}`](#operation-headstudiosbynameimagesbytypebyindex)
- [`GET /Users/{Id}/Images/{Type}`](#operation-getusersbyidimagesbytype)
- [`POST /Users/{Id}/Images/{Type}`](#operation-postusersbyidimagesbytype)
- [`DELETE /Users/{Id}/Images/{Type}`](#operation-deleteusersbyidimagesbytype)
- [`HEAD /Users/{Id}/Images/{Type}`](#operation-headusersbyidimagesbytype)
- [`GET /Users/{Id}/Images/{Type}/{Index}`](#operation-getusersbyidimagesbytypebyindex)
- [`POST /Users/{Id}/Images/{Type}/{Index}`](#operation-postusersbyidimagesbytypebyindex)
- [`DELETE /Users/{Id}/Images/{Type}/{Index}`](#operation-deleteusersbyidimagesbytypebyindex)
- [`HEAD /Users/{Id}/Images/{Type}/{Index}`](#operation-headusersbyidimagesbytypebyindex)
- [`POST /Users/{Id}/Images/{Type}/{Index}/Delete`](#operation-postusersbyidimagesbytypebyindexdelete)
- [`POST /Users/{Id}/Images/{Type}/Delete`](#operation-postusersbyidimagesbytypedelete)

<a id="operation-getartistsbynameimagesbytype"></a>

## GET /Artists/{Name}/Images/{Type}

- Operation ID: `getArtistsByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Artists~1{Name}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getArtistsByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Artists~1{Name}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headartistsbynameimagesbytype"></a>

## HEAD /Artists/{Name}/Images/{Type}

- Operation ID: `headArtistsByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Artists~1{Name}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headArtistsByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Artists~1{Name}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getartistsbynameimagesbytypebyindex"></a>

## GET /Artists/{Name}/Images/{Type}/{Index}

- Operation ID: `getArtistsByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getArtistsByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headartistsbynameimagesbytypebyindex"></a>

## HEAD /Artists/{Name}/Images/{Type}/{Index}

- Operation ID: `headArtistsByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headArtistsByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Artists~1{Name}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getgamegenresbynameimagesbytype"></a>

## GET /GameGenres/{Name}/Images/{Type}

- Operation ID: `getGamegenresByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getGamegenresByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headgamegenresbynameimagesbytype"></a>

## HEAD /GameGenres/{Name}/Images/{Type}

- Operation ID: `headGamegenresByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headGamegenresByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getgamegenresbynameimagesbytypebyindex"></a>

## GET /GameGenres/{Name}/Images/{Type}/{Index}

- Operation ID: `getGamegenresByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getGamegenresByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headgamegenresbynameimagesbytypebyindex"></a>

## HEAD /GameGenres/{Name}/Images/{Type}/{Index}

- Operation ID: `headGamegenresByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headGamegenresByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1GameGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getgenresbynameimagesbytype"></a>

## GET /Genres/{Name}/Images/{Type}

- Operation ID: `getGenresByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Genres~1{Name}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getGenresByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Genres~1{Name}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headgenresbynameimagesbytype"></a>

## HEAD /Genres/{Name}/Images/{Type}

- Operation ID: `headGenresByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Genres~1{Name}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headGenresByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Genres~1{Name}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getgenresbynameimagesbytypebyindex"></a>

## GET /Genres/{Name}/Images/{Type}/{Index}

- Operation ID: `getGenresByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getGenresByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headgenresbynameimagesbytypebyindex"></a>

## HEAD /Genres/{Name}/Images/{Type}/{Index}

- Operation ID: `headGenresByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headGenresByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Genres~1{Name}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidimages"></a>

## GET /Items/{Id}/Images

- Operation ID: `getItemsByIdImages`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getItemsByIdImages.html).
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
| `200` | array&lt;[ImageInfo](../models.md#model-imageinfo)&gt; | Operation successful. Returning a List<ImageInfo> object. | `#/paths/~1Items~1{Id}~1Images/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidimagesbytype"></a>

## GET /Items/{Id}/Images/{Type}

- Operation ID: `getItemsByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getItemsByIdImagesByType.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidimagesbytype"></a>

## POST /Items/{Id}/Images/{Type}

- Operation ID: `postItemsByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postItemsByIdImagesByType.html).
- Consumes: `application/octet-stream`.
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
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}/post/parameters/3`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteitemsbyidimagesbytype"></a>

## DELETE /Items/{Id}/Images/{Type}

- Operation ID: `deleteItemsByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/deleteItemsByIdImagesByType.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headitemsbyidimagesbytype"></a>

## HEAD /Items/{Id}/Images/{Type}

- Operation ID: `headItemsByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headItemsByIdImagesByType.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidimagesbytypebyindex"></a>

## GET /Items/{Id}/Images/{Type}/{Index}

- Operation ID: `getItemsByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getItemsByIdImagesByTypeByIndex.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidimagesbytypebyindex"></a>

## POST /Items/{Id}/Images/{Type}/{Index}

- Operation ID: `postItemsByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postItemsByIdImagesByTypeByIndex.html).
- Consumes: `application/octet-stream`.
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
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/parameters/3`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteitemsbyidimagesbytypebyindex"></a>

## DELETE /Items/{Id}/Images/{Type}/{Index}

- Operation ID: `deleteItemsByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/deleteItemsByIdImagesByTypeByIndex.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headitemsbyidimagesbytypebyindex"></a>

## HEAD /Items/{Id}/Images/{Type}/{Index}

- Operation ID: `headItemsByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headItemsByIdImagesByTypeByIndex.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidimagesbytypebyindexbytagbyformatbymaxwidthbymaxheightbypercentplayedbyunplayedcount"></a>

## GET /Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}

- Operation ID: `getItemsByIdImagesByTypeByIndexByTagByFormatByMaxwidthByMaxheightByPercentplayedByUnplayedcount`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getItemsByIdImagesByTypeByIndexByTagByFormatByMaxwidthByMaxheightByPercentplayedByUnplayedcount.html).
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
| `PercentPlayed` | `path` | true | integer (int32) | not declared | not declared |
| `UnPlayedCount` | `path` | true | integer (int32) | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `path` | true | integer (int32) | not declared | not declared |
| `MaxHeight` | `path` | true | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `path` | true | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headitemsbyidimagesbytypebyindexbytagbyformatbymaxwidthbymaxheightbypercentplayedbyunplayedcount"></a>

## HEAD /Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}

- Operation ID: `headItemsByIdImagesByTypeByIndexByTagByFormatByMaxwidthByMaxheightByPercentplayedByUnplayedcount`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headItemsByIdImagesByTypeByIndexByTagByFormatByMaxwidthByMaxheightByPercentplayedByUnplayedcount.html).
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
| `PercentPlayed` | `path` | true | integer (int32) | not declared | not declared |
| `UnPlayedCount` | `path` | true | integer (int32) | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `path` | true | integer (int32) | not declared | not declared |
| `MaxHeight` | `path` | true | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `path` | true | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1{Tag}~1{Format}~1{MaxWidth}~1{MaxHeight}~1{PercentPlayed}~1{UnPlayedCount}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidimagesbytypebyindexdelete"></a>

## POST /Items/{Id}/Images/{Type}/{Index}/Delete

- Operation ID: `postItemsByIdImagesByTypeByIndexDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postItemsByIdImagesByTypeByIndexDelete.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidimagesbytypebyindexindex"></a>

## POST /Items/{Id}/Images/{Type}/{Index}/Index

- Operation ID: `postItemsByIdImagesByTypeByIndexIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postItemsByIdImagesByTypeByIndexIndex.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `NewIndex` | `query` | true | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Index/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidimagesbytypebyindexurl"></a>

## POST /Items/{Id}/Images/{Type}/{Index}/Url

- Operation ID: `postItemsByIdImagesByTypeByIndexUrl`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postItemsByIdImagesByTypeByIndexUrl.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Url` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1{Index}~1Url/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidimagesbytypedelete"></a>

## POST /Items/{Id}/Images/{Type}/Delete

- Operation ID: `postItemsByIdImagesByTypeDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postItemsByIdImagesByTypeDelete.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Images~1{Type}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getmusicgenresbynameimagesbytype"></a>

## GET /MusicGenres/{Name}/Images/{Type}

- Operation ID: `getMusicgenresByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getMusicgenresByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headmusicgenresbynameimagesbytype"></a>

## HEAD /MusicGenres/{Name}/Images/{Type}

- Operation ID: `headMusicgenresByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headMusicgenresByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getmusicgenresbynameimagesbytypebyindex"></a>

## GET /MusicGenres/{Name}/Images/{Type}/{Index}

- Operation ID: `getMusicgenresByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getMusicgenresByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headmusicgenresbynameimagesbytypebyindex"></a>

## HEAD /MusicGenres/{Name}/Images/{Type}/{Index}

- Operation ID: `headMusicgenresByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headMusicgenresByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1MusicGenres~1{Name}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getpersonsbynameimagesbytype"></a>

## GET /Persons/{Name}/Images/{Type}

- Operation ID: `getPersonsByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Persons~1{Name}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getPersonsByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Persons~1{Name}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headpersonsbynameimagesbytype"></a>

## HEAD /Persons/{Name}/Images/{Type}

- Operation ID: `headPersonsByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Persons~1{Name}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headPersonsByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Persons~1{Name}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getpersonsbynameimagesbytypebyindex"></a>

## GET /Persons/{Name}/Images/{Type}/{Index}

- Operation ID: `getPersonsByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getPersonsByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headpersonsbynameimagesbytypebyindex"></a>

## HEAD /Persons/{Name}/Images/{Type}/{Index}

- Operation ID: `headPersonsByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headPersonsByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Persons~1{Name}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getstudiosbynameimagesbytype"></a>

## GET /Studios/{Name}/Images/{Type}

- Operation ID: `getStudiosByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Studios~1{Name}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getStudiosByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Studios~1{Name}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headstudiosbynameimagesbytype"></a>

## HEAD /Studios/{Name}/Images/{Type}

- Operation ID: `headStudiosByNameImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Studios~1{Name}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headStudiosByNameImagesByType.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Studios~1{Name}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getstudiosbynameimagesbytypebyindex"></a>

## GET /Studios/{Name}/Images/{Type}/{Index}

- Operation ID: `getStudiosByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getStudiosByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headstudiosbynameimagesbytypebyindex"></a>

## HEAD /Studios/{Name}/Images/{Type}/{Index}

- Operation ID: `headStudiosByNameImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headStudiosByNameImagesByTypeByIndex.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Studios~1{Name}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getusersbyidimagesbytype"></a>

## GET /Users/{Id}/Images/{Type}

- Operation ID: `getUsersByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getUsersByIdImagesByType.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Users~1{Id}~1Images~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidimagesbytype"></a>

## POST /Users/{Id}/Images/{Type}

- Operation ID: `postUsersByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postUsersByIdImagesByType.html).
- Consumes: `application/octet-stream`.
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
| `Type` | `path` | true | unspecified | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}/post/parameters/2`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Images~1{Type}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteusersbyidimagesbytype"></a>

## DELETE /Users/{Id}/Images/{Type}

- Operation ID: `deleteUsersByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/deleteUsersByIdImagesByType.html).
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
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Images~1{Type}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headusersbyidimagesbytype"></a>

## HEAD /Users/{Id}/Images/{Type}

- Operation ID: `headUsersByIdImagesByType`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headUsersByIdImagesByType.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Users~1{Id}~1Images~1{Type}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getusersbyidimagesbytypebyindex"></a>

## GET /Users/{Id}/Images/{Type}/{Index}

- Operation ID: `getUsersByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/getUsersByIdImagesByTypeByIndex.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidimagesbytypebyindex"></a>

## POST /Users/{Id}/Images/{Type}/{Index}

- Operation ID: `postUsersByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postUsersByIdImagesByTypeByIndex.html).
- Consumes: `application/octet-stream`.
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
| `Type` | `path` | true | unspecified | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/parameters/2`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteusersbyidimagesbytypebyindex"></a>

## DELETE /Users/{Id}/Images/{Type}/{Index}

- Operation ID: `deleteUsersByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/deleteUsersByIdImagesByTypeByIndex.html).
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
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headusersbyidimagesbytypebyindex"></a>

## HEAD /Users/{Id}/Images/{Type}/{Index}

- Operation ID: `headUsersByIdImagesByTypeByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/headUsersByIdImagesByTypeByIndex.html).
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
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Quality` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Tag` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `CropWhitespace` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableImageEnhancers` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Format` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `BackgroundColor` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ForegroundLayer` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AutoOrient` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `KeepAnimation` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidimagesbytypebyindexdelete"></a>

## POST /Users/{Id}/Images/{Type}/{Index}/Delete

- Operation ID: `postUsersByIdImagesByTypeByIndexDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postUsersByIdImagesByTypeByIndexDelete.html).
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
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1{Index}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidimagesbytypedelete"></a>

## POST /Users/{Id}/Images/{Type}/Delete

- Operation ID: `postUsersByIdImagesByTypeDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ImageService/postUsersByIdImagesByTypeDelete.html).
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
| `Index` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Type` | `path` | true | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Images~1{Type}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
