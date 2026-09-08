# VideoService

Progressive video delivery.

**6 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Videos/{Id}/{StreamFileName}`](#operation-getvideosbyidbystreamfilename)
- [`HEAD /Videos/{Id}/{StreamFileName}`](#operation-headvideosbyidbystreamfilename)
- [`GET /Videos/{Id}/stream`](#operation-getvideosbyidstream)
- [`HEAD /Videos/{Id}/stream`](#operation-headvideosbyidstream)
- [`GET /Videos/{Id}/stream.{Container}`](#operation-getvideosbyidstreambycontainer)
- [`HEAD /Videos/{Id}/stream.{Container}`](#operation-headvideosbyidstreambycontainer)

<a id="operation-getvideosbyidbystreamfilename"></a>

## GET /Videos/{Id}/{StreamFileName}

- Operation ID: `getVideosByIdByStreamfilename`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{StreamFileName}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/VideoService/getVideosByIdByStreamfilename.html).
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
| `DeviceProfileId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StreamFileName` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `query` | true | string | not declared | not declared |
| `AudioCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableAutoStreamCopy` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioSampleRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxAudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Static` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `MaxVideoBitDepth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{StreamFileName}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidbystreamfilename"></a>

## HEAD /Videos/{Id}/{StreamFileName}

- Operation ID: `headVideosByIdByStreamfilename`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1{StreamFileName}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/VideoService/headVideosByIdByStreamfilename.html).
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
| `DeviceProfileId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StreamFileName` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `query` | true | string | not declared | not declared |
| `AudioCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableAutoStreamCopy` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioSampleRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxAudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Static` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `MaxVideoBitDepth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1{StreamFileName}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1{StreamFileName}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidstream"></a>

## GET /Videos/{Id}/stream

- Operation ID: `getVideosByIdStream`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1stream/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/VideoService/getVideosByIdStream.html).
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
| `DeviceProfileId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `query` | true | string | not declared | not declared |
| `AudioCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableAutoStreamCopy` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioSampleRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxAudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Static` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `MaxVideoBitDepth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1stream/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidstream"></a>

## HEAD /Videos/{Id}/stream

- Operation ID: `headVideosByIdStream`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1stream/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/VideoService/headVideosByIdStream.html).
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
| `DeviceProfileId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `query` | true | string | not declared | not declared |
| `AudioCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableAutoStreamCopy` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioSampleRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxAudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Static` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `MaxVideoBitDepth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1stream/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidstreambycontainer"></a>

## GET /Videos/{Id}/stream.{Container}

- Operation ID: `getVideosByIdStreamByContainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1stream.{Container}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/VideoService/getVideosByIdStreamByContainer.html).
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
| `DeviceProfileId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `path` | true | string | not declared | not declared |
| `AudioCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableAutoStreamCopy` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioSampleRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxAudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Static` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `MaxVideoBitDepth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1stream.{Container}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidstreambycontainer"></a>

## HEAD /Videos/{Id}/stream.{Container}

- Operation ID: `headVideosByIdStreamByContainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1stream.{Container}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/VideoService/headVideosByIdStreamByContainer.html).
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
| `DeviceProfileId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `DeviceId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Container` | `path` | true | string | not declared | not declared |
| `AudioCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableAutoStreamCopy` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AudioSampleRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `AudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxAudioChannels` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Static` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `CopyTimestamps` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartTimeTicks` | `query` | omitted (Swagger default: false) | integer (int64) | not declared | not declared |
| `Width` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Height` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxWidth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxHeight` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoBitRate` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SubtitleMethod` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `MaxVideoBitDepth` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoCodec` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `VideoStreamIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1stream.{Container}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1stream.{Container}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
