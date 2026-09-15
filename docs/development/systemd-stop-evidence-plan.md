# Systemd exit evidence correction

Status: both real control-service cases passed, following independent static
review and remote syntax, symbol, dispatch and invalid-input entry checks.
Independent readback verified the results and closed fixture. See the
[verification receipt](systemd-stop-evidence-verification.json).
Application-installation acceptance remains paused. Reviewed on 2026-09-15.

The E12 failure closeout retained stopped units with zero process-exit fields
and an empty invocation identifier. The installed systemd 257 documentation
states that unit garbage collection discards execution results, and that an
active IPC client can pin a unit. The installed Manager interface exposes
`RefUnit` and `UnrefUnit`. A completed read-only probe confirmed those signatures,
dbus-python 1.4.0 and closure of its private bus connection. This supports a
bounded experiment; it does not establish that references retain every field.

## Fixed experiment

Use `/opt/goby-test/m6-systemd-stop-evidence-20260915` for private evidence.
Create only `/run/goby-m6-exit-observer-20260915/payload.py` and the two matching
`goby-m6-exit-observer-20260915-{ok,error}.service` files in
`/run/systemd/system`. Preserve source copies privately before installing them.
The payload is a fixed Python SIGTERM handler, with expected exit codes 0 and 7.
It uses the existing goby account and a private network, with a 64 MiB memory
ceiling, no restart, a 45-second service lifetime and a five-second stop timeout.
`CollectMode=inactive-or-failed` is specific to these disposable control cases.

Run the cases sequentially. On one dedicated system-bus connection, load the
owned unit, acquire its reference and confirm that the connection appears in
`Refs`. Start once, bind the actual nonroot PID, executable, cgroup, namespace,
invocation and start timestamp, then observe its real readiness journal event.
Submit one stop command and retain typed terminal properties while the same
reference is held. Require the original execution PID/start timestamp, exit
code 0 or 7 as appropriate, the matching manager result, no restarts and closed
process/cgroup. Recheck the terminal snapshot after a short delay.

Record whether the invocation identifier itself remains available; do not
invent an identifier or process exit result. Associate the retained result with
the original PID/start tuple and continuously held unit reference. The normal
case explicitly releases its reference. The nonzero control closes its bus
connection to test disconnection release. Each must then become unavailable to
`GetUnit` without loading it again. An unavailable expected result fails the
experiment; successful stop submission alone is insufficient.

The outer business budget is 120 seconds, with 60 seconds for closing owned
resources and ten seconds for the final receipt. Require a fresh memory/disk
admission and retain the existing seven-unit/three-PG/two-application protection
checks. The fixed process footprint is one test service at a time. Intent and
actual command dispatch are recorded separately. Failure cleanup cannot repeat
a dispatched stop. Close references/connections, remove only matching owned
files and the empty owned directory, reload the manager and verify both unit
registrations are absent. Keep all original and failed evidence private.

## Result and integration boundary

The zero-exit and exit-seven cases both retained the original execution PID,
start timestamp, invocation identifier, exit code and manager result while
referenced. Their exit timestamps fell within the recorded stop-request and
observation windows. The normal case reported `success`; the deliberate
nonzero case reported `exit-code`. The snapshot remained stable after a delay.
Both explicit `UnrefUnit` and private-connection closure allowed subsequent
`GetUnit` to report no such loaded unit. Thirty commands completed; the two
unit files, payload directory, processes, cgroups and references are closed,
with original protected state unchanged.

This experiment starts two tiny control services, zero Goby or PostgreSQL
processes, and makes no HTTP or SQL request. It does not repeat E12, establish
normal Goby restart, or lift the installation pause.

Use the verified bounded reference lifetime around
existing runtime and sealer stop operations. Save the exit evidence before
releasing the reference; later reviewers consume that immutable record rather
than expecting a later query to retain it. Keep the original application
shutdown journal, process/cgroup/backend, asset, directory, catalog and UserData
checks. A missing result remains unknown. Reference acquisition, observation
or persistence failure must leave the owned-service cleanup path available.

The normal PG archive path also needs the already demonstrated precise
`postmaster.opts` exception. Its exact path, regular-file type, UID/GID 103/106,
mode 0600 and single link distinguish that parameter file from master-key files.
All actual master/key content remains outside verification reads and archives.

## Prospective integration checkpoint

The [integration receipt](systemd-stop-integration-verification.json) binds three
revised sources and a separate synthetic-check operator. Remote compilation and
global-symbol checks passed for all four files. Two invalid-input entries were
rejected without valid installation work. Twenty-five synthetic checks passed:
eleven reference contracts, nine saved-record/classification checks and five
stop-failure routing cases. The latter explicitly model receipt ENOSPC, command
failure before and after dispatch, a rejected child show and truncated output.
They require one synthetic stop and reference release, prevent resubmission
after closure, reject a foreign invocation and keep normal acceptance false.
Independent static review of the final delta and readback of the named preflight,
dispatch, result and operator files passed without repeating tests or service
actions. Source hashes, evidence hashes, sizes and private metadata matched.

The actual bounded memory reader previously ran one read-only show against the
absent application unit. Final preflight binds unchanged ASTs for its control
and ownership methods. This supports the PIPE capture and command closure
mechanism, not ownership or termination of a live Goby process. All fault-routing
services, processes, references and stop commands were explicit test doubles.
The old fourteen-check preflight remains tied to its earlier source hashes;
its counts are not added to this final-source result.

These prospective copies have not run a valid installation input and still use
old E12 scope constants. Keep the first attempt and control-service experiment
closed. Next review a fresh scope, complete helper/input rebinding and all three
handoff budgets, then recheck protected state, target absence and capacity.
The existing package binary and completed M5 regression remain reusable while
their bound inputs stay unchanged. No installation acceptance is claimed here.
