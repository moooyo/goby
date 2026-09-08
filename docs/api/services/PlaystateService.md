# PlaystateService

Playback reporting and user play-state changes.

**12 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`POST /Sessions/Playing`](#operation-postsessionsplaying)
- [`POST /Sessions/Playing/Ping`](#operation-postsessionsplayingping)
- [`POST /Sessions/Playing/Progress`](#operation-postsessionsplayingprogress)
- [`POST /Sessions/Playing/Stopped`](#operation-postsessionsplayingstopped)
- [`POST /Users/{UserId}/Items/{ItemId}/UserData`](#operation-postusersbyuseriditemsbyitemiduserdata)
- [`POST /Users/{UserId}/PlayedItems/{Id}`](#operation-postusersbyuseridplayeditemsbyid)
- [`DELETE /Users/{UserId}/PlayedItems/{Id}`](#operation-deleteusersbyuseridplayeditemsbyid)
- [`POST /Users/{UserId}/PlayedItems/{Id}/Delete`](#operation-postusersbyuseridplayeditemsbyiddelete)
- [`POST /Users/{UserId}/PlayingItems/{Id}`](#operation-postusersbyuseridplayingitemsbyid)
- [`DELETE /Users/{UserId}/PlayingItems/{Id}`](#operation-deleteusersbyuseridplayingitemsbyid)
- [`POST /Users/{UserId}/PlayingItems/{Id}/Delete`](#operation-postusersbyuseridplayingitemsbyiddelete)
- [`POST /Users/{UserId}/PlayingItems/{Id}/Progress`](#operation-postusersbyuseridplayingitemsbyidprogress)

<a id="operation-postsessionsplaying"></a>

## POST /Sessions/Playing

- Operation ID: `postSessionsPlaying`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sessions~1Playing/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postSessionsPlaying.html).
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
| `body` | `body` | true | [PlaybackStartInfo](../models.md#model-playbackstartinfo) | not declared | not declared |

### Request body and model references

- `body`: [PlaybackStartInfo](../models.md#model-playbackstartinfo).
  Source pointer: `#/paths/~1Sessions~1Playing/post/parameters/0`.

Direct schema references in all request parameters:

- [PlaybackStartInfo](../models.md#model-playbackstartinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sessions~1Playing/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsessionsplayingping"></a>

## POST /Sessions/Playing/Ping

- Operation ID: `postSessionsPlayingPing`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sessions~1Playing~1Ping/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postSessionsPlayingPing.html).
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
| `PlaySessionId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sessions~1Playing~1Ping/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Ping/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Ping/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Ping/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Ping/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Ping/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsessionsplayingprogress"></a>

## POST /Sessions/Playing/Progress

- Operation ID: `postSessionsPlayingProgress`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sessions~1Playing~1Progress/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postSessionsPlayingProgress.html).
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
| `body` | `body` | true | [PlaybackProgressInfo](../models.md#model-playbackprogressinfo) | not declared | not declared |

### Request body and model references

- `body`: [PlaybackProgressInfo](../models.md#model-playbackprogressinfo).
  Source pointer: `#/paths/~1Sessions~1Playing~1Progress/post/parameters/0`.

Direct schema references in all request parameters:

- [PlaybackProgressInfo](../models.md#model-playbackprogressinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sessions~1Playing~1Progress/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Progress/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Progress/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Progress/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Progress/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Progress/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postsessionsplayingstopped"></a>

## POST /Sessions/Playing/Stopped

- Operation ID: `postSessionsPlayingStopped`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Sessions~1Playing~1Stopped/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postSessionsPlayingStopped.html).
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
| `body` | `body` | true | [PlaybackStopInfo](../models.md#model-playbackstopinfo) | not declared | not declared |

### Request body and model references

- `body`: [PlaybackStopInfo](../models.md#model-playbackstopinfo).
  Source pointer: `#/paths/~1Sessions~1Playing~1Stopped/post/parameters/0`.

Direct schema references in all request parameters:

- [PlaybackStopInfo](../models.md#model-playbackstopinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Sessions~1Playing~1Stopped/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Stopped/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Stopped/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Stopped/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Stopped/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Sessions~1Playing~1Stopped/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseriditemsbyitemiduserdata"></a>

## POST /Users/{UserId}/Items/{ItemId}/UserData

- Operation ID: `postUsersByUseridItemsByItemidUserdata`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postUsersByUseridItemsByItemidUserdata.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `ItemId` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [UserItemDataDto](../models.md#model-useritemdatadto) | not declared | not declared |

### Request body and model references

- `body`: [UserItemDataDto](../models.md#model-useritemdatadto).
  Source pointer: `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/parameters/2`.

Direct schema references in all request parameters:

- [UserItemDataDto](../models.md#model-useritemdatadto).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1Items~1{ItemId}~1UserData/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseridplayeditemsbyid"></a>

## POST /Users/{UserId}/PlayedItems/{Id}

- Operation ID: `postUsersByUseridPlayeditemsById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postUsersByUseridPlayeditemsById.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `DatePlayed` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [UserItemDataDto](../models.md#model-useritemdatadto) | Operation successful. Returning a UserItemDataDto object. | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteusersbyuseridplayeditemsbyid"></a>

## DELETE /Users/{UserId}/PlayedItems/{Id}

- Operation ID: `deleteUsersByUseridPlayeditemsById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/deleteUsersByUseridPlayeditemsById.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [UserItemDataDto](../models.md#model-useritemdatadto) | Operation successful. Returning a UserItemDataDto object. | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseridplayeditemsbyiddelete"></a>

## POST /Users/{UserId}/PlayedItems/{Id}/Delete

- Operation ID: `postUsersByUseridPlayeditemsByIdDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postUsersByUseridPlayeditemsByIdDelete.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [UserItemDataDto](../models.md#model-useritemdatadto) | Operation successful. Returning a UserItemDataDto object. | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayedItems~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseridplayingitemsbyid"></a>

## POST /Users/{UserId}/PlayingItems/{Id}

- Operation ID: `postUsersByUseridPlayingitemsById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postUsersByUseridPlayingitemsById.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `CanSeek` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `PlayMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `LiveStreamId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PlaySessionId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteusersbyuseridplayingitemsbyid"></a>

## DELETE /Users/{UserId}/PlayingItems/{Id}

- Operation ID: `deleteUsersByUseridPlayingitemsById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/deleteUsersByUseridPlayingitemsById.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `NextMediaType` | `query` | true | string | not declared | not declared |
| `PositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `LiveStreamId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PlaySessionId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseridplayingitemsbyiddelete"></a>

## POST /Users/{UserId}/PlayingItems/{Id}/Delete

- Operation ID: `postUsersByUseridPlayingitemsByIdDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postUsersByUseridPlayingitemsByIdDelete.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `NextMediaType` | `query` | true | string | not declared | not declared |
| `PositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `LiveStreamId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PlaySessionId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseridplayingitemsbyidprogress"></a>

## POST /Users/{UserId}/PlayingItems/{Id}/Progress

- Operation ID: `postUsersByUseridPlayingitemsByIdProgress`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PlaystateService/postUsersByUseridPlayingitemsByIdProgress.html).
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
| `UserId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `PositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `IsPaused` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMuted` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VolumeLevel` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `PlayMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `LiveStreamId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PlaySessionId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `RepeatMode` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `SubtitleOffset` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `PlaybackRate` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `body` | `body` | true | [Api.OnPlaybackProgress](../models.md#model-api-onplaybackprogress) | not declared | not declared |

### Request body and model references

- `body`: [Api.OnPlaybackProgress](../models.md#model-api-onplaybackprogress).
  Source pointer: `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/parameters/15`.

Direct schema references in all request parameters:

- [Api.OnPlaybackProgress](../models.md#model-api-onplaybackprogress).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1PlayingItems~1{Id}~1Progress/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
