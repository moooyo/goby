# SubtitleService

Subtitle search, provider downloads, delivery, deletion, and media attachments.

**16 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`](#operation-getitemsbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat)
- [`HEAD /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`](#operation-headitemsbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat)
- [`GET /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`](#operation-getitemsbyidbymediasourceidsubtitlesbyindexstreambyformat)
- [`HEAD /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`](#operation-headitemsbyidbymediasourceidsubtitlesbyindexstreambyformat)
- [`GET /Items/{Id}/RemoteSearch/Subtitles/{Language}`](#operation-getitemsbyidremotesearchsubtitlesbylanguage)
- [`POST /Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}`](#operation-postitemsbyidremotesearchsubtitlesbysubtitleid)
- [`DELETE /Items/{Id}/Subtitles/{Index}`](#operation-deleteitemsbyidsubtitlesbyindex)
- [`POST /Items/{Id}/Subtitles/{Index}/Delete`](#operation-postitemsbyidsubtitlesbyindexdelete)
- [`GET /Providers/Subtitles/Subtitles/{Id}`](#operation-getproviderssubtitlessubtitlesbyid)
- [`GET /Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream`](#operation-getvideosbyidbymediasourceidattachmentsbyindexstream)
- [`GET /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`](#operation-getvideosbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat)
- [`HEAD /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}`](#operation-headvideosbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat)
- [`GET /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`](#operation-getvideosbyidbymediasourceidsubtitlesbyindexstreambyformat)
- [`HEAD /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}`](#operation-headvideosbyidbymediasourceidsubtitlesbyindexstreambyformat)
- [`DELETE /Videos/{Id}/Subtitles/{Index}`](#operation-deletevideosbyidsubtitlesbyindex)
- [`POST /Videos/{Id}/Subtitles/{Index}/Delete`](#operation-postvideosbyidsubtitlesbyindexdelete)

<a id="operation-getitemsbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat"></a>

## GET /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}

- Operation ID: `getItemsByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getItemsByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `path` | true | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headitemsbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat"></a>

## HEAD /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}

- Operation ID: `headItemsByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/headItemsByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `path` | true | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidbymediasourceidsubtitlesbyindexstreambyformat"></a>

## GET /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}

- Operation ID: `getItemsByIdByMediasourceidSubtitlesByIndexStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getItemsByIdByMediasourceidSubtitlesByIndexStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headitemsbyidbymediasourceidsubtitlesbyindexstreambyformat"></a>

## HEAD /Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}

- Operation ID: `headItemsByIdByMediasourceidSubtitlesByIndexStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/headItemsByIdByMediasourceidSubtitlesByIndexStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getitemsbyidremotesearchsubtitlesbylanguage"></a>

## GET /Items/{Id}/RemoteSearch/Subtitles/{Language}

- Operation ID: `getItemsByIdRemotesearchSubtitlesByLanguage`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getItemsByIdRemotesearchSubtitlesByLanguage.html).
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
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `Language` | `path` | true | string | not declared | not declared |
| `IsPerfectMatch` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsForced` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHearingImpaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[RemoteSubtitleInfo](../models.md#model-remotesubtitleinfo)&gt; | Operation successful. Returning a RemoteSubtitleInfo[] object. | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{Language}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidremotesearchsubtitlesbysubtitleid"></a>

## POST /Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}

- Operation ID: `postItemsByIdRemotesearchSubtitlesBySubtitleid`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/postItemsByIdRemotesearchSubtitlesBySubtitleid.html).
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
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `SubtitleId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [Subtitles.SubtitleDownloadResult](../models.md#model-subtitles-subtitledownloadresult) | Operation successful. Returning a SubtitleDownloadResult object. | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1RemoteSearch~1Subtitles~1{SubtitleId}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteitemsbyidsubtitlesbyindex"></a>

## DELETE /Items/{Id}/Subtitles/{Index}

- Operation ID: `deleteItemsByIdSubtitlesByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/deleteItemsByIdSubtitlesByIndex.html).
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
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postitemsbyidsubtitlesbyindexdelete"></a>

## POST /Items/{Id}/Subtitles/{Index}/Delete

- Operation ID: `postItemsByIdSubtitlesByIndexDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/postItemsByIdSubtitlesByIndexDelete.html).
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
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Items~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getproviderssubtitlessubtitlesbyid"></a>

## GET /Providers/Subtitles/Subtitles/{Id}

- Operation ID: `getProvidersSubtitlesSubtitlesById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getProvidersSubtitlesSubtitlesById.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Providers~1Subtitles~1Subtitles~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidbymediasourceidattachmentsbyindexstream"></a>

## GET /Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream

- Operation ID: `getVideosByIdByMediasourceidAttachmentsByIndexStream`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getVideosByIdByMediasourceidAttachmentsByIndexStream.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Attachments~1{Index}~1Stream/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat"></a>

## GET /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}

- Operation ID: `getVideosByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getVideosByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `path` | true | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat"></a>

## HEAD /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}

- Operation ID: `headVideosByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/headVideosByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `path` | true | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1{StartPositionTicks}~1Stream.{Format}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidbymediasourceidsubtitlesbyindexstreambyformat"></a>

## GET /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}

- Operation ID: `getVideosByIdByMediasourceidSubtitlesByIndexStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/getVideosByIdByMediasourceidSubtitlesByIndexStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidbymediasourceidsubtitlesbyindexstreambyformat"></a>

## HEAD /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}

- Operation ID: `headVideosByIdByMediasourceidSubtitlesByIndexStreamByFormat`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/headVideosByIdByMediasourceidSubtitlesByIndexStreamByFormat.html).
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
| `MediaSourceId` | `path` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |
| `Format` | `path` | true | string | not declared | not declared |
| `StartPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `EndPositionTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{MediaSourceId}~1Subtitles~1{Index}~1Stream.{Format}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletevideosbyidsubtitlesbyindex"></a>

## DELETE /Videos/{Id}/Subtitles/{Index}

- Operation ID: `deleteVideosByIdSubtitlesByIndex`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/deleteVideosByIdSubtitlesByIndex.html).
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
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postvideosbyidsubtitlesbyindexdelete"></a>

## POST /Videos/{Id}/Subtitles/{Index}/Delete

- Operation ID: `postVideosByIdSubtitlesByIndexDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/SubtitleService/postVideosByIdSubtitlesByIndexDelete.html).
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
| `MediaSourceId` | `query` | true | string | not declared | not declared |
| `Index` | `path` | true | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1Subtitles~1{Index}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
