# Selected compatibility phase 4 execution record

Status: **closed within the recorded selected and composed scopes**. Consolidated non-AMD
scopes, affected fixture repairs, the actual browser journey, selected mocked UI
suites, selected AMD execution and owned-resource closure have recorded evidence.
This record does not claim a final whole-suite rerun, merge, push or deployment.

Phase 4 follows phase 3 closure at `84d5d74` on
`codex/selected-client-compatibility`. The initial complete implementation freeze
was `8ed6a7b` (203 changed files), followed by the schema catalogs at `096d531`.
The last product change, `daeef6a`, moved the notification fence to the
local-credential mutation path after compilation exposed an invalid fence in
the read path. Repairs through `27d862a` change test sources only. Native
administrator assets remain those built at `8ed6a7b`.

The user authorized local compilation and unit tests; actual integration, media
and browser execution remains on `test-env`, with the selected AMD profile's
existing CT 104 exception through `ssh pve`. Actual local E2E is prohibited.
The `ui-ux-pro-max` skill remained disabled. Original Emby Web commercial
restrictions are not an acceptance gate for the selected server adapters;
earlier blocked-client results remain unchanged.

## Cohesive delivery groups

| Group | Implemented delivery | Recorded acceptance boundary |
| --- | --- | --- |
| D1 Configuration inventory | Closed field contracts, defaults, partial/full/reset behavior, CAS and effective consumers | Settings, migration and adapter tests; field contracts remain explicit |
| D2 Backed execution settings | Desired HTTP bind/port, CPU/authorized AMD selection, per-job threads and CPU H264/HEVC quality, software/Vulkan tone-map gates | Actual CPU output, listener restart and affected selected AMD execution |
| D3 Management adapters and UI | Current policy eligibility counts, supported deletion-folder choices, local import/library options and backed capabilities | Server/library scopes and selected management-form cases; no new live device-page acceptance |
| D4 Client protocols | Selected aliases, client-correlated playback reports, session reconnect and remote command execution | Actual reference client played audio, executed Pause, reconnected and stopped its producer |
| D5 External notifications | GobyWebhookV1 HTTPS transport, sealed credentials, transactional source journal, current-reference authorization, bounded retries and revocation fences | Independent receiver and console consumer observed actual delivery, retry, rotation, no-WebSocket delivery and suppression/revocation/disable |
| D6 Integrated durability | Exact catalogs, target-owned host setting capture, portable encoding preferences, raw archive and secret validation before normalization | Database/backuppg/recoverydb/recovery scopes, affected repair and final owned-resource closure |

Field-level contracts are in [managed execution settings](../api/managed-execution-settings.md),
[encoding controls](../api/encoding-controls.md), [managed hardware](../api/managed-hardware.md),
[HTTP binding](managed-http-binding.md), [selected management](../api/selected-management.md)
and [notifications](../api/notifications.md).

Live TV, EPG, DVR/recording/tuners, DLNA, external channels and group playback
remain excluded. Provider-online, OCI, non-AMD hardware, native arm64, broader
capacity/platform delivery and other unselected work remain deferred.
GobyWebhookV1 does not imply APNs, FCM, proprietary Emby push, mobile background
delivery, OS toast observation or arbitrary third-party-client parity.

## Evidence composition and source identities

The [machine-readable record](selected-compatibility-phase4-results-20260920.json)
retains source identities, executed binary hashes, selectors, original failures
and receipt hashes. Private evidence is retained under
`D:/Code/goby/.git/selected-compatibility-20260920/phase4` and the owned
`/opt/goby-selected-compatibility-20260920-p4a` directory on `test-env`.
Credentials, DSNs, private execution context and environment contents are not
copied here. Intersecting scope counts are not additive.

The frontend build at `8ed6a7b` passed. Its ignored dist archive has SHA-256
`a1911e46157131f7e7853d7a08caf50026efd86e68ac0a85537ee88c15a1fdff`.
Nineteen selected Go artifacts compiled: sixteen test binaries, ordinary and
embedded applications, and the independent notification receiver. Core package
binaries use `096d531`; the server/application/recovery group uses `daeef6a`.
The earlier server compilation failure at `096d531` remains in its receipt.
No actual local E2E or media execution was performed.

A separately compiled exporter applied exact embedded migration prefixes to
fresh PostgreSQL 17 databases and exported:

| Catalog | Bytes | SHA-256 |
| --- | --- | --- |
| Schema 47 | 793851 | `8ae91ff4c23fda580c4f5ab0cbb525cff5be7a083689f1f877b7911a072e422b` |
| Schema 48 | 852066 | `c46a8d7a4b3cbca05ae984a7b7e04e276c18b924fffeb99f9efad1003caf6451` |

Schema 47 adds typed runtime overrides to managed settings. Schema 48 adds five
notification transport, registration, source-journal and delivery tables. The
current inventory is 61 tables. Historical catalogs and digests remain unchanged.

## Consolidated and affected verification

`verification-01.json` records a failed runner attempt: all sixteen groups exited
before any parent test passed because the Go test2json runner lacked a configured
build-cache location. The second runner supplied an explicitly owned cache and
new receipt/log/run identifiers; this was not a product change.

`verification-02.json` is the first complete executed batch. All sixteen process
groups closed. It retains **2857 passing parents, 10 failed parents and 12
explicit parent skips**; its aggregate result remains **FAIL**.

| Scope | Binary source | Passing parents | Failed parents | Skips |
| --- | --- | ---: | ---: | ---: |
| config | `096d531` | 60 | 0 | 0 |
| activity | `096d531` | 29 | 0 | 0 |
| settings | `096d531` | 48 | 0 | 0 |
| identity | `096d531` | 195 | 0 | 0 |
| database | `096d531` | 64 | 0 | 0 |
| playback | `096d531` | 228 | 0 | 0 |
| transcode | `096d531` | 313 | 0 | 9 |
| notificationjournal | `096d531` | 3 | 0 | 0 |
| notifications | `096d531` | 4 | 0 | 0 |
| library | `096d531` | 795 | 7 | 1 |
| server | `daeef6a` | 936 | 1 | 2 |
| backuppg | `daeef6a` | 114 | 1 | 0 |
| recoverydb | `daeef6a` | 12 | 0 | 0 |
| recovery | `daeef6a` | 31 | 1 | 0 |
| cmd/goby | `daeef6a` | 24 | 0 | 0 |
| actual native browser | `daeef6a` | 1 | 0 | 0 |

The skips retain their hardware/RPU, mount-namespace-helper and selected AMD
environment gates. None is counted as a passing execution. The server AMD cases
have separate admitted CT 104 evidence described below.

All ten failed parents were fixture or expected-inventory mismatches:

- Catalog aggregation fixtures needed the transactional notification journal
  interface. They now inspect that journal while retaining publication,
  rollback, merge, resync and limit assertions (`1f1c891`).
- The media-publication fixture used parameterized multi-statement SQL. It now
  seeds the same rows with valid prepared statements; the prepared-recovery
  barrier assertions remain intact (`f6ccaf4`).
- The configuration clone fixture used an ephemeral deployment port as a
  writable override, then supplied an incomplete nullable Network group. The
  fixture now uses a writable port and the complete closed group (`c380fc7`,
  `7783e1c`); production validation was not relaxed.
- The exact catalog suffix now requires `management, runtime_overrides` in that
  order (`df177f5`).
- The recovery witness incorrectly required cancelled delivery refs to remain
  populated. Only the expected source-side projection predicts the specified
  clearing of original pending/sending refs. The target still compares actual
  refs and requires them to be empty; raw fingerprints, ciphertext, source
  journal, terminal history and unrelated fields retain exact checks (`6962a8f`).

`verification-repair-01.json` passed the four affected scopes with no skips or
failed tests and all four process groups closed:

| Scope | Executed source | Passing parents |
| --- | --- | ---: |
| library | `f6ccaf4` | 8 |
| server configuration | `7783e1c` | 1 |
| backuppg inventory | `df177f5` | 1 |
| recovery host/notification archive | `6962a8f` | 1 |

The eleven repair parents include one related library admission guard alongside
the ten original failures. They are not eleven additional unique parents to add
to the first batch. Original receipts remain unchanged. This composes affected
passing scopes rather than claiming a final complete-suite rerun.

## Actual native and reference-client journey

The local directory `browser01` contains run
`selected-phase4-daeef6a-browser02`: the first effective actual browser execution
passed all **18 stages**, each with independent database/file acknowledgement.
There were zero page errors, foreign requests, retired GET reads, active sessions
before fallback cleanup or fallback session revocations.

Stages cover authenticated profile locking, settings save, capability
registration, audio playback, remote Pause, reconnect, stop, notification
configuration/retry/no-WebSocket delivery/hidden-source suppression/rotation/new
target/revocation/disable, listener restart, fresh CSRF editing and cleanup.
The PIN remained an authenticated profile lock; server PIN login was not attempted.

Audio advanced from 1.221333 to 2.058667 seconds across eight samples. The
media-connected Web Audio graph recorded nonzero RMS/peak values without a
synthetic signal. Pause changed the actual element to paused at about 2.330
seconds, with a 204 progress report and matching session state. Reconnect changed
socket generation from one to two and fetched fresh state; missed-command replay
is not claimed. Stop and active-encoding deletion returned 204, media detached,
and its audio context closed. Independent evidence records
`ClientCorrelated=true`, one counted playback and closed stopped-producer
resources. Physical speaker output was not observed.

Saved CPU policy produced a real progressive H264/AAC output: 192/192 expected
frames, eight seconds, 96-by-54 video, one audio stream and complete zero-error
decode. Its admitted plan retained Threads=2, H264 preset=fast, capped_crf and
CRF=23; prepared-only negotiation did not count playback. Output SHA-256:
`b5962e24648a05a281950148d2f15d8be9a2b10fac43fb2b25a4e8b7b18758d7`.
Probe and decode process groups closed. This is CPU evidence, not an AMD result.

After actual listener restart, Active and Desired matched the new loopback port
and RestartRequired was false. A fresh authenticated CSRF save advanced settings
to revision three and Threads=3 while retaining the quality policy. This browser
journey uses IPv4; the separate network tests retain their own additional scopes.

Five masked screenshots were reviewed by the coordinator. The two Settings
captures show the lower page portion and do not establish a complete full-panel
visual review. Other captures show the paused client and configured/disabled
notification controls. They supplement the state and media assertions.

## External notification observations

The HTTPS receiver and console consumer were independent processes. Certificate
verification was not bypassed. The receiver accepted the same Test event after
two attempts and the console consumer recorded consumption. A subsequent
UserDataInvalidated event was accepted and consumed without a WebSocket.

Hidden-source evaluation created no delivery or receiver receipt and did not
change public LastOutcome or registration UpdatedAt. Rotation advanced the
private generation and retired old deliveries; a generation-two event was then
accepted and consumed without a WebSocket. Revocation and disable produced no
new delivery or receipt. Three retained receipt digests bind the retry, offline
and rotated-target observations.

This establishes the recorded GobyWebhookV1 reference-consumer contract, not
APNs/FCM, a human read, an OS toast, or arbitrary third-party-client parity.

## Mocked administrator scope

`mocked-browser-01-selected-acceptance.json` derives the intended scope from the
unchanged raw report: runtime settings **8**, notifications **6**, and management
configuration **4** cases all passed, for **18 selected cases**. No selected case
was skipped or flaky; there were no unexpected results, report errors or
unhandled API requests. Mocked behavior remains separate from actual backend
and browser acceptance.

The runner mistakenly also selected one pre-existing live-device journey. It
correctly skipped without its dedicated live-environment gate. The raw aggregate
receipt remains **FAIL / complete=false**, despite exit code zero and the
intended cases passing. Those eighteen cases were not rerun. No new live device
management UI acceptance is claimed.

## Selected AMD affected acceptance

Status: **two selected cases passed across the retained original and affected
repair runs**. The first non-root CT 104 run used product source `daeef6a`. The
existing AV1-padding fallback case passed. The new managed AMD case reached GPU
Threads=3 and decoded both outputs, but a later CPU assertion selected the old
job through a helper keyed only by reused prepared PlaySessionId and codec. That
original run remains failed.

Test-only `27d862a` selects the unique new producer by excluding the known old
job ID. It does not filter by expected hardware/thread values or relax the CPU
Threads=5 and persisted-plan assertions. Its binary SHA-256 is
`b1c01db3f39850a95c0cbdc22d70b5962a8c27b2b63a9e29b9de3d3396bce252`.

The retained `amd02` worker and supervisor receipts show that only this repaired case passed in the separate
owned CT 104 root `/opt/goby-selected-compatibility-20260920-p4b`, as non-root UID
1000 with render GID 992, using the same pinned FFmpeg 9.0.1 toolchain. Supervisor
and worker complete flags are true and the `amd-27d862a-01` cgroup is closed. The
actual stage log records completed software-decode/VAAPI-encode execution with
Threads=3, then completed software/software execution with Threads=5,
`job_reused=false` and `play_reused=true`. The already passing AV1 case was not
repeated. Both AMD cgroups are closed; receipt and log hashes are recorded in
the companion JSON. The initial failed aggregate remains failed.
These scopes reuse unchanged product code from `daeef6a`; frontend assets remain
`8ed6a7b`, and intervening revisions alter test sources only.

## Resource closure and disposition

The complete batch's sixteen groups and repair batch's four groups closed. The
actual browser receipt confirms joined workers/listeners, removed owned random
schema/media/private context, retired sessions, and closed browser/receiver/
consumer groups and observed descendants. The mocked runner confirms closed
processes/static listener, zero remaining connections, and removed temporary
profiles/dependency link.

`closure-01.json` is complete. The owned PostgreSQL service changed from PID
3498052 to zero; PGDATA and evidence are retained. All five VM worker/service
records have terminal processes and empty cgroups, and both CT AMD cgroups are
closed. The bridge closed its connections and joined all thirteen workers.
Failed worker result states remain failed rather than being rewritten as passes.

The unrelated original-client service retained PID 366598 and invocation
`c883bdf04e2f4d7ea6526fcd858fc342`; it was not restarted or changed. With this
closure, phase 4 is closed for the selected/composed scopes described above.
Handoff/current-status integration, final merge and push remain coordinator work
and are not claimed here. No new tests or runtime probes were executed while
composing this record.
