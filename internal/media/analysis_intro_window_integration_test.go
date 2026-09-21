package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in generated fixture proves extraction-window and source-clock
// mechanics. It is not evidence of intro matching accuracy on real episodes.
func TestAnalysisIntroActualFractionalAudioTailPreservesStrictVisualEnd(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	fingerprint := os.Getenv("GOBY_INTRO_FINGERPRINT")
	if fingerprint == "" {
		t.Skip("set GOBY_INTRO_FINGERPRINT to the separately built pinned PCM helper")
	}
	const (
		videoFrames    = 750
		videoRate      = 25
		audioRate      = 48000
		audioSamples   = 1_440_240
		visualWindow   = 30 * TicksPerSecond
		actualAudioEnd = visualWindow + TicksPerSecond/200
		visualInterval = TicksPerSecond / 2
	)
	path := filepath.Join(t.TempDir(), "video-30s-audio-30_005s.mkv")
	// Twenty-millisecond PCM packets, with padding disabled, leave an actual
	// five-millisecond final packet on Matroska's millisecond timestamp grid.
	// The independent decoded inventory below must prove that this happened.
	audioProbeRunFFmpeg(t, ffmpeg,
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25,trim=end_frame=750",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000,atrim=end_sample=1440240,asetnsamples=n=960:p=0",
		"-map", "0:v:0", "-map", "1:a:0", "-copyts", "-filter_threads", "1",
		"-fps_mode:v", "passthrough", "-enc_time_base:v", "filter",
		"-c:v", "ffv1", "-threads:v", "1", "-pix_fmt", "yuv420p",
		"-c:a", "pcm_s16le", "-threads:a", "1", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatalf("probe actual mixed-duration fixture: %v", err)
	}
	if !info.FormatStartKnown || info.FormatStartTicks != 0 || info.DurationTicks != actualAudioEnd || len(info.Streams) != 2 {
		t.Fatalf("fixture did not preserve the intended actual container horizon: %+v", info)
	}
	audio := audioProbeSingleAudioStream(t, info)
	var video Stream
	videoCount := 0
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture {
			video, videoCount = stream, videoCount+1
		}
	}
	if videoCount != 1 || video.Codec != "ffv1" || audio.Codec != "pcm_s16le" || audio.SampleRate != audioRate || audio.Channels != 1 {
		t.Fatalf("fixture stream inventory changed: %+v", info.Streams)
	}
	encoded, err := runLimited(context.Background(), 30*time.Second, 1<<20, ffprobe,
		"-v", "error", "-threads", "1", "-fflags", "+nofillin", "-show_frames",
		"-show_entries", "frame=media_type,stream_index,pts,duration,pkt_duration,nb_samples", "-of", "json", path)
	if err != nil {
		t.Fatalf("read independent decoded frame inventory: %v", err)
	}
	var inventory struct {
		Frames []struct {
			MediaType      string `json:"media_type"`
			StreamIndex    int    `json:"stream_index"`
			PTS            *int64 `json:"pts"`
			Duration       *int64 `json:"duration"`
			PacketDuration *int64 `json:"pkt_duration"`
			Samples        int    `json:"nb_samples"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(encoded, &inventory); err != nil || len(inventory.Frames) < videoFrames || len(inventory.Frames) > 4096 {
		t.Fatalf("invalid bounded frame inventory: frames=%d, error=%v", len(inventory.Frames), err)
	}
	videoBase, err := analysisTimeBase(video.TimeBase)
	if err != nil {
		t.Fatal(err)
	}
	audioBase, err := analysisTimeBase(audio.TimeBase)
	if err != nil {
		t.Fatal(err)
	}
	var sourceVideo []int64
	var decodedSamples, videoEnd, audioEnd int64
	for index, frame := range inventory.Frames {
		if frame.PTS == nil {
			t.Fatalf("decoded frame %d has no source PTS", index)
		}
		switch frame.MediaType {
		case "video":
			ticks, err := analysisVisualTicks(*frame.PTS, videoBase, info.FormatStartTicks)
			if err != nil || frame.StreamIndex != video.Index || len(sourceVideo) >= videoFrames || ticks != int64(len(sourceVideo))*TicksPerSecond/videoRate {
				t.Fatalf("fixture video frame %d changed its actual cadence: pts=%d ticks=%d error=%v", len(sourceVideo), *frame.PTS, ticks, err)
			}
			duration := frame.Duration
			if duration == nil {
				duration = frame.PacketDuration
			}
			if duration == nil {
				t.Fatalf("video frame %d lacks its decoded duration", index)
			}
			durationTicks, err := analysisVisualTicks(*duration, videoBase, 0)
			if err != nil || durationTicks != TicksPerSecond/videoRate {
				t.Fatalf("fixture video frame duration changed: %d ticks, %v", durationTicks, err)
			}
			sourceVideo = append(sourceVideo, ticks)
			videoEnd = ticks + durationTicks
		case "audio":
			ticks, err := analysisVisualTicks(*frame.PTS, audioBase, info.FormatStartTicks)
			if err != nil || frame.StreamIndex != audio.Index || frame.Samples <= 0 || ticks != decodedSamples*TicksPerSecond/audioRate {
				t.Fatalf("fixture PCM frame lost its sample-bound clock: frame=%d ticks=%d preceding_samples=%d count=%d error=%v", index, ticks, decodedSamples, frame.Samples, err)
			}
			decodedSamples += int64(frame.Samples)
			audioEnd = ticks + int64(frame.Samples)*TicksPerSecond/audioRate
		default:
			t.Fatalf("unexpected decoded frame type %q", frame.MediaType)
		}
	}
	if len(sourceVideo) != videoFrames || sourceVideo[len(sourceVideo)-1] != 749*TicksPerSecond/videoRate || videoEnd != visualWindow ||
		decodedSamples != audioSamples || audioEnd != actualAudioEnd || info.DurationTicks != audioEnd {
		t.Fatalf("fixture does not reproduce a fractional audio-only tail: video_frames=%d video_end=%d audio_samples=%d audio_end=%d duration=%d", len(sourceVideo), videoEnd, decodedSamples, audioEnd, info.DurationTicks)
	}
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe, FingerprintPath: fingerprint, Limits: AnalysisLimits{Timeout: 30 * time.Second}}
	request := IntroAnalysisRequest{AudioStreamIndex: audio.Index, VideoStreamIndex: video.Index}
	features, err := extractor.ExtractIntro(context.Background(), file, info, request)
	if err != nil {
		t.Fatalf("default intro extraction rejected proven full visual periods: duration=%d last_video_pts=%d error=%v", info.DurationTicks, sourceVideo[len(sourceVideo)-1], err)
	}
	if features.WindowTicks != info.DurationTicks || features.VisualWindowTicks != visualWindow || len(features.Visual) != 60 || len(features.Audio) == 0 {
		t.Fatalf("intro horizons or evidence differ: audio_window=%d visual_window=%d visual_samples=%d audio_samples=%d", features.WindowTicks, features.VisualWindowTicks, len(features.Visual), len(features.Audio))
	}
	if features.AudioMetadata.SampleRate <= 0 || features.AudioMetadata.InputSamples <= 30*int64(features.AudioMetadata.SampleRate) {
		t.Fatalf("visual flooring also truncated actual audio evidence: %+v", features.AudioMetadata)
	}
	reference := 0
	for index, sample := range features.Visual {
		nominal := int64(index) * visualInterval
		for reference < len(sourceVideo) && sourceVideo[reference] < nominal {
			reference++
		}
		if reference == len(sourceVideo) || sample.Ticks != sourceVideo[reference] || sample.Ticks >= features.VisualWindowTicks {
			t.Fatalf("visual slot %d did not use its first actual source frame: nominal=%d actual=%d reference_index=%d", index, nominal, sample.Ticks, reference)
		}
	}
	if features.Visual[1].Ticks != 13*TicksPerSecond/videoRate || features.Visual[59].Ticks != 738*TicksPerSecond/videoRate {
		t.Fatalf("visual samples invented half-second PTS: second=%d last=%d", features.Visual[1].Ticks, features.Visual[59].Ticks)
	}
	strict, err := extractor.ExtractVisual(context.Background(), file, info, video.Index, VisualAnalysisOptions{EndTicks: info.DurationTicks})
	if !errors.Is(err, ErrAnalysisUnproven) || len(strict) != 0 {
		t.Fatalf("explicit end silently clipped or duplicated a missing source slot: end=%d last_source_pts=%d samples=%+v error=%v", info.DurationTicks, sourceVideo[len(sourceVideo)-1], strict, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	stopped, err := extractor.ExtractIntro(canceled, file, info, request)
	if !errors.Is(err, context.Canceled) || len(stopped.Audio) != 0 || len(stopped.Visual) != 0 || stopped.WindowTicks != 0 || stopped.VisualWindowTicks != 0 {
		t.Fatalf("canceled intro request returned evidence: %+v, %v", stopped, err)
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 17 {
		t.Fatalf("extraction changed or closed the borrowed descriptor: offset=%d error=%v", position, err)
	}
}
