# PackageService

Emby package catalog and installation.

**6 HTTP operations**. Service candidate: `OUT-OF-SCOPE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Packages`](#operation-getpackages)
- [`GET /Packages/{Name}`](#operation-getpackagesbyname)
- [`POST /Packages/Installed/{Name}`](#operation-postpackagesinstalledbyname)
- [`DELETE /Packages/Installing/{Id}`](#operation-deletepackagesinstallingbyid)
- [`POST /Packages/Installing/{Id}/Delete`](#operation-postpackagesinstallingbyiddelete)
- [`GET /Packages/Updates`](#operation-getpackagesupdates)

<a id="operation-getpackages"></a>

## GET /Packages

- Operation ID: `getPackages`.
- Service candidate: `OUT-OF-SCOPE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Packages/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PackageService/getPackages.html).
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
| `PackageType` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `TargetSystems` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `IsPremium` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsAdult` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[PackageInfo](../models.md#model-packageinfo)&gt; | Operation successful. Returning a PackageInfo[] object. | `#/paths/~1Packages/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getpackagesbyname"></a>

## GET /Packages/{Name}

- Operation ID: `getPackagesByName`.
- Service candidate: `OUT-OF-SCOPE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Packages~1{Name}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PackageService/getPackagesByName.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `AssemblyGuid` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [PackageInfo](../models.md#model-packageinfo) | Operation successful. Returning a PackageInfo object. | `#/paths/~1Packages~1{Name}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1{Name}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1{Name}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1{Name}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1{Name}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1{Name}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postpackagesinstalledbyname"></a>

## POST /Packages/Installed/{Name}

- Operation ID: `postPackagesInstalledByName`.
- Service candidate: `OUT-OF-SCOPE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Packages~1Installed~1{Name}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PackageService/postPackagesInstalledByName.html).
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
| `Name` | `path` | true | string | not declared | not declared |
| `AssemblyGuid` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `Version` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `UpdateClass` | `query` | omitted (Swagger default: false) | unspecified | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Packages~1Installed~1{Name}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installed~1{Name}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installed~1{Name}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installed~1{Name}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installed~1{Name}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installed~1{Name}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletepackagesinstallingbyid"></a>

## DELETE /Packages/Installing/{Id}

- Operation ID: `deletePackagesInstallingById`.
- Service candidate: `OUT-OF-SCOPE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Packages~1Installing~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PackageService/deletePackagesInstallingById.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Packages~1Installing~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postpackagesinstallingbyiddelete"></a>

## POST /Packages/Installing/{Id}/Delete

- Operation ID: `postPackagesInstallingByIdDelete`.
- Service candidate: `OUT-OF-SCOPE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Packages~1Installing~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PackageService/postPackagesInstallingByIdDelete.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Packages~1Installing~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Installing~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getpackagesupdates"></a>

## GET /Packages/Updates

- Operation ID: `getPackagesUpdates`.
- Service candidate: `OUT-OF-SCOPE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Packages~1Updates/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/PackageService/getPackagesUpdates.html).
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
| `PackageType` | `query` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[PackageVersionInfo](../models.md#model-packageversioninfo)&gt; | Operation successful. Returning a PackageVersionInfo[] object. | `#/paths/~1Packages~1Updates/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Updates/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Updates/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Updates/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Updates/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Packages~1Updates/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
