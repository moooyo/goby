# Four authorized temporary directories removed

The user-approved cleanup removed exactly four PowerToys agentic review-related
temporary directories. No Goby ownership is claimed. The [record](test-env-tmp-memory-cleanup-20260916.json)
pins both the aborted first attempt and successful cleanup.

- `/tmp/agentic-compact-workspace-20260914-s8zABi`
- `/tmp/command-exit-observations-20260914`
- `/tmp/triage-evidence-20260914-fix`
- `/tmp/summary-budget-20260914-fix`

The first reference scan counted its own four `O_PATH` handles and aborted
before deletion. The handles were closed, and that original failed receipt
remains unchanged. The subsequent command exited 0; all four selected paths
were absent afterward. No other directory was deleted, and no process, mount
or unrelated cache was changed.

Observed `tmpfs` use fell by 1,436,913,664 bytes, approximately 1.34 GiB.
The after-sample reported 5,829,570,560 bytes of `MemAvailable`. Its observed
increase of 1,536,581,632 bytes cannot be attributed exactly to this cleanup:
concurrent workloads can also change host samples. These observations are not
a reservation for subsequent work.

This is separate from the earlier [root-directory cleanup and disk expansion](test-env-root-maintenance-20260916.md).
Only saved receipts were read for this documentation update.
