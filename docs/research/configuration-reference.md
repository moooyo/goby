# Configuration read-only reference

Research date: 2026-09-10 (Asia/Shanghai).
Status: **bounded reference read study complete**. Later write observations and
native product acceptance are linked separately below.

This study reads configuration from official Emby Server `4.9.5.0`. An
administrator receives the sampled configuration objects. The ordinary viewer's
total-configuration request also returns `200`, but its complete body is **`{}`**;
the viewer does not receive the administrator's 60 fields. Named configuration
reads require the sampled administrator authority. No configuration was written.

## Evidence and source declarations

The `configuration-m5g-` extension contains **86 records: 85 complete HTTP
exchanges and one audit**. It adds to the preceding 1965 records, bringing the
corpus to **2051** at this checkpoint. The capture recorded **104,719 response-body
bytes in 2.438 seconds**, excluding HTTP headers, with no incomplete replies or persistence failures.
The [audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-audit.json)
records successful cleanup and preservation checks.

The [recorder](../../scripts/test-env/reference-configuration.py) used the
original isolated reference instance and its private network namespace. It
pinned reference PID `3131777`, server identity, source ownership, and the
deployed M5f Goby process and executable. Two fresh ordinary login credentials
supplied administrator and viewer authority; their login/logout flows were the
only permitted mutations. Configuration reads were accompanied by task, user,
device, option, identity, and credential-cleanup controls. Limits were 480 total
HTTP attempts, 300 main-phase attempts, 256 KiB per response, 32 MiB total
response bytes, and 360 seconds per phase.

The [pinned SDK service inventory](../api/services/ConfigurationService.md)
declares five operations: total GET/POST, named GET/POST, and
`POST /System/Configuration/Partial`. Its `ServerConfiguration` model declares
69 properties. These are source declarations, not observed response counts or
proof of write behavior. The [current total-configuration HTML reference](https://dev.emby.media/reference/RestAPI/ConfigurationService/getSystemConfiguration.html)
also lists `BannerText` and `ValidateImageTags`, which are absent from the pinned
69-property model and the sampled 60-field object. This comparison does not claim
a complete current-HTML property count.

Named keys were selected from explicit official-client source calls, not guessed:
the historical [encoding settings script](https://github.com/MediaBrowser/Emby/blob/66e4d9ca63dcf46c4e57159252d34f9479515a42/MediaBrowser.WebDashboard/dashboard-ui/scripts/encodingsettings.js#L1)
requests `encoding`, the [JavaScript API client](https://github.com/MediaBrowser/Emby.ApiClient.Javascript/blob/fdd0939ac596406ab92274f30fd83d226a677424/apiclient.js#L2198)
requests `devices`, and the historical [DLNA settings script](https://github.com/MediaBrowser/Emby/blob/2334e1ecd577e3275602f0155793cb9024adc39f/MediaBrowser.WebDashboard/dashboard-ui/scripts/dlnasettings.js#L1)
requests `dlna`. Those source versions establish provenance; the captured GETs
establish that these three names exist in the sampled server.

## Authorization and response shape

All paths below use `GET /emby/System/Configuration`, optionally followed by the
named key. Administrator baseline and final reads returned equal exported
configuration values. Successful responses use JSON; the recorded denial and
unknown-key responses use plain text.

| Configuration | Administrator | Ordinary viewer | Anonymous |
| --- | --- | --- | --- |
| Total | `200`, object with 60 fields | `200`, exactly `{}` | `401` |
| `encoding` | `200`, object with 17 fields | `403` | `401` |
| `devices` | `200`, object with 2 fields | `403` | `401` |
| `dlna` | `200`, object with 3 fields | `403` | `401` |
| Reserved unknown name | `500`, plain-text error | `403` | `401` |

The [administrator total](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-admin-configuration-before-total.json)
and [viewer total](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-viewer-configuration-total.json)
fixtures preserve this visibility distinction. The viewer body is two wire bytes;
the empty object is an observed response, not a redaction of the full object.
Named viewer denial says `User reference-session-m3b does not have access to
ReadServerConfiguration feature.` Anonymous requests say `Access token is invalid
or expired.` The [unknown administrator request](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-admin-configuration-before-unknown.json)
returns the 37-byte text `Sequence contains no matching element`. This recorded
server error does not prescribe Goby's error contract.

## Sampled configuration fields

The administrator total object contains network/server flags, metadata defaults,
library and database settings, startup/logging settings, and migration markers.
Compared with the pinned 69-property model, these nine properties are omitted:
`CertificatePath`, `CertificatePassword`, `MetadataPath`, `MetadataNetworkPath`,
`DashboardSourcePath`, `WanDdns`, `MaintenanceModeMessage`, `RevertDebugLogging`,
and `CachePath`. Omission does not establish null/default values, unsupported
features, or safe mutation semantics.

`AllowLegacyLocalNetworkPassword` is present but conservatively exported as
`[REDACTED_SECRET]` because its name matches the sensitive-field policy. The
marker is not its wire value. The study therefore makes no claim about the
sampled value, even though the pinned SDK declares a Boolean property.

The [encoding fixture](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-admin-configuration-before-encoding.json)
contains the following 17 values:

| Field | Observed value |
| --- | --- |
| `EncodingThreadCount` | `-1` |
| `ExtractionThreadCount` | `1` |
| `DownMixAudioBoost` | `2` |
| `EnableThrottling` | `false` |
| `ThrottleBufferSize` | `120` |
| `ThrottleHysteresis` | `8` |
| `ThrottlingMethod` | `"ByStreamBufferSize"` |
| `H264Crf` | `23` |
| `EnableHardwareEncoding` | `true` |
| `EnableSubtitleExtraction` | `true` |
| `EnableOnTheFlyAttachmentExtraction` | `true` |
| `CodecConfigurations` | `[]` |
| `HardwareAccelerationMode` | `1` |
| `EnableHardwareToneMapping` | `false` |
| `EnableSoftwareToneMapping` | `false` |
| `TranscodingMaxWidth` | `0` |
| `EnableHevcEncoding` | `false` |

The [pinned EncodingOptions declaration](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/reference/pluginapi/MediaBrowser.Model.Configuration.EncodingOptions.html)
marks `EncodingThreadCount` as no longer used and directs callers to
codec-specific options. That declaration and the sampled `-1` do not establish
actual encoder thread counts. Likewise, `TranscodingMaxWidth: 0` does not prove
an unlimited-width execution rule. No media or encoder request ran, so the
hardware and tone-mapping flags do not demonstrate GPU execution.

The [devices object](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-admin-configuration-before-devices.json)
contains `EnabledCameraUploadDevices: []` and
`EnableCameraUploadSubfolders: true`. This is a global named configuration,
separate from per-device `CustomName` options; no camera upload was exercised.
The [DLNA object](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-admin-configuration-before-dlna.json)
contains `EnablePlayTo: true`, `EnableServer: false`, and `EnableDebugLog: false`.
These saved flags do not establish discovery, UPnP, or remote-playback behavior.

## Preservation, cleanup, and redaction

The audit preserves all preceding **1965 records / 3930 raw-export files**,
**240 known source files**, and **2621 private files**. All 22 old listed devices,
their options, and hidden server device `15` remain unchanged. Task membership,
configuration and recorded runtime state remain unchanged. Old user membership
and structure remain intact, with only explicitly attributed participant activity
permitted. The earlier 512 disposable task-study copies remain removed; they
are not additions to the permanent 240-source baseline.

The final device snapshot, taken before logout, contains the two new ordinary
device rows `33` and `34`. Cleanup retires their credentials without a device
deletion request. Both credentials were logged out with `204` and then independently rejected with
`401`: [viewer proof](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-cleanup-invalid-viewer.json)
and [administrator control proof](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-cleanup-invalid-control.json).
The original Emby PID `3131777` and M5f Goby PID `3570491` and executable are
preserved. The recorder issued zero configuration writes, task/device mutations,
application-key requests, existing-credential HTTP requests, media/encoder
requests, or source writes.

All 85 exact private wire records were retained and audited separately from the
safe exports. Deep redaction covers sensitive fields, known credential values,
dictionary keys, and supported nested header representations. Configuration
`Key` fields have no public-name exception. URL userinfo, query values, and
fragments are private. URL recognition decodes at most two percent-encoding
layers; residual encoded URL delimiters beyond that budget redact the entire
value. Wire headers and byte counts are evidence about the original response,
not the length of a redacted JSON rendering.

The [18 pure recorder guard tests](../development/m5g-configuration-recorder-tests.json)
passed with zero HTTP requests and zero capture writes. Their fixtures are
synthetic and memory-only; this is research-safety evidence, not Goby product
acceptance. The recorder SHA-256 is
`baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd`;
the [test source](../../scripts/test-env/test-reference-configuration.py) SHA-256 is
`05d59c80a48df412096a41b89790296d7440baffcffe91d3d977e9f228b92b12`.

## Unobserved behavior and implementation boundary

Total replacement, partial updates, and named POST bodies were not sampled.
The study does not establish their merge/replacement semantics, validation,
atomicity, revision behavior, restart requirements, or hot application. API-key
configuration authority, XML negotiation, additional names, path aliases, camera
upload, and actual encoder/DLNA effects remain unverified. An accepted GET or a
source declaration does not establish any of those write or execution contracts.

These limits describe this read-only capture. The later
[fresh write study](configuration-mutation-reference.md) samples bounded
configuration changes and token-only application-key no-op requests on a
separate disposable instance. Its evidence does not change the historical
requests or claims recorded here.

Goby's [native M5g settings increment](../development/verification-m5g-settings.md)
subsequently passed 1,222 top-level race tests, browser/restart checks, deployment
at schema **20** and probe **6**, and the deployed native workflow. Its three
administrator endpoints have a separate contract; the Emby ConfigurationService
adapter remains unimplemented. M4, M5, M6, and the complete compatibility goal
remain unfinished.
