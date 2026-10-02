package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func generatedAVEffectiveTestJSON() string {
	return `{"frames":[{"media_type":"video","stream_index":0,"pts":0,"duration":3750,"key_frame":1},` +
		`{"media_type":"audio","stream_index":1,"pts":0,"nb_samples":1024,"side_data_list":[{"side_data_type":"Skip Samples","skip_samples":1024,"discard_padding":512}]}],` +
		`"streams":[{"index":0,"codec_type":"video","codec_name":"h264","time_base":"1/90000"},` +
		`{"index":1,"codec_type":"audio","codec_name":"aac","time_base":"1/48000","sample_rate":"48000","channels":2,"sample_fmt":"fltp","channel_layout":"stereo"}]}`
}

func TestGeneratedAVEffectiveDiagnosticKeepsObservedSamplesAndTrimSeparate(t *testing.T) {
	got, err := ParseGeneratedAVEffectiveDecodeDiagnostic(context.Background(), []byte(generatedAVEffectiveTestJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Qualified || got.Complete || !got.FramesParsed || got.Audio.Samples != 1024 || got.Audio.End != (GeneratedRational{Num: 8, Den: 375}) || got.Video.End != (GeneratedRational{Num: 1, Den: 24}) {
		t.Fatalf("decoded samples were nominally trimmed or qualified: %+v", got)
	}
	if len(got.Frames) != 2 || !got.Frames[1].EndDerivedFromSamples || got.Frames[1].DTSKnown || len(got.Frames[1].SideData) != 1 {
		t.Fatal("missing native values were filled")
	}
	side := got.Frames[1].SideData[0]
	if !side.TrimKnown || side.SkipSamples != 1024 || side.DiscardPadding != 512 || side.SkipReasonKnown || side.DiscardReasonKnown {
		t.Fatal("trim provenance or absent reasons were lost")
	}
}

func TestGeneratedAVEffectiveDiagnosticRejectsMissingNativeOrUnknownFields(t *testing.T) {
	for _, data := range []string{
		strings.Replace(generatedAVEffectiveTestJSON(), `"pts":0,`, "", 1),
		strings.Replace(generatedAVEffectiveTestJSON(), `"pts":0,`, `"pts":0,"pts":1,`, 1),
		strings.Replace(generatedAVEffectiveTestJSON(), `"pts":0,`, `"best_effort_timestamp":0,`, 1),
		strings.Replace(generatedAVEffectiveTestJSON(), `"duration":3750`, `"duration":0`, 1),
		strings.Replace(generatedAVEffectiveTestJSON(), `"sample_rate":"48000"`, `"sample_rate":"44100"`, 1),
		strings.Replace(generatedAVEffectiveTestJSON(), `"Skip Samples"`, `"Unknown decoder trim"`, 1),
		generatedAVEffectiveTestJSON() + ` {}`,
	} {
		got, err := ParseGeneratedAVEffectiveDecodeDiagnostic(context.Background(), []byte(data))
		if err == nil || got.FramesParsed || got.Complete || len(got.Frames) != 0 {
			t.Fatal("invalid projection returned effective evidence")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseGeneratedAVEffectiveDecodeDiagnostic(ctx, []byte(generatedAVEffectiveTestJSON())); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was ignored")
	}
}

func TestGeneratedAVPCMStreamingChunkingAndWholeSampleCount(t *testing.T) {
	data := []byte{1, 0, 0xfe, 0xff, 0, 0, 3, 0}
	writer, err := newGeneratedAVPCMWriter(2, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range [][]byte{data[:1], data[1:4], data[4:7], data[7:]} {
		if _, err := writer.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	got, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	if got.Qualified || got.Complete || got.Samples != 2 || got.Bytes != 8 || got.SHA256 != sha256.Sum256(data) || len(got.Bins) != 1 || got.Bins[0].NonzeroValues != 3 || got.Bins[0].SumSquares != ([2]uint64{1, 13}) {
		t.Fatalf("lost streaming sample facts: %+v", got)
	}
	partial, _ := newGeneratedAVPCMWriter(2, 8)
	_, _ = partial.Write(data[:7])
	if _, err := partial.finish(); err == nil {
		t.Fatal("partial interleaved sample was accepted")
	}
	bounded, _ := newGeneratedAVPCMWriter(2, 8)
	cancelled := false
	bounded.cancel = func() { cancelled = true }
	if _, err := bounded.Write(append(data, 0)); !errors.Is(err, ErrTimelineLimit) || !cancelled {
		t.Fatal("PCM budget did not cancel")
	}
}
