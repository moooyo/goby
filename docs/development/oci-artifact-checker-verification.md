# OCI artifact checker verification, September 16

The 12 existing `test_check_artifact.py` methods passed once on `ssh test-env`
for source `18cd4efd117f3314cf5cb46b4736f65795bbdf59`, with no failure or skip.
The [verification record](oci-artifact-checker-verification.json) retains the
source pins, method inventory, raw result and closure references. This verifies
the synthetic checker cases only; no release input, image or deployed runtime
was accepted.

The cases cover valid ELF executable/PIE headers, architecture and strict JSON
fields, external/manifest pin agreement, file/link/size boundaries, input mutation,
existing-output preservation, partial-write retention and bounded CLI errors.
Both source files retained their exact hashes after execution. The suite used
only the Python standard library and small test-owned temporary files; the
oversize case used a sparse file rejected before its contents were read.

Execution was isolated in a private network namespace with a read-only system
and writes limited to its own scope. The recorded limits were 64 MiB memory,
zero swap, 25% CPU and a 90-second runtime maximum. Peak memory was 25743360 bytes.
All 12 methods completed in 0.014 seconds, with process exit 0 and no restart.
The unit was stopped/unloaded, its recorded PID and exact cgroup path were
absent, and temporary/bytecode directories were empty.

Private evidence remains under
`/opt/goby-test/oci-artifact-checker-tests-20260916-2cc5dc68ee3f`.
Root read back the original result, source manifest, output and supplementary
cgroup-absence observation before publishing the summary. Docker, Compose,
image construction, dependency resolution, media/backup/stop behavior and full
M6 acceptance remain unverified by this test.
