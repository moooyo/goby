# Programs actual saved product gate accepted

The second saved-file attempt completed `runtime.load_programs_product` once,
using the default `read_programs_bytes` and the exact runtime helper covered by
the [152 component checks](programs-review-status-verification-20260917.md),
integrated at `8b1dead`. It accepted the 30,701,500-byte binary, 5,405 source
manifest entries, 57 frontend assets and 64 matching frontend source files.
The [checkpoint](programs-saved-product-second-attempt-20260917.json) pins the
actual output and independent composite acceptance/closure records.

The original caller remains `failed_retained` with
`resource_properties_changed` and SSH exit 2. `systemd-run --wait` combined with
`RemainAfterExit=yes` kept waiting after the successful main-process exit.
Root saved `MainPID=0`, `ExecMainStatus=0`, `Result=success` and the required
resource properties, then stopped only the owned unit with exit 0. The caller
subsequently rejected the garbage-collected unit's properties. Its failure and
bytes are preserved; no loader replay was used to obtain acceptance.

Independent composite review accepts the actual product result and closure.
Both recorded PIDs, the caller group and the owned cgroup are absent; source
and input files remain exact. Peak memory was 643,825,664 bytes under 1 GiB,
with swap disabled. The earlier successful `--check-captured` ran only once.

This closes the immutable saved-file product gate only. The internal package
profile and original mount-profile skip remain. Transition/recovery entry review
and concrete admission/client inputs are next; the typed episode/subtitle
framework already exists. The current `run()` captures fresh before-state
before stopping A, so capture03 is not needed to refresh a label. A real recovery
input can only follow a real failure. No startup, transition, v4 or client result
is accepted, and the technical hold remains.
