# Complete Library package diagnosis, September 16

The complete `internal/library` package passed once on `ssh test-env` using
diagnostic source `3d6b79b36b6a5050e174f7a152f214b9356608c8`.
The [result record](library-package-diagnostic-result.json) binds the execution,
source and archived output. The compiled inventory contained exactly 607 unique
Linux top-level tests: 606 passed and the existing
`TestRootBindingFullScanMountNamespaceHelper` profile case was skipped. All 1584
subtests passed; there was no nested skip, failure or race warning.

The actual command ran the complete package without a `-run` filter, with
`-race -p=1 -parallel=1 -count=1 -timeout=1500s`. It completed in 1076.783 seconds,
exit 0, with empty stderr. The deferred-failure parent took 21.33 seconds; its
theme and extra children both passed, in 10.55 and 10.78 seconds. The original
fixture/phase/ownership/cleanup budgets and assertions were unchanged.

The source's 5348 entries matched the frozen manifest. The worker verified an
actual 2 GiB memory limit and zero swap, with the same private network/mount
isolation, 3 GiB tmpfs, 512 MiB ext4 image and declared M2 exclusion used for the
preceding single-case diagnostic. All 20 worker commands completed successfully.
The disposable PostgreSQL and worker exited; the private bind, ext4 mount,
loop attachment and tmpfs were closed. All 44 outer commands closed and the
deployment lock was released. The explicitly stopped-candidate protection
descriptor matched before and after, including all protected units, postmasters,
files/configuration hashes and the original-client process identity.

Independent read-only review matched the source, 607-name compiled/run/terminal
inventories, raw command streams and before/after protection snapshots. Current
process, cgroup, mount and loop observations also matched the closure. The
104266653-byte retained archive remains under
`/opt/goby-test/library-package-diagnosis-20260916/retained/focused-private.tar.gz`,
SHA-256 `26b311a1c21b16d31c675f33e8bec4ac403238ebdcf193a4ba7f17f6444042db`.
The execution receipt is `execution.json` in the same scope, SHA-256
`db7f595374ae8431851560d398b048a0ca1170c5f2113e3ec30e3cb0d6d5ba05`.

The original failure was not reproduced in either the single case or this
complete package. No production correction or root cause is established, and
the original failure remains unchanged. This result does not combine with old
partial-package counts to create a complete repository result. The next gate is
the [fresh 25-package verification and both builds](programs-successor-final-preparation.md).
The M2 profile gap and current candidate recovery/client acceptance remain open.
