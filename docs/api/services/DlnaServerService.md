# DlnaServerService

DLNA descriptions, icons, service descriptors, and control routes.

**16 HTTP operations**. Service candidate: `DEFERRED`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Dlna/{UuId}/connectionmanager/connectionmanager`](#operation-getdlnabyuuidconnectionmanagerconnectionmanager)
- [`HEAD /Dlna/{UuId}/connectionmanager/connectionmanager`](#operation-headdlnabyuuidconnectionmanagerconnectionmanager)
- [`GET /Dlna/{UuId}/connectionmanager/connectionmanager.xml`](#operation-getdlnabyuuidconnectionmanagerconnectionmanagerxml)
- [`HEAD /Dlna/{UuId}/connectionmanager/connectionmanager.xml`](#operation-headdlnabyuuidconnectionmanagerconnectionmanagerxml)
- [`POST /Dlna/{UuId}/connectionmanager/control`](#operation-postdlnabyuuidconnectionmanagercontrol)
- [`GET /Dlna/{UuId}/contentdirectory/contentdirectory`](#operation-getdlnabyuuidcontentdirectorycontentdirectory)
- [`HEAD /Dlna/{UuId}/contentdirectory/contentdirectory`](#operation-headdlnabyuuidcontentdirectorycontentdirectory)
- [`GET /Dlna/{UuId}/contentdirectory/contentdirectory.xml`](#operation-getdlnabyuuidcontentdirectorycontentdirectoryxml)
- [`HEAD /Dlna/{UuId}/contentdirectory/contentdirectory.xml`](#operation-headdlnabyuuidcontentdirectorycontentdirectoryxml)
- [`POST /Dlna/{UuId}/contentdirectory/control`](#operation-postdlnabyuuidcontentdirectorycontrol)
- [`GET /Dlna/{UuId}/description`](#operation-getdlnabyuuiddescription)
- [`HEAD /Dlna/{UuId}/description`](#operation-headdlnabyuuiddescription)
- [`GET /Dlna/{UuId}/description.xml`](#operation-getdlnabyuuiddescriptionxml)
- [`HEAD /Dlna/{UuId}/description.xml`](#operation-headdlnabyuuiddescriptionxml)
- [`GET /Dlna/{UuId}/icons/{Filename}`](#operation-getdlnabyuuidiconsbyfilename)
- [`GET /Dlna/icons/{Filename}`](#operation-getdlnaiconsbyfilename)

<a id="operation-getdlnabyuuidconnectionmanagerconnectionmanager"></a>

## GET /Dlna/{UuId}/connectionmanager/connectionmanager

- Operation ID: `getDlnaByUuidConnectionmanagerConnectionmanager`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidConnectionmanagerConnectionmanager.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headdlnabyuuidconnectionmanagerconnectionmanager"></a>

## HEAD /Dlna/{UuId}/connectionmanager/connectionmanager

- Operation ID: `headDlnaByUuidConnectionmanagerConnectionmanager`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/headDlnaByUuidConnectionmanagerConnectionmanager.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/head/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnabyuuidconnectionmanagerconnectionmanagerxml"></a>

## GET /Dlna/{UuId}/connectionmanager/connectionmanager.xml

- Operation ID: `getDlnaByUuidConnectionmanagerConnectionmanagerXml`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidConnectionmanagerConnectionmanagerXml.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headdlnabyuuidconnectionmanagerconnectionmanagerxml"></a>

## HEAD /Dlna/{UuId}/connectionmanager/connectionmanager.xml

- Operation ID: `headDlnaByUuidConnectionmanagerConnectionmanagerXml`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/headDlnaByUuidConnectionmanagerConnectionmanagerXml.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/head/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1connectionmanager.xml/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postdlnabyuuidconnectionmanagercontrol"></a>

## POST /Dlna/{UuId}/connectionmanager/control

- Operation ID: `postDlnaByUuidConnectionmanagerControl`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1connectionmanager~1control/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/postDlnaByUuidConnectionmanagerControl.html).
- Consumes: `application/octet-stream`.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Dlna~1{UuId}~1connectionmanager~1control/post/parameters/1`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1control/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1control/post/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1control/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1connectionmanager~1control/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnabyuuidcontentdirectorycontentdirectory"></a>

## GET /Dlna/{UuId}/contentdirectory/contentdirectory

- Operation ID: `getDlnaByUuidContentdirectoryContentdirectory`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidContentdirectoryContentdirectory.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headdlnabyuuidcontentdirectorycontentdirectory"></a>

## HEAD /Dlna/{UuId}/contentdirectory/contentdirectory

- Operation ID: `headDlnaByUuidContentdirectoryContentdirectory`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/headDlnaByUuidContentdirectoryContentdirectory.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/head/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnabyuuidcontentdirectorycontentdirectoryxml"></a>

## GET /Dlna/{UuId}/contentdirectory/contentdirectory.xml

- Operation ID: `getDlnaByUuidContentdirectoryContentdirectoryXml`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidContentdirectoryContentdirectoryXml.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headdlnabyuuidcontentdirectorycontentdirectoryxml"></a>

## HEAD /Dlna/{UuId}/contentdirectory/contentdirectory.xml

- Operation ID: `headDlnaByUuidContentdirectoryContentdirectoryXml`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/headDlnaByUuidContentdirectoryContentdirectoryXml.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/head/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1contentdirectory.xml/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postdlnabyuuidcontentdirectorycontrol"></a>

## POST /Dlna/{UuId}/contentdirectory/control

- Operation ID: `postDlnaByUuidContentdirectoryControl`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1contentdirectory~1control/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/postDlnaByUuidContentdirectoryControl.html).
- Consumes: `application/octet-stream`.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Dlna~1{UuId}~1contentdirectory~1control/post/parameters/1`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1control/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1control/post/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1control/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1contentdirectory~1control/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnabyuuiddescription"></a>

## GET /Dlna/{UuId}/description

- Operation ID: `getDlnaByUuidDescription`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1description/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidDescription.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1description/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headdlnabyuuiddescription"></a>

## HEAD /Dlna/{UuId}/description

- Operation ID: `headDlnaByUuidDescription`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1description/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/headDlnaByUuidDescription.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1description/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description/head/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnabyuuiddescriptionxml"></a>

## GET /Dlna/{UuId}/description.xml

- Operation ID: `getDlnaByUuidDescriptionXml`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1description.xml/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidDescriptionXml.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1description.xml/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description.xml/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description.xml/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description.xml/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headdlnabyuuiddescriptionxml"></a>

## HEAD /Dlna/{UuId}/description.xml

- Operation ID: `headDlnaByUuidDescriptionXml`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1description.xml/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/headDlnaByUuidDescriptionXml.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1description.xml/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description.xml/head/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description.xml/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1description.xml/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnabyuuidiconsbyfilename"></a>

## GET /Dlna/{UuId}/icons/{Filename}

- Operation ID: `getDlnaByUuidIconsByFilename`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1{UuId}~1icons~1{Filename}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaByUuidIconsByFilename.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `path` | true | string | not declared | not declared |
| `Filename` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1{UuId}~1icons~1{Filename}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1icons~1{Filename}/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1icons~1{Filename}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1{UuId}~1icons~1{Filename}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getdlnaiconsbyfilename"></a>

## GET /Dlna/icons/{Filename}

- Operation ID: `getDlnaIconsByFilename`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Dlna~1icons~1{Filename}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DlnaServerService/getDlnaIconsByFilename.html).
- Consumes: not declared.
- Produces: not declared.

### Source authentication declarations

```json
{
  "securityDeclared": false,
  "security": null,
  "x-RequiredAuthentication": "No authentication required"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `UuId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filename` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Dlna~1icons~1{Filename}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1icons~1{Filename}/get/responses/400` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1icons~1{Filename}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Dlna~1icons~1{Filename}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
