# LiveStreamService

Live TV recording and live-stream file delivery over HTTP and HLS.

**14 HTTP operations**. Service candidate: `DEFERRED`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /LiveTv/LiveRecordings/{Id}/hls/{Segment}`](#operation-getlivetvliverecordingsbyidhlsbysegment)
- [`HEAD /LiveTv/LiveRecordings/{Id}/hls/{Segment}`](#operation-headlivetvliverecordingsbyidhlsbysegment)
- [`GET /LiveTv/LiveRecordings/{Id}/hls/live.m3u8`](#operation-getlivetvliverecordingsbyidhlslivem3u8)
- [`HEAD /LiveTv/LiveRecordings/{Id}/hls/live.m3u8`](#operation-headlivetvliverecordingsbyidhlslivem3u8)
- [`GET /LiveTv/LiveRecordings/{Id}/hls/master.m3u8`](#operation-getlivetvliverecordingsbyidhlsmasterm3u8)
- [`HEAD /LiveTv/LiveRecordings/{Id}/hls/master.m3u8`](#operation-headlivetvliverecordingsbyidhlsmasterm3u8)
- [`GET /LiveTv/LiveRecordings/{Id}/stream`](#operation-getlivetvliverecordingsbyidstream)
- [`GET /LiveTv/LiveStreamFiles/{Id}/hls/{Segment}`](#operation-getlivetvlivestreamfilesbyidhlsbysegment)
- [`HEAD /LiveTv/LiveStreamFiles/{Id}/hls/{Segment}`](#operation-headlivetvlivestreamfilesbyidhlsbysegment)
- [`GET /LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8`](#operation-getlivetvlivestreamfilesbyidhlslivem3u8)
- [`HEAD /LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8`](#operation-headlivetvlivestreamfilesbyidhlslivem3u8)
- [`GET /LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8`](#operation-getlivetvlivestreamfilesbyidhlsmasterm3u8)
- [`HEAD /LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8`](#operation-headlivetvlivestreamfilesbyidhlsmasterm3u8)
- [`GET /LiveTv/LiveStreamFiles/{Id}/stream.{Container}`](#operation-getlivetvlivestreamfilesbyidstreambycontainer)

<a id="operation-getlivetvliverecordingsbyidhlsbysegment"></a>

## GET /LiveTv/LiveRecordings/{Id}/hls/{Segment}

- Operation ID: `getLivetvLiverecordingsByIdHlsBySegment`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLiverecordingsByIdHlsBySegment.html).
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
| `Segment` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvliverecordingsbyidhlsbysegment"></a>

## HEAD /LiveTv/LiveRecordings/{Id}/hls/{Segment}

- Operation ID: `headLivetvLiverecordingsByIdHlsBySegment`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/headLivetvLiverecordingsByIdHlsBySegment.html).
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
| `Segment` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1{Segment}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvliverecordingsbyidhlslivem3u8"></a>

## GET /LiveTv/LiveRecordings/{Id}/hls/live.m3u8

- Operation ID: `getLivetvLiverecordingsByIdHlsLiveM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLiverecordingsByIdHlsLiveM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvliverecordingsbyidhlslivem3u8"></a>

## HEAD /LiveTv/LiveRecordings/{Id}/hls/live.m3u8

- Operation ID: `headLivetvLiverecordingsByIdHlsLiveM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/headLivetvLiverecordingsByIdHlsLiveM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1live.m3u8/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvliverecordingsbyidhlsmasterm3u8"></a>

## GET /LiveTv/LiveRecordings/{Id}/hls/master.m3u8

- Operation ID: `getLivetvLiverecordingsByIdHlsMasterM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLiverecordingsByIdHlsMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvliverecordingsbyidhlsmasterm3u8"></a>

## HEAD /LiveTv/LiveRecordings/{Id}/hls/master.m3u8

- Operation ID: `headLivetvLiverecordingsByIdHlsMasterM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/headLivetvLiverecordingsByIdHlsMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1hls~1master.m3u8/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvliverecordingsbyidstream"></a>

## GET /LiveTv/LiveRecordings/{Id}/stream

- Operation ID: `getLivetvLiverecordingsByIdStream`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLiverecordingsByIdStream.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveRecordings~1{Id}~1stream/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlivestreamfilesbyidhlsbysegment"></a>

## GET /LiveTv/LiveStreamFiles/{Id}/hls/{Segment}

- Operation ID: `getLivetvLivestreamfilesByIdHlsBySegment`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLivestreamfilesByIdHlsBySegment.html).
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
| `Segment` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvlivestreamfilesbyidhlsbysegment"></a>

## HEAD /LiveTv/LiveStreamFiles/{Id}/hls/{Segment}

- Operation ID: `headLivetvLivestreamfilesByIdHlsBySegment`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/headLivetvLivestreamfilesByIdHlsBySegment.html).
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
| `Segment` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1{Segment}/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlivestreamfilesbyidhlslivem3u8"></a>

## GET /LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8

- Operation ID: `getLivetvLivestreamfilesByIdHlsLiveM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLivestreamfilesByIdHlsLiveM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvlivestreamfilesbyidhlslivem3u8"></a>

## HEAD /LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8

- Operation ID: `headLivetvLivestreamfilesByIdHlsLiveM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/headLivetvLivestreamfilesByIdHlsLiveM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1live.m3u8/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlivestreamfilesbyidhlsmasterm3u8"></a>

## GET /LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8

- Operation ID: `getLivetvLivestreamfilesByIdHlsMasterM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLivestreamfilesByIdHlsMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvlivestreamfilesbyidhlsmasterm3u8"></a>

## HEAD /LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8

- Operation ID: `headLivetvLivestreamfilesByIdHlsMasterM3u8`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/headLivetvLivestreamfilesByIdHlsMasterM3u8.html).
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
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1hls~1master.m3u8/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlivestreamfilesbyidstreambycontainer"></a>

## GET /LiveTv/LiveStreamFiles/{Id}/stream.{Container}

- Operation ID: `getLivetvLivestreamfilesByIdStreamByContainer`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveStreamService/getLivetvLivestreamfilesByIdStreamByContainer.html).
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
| `Container` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1LiveStreamFiles~1{Id}~1stream.{Container}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
