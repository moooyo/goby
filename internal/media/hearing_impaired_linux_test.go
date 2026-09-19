//go:build linux

package media

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHearingImpairedActualFFprobeDisposition(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("actual FFmpeg and ffprobe paths are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	directory := t.TempDir()
	caption := filepath.Join(directory, "dialogue.srt")
	if err := os.WriteFile(caption, []byte("1\n00:00:00,000 --> 00:00:00,800\nMeasured caption\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "hearing.mkv")
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=black:s=160x90:r=12:d=1", "-i", caption,
		"-map", "0:v:0", "-map", "1:s:0", "-map", "1:s:0", "-c:v", "libx264", "-threads:v", "1", "-c:s", "srt",
		"-disposition:s:0", "hearing_impaired", "-disposition:s:1", "0", path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil || stderr.Len() != 0 {
		t.Fatalf("create hearing-impaired source: %v: %s", err, stderr.String())
	}
	info, err := (Prober{FFprobePath: ffprobe}).Probe(ctx, path)
	if err != nil || info.ProbeVersion != CurrentProbeVersion || len(info.Streams) != 3 ||
		!info.Streams[1].IsHearingImpaired || info.Streams[2].IsHearingImpaired {
		t.Fatalf("actual ffprobe did not preserve the two distinct subtitle dispositions: %v", err)
	}
}
