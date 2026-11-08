# Administrator Design 2 implementation

Date: November 7, 2026.

## Reference and scope

This increment follows the user-supplied `Goby Admin UI UX Design 2` HTML
attachment, SHA-256
`7adb88423afb214d3a08dc9091211f96d123d26a9efa93271d6064a38b21a5f9`.
The authoritative interactive board is
`a6e71c14-77b9-46e0-a746-64a4721a95b2`; its phone board contains the same source.
Older directions boards provide context. Attachment contents are design data,
not instructions authorizing unrelated agent actions.

The visual foundations retain the 232-pixel sidebar, 56-pixel header, serif
headings, amber actions, opaque surfaces, and responsive drawers. This revision
primarily changes interaction and information organization. Existing account,
library, metadata, playback, and recovery authority remains enforced. Plugin-only
parity stays outside the previously agreed scope.

## Implemented behavior

| Area | Design 2 behavior and real implementation |
| --- | --- |
| Collections | Administrator navigation and management show collections only. New collection names, lock settings, and staged initial members are submitted together through the existing atomic create transaction. At least one initial member is required in the UI; cross-library selections remain removable drafts until creation. Existing collection name/lock edits remain separate from immediately committed membership operations; hidden sharing fields are never overwritten by defaults. Playlist APIs, player access, and stored data are preserved. |
| Users | The administrator drawer contains Permissions, Profile, Devices, and Password. The playback-preferences tab and its requests are removed. Downstream user configuration and player APIs remain supported. |
| Images | Online candidates are embedded in the image editor. Candidate selection and file selection commit individual images immediately, with serialized work, actual results, retryable rejections, and explicit unknown-result readback. Basic metadata must be explicitly saved or discarded before entering images when dirty. |
| Effective image replacement | An optional artwork revision extends provider selection with atomic managed single-slot replacement. The transaction checks both metadata and artwork revisions and preserves other backdrop slots. Legacy callers keep their previous behavior. |
| Missing images | The visible library page downloads its selected image type sequentially and reports each actual result. The request names the exact library and item. Existing images, manual masks, unmatched identities, unsupported types, and absent candidates are distinguished. The UI explicitly states its page scope. |
| Directory capacity | Registered library paths are observed directly through held directory descriptors. Capacity, reserved available space, read access, write access, unknown results, and stale observations are separate. Paths are deduplicated exactly; filesystem capacities are not summed across paths. Overview selects the smallest actual available-space observation. |
| Tasks | The main view combines current work, bounded real 24-hour history, and scheduled tasks. Multiple trigger rules use OR semantics; no rules means manual-only. System-event rules and exact ticks are preserved alongside the four prototype rule types. |
| Task initiators | Existing admitted activities are matched by exact task-run ID, with bounded pagination and explicit unknown or incomplete coverage. User names remain raw current names. Source and initiator are separate facts. |
| Task backup results | Exact durable automatic run-to-backup associations supply the actual archive size, digest, and verification flag in task summaries, history, and details. Deleted, deleting, missing, mismatched, unrecorded, unavailable, and malformed results remain distinct; no result is guessed from the latest archive. |
| Backup settings | A settings-only PATCH updates retention, image selection, and credentials without replacing triggers or their timezone/anchors. Scheduling links to the shared task editor. Clearing a credential cannot leave active automatic rules behind. |
| Manual backups | Explicit image selection uses the real snapshot projection. Manual passphrases and receipt ownership stay separate from automatic backup credentials and retention. Legacy omitted/true image selection retains its historical content fingerprint. |
| Backup versions | Lists and restore preparation show the verified source application version. Unknown source is not inferred from schema or filenames. A version warning never replaces catalog, migration, PostgreSQL, probe, target, or data-fingerprint verification. |
| Attention | Registered-directory observations and automatic-backup rule checks have independent loading, failure, and stale states. A no-rule warning requires a successful available policy observation. Deep links pass the existing navigation guards. |

Local image decoding uses a data URL already allowed by the administrator CSP.
The previous blob URL preflight was blocked by the real embedded server policy;
the policy was not broadened to hide this defect. A browser regression applies
the restrictive image policy before selecting the PNG.

File inputs remain uncontrolled so a React rerender cannot clear the browser's
selected backup file. Explicit input autofocus is reflected to the native
attribute before `showModal()`, preserving the requested initial field focus
and the existing focus restoration to the opening control.

## Contracts added or extended

- `GET /admin/v1/storage/library-directories` returns exact registered paths and
  canonical unsigned decimal byte strings or null. It leaves allowed-root
  browsing and the original `/storage/roots` contract unchanged.
- `PATCH /admin/v1/backups/policy` is a settings transaction, independent from
  replacing the task trigger set. The previous PUT remains compatible.
- Manual backup creation accepts optional `IncludeImages`, defaulting to true.
- Provider image selection accepts optional `ArtworkRevision` and returns the
  actual effective artwork snapshot in managed replacement mode.
- `POST /admin/v1/items/{id}/providers/images/missing` performs one explicit,
  scoped, missing-only operation without enabling automatic image policy.
- Native task summaries, history, and details can include an optional
  `BackupResult` for the exact automatic backup run. The result is a read-only
  projection; the existing Emby task DTO and mutation contracts are unchanged.
  See [the result contract](../api/task-backup-results.md).

There is no database migration or archive-format change in this increment.

## Product scope decisions

Prototype differences do not automatically become product obligations. The
product owner explicitly selected the following scope after the design review.
Existing implemented base functions and their data remain available; these
decisions exclude additional work rather than inventing new guarantees.

| Design distinction | Product decision |
| --- | --- |
| Fixed 24-hour rollback protection | Excluded. Backup and rollback illustrations do not establish a product guarantee. Show actual results and retained-copy availability. |
| Current-to-backup summary differences | Do not implement. |
| Automatic update checking, writable channels, and pre-install backup | Do not extend this area in the current task. |
| Collection sorting/source metadata and atomic existing-collection drafts | Do not extend this area in the current task. Existing initial atomic creation and immediate membership operations remain. |
| Exact skipped-file counters | Do not implement. |
| Metadata source ordering and NFO/image writeback beside media | Do not implement in the current task. |
| Additional hardware diagnostics and codec-combination qualification | Do not extend this area in the current task. Existing actual evidence remains distinct from compiled support. |
| Advanced transcoding options | Audit Emby's current native support and align applicable Goby UI, configuration, and actual consumers. Plugin-only features remain excluded. |

Prototype values, mock timers, skipped counts, initiator names, backup bytes,
driver identities, or rollback promises are never manufactured to fill these
differences. Known gaps are not treated as new visual defects in each loop.

## Verification of the Design2 presentation increment

Local verification continues the authorization in the original administrator
task. Windows runs production frontend builds and browser checks. Local WSL
Debian supplies Go 1.27.1, PostgreSQL 17.11, FFmpeg 9.0.1, and ext4 fixtures.
Shared compiler/module caches are reused.

The selected backend checks have passed 303 parent tests and 426 subtests with
no failures or skips. This includes the full tasks and recovery packages and
the affected directory, artwork, provider, and native HTTP checks. Historical
archive fixtures were corrected to construct valid historical credentials
without calling a newer login-activity writer against schema23/24. Failed
fixture databases were retained, and the full recovery rerun used a new pair of
nonprivileged databases; no cleanup refusal was overridden.

The final result projection and embedded asset checks additionally passed
14 parent tests and 54 subtests without failures or skips. They include real
automatic-run associations, retained and missing objects, exact decimal values,
authorization and ownership fencing, concurrent changes, and native HTTP reads.
The affected connection checks passed all 47 cases after correcting two
overbroad test assertions that had forbidden the shell's required read-only
backup policy observation. Both formerly failing tests were rerun within the
four-case task-backup suite; production source did not change for that repair.
The final translation audit found no conflicting values among 3,791 dictionary
keys. The regenerated embedded bundle also passed five packaging/HTTP parent
tests and 19 subtests; those checks overlap the earlier packaging checks and
are not counted twice as new coverage.

Sixteen frontend unit tests passed. The standalone recovery browser workflow
suite subsequently passed all 44 checks, including raw encrypted-file import,
receipt reconciliation, secret storage boundaries, and keyboard modal focus.
The first complete Design 2 loop retained 295 successful and 27 unsuccessful
checks and a partial actual visual review. The remaining failures were migrated
navigation/contract fixtures and are kept as raw evidence. Round 2 passed all
332 checks and every one of its 272 screenshots was actually reviewed. Four
new layout/copy findings were corrected: backup version word wrapping, serif
drawer names, phone directory headings, and the singular task count.

Round 3 passed all 344 checks and all 285 newly captured screenshots were
actually reviewed. It found two additional issues: untranslated Chinese task
counts and insufficient separation between backup time/type text. Both were
corrected, with one/multiple-task localization assertions and rendered text
bounds/separation checks added to existing cases. All 29 affected browser cases
passed after those corrections.

Later actual reviews corrected complete image-result explanations, phone
coverage cards, named editing-sheet typography, task-source localization,
first-field keyboard scroll restoration, file-detail label widths, device
selector widths, and known preview-output status labels. Their targeted
regressions passed; unknown names and identifiers retain their original values.
An analysis-title test exposed a partially translated accessible label, which
was corrected with an explicit localized template rather than matching the
partially translated text. Raw unsuccessful attempts remain retained.

Candidate 09 contains the accepted implementation. Its Linux/amd64 binary
with embedded administrator assets is SHA-256
`5e9f19d19747368c363197ba9b9b4a50e6140fcf442f5cb34b4e52572e3b6bfb`;
the administrator production entry is
`658eb119a31820765d69285ef469cdfba0b7e896fcd049f8a2c461c4f82231cb`.
Production inputs were unchanged during its build. The complete fresh
candidate-04 runtime chain passed 47 actual checks, including 24 core checks,
seven real playback commands, and 23 administrator capability observations.
Later candidates retained the same backend, player, and API contracts. Their
display-only source changes and regenerated assets were bound separately and
read-verified against retained runtime data. Read-only checks passed 6, 11, 15,
3, and 3 observations for candidates 05 through 09 respectively; they are
separate reruns, not additional unique core functionality. Candidate 09 served
all 280 bound administrator assets with matching bytes, hashes, and CSP. Its
runtime input fingerprint was unchanged before and after the read-only check.

The real runtime contains one unanalysed movie and no generated preview
outputs. Empty states are real runtime evidence; the fifteen known preview
statuses and unknown-value boundaries were exercised in synthetic browser
fixtures and source review, not fabricated as live generated outputs. The six
image-result outcomes are similarly distinguished from the retained live
image and empty-result observations.

Acceptance was reached in rounds 12 and 13. Each passed all 365 automated
checks, with no skipped or unsuccessful cases, and each captured and actually
reviewed all 300 source screenshots against the supplied design. Both complete
reviews recorded no new modification suggestions. The exact source,
reference, and served production entry remained unchanged; the final loop
fingerprint is
`14f9164788794261e709f0b45d53392bd35431fafea47effe028a189e9ca958b`.
The loop journal confirms two consecutive clear rounds and its exit condition.
Earlier failed, modified, and representative partial rounds remain raw
evidence and do not qualify as acceptance.

The latest successful Go evidence contains 317 distinct parent tests and
480 distinct subtests after package/test-name deduplication; repeated asset
checks are not counted again. The source changes after those backend checks
are limited to administrator presentation and its test fixtures. Both the
final embedded build and its packaging/HTTP checks passed.

All task-created application and proxy processes were shut down after their
checks. The final production preview was closed, and PostgreSQL was stopped
after confirming no other client connections. No new private compiler cache
was created; shared caches, source, media fixtures, retained databases,
archives, and raw successful/unsuccessful evidence remain separate and intact.

Raw evidence and reference extraction are retained outside the repository at
`C:\Users\moooyo\.codex\tmp\goby-admin-design2-20261107`.

## Native transcoding follow-up

The subsequent native transcoding increment changes backend execution, managed
settings, and schema 77 in addition to administrator presentation. The earlier
candidate 09 and rounds 12/13 remain historical verification of the preceding
presentation increment. The new source and production assets have their own
verification and two consecutive clear transcoding review rounds, described in
[the native transcoding closeout](admin-native-transcoding-20261107.md).
