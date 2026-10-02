//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func generatedFramingTestPlan(audio bool) Plan {
	plan := commandPlan()
	plan.Container = "mp4"
	plan.DurationTicks, plan.SegmentSeconds = 3*ticksPerSecond, 1
	plan.HLS.SegmentType = "fmp4"
	plan.HLS.Window = HLSWindow{EndTicks: 2 * ticksPerSecond}
	if !audio {
		plan.AudioStreamIndex, plan.AudioCodec = -1, ""
		plan.AudioBitrate, plan.AudioChannels, plan.AudioSampleRate = 0, 0, 0
	}
	return plan
}

func generatedFramingTestIndex(id uint32, version byte, firstOffset uint64, sizes ...uint32) []byte {
	data := videoReadyU32(id, 16000)
	if version == 0 {
		data = append(data, videoReadyU32(0, uint32(firstOffset))...)
	} else {
		data = append(data, videoReadyU64(0, firstOffset)...)
	}
	data = append(data, 0, 0, byte(len(sizes)>>8), byte(len(sizes)))
	for _, size := range sizes {
		data = append(data, videoReadyU32(size, 1000, 0x90000000)...)
	}
	return videoReadyFullBox("sidx", version, 0, data)
}

func generatedFramingTestValidate(t *testing.T, plan Plan, init, segment []byte) error {
	t.Helper()
	var initialization *os.File
	if init != nil {
		initialization = generatedBoundsTestFile(t, init)
		if _, err := initialization.Seek(3, io.SeekStart); err != nil {
			t.Fatal(err)
		}
	}
	media := generatedBoundsTestFile(t, segment)
	if _, err := media.Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	err := ValidateGeneratedWindowFraming(plan, initialization, media)
	if offset, seekErr := media.Seek(0, io.SeekCurrent); seekErr != nil || offset != 5 {
		t.Fatalf("framing moved or closed borrowed media: offset=%d, err=%v", offset, seekErr)
	}
	if initialization != nil {
		if offset, seekErr := initialization.Seek(0, io.SeekCurrent); seekErr != nil || offset != 3 {
			t.Fatalf("framing moved or closed initialization: offset=%d, err=%v", offset, seekErr)
		}
	}
	return err
}

func TestGeneratedWindowFramingValidatesEveryFragmentAndCompleteMdat(t *testing.T) {
	for _, audio := range []bool{false, true} {
		for _, extended := range []bool{false, true} {
			t.Run(fmt.Sprintf("audio-%t-extended-%t", audio, extended), func(t *testing.T) {
				tracks := []videoReadyTrack{videoReadyH264Track(11)}
				runs := []videoReadyRun{{trackID: 11, samples: [][]byte{videoReadyIDR(), videoReadyIDR()}, defaultSize: true}}
				if audio {
					tracks = append(tracks, videoReadyAACTrack(22))
					runs = append(runs, videoReadyRun{trackID: 22, samples: [][]byte{{0x21, 0x10, 0x04, 0x60}}, defaultSize: true})
				}
				init := videoReadyInit(tracks...)
				first := videoReadyFragment(videoReadyFragmentOptions{extendedMDAT: extended}, runs...)
				second := videoReadyFragment(videoReadyFragmentOptions{extendedMDAT: extended, sequence: 2}, runs...)
				if err := generatedFramingTestValidate(t, generatedFramingTestPlan(audio), init, videoReadyJoin(first, second)); err != nil {
					t.Fatalf("complete multi-fragment output: %v", err)
				}
				brokenSecond := bytes.Clone(second)
				// A complete first fragment must not hide an incomplete later one.
				if err := generatedFramingTestValidate(t, generatedFramingTestPlan(audio), init, videoReadyJoin(first, brokenSecond[:len(brokenSecond)-1])); !errors.Is(err, ErrInvalidTimeline) {
					t.Fatalf("truncated second fragment accepted: %v", err)
				}
			})
		}
	}
}

func TestGeneratedWindowFramingSupportsStypAndCompleteSidxReferences(t *testing.T) {
	init := videoReadyInit(videoReadyH264Track(11), videoReadyAACTrack(22))
	fragment := videoReadyFragment(videoReadyFragmentOptions{},
		videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}},
		videoReadyRun{trackID: 22, samples: [][]byte{{0x21, 0x10, 0x04, 0x60}}})
	styp := videoReadyBox("styp", []byte("msdh\x00\x00\x00\x00msdhmsix"))
	for _, version := range []byte{0, 1} {
		audioIndex := generatedFramingTestIndex(22, version, 0, uint32(len(fragment)))
		videoIndex := generatedFramingTestIndex(11, version, uint64(len(audioIndex)), uint32(len(fragment)))
		if err := generatedFramingTestValidate(t, generatedFramingTestPlan(true), init, videoReadyJoin(styp, videoIndex, audioIndex, fragment)); err != nil {
			t.Fatalf("two-track sidx version %d: %v", version, err)
		}
	}
	second := videoReadyFragment(videoReadyFragmentOptions{sequence: 2}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})
	for _, index := range [][]byte{
		generatedFramingTestIndex(11, 0, 0, uint32(len(fragment)+len(second))),
		generatedFramingTestIndex(11, 1, 0, uint32(len(fragment)), uint32(len(second))),
	} {
		if err := generatedFramingTestValidate(t, generatedFramingTestPlan(true), init, videoReadyJoin(index, fragment, second)); err != nil {
			t.Fatalf("complete multi-fragment index: %v", err)
		}
	}
}

func TestGeneratedWindowFramingRejectsUnmatchedAndUnexplainedMedia(t *testing.T) {
	init := videoReadyInit(videoReadyH264Track(11))
	fragment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})
	moofLength := int(binary.BigEndian.Uint32(fragment[:4]))
	moof := fragment[:moofLength]
	mdat := fragment[moofLength:]
	zeroLength := bytes.Clone(fragment)
	binary.BigEndian.PutUint32(zeroLength[moofLength:moofLength+4], 0)
	unreferenced := videoReadyJoin(moof, videoReadyBox("mdat", append(bytes.Clone(mdat[8:]), 0)))
	badExtent := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}, offsetDelta: 1})
	badSecond := videoReadyFragment(videoReadyFragmentOptions{sequence: 2}, videoReadyRun{trackID: 99, samples: [][]byte{videoReadyIDR()}})
	unknownMoof := videoReadyBox("moof", videoReadyJoin(moof[8:], videoReadyBox("junk", nil)))
	for name, media := range map[string][]byte{
		"partial header":                append(bytes.Clone(fragment), 0, 1, 2),
		"zero mdat length":              zeroLength,
		"empty mdat":                    videoReadyJoin(moof, videoReadyBox("mdat", nil)),
		"missing mdat":                  moof,
		"mdat without moof":             mdat,
		"duplicate moof":                videoReadyJoin(moof, moof, mdat),
		"duplicate mdat":                videoReadyJoin(fragment, mdat),
		"unreferenced payload":          unreferenced,
		"extent into payload gap":       badExtent,
		"unknown second fragment track": videoReadyJoin(fragment, badSecond),
		"unknown moof metadata":         videoReadyJoin(unknownMoof, mdat),
		"unexpected trailer":            videoReadyJoin(fragment, videoReadyBox("free", nil)),
		"initialization in media":       videoReadyJoin(init, fragment),
		"late segment type":             videoReadyJoin(fragment, videoReadyBox("styp", []byte("msdh\x00\x00\x00\x00"))),
	} {
		t.Run(name, func(t *testing.T) {
			if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), init, media); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("invalid media framing accepted: %v", err)
			}
		})
	}
}

func TestGeneratedWindowFramingRejectsInvalidSidxAndOverflowLengths(t *testing.T) {
	init := videoReadyInit(videoReadyH264Track(11))
	fragment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})
	valid := generatedFramingTestIndex(11, 0, 0, uint32(len(fragment)))
	for name, change := range map[string]func([]byte){
		"version":                    func(data []byte) { data[8] = 2 },
		"flags":                      func(data []byte) { data[11] = 1 },
		"unknown track":              func(data []byte) { binary.BigEndian.PutUint32(data[12:16], 99) },
		"zero timescale":             func(data []byte) { binary.BigEndian.PutUint32(data[16:20], 0) },
		"reserved field":             func(data []byte) { data[28] = 1 },
		"zero count":                 func(data []byte) { data[31] = 0 },
		"hierarchical reference":     func(data []byte) { data[32] |= 0x80 },
		"zero reference size":        func(data []byte) { binary.BigEndian.PutUint32(data[32:36], 0) },
		"zero reference duration":    func(data []byte) { binary.BigEndian.PutUint32(data[36:40], 0) },
		"partial fragment reference": func(data []byte) { binary.BigEndian.PutUint32(data[32:36], uint32(len(fragment)-1)) },
		"offset into fragment":       func(data []byte) { binary.BigEndian.PutUint32(data[24:28], 1) },
		"oversized reference":        func(data []byte) { binary.BigEndian.PutUint32(data[32:36], math.MaxInt32) },
	} {
		t.Run(name, func(t *testing.T) {
			index := bytes.Clone(valid)
			change(index)
			if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), init, videoReadyJoin(index, fragment)); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("invalid sidx accepted: %v", err)
			}
		})
	}
	extendedIndex := generatedFramingTestIndex(11, 1, math.MaxUint64, uint32(len(fragment)))
	if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), init, videoReadyJoin(extendedIndex, fragment)); !errors.Is(err, ErrInvalidTimeline) {
		t.Fatalf("overflowing index offset: %v", err)
	}
	for name, header := range map[string][]byte{
		"short extended":    videoReadyJoin(videoReadyU32(1), []byte("moof"), videoReadyU32(0)),
		"overflow extended": videoReadyJoin(videoReadyU32(1), []byte("mdat"), videoReadyU64(math.MaxUint64)),
		"zero box":          videoReadyJoin(videoReadyU32(0), []byte("moof")),
		"short box":         videoReadyJoin(videoReadyU32(7), []byte("moof")),
	} {
		t.Run(name, func(t *testing.T) {
			if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), init, header); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("invalid box length: %v", err)
			}
		})
	}
}

func TestGeneratedWindowFramingRejectsInvalidInitializationAndResourceExhaustion(t *testing.T) {
	init := videoReadyInit(videoReadyH264Track(11))
	fragment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})
	for name, data := range map[string][]byte{
		"missing ftyp":               init[28:],
		"wrong movie":                videoReadyInit(videoReadyH264Track(99)),
		"duplicate initialization":   videoReadyJoin(init, init),
		"fragment in initialization": videoReadyJoin(init, fragment),
		"partial initialization":     init[:len(init)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), data, fragment); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("invalid initialization: %v", err)
			}
		})
	}
	ftypLength := int(binary.BigEndian.Uint32(init[:4]))
	oversizedMovie := videoReadyJoin(init[:ftypLength], videoReadyBox("moov", make([]byte, maxGeneratedFramingMetadata+1)))
	if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), oversizedMovie, fragment); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("metadata budget = %v", err)
	}
	var manyBoxes bytes.Buffer
	manyBoxes.Write(init[:ftypLength])
	for index := 0; index < maxGeneratedFramingBoxes; index++ {
		manyBoxes.Write(videoReadyBox("free", nil))
	}
	manyBoxes.Write(init[ftypLength:])
	if err := generatedFramingTestValidate(t, generatedFramingTestPlan(false), manyBoxes.Bytes(), fragment); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("box budget = %v", err)
	}
}

func generatedFramingTestTransport() []byte {
	data := make([]byte, 3*188)
	for offset := 0; offset < len(data); offset += 188 {
		data[offset], data[offset+2], data[offset+3] = 0x47, 0x10, 0x10
	}
	return data
}

func TestGeneratedWindowFramingValidatesTransportAdaptationFields(t *testing.T) {
	plan := generatedFramingTestPlan(false)
	plan.HLS.SegmentType = "mpegts"
	plan.Container = "ts"
	valid := generatedFramingTestTransport()
	if err := generatedFramingTestValidate(t, plan, nil, valid); err != nil {
		t.Fatal(err)
	}
	adaptationOnly := bytes.Clone(valid)
	adaptationOnly[3], adaptationOnly[4], adaptationOnly[5] = 0x20, 183, 0
	for index := 6; index < 188; index++ {
		adaptationOnly[index] = 0xff
	}
	if err := generatedFramingTestValidate(t, plan, nil, adaptationOnly); err != nil {
		t.Fatalf("complete adaptation-only packet = %v", err)
	}
	for name, change := range map[string]func([]byte){
		"bad sync":               func(data []byte) { data[188] = 0 },
		"transport error":        func(data []byte) { data[189] |= 0x80 },
		"reserved control":       func(data []byte) { data[191] = 0 },
		"adaptation overflow":    func(data []byte) { data[3], data[4] = 0x30, 183 },
		"short adaptation-only":  func(data []byte) { data[3], data[4] = 0x20, 1 },
		"partial PCR":            func(data []byte) { data[3], data[4], data[5] = 0x30, 1, 0x10 },
		"missing private length": func(data []byte) { data[3], data[4], data[5] = 0x30, 1, 0x02 },
		"private overflow":       func(data []byte) { data[3], data[4], data[5], data[6] = 0x30, 2, 0x02, 2 },
		"extension overflow":     func(data []byte) { data[3], data[4], data[5], data[6] = 0x30, 2, 0x01, 2 },
		"partial LTW":            func(data []byte) { data[3], data[4], data[5], data[6], data[7] = 0x30, 3, 0x01, 1, 0x80 },
		"invalid stuffing":       func(data []byte) { data[3], data[4], data[5], data[6] = 0x30, 2, 0, 0 },
	} {
		t.Run(name, func(t *testing.T) {
			data := bytes.Clone(valid)
			change(data)
			if err := generatedFramingTestValidate(t, plan, nil, data); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("invalid transport framing = %v", err)
			}
		})
	}
	if err := generatedFramingTestValidate(t, plan, nil, valid[:len(valid)-1]); !errors.Is(err, ErrInvalidTimeline) {
		t.Fatalf("partial transport packet = %v", err)
	}
}

func TestGeneratedWindowFramingRejectsInvalidBorrowedInputsAndUnsupportedSelection(t *testing.T) {
	plan := generatedFramingTestPlan(false)
	init := generatedBoundsTestFile(t, videoReadyInit(videoReadyH264Track(11)))
	if err := ValidateGeneratedWindowFraming(plan, init, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil segment = %v", err)
	}
	segment := generatedBoundsTestFile(t, videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}}))
	if err := ValidateGeneratedWindowFraming(plan, nil, segment); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("missing initialization = %v", err)
	}
	audioPlan := plan
	audioPlan.VideoStreamIndex, audioPlan.AudioStreamIndex, audioPlan.VideoCodec, audioPlan.AudioCodec = -1, 0, "", "aac"
	audioPlan.Width, audioPlan.Height, audioPlan.FrameRate, audioPlan.VideoBitrate = 0, 0, 0, 0
	if err := ValidateGeneratedWindowFraming(audioPlan, init, segment); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("unsupported audio-only fMP4 = %v", err)
	}
	if err := os.Link(segment.Name(), filepath.Join(t.TempDir(), "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGeneratedWindowFraming(plan, init, segment); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("hardlinked segment = %v", err)
	}
	sparse := generatedBoundsTestFile(t, []byte("sparse"))
	if err := os.Truncate(sparse.Name(), maxGeneratedBoundsInputBytes+1); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGeneratedWindowFraming(plan, init, sparse); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("input byte limit = %v", err)
	}
}

func TestGeneratedWindowFramingRealClosedHLS(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("set GOBY_FFMPEG for real media verification")
	}
	for _, format := range []string{"mpegts", "fmp4"} {
		t.Run(format, func(t *testing.T) {
			directory := t.TempDir()
			extension := "ts"
			if format == "fmp4" {
				extension = "m4s"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-threads", "1", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=12", "-t", "2", "-an", "-c:v", "libx264", "-preset", "ultrafast",
				"-bf", "0", "-g", "12", "-threads", "1", "-f", "hls", "-hls_time", "1", "-hls_list_size", "0", "-hls_flags", "independent_segments",
				"-hls_segment_type", format, "-hls_segment_filename", filepath.Join(directory, "seg-%03d."+extension), filepath.Join(directory, "main.m3u8"))
			command.Env, command.Dir = processEnvironment(), directory
			if data, err := command.CombinedOutput(); err != nil {
				t.Fatalf("create closed segments = %v: %s", err, data)
			}
			plan := generatedFramingTestPlan(false)
			plan.HLS.SegmentType = format
			if format == "mpegts" {
				plan.Container = "ts"
			}
			var initialization *os.File
			if format == "fmp4" {
				var err error
				initialization, err = os.Open(filepath.Join(directory, "init.mp4"))
				if err != nil {
					t.Fatal(err)
				}
				defer initialization.Close()
			}
			for index := 0; index < 2; index++ {
				path := filepath.Join(directory, fmt.Sprintf("seg-%03d.%s", index, extension))
				media, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				err = ValidateGeneratedWindowFraming(plan, initialization, media)
				_ = media.Close()
				if err != nil {
					t.Fatalf("closed %s segment %d = %v", format, index, err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				truncated := generatedBoundsTestFile(t, data[:len(data)-1])
				if err := ValidateGeneratedWindowFraming(plan, initialization, truncated); !errors.Is(err, ErrInvalidTimeline) {
					t.Fatalf("truncated actual %s segment %d = %v", format, index, err)
				}
			}
		})
	}
}
