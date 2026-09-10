# M5j backup administration UI verification

Status: PASS. Attempt 4 passed TypeScript checking and all 25 browser scenarios on the frozen source archive recorded below.

## Isolation

The UI lane uses a private owner directory under `/opt/goby-test/exec-work-m5j`, an immutable source archive per attempt, and the existing dependency tree at `/opt/goby-test/inactive-dependencies-m5h/node_modules`. The dependency tree is read-only inside each systemd unit. No package installation was performed.

Vite and Chromium share a private network namespace. The only application endpoint is the runner's own `127.0.0.1:19137` listener; the Vite configuration has no API proxy. Every browser administrator API request is intercepted by the backup fixture, and unconfigured routes fail closed. No production or reference service, real database, HBA rule, or real recovery operation was changed.

Resource limits are one CPU, a 2 GiB memory maximum, a 1.5 GiB memory high watermark, zero swap, 192 tasks, one Playwright worker, and bounded execution time. Type checking and browser execution run sequentially. The coordinator reserves the memory window and runs PostgreSQL/age/KDF work separately.

## Scope

- TypeScript `tsc --noEmit` checks the complete administrator source snapshot.
- Playwright runs all 25 `backups.spec.ts` scenarios using the Vite development server.
- Scenarios cover exact UTF-8 passphrases, confirmation, binary imports, CSRF, unknown admissions, complete reconciliation before a new request ID, cancellation revisions, restore planning options, inspection and apply revisions, unknown activation, rollback consent, digest deletion, authenticated attachment downloads without Blob buffering, malformed DTO rejection, session expiry, and mobile keyboard/layout behavior.
- Existing non-backup browser suites require live credentials, dedicated database fixtures, or direct APIRequestContext traffic. They are not safe to run in this pure mock lane. The full backup suite includes regressions for the shared JSON/raw/HEAD request helper, 401/403 handling, App routing, and mobile navigation.
- This lane does not establish actual backup encryption, database restore, startup acceptance, or production bundle acceptance. Those remain separate coordinator checks.
- No local test, build verification, smoke test, or runtime probe was run.

## Attempt history

| Attempt | Source archive SHA-256 | TypeScript | Browser outcome |
| --- | --- | --- | --- |
| 1 | `a486e4ae3c555d7f92cfb1a3258ccc4b26d3bd9c15b4745a63ea6bf3eabd9d17` | Passed; 5.714 s, 695.7 MiB peak, zero swap | Manually stopped after 59.195 s. The runner denied symlinked font assets; strict required-label selectors also needed adjustment. This is not a successful browser result. |
| 2 | `5d5edef0c2c02e8712e773606317e5558fbba2fc56baea406cd9b0d2dd2c344c` | Passed; 5.031 s, 609.7 MiB peak, zero swap | 15 passed, one attachment filename assertion failed, nine not run; 40.598 s unit duration, 1003.7 MiB peak, zero swap. |
| 3 | `5a90693ddae1673d4b9c9fc00520940f96df7fafc08ea1f5248eb68e9180d711` | Passed; 5.147 s, 607.1 MiB peak, zero swap | 24 passed, one mobile initial-focus assertion failed; 60.491 s unit duration, 1004.8 MiB peak, zero swap. Attachment behavior passed after relying on Content-Disposition. |
| 4 | `1694ca79f2043848dfffa0d8df18060841d3b7407c7216e264cfdbdeaab2f0cf` | Passed; 5.633 s, 710.5 MiB peak, zero swap | All 25 passed in 52.9 s; 56.178 s unit duration, 1018.7 MiB peak, zero swap. The creation/import dialog explicitly focuses its first field after entering. |

Attempt 4 owner: `/opt/goby-test/exec-work-m5j/ui-backups-20260910_211817-64a5aad8-a4`.

## Fixes made during verification

1. The runner now allows the exact read-only dependency path so bundled local fonts are served.
2. Test label selectors accept MUI's optional required-field asterisk, and mobile navigation waits for focus restoration before opening a form.
3. Browser downloads rely on the authenticated server's attachment response headers rather than a forced client download attribute. The request remains a native streamed GET after the authenticated HEAD check.
4. The backup admission dialog focuses its first input when its transition finishes, preserving keyboard entry after mobile navigation.

## Final evidence

- `artifacts/typecheck.exit`: 0.
- `artifacts/playwright.exit`: 0.
- `artifacts/playwright.log`: all 25 scenarios passed.
- `artifacts/results.json`: structured Playwright report.
- `artifacts/results/backups-mobile-backup-card-602e9-and-support-keyboard-access-chromium/backups-mobile-create.png`: mobile form evidence, including keyboard focus and viewport overflow assertions.
- Both final systemd units completed successfully. The runner terminates its Vite child on exit, and the cgroup policy collects the remaining child processes. The UI memory window has been returned to the coordinator.
- Production assets were not built or replaced. A final production build is still required after the coordinator freezes the complete implementation.
