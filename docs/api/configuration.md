# Configuration compatibility API

The schema49 [sorting and dynamic artwork increment](sorting-library-options.md)
adds persisted `SortRemoveWords` with an immediate atomic catalog rebuild and
the real Audio embedded-image selector. Its source is complete; consolidated
phase 1 verification remains pending.

The selected Phase 4 port, CPU H.264 CRF and tone-mapping extensions below are
implemented in source and await consolidated Phase 4 verification. Earlier
acceptance records do not establish acceptance of these extensions.

**The selected phase 3 configuration contract is verified and closed within its
recorded boundaries.** The current server fields, named management sections,
consumers and restart behavior below are covered by the
[phase 3 record](../development/amd-media-phase3-20260919.md), not by a claim of
full upstream configuration parity.

**Historical M5h increment complete and deployed: schema 21/probe 6.** The complete
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
| `GET /emby/System/Configuration/{key}` | None | Administrator/key: `encoding`, `subtitles`, or the Goby `tasks` section |
| `POST /emby/System/Configuration` | Supported server fields below | Empty `204`; replaces supported name, metadata and desired-port settings; preserves other sections |
| `POST /emby/System/Configuration/Partial` | Supported server fields plus H264Crf and the two tone-mapping flags | Empty `204`; applies only supplied writable fields |
| `POST /emby/System/Configuration/{key}` | Supported fields of exactly one named section | Empty `204`; replaces that section using its documented omission rules |

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

Administrator/key total GET exposes `IsStartupWizardCompleted`,
`PreferredMetadataLanguage`, `MetadataCountryCode`, `EnableInternetProviders`,
`SortRemoveWords` and `HttpServerPortNumber`, plus `ServerName` when configured. The port is the
resolved desired HTTP configuration, not an assertion that a new socket is
already active. Authenticated SystemInfo separately reports the actual running
port and pending restart. The initialization Boolean is read from the actual
setup-completed marker or surviving users; it is not a writable placeholder.
An unset name is omitted, never emitted as null. No native revision, defaults,
override map, or deployment structure is added to this compatibility DTO.

The `encoding` section contains `TranscodingMaxWidth`,
`EnableSoftwareToneMapping` and `EnableHardwareToneMapping`. It also contains
`H264Crf` only when the effective CPU H.264 rate-control mode is `capped_crf`.
TranscodingMaxWidth is an integer `0..8192`; native MaxWidth is independent.
H264Crf is an integer `18..35`. The two non-null booleans gate the actual software
and Vulkan filter backends, respectively; the latter is not a universal hardware
encoder or device switch. Native controls expose the complete CPU preset,
rate-control, HEVC, thread and authorized hardware choices.

| Compatibility field | Writable surface | Complete-object omission | Partial omission |
| --- | --- | --- | --- |
| `HttpServerPortNumber` | Full server and Partial | Clear only the port override to the deployment default; preserve native BindHost | Preserve |
| `SortRemoveWords` | Full server and Partial | Reset to `[]`; an explicit array replaces all rules | Preserve |
| `H264Crf` | Named encoding and Partial | If currently capped_crf, return to bitrate with CRF 23 while preserving the preset; if already bitrate, preserve the entire quality group including its inactive CRF | Preserve |
| `EnableSoftwareToneMapping` | Named encoding and Partial | Clear the override to the deployment software-tone default | Preserve |
| `EnableHardwareToneMapping` | Named encoding and Partial | Clear the override to the deployment Vulkan-tone default | Preserve |

An explicit H264Crf selects capped_crf and preserves the currently resolved CPU
H.264 preset. It never widens the source, client, server or user bitrate ceilings.
An explicit port must be an integer `1..65535`; deployment-owned port zero may
appear in GET before a private ephemeral reservation, but is not writable.
Null, fractions and exponent notation are invalid for these compatibility
numbers, and null is invalid for the two booleans. Native group-null/reset is
the explicit reset interface. Full server objects reject the encoding fields;
named encoding objects reject the port.

The current named management sections share the native persisted Management
object. They are closed projections, not arbitrary upstream configuration bags:

| Section | Fields and defaults | Validation |
| --- | --- | --- |
| `subtitles` | `DownloadLanguages: ["en"]`, `DownloadMovieSubtitles: true`, `DownloadEpisodeSubtitles: true` | At most eight distinct language codes; two lowercase letters with an optional uppercase two-letter region; actual JSON booleans |
| `tasks` (Goby extension) | `MaxConcurrent: 2`, `CacheRetentionDays: 30`, `CacheMaxEntries: 10000` | Integers `1..16`, `1..3650`, and `1..1000000`, respectively |

Total configuration accepts a lowercase two- or three-letter metadata language
with an optional uppercase two-letter region, an uppercase two-letter country,
and a non-null internet-provider Boolean.
Full POST resets omitted metadata fields to `en`, `US`, and `false`; Partial
POST preserves omitted fields. Named POST replaces its selected section;
`{}` restores that section's defaults. Nulls, mixed-section fields, unknown
fields and duplicate aliases reject the whole mutation. Native reset uses the
same defaults. No deployment path, provider credential or process budget is
exposed by these fields.

For authorized named reads/writes, `devices` and `dlna` return `501` with
`This configuration section is not implemented.` An unknown section returns
`404` with `Configuration not found.` Viewer denial and invalid-token rejection
take precedence over these section errors. The reference's unknown-key `500`
is recorded separately and is not copied into Goby's registry behavior.

Fields such as case-sensitive-ID policy, undeclared metadata options,
`EnableHardwareEncoding`, `EncodingThreadCount`, arbitrary codec parameters,
and device paths remain unsupported. A write containing any unsupported field is rejected
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
are limited to 4096 raw bytes and permit the declared
[compatibility transport carriers](compatibility-transport.md); configuration
values and unknown query parameters are rejected.

| Request | Server-name effect | Other settings |
| --- | --- | --- |
| Full POST with absent `ServerName` | Store `unset` | Apply metadata and port replacement/default rules; preserve numeric overrides, encoding, subtitles and task settings |
| Partial POST with absent `ServerName` | Preserve current mode and raw name | Apply only supplied metadata, port and supported runtime encoding fields; preserve unrelated settings |
| Full/Partial with `ServerName: null` | Store `unset` | Apply the selected full/partial metadata rules |
| Full/Partial with `ServerName: ""` | Store `empty` | Apply the selected full/partial metadata rules |
| Full/Partial with a valid nonempty name | Store `custom` with the exact text | Apply the selected full/partial metadata rules |
| Encoding POST with absent `TranscodingMaxWidth` | Preserve name | Set the independent compatibility width to `0`; preserve all native numeric overrides |
| Encoding POST with an integer width | Preserve name | Replace the compatibility width and apply the public CRF/tone-map omission rules above |

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

After commit, settings are published through the shared runtime snapshot and
survive reload. Task concurrency is read before dispatch; cache retention/count
and subtitle-download options are captured when their task child starts.
Already running work retains its captured settings. Internet-provider execution
requires both the deployment provider gate and the runtime setting; enabling
this HTTP field cannot override a disabled deployment. A changed configuration
also produces a durable `ConfigurationChanged` signal; rejected writes, rolled
back transactions and no-ops do not. See the [task contract](tasks.md).

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
