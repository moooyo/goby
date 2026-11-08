//go:build linux

package transcode

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestExecutionDownMixBoostActualPCMRetainsExactSampleWindow(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const sampleCount = 60000
	source := writeDownMixPCMFixture(t, sampleCount)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || len(info.Streams) != 1 || info.Streams[0].Channels != 6 || info.DurationTicks != sampleCount*ticksPerSecond/48000 {
		t.Fatalf("fixture lacks the actual surround facts: %+v: %v", info, err)
	}
	for _, start := range []int64{0, 12345} {
		t.Run("start="+tickSeconds(start), func(t *testing.T) {
			plan := progressivePlan("wav", "pcm_s16le")
			plan.DurationTicks, plan.StartTicks = info.DurationTicks, start
			plan.AudioSourceSampleCount, plan.AudioSampleSeek = sampleCount, true
			plan.SourceAudioChannels = info.Streams[0].Channels
			outputs := make([][]byte, 2)
			for index, boost := range []float64{1, 2} {
				options := DefaultExecutionOptions(1)
				options.DownMixAudioBoost = boost
				captured, err := CaptureExecution(plan, options)
				if err != nil {
					t.Fatal(err)
				}
				input, err := os.Open(source)
				if err != nil {
					t.Fatal(err)
				}
				directory := t.TempDir()
				result, runErr := Run(ctx, ffmpeg, directory, input, captured, 64, nil)
				input.Close()
				if runErr != nil {
					t.Fatalf("downmix encoding failed: %v: %s", runErr, result.StderrTail)
				}
				outputs[index] = decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(directory, "stream.bin"), 16)
			}
			wantSamples, err := ProgressiveOutputSamples(plan, plan.AudioSampleRate)
			if err != nil || len(outputs[0]) != int(wantSamples)*4 || len(outputs[1]) != len(outputs[0]) {
				t.Fatalf("gain changed the exact source sample window: bytes=%d/%d, samples=%d: %v", len(outputs[0]), len(outputs[1]), wantSamples, err)
			}
			var baselineEnergy, boostedEnergy float64
			for offset := 0; offset < len(outputs[0]); offset += 2 {
				baseline := int16(binary.LittleEndian.Uint16(outputs[0][offset:]))
				boosted := int16(binary.LittleEndian.Uint16(outputs[1][offset:]))
				if math.Abs(float64(boosted)-2*float64(baseline)) > 2 {
					t.Fatalf("boost changed the source sample alignment at %d: %d -> %d", offset/4, baseline, boosted)
				}
				baselineEnergy += float64(baseline) * float64(baseline)
				boostedEnergy += float64(boosted) * float64(boosted)
			}
			ratio := math.Sqrt(boostedEnergy / baselineEnergy)
			if baselineEnergy == 0 || math.Abs(ratio-2) > .002 {
				t.Fatalf("saved boost did not change actual stereo amplitude: RMS ratio %.6f", ratio)
			}
			t.Logf("actual 5.1-to-stereo output: %d samples, gain ratio %.6f, start %d ticks", wantSamples, ratio, start)
		})
	}
}

func TestExecutionDownMixBoostActualAACAndMP3PreserveAudioClock(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const sampleCount = 60000
	source := writeDownMixPCMFixture(t, sampleCount)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || len(info.Streams) != 1 || info.Streams[0].Channels != 6 {
		t.Fatalf("fixture lacks actual surround channels: %+v: %v", info, err)
	}
	for _, codec := range []string{"aac", "mp3"} {
		t.Run(codec, func(t *testing.T) {
			plan := progressivePlan(codec, codec)
			plan.DurationTicks, plan.StartTicks = info.DurationTicks, 12345
			plan.AudioSourceSampleCount, plan.AudioSampleSeek = sampleCount, true
			plan.SourceAudioChannels = info.Streams[0].Channels
			plan.AudioBitrate = 192000
			outputs := make([][]byte, 2)
			frames := make([][]progressiveVideoFrame, 2)
			for index, boost := range []float64{1, 2} {
				options := DefaultExecutionOptions(1)
				options.DownMixAudioBoost = boost
				captured, err := CaptureExecution(plan, options)
				if err != nil {
					t.Fatal(err)
				}
				input, err := os.Open(source)
				if err != nil {
					t.Fatal(err)
				}
				directory := t.TempDir()
				result, runErr := Run(ctx, ffmpeg, directory, input, captured, 64, nil)
				input.Close()
				if runErr != nil {
					t.Fatalf("downmix %s encoding failed: %v: %s", codec, runErr, result.StderrTail)
				}
				path := filepath.Join(directory, "stream.bin")
				outputs[index] = decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
				frames[index] = probeProgressiveVideoFrames(t, ctx, ffprobe, path).audio
			}
			if len(outputs[0]) != len(outputs[1]) || len(frames[0]) == 0 || len(frames[0]) != len(frames[1]) {
				t.Fatalf("gain changed encoded audio frame or sample counts: bytes=%d/%d, frames=%d/%d", len(outputs[0]), len(outputs[1]), len(frames[0]), len(frames[1]))
			}
			for index, frame := range frames[0] {
				boosted := frames[1][index]
				if frame.Samples != boosted.Samples || math.Abs(frame.time(t)-boosted.time(t)) > .000001 || math.Abs(frame.duration(t)-boosted.duration(t)) > .000001 {
					t.Fatalf("gain changed %s frame %d presentation clock", codec, index)
				}
			}
			// Exclude priming and final padding; lossy quantization need not
			// produce exact doubled integers, but the source phase must agree.
			var baselineEnergy, boostedEnergy, dot float64
			for offset := 2048 * 4; offset < len(outputs[0])-2048*4; offset += 2 {
				baseline := float64(int16(binary.LittleEndian.Uint16(outputs[0][offset:])))
				boosted := float64(int16(binary.LittleEndian.Uint16(outputs[1][offset:])))
				baselineEnergy += baseline * baseline
				boostedEnergy += boosted * boosted
				dot += baseline * boosted
			}
			ratio := math.Sqrt(boostedEnergy / baselineEnergy)
			correlation := dot / math.Sqrt(baselineEnergy*boostedEnergy)
			if baselineEnergy == 0 || math.Abs(ratio-2) > .04 || correlation < .995 {
				t.Fatalf("gain changed rematrix normalization or source phase: RMS ratio %.6f, correlation %.6f", ratio, correlation)
			}
			t.Logf("actual 5.1-to-stereo %s output: %d frames, gain ratio %.6f, correlation %.6f", codec, len(frames[0]), ratio, correlation)
		})
	}
}

func TestExecutionDownMixBoostActualPCMClipsWithoutIntegerOverflow(t *testing.T) {
	ffmpeg, _ := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const sampleCount = 4800
	source := writeDownMixPCMFixture(t, sampleCount)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	// A legal high gain must saturate a positive surround signal, rather than
	// wrap a fixed-point mixing accumulator to a negative value before clipping.
	for offset := 68; offset < len(data); offset += 2 {
		binary.LittleEndian.PutUint16(data[offset:], 20000)
	}
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	plan := progressivePlan("wav", "pcm_s16le")
	plan.DurationTicks, plan.AudioSourceSampleCount = ticksPerSecond/10, sampleCount
	plan.SourceAudioChannels = 6
	options := DefaultExecutionOptions(1)
	options.DownMixAudioBoost = 10
	captured, err := CaptureExecution(plan, options)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	directory := t.TempDir()
	result, err := Run(ctx, ffmpeg, directory, input, captured, 64, nil)
	if err != nil {
		t.Fatalf("high-gain downmix failed: %v: %s", err, result.StderrTail)
	}
	pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(directory, "stream.bin"), 16)
	if len(pcm) != sampleCount*4 {
		t.Fatalf("high gain changed output samples: %d bytes", len(pcm))
	}
	for offset := 0; offset < len(pcm); offset += 2 {
		if value := int16(binary.LittleEndian.Uint16(pcm[offset:])); value != math.MaxInt16 {
			t.Fatalf("positive high gain overflowed before saturation at sample %d: %d", offset/4, value)
		}
	}
	t.Logf("actual 5.1-to-stereo gain 10 saturated %d positive samples without wraparound", sampleCount)
}

func writeDownMixPCMFixture(t *testing.T, samples int) string {
	t.Helper()
	const channels = 6
	const headerBytes = 68
	data := make([]byte, headerBytes+samples*channels*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 40)
	binary.LittleEndian.PutUint16(data[20:], 0xfffe)
	binary.LittleEndian.PutUint16(data[22:], channels)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], 48000*channels*2)
	binary.LittleEndian.PutUint16(data[32:], channels*2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	binary.LittleEndian.PutUint16(data[36:], 22)
	binary.LittleEndian.PutUint16(data[38:], 16)
	binary.LittleEndian.PutUint32(data[40:], 0x3f)
	copy(data[44:], []byte{1, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71})
	copy(data[60:], "data")
	binary.LittleEndian.PutUint32(data[64:], uint32(len(data)-headerBytes))
	for sample := 0; sample < samples; sample++ {
		for channel := 0; channel < channels; channel++ {
			frequency := float64(300 + channel*79)
			amplitude := 700 * math.Sin(2*math.Pi*frequency*float64(sample)/48000)
			binary.LittleEndian.PutUint16(data[headerBytes+(sample*channels+channel)*2:], uint16(int16(amplitude)))
		}
	}
	path := filepath.Join(t.TempDir(), "surround.wav")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
