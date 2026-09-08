# ItemRefreshService

Item metadata refresh requests.

**1 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`POST /Items/{Id}/Refresh`](#operation-postitemsbyidrefresh)

<a id="operation-postitemsbyidrefresh"></a>

## POST /Items/{Id}/Refresh

- Operation ID: `postItemsByIdRefresh`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Refresh/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/ItemRefreshService/postItemsByIdRefresh.html).
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
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MetadataRefreshMode` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `ImageRefreshMode` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `ReplaceAllMetadata` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ReplaceAllImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `body` | `body` | true | [BaseRefreshRequest](../models.md#model-baserefreshrequest) | not declared | not declared |

### Request body and model references

- `body`: [BaseRefreshRequest](../models.md#model-baserefreshrequest).
  Source pointer: `#/paths/~1Items~1{Id}~1Refresh/post/parameters/6`.

Direct schema references in all request parameters:

- [BaseRefreshRequest](../models.md#model-baserefreshrequest).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Items~1{Id}~1Refresh/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Refresh/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Refresh/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Refresh/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Refresh/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Refresh/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
