package transcode

import "strconv"

// appendLiveBitmapInputArgs consumes the independently owned source pipe.
// A/V streams must remain enabled: their source-clock packets drive FFmpeg's
// sub2video heartbeat between sparse bitmap events. The companion null output
// requests stream copy, so this demuxer never decodes an additional video.
func appendLiveBitmapInputArgs(args []string, p Plan, threads int) []string {
	if p.SourceMode != "stream" || !hasBitmapSubtitleInput(p) {
		return args
	}
	return append(args, "-threads", strconv.Itoa(threads), "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats,
		"-itsoffset", signedTickSeconds(LiveSourceClockBiasTicks(p)), "-i", "pipe:"+strconv.Itoa(liveBitmapFD(p)))
}

// appendLiveBitmapHeartbeatOutputArgs must follow all media, subtitle, and
// packet-clock outputs, preserving their output indices. Copying selected A/V
// packets to the null muxer keeps demux_send active without another decoder,
// independent timestamp normalization, or a fabricated wall-clock heartbeat.
func appendLiveBitmapHeartbeatOutputArgs(args []string, p Plan) []string {
	if p.SourceMode != "stream" || !hasBitmapSubtitleInput(p) || p.VideoStreamIndex < 0 {
		return args
	}
	args = append(args, "-map", "1:"+strconv.Itoa(p.VideoStreamIndex))
	if p.AudioStreamIndex >= 0 {
		args = append(args, "-map", "1:"+strconv.Itoa(p.AudioStreamIndex))
	}
	return append(args, "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn", "-c", "copy",
		"-avoid_negative_ts", "disabled", "-f", "null", "pipe:1")
}
