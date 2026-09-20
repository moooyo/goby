//go:build linux

package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtractEmbeddedArtworkUsesIsolatedDescriptorAndAbsoluteStream(t *testing.T) {
	for name, value := range map[string]string{
		"GOBY_DATABASE_URL":     "must-not-reach-artwork-extractor",
		"GOBY_AUTH_SECRET":      "must-not-reach-artwork-extractor",
		"FFREPORT":              "file=must-not-write-artwork-report",
		"LD_PRELOAD":            "/must-not-load-artwork-library",
		"HTTP_PROXY":            "http://must-not-use-artwork-proxy.invalid",
		"HTTPS_PROXY":           "http://must-not-use-artwork-proxy.invalid",
		"AWS_SECRET_ACCESS_KEY": "must-not-reach-artwork-extractor",
	} {
		t.Setenv(name, value)
	}
	payload := embeddedArtworkLinuxPNG(t)
	file := subtitleExtractTestSource(t, payload)
	if err := os.Rename(file.Name(), file.Name()+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file.Name(), []byte("replacement must not be read"), 0600); err != nil {
		t.Fatal(err)
	}
	stream := embeddedArtworkLinuxStream(37)
	stream.Title = "../../untrusted; $title.png"
	info := embeddedArtworkLinuxInfo(t, file, stream)
	guard := "if [ -n \"${AWS_SECRET_ACCESS_KEY-}\" ]; then exit 92; fi\n"
	prober, probeDirectory, extractDirectory := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream),
		guard+"cat /proc/self/fd/3 > \"$directory/source\"\ncat \"$directory/metadata.json\"",
		guard+"cat /proc/self/fd/3")
	result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
	if err != nil || result.Version != EmbeddedArtworkVersion || len(result.Pictures) != 1 {
		t.Fatalf("extract through the isolated descriptor: version=%d, count=%d, error=%v", result.Version, len(result.Pictures), err)
	}
	picture := result.Pictures[0]
	if picture.StreamIndex != 37 || picture.PictureType != "Front" || picture.MIMEType != "image/png" ||
		picture.Width != 12 || picture.Height != 8 || picture.Hash != fmt.Sprintf("%x", sha256.Sum256(payload)) ||
		!bytes.Equal(picture.Data, payload) {
		t.Fatalf("extraction lost the original picture or absolute index: %+v", picture)
	}
	probed, err := os.ReadFile(filepath.Join(probeDirectory, "source"))
	if err != nil || !bytes.Equal(probed, payload) {
		t.Fatalf("metadata inspection did not read the original descriptor: %q, %v", probed, err)
	}
	for _, directory := range []string{probeDirectory, extractDirectory} {
		args := subtitleExtractTestArguments(t, directory)
		subtitleExtractAssertArgument(t, args, "-i", "/proc/self/fd/3")
		subtitleExtractAssertArgument(t, args, "-protocol_whitelist", "file,pipe")
		subtitleExtractAssertArgument(t, args, "-format_whitelist", probeFormats)
		if strings.Contains(strings.Join(args, "\n"), file.Name()) || strings.Contains(strings.Join(args, "\n"), stream.Title) {
			t.Fatalf("an untrusted pathname or stream title entered the subprocess arguments: %#v", args)
		}
	}
	probeArgs := subtitleExtractTestArguments(t, probeDirectory)
	subtitleExtractAssertArgument(t, probeArgs, "-of", "json")
	subtitleExtractAssertArgument(t, probeArgs, "-show_entries",
		"stream=index,codec_name,codec_type,width,height:stream_disposition=attached_pic:stream_tags=title,comment")
	extractArgs := subtitleExtractTestArguments(t, extractDirectory)
	for name, value := range map[string]string{"-map": "0:37", "-c:v": "copy", "-f": "image2pipe", "-frames:v": "1", "-threads": "1"} {
		subtitleExtractAssertArgument(t, extractArgs, name, value)
	}
	for _, argument := range []string{"-nostdin", "-an", "-sn", "-dn"} {
		if !strings.Contains("\n"+strings.Join(extractArgs, "\n")+"\n", "\n"+argument+"\n") {
			t.Fatalf("missing fixed extraction restriction %s: %#v", argument, extractArgs)
		}
	}
	if extractArgs[len(extractArgs)-1] != "pipe:1" {
		t.Fatalf("the extractor did not write only to stdout: %#v", extractArgs)
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractEmbeddedArtworkRejectsInvalidSourceBeforeStartingTools(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *Prober, **os.File, *Info)
	}{
		{"nil_file", func(_ *testing.T, _ *Prober, file **os.File, _ *Info) { *file = nil }},
		{"closed_file", func(t *testing.T, _ *Prober, file **os.File, _ *Info) {
			if err := (*file).Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory", func(t *testing.T, _ *Prober, file **os.File, _ *Info) {
			directory, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = directory.Close() })
			*file = directory
		}},
		{"old_probe", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.ProbeVersion = CurrentProbeVersion - 1 }},
		{"different_size", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.Size++ }},
		{"different_change_time", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.FileChangeTimeNs++ }},
		{"relative_ffprobe", func(_ *testing.T, prober *Prober, _ **os.File, _ *Info) { prober.FFprobePath = "ffprobe" }},
		{"relative_ffmpeg", func(_ *testing.T, prober *Prober, _ **os.File, _ *Info) { prober.FFmpegPath = "ffmpeg" }},
		{"negative_index", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.Streams[0].Index = -1 }},
		{"oversized_index", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.Streams[0].Index = 4096 }},
		{"audio_attachment", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.Streams[0].CodecType = "audio" }},
		{"external_attachment", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) { info.Streams[0].IsExternal = true }},
		{"duplicate_attachment", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) {
			info.Streams = append(info.Streams, info.Streams[0])
		}},
		{"too_many_pictures", func(_ *testing.T, _ *Prober, _ **os.File, info *Info) {
			info.Streams = nil
			for index := 0; index <= MaxEmbeddedArtworkPictures; index++ {
				info.Streams = append(info.Streams, embeddedArtworkLinuxStream(index))
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
			stream := embeddedArtworkLinuxStream(13)
			info := embeddedArtworkLinuxInfo(t, file, stream)
			prober, probeDirectory, extractDirectory := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream), "", "")
			test.mutate(t, &prober, &file, &info)
			result, err := prober.extractEmbeddedArtwork(context.Background(), file, info)
			if !errors.Is(err, ErrEmbeddedArtwork) || len(result.Pictures) != 0 {
				t.Fatalf("invalid source produced pictures or omitted its extraction error: count=%d, error=%v", len(result.Pictures), err)
			}
			embeddedArtworkLinuxAssertNotStarted(t, probeDirectory, extractDirectory)
		})
	}
}

func TestExtractEmbeddedArtworkResolvesConfiguredNamesThroughSymlinks(t *testing.T) {
	for _, name := range []string{"configured_names", "default_names"} {
		t.Run(name, func(t *testing.T) {
			payload := embeddedArtworkLinuxPNG(t)
			file := subtitleExtractTestSource(t, payload)
			stream := embeddedArtworkLinuxStream(17)
			info := embeddedArtworkLinuxInfo(t, file, stream)
			_, probeDirectory, extractDirectory := embeddedArtworkLinuxPathTools(t, stream)
			prober := Prober{}
			if name == "configured_names" {
				prober.FFprobePath, prober.FFmpegPath = "ffprobe", "ffmpeg"
			}
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if err != nil || result.Version != EmbeddedArtworkVersion || len(result.Pictures) != 1 {
				t.Fatalf("resolve configured tool names to their symlink targets: version=%d, count=%d, error=%v", result.Version, len(result.Pictures), err)
			}
			if picture := result.Pictures[0]; picture.StreamIndex != 17 || !bytes.Equal(picture.Data, payload) {
				t.Fatalf("resolved tools lost the original picture or absolute stream: %+v", picture)
			}
			// Helpers keep their metadata and invocation markers next to their
			// real executable, so success also proves symlink canonicalization.
			for _, directory := range []string{probeDirectory, extractDirectory} {
				subtitleExtractAssertArgument(t, subtitleExtractTestArguments(t, directory), "-i", "/proc/self/fd/3")
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
		})
	}
}

func TestExtractEmbeddedArtworkRejectsUnresolvableOrUnsafeConfiguredNames(t *testing.T) {
	for _, test := range []struct {
		name, ffprobe, ffmpeg string
		createUnsafeAlias     bool
	}{
		{name: "missing_ffprobe", ffprobe: "missing-configured-ffprobe", ffmpeg: "ffmpeg"},
		{name: "missing_ffmpeg", ffprobe: "ffprobe", ffmpeg: "missing-configured-ffmpeg"},
		{name: "newline_ffprobe", ffprobe: "ffprobe\n", ffmpeg: "ffmpeg", createUnsafeAlias: true},
		{name: "carriage_return_ffmpeg", ffprobe: "ffprobe", ffmpeg: "ffmpeg\r", createUnsafeAlias: true},
		{name: "nul_ffprobe", ffprobe: "ffprobe\x00", ffmpeg: "ffmpeg"},
		{name: "nul_ffmpeg", ffprobe: "ffprobe", ffmpeg: "ffmpeg\x00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
			stream := embeddedArtworkLinuxStream(17)
			info := embeddedArtworkLinuxInfo(t, file, stream)
			absolute, probeDirectory, extractDirectory := embeddedArtworkLinuxPathTools(t, stream)
			if test.createUnsafeAlias {
				// These names are valid Linux filenames. A real alias prevents
				// lookup failure from accidentally standing in for policy checks.
				for name, target := range map[string]string{test.ffprobe: absolute.FFprobePath, test.ffmpeg: absolute.FFmpegPath} {
					if !strings.ContainsAny(name, "\r\n") {
						continue
					}
					if err := os.Symlink(target, filepath.Join(os.Getenv("PATH"), name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			prober := Prober{FFprobePath: test.ffprobe, FFmpegPath: test.ffmpeg}
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if !errors.Is(err, ErrEmbeddedArtwork) || len(result.Pictures) != 0 {
				t.Fatalf("unsafe or missing configured tool did not fail extraction: count=%d, error=%v", len(result.Pictures), err)
			}
			embeddedArtworkLinuxAssertNotStarted(t, probeDirectory, extractDirectory)
			subtitleExtractAssertBorrowedOffset(t, file, 7)
		})
	}
}

func TestExtractEmbeddedArtworkEmptyCatalogNeedsNoExecutables(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	file := subtitleExtractTestSource(t, []byte("audio without attached pictures"))
	info := embeddedArtworkLinuxInfo(t, file, Stream{Index: 0, CodecType: "audio", Codec: "mp3"})
	result, err := (Prober{}).ExtractEmbeddedArtwork(context.Background(), file, info)
	if err != nil || result.Version != EmbeddedArtworkVersion || len(result.Pictures) != 0 {
		t.Fatalf("a current empty artwork catalog must be an explicit successful result: %+v, %v", result, err)
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractEmbeddedArtworkRejectsDirtyOrInvalidToolResults(t *testing.T) {
	for _, test := range []struct {
		name        string
		probeBody   string
		extractBody string
		metadata    func([]byte) []byte
		payload     func([]byte) []byte
		width       int
		noExtract   bool
	}{
		{name: "metadata_stderr", probeBody: "printf 'decoder warning' >&2\ncat \"$directory/metadata.json\"", noExtract: true},
		{name: "metadata_exit", probeBody: "exit 4", noExtract: true},
		{name: "metadata_missing_picture", metadata: func(_ []byte) []byte { return []byte(`{"streams":[]}`) }, noExtract: true},
		{name: "metadata_wrong_dimensions", metadata: func(data []byte) []byte { return bytes.Replace(data, []byte(`"width":12`), []byte(`"width":13`), 1) }, noExtract: true},
		{name: "metadata_trailing_json", metadata: func(data []byte) []byte { return append(data, []byte(` {"streams":[]}`)...) }, noExtract: true},
		{name: "extractor_stderr", extractBody: "printf 'decoder warning' >&2\ncat /proc/self/fd/3"},
		{name: "extractor_exit", extractBody: "cat /proc/self/fd/3\nexit 4"},
		{name: "empty_picture", extractBody: "exit 0"},
		{name: "non_image", payload: func(_ []byte) []byte { return []byte("not a decodable image") }},
		{name: "truncated_png", payload: func(data []byte) []byte { return data[:len(data)-8] }},
		{name: "actual_dimensions_disagree", width: 13},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := embeddedArtworkLinuxPNG(t)
			if test.payload != nil {
				payload = test.payload(payload)
			}
			file := subtitleExtractTestSource(t, payload)
			stream := embeddedArtworkLinuxStream(19)
			if test.width != 0 {
				stream.Width = test.width
			}
			info := embeddedArtworkLinuxInfo(t, file, stream)
			metadata := embeddedArtworkLinuxMetadata(stream)
			if test.metadata != nil {
				metadata = test.metadata(metadata)
			}
			prober, _, extractDirectory := embeddedArtworkLinuxProber(t, metadata, test.probeBody, test.extractBody)
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if !errors.Is(err, ErrEmbeddedArtwork) || len(result.Pictures) != 0 {
				t.Fatalf("invalid tool evidence produced artwork: count=%d, error=%v", len(result.Pictures), err)
			}
			if test.noExtract {
				embeddedArtworkLinuxAssertNotStarted(t, extractDirectory)
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
			if len(embeddedArtworkSlots) != 0 {
				t.Fatal("invalid tool evidence retained an extraction slot")
			}
		})
	}
}

func TestExtractEmbeddedArtworkStopsAtToolOutputBudgets(t *testing.T) {
	for _, stage := range []string{"metadata", "image"} {
		t.Run(stage, func(t *testing.T) {
			file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
			stream := embeddedArtworkLinuxStream(7)
			info := embeddedArtworkLinuxInfo(t, file, stream)
			probeBody, extractBody := "", ""
			if stage == "metadata" {
				probeBody = "dd if=/dev/zero bs=1048576 count=1 2>/dev/null\nprintf x\nsleep 30"
			} else {
				extractBody = fmt.Sprintf("dd if=/dev/zero bs=1048576 count=%d 2>/dev/null\nprintf x\nsleep 30", MaxEmbeddedArtworkBytes/(1<<20))
			}
			prober, _, extractDirectory := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream), probeBody, extractBody)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := prober.ExtractEmbeddedArtwork(ctx, file, info)
			if !errors.Is(err, ErrOutputLimit) || !errors.Is(err, ErrEmbeddedArtwork) || len(result.Pictures) != 0 {
				t.Fatalf("%s output exceeded its budget without a bounded failure: count=%d, error=%v", stage, len(result.Pictures), err)
			}
			if stage == "metadata" {
				embeddedArtworkLinuxAssertNotStarted(t, extractDirectory)
			}
			if len(embeddedArtworkSlots) != 0 {
				t.Fatal("output limit retained an extraction slot")
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
		})
	}
}

func TestExtractEmbeddedArtworkEnforcesRemainingAggregateBudget(t *testing.T) {
	file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
	first, second := embeddedArtworkLinuxStream(5), embeddedArtworkLinuxStream(11)
	info := embeddedArtworkLinuxInfo(t, file, first, second)
	// Each valid PNG packet is below the per-picture limit. Its trailing
	// padding makes the second packet exceed the remaining aggregate budget.
	prober, _, _ := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(first, second), "",
		"cat /proc/self/fd/3\ndd if=/dev/zero bs=1048576 count=17 2>/dev/null")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := prober.ExtractEmbeddedArtwork(ctx, file, info)
	if !errors.Is(err, ErrEmbeddedArtwork) || !errors.Is(err, ErrOutputLimit) || len(result.Pictures) != 0 {
		t.Fatalf("aggregate overflow must stop bounded output and discard the earlier picture: count=%d, error=%v", len(result.Pictures), err)
	}
	if len(embeddedArtworkSlots) != 0 {
		t.Fatal("aggregate output limit retained an extraction slot")
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractEmbeddedArtworkAcceptsPictureCountLimit(t *testing.T) {
	payload := embeddedArtworkLinuxPNG(t)
	file := subtitleExtractTestSource(t, payload)
	streams := make([]Stream, MaxEmbeddedArtworkPictures)
	for index := range streams {
		streams[index] = embeddedArtworkLinuxStream(2*index + 3)
	}
	info := embeddedArtworkLinuxInfo(t, file, streams...)
	prober, _, _ := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(streams...), "", "")
	result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
	if err != nil || len(result.Pictures) != MaxEmbeddedArtworkPictures {
		t.Fatalf("the supported picture-count boundary was rejected or truncated: count=%d, error=%v", len(result.Pictures), err)
	}
	for index, picture := range result.Pictures {
		if picture.StreamIndex != streams[index].Index || !bytes.Equal(picture.Data, payload) {
			t.Fatalf("picture %d lost its source index or bytes at the supported count limit", index)
		}
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractEmbeddedArtworkDoesNotPublishPartialPictureSet(t *testing.T) {
	file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
	first, second := embeddedArtworkLinuxStream(5), embeddedArtworkLinuxStream(11)
	info := embeddedArtworkLinuxInfo(t, file, first, second)
	prober, _, _ := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(first, second), "",
		"for argument in \"$@\"; do\n  if [ \"$argument\" = '0:11' ]; then printf 'invalid second picture'; exit 0; fi\ndone\ncat /proc/self/fd/3")
	result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
	if !errors.Is(err, ErrEmbeddedArtwork) || len(result.Pictures) != 0 {
		t.Fatalf("a later corrupt picture must discard the earlier valid picture: count=%d, error=%v", len(result.Pictures), err)
	}
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractEmbeddedArtworkCancellationStopsDescendantsAndReleasesSlot(t *testing.T) {
	for _, stage := range []string{"metadata", "image"} {
		t.Run(stage, func(t *testing.T) {
			file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
			stream := embeddedArtworkLinuxStream(13)
			info := embeddedArtworkLinuxInfo(t, file, stream)
			waiting := "cat /proc/self/fd/3 > /dev/null\nsleep 30 &\nchild=$!\nprintf '%s\\n' \"$child\" > \"$directory/ready\"\nwait \"$child\""
			probeBody, extractBody := "", ""
			if stage == "metadata" {
				probeBody = waiting
			} else {
				extractBody = waiting
			}
			prober, probeDirectory, extractDirectory := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream), probeBody, extractBody)
			readyDirectory := extractDirectory
			if stage == "metadata" {
				readyDirectory = probeDirectory
			}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("canceled artwork extraction did not finish during cleanup")
				}
			})
			go func() {
				_, err := prober.ExtractEmbeddedArtwork(ctx, file, info)
				result <- err
				close(finished)
			}()
			pid := embeddedArtworkLinuxWaitForChild(t, readyDirectory, result)
			if len(embeddedArtworkSlots) != 1 {
				t.Fatalf("active extraction holds %d slots, want one", len(embeddedArtworkSlots))
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrEmbeddedArtwork) {
					t.Errorf("canceling the active %s subprocess returned %v", stage, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled artwork extraction retained a process or pipe")
			}
			if len(embeddedArtworkSlots) != 0 {
				t.Fatal("canceled extraction retained an extraction slot")
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
			subtitleExtractAssertProcessStopped(t, pid)
			next, _, _ := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream), "", "")
			if _, err := next.ExtractEmbeddedArtwork(context.Background(), file, info); err != nil {
				t.Fatalf("extraction could not resume after cancellation: %v", err)
			}
		})
	}
}

func TestExtractEmbeddedArtworkCancellationWhileQueuedStartsNoTool(t *testing.T) {
	if len(embeddedArtworkSlots) != 0 {
		t.Fatal("another extraction retained a worker slot before the queue test")
	}
	for range cap(embeddedArtworkSlots) {
		embeddedArtworkSlots <- struct{}{}
	}
	defer func() {
		for range cap(embeddedArtworkSlots) {
			<-embeddedArtworkSlots
		}
	}()
	file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
	stream := embeddedArtworkLinuxStream(5)
	info := embeddedArtworkLinuxInfo(t, file, stream)
	prober, probeDirectory, extractDirectory := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream), "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := prober.ExtractEmbeddedArtwork(ctx, file, info)
	if !errors.Is(err, context.DeadlineExceeded) || len(result.Pictures) != 0 {
		t.Fatalf("queued cancellation returned pictures or lost its deadline: count=%d, error=%v", len(result.Pictures), err)
	}
	embeddedArtworkLinuxAssertNotStarted(t, probeDirectory, extractDirectory)
	subtitleExtractAssertBorrowedOffset(t, file, 7)
}

func TestExtractEmbeddedArtworkRejectsSourceMutationDuringExtraction(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"growth", "printf mutation >> /proc/self/fd/3"},
		{"same_size_timestamp_change", "touch -m -t 200001010000 /proc/self/fd/3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := subtitleExtractTestSource(t, embeddedArtworkLinuxPNG(t))
			before, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			stream := embeddedArtworkLinuxStream(23)
			info := embeddedArtworkLinuxInfo(t, file, stream)
			prober, _, _ := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream), "", "cat /proc/self/fd/3\n"+test.mutation)
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if !errors.Is(err, ErrEmbeddedArtwork) || len(result.Pictures) != 0 {
				t.Fatalf("a changing source published extracted pictures: count=%d, error=%v", len(result.Pictures), err)
			}
			after, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && FileChangeTime(before) == FileChangeTime(after) {
				t.Fatal("the controlled subprocess did not actually change its borrowed source")
			}
			subtitleExtractAssertBorrowedOffset(t, file, 7)
		})
	}
}

func embeddedArtworkLinuxPNG(t *testing.T) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, 12, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 13), G: uint8(y * 19), B: 73, A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func embeddedArtworkLinuxStream(index int) Stream {
	return Stream{Index: index, CodecType: "video", Codec: "png", IsAttachedPicture: true, Width: 12, Height: 8}
}

func embeddedArtworkLinuxInfo(t *testing.T, file *os.File, streams ...Stream) Info {
	t.Helper()
	stat, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return Info{ProbeVersion: CurrentProbeVersion, Size: stat.Size(), FileChangeTimeNs: FileChangeTime(stat), Streams: streams}
}

func embeddedArtworkLinuxMetadata(streams ...Stream) []byte {
	entries := make([]map[string]any, 0, len(streams))
	for _, stream := range streams {
		attached := 0
		if stream.IsAttachedPicture {
			attached = 1
		}
		entries = append(entries, map[string]any{"index": stream.Index, "codec_name": stream.Codec, "codec_type": stream.CodecType,
			"width": stream.Width, "height": stream.Height, "disposition": map[string]int{"attached_pic": attached},
			"tags": map[string]string{"comment": "Cover (front)", "title": stream.Title}})
	}
	data, _ := json.Marshal(map[string]any{"streams": entries})
	return data
}

func embeddedArtworkLinuxProber(t *testing.T, metadata []byte, probeBody, extractBody string) (Prober, string, string) {
	t.Helper()
	if probeBody == "" {
		probeBody = "cat \"$directory/metadata.json\""
	}
	if extractBody == "" {
		extractBody = "cat /proc/self/fd/3"
	}
	ffprobe, probeDirectory := subtitleExtractTestTool(t, probeBody)
	if err := os.WriteFile(filepath.Join(probeDirectory, "metadata.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	ffmpeg, extractDirectory := subtitleExtractTestTool(t, extractBody)
	return Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg}, probeDirectory, extractDirectory
}

func embeddedArtworkLinuxPathTools(t *testing.T, stream Stream) (Prober, string, string) {
	t.Helper()
	prober, probeDirectory, extractDirectory := embeddedArtworkLinuxProber(t, embeddedArtworkLinuxMetadata(stream),
		"/bin/cat \"$directory/metadata.json\"", "/bin/cat /proc/self/fd/3")
	directory := t.TempDir()
	for name, target := range map[string]string{"ffprobe": prober.FFprobePath, "ffmpeg": prober.FFmpegPath} {
		if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	// Only the controlled names are discoverable. The helpers use an absolute
	// cat path so successful extraction does not depend on the inherited PATH.
	t.Setenv("PATH", directory)
	return prober, probeDirectory, extractDirectory
}

func embeddedArtworkLinuxAssertNotStarted(t *testing.T, directories ...string) {
	t.Helper()
	for _, directory := range directories {
		if _, err := os.Stat(filepath.Join(directory, "arguments")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("an extraction tool unexpectedly started, or its marker cannot be inspected: %v", err)
		}
	}
}

func embeddedArtworkLinuxWaitForChild(t *testing.T, directory string, result <-chan error) int {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		if data, err := os.ReadFile(filepath.Join(directory, "ready")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		select {
		case err := <-result:
			t.Fatalf("extraction exited before starting its controlled descendant: %v", err)
		case <-deadline.C:
			t.Fatal("extraction never started its controlled descendant")
		case <-poll.C:
		}
	}
}
