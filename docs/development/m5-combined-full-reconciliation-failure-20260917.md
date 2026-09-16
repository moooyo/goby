# M5 combined full: reconciliation failures

The `fcf3c8f4a474` M5 full attempt failed the Library package and is closed.
Independent review accepts the failure evidence and owned-resource closure,
not a passing full suite. The [record](m5-combined-full-reconciliation-failure-20260917.json)
pins the actual input, execution, archive, coverage and review.

Eleven complete packages passed with 492 top-level and 1993 subtest passes.
Library's 604 raw top-level and 1583 subtest passes are separate partial
evidence; its result includes two failed top-level tests, one failed child and
the original M2 mount-profile skip. The package elapsed time was 1225.047 seconds.

- `TestScanReconciliationMusicMissingInvalidMemberAllowsAlbumRecovery/OldVersionOneMember` failed at `scan_reconciliation_music_integration_test.go:154` after 6.23 seconds; its parent also failed.
- `TestScanReconciliationScanReplacementRetainsThenOriginalResumesWithoutRebind` failed at `scan_reconciliation_scan_integration_test.go:228` after 6.45 seconds.

Both reported `Missing catalog records could not be reconciled safely`.
The saved Go streams contain no race or Go-timeout marker. Adjacent PostgreSQL
ERROR/FATAL entries follow the second failure and do not establish its cause.
Library production code is unchanged from the separately passed `3d6b79b`
source. No root cause or product fix is established.

Seventeen selected new top-level tests passed within the completed packages:
seven user-deletion store, two activity, two schema-29/schema-28 archive and
six diagnostic-configuration tests. They are a subset of the totals above.
Later media/server/transcode packages did not run. The selected server HTTP,
real-FFmpeg and diagnostic-owner tests, plus the prepared server-runtime,
conversion-reservation and Linux media groups, remain unexecuted. Neither build
ran, so release `--frontend-contributions` and built-package 59-asset equality
remain unproved by this scope; the original frontend/S1 evidence is retained.

The adapter exited naturally with 1 and the worker reported `test_failure`.
The outer run took 1804.126 seconds with no signals. Independent review parsed
24 Go streams and checked 128 streams from 64 outer commands. All 68 recorded
PIDs, owned PostgreSQL, cgroup, three volumes, loop/backing resources and lock
closed; protected state remained exact. Observed worker peak was
2,471,272,448 bytes against a 3 GiB cap, which establishes no OOM cause.

A focused diagnostic of the two failures is being prepared and has not run.
The old `195824` capacity failure and accepted frontend/storage results remain
unchanged. Programs retains its independently passed artifact and prepared
capture02 source, input and r02 dispatcher: its capture depends on coordinating the shared
resource window, not on M5 full passing.
