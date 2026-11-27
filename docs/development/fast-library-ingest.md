# Fast library ingest

`GOBY_FAST_LIBRARY_INGEST=true` opts ordinary movie and TV scans into fast
catalog publication. It is disabled by default. Music, mixed libraries,
auxiliary theme and extra media, and the explicit **Refresh media details** task
use their existing probe policy. This feature does not change how a TV layout
identifies series and seasons.

1. The scan accepts the rooted media source, technical playback facts, and
   catalog hierarchy, then publishes each item on its existing transaction
   boundary. It retains the existing subtitle registration path. Normal
   missing-media reconciliation still requires complete rooted observations
   and a fresh proof of absence. Deferred descriptions are not proof of file
   presence or absence.
2. **Import local metadata** reads NFO and local images in a new, fenced task
   after the scan completes. Pending NFO is not an accepted rating or tag;
   viewers with applicable rating or tag restrictions wait for that proof.
   Invalid preferred NFOs and unstable sources retain previous facts and remain
   pending. A verified absence clears the local layer, not manual overrides.
3. **Enrich newly scanned metadata** can fill missing TMDB matches after local
   work. Set `GOBY_FAST_LIBRARY_ONLINE_METADATA=true` as a separate opt-in; the
   deployment provider gate, managed internet-provider setting, and TMDB
   credentials must also be enabled. Only known IDs or an exact unique match
   are accepted. Series identity precedes dependent seasons and episodes. The
   task never downloads a subtitle or writes an NFO or image to a media root.
   Online image downloads remain governed by their existing library option.
4. **Index video seek points** builds optional H.264/HEVC evidence from one
   preferred video track in the shared background analysis lane. The private
   cache is source-revision-bound and never changes the original media record,
   playback validators, track selections, or user history. An eligible
   progressive MP4 encode may use the ready result after source authorization;
   the existing bounded restart proof still applies. No playback request
   builds the full index. Original playback and permitted encoding remain
   available without it. Fast ingest does not supply HLS keyframe timelines,
   AV1/AAC joint seek evidence, or new nonzero copy-only plans. Use the explicit
   complete refresh for the original full probe evidence.

5. **Verify Dolby Vision** keeps the declared Dolby Vision profile, color and
   original stream indexes in the initial catalog, but verifies full RPU
   coverage, CRC and syntax in a separate task. It performs the same two
   read-only passes as the complete probe, so this moves I/O off the scan's
   critical path rather than reducing total reads. The task uses the shared
   background analysis slot and yields between streams. Its defaults include a
   successful scan signal, startup recovery, and a check every 15 minutes,
   with a 20-minute automatic run limit. Temporary verification failures back
   off for 15 and 30 minutes; a third failure needs a deliberate retry.
   A warm scan of an unchanged source does not reset its retry budget.

Dolby Vision verification is private, tied to the indexed source revision,
stream, verification version, and installed FFmpeg. Keep FFmpeg and its runtime
libraries unchanged while Goby is running. After a replacement and restart, a
successful fast rescan can enqueue new checks for previously settled sources.
Original playback and client-supported direct play need no result. New
conversion and background clip plans use applicable proof only after authorizing
the source; a request does not run the verifier. Unproved Dolby Vision is not
treated as ordinary HDR or SDR, and an existing HLS plan is not replaced. The
administrator's media details show verification status per
stream. `verified` means both physical checks completed, not that every
conversion is supported. `pending` or `retrying` is not proof; an unfinished
scan may need a successful rescan before this work can run. `unsupported`,
`failed`, and `unknown` do not authorize conversion.

The other fast-ingest tasks have hourly default retry opportunities.
Administrator cancellation of an ingest task suppresses automatic retry of the
affected scan generations; a fresh successful scan or a manual start can retry
them. An interrupted or cancelled scan cannot authorize its queued work.
Removing the fast-ingest setting stops new deferred work. Pending local imports
can still finish, and a synchronous rescan can reauthorize them without bypassing
the final source proof. No code writes sidecars to the media filesystem in this
feature.
