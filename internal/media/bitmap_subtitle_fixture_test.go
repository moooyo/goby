package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"
)

// subtitleAuthoredGlyphBitmap uses independently authored 5x7 Latin glyphs for
// deterministic decoder tests without a host font dependency. Chinese OCR
// acceptance uses the real pinned Noto font in bitmap-subtitle-fixtures.py.
// This fixture must not be described as proof that an OCR engine recognized text.
func subtitleAuthoredGlyphBitmap() *image.NRGBA {
	glyphs := map[rune][7]string{
		'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
		'e': {"00000", "00000", "01110", "10001", "11111", "10000", "01111"},
		'l': {"01100", "00100", "00100", "00100", "00100", "00100", "01110"},
		'o': {"00000", "00000", "01110", "10001", "10001", "10001", "01110"},
		'w': {"00000", "00000", "10001", "10001", "10101", "10101", "01010"},
		'r': {"00000", "00000", "10110", "11001", "10000", "10000", "10000"},
		'd': {"00001", "00001", "01101", "10011", "10001", "10001", "01111"},
		' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
	}
	const text, scale = "Hello world", 2
	bitmap := image.NewNRGBA(image.Rect(0, 0, (len(text)*6-1)*scale, 7*scale))
	for position, character := range text {
		for row, pixels := range glyphs[character] {
			for column, pixel := range pixels {
				if pixel != '1' {
					continue
				}
				for y := 0; y < scale; y++ {
					for x := 0; x < scale; x++ {
						bitmap.SetNRGBA((position*6+column)*scale+x, row*scale+y, color.NRGBA{255, 255, 255, 255})
					}
				}
			}
		}
	}
	return bitmap
}

type subtitleFixturePGSReference struct {
	id     uint16
	x, y   uint16
	forced bool
}

// subtitleFixturePGSPackets includes clear events and an actual overlapping
// ordinary/forced presentation. Each packet is a complete display set. Setting
// withSUPHeaders also includes every real PG/PTS/DTS segment envelope; packet
// PTS remains expressed in Goby's 100 ns timebase, as after demuxing.
func subtitleFixturePGSPackets(withSUPHeaders bool) []bitmapSubtitlePacket {
	type display struct {
		milliseconds int64
		references   []subtitleFixturePGSReference
	}
	one := subtitleFixturePGSReference{id: 1, x: 21, y: 40}
	two := subtitleFixturePGSReference{id: 2, x: 93, y: 100, forced: true}
	displays := []display{
		{milliseconds: 0},
		{milliseconds: 1024, references: []subtitleFixturePGSReference{one}},
		{milliseconds: 2560, references: []subtitleFixturePGSReference{one, two}},
		{milliseconds: 4096, references: []subtitleFixturePGSReference{two}},
		{milliseconds: 5632},
	}
	bitmap := subtitleAuthoredGlyphBitmap()
	var rle []byte
	for y := 0; y < bitmap.Bounds().Dy(); y++ {
		for x := 0; x < bitmap.Bounds().Dx(); x++ {
			if bitmap.NRGBAAt(x, y).A != 0 {
				rle = append(rle, 1)
			} else {
				// A single transparent pixel; deliberately avoid sharing the
				// production decoder's run-length implementation.
				rle = append(rle, 0, 1)
			}
		}
		rle = append(rle, 0, 0)
	}
	var packets []bitmapSubtitlePacket
	for number, event := range displays {
		var encoded bytes.Buffer
		segment := func(kind byte, payload []byte) {
			if withSUPHeaders {
				header := make([]byte, 10)
				copy(header, "PG")
				binary.BigEndian.PutUint32(header[2:6], uint32(event.milliseconds*90))
				binary.BigEndian.PutUint32(header[6:10], uint32(event.milliseconds*90))
				encoded.Write(header)
			}
			encoded.WriteByte(kind)
			encoded.Write([]byte{byte(len(payload) >> 8), byte(len(payload))})
			encoded.Write(payload)
		}
		presentation := make([]byte, 11)
		binary.BigEndian.PutUint16(presentation[0:2], 320)
		binary.BigEndian.PutUint16(presentation[2:4], 192)
		presentation[4], presentation[7] = 0x20, 0x80
		binary.BigEndian.PutUint16(presentation[5:7], uint16(number))
		presentation[10] = byte(len(event.references))
		for _, reference := range event.references {
			entry := make([]byte, 8)
			binary.BigEndian.PutUint16(entry[0:2], reference.id)
			if reference.forced {
				entry[3] = 0x40
			}
			binary.BigEndian.PutUint16(entry[4:6], reference.x)
			binary.BigEndian.PutUint16(entry[6:8], reference.y)
			presentation = append(presentation, entry...)
		}
		segment(0x16, presentation)
		if len(event.references) != 0 {
			segment(0x17, []byte{1, 0, 0, 0, 0, 0, 1, 64, 0, 192})
			segment(0x14, []byte{0, 0, 0, 16, 128, 128, 0, 1, 235, 128, 128, 255})
			for _, reference := range event.references {
				object := make([]byte, 11)
				binary.BigEndian.PutUint16(object[0:2], reference.id)
				object[3] = 0xc0
				length := len(rle) + 4
				object[4], object[5], object[6] = byte(length>>16), byte(length>>8), byte(length)
				binary.BigEndian.PutUint16(object[7:9], uint16(bitmap.Bounds().Dx()))
				binary.BigEndian.PutUint16(object[9:11], uint16(bitmap.Bounds().Dy()))
				segment(0x15, append(object, rle...))
			}
		}
		segment(0x80, nil)
		packets = append(packets, bitmapSubtitlePacket{PTS: event.milliseconds * 10000, Data: encoded.Bytes()})
	}
	return packets
}

// subtitleFixtureDVDPacket produces a real two-field DVD SPU packet. Its
// optional start command is at zero and its stop command is exactly 270 units
// of the 90 kHz/1024 clock later (3.072 seconds). Every nibble is a one-pixel run,
// giving the decoder an independently constructed pixel/field-order oracle.
func subtitleFixtureDVDPacket(forced bool) (bitmapSubtitlePacket, []byte) {
	bitmap := subtitleAuthoredGlyphBitmap()
	var fields [2][]byte
	for field := range fields {
		for y := field; y < bitmap.Bounds().Dy(); y += 2 {
			for x := 0; x < bitmap.Bounds().Dx(); x += 2 {
				first, second := byte(4), byte(4)
				if bitmap.NRGBAAt(x, y).A != 0 {
					first = 5
				}
				if bitmap.NRGBAAt(x+1, y).A != 0 {
					second = 5
				}
				fields[field] = append(fields[field], first<<4|second)
			}
		}
	}
	coordinate := func(first, last int) []byte {
		return []byte{byte(first >> 4), byte(first<<4) | byte(last>>8), byte(last)}
	}
	appendWord := func(buffer []byte, value int) []byte {
		return append(buffer, byte(value>>8), byte(value))
	}
	control := 4 + len(fields[0]) + len(fields[1])
	commands := []byte{0x03, 0x32, 0x10, 0x04, 0x00, 0xf0, 0x05}
	commands = append(commands, coordinate(21, 21+bitmap.Bounds().Dx()-1)...)
	commands = append(commands, coordinate(40, 40+bitmap.Bounds().Dy()-1)...)
	commands = append(commands, 0x06)
	commands = appendWord(commands, 4)
	commands = appendWord(commands, 4+len(fields[0]))
	startCommand := byte(0x01)
	if forced {
		startCommand = 0x00
	}
	commands = append(commands, startCommand, 0xff)
	stop := control + 4 + len(commands)
	packet := appendWord([]byte{0, 0}, control)
	packet = append(packet, fields[0]...)
	packet = append(packet, fields[1]...)
	packet = appendWord(packet, 0)
	packet = appendWord(packet, stop)
	packet = append(packet, commands...)
	packet = appendWord(packet, 270)
	packet = appendWord(packet, stop)
	packet = append(packet, 0x02, 0xff)
	binary.BigEndian.PutUint16(packet[0:2], uint16(len(packet)))
	private := []byte("size: 320x192\npalette: 000000, ffffff, 000000, 808080, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000\n")
	return bitmapSubtitlePacket{PTS: 10240000, Duration: 30720000, Data: packet}, private
}

func TestBitmapSubtitleAuthoredGlyphPGSTimeline(t *testing.T) {
	for _, withSUPHeaders := range []bool{false, true} {
		name := "demuxed"
		if withSUPHeaders {
			name = "SUP"
		}
		t.Run(name, func(t *testing.T) {
			got, warnings, err := decodePGSSubtitles(context.Background(), subtitleFixturePGSPackets(withSUPHeaders), bitmapSubtitleLimits(70000000))
			if err != nil || len(warnings) != 0 || len(got) != 4 {
				t.Fatalf("decode authored display events: count=%d warnings=%v error=%v", len(got), warnings, err)
			}
			want := []struct {
				start, end int64
				x, y       int
				forced     bool
			}{
				{10240000, 25600000, 21, 40, false},
				{25600000, 40960000, 21, 40, false},
				{25600000, 40960000, 93, 100, true},
				{40960000, 56320000, 93, 100, true},
			}
			bitmap := subtitleAuthoredGlyphBitmap()
			for index, expected := range want {
				cue := got[index]
				if cue.StartTicks != expected.start || cue.EndTicks != expected.end ||
					cue.X != expected.x || cue.Y != expected.y || cue.Forced != expected.forced {
					t.Errorf("cue %d lost exact timing, coordinates, or forced membership: %+v", index, cue)
				}
				if cue.Image == nil || cue.Image.Bounds() != bitmap.Bounds() || !bytes.Equal(cue.Image.Pix, bitmap.Pix) {
					t.Errorf("cue %d differs from the authored text pixels", index)
				}
			}
		})
	}
}

func TestBitmapSubtitleAuthoredGlyphDVDFieldsAndControlClock(t *testing.T) {
	for _, forced := range []bool{false, true} {
		name := "ordinary"
		if forced {
			name = "forced"
		}
		t.Run(name, func(t *testing.T) {
			packet, private := subtitleFixtureDVDPacket(forced)
			got, warnings, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{packet}, private, bitmapSubtitleLimits(70000000))
			if err != nil || len(warnings) != 0 || len(got) != 1 {
				t.Fatalf("decode authored DVD fields: count=%d warnings=%v error=%v", len(got), warnings, err)
			}
			cue := got[0]
			if cue.StartTicks != 10240000 || cue.EndTicks != 40960000 || cue.X != 21 || cue.Y != 40 || cue.Forced != forced {
				t.Fatalf("DVD control clock or forced flag changed: %+v", cue)
			}
			bitmap := subtitleAuthoredGlyphBitmap()
			if cue.Image == nil || cue.Image.Bounds() != bitmap.Bounds() || !bytes.Equal(cue.Image.Pix, bitmap.Pix) {
				t.Fatal("DVD even/odd field reconstruction differs from the independently authored glyph pixels")
			}
		})
	}
}

type subtitleOCRFixtureInterval struct {
	StartTicks int64  `json:"start_ticks"`
	EndTicks   int64  `json:"end_ticks"`
	Text       string `json:"text"`
	Forced     bool   `json:"forced"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RGBASHA256 string `json:"rgba_sha256"`
}

type subtitleOCRFixtureManifest struct {
	Format        string `json:"format"`
	DurationTicks int64  `json:"duration_ticks"`
	Font          struct {
		SHA256      string `json:"sha256"`
		GitBlobSHA1 string `json:"git_blob_sha1"`
		License     string `json:"license"`
	} `json:"font"`
	Cases []struct {
		Name                    string                       `json:"name"`
		PGSMatroskaFile         string                       `json:"pgs_matroska_file"`
		PGSFormatStartKnown     *bool                        `json:"pgs_format_start_known"`
		PGSContainerOriginTicks int64                        `json:"pgs_container_origin_ticks"`
		DVDMatroskaFile         string                       `json:"dvd_matroska_file"`
		DVDFormatStartKnown     *bool                        `json:"dvd_format_start_known"`
		DVDContainerOriginTicks int64                        `json:"dvd_container_origin_ticks"`
		DVDFirstPacketPTSTicks  int64                        `json:"dvd_first_packet_pts_ticks"`
		Models                  []string                     `json:"ocr_models"`
		PGSIntervals            []subtitleOCRFixtureInterval `json:"pgs_intervals"`
		DVDIntervals            []subtitleOCRFixtureInterval `json:"dvd_intervals"`
	} `json:"cases"`
	Files map[string]struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// TestRecognizeBitmapSubtitlesActualFixtures is opt-in real media/OCR
// acceptance. Provision fixtures with bitmap-subtitle-fixtures.py on test-env,
// then provide its absolute directory through GOBY_BITMAP_OCR_FIXTURES and an
// absolute SubtitleOCRConfig JSON file through GOBY_BITMAP_OCR_CONFIG. It calls
// the actual admitted FFprobe/Tesseract/model files, not a mock executable.
// Both environment variables stay unset in normal local unit test runs.
func TestRecognizeBitmapSubtitlesActualFixtures(t *testing.T) {
	directory, configPath := os.Getenv("GOBY_BITMAP_OCR_FIXTURES"), os.Getenv("GOBY_BITMAP_OCR_CONFIG")
	if directory == "" && configPath == "" {
		t.Skip("set GOBY_BITMAP_OCR_FIXTURES and GOBY_BITMAP_OCR_CONFIG for real Linux OCR acceptance")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("actual bitmap OCR acceptance must run on the authorized Linux verification host")
	}
	if !filepath.IsAbs(directory) || !filepath.IsAbs(configPath) {
		t.Fatal("both fixture directory and OCR configuration paths must be absolute")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	manifestFile, err := root.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes := subtitleFixtureReadBounded(t, manifestFile, 1<<20)
	if err := manifestFile.Close(); err != nil {
		t.Fatal(err)
	}
	var manifest subtitleOCRFixtureManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != "goby-bitmap-subtitle-fixtures-v2" || manifest.DurationTicks != 97280000 ||
		manifest.Font.GitBlobSHA1 != "dc15562470b4f842321894787a0d066879ccff8b" ||
		manifest.Font.License != "SIL-OFL-1.1" || len(manifest.Font.SHA256) != 64 {
		t.Fatal("fixture manifest does not describe the pinned real-text corpus")
	}
	configurationFile, err := os.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configurationBytes := subtitleFixtureReadBounded(t, configurationFile, 64<<10)
	if err := configurationFile.Close(); err != nil {
		t.Fatal(err)
	}
	var config SubtitleOCRConfig
	configurationDecoder := json.NewDecoder(bytes.NewReader(configurationBytes))
	configurationDecoder.DisallowUnknownFields()
	if err := configurationDecoder.Decode(&config); err != nil {
		t.Fatalf("invalid pinned OCR configuration: %v", err)
	}
	var trailing any
	if err := configurationDecoder.Decode(&trailing); err != io.EOF {
		t.Fatal("OCR configuration has trailing JSON data")
	}
	modelDigests := make(map[string]string)
	for _, model := range config.Models {
		if _, duplicate := modelDigests[model.ID]; duplicate || len(model.SHA256) != 64 {
			t.Fatal("OCR configuration has duplicate or incomplete model identities")
		}
		modelDigests[model.ID] = model.SHA256
	}
	for _, model := range []string{"eng", "chi_sim", "chi_tra"} {
		if modelDigests[model] == "" {
			t.Fatalf("real acceptance requires the configured %s model", model)
		}
	}
	wantModels := map[string]string{"english": "eng", "chinese-forced": "chi_sim", "chinese-traditional": "chi_tra",
		"overlap": "chi_sim+eng", "mixed-forced": "chi_sim+eng"}
	if len(manifest.Cases) != len(wantModels) {
		t.Fatal("real OCR corpus must include English, both Chinese scripts, overlap, and mixed forced objects")
	}
	seen := make(map[string]bool)
	for _, fixture := range manifest.Cases {
		models := append([]string(nil), fixture.Models...)
		sort.Strings(models)
		if expected, ok := wantModels[fixture.Name]; !ok || seen[fixture.Name] || strings.Join(models, "+") != expected {
			t.Fatalf("unexpected, duplicate, or incorrectly labelled fixture case %q", fixture.Name)
		}
		seen[fixture.Name] = true
		expectedModelDigest := modelDigests[models[0]]
		if len(models) > 1 {
			var entries []string
			for _, id := range models {
				entries = append(entries, id+":"+modelDigests[id]+"\n")
			}
			digest := sha256.Sum256([]byte(strings.Join(entries, "")))
			expectedModelDigest = hex.EncodeToString(digest[:])
		}
		for _, test := range []struct {
			name, path, codec string
			origin            int64
			originKnown       *bool
			firstPacketPTS    int64
			intervals         []subtitleOCRFixtureInterval
		}{
			{"PGS", fixture.PGSMatroskaFile, "hdmv_pgs_subtitle", fixture.PGSContainerOriginTicks, fixture.PGSFormatStartKnown, 0, fixture.PGSIntervals},
			{"DVD", fixture.DVDMatroskaFile, "dvd_subtitle", fixture.DVDContainerOriginTicks, fixture.DVDFormatStartKnown, fixture.DVDFirstPacketPTSTicks, fixture.DVDIntervals},
		} {
			if test.path == "" {
				if fixture.Name != "mixed-forced" || test.name != "DVD" || len(test.intervals) != 0 {
					t.Fatalf("missing real %s container for %s", test.name, fixture.Name)
				}
				continue // DVD's one forced bit cannot encode mixed forced objects.
			}
			t.Run(fixture.Name+"/"+test.name, func(t *testing.T) {
				subtitleFixtureRequireCorpusTimeline(t, fixture.Name, test.name, test.origin, test.originKnown, test.firstPacketPTS, test.intervals)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				if filepath.Base(test.path) != test.path || len(test.intervals) == 0 {
					t.Fatal("fixture filename or expected intervals are invalid")
				}
				identity, exists := manifest.Files[test.path]
				if !exists || identity.Bytes <= 0 || identity.Bytes > 32<<20 || len(identity.SHA256) != 64 {
					t.Fatal("fixture file is not present in the generation manifest")
				}
				file, err := root.Open(test.path)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				before, err := file.Stat()
				if err != nil || !before.Mode().IsRegular() || before.Size() != identity.Bytes {
					t.Fatal("fixture is not the generated regular file")
				}
				hash := sha256.New()
				if _, err := io.Copy(hash, io.NewSectionReader(file, 0, before.Size())); err != nil ||
					hex.EncodeToString(hash.Sum(nil)) != identity.SHA256 {
					t.Fatal("fixture digest differs from its generation manifest")
				}
				if _, err := file.Seek(7, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				info, err := (Prober{FFprobePath: config.FFprobePath, Timeout: 30 * time.Second}).ProbeFile(ctx, file)
				if err != nil {
					t.Fatalf("probe actual authored container: %v", err)
				}
				if info.Container != "matroska,webm" || len(info.Streams) != 1 || info.Streams[0].CodecType != "subtitle" ||
					info.Streams[0].Codec != test.codec || info.DurationTicks != manifest.DurationTicks ||
					info.FormatStartKnown != *test.originKnown || info.FormatStartTicks != test.origin {
					t.Fatalf("authored container facts changed: %+v", info)
				}
				result, err := RecognizeBitmapSubtitles(ctx, config, file, info.Streams[0], info, models)
				if err != nil {
					t.Fatalf("recognize real %s text with %v: %v", test.codec, models, err)
				}
				if result.EngineSHA256 != config.TesseractSHA256 || len(result.Models) != len(models) ||
					result.ModelID != strings.Join(models, "+") || result.ModelSHA256 != expectedModelDigest {
					t.Fatal("recognition lost admitted engine or combined-model provenance")
				}
				for index, model := range result.Models {
					if model.ID != models[index] || model.SHA256 != modelDigests[model.ID] {
						t.Fatal("recognition did not retain every selected model digest")
					}
				}
				if len(result.Cues) != len(test.intervals) {
					t.Fatalf("real display cue count=%d, want %d; warnings=%v", len(result.Cues), len(test.intervals), result.Warnings)
				}
				for index, expected := range test.intervals {
					cue := result.Cues[index]
					if cue.StartTicks != expected.StartTicks-test.origin || cue.EndTicks != expected.EndTicks-test.origin ||
						cue.Forced != expected.Forced || cue.HearingImpaired || cue.X != expected.X || cue.Y != expected.Y ||
						cue.Width != expected.Width || cue.Height != expected.Height {
						t.Errorf("cue %d lost exact display facts: ticks=%d..%d xy=%d,%d size=%dx%d forced=%t hearing_impaired=%t", index,
							cue.StartTicks, cue.EndTicks, cue.X, cue.Y, cue.Width, cue.Height, cue.Forced, cue.HearingImpaired)
					}
					if subtitleFixtureComparableText(cue.Text) != subtitleFixtureComparableText(expected.Text) ||
						strings.TrimSpace(cue.Text) == "" || math.IsNaN(cue.Confidence) || cue.Confidence <= 0 || cue.Confidence > 100 {
						t.Errorf("cue %d actual OCR text=%q confidence=%g, want real text %q and positive confidence", index, cue.Text, cue.Confidence, expected.Text)
					}
					if len(cue.ImagePNG) == 0 || len(cue.ImagePNG) > MaxSubtitleOCRCueImageBytes {
						t.Fatalf("cue %d did not retain its bounded source image", index)
					}
					imageDigest := sha256.Sum256(cue.ImagePNG)
					if hex.EncodeToString(imageDigest[:]) != cue.ImageSHA256 {
						t.Errorf("cue %d image digest does not bind the review bytes", index)
					}
					decoded, err := png.Decode(bytes.NewReader(cue.ImagePNG))
					if err != nil {
						t.Fatalf("decode retained cue image: %v", err)
					}
					if decoded.Bounds().Dx() != expected.Width || decoded.Bounds().Dy() != expected.Height ||
						subtitleFixtureRGBADigest(decoded) != expected.RGBASHA256 {
						t.Errorf("cue %d original decoded pixels differ from the independently rendered source", index)
					}
					t.Logf("actual OCR cue=%d codec=%s models=%s text=%q confidence=%.3f ticks=%d..%d forced=%t image_sha256=%s",
						index, test.codec, result.ModelID, cue.Text, cue.Confidence, cue.StartTicks, cue.EndTicks, cue.Forced, cue.ImageSHA256)
				}
				position, err := file.Seek(0, io.SeekCurrent)
				if err != nil || position != 7 || !subtitleFileUnchanged(file, before) {
					t.Fatal("real probe/OCR changed or closed its borrowed source descriptor")
				}
				t.Logf("actual OCR source_sha256=%s engine_sha256=%s warnings=%v", identity.SHA256, result.EngineSHA256, result.Warnings)
			})
		}
	}
}

func subtitleFixtureReadBounded(t *testing.T, file *os.File, limit int64) []byte {
	t.Helper()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		t.Fatal("fixture metadata/configuration is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) != info.Size() {
		t.Fatal("could not read complete fixture metadata/configuration")
	}
	return data
}

func subtitleFixtureRequireCorpusTimeline(t *testing.T, name, codec string, origin int64, originKnown *bool, firstPacketPTS int64, intervals []subtitleOCRFixtureInterval) {
	t.Helper()
	type requiredCue struct {
		startMS, endMS int64
		text           string
		forced         bool
	}
	// Keep scenario coverage independent of the generator's manifest. A change
	// that removes Chinese glyphs, repeated cues, simultaneous objects, or their
	// different forced flags must not silently reduce the acceptance boundary.
	timelines := map[string][]requiredCue{
		"english":             {{1024, 4096, "Hello world", false}},
		"chinese-forced":      {{2560, 5632, "中文测试", true}},
		"chinese-traditional": {{2560, 5632, "中文測試", false}},
		"overlap": {
			{1024, 2560, "Hello world", false},
			{2560, 4096, "Hello world\n中文测试", false},
			{4096, 5632, "中文测试", false},
			{6656, 8704, "Hello world", false},
		},
		"mixed-forced": {
			{1024, 2560, "Hello world", false},
			{2560, 4096, "Hello world", false},
			{2560, 4096, "中文测试", true},
			{4096, 5632, "中文测试", true},
		},
	}
	required, exists := timelines[name]
	if !exists || len(intervals) != len(required) {
		t.Fatal("manifest removed a required real text/display scenario")
	}
	wantFirstPTS := int64(0)
	if codec == "DVD" {
		wantFirstPTS = required[0].startMS * 10000
	}
	// The actual admitted demuxer reports no format origin for the DVD-only
	// files, even though their SPU packets have exact nonzero PTS. Unknown is
	// not equivalent to the first caption's PTS: its initial gap must survive.
	if originKnown == nil || *originKnown != (codec == "PGS") || origin != 0 || firstPacketPTS != wantFirstPTS {
		t.Fatal("manifest changed the distinct format-origin and packet-clock facts")
	}
	for index, want := range required {
		got := intervals[index]
		if got.StartTicks != want.startMS*10000 || got.EndTicks != want.endMS*10000 || got.Text != want.text || got.Forced != want.forced {
			t.Fatalf("manifest changed required cue %d text/timing/forced structure", index)
		}
	}
}

func subtitleFixtureComparableText(text string) string {
	// Tesseract may separate CJK glyphs or wrap a line. Only whitespace is
	// ignored; case, punctuation, and simplified/traditional code points remain.
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, text)
}

func subtitleFixtureRGBADigest(bitmap image.Image) string {
	hash := sha256.New()
	for y := bitmap.Bounds().Min.Y; y < bitmap.Bounds().Max.Y; y++ {
		for x := bitmap.Bounds().Min.X; x < bitmap.Bounds().Max.X; x++ {
			pixel := color.NRGBAModel.Convert(bitmap.At(x, y)).(color.NRGBA)
			_, _ = hash.Write([]byte{pixel.R, pixel.G, pixel.B, pixel.A})
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}
