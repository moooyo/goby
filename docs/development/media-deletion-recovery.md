# Recoverable file deletion

`GET /Items/{Id}/Download` and `GET /Items/{Id}/File` apply current catalog
visibility and `EnableContentDownloading`, independently of playback permission.
Long downloads revalidate credentials, policy, and source identity; a failed
revalidation closes the response. The routes accept no filesystem selector.

`DELETE /Items/{Id}` and `POST /Items/{Id}/Delete` delete one ordinary, indexed
media file. Collections dispatch to their own record-only removal workflow.
Physical folders and media with dependent child media are explicitly rejected;
there is no recursive filesystem deletion. `DeleteInfo` lists the selected
source path after applying the same permission and file-only contract.

Media deletion requires `EnableContentDeletion` or a matching ancestor/library
in `EnableContentDeletionFromFolders`, plus full current item visibility.
Subtitle deletion requires `EnableSubtitleManagement`; embedded streams remain
read-only. Complete application principals use their independent authority.

The physical deletion profile requires Linux, a stable `/etc/machine-id`, and a
persistently approved root topology. Unbound or unsupported storage is rejected.
The root mapping, binding revision, approved fingerprint, current named
directories, source inode, size, modification time, and change time are checked.

The durable `media_deletion_operations` record has two phases:

1. `prepared` records the exact source and a server-generated private quarantine
   name. Filesystem work occurs outside database transactions. A no-replace
   rename moves the directory entry into that private directory. The moved
   entry must match the already opened source before catalog removal can commit.
2. `catalog_removed` commits catalog removal, audit, notification facts, and the
   observed quarantine identity together. Only a newly opened capture using
   these persisted facts can unlink the quarantined regular file. The original
   pathname is never unlinked by name, and a replacement file is never removed.

Policy and credential changes that commit before catalog removal stop that
phase. The prepared move is restored without overwriting any new original-path
entry. Restoration changes inode ctime, so a rescan is required before subsequent
source access or another deletion. Empty quarantine directories are retained;
the scanner ignores their reserved dot-prefixed names.

Any uncertain database commit outcome retains the journal and quarantined file.
A request never reports successful physical deletion until purge and journal
cleanup finish. Pending records prevent new scans and root-binding changes for
the affected source, and prevent removal of the required root/library mapping.

Recovery is explicit: repeat the same deletion request with a current authorized
session. A prepared operation restores the source and reports a source-change
conflict requiring a rescan. A committed operation completes its verified purge.
If a prepared stage is independently proven empty, recovery clears only the
journal; it does not alter the current source. Conflicting or unverifiable
staged data remains intact and returns `media_deletion_pending` for operator
inspection. Original paths are never overwritten to make recovery succeed.

Backups preserve pending journals. There is no startup or restore-time automatic
deletion. An explicit retry must match the originating machine identity, current
root mapping and binding approval, and the recorded inode/staging facts. Resolve
pending operations on their original storage host before migrating a backup.

This implementation and its source tests have not yet been executed in the
designated verification environment during the current code-only phase.
