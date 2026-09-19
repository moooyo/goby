package transcode

import (
	"bytes"
	"testing"
)

func videoReadyHEVCTrack(id uint32) videoReadyTrack {
	config := make([]byte, 23)
	config[0], config[1], config[12] = 1, 1, 120
	config[13], config[15], config[16], config[17], config[18] = 0xf0, 0xfc, 0xfd, 0xf8, 0xf8
	config[21], config[22] = 15, 3
	for _, kind := range []byte{32, 33, 34} {
		config = append(config, 0x80|kind, 0, 1, 0, 3, kind<<1, 1, 0x80)
	}
	return videoReadyTrack{id: id, handler: "vide", entryKind: "hvc1", configBoxes: [][]byte{videoReadyBox("hvcC", config)}, timescale: 16000}
}

func videoReadyAV1Track(id uint32) videoReadyTrack {
	return videoReadyTrack{id: id, handler: "vide", entryKind: "av01", timescale: 16000,
		configBoxes: [][]byte{videoReadyBox("av1C", []byte{0x81, 4, 0x0c, 0, 0x0a, 1, 0x18})}}
}

func TestProgressiveVideoReadyHEVCAndAV1CodecBinding(t *testing.T) {
	for _, test := range []struct {
		codec  string
		track  videoReadyTrack
		sample []byte
	}{
		{"hevc", videoReadyHEVCTrack(11), []byte{0, 0, 0, 3, 19 << 1, 1, 0x80}},
		{"av1", videoReadyAV1Track(11), []byte{0x32, 2, 0x80, 0}},
	} {
		t.Run(test.codec, func(t *testing.T) {
			plan := videoReadyPlan(true)
			plan.VideoCodec = test.codec
			init := videoReadyInit(test.track, videoReadyAACTrack(22))
			fragment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{test.sample}})
			data := videoReadyJoin(init, fragment)
			videoReadyWantReady(t, plan, data)
			if ready, err := ProgressiveVideoReady(plan, data[:len(data)-1]); ready || err != nil {
				t.Fatalf("partial video sample became ready: %v, %v", ready, err)
			}
			plan.VideoCodec, plan.VideoCopyCodec = "copy", test.codec
			videoReadyWantReady(t, plan, data)
			plan.VideoCodec, plan.VideoCopyCodec = "h264", ""
			videoReadyWantInvalid(t, plan, data)
			plan.VideoCodec = test.codec
			test.track.configBoxes = append(test.track.configBoxes, videoReadyH264Track(11).configBoxes...)
			videoReadyWantInvalid(t, plan, videoReadyJoin(videoReadyInit(test.track, videoReadyAACTrack(22)), fragment))
		})
	}
}

func TestProgressiveVideoReadyHEVCRequiresParameterSetsAndVCL(t *testing.T) {
	plan := videoReadyPlan(false)
	plan.VideoCodec = "hevc"
	track := videoReadyHEVCTrack(11)
	config := bytes.Clone(track.configBoxes[0][8:])
	for name, mutation := range map[string]func([]byte) []byte{
		"missing PPS":           func(data []byte) []byte { data[22] = 2; return data[:len(data)-8] },
		"invalid parameter NAL": func(data []byte) []byte { data[28] |= 0x80; return data },
		"zero temporal id":      func(data []byte) []byte { data[29] = 0; return data },
		"reserved width":        func(data []byte) []byte { data[21] = data[21]&0xfc | 2; return data },
		"trailing data":         func(data []byte) []byte { return append(data, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := track
			invalid.configBoxes = [][]byte{videoReadyBox("hvcC", mutation(bytes.Clone(config)))}
			videoReadyWantInvalid(t, plan, videoReadyInit(invalid))
		})
	}
	metadata := videoReadyJoin(videoReadyInit(track), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{{0, 0, 0, 3, 39 << 1, 1, 0x80}}}))
	if ready, err := ProgressiveVideoReady(plan, metadata); ready || err != nil {
		t.Fatalf("metadata-only HEVC sample became playable: %v, %v", ready, err)
	}
}

func TestProgressiveVideoReadyAV1RequiresSequenceAndFramePayload(t *testing.T) {
	plan := videoReadyPlan(false)
	plan.VideoCodec = "av1"
	for name, sample := range map[string][]byte{
		"metadata":                   {0x2a, 1, 0x80},
		"frame header without tiles": {0x1a, 1, 0x80},
		"tiles without frame header": {0x22, 1, 0x80},
		"empty frame":                {0x32, 0},
	} {
		t.Run(name, func(t *testing.T) {
			data := videoReadyJoin(videoReadyInit(videoReadyAV1Track(11)), videoReadyFragment(videoReadyFragmentOptions{},
				videoReadyRun{trackID: 11, samples: [][]byte{sample}}))
			if ready, err := ProgressiveVideoReady(plan, data); ready || err != nil {
				t.Fatalf("non-picture AV1 sample became playable: %v, %v", ready, err)
			}
		})
	}
	for name, sample := range map[string][]byte{
		"truncated size":     {0x32, 0x80},
		"truncated payload":  {0x32, 5, 0x80},
		"forbidden header":   {0xb2, 1, 0x80},
		"reserved extension": {0x36, 1, 1, 0x80},
	} {
		t.Run(name, func(t *testing.T) {
			data := videoReadyJoin(videoReadyInit(videoReadyAV1Track(11)), videoReadyFragment(videoReadyFragmentOptions{},
				videoReadyRun{trackID: 11, samples: [][]byte{sample}}))
			videoReadyWantInvalid(t, plan, data)
		})
	}
	track := videoReadyAV1Track(11)
	track.configBoxes = [][]byte{videoReadyBox("av1C", []byte{0x81, 4, 0x0c, 0})}
	frame := []byte{0x32, 2, 0x80, 0}
	data := videoReadyJoin(videoReadyInit(track), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{frame}}))
	videoReadyWantInvalid(t, plan, data)
	data = videoReadyJoin(videoReadyInit(track), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{{0x32, 1, 0x80, 0x0a, 1, 0x18}}}))
	videoReadyWantInvalid(t, plan, data)
	data = videoReadyJoin(videoReadyInit(track), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{append([]byte{0x0a, 1, 0x18}, frame...)}}))
	videoReadyWantReady(t, plan, data)
	data = videoReadyJoin(videoReadyInit(videoReadyAV1Track(11)), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{{0x30, 0x80}}}))
	videoReadyWantReady(t, plan, data)
	data = videoReadyJoin(videoReadyInit(videoReadyAV1Track(11)), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{{0x1a, 1, 0x80, 0x22, 1, 0x80}}}))
	videoReadyWantReady(t, plan, data)
}

func TestProgressiveVideoReadyCopyKeepsSourceProfileAndChroma(t *testing.T) {
	plan := videoReadyPlan(false)
	plan.VideoCodec = "copy"
	hevc := videoReadyHEVCTrack(11)
	config := bytes.Clone(hevc.configBoxes[0][8:])
	config[1], config[16], config[17], config[18] = 4, 0xff, 0xfc, 0xfc
	hevc.configBoxes = [][]byte{videoReadyBox("hvcC", config)}
	plan.VideoCopyCodec = "hevc"
	data := videoReadyJoin(videoReadyInit(hevc), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{{0, 0, 0, 3, 19 << 1, 1, 0x80}}}))
	videoReadyWantReady(t, plan, data)
	for _, kind := range []byte{10, 15, 22, 31} {
		data = videoReadyJoin(videoReadyInit(hevc), videoReadyFragment(videoReadyFragmentOptions{},
			videoReadyRun{trackID: 11, samples: [][]byte{{0, 0, 0, 3, kind << 1, 1, 0x80}}}))
		videoReadyWantInvalid(t, plan, data)
	}
	av1 := videoReadyAV1Track(11)
	av1.configBoxes = [][]byte{videoReadyBox("av1C", []byte{0x81, 0x24, 0, 0, 0x0a, 1, 0x38})}
	plan.VideoCopyCodec = "av1"
	data = videoReadyJoin(videoReadyInit(av1), videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{{0x32, 1, 0x80}}}))
	videoReadyWantReady(t, plan, data)
	av1.configBoxes = [][]byte{videoReadyBox("av1C", []byte{0x81, 0x24, 0, 0, 0x2a, 1, 0, 0x0a, 1, 0x38})}
	videoReadyWantInvalid(t, plan, videoReadyInit(av1))
}
