# Phase 3 HTTP and media probe

Status: source implementation only. No acceptance result is established by this
document. Run only in the explicitly owned remote guest after complete Phase 3
source freeze and campaign admission.

`media-analysis-phase3-probe.py --binding PRIVATE_JSON` reads one bounded JSON
request from stdin and writes one JSON response. `--context` is an alias. The
recovery transport independently verifies the helper and binding hashes before
dispatch. Credentials, source paths, references and downloaded bytes stay in the
guest's private directories. A successful RPC reports actual observations; the
external oracle determines whether the declared fault/recovery requirements pass.

## Binding and envelope

The binding has `schema_version: 1`, `vmid: 106`, `run_id`, `owner_id`,
`profile_id`, `source_revision`, `tier`, `machine_id`, `smbios_uuid`,
`owner_file`, loopback-only `origin`, and a pre-existing root-owned 0700
`artifacts_directory`. `owner_file` is a root-owned 0600 JSON object whose
`owner_id` matches. `service` binds `unit` and `executable_sha256`; the actual
running MainPID, start ticks, cgroup and executable are checked for every RPC.
A changed application generation during the RPC fails the observation. It is
not a service-control endpoint.

`credentials` contains `admin_cookie`, `csrf_token`, `emby_token`,
`emby_authorization` and `user_id`, plus an optional `device_id`. The exact Emby
authorization header must match the login that issued the token. It is never
included in a response. `tools.ffmpeg` and `tools.ffprobe` each bind an absolute
executable path and SHA-256. Tool execution uses a held, verified executable FD,
a private process group, bounded output/deadline and actual group closure.

Each entry of `roots` contains:

- `root_id`, `library_id`, `item_id`, `media_source_id`, and `item_id_ref`.
- `lock_item_id` and `preview_item_id` for roots selected for PostgreSQL lock
  and derivative-volume ENOSPC scenarios. These are actual prepared item IDs.
- `container`, `source_sha256`, `source_bytes` (at most 128 MiB), and an
  immutable `reference_file` outside fault mounts.
- `source_clock_origin_ticks` and `tolerance_ticks` (at most 30,000,000).
- `source_identity: {path, device, inode, mount_id}` from preparation. These are
  explicitly expected facts, not a substitute for the fault helper's observed
  `/proc` FD/device/mount witnesses.
- `search_params`, exact `search_total` and ordered `search_ids` for a fixed,
  bounded healthy-root query.
- `playback_body`, a real PlaybackInfo direct-play profile compatible with that
  selected reference. The helper fills the current user, start position and
  source selection at reconnect; it never forces an old source revision after
  a legitimate storage recovery.

The request has exactly `schema_version`, `request_id`, `operation`, `run_id`,
`owner_id`, `profile_id`, `source_revision`, `tier`, `scenario`, and `payload`.
The response has `schema_version`, `request_id`, `operation`, `owner_id`,
`vmid`, `status` (`ok` or `failed`) and `data`. Failures use bounded machine
codes, never exception text containing a credential, path or URL query.

## Operations

| Operation | Payload | Actual observations |
| --- | --- | --- |
| `healthy_probes` | `{earliest_unix_ns, roots:[root IDs]}` | Per-root exact search; full bounded original HTTP media with byte hash and decoded-frame comparison; actual native scan admission and returned job/run IDs. Admission is not scan completion; the oracle must inspect the named job. |
| `affected_probe` | `{root_id, kind:"read"|"metadata"|"scan"|"lock_metadata"|"preview", timeout_seconds}` | Actual media/binding reads, Force scan admission, unchanged native metadata PUT taking the catalog-owner row lock, or Force preview admission. Admission is not task completion or a product write failure. The oracle must inspect the actual named task and SQLSTATE/error. |
| `runtime_resources` | `{}` | Actual native runtime resource DTO plus HTTP receipt and application PID/start. The oracle binds real original-stream lease records, scan retirement and observation-slot facts to the relevant operation. |
| `readiness` | `{}` | Actual `/readyz` response, accepting either 200 or 503 as an observation. HTTP status is not a guessed SQLSTATE. |
| `playback_resume` | `{persisted_ticks, snapshot_sha256, item_id_ref}` | Current HTTP UserData readback, fresh PlaybackInfo and Playing admission, source-bound HTTP delivery, actual client frame selection at the persisted position, and successful Stopped response. |

HTTP receipts include `http_status`, `duration_ms`, start/completion Unix
nanoseconds, `response_sha256`, `bytes_received`, `client_timed_out`,
`client_disconnected`, server `request_id` when available, `method`, and
`path_sha256` without the URL query. A timeout is only a caller observation.
It does not establish that a storage worker stopped.
Connect, request, headers and body reads share the request's wall-clock budget.
An operation failure retains completed HTTP receipts without exposing request
credentials or raw response bodies.

Media evidence contains the source and delivered media hashes, source/response
RGB frame hashes, requested and measured start ticks, one decoded 64x64 RGB
frame, and the actual FFprobe/FFmpeg child identities, exit status, output hashes
and group-closure receipts. FFprobe measures actual presentation timestamps;
the requested position is never substituted for a measured value. FFmpeg uses
`-copyts -seek_timestamp 1` for source-time selection, following the pinned
[FFmpeg 9.0.1 input option contract](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/doc/ffmpeg.texi).
Both the downloaded bytes and the reference are rehashed after decoding.

Resume follows the fresh server-generated original-media URL and validates its
item, source and PlaySession scope. The URL query and its token are not exported.
The response includes `current_media_source_id`, `media_decode`, and complete
`http_evidence`, including the actual Stopped response. The external observer
must separately prove that the starting position belongs to the durable ACK
chain. Frame equality alone is not persistence evidence.
The Playing/Stopped calls legitimately update playback state. Compare durable
state before those calls, and acknowledge their actual postimage separately if
later recovery comparisons include it.
The response additionally identifies the actual user, item and PlaySession.
A successful resume writes a bounded request-derived `.probe.json` filename and returns its
`private_artifact` reference. The file contains the complete original successful
RPC response before adding that reference, avoiding a self-referential hash.
For state acknowledgement, `artifacts_directory` must equal the state observer's
private directory, and `snapshot_sha256` must be the before-snapshot SQLite
file hash. The state helper independently checks the new stopped/counting
session and durable user-data postimage.

Only exclusively created per-request media files are removed; identity is
rechecked before unlink. Evidence JSON, reference media, application data and
fault mounts are not deleted by this helper. Missing/corrupt evidence, an
unclosed child or a changed instance fails the RPC and cannot become an
acceptance pass.
