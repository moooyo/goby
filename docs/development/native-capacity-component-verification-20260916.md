# Native-capacity component verification, September 16

The corrected component set passed **47 test methods (14 + 5 + 8 + 20), with no failures or skips**, in a new isolated scope. **M2 remains held:** these results do not prove actual scan overlap or native-capacity acceptance. Exact pins and the eight-file source inventory are in [the checkpoint metadata](native-capacity-component-verification-20260916.json).

| Check group | First scope `99c282f0943e` | New scope `4926398841ad` |
|---|---|---|
| Measurement | 14 passed | 14 passed |
| Closure | 5 passed | 5 passed |
| Failure routing | 8 ran; one failure at `fallback_fails=True` | 8 passed |
| Result reader | Not run after the failure | 20 passed |

When both result publications failed, recording the fallback error incorrectly replaced the terminal status with `failed_pending_owned_closure`. One added statement restores `native_capacity_attempt_failed`. The other seven source files, including all four test files, are unchanged. All eight files in the successful saved manifest match the current worktree; the controller SHA is `f917c5ef3e33dbb3f7308732b15e9abb448aed0494e2630cefaee54937de06b8`.

Independent read-only review matched the result, closure and manifest pins; all seven pairs of raw stdout/stderr; both saved source sets; and the one-line source diff. Raw stderr confirms the reported unittest counts and results. The first failed result remains preserved. Both closure records show empty cgroups, recorded test PIDs absent, and empty scratch directories; the successful unit ended inactive/dead with MainPID zero.

The successful unit used a **128 MiB memory cap, zero swap, 25% CPU quota, 240-second declared runtime limit, and 8 MiB file-size limit**. It had private mounts/network, `ProtectSystem=strict`, and write access limited to its own scope. Its recorded memory peak was **31,723,520 bytes**.

The scope comprises pure/mock routing checks and small real Python subprocess RLIMIT/closure checks. No Goby workload, product/native workload, application SQL, or HTTP ran. This review read saved evidence and file hashes only; it did not rerun tests or inspect live processes.
