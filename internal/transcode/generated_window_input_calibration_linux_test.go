//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This calibration records the pinned FFmpeg build's input association before
// source-origin arithmetic is enabled. It does not itself authorize a source
// range, infer CFR from metadata, or substitute for output closure evidence.
func TestGeneratedWindowInputClockCalibration(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for input-clock calibration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, offset := range []int{0, 2} {
		t.Run("format_offset_"+strconv.Itoa(offset), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "clock.mkv")
			generate := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=8", "-c:v", "ffv1", "-threads:v", "1",
				"-output_ts_offset", strconv.Itoa(offset), source)
			if output, err := generate.CombinedOutput(); err != nil {
				t.Fatalf("create input-clock fixture: %v: %s", err, output)
			}
			command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
				"-ss", "3", "-i", source, "-t", "2", "-map", "0:v:0", "-an", "-c:v", "libx264", "-threads:v", "1", "-r", "24", "-bf", "0",
				"-stats_enc_pre:v:0", "pipe:1", "-stats_enc_pre_fmt:v:0", "GOBY_INPUT {fidx} {sidx} {n} {ni} {tb} {pts} {tbi} {ptsi}",
				"-f", "null", "-")
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil || stderr.Len() != 0 {
				t.Fatalf("calibrate source input association: %v: %s", err, stderr.String())
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) != 48 {
				t.Fatalf("input observer omitted encoder frames: %d", len(lines))
			}
			for index, line := range lines {
				parts := strings.Fields(line)
				if len(parts) != 9 || parts[0] != "GOBY_INPUT" || parts[3] != strconv.Itoa(index) || parts[4] == "-1" || parts[7] == "0/1" || parts[8] == "9223372036854775807" {
					t.Fatalf("input association was unavailable or malformed: %s", line)
				}
			}
			observation, err := json.Marshal(map[string]any{"format_offset_seconds": offset, "input_seek_seconds": 3,
				"duration_seconds": 2, "frames": len(lines), "first": lines[0], "last": lines[len(lines)-1]})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("generated_input_clock_calibration %s", observation)
		})
	}
}
