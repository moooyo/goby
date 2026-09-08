# UserService

User authentication, accounts, configuration, and policy.

**21 HTTP operations**. Service candidate: `CORE-CANDIDATE`. Status: `planned-unimplemented`.
This candidate does not place every operation in the MVP; see [implementation scope](../implementation-scope.md).

[Catalog](../catalog.md) | [Models](../models.md) | [Pinned source](../../sources/emby-sdk-openapi.snapshot.json)

The source authentication text is preserved verbatim below and may be inconsistent.
It is not the project authorization policy. Required/default/enum columns report source declarations.
Missing schema declarations are gaps to investigate, not proof that a value or body is absent.
Official reference URLs are derived links; these individual pages have not been checked.

## Operations

- [`GET /Users/{Id}`](#operation-getusersbyid)
- [`POST /Users/{Id}`](#operation-postusersbyid)
- [`DELETE /Users/{Id}`](#operation-deleteusersbyid)
- [`POST /Users/{Id}/Authenticate`](#operation-postusersbyidauthenticate)
- [`POST /Users/{Id}/Configuration`](#operation-postusersbyidconfiguration)
- [`POST /Users/{Id}/Configuration/Partial`](#operation-postusersbyidconfigurationpartial)
- [`POST /Users/{Id}/Delete`](#operation-postusersbyiddelete)
- [`POST /Users/{Id}/Password`](#operation-postusersbyidpassword)
- [`POST /Users/{Id}/Policy`](#operation-postusersbyidpolicy)
- [`DELETE /Users/{Id}/TrackSelections/{TrackType}`](#operation-deleteusersbyidtrackselectionsbytracktype)
- [`POST /Users/{Id}/TrackSelections/{TrackType}/Delete`](#operation-postusersbyidtrackselectionsbytracktypedelete)
- [`GET /Users/{UserId}/TypedSettings/{Key}`](#operation-getusersbyuseridtypedsettingsbykey)
- [`POST /Users/{UserId}/TypedSettings/{Key}`](#operation-postusersbyuseridtypedsettingsbykey)
- [`POST /Users/AuthenticateByName`](#operation-postusersauthenticatebyname)
- [`POST /Users/ForgotPassword`](#operation-postusersforgotpassword)
- [`POST /Users/ForgotPassword/Pin`](#operation-postusersforgotpasswordpin)
- [`GET /Users/ItemAccess`](#operation-getusersitemaccess)
- [`POST /Users/New`](#operation-postusersnew)
- [`GET /Users/Prefixes`](#operation-getusersprefixes)
- [`GET /Users/Public`](#operation-getuserspublic)
- [`GET /Users/Query`](#operation-getusersquery)

<a id="operation-getusersbyid"></a>

## GET /Users/{Id}

- Operation ID: `getUsersById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/getUsersById.html).
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
| `200` | [UserDto](../models.md#model-userdto) | Operation successful. Returning a UserDto object. | `#/paths/~1Users~1{Id}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyid"></a>

## POST /Users/{Id}

- Operation ID: `postUsersById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersById.html).
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
| `body` | `body` | true | [UserDto](../models.md#model-userdto) | not declared | not declared |

### Request body and model references

- `body`: [UserDto](../models.md#model-userdto).
  Source pointer: `#/paths/~1Users~1{Id}/post/parameters/1`.

Direct schema references in all request parameters:

- [UserDto](../models.md#model-userdto).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteusersbyid"></a>

## DELETE /Users/{Id}

- Operation ID: `deleteUsersById`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/deleteUsersById.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidauthenticate"></a>

## POST /Users/{Id}/Authenticate

- Operation ID: `postUsersByIdAuthenticate`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Authenticate/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdAuthenticate.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [AuthenticateUser](../models.md#model-authenticateuser) | not declared | not declared |

### Request body and model references

- `body`: [AuthenticateUser](../models.md#model-authenticateuser).
  Source pointer: `#/paths/~1Users~1{Id}~1Authenticate/post/parameters/1`.

Direct schema references in all request parameters:

- [AuthenticateUser](../models.md#model-authenticateuser).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [Authentication.AuthenticationResult](../models.md#model-authentication-authenticationresult) | Operation successful. Returning a AuthenticationResult object. | `#/paths/~1Users~1{Id}~1Authenticate/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Authenticate/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Authenticate/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Authenticate/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Authenticate/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Authenticate/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidconfiguration"></a>

## POST /Users/{Id}/Configuration

- Operation ID: `postUsersByIdConfiguration`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Configuration/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdConfiguration.html).
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
| `body` | `body` | true | [UserConfiguration](../models.md#model-userconfiguration) | not declared | not declared |

### Request body and model references

- `body`: [UserConfiguration](../models.md#model-userconfiguration).
  Source pointer: `#/paths/~1Users~1{Id}~1Configuration/post/parameters/1`.

Direct schema references in all request parameters:

- [UserConfiguration](../models.md#model-userconfiguration).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Configuration/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidconfigurationpartial"></a>

## POST /Users/{Id}/Configuration/Partial

- Operation ID: `postUsersByIdConfigurationPartial`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Configuration~1Partial/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdConfigurationPartial.html).
- Consumes: `application/octet-stream`.
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
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/parameters/1`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Configuration~1Partial/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyiddelete"></a>

## POST /Users/{Id}/Delete

- Operation ID: `postUsersByIdDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdDelete.html).
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
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidpassword"></a>

## POST /Users/{Id}/Password

- Operation ID: `postUsersByIdPassword`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Password/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdPassword.html).
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
| `body` | `body` | true | [UpdateUserPassword](../models.md#model-updateuserpassword) | not declared | not declared |

### Request body and model references

- `body`: [UpdateUserPassword](../models.md#model-updateuserpassword).
  Source pointer: `#/paths/~1Users~1{Id}~1Password/post/parameters/1`.

Direct schema references in all request parameters:

- [UpdateUserPassword](../models.md#model-updateuserpassword).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Password/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Password/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Password/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Password/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Password/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Password/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidpolicy"></a>

## POST /Users/{Id}/Policy

- Operation ID: `postUsersByIdPolicy`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1Policy/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdPolicy.html).
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
| `Id` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | [UserPolicy](../models.md#model-userpolicy) | not declared | not declared |

### Request body and model references

- `body`: [UserPolicy](../models.md#model-userpolicy).
  Source pointer: `#/paths/~1Users~1{Id}~1Policy/post/parameters/1`.

Direct schema references in all request parameters:

- [UserPolicy](../models.md#model-userpolicy).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1Policy/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Policy/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Policy/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Policy/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Policy/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1Policy/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-deleteusersbyidtrackselectionsbytracktype"></a>

## DELETE /Users/{Id}/TrackSelections/{TrackType}

- Operation ID: `deleteUsersByIdTrackselectionsByTracktype`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/deleteUsersByIdTrackselectionsByTracktype.html).
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
| `TrackType` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}/delete/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyidtrackselectionsbytracktypedelete"></a>

## POST /Users/{Id}/TrackSelections/{TrackType}/Delete

- Operation ID: `postUsersByIdTrackselectionsByTracktypeDelete`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByIdTrackselectionsByTracktypeDelete.html).
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
| `TrackType` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{Id}~1TrackSelections~1{TrackType}~1Delete/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getusersbyuseridtypedsettingsbykey"></a>

## GET /Users/{UserId}/TypedSettings/{Key}

- Operation ID: `getUsersByUseridTypedsettingsByKey`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/getUsersByUseridTypedsettingsByKey.html).
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
| `Key` | `path` | true | string | not declared | not declared |
| `UserId` | `path` | true | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Response content unknown. | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersbyuseridtypedsettingsbykey"></a>

## POST /Users/{UserId}/TypedSettings/{Key}

- Operation ID: `postUsersByUseridTypedsettingsByKey`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersByUseridTypedsettingsByKey.html).
- Consumes: `application/octet-stream`.
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
| `Key` | `path` | true | string | not declared | not declared |
| `body` | `body` | true | string (binary) | not declared | not declared |

### Request body and model references

- `body`: string (binary).
  Source pointer: `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/parameters/2`.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | not declared | Operation successful. Empty response. | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1{UserId}~1TypedSettings~1{Key}/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersauthenticatebyname"></a>

## POST /Users/AuthenticateByName

- Operation ID: `postUsersAuthenticatebyname`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1AuthenticateByName/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersAuthenticatebyname.html).
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
| `X-Emby-Authorization` | `header` | true | string | not declared | not declared |
| `body` | `body` | true | [AuthenticateUserByName](../models.md#model-authenticateuserbyname) | not declared | not declared |

### Request body and model references

- `body`: [AuthenticateUserByName](../models.md#model-authenticateuserbyname).
  Source pointer: `#/paths/~1Users~1AuthenticateByName/post/parameters/1`.

Direct schema references in all request parameters:

- [AuthenticateUserByName](../models.md#model-authenticateuserbyname).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [Authentication.AuthenticationResult](../models.md#model-authentication-authenticationresult) | Operation successful. Returning a AuthenticationResult object. | `#/paths/~1Users~1AuthenticateByName/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1AuthenticateByName/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1AuthenticateByName/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1AuthenticateByName/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1AuthenticateByName/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1AuthenticateByName/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersforgotpassword"></a>

## POST /Users/ForgotPassword

- Operation ID: `postUsersForgotpassword`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1ForgotPassword/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersForgotpassword.html).
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
| `body` | `body` | true | [ForgotPassword](../models.md#model-forgotpassword) | not declared | not declared |

### Request body and model references

- `body`: [ForgotPassword](../models.md#model-forgotpassword).
  Source pointer: `#/paths/~1Users~1ForgotPassword/post/parameters/0`.

Direct schema references in all request parameters:

- [ForgotPassword](../models.md#model-forgotpassword).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [ForgotPasswordResult](../models.md#model-forgotpasswordresult) | Operation successful. Returning a ForgotPasswordResult object. | `#/paths/~1Users~1ForgotPassword/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersforgotpasswordpin"></a>

## POST /Users/ForgotPassword/Pin

- Operation ID: `postUsersForgotpasswordPin`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1ForgotPassword~1Pin/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersForgotpasswordPin.html).
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
| `body` | `body` | true | [ForgotPasswordPin](../models.md#model-forgotpasswordpin) | not declared | not declared |

### Request body and model references

- `body`: [ForgotPasswordPin](../models.md#model-forgotpasswordpin).
  Source pointer: `#/paths/~1Users~1ForgotPassword~1Pin/post/parameters/0`.

Direct schema references in all request parameters:

- [ForgotPasswordPin](../models.md#model-forgotpasswordpin).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [PinRedeemResult](../models.md#model-pinredeemresult) | Operation successful. Returning a PinRedeemResult object. | `#/paths/~1Users~1ForgotPassword~1Pin/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword~1Pin/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword~1Pin/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword~1Pin/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword~1Pin/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ForgotPassword~1Pin/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getusersitemaccess"></a>

## GET /Users/ItemAccess

- Operation ID: `getUsersItemaccess`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1ItemAccess/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/getUsersItemaccess.html).
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
| `IsHidden` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsDisabled` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserDto](../models.md#model-queryresult_userdto) | Operation successful. Returning a QueryResult<UserDto> object. | `#/paths/~1Users~1ItemAccess/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ItemAccess/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ItemAccess/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ItemAccess/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ItemAccess/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1ItemAccess/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-postusersnew"></a>

## POST /Users/New

- Operation ID: `postUsersNew`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1New/post`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersNew.html).
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
| `body` | `body` | true | [CreateUserByName](../models.md#model-createuserbyname) | not declared | not declared |

### Request body and model references

- `body`: [CreateUserByName](../models.md#model-createuserbyname).
  Source pointer: `#/paths/~1Users~1New/post/parameters/0`.

Direct schema references in all request parameters:

- [CreateUserByName](../models.md#model-createuserbyname).

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [UserDto](../models.md#model-userdto) | Operation successful. Returning a UserDto object. | `#/paths/~1Users~1New/post/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1New/post/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1New/post/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1New/post/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1New/post/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1New/post/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getusersprefixes"></a>

## GET /Users/Prefixes

- Operation ID: `getUsersPrefixes`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1Prefixes/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/getUsersPrefixes.html).
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
| `IsHidden` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsDisabled` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | array&lt;[NameIdPair](../models.md#model-nameidpair)&gt; | Operation successful. Returning a NameIdPair[] object. | `#/paths/~1Users~1Prefixes/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Prefixes/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Prefixes/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Prefixes/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Prefixes/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Prefixes/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getuserspublic"></a>

## GET /Users/Public

- Operation ID: `getUsersPublic`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1Public/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/getUsersPublic.html).
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
| `200` | array&lt;[UserDto](../models.md#model-userdto)&gt; | Operation successful. Returning a UserDto[] object. | `#/paths/~1Users~1Public/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Public/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Public/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Public/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Public/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Public/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.

<a id="operation-getusersquery"></a>

## GET /Users/Query

- Operation ID: `getUsersQuery`.
- Service candidate: `CORE-CANDIDATE`; status: `planned-unimplemented`.
- Source pointer: `#/paths/~1Users~1Query/get`.
- [Derived official reference](https://dev.emby.media/reference/RestAPI/UserService/getUsersQuery.html).
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
| `IsHidden` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `IsDisabled` | `query` | omitted (Swagger default: false) | boolean | not declared | not declared |
| `StartIndex` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `Limit` | `query` | omitted (Swagger default: false) | integer (int32) | not declared | not declared |
| `NameStartsWithOrGreater` | `query` | omitted (Swagger default: false) | string | not declared | not declared |
| `SortOrder` | `query` | omitted (Swagger default: false) | string | not declared | not declared |

### Request body and model references

No `in: body` parameter is declared. Query, header, or form parameters may still carry input.

### Responses

| Status | Schema or reusable response reference | Source description | Source pointer |
| --- | --- | --- | --- |
| `200` | [QueryResult_UserDto](../models.md#model-queryresult_userdto) | Operation successful. Returning a QueryResult<UserDto> object. | `#/paths/~1Users~1Query/get/responses/200` |
| `400` | [#/responses/400](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Query/get/responses/400` |
| `401` | [#/responses/401](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Query/get/responses/401` |
| `403` | [#/responses/403](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Query/get/responses/403` |
| `404` | [#/responses/404](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Query/get/responses/404` |
| `500` | [#/responses/500](../../sources/emby-sdk-openapi.snapshot.json) | not declared | `#/paths/~1Users~1Query/get/responses/500` |

Reusable error responses are defined under `#/responses` in the source; their error body schemas
are not declared. A listed HTTP status is a source claim, not observed server behavior.
