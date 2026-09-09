# Application Keys Implementation and Operations

This describes the M5d implementation. The [verification report](verification-m5d-application-keys.md)
records the Linux race suite, browser/restart checks and deployment evidence;
these bounded results do not establish complete client compatibility.
The [API contract](../api/application-keys.md) covers request and response
details and links the independent reference studies.

## Credential and session model

`sessions` remains the shared authentication table. Application credentials
have `kind = 'application_key'`, a real token hash, no user ID, and no expiry.
`application_keys` stores the numeric management ID, encrypted recoverable
token, creator audit reference, and activity metadata. Issuing a key never
creates a user or an administrator login. Creator deletion does not revoke it.

`application_key_clients` holds persistent contexts under the real parent
credential. Its unique identity is `(credential_id, client_name, device_id)`;
device name and version are mutable metadata. Each context has its own real
wire session ID, capabilities, presence, playback, and event scope. Creation
includes a default context; the per-key cap is 256. Changing metadata does not
mint credentials. Ordinary logins retain their existing single-context IDs.

Playback ownership includes the parent credential and client context, with a
nullable user. Userless lifecycle reports do not write user item data. Catalog
operations carry the authenticated credential separately from an optional real
target user; the latter supplies catalog visibility and explicit user-state
scope. Playback target conversion policy does not become session ownership.

Management serializes through the existing advisory management lock and
revalidates actors before commit. Revocation locks and retires the parent,
making every client context unauthorized. Committed local revocations also
disconnect credential-scoped WebSockets and cancel matching conversion work.
Long original-media responses check authorization before headers and every
five seconds; WebSockets also periodically revalidate. A playback ID only
restores an already authenticated parent's owned context.

## Persistent secret vault

Set `GOBY_API_KEY_MASTER_KEY_FILE` to a persistent file. The default is
`application-key-master.key`, resolved against the process working directory
during configuration loading. With the supplied systemd unit's
`WorkingDirectory=/var/lib/goby`, it resolves to
`/var/lib/goby/application-key-master.key`. Configuration loading does not
create the file. Use an existing private directory owned by the service account;
the vault does not create its parent directories.

The vault is implemented for Linux. It requires an ordinary 32-byte binary file
owned by the effective service UID, with exactly mode `0600` and one hard link.
Do not encode its contents as hex or base64. Every path component is opened
without following symlinks. Readers and creators take an exclusive directory
`flock` with bounded waiting. First creation uses exclusive file creation,
cryptographic randomness, explicit permissions, and file and directory `fsync`.
Non-Linux secret operations fail as unsupported rather than creating a weaker
file. Parent-directory privacy is an operator deployment requirement.

Tokens use AES-256-GCM with a fresh 12-byte nonce and a versioned envelope.
Additional authenticated data includes the credential ID and a fixed purpose
string, preventing ciphertext from being reassigned to another credential.
Authentication compares the stored token hash and does not decrypt a token;
decryption supports native reveal and the compatibility full-token list.

Master creation is lazy and is allowed only when the file is absent and no
application-key ciphertext exists. Existing rows, including revoked history,
act as a witness: creation must decrypt an existing row first. During a process
lifetime, changes to the observed file identity or key digest are rejected.
Missing, malformed, replaced, or mismatched keys are never silently repaired
or replaced by a new master.

If the master is lost or inaccessible, native safe metadata listing,
hash-based authentication, and revocation remain available. Creation and
active-key reveal return `503`; compatibility listing also requires the vault
when it returns active secrets. Restore the matching master instead of
deleting history or generating an unrelated replacement.

## Backup, restoration, and migration

Back up the database and its matching master file together, with secret-safe
storage and access controls. Preserve the service UID ownership, `0600` mode,
single-link regular-file properties, and private parent directory on restore.
Restore the matching pair before starting the service; restoring the original
file after a running process has observed a different inode may require a
restart. Database-only backups cannot recover full tokens. The product backup
and restore workflow is not implemented, and there is no turnkey master-key
rotation workflow. Replacing a master file is not key rotation.

Migration `0016_application_keys.sql` extends `sessions` with constrained
nullable user and expiry columns, creates the two application-key tables, and
adds nullable user/context ownership to playback, encoding jobs, and client
playback references. Context foreign keys retain cascade behavior. Updated
uniqueness uses `NULLS NOT DISTINCT` so userless owners remain correctly
deduplicated. Existing login and playback rows are preserved; they are not
converted into keys. The migration's historical-row preservation is covered
by the linked verification report; product recovery tooling remains unfinished.

Implementation and regression sources cover management, vault handling,
catalog scope, sessions, media ownership, events, and migration preservation.
Run verification only on the authorized remote `test-env` environment unless
the current task explicitly permits local verification. Reference captures
establish only their documented request matrices; they do not certify all
clients, full reference behavior, or GPU conversion execution.
