package media

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

const (
	maxVideoSeekScanBytes = 256 * 1024 * 1024
	maxVideoSeekRecords   = 8_000_000
	maxVideoSeekPending   = 4096
	maxVideoSeekLine      = 8192
)

type videoSeekHashRecord struct {
	pts, dts, size int64
	hash           string
	sideData       []videoSeekHashSideData
}

type videoSeekHashSideData struct {
	size int64
	hash string
}

type videoSeekHashStream struct {
	timeBase *big.Rat
	width    int
	height   int
	codec    string
	media    string
	seen     map[string]bool
	previous *videoSeekHashRecord
	pending  []videoSeekHashRecord
}

// ParseVideoSeekFrameHash pairs actual compressed IDRs with decoded pictures by
// exact rational PTS. Other decoded key pictures never become seek evidence.
// Parsing and pending queues are bounded independently of retained index size.
// Analysis requires an empty fifth branch to establish the complete NAL scope;
// a preflight of an already scoped index uses only the three restart branches.
// The scope guard covers packet NAL units. Initial codec extradata is supplied
// on every open of the same held source; later extradata side data is rejected.
func ParseVideoSeekFrameHash(input io.Reader, base VideoSeekIndex, maxEntries int) (VideoSeekIndex, error) {
	if input == nil || maxEntries <= 0 || maxEntries > MaxVideoSeekEntries ||
		base.TimeBaseNumerator <= 0 || base.TimeBaseDenominator <= 0 || base.DurationTicks <= 0 {
		return VideoSeekIndex{}, fmt.Errorf("invalid video seek parser configuration")
	}
	index := base
	index.Entries = nil
	streamCount := 3
	if !base.NALScopeChecked {
		streamCount = 5
	}
	streams := make([]videoSeekHashStream, streamCount)
	for number := range streams {
		streams[number].seen = make(map[string]bool)
	}
	headers := make(map[string]bool)
	started := false
	bounded := &io.LimitedReader{R: input, N: maxVideoSeekScanBytes + 1}
	reader := bufio.NewReaderSize(bounded, maxVideoSeekLine)
	records := 0
	var lastRetained *big.Rat
	interval := new(big.Rat)
	if maxEntries > 1 {
		interval.SetFrac(big.NewInt(base.DurationTicks), new(big.Int).Mul(big.NewInt(TicksPerSecond), big.NewInt(int64(maxEntries-1))))
	}
	for {
		raw, err := reader.ReadSlice('\n')
		if err == io.EOF && len(raw) == 0 {
			break
		}
		if err != nil || len(raw) > maxVideoSeekLine || bounded.N <= 0 {
			return VideoSeekIndex{}, fmt.Errorf("video seek output is truncated or exceeds its byte budget")
		}
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if started {
				return VideoSeekIndex{}, fmt.Errorf("video seek header changed after records")
			}
			if err := parseVideoSeekHashHeader(line, streams, headers); err != nil {
				return VideoSeekIndex{}, err
			}
			continue
		}
		if !started {
			if err := validateVideoSeekHashHeaders(streams, headers, base); err != nil {
				return VideoSeekIndex{}, err
			}
			started = true
		}
		records++
		if records > maxVideoSeekRecords {
			return VideoSeekIndex{}, fmt.Errorf("video seek output exceeds its record budget")
		}
		streamNumber, record, err := parseVideoSeekHashRecord(line)
		if err != nil {
			return VideoSeekIndex{}, err
		}
		if streamNumber >= 4 {
			return VideoSeekIndex{}, fmt.Errorf("video seek source contains an unsupported NAL unit")
		}
		if streamNumber >= len(streams) {
			return VideoSeekIndex{}, fmt.Errorf("unexpected video seek output stream")
		}
		stream := &streams[streamNumber]
		if stream.previous != nil && (streamNumber != 3 && record.pts <= stream.previous.pts || record.dts <= stream.previous.dts) {
			return VideoSeekIndex{}, fmt.Errorf("video seek records are not strictly ordered")
		}
		copyRecord := record
		stream.previous = &copyRecord
		if streamNumber == 3 {
			if !videoSeekSupportedPacketSideData(record.sideData) {
				return VideoSeekIndex{}, fmt.Errorf("video seek original packet has unsupported side data")
			}
			index.PacketSideDataChecked = true
			continue
		}
		if streamNumber == 2 {
			if index.ParameterSetsSHA256 != "" && index.ParameterSetsSHA256 != record.hash {
				return VideoSeekIndex{}, fmt.Errorf("video seek parameter sets are not static")
			}
			index.ParameterSetsSHA256 = record.hash
			continue
		}
		if streamNumber == 1 {
			if index.DecodedFrameBytes != 0 && index.DecodedFrameBytes != record.size {
				return VideoSeekIndex{}, fmt.Errorf("video seek decoded representation changed")
			}
			index.DecodedFrameBytes = record.size
		}
		stream.pending = append(stream.pending, record)
		for len(streams[0].pending) > 0 && len(streams[1].pending) > 0 {
			coded, decoded := streams[0].pending[0], streams[1].pending[0]
			codedTime := new(big.Rat).Mul(new(big.Rat).SetInt64(coded.pts), streams[0].timeBase)
			decodedTime := new(big.Rat).Mul(new(big.Rat).SetInt64(decoded.pts), streams[1].timeBase)
			switch codedTime.Cmp(decodedTime) {
			case -1:
				return VideoSeekIndex{}, fmt.Errorf("video seek IDR has no matching decoded picture")
			case 1:
				streams[1].pending = streams[1].pending[1:]
				continue
			}
			streams[0].pending = streams[0].pending[1:]
			streams[1].pending = streams[1].pending[1:]
			if len(index.Entries) < maxEntries && (lastRetained == nil || new(big.Rat).Sub(codedTime, lastRetained).Cmp(interval) >= 0) {
				index.Entries = append(index.Entries, VideoSeekPoint{PTS: coded.pts, DTS: coded.dts, CodedSHA256: coded.hash, DecodedSHA256: decoded.hash})
				lastRetained = codedTime
			}
		}
		if len(streams[0].pending)+len(streams[1].pending) > maxVideoSeekPending {
			return VideoSeekIndex{}, fmt.Errorf("video seek output exceeds its pending record budget")
		}
	}
	if !started || len(streams[0].pending) != 0 || streams[2].previous == nil || len(streams) > 3 && streams[3].previous == nil {
		return VideoSeekIndex{}, fmt.Errorf("video seek output has incomplete restart evidence")
	}
	index.NALScopeChecked = true
	if err := ValidateVideoSeekIndex(index); err != nil {
		return VideoSeekIndex{}, err
	}
	return index, nil
}

func parseVideoSeekHashHeader(line string, streams []videoSeekHashStream, headers map[string]bool) error {
	for _, name := range []string{"format", "version", "hash"} {
		prefix := "#" + name + ":"
		if strings.HasPrefix(line, prefix) {
			expected := map[string]string{"format": "frame checksums", "version": "2", "hash": "SHA256"}[name]
			if headers[name] || strings.TrimSpace(strings.TrimPrefix(line, prefix)) != expected {
				return fmt.Errorf("invalid or duplicate video seek hash header")
			}
			headers[name] = true
			return nil
		}
	}
	for _, name := range []string{"tb", "media_type", "codec_id", "dimensions"} {
		prefix := "#" + name + " "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, prefix), ":", 2)
		if len(parts) != 2 || len(parts[0]) != 1 || parts[0][0] < '0' || int(parts[0][0]-'0') >= len(streams) {
			return fmt.Errorf("invalid video seek stream header")
		}
		stream := &streams[int(parts[0][0]-'0')]
		if stream.seen[name] {
			return fmt.Errorf("duplicate video seek stream header")
		}
		stream.seen[name] = true
		value := strings.TrimSpace(parts[1])
		switch name {
		case "tb":
			parts := strings.Split(value, "/")
			if len(parts) != 2 {
				return fmt.Errorf("invalid video seek time base")
			}
			numerator, err1 := strconv.ParseInt(parts[0], 10, 64)
			denominator, err2 := strconv.ParseInt(parts[1], 10, 64)
			if err1 != nil || err2 != nil || numerator <= 0 || denominator <= 0 {
				return fmt.Errorf("invalid video seek time base")
			}
			stream.timeBase = videoSeekTimeBase(numerator, denominator)
		case "dimensions":
			parts := strings.Split(value, "x")
			if len(parts) != 2 {
				return fmt.Errorf("invalid video seek dimensions")
			}
			var err1, err2 error
			stream.width, err1 = strconv.Atoi(parts[0])
			stream.height, err2 = strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil {
				return fmt.Errorf("invalid video seek dimensions")
			}
		case "codec_id":
			stream.codec = value
		case "media_type":
			stream.media = value
		}
		return nil
	}
	// Extradata, sample aspect ratio, software, and column comments do not
	// authorize a restart. The line and total-output limits still cover them.
	return nil
}

func validateVideoSeekHashHeaders(streams []videoSeekHashStream, headers map[string]bool, base VideoSeekIndex) error {
	if !headers["format"] || !headers["version"] || !headers["hash"] {
		return fmt.Errorf("missing video seek hash header")
	}
	for number, stream := range streams {
		codec := "h264"
		if number == 1 {
			codec = "rawvideo"
		}
		if len(stream.seen) != 4 || stream.timeBase == nil || stream.media != "video" || stream.codec != codec ||
			stream.width != base.Width || stream.height != base.Height {
			return fmt.Errorf("video seek stream metadata does not match the source")
		}
	}
	if streams[0].timeBase.Cmp(videoSeekTimeBase(base.TimeBaseNumerator, base.TimeBaseDenominator)) != 0 {
		return fmt.Errorf("video seek packet time base does not match the source")
	}
	for number := 2; number < len(streams); number++ {
		if streams[number].timeBase.Cmp(streams[0].timeBase) != 0 {
			return fmt.Errorf("video seek packet time base does not match the source")
		}
	}
	return nil
}

func parseVideoSeekHashRecord(line string) (int, videoSeekHashRecord, error) {
	parts := strings.Split(line, ",")
	if len(parts) < 6 {
		return 0, videoSeekHashRecord{}, fmt.Errorf("invalid video seek hash record")
	}
	for position := range parts {
		parts[position] = strings.TrimSpace(parts[position])
	}
	stream, err := strconv.Atoi(parts[0])
	if err != nil || stream < 0 || stream > 4 {
		return 0, videoSeekHashRecord{}, fmt.Errorf("unexpected video seek output stream")
	}
	values := [4]int64{}
	for position := range values {
		values[position], err = strconv.ParseInt(parts[position+1], 10, 64)
		if err != nil || values[position] == -1<<63 {
			return 0, videoSeekHashRecord{}, fmt.Errorf("unknown or invalid video seek timestamp")
		}
	}
	if values[2] < 0 || values[3] <= 0 || values[3] > 512*1024*1024 || !videoSeekSHA256(parts[5]) {
		return 0, videoSeekHashRecord{}, fmt.Errorf("invalid video seek hash or size")
	}
	var sideData []videoSeekHashSideData
	if len(parts) > 6 {
		if !strings.HasPrefix(parts[6], "S=") {
			return 0, videoSeekHashRecord{}, fmt.Errorf("invalid video seek packet side data")
		}
		count, err := strconv.Atoi(strings.TrimPrefix(parts[6], "S="))
		if err != nil || count < 0 || count > 16 || len(parts) != 7+2*count {
			return 0, videoSeekHashRecord{}, fmt.Errorf("invalid video seek packet side data")
		}
		for position := 0; position < count; position++ {
			size, err := strconv.ParseInt(parts[7+2*position], 10, 64)
			if err != nil || size < 0 || size > 512*1024*1024 || !videoSeekSHA256(parts[8+2*position]) {
				return 0, videoSeekHashRecord{}, fmt.Errorf("invalid video seek packet side data")
			}
			sideData = append(sideData, videoSeekHashSideData{size: size, hash: parts[8+2*position]})
		}
	}
	return stream, videoSeekHashRecord{dts: values[0], pts: values[1], size: values[3], hash: parts[5], sideData: sideData}, nil
}

func videoSeekSupportedPacketSideData(sideData []videoSeekHashSideData) bool {
	if len(sideData) == 0 {
		return true
	}
	if len(sideData) != 1 || sideData[0].size != 1 {
		return false
	}
	// MPEG-TS attaches the video PES stream identifier as a single byte. Every
	// other side-data payload, including NEW_EXTRADATA, declines the whole index.
	for streamID := 0xe0; streamID <= 0xef; streamID++ {
		if sideData[0].hash == fmt.Sprintf("%x", sha256.Sum256([]byte{byte(streamID)})) {
			return true
		}
	}
	return false
}
