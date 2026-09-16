# M5 full-storage prerequisite verification

The third isolated storage attempt passed and received independent review on
September 16. This accepts the bounded storage prerequisite; the M5 full
backend/build input is still being prepared. The [record](m5-full-storage-verification-20260916.json)
pins all three original results, reviews, closure records and selected sources.

| Scope suffix | Actual outcome | Retained boundary |
| --- | --- | --- |
| `376dbde4b1b3` | Failed before volume or payload creation; 30 commands and 60 streams closed | The missing-directory classification is inferred from saved order and source. No raw traceback was retained; the failed controller record was not reset |
| `d45b028d1434` | Volumes were prepared, but the payload reported `PermissionError` before commands or sentinels; 72 controller commands and 144 streams closed | Reading PID 1 namespace links from the capability-free payload is a static access conflict. No retained errno, filename or traceback identifies the exact failed syscall |
| `7cb8cc30cba6` | Passed 73 controller and three payload commands, 152 streams and four sentinel writes/readbacks | Three filesystems and distinct actual network/mount namespaces were verified; no product worker ran |

Two small corrections preceded success. Both the profile and probe now create
the exact owned workspace-owner/private directories before validating them.
The host controller then supplies hash-bound namespace identities to payload
admission; the capability-free payload reads its own namespace links and
requires actual separation. Budgets, capabilities and filesystem protections
were not widened.

The final archive contains 11 files and six directories; every file body was
stream-hashed during review. Compiler scratch was deleted before archiving.
Workspace backing was removed only after archive readback. The recorded 79 PIDs,
both cgroups, owned mounts, loop references and backing files were absent;
three original mountpoint identities and empty directories matched. Source,
tool and protected-state comparisons remained exact.

The controller journal reports a 256 MiB peak and successful exit. That peak
alone is not an OOM finding or proof of the full 3 GiB worker's capacity.
The payload used a 64 MiB limit, zero swap, private network/mount namespaces
and strict filesystem protection. No PostgreSQL, Go, npm, product test,
SQL, HTTP or browser acceptance ran. Fresh full-source/input admission and the
actual combined verification remain required. All three scopes retain their
original outcomes and must not be replayed.
