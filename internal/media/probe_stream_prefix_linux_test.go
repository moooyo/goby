//go:build linux

package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeStreamPrefixNeverRunsCompleteAudioTimingScan(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "ffprobe")
	script := `#!/bin/sh
for argument in "$@"; do
  case "$argument" in
    -show_packets|-show_frames|-show_chapters) exit 41 ;;
  esac
done
printf '%s' '{"format":{"format_name":"aac","duration":"9000","size":"99999999","bit_rate":"128000"},"streams":[{"index":0,"codec_name":"aac","codec_type":"audio","sample_rate":"48000","channels":2,"duration":"9000","time_base":"1/48000"}]}'
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(directory, "prefix-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("partial authorized prefix")); err != nil {
		t.Fatal(err)
	}
	info, err := (Prober{FFprobePath: executable, AnalyzeVideoSeek: true, FFmpegPath: "/must-not-run"}).ProbeStreamPrefix(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Streams) != 1 || info.Streams[0].Codec != "aac" || info.Streams[0].SampleRate != 48000 || info.DurationTicks != 0 || info.Size != 0 || info.AudioDurationExact || info.Streams[0].AudioTiming != nil || len(info.VideoSeekIndexes) != 0 || info.FileChangeTimeNs != 0 {
		t.Fatalf("prefix invented complete presentation facts: %+v", info)
	}
	args := strings.Join([]string{info.Container, info.AudioDurationReason}, " ")
	if !strings.Contains(args, "streaming_input") {
		t.Fatal("missing prefix provenance")
	}
}

func TestProbeStreamPrefixRejectsUnboundedAndCancelledInputs(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "prefix-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate((1 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := (Prober{FFprobePath: "/must-not-run"}).ProbeStreamPrefix(context.Background(), file); err == nil {
		t.Fatal("oversized prefix accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Prober{}).ProbeStreamPrefix(ctx, file); err != context.Canceled {
		t.Fatalf("cancelled prefix = %v", err)
	}
}
