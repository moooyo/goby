# Complete-source Programs regression and build result

The complete frozen source passed all 25 packages with race instrumentation:
**2309 top-level passes, 7577 subtest passes, zero failures and one declared
mount-profile skip**. Independent review parsed all 39739 Go JSON events and
checked the raw command streams. The two inert mount-helper returns do not
establish mount-scenario coverage. Exact evidence, source and artifact pins are
in the [result record](programs-complete-source-full-result-20260916.json).

Both required builds succeeded: the ordinary Linux amd64 binary and the embedded
administrator systemd package. All five package files, the complete source
mapping and the original 57 administrator assets matched their recorded bytes
and hashes. No frontend rebuild ran. The nine previously omitted fixtures are
present, and both formerly failing server tests, including the eight trigger
subcases, passed in this one full run.

Independent resource and materialization review confirmed all 55 outer and 55
worker commands closed, the owned PostgreSQL and unit ended, both loops detached,
RAM and ext4 mounts closed, the compiler backing file was deleted, and the lock
was released. Protected state matched exactly. The four deployment artifacts
and source bridge were copied out and read back, totaling 46022720 bytes. Root
also independently hashed the retained archive and all four copied artifacts.

This closes the frozen-source regression/build step. The historical Library
failure did not recur, but no product root-cause correction is established.
Earlier failures and partial scores remain unchanged and were not combined.
The declared mount profile, application recovery, current authority binding,
direct A transition, client acceptance and main promotion remain separate.
