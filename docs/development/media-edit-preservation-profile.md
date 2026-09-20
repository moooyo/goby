# Embedded subtitle removal preservation profile

`media.RemuxSubtitleRemoval` prepares an unpublished candidate by removing one
absolute embedded subtitle stream index. It takes an already authorized source
descriptor and an independent, empty, read/write candidate descriptor. It does
not resolve request paths, modify the source, update the catalog, or publish the
candidate. The library owns root binding, authorization, durable operation state,
publication, explicit recovery, and the final source and candidate identity checks.

## Admission and preservation

The supported editing profile is narrower than the playback profile. An edit is
accepted only when the implementation can prove all retained semantics:

- MKV/MKA must contain a single Matroska segment, ordinary tracks, plain text
  metadata, optional flat chapters, and bounded attachments. The structural
  scanner examines each container and block header without loading media packet
  payloads into memory. Unsupported elements, duplicate metadata projections,
  linked segments, ordered or nested chapters, track content encodings,
  encryption, invisible/discardable blocks, and unrepresented block additions
  are rejected.
- MP4 is limited to ordinary, self-contained `avc1`, `mp4a`, and `tx3g` tracks.
  Fragmentation, encryption, external references, alternate sample descriptions,
  private atoms, unknown sample groups, chapter references, and nontrivial edit
  lists are rejected. AAC requires an explicit `roll` sample-group pair declaring
  exactly one preceding access unit for every sample. Its description and mapping
  are read directly from `sgpd`/`sbgp`; their normalized meaning must match after
  remuxing and their sample counts must equal the bound stream's actual packet
  counts. Other recovery distances, `prol` or unknown group types, and missing AAC
  preroll declarations remain outside the profile. Sample counts and duration
  declarations must describe complete
  sample-table timelines. Typical AAC files with priming edits may be outside
  this profile; their clocks are never silently shifted to make an edit pass.
- DOVI/HDR10+ Matroska files that use advanced block-addition mappings are outside
  this editing profile. Ordinary supported HDR stream metadata is compared
  completely. Playback support for a source does not imply edit support.

FFmpeg maps every stream except the selected absolute subtitle index and copies
all retained codecs. Chapters and metadata are explicitly mapped. Every output
stream disposition is set explicitly so FFmpeg cannot promote another stream to
default merely because the selected subtitle was removed.

An independent ffprobe pass hashes every retained packet payload in stream order,
including packet boundaries, sizes, and counts. A separate hash covers exact
rational PTS, DTS, duration, flags, and complete supported packet side data. There
is no timestamp rounding allowance. Currently, only the fully rendered `Skip
Samples` packet side-data structure is admitted; opaque or unknown side data is
rejected. Codec extradata hashes prove retained codec headers and attachment
bytes, including attachments that produce no packets.

All retained user tags, stream tags, dispositions, rendered stream facts, and
chapter metadata must match. Container-local track and chapter IDs, indexes,
padding, size/bitrate statistics, and demuxer-derived clocks may be regenerated;
the independent packet and chapter clocks still must match exactly. Only global
`encoder` and Matroska `MuxingApp`/`WritingApp` provenance may change. Every such
change appears as an explicit before/after record in `WriterChanges`. Creation
time, title, comment, and per-stream encoder tags have no such exception.

Matroska chapter display title, language, display presence, and exact nanosecond
interval are also compared directly from the container. An absent `ChapLanguage`
means `eng`; it is not equivalent to `und`. FFmpeg emits `und` when it writes
chapters, while ffprobe does not expose this language. Therefore `eng` or `fra`
must never silently become `und`, even when their ffprobe chapter titles match.
The direct proof accepts ordinary three-letter languages and rejects changes.
BCP47 overrides, country fields, and multiple chapter displays remain outside
this profile. These behaviors follow the [Matroska element definition](https://github.com/ietf-wg-cellar/matroska-specification/blob/master/ebml_matroska.xml)
and [FFmpeg's chapter writer](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/matroskaenc.c).

EBML ASCII/UTF-8 values may include zero-byte padding after their text. The
scanner removes only an entirely zero-filled suffix, validates the complete
remaining value with its declared encoding, and rejects a nonzero hidden tail
or interrupted UTF-8 character. This preserves the string semantics defined by
[RFC 8794 section 13](https://www.rfc-editor.org/rfc/rfc8794.html#section-13).
It also admits FFmpeg's ordinary `DURATION` tag: its writer reserves a fixed
19-byte string payload and can write zero padding after the formatted duration.
See [`DURATION_STRING_LENGTH` and the duration writer](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/matroskaenc.c).

FFmpeg reconstructs AAC's all-sample, one-access-unit `roll` mapping rather than
copying arbitrary original sample-group boxes. Its demuxer does not expose those
groups through ffprobe. The source/candidate comparison therefore uses direct
structural evidence instead of treating matching packet bytes as sufficient.
See the [pinned FFmpeg preroll writer](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/movenc.c#L3305)
and [sample-group reader](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/mov.c#L3922).

Track-level MP4 `trak/udta` has a separate admission rule from movie metadata.
Only one nonempty UTF-8 `name` (up to 4 KiB, without hidden NUL bytes) and the
complete standard `kind` pairs are accepted. `kind` requires version/flags zero,
exactly two terminated strings, the exact `urn:mpeg:dash:role:2011` scheme, and
one of `caption`, `commentary`, `description`, `dub`, or `forced-subtitle`.
Unknown children, scheme/value prefixes with extra text, duplicates, ambiguous
strings, and trailing data are rejected. The raw name presence/value and sorted
kind pairs are compared for each retained track and included in `ContainerSHA256`.

The raw name must match its complete ffprobe `tags.name` projection. FFmpeg
reads that atom as `name` but writes it from `title`, so the remux command
explicitly supplies the proven value as the output stream's `title` alias.
The existing complete stream-tag comparison remains unchanged. Each kind also
must match all of its disposition bits, including both hearing-impaired/captions
and both visual-impaired/descriptions bits. This avoids trusting the demuxer's
prefix matching or losing track names during a metadata-only copy. See the
[track metadata writer](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/movenc.c#L4298),
[string reader](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/mov.c#L490),
and [complete disposition mapping](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/isom.c#L450).

Source SHA-256, candidate SHA-256, candidate byte length, a canonical metadata
digest, a `ContainerSHA256` digest of retained structural semantics, retained stream
mapping, packet digests, and writer changes are returned as
`SubtitleRemovalEvidence`. The candidate is hashed twice around verification.
Source and candidate identities, lengths, modification times, and Linux change
times are rechecked. This evidence authorizes no publication on its own.

## Resource and execution limits

The operation requires Linux and a root-owned, non-group/world-writable
`/usr/bin/prlimit` with protected ancestor directories. Missing or unsafe resource
limiting fails closed. Configured FFmpeg/ffprobe executables run with the existing
isolated media environment, restricted local protocols/formats, inherited
descriptors, and process-group cancellation. No shell command is constructed.

- Maximum source size: 1 TiB; maximum total operation time: two hours, including
  slot admission, complete packet scans, and file hashing.
- Two concurrent operations; each child has a 4 GiB address-space limit and a
  64-descriptor limit. Individual FFmpeg allocations are also bounded.
- The candidate has an exact `RLIMIT_FSIZE` ceiling, defaulting to source bytes
  plus 64 MiB. This also protects sparse writes and seekable MP4 trailer rewrites.
  Probe processes have a zero-byte regular-file write ceiling.
- At most 256 streams, 10,000 chapters, two million container headers, 64 MiB of
  container metadata, and 256 MiB of attachment payloads are admitted.
- ffprobe metadata output is limited to 8 MiB. Packet JSON is streamed with a
  64 KiB per-record limit, a 32 GiB total limit, and a 100-million-packet limit.
  Each reported packet payload is limited to 256 MiB. Complete media packet lists
  are never retained in memory.

Unknown preservation cases and exceeded budgets produce failed, unpublished
operations. The caller retains responsibility for cleaning up failed candidates.

## Verification status

The first consolidated Linux media verification run reached all three actual
subtitle-removal profiles and failed during structural admission: MKV/MKA on
the unexposed chapter display language, and MP4 on AAC's standard `sgpd` preroll
group. Direct chapter-language and AAC sample-group preservation proofs were
added in response. A second targeted run passed 66 parent tests but again failed
the three actual-remux profiles at admission: MKV/MKA rejected legal EBML text
padding, and MP4 rejected track-level `udta`. Both failed runs are retained.
The corresponding text-padding and exact track-userdata proofs are now added;
targeted re-verification is pending. These repairs have not yet established a
passing actual remux result.

Unit coverage includes exact rational clocks, payload tampering, reordered and
missing packets, duplicate JSON keys, malformed metadata, chapter and attachment
changes, absolute stream mapping, descriptor offsets, hard file-size limits,
environment isolation, and descendant cancellation. Structural tests exercise
supported synthetic containers and rejected hidden metadata and timeline cases.

`TestSubtitleRemovalActualFFmpegPreservesSelectedProfiles` uses
`GOBY_FFMPEG`/`GOBY_FFPROBE` for actual MKV, MKA, and selected MP4 candidates. MKV/MKA
fixtures include two audio tracks, two subtitle tracks, chapters, metadata, and
an opaque attachment with a font MIME type. The MP4 fixture explicitly disables
edit lists to stay within the selected profile. It verifies complete returned
evidence and independently probes the resulting stream/chapter inventory. These
tests must run in the designated Linux verification environment; their presence
alone is not evidence that actual remuxing has passed.

The optional `GOBY_MEDIA_EDIT_EVIDENCE_DIR` setting retains actual-test evidence
in a unique private run directory (0700), with source/candidate copies and a
manifest (0600). Cleanup records the bytes present at the current test stage,
their hashes, and the result, including failures. This diagnostic capture does
not run another probe or change the fixtures. It permits the next repair to
inspect the exact failed inputs and candidate instead of discarding them.
