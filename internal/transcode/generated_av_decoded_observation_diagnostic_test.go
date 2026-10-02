package transcode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func generatedAVObservationActualShape() string {
	streams := `[{"index":0,"codec_name":"h264","codec_type":"video","time_base":"1/90000"},{"index":1,"codec_name":"aac","codec_type":"audio","sample_fmt":"fltp","sample_rate":"48000","channels":2,"channel_layout":"stereo","time_base":"1/90000"}]`
	return `{"frames":[{"media_type":"video","stream_index":0,"key_frame":1,"pts":1920,"pkt_dts":1920,"side_data_list":[{"side_data_type":"H.26[45] User Data Unregistered SEI message"}]},` +
		`{"media_type":"audio","stream_index":1,"key_frame":1,"pts":0,"pkt_dts":0,"nb_samples":1024},{"media_type":"audio","stream_index":1,"key_frame":1,"nb_samples":1024},` +
		`{"media_type":"video","stream_index":0,"key_frame":0,"pts":5670,"pkt_dts":5670}],"programs":[{"streams":` + streams + `}],"stream_groups":[],"streams":` + streams + `}`
}

func TestGeneratedAVDecodedObservationKeepsActualUnknownFields(t *testing.T) {
	data := []byte(generatedAVObservationActualShape())
	got, err := ParseGeneratedAVDecodedObservation(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Qualified || got.Complete || !got.FramesParsed || got.NativeClockComplete || got.Video.Frames != 2 || got.Video.NativePTSFrames != 2 || got.Video.NativeDTSFrames != 2 || got.Video.NativeDurationFrames != 0 || got.Audio.Frames != 2 || got.Audio.NativePTSFrames != 1 || got.Audio.Samples != 2048 {
		t.Fatalf("unknown actual fields were filled or qualified: %+v", got)
	}
	for _, frame := range got.Frames {
		if frame.DurationKnown || frame.Duration != 0 || frame.EndKnown || frame.End != (GeneratedRational{}) {
			t.Fatal("samples, adjacent PTS or nominal cadence fabricated native duration/end")
		}
	}
	if got.Frames[2].PTSKnown || got.Frames[2].DTSKnown || got.Frames[2].PTS != 0 || got.Audio.ClockComplete || got.Audio.AggregateEndKnown || got.Audio.GapFactsKnown || got.Video.ClockComplete || got.Video.AggregateEndKnown || got.Video.GapFactsKnown {
		t.Fatal("a missing timestamp became a complete clock or aggregate interval")
	}
	if _, err := ParseGeneratedAVEffectiveDecodeDiagnostic(context.Background(), data); err == nil {
		t.Fatal("old strict source/effective parser was loosened")
	}
	strict := strings.Replace(generatedAVEffectiveTestJSON(), `"pts":0,`, "", 1)
	if _, err := ParseGeneratedAVEffectiveDecodeDiagnostic(context.Background(), []byte(strict)); err == nil {
		t.Fatal("old mandatory native PTS was repaired")
	}
	strictDuration := strings.Replace(generatedAVEffectiveTestJSON(), `"duration":3750,`, "", 1)
	if _, err := ParseGeneratedAVEffectiveDecodeDiagnostic(context.Background(), []byte(strictDuration)); err == nil {
		t.Fatal("old mandatory returned video duration was repaired")
	}
	strictAudioPTS := strings.Replace(generatedAVEffectiveTestJSON(), `"stream_index":1,"pts":0,`, `"stream_index":1,`, 1)
	if _, err := ParseGeneratedAVEffectiveDecodeDiagnostic(context.Background(), []byte(strictAudioPTS)); err == nil {
		t.Fatal("old mandatory returned audio PTS was repaired")
	}
}

func generatedAVObservationMutation(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(generatedAVObservationActualShape()), &doc); err != nil {
		t.Fatal(err)
	}
	mutate(doc)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGeneratedAVDecodedObservationProgramAssociationRejectsSubstitution(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"programs null":        func(doc map[string]any) { doc["programs"] = nil },
		"groups null":          func(doc map[string]any) { doc["stream_groups"] = nil },
		"program streams null": func(doc map[string]any) { doc["programs"].([]any)[0].(map[string]any)["streams"] = nil },
		"extra program": func(doc map[string]any) {
			doc["programs"] = append(doc["programs"].([]any), doc["programs"].([]any)[0])
		},
		"nonempty group":        func(doc map[string]any) { doc["stream_groups"] = []any{map[string]any{}} },
		"program unknown field": func(doc map[string]any) { doc["programs"].([]any)[0].(map[string]any)["program_id"] = 1 },
		"nested duplicate index": func(doc map[string]any) {
			streams := doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)
			streams[1] = streams[0]
		},
		"nested unknown field": func(doc map[string]any) {
			doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)[0].(map[string]any)["unknown"] = 1
		},
		"nested null": func(doc map[string]any) {
			doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)[0].(map[string]any)["time_base"] = nil
		},
		"nested wrong type": func(doc map[string]any) {
			doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)[0].(map[string]any)["time_base"] = 90000
		},
		"nested contradictory timebase": func(doc map[string]any) {
			doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)[0].(map[string]any)["time_base"] = "1/24000"
		},
		"nested contradictory codec": func(doc map[string]any) {
			doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)[0].(map[string]any)["codec_name"] = "hevc"
		},
		"nested contradictory layout": func(doc map[string]any) {
			doc["programs"].([]any)[0].(map[string]any)["streams"].([]any)[1].(map[string]any)["channel_layout"] = "mono"
		},
		"top unknown field": func(doc map[string]any) { doc["unknown"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			data := generatedAVObservationMutation(t, mutate)
			got, err := ParseGeneratedAVDecodedObservation(context.Background(), data)
			if err == nil || got.FramesParsed || len(got.Frames) != 0 {
				t.Fatal("unknown or contradictory description became observed facts")
			}
		})
	}
	duplicate := strings.Replace(generatedAVObservationActualShape(), `"index":0,`, `"index":0,"index":0,`, 1)
	if _, err := ParseGeneratedAVDecodedObservation(context.Background(), []byte(duplicate)); err == nil {
		t.Fatal("nested duplicate JSON key was ignored")
	}
}

func TestGeneratedAVDecodedObservationUsesOnlyReturnedNativeDuration(t *testing.T) {
	data := generatedAVObservationMutation(t, func(doc map[string]any) {
		for _, raw := range doc["frames"].([]any) {
			frame := raw.(map[string]any)
			if frame["media_type"] == "video" {
				frame["duration"] = float64(3750)
			} else {
				frame["duration"] = float64(1920)
				if _, known := frame["pts"]; !known {
					frame["pts"] = float64(1920)
					frame["pkt_dts"] = float64(1920)
				}
			}
		}
	})
	got, err := ParseGeneratedAVDecodedObservation(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NativeClockComplete || !got.Video.ClockComplete || !got.Audio.ClockComplete || !got.Video.AggregateEndKnown || !got.Audio.AggregateEndKnown || got.Qualified || got.Complete {
		t.Fatal("returned explicit duration facts were lost or qualified")
	}
}
