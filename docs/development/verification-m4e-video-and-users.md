# M4e/M5a: progressive video and native user management

Verification date: 2026-09-09. Status: **passed for this increment**.

This increment adds progressive MP4 video, ordered Video PlaybackInfo profiles,
independent container-clock facts, native account/policy/password management,
and periodic authorization of active original-file responses. It does not
complete the M4 conversion, M5 administrator, or full compatibility milestones.

## Verification boundary

All functional, PostgreSQL, FFmpeg, HTTP and browser verification runs through
`ssh test-env`. Earlier authorized local Go and frontend builds passed; the final
backend binary was compiled on Linux. The host uses Go 1.27.1, FFmpeg/ffprobe
9.0.1 and PostgreSQL 17.11. The application database remains on port 5432; Go
integration tests use isolated schemas in the owned scratch cluster on 15432.
Browser verification uses another disposable database and a dedicated account.

The tested source is `/opt/goby-test/verify-m4e-20260909`, backed by the marked
directory `/dev/shm/goby-verify-m4e-20260909`. Both `TMPDIR` and `GOTMPDIR` use
`/opt/goby-test/exec-scratch`; Go caches use the existing dedicated tmpfs paths.
Only checked, obsolete Goby source snapshots and archives were reclaimed to
make room. The common `/dev/shm` mount remains non-executable.

## Complete Linux suite

`go test -race -count=1 -v ./...` passed with **854 top-level tests**, all twelve
tested packages, and zero skipped tests. The command package has no tests. Both
the Go process and SSH wrapper returned zero. The [machine-readable summary](m4e-full-race-summary.json)
records timestamps, every package, the full log hash, and source provenance.

| Package | Duration |
| --- | ---: |
| artwork | 1.631 s |
| config | 1.026 s |
| database | 3.328 s |
| events | 1.080 s |
| identity | 99.858 s |
| library | 25.372 s |
| media | 5.269 s |
| metadata | 1.582 s |
| playback | 1.250 s |
| server | 300.570 s |
| subtitle | 1.179 s |
| transcode | 55.780 s |

All 267 Go/module files in the staged source match the tested source byte for
byte. The aggregate manifest SHA-256 is
`5de51b960ae2aa3735f66cce7c2f564b5dc635375ec91254e3536e2f762075be`.
The final compiled backend SHA-256 is
`2008e104bca56920721a45c8342260a09e0f2c326f60e6a0d6386074d8e8a3ac`.

## Media and HTTP evidence

The real-media engine matrix covers MP4, Matroska and MPEG-TS inputs with 27
copy/encode/no-audio/start combinations. Checks compare source positions, decoded
frame sequences, pixel content and chirp audio alignment, and strictly decode
the complete output. Separate variable-frame-rate tests verify preserved source
timestamps when no output rate is requested and physical frame conversion when
a rate is requested. The manager accepts a validated H.264/AAC initialization
and a usable video sample within its existing bounded observation prefix.

The real HTTP tests cover standard PlaybackInfo URLs, HEAD without an encoding
job, ignored progressive Range requests, complete MP4 decoding, a 4.25-second
non-keyframe encoded seek, video-copy/AAC-encode output, a silent source, profile
ordering, current permissions, ownership, and cancellation after media headers.
Expected frame counts are 288 for the complete color fixture, 186 after the
seek, and 144 for the silent source. Source colors independently distinguish
the requested source region. Stop must abort the response and release the actual
producer, readers and cache pins.

Original-response tests use a sparse 256 MiB source with at most 1 MiB physically
allocated. Native disable and library-ACL mutations interrupt the affected
blocked TCP response while another authorized user's response continues. Server
shutdown also interrupts a blocked response and joins authorization watchers.
The enforcement is periodic rather than instantaneous; transient database or
storage observations do not erase an existing grant.

An initial media test incorrectly assumed a short video-only MKV exposed
`format.start_time=1.25`. Independent FFprobe controls found four packet PTS
values from 1.25 to 1.55 seconds but no format start. The corrected regression
retains that unknown clock instead of inferring it from packets. MP4 and TS
fixtures preserve the explicit 1.25-second origin; a separate AAC-bearing MKV
preserves explicit zero. No production probe logic was weakened.

The initial HTTP run also detected a changed missing-item error code and old
501 expectations. The video adapter now preserves the existing `not_found`
contract. Tests now expect an unsupported conversion to return 415 and a
contradictory original alias with `Static=false` to return 400. The final suite
includes these corrections.

## Native user management evidence

Migration 0013 introduces a positive management revision. HTTP tests cover
complete strict JSON bodies, decimal-string revisions, stale-update conflicts,
account/ACL/playback flags, password reset, session revocation, and protection
of the final enabled administrator. Concurrent tests revalidate authority
inside transactions and enforce account-before-session lock ordering.
Migration tests verify defaults and preservation across 12 to 13.

The deployed migration audit records aggregate hashes for all existing rows in
ten stable tables, including users, catalog relations, artwork, subtitles and
every user-state row. Its [before snapshot](m4e-migration-before.json) uses schema
12; the [after snapshot](m4e-migration-after.json) confirms schema 13, unchanged
rows and the default revision on all ten existing users. Authentication and
background-job tables are deliberately excluded because
their ordinary lifecycle can change independently of migration. Passwords and
raw database rows are never included in the report.

## Deployed video and library upgrade

The final backend starts as UID 995 (`goby`) with PID 3330261 against the existing
5432 database. The prior binary, administrator assets and a private PostgreSQL
custom-format dump are retained in the marked deployment backup directory. No
credentials or database dump are copied into this repository.

The [deployment evidence](m4e-deployment-evidence.json) confirms running/on-disk
executable equality, all 267 staged Go/module files matching the tested source,
and all 26 final administrator asset files matching the browser-tested archive.
Live HTTP index and JavaScript responses also match those assets.

The [deployed video report](m4e-deployed-progressive-video.json) passed on its
first run, with no retries and no cleanup errors. Normal scans upgraded all
eleven media records in five existing libraries from probe 4 to 5. No item was
added. All 21 catalog items, 15 complete user-state rows, 20 fixture files and
seven fixture ownership records remain unchanged apart from the intended probe
fields and scan bookkeeping. Nine media clocks are known and two remain unknown;
the new per-item clock facts are stable after the upgrade.

| Deployed check | Result |
| --- | --- |
| Ordered HTTP/HLS, omitted and empty HTTP protocol | Correct first constructible profile; no negotiation job |
| Progressive HEAD | No encoder job or invented length |
| Complete video/audio copy | 360 video frames, identical decoded source sequence; strict A/V decode |
| H.264/AAC encoding at 160x90 | 360 video frames; correct first, middle and last source positions |
| Same encode URL changed to 6.37-second start | 207 video frames; first video PTS 0.004964 s, matching source frame 153 at 6.375 s |
| Seek middle and last frames | Exact nearest-source matches at frames 256 and 359 |
| Progressive Range request | Ignored with HTTP 200; identical retry reuses its job |
| Nonzero copied-video seek | HTTP 415 without creating a job |
| Original representation | Complete source SHA-256 unchanged; range HTTP 206 |
| Cleanup and state | ActiveEncodings DELETE 204, zero active owned encoders, logout invalidates the URL; user data unchanged |

This deployed verifier deliberately sends no Playing or Stopped reports, because
those are state-changing client reports rather than a prerequisite for verifying
transport bytes. It uses owned encoding cleanup and logout. Actual Stop-driven
response interruption is covered by the separate real HTTP integration tests.

## Administrator browser and cleanup

The [final browser report](m5a-managed-users-browser.json) passed in 12.370 seconds
with one complete workflow, no skips, no failures and no flaky tests. It uses
Node 20.19.2 and Playwright 1.63.0 on Linux. The dedicated application runs as
UID 995 with the same final backend binary. Restart preserves the complete
account, library and configuration snapshot. The temporary role has no superuser,
database-creation, role-creation, replication or bypass-RLS privileges.

All ten cleanup checks pass: the dedicated database/role, processes, runtime and
credential-bearing temporary files are removed; the original database catalog
and application service are unchanged; and HBA bytes are restored exactly.
The final artifact SHA-256 is
`b7d39f882602a0926a036cafa5f71aa1e17279617f38a63409c4cf4c9f01bdd6`.

The [first attempt](m5a-managed-users-browser-attempt-1.json) passed the browser
and restart checks but its cleanup guard rejected its own OID because PostgreSQL
encoded it as a string. The [recovery record](m5a-managed-users-browser-cleanup-recovery.json)
confirms exact ownership and removal after correcting the type comparison. The
[next successful run](m5a-managed-users-browser-before-display-fix.json) exposed
a visual issue: a global success toast temporarily covered dialog buttons.
The Users page now hides that toast while an edit/create dialog is open, retaining
the dialog's own saved status. An authorized frontend build and another complete
Linux browser run passed after the change. Desktop and mobile screenshots were
inspected to confirm unobscured controls; no Go source changed after its final
complete suite.

Screenshots: [desktop](screenshots/users-management-desktop.png) and
[mobile](screenshots/users-management-mobile.png).

## Remaining limits

The 828-record official reference corpus includes 92 new video records and 61
complete new HTTP captures. Preserved incomplete reference responses are not
counted as successful workflows. The separate [copy-seek controls](../research/video-copy-seek/README.md)
show why an edit list or successful FFmpeg exit alone does not prove correct
client presentation.

Nonzero copied-video progressive seeking remains rejected. Encoded nonzero
seeks currently decode the source prefix and can exceed the startup budget for
long media; efficient proven seek points remain work. Additional codecs,
subtitles, HDR/interlaced paths, hard CPU/memory isolation, actual hardware
decode/encode, and real-client acceptance remain open. Native metadata/settings,
device/key/task/log management and backup/restore also remain open. No consumer
playback page is included in the administrator dashboard.
