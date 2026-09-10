# Application-Key Device Reference

Research date: 2026-09-10 (Asia/Shanghai).
Status: **bounded reference study complete; original capture failures are
preserved, independent recovery audit and final fixture teardown passed**.

This study follows the [ordinary user-device reference](devices-reference.md)
and [application-key reference](api-key-reference.md). It asks how an application
key's server-device metadata and client contexts relate to the device registry,
and what device deletion revokes. Existing key-session and ordinary-device
observations alone do not establish those cross-credential behaviors. In the
fresh instance, two keys shared one server-device record; deleting it revoked
both credentials, while three metadata session DTOs remained visible despite
rejected authentication. A key also renamed and deleted an ordinary user's device.

## Evidence accounting

This extension adds **189 records** to the preceding 1476, bringing the corpus
to **1665**. The stages are deliberately counted separately:

| Stage | Complete HTTP exchanges | Other records | Total |
| --- | ---: | --- | ---: |
| Guarded existing-instance capture | 61 | One audit | 62 |
| Fresh-instance setup | 16 | One initial connection-refused observation, with no HTTP response | 17 |
| Fresh key/device capture | 109 | None; original audit finalization failed | 109 |
| Independent offline recovery | 0 | One recovery audit | 1 |

The extension therefore contains 186 complete HTTP exchanges, two audits, and
one non-HTTP startup observation. The [teardown report](key-devices-fresh-teardown.json)
is derived operational evidence and is not another corpus record. Failed
constructor/finalization logs remain preserved separately; they do not become
successful HTTP exchanges through recovery.

## Guarded capture on the existing reference

The [guarded recorder](../../scripts/test-env/reference-key-devices.py) used
the existing isolated official Emby Server 4.9.5.0, PID 3131777, through
authorized root SSH on `test-env`. Its new `key-devices-m5e-` dataset contains
**61 complete HTTP exchanges and one audit**, with no incomplete HTTP replies.
It captured **67,399 response bytes in 0.431 seconds**, extending its original
1476-record baseline by 62. Its partial outcome is retained unchanged.

The key/device branch remained read-only. A new ordinary control login supplied
the temporary administrator authority; no old token was used in an HTTP
request. Before any key creation, the recorder checked both the complete
bounded key/device list baselines and direct server-device lookups. Active key
listing was empty, and the device list omitted the previously known server
device. Direct lookup nevertheless returned it:

| Field | Observed value |
| --- | --- |
| `Id` | `"15"` |
| `ReportedDeviceId` | `"ec69ef1cf84140e88489c30326529308"` |
| `Name` | `"Goby Emby Reference"` |
| `AppName` | `"Goby Keys Scope M5d 20260910 01 Alpha"` |
| `AppVersion` | `"4.9.5.0"` |

Both the [reported-ID lookup](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-m5e-server-info-before-0.json)
and [decimal-ID lookup](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-m5e-server-info-before-1.json)
returned `200` and the same existing device projection. This is evidence that
absence from this device list did not establish absence of its underlying
record. It does not establish a universal hidden-device rule or the effects
of creating another application key.

The creation gate required the reported server identity to be absent with
`404` before a new key could be issued safely in this preserved environment.
That condition failed. Creating a key could touch the old server-device record,
so the recorder stopped with `RuntimeError` before attempting key creation.
**Zero application keys were created, zero key-client requests were made,
and neither header-device nor server-device deletion was attempted.** This
partial result does not verify key/device mapping or key revocation by device
deletion.

## Preservation and control cleanup

The [guarded audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-m5e-audit.json)
passes every cleanup and preservation check despite the incomplete research
branch. All 2952 preceding raw/export files, 240 known source paths, and 1958
preexisting private files remain unchanged. The 19 listed baseline devices,
their options, and the hidden device's Info/options are preserved. User
membership and structural/policy fields are unchanged; the control account's
attributed activity update is recorded separately. The reference process is
unchanged, and the empty active-key set remains empty.

The control device, `Id = "30"`, remains as explicitly retained history. Its
only new login was logged out with `204`, then independently rejected with
`401`. The audit records no source writes, user-policy writes, camera uploads,
or encoder requests. Successful cleanup of this ordinary control does not
stand in for a key-deletion test that never ran.

The guarded recorder's remote synthetic safety suite passed **16 tests**.
Its executed source SHA-256 is
`678ae21ca471b3ea77c73484dfdf0d9058e437494954745cd0cf899894e196d9`.
The [test source](../../scripts/test-env/test-reference-key-devices.py) covers
the recorder's bounded ownership and preservation checks. Raw credentials and
responses remain in protected remote evidence; repository fixtures are
sanitized. Capture and functional verification ran only through `test-env`.

## Fresh isolated instance and retained preparation failures

Because the old server-device record belongs to preserved history, subsequent
key/device mutations require an entirely owned fresh instance. The
[preparation operator](../../scripts/test-env/prepare-key-devices-fresh.py)
shares the extracted official package read-only and allocates independent
program data, credentials, output paths, and a private network namespace.
The old Emby reference and deployed Goby service are outside its mutable scope.

Preparation attempt 01 failed before any HTTP request because systemd rejected
its `WorkingDirectory` under `/dev/shm`. Cleanup of the owned resources passed,
and the failure evidence was retained. Attempt 02 reached **READY** with a new
server identity, PID 3496037, and port 18098 inside its own network namespace.
Its setup log contains 17 records: 16 complete HTTP exchanges and one startup
connection failure. Its independently bootstrapped administrator and viewer
logins were logged out and each subsequently denied with `401`.

The operator's synthetic suite passed **28 tests** after correcting a missing
fixture-run field. The operator source SHA-256 is
`1edd854b8f8a4a4aec2debe19f1bf8ddc0d9eb7cc384f1eafb28667d1e12b56b`.
Its preparation evidence remains separate from the subsequent key/device
capture and the final operator teardown.

The first constructor attempt of the
[fresh recorder](../../scripts/test-env/reference-key-devices-fresh.py) stopped
before HTTP because it incorrectly required a single hard link for 201 known,
read-only media paths. No fresh key/device request ran, and the ready service
was not restarted. Capture attempt 2 used a separate output root and the
corrected read-only preservation rule. Its source and **24 passing synthetic
tests** are identified below. All 109 HTTP exchanges are complete and match
the fresh instance's authority; the original capture tree remains immutable.

The fresh server ID was `00475e7660c6499e9065f988188e7a57`. Bootstrap logins
were not reused for study requests. The study created a control login, an
ordinary viewer login, and two independent application keys; every mutation
had a preceding ownership proof or a reserved creation identity.

## Shared server device and client-session projections

The [two-key list](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-keys-created-sibling.json)
has numeric credential row IDs `5` and `6`. Both rows report numeric
`DeviceId: 5`, the same server `ReportedDeviceId`, and `UserId: 0`.
The [server-device Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-keys-client-server-info-0.json)
returns string `Id: "5"` for that reported server ID. Credential row IDs,
device registry IDs, reported IDs, and session IDs are distinct identifiers;
equal numeric values in this sample do not make their roles interchangeable.

Three explicit client-metadata variants create userless Session DTOs with
their supplied device IDs and numeric `InternalDeviceId` values `6`, `7`, and
`8`. The two token-only default sessions use server device `5`. The bounded
[administrator device list](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-devices-after-key-clients.json)
contains the four ordinary bootstrap/control/viewer devices, but neither the
server-device row nor those three metadata identities. Its nonempty list again
reports `TotalRecordCount: 0`.

This establishes what the list exposed, not absence of hidden records. No
Info lookup queried the metadata-reported IDs or their numeric internal IDs.
The [header-device deletion branch](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-header-delete-candidates.json)
was not executed because no positively owned Alpha candidate appeared in the
bounded list. Hidden metadata-device existence and deletion effects remain
unverified. Client traffic also changes selected server-device display fields;
this short capture does not establish a complete cache or metadata-update rule.

## A key can manage the owned ordinary device

The key-authenticated [options write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-key-rename-viewer.json)
to `POST /Devices/Options?Id=4` returns `204`. The subsequent
[options response](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-viewer-after-key-rename-options.json)
and Info projection contain the new `CustomName`. The same key's
[DELETE](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-key-delete-viewer.json)
of the ordinary viewer's device returns `204`.

The viewer's next [protected request](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-viewer-after-key-delete-protected.json)
returns `401`; the two application keys remain authorized with `200`
([Alpha](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-viewer-delete-key-protected-alpha.json),
[Sibling](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-viewer-delete-key-protected-sibling.json)).
The unrelated administrator control remains usable. This is observed device
management by a privileged key, with the deletion effect checked separately
for ordinary credentials and application credentials.

## Deleting the shared server device revokes both keys

After refreshing the positive server-device/key ownership proof,
[`DELETE /Devices?Id=5`](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-delete-owned-server-device.json)
returns `204`. Both unpaged and bounded paged key lists become empty; the
[unpaged response](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-server-delete-keys-after.json)
is not the sole proof. Independent protected requests return `401` for all
five tested scopes: Alpha token-only, Alpha metadata, Beta metadata under
Alpha, Sibling token-only, and Sibling metadata. The
[control request](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-server-delete-control-protected.json)
still returns `200`.

Both token-only default Session DTOs disappear. The three metadata Session
DTOs remain in the control's list before and after the failed credential
probes. Their visibility is a stale projection, not evidence of continuing
authorization. The recovery audit maps each raw token and context to its own
post-delete and final `401` proofs. No WebSocket connection or media stream
was created, so connection termination and in-flight playback are not measured.
Numeric Info after deletion returns an empty `204`; that response alone is
not used as proof of absence.

## Independent offline recovery of audit finalization

The live recorder completed the HTTP sequence and credential cleanup, but
its final audit serialization raised `Secret survived export redaction`.
The [independent recovery audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-recovery-audit.json)
retains this failure and diagnoses it with a synthetic reproduction. Logical
credential labels stored under a generic `key` field were collected as if they
were secrets; the legacy sanitizer did not rewrite dictionary keys containing
those labels. The failed finalization was not an HTTP capture failure or an
actual credential-cleanup failure.

The [offline analyzer](../../scripts/test-env/audit-key-devices-fresh.py)
independently verifies all 109 original exports against deterministic
redaction of their raw records. None requires modification and no raw secret
survives those exports. It writes one new recovery audit, sanitizing that
audit's dictionary keys with collision checks. It issues zero HTTP requests,
launches zero processes, makes zero service-manager queries, and leaves the
original capture tree and failed-finalization evidence unchanged.

Its first recovery attempt correctly refused to treat setup record 001 as a
complete HTTP response. The final analyzer explicitly recognizes only that
initial anonymous readiness `ConnectionRefusedError`, with zero wire bytes
and a later successful public Info response, as a separate non-HTTP
observation. The [five synthetic regression tests](../../scripts/test-env/test-audit-key-devices-fresh.py)
pass after this correction. Recovery changes neither the original failure
record nor the successful-exchange count.

## Final preservation and teardown

The recovery audit reports `cleanupPassed: true`, with ten owned mutations
acknowledged. Both key credentials and all five tested key scopes have final
`401` proofs. The viewer and control study logins also end with `401`; the two
bootstrap tokens were independently mapped, had final `401` proofs, and were
unused in the study. The only new listed device retained before teardown is
the logged-out control, `Id: "3"`; fresh baseline devices `1` and `2` are
preserved.

| Protected evidence set | Files | Result |
| --- | ---: | --- |
| Preceding raw/export files, including the 17 setup records | 3110 | Hashes unchanged |
| Known source paths | 240 | Hashes unchanged |
| Preexisting private files | 2080 | Hashes unchanged |
| Fresh authority files | 3 | Hashes unchanged |
| Preparation-attempt-01 failure evidence | 11 | Hashes unchanged |
| Capture-attempt-01 failure evidence | 3 | Hashes unchanged |

These sets overlap and are not additive. Old device fields, activity/options,
and user structure/policy are preserved; only attributed participant login
activity differs. The old Emby reference and deployed Goby process identities
remain unchanged. Source writes, media/encoder requests, and user-policy
writes are zero.

The separate [operator teardown](key-devices-fresh-teardown.json) then verifies
the owned process is gone, the fresh unit is inactive, its program data is
removed, old preservation checks pass, and evidence remains retained. The
operator teardown is distinct from the offline credential audit and adds no
corpus fixture.

## Executed source fingerprints

| Source | SHA-256 | Synthetic tests |
| --- | --- | ---: |
| Guarded recorder | `678ae21ca471b3ea77c73484dfdf0d9058e437494954745cd0cf899894e196d9` | 16 |
| Fresh preparation operator | `1edd854b8f8a4a4aec2debe19f1bf8ddc0d9eb7cc384f1eafb28667d1e12b56b` | 28 |
| Fresh capture recorder | `39357835081eb068e7a089db7c011acc0e58c9c1676e2c1a0c6367313fd00e80` | 24 |
| Independent offline analyzer | `b93682ccebb97fabb161779a06c7dcbd84ffd3dc498143dd62b0f29be827185d` | 5 |

The fresh-recorder test source hash is
`995bca43caf21716666bac9de40bf6e6a4ef437d2ea902e957fe6b1045079825`;
the offline-analyzer test source hash is
`d6c62939a0713e43520a7728be7bc1d7d844aa220f2b64f1879a9f7356e7e5cf`.
Synthetic tests and recovery ran through authorized SSH on `test-env`; no
local functional verification was performed for this research documentation.

## Implementation implications and remaining scope

Device-registry identity must remain separate from credentials and cached
session projections. The observed shared server-device deletion affects
multiple keys, while deleting an ordinary user's device leaves those keys
authorized. An implementation should revalidate credentials independently of
whether a Session DTO is still visible.

This study did not create another key after deleting the shared server device.
Starting a new application-key device generation on later creation is a Goby
safety design, not an observed reference recreation contract. Header-specific
hidden Info/deletion, device recreation with old keys, complete metadata cache
rules, WebSocket termination, media execution, and full-client interoperability
remain unverified.

At this research checkpoint, the accepted deployed product was M5d `563cd0e`,
schema 16/probe 6, with its separate 1046-test acceptance. The subsequent
[M5e implementation acceptance](../development/verification-m5e-devices.md)
tracks Goby's device API, dashboard, migrations, and deployment independently
from these reference captures. M4, M5, M6, and the complete planned goal remain
unfinished.
