# Library theme failure diagnostic preparation

This branch prepares one targeted observation of the retained Programs regression
failure. It changes only test diagnostics. No product fix or root cause is claimed,
and the failed full-run evidence remains unchanged.

The existing scan waiter now reports its last successful Job status, time spent
waiting and reading, and the context state before and after a failed read. The
original deferred-failure test records a fixed set of phase markers and the
atomic closing/ownership-loss flags before the fixture's Store.Close cleanup.
The first rejected scan's error remains visible. No polling, SQL call, assertion,
timeout, or production ownership behavior is added or relaxed.
Independent static review found no new blocking operation or data race. Phase
times begin after fixture construction; the flags are discrete observations,
not proof of the precise time or cause of ownership loss.

After the coordinated test-environment window is available, run the original
ordinary subcase once with the selector
`^TestAuxiliaryCatalogChangesDeferredFailureIsQuietAndRecoverable$/^theme$`.
Use a new frozen source/input and the existing bounded worker and disposable
PostgreSQL workflow. Preserve the fixture's 90-second lifetime, 15-second wait,
20-second ownership transaction, 5-second ownership probe, and existing cleanup
limits. Compilation and cleanup need their own admitted outer resource budget.
Do not replay the consumed full worker or alter either retained candidate.

Read the phase markers together with the original test and cleanup errors.
Failure to reproduce is only non-reproduction; it neither clears the original
failure nor proves that OOM or request cancellation caused it. Further changes
must follow the observed failure stage.

Both files were formatted with gofmt on `ssh test-env`; no Go test, build, SQL,
service or browser action ran. The source-preparation record is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/theme-diagnosis-source-01/source-preparation.json`
(1,440 bytes, SHA-256
`2a3e76b1697cad2a28a7a0f32d83a8a58da84bdcc452c1ae812a0c5449f2a581`).
The two source hashes are recorded there. Main's Programs source remains
unchanged until this separate diagnostic snapshot is selected for execution.
