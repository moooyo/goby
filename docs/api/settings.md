# Native settings API

**M5h increment complete and deployed: schema 21/probe 6.**
Schema 21 adds explicit name modes and an independent encoding-width setting
to the three native routes. The [configuration adapter](configuration.md),
native/UI changes, complete [1252-test race run](../development/m5h-full-race.json),
[browser/restarts](../development/m5h-configuration-browser.json),
[deployment](../development/m5h-deployment-evidence.json), and
[main workflow](../development/m5h-deployed-configuration.json) have passed.
See [settings operation](../development/settings.md) for their exact scope.
M4, M5, M6, broader configuration, and full Emby compatibility remain unfinished.

The earlier **M5g, schema 20/probe 6** deployment is historical evidence. Its
[complete race run](../development/m5g-native-full-race.json) passed 1222 top-level
tests across fourteen packages, with zero test skips or race findings, followed
by browser/restarts, [deployment](../development/m5g-deployment-evidence.json),
and the [main workflow](../development/m5g-deployed-settings.json). The
[M5g report](../development/verification-m5g-settings.md) retains that candidate's
identity and first-run fixture-packaging failure separately from M5h.

## Routes and authorization

All three operations require an enabled administrator's native `goby_session`
cookie. An Emby login or application key is not accepted, including when placed
in that cookie. PUT and POST additionally require `X-CSRF-Token` and the normal
same-origin checks. Current account and credential authority are checked again
inside the owned database transaction. Responses use JSON,
`Cache-Control: no-store`, and `Pragma: no-cache`.

| Method and path | Input | Success |
| --- | --- | --- |
| `GET /admin/v1/settings` | No query | `200`, complete settings object |
| `PUT /admin/v1/settings` | Required `{Revision, Overrides}`; optional `ServerNameMode`, `Encoding` | `200`, committed settings object |
| `POST /admin/v1/settings/reset` | `{Revision, Fields}` | `200`, committed settings object |

No query is accepted, including an empty trailing `?`. A mutation body must be
exactly one lossless UTF-8 JSON object of at most **16 KiB**. Supply exactly one
`Content-Type: application/json` header, optionally with `charset=utf-8` and no
other parameters. Empty bodies, trailing JSON, invalid UTF-8, unpaired Unicode
surrogate escapes, unknown or duplicate fields, wrong casing, missing fields,
and incorrect types are rejected. Escaped spellings do not evade duplicate-field
checks. These are native contracts, separate from the
[read-only Emby configuration observations](../research/configuration-reference.md).

## Managed fields and response

`Overrides` retains these five fields. `ServerNameMode` and the independent
`Encoding` object extend the managed state separately. Defaults below are
built-in values; `Defaults` in a response reflects the current deployment's
validated startup configuration.

| Field | JSON value when overridden | Built-in default | Deployment default source |
| --- | --- | --- | --- |
| `ServerName` | String or null consistent with `ServerNameMode`, below | `"Goby"` | `GOBY_SERVER_NAME` |
| `MaxBitrate` | Integer `1`–`1000000000`, bits per second | `20000000` | `GOBY_TRANSCODE_MAX_BITRATE` |
| `MaxWidth` | Integer `1`–`8192`, pixels | `1920` | `GOBY_TRANSCODE_MAX_WIDTH` |
| `MaxHeight` | Integer `1`–`8192`, pixels | `1080` | `GOBY_TRANSCODE_MAX_HEIGHT` |
| `MaxAudioChannels` | Integer `1`–`8` | `8` | `GOBY_TRANSCODE_MAX_AUDIO_CHANNELS` |

Each numeric override accepts explicit `null` to use its deployment default;
zero is invalid for those four fields. Numbers use integer JSON syntax, not
strings, fractions, or exponent notation. `Encoding.TranscodingMaxWidth` is a
separate integer from `0` through `8192`, initially zero. Zero means no extra
width ceiling; it never resets native `MaxWidth`.

| `ServerNameMode` | Raw `Overrides.ServerName` | Effective/public name | `Sources.ServerName` |
| --- | --- | --- | --- |
| `deployment` | null | Deployment default | `"deployment"` |
| `custom` | Valid nonempty string | Exact custom string | `"database"` |
| `empty` | `""` | Startup host name | `"database"` |
| `unset` | null | Startup host name | `"database"` |

A custom name must be nonblank, valid UTF-8, at most 128 bytes, and free of NUL.
Valid text, including surrounding whitespace, is preserved exactly. Empty mode
is an explicit state, not a blank custom name. Null alone does not identify the
name's source. The [compatibility DTO](configuration.md#name-modes-and-width-behavior)
emits an empty string for empty mode and omits the name for unset mode.

The complete response has exactly these nine top-level fields:

| Field | Meaning |
| --- | --- |
| `Revision` | Positive canonical decimal string for the stored managed-state revision |
| `ServerNameMode` | `"deployment"`, `"custom"`, `"empty"`, or `"unset"` |
| `Defaults` | All five non-null startup default values |
| `Overrides` | All five raw fields, interpreted with the name mode |
| `Effective` | All five non-null native values; `MaxWidth` is the native width, not the combined runtime ceiling |
| `Sources` | All five fields, each `"database"` or `"deployment"` |
| `Encoding` | Exact object `{TranscodingMaxWidth: integer}` |
| `UpdatedAt` | UTC RFC 3339 timestamp of the stored settings row |
| `Deployment` | Eight explicitly allowed, read-only startup values listed below |

`Deployment` contains string `HostName`, Boolean `TranscodingEnabled`, strings
`HardwareDecoder` and `HardwareEncoder`, and integer `Threads`, `MaxJobs`,
`MaxUserJobs`, and `MaxSessionJobs`. `HostName` comes from `os.Hostname()` at
startup, independently of `GOBY_SERVER_NAME`; it is not a writable environment
option in this API. The other values retain their startup-configuration source.
Connection strings, tokens, master-key paths, media/cache/web paths, and hardware
device paths remain excluded. No Deployment field can be supplied in an update.
Saved output limits do not enable a disabled transcoder.

## Replace, reset, and conflicts

PUT replaces the **complete five-field override set**. Supply the latest
`Revision` and every override field. Mode-aware clients should also send the
current or selected `ServerNameMode` explicitly. This example replaces the
native overrides and the independent encoding width in one transaction:

```json
{
  "Revision": "1",
  "ServerNameMode": "custom",
  "Overrides": {
    "ServerName": "Living room server",
    "MaxBitrate": 12000000,
    "MaxWidth": 1280,
    "MaxHeight": 720,
    "MaxAudioChannels": null
  },
  "Encoding": {"TranscodingMaxWidth": 960}
}
```

Omitting `ServerNameMode` retains schema-20 input rules: null means deployment,
and a non-null name means custom. Thus `""` without explicit empty mode fails,
and an old-style full update with a null name replaces an unset mode with
deployment. A supplied mode must match its raw value in the table above.
Omitting `Encoding` preserves its current value. Supplying it requires the
sole `TranscodingMaxWidth` field; null, an empty object, and extra fields fail.

Reset changes only the selected state. `Fields` must contain one to six unique
names: `ServerName`, `MaxBitrate`, `MaxWidth`, `MaxHeight`, `MaxAudioChannels`,
or `TranscodingMaxWidth`. Empty, null, duplicate, or unknown selections fail.
`ServerNameMode` and `Encoding.TranscodingMaxWidth` are not reset selectors.
This example restores the deployment name and removes the extra width ceiling:

```json
{"Revision":"2","Fields":["ServerName","TranscodingMaxWidth"]}
```

Name reset sets mode to deployment with a null raw name. Numeric reset clears
the selected native override. Extra-width reset sets only its independent value
to zero. Resetting native `MaxWidth` preserves the extra width, and the reverse
also holds. Select all six explicitly to reset all managed state.

A changed raw override, name mode, or encoding width increments `Revision` once
and updates `UpdatedAt`. Unchanged managed state returns `200` without changing
either value, but still requires the current revision and authority. Saving a
value equal to its deployment
default remains a database override and can therefore be a real change even
when `Effective` looks unchanged. A stale revision returns `409`, including for
an otherwise no-op request; reload before deciding what to save. Input revisions
range from `"1"` through `"9223372036854775806"`, leaving room for a successor.

Changing startup defaults or host name and restarting does not itself change
stored state, revision, or `UpdatedAt`. Null native numeric overrides and
deployment-mode names use the new deployment defaults; empty/unset names use
the new startup host name. Reset uses the current process's defaults. Revision
identifies persisted managed state, not all effective deployment values across
restarts. Successful section writes through the compatibility API
share this revision and can make a previously loaded native revision stale.

## Runtime and errors

The committed server name appears in new public-info/administrator-overview
requests and in subsequently created application-key name snapshots. Existing
credential/client name snapshots are retained. Native output ceilings apply to
new negotiations and conversion plans; they are not HTTP rate limits. A positive
`Encoding.TranscodingMaxWidth` combines with native `Effective.MaxWidth` using
the smaller value. Zero removes only the extra ceiling; native width still
applies. The [Emby 4.9.5.0 execution study](../development/m5h-encoding-width-reference.json)
produced 1280 by 720 output at width 1280 and 3840 by 2160 at zero from the
4K fixture, with eight fully decoded software-encoded frames per output.
This supports the sampled width behavior, not a universal unlimited-width rule,
missing-field reset semantics, or hardware coverage. Existing
registered outputs keep their concrete plans while current authentication,
source, and user-permission checks continue. A progressive PlaybackInfo URL
does not reserve a plan: its later GET/HEAD plans again using that request's
settings, so tightened limits can change or reject the result. An old URL with
exact output choices is not guaranteed to be automatically reduced to fit.

| Status / code | Meaning |
| --- | --- |
| `400 invalid_input` | Invalid body, query, revision, field, value, or reset selection; `Error.Fields` identifies supported validation locations |
| `401` | Missing, expired, revoked, or wrong-audience native credential |
| `403` | Administrator authority, CSRF, or origin check denied |
| `409 revision_conflict` | Reload the committed settings before another mutation |
| `415 unsupported_media_type` | Unsupported or ambiguous Content-Type |
| `503 settings_unavailable` | Settings service/stored state unavailable or a supported operation deadline exceeded |
| `500 internal_error` | Other operation failure; no internal database details are returned |

Errors use the native `Error` and `RequestId` envelope. Validation and database
errors do not echo rejected values or deployment secrets. A lost response does
not prove rollback: the commit may already have succeeded. Reload to reconcile
the saved state before retrying a mutation.
