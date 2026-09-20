# Selected management and protocol adapters

This contract describes the selected management extension after the account,
media-processing and discovery phases. It does not add Live TV, EPG, DVR,
tuners, DLNA, external channels or group playback. Implementation and the final
Phase 4 verification receipt remain separate.

## Device credential eligibility

Native `GET /admin/v1/devices` retains `ActiveLoginCount`, but counts only the
ordinary Emby credentials in that device generation that are currently eligible
under stored account and login policy. Credentials must be unrevoked and
unexpired, their account must be enabled, and the current reported-device,
access-schedule and lockout checks must pass. Policy evaluation uses the same
`loginPolicyAllows` predicate as authentication, the actual session device ID
and one database observation time for the complete projection.

This is neither online presence nor a guarantee about a future network
connection. A registry's last IP cannot establish the trusted peer of all its
credentials. Remote access and local-authentication transport restrictions are
still checked on each real request. Native dashboard cookies and shared
application-key contexts are not ordinary-device credentials.

Page membership, total, device fields and credential policies come from one SQL
statement snapshot. Policy rows are streamed without retaining a credential
array. One projection admits at most 100,000 candidate credentials and 64 MiB of
policy input; the compatibility complete inventory admits at most 10,000
devices. An oversized stored policy is ineligible. Exceeding a projection budget
returns `422 device_projection_limit`, with no partial list or truncated count.
Narrow native pages remain independent of credentials on unselected devices.
Device deletion does not depend on this projection and can retire an oversized
generation. Count calculation does not modify credentials or device revisions.

CustomName remains the only supported per-device option. Existing native CAS,
compatibility clear/name semantics, registry generation identity and revocation
behavior are retained. Camera-upload configuration is a different, unselected
family.

## Folder-scoped media deletion

`Policy.EnableContentDeletionFromFolders` accepts existing physical library IDs
and current ordinary catalog folder IDs. The selected folder types are Folder,
Series, Season, MusicAlbum and MusicArtist, with a current root in the same
library. Scanner-generated organizational folders can have an empty path.
Library grants use the library ID, not a library-root registry ID. Files,
auxiliary/reserved resources, virtual collection containers and missing-episode
facts are not new folder grants.

Native and compatibility policy updates call the same identity validator.
New grants lock their libraries and catalog items in deterministic order and
re-read current relationships after any lock wait. A source that was deleted,
moved or reclassified while the request waited cannot be granted from an old
discovery result. EnabledFolders continues to name libraries only; its validator
is not broadened by this feature.

Unchanged legacy paths or unresolved values already saved on that account are
retained across unrelated policy edits. The editor displays them as saved
grants and permits explicit removal; it does not offer arbitrary new path
entry. Copying a policy creates grants on a new account and therefore validates
every copied scope, rejecting unresolved legacy values rather than silently
dropping or granting them.

The existing actual deletion workflow remains authoritative. It checks current
login/catalog policy and same-library ancestors, requires a single ordinary
indexed media file with no dependent media, and validates the root/source before
and after staging. A folder grant never permits recursive directory deletion or
bypasses item ACLs. Policy withdrawal while a file is staged restores the
original source instead of committing the deletion. Original sibling files are
unchanged. DeleteInfo is a current permission projection, not a reservation of
future authority.

The native picker is:

```text
GET /admin/v1/policy/deletion-folders?LibraryId=...&SearchTerm=...&StartIndex=0&Limit=100
```

It requires a current native administrator and returns `Cache-Control: no-store`.
LibraryId is optional; SearchTerm is a literal bounded name/library/ID search of
at most 256 UTF-8 bytes. StartIndex is a canonical nonnegative int32. Limit is
1 through 200 and defaults to 100. Unknown or repeated fields and request bodies
are rejected. The response is:

```json
{
  "Items": [{"Id":"folder-id","Name":"Season 1","Type":"Season","LibraryId":"library-id","LibraryName":"TV","ParentId":"series-id","Path":""}],
  "TotalRecordCount":1,
  "StartIndex":0,
  "Limit":100
}
```

The picker and its exact total share a read snapshot. A selection only changes
the administrator's account-edit draft; the saved policy still uses its existing
revision CAS and strict write parser. Displaying a folder never grants it.

## Supported policy choices

New BlockUnratedItems choices are Movie, Trailer, Series, Music and Other.
Previously saved SDK categories for games, books, live television and external
channels remain readable and can be retained or removed, but are not presented
as new editable capabilities. Unrelated saves preserve them. A new explicit
inactive category, including through a copied policy, returns a field validation
error. Reading historical runtime policy remains compatible; this write rule
does not create the excluded content families.

## Library options and native creation

The two supported native options remain `EnableLocalMetadata` and
`EnableLocalImages`, both default true. Native creation now exposes and sends
both values through its already implemented LibraryOptions input. The artwork
switch covers local directory images and admitted embedded audio covers.
Disabling an importer retains previously accepted source facts and manual
overrides; later scans use the saved switches. These settings do not require a
process restart.

Existing native partial editing and revision CAS remain unchanged. Omitted
native options preserve existing values; explicit true restores each supported
default. Compatibility `DisabledLocalMetadataReaders: []` enables Nfo, while
`["Nfo"]` disables it. The compatibility Nfo update never resets the native-only
image option. Unsupported library properties/readers are rejected.

Authenticated `GET /Libraries/AvailableOptions` returns only the implemented
reader and actual defaults:

```json
{
  "MetadataSavers":[],
  "MetadataReaders":[{"Name":"Nfo","DefaultEnabled":true,"Features":[]}],
  "SubtitleFetchers":[],
  "LyricsFetchers":[],
  "TypeOptions":[],
  "DefaultLibraryOptions":{"DisabledLocalMetadataReaders":[]}
}
```

This safe read is available to authenticated users and complete application
credentials, contains no deployment paths or secrets, and is not cached. Only
authentication transport query parameters are accepted. It does not advertise
unselected providers, lyrics, metadata savers, plugin URLs, custom image-fetcher
names or every option in the SDK. Library mutations still require their existing
current administrator authority.

Creation commits before optional scan admission. The native response separately
reports a scan-admission error; the compatibility create retains its empty 204
success after a committed create. Removing a library removes catalog data and
preserves files on disk. Existing scan/publication/deletion busy guards remain.

## Literal aliases and library removal refresh

The `/emby` prefix remains optional, and only declared route literals are
case-insensitive. The selected additions are Playlists/{Id}/AddToPlaylistInfo,
System/ActivityLog/Entries and System/Logs Query/download/Lines operations,
plus Libraries/AvailableOptions. Opaque IDs and log names retain case and escaped
segment boundaries. Existing authentication, native/compatibility error formats,
response envelopes and the observability HEAD 404 contract remain unchanged.
There is no new independent SystemActivity endpoint or legacy log envelope.

A committed library-root deletion has no surviving parent to include in a
LibraryChanged payload. The server checks the trusted root-removal scope against
the receiver's current library authority. An allowed receiver is disconnected
through the existing per-socket resynchronization path without any historical
ID payload. Other receivers stay connected; ordinary events filtered to no
visible items remain silent. This does not invoke a global disconnect or add an
unrecognized upstream resync message.

The consumer reconnects, establishes fresh connection-local subscriptions and
refreshes its views over the authorized HTTP API. Old queued messages are not
replayed. A revoked credential cannot reconnect. Existing ordered bounded
event queues, slow-consumer closure and remote-command delivery checks remain
in effect. Remote command HTTP 204 is admission rather than a player execution
acknowledgement; only the receiving client's actual action and subsequent valid
playback report change playback state.
