//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/subtitle"
)

func liveCaptionMediaCommand(t *testing.T, executable, directory string, files []*os.File, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir, command.ExtraFiles = directory, files
	command.Env = processEnvironment()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil || stderr.Len() != 0 {
		t.Fatalf("actual caption command failed: %v: %s", err, stderr.String())
	}
	return stdout.Bytes()
}

func liveCaptionCSVTicks(t *testing.T, raw string) int64 {
	t.Helper()
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 20 {
		t.Fatal("caption fixture returned an invalid reference clock")
	}
	return int64(math.Round(seconds * float64(ticksPerSecond)))
}

// This gate checks the actual builder and finite descriptor extractor together.
// The separately retained pipe-open probe also verifies that copied companions
// publish sparse/empty tracks before input EOF; this finite test does not claim
// a live scheduling result merely because FFmpeg eventually exits successfully.
func TestLiveCaptionActualCompanionsPreserveSourceClocksAndCrossingCues(t *testing.T) {
	executable := os.Getenv("GOBY_FFMPEG")
	if executable == "" {
		t.Skip("GOBY_FFMPEG is required for actual caption companion verification")
	}
	for _, test := range []struct{ name, audio, caption, container string }{
		{"aac-subrip", "aac", "subrip", "matroska"},
		{"opus-subrip", "libopus", "subrip", "matroska"},
		{"aac-mov-text", "aac", "mov_text", "mp4"},
		{"video-b-frames", "", "subrip", "matroska"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			cuePath := filepath.Join(directory, "long.srt")
			if err := os.WriteFile(cuePath, []byte("1\n00:00:00,500 --> 00:00:05,500\nLONG\n\n"), 0600); err != nil {
				t.Fatal(err)
			}
			p := liveCaptionTestPlan()
			for slot := range p.HLS.Subtitles.Count {
				p.HLS.Subtitles.Tracks[slot].Codec = test.caption
			}
			args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=s=160x90:r=12:d=8"}
			subtitleInput := "2:s:0"
			if test.audio != "" {
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=8")
			} else {
				subtitleInput = "1:s:0"
				p.AudioStreamIndex, p.AudioCodec, p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = -1, "", 0, 0, 0
				p.HLS.Subtitles.Tracks[0].StreamIndex, p.HLS.Subtitles.Tracks[1].StreamIndex = 1, 2
			}
			args = append(args, "-i", cuePath, "-map", "0:v:0")
			if test.audio != "" {
				args = append(args, "-map", "1:a:0", "-c:a", test.audio, "-threads:a", "1")
			}
			captionEncoder := test.caption
			if captionEncoder == "subrip" {
				captionEncoder = "srt"
			}
			args = append(args, "-map", subtitleInput, "-map", subtitleInput, "-c:v", "libx264", "-threads:v", "1",
				"-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-bf", "2", "-pix_fmt", "yuv420p",
				"-c:s", captionEncoder, "-bsf:s:1", "noise=drop=1")
			sourcePath := filepath.Join(directory, "source.mkv")
			if test.container == "mp4" {
				sourcePath = filepath.Join(directory, "source.mp4")
				args = append(args, "-movflags", "+empty_moov+frag_keyframe+default_base_moof")
			} else {
				args = append(args, "-live", "1")
			}
			args = append(args, sourcePath)
			liveCaptionMediaCommand(t, executable, directory, nil, args...)
			baselineBytes := liveCaptionMediaCommand(t, executable, directory, nil, "-hide_banner", "-nostdin", "-v", "error", "-copyts", "-threads", "1",
				"-i", sourcePath, "-map", "0:s:0", "-vn", "-an", "-dn", "-c:s", "webvtt", "-avoid_negative_ts", "disabled", "-f", "webvtt", "pipe:1")
			baseline, err := subtitle.Parse(baselineBytes, subtitle.FormatWebVTT)
			if err != nil || len(baseline.Cues) != 1 || baseline.Cues[0].Text != "LONG" {
				t.Fatal("actual source does not contain the expected complete long cue")
			}
			want := baseline.Cues[0]
			want.StartTicks += LiveSourceClockBiasTicks(p)
			want.EndTicks += LiveSourceClockBiasTicks(p)
			for slot := range p.HLS.Subtitles.Count {
				input, err := os.Open(sourcePath)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = input.Close() })
				journalPath := filepath.Join(directory, "list-"+strconv.Itoa(slot)+".csv")
				journal, err := os.OpenFile(journalPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					input.Close()
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = journal.Close() })
				output, err := BuildLiveCaptionCompanionArgs(p, slot, 4)
				if err != nil {
					t.Fatal(err)
				}
				command := []string{"-hide_banner", "-nostdin", "-v", "error", "-copyts", "-threads", "1",
					"-itsoffset", signedTickSeconds(LiveSourceClockBiasTicks(p)), "-i", "/proc/self/fd/3"}
				liveCaptionMediaCommand(t, executable, directory, []*os.File{input, journal}, append(command, output...)...)
				input.Close()
				journal.Close()
				raw, err := os.ReadFile(journalPath)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
				if err != nil || len(rows) < 3 {
					t.Fatal("reference-clock companion did not close its caption windows")
				}
				var firstDocument subtitle.Document
				for sequence, row := range rows {
					name, err := LiveCaptionName(p, slot, int64(sequence))
					if err != nil || len(row) != 3 || row[0] != name {
						t.Fatal("caption CSV escaped the expected private sequence")
					}
					file, err := os.Open(filepath.Join(directory, name))
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = file.Close() })
					if _, err := file.Seek(7, io.SeekStart); err != nil {
						t.Fatal(err)
					}
					segment := LiveCaptionSegment{Slot: slot, Sequence: int64(sequence), StartTicks: liveCaptionCSVTicks(t, row[1]),
						EndTicks: liveCaptionCSVTicks(t, row[2]), File: file, SubtitleStreamIndex: 1, Codec: test.caption, Container: test.container}
					data, extractErr := ExtractLiveCaption(context.Background(), executable, segment)
					position, seekErr := file.Seek(0, io.SeekCurrent)
					file.Close()
					if extractErr != nil || seekErr != nil || position != 7 {
						t.Fatalf("caption extraction failed or changed the borrowed descriptor: %v, %v, %d", extractErr, seekErr, position)
					}
					document, err := subtitle.Parse(data, subtitle.FormatWebVTT)
					if err != nil {
						t.Fatal("finite caption output is not valid WebVTT")
					}
					if slot == 1 || sequence > 0 {
						if len(document.Cues) != 0 {
							t.Fatal("quiet copied text manufactured repeated or delayed packets")
						}
					} else {
						if len(document.Cues) != 1 || document.Cues[0].Text != want.Text ||
							absLiveCaptionTicks(document.Cues[0].StartTicks-want.StartTicks) > 10_000 || absLiveCaptionTicks(document.Cues[0].EndTicks-want.EndTicks) > 10_000 ||
							segment.EndTicks >= want.EndTicks {
							t.Fatal("companion changed the source cue clock or let its long duration advance the reference watermark")
						}
						firstDocument = document
					}
					if slot == 0 && sequence == 1 {
						window, err := subtitle.RenderHLSWindow(firstDocument, segment.StartTicks, segment.EndTicks, 0, 0)
						if err != nil || !strings.Contains(string(window.Data), "LONG") {
							t.Fatal("a complete long cue did not survive the following empty companion segment")
						}
					}
				}
			}
		})
	}
}

func absLiveCaptionTicks(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func TestExtractLiveCaptionRejectsUnboundInputAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractLiveCaption(ctx, "/not-executed", LiveCaptionSegment{}); !errors.Is(err, context.Canceled) {
		t.Fatal("caption extraction ignored prior cancellation")
	}
	for _, segment := range []LiveCaptionSegment{{}, {Complete: true}, {Slot: -1}, {Slot: 8}, {SubtitleStreamIndex: 2}} {
		if _, err := ExtractLiveCaption(context.Background(), "/not-executed", segment); !errors.Is(err, ErrLiveCaption) {
			t.Fatal("caption extraction accepted unbound source metadata")
		}
	}
}
