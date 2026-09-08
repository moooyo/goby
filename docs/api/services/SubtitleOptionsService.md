# SubtitleOptionsService

Reading and updating subtitle options.

**2 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Encoding/SubtitleOptions`](#operation-getencodingsubtitleoptions)
- [`POST /Encoding/SubtitleOptions`](#operation-postencodingsubtitleoptions)

<a id="operation-getencodingsubtitleoptions"></a>

## GET /Encoding/SubtitleOptions

- Operation ID: `getEncodingSubtitleoptions`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Encoding~1SubtitleOptions/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleOptionsService/getEncodingSubtitleoptions.html).
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
| `200` | [EditObjectContainer](../models.md#model-editobjectcontainer) | Operation successful. Returning a EditObjectContainer object. | `#/paths/~1Encoding~1SubtitleOptions/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postencodingsubtitleoptions"></a>

## POST /Encoding/SubtitleOptions

- Operation ID: `postEncodingSubtitleoptions`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Encoding~1SubtitleOptions/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleOptionsService/postEncodingSubtitleoptions.html).
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
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Encoding~1SubtitleOptions/post/parameters/0`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Encoding~1SubtitleOptions/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Encoding~1SubtitleOptions/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
