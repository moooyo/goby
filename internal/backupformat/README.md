# Goby backup format v1

`backupformat` is the streaming container layer for Goby-native backups. It has
no filesystem or database access. Importing an archive does not execute its SQL,
restore a database, replace configuration, or activate a master key.

The complete artifact is one binary `age-encryption.org/v1` file encrypted to
one passphrase recipient using `filippo.io/age`. Creation fixes the scrypt work
factor at 18. Import calls `SetMaxWorkFactor(18)` before decryption; lower work
factors remain readable, but production creation cannot select one. The clear
age header is limited to 16 KiB before the age parser or KDF runs. The official
age implementation authenticates the header and the complete chunked payload.

The plaintext is an exact tar stream with these members, in order:

| Member | Requirement |
| --- | --- |
| `manifest.json` | First; canonical JSON described below |
| `database.dump` | Nonempty opaque PostgreSQL dump bytes |
| `configuration.json` | Nonempty opaque versioned configuration bytes |
| `master.key` | Optional; exactly 32 bytes |

Every member is a regular file with the canonical header emitted by Go's
`archive/tar` using `FormatGNU`, mode `0600`, UID/GID zero, Unix epoch modification
time, and no other metadata. GNU headers permit sizes above the USTAR 8 GiB
limit without PAX extensions. Import reads the raw 512-byte header, rejects any
non-regular type before the standard tar reader can hide extensions, and compares
every byte with a freshly generated canonical header. Names are exact constants,
not paths. Path variations, duplicate or extra members, links, PAX, sparse files,
extended names, ownership metadata, alternative headers, and special files are
rejected. Member padding is zero, and the archive ends with exactly two zero
blocks. No extra plaintext padding or concatenated archive is accepted.

The manifest uses the field order, field spelling, integer encoding, escaping,
and whitespace emitted by `encoding/json.Marshal(Manifest)`, with the timestamp
normalized to UTC. Import decodes with unknown fields disabled, validates the
typed structure, marshals it again, and requires identical bytes. This also
rejects duplicate JSON members and ambiguous alternate representations. The
format string is exactly `goby.backup.v1`; the artifact ID is 32 lowercase hex
characters. The source server ID is bounded opaque UTF-8, not necessarily a hex
identifier. Database schema and table names use conservative ASCII identifiers
matching `[a-z_][a-z0-9_]{0,62}`. Table facts are strictly sorted and unique.

Source facts also include `schema_sha256`, the engine-defined schema catalog
fingerprint, and `migration_checksums`, the embedded migration source inventory.
Migration entries have exact consecutive versions from 1 through `schema_version`,
names matching the version's four-digit prefix followed by
`_[a-z][a-z0-9_]*.sql`, and lowercase SHA-256 digests. Names are at most 128 bytes,
and the four-digit format bounds schema versions to 9999. These descriptors
identify source code and expected catalog structure; they do not claim that an
older Goby database stored a checksum column or prove its executed SQL history.
The engine must compare them with its own trusted migration and catalog facts.

The manifest lists sizes and lowercase SHA-256 digests for every content member.
Creation rechecks each source while writing and requires EOF at its declared
size. Import checks each header size, actual byte count, and digest while
streaming. Import then reads the age reader through authenticated EOF. Stopping
at the tar footer would not be sufficient: it could miss unauthenticated final
ciphertext or appended plaintext.

`Create` applies `DefaultLimits()`. `CreateWithLimits`, `Extract`, and `Inspect`
accept caller-selected lower storage budgets or bounded larger budgets. The
defaults are 256 KiB for the manifest, 16 GiB for the dump, 1 MiB for configuration,
17 GiB for the complete plaintext, 18 GiB for ciphertext, and 512 table facts.
Hard ceilings are 1 MiB, 1 TiB, 16 MiB, 2 TiB, 2 TiB, and 4096 tables respectively.
Sizes are checked before allocation or streaming. A stream budget allows one
lookahead byte solely to distinguish exact EOF from an input over its limit;
reaching the configured limit is never silently converted into EOF. Dump I/O
uses 64 KiB buffers instead of retaining the dump in memory. The scrypt KDF has
its own bounded memory cost, approximately 256 MiB at factor 18.

Callers supply fixed-purpose `Entries` or `Sinks`, keep them private, and own all
handle closing. `Create` can write bytes before detecting changed source content;
it deliberately leaves the age stream unfinished on error. The caller must
discard that output and publish only after successful creation and its own
sync/close. `Extract` can write staging bytes before a later validation or
authentication error. Every sink must be discarded after any error and used
only after successful extraction plus the engine's independent checks.
`Inspect` discards contents but performs exactly the same full verification; it
does not return an unverified early manifest.

Passphrases must be nonempty valid UTF-8 of at most 1024 bytes. A UI or CLI must
apply its own minimum-strength and confirmation policy. Passphrases are accepted
as byte slices, but the age API requires a string and keeps internal copies; Go
does not guarantee erasure of those copies. Context cancellation is checked
around I/O and before and after expensive work. A synchronous scrypt operation
cannot be interrupted midway through the library call. A caller supplying a
blocking reader or writer must arrange its interruption, such as closing its
own handle on cancellation. The package does not create detached I/O goroutines.

Format consistency and age authentication do not establish source identity or
SQL safety. SHA-256 describes content equality, not provenance. Anyone knowing
the passphrase can create a new valid artifact. The engine must verify source
compatibility, the expected table inventory, snapshot fingerprint rules, and
configuration semantics before accepting a restore candidate. Sequence state
is not implicitly covered by table fingerprints and requires separate engine
handling. Never execute a decrypted dump merely because this package accepted
its container.

Public errors are fixed categories, with context cancellation preserved.
Attacker-controlled age errors, raw member contents, passphrases, and underlying
filesystem error text are not returned as diagnostics.

Tests construct independent real age/tar fixtures, including malformed and
authenticated adversarial plaintext. They cover a production-factor round trip,
incorrect passwords, corruption, truncation, ciphertext and plaintext suffixes,
full final age chunks, tar extensions and types, canonical JSON, integer bounds,
streaming I/O, source changes, sink failures, and cancellation. Tests must run
on the project's authorized remote verification environment.
