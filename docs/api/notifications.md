# GobyWebhookV1 notifications

Goby provides a named HTTPS JSON notification transport and an independent
standard-library receiver/console client. It does not implement APNs, FCM,
proprietary Emby push, remote commands, or OS read receipts. HTTP acceptance and
client consumption are separate evidence. Existing WebSocket notifications and
remote control keep their existing transport and authority.

## Administrative configuration

`GET /admin/v1/notifications` requires a current native administrator session.
Its safe result contains `Revision` (decimal string), `Enabled`, `Endpoint`,
`AllowedNetworks`, `HasReceiverCredential`, `SupportedEvents`, and `PendingCount`.
`PUT` uses the same native cookie/CSRF boundary and requires the current Revision,
Enabled, Endpoint and AllowedNetworks. Optional ReceiverCredential is write-only:
absent preserves it, an empty string clears it, and a nonempty string replaces it.
Enabled requires a nonempty endpoint and a stored receiver credential. A disabled
configuration may have an empty endpoint. Null/unknown/repeated fields fail.

Endpoint is at most 2,048 bytes and must be HTTPS with no userinfo, query or
fragment. AllowedNetworks contains at most 32 canonical CIDR networks; private
destinations require explicit inclusion. Nil is not an empty array. Receiver
credentials and personal target tokens use 16-2,048 visible ASCII characters,
without spaces or control characters. Secrets are encrypted with separate AEAD
purposes and generation-bound associated data, never runtime-settings JSON.
Only presence/status is returned. Configuration changes increment the revision,
cancel prior-generation deliveries, and wait for active attempts to stop before
reporting success. Existing registrations retain their own enabled state.

TLS verifies the certificate chain and hostname. Default trust is the operating
system trust store. Embedding deployments can supply an explicit cloned
RootCertPool with `WithNotificationTrustRoots`; this does not disable certificate
verification. Redirects and ambient HTTP proxies are disabled. Each dial resolves
and checks all addresses, then dials the checked literal. Unspecified, multicast
and link-local addresses are never allowed. Private or reserved addresses cannot
be reached through ordinary client-provided URLs or token contents.

## Personal registration

Only an ordinary authenticated Emby login can use the personal routes below.
The session/user/device tuple is read from current credentials. An application
key or administrator cookie cannot become a personalized notification owner.

| Method and path | Meaning |
| --- | --- |
| `GET /Sessions/Notifications` | Safe state of this login's registration |
| `PUT /Sessions/Notifications` | Register or rotate this login's target |
| `DELETE /Sessions/Notifications?Revision=...` | Revoke this login's registration |
| `POST /Sessions/Notifications/Test` | Queue a self-targeted test; 202 is admission, not delivery |

The `/emby` prefix and route-literal case aliases apply. PUT requires Revision,
Transport=`GobyWebhookV1`, and EventIds containing distinct CatalogInvalidated
and/or UserDataInvalidated. TargetToken is required on first registration; on
later PUTs absence retains it and a supplied value rotates it. DELETE is the
explicit clear/revoke operation. An absent registration has Revision=`0`.

The safe DTO contains Id, Revision, Transport, Enabled, EventIds, HasTargetToken,
and LastOutcome. Successful delivery sets LastOutcome=`delivered`; a pending
retry uses `retry`; rotation clears it; revocation uses `revoked`. Configuration
disable does not fabricate a new personal outcome. Neither a token, its digest,
ciphertext, nor the receiver credential is returned. Each registration change
increments its revision, keeps its stable ID, cancels old deliveries, and waits
for their active request cancellation. Hidden-only source changes do not update
the personal outcome or its visible state.

`Sessions/Capabilities/Full` remains unchanged: Firebase/APNs/unknown vendor
PushToken declarations receive the established capability response and are
discarded. They never create a notification registration. A successful capability
declaration is not accepted vendor push registration.

## Source authority and durability

Source changes write a bounded SQL journal in the SAME transaction as the source
mutation. The common journal package has no network or domain dependencies.
Catalog changes use the owned transaction's trusted item/library/parent facts;
favorite/played/patch/entity/playback state changes use the exact state owner.
No HTTP response or after-commit callback supplies durability. A copied account
does not inherit registrations, tokens or another account's delivery history.

Fanout advances a registration's private cursor and its durable delivery rows in
one transaction. Before enqueue and before EVERY attempt/retry, it refreshes the
current credential and Item/Entity permissions. Catalog parent references retain
a private changed-item anchor: a visible parent cannot reveal that a hidden item
changed. These private anchors are removed from the wire payload. Historical
deleted item IDs are not sufficient current authority and remain suppressed.
User-data markers reach only their current owner. Opaque decimal item IDs and
numeric entity IDs remain explicitly typed. No WebSocket connection is needed.

Only currently authorized invalidations may be coalesced into a recipient's
identifier-free ResyncRequired message. Its private source references remain
available for every retry's permission check. There is no global source-resync
message that reveals hidden-only activity. The receiver/client must refresh its
current authorized catalog or state on resync.

Source journal bounds are 512 rows, 4 MiB total reference text, and a bounded
4,096-reference/512 KiB individual record. Recipient queues allow eight active
rows per registration, 512 globally and 16 MiB total; registration admission is
bounded at 64 enabled globally/four per user, with 512 retained registrations.
Terminal delivery history is pruned to 32 per registration. Queue controls and
claims are persisted, not process-only counters.

**Source backpressure is transactional.** A full journal or an unprovable scoped
batch returns an explicit retryable capacity error and rolls back the associated
database mutation. Once capacity is released, the caller can retry. Disabled
delivery or no eligible registration produces no backlog. This deliberately
avoids silently losing source changes or broadcasting a scope-free resync.
For filesystem publication which has already exchanged the file, the existing
prepared/recovery barrier and original backup remain; capacity failure cannot
declare success or automatically replay the exchange. The interaction has a
dedicated regression source fixture.

## Delivery protocol

An HTTPS POST carries:

```json
{
  "Version": 1,
  "EventId": "32-lowercase-hex-characters",
  "RegistrationId": "32-lowercase-hex-characters",
  "Generation": "2",
  "Kind": "UserDataInvalidated",
  "OccurredAt": "2026-09-20T00:00:00Z",
  "References": [{"Kind":"Item","Id":"opaque-item-id"}],
  "Recursive": false
}
```

EventId identifies the immutable recipient delivery and is stable across retries.
Generation is the registration revision as decimal text. OccurredAt is the
delivery's admission timestamp. Catalog references also carry an authorized
LibraryId. The receiver must treat this as invalidation of its current state,
not an unrestricted cached item/user-data DTO. A payload exceeding 32 KiB becomes
a ResyncRequired message after source authorization. Test and ResyncRequired
carry no item details. The server never grants playback or remote control through
this channel.

Headers are `X-Goby-Notification-Version: 1`, `X-Goby-Target-Token`,
`X-Goby-Timestamp` (Unix seconds), and `X-Goby-Signature` (lowercase hex HMAC-SHA256).
The HMAC key is the receiver credential; input bytes are the timestamp, `.`, and
the exact request body. The target token is a distinct receiver-issued secret.
The receiver validates authentication, replay window and target, durably
deduplicates EventId, and returns a 2xx JSON acknowledgement:

```json
{"EventId":"same-event-id","Accepted":true}
```

An invalid/oversized acknowledgement is not delivery success. 408/429, 5xx and
temporary network failures retry; bounded Retry-After is honored up to one hour.
Other delays use bounded exponential backoff. At most five attempts and 24 hours
are admitted per delivery. 401/403/404/410 invalidate that registration target.
Other permanent failures become terminal safe codes. Raw receiver bodies and
transport errors are not stored or logged. Request deadlines are 10 seconds,
with bounded dial/TLS/response headers and a 4 KiB response-body cap.

At-least-once delivery requires receiver deduplication: a timeout can follow a
successfully received request. Per-registration source ordering is retained
across pending retries. Current generation and identity are monitored during
active requests; revocation/rotation/disable cancels and joins them. Bytes already
accepted by a receiver cannot be retracted. A 2xx acknowledgement does not prove
an OS notification, display to a human, or a read receipt.

## Reference receiver and console client

`cmd/goby-notification-receiver` uses only the Go standard library and is
independent from the server implementation. It accepts only `/events` over HTTPS.
Use service-private credential files (0600) and a private receipt directory (0700).

```text
goby-notification-receiver -mode receive -listen 127.0.0.1:8443 -tls-cert receiver.pem -tls-key receiver-key.pem -credential-file receiver.secret -target-token-file target.secret -receipts private-receipts
goby-notification-receiver -mode consume -receipts private-receipts
```

Receive mode writes the original message to `<EventId>.json`, emits a safe
acceptance receipt, and acknowledges only after file synchronization. Consume
mode is a separate console client which reads those messages once and emits
EventId, Kind, ReferenceCount, Consumed and RequiresRefresh. It claims console
consumption only. Credential files are re-read on each request to support explicit
rotation. `-transient-failures 1` is a controlled receiver fault for actual transport
acceptance: the first authenticated request gets 503/Retry-After before normal
processing. It does not mock Goby business responses. No fault is enabled by
default. Receipt storage/consumption is bounded.

## Restart, restore and close

An ordinary restart can reclaim expired 30-second leases and retry valid pending
generations with the same EventId, after current authority checks. Graceful close
cancels requests and retry waits, joins the worker, and releases database/catalog
dependencies only afterward. A caller timeout does not create a second shutdown
worker or abandon the original join.

Backup authenticates both secret purposes from the exact exported snapshot,
including disabled history, before archiving the protected master. Restore
validates raw fingerprints and ciphertext first, then disables imported transport
and registrations and cancels active delivery rows as backup_restored. This is
idempotent and checked for revision overflow. No restored queue automatically
sends external traffic or rebinds a target to a new login. Fresh credentials and
explicit registration/enable are required. Secret bytes remain out of DTOs,
logs and plaintext database/configuration material.

Phase 4 consolidated test and actual receiver/client results are recorded in the
phase delivery evidence after the full source freeze; this document does not
claim that merely queuing a test message demonstrates external consumption.
