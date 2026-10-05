package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

func waveformTestPCM(frames int, values ...float32) []byte {
	result := make([]byte, frames*len(values)*4)
	for index := range frames {
		for channel, value := range values {
			binary.LittleEndian.PutUint32(result[(index*len(values)+channel)*4:], math.Float32bits(value))
		}
	}
	return result
}

func waveformTestData(t *testing.T) AudioWaveformData {
	t.Helper()
	stream := Stream{Index: 3, CodecType: "audio", Channels: 2, ChannelLayout: "stereo", SampleRate: 4096, TimeBase: "1/4096"}
	accumulator := newAudioWaveformAccumulator(TicksPerSecond, 0, stream)
	if err := accumulator.add(0, waveformTestPCM(4096, 0.5, -0.5)); err != nil {
		t.Fatal(err)
	}
	track, err := accumulator.finish()
	if err != nil {
		t.Fatal(err)
	}
	return AudioWaveformData{Profile: AudioWaveformProfile, FFmpegSHA256: strings.Repeat("ab", 32), DurationTicks: TicksPerSecond, Tracks: []AudioWaveformTrack{track}}
}

func TestAudioWaveformPhaseInversionDoesNotCancelAndLevelsUseEnergy(t *testing.T) {
	data := waveformTestData(t)
	for _, level := range data.Tracks[0].Levels {
		for index := range level.BucketCount {
			if level.Peaks[index] != 32768 || level.RMS[index] != 32768 || level.Validity[index/8]&(1<<uint(index%8)) == 0 {
				t.Fatalf("opposite channels cancelled at %d/%d", level.BucketCount, index)
			}
		}
	}
	stream := Stream{Index: 0, Channels: 1, SampleRate: 4096, TimeBase: "1/4096"}
	a := newAudioWaveformAccumulator(TicksPerSecond, 0, stream)
	if err := a.add(0, waveformTestPCM(1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := a.add(1, waveformTestPCM(4095, 0)); err != nil {
		t.Fatal(err)
	}
	track, err := a.finish()
	if err != nil {
		t.Fatal(err)
	}
	if track.Levels[0].Peaks[0] != 65535 || track.Levels[0].RMS[0] != audioWaveformQuantize(math.Sqrt(1.0/8)) {
		t.Fatal("coarse RMS averaged amplitudes instead of sample energy")
	}
}

func TestAudioWaveformDelayGapSilenceAndTailHaveDistinctCoverage(t *testing.T) {
	stream := Stream{Index: 1, Channels: 1, SampleRate: 4096, TimeBase: "1/4096"}
	a := newAudioWaveformAccumulator(TicksPerSecond, 2*TicksPerSecond, stream)
	// A negative item-relative sample is decoder preroll. The visible silent
	// interval starts one quarter into the item; an actual gap remains empty.
	if err := a.add(8190, waveformTestPCM(1, .75)); err != nil {
		t.Fatal(err)
	}
	if err := a.add(9216, waveformTestPCM(1024, 0)); err != nil {
		t.Fatal(err)
	}
	if err := a.add(11264, waveformTestPCM(1030, .25)); err != nil {
		t.Fatal(err)
	}
	track, err := a.finish()
	if err != nil {
		t.Fatal(err)
	}
	if track.SampleCount != 2048 || track.CoverageStartTicks != TicksPerSecond/4 || track.CoverageEndTicks != TicksPerSecond {
		t.Fatalf("coverage %+v", track.AudioWaveformTrackSummary)
	}
	fine := track.Levels[3]
	for index := range 4096 {
		valid := fine.Validity[index/8]&(1<<uint(index%8)) != 0
		want := index >= 1024 && index < 2048 || index >= 3072
		if valid != want {
			t.Fatalf("bucket %d validity=%v want %v", index, valid, want)
		}
		if index >= 1024 && index < 2048 && fine.Peaks[index] != 0 {
			t.Fatal("silence was invented")
		}
	}
}

func TestAudioWaveformRejectsOverlapNonfiniteAndDoesNotResample(t *testing.T) {
	for _, value := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		a := newAudioWaveformAccumulator(TicksPerSecond, 0, Stream{Channels: 1, SampleRate: 48000, TimeBase: "1/48000"})
		if err := a.add(0, waveformTestPCM(1, value)); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatal("non-finite PCM accepted")
		}
	}
	a := newAudioWaveformAccumulator(TicksPerSecond, 0, Stream{Channels: 1, SampleRate: 48000, TimeBase: "1/1000"})
	if err := a.add(0, waveformTestPCM(1024, .25)); err != nil {
		t.Fatal(err)
	}
	if err := a.add(1008, waveformTestPCM(1024, .25)); err != nil {
		t.Fatalf("declared millisecond quantization rejected: %v", err)
	}
	if a.previousEnd != 2048 {
		t.Fatal("quantization changed the sample count")
	}
	if err := a.add(1800, waveformTestPCM(10, .25)); !errors.Is(err, ErrAnalysisUnproven) {
		t.Fatal("large overlap accepted")
	}
	args := strings.Join(audioWaveformArgs(Stream{Index: 7, Channels: 6, SampleRate: 48000}), " ")
	for _, forbidden := range []string{"-ac ", "-ar ", "asetnsamples", "-ss ", "aresample="} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("waveform path loses source samples or gaps: %s", forbidden)
		}
	}
	if !strings.Contains(args, "-map 0:7 -vn -sn -dn") || !strings.Contains(args, "ashowinfo@audio_waveform") {
		t.Fatal("original stream or timestamp mapping missing")
	}
}

func TestAudioWaveformArtifactStrictRoundTrip(t *testing.T) {
	data := waveformTestData(t)
	encoded, err := MarshalAudioWaveforms(data)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseAudioWaveforms(encoded)
	if err != nil || !reflect.DeepEqual(data, parsed) {
		t.Fatalf("round trip: %v", err)
	}
	if summary := parsed.Summary(int64(len(encoded))); summary.Bytes != int64(len(encoded)) || summary.Tracks[0] != data.Tracks[0].AudioWaveformTrackSummary {
		t.Fatal("summary mismatch")
	}
	for _, bad := range [][]byte{encoded[:51], encoded[:len(encoded)-1], append(append([]byte{}, encoded...), 0), bytes.Repeat([]byte{0}, int(MaxAudioWaveformBytes)+1)} {
		if _, err := ParseAudioWaveforms(bad); err == nil {
			t.Fatal("malformed artifact accepted")
		}
	}
	for _, mutate := range []func(*AudioWaveformData){
		func(v *AudioWaveformData) { v.Tracks[0].Levels[0].Peaks[0]-- },
		func(v *AudioWaveformData) { v.Tracks[0].Levels[3].Validity[0] = 0 },
		func(v *AudioWaveformData) { v.Tracks[0].Levels[3].RMS[0] = 65535 },
		func(v *AudioWaveformData) { v.Tracks[0].ChannelLayout = "stereo\n" },
		func(v *AudioWaveformData) { v.Tracks[0].SampleCount = -1 },
		func(v *AudioWaveformData) { v.Tracks = append(v.Tracks, v.Tracks[0]) },
	} {
		copyValue := waveformTestData(t)
		mutate(&copyValue)
		if _, err := MarshalAudioWaveforms(copyValue); err == nil {
			t.Fatal("inconsistent artifact accepted")
		}
	}
}

func TestAudioWaveformAllOriginalStreamsRemainSelected(t *testing.T) {
	valid := Stream{Index: 7, CodecType: "audio", Channels: 2, SampleRate: 48000, TimeBase: "1/1000", ChannelLayout: "stereo"}
	other := valid
	other.Index = 2
	streams, err := audioWaveformStreams(Info{Streams: []Stream{valid, {Index: 0, CodecType: "video"}, other}})
	if err != nil || len(streams) != 2 || streams[0].Index != 2 || streams[1].Index != 7 {
		t.Fatalf("selection %+v %v", streams, err)
	}
	for _, mutate := range []func(*Stream){func(s *Stream) { s.IsExternal = true }, func(s *Stream) { s.Channels = 65 }, func(s *Stream) { s.TimeBase = "" }, func(s *Stream) { s.SampleRate = 0 }} {
		bad := valid
		mutate(&bad)
		if _, err := audioWaveformStreams(Info{Streams: []Stream{other, bad}}); !errors.Is(err, ErrAudioWaveformUnsupported) {
			t.Fatal("unsupported track silently dropped")
		}
	}
}

func TestAudioWaveformLogIsBoundedAndReleasesMissingMetadata(t *testing.T) {
	stream := Stream{Index: 1, CodecType: "audio", Channels: 2, SampleRate: 48000, TimeBase: "1/1000"}
	log, err := newAudioWaveformLog(stream, TicksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	pcm := waveformTestPCM(2, .25, -.25)
	line := analysisTestAudioPackets + fmt.Sprintf("[ashowinfo@audio_waveform @ x] [info] n:0 pts:48000 pts_time:1 fmt:flt channels:2 chlayout:stereo rate:48000 nb_samples:2 checksum:%08X plane_checksums: [ 00000000 ]\n", analysisPCMChecksum(pcm))
	for _, value := range []byte(line) {
		if _, err := log.Write([]byte{value}); err != nil {
			t.Fatal(err)
		}
	}
	log.Close(nil)
	frame, err := log.next(context.Background())
	if err != nil || frame.samples != 2 || frame.checksum != analysisPCMChecksum(pcm) {
		t.Fatalf("frame %+v %v", frame, err)
	}
	if _, err := log.next(context.Background()); err != io.EOF {
		t.Fatalf("EOF %v", err)
	}
	if err := log.result(); err != nil {
		t.Fatal(err)
	}
	bad, _ := newAudioWaveformLog(stream, TicksPerSecond)
	bad.Close(context.Canceled)
	if _, err := bad.next(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("closed waiter blocked or ignored cancellation")
	}
	bad, _ = newAudioWaveformLog(stream, TicksPerSecond)
	if _, err := bad.Write(bytes.Repeat([]byte{'x'}, (16<<10)+1)); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatal("unbounded log line")
	}
}
