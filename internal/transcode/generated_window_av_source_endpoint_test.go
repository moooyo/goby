package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"
)

type generatedAVSourceTestTrack struct {
	kind                                                                  string
	id                                                                    uint32
	originMovie, mediaTime, effectiveUnits, samples, delta, tableDuration int64
	channels                                                              int
}

func generatedAVSourceTestTracks(origin int64, channels int) []generatedAVSourceTestTrack {
	return []generatedAVSourceTestTrack{
		{kind: "vide", id: 1, originMovie: origin * 48000, effectiveUnits: 48000, samples: 48, delta: 1000, tableDuration: 48000},
		{kind: "soun", id: 2, originMovie: origin * 48000, mediaTime: 1024, effectiveUnits: 96000, samples: 95, delta: 1024, tableDuration: 97024, channels: channels},
	}
}

// These structural fixtures are not decodable payload evidence. Video declares
// 24 fps; AAC declares 95 coded blocks and a 768-unit final table duration.
func generatedAVSourceTestFixture(tracks []generatedAVSourceTestTrack) []byte {
	box, join, u32 := generatedEndpointTestBox, generatedEndpointTestJoin, generatedEndpointTestU32
	ftyp := box("ftyp", join([]byte("isom"), u32(512), []byte("isomiso2avc1mp41")))
	var payload []byte
	var offsets []uint32
	var duration int64
	for _, track := range tracks {
		offsets = append(offsets, uint32(len(ftyp)+8+len(payload)))
		payload = append(payload, bytes.Repeat([]byte{0x65}, int(track.samples*4))...)
		scale := int64(24000)
		if track.kind == "soun" {
			scale = 48000
		}
		end := track.originMovie + track.effectiveUnits*48000/scale
		if end > duration {
			duration = end
		}
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:16], 48000)
	binary.BigEndian.PutUint32(mvhd[16:20], uint32(duration))
	var parts [][]byte
	parts = append(parts, box("mvhd", mvhd))
	handler := func(kind string) []byte { body := make([]byte, 25); copy(body[8:12], kind); return box("hdlr", body) }
	for index, track := range tracks {
		scale := int64(24000)
		if track.kind == "soun" {
			scale = 48000
		}
		editDuration := track.effectiveUnits * 48000 / scale
		tkhd := make([]byte, 84)
		tkhd[3] = 3
		binary.BigEndian.PutUint32(tkhd[12:16], track.id)
		binary.BigEndian.PutUint32(tkhd[20:24], uint32(track.originMovie+editDuration))
		mdhd := make([]byte, 24)
		binary.BigEndian.PutUint32(mdhd[12:16], uint32(scale))
		binary.BigEndian.PutUint32(mdhd[16:20], uint32(track.tableDuration))
		var entry, mediaHeader []byte
		if track.kind == "vide" {
			avc := make([]byte, 78)
			binary.BigEndian.PutUint16(avc[6:8], 1)
			binary.BigEndian.PutUint16(avc[24:26], 160)
			binary.BigEndian.PutUint16(avc[26:28], 90)
			binary.BigEndian.PutUint16(avc[40:42], 1)
			binary.BigEndian.PutUint16(avc[74:76], 24)
			binary.BigEndian.PutUint16(avc[76:78], 0xffff)
			avcc := []byte{1, 66, 0, 10, 0xff, 0xe1, 0, 4, 0x67, 66, 0, 10, 1, 0, 2, 0x68, 0}
			entry = box("avc1", append(avc, box("avcC", avcc)...))
			mediaHeader = box("vmhd", join(u32(1), make([]byte, 8)))
		} else {
			aac := make([]byte, 28)
			binary.BigEndian.PutUint16(aac[6:8], 1)
			binary.BigEndian.PutUint16(aac[16:18], uint16(track.channels))
			binary.BigEndian.PutUint16(aac[18:20], 16)
			binary.BigEndian.PutUint32(aac[24:28], 48000<<16)
			asc := []byte{0x11, 0x88, 0x56, 0xe5, 0}
			if track.channels == 2 {
				asc[1] = 0x90
			}
			entry = box("mp4a", append(aac, box("esds", generatedAVSourceTestESDS(asc))...))
			mediaHeader = box("smhd", make([]byte, 8))
		}
		stts := u32(0, 1, uint32(track.samples), uint32(track.delta))
		if track.kind == "soun" && track.tableDuration != track.samples*track.delta {
			stts = u32(0, 2, uint32(track.samples-1), uint32(track.delta), 1, uint32(track.tableDuration-(track.samples-1)*track.delta))
		}
		stbl := box("stbl", join(box("stsd", join(u32(0, 1), entry)), box("stts", stts), box("stsz", u32(0, 4, uint32(track.samples))),
			box("stsc", u32(0, 1, 1, uint32(track.samples), 1)), box("stco", u32(0, 1, offsets[index]))))
		dref := box("dref", join(u32(0, 1), box("url ", u32(1))))
		minf := box("minf", join(mediaHeader, box("dinf", dref), stbl))
		mdia := box("mdia", join(box("mdhd", mdhd), handler(track.kind), minf))
		elst := u32(0, 1, uint32(editDuration), uint32(track.mediaTime), 0x00010000)
		if track.originMovie != 0 {
			elst = u32(0, 2, uint32(track.originMovie), 0xffffffff, 0x00010000, uint32(editDuration), uint32(track.mediaTime), 0x00010000)
		}
		parts = append(parts, box("trak", join(box("tkhd", tkhd), box("edts", box("elst", elst)), mdia)))
	}
	return join(ftyp, box("mdat", payload), box("moov", join(parts...)))
}

func generatedAVSourceTestESDS(asc []byte) []byte {
	descriptor := func(tag byte, body []byte) []byte { return append([]byte{tag, byte(len(body))}, body...) }
	decoder := append([]byte{0x40, 0x15, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, descriptor(5, asc)...)
	es := append([]byte{0, 2, 0}, descriptor(4, decoder)...)
	es = append(es, descriptor(6, []byte{2})...)
	return append(make([]byte, 4), descriptor(3, es)...)
}

func generatedAVSourceTestProjection(candidate generatedAVSourceCandidate, origin string) []byte {
	streams := make([]map[string]any, 0, len(candidate.tracks))
	for index, track := range candidate.tracks {
		start := generatedAVSourceRat(track.common.EffectiveFirst)
		start.Mul(start, new(big.Rat).SetInt64(track.common.MediaTimeScale))
		stream := map[string]any{"index": index, "id": fmt.Sprintf("0x%x", track.common.TrackID), "codec_type": "video", "codec_name": "h264",
			"time_base": fmt.Sprintf("1/%d", track.common.MediaTimeScale), "start_pts": start.Num().Int64()}
		if track.kind == "soun" {
			stream["codec_type"] = "audio"
			stream["codec_name"] = "aac"
			stream["sample_rate"] = "48000"
			stream["channels"] = track.audio.Channels
		}
		streams = append(streams, stream)
	}
	data, _ := json.Marshal(map[string]any{"streams": streams, "format": map[string]string{"start_time": origin}})
	return data
}

func generatedAVSourceTestCandidate(t *testing.T, tracks []generatedAVSourceTestTrack, video, audio int) (generatedAVSourceCandidate, []byte) {
	t.Helper()
	source := generatedAVSourceTestFixture(tracks)
	got, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), video, audio)
	if err != nil {
		t.Fatalf("structural A/V candidate failed: %v", err)
	}
	return got, source
}

func generatedAVSourceTestBoxNth(source []byte, kind string, nth int) int {
	// Walk only genuine fixture boxes. In particular, the avc1 compatible
	// brand inside ftyp is not a sample entry or a mutable ancestor header.
	prefixes := map[string]int{"moov": 0, "trak": 0, "mdia": 0, "minf": 0, "stbl": 0, "edts": 0, "dinf": 0,
		"udta": 0, "ilst": 0, "dref": 8, "stsd": 8, "avc1": 78, "mp4a": 28, "meta": 4}
	count, found := 0, -1
	var walk func([]byte, int)
	walk = func(data []byte, base int) {
		for offset := 0; len(data)-offset >= 8 && found < 0; {
			size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			if size < 8 || size > uint64(len(data)-offset) {
				return
			}
			current := string(data[offset+4 : offset+8])
			if current == kind {
				if count == nth {
					found = base + offset
					return
				}
				count++
			}
			if prefix, container := prefixes[current]; container && int(size) >= 8+prefix {
				walk(data[offset+8+prefix:offset+int(size)], base+offset+8+prefix)
			}
			offset += int(size)
		}
	}
	walk(source, 0)
	return found
}

func generatedAVSourceTestPut(source []byte, kind string, nth, bodyOffset int, value uint32) {
	pos := generatedAVSourceTestBoxNth(source, kind, nth) + 8 + bodyOffset
	binary.BigEndian.PutUint32(source[pos:pos+4], value)
}

func generatedAVSourceTestReplace(source []byte, kind string, nth int, replacement []byte) []byte {
	start := generatedAVSourceTestBoxNth(source, kind, nth)
	end := start + int(binary.BigEndian.Uint32(source[start:start+4]))
	delta := len(replacement) - (end - start)
	copySource := bytes.Clone(source)
	for _, ancestor := range []string{"moov", "trak", "mdia", "minf", "stbl", "stsd", "avc1", "mp4a", "dinf", "dref", "edts"} {
		for index := 0; ; index++ {
			pos := generatedAVSourceTestBoxNth(source, ancestor, index)
			if pos < 0 {
				break
			}
			size := int(binary.BigEndian.Uint32(source[pos : pos+4]))
			if pos < start && pos+size >= end {
				binary.BigEndian.PutUint32(copySource[pos:pos+4], uint32(size+delta))
			}
		}
	}
	return generatedEndpointTestJoin(copySource[:start], replacement, copySource[end:])
}

func TestGeneratedAVSourceCertificateKeepsIndependentClocksAndPriming(t *testing.T) {
	for _, channels := range []int{1, 2} {
		candidate, source := generatedAVSourceTestCandidate(t, generatedAVSourceTestTracks(2, channels), 0, 1)
		if candidate.certificate.DemuxOrigin != (GeneratedRational{}) {
			t.Fatal("metadata guessed a demux format origin")
		}
		got, err := bindGeneratedAVDemuxOrigin(candidate, generatedAVSourceTestProjection(candidate, "2.000000"))
		if err != nil {
			t.Fatal(err)
		}
		if got.SourceIdentity != "" || got.DemuxOrigin != (GeneratedRational{Num: 2, Den: 1}) || got.PresentationEpoch != (GeneratedRational{Num: 2, Den: 1}) || got.DurationTicks != 2*ticksPerSecond ||
			got.Video.FrameRate != 24 || got.Video.EffectiveFrames != 48 || got.Video.CodedEnd != (GeneratedRational{Num: 4, Den: 1}) || got.Audio.SampleCount != 95 ||
			got.Audio.CodedSamples != 97280 || got.Audio.HeadTrimSamples != 1024 || got.Audio.TailTrimSamples != 256 || got.Audio.EffectiveSamples != 96000 ||
			got.Audio.TableDurationUnits != 97024 || got.Audio.MediaTableEnd != (GeneratedRational{Num: 4, Den: 1}) || got.Audio.EffectiveEnd != (GeneratedRational{Num: 4, Den: 1}) ||
			got.Audio.CodedFirst != (GeneratedRational{Num: 742, Den: 375}) || got.Audio.CodedEnd != (GeneratedRational{Num: 1502, Den: 375}) || got.Audio.Channels != channels || got.Audio.BlockSamples != 1024 {
			t.Fatalf("A/V certificate lost distinct table/block/edit clocks: %+v", got)
		}
		moov := generatedAVSourceTestBoxNth(source, "moov", 0)
		if got.MetadataSHA256 != sha256.Sum256(source[moov:]) || got.AllExtentsSHA256 == ([32]byte{}) {
			t.Fatal("complete metadata or aggregate extent hash is absent")
		}
		// The public value remains comparable and independent of metadata bytes.
		copyGot := got
		source[generatedAVSourceTestBoxNth(source, "esds", 0)+12] = 0
		if got != copyGot {
			t.Fatal("mutable metadata aliased the certificate")
		}
	}
	tracks := generatedAVSourceTestTracks(2, 2)
	tracks = append([]generatedAVSourceTestTrack{{kind: "vide", id: 9, effectiveUnits: 48000, samples: 48, delta: 1000, tableDuration: 48000}}, tracks...)
	candidate, _ := generatedAVSourceTestCandidate(t, tracks, 1, 2)
	got, err := bindGeneratedAVDemuxOrigin(candidate, generatedAVSourceTestProjection(candidate, "0.000000"))
	if err != nil || got.DemuxOrigin != (GeneratedRational{Den: 1}) || got.PresentationEpoch != (GeneratedRational{Num: 2, Den: 1}) {
		t.Fatalf("unselected earlier track collapsed F into P: %+v %v", got, err)
	}
}

func TestGeneratedAVSourceWholeFrameVideoEditsAndExactDuration(t *testing.T) {
	tracks := generatedAVSourceTestTracks(0, 2)
	tracks[0].mediaTime = 24000
	tracks[0].effectiveUnits = 24000
	tracks[1].samples = 48
	tracks[1].tableDuration = 49024
	tracks[1].effectiveUnits = 48000
	candidate, _ := generatedAVSourceTestCandidate(t, tracks, 0, 1)
	if candidate.certificate.Video.HeadTrimFrames != 24 || candidate.certificate.Video.EffectiveFrames != 24 || candidate.certificate.DurationTicks != ticksPerSecond ||
		candidate.certificate.Video.CodedFirst != (GeneratedRational{Num: -1, Den: 1}) {
		t.Fatalf("whole-frame video edit lost its coded preroll: %+v", candidate.certificate)
	}
	tracks = generatedAVSourceTestTracks(0, 2)
	tracks[0].samples = 1
	tracks[0].tableDuration = 1000
	tracks[0].effectiveUnits = 1000
	tracks[1].samples = 3
	tracks[1].tableDuration = 3024
	tracks[1].effectiveUnits = 2000
	source := generatedAVSourceTestFixture(tracks)
	got, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1)
	if err == nil || got.certificate != (GeneratedAVSourceCertificate{}) {
		t.Fatalf("non-tick-exact selected duration was admitted: %+v %v", got, err)
	}
}

func TestGeneratedAVSourceNativeTrackHeaderSelectionRemainsExplicit(t *testing.T) {
	tracks := generatedAVSourceTestTracks(2, 2)
	tracks = append([]generatedAVSourceTestTrack{{kind: "vide", id: 9, effectiveUnits: 48000, samples: 48, delta: 1000, tableDuration: 48000}}, tracks...)
	source := generatedAVSourceTestFixture(tracks)
	generatedAVSourceTestPut(source, "tkhd", 1, 0, 2)
	group := generatedAVSourceTestBoxNth(source, "tkhd", 2) + 8 + 34
	binary.BigEndian.PutUint16(source[group:group+2], 1)
	candidate, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 1, 2)
	if err != nil || candidate.certificate.Video.HeaderFlags != 2 || candidate.certificate.Video.AlternateGroup != 0 ||
		candidate.certificate.Audio.HeaderFlags != 3 || candidate.certificate.Audio.AlternateGroup != 1 {
		t.Fatalf("explicit track selection lost native flags or alternate group: %+v %v", candidate, err)
	}
	got, err := bindGeneratedAVDemuxOrigin(candidate, generatedAVSourceTestProjection(candidate, "0.000000"))
	if err != nil || got.Video.StreamIndex != 1 || got.Audio.StreamIndex != 2 || got.PresentationEpoch != (GeneratedRational{Num: 2, Den: 1}) {
		t.Fatalf("disabled default track changed explicit demux selection: %+v %v", got, err)
	}
	for _, test := range []struct {
		name  string
		flags uint32
		kind  string
		group int16
	}{
		{"not in movie", 1, "vide", 0}, {"disabled and not in movie", 0, "vide", 0}, {"preview flag", 7, "vide", 0},
		{"unknown flag", 19, "vide", 0}, {"video alternate group", 3, "vide", 1}, {"unknown audio alternate group", 3, "soun", 2},
		{"negative alternate group", 3, "soun", -1}, {"unsupported handler", 3, "text", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := make([]byte, 84)
			binary.BigEndian.PutUint32(body[:4], test.flags)
			binary.BigEndian.PutUint32(body[12:16], 1)
			binary.BigEndian.PutUint32(body[20:24], 48000)
			binary.BigEndian.PutUint16(body[34:36], uint16(test.group))
			if _, _, _, _, err := generatedAVSourceTrackHeader(body, test.kind); err == nil {
				t.Fatal("unsupported track header escaped the narrow A/V mode")
			}
		})
	}
	versionOne := make([]byte, 96)
	versionOne[0] = 1
	versionOne[3] = 2
	binary.BigEndian.PutUint32(versionOne[20:24], 2)
	binary.BigEndian.PutUint64(versionOne[28:36], 96000)
	binary.BigEndian.PutUint16(versionOne[46:48], 1)
	if id, duration, flags, group, err := generatedAVSourceTrackHeader(versionOne, "soun"); err != nil || id != 2 || duration != 96000 || flags != 2 || group != 1 {
		t.Fatalf("version-one native A/V header changed its exact fields: %d %d %d %d %v", id, duration, flags, group, err)
	}
}

func TestGeneratedAVSourceRejectsAdversarialMetadata(t *testing.T) {
	base := generatedAVSourceTestFixture(generatedAVSourceTestTracks(0, 2))
	box, u32 := generatedEndpointTestBox, generatedEndpointTestU32
	mutations := map[string]func([]byte) []byte{
		"cross track payload alias": func(s []byte) []byte {
			first := binary.BigEndian.Uint32(s[generatedAVSourceTestBoxNth(s, "stco", 0)+16:])
			generatedAVSourceTestPut(s, "stco", 1, 8, first)
			return s
		},
		"duplicate track identity": func(s []byte) []byte { generatedAVSourceTestPut(s, "tkhd", 1, 12, 1); return s },
		"reordered video composition": func(s []byte) []byte {
			stbl := generatedAVSourceTestBoxNth(s, "stbl", 0)
			end := stbl + int(binary.BigEndian.Uint32(s[stbl:]))
			replacement := append(bytes.Clone(s[stbl:end]), box("ctts", u32(0, 1, 48, 1))...)
			binary.BigEndian.PutUint32(replacement[:4], uint32(len(replacement)))
			return generatedAVSourceTestReplace(s, "stbl", 0, replacement)
		},
		"fractional video cadence":      func(s []byte) []byte { generatedAVSourceTestPut(s, "stts", 0, 12, 1001); return s },
		"AAC short intermediate packet": func(s []byte) []byte { generatedAVSourceTestPut(s, "stts", 1, 12, 1000); return s },
		"AAC final short run":           func(s []byte) []byte { generatedAVSourceTestPut(s, "stts", 1, 16, 2); return s },
		"AAC media clock resampling":    func(s []byte) []byte { generatedAVSourceTestPut(s, "mdhd", 1, 12, 44100); return s },
		"AAC sample entry resampling":   func(s []byte) []byte { generatedAVSourceTestPut(s, "mp4a", 0, 24, 44100<<16); return s },
		"AAC channel conflict": func(s []byte) []byte {
			pos := generatedAVSourceTestBoxNth(s, "mp4a", 0) + 24
			binary.BigEndian.PutUint16(s[pos:pos+2], 1)
			return s
		},
		"AAC 960 sample flag": func(s []byte) []byte {
			return generatedAVSourceTestReplace(s, "esds", 0, box("esds", generatedAVSourceTestESDS([]byte{0x11, 0x94})))
		},
		"AAC SBR present": func(s []byte) []byte {
			return generatedAVSourceTestReplace(s, "esds", 0, box("esds", generatedAVSourceTestESDS([]byte{0x11, 0x90, 0x56, 0xe5, 0x80})))
		},
		"AAC PCE": func(s []byte) []byte {
			return generatedAVSourceTestReplace(s, "esds", 0, box("esds", generatedAVSourceTestESDS([]byte{0x11, 0x80})))
		},
		"AAC unknown descriptor": func(s []byte) []byte {
			esds := generatedAVSourceTestESDS([]byte{0x11, 0x90})
			esds[9] = 7
			return generatedAVSourceTestReplace(s, "esds", 0, box("esds", esds))
		},
		"AAC duplicate decoder descriptor": func(s []byte) []byte {
			esds := generatedAVSourceTestESDS([]byte{0x11, 0x90})
			decoder := bytes.Clone(esds[9:28])
			esds = append(esds[:28], append(decoder, esds[28:]...)...)
			esds[5] += byte(len(decoder))
			return generatedAVSourceTestReplace(s, "esds", 0, box("esds", esds))
		},
		"edit extends sample set":   func(s []byte) []byte { generatedAVSourceTestPut(s, "elst", 1, 8, 96001); return s },
		"edit dwell rate":           func(s []byte) []byte { generatedAVSourceTestPut(s, "elst", 1, 16, 0); return s },
		"video edit cuts frame":     func(s []byte) []byte { generatedAVSourceTestPut(s, "elst", 0, 12, 1); return s },
		"audio excessive head trim": func(s []byte) []byte { generatedAVSourceTestPut(s, "elst", 1, 12, 2048); return s },
		"different selected starts": func(s []byte) []byte {
			return generatedAVSourceTestReplace(s, "elst", 1, box("elst", u32(0, 2, 48000, 0xffffffff, 0x10000, 96000, 1024, 0x10000)))
		},
		"different selected ends": func(s []byte) []byte { generatedAVSourceTestPut(s, "elst", 1, 8, 95000); return s },
		"external data reference": func(s []byte) []byte { generatedAVSourceTestPut(s, "url ", 1, 0, 0); return s },
		"sample outside mdat":     func(s []byte) []byte { generatedAVSourceTestPut(s, "stco", 1, 8, uint32(len(s)-1)); return s },
		"ambiguous offset tables": func(s []byte) []byte {
			stbl := generatedAVSourceTestBoxNth(s, "stbl", 1)
			end := stbl + int(binary.BigEndian.Uint32(s[stbl:]))
			replacement := append(bytes.Clone(s[stbl:end]), box("co64", append(u32(0, 1), make([]byte, 8)...))...)
			binary.BigEndian.PutUint32(replacement[:4], uint32(len(replacement)))
			return generatedAVSourceTestReplace(s, "stbl", 1, replacement)
		},
		"unknown nested timing atom": func(s []byte) []byte {
			mdia := generatedAVSourceTestBoxNth(s, "mdia", 1)
			end := mdia + int(binary.BigEndian.Uint32(s[mdia:]))
			replacement := append(bytes.Clone(s[mdia:end]), box("elst", u32(0, 1, 96000, 0, 0x10000))...)
			binary.BigEndian.PutUint32(replacement[:4], uint32(len(replacement)))
			return generatedAVSourceTestReplace(s, "mdia", 1, replacement)
		},
		"unknown extra track handler": func(s []byte) []byte {
			pos := generatedAVSourceTestBoxNth(s, "hdlr", 1) + 16
			copy(s[pos:pos+4], "text")
			return s
		},
		"movie duration mismatch": func(s []byte) []byte { generatedAVSourceTestPut(s, "mvhd", 0, 16, 96001); return s },
		"truncated metadata":      func(s []byte) []byte { return s[:len(s)-1] },
		"fragmented top level":    func(s []byte) []byte { return append(s, box("moof", nil)...) },
		"duplicate movie": func(s []byte) []byte {
			pos := generatedAVSourceTestBoxNth(s, "moov", 0)
			return append(s, bytes.Clone(s[pos:])...)
		},
		"implicit top level size": func(s []byte) []byte { binary.BigEndian.PutUint32(s[:4], 0); return s },
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			source := mutation(bytes.Clone(base))
			got, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1)
			if err == nil || got.certificate != (GeneratedAVSourceCertificate{}) || len(got.tracks) != 0 {
				t.Fatalf("unsupported evidence returned a candidate: %+v %v", got, err)
			}
		})
	}
}

func TestGeneratedAVSourceRejectsSelectionAndBudgets(t *testing.T) {
	source := generatedAVSourceTestFixture(generatedAVSourceTestTracks(0, 2))
	for _, selection := range [][2]int{{-1, 1}, {0, 0}, {0, 8}, {1, 0}, {0, 2}} {
		if got, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), selection[0], selection[1]); err == nil || got.certificate != (GeneratedAVSourceCertificate{}) {
			t.Fatalf("invalid selection was admitted: %v %+v %v", selection, got, err)
		}
	}
	tracks := generatedAVSourceTestTracks(0, 2)
	for index := 2; index <= generatedAVSourceMaxTracks; index++ {
		extra := tracks[0]
		extra.id = uint32(index + 1)
		tracks = append(tracks, extra)
	}
	source = generatedAVSourceTestFixture(tracks)
	if _, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("track count escaped its bound: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseGeneratedMP4AVSourceMetadata(ctx, bytes.NewReader(source), int64(len(source)), 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost precedence: %v", err)
	}
	if _, err := parseGeneratedMP4AVSourceMetadata(nil, bytes.NewReader(source), int64(len(source)), 0, 1); err == nil {
		t.Fatal("nil context was admitted")
	}
	if _, err := parseGeneratedMP4AVSourceMetadata(context.Background(), nil, int64(len(source)), 0, 1); err == nil {
		t.Fatal("nil reader was admitted")
	}
}

func TestGeneratedAVSourceRejectsAdversarialDemuxProjection(t *testing.T) {
	candidate, _ := generatedAVSourceTestCandidate(t, generatedAVSourceTestTracks(0, 2), 0, 1)
	projection := generatedAVSourceTestProjection(candidate, "0.000000")
	var base map[string]any
	if err := json.Unmarshal(projection, &base); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"wrong track id":                   func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["id"] = "0x1" },
		"wrong index":                      func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["index"] = 0 },
		"wrong first clock":                func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["start_pts"] = -1024 },
		"missing first clock":              func(d map[string]any) { delete(d["streams"].([]any)[1].(map[string]any), "start_pts") },
		"wrong timebase":                   func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["time_base"] = "1/44100" },
		"wrong rate":                       func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["sample_rate"] = "44100" },
		"wrong channels":                   func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["channels"] = 1 },
		"wrong codec":                      func(d map[string]any) { d["streams"].([]any)[1].(map[string]any)["codec_name"] = "mp3" },
		"extra track":                      func(d map[string]any) { d["streams"] = append(d["streams"].([]any), d["streams"].([]any)[0]) },
		"program inventory":                func(d map[string]any) { d["programs"] = []any{map[string]any{}} },
		"missing format origin":            func(d map[string]any) { d["format"] = map[string]any{} },
		"rounded format origin":            func(d map[string]any) { d["format"].(map[string]any)["start_time"] = "0.00000001" },
		"origin outside bound":             func(d map[string]any) { d["format"].(map[string]any)["start_time"] = "99999999999.0" },
		"unrelated positive format origin": func(d map[string]any) { d["format"].(map[string]any)["start_time"] = "2.000000" },
		"unrelated negative format origin": func(d map[string]any) { d["format"].(map[string]any)["start_time"] = "-0.021333" },
		"unknown projection field":         func(d map[string]any) { d["streams"].([]any)[0].(map[string]any)["duration"] = 2 },
		"nonnull side data": func(d map[string]any) {
			d["streams"].([]any)[0].(map[string]any)["side_data_list"] = []any{map[string]any{"rotation": 0}}
		},
		"null side data": func(d map[string]any) { d["streams"].([]any)[0].(map[string]any)["side_data_list"] = nil },
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(projection, &document); err != nil {
				t.Fatal(err)
			}
			mutation(document)
			data, _ := json.Marshal(document)
			got, err := bindGeneratedAVDemuxOrigin(candidate, data)
			if err == nil || got != (GeneratedAVSourceCertificate{}) {
				t.Fatalf("invalid fresh projection returned evidence: %+v %v", got, err)
			}
		})
	}
	for _, data := range [][]byte{[]byte(strings.Replace(string(projection), `"start_time":"0.000000"`, `"start_time":"0.000000","start_time":"0.000000"`, 1)), bytes.Repeat([]byte{' '}, generatedAVSourceProjectionBytes+1)} {
		if got, err := bindGeneratedAVDemuxOrigin(candidate, data); err == nil || got != (GeneratedAVSourceCertificate{}) {
			t.Fatal("duplicate or oversized demux projection was admitted")
		}
	}
	base["streams"].([]any)[0].(map[string]any)["side_data_list"] = []any{map[string]any{}}
	data, _ := json.Marshal(base)
	if _, err := bindGeneratedAVDemuxOrigin(candidate, data); err != nil {
		t.Fatalf("bounded empty projected side data failed: %v", err)
	}
}

func generatedAVSourceTestRollBoxes(samples uint32) []byte {
	description := generatedEndpointTestJoin(generatedEndpointTestU32(0x01000000), []byte("roll"), generatedEndpointTestU32(2, 1), []byte{0xff, 0xff})
	mapping := generatedEndpointTestJoin(generatedEndpointTestU32(0), []byte("roll"), generatedEndpointTestU32(1, samples, 1))
	return generatedEndpointTestJoin(generatedEndpointTestBox("sgpd", description), generatedEndpointTestBox("sbgp", mapping))
}

func generatedAVSourceTestAppendTables(source []byte, nth int, extra []byte) []byte {
	start := generatedAVSourceTestBoxNth(source, "stbl", nth)
	end := start + int(binary.BigEndian.Uint32(source[start:]))
	replacement := append(bytes.Clone(source[start:end]), extra...)
	binary.BigEndian.PutUint32(replacement[:4], uint32(len(replacement)))
	return generatedAVSourceTestReplace(source, "stbl", nth, replacement)
}

func TestGeneratedAVSourceRollGroupsAreBoundedMetadataOnly(t *testing.T) {
	base := generatedAVSourceTestFixture(generatedAVSourceTestTracks(0, 2))
	source := generatedAVSourceTestAppendTables(base, 1, generatedAVSourceTestRollBoxes(95))
	candidate, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1)
	if err != nil || !candidate.certificate.Audio.RollGroupPresent || candidate.certificate.Audio.RollDistance != -1 || candidate.certificate.Audio.RollMappedSamples != 95 {
		t.Fatalf("complete native roll declaration was not preserved: %+v %v", candidate, err)
	}
	mutations := map[string]func([]byte) []byte{
		"unpaired description": func(s []byte) []byte { return generatedAVSourceTestReplace(s, "sbgp", 0, nil) },
		"unpaired mapping":     func(s []byte) []byte { return generatedAVSourceTestReplace(s, "sgpd", 0, nil) },
		"unknown grouping": func(s []byte) []byte {
			pos := generatedAVSourceTestBoxNth(s, "sgpd", 0) + 12
			copy(s[pos:pos+4], "prol")
			return s
		},
		"wrong roll distance": func(s []byte) []byte {
			pos := generatedAVSourceTestBoxNth(s, "sgpd", 0) + 24
			binary.BigEndian.PutUint16(s[pos:pos+2], 0xfffe)
			return s
		},
		"incomplete mapping":  func(s []byte) []byte { generatedAVSourceTestPut(s, "sbgp", 0, 12, 94); return s },
		"unknown group index": func(s []byte) []byte { generatedAVSourceTestPut(s, "sbgp", 0, 16, 2); return s },
		"duplicate mapping": func(s []byte) []byte {
			return generatedAVSourceTestAppendTables(s, 1, generatedEndpointTestBox("sbgp", generatedEndpointTestJoin(generatedEndpointTestU32(0), []byte("roll"), generatedEndpointTestU32(1, 95, 1))))
		},
		"video roll group": func(s []byte) []byte {
			return generatedAVSourceTestAppendTables(s, 0, generatedAVSourceTestRollBoxes(48))
		},
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			s := mutation(bytes.Clone(source))
			got, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(s), int64(len(s)), 0, 1)
			if err == nil || got.certificate != (GeneratedAVSourceCertificate{}) {
				t.Fatalf("ambiguous roll evidence was admitted: %+v %v", got, err)
			}
		})
	}
}

func TestGeneratedAVSourceAdmitsOnlyExactUneditedTablesAndBoundedAggregate(t *testing.T) {
	tracks := generatedAVSourceTestTracks(0, 1)
	tracks[0].samples = 192
	tracks[0].tableDuration = 192000
	tracks[0].effectiveUnits = 192000
	tracks[1].samples = 375
	tracks[1].mediaTime = 0
	tracks[1].tableDuration = 384000
	tracks[1].effectiveUnits = 384000
	source := generatedAVSourceTestFixture(tracks)
	source = generatedAVSourceTestReplace(source, "edts", 1, nil)
	source = generatedAVSourceTestReplace(source, "edts", 0, nil)
	source = generatedAVSourceTestReplace(source, "esds", 0, generatedEndpointTestBox("esds", generatedAVSourceTestESDS([]byte{0x11, 0x88})))
	candidate, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1)
	if err != nil || candidate.certificate.Audio.Edit.Explicit || candidate.certificate.Video.Edit.Explicit || candidate.certificate.Audio.HeadTrimSamples != 0 || candidate.certificate.DurationTicks != 8*ticksPerSecond {
		t.Fatalf("exact unedited native table profile failed: %+v %v", candidate, err)
	}
	tracks = generatedAVSourceTestTracks(0, 2)
	tracks[0].samples = 600000
	tracks[0].tableDuration = 600000000
	tracks[0].effectiveUnits = 600000000
	tracks[1].samples = 600000
	tracks[1].tableDuration = 614400000
	tracks[1].mediaTime = 1024
	tracks[1].effectiveUnits = 614398976
	source = generatedAVSourceTestFixture(tracks)
	got, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1)
	if !errors.Is(err, ErrTimelineLimit) || got.certificate != (GeneratedAVSourceCertificate{}) {
		t.Fatalf("aggregate declared samples escaped their global bound: %+v %v", got, err)
	}
}

type generatedAVSourceNoPayloadReader struct {
	source     []byte
	start, end int64
}

func (reader *generatedAVSourceNoPayloadReader) ReadAt(data []byte, offset int64) (int, error) {
	if offset < reader.end && offset+int64(len(data)) > reader.start {
		return 0, errors.New("payload read forbidden")
	}
	return bytes.NewReader(reader.source).ReadAt(data, offset)
}

func TestGeneratedAVSourceReadsMetadataWithoutClaimingPayload(t *testing.T) {
	source := generatedAVSourceTestFixture(generatedAVSourceTestTracks(0, 2))
	mdat := generatedAVSourceTestBoxNth(source, "mdat", 0)
	reader := &generatedAVSourceNoPayloadReader{source: source, start: int64(mdat + 8), end: int64(mdat) + int64(binary.BigEndian.Uint32(source[mdat:]))}
	if _, err := parseGeneratedMP4AVSourceMetadata(context.Background(), reader, int64(len(source)), 0, 1); err != nil {
		t.Fatalf("endpoint parser tried to treat coded payload as metadata: %v", err)
	}
	// Identical extents and metadata are not a payload-completeness certificate.
	one, _ := generatedAVSourceTestCandidate(t, generatedAVSourceTestTracks(0, 2), 0, 1)
	for index := reader.start; index < reader.end; index++ {
		source[index] = 0
	}
	two, err := parseGeneratedMP4AVSourceMetadata(context.Background(), bytes.NewReader(source), int64(len(source)), 0, 1)
	if err != nil || one.certificate != two.certificate {
		t.Fatalf("payload bytes improperly changed structural evidence: %v", err)
	}
	truncated := io.NewSectionReader(bytes.NewReader(source), 0, int64(len(source)-1))
	if _, err := parseGeneratedMP4AVSourceMetadata(context.Background(), truncated, int64(len(source)), 0, 1); err == nil {
		t.Fatal("short metadata reader returned a partial candidate")
	}
}
