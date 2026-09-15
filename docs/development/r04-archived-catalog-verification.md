# R04 archived catalog verification

Status on 2026-09-15: **the corrected copy passed with independent review;
the internal amd64 systemd installation gate is accepted**. The first copy
remains failed and closed. This supplements the
[r04 evidence resolution](r04-installation-evidence-resolution.md).
It does not replay the fourth installation or create a fifth installation.

## First fixed admission and retained failure

Use [verify-r04-archived-catalog.py](../../scripts/test-env/verify-r04-archived-catalog.py),
41,596 bytes, SHA256
`70d08bd0bd8206fb8f92231920a374ac2db8e2ded2b64b43538e37736e187094`.
Its fourteen targeted remote checks passed after preserving the first
metadata-check failure caused by the `postmaster.opts` name. The correction
permits only the exact declared configuration members to be hashed and omitted;
unknown master/secret/key names remain rejected.

The passing dispatch is
`/opt/goby-test/review-resume-20260915-r04-archive-checks-02/private/dispatch.json`,
SHA256 `4ede8fbaadbfc5194866ae5d16b8f646e2b6c3fe5b091e3769495ae82a218f72`.
These checks used synthetic fixtures, finite Python child processes and saved
archive metadata, with no PostgreSQL start, SQL input or mount.

The fixed input is
`/opt/goby-test/review-resume-20260915-r04-archive-checks-02/private/execution-preparation/input.json`,
6,631 bytes, SHA256
`54c58e4b9ef6a76c9e573acfeefacfb24c95561852026c7ec625b4eb3c890942`.
Its independent input review is
`/opt/goby-test/review-resume-20260915-r04-archive-checks-02/private/input-review.json`,
11,604 bytes, SHA256
`62fcfd5f41f22410835f83e346e661117c252812e275093a044cbea8eaea7863`.
The review permits only this input once. Entry reacquires the existing lock and
rechecks the source, tools, seven protected units, five selected protected files,
archive/snapshot pins, current capacity and both target paths' absence.

Preparation observed 4,955,279,360 free root bytes and 6,662,295,552 available
memory bytes; these are not reservations. Master-key content is never read.
The original archive and configuration remain unchanged; copied configuration
members are omitted after hashing, and the initialization-password member is
omitted using metadata only.

## Scope and limits

- Evidence: `/opt/goby-test/r04-archived-catalog-20260915-copy0001`.
- Disposable copy: `/opt/goby-r04-archive-20260915-copy0001` with one new 512 MiB
  tmpfs. Preserve empty underlying directories after ordinary unmount.
- One PostgreSQL 17 `--single` child, one fixed read-only SQL transaction and
  tagged hexadecimal COPY output. No listener, service, anchor, D-Bus, psql,
  initdb, repair, migration, Goby process or business HTTP request.
- Child limits: 512 MiB address space, 30 CPU seconds, 32 MiB per data file,
  separately bounded 1 MiB stdout/stderr and a 45-second wait. Preparation and
  closure have monotonic budgets of 180 and 60 seconds; these are not an external
  hard wall-clock guarantee.
- Preserve the child PID with WNOWAIT until the owned process group is closed;
  never signal its PGID after reaping. TERM/INT/HUP enter bounded owned cleanup.
- Require clean `pg_controldata` before and after, natural exit zero, complete
  error-free output, exact nine-table rows and xmin, all eight revoked sessions,
  schema and serverId, ordinary unmount and protected metadata readback.

The [PostgreSQL 17 manual](https://www.postgresql.org/docs/17/app-postgres.html)
describes single-user operation and its implicit superuser identity. This mode
does not reproduce normal interprocess locking or the original sealer's role and
transaction timing. The result can prove the archived logical terminal state;
it cannot claim the missing original final observer ran.

## Result handling

Preserve any first failure and close only the new copy. Do not retry this input,
restart Goby, repair the archived cluster or broaden the query to obtain a pass.
Review the actual result and resource closure independently before deciding
which installation assertions the supplemental evidence closes. The original
failed seal and its unexecuted final SQL transaction retain their original status.

## First-copy result and corrected second admission

The first PostgreSQL child exited naturally with status 1 before producing any
COPY output. `umask 0077` reduced the requested fixture mode 0711 to 0700, so UID
103 could not traverse the root-owned parent directory. A complete stdin write
does not establish that the SQL transaction ran. Child, volume and lock closed,
and all selected protected metadata and original pins remained unchanged.

The failed result is
`/opt/goby-test/r04-archived-catalog-20260915-copy0001/private/result.json`,
SHA256 `d1a0f750dbd5299ad714948583d852815b71a094bb5ae73a0424c63f5e572a95`.
Its independent failure review is `private/independent-failure-review.json` in
that same scope, SHA256
`773ca7dff4313a17710db0c3ce6194c180695d875a0102c8ff1ca5789dc6707f`.
The scope and input are consumed and unchanged.

The correction explicitly sets mode 0711 through a verified directory file
descriptor after exclusive creation. PG data retains mode 0700 and UID 103.
Fifteen remote checks passed, including an actual UID 103 leaf process that
fails on the old umask-masked directory and succeeds on the corrected directory.
This test starts no PostgreSQL and uses no real database data.

The corrected source is 42,409 bytes, SHA256
`d7e6d972f7af69d50422958a2070dbd7465b4a5552c635a2d5684fde7044dcfc`.
Its verification scope is `/opt/goby-test/review-resume-20260915-r04-archive-checks-03`:

| Artifact under that scope | SHA256 |
| --- | --- |
| `private/dispatch.json` | `7e4561913e5b247b387bda76b972812facb1c5a1cb405da462a6fa3c2da0d3d2` |
| `private/execution-preparation/input.json` | `1929eb18ce9077524c20d2c85bbdc904c732fdc8031742da40f5ad370b36b459` |
| `private/execution-preparation/result.json` | `881eb490d52e1c5b0d4d7d413abfdd200c330c338b37643d2fd02b7ea52375ce` |
| `private/input-review.json` | `5deb7468036517bbf76a4beebecc7aa92ac6c7d822b3f72efff07ed9bf89de56` |

The second input changes the scope suffix to `20260915-copy0002`, with fresh
absence, capacity, tool and protection evidence. Its seven unit and five file
checks, SQL, process limits, archive pin and snapshot remain unchanged. The
independent review admits that exact input once; it does not replay copy0001.

## Accepted result and installation disposition

Copy0002 completed in approximately 0.867 seconds. Its natural PostgreSQL exit
was zero. The unique 99,336-byte stdout frame matched the separately saved
parsed result; stderr contained two checkpoint LOG records and no warning/error.
All nine tables' 59 rows and xmin, the eight revoked sessions' declared fields,
schema and serverId matched the original after-journey-2 snapshot. Both control
observations were cleanly shut down. Eighteen metadata/volume commands closed;
the process/group and mount were absent, the lock was free, and the seven unit
and five file protection checks matched.

The actual result is
`/opt/goby-test/r04-archived-catalog-20260915-copy0002/private/result.json`, SHA256
`0ecae4928ae0a566c9b7cbf2691acb34874e5511b05959c4c85c4f2a263e13c6`.
Its independent review is `private/independent-review.json` in that scope,
SHA256 `d8f908c4e435452ad4b2fb532a6a759067130d2bfd9ec6c5a50ed4f6dcae97b3`.
The review reread the actual frame, original snapshot, source/input/archives and
current resource boundaries without another PG/SQL execution.

The [composite installation acceptance](internal-amd64-installation-acceptance.json)
combines this final-state proof with the original two nonroot starts/stops,
complete saved HTTP review and original resource closure. G2 has no remaining
product or safety assertion within its declared internal Linux amd64 systemd
scope. The missing original final observer remains unexecuted; its role/PID/time
are not recreated or inferred. G1, G3, the wider M6 platform/distribution matrix
and complete M2-M6 remain open.
