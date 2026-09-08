package media

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeActualFFprobeAndStableDescriptor(t *testing.T) {
	ffprobe, ffmpeg := os.Getenv("GOBY_FFPROBE"), os.Getenv("GOBY_FFMPEG")
	if ffprobe == "" || ffmpeg == "" {
		t.Skip("set GOBY_FFPROBE and GOBY_FFMPEG to run the Linux media integration test")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("media runtime verification must run on Linux")
	}
	path := filepath.Join(t.TempDir(), "movie; $filename.wav")
	_, err := runLimited(context.Background(), 20*time.Second, 1024, ffmpeg,
		"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2",
		"-c:v", "libx264", "-c:a", "aac", "-metadata:s:a:0", "language=eng",
		"-metadata:s:a:0", "title=Original audio", "-f", "matroska", path)
	if err != nil {
		t.Fatalf("generate fixture: %v", err)
	}
	prober := Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}
	info, err := prober.Probe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "matroska,webm" || len(info.Streams) != 2 || info.Streams[0].Codec != "h264" ||
		info.Streams[0].Width != 160 || info.Streams[0].Height != 90 || info.Streams[1].Codec != "aac" ||
		info.Streams[1].Language != "eng" || info.Streams[1].Title != "Original audio" || info.Size <= 0 ||
		info.DurationTicks < 20_000_000 || info.DurationTicks > 21_000_000 {
		t.Fatalf("unexpected media metadata: %+v", info)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Seek(13, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	// Replacing the pathname after a contained open must not change the object
	// handed to ffprobe. The replacement cannot be probed as media.
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a media file"), 0600); err != nil {
		t.Fatal(err)
	}
	held, err := prober.ProbeFile(context.Background(), file)
	if err != nil || held.Size != info.Size || held.Streams[0].Codec != "h264" {
		t.Fatalf("held descriptor did not preserve the source: %+v, %v", held, err)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 13 {
		t.Fatalf("caller file was closed or its offset changed: %d, %v", position, err)
	}
	if _, err := prober.Probe(context.Background(), path); err == nil {
		t.Fatal("the replacement invalid media file was accepted")
	}
}

func TestProbeActualFFprobeCannotFollowNetworkPlaylist(t *testing.T) {
	ffprobe := os.Getenv("GOBY_FFPROBE")
	if ffprobe == "" {
		t.Skip("set GOBY_FFPROBE to verify protocol restrictions on Linux")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("media runtime verification must run on Linux")
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "the media probe must not request this resource", http.StatusForbidden)
	}))
	defer server.Close()
	playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:2,\n" +
		server.URL + "/segment.ts\n#EXT-X-ENDLIST\n"
	path := filepath.Join(t.TempDir(), "untrusted.m3u8")
	if err := os.WriteFile(path, []byte(playlist), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (Prober{FFprobePath: ffprobe, Timeout: 5 * time.Second}).Probe(context.Background(), path)
	if err == nil {
		t.Fatal("the network playlist was accepted")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("ffprobe made %d prohibited network requests", got)
	}
}

func TestProbeActualFFprobeCannotFollowExternalFileManifest(t *testing.T) {
	ffprobe, ffmpeg := os.Getenv("GOBY_FFPROBE"), os.Getenv("GOBY_FFMPEG")
	if ffprobe == "" || ffmpeg == "" {
		t.Skip("set GOBY_FFPROBE and GOBY_FFMPEG to verify manifest restrictions on Linux")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("media runtime verification must run on Linux")
	}
	outside := filepath.Join(t.TempDir(), "outside-library.ts")
	_, err := runLimited(context.Background(), 20*time.Second, 1024, ffmpeg,
		"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24",
		"-t", "1", "-c:v", "libx264", "-f", "mpegts", outside)
	if err != nil {
		t.Fatal(err)
	}
	prober := Prober{FFprobePath: ffprobe, Timeout: 5 * time.Second}
	if _, err := prober.Probe(context.Background(), outside); err != nil {
		t.Fatalf("the external target must itself be valid standalone media: %v", err)
	}
	libraryRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer libraryRoot.Close()
	for name, content := range map[string]string{
		"playlist.m3u8":     "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n" + outside + "\n#EXT-X-ENDLIST\n",
		"playlist.ffconcat": "ffconcat version 1.0\nfile '" + outside + "'\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := libraryRoot.WriteFile(name, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := libraryRoot.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			_, err = prober.ProbeFile(context.Background(), file)
			if err == nil {
				t.Fatal("a manifest referencing media outside the opened root was accepted")
			}
			// FFmpeg 9 can reject HLS before selecting a demuxer because the
			// procfs input has no playlist extension. Concat probes by content.
			if strings.HasSuffix(name, ".ffconcat") && !strings.Contains(err.Error(), "whitelist") {
				t.Fatalf("the concat demuxer was not explicitly rejected: %v", err)
			}
		})
	}
}
