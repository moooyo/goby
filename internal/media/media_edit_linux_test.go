package media

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mediaEditProcessTestFiles(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	input, err := os.CreateTemp(t.TempDir(), "source-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	if _, err := input.WriteString("authorized source bytes"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	candidate, err := os.CreateTemp(t.TempDir(), "candidate-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = candidate.Close() })
	return input, candidate
}

func mediaEditProcessTestTool(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trusted-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSubtitleRemovalProcessHardOutputBudgetAndDescriptorIsolation(t *testing.T) {
	input, candidate := mediaEditProcessTestFiles(t)
	t.Setenv("GOBY_DATABASE_URL", "must-not-reach-media-edit")
	t.Setenv("LD_PRELOAD", "/must-not-load")
	t.Setenv("FFREPORT", "file=must-not-write")
	t.Setenv("HTTP_PROXY", "http://must-not-use.invalid")
	tool := mediaEditProcessTestTool(t, `if [ -n "${GOBY_DATABASE_URL-}${LD_PRELOAD-}${FFREPORT-}${HTTP_PROXY-}" ]; then exit 91; fi
cat /proc/self/fd/3 > /proc/self/fd/4`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runMediaEditRemux(ctx, tool, input, candidate, 4096, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(candidate.Name())
	if err != nil || string(data) != "authorized source bytes" {
		t.Fatalf("remux read the wrong source: %q, %v", data, err)
	}
	position, err := input.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 {
		t.Fatalf("remux moved or closed its borrowed input: %d, %v", position, err)
	}
	if err := candidate.Truncate(0); err != nil {
		t.Fatal(err)
	}
	oversized := mediaEditProcessTestTool(t, "dd if=/dev/zero of=/proc/self/fd/4 bs=8192 count=8 2>/dev/null")
	if err := runMediaEditRemux(ctx, oversized, input, candidate, 4096, nil); !errors.Is(err, ErrSubtitleRemovalBudget) {
		t.Fatalf("hard file budget returned %v", err)
	}
	info, err := candidate.Stat()
	if err != nil || info.Size() > 4096 {
		t.Fatalf("candidate crossed the hard disk budget: %+v, %v", info, err)
	}
	if err := candidate.Truncate(0); err != nil {
		t.Fatal(err)
	}
	seekPastLimit := mediaEditProcessTestTool(t, "exec dd if=/dev/zero of=/proc/self/fd/4 bs=4096 seek=2 count=1 2>/dev/null")
	if err := runMediaEditRemux(ctx, seekPastLimit, input, candidate, 4096, nil); !errors.Is(err, ErrSubtitleRemovalBudget) {
		t.Fatalf("seek past hard file budget returned %v", err)
	}
	info, err = candidate.Stat()
	if err != nil || info.Size() > 4096 {
		t.Fatalf("seek write crossed the hard disk budget: %+v, %v", info, err)
	}
}

func TestSubtitleRemovalProcessCancellationRetiresDescendants(t *testing.T) {
	input, candidate := mediaEditProcessTestFiles(t)
	tool := mediaEditProcessTestTool(t, `directory=${0%/*}
sleep 30 &
child=$!
printf '%s\n' "$child" > "$directory/ready"
wait "$child"`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		result <- runMediaEditRemux(ctx, tool, input, candidate, 1<<20, nil)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("canceled remux did not finish during cleanup")
		}
	})
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	var pid int
	for pid == 0 {
		if data, err := os.ReadFile(filepath.Join(filepath.Dir(tool), "ready")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if pid != 0 {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("remux exited before starting descendant: %v", err)
		case <-deadline.C:
			t.Fatal("remux did not start its controlled descendant")
		case <-poll.C:
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled remux retained a process or output pipe")
	}
	subtitleExtractAssertProcessStopped(t, pid)
}

func TestSubtitleRemovalMP4RejectsScratchBudgetBeforeMediaInspection(t *testing.T) {
	input, candidate := mediaEditProcessTestFiles(t)
	before, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	_, err = RemuxSubtitleRemoval(context.Background(), input, candidate, SubtitleRemovalOptions{Container: "mp4", StreamIndex: 1,
		FFmpegPath: "/must-not-launch-ffmpeg", FFprobePath: "/must-not-launch-ffprobe", MaxOutputBytes: before.Size() - 1})
	if !errors.Is(err, ErrSubtitleRemovalBudget) {
		t.Fatalf("equal-size structural copy was not rejected before inspecting invalid media bytes: %v", err)
	}
	after, err := candidate.Stat()
	if err != nil || after.Size() != 0 {
		t.Fatalf("budget rejection wrote a candidate: %+v %v", after, err)
	}
}

func TestSubtitleRemovalActualMediaPreservesSelectedProfiles(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set GOBY_FFMPEG and GOBY_FFPROBE for the Linux subtitle removal integration test")
	}
	for _, container := range []string{"mkv", "mka", "mp4"} {
		t.Run(container, func(t *testing.T) {
			directory := t.TempDir()
			record := retainMediaEditTestEvidence(t, directory, container)
			for name, data := range map[string][]byte{
				"remove.srt":     []byte("1\n00:00:00,200 --> 00:00:01,100\nRemove exactly this track\n"),
				"keep.srt":       []byte("1\n00:00:00,400 --> 00:00:01,300\nRetain this track\n"),
				"chapters.txt":   []byte(";FFMETADATA1\ntitle=Preserved fixture\ncomment=Preserved global comment\ncreation_time=2026-01-01T00:00:00Z\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=800\ntitle=Opening\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=800\nEND=2000\ntitle=Body\n"),
				"attachment.ttf": {0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			} {
				if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(directory, "source."+container)
			args := []string{"-v", "error", "-threads", "1", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
				"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-i", filepath.Join(directory, "remove.srt"), "-i", filepath.Join(directory, "keep.srt"),
				"-f", "ffmetadata", "-i", filepath.Join(directory, "chapters.txt")}
			removedIndex, retainedStreams := 3, 5
			if container != "mka" {
				args = append(args, "-map", "0:v:0")
			} else {
				removedIndex, retainedStreams = 2, 4
			}
			args = append(args, "-map", "1:a:0", "-map", "2:a:0", "-map", "3:s:0", "-map", "4:s:0", "-map_metadata", "5", "-c:v", "libx264", "-bf", "0", "-threads", "1", "-t", "2",
				"-metadata:s:a:0", "language=eng", "-metadata:s:a:0", "title=English audio", "-metadata:s:a:1", "language=chi", "-metadata:s:a:1", "title=Chinese audio",
				"-metadata:s:s:0", "language=eng", "-metadata:s:s:1", "language=chi", "-disposition:s:0", "default", "-disposition:s:1", "forced+hearing_impaired")
			if container == "mp4" {
				args = append(args, "-map_chapters", "-1", "-c:a", "aac", "-c:s", "mov_text", "-use_editlist", "0", "-movflags", "use_metadata_tags", "-f", "mp4")
				retainedStreams--
			} else {
				args = append(args, "-map_chapters", "5", "-c:a", "pcm_s16le", "-c:s", "srt", "-attach", filepath.Join(directory, "attachment.ttf"), "-metadata:s:t:0", "filename=Fixture.ttf", "-metadata:s:t:0", "mimetype=font/ttf", "-f", "matroska")
			}
			args = append(args, path)
			if _, err := runLimited(context.Background(), 30*time.Second, 4096, ffmpeg, args...); err != nil {
				record.Failure = err.Error()
				t.Fatalf("generate %s source: %v", container, err)
			}
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if _, err := input.Seek(13, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			candidate, err := os.CreateTemp(directory, "candidate-*.")
			if err != nil {
				t.Fatal(err)
			}
			defer candidate.Close()
			record.Stage = "candidate_edit_and_proof"
			evidence, err := RemuxSubtitleRemoval(context.Background(), input, candidate, SubtitleRemovalOptions{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Container: container, StreamIndex: removedIndex, Timeout: 90 * time.Second})
			record.Result = &evidence
			if err != nil {
				record.Failure = err.Error()
				t.Fatalf("remove subtitle with complete preservation evidence: %v", err)
			}
			if evidence.Version != mediaEditProofVersion || evidence.RemovedIndex != removedIndex || len(evidence.RetainedStreams) != retainedStreams || evidence.CandidateBytes <= 0 || len(evidence.MetadataSHA256) != 64 || len(evidence.ContainerSHA256) != 64 || len(evidence.SourceSHA256) != 64 || len(evidence.CandidateSHA256) != 64 {
				t.Fatalf("incomplete preservation evidence: %+v", evidence)
			}
			if container == "mp4" {
				if evidence.Engine != "structural_edit" || len(evidence.PreservedBytesSHA256) != 64 || evidence.SourceBytes != evidence.CandidateBytes {
					t.Fatalf("MP4 structural editing evidence is incomplete: %+v", evidence)
				}
			} else if evidence.Engine != "ffmpeg_remux" || evidence.PreservedBytesSHA256 != "" {
				t.Fatalf("incorrect Matroska preparation engine evidence: %+v", evidence)
			}
			for _, stream := range evidence.RetainedStreams {
				if stream.SourceIndex == removedIndex || stream.CodecType != "attachment" && (stream.Packets == 0 || len(stream.PayloadSHA256) != 64 || len(stream.TimingSHA256) != 64) || stream.CodecType == "attachment" && len(stream.ExtradataSHA256) != 64 {
					t.Fatalf("incomplete retained stream proof: %+v", stream)
				}
			}
			position, err := input.Seek(0, io.SeekCurrent)
			if err != nil || position != 13 {
				t.Fatalf("borrowed source offset changed: %d, %v", position, err)
			}
			record.Stage = "candidate_probe"
			info, err := (Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), candidate)
			record.CandidateInfo = &info
			if err != nil || len(info.Streams) != retainedStreams {
				t.Fatalf("published facts are unavailable: %+v, %v", info, err)
			}
			subtitles := 0
			for _, stream := range info.Streams {
				if stream.CodecType == "subtitle" {
					subtitles++
					if stream.Language != "chi" {
						t.Fatalf("the wrong subtitle was removed: %+v", stream)
					}
				}
			}
			if subtitles != 1 || container != "mp4" && len(info.Chapters) != 2 {
				t.Fatalf("chapter or subtitle preservation failed: %+v", info)
			}
			record.Stage = "candidate_decode"
			decodeEvidence, err := decodeMediaEditTestCandidate(context.Background(), ffmpeg, candidate, info)
			record.CandidateDecode = &decodeEvidence
			if err != nil {
				record.Failure = err.Error()
				t.Fatalf("complete retained audio/video decode failed: %v", err)
			}
			expectedVideo := 1
			if container == "mka" {
				expectedVideo = 0
			}
			if !decodeEvidence.Complete || !decodeEvidence.ProgressEnd || decodeEvidence.OutTimeUS < 1_900_000 || decodeEvidence.OutTimeUS > 2_100_000 || decodeEvidence.AudioStreams != 2 ||
				decodeEvidence.VideoStreams != expectedVideo || len(decodeEvidence.MappedIndexes) != 2+expectedVideo || expectedVideo != 0 && decodeEvidence.VideoFrames != 50 {
				record.Failure = "candidate decode evidence is incomplete"
				t.Fatalf("candidate decode did not prove complete mapped A/V output: %+v", decodeEvidence)
			}
			record.Stage = "complete"
		})
	}
}
