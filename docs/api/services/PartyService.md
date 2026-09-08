# PartyService

Synchronized group playback.

**5 HTTP operations**. Service candidate: `DEFERRED`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Parties`](#operation-getparties)
- [`POST /Parties`](#operation-postparties)
- [`POST /Parties/{Id}/Join`](#operation-postpartiesbyidjoin)
- [`GET /Parties/Info`](#operation-getpartiesinfo)
- [`POST /Parties/Leave`](#operation-postpartiesleave)

<a id="operation-getparties"></a>

## GET /Parties

- Operation ID: `getParties`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Parties/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PartyService/getParties.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Parties/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postparties"></a>

## POST /Parties

- Operation ID: `postParties`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Parties/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PartyService/postParties.html).
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
| `200` | [Session.PartyInfoResult](../models.md#model-session-partyinforesult) | Operation successful. Returning a PartyInfoResult object. | `#/paths/~1Parties/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postpartiesbyidjoin"></a>

## POST /Parties/{Id}/Join

- Operation ID: `postPartiesByIdJoin`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Parties~1{Id}~1Join/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PartyService/postPartiesByIdJoin.html).
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
| `200` | [Session.PartyInfoResult](../models.md#model-session-partyinforesult) | Operation successful. Returning a PartyInfoResult object. | `#/paths/~1Parties~1{Id}~1Join/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1{Id}~1Join/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1{Id}~1Join/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1{Id}~1Join/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1{Id}~1Join/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1{Id}~1Join/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getpartiesinfo"></a>

## GET /Parties/Info

- Operation ID: `getPartiesInfo`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Parties~1Info/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PartyService/getPartiesInfo.html).
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
| `200` | [Session.PartyInfoResult](../models.md#model-session-partyinforesult) | Operation successful. Returning a PartyInfoResult object. | `#/paths/~1Parties~1Info/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Info/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Info/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Info/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Info/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Info/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postpartiesleave"></a>

## POST /Parties/Leave

- Operation ID: `postPartiesLeave`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Parties~1Leave/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PartyService/postPartiesLeave.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Parties~1Leave/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Leave/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Leave/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Leave/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Leave/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Parties~1Leave/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
