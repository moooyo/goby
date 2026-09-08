# EnvironmentService

Administrative filesystem discovery.

**8 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Environment/DefaultDirectoryBrowser`](#operation-getenvironmentdefaultdirectorybrowser)
- [`GET /Environment/DirectoryContents`](#operation-getenvironmentdirectorycontents)
- [`POST /Environment/DirectoryContents`](#operation-postenvironmentdirectorycontents)
- [`GET /Environment/Drives`](#operation-getenvironmentdrives)
- [`GET /Environment/NetworkDevices`](#operation-getenvironmentnetworkdevices)
- [`GET /Environment/NetworkShares`](#operation-getenvironmentnetworkshares)
- [`GET /Environment/ParentPath`](#operation-getenvironmentparentpath)
- [`POST /Environment/ValidatePath`](#operation-postenvironmentvalidatepath)

<a id="operation-getenvironmentdefaultdirectorybrowser"></a>

## GET /Environment/DefaultDirectoryBrowser

- Operation ID: `getEnvironmentDefaultdirectorybrowser`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1DefaultDirectoryBrowser/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/getEnvironmentDefaultdirectorybrowser.html).
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
| `200` | [DefaultDirectoryBrowserInfo](../models.md#model-defaultdirectorybrowserinfo) | Operation successful. Returning a DefaultDirectoryBrowserInfo object. | `#/paths/~1Environment~1DefaultDirectoryBrowser/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DefaultDirectoryBrowser/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DefaultDirectoryBrowser/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DefaultDirectoryBrowser/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DefaultDirectoryBrowser/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DefaultDirectoryBrowser/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getenvironmentdirectorycontents"></a>

## GET /Environment/DirectoryContents

- Operation ID: `getEnvironmentDirectorycontents`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1DirectoryContents/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/getEnvironmentDirectorycontents.html).
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
| `Path` | `query` | true | string | not declared | not declared |
| `IncludeFiles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IncludeDirectories` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[IO.FileSystemEntryInfo](../models.md#model-io-filesystementryinfo)&gt; | Operation successful. Returning a List<FileSystemEntryInfo> object. | `#/paths/~1Environment~1DirectoryContents/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postenvironmentdirectorycontents"></a>

## POST /Environment/DirectoryContents

- Operation ID: `postEnvironmentDirectorycontents`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1DirectoryContents/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/postEnvironmentDirectorycontents.html).
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
| `Path` | `query` | true | string | not declared | not declared |
| `IncludeFiles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IncludeDirectories` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `body` | `body` | true | [GetDirectoryContents](../models.md#model-getdirectorycontents) | not declared | not declared |

### Request body and model references

- `body`: [GetDirectoryContents](../models.md#model-getdirectorycontents).
  Source pointer: `#/paths/~1Environment~1DirectoryContents/post/parameters/3`.

Direct schema references in all request parameters:

- [GetDirectoryContents](../models.md#model-getdirectorycontents).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[IO.FileSystemEntryInfo](../models.md#model-io-filesystementryinfo)&gt; | Operation successful. Returning a List<FileSystemEntryInfo> object. | `#/paths/~1Environment~1DirectoryContents/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1DirectoryContents/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getenvironmentdrives"></a>

## GET /Environment/Drives

- Operation ID: `getEnvironmentDrives`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1Drives/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/getEnvironmentDrives.html).
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
| `200` | array&lt;[IO.FileSystemEntryInfo](../models.md#model-io-filesystementryinfo)&gt; | Operation successful. Returning a List<FileSystemEntryInfo> object. | `#/paths/~1Environment~1Drives/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1Drives/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1Drives/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1Drives/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1Drives/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1Drives/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getenvironmentnetworkdevices"></a>

## GET /Environment/NetworkDevices

- Operation ID: `getEnvironmentNetworkdevices`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1NetworkDevices/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/getEnvironmentNetworkdevices.html).
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
| `200` | array&lt;[IO.FileSystemEntryInfo](../models.md#model-io-filesystementryinfo)&gt; | Operation successful. Returning a List<FileSystemEntryInfo> object. | `#/paths/~1Environment~1NetworkDevices/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkDevices/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkDevices/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkDevices/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkDevices/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkDevices/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getenvironmentnetworkshares"></a>

## GET /Environment/NetworkShares

- Operation ID: `getEnvironmentNetworkshares`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1NetworkShares/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/getEnvironmentNetworkshares.html).
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
| `Path` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[IO.FileSystemEntryInfo](../models.md#model-io-filesystementryinfo)&gt; | Operation successful. Returning a List<FileSystemEntryInfo> object. | `#/paths/~1Environment~1NetworkShares/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkShares/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkShares/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkShares/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkShares/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1NetworkShares/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getenvironmentparentpath"></a>

## GET /Environment/ParentPath

- Operation ID: `getEnvironmentParentpath`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1ParentPath/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/getEnvironmentParentpath.html).
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
| `Path` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | string | Operation successful. Returning a String object. | `#/paths/~1Environment~1ParentPath/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ParentPath/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ParentPath/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ParentPath/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ParentPath/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ParentPath/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postenvironmentvalidatepath"></a>

## POST /Environment/ValidatePath

- Operation ID: `postEnvironmentValidatepath`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Environment~1ValidatePath/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/EnvironmentService/postEnvironmentValidatepath.html).
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
| `Path` | `query` | true | string | not declared | not declared |
| `body` | `body` | true | [ValidatePath](../models.md#model-validatepath) | not declared | not declared |

### Request body and model references

- `body`: [ValidatePath](../models.md#model-validatepath).
  Source pointer: `#/paths/~1Environment~1ValidatePath/post/parameters/1`.

Direct schema references in all request parameters:

- [ValidatePath](../models.md#model-validatepath).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Environment~1ValidatePath/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ValidatePath/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ValidatePath/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ValidatePath/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ValidatePath/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Environment~1ValidatePath/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
