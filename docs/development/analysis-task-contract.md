# Analysis task admission and publication

This is the phase 2 generic-task integration contract. Source tests are added
with the implementation; the phase's consolidated remote run owns execution
results. This document does not claim completed intro or preview acceptance.

`StartRequest.AnalysisInput` is a typed `*library.AnalysisSelection`. Only
`media.intro_analysis` and `media.preview_generation` accept it. Nil on these
new tasks means the normalized empty selection; legacy tasks reject a non-nil
selection, including an empty object. Legacy nil-input request fingerprints
retain the exact existing TaskID/Executor/Source JSON bytes.

Selections are cloned, bounded, deduplicated by rejection, and sorted before
fingerprinting. Named libraries must exist and be physical movies, tvshows or
mixed libraries. Named items must exist, have a matching registered root, be
nonfolder Movie/Episode/Video leaves, and belong to any explicitly selected
library set. Music and the synthetic collection library are incompatible.
Item selection narrows the child-library population to actual item owners.
Source eligibility, authorized supporting cohorts and immutable media/profile
snapshots remain library-analysis responsibilities.

Registration requires an `AnalysisAdmission` callback for analysis tasks and
forbids it for legacy executors. Its signature is:

```go
func(library.OwnedTx, tasks.AnalysisAdmissionRequest) (tasks.AnalysisAdmissionBinding, error)
```

The request contains TaskKey, Source, a detached Selection, and a manual Actor
pointer or nil for explicit scheduler admission. Preparation reads/locks the
current configuration and returns its lowercase SHA-256 fingerprint plus a
required `Bind(tx, runID)` callback. Optional `SnapshotChildren(tx, runID)` creates
bounded cohort/chunk children and the library-owned source snapshots. Bind runs
first; both callbacks run only for a new run, inside the same owner transaction.
The generic repository checks the returned child count, real selected library
membership, nonempty analysis scope keys and unclaimed waiting state. Root
integration supplies bounded children, not a whole-library long-running worker.

A matching RequestID replays its original immutable admission before resolving
today's configuration; changing its normalized input returns ErrRequestConflict.
A new request can coalesce only with identical selection and configuration.
Otherwise ErrActiveRunConflict is returned (HTTP adapters should map it to 409).
Coalescing never replaces the original actor or upgrades a manual run to system
authority. A conflicting scheduled occurrence remains unconsumed for a bounded
scheduler retry rather than being attached to a different scope/profile.
Startup, interval and system-event dispatch return typed `AnalysisDeferrals`
diagnostics while continuing other definitions in that pass. A definition is
skipped at most once per interval/event pass, with at most two additional
conflict visits beyond its normal processing limit. The coordinator retries
only the deferred startup rules at the original startup instant, after ordinary
timed/event dispatch. Once one startup rule conflicts, remaining startup rules
for that same definition are retained without repeating profile preparation in
that pass. Initialization applies the same bound. The coordinator exposes
`DeferredAnalyses()` separately from normal scheduler readiness. This snapshot
is limited to 68 entries, replaces prior diagnostics each successful pass, and
is not a successful occurrence/run receipt. Hard scheduler errors keep their
existing failure behavior; legacy overlap semantics are unchanged.

`Work.AnalysisInput`, `AnalysisConfigFingerprint` and `AnalysisScopeKey` identify
the run/child snapshot. The library executor reads its typed cohort/source data
through ChildID. Public Work fields are separate copies from the sealed fence.
`Work.Fence(tx)` is an unexported-function-backed process capability; JSON round
trips and manually constructed Work values cannot recreate it.

`Work.WithContext(ctx)` returns a detached work value with an additional
publication cancellation/deadline boundary. It retains every earlier context,
including the sealed manager context, and checks them before and after Fence.
Background cannot extend authority, nil denies publication, and changing public
work fields remains invalid. Executors should narrow Work to their cohort/source
context; library publication also checks its own passed context around the final
fence because owned transactions intentionally shield caller cancellation.

Call Fence before business locks, then after all publication, journal and audit
writes as the final database operation. Manual authority locks precede run and
child locks. Fence validates the immutable run identity, current running state,
stop flags, database deadline, exact child/library/scope/token and absence of a
scan-job association. It rechecks current administrator/credential authority,
including the original application key/client binding and authenticated peer.
Library publication must flush any pending catalog/system events before the
final Fence; committing must not introduce a later waiting event write.

Manual audience is derived strictly from source: manual is native, compatibility
is Emby. New analysis scheduler runs explicitly store actor_kind=system with
schedule/startup/system_event source and no user/credential claims. Legacy
scheduler DTOs retain their existing empty actor-kind contract. Neither empty
actor data nor an old worker can become system authority. Existing RecoverRuns
interrupts abandoned pending/running/stopping children before a new manager;
it never issues replacement tokens for them.

The two analysis keys share the `media.analysis` group with capacity one. Waiting
for this group creates no worker and consumes no generic execution slot. A free
slot selects a run different from the previously served run when another pending
or running analysis run has waiting children; remaining ties use creation time
and immutable ID. Child pagination cannot pin the group to an off-page ordinal.
The existing generic MaxConcurrent limit, cancellation, reap and join semantics
remain in force. A durable running-child check also prevents a second claim in
the group. A completed bounded child returns its slot only after result reaping.

## Schema 50 integration fragment

Root owns migration integration. Do not modify published schema49. The generic
code consumes these columns (IDs retain history, so no new identity foreign keys):

```sql
ALTER TABLE task_runs
  ADD COLUMN analysis_input jsonb,
  ADD COLUMN analysis_config_fingerprint text NOT NULL DEFAULT '',
  ADD COLUMN actor_application_key_id bigint NOT NULL DEFAULT 0 CHECK (actor_application_key_id >= 0),
  ADD COLUMN actor_client_session_id text NOT NULL DEFAULT '' CHECK (octet_length(actor_client_session_id) <= 256),
  ADD COLUMN actor_peer_ip text NOT NULL DEFAULT '' CHECK (octet_length(actor_peer_ip) <= 256);

ALTER TABLE task_runs ADD CONSTRAINT task_runs_analysis_input_check CHECK (
  (task_key NOT IN ('media.intro_analysis','media.preview_generation')
    AND analysis_input IS NULL AND analysis_config_fingerprint='')
  OR (task_key IN ('media.intro_analysis','media.preview_generation')
    AND analysis_input IS NOT NULL AND jsonb_typeof(analysis_input)='object'
    AND octet_length(analysis_input::text) <= 65536
    AND analysis_input-'LibraryIds'-'ItemIds'-'Force'='{}'::jsonb
    AND (NOT analysis_input ? 'LibraryIds' OR jsonb_typeof(analysis_input->'LibraryIds')='array')
    AND (NOT analysis_input ? 'ItemIds' OR jsonb_typeof(analysis_input->'ItemIds')='array')
    AND (NOT analysis_input ? 'Force' OR jsonb_typeof(analysis_input->'Force')='boolean')
    AND analysis_config_fingerprint ~ '^[0-9a-f]{64}$')
);

ALTER TABLE task_run_children
  ADD COLUMN analysis_scope_key text NOT NULL DEFAULT '' CHECK (octet_length(analysis_scope_key) <= 256);
ALTER TABLE task_run_children DROP CONSTRAINT task_run_children_run_id_library_id_key;
ALTER TABLE task_run_children ADD CONSTRAINT task_children_library_scope_key
  UNIQUE (run_id,library_id,analysis_scope_key);

CREATE FUNCTION enforce_task_analysis_scope_key() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE analysis boolean;
BEGIN
  SELECT task_key IN ('media.intro_analysis','media.preview_generation')
    INTO STRICT analysis FROM task_runs WHERE id=NEW.run_id;
  IF analysis <> (NEW.analysis_scope_key<>'') THEN
    RAISE EXCEPTION 'task analysis scope key mismatch';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER task_children_analysis_scope_key
  BEFORE INSERT OR UPDATE OF run_id,analysis_scope_key ON task_run_children
  FOR EACH ROW EXECUTE FUNCTION enforce_task_analysis_scope_key();
```

Retain UNIQUE(run_id,ordinal). Legacy children always use the empty scope key,
preserving their previous per-library uniqueness; analysis children use stable
nonempty cohort/chunk keys. The reserved unavailable sentinel uses library_id=''
and analysis_scope_key='availability' only when there are no selected children
and the executor is unavailable; it is terminal and never reaches an executor.
Profiles and source rows should reference the immutable child ID.

Add closed authority checks for analysis rows: native manual requires admin kind,
nonempty user/session and zero key/client; compatibility requires either an Emby
user login with zero key/client or a userless application_key with positive key
ID and nonempty original client/session; schedule/startup/system_event requires
system kind and empty user/session/client/peer with key ID zero. Do not backfill
old manual rows from current sessions or synthesize an application client. The
new authority fields are decoded privately and omitted from Run JSON; no original
authentication token is persisted. Backup/recovery validation must preserve this
closed shape and clear stale execution authority through the existing lifecycle.
