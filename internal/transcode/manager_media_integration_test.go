//go:build linux

package transcode_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

// This exercises the real planner -> PostgreSQL admission -> manager -> FFmpeg
// -> measured playlist -> readable/decodable segment chain. It does not claim
// that the Emby HTTP HLS graph or a third-party client's seek behavior is wired.
func TestManagerPersistsAndDecodesPlannedMedia(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual Linux media verification")
	}
	ctx, pool, repository, owner, _ := encodingRepositoryFixture(t)
	fileName := filepath.Join(t.TempDir(), "owned-source.mp4")
	generate := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=12:duration=8", "-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=8",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0", "-bf", "0",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-t", "8", fileName)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate owned manager media: %v: %s", err, output)
	}
	initial, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(initial)
	prober := media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}
	info, err := prober.Probe(ctx, fileName)
	if err != nil {
		t.Fatalf("probe manager source: %v", err)
	}
	source := playback.Source{ItemID: owner.ItemID, MediaSourceID: owner.SourceID, Path: fileName, ItemType: "Movie", Info: info}
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET duration_ticks = $2 WHERE id = $1", owner.PlaySessionID, info.DurationTicks); err != nil {
		t.Fatal(err)
	}
	manager, err := transcode.NewManager(ctx, transcode.Options{
		Root: t.TempDir(), FFmpegPath: ffmpeg, Repository: repository, Threads: 1,
		MaxJobs: 1, MaxUserJobs: 1, MaxSessionJobs: 1, MaxBytes: 32 << 20, MaxJobBytes: 8 << 20, MinFreeBytes: 1 << 20,
		StartupTimeout: 20 * time.Second, NoProgressTimeout: 20 * time.Second, IdleTimeout: time.Minute, MaxRuntime: time.Minute,
	})
	if err != nil {
		t.Fatalf("create real conversion manager: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Errorf("close real conversion manager: %v", err)
		}
	})
	truth, falsity := true, false
	segmentLength, maxWidth, maxHeight := 2, 96, 54
	for _, mode := range []string{"remux", "audio", "video"} {
		t.Run(mode, func(t *testing.T) {
			profile := playback.TranscodingProfile{Container: "ts", Protocol: "hls", Type: playback.DlnaProfileTypeVideo,
				VideoCodec: "h264", AudioCodec: "aac", SegmentLength: &segmentLength}
			request := playback.Request{ID: owner.ItemID, MediaSourceID: owner.SourceID, EnableDirectPlay: &falsity,
				EnableDirectStream: &truth, EnableTranscoding: &truth}
			if mode == "audio" {
				request.AllowAudioStreamCopy = &falsity
				profile.AudioCodec = "mp3"
			}
			if mode == "video" {
				request.AllowVideoStreamCopy = &falsity
				profile.MaxWidth, profile.MaxHeight = &maxWidth, &maxHeight
			}
			request.DeviceProfile = &playback.DeviceProfile{TranscodingProfiles: []playback.TranscodingProfile{profile}}
			decision, err := playback.PlanConversion(source, request, playback.ConversionLimits{
				AllowRemux: true, AllowAudioTranscode: true, AllowVideoTranscode: true,
			})
			if err != nil || decision.Plan == nil {
				t.Fatalf("plan %s conversion: %v, reasons = %+v", mode, err, decision.Reasons)
			}
			input, err := os.Open(fileName)
			if err != nil {
				t.Fatal(err)
			}
			spec := transcode.Spec{Scope: owner, SourceStamp: hex.EncodeToString(digest[:]), Plan: *decision.Plan}
			record, err := manager.Ensure(ctx, spec, input)
			if err != nil {
				t.Fatalf("admit planned conversion: %v", err)
			}
			if _, err := manager.WaitReady(ctx, owner, record.ID); err != nil {
				t.Fatalf("wait for actual playable output: %v", err)
			}
			for {
				stored := encodingStoredRecord(t, ctx, pool, record.ID)
				if stored.State == "completed" {
					if stored.OutputBytes <= 0 || stored.ErrorCode != "" {
						t.Fatalf("completed conversion lacks durable output facts: %+v", stored)
					}
					break
				}
				if stored.State != "running" && stored.State != "queued" {
					t.Fatalf("conversion failed: state = %s, code = %s", stored.State, stored.ErrorCode)
				}
				select {
				case <-ctx.Done():
					t.Fatal("conversion completion exceeded its test budget")
				case <-time.After(20 * time.Millisecond):
				}
			}
			handle, err := manager.Open(ctx, owner, record.ID, "main.m3u8")
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := io.ReadAll(io.LimitReader(handle, transcode.MaxPlaylistBytes+1))
			_ = handle.Close()
			if err != nil {
				t.Fatal(err)
			}
			playlist, err := transcode.ParseMediaPlaylist(manifest)
			if err != nil || !playlist.Ended || len(playlist.Segments) < 3 {
				t.Fatalf("parse measured manager output: %+v, error = %v", playlist, err)
			}
			var duration int64
			for _, segment := range playlist.Segments {
				handle, err := manager.Open(ctx, owner, record.ID, segment.Name)
				if err != nil {
					t.Fatal(err)
				}
				// Probe and decode the exact opened output descriptor, retaining
				// the reader lease until both consumers have completed.
				facts, err := prober.ProbeFile(ctx, handle.File)
				if err != nil {
					_ = handle.Close()
					t.Fatalf("probe actual segment: %v", err)
				}
				videoFound, audioFound := false, false
				for _, stream := range facts.Streams {
					if stream.CodecType == "video" {
						videoFound = stream.Codec == "h264"
						if mode == "video" && (stream.Width != maxWidth || stream.Height != maxHeight) {
							t.Errorf("encoded dimensions = %dx%d", stream.Width, stream.Height)
						}
					}
					if stream.CodecType == "audio" {
						wanted := "aac"
						if mode == "audio" {
							wanted = "mp3"
						}
						audioFound = stream.Codec == wanted
					}
				}
				if !videoFound || !audioFound {
					t.Error("planned codecs differ from actual media streams")
				}
				decode := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-threads", "1",
					"-i", "/proc/self/fd/3", "-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-f", "null", "-")
				decode.ExtraFiles = []*os.File{handle.File}
				output, decodeErr := decode.CombinedOutput()
				_ = handle.Close()
				if decodeErr != nil || len(output) != 0 {
					t.Fatalf("decode planned segment: %v: %s", decodeErr, output)
				}
				duration += segment.DurationTicks
			}
			if delta := duration - info.DurationTicks; delta < -media.TicksPerSecond/5 || delta > media.TicksPerSecond/5 {
				t.Errorf("playlist/source duration difference = %d ticks", delta)
			}
			duplicate, err := os.Open(fileName)
			if err != nil {
				t.Fatal(err)
			}
			reused, err := manager.Ensure(ctx, spec, duplicate)
			if err != nil || reused.ID != record.ID {
				t.Fatalf("completed media was not reused: %v", err)
			}
			if _, err := duplicate.Stat(); err == nil {
				t.Error("duplicate source descriptor remained owned by the caller")
			}
		})
	}
	final, err := os.ReadFile(fileName)
	if err != nil || sha256.Sum256(final) != digest {
		t.Fatal("conversion modified the original source bytes")
	}
	var position int64
	var plays int
	if err := pool.QueryRow(ctx, "SELECT playback_position_ticks, play_count FROM user_item_data WHERE user_id=$1 AND item_id=$2", owner.UserID, owner.ItemID).Scan(&position, &plays); err != nil || position != 0 || plays != 7 {
		t.Fatalf("conversion fabricated playback state: position = %d, plays = %d, error = %v", position, plays, err)
	}
}
