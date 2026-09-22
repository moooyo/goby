# Phase 3 native fixture preparer

Status: **unexecuted source; no admitted capacity profile**. This source belongs
to consolidated Phase 3 remote verification, after all Phase 3 implementation
is complete. It must not run on a developer machine or the shared test host.

The preparer consumes a frozen, explicitly approved single-tier manifest from
`media-analysis-phase3-workload-profiles.example.json` and a private operator
binding. The controller must already own an isolated VM 106, PostgreSQL and a
native Goby instance with no initialized administrator or libraries. Goby must
allow the future workspace media root, have real FFmpeg/ffprobe and the pinned
intro fingerprint dependency configured, enable analysis and scan disk evidence,
and have the preview width 240 available. The actor does not configure service
deployment, start a service, mount a volume, or claim profile admission.

All accounts, libraries and user state are created through production HTTP.
Read-only PostgreSQL observations bind resulting opaque IDs and query truth.
The script never inserts catalog rows. Both its generated short fixtures and
the licensed sources are scanned by Goby's actual prober.

## Private operator binding

Invoke a fresh source-pinned process with fixed argv:

```text
/usr/bin/python3 -I -B media-analysis-phase3-prepare.py prepare --operator /owned/controller/operator.json --output /owned/results/new-preparation
```

The operator file is canonical absolute, owner UID, mode `0600`, one link and at
most 2 MiB. Its exact keys are:

```json
{
  "schema_version":1,
  "manifest_path":"/owned/controller/profile.json",
  "manifest_sha256":"<exact approved profile SHA256>",
  "workspace_root":"/owned/phase3-new-fixture",
  "owner_file":"/owned/controller/owner.json",
  "origin":"http://127.0.0.1:18099",
  "app_pid":1234,"app_start_ticks":123456,
  "cgroup_path":"/sys/fs/cgroup/owned-phase3-service",
  "postgres":{"cgroup_path":"/sys/fs/cgroup/owned-phase3-postgres","postmaster_pid":2345,"postmaster_start_ticks":234567,"postmaster_started_at":"2026-09-22T00:00:00.000000Z"},
  "process_observer":{"argv":["/usr/bin/sudo","-n","/usr/bin/python3.13","-I","-B","/opt/goby-phase3-runtime-setup-20260922-01/process-observer.py","--binding","/var/lib/goby-phase3/tier10k/control/process-observer.json","--binding-sha256","<binding SHA256>"],"binding_sha256":"<binding SHA256>","source_sha256":"<root observer source SHA256>"},
  "media_read_gid":1234,
  "pg_env":{"PGHOST":"/owned/postgres/socket","PGPORT":"5432","PGDATABASE":"owned_phase3","PGUSER":"owned_observer","PGPASSWORD":"<private>"},
  "tools":{"psql":"/absolute/canonical/psql","ffmpeg":"/absolute/canonical/ffmpeg","ffprobe":"/absolute/canonical/ffprobe"},
  "tool_sha256":{"psql":"<sha256>","ffmpeg":"<sha256>","ffprobe":"<sha256>"},
  "setup_token":"<native deployment setup token>",
  "admin":{"Name":"Phase 3 administrator","Password":"<fresh private password>"},
  "viewer":{"Name":"Phase 3 workload viewer","Password":"<fresh private password>"},
  "licensed_manifest":{"path":"/owned/controller/phase2-manifest.json","sha256":"<frozen Phase2 manifest SHA256>"},
  "licensed_paths":{"<case_id>":"/owned/source-copies/actual-source.mp4"},
  "analysis_case_ids":["<selected repeated episode case>","<another case in that cohort>"],
  "preparation_seconds":14400,
  "max_fixture_allocated_bytes":17179869184,
  "scan_evidence_path":"/owned/controller/scan-evidence.json"
}
```

`licensed_paths` must map all 14 case IDs from the retained Phase 2 manifest to
their already transferred guest-local files. This explicit path rebinding does
not rewrite the original manifest or labels. Each source's original byte count
and SHA256 must match before copying; the owned copy and original must still
match afterward. The script never downloads, reads the shared test host over a
network, or interprets license evidence as permission to invent new sources.
The controller retains the reviewed licensing/provenance receipt separately.

`analysis_case_ids` selects two through fourteen actual source cases, including
at least two from the same preserved role/split/series/season cohort. The script
maps them to actual scanned item IDs. The capacity workload does not relabel
these sources or claim a new intro accuracy calibration. Every licensed case is
included in the media inventory, including unselected analysis cases.
Cases that share an original episode, variant group or content hash are merged
transitively into one independent source group. Variants of one episode retain
the same catalog episode number inside their cohort. A private mapping preserves
the original series/season/episode identities and the explicit
`test_indexing_container` role of the generated Season 01 directory. Fourteen
licensed source cases are never reported as fourteen independent originals.

The controller's owner file must match `owner_id`, `run_id`, `source_revision`,
`guest`, `app_pid`, `app_start_ticks`, `cgroup_path`, `postgres`, and `workspace_root`.
The workspace must not exist. Every write stays beneath that exact workspace.
The service and PostgreSQL run in separate, nonnested measured cgroups; the
preparer and generators remain outside both. Goby and the actor have different
UIDs and share the runtime-created `media_read_gid` supplementary group. The
workspace root is actor-owned `0710` in that group; registered media directories
and files are explicitly `0750` and `0640`. Actor `private/` and credentials stay
`0700`/`0600`. Public fixture templates and addition staging live separately in
`media-fixtures/`, so moving a media file does not make it unreadable to Goby.
No default ACL is relied on. The runtime's fixed read-only root process broker
supplies protected `/proc` and scan-configuration observations; its exact argv,
source and binding hashes are included in the typed context.
The service's allowlist must permit the named future media subtree before it
starts. An unsuccessful run is never retried over partial state.

## Generated layout and frozen truth

The preparer creates three small, genuine generated media templates: H.264 MP4,
H.264/AAC MP4 for 120-second playback/seek/user-state witnesses, and FLAC. The
large population uses hardlink batches capped at 10,000 aliases per template
inode, so the 100k fixture does not depend on an unlimited filesystem link count.
It records media paths, content templates and hardlink batches separately. It
does not call aliases distinct licensed originals.
Before writes, the actor reserves conservative space for copied media, template
limits, staging, directory/inode entries, hardlink directory entries and the
inventory. Hashing/copying reads a fixed preobserved length, checks the deadline
between chunks, and rejects source growth, replacement or metadata changes.
Inventory serialization measures the exact JSONL byte count before reserving
space, then writes the same row order incrementally and releases the source
rows. It retains private exclusive creation, flush/fsync and the final content
hash without retaining a second full encoded inventory in memory.
The final physical allocation counts unique device/inode identities and is
checked against the same ceiling. Decoder `-fs` limits are emergency bounds;
ffprobe must still confirm full template duration, so truncation cannot pass.

There are 4,200 traversed directories and 80 long-named, zero-byte nonmedia
entries in each directory. Their raw UTF-8 entry names alone exceed 64 MiB.
This independently exercises more than the old 4,096-directory handle boundary
and the old raw-evidence byte boundary. These entries are explicitly reported
as nonmedia fixtures; they do not inflate actual media bytes or catalog leaves.

The initial real scan indexes `tier/2 + 100` stable seed movies, eight separate
playback/user-state/mutation/audio witnesses, the fixed directory hierarchy and
the 14 real licensed cases in preserved isolated cohorts. The script observes
the actual total including all library, folder, series and season items, then
adds exactly `tier - initial_catalog_count` media files beneath existing
directories. No new directory is introduced after the count is measured.
The final expected catalog count is therefore exactly 10,000 or 100,000.

New expansion names sort after every frozen page. The cold phase queries the
already indexed Seed folder while forcing a real probe of the whole capacity
library. Cached and incremental list/count/search queries cover the complete
capacity library. Exact shallow and deep page IDs are calculated independently
from the preindexed rows; the future expansion count supplies exact full-tier
totals. The actor never copies expectations from the API response it later
checks. Deep pages start at `tier/2` and have 20 known seed IDs. Unicode matches
are fixed seed names. A dedicated State folder supplies one stable favorite,
Resume and Latest witness; active playback uses different items.

Deletion, addition and the cross-root move all use separate single-link files.
The deletion and addition are both Movie leaves, so incremental totals stay
constant; neither appears on a frozen returned page. The move retains the
basename and inode across two registered roots of the same library, with a
favorite user-state witness. `The Witness` supplies the real generated-sort-key
transition while concurrent metadata edits affect only Overview. Names, users
and returned-page identities are kept separate from these mutations.

Cold means `ForceProbe:true` over a partially indexed corpus. It explicitly does
not mean a brand-new empty catalog or a cold operating-system page cache. Initial
scanning, hashing and decoding have already read the media. Cached and
incremental pass expectations are frozen before the workload begins.

## Handoff and closure

On successful preparation the private workspace contains:

- `private/workload-manifest.json`, exact approved manifest bytes.
- `private/workload-context.json`, complete typed driver input with actual IDs,
  credentials, root bindings, per-phase expectations and source paths.
- `private/inventory.jsonl`, one explicitly classified row per media pathname.
- `workload-owner.json` (`0600`), derived finite scope bound to the original
  controller ownership and actual native root identities.
- `media-fixtures/templates/` and `media-fixtures/staging/`, generated source and mutation
  inputs. These are outside every registered root and excluded from scan counts.

The preparation output's `prepared.json` is safe to review and always says
`accepted_capacity:false`. Its private receipt includes exact handoff/template
paths and actual root IDs. Raw responses can contain credentials and media
paths; never publish the private directory. A failed run preserves a private
partial owned-state receipt and returns nonzero. The controller must reconcile
possibly late admissions and close all credentials, jobs, services, PostgreSQL,
guest resources and the partial workspace. Preparation does not prove closure.

## Small fault fixture extension

After the complete compound capacity journey has produced its accepted result,
and after the guest helper's actual `prepare_volumes` operation, invoke:

```text
/usr/bin/python3 -I -B media-analysis-phase3-prepare.py fault-fixture --operator /owned/controller/fault-operator.json --output /owned/results/new-fault-preparation
```

Its private operator has exactly `schema_version:1`, `manifest`, `base_context`,
`guest_binding`, `prepare_receipt`, `templates`, `media_read_gid`,
`after_compound:true` and `compound_receipts`. The four ordinary
reference inputs are pinned `{path,sha256}` references to actor-owned `0600`
copies of the controller's exact frozen bytes. `templates` has exactly
`short_video` and `playback_video`, each `{path,sha256,bytes}`, referring to this
workspace's approved `media-fixtures/templates/short.mp4` and `playback.mp4`.
No arbitrary source, executable, SQL or target path is accepted.

`compound_receipts` has exactly `result`, `events`, `mutation_before` and
`move_userdata_before`, all `{path,sha256}` references frozen from the actual
completed run. `result` is that output directory's `result.json`; `events` is
its `events.jsonl`. The two SQL receipt paths must be inside the same `private/`
directory. `mutation_before` selects the actual Actor snapshot with full-tier
count, the original `move_from` row and the pre-deletion `delete_id`.
`move_userdata_before` selects the actual SQL receipt querying all rows for that
same moved item. The exporter finds these by their captured statement and raw
values, never by inventing IDs. Each SQL receipt's child-output digest is
checked. The frozen events bind the HTTP body references used to recover the
original settings and metadata controls.

This mode requires `accepted=true`, `execution_complete=true`, all result checks
true, empty failure/cleanup errors, exact original run/owner/source/tier/
manifest/driver binding, all three scan/count/overlap phase records and the
nonzero deletion/stable-move proof. A fault wrapper's partial run cannot qualify.
The original context and capacity artifacts remain unchanged. The old initial
seed count is not applied to the finished full-tier catalog.

The complete Actor cleanup reverses the three owned filesystem renames but does
not scan again. Therefore the original deleted file is back on disk while its
old database ID remains deleted, the incremental added ID still points at a file
returned to staging, and the stable moved ID still points at its temporary root.
Total count alone cannot establish a consistent cached baseline.

Before any fault-library or sentinel write, the preparer checks actual absence
of active scans, tasks, encoders, playback sessions, original leases, media
children and retiring spool work. It rechecks the original inventory, full
media bytes, the three restored single-link paths and absent temporary targets.
It binds the moved inode to the original captured file identity, and the staged
addition to the still-catalogued incremental item's file identity. The old
deleted file's pre-mutation inode was not independently recorded by the original
Actor, so the receipt does not invent that observation; its current content is
bound to the admitted inventory and the source-pinned successful cleanup.

`after-compound-plan-private.json` is frozen before one real reconciliation
admission. The request uses `ForceProbe:false`, with `Scanned=M` from the
original full capacity library and **Added=1, Updated=1**. This follows the
frozen path classes: the restored deleted pathname creates one new item, the
moved inode updates its one existing row back to the original root/path, and
the staged-out incremental item is removed. An admission timeout is reconciled
against the sole newly observed scan ID; the POST is never blindly repeated.

Completion requires those predeclared counters, unchanged full-tier count,
exactly one removed incremental ID, a new identity at the restored deleted path,
the moved ID/root/file identity and user data preserved, and SHA256 equality of
all unaffected catalog rows, user data, settings and the metadata witness.
The catalog and user-data digests use the versioned
`sha256-ordered-row-hashes-v1` format. Each selected catalog row first hashes the
UTF-8 PostgreSQL canonical `jsonb_build_array` text for the unchanged columns
`id,library_id,root_id,parent_id,type,path,relative_path,name,sort_name,file_identity,file_size`.
The lowercase 64-character SHA256 values are then joined with one LF between
rows, ordered by `id`, with no trailing LF; SHA256 of that UTF-8 string is the
population digest. User data retains every previous `to_jsonb(u)` field,
including both identity columns, and uses the same two-layer format ordered by
`user_id,item_id`. An empty set hashes the empty byte string. Row identities
remain in their row digests, so neither ordering nor row boundaries are omitted.

A materialized count-only CTE and `CASE` gates precede each population aggregate.
Catalog population may not exceed the original tier (itself at most 200,000),
and user data may not exceed 200,000 rows. Counts are returned in the same SQL
snapshot; the transition still requires the exact original tier before and
after. The unchanged-row selection and identity-difference checks are unchanged.
There is no `LIMIT` or silent tail truncation. A 100,000-row digest concatenates
at most 6,499,999 bytes of row hashes, rather than complete row JSON repeatedly.
The separate moved-item witness array is materialized only when its count
matches the already pinned original witness count, bounded at 4,096 rows.

The old deleted ID and its user state are not revived. API readback supplies the
actual new media-source and source-revision references. Media files must remain
unchanged by this scan. A separate `after-compound-handoff-private.json` records
these observations and the original result reference; this preparation scan is
outside, and never retroactively added to, the capacity latency/count result.

The guest binding includes `fixture_access:{actor_uid,media_read_gid}` and at
least two media volumes, one replacement and one derivatives volume. The root
helper creates fixed media/replacement `fixture-media` children owned by the
actor (`0750`) and derivatives `analysis-cache` owned by Goby (`0700`). Mount
roots and their controlled parent chain permit group traversal. The helper's
actual prepare receipt binds each directory's path, inode, device, permissions,
actual filesystem UUID and DM UUID. The preparer verifies live mountinfo,
device/inode, DM identity and loop backing before writes. All volumes stay
mounted read/write during this step.

At most two approved media copies are written per media/replacement root, one
at the top and one under `nested/`; aggregate reserved allocation is capped at
64 MiB. Every library is created and scanned through native APIs. Native root
binding UUIDs must match the volume identity. A new ordinary-filesystem healthy
library contains separate progress, metadata and PostgreSQL-lock witnesses.
A fresh sentinel user and real persisted resume progress are created through
the APIs. These identities differ from the compound actor's editable/playing
items and user.

`fault-fixture-private.json` contains actual library/root/item/media-source IDs,
native root binding DTOs, source hashes/stat identities, media clocks, sentinel
credentials and `state_refs`. `mutation_ids` are finite names for future
acknowledged operations, not proof those mutations already occurred.
`fault-prepared.json` contains safe preparation counters and never claims
capacity or recovery passed. The runtime/oracle builder consumes the private
receipt; business IDs must not be typed by an operator.

New `private/fault-workload-context.json` and inventory include these actual
roots, and their initial/cached count is the observed full tier plus the actual
fault extension delta. The fault receipt pins the original compound result and
the independent reconciliation handoff and declares
`allowed_workload_phases:["cached"]`. The wrapper/exporter must enforce this
closed phase set; it must not call the full Actor `execute()` or rerun cold or
incremental work from this derived context. Historical unused selectors remain
source facts, not another whole-profile acceptance contract. The cached scan
keeps the original `M/0/0` counters. The old deleted ID and incremental added ID
must not appear in frozen query pages, playback, analysis or metadata selections;
otherwise preparation fails rather than silently replacing those references.
The original exact 10k/100k capacity context and report remain unchanged.

The controller separately freezes the fault profile, remounts declared read-only media, and verifies that Goby's
derivative-cache deployment uses the returned `analysis-cache` path. Populating
a derivatives volume alone is not ENOSPC consumer evidence.
