# M5f task transaction foundations

Status: **internal foundations verified; scheduled-task product incomplete**.
Verification ran through `ssh test-env` on Linux on 2026-09-10. This increment
adds no HTTP route, scheduler, task definition, database migration, or dashboard
behavior. The deployed service remains the accepted M5e `d5d696f`, schema 18,
probe 6. The [task plan](../research/scheduled-tasks-plan.md) and
[read-only reference](../research/scheduled-tasks-reference.md) describe the
remaining integration and compatibility work.

## Implementation

[WithOwnedTx](../../internal/library/owned_transactions.go) gives an internal
repository bounded access to the exact PostgreSQL session holding catalog
ownership. Its callback exposes only `Exec`, `QueryRow`, and `Query`; row
wrappers expose no raw connection or transaction controls. Queries retain the
existing independent 20-second write context once admitted, so an HTTP caller
disconnect cannot destroy the owner session. Callback completion closes all
remaining cursors before commit or rollback. Query and cursor connection loss
permanently fences the old owner; handles retained past the callback cannot
write or read again. Panic and Goexit retain their control flow after cleanup.
This is an interface for trusted internal SQL, not a SQL sandbox.

[CheckAdministrator](../../internal/identity/administrator_authorization.go)
validates current authority inside the caller's transaction. Native operations
accept administrator-cookie credentials; the Emby audience accepts current
administrator logins or complete application-key principals. Ordinary actors
lock account then credential. Keys lock parent credential, sidecar, and client
context. The helper checks before and after acquiring shared locks. Callers
must recheck after business-row waits and immediately before commit; fresh
database time detects natural expiry while locks are held. It provides no
self-revocation or system-actor exception.

Two existing private authorization helpers only change their parameter type
from `pgx.Tx` to the narrower query interface; their SQL and authorization
semantics are unchanged. Future task repositories need an explicit adapter
between the context-free owner interface and `AuthorizationTx.QueryRow`.
The adapter must preserve the protected context and return only the `Scan`
interface. These foundations are not yet integrated into task HTTP handlers.

## Remote verification

Both commands ran from `/dev/shm/goby-verify-m5f-20260910`, using the protected
test environment and isolated PostgreSQL cluster on port 15432. They did not
use the deployed service database on port 5432.

| Command | Top-level tests | Result |
| --- | ---: | --- |
| `go test -race -count=1 -json -p 2 ./internal/identity -run '^TestCheckAdministrator'` | 7 | Passed; zero skips or race findings |
| `go test -race -count=1 -json -p 2 ./internal/library -run '^Test(WithOwnedTx\|Owned)'` | 14 | Passed; zero skips or race findings |

The owner suite covers real connection termination and successor ownership,
caller cancellation, unconsumed results, callback rejection and panic,
transaction reuse, late cursor failures, handled missing rows, and access after
callback completion. The authorization suite covers audience separation,
mixed identities, incorrect key contexts, revoked/disabled/demoted/expired
actors, revocation during a lock wait, locks retained until caller commit, and
rollback after natural expiry following a write.

The [authorization report](m5f-administrator-authorization-tests.json),
[owner report](m5f-owned-transactions-tests.json), and
[source-bound evidence](m5f-task-foundation-evidence.json) preserve the results,
log hashes, eight changed/new source hashes, and deployed process identity.
The toolchain is Go 1.27.1 in the test environment. An initial evidence assembler
queried the ambient SSH environment outside the module and reported Go 1.26.7;
the retained correction records the version selected in the actual test
environment and working directory. Test logs and outcomes were not rewritten.

All **21 new top-level tests** passed. This was targeted foundation verification,
not a new full-suite or release acceptance run. M5e's preceding 1085-test full
race result remains evidence for its deployed increment. No media, task-run,
browser, scheduler restart, migration, GPU, or main-service deployment acceptance
was performed for these unused foundation APIs.
