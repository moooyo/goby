# Third systemd installation attempt

Status: the actual r03 runtime failed on the SQL observer's OID representation.
The first nonroot startup, 147 HTTP requests, credential closure and APP stop
completed; no second application start occurred. The failed sealer closed PG
and the anchor. Failure preservation archived the evidence, retained directory
trees and installation copies, and released the installation and PG tmpfs.
Independent preservation readback passed for both archives, all five retained
trees and copies, and the resource/protected-state boundaries. See the
[actual result](systemd-installation-third-attempt.json). No new attempt is admitted.

The preceding startup/cleanup corrections passed 20 ownership cases, eight
runtime cases, eight sealer cases and two failure-marker reader checks remotely.
Independent readback passed. The fresh sources and preparation input passed
read-only preflight and independent review. The historical
[recorded decision](systemd-installation-third-execution-decision.json)
admitted this one consumed sequence. The
[controller verification](systemd-startup-transition-verification.json) retains
the exact source/check bindings and their synthetic scope.

Both saved SQL observer markers report the expected database identity, but
PostgreSQL emitted the uncast `oid` value as a JSON string while the controller
expected an integer. The strict comparison failed before recording backend
closure. Independent post-close inspection confirmed both reported backend PIDs
gone after infrastructure shutdown; the original failed receipts remain intact.
The proposed correction casts all five numeric OID outputs to bigint. A new
shared marker/reader contract will be exercised through one real read-only
observer before the first APP start in any subsequently admitted fresh scope.
This candidate is not yet remotely verified or admitted.

The first two failed attempts remain consumed, privately preserved and closed.
The package binary, shipped Type=simple unit and production source are unchanged.
Reuse the E11 archive with SHA256
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`.
The M5 ordinary regression retains its original accepted scope.

## Changed controller behavior

Wait at most ten seconds, within the existing runtime budget, for the final
Goby executable, credentials, inode, namespace and hardening before freezing
accepted process configuration. The observed systemd-executor state cannot
qualify as a final nonroot process. Establish failure-stop authority separately
from the owned unit and start lifetime; retain the original reference-based
normal-exit checks.

An owned submitted start with no observed live PID uses an explicit unit-only
cancellation/physical-closure record. It never invents a process lifetime or
passes normal acceptance. Automatic restarts remain failures; their cancellation
and closure use bounded causality checks. When a later live process is observed,
older pending authority moves to history. A saved runtime stop dispatch prevents
the sealer from submitting a second APP stop. The sealer records failed physical
closure separately from normal exit, including a stopped restart whose live
configuration was not sampled.

All controller cases used explicit external-dependency substitutes. They verify
the changed predicates and routing, not real service behavior, actual signal
timing, HTTP, SQL, D-Bus or installed-package acceptance.

## One fresh execution scope

Use evidence `/opt/goby-test/m6-systemd-install-20260915-r03`, fixture
`/opt/goby-m6-install-fixture-20260915-r03`, and unit prefix
`goby-m6-install-20260915-r03`. Eight helpers and the preparation input are frozen
before the first service starts. Function/class ASTs match the tested candidates
or retained unchanged helpers after scope rebinding. Existing tool evidence
remains at its original E12 paths; actual required tool identities are rechecked.

Retain the full [systemd package acceptance](systemd-package-plan.md): exact
standard installation paths and shipped nonroot unit, new private PG 17 cluster
and credentials, independent 768 MiB PG tmpfs/network namespace, seven real
media files, two distinct Goby starts, bootstrap/login and credential closure,
all embedded assets, catalog/ACL/UserData preservation, normal stops and
independent evidence/resource sealing. No existing candidate or PG is reused.

Run prepare, runtime and seal consecutively using actual preceding receipts.
Their ceilings remain 660/1020/1200 seconds, with two 60-second handoffs and a
final 60-second stop margin. The anchor's original 3600-second clock is never
reset; the 2400/2340/1260-second handoff gates remain. Preserve the 210-second
preparation-authority entry-failure close path. A failed runtime receipt is
only eligible for failure closure, not normal acceptance or automatic replay.

Initial preflight found 5,457,743,872 root bytes free and 6,706,163,712 available
memory bytes; the live mutation boundary must recheck the established capacity,
identity and path-absence gates. Keep the paused M2 mount and protected resources
unchanged. No later attempt is authorized by this scope's decision.

Only the complete real journey plus successful independent sealing and readback
can accept this installation increment. Core video, upgrade, native arm64,
capacity/durability, licensing and the remaining M2-M6 requirements stay open.
