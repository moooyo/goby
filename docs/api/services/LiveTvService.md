# LiveTvService

Tuners, guide data, channels, recordings, and recording schedules.

**61 HTTP operations**. Service candidate: `DEFERRED`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /LiveTv/AvailableRecordingOptions`](#operation-getlivetvavailablerecordingoptions)
- [`GET /LiveTv/ChannelMappingOptions`](#operation-getlivetvchannelmappingoptions)
- [`POST /LiveTv/ChannelMappingOptions`](#operation-postlivetvchannelmappingoptions)
- [`PUT /LiveTv/ChannelMappingOptions`](#operation-putlivetvchannelmappingoptions)
- [`DELETE /LiveTv/ChannelMappingOptions`](#operation-deletelivetvchannelmappingoptions)
- [`HEAD /LiveTv/ChannelMappingOptions`](#operation-headlivetvchannelmappingoptions)
- [`GET /LiveTv/ChannelMappings`](#operation-getlivetvchannelmappings)
- [`POST /LiveTv/ChannelMappings`](#operation-postlivetvchannelmappings)
- [`PUT /LiveTv/ChannelMappings`](#operation-putlivetvchannelmappings)
- [`DELETE /LiveTv/ChannelMappings`](#operation-deletelivetvchannelmappings)
- [`HEAD /LiveTv/ChannelMappings`](#operation-headlivetvchannelmappings)
- [`GET /LiveTv/Channels`](#operation-getlivetvchannels)
- [`GET /LiveTv/Channels/{Id}`](#operation-getlivetvchannelsbyid)
- [`GET /LiveTv/ChannelTags`](#operation-getlivetvchanneltags)
- [`GET /LiveTv/ChannelTags/Prefixes`](#operation-getlivetvchanneltagsprefixes)
- [`GET /LiveTv/EPG`](#operation-getlivetvepg)
- [`GET /LiveTv/Folder`](#operation-getlivetvfolder)
- [`GET /LiveTv/GuideInfo`](#operation-getlivetvguideinfo)
- [`GET /LiveTv/Info`](#operation-getlivetvinfo)
- [`GET /LiveTv/ListingProviders`](#operation-getlivetvlistingproviders)
- [`POST /LiveTv/ListingProviders`](#operation-postlivetvlistingproviders)
- [`DELETE /LiveTv/ListingProviders`](#operation-deletelivetvlistingproviders)
- [`GET /LiveTv/ListingProviders/Available`](#operation-getlivetvlistingprovidersavailable)
- [`GET /LiveTv/ListingProviders/Default`](#operation-getlivetvlistingprovidersdefault)
- [`POST /LiveTv/ListingProviders/Delete`](#operation-postlivetvlistingprovidersdelete)
- [`GET /LiveTv/ListingProviders/Lineups`](#operation-getlivetvlistingproviderslineups)
- [`GET /LiveTv/Manage/Channels`](#operation-getlivetvmanagechannels)
- [`POST /LiveTv/Manage/Channels/{Id}/Disabled`](#operation-postlivetvmanagechannelsbyiddisabled)
- [`POST /LiveTv/Manage/Channels/{Id}/SortIndex`](#operation-postlivetvmanagechannelsbyidsortindex)
- [`GET /LiveTv/Programs`](#operation-getlivetvprograms)
- [`POST /LiveTv/Programs`](#operation-postlivetvprograms)
- [`GET /LiveTv/Programs/Recommended`](#operation-getlivetvprogramsrecommended)
- [`GET /LiveTv/Recordings`](#operation-getlivetvrecordings)
- [`GET /LiveTv/Recordings/{Id}`](#operation-getlivetvrecordingsbyid)
- [`DELETE /LiveTv/Recordings/{Id}`](#operation-deletelivetvrecordingsbyid)
- [`POST /LiveTv/Recordings/{Id}/Delete`](#operation-postlivetvrecordingsbyiddelete)
- [`GET /LiveTv/Recordings/Folders`](#operation-getlivetvrecordingsfolders)
- [`GET /LiveTv/Recordings/Groups`](#operation-getlivetvrecordingsgroups)
- [`GET /LiveTv/Recordings/Series`](#operation-getlivetvrecordingsseries)
- [`GET /LiveTv/SeriesTimers`](#operation-getlivetvseriestimers)
- [`POST /LiveTv/SeriesTimers`](#operation-postlivetvseriestimers)
- [`GET /LiveTv/SeriesTimers/{Id}`](#operation-getlivetvseriestimersbyid)
- [`POST /LiveTv/SeriesTimers/{Id}`](#operation-postlivetvseriestimersbyid)
- [`DELETE /LiveTv/SeriesTimers/{Id}`](#operation-deletelivetvseriestimersbyid)
- [`POST /LiveTv/SeriesTimers/{Id}/Delete`](#operation-postlivetvseriestimersbyiddelete)
- [`GET /LiveTv/Timers`](#operation-getlivetvtimers)
- [`POST /LiveTv/Timers`](#operation-postlivetvtimers)
- [`GET /LiveTv/Timers/{Id}`](#operation-getlivetvtimersbyid)
- [`POST /LiveTv/Timers/{Id}`](#operation-postlivetvtimersbyid)
- [`DELETE /LiveTv/Timers/{Id}`](#operation-deletelivetvtimersbyid)
- [`POST /LiveTv/Timers/{Id}/Delete`](#operation-postlivetvtimersbyiddelete)
- [`GET /LiveTv/Timers/Defaults`](#operation-getlivetvtimersdefaults)
- [`GET /LiveTv/TunerHosts`](#operation-getlivetvtunerhosts)
- [`POST /LiveTv/TunerHosts`](#operation-postlivetvtunerhosts)
- [`DELETE /LiveTv/TunerHosts`](#operation-deletelivetvtunerhosts)
- [`GET /LiveTv/TunerHosts/Default/{Type}`](#operation-getlivetvtunerhostsdefaultbytype)
- [`POST /LiveTv/TunerHosts/Delete`](#operation-postlivetvtunerhostsdelete)
- [`GET /LiveTv/TunerHosts/Types`](#operation-getlivetvtunerhoststypes)
- [`POST /LiveTv/Tuners/{Id}/Reset`](#operation-postlivetvtunersbyidreset)
- [`GET /LiveTv/Tuners/Discover`](#operation-getlivetvtunersdiscover)
- [`GET /LiveTv/Tuners/Discvover`](#operation-getlivetvtunersdiscvover)

<a id="operation-getlivetvavailablerecordingoptions"></a>

## GET /LiveTv/AvailableRecordingOptions

- Operation ID: `getLivetvAvailablerecordingoptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1AvailableRecordingOptions/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvAvailablerecordingoptions.html).
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
| `200` | [Api.AvailableRecordingOptions](../models.md#model-api-availablerecordingoptions) | Operation successful. Returning a AvailableRecordingOptions object. | `#/paths/~1LiveTv~1AvailableRecordingOptions/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1AvailableRecordingOptions/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1AvailableRecordingOptions/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1AvailableRecordingOptions/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1AvailableRecordingOptions/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1AvailableRecordingOptions/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvchannelmappingoptions"></a>

## GET /LiveTv/ChannelMappingOptions

- Operation ID: `getLivetvChannelmappingoptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappingOptions/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvChannelmappingoptions.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappingOptions/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvchannelmappingoptions"></a>

## POST /LiveTv/ChannelMappingOptions

- Operation ID: `postLivetvChannelmappingoptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappingOptions/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvChannelmappingoptions.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappingOptions/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-putlivetvchannelmappingoptions"></a>

## PUT /LiveTv/ChannelMappingOptions

- Operation ID: `putLivetvChannelmappingoptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappingOptions/put`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/putLivetvChannelmappingoptions.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappingOptions/put/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/put/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/put/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/put/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/put/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/put/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvchannelmappingoptions"></a>

## DELETE /LiveTv/ChannelMappingOptions

- Operation ID: `deleteLivetvChannelmappingoptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappingOptions/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvChannelmappingoptions.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappingOptions/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvchannelmappingoptions"></a>

## HEAD /LiveTv/ChannelMappingOptions

- Operation ID: `headLivetvChannelmappingoptions`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappingOptions/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/headLivetvChannelmappingoptions.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappingOptions/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappingOptions/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvchannelmappings"></a>

## GET /LiveTv/ChannelMappings

- Operation ID: `getLivetvChannelmappings`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappings/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvChannelmappings.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappings/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvchannelmappings"></a>

## POST /LiveTv/ChannelMappings

- Operation ID: `postLivetvChannelmappings`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappings/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvChannelmappings.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |
| `body` | `body` | true | [Api.SetChannelMapping](../models.md#model-api-setchannelmapping) | not declared | not declared |

### Request body and model references

- `body`: [Api.SetChannelMapping](../models.md#model-api-setchannelmapping).
  Source pointer: `#/paths/~1LiveTv~1ChannelMappings/post/parameters/1`.

Direct schema references in all request parameters:

- [Api.SetChannelMapping](../models.md#model-api-setchannelmapping).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappings/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-putlivetvchannelmappings"></a>

## PUT /LiveTv/ChannelMappings

- Operation ID: `putLivetvChannelmappings`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappings/put`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/putLivetvChannelmappings.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |
| `body` | `body` | true | [Api.SetChannelMapping](../models.md#model-api-setchannelmapping) | not declared | not declared |

### Request body and model references

- `body`: [Api.SetChannelMapping](../models.md#model-api-setchannelmapping).
  Source pointer: `#/paths/~1LiveTv~1ChannelMappings/put/parameters/1`.

Direct schema references in all request parameters:

- [Api.SetChannelMapping](../models.md#model-api-setchannelmapping).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappings/put/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/put/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/put/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/put/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/put/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/put/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvchannelmappings"></a>

## DELETE /LiveTv/ChannelMappings

- Operation ID: `deleteLivetvChannelmappings`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappings/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvChannelmappings.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappings/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-headlivetvchannelmappings"></a>

## HEAD /LiveTv/ChannelMappings

- Operation ID: `headLivetvChannelmappings`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelMappings/head`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/headLivetvChannelmappings.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ProviderId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1ChannelMappings/head/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/head/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/head/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/head/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/head/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelMappings/head/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvchannels"></a>

## GET /LiveTv/Channels

- Operation ID: `getLivetvChannels`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Channels/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvChannels.html).
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
| `Type` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `IsLiked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsDisliked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableFavoriteSorting` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AddCurrentProgram` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1LiveTv~1Channels/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvchannelsbyid"></a>

## GET /LiveTv/Channels/{Id}

- Operation ID: `getLivetvChannelsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Channels~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvChannelsById.html).
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
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [BaseItemDto](../models.md#model-baseitemdto) | Operation successful. Returning a BaseItemDto object. | `#/paths/~1LiveTv~1Channels~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Channels~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvchanneltags"></a>

## GET /LiveTv/ChannelTags

- Operation ID: `getLivetvChanneltags`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelTags/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvChanneltags.html).
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
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1LiveTv~1ChannelTags/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvchanneltagsprefixes"></a>

## GET /LiveTv/ChannelTags/Prefixes

- Operation ID: `getLivetvChanneltagsPrefixes`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvChanneltagsPrefixes.html).
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
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[Api.TagItem](../models.md#model-api-tagitem)&gt; | Operation successful. Returning a TagItem[] object. | `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ChannelTags~1Prefixes/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvepg"></a>

## GET /LiveTv/EPG

- Operation ID: `getLivetvEPG`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1EPG/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvEPG.html).
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
| `Type` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `IsLiked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsDisliked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableFavoriteSorting` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AddCurrentProgram` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ChannelIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_Api.EpgRow](../models.md#model-queryresult_api-epgrow) | Operation successful. Returning a QueryResult<EpgRow> object. | `#/paths/~1LiveTv~1EPG/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1EPG/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1EPG/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1EPG/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1EPG/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1EPG/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvfolder"></a>

## GET /LiveTv/Folder

- Operation ID: `getLivetvFolder`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Folder/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvFolder.html).
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
| `200` | [BaseItemDto](../models.md#model-baseitemdto) | Operation successful. Returning a BaseItemDto object. | `#/paths/~1LiveTv~1Folder/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Folder/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Folder/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Folder/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Folder/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Folder/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvguideinfo"></a>

## GET /LiveTv/GuideInfo

- Operation ID: `getLivetvGuideinfo`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1GuideInfo/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvGuideinfo.html).
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
| `200` | [LiveTv.GuideInfo](../models.md#model-livetv-guideinfo) | Operation successful. Returning a GuideInfo object. | `#/paths/~1LiveTv~1GuideInfo/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1GuideInfo/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1GuideInfo/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1GuideInfo/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1GuideInfo/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1GuideInfo/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvinfo"></a>

## GET /LiveTv/Info

- Operation ID: `getLivetvInfo`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Info/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvInfo.html).
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
| `200` | [LiveTv.LiveTvInfo](../models.md#model-livetv-livetvinfo) | Operation successful. Returning a LiveTvInfo object. | `#/paths/~1LiveTv~1Info/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Info/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Info/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Info/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Info/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Info/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlistingproviders"></a>

## GET /LiveTv/ListingProviders

- Operation ID: `getLivetvListingproviders`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvListingproviders.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `ChannelId` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[LiveTv.ListingsProviderInfo](../models.md#model-livetv-listingsproviderinfo)&gt; | Operation successful. Returning a ListingsProviderInfo[] object. | `#/paths/~1LiveTv~1ListingProviders/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvlistingproviders"></a>

## POST /LiveTv/ListingProviders

- Operation ID: `postLivetvListingproviders`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvListingproviders.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `body` | `body` | true | [LiveTv.ListingsProviderInfo](../models.md#model-livetv-listingsproviderinfo) | not declared | not declared |

### Request body and model references

- `body`: [LiveTv.ListingsProviderInfo](../models.md#model-livetv-listingsproviderinfo).
  Source pointer: `#/paths/~1LiveTv~1ListingProviders/post/parameters/0`.

Direct schema references in all request parameters:

- [LiveTv.ListingsProviderInfo](../models.md#model-livetv-listingsproviderinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [LiveTv.ListingsProviderInfo](../models.md#model-livetv-listingsproviderinfo) | Operation successful. Returning a ListingsProviderInfo object. | `#/paths/~1LiveTv~1ListingProviders/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvlistingproviders"></a>

## DELETE /LiveTv/ListingProviders

- Operation ID: `deleteLivetvListingproviders`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvListingproviders.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1ListingProviders/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlistingprovidersavailable"></a>

## GET /LiveTv/ListingProviders/Available

- Operation ID: `getLivetvListingprovidersAvailable`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders~1Available/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvListingprovidersAvailable.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
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
| `200` | array&lt;[Api.ListingProviderTypeInfo](../models.md#model-api-listingprovidertypeinfo)&gt; | Operation successful. Returning a ListingProviderTypeInfo[] object. | `#/paths/~1LiveTv~1ListingProviders~1Available/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Available/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Available/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Available/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Available/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Available/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlistingprovidersdefault"></a>

## GET /LiveTv/ListingProviders/Default

- Operation ID: `getLivetvListingprovidersDefault`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders~1Default/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvListingprovidersDefault.html).
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
| `200` | [LiveTv.ListingsProviderInfo](../models.md#model-livetv-listingsproviderinfo) | Operation successful. Returning a ListingsProviderInfo object. | `#/paths/~1LiveTv~1ListingProviders~1Default/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Default/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Default/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Default/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Default/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Default/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvlistingprovidersdelete"></a>

## POST /LiveTv/ListingProviders/Delete

- Operation ID: `postLivetvListingprovidersDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvListingprovidersDelete.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1ListingProviders~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvlistingproviderslineups"></a>

## GET /LiveTv/ListingProviders/Lineups

- Operation ID: `getLivetvListingprovidersLineups`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1ListingProviders~1Lineups/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvListingprovidersLineups.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Type` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Location` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Country` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[NameIdPair](../models.md#model-nameidpair)&gt; | Operation successful. Returning a List<NameIdPair> object. | `#/paths/~1LiveTv~1ListingProviders~1Lineups/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Lineups/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Lineups/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Lineups/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Lineups/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1ListingProviders~1Lineups/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvmanagechannels"></a>

## GET /LiveTv/Manage/Channels

- Operation ID: `getLivetvManageChannels`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Manage~1Channels/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvManageChannels.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1LiveTv~1Manage~1Channels/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvmanagechannelsbyiddisabled"></a>

## POST /LiveTv/Manage/Channels/{Id}/Disabled

- Operation ID: `postLivetvManageChannelsByIdDisabled`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvManageChannelsByIdDisabled.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [Api.SetChannelDisabled](../models.md#model-api-setchanneldisabled) | not declared | not declared |

### Request body and model references

- `body`: [Api.SetChannelDisabled](../models.md#model-api-setchanneldisabled).
  Source pointer: `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/parameters/1`.

Direct schema references in all request parameters:

- [Api.SetChannelDisabled](../models.md#model-api-setchanneldisabled).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_ChannelManagementInfo](../models.md#model-queryresult_channelmanagementinfo) | Operation successful. Returning a QueryResult<ChannelManagementInfo> object. | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1Disabled/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvmanagechannelsbyidsortindex"></a>

## POST /LiveTv/Manage/Channels/{Id}/SortIndex

- Operation ID: `postLivetvManageChannelsByIdSortindex`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvManageChannelsByIdSortindex.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [Api.SetChannelSortIndex](../models.md#model-api-setchannelsortindex) | not declared | not declared |

### Request body and model references

- `body`: [Api.SetChannelSortIndex](../models.md#model-api-setchannelsortindex).
  Source pointer: `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/parameters/1`.

Direct schema references in all request parameters:

- [Api.SetChannelSortIndex](../models.md#model-api-setchannelsortindex).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_ChannelManagementInfo](../models.md#model-queryresult_channelmanagementinfo) | Operation successful. Returning a QueryResult<ChannelManagementInfo> object. | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Manage~1Channels~1{Id}~1SortIndex/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvprograms"></a>

## GET /LiveTv/Programs

- Operation ID: `getLivetvPrograms`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Programs/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvPrograms.html).
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
| `ChannelIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1Programs/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvprograms"></a>

## POST /LiveTv/Programs

- Operation ID: `postLivetvPrograms`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Programs/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvPrograms.html).
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
| `ChannelIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `body` | `body` | true | [Api.BaseItemsRequest](../models.md#model-api-baseitemsrequest) | not declared | not declared |

### Request body and model references

- `body`: [Api.BaseItemsRequest](../models.md#model-api-baseitemsrequest).
  Source pointer: `#/paths/~1LiveTv~1Programs/post/parameters/100`.

Direct schema references in all request parameters:

- [Api.BaseItemsRequest](../models.md#model-api-baseitemsrequest).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1Programs/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvprogramsrecommended"></a>

## GET /LiveTv/Programs/Recommended

- Operation ID: `getLivetvProgramsRecommended`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Programs~1Recommended/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvProgramsRecommended.html).
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
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1LiveTv~1Programs~1Recommended/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs~1Recommended/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs~1Recommended/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs~1Recommended/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs~1Recommended/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Programs~1Recommended/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvrecordings"></a>

## GET /LiveTv/Recordings

- Operation ID: `getLivetvRecordings`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvRecordings.html).
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
| `ChannelId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Status` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `IsInProgress` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesTimerId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1LiveTv~1Recordings/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvrecordingsbyid"></a>

## GET /LiveTv/Recordings/{Id}

- Operation ID: `getLivetvRecordingsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvRecordingsById.html).
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
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [BaseItemDto](../models.md#model-baseitemdto) | Operation successful. Returning a BaseItemDto object. | `#/paths/~1LiveTv~1Recordings~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvrecordingsbyid"></a>

## DELETE /LiveTv/Recordings/{Id}

- Operation ID: `deleteLivetvRecordingsById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvRecordingsById.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Recordings~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvrecordingsbyiddelete"></a>

## POST /LiveTv/Recordings/{Id}/Delete

- Operation ID: `postLivetvRecordingsByIdDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvRecordingsByIdDelete.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvrecordingsfolders"></a>

## GET /LiveTv/Recordings/Folders

- Operation ID: `getLivetvRecordingsFolders`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings~1Folders/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvRecordingsFolders.html).
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
| `ArtistType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MaxOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `HasThemeSong` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasThemeVideo` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSubtitles` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasSpecialFeature` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTrailer` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSpecialSeason` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `AdjacentTo` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartItemId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `MinIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxStartDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxEndDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MaxPlayers` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `ParentIndexNumber` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `HasParentalRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsHD` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsUnaired` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MinCommunityRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `MinCriticRating` | `query` | omitted (Swagger default: false) | number (double) | not declared | not declared |
| `AiredDuringSeason` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `MinPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSaved` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MinDateLastSavedForUser` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `MaxPremiereDate` | `query` | omitted (Swagger default: false) | string (date-time) | not declared | not declared |
| `HasOverview` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasImdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTmdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasTvdbId` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ExcludeItemIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Recursive` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SearchTerm` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ParentId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Fields` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IncludeItemTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AnyProviderIdEquals` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Filters` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsFavorite` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsMovie` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSeries` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsFolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNews` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsKids` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsSports` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNew` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsNewOrPremiere` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsRepeat` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ProjectToMedia` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `MediaTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPlayed` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Genres` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `OfficialRatings` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Tags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExcludeTags` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Years` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `EnableImages` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `EnableUserData` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `ImageTypeLimit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `EnableImageTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Person` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `PersonTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Studios` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `StudioIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Artists` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistIds` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Albums` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Ids` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Containers` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AudioLayouts` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `VideoCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ExtendedVideoTypes` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SubtitleCodecs` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Path` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UserId` | `query` | omitted (Swagger default: false) | string (guid) | not declared | not declared |
| `MinOfficialRating` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsLocked` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsPlaceHolder` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `HasOfficialRating` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `GroupItemsIntoCollections` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `Is3D` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `SeriesStatus` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `ArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `AlbumArtistStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameStartsWith` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `NameLessThan` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[BaseItemDto](../models.md#model-baseitemdto)&gt; | Operation successful. Returning a BaseItemDto[] object. | `#/paths/~1LiveTv~1Recordings~1Folders/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Folders/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Folders/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Folders/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Folders/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Folders/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvrecordingsgroups"></a>

## GET /LiveTv/Recordings/Groups

- Operation ID: `getLivetvRecordingsGroups`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings~1Groups/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvRecordingsGroups.html).
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1LiveTv~1Recordings~1Groups/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Groups/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Groups/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Groups/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Groups/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Groups/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvrecordingsseries"></a>

## GET /LiveTv/Recordings/Series

- Operation ID: `getLivetvRecordingsSeries`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Recordings~1Series/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvRecordingsSeries.html).
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
| `200` | [QueryResult_BaseItemDto](../models.md#model-queryresult_baseitemdto) | Operation successful. Returning a QueryResult<BaseItemDto> object. | `#/paths/~1LiveTv~1Recordings~1Series/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Series/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Series/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Series/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Series/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Recordings~1Series/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvseriestimers"></a>

## GET /LiveTv/SeriesTimers

- Operation ID: `getLivetvSeriestimers`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1SeriesTimers/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvSeriestimers.html).
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
| `SortBy` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_LiveTv.SeriesTimerInfoDto](../models.md#model-queryresult_livetv-seriestimerinfodto) | Operation successful. Returning a QueryResult<SeriesTimerInfoDto> object. | `#/paths/~1LiveTv~1SeriesTimers/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvseriestimers"></a>

## POST /LiveTv/SeriesTimers

- Operation ID: `postLivetvSeriestimers`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1SeriesTimers/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvSeriestimers.html).
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
| `body` | `body` | true | [LiveTv.SeriesTimerInfo](../models.md#model-livetv-seriestimerinfo) | not declared | not declared |

### Request body and model references

- `body`: [LiveTv.SeriesTimerInfo](../models.md#model-livetv-seriestimerinfo).
  Source pointer: `#/paths/~1LiveTv~1SeriesTimers/post/parameters/0`.

Direct schema references in all request parameters:

- [LiveTv.SeriesTimerInfo](../models.md#model-livetv-seriestimerinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [LiveTv.SeriesTimerInfoDto](../models.md#model-livetv-seriestimerinfodto) | Operation successful. Returning a SeriesTimerInfoDto object. | `#/paths/~1LiveTv~1SeriesTimers/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvseriestimersbyid"></a>

## GET /LiveTv/SeriesTimers/{Id}

- Operation ID: `getLivetvSeriestimersById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvSeriestimersById.html).
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
| `200` | [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto) | Operation successful. Returning a TimerInfoDto object. | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvseriestimersbyid"></a>

## POST /LiveTv/SeriesTimers/{Id}

- Operation ID: `postLivetvSeriestimersById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvSeriestimersById.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [LiveTv.SeriesTimerInfo](../models.md#model-livetv-seriestimerinfo) | not declared | not declared |

### Request body and model references

- `body`: [LiveTv.SeriesTimerInfo](../models.md#model-livetv-seriestimerinfo).
  Source pointer: `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/parameters/1`.

Direct schema references in all request parameters:

- [LiveTv.SeriesTimerInfo](../models.md#model-livetv-seriestimerinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvseriestimersbyid"></a>

## DELETE /LiveTv/SeriesTimers/{Id}

- Operation ID: `deleteLivetvSeriestimersById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvSeriestimersById.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvseriestimersbyiddelete"></a>

## POST /LiveTv/SeriesTimers/{Id}/Delete

- Operation ID: `postLivetvSeriestimersByIdDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvSeriestimersByIdDelete.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1SeriesTimers~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtimers"></a>

## GET /LiveTv/Timers

- Operation ID: `getLivetvTimers`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTimers.html).
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
| `ChannelId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SeriesTimerId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_LiveTv.TimerInfoDto](../models.md#model-queryresult_livetv-timerinfodto) | Operation successful. Returning a QueryResult<TimerInfoDto> object. | `#/paths/~1LiveTv~1Timers/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvtimers"></a>

## POST /LiveTv/Timers

- Operation ID: `postLivetvTimers`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvTimers.html).
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
| `body` | `body` | true | [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto) | not declared | not declared |

### Request body and model references

- `body`: [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto).
  Source pointer: `#/paths/~1LiveTv~1Timers/post/parameters/0`.

Direct schema references in all request parameters:

- [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Timers/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtimersbyid"></a>

## GET /LiveTv/Timers/{Id}

- Operation ID: `getLivetvTimersById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTimersById.html).
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
| `200` | [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto) | Operation successful. Returning a TimerInfoDto object. | `#/paths/~1LiveTv~1Timers~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvtimersbyid"></a>

## POST /LiveTv/Timers/{Id}

- Operation ID: `postLivetvTimersById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvTimersById.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto) | not declared | not declared |

### Request body and model references

- `body`: [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto).
  Source pointer: `#/paths/~1LiveTv~1Timers~1{Id}/post/parameters/1`.

Direct schema references in all request parameters:

- [LiveTv.TimerInfoDto](../models.md#model-livetv-timerinfodto).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Timers~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvtimersbyid"></a>

## DELETE /LiveTv/Timers/{Id}

- Operation ID: `deleteLivetvTimersById`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvTimersById.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Timers~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvtimersbyiddelete"></a>

## POST /LiveTv/Timers/{Id}/Delete

- Operation ID: `postLivetvTimersByIdDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvTimersByIdDelete.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtimersdefaults"></a>

## GET /LiveTv/Timers/Defaults

- Operation ID: `getLivetvTimersDefaults`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Timers~1Defaults/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTimersDefaults.html).
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
| `ProgramId` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [LiveTv.SeriesTimerInfoDto](../models.md#model-livetv-seriestimerinfodto) | Operation successful. Returning a SeriesTimerInfoDto object. | `#/paths/~1LiveTv~1Timers~1Defaults/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1Defaults/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1Defaults/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1Defaults/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1Defaults/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Timers~1Defaults/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtunerhosts"></a>

## GET /LiveTv/TunerHosts

- Operation ID: `getLivetvTunerhosts`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1TunerHosts/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTunerhosts.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
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
| `200` | array&lt;[LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo)&gt; | Operation successful. Returning a TunerHostInfo[] object. | `#/paths/~1LiveTv~1TunerHosts/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvtunerhosts"></a>

## POST /LiveTv/TunerHosts

- Operation ID: `postLivetvTunerhosts`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1TunerHosts/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvTunerhosts.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `body` | `body` | true | [LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo) | not declared | not declared |

### Request body and model references

- `body`: [LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo).
  Source pointer: `#/paths/~1LiveTv~1TunerHosts/post/parameters/0`.

Direct schema references in all request parameters:

- [LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo) | Operation successful. Returning a TunerHostInfo object. | `#/paths/~1LiveTv~1TunerHosts/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelivetvtunerhosts"></a>

## DELETE /LiveTv/TunerHosts

- Operation ID: `deleteLivetvTunerhosts`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1TunerHosts/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/deleteLivetvTunerhosts.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1TunerHosts/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtunerhostsdefaultbytype"></a>

## GET /LiveTv/TunerHosts/Default/{Type}

- Operation ID: `getLivetvTunerhostsDefaultByType`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTunerhostsDefaultByType.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Type` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo) | Operation successful. Returning a TunerHostInfo object. | `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Default~1{Type}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvtunerhostsdelete"></a>

## POST /LiveTv/TunerHosts/Delete

- Operation ID: `postLivetvTunerhostsDelete`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1TunerHosts~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvTunerhostsDelete.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
}
```

The source header declarations are included in the parameter table and retained verbatim in
`inventory.json`. The source defines `apikeyauth` as query `api_key` and leaves `embyauth` empty.
These source declarations require a separate permission decision before implementation.

### Parameters

| Name | In | Required declaration | Type or schema | Default / enum | Other constraints |
| --- | --- | --- | --- | --- | --- |
| `Id` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1TunerHosts~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtunerhoststypes"></a>

## GET /LiveTv/TunerHosts/Types

- Operation ID: `getLivetvTunerhostsTypes`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1TunerHosts~1Types/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTunerhostsTypes.html).
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
| `200` | array&lt;[NameIdPair](../models.md#model-nameidpair)&gt; | Operation successful. Returning a List<NameIdPair> object. | `#/paths/~1LiveTv~1TunerHosts~1Types/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Types/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Types/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Types/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Types/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1TunerHosts~1Types/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlivetvtunersbyidreset"></a>

## POST /LiveTv/Tuners/{Id}/Reset

- Operation ID: `postLivetvTunersByIdReset`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/postLivetvTunersByIdReset.html).
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
  "x-RequiredAuthentication": "Requires authentication as administrator"
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1{Id}~1Reset/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtunersdiscover"></a>

## GET /LiveTv/Tuners/Discover

- Operation ID: `getLivetvTunersDiscover`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Tuners~1Discover/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTunersDiscover.html).
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
| `200` | array&lt;[LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo)&gt; | Operation successful. Returning a List<TunerHostInfo> object. | `#/paths/~1LiveTv~1Tuners~1Discover/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discover/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discover/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discover/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discover/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discover/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlivetvtunersdiscvover"></a>

## GET /LiveTv/Tuners/Discvover

- Operation ID: `getLivetvTunersDiscvover`.
- Service candidate: `DEFERRED`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1LiveTv~1Tuners~1Discvover/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LiveTvService/getLivetvTunersDiscvover.html).
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
| `200` | array&lt;[LiveTv.TunerHostInfo](../models.md#model-livetv-tunerhostinfo)&gt; | Operation successful. Returning a List<TunerHostInfo> object. | `#/paths/~1LiveTv~1Tuners~1Discvover/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discvover/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discvover/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discvover/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discvover/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1LiveTv~1Tuners~1Discvover/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
