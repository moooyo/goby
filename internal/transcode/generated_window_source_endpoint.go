package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/big"
	"unicode/utf8"
)

const (
	generatedMP4MaxTopBoxes = 1024
	generatedMP4MaxBoxes    = 10_000
	generatedMP4MaxMoov     = 16 << 20
	generatedMP4MaxSamples  = 1_000_000
	generatedMP4MaxSeconds  = maxDurationTicks / ticksPerSecond
)

// GeneratedSourceEndpointCertificate describes the finite sample set declared
// by one admitted ordinary MP4 video track. It does not certify decoder EOF,
// coded payload completeness, or the number of decoded frames. Origin, Last,
// and End are absolute container-source seconds; End-Origin is the sample-set
// duration. The file-descriptor owner supplies SourceIdentity separately.
type GeneratedSourceEndpointCertificate struct {
	SourceIdentity      string
	StreamIndex         int
	TrackID             uint32
	SampleCount         int64
	FrameDuration       GeneratedRational
	Origin              GeneratedRational
	Last                GeneratedRational
	End                 GeneratedRational
	DurationTicks       int64
	DurationTicksExact  bool
	MetadataSHA256      [32]byte
	SampleExtentsSHA256 [32]byte
}

type generatedMP4Extent struct{ start, end uint64 }

type generatedMP4Box struct {
	kind string
	body []byte
}

type generatedMP4EndpointParser struct {
	ctx   context.Context
	boxes int
}

func generatedMP4EndpointError(reason string) error {
	return fmt.Errorf("%w: source MP4 endpoint: %s", ErrTimelineProbe, reason)
}

func generatedMP4EndpointLimit(reason string) error {
	return fmt.Errorf("%w: source MP4 endpoint: %s", ErrTimelineLimit, reason)
}

// parseGeneratedMP4SourceEndpoint reads bounded structural metadata and skips
// every mdat payload. Header durations are consistency checks, never the
// authority for the endpoint, which comes from joined sample and chunk tables.
func parseGeneratedMP4SourceEndpoint(ctx context.Context, reader io.ReaderAt, size int64, videoStreamIndex int) (GeneratedSourceEndpointCertificate, error) {
	var result GeneratedSourceEndpointCertificate
	if ctx == nil || reader == nil || size <= 0 || videoStreamIndex != 0 {
		return result, generatedMP4EndpointError("unsupported descriptor or stream selection")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var moov []byte
	var mdats []generatedMP4Extent
	ftypSeen := false
	for offset, count := int64(0), 0; offset < size; count++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if count >= generatedMP4MaxTopBoxes {
			return result, generatedMP4EndpointLimit("top-level box count")
		}
		var header [16]byte
		if size-offset < 8 {
			return result, generatedMP4EndpointError("truncated top-level header")
		}
		if _, err := reader.ReadAt(header[:8], offset); err != nil {
			return result, fmt.Errorf("%w: source MP4 header read: %w", ErrTimelineProbe, err)
		}
		boxSize, headerSize := uint64(binary.BigEndian.Uint32(header[:4])), int64(8)
		if boxSize == 1 {
			if size-offset < 16 {
				return result, generatedMP4EndpointError("truncated extended top-level header")
			}
			if _, err := reader.ReadAt(header[8:], offset+8); err != nil {
				return result, fmt.Errorf("%w: source MP4 extended header read: %w", ErrTimelineProbe, err)
			}
			boxSize, headerSize = binary.BigEndian.Uint64(header[8:]), 16
		}
		if boxSize < uint64(headerSize) || boxSize > uint64(size-offset) {
			return result, generatedMP4EndpointError("invalid explicit top-level box extent")
		}
		switch string(header[4:8]) {
		case "ftyp":
			if ftypSeen || boxSize-uint64(headerSize) > 4096 {
				return result, generatedMP4EndpointError("duplicate or excessive ftyp")
			}
			body := make([]byte, int(boxSize)-int(headerSize))
			if _, err := reader.ReadAt(body, offset+headerSize); err != nil {
				return result, fmt.Errorf("%w: source MP4 ftyp read: %w", ErrTimelineProbe, err)
			}
			if !generatedMP4OrdinaryBrands(body) {
				return result, generatedMP4EndpointError("unsupported ordinary MP4 brands")
			}
			ftypSeen = true
		case "moov":
			if moov != nil {
				return result, generatedMP4EndpointError("multiple movie boxes")
			}
			if boxSize > generatedMP4MaxMoov {
				return result, generatedMP4EndpointLimit("movie metadata bytes")
			}
			moov = make([]byte, int(boxSize))
			if _, err := reader.ReadAt(moov, offset); err != nil {
				return result, fmt.Errorf("%w: source MP4 movie read: %w", ErrTimelineProbe, err)
			}
		case "mdat":
			mdats = append(mdats, generatedMP4Extent{uint64(offset + headerSize), uint64(offset) + boxSize})
		case "free", "skip":
		default:
			return result, generatedMP4EndpointError("unsupported top-level box")
		}
		offset += int64(boxSize)
	}
	if !ftypSeen || moov == nil || len(mdats) == 0 {
		return result, generatedMP4EndpointError("missing ordinary MP4 structure")
	}
	p := generatedMP4EndpointParser{ctx: ctx}
	var movie generatedMP4Box
	movieCount := 0
	if err := p.walk(moov, func(box generatedMP4Box) error {
		movieCount++
		movie = box
		return nil
	}); err != nil {
		return result, err
	}
	if movieCount != 1 || movie.kind != "moov" {
		return result, generatedMP4EndpointError("movie read changed its framing")
	}
	result, err := p.movie(movie.body, mdats)
	if err != nil {
		return GeneratedSourceEndpointCertificate{}, err
	}
	result.StreamIndex = videoStreamIndex
	result.MetadataSHA256 = sha256.Sum256(moov)
	if err := ctx.Err(); err != nil {
		return GeneratedSourceEndpointCertificate{}, err
	}
	return result, nil
}

func generatedMP4OrdinaryBrands(body []byte) bool {
	if len(body) < 8 || len(body)%4 != 0 {
		return false
	}
	valid := func(brand string) bool {
		switch brand {
		case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "avc1", "mp41", "mp42":
			return true
		}
		return false
	}
	if !valid(string(body[:4])) {
		return false
	}
	for pos := 8; pos < len(body); pos += 4 {
		if !valid(string(body[pos : pos+4])) {
			return false
		}
	}
	return true
}

func (p *generatedMP4EndpointParser) walk(data []byte, visit func(generatedMP4Box) error) error {
	for len(data) != 0 {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		p.boxes++
		if p.boxes > generatedMP4MaxBoxes {
			return generatedMP4EndpointLimit("metadata box count")
		}
		if len(data) < 8 {
			return generatedMP4EndpointError("truncated metadata header")
		}
		size, header := uint64(binary.BigEndian.Uint32(data[:4])), 8
		if size == 1 {
			if len(data) < 16 {
				return generatedMP4EndpointError("truncated extended metadata header")
			}
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		}
		if size < uint64(header) || size > uint64(len(data)) {
			return generatedMP4EndpointError("invalid explicit metadata extent")
		}
		box := generatedMP4Box{string(data[4:8]), data[header:int(size)]}
		if err := visit(box); err != nil {
			return err
		}
		data = data[int(size):]
	}
	return nil
}

// children permits each named structural box once. Padding is opaque because
// the admitted demuxer treats free and skip as leaves at these positions.
func (p *generatedMP4EndpointParser) children(data []byte, allowed ...string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := p.walk(data, func(box generatedMP4Box) error {
		if box.kind == "free" || box.kind == "skip" {
			return nil
		}
		permitted := false
		for _, name := range allowed {
			permitted = permitted || box.kind == name
		}
		if !permitted {
			return generatedMP4EndpointError("unsupported structural box " + box.kind)
		}
		if _, present := result[box.kind]; present {
			return generatedMP4EndpointError("duplicate structural box " + box.kind)
		}
		result[box.kind] = box.body
		return nil
	})
	return result, err
}

func generatedMP4FullBox(body []byte, version byte, flags uint32) bool {
	return len(body) >= 4 && body[0] == version && uint32(body[1])<<16|uint32(body[2])<<8|uint32(body[3]) == flags
}

func generatedMP4ClockHeader(body []byte, movie bool) (int64, int64, error) {
	if len(body) < 4 || body[0] > 1 || !generatedMP4FullBox(body, body[0], 0) {
		return 0, 0, generatedMP4EndpointError("invalid clock header")
	}
	version := body[0]
	want := 24 + int(version)*12
	if movie {
		want = 100 + int(version)*12
	}
	if len(body) != want {
		return 0, 0, generatedMP4EndpointError("invalid clock header size")
	}
	pos := 12 + int(version)*8
	scale := int64(binary.BigEndian.Uint32(body[pos : pos+4]))
	var duration uint64
	if version == 0 {
		duration = uint64(binary.BigEndian.Uint32(body[pos+4 : pos+8]))
		if duration == math.MaxUint32 {
			return 0, 0, generatedMP4EndpointError("unknown clock duration")
		}
	} else {
		duration = binary.BigEndian.Uint64(body[pos+4 : pos+12])
	}
	if scale < 1 || scale > math.MaxInt32 || duration == 0 || duration > math.MaxInt64 || duration > uint64(generatedMP4MaxSeconds*scale) {
		return 0, 0, generatedMP4EndpointError("invalid clock scale or duration")
	}
	return scale, int64(duration), nil
}

func generatedMP4TrackHeader(body []byte) (uint32, int64, error) {
	if len(body) < 4 || body[0] > 1 || !generatedMP4FullBox(body, body[0], 3) || len(body) != 84+int(body[0])*12 {
		return 0, 0, generatedMP4EndpointError("unsupported track header")
	}
	version := body[0]
	pos := 12 + int(version)*8
	id := binary.BigEndian.Uint32(body[pos : pos+4])
	var duration uint64
	if version == 0 {
		duration = uint64(binary.BigEndian.Uint32(body[pos+8 : pos+12]))
		if duration == math.MaxUint32 {
			return 0, 0, generatedMP4EndpointError("unknown track duration")
		}
	} else {
		duration = binary.BigEndian.Uint64(body[pos+8 : pos+16])
	}
	if id == 0 || duration == 0 || duration > math.MaxInt64 || binary.BigEndian.Uint16(body[34+int(version)*12:36+int(version)*12]) != 0 {
		return 0, 0, generatedMP4EndpointError("invalid track identity, duration, or alternate group")
	}
	return id, int64(duration), nil
}

func generatedMP4Handler(body []byte, kind string) bool {
	return len(body) >= 24 && len(body) <= 4096 && generatedMP4FullBox(body, 0, 0) &&
		binary.BigEndian.Uint32(body[4:8]) == 0 && string(body[8:12]) == kind && utf8.Valid(body[24:])
}

func (p *generatedMP4EndpointParser) movie(body []byte, mdats []generatedMP4Extent) (GeneratedSourceEndpointCertificate, error) {
	var result GeneratedSourceEndpointCertificate
	moov, err := p.children(body, "mvhd", "trak", "udta")
	if err != nil {
		return result, err
	}
	movieScale, movieDuration, err := generatedMP4ClockHeader(moov["mvhd"], true)
	if err != nil {
		return result, err
	}
	if metadata, present := moov["udta"]; present {
		if err := p.userData(metadata); err != nil {
			return result, err
		}
	}
	track, err := p.children(moov["trak"], "tkhd", "edts", "mdia")
	if err != nil {
		return result, err
	}
	trackID, trackDuration, err := generatedMP4TrackHeader(track["tkhd"])
	if err != nil {
		return result, err
	}
	result.TrackID = trackID
	mdia, err := p.children(track["mdia"], "mdhd", "hdlr", "minf")
	if err != nil {
		return result, err
	}
	mediaScale, mediaDuration, err := generatedMP4ClockHeader(mdia["mdhd"], false)
	if err != nil {
		return result, err
	}
	if !generatedMP4Handler(mdia["hdlr"], "vide") {
		return result, generatedMP4EndpointError("unsupported media handler")
	}
	minf, err := p.children(mdia["minf"], "vmhd", "dinf", "stbl")
	if err != nil {
		return result, err
	}
	if len(minf["vmhd"]) != 12 || !generatedMP4FullBox(minf["vmhd"], 0, 1) {
		return result, generatedMP4EndpointError("unsupported video media header")
	}
	if err := p.dataReference(minf["dinf"]); err != nil {
		return result, err
	}
	tables, err := p.children(minf["stbl"], "stsd", "stts", "stsz", "stsc", "stco", "co64", "ctts", "stss")
	if err != nil {
		return result, err
	}
	if err := p.sampleDescription(tables["stsd"]); err != nil {
		return result, err
	}
	samples, delta, err := p.timeToSample(tables["stts"])
	if err != nil {
		return result, err
	}
	tableDuration := samples * delta
	if tableDuration != mediaDuration || tableDuration > generatedMP4MaxSeconds*mediaScale {
		return result, generatedMP4EndpointError("media duration differs from the complete sample table")
	}
	if ctts, present := tables["ctts"]; present {
		if err := p.compositionOffsets(ctts, samples); err != nil {
			return result, err
		}
	}
	if stss, present := tables["stss"]; present {
		if err := p.syncSamples(stss, samples); err != nil {
			return result, err
		}
	}
	result.SampleExtentsSHA256, err = p.sampleExtents(tables, samples, mdats)
	if err != nil {
		return result, err
	}
	mediaEditDuration := generatedMP4CeilScale(tableDuration, movieScale, mediaScale)
	leadingEmpty := int64(0)
	if edits, present := track["edts"]; present {
		leadingEmpty, err = p.edits(edits, mediaEditDuration)
		if err != nil {
			return result, err
		}
	}
	if leadingEmpty > generatedMP4MaxSeconds*movieScale || movieDuration != leadingEmpty+mediaEditDuration || trackDuration != movieDuration {
		return result, generatedMP4EndpointError("movie and track durations do not describe the untrimmed sample set")
	}
	var originProduct, originMedia, remainder big.Int
	originProduct.Mul(big.NewInt(leadingEmpty), big.NewInt(mediaScale))
	originMedia.QuoRem(&originProduct, big.NewInt(movieScale), &remainder)
	if remainder.Sign() != 0 || !originMedia.IsInt64() || originMedia.Int64() > generatedMP4MaxSeconds*mediaScale-tableDuration {
		return result, generatedMP4EndpointError("edit origin is not an exact bounded media-clock position")
	}
	origin := originMedia.Int64()
	result.SampleCount = samples
	result.FrameDuration = generatedMP4Rational(delta, mediaScale)
	result.Origin = generatedMP4Rational(origin, mediaScale)
	result.Last = generatedMP4Rational(origin+(samples-1)*delta, mediaScale)
	result.End = generatedMP4Rational(origin+tableDuration, mediaScale)
	var tickProduct, ticks big.Int
	tickProduct.Mul(big.NewInt(tableDuration), big.NewInt(ticksPerSecond))
	ticks.QuoRem(&tickProduct, big.NewInt(mediaScale), &remainder)
	if remainder.Sign() == 0 && ticks.IsInt64() {
		result.DurationTicks, result.DurationTicksExact = ticks.Int64(), true
	}
	return result, nil
}

func generatedMP4Rational(num, den int64) GeneratedRational {
	r := new(big.Rat).SetFrac(big.NewInt(num), big.NewInt(den))
	return GeneratedRational{Num: r.Num().Int64(), Den: r.Denom().Int64()}
}

func generatedMP4CeilScale(value, numerator, denominator int64) int64 {
	var product big.Int
	product.Mul(big.NewInt(value), big.NewInt(numerator))
	product.Add(&product, big.NewInt(denominator-1))
	product.Quo(&product, big.NewInt(denominator))
	return product.Int64()
}

func (p *generatedMP4EndpointParser) dataReference(body []byte) error {
	dinf, err := p.children(body, "dref")
	if err != nil {
		return err
	}
	dref := dinf["dref"]
	if len(dref) < 8 || !generatedMP4FullBox(dref, 0, 0) || binary.BigEndian.Uint32(dref[4:8]) != 1 {
		return generatedMP4EndpointError("unsupported data-reference count")
	}
	count := 0
	err = p.walk(dref[8:], func(box generatedMP4Box) error {
		count++
		if count != 1 || box.kind != "url " || len(box.body) != 4 || !generatedMP4FullBox(box.body, 0, 1) {
			return generatedMP4EndpointError("external or malformed data reference")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count != 1 {
		return generatedMP4EndpointError("missing self-contained data reference")
	}
	return nil
}

func (p *generatedMP4EndpointParser) sampleDescription(body []byte) error {
	if len(body) < 8 || !generatedMP4FullBox(body, 0, 0) || binary.BigEndian.Uint32(body[4:8]) != 1 {
		return generatedMP4EndpointError("unsupported sample-description count")
	}
	count := 0
	err := p.walk(body[8:], func(box generatedMP4Box) error {
		count++
		if count != 1 || box.kind != "avc1" || len(box.body) < 78 {
			return generatedMP4EndpointError("unsupported video sample description")
		}
		avc := box.body
		if !bytes.Equal(avc[:6], make([]byte, 6)) || binary.BigEndian.Uint16(avc[6:8]) != 1 ||
			!bytes.Equal(avc[8:24], make([]byte, 16)) || binary.BigEndian.Uint16(avc[24:26]) == 0 ||
			binary.BigEndian.Uint16(avc[26:28]) == 0 || binary.BigEndian.Uint16(avc[40:42]) != 1 ||
			binary.BigEndian.Uint16(avc[74:76]) != 24 || binary.BigEndian.Uint16(avc[76:78]) != math.MaxUint16 {
			return generatedMP4EndpointError("unsupported avc1 sample-entry fields")
		}
		children, err := p.children(avc[78:], "avcC", "pasp", "btrt", "colr")
		if err != nil {
			return err
		}
		if !generatedMP4AVCConfiguration(children["avcC"]) {
			return generatedMP4EndpointError("malformed AVC configuration")
		}
		if pasp, present := children["pasp"]; present {
			if len(pasp) != 8 || binary.BigEndian.Uint32(pasp[:4]) == 0 || binary.BigEndian.Uint32(pasp[4:]) == 0 {
				return generatedMP4EndpointError("malformed pixel aspect ratio")
			}
		}
		if btrt, present := children["btrt"]; present && len(btrt) != 12 {
			return generatedMP4EndpointError("malformed bitrate metadata")
		}
		if colr, present := children["colr"]; present {
			if len(colr) != 10 && len(colr) != 11 || len(colr) == 10 && string(colr[:4]) != "nclc" ||
				len(colr) == 11 && (string(colr[:4]) != "nclx" || colr[10]&0x7f != 0) {
				return generatedMP4EndpointError("unsupported color metadata")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count != 1 {
		return generatedMP4EndpointError("missing AVC sample entry")
	}
	return nil
}

func generatedMP4AVCConfiguration(data []byte) bool {
	if len(data) < 7 || len(data) > 64<<10 || data[0] != 1 || data[4]&0xfc != 0xfc || data[4]&3 == 2 || data[5]&0xe0 != 0xe0 || data[5]&31 == 0 {
		return false
	}
	pos := 6
	nals := func(count int, kind byte) bool {
		for index := 0; index < count; index++ {
			if len(data)-pos < 2 {
				return false
			}
			size := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			pos += 2
			if size < 1 || size > len(data)-pos || data[pos]&0x80 != 0 || data[pos]&31 != kind {
				return false
			}
			if kind == 7 && (size < 4 || data[pos+1] != data[1] || data[pos+2] != data[2] || data[pos+3] != data[3]) {
				return false
			}
			pos += size
		}
		return true
	}
	if !nals(int(data[5]&31), 7) || pos >= len(data) {
		return false
	}
	pps := int(data[pos])
	pos++
	if pps == 0 || !nals(pps, 8) {
		return false
	}
	if pos == len(data) {
		return true
	}
	switch data[1] {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244:
	default:
		return false
	}
	if len(data)-pos < 4 || data[pos]&0xfc != 0xfc || data[pos+1]&0xf8 != 0xf8 || data[pos+1]&7 > 6 || data[pos+2]&0xf8 != 0xf8 || data[pos+2]&7 > 6 {
		return false
	}
	extensions := int(data[pos+3])
	pos += 4
	return nals(extensions, 13) && pos == len(data)
}

func generatedMP4Table(body []byte, width int, allowVersionOne bool) (int, error) {
	if len(body) < 8 || body[0] > 1 || body[0] == 1 && !allowVersionOne || !generatedMP4FullBox(body, body[0], 0) {
		return 0, generatedMP4EndpointError("malformed sample table header")
	}
	count := uint64(binary.BigEndian.Uint32(body[4:8]))
	if count > generatedMP4MaxSamples {
		return 0, generatedMP4EndpointLimit("sample table entry count")
	}
	if uint64(len(body)) != 8+count*uint64(width) {
		return 0, generatedMP4EndpointError("sample table extent differs from its count")
	}
	return int(count), nil
}

func (p *generatedMP4EndpointParser) timeToSample(body []byte) (int64, int64, error) {
	entries, err := generatedMP4Table(body, 8, false)
	if err != nil {
		return 0, 0, err
	}
	var samples, delta int64
	for index := 0; index < entries; index++ {
		if err := p.ctx.Err(); err != nil {
			return 0, 0, err
		}
		entry := body[8+index*8 : 16+index*8]
		count, current := int64(binary.BigEndian.Uint32(entry[:4])), int64(binary.BigEndian.Uint32(entry[4:]))
		if count == 0 || count > generatedMP4MaxSamples-samples || current == 0 || current > math.MaxInt32 || delta != 0 && current != delta {
			return 0, 0, generatedMP4EndpointError("nonuniform or excessive time-to-sample table")
		}
		samples, delta = samples+count, current
	}
	if samples == 0 {
		return 0, 0, generatedMP4EndpointError("empty source sample set")
	}
	return samples, delta, nil
}

func (p *generatedMP4EndpointParser) compositionOffsets(body []byte, samples int64) error {
	entries, err := generatedMP4Table(body, 8, true)
	if err != nil {
		return err
	}
	var total int64
	for index := 0; index < entries; index++ {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		entry := body[8+index*8 : 16+index*8]
		count := int64(binary.BigEndian.Uint32(entry[:4]))
		if count == 0 || count > samples-total || binary.BigEndian.Uint32(entry[4:]) != 0 {
			return generatedMP4EndpointError("reordered or incomplete composition table")
		}
		total += count
	}
	if total != samples {
		return generatedMP4EndpointError("composition sample count differs")
	}
	return nil
}

func (p *generatedMP4EndpointParser) syncSamples(body []byte, samples int64) error {
	entries, err := generatedMP4Table(body, 4, false)
	if err != nil {
		return err
	}
	var previous uint32
	for index := 0; index < entries; index++ {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		id := binary.BigEndian.Uint32(body[8+index*4 : 12+index*4])
		if id <= previous || int64(id) > samples {
			return generatedMP4EndpointError("invalid sync sample order")
		}
		previous = id
	}
	return nil
}

func (p *generatedMP4EndpointParser) sampleExtents(tables map[string][]byte, samples int64, mdats []generatedMP4Extent) ([32]byte, error) {
	var result [32]byte
	sizes := tables["stsz"]
	if len(sizes) < 12 || !generatedMP4FullBox(sizes, 0, 0) || int64(binary.BigEndian.Uint32(sizes[8:12])) != samples {
		return result, generatedMP4EndpointError("sample size count differs")
	}
	fixedSize := binary.BigEndian.Uint32(sizes[4:8])
	if fixedSize == 0 && int64(len(sizes)) != 12+samples*4 || fixedSize != 0 && len(sizes) != 12 {
		return result, generatedMP4EndpointError("malformed sample size table")
	}
	chunks, width := tables["stco"], 4
	_, has32 := tables["stco"]
	_, has64 := tables["co64"]
	if has32 == has64 {
		return result, generatedMP4EndpointError("ambiguous or absent chunk offsets")
	}
	if has64 {
		chunks, width = tables["co64"], 8
	}
	chunkCount, err := generatedMP4Table(chunks, width, false)
	if err != nil {
		return result, err
	}
	if chunkCount == 0 {
		return result, generatedMP4EndpointError("empty chunk table")
	}
	mapping := tables["stsc"]
	mapCount, err := generatedMP4Table(mapping, 12, false)
	if err != nil {
		return result, err
	}
	if mapCount == 0 || mapCount > chunkCount {
		return result, generatedMP4EndpointError("invalid sample-to-chunk count")
	}
	var previousFirst uint32
	for index := 0; index < mapCount; index++ {
		if err := p.ctx.Err(); err != nil {
			return result, err
		}
		entry := mapping[8+index*12 : 20+index*12]
		first, count := binary.BigEndian.Uint32(entry[:4]), binary.BigEndian.Uint32(entry[4:8])
		if first <= previousFirst || uint64(first) > uint64(chunkCount) || index == 0 && first != 1 || count == 0 || count > generatedMP4MaxSamples || binary.BigEndian.Uint32(entry[8:12]) != 1 {
			return result, generatedMP4EndpointError("unsupported sample-to-chunk mapping")
		}
		previousFirst = first
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("goby-generated-mp4-sample-extents-v1\x00"))
	var encoded [12]byte
	var sample int64
	var previousOffset, previousEnd uint64
	mapIndex, mdatIndex := 0, 0
	for chunk := 0; chunk < chunkCount; chunk++ {
		if err := p.ctx.Err(); err != nil {
			return result, err
		}
		if mapIndex+1 < mapCount && binary.BigEndian.Uint32(mapping[8+(mapIndex+1)*12:12+(mapIndex+1)*12]) == uint32(chunk+1) {
			mapIndex++
		}
		count := int64(binary.BigEndian.Uint32(mapping[12+mapIndex*12 : 16+mapIndex*12]))
		if count > samples-sample {
			return result, generatedMP4EndpointError("chunk mapping exceeds the sample set")
		}
		pos := 8 + chunk*width
		offset := uint64(binary.BigEndian.Uint32(chunks[pos : pos+4]))
		if width == 8 {
			offset = binary.BigEndian.Uint64(chunks[pos : pos+8])
		}
		if chunk > 0 && (offset <= previousOffset || offset < previousEnd) {
			return result, generatedMP4EndpointError("overlapping or unordered chunks")
		}
		end := offset
		for index := int64(0); index < count; index++ {
			if err := p.ctx.Err(); err != nil {
				return result, err
			}
			size := fixedSize
			if size == 0 {
				pos := 12 + int(sample)*4
				size = binary.BigEndian.Uint32(sizes[pos : pos+4])
			}
			if size == 0 || uint64(size) > math.MaxUint64-end {
				return result, generatedMP4EndpointError("empty or overflowing sample extent")
			}
			binary.BigEndian.PutUint64(encoded[:8], end)
			binary.BigEndian.PutUint32(encoded[8:], size)
			_, _ = digest.Write(encoded[:])
			end += uint64(size)
			sample++
		}
		for mdatIndex < len(mdats) && offset >= mdats[mdatIndex].end {
			mdatIndex++
		}
		if mdatIndex >= len(mdats) || offset < mdats[mdatIndex].start || end > mdats[mdatIndex].end {
			return result, generatedMP4EndpointError("chunk is not wholly inside one media-data body")
		}
		previousOffset, previousEnd = offset, end
	}
	if sample != samples {
		return result, generatedMP4EndpointError("chunk mapping leaves unrepresented samples")
	}
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func (p *generatedMP4EndpointParser) edits(body []byte, mediaDuration int64) (int64, error) {
	edts, err := p.children(body, "elst")
	if err != nil {
		return 0, err
	}
	elst := edts["elst"]
	if len(elst) < 8 || elst[0] > 1 || !generatedMP4FullBox(elst, elst[0], 0) {
		return 0, generatedMP4EndpointError("malformed edit list")
	}
	count, width := int(binary.BigEndian.Uint32(elst[4:8])), 12+int(elst[0])*8
	if count < 1 || count > 2 || len(elst) != 8+count*width {
		return 0, generatedMP4EndpointError("unsupported edit-list shape")
	}
	leadingEmpty := int64(0)
	for index := 0; index < count; index++ {
		if err := p.ctx.Err(); err != nil {
			return 0, err
		}
		entry := elst[8+index*width : 8+(index+1)*width]
		var duration uint64
		var mediaTime int64
		if elst[0] == 0 {
			duration, mediaTime = uint64(binary.BigEndian.Uint32(entry[:4])), int64(int32(binary.BigEndian.Uint32(entry[4:8])))
			if duration == math.MaxUint32 {
				return 0, generatedMP4EndpointError("unknown edit duration")
			}
		} else {
			duration, mediaTime = binary.BigEndian.Uint64(entry[:8]), int64(binary.BigEndian.Uint64(entry[8:16]))
		}
		if duration == 0 || duration > math.MaxInt64 || binary.BigEndian.Uint16(entry[width-4:width-2]) != 1 || binary.BigEndian.Uint16(entry[width-2:]) != 0 {
			return 0, generatedMP4EndpointError("invalid edit duration or rate")
		}
		if count == 2 && index == 0 {
			if mediaTime != -1 {
				return 0, generatedMP4EndpointError("edit list lacks a leading empty interval")
			}
			leadingEmpty = int64(duration)
		} else if mediaTime != 0 || int64(duration) != mediaDuration {
			return 0, generatedMP4EndpointError("edit list trims or extends media")
		}
	}
	return leadingEmpty, nil
}

// User metadata is structurally inspected because the MOV demuxer's generic
// atom dispatch can interpret timing or track atoms hidden inside containers.
// Only a conventional mdir textual metadata branch is admitted here.
func (p *generatedMP4EndpointParser) userData(body []byte) error {
	udta, err := p.children(body, "meta")
	if err != nil {
		return err
	}
	meta, present := udta["meta"]
	if !present {
		return generatedMP4EndpointError("missing textual user metadata")
	}
	if !generatedMP4FullBox(meta, 0, 0) {
		return generatedMP4EndpointError("unsupported user metadata header")
	}
	// The MOV demuxer scans the prefix for hdlr before ordinary child dispatch.
	// Requiring hdlr immediately prevents padding or earlier text from supplying
	// an alternate apparent handler followed by hidden timing atoms.
	if len(meta) < 12 || string(meta[8:12]) != "hdlr" {
		return generatedMP4EndpointError("metadata handler is not the first child")
	}
	children, err := p.children(meta[4:], "hdlr", "ilst")
	if err != nil {
		return err
	}
	if !generatedMP4Handler(children["hdlr"], "mdir") {
		return generatedMP4EndpointError("unsupported user metadata handler")
	}
	items, present := children["ilst"]
	if !present {
		return generatedMP4EndpointError("missing textual metadata item list")
	}
	seen := make(map[string]bool)
	return p.walk(items, func(item generatedMP4Box) error {
		switch item.kind {
		case "\xa9too", "\xa9nam", "\xa9ART", "\xa9alb", "\xa9day", "\xa9cmt", "\xa9gen", "\xa9wrt", "aART", "desc", "ldes", "cprt":
		default:
			return generatedMP4EndpointError("unsupported textual metadata item")
		}
		if seen[item.kind] {
			return generatedMP4EndpointError("duplicate textual metadata item")
		}
		seen[item.kind] = true
		count := 0
		err := p.walk(item.body, func(data generatedMP4Box) error {
			count++
			if count != 1 || data.kind != "data" || len(data.body) < 8 || len(data.body) > (64<<10)+8 ||
				binary.BigEndian.Uint32(data.body[:4]) != 1 || binary.BigEndian.Uint32(data.body[4:8]) != 0 || !utf8.Valid(data.body[8:]) {
				return generatedMP4EndpointError("malformed textual metadata data leaf")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if count != 1 {
			return generatedMP4EndpointError("missing textual metadata data leaf")
		}
		return nil
	})
}
