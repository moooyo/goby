# SyncService

Offline synchronization jobs and targets.

**25 HTTP operations**. Service candidate: `DEFERRED`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`POST /Sync/{ItemId}/Status`](#operation-postsyncbyitemidstatus)
- [`DELETE /Sync/{TargetId}/Items`](#operation-deletesyncbytargetiditems)
- [`POST /Sync/{TargetId}/Items/Delete`](#operation-postsyncbytargetiditemsdelete)
- [`POST /Sync/Data`](#operation-postsyncdata)
- [`POST /Sync/Items/Cancel`](#operation-postsyncitemscancel)
- [`GET /Sync/Items/Ready`](#operation-getsyncitemsready)
- [`GET /Sync/JobItems`](#operation-getsyncjobitems)
- [`DELETE /Sync/JobItems/{Id}`](#operation-deletesyncjobitemsbyid)
- [`GET /Sync/JobItems/{Id}/AdditionalFiles`](#operation-getsyncjobitemsbyidadditionalfiles)
- [`POST /Sync/JobItems/{Id}/Delete`](#operation-postsyncjobitemsbyiddelete)
- [`POST /Sync/JobItems/{Id}/Enable`](#operation-postsyncjobitemsbyidenable)
- [`GET /Sync/JobItems/{Id}/File`](#operation-getsyncjobitemsbyidfile)
- [`HEAD /Sync/JobItems/{Id}/File`](#operation-headsyncjobitemsbyidfile)
- [`POST /Sync/JobItems/{Id}/MarkForRemoval`](#operation-postsyncjobitemsbyidmarkforremoval)
- [`POST /Sync/JobItems/{Id}/Transferred`](#operation-postsyncjobitemsbyidtransferred)
- [`POST /Sync/JobItems/{Id}/UnmarkForRemoval`](#operation-postsyncjobitemsbyidunmarkforremoval)
- [`GET /Sync/Jobs`](#operation-getsyncjobs)
- [`POST /Sync/Jobs`](#operation-postsyncjobs)
- [`GET /Sync/Jobs/{Id}`](#operation-getsyncjobsbyid)
- [`POST /Sync/Jobs/{Id}`](#operation-postsyncjobsbyid)
- [`DELETE /Sync/Jobs/{Id}`](#operation-deletesyncjobsbyid)
- [`POST /Sync/Jobs/{Id}/Delete`](#operation-postsyncjobsbyiddelete)
- [`POST /Sync/OfflineActions`](#operation-postsyncofflineactions)
- [`GET /Sync/Options`](#operation-getsyncoptions)
- [`GET /Sync/Targets`](#operation-getsynctargets)

<a id="operation-postsyncbyitemidstatus"></a>

## POST /Sync/{ItemId}/Status

- Operation ID: `postSyncByItemidStatus`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1{ItemId}~1Status/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncByItemidStatus.html).
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
| `ItemId` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [SyncedItemProgress](../models.md#model-synceditemprogress) | not declared | not declared |

### Request body and model references

- `body`: [SyncedItemProgress](../models.md#model-synceditemprogress).
  Source pointer: `#/paths/~1Sync~1{ItemId}~1Status/post/parameters/1`.

Direct schema references in all request parameters:

- [SyncedItemProgress](../models.md#model-synceditemprogress).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1{ItemId}~1Status/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{ItemId}~1Status/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{ItemId}~1Status/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{ItemId}~1Status/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{ItemId}~1Status/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{ItemId}~1Status/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletesyncbytargetiditems"></a>

## DELETE /Sync/{TargetId}/Items

- Operation ID: `deleteSyncByTargetidItems`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1{TargetId}~1Items/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/deleteSyncByTargetidItems.html).
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
| `TargetId` | `path` | true | string | not declared | not declared |
| `ItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1{TargetId}~1Items/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncbytargetiditemsdelete"></a>

## POST /Sync/{TargetId}/Items/Delete

- Operation ID: `postSyncByTargetidItemsDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncByTargetidItemsDelete.html).
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
| `TargetId` | `path` | true | string | not declared | not declared |
| `ItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1{TargetId}~1Items~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncdata"></a>

## POST /Sync/Data

- Operation ID: `postSyncData`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Data/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncData.html).
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
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `TargetId` | `query` | true | string | not declared | not declared |
| `body` | `body` | true | [SyncDataRequest](../models.md#model-syncdatarequest) | not declared | not declared |

### Request body and model references

- `body`: [SyncDataRequest](../models.md#model-syncdatarequest).
  Source pointer: `#/paths/~1Sync~1Data/post/parameters/1`.

Direct schema references in all request parameters:

- [SyncDataRequest](../models.md#model-syncdatarequest).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [SyncDataResponse](../models.md#model-syncdataresponse) | Operation successful. Returning a SyncDataResponse object. | `#/paths/~1Sync~1Data/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Data/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Data/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Data/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Data/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Data/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncitemscancel"></a>

## POST /Sync/Items/Cancel

- Operation ID: `postSyncItemsCancel`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Items~1Cancel/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncItemsCancel.html).
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
| `ItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1Items~1Cancel/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Cancel/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Cancel/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Cancel/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Cancel/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Cancel/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncitemsready"></a>

## GET /Sync/Items/Ready

- Operation ID: `getSyncItemsReady`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Items~1Ready/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncItemsReady.html).
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
| `TargetId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[SyncedItem](../models.md#model-synceditem)&gt; | Operation successful. Returning a List<SyncedItem> object. | `#/paths/~1Sync~1Items~1Ready/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Ready/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Ready/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Ready/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Ready/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Items~1Ready/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncjobitems"></a>

## GET /Sync/JobItems

- Operation ID: `getSyncJobitems`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncJobitems.html).
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
| `TargetId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_SyncJobItem](../models.md#model-queryresult_syncjobitem) | Operation successful. Returning a QueryResult<SyncJobItem> object. | `#/paths/~1Sync~1JobItems/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletesyncjobitemsbyid"></a>

## DELETE /Sync/JobItems/{Id}

- Operation ID: `deleteSyncJobitemsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/deleteSyncJobitemsById.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1JobItems~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncjobitemsbyidadditionalfiles"></a>

## GET /Sync/JobItems/{Id}/AdditionalFiles

- Operation ID: `getSyncJobitemsByIdAdditionalfiles`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncJobitemsByIdAdditionalfiles.html).
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
| `Name` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1AdditionalFiles/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobitemsbyiddelete"></a>

## POST /Sync/JobItems/{Id}/Delete

- Operation ID: `postSyncJobitemsByIdDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobitemsByIdDelete.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobitemsbyidenable"></a>

## POST /Sync/JobItems/{Id}/Enable

- Operation ID: `postSyncJobitemsByIdEnable`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobitemsByIdEnable.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Enable/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncjobitemsbyidfile"></a>

## GET /Sync/JobItems/{Id}/File

- Operation ID: `getSyncJobitemsByIdFile`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1File/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncJobitemsByIdFile.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Sync~1JobItems~1{Id}~1File/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headsyncjobitemsbyidfile"></a>

## HEAD /Sync/JobItems/{Id}/File

- Operation ID: `headSyncJobitemsByIdFile`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1File/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/headSyncJobitemsByIdFile.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Sync~1JobItems~1{Id}~1File/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1File/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobitemsbyidmarkforremoval"></a>

## POST /Sync/JobItems/{Id}/MarkForRemoval

- Operation ID: `postSyncJobitemsByIdMarkforremoval`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobitemsByIdMarkforremoval.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1MarkForRemoval/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobitemsbyidtransferred"></a>

## POST /Sync/JobItems/{Id}/Transferred

- Operation ID: `postSyncJobitemsByIdTransferred`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobitemsByIdTransferred.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1Transferred/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobitemsbyidunmarkforremoval"></a>

## POST /Sync/JobItems/{Id}/UnmarkForRemoval

- Operation ID: `postSyncJobitemsByIdUnmarkforremoval`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobitemsByIdUnmarkforremoval.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1JobItems~1{Id}~1UnmarkForRemoval/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncjobs"></a>

## GET /Sync/Jobs

- Operation ID: `getSyncJobs`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Jobs/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncJobs.html).
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
| `200` | [QueryResult_SyncJob](../models.md#model-queryresult_syncjob) | Operation successful. Returning a QueryResult<SyncJob> object. | `#/paths/~1Sync~1Jobs/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobs"></a>

## POST /Sync/Jobs

- Operation ID: `postSyncJobs`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Jobs/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobs.html).
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
  "x-RequiredAuthentication": "Requires authentication as user"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `body` | `body` | true | [SyncJobRequest](../models.md#model-syncjobrequest) | not declared | not declared |

### Request body and model references

- `body`: [SyncJobRequest](../models.md#model-syncjobrequest).
  Source pointer: `#/paths/~1Sync~1Jobs/post/parameters/0`.

Direct schema references in all request parameters:

- [SyncJobRequest](../models.md#model-syncjobrequest).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [SyncJobCreationResult](../models.md#model-syncjobcreationresult) | Operation successful. Returning a SyncJobCreationResult object. | `#/paths/~1Sync~1Jobs/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncjobsbyid"></a>

## GET /Sync/Jobs/{Id}

- Operation ID: `getSyncJobsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Jobs~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncJobsById.html).
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
| `200` | [SyncJob](../models.md#model-syncjob) | Operation successful. Returning a SyncJob object. | `#/paths/~1Sync~1Jobs~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobsbyid"></a>

## POST /Sync/Jobs/{Id}

- Operation ID: `postSyncJobsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Jobs~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobsById.html).
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
| `Id` | `path` | true | integer (int64) | not declared | not declared |
| `body` | `body` | true | [SyncJob](../models.md#model-syncjob) | not declared | not declared |

### Request body and model references

- `body`: [SyncJob](../models.md#model-syncjob).
  Source pointer: `#/paths/~1Sync~1Jobs~1{Id}/post/parameters/1`.

Direct schema references in all request parameters:

- [SyncJob](../models.md#model-syncjob).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1Jobs~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletesyncjobsbyid"></a>

## DELETE /Sync/Jobs/{Id}

- Operation ID: `deleteSyncJobsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Jobs~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/deleteSyncJobsById.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1Jobs~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncjobsbyiddelete"></a>

## POST /Sync/Jobs/{Id}/Delete

- Operation ID: `postSyncJobsByIdDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncJobsByIdDelete.html).
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

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Jobs~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsyncofflineactions"></a>

## POST /Sync/OfflineActions

- Operation ID: `postSyncOfflineactions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1OfflineActions/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/postSyncOfflineactions.html).
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
| `body` | `body` | true | array&lt;[UserAction](../models.md#model-useraction)&gt; | not declared | not declared |

### Request body and model references

- `body`: array&lt;[UserAction](../models.md#model-useraction)&gt;.
  Source pointer: `#/paths/~1Sync~1OfflineActions/post/parameters/0`.

Direct schema references in all request parameters:

- [UserAction](../models.md#model-useraction).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sync~1OfflineActions/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1OfflineActions/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1OfflineActions/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1OfflineActions/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1OfflineActions/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1OfflineActions/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsyncoptions"></a>

## GET /Sync/Options

- Operation ID: `getSyncOptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Options/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncOptions.html).
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
| `UserId` | `query` | true | string | not declared | not declared |
| `ItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `TargetId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Category` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [SyncDialogOptions](../models.md#model-syncdialogoptions) | Operation successful. Returning a SyncDialogOptions object. | `#/paths/~1Sync~1Options/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Options/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Options/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Options/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Options/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Options/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getsynctargets"></a>

## GET /Sync/Targets

- Operation ID: `getSyncTargets`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sync~1Targets/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SyncService/getSyncTargets.html).
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
| `UserId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[SyncTarget](../models.md#model-synctarget)&gt; | Operation successful. Returning a List<SyncTarget> object. | `#/paths/~1Sync~1Targets/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Targets/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Targets/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Targets/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Targets/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sync~1Targets/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
