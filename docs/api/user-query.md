# User Query

`GET /emby/Users/Query` returns an administrator-visible user directory as
`{"Items": [...], "TotalRecordCount": n}`. `Items` always contains an array,
including when the page is empty. The legacy `GET /emby/Users` array response and
the public login-user directory have separate contracts.

The parameter names and result envelope come from the pinned
[UserService contract](services/UserService.md#operation-getusersquery) and
[SDK snapshot](../sources/emby-sdk-openapi.snapshot.json). The snapshot declares
the fields but does not specify defaults, string collation, or accepted
`SortOrder` values. The details below define Goby's implemented contract; they
do not claim a live upstream-server observation.

| Parameter | Behavior when supplied | Default |
| --- | --- | --- |
| `IsHidden` | Exact Boolean match against the supported `Policy.IsHidden` projection. | Both states. |
| `IsDisabled` | Exact Boolean match against the current account's disabled column. | Both states. |
| `NameStartsWithOrGreater` | Inclusive, case-insensitive lexical lower bound on the complete account name. | No name restriction. |
| `SortOrder` | `Ascending` or `Descending`, ignoring value casing. | `Ascending`. |
| `StartIndex` | Nonnegative 32-bit integer, applied after all filters and sorting. | `0`. |
| `Limit` | Nonnegative 32-bit integer, capped at `1000`; `0` returns only the filtered count. | `100`. |

Filter values combine with AND. An explicit `false` is a filter, not an omitted
value. `IsHidden` does not stand for `IsHiddenRemotely` or device-dependent public
visibility. Missing `IsHidden` in a valid legacy policy means `false`; a malformed
stored policy uses the existing conservative projection and therefore counts as
hidden. `IsDisabled` inside stored policy JSON cannot override the account column.

Name comparison uses the same Unicode simple-fold key as account-name uniqueness
and fixed PostgreSQL `C` collation. Ordering uses that key followed by immutable
user ID, with both keys following the requested direction. The lower bound is
unchanged by descending sort. For example, a bound of `Bravo` includes `Bravo`,
`bravo later`, and `Charlie`; it is not a substring or prefix-only search. `%` and
`_` are literal characters. Query values retain their leading and trailing spaces.
An empty name bound applies no restriction. Name bounds must be valid UTF-8,
contain at most 128 characters and 512 bytes, and contain no control characters.

Business parameter names are case-insensitive. Duplicate names, including case
aliases, malformed values, and unsupported business parameters return `400`.
Booleans accept only `true` and `false`, ignoring casing. Numeric input retains
compatible leading-zero and encoded-plus forms; fractions, negative values, and
overflow are rejected. Supported Emby credential and client metadata query
parameters pass through the shared compatibility parser and retain its conflict
checks. Input values are not echoed in error responses.

The response requires an active Emby administrator login or a complete, active
application-key principal. Ordinary users receive `403`; missing, expired,
disabled, or revoked credentials are rejected. The identity operation checks the
current account, credential, and application client in its transaction, retains
the caller's authorization locks, and checks authority again before returning.
A stale caller snapshot cannot retain access after demotion or revocation.

The filtered total and page come from one directory SELECT. Hidden-state
projection runs before counting or taking the page, and a start index past the
end keeps the filtered total while returning an empty array. Only the requested
page is retained in memory. Counts still inspect the matching directory rows so
they remain exact when malformed policy requires conservative projection.

Results use the existing safe user DTO and avatar projection, with
`Cache-Control: no-store`. Password hashes, application tokens, arbitrary stored
policy/configuration extensions, and another user's profile PIN are not added
to this endpoint.

Focused coverage lives in `internal/server/user_query_test.go`,
`internal/server/user_query_integration_test.go`, and
`internal/identity/user_query_test.go`. The integration cases require Linux and
an isolated PostgreSQL schema through the existing test fixtures.
