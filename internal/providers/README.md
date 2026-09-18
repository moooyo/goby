# Online catalog providers

Goby includes concrete adapters for TMDB movie and television metadata and artwork,
MusicBrainz music metadata, and OpenSubtitles text subtitles. Provider access is
disabled until the management setting enables it and the corresponding private
server configuration is present. Public status responses expose readiness and
attribution, never credentials.

## Credentials and service contracts

- TMDB uses an API Read Access Token in the Authorization header. Movie and TV
  search results are resolved through detail endpoints; season and episode
  identities retain their parent series and structural numbers. Artwork selection
  is checked against a fresh provider image list before fetching a fixed CDN path.
  The application must include TMDB's approved logo and required notice in its
  credits. The developer API has separate commercial licensing terms.
- MusicBrainz requires an identifying User-Agent with a maintainer contact.
  All adapter instances share a request gate with at least one second between
  completed requests. Albums use release groups, artists use artists, and audio
  tracks use recordings. The adapter preserves typed MusicBrainz identifiers.
- OpenSubtitles uses an application API key and User-Agent, plus a username and
  password for downloads. Each explicit download authenticates before requesting
  an SRT file. Account download quotas and rate limits are returned as bounded
  errors; quota-bearing operations are not retried automatically. Temporary
  download URLs and login tokens are never returned to callers or persisted.

## Data and resource boundaries

Only HTTPS provider hosts are allowed. Each new connection resolves and rejects
private or reserved addresses, then dials the checked literal address. Redirects
cannot cross hosts. MusicBrainz redirects are disabled to preserve its request
gate. The transport ignores proxy environment variables, shares bounded connection
and request pools, and caps response headers, request durations, and body sizes.

Provider JSON is limited to 4 MiB; artwork to 20 MiB; subtitles to 8 MiB. Artwork
is fully decoded under the existing image limits before storage. Selected artwork
uses a database cache capped at 512 MiB and 10,000 entries. Maintenance removes
expired selected artwork so the catalog can fall back to local artwork. It never
deletes media or subtitle files.

Accepted provider metadata remains separate from local source facts. Existing
administrator overrides and captured locks retain precedence. A scan refreshes
the local baseline and reapplies the online source only for its matching item type.
Online refresh uses optimistic metadata revisions. Automatic matching accepts
only a unique exact title and, when available, matching year; ambiguous searches
require explicit selection.

Downloaded subtitles are parsed before publication, written through held media
root descriptors, and published without replacing existing files. Catalog,
session, policy, source identity, and filesystem observations are checked again
before registration. Compatibility subtitle selections use expiring signed tokens
bound to the user session, item, and media snapshot. Application keys cannot
redeem ordinary-user subtitle download selections.

## Primary references

- [TMDB authentication](https://developer.themoviedb.org/docs/authentication-application)
- [TMDB image paths](https://developer.themoviedb.org/docs/image-basics)
- [TMDB attribution and service terms](https://developer.themoviedb.org/docs/faq)
- [TMDB approved logos](https://www.themoviedb.org/about/logos-attribution)
- [MusicBrainz API](https://musicbrainz.org/doc/MusicBrainz_API)
- [MusicBrainz rate limiting](https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting)
- [MusicBrainz search](https://musicbrainz.org/doc/MusicBrainz_API/Search)
- [OpenSubtitles official client](https://github.com/opensubtitles/service.subtitles.opensubtitles-com/blob/master/resources/lib/osclient/provider.py)
