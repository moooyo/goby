package transcode

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

const generatedBoundsVideoStream = `{"index":0,"codec_name":"h264","codec_type":"video","time_base":"1/90000"}`
const generatedBoundsAudioStream = `{"index":1,"codec_name":"aac","codec_type":"audio","sample_rate":"48000","time_base":"1/48000"}`

func generatedBoundsPacketJSON(stream, pts, dts, duration int64, flags string) string {
	return fmt.Sprintf(`{"stream_index":%d,"pts":%d,"dts":%d,"duration":%d,"flags":%q}`, stream, pts, dts, duration, flags)
}

func generatedBoundsDocument(packets, streams string) string {
	return `{"packets":[` + packets + `],"programs":[],"stream_groups":[],"streams":[` + streams + `]}`
}

func generatedBoundsParse(t *testing.T, document string, video bool, chunk int) (GeneratedSegmentBounds, error) {
	t.Helper()
	budget := &generatedBoundsBudget{cancel: func() {}}
	output := &generatedBoundsOutput{budget: budget}
	for offset := 0; offset < len(document); {
		end := min(offset+chunk, len(document))
		if _, err := output.Write([]byte(document[offset:end])); err != nil {
			return GeneratedSegmentBounds{}, err
		}
		offset = end
	}
	return output.finish(video)
}

func TestGeneratedSegmentBoundsSupportsIntegralBFrameReordering(t *testing.T) {
	packets := []string{
		generatedBoundsPacketJSON(0, 0, -2, 1, "K__"),
		generatedBoundsPacketJSON(0, 2, -1, 1, "___"),
		generatedBoundsPacketJSON(0, 1, 0, 1, "___"),
	}
	for _, chunk := range []int{1, 7, 4096} {
		got, err := generatedBoundsParse(t, generatedBoundsDocument(strings.Join(packets, ","), generatedBoundsVideoStream), true, chunk)
		if err != nil {
			t.Fatalf("chunk %d: %v", chunk, err)
		}
		if got.Video.FirstPTS != 0 || got.Video.LastPTS != 2 || got.Video.EndPTS != 3 || got.Video.FirstDTS != -2 ||
			got.Video.LastDTS != 0 || got.Video.PacketCount != 3 || !got.Video.FirstKey || got.Video.MaxPresentationGapTicks != 0 ||
			got.Video.LastPacketPTS != 1 || got.Video.TimeBase != (GeneratedRational{Num: 1, Den: 90000}) {
			t.Fatalf("unexpected reordered bounds: %+v", got.Video)
		}
	}
}

func TestGeneratedSegmentBoundsRetainsBothTracksAndAudioTrimMetadata(t *testing.T) {
	video := generatedBoundsPacketJSON(0, 3000, 3000, 3000, "K__")
	audioFirst := `{"stream_index":1,"pts":-1024,"dts":-1024,"duration":1024,"flags":"KD_","side_data_list":[{"side_data_type":"Skip Samples","skip_samples":1024,"discard_padding":0}]}`
	audioLast := `{"stream_index":1,"pts":0,"dts":0,"duration":1024,"flags":"K__","side_data_list":[{"side_data_type":"Skip Samples","skip_samples":0,"discard_padding":32}]}`
	got, err := generatedBoundsParse(t, generatedBoundsDocument(video+","+audioFirst+","+audioLast, generatedBoundsVideoStream+","+generatedBoundsAudioStream), true, 13)
	if err != nil || !got.Video.Present || !got.Audio.Present || got.Audio.SkipSamples != 1024 || got.Audio.DiscardPadding != 32 ||
		!got.Audio.HasDiscard || got.Audio.FirstPTS != -1024 || got.Audio.EndPTS != 1024 || got.Audio.PacketCount != 2 {
		t.Fatalf("track and trim evidence = %+v, %v", got, err)
	}
}

func TestGeneratedSegmentBoundsObservesPacketsBeyondOldClockProbeLimit(t *testing.T) {
	var packets strings.Builder
	for index := int64(0); index < 9000; index++ {
		if index != 0 {
			packets.WriteByte(',')
		}
		flags := "___"
		if index == 0 {
			flags = "K__"
		}
		packets.WriteString(generatedBoundsPacketJSON(0, index*2, index*2, 2, flags))
	}
	got, err := generatedBoundsParse(t, generatedBoundsDocument(packets.String(), generatedBoundsVideoStream), true, 8191)
	if err != nil || got.Video.PacketCount != 9000 || got.Video.EndPTS != 18000 {
		t.Fatalf("complete packet count = %+v, %v", got.Video, err)
	}
}

func TestGeneratedSegmentBoundsRejectsIncompleteOrAmbiguousEvidence(t *testing.T) {
	validPacket := generatedBoundsPacketJSON(0, 0, 0, 2, "K__")
	valid := generatedBoundsDocument(validPacket, generatedBoundsVideoStream)
	for name, document := range map[string]string{
		"empty":                  "",
		"truncated root":         strings.TrimSuffix(valid, "}"),
		"truncated packet":       `{"packets":[{"pts":0`,
		"trailing root":          valid + `{}`,
		"trailing bytes":         valid + `garbage`,
		"trailing comma":         strings.Replace(valid, `],"programs"`, `,],"programs"`, 1),
		"duplicate root section": strings.Replace(valid, `"programs":[]`, `"packets":[]`, 1),
		"duplicate clock":        strings.Replace(valid, `"pts":0`, `"pts":0,"pts":100`, 1),
		"case variant clock":     strings.Replace(valid, `"pts":0`, `"pts":0,"PTS":100`, 1),
		"case variant stream":    strings.Replace(valid, `"index":0`, `"index":0,"Index":1`, 1),
		"missing duration":       strings.Replace(valid, `,"duration":2`, "", 1),
		"unavailable duration":   strings.Replace(valid, `"duration":2`, `"duration":"N/A"`, 1),
		"float integer":          strings.Replace(valid, `"pts":0`, `"pts":0.0`, 1),
		"exponent integer":       strings.Replace(valid, `"pts":0`, `"pts":0e0`, 1),
		"overflow end":           strings.Replace(valid, `"pts":0`, fmt.Sprintf(`"pts":%d`, math.MaxInt64), 1),
		"missing keyframe":       strings.Replace(valid, `"K__"`, `"___"`, 1),
		"corrupt packet":         strings.Replace(valid, `"K__"`, `"KC_"`, 1),
		"video discard":          strings.Replace(valid, `"K__"`, `"KD_"`, 1),
		"unsupported flags":      strings.Replace(valid, `"K__"`, `"KS_"`, 1),
		"missing streams":        strings.Replace(valid, `"streams"`, `"unsupported"`, 1),
		"duplicate stream":       generatedBoundsDocument(validPacket, generatedBoundsVideoStream+","+generatedBoundsVideoStream),
		"invalid timebase":       strings.Replace(valid, `1/90000`, `1/0`, 1),
		"invalid UTF8":           strings.Replace(valid, `h264`, "h\xff264", 1),
		"invalid surrogate":      strings.Replace(valid, `h264`, `h\ud800264`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := generatedBoundsParse(t, document, true, 3); !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("ambiguous evidence accepted: %v", err)
			}
		})
	}
}

func TestGeneratedSegmentBoundsRejectsDecodeGapsAndPresentationHoles(t *testing.T) {
	first := generatedBoundsPacketJSON(0, 0, 0, 2, "K__")
	for name, second := range map[string]string{
		"decode regression":    generatedBoundsPacketJSON(0, 2, 0, 2, "___"),
		"decode overlap":       generatedBoundsPacketJSON(0, 2, 1, 2, "___"),
		"decode hole":          generatedBoundsPacketJSON(0, 2, 4, 2, "___"),
		"presentation overlap": generatedBoundsPacketJSON(0, 1, 2, 2, "___"),
		"presentation hole":    generatedBoundsPacketJSON(0, 100, 2, 2, "___"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := generatedBoundsParse(t, generatedBoundsDocument(first+","+second, generatedBoundsVideoStream), true, 7); !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("invalid packet coverage: %v", err)
			}
		})
	}
	unitFirst := generatedBoundsPacketJSON(0, 0, 0, 1, "K__")
	unitGap := generatedBoundsPacketJSON(0, 2, 1, 1, "___")
	if _, err := generatedBoundsParse(t, generatedBoundsDocument(unitFirst+","+unitGap, generatedBoundsVideoStream), true, 7); !errors.Is(err, ErrTimelineProbe) {
		t.Fatalf("one-unit frame gap accepted: %v", err)
	}
}

func TestGeneratedSegmentBoundsReportsCumulativeClockRounding(t *testing.T) {
	packets := strings.Join([]string{
		generatedBoundsPacketJSON(0, 0, 0, 2, "K__"),
		generatedBoundsPacketJSON(0, 3, 3, 2, "___"),
		generatedBoundsPacketJSON(0, 6, 6, 2, "___"),
	}, ",")
	got, err := generatedBoundsParse(t, generatedBoundsDocument(packets, generatedBoundsVideoStream), true, 1)
	if err != nil || got.Video.MaxPresentationGapTicks != 1 || got.Video.TotalPresentationGapTicks != 2 ||
		got.Video.MaxDecodeGapTicks != 1 || got.Video.TotalDecodeGapTicks != 2 {
		t.Fatalf("clock rounding evidence = %+v, %v", got.Video, err)
	}
}

func TestGeneratedSegmentBoundsAccumulatesEveryPacketDuration(t *testing.T) {
	packets := strings.Join([]string{
		generatedBoundsPacketJSON(0, 0, 0, 2, "K__"),
		generatedBoundsPacketJSON(0, 2, 2, 1, "___"),
		generatedBoundsPacketJSON(0, 3, 3, 3, "___"),
		generatedBoundsPacketJSON(0, 6, 6, 2, "___"),
	}, ",")
	got, err := generatedBoundsParse(t, generatedBoundsDocument(packets, generatedBoundsVideoStream), true, 5)
	if err != nil || got.Video.PacketCount != 4 || got.Video.EndPTS != 8 || got.Video.FirstPacketDuration != 2 || got.Video.LastPacketDuration != 2 ||
		got.Video.MinPacketDuration != 1 || got.Video.MaxPacketDuration != 3 || !got.Video.PresentationDecodeAligned || got.Video.TotalPresentationGapTicks != 0 {
		t.Fatalf("intermediate duration evidence = %+v, %v", got.Video, err)
	}
	// The same count, first/last durations and complete envelope can otherwise
	// hide two changed middle durations. Closure must check both extrema.
	uniform := strings.Join([]string{
		generatedBoundsPacketJSON(0, 0, 0, 2, "K__"),
		generatedBoundsPacketJSON(0, 2, 2, 2, "___"),
		generatedBoundsPacketJSON(0, 4, 4, 2, "___"),
		generatedBoundsPacketJSON(0, 6, 6, 2, "___"),
	}, ",")
	stable, err := generatedBoundsParse(t, generatedBoundsDocument(uniform, generatedBoundsVideoStream), true, 5)
	if err != nil || stable.Video.MinPacketDuration != 2 || stable.Video.MaxPacketDuration != 2 || !stable.Video.PresentationDecodeAligned ||
		stable.Video.PacketCount != got.Video.PacketCount || stable.Video.FirstPTS != got.Video.FirstPTS || stable.Video.EndPTS != got.Video.EndPTS {
		t.Fatalf("uniform duration comparison = %+v, %v", stable.Video, err)
	}
}

func TestGeneratedSegmentBoundsRetainsIntermediatePresentationDecodeMismatch(t *testing.T) {
	packets := strings.Join([]string{
		generatedBoundsPacketJSON(0, 0, 0, 1, "K__"),
		generatedBoundsPacketJSON(0, 2, 1, 1, "___"),
		generatedBoundsPacketJSON(0, 1, 2, 1, "___"),
		generatedBoundsPacketJSON(0, 3, 3, 1, "___"),
	}, ",")
	got, err := generatedBoundsParse(t, generatedBoundsDocument(packets, generatedBoundsVideoStream), true, 7)
	if err != nil || got.Video.FirstPTS != got.Video.FirstDTS || got.Video.EndPTS != got.Video.EndDTS || got.Video.PresentationDecodeAligned ||
		got.Video.MinPacketDuration != 1 || got.Video.MaxPacketDuration != 1 || got.Video.PacketCount != 4 {
		t.Fatalf("intermediate reordering was hidden by aligned endpoints: %+v, %v", got.Video, err)
	}
}

func TestGeneratedSegmentBoundsRejectsAmbiguousSideData(t *testing.T) {
	base := `{"stream_index":1,"pts":0,"dts":0,"duration":1024,"flags":"K__","side_data_list":[%s]}`
	valid := `{"side_data_type":"Skip Samples","skip_samples":0,"discard_padding":0}`
	for name, side := range map[string]string{
		"unknown":                 `{"side_data_type":"unknown"}`,
		"case override":           `{"side_data_type":"Skip Samples","skip_samples":0,"SKIP_SAMPLES":1024,"discard_padding":0}`,
		"duplicate key":           `{"side_data_type":"Skip Samples","skip_samples":0,"skip_samples":1,"discard_padding":0}`,
		"duplicate type":          valid + "," + valid,
		"missing trim":            `{"side_data_type":"Skip Samples","skip_samples":0}`,
		"negative trim":           `{"side_data_type":"Skip Samples","skip_samples":-1,"discard_padding":0}`,
		"oversized trim":          `{"side_data_type":"Skip Samples","skip_samples":2048,"discard_padding":0}`,
		"combined oversized trim": `{"side_data_type":"Skip Samples","skip_samples":1024,"discard_padding":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := generatedBoundsParse(t, generatedBoundsDocument(fmt.Sprintf(base, side), generatedBoundsAudioStream), false, 5); !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("ambiguous side data accepted: %v", err)
			}
		})
	}
}

func TestGeneratedSegmentBoundsEnforcesFixedRecordAndPacketBudgets(t *testing.T) {
	oversized := generatedBoundsDocument(`{"stream_index":0,"unknown":"`+strings.Repeat("x", maxGeneratedBoundsRecordBytes)+`"}`, generatedBoundsVideoStream)
	if _, err := generatedBoundsParse(t, oversized, true, 23); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("record budget: %v", err)
	}
	budget := &generatedBoundsBudget{cancel: func() {}}
	output := &generatedBoundsOutput{budget: budget, section: "packets", packets: maxGeneratedBoundsPackets}
	if err := output.consumeRecord([]byte(generatedBoundsPacketJSON(0, 0, 0, 2, "K__"))); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("packet budget: %v", err)
	}
	budget.bytes = maxGeneratedBoundsOutputBytes
	if _, err := output.Write([]byte(" ")); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("output budget: %v", err)
	}
}

func TestGeneratedSegmentBoundsCapsUnmergedBFrameIntervals(t *testing.T) {
	var packets []string
	for index := int64(0); index <= maxGeneratedBoundsIntervals; index++ {
		flags := "___"
		if index == 0 {
			flags = "K__"
		}
		packets = append(packets, generatedBoundsPacketJSON(0, index*4, index*2, 2, flags))
	}
	if _, err := generatedBoundsParse(t, generatedBoundsDocument(strings.Join(packets, ","), generatedBoundsVideoStream), true, 11); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("reordering interval budget: %v", err)
	}
}

func TestGeneratedBoundsBudgetCancellationIsShared(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	budget := &generatedBoundsBudget{cancel: cancel, bytes: maxGeneratedBoundsOutputBytes}
	if err := budget.add(1); !errors.Is(err, ErrTimelineLimit) || ctx.Err() != context.Canceled {
		t.Fatalf("shared cancellation = %v, %v", err, ctx.Err())
	}
	if err := budget.fail(ErrTimelineProbe); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("first budget failure replaced: %v", err)
	}
}
