//go:build linux

package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func subtitleExtractTestSource(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source with spaces; literal.mkv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return file
}

func subtitleExtractTestTool(t *testing.T, body string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "ffmpeg-helper")
	program := "#!/bin/sh\nset -eu\n" +
		"if [ -n \"${GOBY_DATABASE_URL-}${GOBY_AUTH_SECRET-}${FFREPORT-}${LD_PRELOAD-}${HTTP_PROXY-}${HTTPS_PROXY-}\" ]; then exit 91; fi\n" +
		"directory=${0%/*}\nprintf '%s\\n' \"$@\" > \"$directory/arguments\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	return path, directory
}

func subtitleExtractTestArguments(t *testing.T, directory string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func subtitleExtractAssertArgument(t *testing.T, args []string, name, value string) {
	t.Helper()
	count := 0
	for index, argument := range args {
		if argument != name {
			continue
		}
		count++
		if index+1 >= len(args) || args[index+1] != value {
			t.Fatalf("argument %s must be followed by %q: %#v", name, value, args)
		}
	}
	if count != 1 {
		t.Fatalf("argument %s occurred %d times, want once: %#v", name, count, args)
	}
}

func subtitleExtractAssertBorrowedOffset(t *testing.T, file *os.File, want int64) {
	t.Helper()
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != want {
		t.Fatalf("extraction closed or moved the borrowed descriptor: position %d, error %v", position, err)
	}
}

func TestExtractSubtitleUsesAbsoluteStreamAndIsolatedBorrowedDescriptor(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "must-not-reach-subtitle-extractor")
	t.Setenv("GOBY_AUTH_SECRET", "must-not-reach-subtitle-extractor")
	t.Setenv("FFREPORT", "file=must-not-write-subtitle-report")
	t.Setenv("LD_PRELOAD", "/must-not-load-subtitle-library")
	t.Setenv("HTTP_PROXY", "http://must-not-use-subtitle-proxy.invalid")
	t.Setenv("HTTPS_PROXY", "http://must-not-use-subtitle-proxy.invalid")
	for _, test := range []struct {
		name, codec, format, outputCodec, muxer string
	}{
		{"srt", "mov_text", "srt", "srt", "srt"},
		{"vtt", "webvtt", "vtt", "webvtt", "webvtt"},
		{"ass_copy", "ass", "ass", "copy", "ass"},
		{"ssa_copy", "ssa", "ssa", "copy", "ass"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := []byte("1\n00:00:01,000 --> 00:00:02,000\nHeld source\n")
			file := subtitleExtractTestSource(t, payload)
			// A replaced pathname must not redirect the authorized descriptor.
			if err := os.Rename(file.Name(), file.Name()+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file.Name(), []byte("replacement must not be read"), 0600); err != nil {
				t.Fatal(err)
			}
			executable, directory := subtitleExtractTestTool(t, "cat /proc/self/fd/3")
			data, err := ExtractSubtitle(context.Background(), executable, file, Stream{Index: 11, CodecType: "subtitle", Codec: test.codec}, test.format)
			if err != nil || !bytes.Equal(data, payload) {
				t.Fatalf("descriptor extraction = %q, %v; want %q", data, err, payload)
			}
			args := subtitleExtractTestArguments(t, directory)
			subtitleExtractAssertArgument(t, args, "-map", "0:11")
			subtitleExtractAssertArgument(t, args, "-i", "/proc/self/fd/3")
			subtitleExtractAssertArgument(t, args, "-protocol_whitelist", "file,pipe")
			subtitleExtractAssertArgument(t, args, "-format_whitelist", probeFormats)
			subtitleExtractAssertArgument(t, args, "-c:s", test.outputCodec)
			subtitleExtractAssertArgument(t, args, "-f", test.muxer)
			subtitleExtractAssertArgument(t, args, "-threads", "1")
			for _, required := range []string{"-nostdin", "-vn", "-an", "-dn"} {
				if !strings.Contains("\n"+strings.Join(args, "\n")+"\n", "\n"+required+"\n") {
					t.Fatalf("missing fixed restriction %s: %#v", required, args)
				}
			}
			if args[len(args)-1] != "pipe:1" || strings.Contains(strings.Join(args, "\n"), file.Name()) {
				t.Fatalf("extraction did not use only descriptor input and stdout output: %#v", args)
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
		})
	}
}

func TestExtractSubtitleStopsAtOutputBudget(t *testing.T) {
	file := subtitleExtractTestSource(t, []byte("controlled source"))
	executable, _ := subtitleExtractTestTool(t,
		"dd if=/dev/zero bs=1048576 count=8 2>/dev/null\nprintf x\nsleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := ExtractSubtitle(ctx, executable, file, Stream{Index: 7, CodecType: "subtitle", Codec: "subrip"}, "srt")
	if !errors.Is(err, ErrOutputLimit) || !errors.Is(err, ErrSubtitleExtraction) || len(data) != 0 {
		t.Fatalf("8 MiB plus one byte returned %d bytes, %v", len(data), err)
	}
	if len(subtitleExtractionSlots) != 0 {
		t.Fatal("output-limit failure retained an extraction slot")
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractSubtitleCancellationReleasesSlotAndPreservesDescriptor(t *testing.T) {
	file := subtitleExtractTestSource(t, []byte("controlled source"))
	executable, directory := subtitleExtractTestTool(t,
		"cat /proc/self/fd/3 > /dev/null\nsleep 30 &\nchild=$!\nprintf '%s\\n' \"$child\" > \"$directory/ready\"\nwait \"$child\"")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("canceled extraction did not finish during cleanup")
		}
	})
	go func() {
		_, err := ExtractSubtitle(ctx, executable, file, Stream{Index: 13, CodecType: "subtitle", Codec: "ass"}, "ass")
		result <- err
		close(finished)
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	var pid int
	for pid == 0 {
		if data, err := os.ReadFile(filepath.Join(directory, "ready")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if pid > 0 {
				break
			}
		}
		select {
		case err := <-result:
			t.Fatalf("extractor exited before starting its controlled descendant: %v", err)
		case <-deadline.C:
			t.Fatal("extractor never started its controlled descendant")
		case <-poll.C:
		}
	}
	if len(subtitleExtractionSlots) != 1 {
		t.Fatalf("active extraction holds %d slots, want one", len(subtitleExtractionSlots))
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrSubtitleExtraction) {
			t.Errorf("active extraction cancellation returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled extraction retained a live process or pipe")
	}
	if len(subtitleExtractionSlots) != 0 {
		t.Fatal("canceled extraction retained its slot")
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
	subtitleExtractAssertProcessStopped(t, pid)
	// A subsequent request must be able to use the descriptor and worker slots.
	nextTool, _ := subtitleExtractTestTool(t, "cat /proc/self/fd/3")
	if _, err := ExtractSubtitle(context.Background(), nextTool, file, Stream{Index: 13, CodecType: "subtitle", Codec: "ass"}, "ass"); err != nil {
		t.Fatalf("extraction could not resume after cancellation: %v", err)
	}
}

func subtitleExtractAssertProcessStopped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		closing := strings.LastIndexByte(string(data), ')')
		if err == nil && closing >= 0 && len(data) > closing+2 && data[closing+2] == 'Z' {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("canceled extraction left a running descendant")
		case <-poll.C:
		}
	}
}

func TestExtractFontAttachmentChecksMagicAndAbsoluteIndex(t *testing.T) {
	for _, test := range []struct {
		name, magic string
		valid       bool
	}{
		{"truetype", "\x00\x01\x00\x00", true},
		{"opentype", "OTTO", true},
		{"collection", "ttcf", true},
		{"apple_truetype", "true", true},
		{"png_disguised_as_font", "\x89PNG", false},
		{"short_font", "OT", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := append([]byte(test.magic), make([]byte, 8)...)
			file := subtitleExtractTestSource(t, payload)
			executable, directory := subtitleExtractTestTool(t, "cat /proc/self/fd/3")
			stream := Stream{Index: 19, CodecType: "attachment", Codec: "ttf", MIMEType: "font/ttf", Filename: "../../must-not-write.ttf"}
			data, err := ExtractFontAttachment(context.Background(), executable, file, stream)
			if test.valid {
				if err != nil || !bytes.Equal(data, payload) {
					t.Fatalf("valid font extraction = %q, %v", data, err)
				}
			} else if !errors.Is(err, ErrSubtitleExtraction) || len(data) != 0 {
				t.Fatalf("invalid font magic returned %q, %v", data, err)
			}
			args := subtitleExtractTestArguments(t, directory)
			subtitleExtractAssertArgument(t, args, "-dump_attachment:19", "pipe:1")
			subtitleExtractAssertArgument(t, args, "-i", "/proc/self/fd/3")
			subtitleExtractAssertArgument(t, args, "-protocol_whitelist", "file,pipe")
			subtitleExtractAssertArgument(t, args, "-format_whitelist", probeFormats)
			if strings.Contains(strings.Join(args, "\n"), stream.Filename) {
				t.Fatal("untrusted attachment filename reached the extractor")
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
		})
	}
}

func TestExtractSubtitleActualFFmpegPreservesASSAndEmbeddedFont(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set GOBY_FFMPEG and GOBY_FFPROBE to run the Linux subtitle extraction integration test")
	}
	directory := t.TempDir()
	srtPath := filepath.Join(directory, "captions.srt")
	assPath := filepath.Join(directory, "styled.ass")
	fontPath := filepath.Join(directory, "embedded.ttf")
	mediaPath := filepath.Join(directory, "subtitles-and-font.mkv")
	srt := "1\n00:00:00,100 --> 00:00:00,900\nPlain embedded caption\n"
	ass := "[Script Info]\nScriptType: v4.00+\nPlayResX: 640\nPlayResY: 360\n\n[V4+ Styles]\n" +
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Accent,Embedded,28,&H0000FF00,&H00FFFFFF,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1\n\n[Events]\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:00.10,0:00:00.90,Accent,,0,0,0,,{\\i1}Styled embedded caption\n"
	// Attachment extraction preserves opaque font data; this controlled sfnt
	// header is sufficient to distinguish the attachment from subtitle packets.
	font := []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	for path, data := range map[string][]byte{srtPath: []byte(srt), assPath: []byte(ass), fontPath: font} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := runLimited(context.Background(), 20*time.Second, 1024, ffmpeg,
		"-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "anullsrc=r=8000:cl=mono",
		"-i", srtPath, "-i", assPath, "-map", "0:a:0", "-map", "1:0", "-map", "2:0",
		"-c:a", "pcm_s16le", "-c:s", "copy", "-t", "1", "-attach", fontPath,
		"-metadata:s:t:0", "mimetype=application/x-truetype-font", "-metadata:s:t:0", "filename=embedded.ttf",
		"-f", "matroska", mediaPath)
	if err != nil {
		t.Fatalf("generate subtitle fixture: %v", err)
	}
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(context.Background(), mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	streams := make(map[int]Stream)
	for _, stream := range info.Streams {
		streams[stream.Index] = stream
	}
	if streams[0].CodecType != "audio" || streams[1].Codec != "subrip" || streams[2].Codec != "ass" || !FontAttachment(streams[3]) {
		t.Fatalf("fixture did not retain absolute stream indexes: %#v", info.Streams)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	plain, err := ExtractSubtitle(context.Background(), ffmpeg, file, streams[1], "srt")
	if err != nil || !strings.Contains(string(plain), "Plain embedded caption") || strings.Contains(string(plain), "Styled embedded caption") {
		t.Fatalf("selected SRT stream = %q, %v", plain, err)
	}
	styled, err := ExtractSubtitle(context.Background(), ffmpeg, file, streams[2], "ass")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"[Script Info]", "PlayResX: 640", "[V4+ Styles]", "Style: Accent,Embedded,28", "&H0000FF00", `{\i1}Styled embedded caption`} {
		if !strings.Contains(string(styled), fragment) {
			t.Errorf("native ASS extraction lost %q: %q", fragment, styled)
		}
	}
	embedded, err := ExtractFontAttachment(context.Background(), ffmpeg, file, streams[3])
	if err != nil || !bytes.Equal(embedded, font) {
		t.Fatalf("embedded font = %q, %v; want %q", embedded, err, font)
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}
