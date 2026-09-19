# Live bitmap subtitle clock

A live bitmap burn uses two independent readers of the same authorized byte
generation. Input zero provides the main video/audio. Input one decodes the
selected bitmap subtitle and stream-copies the selected video and optional audio
to a trailing null output. The second input uses the same
`LiveSourceClockBiasTicks` as the first; the viewer's subtitle offset stays in the
subtitle filter and is not applied twice.

The trailing output is required for sparse subtitle events. In FFmpeg n9.0.1,
unused/discarded A/V packets bypass `demux_send`, which supplies sub2video
heartbeats. A subtitle-only demuxer therefore can stop advancing after its first
empty frame until a future cue or EOF arrives. Keeping selected source A/V packets
active supplies real source timestamps without another A/V decode or an invented
wall-clock cadence. The filter initializes an empty canvas on a heartbeat, then
honors actual subtitle display and clear events. See the upstream
[demuxer](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_demux.c) and
[sub2video filter integration](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_filter.c).

The null output must remain after all media, subtitle, and packet-clock outputs,
preserving their output indices. It must not use `shortest`, frame limits, an
encoder, or an independent timestamp adjustment. Video-only sources need only
the selected video packet stream. Bitmap burn without a main video is rejected.
Unselected and finite sources do not acquire this output. The companion output
does not imply complete text subtitle delivery or provide a cue-completeness
watermark.

The bounded diagnostic uses authored PGS events and Matroska clusters delivered
according to their actual source timestamps. The discard control emits only one
frame during the first five seconds of both a silent subtitle stream and a stream
whose first displayed cue begins at six seconds. The heartbeat version emits 79
frames in that interval. Both versions eventually emit all 192 frames of the
12-second source, so final frame count alone would miss the stall. The successful
version also preserves source PTS and the exact visible frame interval 96 through
111, followed by a clear canvas.

`TestLiveBitmapActualSilentPeriodsAdvanceBeforeFutureCuesAndEOF` makes future
clusters unavailable until the output advances, independently of wall-clock
pacing or FFmpeg `-re`. It checks video-only empty subtitles and delayed PGS with
audio, confirms progress before future cues and before EOF, and checks every
frame's PTS and caption pixels. Unit tests separately cover input ownership,
shared clock bias, selected stream mapping, and unaffected non-bitmap paths.
