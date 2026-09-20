# Selected compatibility phase 2 execution record

Status: **closed from the recorded consolidated and affected repair scopes**.
The final affected scopes passed at `bd20b23`: 114 media parents, 12 library
parents and the native browser parent with all 16 independently acknowledged
stages. Earlier passing scopes remain part of the evidence; earlier failed
batches remain failed. This is not a claim that every package was rerun at the
final revision. Counts from overlapping scopes must not be added.

The machine-readable [result record](selected-compatibility-phase2-results-20260920.json)
contains source commits, executed binary hashes, selectors, retained failures,
tool identities and closure evidence. Private evidence is retained under
`D:/Code/goby/.git/selected-compatibility-20260920/phase2` and
`/opt/goby-selected-compatibility-20260920-p2a` on `test-env`.

Phase 1 closed at `f02b81f` under the user's third-party-client adapter boundary.
Original Web commercial licensing remains a recorded client limitation, not an
acceptance gate. This phase uses Goby's native administration interface and a
private media test consumer; it does not claim arbitrary third-party clients
were tested. Work remains on `codex/selected-client-compatibility`. This record
does not claim completion of phases 3 and 4, or merge, push or deployment of the
four-phase increment.

All phase delivery code preceded consolidated verification, with the initial
implementation frozen at `5f0cdc0`. Local compilation and unit tests were
explicitly authorized. Actual media, database, browser and integration execution
took place only on `test-env`. The ui-ux-pro-max skill remained disabled.

## Delivery inventory

| Requirement | Delivered boundary | Accepted evidence |
| --- | --- | --- |
| B1 Embedded subtitle removal | Explicit prepare/review/apply; admitted MKV/MKA remux or selected MP4 structural edit; packet, metadata, chapter, role and attachment preservation; atomic exchange retains the original | Three actual container profiles and complete A/V decode; 12-second, 24 fps AAC regression; source/authority/publication races and native apply |
| B2 Bitmap subtitle OCR | Actual PGS/DVD display-event decoding with pinned Tesseract models; original and reviewed cues/images; independent owned SRT/VTT publication | All nine real OCR scenarios; review/correction; actual subtitle on/off/on playback and owned-track HTTP/HLS/source checks |
| B3 Embedded audio covers | Bounded MP3 APIC, FLAC PICTURE and M4A covr extraction with source provenance; ready/none/failed state and managed/provider/sidecar/embedded precedence | Actual tagged-container tests, rescan/cache/source/authority checks and native authorized cover HTTP observations |
| B4 Generated collages | Deterministic authorized library/genre members and source manifest; current authority/source checks before cache and conditional responses | Empty/partial/multiple/deduplicated members, rendered pixels and revocation tests; native decoded generated artwork |
| B5 Image transformations | Crop, EXIF orientation, fixed foreground shapes, backgrounds and user-state overlays; complete GIF composition and timing | 47 local artwork unit parents plus remote HTTP/cache, disposal/loop/delay/transparency, budget and cancellation coverage |
| B6 Jobs and management | Parameterized operations, idempotency/CAS, review/apply/cancel/recover, native Tasks/item controls and durable derivative state | Native 16-stage journey, restart persistence, cancellation and worker closure; archive normalization, corruption rejection and old-grant invalidation |

Schema 44 adds `media_operations`, `media_operation_cues` and
`item_owned_subtitles`; schema 45 adds `item_embedded_artwork`. The SQL manifest
was appended without changing historical entries. A fresh PostgreSQL 17 schema
45 catalog was committed at `93f052e`: 742,656 bytes, SHA-256
`e06dcea44b39804aea09c46891dfe93bfe881bf043802d2baec257d77d91332e`.

## Preservation and lifecycle contracts

The coordinator shares the existing catalog owner and joins admitted worker,
process and storage activity before releasing it. Strict operator-owned
configuration binds tool/model identities to each execution. Ready results do
not publish changes. Apply binds the reviewed revision, source revision and
result hash; only unresolved B1 `prepared` or `catalog_committed` publication
journals reserve their source. Disabled processing still permits bounded
explicit cancel/discard without admitting new processing.

MKV/MKA use FFmpeg remux with explicit default-disposition passthrough. Strict
proofs admit bounded opaque attachments and valid zero-padded Matroska strings.
The nonzero `CodecDelay` exception is limited to AAC: raw track identity, codec
private data, sample frequency, probe padding and exact sample-to-nanosecond
conversion must agree. Source/candidate delay and all retained packet timing,
side data and payloads remain checked; this does not extend admission to Opus.

In the observed 24 fps case, FFmpeg rewrites video `DURATION` from 12.000 to
11.999 seconds despite unchanged packets. The repair restores the exact source
tag only within a narrowly proved same-width representation, with the same
nonintegral `DefaultDuration` and millisecond timecode scale. It validates
covering CRCs, updates them from inner to outer elements, verifies intended edits
and unchanged ranges, and binds final bytes to a validated snapshot and whole
file hash. Exact metadata comparison remains in force; `DURATION` is neither
ignored nor compared using a blanket tolerance.

The admitted nonfragmented, self-contained MP4 profile uses bounded descriptor
streaming. It replaces only the selected `tx3g` track's `trak` with a same-size
`free` box and zeroed body, preserving all other bytes including `mdat` and
`mvhd`. Exact extent, track mapping, preserved-range digest and whole-candidate
digest remain bound to source/candidate snapshots. Cross-track references and
unproved profiles are rejected. Unreferenced subtitle bytes can remain in
`mdat`; this operation does not promise forensic erasure.

B1 atomically exchanges files on the same Linux filesystem and retains the
original inode/bytes in its private payload. That media backup is outside
database backup coverage. Restart and restore do not automatically rename,
purge or replay publication; uncertain publication needs explicit recovery by a
current administrator. Public media, download, subtitle and image opens recheck
publication and complete source identity after opening. Replacement expires old
playback identities and joins the exact source's readers and conversion jobs
while unrelated sources remain active.

OCR streams display events instead of sampling video frames. It preserves an
unknown DVD format origin and the initial blank interval, and separates original
evidence from editable text/times. One to three models may be chosen from
`eng`, `chi_sim` and `chi_tra`. Owned subtitle indexes are not reused; delivery
URLs bind their content tag. Restore verifies raw archive fingerprints before
normalization, retains clean ready review/derivative data, clears old execution
and apply grants, and marks unfinished work interrupted or requiring recovery
without touching original media.

## Verification composition

Frontend compilation and 47 Windows artwork unit parents passed at `5f0cdc0`.
Ordinary/embedded Linux builds and package test binaries compiled at `93f052e`;
the library fixture inventory repair was rebuilt at `a8ff413`. Final media,
library and tagged browser binaries plus ordinary/embedded Linux builds compiled
at `bd20b23`. Compilation is separate from actual execution evidence.

| Receipt | Source and result | Accepted scope or retained failure |
| --- | --- | --- |
| `verification-01.json` | `a8ff413`, **FAIL**; default binaries `93f052e`, library `a8ff413` | Config 53, transcode 301, database 62, library 760, server 861 and recoverydb 12 parents passed. Media passed 378 parents and failed three; backuppg passed 104 and failed one; recovery passed 27 and failed one; native browser failed before admission. |
| `verification-repair-01.json` | `02f3da6`, **FAIL**; media/library/server binaries `bef3c21` | All nine actual OCR cases and PNG budget repairs passed. Library 22, server 22, backup 4 and recovery 1 parents passed. B1 actual-container preservation still failed. |
| `verification-final-01.json` | `b926449`, **FAIL** | Media passed 57 parents; B1 still rejected opaque Matroska attachment and MP4 preservation facts. Native execution was gated. |
| `verification-final-02.json` | `a3b713d`, **FAIL** | Media 79 and library 11 parents passed, including three actual container profiles and full A/V decode. Native setup failed on the incomplete metadata fixture. |
| `verification-native-03.json` | `0dae905`, **FAIL** | Corrected metadata fixture reached authentication; real subtitle-removal execution failed. |
| `verification-native-04.json` (remote retained receipt) | `680eeef`, **FAIL** | Transparent real-executor diagnostics retained the same source inode and failed candidate; the AAC `CodecDelay` guard was identified. |
| `verification-final-03.json` | `b2ab29b`, **FAIL** | Media passed 91 parents; the added 12-second AAC case exposed the one-millisecond `DURATION` rewrite. Later scopes were gated. |
| `verification-final-04.json` | `bd20b23`, **PASS for selected scopes** | Media 114, library 12 and native browser 1 parent passed, with no skips or failures; all 16 browser stages completed. |
| `mocked-browser-02-receipt.json` | `bef3c21`, **PASS for mocked UI** | All 12 selected administrator tests passed; zero skipped, flaky or unexpected tests, report errors or unhandled API requests. |

The first media failures led to a separate version 2 OCR fixture corpus,
preserving version 1 files and logs. The passing actual scenarios are English,
Simplified Chinese forced, Traditional Chinese and overlap in both PGS and DVD,
plus mixed forced PGS. Exact display times, forced state, recognized text and
image evidence are checked. Pinned Tesseract 5.5.0, model digests, FFmpeg/FFprobe,
Python, Noto font/license and generator identities are recorded in the result
JSON. No earlier failure is relabeled.

The first backup failure occurred before deliberate corruption because complete
row witnesses used different timestamp formatting. Its test-only repair applies
canonical archive settings in a nested savepoint, preserves exact values and
checks a one-microsecond change. The recovery fixture uses explicit `bigint`
casts and still exercises revision `9007199254740993`. Fresh repair database
pairs passed corruption/finalizer and authority-normalization scopes without
relaxing product validation; earlier databases remain retained.

The final B1 scope decoded every retained A/V stream in MKV, MKA and MP4. The
base MKV/MP4 cases contain 50 video frames and two audio streams; MKA contains
two audio streams. The added 12-second, 24 fps case decoded 288 frames and AAC
audio while retaining 1,024-sample priming and packet proofs. Independent native
post-publication decode again completed 288 frames and one audio stream,
reported 12.0105 seconds, and produced zero stderr bytes. The retained French
subtitle was independently fetched as VTT with HTTP 200.

Hardware-specific AMD/RPU skips and the mount-namespace helper skip from the
first batch remain explicit skips. The final B1/library/native scope had none.
Passing parent counts exclude subtest events, while original failed-test arrays
can contain parents and subtests. They describe their recorded revisions, not a
new whole-suite result for `bd20b23`.

## Native browser acceptance

Run `selected-phase2-bd20b23-browser06` passed authentication, removal prepare and
apply, OCR recognition/review/apply, subtitle selected/off/reselected/stopped,
cancel prepare/cancelled, artwork, restart, persisted history and cleanup. Every
stage has independent database/file observations. Native controls use normal
authentication and CSRF enforcement. No business-response mocks or CSP bypass
were used. There were zero page errors and foreign requests.

The private test consumer uses an actual `HTMLVideoElement`, pinned HLS.js
1.6.0-beta.2 and real viewer authentication, `PlaybackInfo` and fMP4 HLS routes.
Three actual seek/play transitions each observed at least three advancing
frames. The reviewed cue appeared with selection, disappeared when off and
reappeared after reselection, with expected text and a 1.25-2.4-second window.
The same HLS identity and single producer were independently observed across
changes. Active-encoding stop and viewer logout both returned 204; readers,
producer cache, session contexts, stream slots and policy leases closed.

The consumer sent no `Playing` or other playback reports. It does not establish
play-count behavior: one unstarted, uncounted `Prepared` history row remains with
authentication revoked, together with terminal encoding history. These history
rows are not active resources. The real server restarted once and identical
operation, subtitle, artwork and retained-file state was verified. Two embedded
covers and the generated collage were independently fetched and decoded through
authorized HTTP routes.

All 16 checks passed with zero fallback session revocations. Browser process
groups and observed descendants, HTTP listener, server workers and diagnostic
descriptors closed; the owned browser schema/media root/private context were
removed. Four retained PNGs provide masked native-layout and actual subtitle
presentation evidence. Sensitive native fields are masked, so these captures
do not claim unrestricted visual review of their contents.

## Resource closure

`closure-01.json` records phase-owned PostgreSQL stopping from PID `3251664`
to `MainPID=0`, inactive/dead, with its data preserved. All recorded workers are
terminal; failed unit statuses remain failed rather than being cleared. The
successful final scope separately records every process group closed. Evidence
and failed-run files remain retained. No new tests ran during closure.

The unrelated `goby-core-av-original-client-01.service` remains active with the
same PID `366598` and invocation identity. Original client service and assets
were not changed. Phase-owned database and worker resources are closed, allowing
phase 3 implementation to begin.

## Related contracts

- [Machine-readable results](selected-compatibility-phase2-results-20260920.json)
- [Native media processing API](../api/media-processing.md)
- [Media publication and recovery](media-edit-publication.md)
- [Admitted media preservation profiles](media-edit-preservation-profile.md)
- [Bitmap fixture and actual OCR profile](../../scripts/test-env/bitmap-subtitle-fixtures.md)
- [Artwork rendering contract](../../internal/artwork/README.md)
- [Four-phase plan](../planning/selected-compatibility-plan-20260920.md)
