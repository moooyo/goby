# RemoteImageService

External image search and image selection.

**4 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Images/Remote`](#operation-getimagesremote)
- [`GET /Items/{Id}/RemoteImages`](#operation-getitemsbyidremoteimages)
- [`POST /Items/{Id}/RemoteImages/Download`](#operation-postitemsbyidremoteimagesdownload)
- [`GET /Items/{Id}/RemoteImages/Providers`](#operation-getitemsbyidremoteimagesproviders)

<a id="operation-getimagesremote"></a>

## GET /Images/Remote

- Operation ID: `getImagesRemote`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Images~1Remote/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/RemoteImageService/getImagesRemote.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Images~1Remote/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Images~1Remote/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Images~1Remote/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Images~1Remote/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Images~1Remote/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Images~1Remote/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidremoteimages"></a>

## GET /Items/{Id}/RemoteImages

- Operation ID: `getItemsByIdRemoteimages`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1RemoteImages/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/RemoteImageService/getItemsByIdRemoteimages.html).
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
| `Type` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ProviderName` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeAllLanguages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableSeriesImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [RemoteImageResult](../models.md#model-remoteimageresult) | Operation successful. Returning a RemoteImageResult object. | `#/paths/~1Items~1{Id}~1RemoteImages/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidremoteimagesdownload"></a>

## POST /Items/{Id}/RemoteImages/Download

- Operation ID: `postItemsByIdRemoteimagesDownload`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/RemoteImageService/postItemsByIdRemoteimagesDownload.html).
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
| `Type` | `query` | true | unspecified | not declared | not declared |
| `ProviderName` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageUrl` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `body` | `body` | true | [Images.BaseDownloadRemoteImage](../models.md#model-images-basedownloadremoteimage) | not declared | not declared |

### Request body and model references

- `body`: [Images.BaseDownloadRemoteImage](../models.md#model-images-basedownloadremoteimage).
  Source pointer: `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/parameters/4`.

Direct schema references in all request parameters:

- [Images.BaseDownloadRemoteImage](../models.md#model-images-basedownloadremoteimage).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Download/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidremoteimagesproviders"></a>

## GET /Items/{Id}/RemoteImages/Providers

- Operation ID: `getItemsByIdRemoteimagesProviders`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/RemoteImageService/getItemsByIdRemoteimagesProviders.html).
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
| `200` | array&lt;[ImageProviderInfo](../models.md#model-imageproviderinfo)&gt; | Operation successful. Returning a List<ImageProviderInfo> object. | `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteImages~1Providers/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
