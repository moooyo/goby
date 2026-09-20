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
  lists are rejected. Explicit 32-bit or 64-bit track-box sizes are required.
  AAC requires an explicit `roll` sample-group pair declaring
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

For MKV/MKA, FFmpeg maps every stream except the selected absolute subtitle index
and copies all retained codecs. Chapters and metadata are explicitly mapped.
Every output stream disposition is set explicitly and Matroska's default mode
is fixed to `passthrough`.

MP4 uses `Engine=structural_edit`, without invoking the FFmpeg muxer. A bounded
descriptor copy changes only the selected `tx3g` track's four-byte `trak` type
to `free` and fills that track body with zeroes. The original size fields, every
other box, all sample tables, all offsets, and every `mdat` byte remain identical.
The selected track ID is bound to the requested absolute ffprobe stream index.
The original `mvhd` duration is preserved; the request is rejected if removal
would invalidate its existing complete-timeline invariant. Track references,
fragmentation, external data references, and unknown container semantics remain
rejected by admission.

This removes a track declaration, rather than securely erasing its former media
payload. Unreferenced `mdat` bytes remain and the candidate has the same size as
the source. Scratch space must cover that complete size, checked before media
scanning or hashing. The output is still a separate unpublished file. An
independent descriptor reread proves the three unchanged byte ranges and the
exact `free`/zero-filled replacement; the normalized preserved-range digest is
returned as `PreservedBytesSHA256`. The same exact-size, range-digest and cleared-
body checks run again inside the final candidate proof baseline, binding that
digest to `CandidateSHA256` even if unreferenced bytes change after copying.
All existing stream, metadata, packet,
extradata, and structural proofs still run after this edit.

An independent ffprobe pass hashes every retained packet payload in stream order,
including packet boundaries, sizes, and counts. A separate hash covers exact
rational PTS, DTS, duration, flags, and complete supported packet side data. There
is no timestamp rounding allowance. Currently, only the fully rendered `Skip
Samples` packet side-data structure is admitted; opaque or unknown side data is
rejected. Codec extradata hashes prove retained codec headers and attachment
bytes, including attachments that produce no packets.

An opaque Matroska attachment may legitimately have no recognized codec name.
It is admitted only with explicit attachment type, positive bounded extradata,
a complete SHA-256 hash, filename, and valid MIME type. Each retained attachment
still has independent index, metadata, length, and full-content proofs; ordinary
unknown audio, video, subtitle, or data streams remain rejected. FFmpeg's
[Matroska reader](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/matroskadec.c)
copies unrecognized-MIME attachment bytes into extradata, and its
[attachment writer](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/matroskaenc.c)
writes that complete payload back. In particular, `font/ttf` need not result in
a recognized `codec_name` to retain the attachment exactly.

Nonzero Matroska `CodecDelay` has an AAC-only admission path. It requires an
audio TrackEntry with exact `A_AAC` CodecID, complete nonempty CodecPrivate,
known integral base/output sampling frequencies and channel count, and zero
`SeekPreRoll`. The raw delay remains an integer number of nanoseconds. With
effective sample rate `F`, the implementation computes
`S = roundNearInf(CodecDelay * F / 1,000,000,000)` using bounded integer
arithmetic and requires the reverse rescale to reproduce the original
nanoseconds exactly. `S` must fit a nonnegative signed 32-bit initial-padding
count, and a nonzero delay must produce positive `S`. The ffprobe
`initial_padding` must equal it. Only absent and explicitly zero delay are
semantically equivalent; one-nanosecond differences are not normalized away.

Matroska ffprobe IDs do not identify raw TrackNumber or TrackUID. For a delayed
AAC source/candidate, the complete raw TrackEntry encounter order is bound to
contiguous probe indexes, matching each known codec/type, CodecPrivate length
and SHA-256, and audio sampling/channel facts. The complete attachment count
must occupy the remaining probe inventory, with image attachments explicitly
identified as attached pictures. Unsupported or transformed CodecPrivate
projections are rejected rather than guessed. Track numbers/UIDs may be
regenerated, but the selected subtitle and every retained stream still follow
the verified source-to-candidate mapping.

Both sides must preserve the exact AAC delay, base/output sample rates, channel
count, and CodecPrivate. That proof is included in `ContainerSHA256`. The
existing per-packet payload, rational timestamp, duration, flags, and complete
`Skip Samples` comparison is unchanged. Nonzero delay for other codecs,
including Opus, and all nonzero `SeekPreRoll` remain outside this profile.
This follows the pinned FFmpeg [audio delay reader](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/matroskadec.c#L2879)
and [delay writer](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/matroskaenc.c#L2090),
which separately rescale padding samples and presentation timestamps.

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

The stream `DURATION` tag remains an exact metadata requirement. FFmpeg's
pinned [trailer writer](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/matroskaenc.c#L3452)
regenerates it from the largest packet end. With a fractional-millisecond video
`DefaultDuration`, that can reduce an authored duration by one millisecond even
when all retained packets are identical. The unpublished candidate has one
narrow restoration path for this known case: both timestamp scales must be
exactly one millisecond; the retained video must bind its raw TrackUID through
the complete raw/probe track inventory, codec, and codec-private proof; and its
nonzero fractional-millisecond `DefaultDuration` must match exactly. Both raw
tag values must match their probe projections, use canonical
`HH:MM:SS.fffffffff` text with two-digit hours in equal 19-byte payloads, and
the candidate must be exactly one millisecond shorter. The implementation
copies the source's exact text and zero padding into that same candidate
extent. Missing tags, changes on audio or subtitles, and other differences
are rejected. Files whose tags already match do not enter this narrower
codec-private binding path.

Before mutation, the implementation verifies every affected ancestor CRC in
both containers. It requires the CRC to be the first child and uses the IEEE
CRC-32 over the complete parent payload except the CRC element itself. It
updates affected candidate checksums from inner to outer and verifies them
again, following [RFC 8794 sections 11.3.1 and 14](https://www.rfc-editor.org/rfc/rfc8794.html#section-14).
Root-level CRCs without a parent element are outside this restoration profile.
CRC coverage is deduplicated, limited to 4,096 scopes, and read in 64 KiB
chunks under the same operation deadline. A complete complement-range digest
proves that only the tag payloads and the affected four-byte CRC values changed.
The helper's validated file snapshot and complete SHA-256 remain the final
proof baseline, so padding changes or appended Void bytes after restoration
cannot be adopted by a later Stat. Container scanning, metadata probing, and
all existing packet/metadata checks run again against that baseline; no
packet timing or metadata comparison is relaxed. `RestoredDurationTags` and
`DurationPreservedBytesSHA256` record this operation in the returned evidence.

FFmpeg reconstructs AAC's all-sample, one-access-unit `roll` mapping rather than
copying arbitrary original sample-group boxes. Its demuxer does not expose those
groups through ffprobe. The MP4 structural path preserves the original group
boxes and independently compares their decoded semantics, instead of treating
matching packet bytes as sufficient.
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

The raw name must match its complete ffprobe `tags.name` projection. FFmpeg reads
that atom as `name` but writes it from `title`; the structural editing path
avoids that lossy metadata-copy boundary by preserving the original atom bytes.
The complete stream-tag comparison remains unchanged. Each kind also
must match all of its disposition bits, including both hearing-impaired/captions
and both visual-impaired/descriptions bits. This avoids trusting the demuxer's
prefix matching or losing track names during a metadata-only copy. See the
[track metadata writer](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/movenc.c#L4298),
[string reader](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/mov.c#L490),
and [complete disposition mapping](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavformat/isom.c#L450).

The byte-preserving MP4 path also keeps the original `ftyp` and explicit global
`mdta` namespaces intact. It does not re-copy the flattened ffprobe metadata
projection: doing so duplicates derived brand tags, and a semicolon-combined
creation-time projection can no longer reconstruct the original header time.
The implementation never splits arbitrary user metadata on semicolons, removes
explicit `mdta` values, or exempts creation time from preservation.

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
- The Matroska candidate has an exact `RLIMIT_FSIZE` ceiling, defaulting to source
  bytes plus 64 MiB. The MP4 structural copy rejects a smaller-than-source budget
  before scanning and uses bounded `WriteAt` calls that never extend beyond the
  source extent. Probe processes have a zero-byte regular-file write ceiling.
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
The corresponding text-padding and exact track-userdata proofs were added. The
third run progressed to a Matroska opaque-attachment admission issue and MP4
metadata comparison. Its retained source/candidate bytes were then inspected
with one authorized read-only diagnostic pass. The MP4 writer had duplicated
derived brand metadata, lost track creation times, promoted the remaining
subtitle to default, appended 20 bytes of subtitle extradata, and added a fourth
subtitle packet. The original first three subtitle packets and all retained A/V
packets were identical. These differences are not accepted as normalization.

The admitted MP4 profile now uses structural editing to preserve those original
bytes and semantics. Matroska opaque attachments retain their independent full
extradata proof. Commit `a3b713d` subsequently passed the consolidated media
scope: 79 parent tests without skips, including all three original profiles and
their complete A/V decode. The 11 library `TestMediaEdit` parent tests also
passed. That result establishes the earlier profile baseline, not the later
native AAC regression.

Native run `selected-phase2-680eeef-browser04` then recorded an explicit source
admission failure for Matroska element `0x56AA`. Its preserved 12-second source
has `CodecDelay=21333333` ns; the authorized read-only diagnostic bound its AAC
track, 48 kHz rate, five codec-private bytes, probe `initial_padding=1024`, and
the first packet's complete `Skip Samples` record. No candidate was produced.
The AAC-only delay proof was then included in the `b2ab29b` consolidated run:
91 parent media tests passed without skips, while the exact native-parameter
regression failed on the retained video `DURATION` metadata. One authorized
read-only diagnostic of its retained source and candidate found the authored
12-second tag had become 11.999 seconds. Every retained packet was identical
apart from physical positions and the expected subtitle index mapping: 288
video packets, 563 AAC packets, and one retained subtitle packet. AAC delay,
Skip Samples, chapters, raw DefaultDuration, and all other semantic stream
metadata also matched. The duration restoration above addresses that observed
loss; it and its new unit cases await the next consolidated verification.

Unit coverage includes exact rational clocks, payload tampering, reordered and
missing packets, duplicate JSON keys, malformed metadata, chapter and attachment
changes, absolute stream mapping, descriptor offsets, hard file-size limits,
environment isolation, and descendant cancellation. Structural tests exercise
supported synthetic containers and rejected hidden metadata and timeline cases.

`TestSubtitleRemovalActualMediaPreservesSelectedProfiles` uses
`GOBY_FFMPEG`/`GOBY_FFPROBE` for actual MKV, MKA, and selected MP4 candidates. MKV/MKA
fixtures include two audio tracks, two subtitle tracks, chapters, metadata, and
an opaque attachment with a font MIME type. The MP4 fixture explicitly disables
edit lists to stay within the selected profile. It verifies complete returned
evidence and independently probes the resulting stream/chapter inventory. It
then performs one complete bounded FFmpeg decode of every retained A/V stream
with explicit maps, `-xerror`, and `-err_detect explode`, with no time/frame/seek
truncation. A clean exit, final `progress=end`, positive output time, and positive
video-frame count when video exists are required. The fixed two-second fixtures
additionally require 50 video frames when present and output time within
1.9–2.1 seconds, allowing bounded AAC padding. The three earlier failed runs
used the historical name `TestSubtitleRemovalActualFFmpegPreservesSelectedProfiles`.
These tests must run in the designated Linux verification environment; their presence
alone is not evidence that actual remuxing has passed.

The optional `GOBY_MEDIA_EDIT_EVIDENCE_DIR` setting retains actual-test evidence
in a unique private run directory (0700), with source/candidate copies and a
manifest (0600). Cleanup records the bytes present at the current test stage,
their hashes, the engine/preservation result, probe facts, and the one complete
decode result, including failures. This diagnostic capture does not itself run
another probe or decode or change the fixtures. It permits the next repair to
inspect the exact failed inputs and candidate instead of discarding them.

The later native04 browser execution identified a specific additional admission
failure: its Matroska AAC track carried `CodecDelay=21,333,333` nanoseconds, while
the prior profile required zero. Authorized read-only diagnostics of the
retained source bound that raw track to probe index 1, 48 kHz mono AAC,
`initial_padding=1024`, five matching CodecPrivate bytes, and an initial
`Skip Samples` record of 1024 samples. The source identity and bytes were
unchanged by that diagnostic pass. The AAC-only extension above addresses this
known precondition without weakening packet preservation.

`TestSubtitleRemovalActualMatroskaAACCodecDelay24FPS` adds the same twelve-second
720x576, 24 fps H.264/AAC source with both authored subtitles and chapters. It
checks the raw and probe delay bindings before removal and preserves the full
packet/metadata/structural chain plus complete A/V decoding afterward. The
regression exposed the duration loss documented above. Its source assertion
continues to require the authored 12-second video tag, and its final metadata
and complete-packet assertions remain unchanged. The restoration is pending
consolidated verification; no passing media or browser outcome is inferred
from diagnostic evidence or static review.
