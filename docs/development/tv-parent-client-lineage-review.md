# Actual successor lineage reader review

The final [component verification](tv-parent-client-lineage-verification.json)
passed 57 checks: 32 closeout cases including the actual saved successor through
the production loader, twelve v3 integration cases and thirteen movie05 replay
cases. The [final controller verification](tv-parent-client-controller-final-verification.json)
passed thirty guards and three actual saved admission/hosting/source checks.
The selected closer is `94910123...`, and the controller is `e47f8d19...`.
These checks issued no HTTP, SQL, browser or service operation.

Real saved-file replay exposed two errors before any new client run. The fixed
admission03 closeout, SHA-256 `090d0442...`, is a root-owned, single-link public
0644 report. Its loader had incorrectly required 0600. Only that exact
path/digest now uses the existing public-report read rule; all other private
file rules remain unchanged. No historical file permission was altered.

Two original seed-cleanup tuples retained a token digest but no credential ID.
The old binding records those IDs as explicit nulls. All six predecessor
kind/digest pairs match exactly one of the fifteen fully identified successor
rows, and all four known predecessor IDs match. Ancestor preservation now
accepts the unique match when the historical ID is explicitly null. Missing or
undefined IDs, missing digests, ambiguous matches and changed known IDs still
fail. Historical bindings and source records were not rewritten.

The first standalone replay remains at
`current-client-lineage-replay-01.json`, SHA-256
`2bab43f6fb5bc04d6c06a1b656aeb3022ae07746dfbf28d6e051b4e7edd38d58`.
It stopped at the public-file metadata check. Component scope02 remains at
`tv-parent-client-component-verification-02/verification.json`, SHA-256
`3c176a544e5dbd79360efad06c1bcbe42b56d5bda58480d331a43b2e69ba34db`;
31 of its 32 closeout cases passed, with the actual successor case exposing the
null-ID assumption. The successful scope03 follows that recorded correction.
All paths are beneath the retained delivery root. No business input was retried.

The production loader itself is now exercised by the saved regression, avoiding
another copy of the file-reading path. Candidate admission05 remains valid;
its product, API observations and seventeen revoked sessions were unchanged
through this tool correction. The next business input is the separately
[reviewed first MP3 scenario](audited-mp3-client01-plan.md).
