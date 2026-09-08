# NotificationsService

Administrative notifications and notification type discovery.

**2 HTTP operations**. Service candidate: `EXPANSION`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`POST /Notifications/Admin`](#operation-postnotificationsadmin)
- [`GET /Notifications/Types`](#operation-getnotificationstypes)

<a id="operation-postnotificationsadmin"></a>

## POST /Notifications/Admin

- Operation ID: `postNotificationsAdmin`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Notifications~1Admin/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/NotificationsService/postNotificationsAdmin.html).
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
| `Name` | `query` | true | string | not declared | not declared |
| `Description` | `query` | true | string | not declared | not declared |
| `ImageUrl` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Url` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Level` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `body` | `body` | true | [Api.AddAdminNotification](../models.md#model-api-addadminnotification) | not declared | not declared |

### Request body and model references

- `body`: [Api.AddAdminNotification](../models.md#model-api-addadminnotification).
  Source pointer: `#/paths/~1Notifications~1Admin/post/parameters/5`.

Direct schema references in all request parameters:

- [Api.AddAdminNotification](../models.md#model-api-addadminnotification).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Notifications~1Admin/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Admin/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Admin/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Admin/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Admin/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Admin/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getnotificationstypes"></a>

## GET /Notifications/Types

- Operation ID: `getNotificationsTypes`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Notifications~1Types/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/NotificationsService/getNotificationsTypes.html).
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
| `200` | array&lt;[NotificationCategoryInfo](../models.md#model-notificationcategoryinfo)&gt; | Operation successful. Returning a NotificationCategoryInfo[] object. | `#/paths/~1Notifications~1Types/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Types/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Types/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Types/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Types/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Notifications~1Types/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
