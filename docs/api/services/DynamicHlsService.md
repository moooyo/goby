# DynamicHlsService

Adaptive audio/video HLS playlists and segments.

**14 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`](#operation-getaudiobyidhls1byplaylistidbysegmentidbysegmentcontainer)
- [`HEAD /Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`](#operation-headaudiobyidhls1byplaylistidbysegmentidbysegmentcontainer)
- [`GET /Audio/{Id}/live.m3u8`](#operation-getaudiobyidlivem3u8)
- [`GET /Audio/{Id}/main.m3u8`](#operation-getaudiobyidmainm3u8)
- [`GET /Audio/{Id}/master.m3u8`](#operation-getaudiobyidmasterm3u8)
- [`HEAD /Audio/{Id}/master.m3u8`](#operation-headaudiobyidmasterm3u8)
- [`GET /Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`](#operation-getvideosbyidhls1byplaylistidbysegmentidbysegmentcontainer)
- [`HEAD /Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}`](#operation-headvideosbyidhls1byplaylistidbysegmentidbysegmentcontainer)
- [`GET /Videos/{Id}/live_subtitles.m3u8`](#operation-getvideosbyidlivesubtitlesm3u8)
- [`GET /Videos/{Id}/live.m3u8`](#operation-getvideosbyidlivem3u8)
- [`GET /Videos/{Id}/main.m3u8`](#operation-getvideosbyidmainm3u8)
- [`GET /Videos/{Id}/master.m3u8`](#operation-getvideosbyidmasterm3u8)
- [`HEAD /Videos/{Id}/master.m3u8`](#operation-headvideosbyidmasterm3u8)
- [`GET /Videos/{Id}/subtitles.m3u8`](#operation-getvideosbyidsubtitlesm3u8)

<a id="operation-getaudiobyidhls1byplaylistidbysegmentidbysegmentcontainer"></a>

## GET /Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}

- Operation ID: `getAudioByIdHls1ByPlaylistidBySegmentidBySegmentcontainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getAudioByIdHls1ByPlaylistidBySegmentidBySegmentcontainer.html).
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
| `SegmentContainer` | `path` | true | string | not declared | not declared |
| `SegmentId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `PlaylistId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headaudiobyidhls1byplaylistidbysegmentidbysegmentcontainer"></a>

## HEAD /Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}

- Operation ID: `headAudioByIdHls1ByPlaylistidBySegmentidBySegmentcontainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/headAudioByIdHls1ByPlaylistidBySegmentidBySegmentcontainer.html).
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
| `SegmentContainer` | `path` | true | string | not declared | not declared |
| `SegmentId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `PlaylistId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getaudiobyidlivem3u8"></a>

## GET /Audio/{Id}/live.m3u8

- Operation ID: `getAudioByIdLiveM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1live.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getAudioByIdLiveM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1live.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1live.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1live.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1live.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1live.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1live.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getaudiobyidmainm3u8"></a>

## GET /Audio/{Id}/main.m3u8

- Operation ID: `getAudioByIdMainM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1main.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getAudioByIdMainM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1main.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1main.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1main.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1main.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1main.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1main.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getaudiobyidmasterm3u8"></a>

## GET /Audio/{Id}/master.m3u8

- Operation ID: `getAudioByIdMasterM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1master.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getAudioByIdMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1master.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headaudiobyidmasterm3u8"></a>

## HEAD /Audio/{Id}/master.m3u8

- Operation ID: `headAudioByIdMasterM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Audio~1{Id}~1master.m3u8/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/headAudioByIdMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Audio~1{Id}~1master.m3u8/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Audio~1{Id}~1master.m3u8/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidhls1byplaylistidbysegmentidbysegmentcontainer"></a>

## GET /Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}

- Operation ID: `getVideosByIdHls1ByPlaylistidBySegmentidBySegmentcontainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getVideosByIdHls1ByPlaylistidBySegmentidBySegmentcontainer.html).
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
| `SegmentContainer` | `path` | true | string | not declared | not declared |
| `SegmentId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `PlaylistId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidhls1byplaylistidbysegmentidbysegmentcontainer"></a>

## HEAD /Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}

- Operation ID: `headVideosByIdHls1ByPlaylistidBySegmentidBySegmentcontainer`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/headVideosByIdHls1ByPlaylistidBySegmentidBySegmentcontainer.html).
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
| `SegmentContainer` | `path` | true | string | not declared | not declared |
| `SegmentId` | `path` | true | string | not declared | not declared |
| `Id` | `path` | true | string | not declared | not declared |
| `PlaylistId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1hls1~1{PlaylistId}~1{SegmentId}.{SegmentContainer}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidlivesubtitlesm3u8"></a>

## GET /Videos/{Id}/live_subtitles.m3u8

- Operation ID: `getVideosByIdLiveSubtitlesM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getVideosByIdLiveSubtitlesM3u8.html).
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
| `SubtitleSegmentLength` | `query` | true | integer (int32) | not declared | not declared |
| `ManifestSubtitles` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live_subtitles.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidlivem3u8"></a>

## GET /Videos/{Id}/live.m3u8

- Operation ID: `getVideosByIdLiveM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1live.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getVideosByIdLiveM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1live.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1live.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidmainm3u8"></a>

## GET /Videos/{Id}/main.m3u8

- Operation ID: `getVideosByIdMainM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1main.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getVideosByIdMainM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1main.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1main.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1main.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1main.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1main.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1main.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidmasterm3u8"></a>

## GET /Videos/{Id}/master.m3u8

- Operation ID: `getVideosByIdMasterM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1master.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getVideosByIdMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1master.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headvideosbyidmasterm3u8"></a>

## HEAD /Videos/{Id}/master.m3u8

- Operation ID: `headVideosByIdMasterM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1master.m3u8/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/headVideosByIdMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1master.m3u8/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1master.m3u8/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getvideosbyidsubtitlesm3u8"></a>

## GET /Videos/{Id}/subtitles.m3u8

- Operation ID: `getVideosByIdSubtitlesM3u8`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/DynamicHlsService/getVideosByIdSubtitlesM3u8.html).
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
| `SubtitleSegmentLength` | `query` | true | integer (int32) | not declared | not declared |
| `ManifestSubtitles` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Videos~1{Id}~1subtitles.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
