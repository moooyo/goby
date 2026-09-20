# Selected compatibility phase 4 execution record

## Status and authority

Phase 4 implementation follows the closed phase 3 documentation at `84d5d74`
on `codex/selected-client-compatibility`. Source, administrator UI, test fixtures
and documentation are being integrated. No phase 4 compilation, tests, runtime
probes or browser verification have run. Source presence is not acceptance.

The user authorized local compilation and unit tests, remote integration/media
and browser checks on `test-env`, and final merge to `main` plus push after all
four phases close. Actual local E2E is prohibited. The selected AMD profile
retains its existing CT 104 exception through `ssh pve`. Complete phase code
before consolidated verification. `ui-ux-pro-max` remains disabled for this task.

The user accepted server adapter delivery for third-party clients. Original
Emby Web commercial restrictions are not an acceptance gate; earlier blocked
client results remain unchanged. PIN remains an encrypted profile lock with
owner-only authenticated projection; password authentication remains required.

## Delivery inventory

| Work | Current implementation | Required verification |
| --- | --- | --- |
| D1/D2 managed settings | Strict partial/reset/CAS updates; deployment defaults and stored overrides; network, CPU/AMD profile, threads, H264/HEVC quality and tone mapping | Default/reset/migration semantics; actual new execution uses settings; admitted jobs keep their snapshots; selected device availability |
| Network lifecycle | Desired versus reserved active binding; pending restart state; actual TCP endpoint origin checks; honest occupied-port failure | Real restart and authentication on changed IPv4/IPv6 endpoints; old listener closure and fresh CSRF |
| D3 management | Current policy eligibility counts, supported deletion-folder grants, local metadata/library options and minimal backed capabilities | Current authority, stale writes, unsupported values, native forms and compatibility projections |
| D4 protocols | Selected aliases, library-root removal resync, session correlation and remote commands | Matched client playback/reports, executed Pause, disconnect/reconnect state refresh and revocation |
| D5 notifications | GobyWebhookV1 HTTPS transport and independent reference receiver; sealed credentials; transactional source journal; bounded retries; current source authorization; revocation fences | Actual trusted TLS delivery and consumer observation, first-attempt retry, hidden-source suppression, rotation, revocation, disable and cleanup |
| D6 recovery and closeout | Preserve trusted target host settings, restore portable quality settings, authenticate sealed state before notification reset, exact schema inventory | Schema 47/48 catalogs, migration, backup/recovery, affected regression, application builds, final native journey and owned-resource closure |

Field-level contracts are in [managed execution settings](../api/managed-execution-settings.md),
[encoding controls](../api/encoding-controls.md), [managed hardware](../api/managed-hardware.md),
[HTTP binding](managed-http-binding.md), [selected management](../api/selected-management.md)
and [notifications](../api/notifications.md).

## Boundaries and evidence discipline

GobyWebhookV1 is the selected transport. It does not claim APNs, FCM, proprietary
Emby push, mobile background delivery or arbitrary third-party client support.
The controlled independent receiver and its matching reference consumer are
the actual transport acceptance target. Vendor capability tokens remain
unsupported and are discarded without creating a registration.

Private contracts and the checkpoint are retained under
`D:/Code/goby/.git/selected-compatibility-20260920/`; phase 4 evidence belongs in
its `phase4` directory. No phase 4 remote verification runtime is initialized.
Earlier phase workers and PostgreSQL services remain stopped, with their
evidence and databases retained. Preserve the unrelated original-client service.

Live TV, EPG, DVR/recording/tuners, DLNA, external channels and group playback
remain explicitly excluded. Provider-online, OCI, non-AMD hardware, native arm64,
broader capacity/platform delivery and other unselected work remain deferred.
No deployment or publication is claimed by this implementation record.
