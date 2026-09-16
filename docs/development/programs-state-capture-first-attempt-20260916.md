# First Programs state capture: failed and closed

The first actual Programs fresh capture exited 2 with
`programs_unreviewed_key_path`. Its failed result, partial evidence and resource
closure passed independent review. The [record](programs-state-capture-first-attempt-20260916.json)
pins the consumed input, frozen CLI/runtime sources, dispatch, failure and
closure evidence.

The invocation completed 14 read-only SQL frontends, including one lease SELECT.
All were acknowledged with exit 0 and closed process groups. It retained 71
partial files totaling 718,244 bytes, but published no reviewed state,
source-reviewed state, successful capture result or successor epoch. The SQL
acknowledgements do not establish complete capture or new database acceptance;
PostgreSQL backend disappearance was not independently observed.

The pinned `ProgramsSuccessor.file` guard rejects an unlisted `.key` suffix
before its content-opening or hashing branch. The original failure contains a
code and broad stage, but no filename. No specific rejected key path or
generation master is established, and no key body was read or hashed by the
independent review or this documentation update.

CLI PID 1741512, invocation `1964cc43b2a640508adeb0673d4ff125`, and outer
PID 1741509 closed. Seven owned control commands completed successfully; saved
readback recorded 23 absent PIDs and the absent capture cgroup. The failed unit
record is retained. Lock metadata and its saved hash remained unchanged;
resources closed before unlock and descriptor close. Recorded memory peak was
101,261,312 bytes. CLI exit 2 is known; the original numeric outer SSH exit
remains unknown/null and is not reconstructed from caller-tool status.

The input and output scope are consumed and must not be retried. A bounded key
metadata-rule correction is being prepared separately and remains unverified.
It does not alter the frozen 44/28/70 results. After that correction is verified,
any later capture needs a new reviewed scope/input. Captured-state checking,
actual product loading, transition and client journeys did not run. The prior two-SQL
runtime observation/envelope and all M5 results retain their own boundaries.
