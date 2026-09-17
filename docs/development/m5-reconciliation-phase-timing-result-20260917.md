# M5 reconciliation phase timing

The single `30a3e933b9f4` diagnostic passed both selected top-level tests and both
music subtests once. Go reported 15.809 seconds; SSH session `83625` exited 0.
Independent saved-evidence and resource review passed. Exact source, result,
metrics and closure pins are in the [checkpoint](m5-reconciliation-phase-timing-result-20260917.json).

The two auxiliary snapshots consumed 99.2-99.4% of each rounded removal proof
interval, about 2.2 seconds per snapshot. Deletion took about 1-2 ms and final
filesystem revalidation about 3 ms. All observed contexts remained active and
no diagnostic error was recorded.

| Removal case | Proof | Auxiliary before / after | Share of proof | Delete / final revalidate |
| --- | ---: | ---: | ---: | ---: |
| `OldVersionOneMember` | 4438 ms | 2221.497 / 2181.912 ms | 99.220572% | 1.280 / 3.024 ms |
| `InvalidTypedMetadata` | 4453 ms | 2208.520 / 2217.100 ms | 99.385134% | 1.761 / 2.993 ms |
| Scan replacement | 4443 ms | 2195.314 / 2214.641 ms | 99.256246% | 2.063 / 3.364 ms |

This localizes the measured delay to auxiliary snapshots; it does not establish
whether JIT, planning, execution or another SQL/filesystem cause explains it.
No query-plan experiment ran in this scope, and no such result is accepted here.
The original full failure was not reproduced and remains failed. The earlier
[`e1f47328ab38` diagnostic and `255a27096f6d` rejection](m5-reconciliation-focused-diagnostic-result-20260917.md)
retain their separate outcomes and original evidence.

Closure accounts for 64 adapter and 23 worker commands, 67 recorded PIDs, owned
PostgreSQL, the worker unit/cgroup, outer group, three volumes, loop/backing
references and lock. Protected state and source were exact. This diagnosis-only
source supplies no product fix, full-suite, build, release, frontend or browser
acceptance. The documentation update only read saved evidence; it ran no tests,
builds, runtime probes, SQL or service actions.
