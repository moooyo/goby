# Device Registry Implementation and Operations

**Implemented and accepted in M5e.** The
[verification report](verification-m5e-devices.md) records 1085 passing
top-level Linux race tests with zero skipped tests, isolated browser/restart
acceptance, schema-18 deployment, and the main-service device workflow.
Reference captures and their cleanup audits remain independent evidence.
The broader administrator milestone and complete client compatibility remain
unfinished. See the [API contract](../api/devices.md) for exact routes and DTOs.

## Identity boundaries

Device records group ordinary Emby logins by their nonempty reported device
identifier across users and client applications. A numeric registry ID
identifies one generation of that grouping. Each login keeps its existing
credential ID, token hash, user authority, expiry, and raw client metadata.
Device grouping never merges authentication authority or transfers playback
ownership between users.

Three persistence scopes remain separate:

| Scope | Registry and relationship | Management surface |
| --- | --- | --- |
| Ordinary Emby logins | `devices`; `sessions.device_registry_id` references a generation only for `kind = 'emby'` | Native Devices page and compatibility DeviceService |
| Native administrator logins | Existing `sessions` rows with `kind = 'admin'`; no physical-device registration | Native Sessions administration |
| Application credentials | `application_key_devices`; `application_keys.reported_device_numeric_id` references a shared server generation; `application_key_clients` remains under each parent credential | API keys administration and direct privileged compatibility server-device operations |

Credential kind establishes this separation. The native dashboard's fixed
`goby-dashboard` identifier does not create an ordinary device. A real Emby
login that independently reports that literal string still registers an
ordinary device. Key client headers do not register ordinary rows, even when
their device string matches an existing ordinary device or the server identity.

The source for ordinary registration is
[device_registration.go](../../internal/identity/device_registration.go).
Successful Emby login registers or refreshes the current reported-ID row and
inserts its credential association in one transaction. A partial unique index
permits only one current ordinary row per reported identifier. An empty
reported identifier remains unregistered.

Activity uses authenticated database metadata, rather than a custom-name
projection or caller-modified principal, to update the registry's reported
name, application, version, user, address, and last activity. Unchanged activity
uses the existing session touch interval. The stored session's raw name stays
available for later override removal.

## Names, revisions, and list snapshots

An ordinary device's custom name is an optional administrator override.
Changing it updates the registry only; it does not rewrite the credential's
raw reported name. Principal and session DTO queries project the override
through the device association. Clearing it immediately exposes each session's
own raw name, while the registry independently selects its latest reported
name or fallback. This deliberately avoids the reference behavior in which
clearing device options left stale custom names in cached session DTOs.

For a live ordinary device, `Revision` advances only when the manual name
override changes. Routine activity and registration metadata updates do not
invalidate an open administrator edit. Unchanged names retain the revision;
a stale submitted revision still fails. Removal advances the stored revision
and sets the tombstone. The hidden shared-device table has internal metadata
revision updates too; those are not the native ordinary-device edit contract.

The native list reads filtered rows, count, and page from one statement. It
orders by activity descending and numeric ID descending and uses literal
case-insensitive substring search. Compatibility listing uses the same
ordinary registry ordering, returns all current rows, and supplies a coherent
total. Both lists omit the shared application-key server record.

`ActiveLoginCount` counts nonrevoked, unexpired ordinary credentials whose
accounts are enabled, evaluated against one observation timestamp. It does
not measure sockets, recent presence, or active playback. Revoked and expired
login history can leave a listed device with zero authorized logins. A last
user reference can become null after account deletion without removing the
registry history.

The React/MUI [Devices page](../../web/admin/src/DevicesPage.tsx) provides
search, pagination, effective and reported names, application details, last
user/activity/address, authorization counts, and rename/removal dialogs.
Conflict, not-found, and uncertain mutation outcomes require an explicit
refresh. Mutations are not automatically retried. The dashboard remains for
administrators and contains no end-user playback page.

## Ordinary removal and concurrency

Removal soft-deletes one generation and marks its associated ordinary Emby
credentials revoked. Authentication and playback rows remain stored; it does
not cascade into catalog metadata, media files, or personal playback state.
A later login with the same reported ID registers a new numeric generation,
without reviving the old credentials or inheriting its custom name.

The management transaction revalidates the actor before and after locks and
before commit. Native operations require the current native administrator
credential. Compatibility operations require a current ordinary Emby
administrator or application credential. Account disablement, credential
revocation, and administrator demotion cannot be bypassed with an earlier
middleware principal. A successful self-removal permits only the revocation
performed by that same transaction when doing the final authorization check.

Deletion takes the reported-ID registration advisory lock before acquiring
the participating account and credential rows in deterministic order. Login
registration takes that same device lock before its account lock. Therefore a
new credential cannot join the selected generation after deletion has fixed
its membership. The management lock also serializes competing administrator
changes. Ordinary activity avoids taking another registration/account lock
after its existing account and credential locks.

The returned deletion result contains every credential ID associated with the
generation, including on a repeated numeric deletion. The HTTP layer performs
process-local retirement only after database commit: it disconnects each
credential's event/WebSocket consumers and cancels matching conversion/HLS
work. Including the full retirement set on a retry allows that local cleanup
to be repeated after an interrupted response. The retry returns the original
deletion time and zero newly revoked credentials. Existing original-media
authorization watchers continue to provide bounded revalidation during long
responses.

## Shared application-key generations

[application_key_devices.go](../../internal/identity/application_key_devices.go)
maintains a separate hidden server-device family. Multiple keys created for
the same current server identity attach to one active generation. A key's
client contexts remain independently identified under their parent
credential; they do not become ordinary registry entries. Direct compatibility
Info can expose the server record even when device listing omits it.

The shared record's reported server ID remains stable. Recorded display name
and version can follow key-client usage; its application label comes from the
parent credential. A custom server-device name is a registry override and
does not replace client-context identity or stored raw names. Native device
routes cannot read or mutate this hidden row through their ordinary registry.

The first generation preserves numeric ID `1`. Subsequent generations draw
from `devices_id_seq`, the same allocator used for ordinary device records,
so a removed shared ID is never reused for an ordinary device or another
shared generation. Existing keys retain their original foreign key. If every
key is independently revoked, its shared registry history is still retained;
revoking a key and removing the shared device are distinct operations.

Shared-device deletion selects all parent application credentials attached to
exactly that generation and revokes them in one transaction. All their child
contexts become unauthorized. The HTTP retirement loop receives every parent
credential ID, so it closes all associated WebSockets and cancels every
matching conversion consumer, including contexts with different header device
IDs. Ordinary devices and native administrator logins retain their independent
credential scopes.

Shared management acquires the existing management advisory lock, then actor
account, sorted parent credential rows, key sidecars, actor context, and device
row in the established order. A key deleting its own generation does not hold
one client sidecar while waiting for a sibling parent's credential lock.
Final actor revalidation permits only that transaction's intentional
self-revocation. Storage errors or lost authority do not trigger lookup
fallback to another device family.

Compatibility adapters try the shared family before the ordinary registry.
`ErrApplicationKeyDeviceRemoved` preserves ownership of a known removed
server alias while retaining not-found behavior for reads. Thus repeated
deletion of a removed shared reported alias cannot accidentally remove an
ordinary device with the same reported string. A numeric ID addresses its
original generation; a reported alias selects the current shared generation
when one exists, otherwise retained shared history. Newly created keys after
shared removal receive a fresh generation. This generation/allocation policy
is Goby's design, not a claim established by the reference capture.

## PostgreSQL migrations and Linux deployment

[0017_devices.sql](../../internal/database/migrations/0017_devices.sql)
creates the ordinary registry with an identity sequence starting at `2`,
reserving `1` for existing application-key metadata. It adds the nullable
`sessions.device_registry_id` foreign key and a constraint allowing associations
only for ordinary Emby credentials. A partial unique index limits current
reported-ID membership; historical rows remain separate.

Backfill includes every nonempty reported device ID from ordinary sessions,
including revoked and expired history. It takes the earliest creation time,
latest activity time, and a deterministic latest session for display metadata
and last user. It associates existing ordinary credentials without replacing
their IDs, hashes, lifetimes, revocation dates, or raw client metadata. Native
administrator and application-key credentials stay outside this association.

[0018_application_key_devices.sql](../../internal/database/migrations/0018_application_key_devices.sql)
creates the shared server registry and replaces the previous constant-`1`
constraint with a generation foreign key. When existing key rows are present,
it backfills generation `1` from their stored server identity and activity
projection. Existing keys, including revoked history, retain numeric identity
`1`; an installation without keys does not need a fabricated backfill row.
The migration does not rewrite ciphertext, token hashes, key audit metadata,
client contexts, or existing playback ownership.

Deployment remains Linux-only with Go and PostgreSQL. Device state uses the
existing PostgreSQL connection and requires no SQLite file or new independent
device database. Apply migrations through the normal startup path before the
listener opens. The existing [Linux deployment instructions](running.md)
cover the administrator assets, service identity, database connection, and
FFmpeg configuration; device management introduces no new encoder setting.

Retain the PostgreSQL database and its matching application-key master file
together. Deleted key/device history still carries recoverable ciphertext and
the existing vault witness; removing a device must not discard or replace the
master. Follow the [vault and restoration requirements](application-keys.md).
These migrations are additive history-preserving upgrades; this document does
not provide an in-place downgrade or completed backup product workflow.

## Evidence and acceptance boundaries

The [ordinary reference studies](../research/devices-reference.md) establish
reported-ID grouping, option clear forms, numeric missing-ID replies, deletion
of ordinary credentials, and new ordinary registration after removal. They
also record reference count/cache defects and credential/session equalities
that do not redefine Goby's existing authentication ownership model.

The separate fresh-instance dataset contains 109 complete HTTP exchanges.
Its [recovery audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-recovery-audit.json)
preserves the original finalization failure and supplies an independent
cleanup/preservation proof. It shows a
[directly addressable hidden server record](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-keys-client-server-info-0.json),
ordinary lists without header-device registrations, and
[credential denial after shared deletion](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-server-delete-key-protected-owned-key-1-alpha.json).
It does not establish new-key generation behavior after deletion or execute
WebSocket/HLS retirement.

The accepted [M5e verification](verification-m5e-devices.md) covers both
migrations and historical-row preservation, native/compatibility HTTP
contracts, actor revalidation, registration/removal races, shared-family alias
isolation, all-parent key revocation, transport retirement, the administrator
browser workflow, and deployed state preservation. The isolated browser
journey took 6.930194 seconds and its restart comparison preserved all rows
across 21 public tables. The main-service ordinary-device workflow took
1.824 seconds; it retained every preexisting row and the existing key namespace,
then revoked all new credentials and soft-deleted its three owned generations.
Shared-key mutation was verified separately in disposable acceptance, preserving
historical shared records on the maintained service.

The [deployment evidence](m5e-deployment-evidence.json) records schema 18,
probe version 6, UID 995, and PID 3535438 at the accepted checkpoint. Existing
21 catalog items, 11 media files, and the matching master file were preserved;
the device upgrade issued no rescan. The deployed executable SHA-256 is
`394430272da8ad6268534c1bcaa925ccffc92f61ed0b136a8ec6bbc4dae88541`.
The [main-service workflow report](m5e-deployed-devices.json) records its exact
scope and cleanup. Recovery retained the migrated database; these results do
not establish a successfully executed database restore or a finished
backup/restore product.

Run functional tests and runtime/browser probes through authorized
`ssh test-env`; local build permission alone does not authorize local tests.
Camera uploads, camera-upload history, real-client certification, and full
hardware conversion acceptance are not established by this device increment.
