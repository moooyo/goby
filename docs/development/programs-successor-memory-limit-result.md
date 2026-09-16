# Programs full verification memory-limit failure

The September 16 successor full verification stopped during `internal/recovery`
at its 2 GiB worker cgroup memory limit. The [result record](programs-successor-memory-limit-result.json)
binds the execution, archive, source, commands and kernel observations.

Sixteen packages completed successfully, with 1405 top-level passes and the
existing Library M2 skip. Recovery had 12 top-level raw passes, or 24 pass events
including children, but no package terminal event; these are excluded. No
assertion failure or race warning was recorded. Neither build ran.

At 05:42:38 UTC, the kernel recorded `CONSTRAINT_MEMCG` for the owned worker and
killed `recovery.test` PID 1361777. Systemd recorded `Result=oom-kill`,
`OOMPolicy=stop`, MemoryMax 2147483648 and MemoryPeak 2147557376 bytes. The Go
command returned -15 after 147.062 seconds; its interruption flag is not evidence
that the original 1560-second command or 1500-second test timeout elapsed.

The cgroup aggregate included 1187790848 anonymous bytes and 928874496 shmem
bytes. These are not victim RSS. The victim separately had 1087320064 anonymous,
1658880 file-backed and 18014208 shmem RSS bytes. The tighter limit that fitted
the Library diagnostics did not fit this complete workload.

Independent read-only review matched the source, completed package results,
command streams and interruption. The disposable PostgreSQL, worker, private
bind, fixture loop and tmpfs were closed. All outer commands closed, the lock was
released and protected before/after snapshots are identical. No existing-service
action or database-health claim follows. The failed unit state is retained.

The execution receipt is
`/opt/goby-test/livetv-programs-successor-final-20260916/execution.json`,
58657 bytes, SHA-256
`4c1a6ce91afd94fa00a317dc15a2efd0855fd58eab842d98c53681dab2417e04`.
Its 108591467-byte archive has SHA-256
`3e9af3aaf35f5b78a8c4ae80150e28ad470860143bc46dd1eb76ef42e7884186`.

## Resource correction checkpoint

The [disk compiler-volume functional check](compiler-disk-volume-verification-20260916.md)
has passed with real execution and cache/temporary writes in the isolated unit,
followed by complete owned closure. The first check exposed overlapping systemd
mounts; r03 now selects the visible mount through an opened directory FD and its
mount ID. A [fresh full run](programs-disk-full-preparation.json) has started;
its final suite/build result and closure remain pending.

The full adapter restores the original 3 GiB worker cap, moves `GOCACHE` and
compiler temporary files to a separately bounded 2 GiB root-backed ext4 volume,
reduces RAM scratch to 2 GiB and retains the combined 5 GiB memory floor. Actual test `TMPDIR/GOTMPDIR`
and PostgreSQL keep their existing fixture/RAM behavior. Preserve assertions,
timeouts, all 25 packages, both builds and the 832 MiB package RAM gate.

Preallocate the disk image, format without discard and verify retained block
allocation. Count the unallocated image in preparation admission and avoid
counting it twice afterward. Keep the existing separate archive/artifact
retention budget. Compiler paths, write permissions, independent loop identities
and every failure/closure path need matching changes. Compiler cache is excluded
from archives; its owned backing file can be retired only after verified closure.

This is infrastructure verification, not a passing full rerun or a guarantee
of capacity. Do not modify or replay the consumed input, combine scores from different attempts,
relax test budgets or infer a production defect from this resource interruption.
