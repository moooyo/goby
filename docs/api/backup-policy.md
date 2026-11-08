# Automatic backup policy

Automatic backups use Goby's existing durable task scheduler and encrypted
backup engine. This is a Goby management contract, not an Emby archive format.

## Administrator API

`GET /admin/v1/backups/policy` returns the saved policy, the exact decimal
revision of its `backup.create` task definition, next occurrence, last automatic
result, and whether an encrypted passphrase is present. It never returns the
passphrase, ciphertext, deployment paths, database credentials, or vault key.

`PATCH /admin/v1/backups/policy` requires administrator-cookie authentication,
same-origin CSRF protection, the observed `Revision`, and these settings:

```json
{
  "Revision": "1",
  "KeepCount": 7,
  "IncludeImages": true,
  "Passphrase": "an independently saved encryption passphrase"
}
```

`Passphrase` is optional. The other three fields are required. The endpoint
rejects scheduling fields, unknown fields, duplicate fields, null values, and
lossy revisions. It returns the same policy projection as GET. A successful
change advances the shared task revision without touching task triggers,
interval anchors, next occurrences, the schedule timezone, or the preferred
legacy daily/weekly schedule. A stale task editor or policy editor must reload
after a revision conflict. The change records `settings.updated` for
`backup_policy`; it does not claim that a task schedule changed.

The Tasks trigger editor is the scheduling authority. Its complete rule-set
replacement accepts daily, weekly, interval, startup, and supported system-event
rules. Any rule can trigger a run, and overlapping work joins the active task.
An empty rule set leaves manual execution available when its prerequisites are
configured. Adding automatic backup rules requires an available backup executor,
a saved encryption passphrase, and a retention count that leaves a free backup
object slot. Removing all rules remains possible when the executor is
unavailable. The credential check and settings changes share the task-definition
lock, so concurrent trigger creation and credential clearing cannot commit an
automatic schedule without its required secret.

The earlier `PUT /admin/v1/backups/policy` contract remains available for clients
that explicitly replace a policy's daily/weekly schedule. It requires the same
authentication, CSRF protection, the observed `Revision`, and these fields:

```json
{
  "Revision": "1",
  "Enabled": true,
  "Schedule": {
    "Kind": "daily",
    "DayOfWeek": null,
    "TimeOfDay": "03:00",
    "Timezone": "Asia/Shanghai"
  },
  "KeepCount": 7,
  "IncludeImages": true,
  "Passphrase": "an independently saved encryption passphrase"
}
```

Weekly schedules use `Kind: "weekly"` and a `DayOfWeek` from zero (Sunday) to
six (Saturday). Times have minute precision and use an explicit IANA zone.
Disabling a policy retires its active task triggers but retains the preferred
time and content settings. A custom rule set edited on Tasks is reported as
`custom`; saving a daily or weekly policy explicitly replaces that whole set.

Omitting `Passphrase` preserves the saved secret. An empty string clears it and
is accepted by PATCH only after all active automatic backup triggers have been
removed on Tasks. A rejected clear leaves the rules and credential unchanged;
it never disables the schedule implicitly. Legacy PUT can explicitly disable
its schedule and clear the credential together. A replacement keeps
its exact UTF-8 bytes and must contain 12 through 1,024 bytes. The secret is
encrypted with the existing credential vault using a separate purpose and
generation binding. Administrators must separately retain the passphrase to
restore an archive. Unknown mutation results require a fresh GET before another
save; the interface never automatically replays a secret update.

## Execution and retention

The registered global `backup.create` executor receives the task manager's
process-local `Work.Fence` capability. It never synthesizes an administrator
token or restores a human session to run a schedule. Each task run binds one
generated backup id in `backup_policy_runs`. Repeating a completed task request
does not create another archive. Startup reconciles exact already-published
objects; interrupted encryption is never recreated by guessing its result.

`backup.finished` retains its existing role as the durable authorization to
publish prepared bytes. Successful-completion notifications instead consume
`backup.published`, recorded only after the store reports the exact backup ready
with the expected SHA256 and length. A permanent publication receipt is committed
in the same transaction as that activity. Recovery retries, uncertain COMMIT
results, and display-log retention cannot create another notification source for
the same backup id. No publication receipt is created for a merely prepared or
failed object.

Retention is applied after a successful automatic backup. It selects only
completed automatic rows owned by the current private deployment. Before any
deletion, the backup store must report a generated, ready, verified object with
the exact recorded id and SHA256. A durable deletion receipt precedes removal.
Manual, imported, failed, and unrelated objects are excluded. Active readers
defer retention instead of being interrupted. KeepCount is between 1 and 30 and
must leave a free object slot under the deployment's configured object limit;
byte quotas and other backups can still prevent a new creation.

Changing a policy applies to subsequent task executions. An already admitted
execution retains its captured content and retention values. Task cancellation,
generation shutdown, and database ownership loss cancel its work. Restoring an
archive retires all automatic backup triggers and marks imported run receipts
as restored, preventing them from authorizing deletions in the target deployment.

## Image selection

Manual `POST /admin/v1/backups` requests may also supply the optional boolean
`IncludeImages`. Omission defaults to true for compatibility. Null and values
other than booleans are rejected. The request keeps its independent manual
passphrase and never enters automatic retention. The dashboard can explicitly
send the saved policy's image choice when starting a manual backup. Omission and
explicit true share the existing full-image request fingerprint; false has a
different fingerprint, so reusing a RequestId for different contents conflicts,
including after a restart. No archive-format or producer-version change is
needed for image selection.

The default includes uploaded and downloaded library artwork, automatically
fetched images, cached embedded covers, avatars, and manual image override state.
`IncludeImages: false` actually excludes PostgreSQL data for `artwork_images`,
`artwork_state`, `item_provider_images`, and `item_embedded_artwork` from the same
exported snapshot; it does not change the source database. The selected tables'
archived facts are exact zero-row fingerprints. The restore decoder permits
missing COPY entries only for these compiled table names when every applicable
artwork table in the trusted schema prefix attests to empty data. All other
table, sequence, constraint, fingerprint, and application-state checks remain
required. Restored items can use source-folder images when available; downloaded
or extracted images may need to be rebuilt, and managed user avatars are absent.

Subtitle review text and its `media_operation_cues.image_png` evidence remain
included. This setting selects library artwork and avatars; it does not discard
all binary values or remove unrelated review and publication history.

Media files and source-folder image files are never copied into either form of
backup. Their existing file references and all ordinary metadata remain included.
This option does not promise a full filesystem or media-library backup.
