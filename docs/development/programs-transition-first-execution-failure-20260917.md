# Programs successor is running; transition acceptance failed

S2 actually stopped, replaced and started candidate A once, and three GETs
succeeded. It then failed at `after_preservation` with
`diagnostic_file_membership_changed`. Original SSH, caller and CLI exits are 2;
the PowerShell tool invocation returned 1 in session `5509` (`6af88e` to `ef1b62`).
The [checkpoint](programs-transition-first-execution-failure-20260917.json) pins
the publication, consumed input/output and original failure/before/after evidence.

The retained A process is now PID **1907978**, start ticks `34901535`, invocation
`2d8403319f3943dbb3d1da638f33093e`, running binary `ead67c8f...` at inode `1580898`.
Old PID `1648477`, the old runtime envelope and old guard are historical authority.
Do not replay `d0006145...` or repeat stop/replace/start. S1's earlier zero-launch
preflight failure remains historical; it does not describe this S2 execution.

Saved-state review proves one new closed 143-byte diagnostic file and one new
active 1101-byte file, with all nine original logs retained. Other observed changes
match the expected binary/process/lease/listener, `library.refresh_media`
definition and 1244-byte unit-log append. Separate source-author body reading and
old-source binding attribute the closed file to old-A shutdown across the UTC
date boundary, and the active file to successor startup/GETs. The independent
metadata review did not read those bodies. The original strict membership check
rejected the two-file addition; no existing failure or snapshot is rewritten.

The hosting r02 correction had separately passed [three mocked methods](programs-hosting-namespace-r02-components-20260917.md),
then its caller/closer were published and read back. The actual operation still
uses the original S1-path `72c177d4...` operator and `2bbf5e4c...` runtime helper.
The original closer ran once and exited 1: resources true, evidence false.
The metadata supplement matches the new A and protected state and records 78
owned PIDs gone; it is not a fresh SQL lease-grant proof. Final independent
failure/closure supplementation is complete, preserving all original failures
and the running A/backend. Its first version remains incomplete after treating
a PostgreSQL log prefix-comparison field as a physical change; r02 corrected
only saved-field comparison. The PG log was physically unchanged. No successful
preservation, epoch, binding, V4 or client admission was produced.

An isolated finalization worktree is preparing strict UTC log authorization,
an optional V4 finalization descriptor and a continuation/publication program.
Its actual calls remain zero. Preserve the running successor and all original
records; do not handwrite V4 or perform another transition. Complete M5 full and
M2-M6 delivery remain unfinished. This documentation pass only read saved records.
