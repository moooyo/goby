package media

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math"
	"math/big"
	"strconv"
	"unicode/utf8"
)

const (
	maxMediaEditPacketJSONBytes    = 64 * 1024
	maxMediaEditPacketJSONDepth    = 32
	maxMediaEditJSONDepth          = 64
	maxMediaEditPacketJSONGap      = 16 * 1024
	maxMediaEditPacketStreams      = 256
	maxMediaEditPacketRecords      = 100_000_000
	maxMediaEditPacketPayloadBytes = 256 * 1024 * 1024
)

type mediaEditPacketDigest struct {
	Packets        int64
	MaxPacketBytes int64
	PayloadSHA256  string
	TimingSHA256   string
}

type mediaEditPacketState struct {
	base    *big.Rat
	payload hash.Hash
	timing  hash.Hash
	digest  mediaEditPacketDigest
}

// parseMediaEditPackets consumes the complete ffprobe packet document without
// retaining the media's packet list. Each record and nesting depth have limits
// before JSON decoding, including records for streams a caller will remove.
// Stream indexes identify input records but are excluded from hashes so a
// remuxed stream can be compared after its index changes. Nil time bases are
// allowed only for streams with no packets, such as ordinary attachments.
func parseMediaEditPackets(reader io.Reader, timeBases map[int]*big.Rat, maxPackets int64) (map[int]mediaEditPacketDigest, error) {
	if reader == nil || len(timeBases) == 0 || len(timeBases) > maxMediaEditPacketStreams ||
		maxPackets <= 0 || maxPackets > maxMediaEditPacketRecords {
		return nil, fmt.Errorf("invalid media edit packet parser configuration")
	}
	states := make(map[int]*mediaEditPacketState, len(timeBases))
	for index, base := range timeBases {
		if index < 0 || int64(index) > math.MaxInt32 || base != nil &&
			(base.Sign() <= 0 || !base.Num().IsInt64() || !base.Denom().IsInt64()) {
			return nil, fmt.Errorf("invalid media edit stream time base")
		}
		state := &mediaEditPacketState{}
		if base != nil {
			state.base = new(big.Rat).Set(base)
		}
		states[index] = state
	}

	input := bufio.NewReaderSize(reader, 4096)
	for _, expected := range []byte{'{', '"'} {
		if err := mediaEditPacketExpect(input, expected); err != nil {
			return nil, err
		}
	}
	// The bounded command requests only packets. Reject unexpected sections,
	// including ffprobe error objects or another packets key.
	for _, expected := range []byte("packets\"") {
		actual, err := input.ReadByte()
		if err != nil || actual != expected {
			return nil, fmt.Errorf("unexpected media edit packet document field")
		}
	}
	for _, expected := range []byte{':', '['} {
		if err := mediaEditPacketExpect(input, expected); err != nil {
			return nil, err
		}
	}

	var total int64
	first, err := mediaEditPacketNext(input)
	if err != nil {
		return nil, fmt.Errorf("truncated media edit packet array: %w", err)
	}
	for first != ']' {
		if first != '{' {
			return nil, fmt.Errorf("media edit packet must be an object")
		}
		total++
		if total > maxPackets {
			return nil, fmt.Errorf("media edit packets exceed the record budget")
		}
		raw, err := readMediaEditPacketObject(input)
		if err != nil {
			return nil, err
		}
		if err := appendMediaEditPacket(states, raw); err != nil {
			return nil, fmt.Errorf("media edit packet %d: %w", total, err)
		}
		separator, err := mediaEditPacketNext(input)
		if err != nil {
			return nil, fmt.Errorf("truncated media edit packet array: %w", err)
		}
		if separator == ']' {
			break
		}
		if separator != ',' {
			return nil, fmt.Errorf("invalid media edit packet separator")
		}
		first, err = mediaEditPacketNext(input)
		if err != nil || first == ']' {
			return nil, fmt.Errorf("truncated or trailing media edit packet separator")
		}
	}
	if err := mediaEditPacketExpect(input, '}'); err != nil {
		return nil, err
	}
	if _, err := mediaEditPacketNext(input); err != io.EOF {
		return nil, fmt.Errorf("media edit packet document has trailing data or exceeds its whitespace budget")
	}

	digests := make(map[int]mediaEditPacketDigest, len(states))
	for index, state := range states {
		if state.digest.Packets != 0 {
			for _, sum := range []hash.Hash{state.payload, state.timing} {
				mediaEditPacketHashField(sum, []byte("end"))
				mediaEditPacketHashInt(sum, state.digest.Packets)
			}
			state.digest.PayloadSHA256 = hex.EncodeToString(state.payload.Sum(nil))
			state.digest.TimingSHA256 = hex.EncodeToString(state.timing.Sum(nil))
		}
		digests[index] = state.digest
	}
	return digests, nil
}

func appendMediaEditPacket(states map[int]*mediaEditPacketState, raw []byte) error {
	value, err := mediaEditDecodeJSON(raw)
	if err != nil {
		return err
	}
	packet, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("packet is not a JSON object")
	}
	for key := range packet {
		switch key {
		case "stream_index", "pts", "dts", "duration", "size", "flags", "data_hash", "side_data_list":
		default:
			return fmt.Errorf("packet contains an unexpected field")
		}
	}
	index, err := mediaEditPacketInteger(packet["stream_index"])
	if err != nil || index < 0 || index > math.MaxInt32 {
		return fmt.Errorf("packet has an invalid stream index")
	}
	state := states[int(index)]
	if state == nil || state.base == nil {
		return fmt.Errorf("packet stream is unknown or has no provable time base")
	}
	size, err := mediaEditPacketInteger(packet["size"])
	if err != nil || size < 0 || size > maxMediaEditPacketPayloadBytes {
		return fmt.Errorf("packet payload size is invalid or exceeds its budget")
	}
	dataHash, ok := packet["data_hash"].(string)
	if !ok || len(dataHash) != 71 || dataHash[:7] != "SHA256:" {
		return fmt.Errorf("packet has no SHA256 payload hash")
	}
	payload, err := hex.DecodeString(dataHash[7:])
	if err != nil || len(payload) != sha256.Size {
		return fmt.Errorf("packet payload hash is malformed")
	}
	flags, ok := packet["flags"].(string)
	if !ok || len(flags) == 0 || len(flags) > 32 {
		return fmt.Errorf("packet flags are invalid")
	}
	for _, flag := range flags {
		if flag != '_' && !(flag >= 'A' && flag <= 'Z') && !(flag >= 'a' && flag <= 'z') {
			return fmt.Errorf("packet flags are invalid")
		}
	}
	times := make([]string, 3)
	for position, field := range []string{"pts", "dts", "duration"} {
		times[position], err = mediaEditPacketTime(packet, field, state.base)
		if err != nil {
			return err
		}
	}
	if times[0] == "missing" && times[1] == "missing" {
		return fmt.Errorf("packet has no provable presentation or decoding timestamp")
	}

	// ffprobe describes some side-data types without exposing their payload.
	// A matching description cannot prove preservation of those hidden bytes.
	// Only a fully reported Skip Samples structure currently has a proof.
	sideData := []any{}
	if value, present := packet["side_data_list"]; present {
		var ok bool
		sideData, ok = value.([]any)
		if !ok {
			return fmt.Errorf("packet side data is not an array")
		}
		for _, entry := range sideData {
			if err := validateMediaEditPacketSideData(entry); err != nil {
				return err
			}
		}
	}
	canonicalSideData, err := json.Marshal(sideData)
	if err != nil {
		return fmt.Errorf("cannot canonicalize packet side data: %w", err)
	}
	if state.payload == nil {
		state.payload, state.timing = sha256.New(), sha256.New()
		mediaEditPacketHashField(state.payload, []byte("goby-media-edit-payload-v1"))
		mediaEditPacketHashField(state.timing, []byte("goby-media-edit-timing-v1"))
	}
	state.digest.Packets++
	state.digest.MaxPacketBytes = max(state.digest.MaxPacketBytes, size)
	mediaEditPacketHashInt(state.payload, state.digest.Packets)
	mediaEditPacketHashInt(state.payload, size)
	mediaEditPacketHashField(state.payload, payload)
	mediaEditPacketHashInt(state.timing, state.digest.Packets)
	for _, timestamp := range times {
		mediaEditPacketHashField(state.timing, []byte(timestamp))
	}
	mediaEditPacketHashField(state.timing, []byte(flags))
	mediaEditPacketHashField(state.timing, canonicalSideData)
	return nil
}

func validateMediaEditPacketSideData(value any) error {
	entry, ok := value.(map[string]any)
	if !ok || len(entry) != 5 || entry["side_data_type"] != "Skip Samples" {
		return fmt.Errorf("cannot prove packet side data")
	}
	for _, field := range []string{"skip_samples", "discard_padding", "skip_reason", "discard_reason"} {
		value, err := mediaEditPacketInteger(entry[field])
		limit := int64(math.MaxUint32)
		if field == "skip_reason" || field == "discard_reason" {
			limit = math.MaxUint8
		}
		if err != nil || value < 0 || value > limit {
			return fmt.Errorf("cannot prove packet side data")
		}
	}
	return nil
}

func mediaEditPacketInteger(value any) (int64, error) {
	var text string
	switch value := value.(type) {
	case string:
		text = value
	case json.Number:
		text = value.String()
	default:
		return 0, fmt.Errorf("packet integer has an invalid JSON type")
	}
	if len(text) == 0 || len(text) > 20 {
		return 0, fmt.Errorf("packet integer has an invalid length")
	}
	for position, digit := range text {
		if position == 0 && digit == '-' {
			continue
		}
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("packet integer is not a decimal integer")
		}
	}
	return strconv.ParseInt(text, 10, 64)
}

func mediaEditPacketTime(packet map[string]any, field string, base *big.Rat) (string, error) {
	value, present := packet[field]
	if !present || value == "N/A" {
		return "missing", nil
	}
	timestamp, err := mediaEditPacketInteger(value)
	if err != nil || field == "duration" && timestamp < 0 {
		return "", fmt.Errorf("packet %s is invalid", field)
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt64(timestamp), base).RatString(), nil
}

func mediaEditPacketHashField(sum hash.Hash, value []byte) {
	mediaEditPacketHashInt(sum, int64(len(value)))
	_, _ = sum.Write(value)
}

func mediaEditPacketHashInt(sum hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = sum.Write(encoded[:])
}

func mediaEditPacketNext(reader *bufio.Reader) (byte, error) {
	for count := 0; count <= maxMediaEditPacketJSONGap; count++ {
		value, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		if value != ' ' && value != '\t' && value != '\r' && value != '\n' {
			return value, nil
		}
	}
	return 0, fmt.Errorf("media edit packet JSON exceeds its whitespace budget")
}

func mediaEditPacketExpect(reader *bufio.Reader, expected byte) error {
	actual, err := mediaEditPacketNext(reader)
	if err != nil || actual != expected {
		return fmt.Errorf("invalid or truncated media edit packet JSON")
	}
	return nil
}

// The opening brace has already been consumed. Scanning bytes before handing
// the object to encoding/json prevents an unbounded string allocation there.
func readMediaEditPacketObject(reader *bufio.Reader) ([]byte, error) {
	result := make([]byte, 1, 1024)
	result[0] = '{'
	depth, quoted, escaped := 1, false, false
	for depth != 0 {
		if len(result) >= maxMediaEditPacketJSONBytes {
			return nil, fmt.Errorf("media edit packet JSON exceeds its byte budget")
		}
		value, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("truncated media edit packet object: %w", err)
		}
		result = append(result, value)
		if quoted {
			if escaped {
				escaped = false
			} else if value == '\\' {
				escaped = true
			} else if value == '"' {
				quoted = false
			}
			continue
		}
		switch value {
		case '"':
			quoted = true
		case '{', '[':
			depth++
			if depth > maxMediaEditPacketJSONDepth {
				return nil, fmt.Errorf("media edit packet JSON exceeds its nesting budget")
			}
		case '}', ']':
			depth--
		}
	}
	return result, nil
}

// mediaEditDecodeJSON preserves numeric tokens and rejects duplicate keys at
// every depth. Callers must bound data before reading or allocating it; this
// helper also validates probe metadata whose document budget is larger than
// one packet. The normal map decoder would silently lose duplicate evidence.
func mediaEditDecodeJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("media edit JSON is not valid UTF-8")
	}
	if err := mediaEditJSONUnicode(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readMediaEditJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("media edit JSON contains trailing data")
	}
	return value, nil
}

// encoding/json replaces unpaired UTF-16 escapes with a replacement rune.
// Reject them instead so distinct invalid metadata cannot acquire one proof.
func mediaEditJSONUnicode(data []byte) error {
	quoted := false
	for position := 0; position < len(data); position++ {
		if data[position] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[position] != '\\' {
			continue
		}
		position++
		if position >= len(data) {
			return fmt.Errorf("media edit JSON has a truncated string escape")
		}
		if data[position] != 'u' {
			continue
		}
		if position+4 >= len(data) {
			return fmt.Errorf("media edit JSON has a truncated Unicode escape")
		}
		code, err := strconv.ParseUint(string(data[position+1:position+5]), 16, 16)
		if err != nil {
			return fmt.Errorf("media edit JSON has an invalid Unicode escape")
		}
		position += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return fmt.Errorf("media edit JSON has an unpaired Unicode surrogate")
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if position+6 >= len(data) || data[position+1] != '\\' || data[position+2] != 'u' {
			return fmt.Errorf("media edit JSON has an unpaired Unicode surrogate")
		}
		low, err := strconv.ParseUint(string(data[position+3:position+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return fmt.Errorf("media edit JSON has an unpaired Unicode surrogate")
		}
		position += 6
	}
	return nil
}

func readMediaEditJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxMediaEditJSONDepth {
		return nil, fmt.Errorf("media edit JSON exceeds its nesting budget")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return nil, fmt.Errorf("invalid media edit JSON object key")
			}
			if _, present := object[key]; present {
				return nil, fmt.Errorf("duplicate media edit JSON object key")
			}
			value, err := readMediaEditJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("invalid media edit JSON object end")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := readMediaEditJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("invalid media edit JSON array end")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected media edit JSON delimiter")
	}
}
