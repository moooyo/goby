# Catalog item capabilities

Item detail responses and `Fields=CanDelete,CanDownload` project the current
authenticated actor's capabilities. `UserId` can select another user's catalog
state when authorized, but cannot grant that user's deletion or download rights.
The shared final `ExcludeFields` projection can remove either capability.

Capabilities describe current credential authority and retained catalog
eligibility. They do not reserve a job, open storage, probe media, establish that
a file is currently present, or guarantee that a later operation will succeed.
Download and deletion revalidate their own authorization and live source state.

- `CanDownload` requires a visible original local Movie, Episode, Video or Audio
  source with current probe/source facts and a valid indexed root/path mapping.
  Download permission and the downloads feature gate are independent of playback
  permission. A pending deletion or prepared/committed publication disables it.
- Physical `CanDelete` additionally requires ordinary media without child items,
  owned themes or extras, current global or ancestor-scoped deletion permission,
  a valid persisted root binding, and no conflicting scan/publication/deletion.
  Being an administrator alone does not grant physical deletion permission.
- Playlist and BoxSet deletion follows the actual collection contract: current
  owner or administrator, an enabled collection feature, and an unlocked
  collection. An editing share alone does not grant collection deletion.
- Application keys retain their independent server authority. Disabled account
  or restricted account state cannot substitute for an application credential.
- Folders, entity-only IDs and expected missing episodes do not receive physical
  download/deletion capabilities. Unknown or currently hidden IDs are false.

The implementation splits already bounded response populations into batches of
at most 1,000 distinct IDs. Each batch checks the live credential and current
policy in a repeatable-read database snapshot, reads those IDs in one query,
and reads their distinct root bindings in a second query. Stored binding data
has an 8 MiB parsing budget per batch. No per-item SQL calls or filesystem work occur.
Implementation and acceptance are separate: the current phase's consolidated
verification record owns executed results.
