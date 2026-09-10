# Native settings API

**M5g native settings increment accepted.** This documents the current native source
contract. The [complete native race suite](../development/m5g-native-full-race.json)
passed 1222 top-level tests across fourteen tested packages, with zero skipped
tests or race findings. Targeted checks and the isolated browser/restart workflow
also passed, followed by [deployment](../development/m5g-deployment-evidence.json)
and the [main-service workflow](../development/m5g-deployed-settings.json).
The live service uses schema 20/probe 6. The Emby ConfigurationService adapter
remains unimplemented; this acceptance covers the three native settings routes.
See [settings persistence and operation](../development/settings.md) for the
schema-20 change and runtime behavior, and the
[verification report](../development/verification-m5g-settings.md) for candidate
identity, the retained first-run failure, and acceptance boundaries.

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
| `PUT /admin/v1/settings` | `{Revision, Overrides}` | `200`, committed settings object |
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

Only these five fields are writable. Defaults below are built-in values;
`Defaults` in an actual response reflects the current deployment's validated
startup configuration.

| Field | JSON value when overridden | Built-in default | Deployment default source |
| --- | --- | --- | --- |
| `ServerName` | Nonblank UTF-8 string, at most 128 bytes, without NUL | `"Goby"` | `GOBY_SERVER_NAME` |
| `MaxBitrate` | Integer `1`–`1000000000`, bits per second | `20000000` | `GOBY_TRANSCODE_MAX_BITRATE` |
| `MaxWidth` | Integer `1`–`8192`, pixels | `1920` | `GOBY_TRANSCODE_MAX_WIDTH` |
| `MaxHeight` | Integer `1`–`8192`, pixels | `1080` | `GOBY_TRANSCODE_MAX_HEIGHT` |
| `MaxAudioChannels` | Integer `1`–`8` | `8` | `GOBY_TRANSCODE_MAX_AUDIO_CHANNELS` |

Every override also accepts explicit `null`, meaning use the current deployment
default. Zero is not a reset value. Numbers are JSON integers, not strings,
fractions, or exponent notation. Valid server-name text is preserved exactly,
including surrounding whitespace; an entirely whitespace name is rejected.
The name policy rejects NUL without claiming to reject every control character.

The complete response has exactly these seven top-level fields:

| Field | Meaning |
| --- | --- |
| `Revision` | Positive canonical decimal string for the stored override revision |
| `Defaults` | All five non-null startup default values |
| `Overrides` | All five fields, each an explicit value or null |
| `Effective` | All five non-null values after applying overrides |
| `Sources` | All five fields, each `"database"` or `"deployment"` |
| `UpdatedAt` | UTC RFC 3339 timestamp of the stored settings row |
| `Deployment` | Seven explicitly allowed, read-only startup settings listed below |

`Deployment` contains `TranscodingEnabled` (Boolean), `HardwareDecoder` and
`HardwareEncoder` (strings), and integer `Threads`, `MaxJobs`, `MaxUserJobs`,
and `MaxSessionJobs`. It excludes connection strings, tokens, master-key paths,
media/cache/web paths, and hardware device paths. Those seven fields cannot be
included in an update. They require deployment configuration and a restart to
change; saved output limits do not enable a disabled transcoder.

## Replace, reset, and conflicts

PUT replaces the **complete five-field override set**. It is not a partial
update. Use the `Revision` from the latest response and include every field:

```json
{
  "Revision": "1",
  "Overrides": {
    "ServerName": "Living room server",
    "MaxBitrate": 12000000,
    "MaxWidth": 1280,
    "MaxHeight": 720,
    "MaxAudioChannels": null
  }
}
```

Reset clears only the selected stored overrides. `Fields` must contain one to
five unique supported field names; an empty, null, duplicate, or unknown
selection is invalid. This example resets two fields; select all five
explicitly to reset everything:

```json
{"Revision":"2","Fields":["ServerName","MaxWidth"]}
```

A changed override set increments `Revision` once and updates `UpdatedAt`.
An unchanged set returns `200` without changing either value, but still requires
the current revision and authority. Saving a value equal to its deployment
default remains a database override and can therefore be a real change even
when `Effective` looks unchanged. A stale revision returns `409`, including for
an otherwise no-op request; reload before deciding what to save. Input revisions
range from `"1"` through `"9223372036854775806"`, leaving room for a successor.

Changing startup defaults and restarting does not change stored overrides,
revision, or `UpdatedAt`. Fields with null overrides use the new deployment
defaults. Reset after that restart uses those new defaults, not the values from
the original installation. Thus `Revision` identifies stored overrides, not
the complete deployment configuration across restarts.

## Runtime and errors

The committed server name appears in new public-info/administrator-overview
requests and in subsequently created application-key name snapshots. Existing
credential/client name snapshots are retained. Four output ceilings apply to
new negotiations and conversion plans; they are not HTTP rate limits. Existing
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
