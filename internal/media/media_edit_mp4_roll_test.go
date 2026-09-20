package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestMediaEditMP4RollCanonicalAndDescriptionOrder(t *testing.T) {
	for _, order := range [][]string{{"sgpd", "sbgp"}, {"sbgp", "sgpd"}} {
		t.Run(order[0]+"_first", func(t *testing.T) {
			track := &mediaEditMP4Track{}
			for _, kind := range order {
				payload := mediaEditMP4RollTestDescription(-1)
				if kind == "sbgp" {
					payload = mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1})
				}
				if err := mediaEditMP4RollTestParse(t, kind, payload, track); err != nil {
					t.Fatalf("group preceding its sample description was rejected: %v", err)
				}
			}
			track.codec, track.handler = "mp4a", "soun"
			track.tableSamples, track.sizeSamples = 12, 12
			scanner := &mediaEditContainerScanner{ctx: context.Background()}
			proof, err := scanner.mp4RollProof(track)
			if err != nil || proof.Samples != 12 || proof.Distance != -1 {
				t.Fatalf("unexpected canonical roll proof: %#v, %v", proof, err)
			}
			if !track.rollDescriptions || !track.rollMapping || track.rollDistance == nil || *track.rollDistance != -1 || track.rollSamples != 12 {
				t.Fatalf("canonical roll fields were not retained: %#v", track)
			}
		})
	}
}

func TestMediaEditMP4RollEquivalentSegmentedMappings(t *testing.T) {
	cases := map[string][]mediaEditMP4RollTestRun{
		"single run":    {{12, 1}},
		"two runs":      {{7, 1}, {5, 1}},
		"several runs":  {{1, 1}, {1, 1}, {10, 1}},
		"sample budget": {{100_000_000, 1}},
		"entry budget":  make([]mediaEditMP4RollTestRun, 65_536),
	}
	for index := range cases["entry budget"] {
		cases["entry budget"][index] = mediaEditMP4RollTestRun{1, 1}
	}
	for name, runs := range cases {
		t.Run(name, func(t *testing.T) {
			track := &mediaEditMP4Track{codec: "mp4a", handler: "soun"}
			for _, run := range runs {
				track.tableSamples += uint64(run.samples)
			}
			track.sizeSamples = track.tableSamples
			if err := mediaEditMP4RollTestParse(t, "sgpd", mediaEditMP4RollTestDescription(-1), track); err != nil {
				t.Fatal(err)
			}
			if err := mediaEditMP4RollTestParse(t, "sbgp", mediaEditMP4RollTestMapping(runs...), track); err != nil {
				t.Fatalf("equivalent all-minus-one mapping was rejected: %v", err)
			}
			scanner := &mediaEditContainerScanner{ctx: context.Background()}
			proof, err := scanner.mp4RollProof(track)
			if err != nil || proof.Samples != track.tableSamples || proof.Distance != -1 {
				t.Fatalf("unexpected normalized roll proof: %#v, %v", proof, err)
			}
		})
	}
}

func TestMediaEditMP4RollRejectsDescriptionFields(t *testing.T) {
	change := func(update func([]byte)) []byte {
		payload := mediaEditMP4RollTestDescription(-1)
		update(payload)
		return payload
	}
	cases := map[string][]byte{
		"version zero":      change(func(data []byte) { data[0] = 0 }),
		"version two":       change(func(data []byte) { data[0] = 2 }),
		"high flag":         change(func(data []byte) { data[1] = 1 }),
		"low flag":          change(func(data []byte) { data[3] = 1 }),
		"other group":       change(func(data []byte) { copy(data[4:8], "prol") }),
		"sync group":        change(func(data []byte) { copy(data[4:8], "sync") }),
		"zero length":       change(func(data []byte) { binary.BigEndian.PutUint32(data[8:12], 0) }),
		"long length":       change(func(data []byte) { binary.BigEndian.PutUint32(data[8:12], 3) }),
		"no entries":        change(func(data []byte) { binary.BigEndian.PutUint32(data[12:16], 0) }),
		"two entries":       change(func(data []byte) { binary.BigEndian.PutUint32(data[12:16], 2) }),
		"zero distance":     mediaEditMP4RollTestDescription(0),
		"positive distance": mediaEditMP4RollTestDescription(1),
		"other preroll":     mediaEditMP4RollTestDescription(-2),
		"minimum distance":  mediaEditMP4RollTestDescription(-32768),
		"trailing bytes":    append(mediaEditMP4RollTestDescription(-1), 0),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if err := mediaEditMP4RollTestParse(t, "sgpd", payload, &mediaEditMP4Track{}); err == nil {
				t.Fatal("unproven roll description was accepted")
			}
		})
	}
	for length := 0; length < 18; length++ {
		t.Run(fmt.Sprintf("truncated_%d", length), func(t *testing.T) {
			if err := mediaEditMP4RollTestParse(t, "sgpd", mediaEditMP4RollTestDescription(-1)[:length], &mediaEditMP4Track{}); err == nil {
				t.Fatal("truncated roll description was accepted")
			}
		})
	}
}

func TestMediaEditMP4RollRejectsMappingFieldsAndReferences(t *testing.T) {
	change := func(update func([]byte)) []byte {
		payload := mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1})
		update(payload)
		return payload
	}
	tooManyRuns := make([]mediaEditMP4RollTestRun, 65_537)
	for index := range tooManyRuns {
		tooManyRuns[index] = mediaEditMP4RollTestRun{1, 1}
	}
	cases := map[string][]byte{
		"version one":        change(func(data []byte) { data[0] = 1 }),
		"high flag":          change(func(data []byte) { data[1] = 1 }),
		"low flag":           change(func(data []byte) { data[3] = 1 }),
		"other group":        change(func(data []byte) { copy(data[4:8], "prol") }),
		"no entries":         mediaEditMP4RollTestMapping(),
		"missing run":        change(func(data []byte) { binary.BigEndian.PutUint32(data[8:12], 2) }),
		"trailing bytes":     append(mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1}), 0),
		"zero samples":       mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{0, 1}),
		"zero group":         mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 0}),
		"absent group":       mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 2}),
		"fragment group":     mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 0x10001}),
		"later zero samples": mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1}, mediaEditMP4RollTestRun{0, 1}),
		"later other group":  mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{7, 1}, mediaEditMP4RollTestRun{5, 2}),
		"sample budget":      mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{100_000_001, 1}),
		"sum budget":         mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{60_000_000, 1}, mediaEditMP4RollTestRun{40_000_001, 1}),
		"large sample count": mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{0xffffffff, 1}),
		"entry budget":       mediaEditMP4RollTestMapping(tooManyRuns...),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if err := mediaEditMP4RollTestParse(t, "sbgp", payload, &mediaEditMP4Track{}); err == nil {
				t.Fatal("unproven roll mapping was accepted")
			}
		})
	}
	for length := 0; length < 20; length++ {
		t.Run(fmt.Sprintf("truncated_%d", length), func(t *testing.T) {
			if err := mediaEditMP4RollTestParse(t, "sbgp", mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1})[:length], &mediaEditMP4Track{}); err == nil {
				t.Fatal("truncated roll mapping was accepted")
			}
		})
	}
}

func TestMediaEditMP4RollRejectsDuplicateBoxes(t *testing.T) {
	for _, kind := range []string{"sgpd", "sbgp"} {
		t.Run(kind, func(t *testing.T) {
			track := &mediaEditMP4Track{}
			payload := mediaEditMP4RollTestDescription(-1)
			if kind == "sbgp" {
				payload = mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1})
			}
			if err := mediaEditMP4RollTestParse(t, kind, payload, track); err != nil {
				t.Fatalf("first group box was rejected: %v", err)
			}
			if err := mediaEditMP4RollTestParse(t, kind, payload, track); err == nil {
				t.Fatal("duplicate group box was accepted")
			}
		})
	}
}

func TestMediaEditMP4RollProofRejectsUnmatchedTrackSemantics(t *testing.T) {
	cases := map[string]func(*mediaEditMP4Track){
		"no boxes": func(track *mediaEditMP4Track) {
			track.rollDescriptions, track.rollMapping = false, false
			track.rollDistance, track.rollSamples = nil, 0
		},
		"missing description": func(track *mediaEditMP4Track) { track.rollDescriptions, track.rollDistance = false, nil },
		"missing mapping":     func(track *mediaEditMP4Track) { track.rollMapping, track.rollSamples = false, 0 },
		"missing distance":    func(track *mediaEditMP4Track) { track.rollDistance = nil },
		"changed distance":    func(track *mediaEditMP4Track) { *track.rollDistance = -2 },
		"zero mapping count":  func(track *mediaEditMP4Track) { track.rollSamples = 0 },
		"short mapping":       func(track *mediaEditMP4Track) { track.rollSamples = 11 },
		"long mapping":        func(track *mediaEditMP4Track) { track.rollSamples = 13 },
		"sample tables differ": func(track *mediaEditMP4Track) {
			track.sizeSamples = 13
		},
		"empty samples": func(track *mediaEditMP4Track) {
			track.tableSamples, track.sizeSamples, track.rollSamples = 0, 0, 0
		},
		"wrong handler": func(track *mediaEditMP4Track) { track.handler = "vide" },
		"video groups":  func(track *mediaEditMP4Track) { track.codec, track.handler = "avc1", "vide" },
		"text groups":   func(track *mediaEditMP4Track) { track.codec, track.handler = "tx3g", "sbtl" },
		"video description only": func(track *mediaEditMP4Track) {
			track.codec, track.handler = "avc1", "vide"
			track.rollMapping, track.rollSamples = false, 0
		},
		"video mapping only": func(track *mediaEditMP4Track) {
			track.codec, track.handler = "avc1", "vide"
			track.rollDescriptions, track.rollDistance = false, nil
		},
		"unknown codec": func(track *mediaEditMP4Track) { track.codec = "Opus" },
		"missing codec": func(track *mediaEditMP4Track) { track.codec = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			track := mediaEditMP4RollTestCompleteTrack()
			mutate(track)
			scanner := &mediaEditContainerScanner{ctx: context.Background()}
			if proof, err := scanner.mp4RollProof(track); err == nil {
				t.Fatalf("unproven track roll semantics were accepted: %#v", proof)
			}
		})
	}
	for _, codec := range []string{"avc1", "tx3g"} {
		t.Run(codec+"_without_groups", func(t *testing.T) {
			track := &mediaEditMP4Track{codec: codec, handler: "vide", tableSamples: 12, sizeSamples: 12}
			if codec == "tx3g" {
				track.handler = "sbtl"
			}
			scanner := &mediaEditContainerScanner{ctx: context.Background()}
			proof, err := scanner.mp4RollProof(track)
			if err != nil || proof.Samples != 0 || proof.Distance != 0 {
				t.Fatalf("group-free non-AAC track acquired a roll proof: %#v, %v", proof, err)
			}
		})
	}
}

func TestMediaEditMP4RollHonorsDescriptorExtentAndCancellation(t *testing.T) {
	for _, kind := range []string{"sgpd", "sbgp"} {
		payload := mediaEditMP4RollTestDescription(-1)
		if kind == "sbgp" {
			payload = mediaEditMP4RollTestMapping(mediaEditMP4RollTestRun{12, 1})
		}
		t.Run(kind+"_box_extent", func(t *testing.T) {
			scanner, box := mediaEditMP4RollTestScanner(t, kind, payload)
			box.end--
			if err := scanner.mp4SampleGroup(box, &mediaEditMP4Track{}); err == nil {
				t.Fatal("group parser read valid trailing bytes outside the declared box")
			}
		})
		t.Run(kind+"_descriptor_extent", func(t *testing.T) {
			scanner, box := mediaEditMP4RollTestScanner(t, kind, payload)
			scanner.size = box.end - 1
			if err := scanner.mp4SampleGroup(box, &mediaEditMP4Track{}); err == nil {
				t.Fatal("group parser read outside the admitted descriptor extent")
			}
		})
		t.Run(kind+"_canceled", func(t *testing.T) {
			scanner, box := mediaEditMP4RollTestScanner(t, kind, payload)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			scanner.ctx = ctx
			if err := scanner.mp4SampleGroup(box, &mediaEditMP4Track{}); !errors.Is(err, context.Canceled) {
				t.Fatalf("group parser did not preserve operation cancellation: %v", err)
			}
		})
	}
}

type mediaEditMP4RollTestRun struct {
	samples uint32
	group   uint32
}

func mediaEditMP4RollTestDescription(distance int16) []byte {
	data := make([]byte, 18)
	data[0] = 1
	copy(data[4:8], "roll")
	binary.BigEndian.PutUint32(data[8:12], 2)
	binary.BigEndian.PutUint32(data[12:16], 1)
	binary.BigEndian.PutUint16(data[16:18], uint16(distance))
	return data
}

func mediaEditMP4RollTestMapping(runs ...mediaEditMP4RollTestRun) []byte {
	data := make([]byte, 12+8*len(runs))
	copy(data[4:8], "roll")
	binary.BigEndian.PutUint32(data[8:12], uint32(len(runs)))
	for index, run := range runs {
		offset := 12 + 8*index
		binary.BigEndian.PutUint32(data[offset:offset+4], run.samples)
		binary.BigEndian.PutUint32(data[offset+4:offset+8], run.group)
	}
	return data
}

func mediaEditMP4RollTestCompleteTrack() *mediaEditMP4Track {
	distance := int16(-1)
	return &mediaEditMP4Track{codec: "mp4a", handler: "soun", tableSamples: 12, sizeSamples: 12,
		rollDistance: &distance, rollSamples: 12, rollDescriptions: true, rollMapping: true}
}

func mediaEditMP4RollTestParse(t *testing.T, kind string, payload []byte, track *mediaEditMP4Track) error {
	t.Helper()
	scanner, box := mediaEditMP4RollTestScanner(t, kind, payload)
	return scanner.mp4SampleGroup(box, track)
}

func mediaEditMP4RollTestScanner(t *testing.T, kind string, payload []byte) (*mediaEditContainerScanner, mediaEditMP4Box) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "roll-box-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	// Nonzero offsets and trailing bytes make the admitted box extent observable.
	data := append([]byte{0x91, 0x82, 0x73, 0x64, 0x55, 0x46, 0x37}, payload...)
	box := mediaEditMP4Box{kind: kind, start: 7, end: int64(len(data))}
	data = append(data, 0xa1, 0xb2, 0xc3)
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	return &mediaEditContainerScanner{ctx: context.Background(), file: file, size: int64(len(data))}, box
}
