// Package metadata parses Goby's initial local NFO contract.
//
// ParseNFO accepts one UTF-8 XML 1.0 document with a movie, tvshow,
// episodedetails, season, album, or artist root. This is a deliberately bounded
// local metadata format, not a claim of complete Emby or Kodi NFO compatibility.
// It never resolves paths, downloads artwork, or accesses external entities.
//
// Scalar fields are trimmed. Empty values remain absent; conflicting nonempty
// repetitions are errors. Genre, tag, and studio values are deduplicated in
// document order. Provider IDs use Imdb, Tmdb, and Tvdb for the known keys;
// unknown ASCII alphanumeric keys retain their spelling. Identical repeated IDs
// are allowed, while conflicting IDs are errors. An error returns no metadata.
//
// Titles use title, with name as a fallback for album and artist documents.
// Dates use premiered before aired and accept YYYY-MM-DD or RFC3339 with at
// most nanosecond precision, normalized to UTC. Both date fields must be valid.
// Episode and season numbers may be zero for specials; years must be 1 through
// 9999 and ratings 0 through
// 10. Missing optional numbers and dates are represented by nil pointers.
//
// Only direct children of the root supply metadata. Unknown tags and namespaced
// extensions are ignored, but still count toward all structural limits. The
// parser permits at most 2 MiB of input, depth 64 including the root, 16,384
// elements, 64 attributes per element, 64 KiB of direct text per element or
// attribute value, and 1,024 entries in each metadata collection. Provider keys
// are limited to 64 bytes and provider values to 256 safe ASCII bytes.
package metadata
