# M5 full attempt: HTTP diagnostic test failed and resources closed

Full scope `0add3118b99f` on `9e70e4b` passed 18 complete packages, then the
19th package, `internal/server`, ended with one failure:
`media_diagnostic_integration_test.go:330: concurrent identical start returned 403`
in `TestHTTPMediaDiagnosticDuplicatePostAndInstanceBoundLookup`.
Original SSH session `43043` exited 1 (`91b1e8`). Independent failure-evidence
and owned-resource review are complete; exact pins are in the [checkpoint](m5-combined-full-http-header-failure-20260917.json).

The raw totals are 2,129 top-level passes, one top-level failure, the one original
mount-profile skip and 6,614 child passes. They include the failed server
package's 590 top-level and 1,751 child passes. Server reached its terminal
failure normally; the remaining six packages, both builds and artifact
materialization did not run. These totals do not constitute a full-suite pass.
All 24 JIT-correction/original-reconciliation identities (eight top-level and
sixteen children) ran and passed in this full invocation; they are already
included in the totals.

Subsequent static inspection of the test helper, authorization lookup and actual
Go 1.27.1 `Header.Get`/`Header.Set` source found the request-header mismatch.
The concurrent request assigned a raw map key `X-CSRF-Token`; canonical lookup
could not find it. Ordinary `f.request` uses `Header.Add`, which canonicalizes
the key. Separate commit `137b41bd73ee29b6a8856c63a45a2ca50fdd8bfd`, based on
`9e70e4b`, changes only that test helper to `Header.Set` (four added/one deleted
line). It has not been remotely verified and changes neither product
authorization nor the JIT correction. The original failure remains failed.

Independent closure confirms 64 adapter/45 worker commands, 67 recorded PIDs,
owned PostgreSQL, the cgroup, three volumes, outer group/streams and lock closed,
with protection and source unchanged. The failed systemd unit record remains
`loaded/failed/MainPID=0`; absent owned processes do not imply its deletion.

Next is remote verification of the test-helper correction and the remaining
full/build requirements. Earlier failed and targeted scopes retain their own
outcomes; complete M2-M6 delivery remains unfinished. This documentation pass
read only the two saved JSON records and the local committed diff, with no
archive rescan, test, validator, runtime probe, SQL or service action.
