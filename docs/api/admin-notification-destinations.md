# Administrator notification destinations

Administrator destinations deliver committed server events to multiple HTTPS
webhooks or email recipients. They are separate from the personal
`GobyWebhookV1` receiver and its session-scoped invalidation registrations.
Changing administrator destinations does not change a personal receiver,
registration, source cursor, or delivery generation.

## Native management API

All routes require an active native administrator login. Mutations also use the
normal same-origin and CSRF checks.

| Method | Route | Result |
| --- | --- | --- |
| GET | `/admin/v1/notification-destinations` | `Items`, `SupportedEvents`, and `Limit` |
| POST | `/admin/v1/notification-destinations` | Create using `Revision: "0"`; return the saved destination |
| PUT | `/admin/v1/notification-destinations/{id}` | Update using the exact saved revision string |
| DELETE | `/admin/v1/notification-destinations/{id}` | JSON `Revision`; return 204 after local senders retire |
| POST | `/admin/v1/notification-destinations/{id}/test` | JSON `Revision`; return 202 with delivery `Id` and `State: "pending"` |

A destination contains `Id`, `Revision`, `Name`, `Transport`, `Enabled`,
`EventIds`, `AllowedNetworks`, `Endpoint`, `SMTP`, `HasSecret`, `LastOutcome`,
optional `LastDeliveredAt`, `PendingCount`, and `DroppedCount`. Revision values
are decimal strings, including values above JavaScript's safe integer range.

Create and update requests contain `Revision`, `Name`, `Transport`, `Enabled`,
`EventIds`, `AllowedNetworks`, `Endpoint`, `SMTP`, and optional `Secret`.
Omitting `Secret` preserves it, a nonempty value replaces it, and an empty value
clears it. Saved values and ciphertext never enter a response. Changing the
transport, webhook endpoint, SMTP host, or SMTP username requires explicitly
replacing or clearing an existing secret.

`Transport` is `webhook` or `smtp`. SMTP configuration has `Host`, `Port`,
`Security` (`starttls` or `tls`), `Username`, `From`, and `To`. In webhook
responses, unused SMTP configuration is
`{"Host":"","Port":587,"Security":"starttls","Username":"","From":"","To":[]}`.
SMTP responses have an empty `Endpoint`.

Names use at most 120 UTF-8 bytes. Secrets use at most 1,024 UTF-8 bytes and
contain no control characters. A webhook signing secret additionally requires
16 to 1,024 visible ASCII characters. SMTP accepts 1 to 16 distinct ASCII email
addresses without display names; international domains can use Punycode.
An SMTP username requires a password when enabling delivery. Anonymous TLS
relays use neither username nor password.

## Event subscriptions

Only existing committed activity actions are exposed. A trigger records the
delivery in the source transaction, so rolled-back actions cannot escape.

| Event | Activity actions |
| --- | --- |
| `UserChanges` | `user.created`, `user.updated`, `user.deleted`, `user.password_reset` |
| `SignIn` | `session.login` |
| `LibraryChanges` | `library.created`, `library.updated`, `library.removed`, `library.root_binding.updated` |
| `ScanFinished` | `scan.finished` |
| `TaskFinished` | `task.finished` |
| `BackupFinished` | `backup.published` with completed state |
| `MetadataChanges` | `metadata.updated` |

Backup request, import, download, restore, and pre-publication `backup.finished`
authorization actions do not produce a `BackupFinished` notification. Only the
idempotent receipt recorded after successful publication can produce it.
Subscriptions start with future events; creating,
editing, disabling, and re-enabling a destination never replays old activity.
An edit cancels pending deliveries from the old revision.

## Delivery behavior

The background worker admits two administrator sends independently of the
personal notification worker. There are at most 16 destinations and 256 pending
or sending deliveries per destination. A saturated queue increments the visible
`DroppedCount`; it does not pretend the event was delivered or block the source
business transaction. The internal terminal queue retains at most 64 entries per
destination for at most seven days. It is not a permanent delivery-history API.

Webhook requests contain JSON `EventId`, `Kind`, `Title`, `Summary`, and
`OccurredAt`, together with `Idempotency-Key`, `X-Goby-Notification-Version`,
`X-Goby-Timestamp`, and `X-Goby-Signature`. The signature is the existing SHA-256
HMAC over the timestamp, a dot, and the exact body. A 2xx response acknowledges
acceptance. This generic endpoint does not require the personal receiver's JSON
acknowledgement or target-token header. Receivers should deduplicate stable
event IDs when a request receives an explicit retryable failure.

SMTP requires verified implicit TLS or STARTTLS and supports PLAIN or LOGIN
authentication inside TLS. DNS resolution and every literal connection address
are checked against the public-address policy and optional canonical private
network allowlist. Neither channel follows redirects or disables certificate
verification. Send attempts have a total ten-second budget.

The UI reports a test as queued until the destination's actual result is read.
`delivered` means the remote endpoint acknowledged acceptance, not that a person
read an email. `delivery_unconfirmed` is terminal and is never automatically
replayed. A sending lease left by a process interruption likewise becomes
unconfirmed instead of blindly resending. Safe pre-delivery failures have at
most three attempts.

Browser mutations have a twenty-second deadline. A timeout is an unconfirmed
result and blocks further writes; it is not proof that the server rejected the
change. An explicit saved-state read has a fifteen-second deadline and only a
successful response clears this review gate. A failed or timed-out read keeps
the draft and blocks writes, and an older background poll cannot replace the
successfully reloaded state.

Administrator authority and destination generation are checked before external
content is sent and observed during the attempt. Disabling, deleting, or editing
a destination fences its old local senders. A destination whose configuring
administrator is disabled or demoted is disabled and its queue cancelled; an
active administrator can save it again. Backup recovery validates the sealed
credential witness, disables restored destinations, and cancels restored
pending work before activation.

ntfy, permanent delivery history, and notification events without an actual
server source remain outside this contract.
