//go:build linux

package transcode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureGeneratedSourceRangeActualSEIEmptyProjection(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	path := filepath.Join(t.TempDir(), "actual-sei-source.mp4")
	// Keep the original zero-origin source. Encoder SEI is retained so the
	// closed JSON parser, rather than source rewriting, must handle projection.
	generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=160x96:r=24:d=12",
		"-an", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-g", "24", "-use_editlist", "0", "-movflags", "+faststart", path)
	data := generatedWindowMediaCommand(t, ctx, ffprobe,
		"-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats,
		"-select_streams", "0", "-read_intervals", "0%6", "-show_frames", "-show_streams", "-show_format", "-show_entries",
		"frame=media_type,stream_index,pts,duration:frame_side_data=:stream=index,codec_type,time_base:stream_tags=:stream_disposition=:stream_side_data=:format=start_time:format_tags=",
		"-of", "json", "-i", path)
	var document generatedSourceRangeDocument
	if err := json.Unmarshal(data, &document); err != nil || len(document.Frames) != 144 {
		t.Fatal("actual source projection did not retain its complete 144-frame interval")
	}
	var first generatedSourceRangeFrame
	if err := json.Unmarshal(document.Frames[0], &first); err != nil || len(first.ProjectedSideData) == 0 {
		t.Fatal("actual encoder SEI did not exercise an empty projected frame-side-data wrapper")
	}
	plan := generatedClosureMediaPlan("mpegts", 12, 0, 6, false)
	parsed, err := parseGeneratedSourceRange(data, plan, 0)
	if err != nil || parsed.FrameCount != 144 || parsed.First != (GeneratedRational{Num: 0, Den: 1}) ||
		parsed.Last != (GeneratedRational{Num: 143, Den: 24}) || parsed.End != (GeneratedRational{Num: 6, Den: 1}) {
		t.Fatalf("actual empty SEI projection changed source-clock coverage: %+v, %v", parsed, err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	measured, err := MeasureGeneratedSourceRange(ctx, ffprobe, source, plan, 0)
	if err != nil || measured != parsed {
		t.Fatalf("governed actual source observation differs from the complete projected frame clocks: %+v, %v", measured, err)
	}
	certificate, err := MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
	if err != nil || certificate.Origin != (GeneratedRational{Num: 0, Den: 1}) || certificate.DurationTicks != 12*ticksPerSecond || certificate.SampleCount != 288 {
		t.Fatalf("empty frame projection changed the independent native sample-set endpoint: %+v, %v", certificate, err)
	}
	t.Logf("actual original SEI source projection: frames=%d first=%+v last=%+v end=%+v projected_bytes=%d", measured.FrameCount,
		measured.First, measured.Last, measured.End, len(first.ProjectedSideData))
}
