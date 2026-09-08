# BackupApi

Backup metadata and restoration; plugin/version provenance remains unresolved.

**3 HTTP operations**. Service candidate: `EXPANSION`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /BackupRestore/BackupInfo`](#operation-getbackuprestorebackupinfo)
- [`POST /BackupRestore/Restore`](#operation-postbackuprestorerestore)
- [`POST /BackupRestore/RestoreData`](#operation-postbackuprestorerestoredata)

<a id="operation-getbackuprestorebackupinfo"></a>

## GET /BackupRestore/BackupInfo

- Operation ID: `getBackuprestoreBackupinfo`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1BackupRestore~1BackupInfo/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/BackupApi/getBackuprestoreBackupinfo.html).
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
| `200` | [MBBackup.Api.AllBackupsInfo](../models.md#model-mbbackup-api-allbackupsinfo) | Operation successful. Returning a AllBackupsInfo object. | `#/paths/~1BackupRestore~1BackupInfo/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1BackupInfo/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1BackupInfo/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1BackupInfo/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1BackupInfo/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1BackupInfo/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postbackuprestorerestore"></a>

## POST /BackupRestore/Restore

- Operation ID: `postBackuprestoreRestore`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1BackupRestore~1Restore/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/BackupApi/postBackuprestoreRestore.html).
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
| `body` | `body` | true | [MBBackup.Api.RestoreOptions](../models.md#model-mbbackup-api-restoreoptions) | not declared | not declared |

### Request body and model references

- `body`: [MBBackup.Api.RestoreOptions](../models.md#model-mbbackup-api-restoreoptions).
  Source pointer: `#/paths/~1BackupRestore~1Restore/post/parameters/0`.

Direct schema references in all request parameters:

- [MBBackup.Api.RestoreOptions](../models.md#model-mbbackup-api-restoreoptions).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1BackupRestore~1Restore/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1Restore/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1Restore/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1Restore/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1Restore/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1Restore/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postbackuprestorerestoredata"></a>

## POST /BackupRestore/RestoreData

- Operation ID: `postBackuprestoreRestoredata`.
- Service candidate: `EXPANSION`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1BackupRestore~1RestoreData/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/BackupApi/postBackuprestoreRestoredata.html).
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
| `body` | `body` | true | [MBBackup.Api.DataRestoreOptions](../models.md#model-mbbackup-api-datarestoreoptions) | not declared | not declared |

### Request body and model references

- `body`: [MBBackup.Api.DataRestoreOptions](../models.md#model-mbbackup-api-datarestoreoptions).
  Source pointer: `#/paths/~1BackupRestore~1RestoreData/post/parameters/0`.

Direct schema references in all request parameters:

- [MBBackup.Api.DataRestoreOptions](../models.md#model-mbbackup-api-datarestoreoptions).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1BackupRestore~1RestoreData/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1RestoreData/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1RestoreData/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1RestoreData/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1RestoreData/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1BackupRestore~1RestoreData/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
