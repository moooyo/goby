package transcode

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func liveRestartAnnexBFixture(nals ...[]byte) []byte {
	var data []byte
	for _, nal := range nals {
		data = append(data, 0, 0, 0, 1)
		data = append(data, nal...)
	}
	return data
}

func liveRestartH264Packet() []byte {
	return liveRestartAnnexBFixture(videoReadySPS(), videoReadyPPS(), []byte{0x65, 0x88, 0x84, 0x80})
}

func TestLiveVideoRestartNALSyntaxRejectsOpenGOPAndMissingConfiguration(t *testing.T) {
	h264 := liveRestartH264Packet()
	hevc := liveRestartAnnexBFixture([]byte{64, 1, 0x80}, []byte{66, 1, 0x80}, []byte{68, 1, 0x80}, []byte{38, 1, 0x80})
	for _, test := range []struct {
		codec          string
		config, packet []byte
		fragmented     bool
		want           bool
	}{
		{"h264", nil, h264, false, true},
		{"hevc", nil, hevc, false, true},
		{"h264", nil, liveRestartAnnexBFixture([]byte{0x65, 0x88, 0x84, 0x80}), false, false},
		{"hevc", nil, liveRestartAnnexBFixture([]byte{38, 1, 0x80}), false, false},
		{"h264", videoReadyH264Track(1).configBoxes[0][8:], videoReadyIDR(), true, true},
		{"hevc", videoReadyHEVCTrack(1).configBoxes[0][8:], videoReadyNALs([]byte{40, 1, 0x80}), true, true},
		{"hevc", videoReadyHEVCTrack(1).configBoxes[0][8:], videoReadyNALs([]byte{42, 1, 0x80}), true, false},
		{"hevc", videoReadyHEVCTrack(1).configBoxes[0][8:], videoReadyNALs([]byte{32, 1, 0x80}), true, false},
		{"h264", videoReadyH264Track(1).configBoxes[0][8:], videoReadyNALs([]byte{0x61, 0x88, 0x84}), true, false},
		{"h264", nil, videoReadyIDR(), true, false},
	} {
		if got := liveVideoRestartPacket(test.codec, test.config, test.packet, test.fragmented); got != test.want {
			t.Fatalf("%s fragmented=%t restart=%t want=%t", test.codec, test.fragmented, got, test.want)
		}
	}
	late := liveRestartAnnexBFixture([]byte{0x65, 0x88, 0x84, 0x80}, videoReadySPS(), videoReadyPPS())
	if liveVideoRestartPacket("h264", nil, late, false) {
		t.Fatal("later parameter sets repaired an uninitialized first access unit")
	}
	for _, packet := range [][]byte{videoReadyIDR()[:4], append(videoReadyIDR(), 0), []byte{0, 0, 0, 255, 0x65, 0x88}} {
		if liveVideoRestartPacket("h264", videoReadyH264Track(1).configBoxes[0][8:], packet, true) {
			t.Fatal("truncated sample framing became a restart proof")
		}
	}
}

func TestLiveVideoRestartAV1UsesCompleteShownKeyClassification(t *testing.T) {
	sequence, _ := hex.DecodeString("000000043cffbc6af940c0")
	obu := append([]byte{0x0a, byte(len(sequence))}, sequence...)
	config := append([]byte{0x81, 4, 0x0c, 0}, obu...)
	frame := []byte{0x32, 2, 0x10, 0x80}
	for _, packet := range [][]byte{frame, append(append([]byte{}, obu...), frame...), append([]byte{0x12, 0}, frame...)} {
		if !liveVideoRestartPacket("av1", config, packet, true) {
			t.Fatal("initialized displayed AV1 key frame was rejected")
		}
	}
	for _, prefix := range []byte{0x00, 0x30, 0x50, 0x70, 0x80} {
		if liveVideoRestartPacket("av1", config, []byte{0x32, 2, prefix, 0x80}, true) {
			t.Fatal("hidden, inter, intra-only, switch, or show-existing AV1 picture was accepted")
		}
	}
	if liveVideoRestartPacket("av1", []byte{0x81, 4, 0x0c, 0}, frame, true) || liveVideoRestartPacket("av1", config, frame[:3], true) {
		t.Fatal("missing sequence or incomplete AV1 payload was accepted")
	}
}

func liveRestartHexdump(data []byte) string {
	var output strings.Builder
	output.WriteByte('\n')
	for offset := 0; offset < len(data); offset += 16 {
		row := data[offset:min(offset+16, len(data))]
		fmt.Fprintf(&output, "%08x: ", offset)
		for index, value := range row {
			fmt.Fprintf(&output, "%02x", value)
			if index&1 != 0 {
				output.WriteByte(' ')
			}
		}
		output.WriteString(strings.Repeat(" ", 41-2*len(row)-len(row)/2))
		for _, value := range row {
			if value < 32 || value > 126 {
				value = '.'
			}
			output.WriteByte(value)
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func liveRestartProbeFixture(packet []byte) map[string]any {
	return map[string]any{"packets_and_frames": []any{
		map[string]any{"type": "packet", "stream_index": 0, "size": fmt.Sprint(len(packet)), "flags": "K__", "data": liveRestartHexdump(packet)},
		map[string]any{"type": "frame", "media_type": "video", "stream_index": 0, "key_frame": 1, "pict_type": "I", "width": 64, "height": 64},
	}, "streams": []any{map[string]any{"index": 0, "codec_name": "h264", "codec_type": "video", "extradata_size": 0}}}
}

func TestLiveVideoRestartProbeRequiresRealDecodedFirstPacketAssociation(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"valid":                func(map[string]any) {},
		"non-key flag":         func(d map[string]any) { d["packets_and_frames"].([]any)[0].(map[string]any)["flags"] = "___" },
		"corrupt flag":         func(d map[string]any) { d["packets_and_frames"].([]any)[0].(map[string]any)["flags"] = "K_C" },
		"no decoded frame":     func(d map[string]any) { d["packets_and_frames"] = d["packets_and_frames"].([]any)[:1] },
		"decoded other stream": func(d map[string]any) { d["packets_and_frames"].([]any)[1].(map[string]any)["stream_index"] = 1 },
		"decoded non-key":      func(d map[string]any) { d["packets_and_frames"].([]any)[1].(map[string]any)["key_frame"] = 0 },
		"decoded wrong codec":  func(d map[string]any) { d["streams"].([]any)[0].(map[string]any)["codec_name"] = "hevc" },
		"unknown packet state": func(d map[string]any) {
			d["packets_and_frames"].([]any)[0].(map[string]any)["side_data_list"] = []any{map[string]any{"side_data_type": "New Extradata"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			document := liveRestartProbeFixture(liveRestartH264Packet())
			change(document)
			data, _ := json.Marshal(document)
			err := parseLiveVideoRestartProbe(data, "h264", false)
			if (err == nil) != (name == "valid") {
				t.Fatalf("probe evidence = %v", err)
			}
		})
	}
	packet := bytes.Replace(liveRestartH264Packet(), []byte{0x65, 0x88}, []byte{0x61, 0x88}, 1)
	document, _ := json.Marshal(liveRestartProbeFixture(packet))
	if parseLiveVideoRestartProbe(document, "h264", false) == nil {
		t.Fatal("container and decoded key flags overrode non-IDR packet contents")
	}
}
