package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestMediaEditPacketsPreserveExactClocksAndIndependentStreamOrder(t *testing.T) {
	source := mediaEditPacketsDocument(
		mediaEditPacketFixture(5, -1000, -1001, 20, 8, 'a'),
		mediaEditPacketFixture(2, 480, 480, 1024, 99, 'b'),
		mediaEditPacketFixture(5, -980, -981, 20, 9, 'c'),
	)
	candidate := mediaEditPacketsDocument(
		mediaEditPacketFixture(0, -90000, -90090, 1800, 8, 'a'),
		mediaEditPacketFixture(0, -88200, -88290, 1800, 9, 'c'),
		mediaEditPacketFixture(1, 240, 240, 512, 99, 'b'),
	)
	before, err := parseMediaEditPackets(iotest.OneByteReader(strings.NewReader(source)), map[int]*big.Rat{
		5: big.NewRat(1, 1000), 2: big.NewRat(1, 48000), 9: nil,
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	after, err := parseMediaEditPackets(strings.NewReader(candidate), map[int]*big.Rat{
		0: big.NewRat(1, 90000), 1: big.NewRat(1, 24000), 2: nil,
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if before[5] != after[0] || before[2] != after[1] {
		t.Fatalf("equivalent streams differ: before=%+v after=%+v", before, after)
	}
	if before[5].Packets != 2 || before[5].MaxPacketBytes != 9 ||
		len(before[5].PayloadSHA256) != 64 || len(before[5].TimingSHA256) != 64 {
		t.Fatalf("incomplete packet evidence: %+v", before[5])
	}
	if before[9] != (mediaEditPacketDigest{}) || after[2] != (mediaEditPacketDigest{}) {
		t.Fatal("packetless attachment must not acquire packet evidence")
	}
}

func TestMediaEditPacketsBindPayloadBoundariesOrderAndTimeline(t *testing.T) {
	first := mediaEditPacketFixture(0, -1, -2, 40, 5, 'a')
	second := mediaEditPacketFixture(0, 39, 38, 40, 6, 'b')
	baseline := mediaEditPacketMustParse(t, mediaEditPacketsDocument(first, second), big.NewRat(1, 1000))
	cases := []struct {
		name    string
		packets []string
		payload bool
		timing  bool
	}{
		{"payload", []string{strings.Replace(first, strings.Repeat("a", 64), strings.Repeat("c", 64), 1), second}, true, false},
		{"packet size", []string{strings.Replace(first, `"size":"5"`, `"size":"4"`, 1), second}, true, false},
		{"presentation offset", []string{strings.Replace(first, `"pts":-1`, `"pts":0`, 1), second}, false, true},
		{"decoding offset", []string{strings.Replace(first, `"dts":-2`, `"dts":-3`, 1), second}, false, true},
		{"duration", []string{strings.Replace(first, `"duration":40`, `"duration":41`, 1), second}, false, true},
		{"flags", []string{strings.Replace(first, `"flags":"K_"`, `"flags":"__"`, 1), second}, false, true},
		{"order", []string{second, first}, true, true},
		{"missing packet", []string{first}, true, true},
		{"duplicate packet", []string{first, first, second}, true, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := mediaEditPacketMustParse(t, mediaEditPacketsDocument(test.packets...), big.NewRat(1, 1000))
			if (changed.PayloadSHA256 != baseline.PayloadSHA256) != test.payload ||
				(changed.TimingSHA256 != baseline.TimingSHA256) != test.timing {
				t.Fatalf("unexpected preservation result: before=%+v after=%+v", baseline, changed)
			}
		})
	}
}

func TestMediaEditPacketsNeverRoundClockFractions(t *testing.T) {
	packet := mediaEditPacketFixture(0, 1, 1, 1, 10, 'a')
	thirds := mediaEditPacketMustParse(t, mediaEditPacketsDocument(packet), big.NewRat(1, 3))
	rounded := mediaEditPacketMustParse(t, mediaEditPacketsDocument(mediaEditPacketFixture(0, 333, 333, 333, 10, 'a')), big.NewRat(1, 1000))
	if thirds.PayloadSHA256 != rounded.PayloadSHA256 || thirds.TimingSHA256 == rounded.TimingSHA256 {
		t.Fatal("rounded clocks were accepted as exact preservation")
	}
	large := mediaEditPacketFixture(0, math.MinInt64, math.MaxInt64, math.MaxInt64, 10, 'a')
	if digest := mediaEditPacketMustParse(t, mediaEditPacketsDocument(large), big.NewRat(math.MaxInt64, 3)); digest.Packets != 1 {
		t.Fatal("exact bounded integer arithmetic lost a packet")
	}
}

func TestMediaEditPacketsNormalizeScalarRepresentationsAndMissingValues(t *testing.T) {
	packet := mediaEditPacketFixture(0, 10, 9, 2, 8, 'a')
	missing := strings.Replace(packet, `"dts":9,`, "", 1)
	unknown := strings.Replace(packet, `"dts":9`, `"dts":"N/A"`, 1)
	quoted := strings.ReplaceAll(unknown, `"pts":10`, `"pts":"10"`)
	quoted = strings.ReplaceAll(quoted, `"size":"8"`, `"size":8`)
	quoted = strings.ReplaceAll(quoted, `SHA256:`+strings.Repeat("a", 64), `SHA256:`+strings.Repeat("A", 64))
	before := mediaEditPacketMustParse(t, mediaEditPacketsDocument(missing), big.NewRat(1, 1000))
	after := mediaEditPacketMustParse(t, mediaEditPacketsDocument(quoted), big.NewRat(1, 1000))
	if before != after {
		t.Fatal("equivalent emitted scalar representations changed evidence")
	}
	known := mediaEditPacketMustParse(t, mediaEditPacketsDocument(packet), big.NewRat(1, 1000))
	if known.TimingSHA256 == after.TimingSHA256 {
		t.Fatal("an absent timestamp must not be equivalent to a known timestamp")
	}
}

func TestMediaEditPacketsProveCompleteSkipSamplesSideData(t *testing.T) {
	packet := mediaEditPacketFixture(0, 0, 0, 1024, 10, 'a')
	first := `{"side_data_type":"Skip Samples","skip_samples":1024,"discard_padding":3,"skip_reason":0,"discard_reason":1}`
	reordered := `{"discard_reason":1,"skip_reason":0,"discard_padding":3,"skip_samples":1024,"side_data_type":"Skip Samples"}`
	withSide := func(side string) string {
		return mediaEditPacketsDocument(strings.TrimSuffix(packet, "}") + `,"side_data_list":[` + side + `]}`)
	}
	before := mediaEditPacketMustParse(t, withSide(first), big.NewRat(1, 48000))
	after := mediaEditPacketMustParse(t, withSide(reordered), big.NewRat(1, 48000))
	if before != after {
		t.Fatal("side data field ordering changed its canonical evidence")
	}
	changed := mediaEditPacketMustParse(t, withSide(strings.Replace(first, `"discard_padding":3`, `"discard_padding":4`, 1)), big.NewRat(1, 48000))
	if changed.TimingSHA256 == before.TimingSHA256 || changed.PayloadSHA256 != before.PayloadSHA256 {
		t.Fatal("skip-sample timing was not bound independently of compressed payload")
	}
	without := mediaEditPacketMustParse(t, mediaEditPacketsDocument(packet), big.NewRat(1, 48000))
	if without.TimingSHA256 == before.TimingSHA256 {
		t.Fatal("omitted side data was accepted as equivalent")
	}
	cases := []string{
		`{"side_data_type":"New Extradata"}`,
		`{"side_data_type":"Unknown","bytes":"aabb"}`,
		`{}`,
		`null`,
		`[]`,
		strings.Replace(first, `,"discard_reason":1`, "", 1),
		strings.Replace(first, `"discard_reason":1`, `"discard_reason":1,"unexpected":0`, 1),
		strings.Replace(first, `"skip_samples":1024`, `"skip_samples":4294967296`, 1),
		strings.Replace(first, `"skip_samples":1024`, `"skip_samples":-1`, 1),
		strings.Replace(first, `"discard_reason":1`, `"discard_reason":256`, 1),
		strings.Replace(first, `"discard_reason":1`, `"discard_reason":1.0`, 1),
		strings.Replace(first, `"discard_reason":1`, `"discard_reason":true`, 1),
		strings.Replace(first, `"discard_reason":1`, `"discard_reason":1,"discard_reason":2`, 1),
	}
	for number, side := range cases {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			if got, err := parseMediaEditPackets(strings.NewReader(withSide(side)), map[int]*big.Rat{0: big.NewRat(1, 48000)}, 10); err == nil || got != nil {
				t.Fatalf("accepted unproven side data: %s", side)
			}
		})
	}
}

func TestMediaEditPacketsRejectMalformedAndIncompleteEvidence(t *testing.T) {
	packet := mediaEditPacketFixture(0, 10, 9, 2, 8, 'a')
	valid := mediaEditPacketsDocument(packet)
	cases := map[string]string{
		"unknown stream":       strings.Replace(valid, `"stream_index":0`, `"stream_index":1`, 1),
		"negative stream":      strings.Replace(valid, `"stream_index":0`, `"stream_index":-1`, 1),
		"large stream":         strings.Replace(valid, `"stream_index":0`, `"stream_index":2147483648`, 1),
		"missing stream":       strings.Replace(valid, `"stream_index":0,`, "", 1),
		"float stream":         strings.Replace(valid, `"stream_index":0`, `"stream_index":0.0`, 1),
		"missing payload hash": strings.Replace(valid, `,"data_hash":"SHA256:`+strings.Repeat("a", 64)+`"`, "", 1),
		"wrong hash algorithm": strings.Replace(valid, "SHA256:", "SHA512:", 1),
		"invalid hash":         strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("z", 64), 1),
		"short hash":           strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("a", 63), 1),
		"missing size":         strings.Replace(valid, `"size":"8",`, "", 1),
		"negative size":        strings.Replace(valid, `"size":"8"`, `"size":-1`, 1),
		"oversize payload":     strings.Replace(valid, `"size":"8"`, `"size":268435457`, 1),
		"overflow integer":     strings.Replace(valid, `"pts":10`, `"pts":9223372036854775808`, 1),
		"exponent integer":     strings.Replace(valid, `"pts":10`, `"pts":1e1`, 1),
		"fraction integer":     strings.Replace(valid, `"pts":10`, `"pts":"1/2"`, 1),
		"nonfinite integer":    strings.Replace(valid, `"pts":10`, `"pts":"NaN"`, 1),
		"null integer":         strings.Replace(valid, `"pts":10`, `"pts":null`, 1),
		"array integer":        strings.Replace(valid, `"pts":10`, `"pts":[]`, 1),
		"empty integer":        strings.Replace(valid, `"pts":10`, `"pts":""`, 1),
		"negative duration":    strings.Replace(valid, `"duration":2`, `"duration":-1`, 1),
		"missing timestamps":   strings.Replace(strings.Replace(valid, `"pts":10,`, "", 1), `"dts":9,`, "", 1),
		"unknown timestamps":   strings.Replace(strings.Replace(valid, `"pts":10`, `"pts":"N/A"`, 1), `"dts":9`, `"dts":"N/A"`, 1),
		"missing flags":        strings.Replace(valid, `"flags":"K_",`, "", 1),
		"invalid flags":        strings.Replace(valid, `"flags":"K_"`, `"flags":"K\n"`, 1),
		"empty flags":          strings.Replace(valid, `"flags":"K_"`, `"flags":""`, 1),
		"duplicate packet key": strings.Replace(valid, `"stream_index":0`, `"stream_index":0,"stream_index":0`, 1),
		"unknown packet key":   strings.Replace(valid, `"stream_index":0`, `"stream_index":0,"future_field":7`, 1),
		"duplicate root key":   strings.TrimSuffix(valid, "}") + `,"packets":[]}`,
		"unknown root key":     strings.Replace(valid, `"packets"`, `"frames"`, 1),
		"trailing value":       valid + `{}`,
		"trailing comma":       strings.TrimSuffix(valid, "]}") + `,]}`,
		"truncated object":     strings.TrimSuffix(valid, "}]}") + `"`,
		"truncated document":   strings.TrimSuffix(valid, "}"),
		"empty document":       `{}`,
		"packet array":         `{"packets":[[]]}`,
		"packet null":          `{"packets":[null]}`,
		"side data null":       mediaEditPacketsDocument(strings.TrimSuffix(packet, "}") + `,"side_data_list":null}`),
		"invalid UTF8":         strings.Replace(valid, `"flags":"K_"`, "\"flags\":\"\xff\"", 1),
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := parseMediaEditPackets(strings.NewReader(document), map[int]*big.Rat{0: big.NewRat(1, 1000)}, 10); err == nil || got != nil {
				t.Fatal("malformed evidence produced a digest")
			}
		})
	}
}

func TestMediaEditPacketsBoundInputBeforeJSONAllocation(t *testing.T) {
	packet := mediaEditPacketFixture(0, 0, 0, 1, 8, 'a')
	cases := []struct {
		name     string
		document string
		limit    int64
		maxRead  int
	}{
		{"record", `{"packets":[{"stream_index":0,"flags":"` + strings.Repeat("x", 1_000_000), 1, maxMediaEditPacketJSONBytes + 8192},
		{"nested", `{"packets":[{"side_data_list":` + strings.Repeat("[", 1000), 1, 8192},
		{"whitespace", strings.Repeat(" ", 1_000_000), 1, maxMediaEditPacketJSONGap + 8192},
		{"count", `{"packets":[` + packet + `,{"flags":"` + strings.Repeat("x", 1_000_000), 1, 8192},
		{"trailing whitespace", mediaEditPacketsDocument(packet) + strings.Repeat(" ", 1_000_000), 1, maxMediaEditPacketJSONGap + 8192},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := &mediaEditPacketCountingReader{Reader: strings.NewReader(test.document)}
			if got, err := parseMediaEditPackets(input, map[int]*big.Rat{0: big.NewRat(1, 1000)}, test.limit); err == nil || got != nil {
				t.Fatal("budget violation produced evidence")
			}
			if input.bytes > test.maxRead {
				t.Fatalf("read %d bytes before rejecting a %s limit", input.bytes, test.name)
			}
		})
	}
}

func TestMediaEditPacketsValidateConfigurationAndRetainNoPartialProof(t *testing.T) {
	valid := mediaEditPacketsDocument(mediaEditPacketFixture(0, 0, 0, 1, 8, 'a'))
	for _, bases := range []map[int]*big.Rat{
		nil,
		{-1: big.NewRat(1, 1)},
		{0: big.NewRat(0, 1)},
		{0: big.NewRat(-1, 1)},
		{0: new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 64))},
	} {
		if got, err := parseMediaEditPackets(strings.NewReader(valid), bases, 1); err == nil || got != nil {
			t.Fatal("invalid stream configuration produced evidence")
		}
	}
	many := make(map[int]*big.Rat)
	for index := 0; index <= maxMediaEditPacketStreams; index++ {
		many[index] = big.NewRat(1, 1)
	}
	if _, err := parseMediaEditPackets(strings.NewReader(valid), many, 1); err == nil {
		t.Fatal("unbounded stream configuration was accepted")
	}
	for _, limit := range []int64{-1, 0, maxMediaEditPacketRecords + 1} {
		if got, err := parseMediaEditPackets(strings.NewReader(valid), map[int]*big.Rat{0: big.NewRat(1, 1)}, limit); err == nil || got != nil {
			t.Fatal("invalid packet budget produced evidence")
		}
	}
	if _, err := parseMediaEditPackets(nil, map[int]*big.Rat{0: big.NewRat(1, 1)}, 1); err == nil {
		t.Fatal("nil input was accepted")
	}
	if _, err := parseMediaEditPackets(strings.NewReader(valid), map[int]*big.Rat{0: nil}, 1); err == nil {
		t.Fatal("packet with an unknown time base was accepted")
	}
	empty, err := parseMediaEditPackets(strings.NewReader(`{"packets":[]}`), map[int]*big.Rat{0: nil}, 1)
	if err != nil || !reflect.DeepEqual(empty, map[int]mediaEditPacketDigest{0: {}}) {
		t.Fatalf("empty attachment evidence is invalid: %+v, %v", empty, err)
	}
	input := io.MultiReader(strings.NewReader(strings.TrimSuffix(valid, "]}")), mediaEditPacketFailReader{})
	if got, err := parseMediaEditPackets(input, map[int]*big.Rat{0: big.NewRat(1, 1)}, 1); err == nil || got != nil {
		t.Fatal("interrupted output returned a partial proof")
	}
}

func TestMediaEditDecodeJSONPreservesNumbersAndRejectsAmbiguousDocuments(t *testing.T) {
	value, err := mediaEditDecodeJSON([]byte(`{"streams":[{"index":9007199254740993,"tags":{"title":"test"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	index := value.(map[string]any)["streams"].([]any)[0].(map[string]any)["index"]
	if index != json.Number("9007199254740993") {
		t.Fatalf("large integer was rounded or lost its type: %v", index)
	}
	for _, document := range []string{
		`{"streams":[],"streams":[]}`,
		`{"streams":[{"tags":{"title":"first","title":"second"}}]}`,
		`{"streams":[{"tags":{"title":"first","\u0074itle":"second"}}]}`,
		`{"value":NaN}`,
		`{"value":Infinity}`,
		`{"value":"\ud800"}`,
		`{"value":"\udc00"}`,
		`{"value":"\ud800\u0061"}`,
		`{"value":1} {"value":2}`,
		`{"value":1} trailing`,
		`{"value":1,}`,
		`{"value":` + strings.Repeat("[", maxMediaEditJSONDepth+1) + "0" + strings.Repeat("]", maxMediaEditJSONDepth+1) + "}",
		"{\"value\":\"\xff\"}",
	} {
		if result, err := mediaEditDecodeJSON([]byte(document)); err == nil || result != nil {
			t.Fatal("ambiguous or malformed document was accepted")
		}
	}
	for _, document := range []string{
		`{"value":"\ud83d\ude00"}`,
		`{"value":"\\ud800"}`,
		`{"value":"escaped \" quote and \u00e9"}`,
	} {
		if _, err := mediaEditDecodeJSON([]byte(document)); err != nil {
			t.Fatalf("valid string escaping was rejected: %v", err)
		}
	}
}

func mediaEditPacketFixture(stream int, pts, dts, duration, size int64, digit byte) string {
	return fmt.Sprintf(`{"stream_index":%d,"pts":%d,"dts":%d,"duration":%d,"size":"%d","flags":"K_","data_hash":"SHA256:%s"}`,
		stream, pts, dts, duration, size, strings.Repeat(string(digit), 64))
}

func mediaEditPacketsDocument(packets ...string) string {
	return `{"packets":[` + strings.Join(packets, ",") + `]}`
}

func mediaEditPacketMustParse(t *testing.T, document string, base *big.Rat) mediaEditPacketDigest {
	t.Helper()
	digest, err := parseMediaEditPackets(strings.NewReader(document), map[int]*big.Rat{0: base}, 100)
	if err != nil {
		t.Fatal(err)
	}
	return digest[0]
}

type mediaEditPacketCountingReader struct {
	io.Reader
	bytes int
}

func (r *mediaEditPacketCountingReader) Read(buffer []byte) (int, error) {
	n, err := r.Reader.Read(buffer)
	r.bytes += n
	return n, err
}

type mediaEditPacketFailReader struct{}

func (mediaEditPacketFailReader) Read([]byte) (int, error) {
	return 0, errors.New("injected packet stream failure")
}
