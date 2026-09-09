package transcode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestProgressiveVideoReadyRequiresCompleteVideoSample(t *testing.T) {
	for _, withAudio := range []bool{false, true} {
		for _, defaultSize := range []bool{false, true} {
			plan := videoReadyPlan(withAudio)
			tracks := []videoReadyTrack{videoReadyH264Track(11)}
			if withAudio {
				tracks = append(tracks, videoReadyAACTrack(22))
			}
			data := videoReadyJoin(videoReadyInit(tracks...), videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{
				trackID: 11, samples: [][]byte{videoReadyIDR()}, defaultSize: defaultSize,
			}))
			original := bytes.Clone(data)
			for size := 0; size < len(data); size++ {
				ready, err := ProgressiveVideoReady(plan, data[:size])
				if ready || err != nil {
					t.Fatalf("audio=%t defaultSize=%t incomplete prefix %d/%d: ready=%t err=%v", withAudio, defaultSize, size, len(data), ready, err)
				}
			}
			videoReadyWantReady(t, plan, data)
			if ready, err := ProgressiveMediaReady(plan, data); !ready || err != nil {
				t.Fatalf("video dispatcher audio=%t defaultSize=%t: ready=%t err=%v", withAudio, defaultSize, ready, err)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("readiness inspection mutated the input prefix")
			}
		}
	}
}

func TestProgressiveVideoReadyHandlesCopySelectionsAndBothTracks(t *testing.T) {
	init := videoReadyInit(videoReadyH264Track(11), videoReadyAACTrack(22))
	for _, videoCodec := range []string{"h264", "copy"} {
		for _, audioCodec := range []string{"aac", "copy"} {
			for _, videoFirst := range []bool{false, true} {
				plan := videoReadyPlan(true)
				plan.VideoCodec, plan.AudioCodec = videoCodec, audioCodec
				video := videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}}
				audio := videoReadyRun{trackID: 22, samples: [][]byte{{0x21, 0x10, 0x04, 0x60}}, defaultSize: true}
				runs := []videoReadyRun{audio, video}
				if videoFirst {
					runs = []videoReadyRun{video, audio}
				}
				videoReadyWantReady(t, plan, videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{}, runs...)))
			}
		}
	}
}

func TestProgressiveVideoReadySkipsAudioAndNonVCLSamples(t *testing.T) {
	plan := videoReadyPlan(true)
	init := videoReadyInit(videoReadyH264Track(11), videoReadyAACTrack(22))
	audio := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 22, samples: [][]byte{{0x21, 0x10, 0x04, 0x60}}})
	audioOnly := videoReadyJoin(init, audio)
	if ready, err := ProgressiveVideoReady(plan, audioOnly); ready || err != nil {
		t.Fatalf("an audio-only fragment must remain pending: ready=%t err=%v", ready, err)
	}
	video := videoReadyFragment(videoReadyFragmentOptions{sequence: 2}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyNALs([]byte{0x41, 0x9a, 0x22})}, decodeTime: 1000})
	videoReadyWantReady(t, plan, videoReadyJoin(audioOnly, video))

	for name, sample := range map[string][]byte{
		"SPS": videoReadyNALs(videoReadySPS()),
		"PPS": videoReadyNALs(videoReadyPPS()),
		"SEI": videoReadyNALs([]byte{0x06, 0x05, 0x01, 0x00, 0x80}),
		"AUD": videoReadyNALs([]byte{0x09, 0xf0}),
	} {
		t.Run(name, func(t *testing.T) {
			prefix := videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{sample}}))
			if ready, _ := ProgressiveVideoReady(plan, prefix); ready {
				t.Fatal("a complete non-VCL sample cannot establish video readiness")
			}
			videoReadyWantReady(t, plan, videoReadyJoin(prefix, video))
			mixed := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{sample, videoReadyIDR()}})
			videoReadyWantReady(t, plan, videoReadyJoin(init, mixed))
		})
	}
}

func TestProgressiveVideoReadyPreservesSignedTimingMetadata(t *testing.T) {
	for _, version := range []byte{0, 1} {
		for _, mediaTime := range []int64{-1, -2048, 1024} {
			track := videoReadyH264Track(11)
			track.tkhdVersion, track.mdhdVersion, track.elstVersion = version, version, version
			track.edits = []videoReadyEdit{{duration: 240, mediaTime: -1}, {duration: 4000, mediaTime: mediaTime}}
			plan := videoReadyPlan(false)
			plan.SourceFormatStartKnown = true
			plan.SourceFormatStartTicks = -2 * ticksPerSecond
			data := videoReadyJoin(videoReadyInit(track), videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{
				trackID: 11, samples: [][]byte{videoReadyIDR()}, cts: []int32{-2048}, decodeTime: 2048, tfdtVersion: version,
			}))
			videoReadyWantReady(t, plan, data)
			plan.SourceFormatStartKnown = false
			plan.SourceFormatStartTicks = 0
			videoReadyWantReady(t, plan, data)
		}
	}
}

func TestProgressiveVideoReadyValidatesSelectedTrackInitialization(t *testing.T) {
	for _, test := range []struct {
		name      string
		withAudio bool
		tracks    []videoReadyTrack
		trexIDs   []uint32
	}{
		{"missing video", true, []videoReadyTrack{videoReadyAACTrack(22)}, nil},
		{"missing selected audio", true, []videoReadyTrack{videoReadyH264Track(11)}, nil},
		{"unselected audio", false, []videoReadyTrack{videoReadyH264Track(11), videoReadyAACTrack(22)}, nil},
		{"two video tracks", false, []videoReadyTrack{videoReadyH264Track(11), videoReadyH264Track(33)}, nil},
		{"two audio tracks", true, []videoReadyTrack{videoReadyH264Track(11), videoReadyAACTrack(22), videoReadyAACTrack(33)}, nil},
		{"duplicate track ID", true, []videoReadyTrack{videoReadyH264Track(11), videoReadyAACTrack(11)}, nil},
		{"zero track ID", false, []videoReadyTrack{videoReadyH264Track(0)}, nil},
		{"unknown trex", false, []videoReadyTrack{videoReadyH264Track(11)}, []uint32{22}},
		{"duplicate trex", false, []videoReadyTrack{videoReadyH264Track(11)}, []uint32{11, 11}},
		{"missing trex", false, []videoReadyTrack{videoReadyH264Track(11)}, []uint32{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			init := videoReadyInit(test.tracks...)
			if test.trexIDs != nil {
				init = videoReadyInitWithTrex(test.tracks, test.trexIDs)
			}
			videoReadyWantInvalid(t, videoReadyPlan(test.withAudio), videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})))
		})
	}

	for name, change := range map[string]func(*videoReadyTrack){
		"wrong video sample entry": func(track *videoReadyTrack) { track.entryKind = "hvc1" },
		"missing avcC":             func(track *videoReadyTrack) { track.configBoxes = nil },
		"duplicate avcC": func(track *videoReadyTrack) {
			track.configBoxes = append(track.configBoxes, bytes.Clone(track.configBoxes[0]))
		},
		"duplicate sample entry": func(track *videoReadyTrack) { track.extraEntry = true },
		"zero timescale":         func(track *videoReadyTrack) { track.timescale = 0 },
		"unknown handler":        func(track *videoReadyTrack) { track.handler = "meta" },
	} {
		t.Run(name, func(t *testing.T) {
			track := videoReadyH264Track(11)
			change(&track)
			videoReadyWantInvalid(t, videoReadyPlan(false), videoReadyInit(track))
		})
	}
	for name, change := range map[string]func(*videoReadyTrack){
		"wrong audio sample entry": func(track *videoReadyTrack) { track.entryKind = "Opus" },
		"missing esds":             func(track *videoReadyTrack) { track.configBoxes = nil },
		"duplicate esds": func(track *videoReadyTrack) {
			track.configBoxes = append(track.configBoxes, bytes.Clone(track.configBoxes[0]))
		},
		"duplicate audio entry": func(track *videoReadyTrack) { track.extraEntry = true },
	} {
		t.Run(name, func(t *testing.T) {
			track := videoReadyAACTrack(22)
			change(&track)
			videoReadyWantInvalid(t, videoReadyPlan(true), videoReadyInit(videoReadyH264Track(11), track))
		})
	}
}

func TestProgressiveVideoReadyRejectsMalformedCodecConfiguration(t *testing.T) {
	validConfig := videoReadyH264Track(11).configBoxes[0][8:]
	wrongSPSType := bytes.Clone(validConfig)
	wrongSPSType[8] = 0x68
	ppsCountOffset := 8 + len(videoReadySPS())
	missingPPS := bytes.Clone(validConfig[:ppsCountOffset+1])
	missingPPS[ppsCountOffset] = 0
	truncatedPPS := bytes.Clone(validConfig)
	binary.BigEndian.PutUint16(truncatedPPS[ppsCountOffset+1:ppsCountOffset+3], uint16(len(videoReadyPPS())+1))
	wrongPPSType := bytes.Clone(validConfig)
	wrongPPSType[ppsCountOffset+3] = 0x67
	for name, config := range map[string][]byte{
		"empty avcC":       {},
		"short avcC":       {1, 66, 0, 30, 0xff},
		"wrong version":    {0, 66, 0, 30, 0xff, 0xe1, 0, 1, 0x67, 1, 0, 1, 0x68},
		"missing SPS":      {1, 66, 0, 30, 0xff, 0xe0, 1, 0, 1, 0x68},
		"truncated SPS":    {1, 66, 0, 30, 0xff, 0xe1, 0, 20, 0x67},
		"wrong SPS type":   wrongSPSType,
		"missing PPS":      missingPPS,
		"truncated PPS":    truncatedPPS,
		"wrong PPS type":   wrongPPSType,
		"invalid NAL size": {1, 66, 0, 30, 0xfe, 0xe1, 0, 1, 0x67, 1, 0, 1, 0x68},
	} {
		t.Run(name, func(t *testing.T) {
			track := videoReadyH264Track(11)
			track.configBoxes = [][]byte{videoReadyBox("avcC", config)}
			videoReadyWantInvalid(t, videoReadyPlan(false), videoReadyInit(track))
		})
	}
	for name, descriptors := range map[string][]byte{
		"empty esds":                    {},
		"truncated ES descriptor":       {3, 127, 0, 22, 0},
		"missing AudioSpecificConfig":   videoReadyESDescriptors(nil, 0x40),
		"non-AAC object type":           videoReadyESDescriptors([]byte{0x12, 0x10}, 0x6b),
		"short AudioSpecificConfig":     videoReadyESDescriptors([]byte{0x12}, 0x40),
		"invalid audio object type":     videoReadyESDescriptors([]byte{0x00, 0x10}, 0x40),
		"missing program configuration": videoReadyESDescriptors([]byte{0x12, 0x00}, 0x40),
		"missing core coder delay":      videoReadyESDescriptors([]byte{0x12, 0x12}, 0x40),
	} {
		t.Run(name, func(t *testing.T) {
			track := videoReadyAACTrack(22)
			track.configBoxes = [][]byte{videoReadyFullBox("esds", 0, 0, descriptors)}
			videoReadyWantInvalid(t, videoReadyPlan(true), videoReadyInit(videoReadyH264Track(11), track))
		})
	}
}

func TestProgressiveVideoReadyAcceptsCompleteAACSpecificConfiguration(t *testing.T) {
	for name, config := range map[string][]byte{
		"native AAC-LC extension": {0x12, 0x10, 0x56, 0xe5, 0x00},
		// AAC-LC at 44.1 kHz, channel_configuration=0, and one front channel
		// pair in a matching LC PCE; no mixdown fields or comment bytes.
		"stereo program configuration": {0x12, 0x00, 0x05, 0x04, 0x00, 0x00, 0x20, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			track := videoReadyAACTrack(22)
			track.configBoxes = [][]byte{videoReadyFullBox("esds", 0, 0, videoReadyESDescriptors(config, 0x40))}
			data := videoReadyJoin(videoReadyInit(videoReadyH264Track(11), track), videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{
				trackID: 11, samples: [][]byte{videoReadyIDR()},
			}))
			videoReadyWantReady(t, videoReadyPlan(true), data)
		})
	}
}

func TestProgressiveVideoReadyChecksFragmentSampleBounds(t *testing.T) {
	plan := videoReadyPlan(true)
	init := videoReadyInit(videoReadyH264Track(11), videoReadyAACTrack(22))
	for name, runs := range map[string][]videoReadyRun{
		"unknown track":                {{trackID: 99, samples: [][]byte{videoReadyIDR()}}},
		"offset inside moof":           {{trackID: 11, samples: [][]byte{videoReadyIDR()}, absoluteOffset: videoReadyInt32(8)}},
		"offset inside mdat header":    {{trackID: 11, samples: [][]byte{videoReadyIDR()}, offsetDelta: -4}},
		"negative data offset":         {{trackID: 11, samples: [][]byte{videoReadyIDR()}, absoluteOffset: videoReadyInt32(-1)}},
		"outside declared mdat":        {{trackID: 11, samples: [][]byte{videoReadyIDR()}, offsetDelta: 1}},
		"declared sample exceeds mdat": {{trackID: 11, samples: [][]byte{videoReadyIDR()}, sizes: []uint32{uint32(len(videoReadyIDR()) + 1)}}},
		"overlapping audio sample": {
			{trackID: 11, samples: [][]byte{videoReadyIDR()}},
			{trackID: 22, samples: [][]byte{{1, 2, 3, 4}}, offsetDelta: -4},
		},
		"overlapping video sample": {
			{trackID: 22, samples: [][]byte{{1, 2, 3, 4}}},
			{trackID: 11, samples: [][]byte{videoReadyIDR()}, offsetDelta: -4},
		},
	} {
		t.Run(name, func(t *testing.T) {
			videoReadyWantInvalid(t, plan, videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{}, runs...)))
		})
	}
	for name, sample := range map[string][]byte{
		"zero length NAL":      {0, 0, 0, 0},
		"truncated NAL length": {0, 0, 0},
		"NAL exceeds sample":   {0, 0, 0, 20, 0x65},
		"forbidden NAL bit":    videoReadyNALs([]byte{0xe5, 0x80}),
		"trailing partial NAL": append(videoReadyIDR(), 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			data := videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{sample}}))
			videoReadyWantInvalid(t, plan, data)
		})
	}
}

func TestProgressiveVideoReadyAllowsLargeMediaBoxWithinPrefixBudget(t *testing.T) {
	plan := videoReadyPlan(false)
	init := videoReadyInit(videoReadyH264Track(11))
	for _, extended := range []bool{false, true} {
		data := videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{declaredMediaSize: 64 * 1024 * 1024, extendedMDAT: extended}, videoReadyRun{
			trackID: 11, samples: [][]byte{videoReadyIDR()}, defaultSize: true,
		}))
		videoReadyWantReady(t, plan, data)
		if ready, err := ProgressiveVideoReady(plan, data[:len(data)-1]); ready || err != nil {
			t.Fatalf("partial first sample in large mdat extended=%t: ready=%t err=%v", extended, ready, err)
		}
	}
	for _, defaultSize := range []bool{false, true} {
		data := videoReadyJoin(init, videoReadyFragment(videoReadyFragmentOptions{declaredMediaSize: 64 * 1024 * 1024}, videoReadyRun{
			trackID: 11, samples: [][]byte{videoReadyIDR()}, defaultSize: defaultSize, sizes: []uint32{MaxProgressivePrefixBytes},
		}))
		videoReadyWantInvalid(t, plan, data)
	}
	lateVideo := videoReadyFragment(videoReadyFragmentOptions{declaredMediaSize: 64 * 1024 * 1024},
		videoReadyRun{trackID: 22, samples: [][]byte{{1, 2, 3, 4}}},
		videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}, absoluteOffset: videoReadyInt32(MaxProgressivePrefixBytes)},
	)
	videoReadyWantInvalid(t, videoReadyPlan(true), videoReadyJoin(videoReadyInit(videoReadyH264Track(11), videoReadyAACTrack(22)), lateVideo))
	oversizedEnd := videoReadyFragment(videoReadyFragmentOptions{declaredMediaSize: math.MaxUint64, extendedMDAT: true}, videoReadyRun{
		trackID: 11, samples: [][]byte{videoReadyIDR()},
	})
	videoReadyWantInvalid(t, plan, videoReadyJoin(init, oversizedEnd))
	videoReadyWantInvalid(t, plan, make([]byte, MaxProgressivePrefixBytes+1))
	if MaxProgressivePrefixBytes > 4*1024*1024 {
		t.Fatal("video readiness increased the shared prefix memory bound")
	}
	metadata := videoReadyBox("free", make([]byte, MaxProgressivePrefixBytes-len(init)-8))
	videoReadyWantInvalid(t, plan, videoReadyJoin(init, metadata))
}

func TestProgressiveVideoReadyRejectsMalformedBoxMetadata(t *testing.T) {
	plan := videoReadyPlan(false)
	init := videoReadyInit(videoReadyH264Track(11))
	for _, kind := range []string{"ftyp", "moov", "trak", "tkhd", "mdia", "mdhd", "hdlr", "minf", "stbl", "stsd", "avc1", "avcC", "mvex", "trex"} {
		for _, size := range []uint32{0, 7, MaxProgressivePrefixBytes + 1, math.MaxUint32} {
			bad := bytes.Clone(init)
			offset := bytes.Index(bad, []byte(kind)) - 4
			if offset < 0 {
				t.Fatalf("fixture is missing %s", kind)
			}
			binary.BigEndian.PutUint32(bad[offset:offset+4], size)
			videoReadyWantInvalid(t, plan, bad)
		}
	}
	for _, size := range []uint64{0, 15, MaxProgressivePrefixBytes + 1, math.MaxUint64} {
		header := make([]byte, 16)
		binary.BigEndian.PutUint32(header[:4], 1)
		copy(header[4:8], "moov")
		binary.BigEndian.PutUint64(header[8:16], size)
		ftyp := videoReadyBox("ftyp", []byte("iso5\x00\x00\x00\x01iso5"))
		videoReadyWantInvalid(t, plan, videoReadyJoin(ftyp, header))
	}
	fragment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})
	for _, kind := range []string{"moof", "mfhd", "traf", "tfhd", "tfdt", "trun", "mdat"} {
		bad := bytes.Clone(fragment)
		offset := bytes.Index(bad, []byte(kind)) - 4
		binary.BigEndian.PutUint32(bad[offset:offset+4], 7)
		videoReadyWantInvalid(t, plan, videoReadyJoin(init, bad))
	}
	for _, kind := range []string{"tkhd", "mdhd", "stsd", "trex"} {
		bad := bytes.Clone(init)
		offset := bytes.Index(bad, []byte(kind))
		bad[offset+4] = 2
		videoReadyWantInvalid(t, plan, bad)
	}
	for _, kind := range []string{"mfhd", "tfhd", "tfdt", "trun"} {
		bad := bytes.Clone(fragment)
		offset := bytes.Index(bad, []byte(kind))
		bad[offset+4] = 2
		videoReadyWantInvalid(t, plan, videoReadyJoin(init, bad))
	}
	for _, kind := range []string{"tkhd", "mdhd", "hdlr", "stsd", "mvex", "trex"} {
		bad := bytes.Clone(init)
		offset := bytes.Index(bad, []byte(kind))
		copy(bad[offset:offset+4], "free")
		videoReadyWantInvalid(t, plan, bad)
	}
	for _, kind := range []string{"mfhd", "tfhd", "tfdt", "trun"} {
		bad := bytes.Clone(fragment)
		offset := bytes.Index(bad, []byte(kind))
		copy(bad[offset:offset+4], "free")
		videoReadyWantInvalid(t, plan, videoReadyJoin(init, bad))
	}
	for _, kind := range []string{"tkhd", "mdhd", "trex"} {
		bad := bytes.Clone(init)
		offset := bytes.Index(bad, []byte(kind))
		field := offset + 16
		if kind == "trex" {
			field = offset + 12
		}
		binary.BigEndian.PutUint32(bad[field:field+4], 0)
		videoReadyWantInvalid(t, plan, bad)
	}
	bad := bytes.Clone(fragment)
	trun := bytes.Index(bad, []byte("trun"))
	binary.BigEndian.PutUint32(bad[trun+8:trun+12], math.MaxUint32)
	videoReadyWantInvalid(t, plan, videoReadyJoin(init, bad))
}

func TestProgressiveVideoReadyRejectsNonFragmentInitializationAndExternalData(t *testing.T) {
	plan := videoReadyPlan(false)
	init := videoReadyInit(videoReadyH264Track(11))
	for _, kind := range []string{"stts", "stsc", "stsz", "stco"} {
		bad := bytes.Clone(init)
		offset := bytes.Index(bad, []byte(kind)) + 8
		if kind == "stsz" {
			offset += 4
		}
		binary.BigEndian.PutUint32(bad[offset:offset+4], 1)
		videoReadyWantInvalid(t, plan, bad)
	}
	for _, flags := range []byte{0, 2} {
		bad := bytes.Clone(init)
		offset := bytes.Index(bad, []byte("url "))
		bad[offset+7] = flags
		videoReadyWantInvalid(t, plan, bad)
	}
	bad := bytes.Clone(init)
	dref := bytes.Index(bad, []byte("dref"))
	binary.BigEndian.PutUint32(bad[dref+8:dref+12], 2)
	videoReadyWantInvalid(t, plan, bad)
	bad = bytes.Clone(init)
	entry := bytes.Index(bad, []byte("avc1"))
	binary.BigEndian.PutUint16(bad[entry+10:entry+12], 2)
	videoReadyWantInvalid(t, plan, bad)
	for _, version := range []byte{0, 1} {
		track := videoReadyH264Track(11)
		track.elstVersion = version
		track.edits = []videoReadyEdit{{duration: 240, mediaTime: -1}, {duration: 4000, mediaTime: 1024}}
		for name, mutate := range map[string]func([]byte, int){
			"invalid version": func(data []byte, offset int) { data[offset+4] = 2 },
			"invalid entry count": func(data []byte, offset int) {
				binary.BigEndian.PutUint32(data[offset+8:offset+12], math.MaxUint32)
			},
			"short box": func(data []byte, offset int) {
				binary.BigEndian.PutUint32(data[offset-4:offset], 7)
			},
		} {
			t.Run(name, func(t *testing.T) {
				bad := videoReadyInit(track)
				mutate(bad, bytes.Index(bad, []byte("elst")))
				videoReadyWantInvalid(t, plan, bad)
			})
		}
	}
}

func TestProgressiveMediaReadyKeepsAudioDispatchBehavior(t *testing.T) {
	for _, test := range []struct {
		container string
		codec     string
		data      []byte
	}{
		{"mp3", "mp3", progressiveReadyMP3([4]byte{0xff, 0xfb, 0x90, 0}, 417)},
		{"aac", "aac", progressiveReadyADTS(false, 0, []byte{1, 2, 3, 4})},
		{"flac", "flac", progressiveReadyFLAC([]byte{0xff, 0xf8, 0xca, 0x18, 0}, []byte{0, 1, 2})},
		{"ogg", "opus", progressiveReadyOgg()},
		{"wav", "pcm_s16le", progressiveReadyWAV(false, false, []byte{0, 0, 0, 0})},
		{"m4a", "aac", progressiveReadyM4A()},
	} {
		t.Run(test.container, func(t *testing.T) {
			plan := Plan{OutputMode: "progressive", Container: test.container, AudioCodec: test.codec, VideoStreamIndex: -1, AudioStreamIndex: 0}
			for _, prefix := range [][]byte{nil, test.data[:len(test.data)-1], test.data, {0}, make([]byte, MaxProgressivePrefixBytes+1)} {
				wantReady, wantErr := ProgressiveAudioReady(test.container, prefix)
				ready, err := ProgressiveMediaReady(plan, prefix)
				if ready != wantReady || (err == nil) != (wantErr == nil) || errors.Is(err, ErrInvalidProgressiveStream) != errors.Is(wantErr, ErrInvalidProgressiveStream) {
					t.Fatalf("audio dispatch changed for %d bytes: ready=%t err=%v; want ready=%t err=%v", len(prefix), ready, err, wantReady, wantErr)
				}
			}
		})
	}
}

func videoReadyPlan(withAudio bool) Plan {
	plan := Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: "h264", VideoStreamIndex: 3, AudioStreamIndex: -1}
	if withAudio {
		plan.AudioCodec, plan.AudioStreamIndex = "aac", 7
	}
	return plan
}

func videoReadyWantReady(t *testing.T, plan Plan, data []byte) {
	t.Helper()
	if ready, err := ProgressiveVideoReady(plan, data); !ready || err != nil {
		t.Fatalf("complete video prefix: ready=%t err=%v", ready, err)
	}
}

func videoReadyWantInvalid(t *testing.T, plan Plan, data []byte) {
	t.Helper()
	if ready, err := ProgressiveVideoReady(plan, data); ready || !errors.Is(err, ErrInvalidProgressiveStream) {
		t.Fatalf("invalid video prefix: ready=%t err=%v", ready, err)
	}
}

// These fixtures model ISO BMFF structure rather than decodable pictures. The
// track IDs deliberately differ from the selected input stream indexes.
type videoReadyTrack struct {
	id          uint32
	handler     string
	entryKind   string
	configBoxes [][]byte
	timescale   uint32
	tkhdVersion byte
	mdhdVersion byte
	elstVersion byte
	edits       []videoReadyEdit
	extraEntry  bool
}

type videoReadyEdit struct {
	duration  uint64
	mediaTime int64
}

func videoReadyH264Track(id uint32) videoReadyTrack {
	sps, pps := videoReadySPS(), videoReadyPPS()
	config := []byte{1, 66, 0, 30, 0xff, 0xe1, byte(len(sps) >> 8), byte(len(sps))}
	config = append(config, sps...)
	config = append(config, 1, byte(len(pps)>>8), byte(len(pps)))
	config = append(config, pps...)
	return videoReadyTrack{id: id, handler: "vide", entryKind: "avc1", configBoxes: [][]byte{videoReadyBox("avcC", config)}, timescale: 16000}
}

func videoReadyAACTrack(id uint32) videoReadyTrack {
	return videoReadyTrack{id: id, handler: "soun", entryKind: "mp4a", configBoxes: [][]byte{videoReadyFullBox("esds", 0, 0, videoReadyESDescriptors([]byte{0x12, 0x10}, 0x40))}, timescale: 44100}
}

func videoReadyESDescriptors(config []byte, objectType byte) []byte {
	decoder := []byte{objectType, 0x15, 0, 0, 0, 0, 1, 0xf4, 0, 0, 1, 0x77, 0}
	if config != nil {
		decoder = append(decoder, videoReadyDescriptor(5, config)...)
	}
	es := append([]byte{0, 22, 0}, videoReadyDescriptor(4, decoder)...)
	es = append(es, videoReadyDescriptor(6, []byte{2})...)
	return videoReadyDescriptor(3, es)
}

func videoReadyDescriptor(tag byte, payload []byte) []byte {
	if len(payload) >= 128 {
		panic("fixture descriptor exceeds its single-byte length encoding")
	}
	return append([]byte{tag, byte(len(payload))}, payload...)
}

func videoReadyInit(tracks ...videoReadyTrack) []byte {
	ids := make([]uint32, len(tracks))
	for i, track := range tracks {
		ids[i] = track.id
	}
	return videoReadyInitWithTrex(tracks, ids)
}

func videoReadyInitWithTrex(tracks []videoReadyTrack, trexIDs []uint32) []byte {
	mvhd := make([]byte, 96)
	binary.BigEndian.PutUint32(mvhd[8:12], 1000)
	binary.BigEndian.PutUint32(mvhd[16:20], 1<<16)
	binary.BigEndian.PutUint16(mvhd[20:22], 1<<8)
	videoReadyMatrix(mvhd[32:68])
	binary.BigEndian.PutUint32(mvhd[92:96], 100)
	movie := videoReadyFullBox("mvhd", 0, 0, mvhd)
	for _, track := range tracks {
		movie = append(movie, videoReadyTrackBox(track)...)
	}
	var extends []byte
	for _, id := range trexIDs {
		extends = append(extends, videoReadyFullBox("trex", 0, 0, videoReadyU32(id, 1, 1000, 0, 0))...)
	}
	movie = append(movie, videoReadyBox("mvex", extends)...)
	return videoReadyJoin(videoReadyBox("ftyp", []byte("iso5\x00\x00\x00\x01iso5iso6mp41")), videoReadyBox("moov", movie))
}

func videoReadyTrackBox(track videoReadyTrack) []byte {
	tkhd := make([]byte, 80)
	idOffset, matrixOffset, widthOffset := 8, 36, 72
	if track.tkhdVersion == 1 {
		tkhd = make([]byte, 92)
		idOffset, matrixOffset, widthOffset = 16, 48, 84
	}
	binary.BigEndian.PutUint32(tkhd[idOffset:idOffset+4], track.id)
	videoReadyMatrix(tkhd[matrixOffset : matrixOffset+36])
	if track.handler == "vide" {
		binary.BigEndian.PutUint32(tkhd[widthOffset:widthOffset+4], 320<<16)
		binary.BigEndian.PutUint32(tkhd[widthOffset+4:widthOffset+8], 180<<16)
	} else {
		binary.BigEndian.PutUint16(tkhd[matrixOffset-4:matrixOffset-2], 1<<8)
	}
	mdhd := make([]byte, 20)
	timescaleOffset := 8
	if track.mdhdVersion == 1 {
		mdhd, timescaleOffset = make([]byte, 32), 16
	}
	binary.BigEndian.PutUint32(mdhd[timescaleOffset:timescaleOffset+4], track.timescale)
	binary.BigEndian.PutUint16(mdhd[len(mdhd)-4:len(mdhd)-2], 0x55c4)
	handler := make([]byte, 20)
	copy(handler[4:8], track.handler)
	handler = append(handler, []byte("FixtureHandler\x00")...)
	entry := make([]byte, 78)
	if track.handler == "soun" {
		entry = make([]byte, 28)
		binary.BigEndian.PutUint16(entry[16:18], 2)
		binary.BigEndian.PutUint16(entry[18:20], 16)
		binary.BigEndian.PutUint32(entry[24:28], 44100<<16)
	} else {
		binary.BigEndian.PutUint16(entry[24:26], 320)
		binary.BigEndian.PutUint16(entry[26:28], 180)
		binary.BigEndian.PutUint32(entry[28:32], 72<<16)
		binary.BigEndian.PutUint32(entry[32:36], 72<<16)
		binary.BigEndian.PutUint16(entry[40:42], 1)
		binary.BigEndian.PutUint16(entry[74:76], 24)
		binary.BigEndian.PutUint16(entry[76:78], math.MaxUint16)
	}
	binary.BigEndian.PutUint16(entry[6:8], 1)
	entry = append(entry, videoReadyJoin(track.configBoxes...)...)
	entry = videoReadyBox(track.entryKind, entry)
	entries := videoReadyU32(1)
	if track.extraEntry {
		entries = videoReadyU32(2)
	}
	entries = append(entries, entry...)
	if track.extraEntry {
		entries = append(entries, entry...)
	}
	stbl := videoReadyBox("stbl", videoReadyJoin(
		videoReadyFullBox("stsd", 0, 0, entries),
		videoReadyFullBox("stts", 0, 0, videoReadyU32(0)),
		videoReadyFullBox("stsc", 0, 0, videoReadyU32(0)),
		videoReadyFullBox("stsz", 0, 0, videoReadyU32(0, 0)),
		videoReadyFullBox("stco", 0, 0, videoReadyU32(0)),
	))
	dref := videoReadyFullBox("dref", 0, 0, videoReadyJoin(videoReadyU32(1), videoReadyFullBox("url ", 0, 1, nil)))
	mediaHeader := videoReadyFullBox("vmhd", 0, 1, make([]byte, 8))
	if track.handler == "soun" {
		mediaHeader = videoReadyFullBox("smhd", 0, 0, make([]byte, 4))
	}
	minf := videoReadyBox("minf", videoReadyJoin(mediaHeader, videoReadyBox("dinf", dref), stbl))
	mdia := videoReadyJoin(videoReadyFullBox("mdhd", track.mdhdVersion, 0, mdhd), videoReadyFullBox("hdlr", 0, 0, handler), minf)
	contents := videoReadyFullBox("tkhd", track.tkhdVersion, 7, tkhd)
	if track.edits != nil {
		elst := videoReadyU32(uint32(len(track.edits)))
		for _, edit := range track.edits {
			if track.elstVersion == 1 {
				elst = append(elst, videoReadyU64(edit.duration, uint64(edit.mediaTime))...)
			} else {
				elst = append(elst, videoReadyU32(uint32(edit.duration), uint32(edit.mediaTime))...)
			}
			elst = append(elst, 0, 1, 0, 0)
		}
		contents = append(contents, videoReadyBox("edts", videoReadyFullBox("elst", track.elstVersion, 0, elst))...)
	}
	contents = append(contents, videoReadyBox("mdia", mdia)...)
	return videoReadyBox("trak", contents)
}

type videoReadyRun struct {
	trackID        uint32
	samples        [][]byte
	sizes          []uint32
	defaultSize    bool
	cts            []int32
	decodeTime     uint64
	tfdtVersion    byte
	offsetDelta    int32
	absoluteOffset *int32
}

type videoReadyFragmentOptions struct {
	declaredMediaSize uint64
	extendedMDAT      bool
	sequence          uint32
}

func videoReadyFragment(options videoReadyFragmentOptions, runs ...videoReadyRun) []byte {
	mdatHeader := 8
	if options.extendedMDAT {
		mdatHeader = 16
	}
	sequence := options.sequence
	if sequence == 0 {
		sequence = 1
	}
	buildMoof := func(moofSize int) []byte {
		contents := videoReadyFullBox("mfhd", 0, 0, videoReadyU32(sequence))
		payloadOffset := 0
		for _, run := range runs {
			sizes := make([]uint32, len(run.samples))
			for i, sample := range run.samples {
				sizes[i] = uint32(len(sample))
				if run.sizes != nil {
					sizes[i] = run.sizes[i]
				}
			}
			tfhdFlags := uint32(0x020008)
			tfhd := videoReadyU32(run.trackID, 1000)
			trunFlags := uint32(0x000201)
			if run.defaultSize {
				tfhdFlags |= 0x10
				tfhd = append(tfhd, videoReadyU32(sizes[0])...)
				trunFlags &^= 0x200
			}
			offset := int32(moofSize+mdatHeader+payloadOffset) + run.offsetDelta
			if run.absoluteOffset != nil {
				offset = *run.absoluteOffset
			}
			trun := videoReadyU32(uint32(len(run.samples)), uint32(offset))
			trunVersion := byte(0)
			if run.cts != nil {
				trunVersion, trunFlags = 1, trunFlags|0x800
			}
			for i, sample := range run.samples {
				if !run.defaultSize {
					trun = append(trun, videoReadyU32(sizes[i])...)
				}
				if run.cts != nil {
					trun = append(trun, videoReadyU32(uint32(run.cts[i]))...)
				}
				payloadOffset += len(sample)
			}
			tfdt := videoReadyU32(uint32(run.decodeTime))
			if run.tfdtVersion == 1 {
				tfdt = videoReadyU64(run.decodeTime)
			}
			traf := videoReadyJoin(videoReadyFullBox("tfhd", 0, tfhdFlags, tfhd), videoReadyFullBox("tfdt", run.tfdtVersion, 0, tfdt), videoReadyFullBox("trun", trunVersion, trunFlags, trun))
			contents = append(contents, videoReadyBox("traf", traf)...)
		}
		return videoReadyBox("moof", contents)
	}
	moof := buildMoof(0)
	moof = buildMoof(len(moof))
	var payload []byte
	for _, run := range runs {
		payload = append(payload, videoReadyJoin(run.samples...)...)
	}
	size := options.declaredMediaSize
	if size == 0 {
		size = uint64(mdatHeader + len(payload))
	}
	header := make([]byte, mdatHeader)
	copy(header[4:8], "mdat")
	if options.extendedMDAT {
		binary.BigEndian.PutUint32(header[:4], 1)
		binary.BigEndian.PutUint64(header[8:16], size)
	} else {
		binary.BigEndian.PutUint32(header[:4], uint32(size))
	}
	return videoReadyJoin(moof, header, payload)
}

func videoReadySPS() []byte {
	return []byte{0x67, 0x42, 0, 0x1e, 0x95, 0xa8, 0x14, 1, 0x6e, 0x9b, 0x80, 0x80, 0x80, 0xa0}
}
func videoReadyPPS() []byte { return []byte{0x68, 0xce, 0x3c, 0x80} }
func videoReadyIDR() []byte { return videoReadyNALs([]byte{0x65, 0x88, 0x84, 0, 0x33}) }

func videoReadyNALs(nals ...[]byte) []byte {
	var data []byte
	for _, nal := range nals {
		data = append(data, videoReadyU32(uint32(len(nal)))...)
		data = append(data, nal...)
	}
	return data
}

func videoReadyMatrix(data []byte) {
	binary.BigEndian.PutUint32(data[0:4], 1<<16)
	binary.BigEndian.PutUint32(data[16:20], 1<<16)
	binary.BigEndian.PutUint32(data[32:36], 1<<30)
}

func videoReadyBox(kind string, payload []byte) []byte {
	header := videoReadyU32(uint32(len(payload) + 8))
	return videoReadyJoin(header, []byte(kind), payload)
}

func videoReadyFullBox(kind string, version byte, flags uint32, payload []byte) []byte {
	return videoReadyBox(kind, videoReadyJoin([]byte{version, byte(flags >> 16), byte(flags >> 8), byte(flags)}, payload))
}

func videoReadyJoin(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func videoReadyU32(values ...uint32) []byte {
	data := make([]byte, 4*len(values))
	for i, value := range values {
		binary.BigEndian.PutUint32(data[i*4:i*4+4], value)
	}
	return data
}

func videoReadyU64(values ...uint64) []byte {
	data := make([]byte, 8*len(values))
	for i, value := range values {
		binary.BigEndian.PutUint64(data[i*8:i*8+8], value)
	}
	return data
}

func videoReadyInt32(value int32) *int32 { return &value }
