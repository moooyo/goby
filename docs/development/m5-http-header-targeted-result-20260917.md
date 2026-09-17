# M5 HTTP header targeted result, 2026-09-17

The saved run passed **2 top-level tests once each, with 0 child tests, failures or skips**, using the race detector. `TestHTTPMediaDiagnosticsRequireNativeCookieCSRFAndOrigin` took 1.74 seconds and `TestHTTPMediaDiagnosticDuplicatePostAndInstanceBoundLookup` took 1.18 seconds; the selected `internal/server` package result passed in 3.937 seconds. No race, timeout or interruption was observed, and no prior scores were reused.

The execution scope was `/opt/goby-test/m5-diagnostic-http-header-fix-targeted-53ff9b7dac32`. Original SSH session `30627` started in tool chunk `de396b` and completed in `46ecbb` with exit code 0. The actual published source descriptor binds commit `137b41bd73ee29b6a8856c63a45a2ca50fdd8bfd` to the `33f9f1cf1deb` source publication. Exact descriptor, archive, manifest and result pins are in the [JSON record](m5-http-header-targeted-result-20260917.json).

The retained independent review accepted the two selected tests and owned resource closure: 68 recorded PIDs and the owned process groups absent, three owned mounts closed, unit cgroup absent, owned PostgreSQL stopped, private bind removed, lock released, and protected observations and source preserved. The JSON record pins the actual remote independent review and saved-evidence extraction.

These tests use a fake executor owner and HTTP test fixtures. They do not establish real FFmpeg execution, native client acceptance, all server tests, the six packages not reached by the earlier full run, application or embedded builds, frontend or release builds, or deployment acceptance. The earlier full-suite failure and S1 zero-launch Programs failure remain unchanged. No automatic retry or actual Programs transition occurred in this scope.

Independent review checked the saved source binding, manifest, preparation and committed header source; it did not rescan the complete source archive. The original run's archive-member checks remain distinct evidence. This documentation update read saved JSON only and ran no verification or service actions.
