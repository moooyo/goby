# Collection user data

Playlists and BoxSet items expose personal `UserData` in item queries, item
details, collection details, and user-data notifications. Favoriting a container
changes only that account's state for the container. It does not favorite the
referenced media, and favorited media does not implicitly favorite a container.

`Played` and `UnplayedItemCount` are derived from the current, visible playable
membership. BoxSet references to albums, series, or nested collections are
traversed recursively. Each media item contributes once even when playlist
entries repeat it or multiple folders reach it. Empty containers are not played.
Container positions, play counts, and last-played dates are not invented from
member playback; the existing folder projection clears those scalar fields.

BoxSet played/unplayed mutations update the requesting account's visible
members atomically and retain the container's independent favorite. The batch
uses the catalog owner transaction so membership changes cannot commit between
the selected member snapshot and its returned aggregate. Ordinary folder and
single-item user-data writes retain their existing transaction path. Application
credentials keep their independent state-write authority; an explicit target
account still scopes subsequent catalog reads.

The pinned reference has not established playlist-wide played mutation semantics.
`SetPlayedFor` therefore rejects a Playlist target with `ErrInvalidInput` (HTTP
400), without changing any member state. Playlist played status still updates
from actual member playback and appears in `IsPlayed` queries and notifications.

Notifications traverse authorized reverse membership as well as physical
ancestors. Bulk BoxSet notifications include affected members and other visible
containers containing those members. Sharing, feature restrictions, source
library permissions, and metadata policy are reapplied before paging and counts.
These semantics have source tests; final runtime acceptance belongs to the
consolidated remote verification of the integrated source.
