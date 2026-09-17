# Programs UTC rollover evidence review

The saved-byte and source-rule review completed with SSH exit 0 (`c09f46`). It
retains all nine original logs and matches the closed 143-byte shutdown log plus
the captured 1101-byte startup prefix to the exact 1244-byte server-unit-log delta.
The pinned old source, UTC dates and recorded stop/start order support the writer
PID attribution. No write syscall or kernel writer-PID trace was captured; that
attribution remains an inference.

The separate reader closure completed with exit 0 (`04bd2f`). The original
reader recorded 44 opens and 44 closes with none remaining; its PID, parent,
group and cgroup were absent. Closure checked published-file metadata without
reading the original log bodies again.

The retained artifacts are under
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/programs-transition-dispatch-20260917-hosting-namespace-r02/private`:

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `diagnostic-utc-rollover-review.json` | 2311 | `67057f9bb30bf15185bf21bf8c0222eeec186d158be518ce32d1a6821fc82f42` |
| `diagnostic-utc-rollover-review-proof.json` | 53962 | `91c2175c820889b9562ee3fe677410421e3c544796e3114e3a35b4e29e028e87` |
| `diagnostic-utc-rollover-review-reader-closure.json` | 4409 | `cfc2cc5340d39f77523485f32a50d1fc450d9ecfedfad0a6ae17d5c7001bb744` |

This is proof input for finalization. The [original transition failure](programs-transition-first-execution-failure-20260917.md)
remains CLI/caller/SSH exit 2, PowerShell tool exit 1 and closer
`evidenceAccepted=false`. A remains running as PID `1907978`; no V4 or client
admission was produced, and no transition or service action was repeated.

At this review checkpoint, eight finalization files, including Python/JavaScript
sources and the contract, were frozen in isolated commit
`24ad7c1dfd49455f2dd723fca4c9940edca78121`. The revision had not been merged or
tested, and actual finalization had not run. The subsequent
[21-method and saved-evidence verification](programs-finalization-component-result-20260917.md)
has its own result and closure records. Preserve new A and all original records.
M5 full/build and overall M2-M6 remain open.
This documentation update only read the local evidence mirror and ran no checks.
