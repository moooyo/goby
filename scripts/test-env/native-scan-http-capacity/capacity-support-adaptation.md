# Native capacity support adaptation

Status: static source adaptation only. No local or remote verification, fixture
creation, unit action, or business request was performed for this adaptation.

The source is `capacity-support.py`, 43,506 bytes, SHA256
`4e93fabe713d68b918a44ffd96a4c96c4d153430440480a8f840f8a56efc86d0`.
Its baseline is `.git/m6-install-r04/m6-systemd-install-support.py`, 47,122
bytes, SHA256
`8796a8ea8dac42004aae9df92fd56c204b543aa964f246a2eae343460f079c37`.
The baseline was read directly; the comparison was a textual diff, not a test
or an AST equivalence result.

## Scope

- Evidence: `/opt/goby-test/native-scan-http-capacity-20260915`.
- Fixture: `/opt/goby-native-scan-http-capacity-20260915`.
- Units: `goby-native-capacity-20260915-app.service`,
  `goby-native-capacity-20260915-postgres.service`, and
  `goby-native-capacity-20260915-net.service`.
- All three unit files live under `/run/systemd/system`; there is no application
  unit exception under `/etc` and no drop-in.
- Binary: fixture `bin/goby`; environment: fixture `config/goby.env`.
- Writable application directories: fixture `state`, `cache`, and `log`;
  runtime directory: `/run/goby-native-capacity-20260915`.
- Database and application role: `goby_native_capacity`; PostgreSQL port: 25499.

The retained binary SHA256 is unchanged. The generated custom unit is bound by
the preparation source and its private descriptor; it is not the shipped unit
and this helper returns `installationAcceptance: false` explicitly.

## Retained definitions

The following definitions retain their bodies. Their fixed scope globals now
refer to this fixture where applicable:

`reference_api`, `check_stop_reference`, `encoded`, `parse`,
`check_final_observer_identity`, `bootstrap`, `acquire_lock`, `protected`,
`file_pin`, `descriptor`, `pinned`, `check_file`, `pg_mount`,
`check_infrastructure`, `lifetime_budget`, `_runtime_microseconds`,
`anchor_lifetime`, and `app_network`.

The following definitions have narrow, explicit changes:

| Definition | Change |
| --- | --- |
| `application_ownership` | Bind the APP fragment to its fixed `/run` unit path; update unit-file wording. |
| `application_start_cancellation` | Bind both APP snapshots to the fixed `/run` fragment; update unit-file wording. |
| `pending_application_restart` | Bind the APP fragment to its fixed `/run` path; update unit-file wording. |
| `UnitReference` | Resolve all three permitted units under the fixed `/run/systemd/system` root. UID, PID, cgroup, invocation, start and saved exit evidence checks remain intact. |
| `final_observer_marker_sql` | Change only the fixed role literal to `goby_native_capacity`; both OID projections retain explicit `::bigint`. |
| `load_base`, `guard_module` | Rename the isolated module labels; source paths and SHA256 pins remain unchanged. |
| `check_installation` | Require exactly three files at the fixed fixture paths, preserve owner/mode/hash checks, require the retained binary SHA256, and remove the shipped-unit hash claim. |
| `service_properties` | Use a capacity-specific evidence label; still query only the fixed APP. The requested inventory additionally includes `ReadWritePaths`. |

The saved stop-reference schema kind remains `m6-systemd-stop-reference` to
preserve the reviewed typed reader contract. The fresh unit names, file pins,
process tuple and scope establish its new ownership; the schema name does not
admit a consumed installation input.

## Removed definitions

The historical `admit` function is removed in full. It accepted the old
installation input, fixed seven-file journey, package/context assumptions,
preparation status, and capacity gates; none is automatically admitted here.
The now-unused `E11`, `UNIT_SHA`, `JOURNEY_SHA`, and `PACKAGE_SHA` constants are
also removed. There is no new `main`, service launcher or generic command entry.

## Lifetime gates

The original anchor remains limited to 3,600 seconds. Business and closure
allowances are 1,200 and 900 seconds; handoffs are 60 seconds each; final reserve
is 180 seconds. Required remaining lifetimes are 2,400 seconds at preparation
handoff, 2,340 seconds at runtime entry, and 1,080 seconds at closure entry.
The budget functions still read the anchor's original start timestamp and
forbid restart or extension. Preparation's separate 300-second allowance plus
the first handoff gate total 2,700 seconds within the anchor limit.

The PostgreSQL volume check retains the exact 768 MiB tmpfs size and mount flags,
with only the new mount source prefix and fixture path.

## Caller obligations

The new preparation/controller must bind its own fresh source/input descriptors,
context scope, database and role, positive integer OIDs, credentials, tool pins,
package bridge, origin, account identities, capacity and fresh absence proof.
It must retain the initial `preparedServiceProperties` and independently compare
the expected properties from its frozen rendering schema before APP use. An
initial observation is not an independent expected configuration.

The file checker does not decode any environment bytes. Existing guard, base
and D-Bus API receipt paths/pins are unchanged. The protected-state helper uses
the current E9 guard and never calls the stale `base.protected` implementation.
No master/key access was added. Remote compile, changed-contract checks and
fresh read-only admission remain pending under the parent controller.
