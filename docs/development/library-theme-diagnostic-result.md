# Library theme diagnostic result, September 16

The original theme subcase passed once on `ssh test-env` using diagnostic source
`3d6b79b36b6a5050e174f7a152f214b9356608c8`. This is non-reproduction of the retained
full-run failure. It establishes neither a product fix nor a successful complete
Library package or repository regression. The [result record](library-theme-diagnostic-result.json)
binds the exact source, input, worker report, output and resource closure.

The command selected only
`^TestAuxiliaryCatalogChangesDeferredFailureIsQuietAndRecoverable$/^theme$`
in `github.com/moooyo/goby/internal/library`. The worker required exactly one
parent and one child `run` and `pass` event. Both passed, with no failure, skip,
sibling test or reused result. Test compilation/listing took 34.352 seconds;
the test command took 12.306 seconds and the test reported 10.82 seconds.
The fixture, wait, owned transaction, ownership probe and cleanup budgets remain
90/15/20/5/15 seconds. No application build ran.

## Observed phases

Phase timing starts after fixture construction and is not the total test time.

| Phase | Elapsed seconds |
| --- | ---: |
| Expected rejected scan finished | 2.495327152 |
| Recovery scan started | 2.496571728 |
| Recovery scan finished | 3.328808541 |
| Before Store.Close cleanup | 3.330161145 |

The recovery scan finished in approximately 0.832 seconds. Every recorded phase
had `closing=false`, `ownership_lost=false` and an uncanceled fixture context.
The first scan retained its expected auxiliary rejection. These discrete
observations do not explain the earlier package-level timeout or prove any
causal relationship with an OOM event.

## Current host and isolation

Before this run, both candidate applications were found failed with exit status
1 at 10:22:08 CST on September 16. Their invocation IDs, former main PIDs and
start identities still match the September 15 recovery record. The kernel
recorded a global OOM killing `node` PID 1301622 in session 7137 in that same
second. Cause and application/database consequences remain unresolved.

A fresh guard explicitly bound the two stopped applications, retained the
historical seven-unit, three-postmaster, five-file and eight-configuration-hash
protection set, and added the current original-client service/process identity.
The master key was checked by metadata only; configuration bodies were not
decoded or published. Eight negative cases rejected changed application/PG/
reference identities, protected files/configuration and client-authority claims.
No existing application, PostgreSQL, HTTP or database operation was performed.
This guard grants only independent diagnostic preservation, not recovery,
database-integrity, ready-runtime, deployment or client-acceptance authority.

The existing disposable worker was retained with its own network and mount
namespaces, private PostgreSQL and unchanged time limits. Its memory hard limit
was tightened from 3 GiB to 2 GiB, with swap disabled. The separate tmpfs limit
remained 3 GiB, so the new admission floor is their 5 GiB sum. Both admission
checks passed: 5587902464 and 5554819072 available bytes. The worker read back
`MemoryMax=2147483648` and `MemorySwapMax=0`. Root retention and reserve limits
were unchanged; the paused M2 RAM workspace was excluded.

The worker and disposable PostgreSQL exited, the private bind was removed,
the ext4 mount was unmounted, its loop device detached and the tmpfs unmounted.
All owned commands closed, the deployment lock was released, and protected
before/after snapshots are identical. The 15705863-byte private archive retains
the closed PostgreSQL, detached image, logs and reports. Reproducible copied
source, modules, compiler cache and compiler temporary files were discarded by
the existing retention policy. Independent read-only review matched the archived
5348-file source inventory, exact parent/child events, all worker command streams
and protected snapshots. Current process, cgroup, mount and loop observations
also confirmed the recorded closure. The original failed execution receipt and
its archive remain unchanged.

The private execution receipt is
`/opt/goby-test/library-theme-diagnosis-20260916/execution.json`, SHA-256
`b4a08a2d74b0653e82b56f33ef0d38ffd6a02d9db993a1856915858b59f71d59`.
The host observation is `host-failure-observation.json` in that same scope,
1701 bytes, SHA-256
`7c80a74ba7549fb1467faeff679348e20ee392458e06b4b461f07170060a0c1c`.

## Next action

The subsequent [complete Library package](library-package-diagnostic-result.md)
also passed once with the same diagnostic source and original test behavior.
This adds package ordering and accumulated state to the observation. The
[fresh complete repository verification and both builds](programs-successor-final-preparation.md)
are now active. Do not guess a production correction, repeat either diagnostic
scope, or use these results to clear the original full failure. Current candidate
recovery and successor client acceptance remain open.
