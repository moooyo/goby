package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestMediaEditAACCodecDelayCanonicalRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name        string
		delay, rate uint64
		want        int64
		valid       bool
	}{
		{"zero", 0, 48000, 0, true},
		{"48 kHz AAC priming", 21333333, 48000, 1024, true},
		{"44.1 kHz AAC priming", 23219955, 44100, 1024, true},
		{"maximum sample count", uint64(math.MaxInt32) * 1000000000, 1, math.MaxInt32, true},
		{"one nanosecond below", 21333332, 48000, 0, false},
		{"one nanosecond above", 21333334, 48000, 0, false},
		{"rounds to zero", 1, 48000, 0, false},
		{"half sample is not reversible", 1953125, 256, 0, false},
		{"zero sample rate", 21333333, 0, 0, false},
		{"zero delay with invalid rate", 0, 0, 0, false},
		{"sample rate exceeds profile", 21333333, 768001, 0, false},
		{"sample count exceeds int32", (uint64(math.MaxInt32) + 1) * 1000000000, 1, 0, false},
		{"delay exceeds signed extent", uint64(math.MaxInt64) + 1, 48000, 0, false},
		{"unsigned multiplication overflow", math.MaxUint64, 768000, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := mediaEditAACDelaySamples(test.delay, test.rate)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("delay=%d rate=%d: got samples=%d error=%v, want %d", test.delay, test.rate, got, err, test.want)
				}
			} else if !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unprovable delay=%d rate=%d was admitted: samples=%d error=%v", test.delay, test.rate, got, err)
			}
		})
	}
}

func TestMediaEditMatroskaAudioSamplingFacts(t *testing.T) {
	for _, test := range []struct {
		name                 string
		frequency, output    float64
		channels             uint64
		hasFrequency, hasOut bool
		hasChannels, valid   bool
		wantRate, wantCount  uint64
	}{
		{name: "Matroska defaults", valid: true, wantRate: 8000, wantCount: 1},
		{name: "explicit mono", frequency: 48000, channels: 1, hasFrequency: true, hasChannels: true, valid: true, wantRate: 48000, wantCount: 1},
		{name: "separate output frequency", frequency: 24000, output: 48000, channels: 2, hasFrequency: true, hasOut: true, hasChannels: true, valid: true, wantRate: 48000, wantCount: 2},
		{name: "fractional frequency", frequency: 48000.5, hasFrequency: true},
		{name: "fractional output", frequency: 48000, output: 48000.5, hasFrequency: true, hasOut: true},
		{name: "zero frequency", hasFrequency: true},
		{name: "zero output", frequency: 48000, hasFrequency: true, hasOut: true},
		{name: "excessive frequency", frequency: 768001, hasFrequency: true},
		{name: "NaN output", frequency: 48000, output: math.NaN(), hasFrequency: true, hasOut: true},
		{name: "infinite frequency", frequency: math.Inf(1), hasFrequency: true},
		{name: "zero channels", hasChannels: true},
		{name: "excessive channels", channels: 65, hasChannels: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := mediaEditEBMLScope{count: map[uint64]int{}, uints: map[uint64]uint64{}, floats: map[uint64]float64{}}
			if test.hasFrequency {
				scope.count[0xB5], scope.floats[0xB5] = 1, test.frequency
			}
			if test.hasOut {
				scope.count[0x78B5], scope.floats[0x78B5] = 1, test.output
			}
			if test.hasChannels {
				scope.count[0x9F], scope.uints[0x9F] = 1, test.channels
			}
			got := mediaEditMatroskaAudioFromScope(scope)
			if got == nil || got.Known != test.valid {
				t.Fatalf("unexpected audio fact admission: %#v", got)
			}
			if test.valid && (got.SampleRate != test.wantRate || got.OutputSamplingFrequency != test.wantRate || got.Channels != test.wantCount) {
				t.Fatalf("unexpected effective audio sampling: %#v", got)
			}
			if test.valid && test.hasFrequency && got.SamplingFrequency != uint64(test.frequency) {
				t.Fatalf("raw sampling frequency was not retained: %#v", got)
			}
		})
	}
}

func TestMediaEditMatroskaCodecBindingRequiresConsistentType(t *testing.T) {
	for _, test := range []struct {
		name, codecID, codec, codecType string
		trackType                       uint64
		valid                           bool
	}{
		{"AAC audio", "A_AAC", "aac", "audio", 2, true},
		{"AVC video", "V_MPEG4/ISO/AVC", "h264", "video", 1, true},
		{"text subtitle", "S_TEXT/UTF8", "subrip", "subtitle", 17, true},
		{"SSA projection", "S_TEXT/SSA", "ass", "subtitle", 17, true},
		{"PCM audio", "A_PCM/INT/LIT", "pcm_s24le", "audio", 2, true},
		{"wrong codec name", "A_AAC", "mp3", "audio", 2, false},
		{"wrong probe type", "A_AAC", "aac", "video", 2, false},
		{"audio codec in video TrackEntry", "A_AAC", "aac", "video", 1, false},
		{"video codec in audio TrackEntry", "V_MPEG4/ISO/AVC", "h264", "audio", 2, false},
		{"PCM codec in subtitle TrackEntry", "A_PCM/INT/LIT", "pcm_s16le", "subtitle", 17, false},
		{"unsupported metadata TrackEntry", "S_TEXT/UTF8", "subrip", "subtitle", 33, false},
		{"unknown codec", "A_UNKNOWN", "unknown", "audio", 2, false},
		{"Opus remains outside this profile", "A_OPUS", "opus", "audio", 2, false},
		{"missing codec", "", "aac", "audio", 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			track := mediaEditMatroskaTrackProof{TrackType: test.trackType, CodecID: test.codecID}
			if got := mediaEditMatroskaCodecMatches(track, test.codec, test.codecType); got != test.valid {
				t.Fatalf("unexpected raw/probe codec binding: got %t, want %t", got, test.valid)
			}
		})
	}
}

func TestMediaEditMatroskaTrackBindingUsesEncounterOrder(t *testing.T) {
	proof, document := mediaEditMatroskaDelayTestInventory()
	bound, err := bindMediaEditMatroskaTracks(proof, document)
	if err != nil {
		t.Fatal(err)
	}
	if len(bound) != 4 || bound[0].Number != 40 || bound[1].Number != 3 || bound[2].Number != 99 || bound[3].Number != 7 {
		t.Fatalf("raw TrackEntry encounter order was lost: %#v", bound)
	}
	if bound[1].CodecDelayNS != 21333333 || bound[1].SampleRate != 48000 || bound[1].CodecPrivateSHA256 != mediaEditMatroskaDelayTestHash([]byte{1, 2, 3}) {
		t.Fatalf("AAC delay is not bound to the expected audio stream: %#v", bound[1])
	}

	// ffprobe has no usable Matroska stream ID; bogus presentation IDs must not
	// supersede the raw encounter order and the complete inventory checks.
	for _, stream := range document.Streams {
		stream["id"] = "0xffffffff"
	}
	if _, err := bindMediaEditMatroskaTracks(proof, document); err != nil {
		t.Fatalf("Matroska binding depended on ffprobe id: %v", err)
	}
}

func TestMediaEditMatroskaTrackBindingRejectsUnprovenInventory(t *testing.T) {
	for name, mutate := range map[string]func(*mediaEditContainerProof, *mediaEditDocument){
		"missing raw track":         func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks = p.MatroskaTracks[:3] },
		"missing probe stream":      func(_ *mediaEditContainerProof, d *mediaEditDocument) { d.Streams = d.Streams[:3] },
		"negative attachment count": func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaAttachmentCount = -1 },
		"duplicate index":           func(_ *mediaEditContainerProof, d *mediaEditDocument) { d.Streams[1]["index"] = json.Number("0") },
		"nonintegral index":         func(_ *mediaEditContainerProof, d *mediaEditDocument) { d.Streams[1]["index"] = json.Number("1.5") },
		"raw tracks sorted by number": func(p *mediaEditContainerProof, _ *mediaEditDocument) {
			p.MatroskaTracks[0], p.MatroskaTracks[1] = p.MatroskaTracks[1], p.MatroskaTracks[0]
		},
		"zero track number": func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[1].Number = 0 },
		"duplicate track number": func(p *mediaEditContainerProof, _ *mediaEditDocument) {
			p.MatroskaTracks[1].Number = p.MatroskaTracks[0].Number
		},
		"zero UID": func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[1].UID = 0 },
		"duplicate UID": func(p *mediaEditContainerProof, _ *mediaEditDocument) {
			p.MatroskaTracks[1].UID = p.MatroskaTracks[0].UID
		},
		"unsupported primary TrackType": func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[0].TrackType = 33 },
		"missing primary CodecID":       func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[0].CodecID = "" },
		"unknown primary codec":         func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[0].CodecID = "V_UNKNOWN" },
		"different probe codec":         func(_ *mediaEditContainerProof, d *mediaEditDocument) { d.Streams[1]["codec_name"] = "mp3" },
		"private size differs": func(_ *mediaEditContainerProof, d *mediaEditDocument) {
			d.Streams[1]["extradata_size"] = json.Number("4")
		},
		"private size absent": func(_ *mediaEditContainerProof, d *mediaEditDocument) { delete(d.Streams[1], "extradata_size") },
		"private hash differs": func(_ *mediaEditContainerProof, d *mediaEditDocument) {
			d.Streams[1]["extradata_hash"] = "SHA256:" + strings.Repeat("0", 64)
		},
		"private hash absent":     func(_ *mediaEditContainerProof, d *mediaEditDocument) { delete(d.Streams[1], "extradata_hash") },
		"raw private hash absent": func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[1].CodecPrivateSHA256 = "" },
		"delayed AAC has no private data": func(p *mediaEditContainerProof, d *mediaEditDocument) {
			p.MatroskaTracks[1].CodecPrivateBytes, p.MatroskaTracks[1].CodecPrivateSHA256 = 0, ""
			delete(d.Streams[1], "extradata_size")
			delete(d.Streams[1], "extradata_hash")
		},
		"audio facts unknown":   func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[1].audioKnown = false },
		"probe rate differs":    func(_ *mediaEditContainerProof, d *mediaEditDocument) { d.Streams[1]["sample_rate"] = "44100" },
		"probe rate absent":     func(_ *mediaEditContainerProof, d *mediaEditDocument) { delete(d.Streams[1], "sample_rate") },
		"probe channels differ": func(_ *mediaEditContainerProof, d *mediaEditDocument) { d.Streams[1]["channels"] = json.Number("2") },
		"probe channels absent": func(_ *mediaEditContainerProof, d *mediaEditDocument) { delete(d.Streams[1], "channels") },
		"initial padding differs": func(_ *mediaEditContainerProof, d *mediaEditDocument) {
			d.Streams[1]["initial_padding"] = json.Number("1023")
		},
		"initial padding absent": func(_ *mediaEditContainerProof, d *mediaEditDocument) { delete(d.Streams[1], "initial_padding") },
		"noncanonical raw delay": func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[1].CodecDelayNS++ },
		"nonzero seek preroll":   func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[1].SeekPreRollNS = 80000000 },
		"delay on video":         func(p *mediaEditContainerProof, _ *mediaEditDocument) { p.MatroskaTracks[0].CodecDelayNS = 21333333 },
		"delay on non-AAC audio": func(p *mediaEditContainerProof, d *mediaEditDocument) {
			p.MatroskaTracks[1].CodecID = "A_AC3"
			d.Streams[1]["codec_name"] = "ac3"
		},
		"Opus is not an AAC extension": func(p *mediaEditContainerProof, d *mediaEditDocument) {
			p.MatroskaTracks[1].CodecID = "A_OPUS"
			d.Streams[1]["codec_name"] = "opus"
		},
	} {
		t.Run(name, func(t *testing.T) {
			proof, document := mediaEditMatroskaDelayTestInventory()
			mutate(&proof, &document)
			if _, err := bindMediaEditMatroskaTracks(proof, document); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven raw/probe binding was admitted: %v", err)
			}
		})
	}
}

func TestMediaEditMatroskaTrackBindingCountsTrailingAttachments(t *testing.T) {
	for _, test := range []struct {
		name, codecType string
		attachedPic     string
		valid           bool
	}{
		{"ordinary attachment", "attachment", "0", true},
		{"picture attachment", "video", "1", true},
		{"extra primary video", "video", "0", false},
		{"extra primary audio", "audio", "0", false},
		{"extra primary subtitle", "subtitle", "0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			proof, document := mediaEditMatroskaDelayTestInventory()
			proof.MatroskaAttachmentCount = 1
			document.Streams = append(document.Streams, map[string]any{"index": json.Number("4"), "codec_type": test.codecType,
				"disposition": map[string]any{"attached_pic": json.Number(test.attachedPic)}})
			bound, err := bindMediaEditMatroskaTracks(proof, document)
			if test.valid {
				if err != nil || len(bound) != 4 {
					t.Fatalf("valid trailing attachment changed the raw track inventory: %#v, %v", bound, err)
				}
				if _, found := bound[4]; found {
					t.Fatal("attachment was treated as a TrackEntry")
				}
			} else if !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("extra primary stream was admitted as an attachment: %v", err)
			}
		})
	}
	t.Run("unaccounted attachment", func(t *testing.T) {
		proof, document := mediaEditMatroskaDelayTestInventory()
		document.Streams = append(document.Streams, map[string]any{"index": json.Number("4"), "codec_type": "attachment"})
		if _, err := bindMediaEditMatroskaTracks(proof, document); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("attachment without raw inventory was admitted: %v", err)
		}
	})
}

func TestMediaEditMatroskaDelayPreservationAllowsRegeneratedIdentities(t *testing.T) {
	source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
	want, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs)
	if err != nil || len(want) != 64 {
		t.Fatalf("unchanged retained delay was rejected: digest=%q error=%v", want, err)
	}
	for index, track := range candidate {
		track.Number, track.UID = uint64(900-index), uint64(800-index)
		candidate[index] = track
	}
	got, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs)
	if err != nil || got != want {
		t.Fatalf("container-local identity regeneration changed delay semantics: digest=%q error=%v", got, err)
	}
	for _, codecType := range []string{"attachment", "video"} {
		withAttachment := append(append([]MediaEditStreamEvidence{}, pairs...), MediaEditStreamEvidence{SourceIndex: 4, CandidateIndex: 3, CodecType: codecType})
		got, err = compareMediaEditMatroskaDelays(source, candidate, 2, withAttachment)
		if err != nil || got != want {
			t.Fatalf("a separately proven %s attachment changed track delay semantics: digest=%q error=%v", codecType, got, err)
		}
	}
}

func TestMediaEditMatroskaDelayPreservationRejectsChangedTracks(t *testing.T) {
	for name, change := range map[string]func(*mediaEditMatroskaTrackProof){
		"one nanosecond change":       func(track *mediaEditMatroskaTrackProof) { track.CodecDelayNS++ },
		"delay erased":                func(track *mediaEditMatroskaTrackProof) { track.CodecDelayNS = 0 },
		"different effective rate":    func(track *mediaEditMatroskaTrackProof) { track.SampleRate = 44100 },
		"different raw sampling rate": func(track *mediaEditMatroskaTrackProof) { track.SamplingFrequency = 24000 },
		"different raw output rate":   func(track *mediaEditMatroskaTrackProof) { track.OutputSamplingFrequency = 44100 },
		"different channel count":     func(track *mediaEditMatroskaTrackProof) { track.Channels = 2 },
		"different codec":             func(track *mediaEditMatroskaTrackProof) { track.CodecID = "A_AC3" },
		"different codec private":     func(track *mediaEditMatroskaTrackProof) { track.CodecPrivateSHA256 = strings.Repeat("0", 64) },
		"new seek preroll":            func(track *mediaEditMatroskaTrackProof) { track.SeekPreRollNS = 80000000 },
	} {
		t.Run(name, func(t *testing.T) {
			source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
			track := candidate[1]
			change(&track)
			candidate[1] = track
			if _, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("changed retained AAC delay semantics were admitted: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(map[int]mediaEditMatroskaTrackProof, map[int]mediaEditMatroskaTrackProof, *[]MediaEditStreamEvidence){
		"candidate tracks exchanged": func(_ map[int]mediaEditMatroskaTrackProof, after map[int]mediaEditMatroskaTrackProof, _ *[]MediaEditStreamEvidence) {
			after[0], after[1] = after[1], after[0]
		},
		"candidate track missing": func(_ map[int]mediaEditMatroskaTrackProof, after map[int]mediaEditMatroskaTrackProof, _ *[]MediaEditStreamEvidence) {
			delete(after, 1)
		},
		"source track missing": func(before map[int]mediaEditMatroskaTrackProof, _ map[int]mediaEditMatroskaTrackProof, _ *[]MediaEditStreamEvidence) {
			delete(before, 1)
		},
		"retained pair missing": func(_ map[int]mediaEditMatroskaTrackProof, _ map[int]mediaEditMatroskaTrackProof, pairs *[]MediaEditStreamEvidence) {
			*pairs = (*pairs)[:2]
		},
		"source pair repeated": func(_ map[int]mediaEditMatroskaTrackProof, _ map[int]mediaEditMatroskaTrackProof, pairs *[]MediaEditStreamEvidence) {
			(*pairs)[2].SourceIndex = 0
		},
		"candidate pair repeated": func(_ map[int]mediaEditMatroskaTrackProof, _ map[int]mediaEditMatroskaTrackProof, pairs *[]MediaEditStreamEvidence) {
			(*pairs)[2].CandidateIndex = 0
		},
		"pair references absent track": func(_ map[int]mediaEditMatroskaTrackProof, _ map[int]mediaEditMatroskaTrackProof, pairs *[]MediaEditStreamEvidence) {
			(*pairs)[1].CandidateIndex = 99
		},
	} {
		t.Run(name, func(t *testing.T) {
			source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
			mutate(source, candidate, &pairs)
			if _, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("incomplete retained-track mapping was admitted: %v", err)
			}
		})
	}
}

func TestMediaEditMatroskaDelayDigestBindsPreservedSemantics(t *testing.T) {
	for name, mutate := range map[string]func(*mediaEditMatroskaTrackProof){
		"delay": func(track *mediaEditMatroskaTrackProof) { track.CodecDelayNS = 42666667 },
		"effective sample rate": func(track *mediaEditMatroskaTrackProof) {
			track.SamplingFrequency, track.OutputSamplingFrequency, track.SampleRate = 96000, 96000, 96000
		},
		"raw sampling rate": func(track *mediaEditMatroskaTrackProof) { track.SamplingFrequency = 24000 },
		"channel count":     func(track *mediaEditMatroskaTrackProof) { track.Channels = 2 },
		"codec private": func(track *mediaEditMatroskaTrackProof) {
			track.CodecPrivateSHA256 = mediaEditMatroskaDelayTestHash([]byte{4, 5, 6})
		},
	} {
		t.Run(name, func(t *testing.T) {
			source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
			before, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs)
			if err != nil {
				t.Fatal(err)
			}
			for _, bound := range []map[int]mediaEditMatroskaTrackProof{source, candidate} {
				track := bound[1]
				mutate(&track)
				bound[1] = track
			}
			after, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs)
			if err != nil || len(after) != 64 || after == before {
				t.Fatalf("preservation digest did not bind the changed %s: before=%q after=%q error=%v", name, before, after, err)
			}
		})
	}
}

func TestMediaEditMatroskaDelayPreservationBindsSelectedSubtitle(t *testing.T) {
	for name, removedIndex := range map[string]int{"negative selection": -1, "video selection": 0, "audio selection": 1, "different subtitle": 3, "absent selection": 99} {
		t.Run(name, func(t *testing.T) {
			source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
			if _, err := compareMediaEditMatroskaDelays(source, candidate, removedIndex, pairs); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("a different selected removal was admitted: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*mediaEditMatroskaTrackProof){
		"selected track is audio":    func(track *mediaEditMatroskaTrackProof) { track.TrackType = 2 },
		"selected track has delay":   func(track *mediaEditMatroskaTrackProof) { track.CodecDelayNS = 1 },
		"selected track has preroll": func(track *mediaEditMatroskaTrackProof) { track.SeekPreRollNS = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
			removed := source[2]
			mutate(&removed)
			source[2] = removed
			if _, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven selected subtitle was admitted: %v", err)
			}
		})
	}
	for name, pair := range map[string]MediaEditStreamEvidence{
		"negative attachment indexes":         {SourceIndex: -1, CandidateIndex: -1, CodecType: "attachment"},
		"attachment aliases a primary stream": {SourceIndex: 0, CandidateIndex: 3, CodecType: "attachment"},
		"extra tail audio":                    {SourceIndex: 4, CandidateIndex: 3, CodecType: "audio"},
	} {
		t.Run(name, func(t *testing.T) {
			source, candidate, pairs := mediaEditMatroskaDelayTestRetained(t)
			pairs = append(pairs, pair)
			if _, err := compareMediaEditMatroskaDelays(source, candidate, 2, pairs); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven attachment mapping was admitted: %v", err)
			}
		})
	}
}

func TestMediaEditMatroskaDelayRawStructureAndProjection(t *testing.T) {
	// These bytes exercise EBML structure and probe binding only. Neither the
	// private bytes nor the one-byte block payload claim to be decodable AAC.
	private := []byte{1, 2, 3}
	extra := mediaEditMatroskaDelayTestTrackExtra(21333333, 48000, private)
	data := containerTestMKV(nil, extra, nil)
	proof, err := mediaEditMatroskaDelayTestReadProof(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.MatroskaTracks) != 2 || !mediaEditHasMatroskaCodecDelay(proof) {
		t.Fatalf("raw delayed track inventory was lost: %#v", proof.MatroskaTracks)
	}
	audio := proof.MatroskaTracks[0]
	if audio.Number != 1 || audio.UID != 11 || audio.TrackType != 2 || audio.CodecID != "A_AAC" || audio.CodecDelayNS != 21333333 || audio.SeekPreRollNS != 0 ||
		audio.SamplingFrequency != 48000 || audio.OutputSamplingFrequency != 48000 || audio.SampleRate != 48000 || audio.Channels != 1 ||
		audio.CodecPrivateBytes != 3 || audio.CodecPrivateSHA256 != mediaEditMatroskaDelayTestHash(private) || !audio.audioKnown {
		t.Fatalf("raw AAC structural evidence differs: %#v", audio)
	}
	document := mediaEditDocument{Streams: []map[string]any{
		{"index": json.Number("0"), "codec_type": "audio", "codec_name": "aac", "sample_rate": "48000", "channels": json.Number("1"),
			"initial_padding": json.Number("1024"), "extradata_size": json.Number("3"), "extradata_hash": "SHA256:" + mediaEditMatroskaDelayTestHash(private)},
		{"index": json.Number("1"), "codec_type": "subtitle", "codec_name": "subrip"},
	}}
	if _, err := bindMediaEditMatroskaTracks(proof, document); err != nil {
		t.Fatalf("raw structural evidence did not bind its explicit projection: %v", err)
	}

	for name, invalid := range map[string][]byte{
		"noncanonical nanoseconds": containerTestMKV(nil, mediaEditMatroskaDelayTestTrackExtra(21333334, 48000, private), nil),
		"non-AAC codec":            bytes.Replace(data, containerTestEBML(0x86, []byte("A_AAC")), containerTestEBML(0x86, []byte("A_AC3")), 1),
		"non-audio TrackType":      bytes.Replace(data, containerTestUint(0x83, 2), containerTestUint(0x83, 1), 1),
		"missing audio sampling":   containerTestMKV(nil, containerTestUint(0x56AA, 21333333), nil),
		"fractional sampling":      containerTestMKV(nil, mediaEditMatroskaDelayTestTrackExtra(21333333, 48000.5, private), nil),
		"nonzero seek preroll":     containerTestMKV(nil, append(append([]byte{}, extra...), containerTestUint(0x56BB, 80000000)...), nil),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := mediaEditMatroskaDelayTestReadProof(t, invalid); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven raw delay structure was admitted: %v", err)
			}
		})
	}
}

func TestMediaEditMatroskaCodecPrivateDigestUsesExactExtent(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "codec-private-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte{90, 91, 1, 2, 3, 92}); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := mediaEditMatroskaPrivateDigest(context.Background(), file, 2, 3)
	if err != nil || got != mediaEditMatroskaDelayTestHash([]byte{1, 2, 3}) {
		t.Fatalf("private digest did not bind the exact raw extent: %q, %v", got, err)
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 1 {
		t.Fatalf("private digest changed the borrowed descriptor position: %d, %v", offset, err)
	}
	for _, extent := range [][2]int64{{-1, 3}, {2, 0}, {2, (16 << 20) + 1}, {4, 3}} {
		if _, err := mediaEditMatroskaPrivateDigest(context.Background(), file, extent[0], extent[1]); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("invalid private extent %v was admitted: %v", extent, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mediaEditMatroskaPrivateDigest(ctx, file, 2, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("private digest ignored cancellation: %v", err)
	}
}

func mediaEditMatroskaDelayTestHash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func mediaEditMatroskaDelayTestInventory() (mediaEditContainerProof, mediaEditDocument) {
	videoPrivate := mediaEditMatroskaDelayTestHash([]byte{1, 100, 0, 31})
	audioPrivate := mediaEditMatroskaDelayTestHash([]byte{1, 2, 3})
	proof := mediaEditContainerProof{MatroskaTracks: []mediaEditMatroskaTrackProof{
		{Number: 40, UID: 400, TrackType: 1, CodecID: "V_MPEG4/ISO/AVC", CodecPrivateBytes: 4, CodecPrivateSHA256: videoPrivate},
		{Number: 3, UID: 30, TrackType: 2, CodecID: "A_AAC", CodecDelayNS: 21333333, SamplingFrequency: 48000, OutputSamplingFrequency: 48000,
			SampleRate: 48000, Channels: 1, CodecPrivateBytes: 3, CodecPrivateSHA256: audioPrivate, audioKnown: true},
		{Number: 99, UID: 990, TrackType: 17, CodecID: "S_TEXT/UTF8"},
		{Number: 7, UID: 70, TrackType: 17, CodecID: "S_TEXT/UTF8"},
	}}
	document := mediaEditDocument{Streams: []map[string]any{
		{"index": json.Number("0"), "codec_type": "video", "codec_name": "h264", "extradata_size": json.Number("4"), "extradata_hash": "SHA256:" + videoPrivate},
		{"index": json.Number("1"), "codec_type": "audio", "codec_name": "aac", "sample_rate": "48000", "channels": json.Number("1"),
			"initial_padding": json.Number("1024"), "extradata_size": json.Number("3"), "extradata_hash": "SHA256:" + audioPrivate},
		{"index": json.Number("2"), "codec_type": "subtitle", "codec_name": "subrip"},
		{"index": json.Number("3"), "codec_type": "subtitle", "codec_name": "subrip"},
	}}
	return proof, document
}

func mediaEditMatroskaDelayTestRetained(t *testing.T) (map[int]mediaEditMatroskaTrackProof, map[int]mediaEditMatroskaTrackProof, []MediaEditStreamEvidence) {
	t.Helper()
	proof, document := mediaEditMatroskaDelayTestInventory()
	source, err := bindMediaEditMatroskaTracks(proof, document)
	if err != nil {
		t.Fatal(err)
	}
	output := mediaEditContainerProof{}
	outputDocument := mediaEditDocument{}
	pairs := []MediaEditStreamEvidence{}
	for _, sourceIndex := range []int{0, 1, 3} {
		candidateIndex := len(output.MatroskaTracks)
		output.MatroskaTracks = append(output.MatroskaTracks, proof.MatroskaTracks[sourceIndex])
		stream := map[string]any{}
		for name, value := range document.Streams[sourceIndex] {
			stream[name] = value
		}
		stream["index"] = json.Number(strconv.Itoa(candidateIndex))
		outputDocument.Streams = append(outputDocument.Streams, stream)
		codecType, _ := stream["codec_type"].(string)
		pairs = append(pairs, MediaEditStreamEvidence{SourceIndex: sourceIndex, CandidateIndex: candidateIndex, CodecType: codecType})
	}
	candidate, err := bindMediaEditMatroskaTracks(output, outputDocument)
	if err != nil {
		t.Fatal(err)
	}
	return source, candidate, pairs
}

func mediaEditMatroskaDelayTestTrackExtra(delay uint64, rate float64, private []byte) []byte {
	var frequency [8]byte
	binary.BigEndian.PutUint64(frequency[:], math.Float64bits(rate))
	return bytes.Join([][]byte{containerTestUint(0x56AA, delay),
		containerTestEBML(0xE1, containerTestEBML(0xB5, frequency[:]), containerTestUint(0x9F, 1)),
		containerTestEBML(0x63A2, private)}, nil)
}

func mediaEditMatroskaDelayTestReadProof(t *testing.T, data []byte) (mediaEditContainerProof, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "matroska-delay-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	proof, proofErr := mediaEditReadContainerProof(context.Background(), file, int64(len(data)), "mkv")
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 2 {
		t.Fatalf("structural proof changed the borrowed descriptor position: %d, %v", position, err)
	}
	return proof, proofErr
}
