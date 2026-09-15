# Second systemd installation attempt

Status: the attempt failed during the first application configuration check,
before any HTTP request. All three owned services subsequently closed. Failure
preservation archived PG/evidence, retained the five directory trees and five
installation copies, removed the owned installation and unmounted PG. Independent
readback passed: two archives, five directory trees, five retained copies and all
resource/protected-state boundaries matched. See the
[actual result](systemd-installation-second-attempt.json). No further installation
attempt is admitted.

The earlier approval followed independent static review, remote read-only
preflight, six synthetic close-routing cases and fifteen synthetic authority
checks. The eight frozen sources and preparation input remain bound by the
[execution decision](systemd-installation-second-execution-decision.json).
The [initial preflight](systemd-installation-second-preflight.json) retains its
earlier not-yet-admitted state. Both are historical prerequisites for this
consumed attempt, not authority to replay it. Reviewed on 2026-09-15 from source
checkpoint `5e53ddc`; installation acceptance remains open.

The runtime captured `/usr/lib/systemd/systemd-executor` with root credentials
while `Type=simple` was entering the service. Its later saved failure-cleanup
observation showed the same PID, process start and invocation executing the exact
installed Goby binary with UID 995, GID 986 and the required hardening. The
controller had frozen the intermediate configuration as immutable ownership;
its cleanup and the normal sealer then rejected the later executable identity.
Both original failures remain unchanged. Exact unit and lifetime continuity
plus the actual final executable/profile authorized the separate owned-service
closure. The APP cleanup retained a zero exit result while referenced; it does
not make the original runtime or installation pass.

A prospective correction waits within a fixed startup window for final process
configuration before freezing it, while keeping stop authority tied to the
owned unit and process lifetime. The shipped unit and final nonroot requirements
stay unchanged. Those new candidate sources have not been remotely verified or
admitted; reassess them before selecting another scope.

## Scope and intended acceptance

Use the fresh evidence root
`/opt/goby-test/m6-systemd-install-20260915-r02`, fixture root
`/opt/goby-m6-install-fixture-20260915-r02`, and infrastructure unit prefix
`goby-m6-install-20260915-r02`. Keep evidence in root-owned 0700 directories and
0600 files. E12 and the two stopped control-service cases remain immutable.

Reuse the exact internal amd64 package with archive SHA256
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`
and binary SHA256
`7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`.
No product source or package changed. The completed build and M5 ordinary
regression retain their original scopes and are not repeated for helper changes.

Preserve the full installation profile in
[the package plan](systemd-package-plan.md): shipped nonroot `goby.service`,
standard binary/config/state/cache/log/run paths, unchanged shared goby account,
fresh PG 17 cluster and credentials, independent 768 MiB PG tmpfs and network
namespace, seven real media files, two distinct application starts, catalog/ACL/
UserData preservation, all embedded assets, credential closure and normal stops.
This remains an isolated Linux amd64 installation proof, with core-video,
upgrade, native arm64, host durability and whole-M6 gates open.

## Reviewed changes and retained prerequisites

The support/runtime/seal copies contain the reviewed reference integration from
[the prior checkpoint](systemd-stop-integration-verification.json). Their function
and class ASTs match that verified source after scope rebinding. Unit references
remain held through typed exit capture and persistence; cleanup after file
capture failure cannot qualify as a normal accepted stop. Saved records retain
strict identity/type/time binding and failure classification.

The preparation, media and unit logic also retains its previous function/class
ASTs, except that preparation's two tool-evidence paths explicitly use the old
read-only tool scope. Actual tool file hashes, sizes and metadata are rechecked;
the old version-command evidence is reused without relabeling it as a new run.
The new unit-prefix inventory includes both the old date-prefix drop-in and
the more specific r02 drop-in, as systemd applies both ancestor levels.

The first read-only preflight checked all seven helper compilations and global
symbols, the preparation input, sixteen tool identities, package members,
standard-path/unit absence, existing account identities, the protected baseline, and the
eleven D-Bus dependencies without opening a bus. Three invalid actor inputs were
rejected. Sixteen metadata commands closed and the deployment lock was released.
It started no service and made no SQL or HTTP request. Root free space was
6,283,747,328 bytes and MemAvailable was 6,709,420,032 bytes; these observations
must be repeated at the mutation boundary.

## Handoffs and failure routing

Freeze prepare, runtime, seal, the preparation-authority close helper, their
dependency sources and the preparation input before starting the first service.
Deploy the seven original helper basenames under the new `E/private`; the
prospective `*-referenced.py` basenames are not executable admission paths.
Generate later inputs only from the actual preceding receipts and frozen sources.

1. Preparation may start only the new network anchor and PG. A successful
   `preparation.json` has status `prepared_for_systemd_runtime` and binds the
   generated private context, owned service starts and installed files. Its
   failure path stops its owned infrastructure and retains failed evidence.
2. On preparation success, generate the runtime input and proceed directly to
   the runtime entry check. A successful `execution.json` has status
   `systemd_installation_and_normal_restart_passed_pending_seal`.
3. A complete runtime receipt permits the normal sealer input to bind self,
   support, prepare, runtime, units, media, journey, both prior inputs and both
   actual receipts. The sealer independently reviews the saved journey and
   state, closes owned resources and preserves private archives before acceptance.
   A failed runtime receipt is not a passing prerequisite; a usable bound
   failure receipt is supplied only for the sealer's failure-close path.
4. Runtime can reject before producing a usable execution/context binding.
   For that entry-failure case, the separate close helper must validate the
   successful preparation itself, absence of a runtime intent and application
   directories, inactive owned APP state, exact PG/anchor ownership, boot and
   deployment lock. It reuses preparation's bounded memory show/stop controls
   through `__new__`, without generating credentials or reinitializing anything.
   It stops only PG and anchor, preserves files/tmpfs, prevents a second consumed
   invocation, and never claims normal exit or installation acceptance.

Never chain phases with `&&`, replay a failed input, synthesize an execution
receipt, or leave a live fixture waiting for new operator development. A
verification or preservation failure ends this attempt. Preserve its actual
failure and use only the already reviewed owned-resource closure routes.

## Fixed budgets and completion boundary

Preparation is bounded by 420 business plus 240 cleanup seconds; runtime by
900 plus 120; normal sealing by 900 plus 300. Including two 60-second handoffs
and a final 60-second stop margin, the maximum planned sequence is 3,060 seconds.
The anchor's original 3,600-second lifetime is never restarted or refreshed.
The prepare/runtime/seal lifetime gates require at least 2,400/2,340/1,260
seconds respectively. The preparation-authority failure close gets a separate
fixed 210-second ceiling and replaces further business work.

Require at least 6 GiB available memory and 1.5 GiB root free space before
preparation, with the existing detailed runtime/seal memory accounting and
retention bounds. The protected PGs, candidates and paused M2 mount retain their
original resources. Any changed identity, insufficient time/capacity, unexpected
restart, missing exit evidence or unclosed command fails acceptance.

Only successful independent sealing, archive readback, closed resources and
unchanged protected state can establish this installation increment. Publish
its result and commit the scoped checkpoint after those checks. The overall
M2-M6 goal remains active.
