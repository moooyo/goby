package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoSeekActualStaticParameterSetsAndRestartProof(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "restart.mp4")
	audioProbeRunFFmpeg(t, ffmpeg,
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=12", "-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000",
		"-t", "6", "-c:v", "libx264", "-threads:v", "1", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-bf", "2",
		"-pix_fmt", "yuv420p", "-c:a", "aac", source)
	for _, extension := range []string{"mp4", "mkv", "ts"} {
		t.Run(extension, func(t *testing.T) {
			path := source
			if extension != "mp4" {
				path = filepath.Join(directory, "restart."+extension)
				audioProbeRunFFmpeg(t, ffmpeg, "-copyts", "-i", source, "-map", "0", "-c", "copy", "-avoid_negative_ts", "disabled", path)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.Seek(17, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: true, Timeout: 10 * time.Second}).ProbeFile(context.Background(), file)
			if err != nil || len(info.VideoSeekIndexes) != 1 || len(info.VideoSeekIndexes[0].Entries) != 3 {
				t.Fatalf("actual %s index did not prove all three static IDRs: %+v, %v", extension, info.VideoSeekIndexes, err)
			}
			index := info.VideoSeekIndexes[0]
			if !videoSeekSHA256(index.ParameterSetsSHA256) || index.DecodedFrameBytes != 6144 {
				t.Fatalf("missing static parameter sets or decoded representation: %+v", index)
			}
			candidate, err := SelectVideoSeekCandidate(index, 43_700_000)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := ValidateVideoSeekCandidate(candidate)
			if err != nil {
				t.Fatal(err)
			}
			verification, err := VerifyVideoSeekCandidate(context.Background(), ffmpeg, file, candidate, 1)
			validArgument := false
			for _, inputTicks := range videoSeekCandidateAttempts(prepared) {
				validArgument = validArgument || verification.InputSeekTicks == inputTicks
			}
			if err != nil || !verification.Verified || !validArgument {
				t.Fatalf("actual %s preflight did not preserve its proven input argument: %+v, %v", extension, verification, err)
			}
			position, err := file.Seek(0, io.SeekCurrent)
			if err != nil || position != 17 {
				t.Fatalf("index/proof changed the caller's descriptor position: %d, %v", position, err)
			}
		})
	}
}

func TestVideoSeekActualOpenGOPDoesNotAuthorizeOtherKeyPictures(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "open-gop.mp4")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=64x64:rate=12", "-t", "8",
		"-c:v", "libx264", "-threads:v", "1", "-pix_fmt", "yuv420p", "-bf", "2",
		"-x264-params", "open-gop=1:keyint=24:min-keyint=24:scenecut=0", path)
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: true, Timeout: 10 * time.Second}).Probe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	// A conservative unsupported parameter-set layout may discard the whole
	// optional index. Any retained entry must still be the original true IDR.
	for _, index := range info.VideoSeekIndexes {
		if len(index.Entries) != 1 || index.Entries[0].PTS != 0 {
			t.Fatalf("open-GOP key pictures became restart evidence: %+v", index)
		}
		if _, err := SelectVideoSeekCandidate(index, 65_000_000); err == nil {
			t.Fatal("an open-GOP key picture authorized a positive restart")
		}
	}
}
