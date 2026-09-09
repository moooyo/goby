# Generated progressive MP4 fixture

`progressive-video-first-fragment.mp4` contains the initialization and first
complete fragment of synthetic H.264/AAC media, copied into an append-only MP4
with FFmpeg 9.0.1 on the authorized Linux test host. The fixture contains no
authentication token or media URL. Its source and audio were generated for the
Goby verification environment.

- Size: 26,392 bytes.
- SHA-256: `59dead0c726e9b005234a9bc45363a43f4435e2b040520a574443b2af8d6a6ec`.
- Fragment settings: `default_base_moof`, one-second fragment duration, and a
  one-MiB fragment-size target. The fragment boundaries are not a claim that all
  subsequent fragments are independent random-access units.
- Video track: ID 1, `tfhd` flags `0x20038`, `trun` flags `0xa05`, 24 samples,
  relative data offset 556.
- Audio track: ID 2, `tfhd` flags `0x20038`, `trun` flags `0x201`, 44 samples,
  relative data offset 17,413.

The producing engine task decoded this fragment with FFmpeg `-xerror` before
providing it. The local unit tests inspect growing prefixes and observer
dispatch; they do not substitute for deployed complete-stream decoding or
assert that a prefix validates later fragments.

## Detection boundaries

The video detector accepts one `avc1` H.264 track and, when selected, one `mp4a`
AAC track. It checks track identifiers, self-contained data references, empty
initial sample tables, AVC parameter sets, and AAC configuration fields. AAC
GA configurations and SBR/PS extensions are bounded and checked, including
required PCE and core-coder fields. AAC ELD, nonzero ER protection modes, and
unsupported channel-configuration codes are outside this video initialization
subset. The existing audio-only readiness function is unchanged.

Every declared sample run must reference an initialized track, use the
controlled movie-fragment-relative addressing mode, and lie inside that
fragment's single declared `mdat` without overlapping another run. Absolute
base addresses and external data references are rejected.

Readiness requires a complete video sample whose complete length-prefixed NAL
sequence includes a VCL unit. Selected audio must have valid initialization,
but its first samples may arrive later. The detector does not decode slice
macroblocks or certify the complete audio bitstream, subsequent fragments,
final file completion, or random-access independence.

The startup prefix stays capped at 4 MiB, with additional element and sample
count limits. A declared `mdat` may exceed that cap without being allocated or
read in full. A usable complete video sample must nevertheless occur within the
prefix; a larger first usable sample fails startup explicitly. Fragment-size
and duration settings are muxer targets and do not guarantee this bound.

`tfdt`, composition offsets, and signed edit-list times are read structurally.
The detector never modifies them or substitutes a new timeline origin.
