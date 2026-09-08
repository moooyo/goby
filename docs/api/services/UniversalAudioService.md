# UniversalAudioService

Client-directed audio delivery negotiation.

**4 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Audio/{Id}/universal`](#operation-getaudiobyiduniversal)
- [`HEAD /Audio/{Id}/universal`](#operation-headaudiobyiduniversal)
- [`GET /Audio/{Id}/universal.{Container}`](#operation-getaudiobyiduniversalbycontainer)
- [`HEAD /Audio/{Id}/universal.{Container}`](#operation-headaudiobyiduniversalbycontainer)

<a id="operation-getaudiobyiduniversal"></a>

## GET /Audio/{Id}/universal

- Operation ID: `getAudioByIdUniversal`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1universal/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UniversalAudioService/getAudioByIdUniversal.html).
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
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1universal/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headaudiobyiduniversal"></a>

## HEAD /Audio/{Id}/universal

- Operation ID: `headAudioByIdUniversal`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1universal/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UniversalAudioService/headAudioByIdUniversal.html).
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
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1universal/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getaudiobyiduniversalbycontainer"></a>

## GET /Audio/{Id}/universal.{Container}

- Operation ID: `getAudioByIdUniversalByContainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1universal.{Container}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UniversalAudioService/getAudioByIdUniversalByContainer.html).
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
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `path` | true | string | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1universal.{Container}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headaudiobyiduniversalbycontainer"></a>

## HEAD /Audio/{Id}/universal.{Container}

- Operation ID: `headAudioByIdUniversalByContainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1universal.{Container}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UniversalAudioService/headAudioByIdUniversalByContainer.html).
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
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `path` | true | string | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1universal.{Container}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1universal.{Container}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
