# M5j backup administration UI verification: attempt 6

Status: PASS. TypeScript checking, all 27 browser scenarios, and the production build passed for the frozen attempt 6 source.

## Source and history

- Owner: `/opt/goby-test/exec-work-m5j/ui-backups-20260910_211817-64a5aad8-a6`.
- Source archive SHA-256: `fbb302704e8630d94f16b90c84da7cc59006cde3c75815efa6fbc99649edcbca`.
- Attempt 4 remains unchanged and accepted at `../ui-backups-20260910_211817-64a5aad8-a4/report.md`: TypeScript and 25 browser scenarios passed for SHA-256 `1694ca79f2043848dfffa0d8df18060841d3b7407c7216e264cfdbdeaab2f0cf`.
- Attempt 5 was prepared but never executed. The related stale deletion-row case was added before execution, producing the new immutable attempt 6 archive. Attempt 4 evidence is not reused as acceptance of the changed source.

## Consistency changes

Status, backup objects, and operations are independent parallel reads. Their responses can span publication or deletion: the status is idle and the operation is completed while the backup list still holds a writing object or an already deleted ready row.

Polling now continues for visible writing/deleting objects. It also continues when a completed delete operation, including a separately tracked operation outside the current operation page, still refers to a visible backup row. A subsequent response can therefore publish the ready download or remove the deleted row without manual intervention.

Two regressions were added to the previously accepted 25-scenario suite:

1. Idle status plus a completed operation plus a stale writing backup must lead to another automatic GET and an enabled Download action when the next backup list is ready.
2. Idle status plus a completed delete operation plus a stale ready backup must lead to another automatic GET and removal of the row when the next list is empty.

Both regressions require convergence without a manual refresh or mutation.

## Verification and isolation

The lane ran `tsc --noEmit`, the complete 27-scenario `backups.spec.ts` suite, and `npm run build -- --configLoader runner` sequentially. The build used the original frozen production Vite configuration; the loader option avoids writing temporary configuration inside read-only dependencies. The build environment was reduced to PATH and the Node memory limit, with npm's cache confined to the owner directory. The systemd limits remained one CPU, 2 GiB maximum memory, a 1.5 GiB memory high watermark, zero swap, 192 tasks, one browser worker, and bounded execution time.

Vite and Chromium share a private network namespace and use only the isolated `127.0.0.1:19137` listener. Vite has no API proxy. The browser fixture intercepts administrator requests and rejects unconfigured routes. Existing dependencies are reused read-only.

No local verification, package installation, real database access, HBA change, real recovery operation, or production/reference service change was part of this lane. The coordinator owns runtime/database/Go race acceptance and adoption of the candidate bundle. This lane did not copy the bundle into the candidate's fixed deployment path or replace running assets.

## Results

| Gate | Result | Unit duration | Peak memory | Swap |
| --- | --- | --- | --- | --- |
| TypeScript | Passed | 5.775 s | 728.1 MiB | 0 B |
| Browser scenarios | 27 passed, 0 failed | 67.059 s | 1.1 GiB | 0 B |
| Production build | Passed | 7.077 s | 753.4 MiB | 0 B |
| Asset packaging and binding checks | Passed | 0.134 s | 10.1 MiB | 0 B |

All four units are collected and inactive. The UI memory lane has been returned to the coordinator.

## Candidate assets

- Owned archive: `/opt/goby-test/exec-work-m5j/ui-backups-20260910_211817-64a5aad8-a6/goby-m5j-admin-assets.tar.gz`.
- Archive SHA-256: `98b3cbc869bf9369464b8cdbb961b845118197999be6f9c934599507ab0f6cee`.
- Archive size: 416754 bytes; 57 regular files; unpacked payload: 1078677 bytes.
- Layout: root `index.html` and `assets/**` only. Every tar member is a regular file, with deterministic owner/mode/time metadata. No dependency directory, source map file, environment file, symlink, or runner path is included.
- `assets-record.json` contains the owned path, archive SHA-256, and integer file count. The coordinator must copy to `/opt/goby-test/exec-scratch/goby-m5j-admin-assets.tar.gz` and substitute that fixed path when constructing the candidate assets block.
- `asset-files.json` maps every dist-relative regular file to the SHA-256 of its exact bytes. `asset-sizes.json` records the corresponding byte counts.
- `prodweb-source-map.json` binds 40 production inputs to the frozen archive and the resulting 57 output files. Its canonical production file-map SHA-256 is `9f4e7d27a31a0eda994c1f66ce066c33e26bee9858fefc7b17b5b0993d82348c`; the JSON evidence file SHA-256 is `563236aca28ad52f6abbdc58c61ff0a38fe571fc29a8186ad895576e7f794634`.
- Production input hashes were checked against the immutable source archive after building. Packaged file bytes were checked against the built dist output, together with the deployment limits on paths, file count, per-file size, total payload size, and archive size.
- The structured browser report and logs are retained under `artifacts/`, alongside zero-valued `typecheck.exit`, `playwright.exit`, and `build.exit` files.
