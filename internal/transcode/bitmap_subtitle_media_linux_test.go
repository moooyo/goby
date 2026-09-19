//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// One source carries HDR and SDR video views, one independent chirp clock, and
// the same authored PGS events. It covers the changed software paths without
// multiplying the full codec/hardware matrix.
func TestBitmapSubtitleActualSoftwareHDRLadderAndSeekAV(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	source, info := bitmapSubtitleAVFixture(t, ctx, ffmpeg, ffprobe)
	reference := bitmapSubtitleAVReference(t, ctx, ffmpeg, ffprobe, source, info)
	if len(reference.facts.audio) == 0 || len(reference.pcm) != 216000*2 {
		t.Fatal("fixture does not contain its complete 4.5-second mono PCM clock")
	}
	base := Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac",
		VideoStreamIndex: 0, AudioStreamIndex: 2, Width: 320, Height: 192, FrameRate: 16, VideoBitrate: 768000,
		AudioBitrate: 96000, AudioChannels: 1, AudioSampleRate: 48000,
		DurationTicks: info.DurationTicks, StartTicks: 2 * ticksPerSecond,
		SourceFormatStartKnown: info.FormatStartKnown, SourceFormatStartTicks: info.FormatStartTicks,
		VideoFilters: VideoFilters{ToneMap: "hdr10", SourceBitDepth: 10, SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"},
		Subtitle:     SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 3, OffsetTicks: ticksPerSecond / 2}}

	t.Run("software HDR with bitmap", func(t *testing.T) {
		output := runProgressiveVideo(t, ctx, ffmpeg, source, base)
		_, pixels := assertBitmapSubtitleAVOutput(t, ctx, ffmpeg, ffprobe, output, base, reference, 320, 192, true)
		assertBitmapSubtitleSDROutput(t, ctx, ffprobe, output, 320, 192)
		control := base
		control.Subtitle, control.VideoFilters = SubtitlePlan{}, VideoFilters{}
		control.AudioStreamIndex, control.AudioCodec = -1, ""
		control.AudioBitrate, control.AudioChannels, control.AudioSampleRate = 0, 0, 0
		unmapped := runProgressiveVideo(t, ctx, ffmpeg, source, control)
		sourcePixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, unmapped)
		if len(sourcePixels) != len(pixels) {
			t.Fatal("HDR processing changed the selected picture count")
		}
		var difference float64
		for frame := 0; frame < 40; frame++ {
			// This background patch never intersects the caption. The white
			// caption assertion separately proves composition follows mapping.
			for y := 16; y < 48; y++ {
				for x := 16; x < 48; x++ {
					position := frame*320*192 + y*320 + x
					difference += math.Abs(float64(pixels[position]) - float64(sourcePixels[position]))
				}
			}
		}
		if difference/(40*32*32) < 3 {
			t.Fatal("HDR declarations changed without transforming uncaptained pixels")
		}
	})

	t.Run("generated fMP4 ladder with bitmap", func(t *testing.T) {
		plan := base
		plan.OutputMode, plan.SourceFormatStartKnown, plan.SourceFormatStartTicks = "", false, 0
		plan.SegmentSeconds = 1
		plan.HLS = HLSPlan{SegmentType: "fmp4", RenditionCount: 2,
			Renditions: [MaxHLSRenditions]HLSRendition{{Width: 320, Height: 192, VideoBitrate: 768000}, {Width: 160, Height: 96, VideoBitrate: 256000}}}
		input, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		directory := t.TempDir()
		result, err := Run(ctx, ffmpeg, directory, input, plan, 1, nil)
		if err != nil {
			t.Fatalf("software bitmap ladder failed: %v: %s", err, result.StderrTail)
		}
		var firstFacts progressiveVideoFacts
		var firstDurations []int64
		for index := 0; index < plan.HLS.RenditionCount; index++ {
			playlistData, err := os.ReadFile(filepath.Join(directory, HLSPlaylistName(index, plan.HLS.RenditionCount)))
			if err != nil {
				t.Fatal(err)
			}
			playlist, err := ParseMediaPlaylist(playlistData)
			if err != nil || !playlist.Ended || !playlist.Independent || playlist.InitName == "" || len(playlist.Segments) != 3 {
				t.Fatalf("bitmap rendition lacks its completed independent 2.5-second window: %+v, %v", playlist, err)
			}
			combined, err := os.ReadFile(filepath.Join(directory, playlist.InitName))
			if err != nil {
				t.Fatal(err)
			}
			for segmentIndex, segment := range playlist.Segments {
				fragment, err := os.ReadFile(filepath.Join(directory, segment.Name))
				if err != nil {
					t.Fatal(err)
				}
				combined = append(combined, fragment...)
				if index == 0 {
					firstDurations = append(firstDurations, segment.DurationTicks)
				} else if delta := segment.DurationTicks - firstDurations[segmentIndex]; delta < -100 || delta > 100 {
					t.Fatal("bitmap ladder segment boundaries diverged between renditions")
				}
			}
			output := filepath.Join(t.TempDir(), "rendition.mp4")
			if err := os.WriteFile(output, combined, 0600); err != nil {
				t.Fatal(err)
			}
			rendition := plan.HLS.Renditions[index]
			facts, _ := assertBitmapSubtitleAVOutput(t, ctx, ffmpeg, ffprobe, output, plan, reference, rendition.Width, rendition.Height, false)
			assertBitmapSubtitleSDROutput(t, ctx, ffprobe, output, rendition.Width, rendition.Height)
			if index == 0 {
				firstFacts = facts
			} else {
				for frame := range facts.video {
					if math.Abs(facts.video[frame].time(t)-firstFacts.video[frame].time(t)) > .00001 {
						t.Fatalf("caption rendition %d moved video frame %d on the shared timeline", index, frame)
					}
				}
			}
		}
	})

	t.Run("verified video seek with linear audio and bitmap", func(t *testing.T) {
		recorder := progressiveVideoSeekRecorder(t, ffmpeg)
		input, indexed, candidate := progressiveVideoSeekSource(t, ctx, ffprobe, recorder, source, base.StartTicks)
		if len(indexed.VideoSeekIndexes) != 1 || indexed.VideoSeekIndexes[0].StreamIndex != 1 {
			t.Fatal("the independently restartable SDR view was not indexed")
		}
		plan := base
		plan.VideoStreamIndex, plan.VideoFilters, plan.VideoSeekCandidate = 1, VideoFilters{}, candidate
		output := runRecordedProgressiveVideoSeek(t, ctx, recorder, ffmpeg, ffprobe, input, plan, 1)
		assertRecordedProgressiveVideoSeekProof(t, output)
		if strings.Count(strings.Join(output.args, " "), "-i /proc/self/fd/3") != 3 ||
			!hasArgumentPair(output.args, "-map", "2:2") || !strings.Contains(strings.Join(output.args, " "), "[1:3]settb=AVTB") {
			t.Fatalf("the real fast-seek producer did not retain separate video/subtitle/audio inputs: %v", output.args)
		}
		assertBitmapSubtitleAVOutput(t, ctx, ffmpeg, ffprobe, output.path, plan, reference, 320, 192, true)
	})
}

func bitmapSubtitleAVReference(t *testing.T, ctx context.Context, ffmpeg, ffprobe, source string, info media.Info) progressiveVideoReference {
	t.Helper()
	reference := progressiveVideoReference{info: info, pcm: decodeProgressivePCM(t, ctx, ffmpeg, source, 16)}
	// Probe each selected reference stream explicitly. PGS display events do
	// not have the ordinary decoded-frame timestamp contract, and the other
	// video view must not be interleaved into the SDR reference frame list.
	for _, selection := range []struct {
		selector, kind, codec string
		index                 int
	}{
		{"v:1", "video", "h264", 1},
		{"a:0", "audio", "pcm_s16le", 2},
	} {
		data := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-select_streams", selection.selector,
			"-show_frames", "-show_streams", "-show_entries",
			"frame=media_type,best_effort_timestamp_time,duration_time,pkt_duration_time,pict_type,nb_samples:stream=index,codec_type,codec_name",
			"-of", "json", source)
		var document struct {
			Frames  []progressiveVideoFrame `json:"frames"`
			Streams []struct {
				Index int    `json:"index"`
				Type  string `json:"codec_type"`
				Codec string `json:"codec_name"`
			} `json:"streams"`
		}
		if err := json.Unmarshal(data, &document); err != nil || len(document.Streams) != 1 || len(document.Frames) == 0 ||
			document.Streams[0].Index != selection.index || document.Streams[0].Type != selection.kind || document.Streams[0].Codec != selection.codec {
			t.Fatalf("reference probe did not select the intended %s stream: %s, %v", selection.selector, data, err)
		}
		for _, frame := range document.Frames {
			if frame.Type != selection.kind {
				t.Fatalf("reference probe included a different media type: wanted=%s got=%s", selection.kind, frame.Type)
			}
			_ = frame.time(t)
			if selection.kind == "video" {
				reference.facts.video = append(reference.facts.video, frame)
			} else {
				if frame.Samples <= 0 {
					t.Fatal("source audio reference frame has no decoded samples")
				}
				reference.facts.audio = append(reference.facts.audio, frame)
			}
		}
	}
	return reference
}

func bitmapSubtitleAVFixture(t *testing.T, ctx context.Context, ffmpeg, ffprobe string) (string, media.Info) {
	t.Helper()
	directory := t.TempDir()
	subtitle := filepath.Join(directory, "rectangle.sup")
	if err := os.WriteFile(subtitle, bitmapSubtitlePGSFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "views.mkv")
	// The authored neutral levels have different PQ and SDR interpretations.
	// Two codec views permit HDR processing and real H.264 restart proof to
	// share the exact same source audio and PGS streams.
	video := "nullsrc=size=320x192:rate=16:duration=4.5,format=yuv420p10le,geq=lum='256+16*mod(N,2)':cb=512:cr=512,setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc"
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1", "-filter_complex_threads", "1",
		"-f", "lavfi", "-i", video, "-f", "lavfi", "-i", "aevalsrc=0.6*sin(2*PI*(300*t+90*t*t)):s=48000:d=4.5", "-f", "sup", "-i", subtitle,
		"-map", "0:v:0", "-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0",
		"-c:v:0", "ffv1", "-threads:v:0", "1", "-pix_fmt:v:0", "yuv420p10le",
		"-c:v:1", "libx264", "-threads:v:1", "1", "-preset:v:1", "veryfast", "-g:v:1", "16", "-keyint_min:v:1", "16", "-sc_threshold:v:1", "0", "-bf:v:1", "2",
		"-filter:v:1", "format=yuv420p,setparams=range=limited:color_primaries=bt709:color_trc=bt709:colorspace=bt709",
		"-c:a", "pcm_s16le", "-threads:a", "1", "-ac", "1", "-ar", "48000", "-c:s", "copy", "-t", "4.5", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}).Probe(ctx, source)
	durationDelta := info.DurationTicks - 45*ticksPerSecond/10
	if err != nil || !info.FormatStartKnown || len(info.Streams) != 4 || durationDelta < -ticksPerSecond/1000 || durationDelta > ticksPerSecond/1000 ||
		info.Streams[0].Codec != "ffv1" || info.Streams[0].PixelFormat != "yuv420p10le" || info.Streams[0].ColorTransfer != "smpte2084" ||
		info.Streams[1].Codec != "h264" || info.Streams[1].PixelFormat != "yuv420p" || info.Streams[1].ColorTransfer != "bt709" ||
		info.Streams[2].CodecType != "audio" || info.Streams[2].Channels != 1 || info.Streams[2].SampleRate != 48000 || info.Streams[3].Codec != "hdmv_pgs_subtitle" {
		t.Fatalf("shared PGS/HDR/SDR/audio fixture facts are not established: %+v, %v", info, err)
	}
	return source, info
}

func assertBitmapSubtitleSDROutput(t *testing.T, ctx context.Context, ffprobe, path string, width, height int) {
	t.Helper()
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, path)
	if err != nil || len(info.Streams) != 2 || info.Streams[0].Codec != "h264" || info.Streams[0].Width != width || info.Streams[0].Height != height ||
		info.Streams[0].PixelFormat != "yuv420p" || info.Streams[0].ColorTransfer != "bt709" || info.Streams[0].ColorPrimaries != "bt709" || info.Streams[0].ColorSpace != "bt709" {
		t.Fatalf("software HDR/bitmap output does not describe its actual SDR pixels: %+v, %v", info, err)
	}
}

func assertBitmapSubtitleAVOutput(t *testing.T, ctx context.Context, ffmpeg, ffprobe, path string, plan Plan, reference progressiveVideoReference, width, height int, progressive bool) (progressiveVideoFacts, []byte) {
	t.Helper()
	strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
	facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
	assertProgressiveVideoStreams(t, facts, true)
	pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, path)
	if len(facts.video) != 40 || len(pixels) != 40*width*height {
		t.Fatalf("bitmap/audio processing changed the selected video window: frames=%d bytes=%d", len(facts.video), len(pixels))
	}
	origin := facts.video[0].time(t)
	if progressive && math.Abs(origin) > .002 {
		t.Fatalf("progressive bitmap/audio output lost its trim origin: %.6f", origin)
	}
	left, top, rectWidth, rectHeight := 112*width/320, 144*height/192, 96*width/320, 24*height/192
	for index, frame := range facts.video {
		if math.Abs(frame.time(t)-origin-float64(index)/16) > .002 {
			t.Fatalf("bitmap/audio output moved video frame %d: %.6f", index, frame.time(t))
		}
		inside, outside := 0, 0
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if pixels[index*width*height+y*width+x] <= 180 {
					continue
				}
				if x >= left && x < left+rectWidth && y >= top && y < top+rectHeight {
					inside++
				} else if x < left-3 || x >= left+rectWidth+3 || y < top-3 || y >= top+rectHeight+3 {
					outside++
				}
			}
		}
		visible := index >= 16 && index < 32
		if visible && inside < rectWidth*rectHeight*85/100 || !visible && inside > 4 || outside > 4 {
			t.Fatalf("bitmap placement/clear after audio seek is wrong at frame %d: visible=%v inside=%d outside=%d", index, visible, inside, outside)
		}
	}
	lastVideo, lastAudio := facts.video[len(facts.video)-1], facts.audio[len(facts.audio)-1]
	videoEnd := lastVideo.time(t) - origin + lastVideo.duration(t)
	audioFirst := facts.audio[0].time(t) - origin
	audioEnd := lastAudio.time(t) - origin + float64(lastAudio.Samples)/48000
	wantEnd := float64(plan.DurationTicks-plan.StartTicks) / float64(ticksPerSecond)
	const audioPadding = 1024.0/48000 + .002
	if math.Abs(videoEnd-wantEnd) > .002 || math.Abs(audioFirst) > audioPadding || math.Abs(audioEnd-videoEnd) > audioPadding {
		t.Fatalf("bitmap conversion has unequal audio/video boundaries: video=[0,%.6f] audio=[%.6f,%.6f] wanted_end=%.6f", videoEnd, audioFirst, audioEnd, wantEnd)
	}
	// HLS may apply a common mux timestamp shift. Remove only that observed
	// video origin from the comparison copy, never from the encoded output.
	normalized := facts
	normalized.audio = append([]progressiveVideoFrame(nil), facts.audio...)
	for index := range normalized.audio {
		normalized.audio[index].Timestamp = strconv.FormatFloat(facts.audio[index].time(t)-origin, 'f', 9, 64)
	}
	assertProgressiveChirpWindow(t, reference, plan, normalized, decodeProgressivePCM(t, ctx, ffmpeg, path, 16))
	return facts, pixels
}
