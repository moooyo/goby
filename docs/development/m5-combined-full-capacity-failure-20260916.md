# M5 combined full attempt: capacity rejection

The actual M5 full adapter exited naturally with `insufficient_capacity` and
exit code 1. Its failure and owned-resource closure passed independent review.
The [record](m5-combined-full-capacity-failure-20260916.json) pins input, adapter,
execution, closure, outer dispatch and the independent review for
`/opt/goby-test/m5-combined-full-195824c47450`.

The input binds the complete 5459-file S1 source at `138522b`, the successful
frontend command receipt and original contribution sidecar. Source-member
readback passed. The internal gate then recorded 60 samples and 59 waits over
118,022 milliseconds, all below the unchanged 4 GiB availability threshold.
Memory available was 4,140,498,944 bytes first, 2,190,258,176 minimum,
4,247,388,160 maximum and 2,917,715,968 last. Every internal root-disk sample
met its space requirement.

The outer gate had briefly met the memory floor before dispatch. That separate
observation was not a reservation. Adapter RSS/peak was not recorded; the
memory difference establishes no particular process or filesystem consumer as
the cause. The outer tool's successful completion only means its wrapper
finished. The saved child result and wait status remain exit 1.

No worker started, no Go package or ordinary/embedded build ran, and no volume
or archive was created.
All 27 commands, 54 raw streams totaling 11,670 bytes and 29 recorded PIDs
closed. The original stream records held paths/lengths; hashes added by the
saved closure readback were independently matched. Protected state stayed
exact and the shared lock was unlocked/closed.

The earlier `preflight-capacity-rejection-01.json` remains
`not_dispatched` with `inputConsumed:false`. The subsequent actual failure
consumed the scope; it must not be retried or have its 4 GiB gate relaxed.
The accepted storage prerequisite and frontend build retain their outcomes.
Combined product verification still needs a fresh owned scope/input when
capacity meets both gates. No Go result or build acceptance follows from this
failed admission.
