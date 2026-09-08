# BifService

Video preview image index delivery.

**2 HTTP operations**. Service candidate: `EXPANSION`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Items/{Id}/ThumbnailSet`](#operation-getitemsbyidthumbnailset)
- [`GET /Videos/{Id}/index.bif`](#operation-getvideosbyidindexbif)

<a id="operation-getitemsbyidthumbnailset"></a>

## GET /Items/{Id}/ThumbnailSet

- Operation ID: `getItemsByIdThumbnailset`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1ThumbnailSet/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/BifService/getItemsByIdThumbnailset.html).
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
| `Width` | `query` | true | integer (int32) | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [RokuMetadata.Api.ThumbnailSetInfo](../models.md#model-rokumetadata-api-thumbnailsetinfo) | Operation successful. Returning a ThumbnailSetInfo object. | `#/paths/~1Items~1{Id}~1ThumbnailSet/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThumbnailSet/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThumbnailSet/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThumbnailSet/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThumbnailSet/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1ThumbnailSet/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidindexbif"></a>

## GET /Videos/{Id}/index.bif

- Operation ID: `getVideosByIdIndexBif`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1index.bif/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/BifService/getVideosByIdIndexBif.html).
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
| `Width` | `query` | true | integer (int32) | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1index.bif/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1index.bif/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1index.bif/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1index.bif/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1index.bif/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1index.bif/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
