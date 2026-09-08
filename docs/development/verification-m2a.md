# M2a ingestion and library management verification

Recorded on 2026-09-09. This increment implements local-file ingestion and browsing. Local sidecar integration, artwork and playback remain later increments; the full project goal is not complete.

## Results

| Check | Environment | Result |
| --- | --- | --- |
| `go build ./...` | Authorized local Windows, Go 1.27.1 | Passed |
| `npm run build` | Authorized local compilation of the locked React/MUI application | Passed without chunk-size warnings |
| `go test -race -count=1 ./...` | Remote `/opt/goby-test/verify-m2a`, isolated PostgreSQL schemas, FFmpeg/ffprobe 9.0.1 | Passed: database 1.130s, identity 14.779s, library 3.683s, media 2.355s, server 15.488s |
| Initial complete real-browser regression | Remote Chromium against the real Goby service | 4 passed in 18.7s; includes foundation, session replacement, library workflow and cancellation |
| Final mobile focus/visibility adjustment | Remote Chromium library workflow | 1 passed in 7.8s; screenshots inspected |
| Final backend ownership implementation | Remote Chromium `libraries.spec.ts` against the rebuilt service | 2 passed in 11.2s; `/readyz` returned 200 after cancellation |
| Service lifecycle | Remote Linux systemd | Rebuilt and active as unprivileged user `goby` |

The final Go verification directory contains the foundation and M2a source, excluding the separate, not-yet-integrated local NFO parser. No test result in this report relies on a skipped PostgreSQL/media fixture or a mocked HTTP backend in the browser tests. Functional, browser and media execution occurred only on `test-env`.

## Media and filesystem evidence

Real ffprobe tests cover an H.264/AAC fixture, accurate file size and stream indices, decimal time conversion to 100ns ticks, format detection independent of filename extension, and original-descriptor probing after a pathname is replaced. The caller's file descriptor remains open and its offset is preserved.

Process tests cover output limits, cancellation and deadlines. Inputs reject network URLs, non-regular files, FIFOs and externally referencing manifest formats. Linux directory access uses anchored `os.Root` handles and nonblocking/no-follow file opens. Software probing is exercised; hardware support is only enumerated and `HardwareVerified` remains false.

Scanner tests cover movie/TV/music/mixed hierarchy, two-worker admission, persistent progress and cancellation, stable rescan IDs, same-library inode-based renames, unchanged-file probe caching, directory/file replacements, changing files during probe, inaccessible roots and preserved catalog records. Deleting a library removes its database records without deleting media files.

## PostgreSQL and ownership evidence

ACL tests filter before counting, sorting, grouping and pagination. They cover empty allowed-library sets, live policy changes, protected details, recursive scope, malformed/cross-library ancestry and parent cycles. Latest grouping maps matching episodes to series and tracks to albums before applying limits; season/episode sorting and numeric season filters are covered.

The catalog owner holds a PostgreSQL session advisory lock scoped to its actual database/schema. Tests confirm that a second store cannot interrupt active work, ownership transfers after close, constructor errors release the lock, and a misleading `search_path` prefix is rejected.

Terminating the owner backend fences all old writes: two old workers cannot overwrite the successor's recovered job states or insert old media results. Short catalog/job mutations use the original lock-holding session, while queries remain pooled and media probing remains parallel. A separate regression cancels a caller after a transaction begins and confirms the short transaction and later scan remain healthy. Caller cancellation does not accidentally destroy catalog ownership.

## HTTP and browser evidence

PostgreSQL-backed HTTP tests cover administrator library creation, scan requests, progress, query projections, real counts, private-path exclusion, CSRF, account/library scope, restricted-user counts, directory validation, default Latest grouping, season filters, and catalog-only deletion.

The real administrator UI scanned a synthetic movie (`Scanned=1`, `Added=1`), stopped automatic polling after completion, and retained the file's size and modification time after deleting the library. A separate 200-hardlink fixture permitted a real running scan to be cancelled through the UI; the final run cancelled after 102 files. No synthetic delay or mocked API replaced that interaction.

All final browser scenarios reported no page exceptions. The expected unauthenticated session HTTP 401 remains normal. Desktop task/create-library screens and mobile library/task screens were visually reviewed. Mobile navigation restores focus explicitly, and the skip link is hidden until focused.

## Remaining requirements

This is not reference-server or third-party-player compatibility evidence. The Emby adapter still has incomplete query/options/error semantics and DTO fields. Local NFO data is not yet applied by the scanner, artwork endpoints are pending, and no playable media delivery method is advertised. Playback negotiation, streaming, subtitles, user progress, HLS, hardware pipelines, full administrator controls and release validation remain in the active plan.

The catalog implementation assumes one writer process and a session-preserving PostgreSQL connection. A lost ownership session makes readiness fail and requires a service restart. Filesystem reconciliation currently retains missing media records rather than automatically deleting them; persistent missing-item policy, watchers and richer metadata reconciliation remain work.
