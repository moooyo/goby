# LibraryStructureService

Library roots, media paths, and library options.

**10 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`POST /Library/VirtualFolders`](#operation-postlibraryvirtualfolders)
- [`DELETE /Library/VirtualFolders`](#operation-deletelibraryvirtualfolders)
- [`POST /Library/VirtualFolders/Delete`](#operation-postlibraryvirtualfoldersdelete)
- [`POST /Library/VirtualFolders/LibraryOptions`](#operation-postlibraryvirtualfolderslibraryoptions)
- [`POST /Library/VirtualFolders/Name`](#operation-postlibraryvirtualfoldersname)
- [`POST /Library/VirtualFolders/Paths`](#operation-postlibraryvirtualfolderspaths)
- [`DELETE /Library/VirtualFolders/Paths`](#operation-deletelibraryvirtualfolderspaths)
- [`POST /Library/VirtualFolders/Paths/Delete`](#operation-postlibraryvirtualfolderspathsdelete)
- [`POST /Library/VirtualFolders/Paths/Update`](#operation-postlibraryvirtualfolderspathsupdate)
- [`GET /Library/VirtualFolders/Query`](#operation-getlibraryvirtualfoldersquery)

<a id="operation-postlibraryvirtualfolders"></a>

## POST /Library/VirtualFolders

- Operation ID: `postLibraryVirtualfolders`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfolders.html).
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
| `body` | `body` | true | [Library.AddVirtualFolder](../models.md#model-library-addvirtualfolder) | not declared | not declared |

### Request body and model references

- `body`: [Library.AddVirtualFolder](../models.md#model-library-addvirtualfolder).
  Source pointer: `#/paths/~1Library~1VirtualFolders/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.AddVirtualFolder](../models.md#model-library-addvirtualfolder).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelibraryvirtualfolders"></a>

## DELETE /Library/VirtualFolders

- Operation ID: `deleteLibraryVirtualfolders`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/deleteLibraryVirtualfolders.html).
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryvirtualfoldersdelete"></a>

## POST /Library/VirtualFolders/Delete

- Operation ID: `postLibraryVirtualfoldersDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfoldersDelete.html).
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
| `body` | `body` | true | [Library.RemoveVirtualFolder](../models.md#model-library-removevirtualfolder) | not declared | not declared |

### Request body and model references

- `body`: [Library.RemoveVirtualFolder](../models.md#model-library-removevirtualfolder).
  Source pointer: `#/paths/~1Library~1VirtualFolders~1Delete/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.RemoveVirtualFolder](../models.md#model-library-removevirtualfolder).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryvirtualfolderslibraryoptions"></a>

## POST /Library/VirtualFolders/LibraryOptions

- Operation ID: `postLibraryVirtualfoldersLibraryoptions`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfoldersLibraryoptions.html).
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
| `body` | `body` | true | [Library.UpdateLibraryOptions](../models.md#model-library-updatelibraryoptions) | not declared | not declared |

### Request body and model references

- `body`: [Library.UpdateLibraryOptions](../models.md#model-library-updatelibraryoptions).
  Source pointer: `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.UpdateLibraryOptions](../models.md#model-library-updatelibraryoptions).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1LibraryOptions/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryvirtualfoldersname"></a>

## POST /Library/VirtualFolders/Name

- Operation ID: `postLibraryVirtualfoldersName`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Name/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfoldersName.html).
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
| `body` | `body` | true | [Library.RenameVirtualFolder](../models.md#model-library-renamevirtualfolder) | not declared | not declared |

### Request body and model references

- `body`: [Library.RenameVirtualFolder](../models.md#model-library-renamevirtualfolder).
  Source pointer: `#/paths/~1Library~1VirtualFolders~1Name/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.RenameVirtualFolder](../models.md#model-library-renamevirtualfolder).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1Name/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Name/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Name/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Name/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Name/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Name/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryvirtualfolderspaths"></a>

## POST /Library/VirtualFolders/Paths

- Operation ID: `postLibraryVirtualfoldersPaths`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfoldersPaths.html).
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
| `body` | `body` | true | [Library.AddMediaPath](../models.md#model-library-addmediapath) | not declared | not declared |

### Request body and model references

- `body`: [Library.AddMediaPath](../models.md#model-library-addmediapath).
  Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.AddMediaPath](../models.md#model-library-addmediapath).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1Paths/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deletelibraryvirtualfolderspaths"></a>

## DELETE /Library/VirtualFolders/Paths

- Operation ID: `deleteLibraryVirtualfoldersPaths`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/deleteLibraryVirtualfoldersPaths.html).
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

No parameters are declared in the source for this operation.

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1Paths/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryvirtualfolderspathsdelete"></a>

## POST /Library/VirtualFolders/Paths/Delete

- Operation ID: `postLibraryVirtualfoldersPathsDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfoldersPathsDelete.html).
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
| `body` | `body` | true | [Library.RemoveMediaPath](../models.md#model-library-removemediapath) | not declared | not declared |

### Request body and model references

- `body`: [Library.RemoveMediaPath](../models.md#model-library-removemediapath).
  Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.RemoveMediaPath](../models.md#model-library-removemediapath).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postlibraryvirtualfolderspathsupdate"></a>

## POST /Library/VirtualFolders/Paths/Update

- Operation ID: `postLibraryVirtualfoldersPathsUpdate`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/postLibraryVirtualfoldersPathsUpdate.html).
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
| `body` | `body` | true | [Library.UpdateMediaPath](../models.md#model-library-updatemediapath) | not declared | not declared |

### Request body and model references

- `body`: [Library.UpdateMediaPath](../models.md#model-library-updatemediapath).
  Source pointer: `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/parameters/0`.

Direct schema references in all request parameters:

- [Library.UpdateMediaPath](../models.md#model-library-updatemediapath).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Paths~1Update/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getlibraryvirtualfoldersquery"></a>

## GET /Library/VirtualFolders/Query

- Operation ID: `getLibraryVirtualfoldersQuery`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Library~1VirtualFolders~1Query/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/LibraryStructureService/getLibraryVirtualfoldersQuery.html).
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
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_VirtualFolderInfo](../models.md#model-queryresult_virtualfolderinfo) | Operation successful. Returning a QueryResult<VirtualFolderInfo> object. | `#/paths/~1Library~1VirtualFolders~1Query/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Query/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Query/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Query/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Query/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Library~1VirtualFolders~1Query/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
