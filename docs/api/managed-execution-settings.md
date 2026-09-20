# Managed network and execution settings

The selected Phase 4 settings share the existing managed-settings revision,
administrator authority, transaction and ConfigurationChanged publication path.
This document specifies implemented storage and adapter semantics. Actual
execution, browser, restart and recovery acceptance is recorded separately in the
phase delivery record; source and test coverage alone do not claim acceptance.

## Native changes and retained defaults

`GET/PUT /admin/v1/settings` retains its existing default, override, effective and
source projections. The optional `Runtime` update contains independently present
groups. Omitting Runtime or one of its groups preserves that group's stored
override; a present null clears it. `Runtime: null` clears all seven groups.
Codec groups are complete values when present. Unknown fields, partial codec
groups, invalid scalar types, duplicate keys and invalid values fail the entire
request. Native revision strings, CSRF, current administrator checks and
uncertain-result reload behavior are unchanged.

Example Runtime update within the existing native settings request:

```json
{
  "Revision": "12",
  "Overrides": {
    "ServerName": null,
    "MaxBitrate": null,
    "MaxWidth": null,
    "MaxHeight": null,
    "MaxAudioChannels": null
  },
  "Runtime": {
    "Network": { "BindHost": "127.0.0.1", "HttpPort": 8097 },
    "Threads": 4,
    "H264": { "Preset": "fast", "RateControl": "capped_crf", "CRF": 23 },
    "SoftwareToneMapping": true,
    "VulkanToneMapping": false
  }
}
```

The existing native Overrides object still replaces its five fields. A client
must preserve the current values there when editing only Runtime. Runtime does
not reset the existing independent compatibility width or Management sections.

| Group | Bound and default | Actual effect |
| --- | --- | --- |
| Network | BindHost is empty wildcard or one canonical IP literal without brackets, zone or port. HttpPort override is 1..65535. Each nullable member can retain its deployment default. | Desired binding for the next application generation; does not replace the running socket during a save. |
| Hardware | Complete Decode/Encode software or vaapi selection and an opaque DeviceId from the startup-authorized AMD inventory. Hardware requires a device. A software tuple may retain an authorized dormant choice. | New planning/admission resolves the selected approved device; paths and loader environment remain deployment-owned. |
| Threads | Integer 1..64; deployment default, normally 2. | Captured FFmpeg thread setting for each admitted job. It is not a claim that the entire process has at most that many threads. |
| H264 | CPU-only preset veryfast/fast/medium/slow; bitrate or capped_crf; CRF integer 18..35. Default veryfast/bitrate/23. | Final CPU H.264 execution, including an admitted CPU fallback. |
| HEVC | Same closed choices and bounds; default veryfast/bitrate/28. | Final CPU HEVC execution. |
| SoftwareToneMapping | Boolean; deployment default true. | Permits the selected CPU tone-map graph. False rejects a conversion requiring that graph. |
| VulkanToneMapping | Boolean; deployment default true. | Permits the selected Vulkan tone-map graph. False rejects a conversion requiring that graph. |

Explicit values equal to defaults remain explicit stored choices. Reset names are
`Runtime`, `Runtime.Network`, `Runtime.Hardware`, `Runtime.Threads`, `Runtime.H264`,
`Runtime.HEVC`, `Runtime.SoftwareToneMapping` and `Runtime.VulkanToneMapping`.
Reset is atomic and restores current deployment defaults only for those choices.
No-op resets preserve revision, timestamp and events. Large revisions remain
exact decimal integers, and revision exhaustion fails without a partial write.

## Desired and running network state

Settings store Network defaults, overrides and the resolved desired binding.
The generation reserves the socket and supplies the observed running state.
Saving a new bind/port reports pending restart while the current listener stays
available. Reverting to its running configured binding clears pending restart.
A successful restart uses the saved desired binding; reservation failure is
reported rather than silently using another port. The settings API does not
imply a new self-restart or TLS/certificate service.

Deployment-only hostnames and port zero remain available to existing deployment
and private-fixture construction. They are not new managed override values.
The fixed PublicURL remains the advertised/reverse-proxy origin. The additional
native direct HTTP origin is validated using the actual accepted connection's
local address and port; localhost is accepted only for actual loopback.
Arbitrary Host/X-Forwarded-* values never establish origin authority. PublicURL,
CookieSecure and proxy trust are not changed by a managed port save.

## Hardware authorization and availability

The inventory is frozen from deployment-authorized AMD device definitions. An
opaque ID is the API selection boundary; a request cannot introduce a filesystem
path, driver option or environment variable. Authorization and current startup
availability are separate: a known but unavailable device can remain selected,
and an old stored ID missing from the current inventory does not prevent native
administration from starting. Hardware processing using that selection is
unavailable. A new selection of an unknown ID is rejected; unrelated settings
and an unchanged orphan selection can still be saved.

An approved inventory record does not prove a codec, decoder, filter or source
tuple works. The existing exact execution admission remains authoritative. No
new fallback is promised for an unavailable persisted device. Existing accepted
jobs keep their concrete hardware/fallback choice. A legacy non-AMD deployment
tuple can remain the exact deployment default and be restored by a null reset;
it is not a selectable managed non-AMD profile. A legacy default cannot bypass
the managed AMD availability check.

## Encoding applicability and snapshots

Defaults are supplied by the encoding engine's typed execution options. CPU
quality settings are labeled by final output codec. AV1 input converted to CPU
H.264 uses H264 settings; final AV1 and VAAPI encoders do not receive x264/x265
preset or CRF arguments. Their existing admitted codec-specific bitrate policy
remains in effect. CPU choices can remain stored while inactive for a successful
hardware encode, and apply only when CPU output is actually selected.

Bitrate mode keeps the existing bitrate target and VBV ceilings. Capped CRF uses
the selected CRF with the already resolved bitrate ceiling and buffer, and does
not discard source/client/server/user restrictions. Neither CRF nor a preset
changes authorization, codec/profile/dimension bounds or transport admission.
Tone-map controls gate the actual filter backend, independently of which encoder
is selected. Disabling a required backend cannot remove tone mapping and relabel
the resulting HDR pixels as SDR. Vulkan-only admitted Dolby Vision/HDR10 behavior
does not gain an invented CPU substitute.

One request captures committed complete options; final applicable comparable
values are carried into Plan/Spec, job persistence and reuse identities. Queued
and running jobs consume their own execution capture. New preferences alone do
not replan or retire them. Revalidation continues to enforce current account,
ACL, source and existing safety ceilings. Unrelated settings changes do not split
an output cache merely because the overall settings revision changed.

## Supported compatibility scalars

Only supported, backed fields are admitted. The named encoding body still owns
the independent TranscodingMaxWidth ceiling. Thread count, device selection and
CPU preset/HEVC groups remain native; the obsolete EncodingThreadCount and
unproven HardwareAccelerationMode/CodecConfigurations structures are not writable
compatibility aliases.

| Mutation | Exact effect |
| --- | --- |
| Full server HttpServerPortNumber present | Sets only the managed port override. |
| Full server port omitted | Clears only the port override; preserves a managed bind host and all encoding groups. |
| Partial port omitted | Preserves it. |
| H264Crf present in named encoding or Partial | Uses the current effective H264 preset, activates capped_crf and sets the bounded CRF value. |
| Complete named encoding omits H264Crf while capped_crf is active | Returns H264 to bitrate and default inactive CRF 23; preserves its effective preset and unrelated native groups. |
| Complete named encoding omits H264Crf while already in bitrate mode | Strict no-op for H264, including any inactive native CRF value; does not manufacture an override. |
| EnableSoftwareToneMapping / EnableHardwareToneMapping present | Sets CPU / Vulkan tone-map policy respectively. Hardware in the wire name identifies the filter policy, not the encoder. |
| Complete named encoding omits the tone-map flags | Clears their overrides and restores deployment defaults, normally true. |
| Partial omits CRF/tone fields | Preserves them. |

The compatibility reader includes H264Crf only while capped_crf is active; an
inactive stored preference is not advertised as active quality control. Full
server configuration rejects named encoding fields; Partial can explicitly set
the three selected CRF/tone scalars. TranscodingMaxWidth keeps its existing named
section-only rule. Unknown properties remain errors rather than acknowledged
inert settings. Native and compatibility changes share the same revision, so a
compatibility write invalidates an older native CAS draft.

## Persistence, migration and recovery ownership

Schema 47 adds one strict `runtime_overrides` JSON object to managed_settings,
with exactly Network, Hardware, Threads, H264, HEVC, SoftwareToneMapping and
VulkanToneMapping keys. All begin as null; migration does not copy the migration
host's environment into a database override or modify historical revisions and
timestamps. Stored Hardware uses internal DeviceID spelling; native adapters
project DeviceId. Active listener state, resolved device paths and inventory
evidence are never stored in this object.

Settings activity records only the Runtime field name, not addresses, devices,
encoder values or raw input. Runtime changes retain the existing same-transaction
activity and ConfigurationChanged guarantees. A failed transaction cannot
publish a snapshot, and a cancelled HTTP caller cannot cancel publication after
commit.

Logical backups retain exact raw source rows for validation. Only after raw
archive validation, activation-level recovery normalizes Network, Hardware and
Threads to a trusted target-host capture. An offline target without such a
capture clears those overrides to its deployment defaults. Bounded CPU quality
and tone preferences remain portable. Incoming archive values never authorize a
target device or select its listener. Normalization changes the shared revision
once when needed, preserves it for a no-op, and rejects overflow. It runs in the
caller's verified transaction and does not fabricate an administrator event.

The trusted seams are `CaptureTargetHostSettings(Snapshot)`,
`ReadTargetHostSettings(ctx, tx)` for a coordinator-owned target transaction, and
`NormalizeRestoredHostSettings(ctx, tx, TargetHostSettings)`. The recovery owner
must capture from target authority, not from incoming archive metadata, and must
record raw-versus-normalized evidence separately. This normalization does not
rewrite existing job plans or grant execution to historical queued work.
