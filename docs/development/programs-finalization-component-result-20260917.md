# Programs finalization component verification

The fixed source at `24ad7c1dfd49455f2dd723fca4c9940edca78121` passed 21
component methods and one separately counted saved-evidence check on `test-env`.
The original foreground SSH returned terminal exit 0 (`889026`, 5.617984 seconds).
No finalizer, live capture, product test, SQL, HTTP request or application service
action ran in this verification.

The scope is `/opt/goby-test/programs-finalization-component-checks-20260917-9f1b3f72e218`.
The three sequential groups passed 11 Python finalization methods, four selected
admission methods and six Node TAP cases. All names matched the fixed manifest;
none failed or skipped. The original 46/71 suites were not replayed. Python
subtests and JavaScript rejection loops are not additional methods.

The subsequent saved-only reader invoked `load_programs_finalization_inputs`
once and read 33 authorized historical files. Its total actual reads, including
bootstrap sources and inputs, were 5,802,047 bytes. Every explicit read descriptor
closed. It validated the original failure, snapshots, five original action
records, three original GET records, UTC rollover review and prior closure;
it neither captured current state nor produced a new epoch.

| Record in `private/` | Bytes | SHA-256 |
| --- | ---: | --- |
| `source-manifest.json` | 10135 | `df6841c9c826d9f9dc6d8cff3919c27a459c24cdc5a1ff54b608ba5143c11b87` |
| `result.json` | 32858 | `92ba720b32ef18074a13898c71a755bfd8c7a4321c0867dcb8bf3ed816fd89d0` |
| `closure.json` | 27402 | `3a301e2b0b41f8a0dc7507f4b127871648f49e7079aa2f3aa37faec8c5830e5c` |
| `dispatch-result.json` | 2453 | `7756b47e3f9f9fbea923efbc026097396dd6605d6040248bb05df1f92efb2b57` |
| `original-ssh-terminal.json` | 593 | `6de2db0264d115869f4375165cfc91c58933ed717f68472476dff10fbce7d010` |
| `saved-evidence.stdout` | 18032 | `a8d7e9e095dc08a1acd1a49aeca3e60f5007d8d6d8df0653cc22d60e9ad2cd4c` |

The executor reported owned closure and a unit memory peak of 50,692,096 bytes
under the configured 256 MiB limit, zero swap and 25% CPU quota. Independent
saved-output and current physical-closure review is in progress. The original
Windows SSH PID was not recorded; the terminal receipt does not invent it.

Separately, the finalization source and 2916-byte input were published and read
back under `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/programs-finalization-dispatch-20260917-r01`.
Publication `122cc0` exited 0 without dispatching the finalizer. Its manifest is
2632 bytes / `150896a7992da6c9265cf433e3794c254d38174b9c31415bf344719897211563`;
the publication receipt is 6184 bytes /
`2508564c5e12425c6ae229d650685c5b7a1bf2d6d75bc304b4fa8173e1fbe9a5`.

Actual finalization and live client admission remain pending. A stays on the
already running Programs successor; the original failed transition and its
nonzero exits remain unchanged. Complete M5 full/build and M2-M6 delivery are open.
