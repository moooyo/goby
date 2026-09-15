# R04 installation evidence resolution

Reviewed on 2026-09-15. Status: **the complete saved HTTP review passes with the
precise Emby response classification; the original final SQL observation remains
missing**. Overall installation acceptance is still open. This record supplements
the [original r04 result](systemd-installation-fourth-attempt.json); it does not
replace its failed seal, change historical receipts, or admit another installation.
Independent strict readback supports this fixed evidence set, with the helper
implementation limits recorded below. It does not accept the missing final SQL.

## Actual scope and result

The new private scope is
`/opt/goby-test/review-resume-20260915-r04` on `test-env`. It was created exclusively
with mode 0700. New source and receipts are private regular files with mode 0600.
All verification ran remotely. The main reader had a cooperative 45-second
deadline and a 1,200-file ceiling; it read 581 distinct retained evidence files
in approximately 0.132 seconds. This was not a hard wall-clock interruption.

The reader rechecked the recorded r04 execution, failed seal, HTTP diagnosis,
controller sources, SQL outputs and parsed snapshots against their SHA256 pins.
It evaluated only the saved-response review function and its pure catalog
comparisons. Original module entry points and actor constructors were not run;
request methods were replaced with lookup of already recorded responses. No
service action, application start, HTTP request, SQL connection, archive extraction
or modification of original evidence occurred. Raw headers, bodies, credentials,
configuration and database rows remain private on the remote host.

| New artifact | Bytes | SHA256 |
| --- | ---: | --- |
| `saved-evidence-review.py` | 16004 | `2c64f1b4cad6e014a435a4f9876581489f7f489ab2637941a75207eb552f9216` |
| `result.json` | 130076 | `ef6d84f43e9e9532329f357ef2144bbc60556f9ca12dd513be7b7dc8bb03d4ac` |
| `archive-metadata.json` | 1778 | `5306a77ab23271c815ca27dc86aa5f57973c3fce4cfe8f7f971ed0c825fe9cbe` |
| `archive-lineage.json` | 2082 | `2b05b220e27483cbff037bb9255879243a727f08077efd6b49829941ebd51d6e` |

These paths are relative to the new private scope above. The archive supplements
performed bounded saved-file and member-metadata inspection, without extracting
or parsing database members. One intervening SSH connection was rejected by the
SSH agent before remote execution; the subsequent connection succeeded. No local
verification fallback was used.

The independent receipt is
`/opt/goby-test/review-resume-20260915-r04-independent/result.json`, 129,761 bytes,
SHA256 `d92c1f380c3273017435e0ab3f5cb7252551c3a6d53e3b8d6c510d066f08e22c`.
It reread all 581 evidence pins and strictly checked 296 JSON files, 547 JSONL
records and 142 HTTP JSON bodies. All 586 files it read were root-owned and
0600, opened with `O_NOFOLLOW` and checked for stable identity before and after
reading. It did not rerun the original evaluator or any actor, SQL, HTTP or
service operation.

The first reader's helpers are not equivalent to the original sealer: its JSON
loader did not reject duplicate keys or non-finite numbers, and its file reader
omitted the original no-follow, stable-identity and private-mode checks. The
independent strict readback bridges those requirements for this fixed evidence
set only. The statement that only response classification changed applies to
the extracted HTTP evaluator text, not to its complete helper implementation.
Do not reuse this reader for new input with an unqualified equivalence claim.

## Completed HTTP proof

The unchanged saved HTTP evaluator reproduced `http_json_content_type` on the
recorded Emby revocation response. An in-memory copy changed only that response
classification. The retained sealer source itself is unchanged.

The exception requires all of the following: a first-start or second-start
`revoked-check` label for one of the three Emby roles, GET on the recorded Items
route, expected and actual status 401, exactly one `text/plain` Content-Type,
and the exact 35-byte invalid-token body already bound to the packaged product
by the preserved diagnosis. Its body SHA256 is
`64f610e896fbad1d2b9561c266036aad3ca7f64ef69effae892749fee5327255`.
The six affected response sequences are 141, 143, 145, 266, 268 and 270.
Native administrator 401 responses retain the JSON requirement.

The full saved review then passed:

- All 272 response records, intent/completion identities, statuses, lengths,
  request IDs, body/header pins, complete reads and connection closure.
- The original mutation inventory and both catalog, ACL and UserData readbacks.
- Eight distinct privately bound credentials, stored revoked-session bindings,
  and eight logout 204 / same-credential rejection 401 pairs.
- All 57 administrator assets on each start, plus five entry references per start.

The changed classifier also passed one exact positive case, exclusion of the
native administrator route, and five rejection cases for changed Content-Type,
body, method, route or status. This is a saved-evidence result; no response was
reissued. It retains the existing limitation that original request authentication
headers were not captured from the wire.

The pinned original `http_evidence` function has SHA256
`b5e0025087b6c9a5cc094376868d281acf2a6f8158d4beb845d1fa578db8da1f`;
the isolated corrected function has SHA256
`510c9746152812a767fd843f231e68fd05c5d3338c4f32c14c85b11f48f53f74`.
These are hashes of the extracted function text, not whole controller files.

## Exact remaining SQL assertions

The original `Seal.final_sql` contract is in the pinned r04
`private/m6-systemd-install-seal.py`, lines 658-703. Its SHA256 is
`79c320f18031044fb386d85bd7fc29c553bea301ae768a52b6a453101e2ac8cb`.
The saved-only reader confirmed the six runtime observers and three full snapshots.
Rows and `xmin` in all nine core tables match across those snapshots; revoked
session counts are 4, 4 and 8. The last full snapshot is `after-journey-2`, before
the second application stop.

| Original final assertion | Existing evidence | Remaining boundary |
| --- | --- | --- |
| Nine core tables, including every recorded row and `xmin`, equal the last journey snapshot after application stop | The three runtime snapshots pass the same shape/content checks and match each other | `after-stop-2` contains only `kind` and `clients`; it has no row snapshot |
| Final schema, server ID, zero unrevoked sessions and all eight session records equal `after-journey-2` | The last full runtime snapshot establishes these values before stop | Their post-stop values were not queried |
| Exact database/role OIDs, cluster identity, read-only repeatable-read transaction and expected observer context | Real prestart use proved the shared numeric OID projection; all six saved runtime identities match | No final sealer transaction existed, so its identity and timing cannot be reconstructed |
| No other client backends at final observation | `after-stop-2` lists only its own observer; the application backend count is zero | This is the earlier runtime observer's observation, not the missing final observer |
| Final frontend/backend closure and exact infrastructure identity around that query | Runtime readers closed; later independent preservation proves the original resources closed | Original final-query execution and surrounding checks did not occur |

The runtime and final SQL builders use the same nine-table row/`xmin`, schema,
server ID and session projections. The final contract adds a new observation
after stop and requires equality with the prior snapshot. Successful earlier
queries and later process disappearance cannot establish that unobserved equality.
The existing evidence therefore cannot honestly mark the original final SQL
contract, or the original sealer execution, as passed.

## Closed PostgreSQL archive lineage

The preserved physical cluster is:

`/opt/goby-test/m6-systemd-install-20260915-r04/private/failure-preserve-retained/closed-postgres-private.tar.gz`

It is 6,950,651 bytes with SHA256
`cddb636d2651c06466d12eb62c13f9fce9883239513cb9c5b994f25a368beea9`.
Both new archive reads match this pin. Its lineage is bound by the original
failure-preservation receipt, SHA256
`92eef8417eaa2819bd2199fe63fb4396c65f79db171a34844c78bcaad84df16a`,
and the independent preservation readback, SHA256
`e9d51b8c88fcb0f7432dfd40e71d2c1b766dc65f8b46a18f8dd1e6781ed447bf`.

The supplement confirms the preserved PostgreSQL normal exit code 0 and a
shutdown-completed log with no PANIC. That log is 1,392 bytes with SHA256
`cfe807beb52067af83e44bc219352100316b1c950da702022114819bc6ad68ff`.
The archive has 1,488 regular files and 30 directories, totaling 50,863,469
uncompressed file bytes. Member paths are unique and safe. The physical layout
includes `tree/data`, `tree/socket`, `PG_VERSION` and `global/pg_control`, and no
`postmaster.pid`. Configuration and a private initialization-password member are
retained; their contents were not inspected in the metadata review or published.
Original archive payload readback remains the earlier independent proof.

This establishes a concrete archived state that can be investigated. It does not
yet establish its logical catalog contents or physical database integrity.

## Smallest proposed new proof

Assess one fresh, isolated physical PostgreSQL copy of the closed archive. This
would use zero Goby starts and zero business HTTP requests. Do not reuse the
consumed r04 installation, fixture, units, actors or source32 recovery inputs.
The original archive stays immutable and private.

Reuse the resource and closure definitions already demonstrated by
[isolated infrastructure](isolated-restore-infrastructure.json) and
[final fixture disposal](isolated-restore-disposal.json): an independently owned
namespace and root-managed bounded PG volume; exact process, invocation, cluster
and mount identity; naturally closed read-only observers; normal PostgreSQL
shutdown; ordinary unmount; and namespace closure. Their historical execution
receipt hashes are `c0bbc8730f7548d159840ed2f19316f772a8223c3f0b8577d7f66712ab713f10`
and `0fb6cde172d0a0127cc40dc144d7be72b84d538755cdb671e9261b5dd84ced23`.
Reuse their reviewed lifecycle operations with fresh bounded inputs, not their
consumed identities or application/native-restore workflows. Those results do
not themselves verify extracting or starting this physical archive.

Before execution, bind the complete archive member manifest and source pins,
review safe extraction into an exclusive private copy, and verify compatible
PostgreSQL 17 tools and fresh capacity. Derive the copy's configuration and
endpoint explicitly so archived absolute paths, socket locations or startup
options cannot reach the old fixture or protected installations. Any necessary
path/configuration adaptation is confined to the copy and declared. The private
initialization password and application master-key contents need not be read.
Do not run `initdb`, logical import, migration, application startup or ordinary
catalog maintenance against the preserved physical data.

One bounded read-only repeatable-read transaction can then reuse the final SQL
payload to compare the copy's nine-table rows and `xmin`, all session fields,
schema and server ID with the pinned `after-journey-2` snapshot. Preserve the
cluster/database/role identity; explicitly rebind the copy's new process, path,
port and namespace rather than claiming they equal the old live context.
Require no other clients, natural observer closure and complete owned resource
closure. PostgreSQL startup and shutdown necessarily write operational files in
the new copy; that does not authorize changes to the original archive or product
rows. Unexpected archive/startup recovery requirements stop this bounded proof
for reassessment rather than authorizing repair or additional attempts.

A passing result would establish that the preserved, normally stopped r04
physical database exposes the same declared logical state as the last journey
snapshot under the documented copy procedure. Together with the retained r04
runtime, HTTP and closure evidence, it can support an explicit independent
decision about installation acceptance based on archived terminal state.
It cannot recreate the original final sealer transaction, its timestamp,
PID, original infrastructure continuity, or an observation that never occurred.
If the acceptance requirement specifically demands that original live sequence,
it remains unfulfilled; do not relabel an archive comparison as that sequence.

This document assesses that next proof only. No copy was extracted, no PostgreSQL
instance was started, and no SQL was executed. Preserve the completed package
build, M5 regression and r04 runtime without repeating them for this evidence-only
gap. A new scope and review of its exact inputs and failure closure are required
before the proposed archive comparison can execute.
