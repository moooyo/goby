package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type audioOggIntegrationRecord struct {
	Type      string `json:"type"`
	Stream    int    `json:"stream_index"`
	PTS       *int64 `json:"pts"`
	Duration  int64  `json:"duration"`
	Position  string `json:"pos"`
	PacketPos string `json:"pkt_pos"`
	Samples   int64  `json:"nb_samples"`
	SideData  []struct {
		Type    string `json:"side_data_type"`
		Skip    int64  `json:"skip_samples"`
		Discard int64  `json:"discard_padding"`
	} `json:"side_data_list"`
}

func TestProbeActualOggAudioPresentationMatchesDecodedSamples(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	fixtures := []struct {
		name         string
		inputRate    int
		inputSamples int64
		decodedRate  int
		wantSamples  int64
		encoding     []string
		shared       bool
		warmup       bool
	}{
		{
			name: "opus_20ms_frames", inputRate: 48000, inputSamples: 48048, decodedRate: 48000, wantSamples: 48048,
			encoding: []string{"-c:a", "libopus", "-b:a", "24k", "-frame_duration", "20"}, shared: true,
		},
		{
			name: "opus_one_millisecond", inputRate: 48000, inputSamples: 48, decodedRate: 48000, wantSamples: 48,
			encoding: []string{"-c:a", "libopus", "-b:a", "24k", "-frame_duration", "20"},
		},
		{
			name: "opus_lowdelay_2_5ms_frames", inputRate: 48000, inputSamples: 4800, decodedRate: 48000, wantSamples: 4800,
			encoding: []string{"-c:a", "libopus", "-b:a", "24k", "-application", "lowdelay", "-frame_duration", "2.5"}, shared: true,
		},
		{
			name: "opus_24000_input", inputRate: 24000, inputSamples: 2400, decodedRate: 48000, wantSamples: 4800,
			encoding: []string{"-c:a", "libopus", "-b:a", "24k"}, shared: true,
		},
		{
			name: "vorbis_100ms", inputRate: 44100, inputSamples: 4410, decodedRate: 44100, wantSamples: 4282,
			encoding: []string{"-c:a", "libvorbis", "-q:a", "2"}, shared: true, warmup: true,
		},
		{
			name: "vorbis_1001ms", inputRate: 44100, inputSamples: 44144, decodedRate: 44100, wantSamples: 44016,
			encoding: []string{"-c:a", "libvorbis", "-q:a", "2"}, shared: true, warmup: true,
		},
		{
			name: "flac_100ms", inputRate: 48000, inputSamples: 4800, decodedRate: 48000, wantSamples: 4800,
			encoding: []string{"-c:a", "flac"}, shared: true,
		},
		{
			name: "flac_1001ms", inputRate: 48000, inputSamples: 48048, decodedRate: 48000, wantSamples: 48048,
			encoding: []string{"-c:a", "flac"}, shared: true,
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+".ogg")
			encoding := append(append([]string(nil), fixture.encoding...), "-f", "ogg")
			audioProbeGenerateFixture(t, ffmpeg, path, fixture.inputRate, fixture.inputSamples, encoding...)
			info, stream := audioProbeAssertExact(t, ffprobe, ffmpeg, path, fixture.decodedRate, fixture.wantSamples)
			if info.ProbeVersion != CurrentProbeVersion || info.ProbeVersion < 4 {
				t.Fatalf("Ogg timing used an obsolete probe version: %d", info.ProbeVersion)
			}
			records := audioOggIntegrationRecords(t, ffprobe, path)
			packets, frames := audioOggIntegrationStreamRecords(records, stream.Index)
			var frameSamples int64
			positions := make(map[string]int)
			for _, packet := range packets {
				positions[packet.Position]++
			}
			shared := false
			for _, frame := range frames {
				frameSamples += frame.Samples
				shared = shared || positions[frame.PacketPos] > 1
			}
			if frameSamples != stream.AudioTiming.SampleCount || int64(len(packets)) != stream.AudioTiming.PacketCount {
				t.Fatalf("the independent packet/frame scan disagrees: %d packets, %d samples, timing=%+v", len(packets), frameSamples, stream.AudioTiming)
			}
			if fixture.shared && !shared {
				t.Fatal("the fixture did not exercise multiple sample-bearing packets at one Ogg page position")
			}
			if fixture.warmup {
				if len(packets) < 2 || len(frames) != len(packets)-1 ||
					audioOggIntegrationPTS(t, packets[0]) != 0 || packets[0].Duration != 128 ||
					audioOggIntegrationPTS(t, frames[0]) != audioOggIntegrationPTS(t, packets[1]) ||
					stream.AudioTiming.FirstPacketStartTicks != 0 {
					t.Fatalf("Vorbis did not exclude its first overlap packet: packets=%+v frames=%+v timing=%+v", packets[:min(2, len(packets))], frames[:min(1, len(frames))], stream.AudioTiming)
				}
				for _, side := range packets[0].SideData {
					if side.Skip != 0 || side.Discard != 0 {
						t.Fatalf("the Vorbis overlap fixture unexpectedly used sample-skip metadata: %+v", packets[0])
					}
				}
			}
		})
	}
}

func TestProbeActualOggMultipleAudioStreamsKeepIndependentOrigins(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "opus-and-vorbis.ogg")
	audioProbeRunFFmpeg(t, ffmpeg,
		"-f", "lavfi", "-i", "sine=frequency=800:sample_rate=48000",
		"-f", "lavfi", "-i", "sine=frequency=1200:sample_rate=44100",
		"-filter_complex", "[0:a]atrim=end_sample=4800,asetpts=PTS-STARTPTS[a0];[1:a]atrim=end_sample=44144,asetpts=PTS-STARTPTS[a1]",
		"-map", "[a0]", "-map", "[a1]", "-c:a:0", "libopus", "-b:a:0", "24k",
		"-c:a:1", "libvorbis", "-q:a:1", "2", "-f", "ogg", path)
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(context.Background(), path)
	if err != nil || !info.AudioDurationExact || info.AudioDurationReason != "" {
		t.Fatalf("multiplexed Ogg was not proven exact: %+v, %v", info, err)
	}
	records := audioOggIntegrationRecords(t, ffprobe, path)
	var streams []Stream
	var origin *big.Rat
	firstFrames := make(map[int]*big.Rat)
	for _, stream := range info.Streams {
		if stream.CodecType != "audio" {
			continue
		}
		streams = append(streams, stream)
		_, frames := audioOggIntegrationStreamRecords(records, stream.Index)
		if len(frames) == 0 {
			t.Fatalf("stream %d had no independently decoded frame", stream.Index)
		}
		base := audioOggIntegrationTimeBase(t, stream)
		first := new(big.Rat).Mul(new(big.Rat).SetInt64(audioOggIntegrationPTS(t, frames[0])), base)
		firstFrames[stream.Index] = first
		if origin == nil || first.Cmp(origin) < 0 {
			origin = first
		}
	}
	if len(streams) != 2 || streams[0].Codec == streams[1].Codec || streams[0].SampleRate == streams[1].SampleRate {
		t.Fatalf("the fixture must retain two different codecs and sample rates: %+v", streams)
	}
	if origin.Sign() != 0 || info.PresentationOriginTicks != 0 {
		t.Fatalf("the first track did not establish the common zero origin: %s, %+v", origin, info)
	}
	var totalEnd int64
	var delayedStart bool
	for _, stream := range streams {
		packets, frames := audioOggIntegrationStreamRecords(records, stream.Index)
		decoded := audioProbeDecodedSamples(t, ffmpeg, path, stream)
		var scanned int64
		for _, frame := range frames {
			scanned += frame.Samples
		}
		start := new(big.Rat).Sub(firstFrames[stream.Index], origin)
		end := new(big.Rat).Add(start, new(big.Rat).SetFrac64(decoded, int64(stream.SampleRate)))
		wantStart := audioOggIntegrationRationalTicks(start, false)
		wantEnd := audioOggIntegrationRationalTicks(end, true)
		timing := stream.AudioTiming
		if timing == nil || !timing.Exact || timing.SampleCount != decoded || scanned != decoded ||
			timing.PacketCount != int64(len(packets)) || timing.StartTicks != wantStart || timing.EndTicks != wantEnd {
			t.Fatalf("stream %d lost its independent sample timeline: decoded=%d scanned=%d start=%d end=%d timing=%+v", stream.Index, decoded, scanned, wantStart, wantEnd, timing)
		}
		delayedStart = delayedStart || wantStart > 0
		totalEnd = max(totalEnd, wantEnd)
	}
	if !delayedStart || info.DurationTicks != totalEnd {
		t.Fatalf("the shared duration did not preserve the Vorbis overlap origin and longest stream: %+v", info)
	}
}

func TestProbeActualOggRejectsChainedDamagedAndDiscontinuousInputs(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	first := filepath.Join(directory, "first.ogg")
	second := filepath.Join(directory, "second.ogg")
	audioProbeGenerateFixture(t, ffmpeg, first, 48000, 48048, "-c:a", "flac", "-page_duration", "20000", "-f", "ogg")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=1200:sample_rate=48000",
		"-af", "atrim=end_sample=4800,asetpts=PTS-STARTPTS", "-c:a", "flac", "-f", "ogg", second)
	firstData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	pages := audioOggIntegrationPages(t, firstData)
	if len(pages) < 5 {
		t.Fatal("the fixture needs several independent audio pages")
	}
	last := pages[len(pages)-1]
	t.Run("flac_chain", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "chained.ogg")
		data := append(append([]byte(nil), firstData...), secondData...)
		audioOggIntegrationWriteFile(t, path, data)
		// The demuxer can conceal the serial transition behind continuous FLAC PTS.
		audioOggIntegrationAssertUnproven(t, ffprobe, path)
	})
	t.Run("crc_damage", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad-crc.ogg")
		data := append([]byte(nil), firstData...)
		data[last.body] ^= 0x01
		audioOggIntegrationWriteFile(t, path, data)
		audioOggIntegrationAssertUnproven(t, ffprobe, path)
	})
	t.Run("truncated_page", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "truncated.ogg")
		audioOggIntegrationWriteFile(t, path, firstData[:last.end-5])
		audioOggIntegrationAssertUnproven(t, ffprobe, path)
	})
	for _, change := range []struct {
		name  string
		delta int64
	}{
		{name: "timestamp_gap", delta: 2400},
		{name: "timestamp_overlap", delta: -2400},
	} {
		t.Run(change.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), change.name+".ogg")
			data := append([]byte(nil), firstData...)
			// Alter only later page granules and repair CRCs, preserving coded samples.
			for _, page := range pages[len(pages)/2:] {
				granule := int64(binary.LittleEndian.Uint64(data[page.start+6 : page.start+14]))
				if granule < 0 || granule+change.delta < 0 {
					t.Fatalf("unexpected granule at page %d: %d", page.start, granule)
				}
				binary.LittleEndian.PutUint64(data[page.start+6:page.start+14], uint64(granule+change.delta))
				clear(data[page.start+22 : page.start+26])
				binary.LittleEndian.PutUint32(data[page.start+22:page.start+26], audioOggIntegrationCRC(data[page.start:page.end]))
			}
			audioOggIntegrationWriteFile(t, path, data)
			audioOggIntegrationAssertUnproven(t, ffprobe, path)
		})
	}
}

func TestProbeActualOggTinyVorbisPreservesUnprovenMetadata(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "tiny-vorbis.ogg")
	audioProbeGenerateFixture(t, ffmpeg, path, 44100, 44, "-c:a", "libvorbis", "-q:a", "2", "-f", "ogg")
	audioOggIntegrationAssertUnproven(t, ffprobe, path)
	records := audioOggIntegrationRecords(t, ffprobe, path)
	packets, _ := audioOggIntegrationStreamRecords(records, 0)
	if len(packets) < 2 || packets[len(packets)-1].Duration < 1<<31 {
		t.Fatalf("the tiny Vorbis fixture no longer exposes its final-duration underflow: %+v", packets)
	}
}

func TestProbeActualMatroskaAndWebMAudioRemainUnproven(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	fixtures := []struct {
		name     string
		rate     int
		samples  int64
		format   string
		encoding []string
	}{
		{name: "matroska_opus_quantized", rate: 48000, samples: 4800, format: "matroska", encoding: []string{"-c:a", "libopus", "-b:a", "24k"}},
		{name: "matroska_vorbis_quantized", rate: 44100, samples: 4410, format: "matroska", encoding: []string{"-c:a", "libvorbis", "-q:a", "2"}},
		{name: "matroska_flac_quantized", rate: 48000, samples: 4800, format: "matroska", encoding: []string{"-c:a", "flac"}},
		{name: "matroska_pcm_quantized", rate: 24000, samples: 2400, format: "matroska", encoding: []string{"-c:a", "pcm_s16le"}},
		{name: "matroska_aac_short_tail", rate: 24000, samples: 24, format: "matroska", encoding: []string{"-c:a", "aac", "-b:a", "24k"}},
		{name: "webm_opus_short_tail", rate: 48000, samples: 48, format: "webm", encoding: []string{"-c:a", "libopus", "-b:a", "24k"}},
		{name: "webm_vorbis_quantized", rate: 44100, samples: 44144, format: "webm", encoding: []string{"-c:a", "libvorbis", "-q:a", "2"}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+".mka")
			encoding := append(append([]string(nil), fixture.encoding...), "-f", fixture.format)
			audioProbeGenerateFixture(t, ffmpeg, path, fixture.rate, fixture.samples, encoding...)
			audioOggIntegrationAssertUnproven(t, ffprobe, path)
		})
	}
	t.Run("matroska_fixed_lacing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "fixed-lacing.mka")
		audioOggIntegrationWriteFile(t, path, audioOggIntegrationLacedMatroska())
		audioOggIntegrationAssertUnproven(t, ffprobe, path)
		records := audioOggIntegrationRecords(t, ffprobe, path)
		packets, frames := audioOggIntegrationStreamRecords(records, 0)
		if len(packets) != 3 || len(frames) != 3 || packets[0].Position != packets[1].Position ||
			packets[1].Position != packets[2].Position || frames[0].Samples != 240 || frames[1].Samples != 240 || frames[2].Samples != 240 {
			t.Fatalf("the fixture did not retain three PCM laces at one block position: packets=%+v frames=%+v", packets, frames)
		}
	})
}

func audioOggIntegrationRecords(t *testing.T, ffprobe, path string) []audioOggIntegrationRecord {
	t.Helper()
	output, err := runLimited(context.Background(), 20*time.Second, 1024*1024, ffprobe,
		"-v", "error", "-select_streams", "a", "-show_packets", "-show_frames",
		"-show_entries", "packet=stream_index,pts,duration,pos:packet_side_data=side_data_type,skip_samples,discard_padding:frame=stream_index,pts,nb_samples,pkt_pos",
		"-of", "json", path)
	if err != nil {
		t.Fatalf("read independent Ogg packet/frame facts: %v", err)
	}
	var document struct {
		Records []audioOggIntegrationRecord `json:"packets_and_frames"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	return document.Records
}

func audioOggIntegrationStreamRecords(records []audioOggIntegrationRecord, index int) (packets, frames []audioOggIntegrationRecord) {
	for _, record := range records {
		if record.Stream != index {
			continue
		}
		switch record.Type {
		case "packet":
			packets = append(packets, record)
		case "frame":
			if record.Samples > 0 {
				frames = append(frames, record)
			}
		}
	}
	return packets, frames
}

func audioOggIntegrationPTS(t *testing.T, record audioOggIntegrationRecord) int64 {
	t.Helper()
	if record.PTS == nil {
		t.Fatalf("the independent record has no presentation timestamp: %+v", record)
	}
	return *record.PTS
}

func audioOggIntegrationTimeBase(t *testing.T, stream Stream) *big.Rat {
	t.Helper()
	base, ok := new(big.Rat).SetString(stream.TimeBase)
	if !ok || base.Sign() <= 0 {
		t.Fatalf("invalid independent stream time base: %+v", stream)
	}
	return base
}

func audioOggIntegrationRationalTicks(seconds *big.Rat, ceil bool) int64 {
	value := new(big.Rat).Mul(seconds, new(big.Rat).SetInt64(TicksPerSecond))
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(value.Num(), value.Denom(), remainder)
	if remainder.Sign() != 0 {
		if ceil && value.Sign() > 0 {
			whole.Add(whole, big.NewInt(1))
		} else if !ceil && value.Sign() < 0 {
			whole.Sub(whole, big.NewInt(1))
		}
	}
	return whole.Int64()
}

func audioOggIntegrationAssertUnproven(t *testing.T, ffprobe, path string) {
	t.Helper()
	metadataJSON, metadataErr := runLimited(context.Background(), 20*time.Second, 1024*1024, ffprobe,
		"-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	var metadata Info
	if metadataErr == nil {
		metadata, metadataErr = parseProbe(metadataJSON)
	}
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(context.Background(), path)
	if err != nil {
		if metadataErr == nil {
			t.Fatalf("audio with readable metadata was rejected instead of retained as unproven: %v", err)
		}
		return
	}
	if info.AudioDurationExact || info.AudioDurationReason == "" {
		t.Fatalf("unsupported or damaged audio was marked exact: %+v", info)
	}
	for _, stream := range info.Streams {
		if stream.AudioTiming != nil && stream.AudioTiming.Exact {
			t.Fatalf("unproven audio retained an exact stream timing: %+v", stream)
		}
	}
	if metadataErr == nil {
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Container != metadata.Container || info.Size != stat.Size() || info.DurationTicks != metadata.DurationTicks || len(info.Streams) != len(metadata.Streams) {
			t.Fatalf("the original metadata was changed: original=%+v retained=%+v", metadata, info)
		}
		for index, stream := range info.Streams {
			original := metadata.Streams[index]
			if stream.Index != original.Index || stream.Codec != original.Codec || stream.SampleRate != original.SampleRate || stream.Channels != original.Channels {
				t.Fatalf("stream metadata was lost: original=%+v retained=%+v", original, stream)
			}
		}
	}
}

type audioOggIntegrationPage struct {
	start int
	body  int
	end   int
}

func audioOggIntegrationPages(t *testing.T, data []byte) []audioOggIntegrationPage {
	t.Helper()
	var pages []audioOggIntegrationPage
	for start := 0; start < len(data); {
		if len(data)-start < 27 || !bytes.Equal(data[start:start+4], []byte("OggS")) {
			t.Fatalf("generated fixture has no complete Ogg page at %d", start)
		}
		body := start + 27 + int(data[start+26])
		if body > len(data) {
			t.Fatal("generated fixture has an incomplete lacing table")
		}
		end := body
		for _, size := range data[start+27 : body] {
			end += int(size)
		}
		if end > len(data) {
			t.Fatal("generated fixture has an incomplete page payload")
		}
		pages = append(pages, audioOggIntegrationPage{start: start, body: body, end: end})
		start = end
	}
	return pages
}

func audioOggIntegrationCRC(data []byte) uint32 {
	var crc uint32
	for _, value := range data {
		crc ^= uint32(value) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func audioOggIntegrationWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if len(data) > 64*1024 {
		t.Fatalf("generated audio fixture exceeds its storage budget: %d bytes", len(data))
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func audioOggIntegrationLacedMatroska() []byte {
	element := func(id uint32, payload []byte) []byte {
		var identifier [4]byte
		binary.BigEndian.PutUint32(identifier[:], id)
		first := 0
		for first < 3 && identifier[first] == 0 {
			first++
		}
		result := append([]byte(nil), identifier[first:]...)
		if len(payload) < 127 {
			result = append(result, byte(len(payload))|0x80)
		} else {
			result = append(result, byte(len(payload)>>8)|0x40, byte(len(payload)))
		}
		return append(result, payload...)
	}
	unsigned := func(id uint32, value uint64) []byte {
		var payload [8]byte
		binary.BigEndian.PutUint64(payload[:], value)
		first := 0
		for first < 7 && payload[first] == 0 {
			first++
		}
		return element(id, payload[first:])
	}
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	header := element(0x1a45dfa3, join(unsigned(0x4286, 1), unsigned(0x42f7, 1), unsigned(0x42f2, 4),
		unsigned(0x42f3, 8), element(0x4282, []byte("matroska")), unsigned(0x4287, 4), unsigned(0x4285, 2)))
	var frequency, duration [8]byte
	binary.BigEndian.PutUint64(frequency[:], math.Float64bits(24000))
	binary.BigEndian.PutUint64(duration[:], math.Float64bits(30))
	info := element(0x1549a966, join(unsigned(0x2ad7b1, 1000000), element(0x4d80, []byte("Audio test")),
		element(0x5741, []byte("Audio test")), element(0x4489, duration[:])))
	audio := element(0xe1, join(element(0xb5, frequency[:]), unsigned(0x9f, 1), unsigned(0x6264, 16)))
	track := element(0xae, join(unsigned(0xd7, 1), unsigned(0x73c5, 1), unsigned(0x83, 2), unsigned(0x9c, 1),
		element(0x86, []byte("A_PCM/INT/LIT")), unsigned(0x23e383, 10000000), audio))
	tracks := element(0x1654ae6b, track)
	// Three fixed-size laces each carry 240 mono PCM samples, or 10 ms at 24 kHz.
	block := element(0xa3, append([]byte{0x81, 0, 0, 0x84, 2}, make([]byte, 3*240*2)...))
	cluster := element(0x1f43b675, join(unsigned(0xe7, 0), block))
	return join(header, element(0x18538067, join(info, tracks, cluster)))
}
