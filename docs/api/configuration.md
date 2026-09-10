# Configuration compatibility API

**M5h increment complete and deployed: schema 21/probe 6.** The complete
[remote race run](../development/m5h-full-race.json),
[browser/restarts](../development/m5h-configuration-browser.json), bounded
[reference execution study](../development/m5h-encoding-width-reference.json),
[deployment](../development/m5h-deployment-evidence.json), and
[main workflow](../development/m5h-deployed-configuration.json) have passed.
The earlier M5g/schema-20 deployment remains historical evidence. M4, M5, M6,
broader configuration and full Emby compatibility remain unfinished. See
[implementation and remaining scope](../development/configuration-compatibility.md).

## Routes and authority

The adapter registers five operations. Recognized route literals accept the
existing root aliases and case variants, including `/System/Configuration` and
`/EMBY/system/configuration/partial`. Named section matching is case-insensitive.
The namespace adapter preserves segment boundaries; section names never become
filesystem paths.

| Method and route | Allowed body | Success |
| --- | --- | --- |
| `GET /emby/System/Configuration` | None | `200`, backed server fields for an administrator/key; exactly `{}` for an ordinary viewer |
| `GET /emby/System/Configuration/{key}` | None | `200 {TranscodingMaxWidth}` for `encoding`; administrator/key required |
| `POST /emby/System/Configuration` | Object containing only optional `ServerName`, `IsStartupWizardCompleted` | Empty `204`; replaces the supported server-name section |
| `POST /emby/System/Configuration/Partial` | Same supported fields | Empty `204`; changes the name only when present |
| `POST /emby/System/Configuration/{key}` | Object containing only optional `TranscodingMaxWidth` for `encoding` | Empty `204`; replaces the supported encoding section |

These use Emby token authority: an ordinary administrator login or a complete
application-key principal can manage configuration. A native administrator
cookie cannot substitute for that token. Existing token carriers include
`X-Emby-Token` and `api_key`. These compatibility POSTs do not use native CSRF
tokens; the native settings routes retain their separate cookie/CSRF boundary.

Missing, invalid, expired, or revoked credentials produce `401` before the
configuration body or named section is interpreted. A valid ordinary viewer
can read total configuration, but named GETs return `403` with
`User {Name} does not have access to ReadServerConfiguration feature.` All POSTs
return viewer `403` with `ManageServer` in that message, before body/section
validation. Current authority is rechecked transactionally; an expired viewer
is not converted into a successful empty response.

Successful GETs use `Content-Type: application/json`, a measured Content-Length,
and no trailing newline. The viewer's GET body is exactly two bytes, `{}`.
HEAD follows GET authentication and representation headers without a body.
All five routes set `Cache-Control: no-store` and `Pragma: no-cache`.

## Closed projections

Administrator/key total GET exposes only `IsStartupWizardCompleted` and, when
configured, `ServerName`. The initialization Boolean is read from the actual
setup-completed marker or surviving users; it is not a writable placeholder.
An unset name is omitted, never emitted as null. No native revision, defaults,
override map, or deployment structure is added to this compatibility DTO.

The only implemented named section is `encoding`, whose complete projection is
`{"TranscodingMaxWidth":0}` at its initial value. This is a JSON integer from
`0` through `8192`. Native `MaxWidth` is a different setting and is not exposed
under this field name.

For authorized named reads/writes, `devices` and `dlna` return `501` with
`This configuration section is not implemented.` An unknown section returns
`404` with `Configuration not found.` Viewer denial and invalid-token rejection
take precedence over these section errors. The reference's unknown-key `500`
is recorded separately and is not copied into Goby's registry behavior.

Fields such as case-sensitive-ID policy, ports, metadata options, hardware
flags, threads, CRF, and tone mapping are omitted until their semantics and
consumers are implemented. A write containing any unsupported field is rejected
as a whole, even if its value resembles a current deployment value. The adapter
does not accept an arbitrary upstream object and pretend to save it.

## Input and mutation semantics

A body is exactly one lossless UTF-8 JSON object, at most **32 KiB**. Full and
Partial POST require `application/json`; named encoding POST also accepts
`application/octet-stream`, but its bytes must still be JSON. Supply one
Content-Type header, optionally with only `charset=utf-8`. XML is unsupported.
Invalid UTF-8, unpaired surrogate escapes, trailing JSON, arrays/scalars,
ambiguous Content-Type, and wrong value types fail.

Supported field names are case-insensitive on this surface. Duplicate decoded
names, escaped aliases, and aliases differing only by case are rejected.
For example, `ServerName` plus `servername` in one object is invalid. Queries
are limited to 4096 raw bytes and permit only one exact lowercase `api_key`
parameter; configuration values and unknown query parameters are rejected.

| Request | Server-name effect | Other settings |
| --- | --- | --- |
| Full POST with absent `ServerName` | Store `unset` | Preserve all native numeric overrides and compatibility encoding width |
| Partial POST with absent `ServerName` | Preserve current mode and raw name | Preserve all other settings |
| Full/Partial with `ServerName: null` | Store `unset` | Preserve all other settings |
| Full/Partial with `ServerName: ""` | Store `empty` | Preserve all other settings |
| Full/Partial with a valid nonempty name | Store `custom` with the exact text | Preserve all other settings |
| Encoding POST with absent `TranscodingMaxWidth` | Preserve name | Set the independent compatibility width to `0`; preserve all native numeric overrides |
| Encoding POST with an integer width | Preserve name | Replace only the compatibility width |

A custom name must contain non-whitespace text, be valid UTF-8, contain no NUL,
and use at most 128 bytes. Valid surrounding whitespace is retained. Width
accepts integer JSON syntax only; null, strings, fractions, exponent notation,
negative values, and values above 8192 fail. `{}` is a valid object with the
section-specific omission behavior above, not an empty HTTP body.

If `IsStartupWizardCompleted` is present, it must be a non-null Boolean equal
to the actual initialization state read inside the mutation transaction. A
mismatch rejects the entire request, including any otherwise valid name.
It cannot reopen setup or silently change an unsupported startup flag.

Compatibility POST carries no client revision. It applies the selected section
atomically to the latest locked record, preserving unrelated native fields.
A real change advances the shared native revision once; a persisted no-op
leaves revision and UpdatedAt unchanged. A subsequent native write using an
older revision receives the existing `409` conflict.

Not every GET-clone/POST is a database no-op. In `deployment` mode, total GET
includes the deployment name. Explicitly posting that string establishes a
`custom` override, even though the displayed name remains the same. Full POST
with the name omitted means `unset`, not restore the deployment default.

## Name modes and width behavior

The same persisted state supports both native and compatibility clients:

| Mode | Native raw `Overrides.ServerName` | Compatibility total GET | Effective/public name |
| --- | --- | --- | --- |
| `deployment` | null | `ServerName` is the deployment default | Deployment default |
| `custom` | Exact nonempty string | Same string | Same string |
| `empty` | `""` | `ServerName: ""` | Actual host name captured at startup |
| `unset` | null | `ServerName` omitted | Actual host name captured at startup |

The host name is distinct from `GOBY_SERVER_NAME`. Empty/unset do not discard
their stored distinction merely because their current public names match.
The native API/UI extension exposes that distinction as described in the
[development contract](../development/configuration-compatibility.md#native-api-and-dashboard-extension).

The new planning width is the smaller of native effective `MaxWidth` and a
positive compatibility `TranscodingMaxWidth`. Zero removes only this extra
ceiling; it never resets or relaxes native `MaxWidth`. Other native ceilings,
profiles, source facts, and permissions still apply. Existing registered plans
remain concrete; progressive PlaybackInfo URLs are planned again on a later
GET/HEAD. The [Emby 4.9.5.0 study](../development/m5h-encoding-width-reference.json)
now records actual 4K-fixture execution: configured width `1280` produced
1280 by 720 H.264/AAC, while `0` produced 3840 by 2160. Each output has eight
fully decoded frames and proof of software encoding. This supports no extra
width clamp in that zero-valued case; it does not establish a universal
unlimited-width rule, missing-field reset semantics, or hardware behavior.

## Errors and evidence boundary

Configuration parser/domain failures return plain-text `400`:
`Supply valid supported configuration fields.` Unsupported media types return
`415`; unsupported known sections return `501`; unknown sections return `404`.
Unavailable or inconsistent stored settings return `503`. Other operation
failures return `500` without database details or rejected field/value echoing.

The [M5g read study](../research/configuration-reference.md) and
[fresh mutation study](../research/configuration-mutation-reference.md) are
completed reference evidence. The latter records an upstream mixed Partial
failure whose `500` still leaves a name change visible in the same process.
Goby deliberately rejects invalid mixed requests atomically. The three upstream
key POST controls were complete baseline no-ops, not proof of changed-value key
writes. Those earlier studies remain bounded historical observations. Current
M5h acceptance separately verifies this closed adapter and its shared native
state. The main workflow preserved every old row in the other 27 tables,
restored original settings through native CAS, and retained two new revoked
login sessions and one new ordinary device. It performed no media or planning
request, scan, task execution, key creation, or main restart. The wider consumer
and compatibility scope remains open.
