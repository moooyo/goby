package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strconv"
)

const (
	generatedAVSourceMaxTracks       = 8
	generatedAVSourceProjectionBytes = 64 << 10
	generatedAVSourceAudioRate       = int64(48_000)
	generatedAVSourceAudioBlock      = int64(1024)
)

// GeneratedAVTrackEdit is an interpretation of a bounded rate-one MP4 edit
// list, not evidence that a decoder applied either trim. Movie clocks and
// media clocks remain separate. An absent edit has Explicit=false.
type GeneratedAVTrackEdit struct {
	Explicit               bool
	MovieTimeScale         int64
	LeadingEmptyMovieTicks int64
	MediaTime              int64
	DurationMovieTicks     int64
}

// GeneratedAVTrackCertificate describes the complete declared sample set of
// one track. TrackPresentationOrigin is the absolute presentation position of
// media clock zero. Coded clocks include codec blocks outside the media edit;
// MediaTableEnd preserves the potentially shorter final AAC STTS duration.
// Effective clocks interpret metadata edits and require independent decoded
// verification. EffectiveLast is the last video frame or audio sample start.
type GeneratedAVTrackCertificate struct {
	StreamIndex                                 int
	TrackID                                     uint32
	HeaderFlags                                 uint32
	AlternateGroup                              int16
	MediaTimeScale                              int64
	SampleCount                                 int64
	TableDurationUnits                          int64
	Edit                                        GeneratedAVTrackEdit
	TrackPresentationOrigin                     GeneratedRational
	CodedFirst, CodedLast, CodedEnd             GeneratedRational
	MediaTableEnd                               GeneratedRational
	EffectiveFirst, EffectiveLast, EffectiveEnd GeneratedRational
	SampleExtentsSHA256                         [32]byte
}

type GeneratedAVVideoTrackCertificate struct {
	GeneratedAVTrackCertificate
	FrameRate                      int
	FrameDuration                  GeneratedRational
	HeadTrimFrames, TailTrimFrames int64
	EffectiveFrames                int64
}

// GeneratedAVAudioTrackCertificate retains an immutable, comparable AAC ASC.
// HeadTrimSamples, TailTrimSamples and EffectiveSamples are metadata claims.
// A short last STTS delta never changes the 1024-sample coded AAC block size.
type GeneratedAVAudioTrackCertificate struct {
	GeneratedAVTrackCertificate
	AudioSpecificConfig                                              [64]byte
	AudioSpecificConfigLength                                        uint8
	SampleRate                                                       int64
	Channels                                                         int
	BlockSamples                                                     int64
	CodedSamples, HeadTrimSamples, TailTrimSamples, EffectiveSamples int64
	RollGroupPresent                                                 bool
	RollDistance                                                     int16
	RollMappedSamples                                                int64
}

// GeneratedAVSourceCertificate is a metadata candidate for selected finite
// AVC/AAC tracks, plus a freshly and independently observed demux format
// origin. It does not certify decoded head/tail coverage, effective audio,
// payload completeness, decoder EOF, resampling, remixing or publication.
// Public source-relative S maps to PresentationEpoch+S. DemuxOrigin is not an
// alias for PresentationEpoch and must not be substituted for that mapping.
type GeneratedAVSourceCertificate struct {
	SourceIdentity    string
	DemuxOrigin       GeneratedRational
	PresentationEpoch GeneratedRational
	Video             GeneratedAVVideoTrackCertificate
	Audio             GeneratedAVAudioTrackCertificate
	DurationTicks     int64
	MetadataSHA256    [32]byte
	AllExtentsSHA256  [32]byte
}

type generatedAVSourceTrack struct {
	kind   string
	common GeneratedAVTrackCertificate
	video  GeneratedAVVideoTrackCertificate
	audio  GeneratedAVAudioTrackCertificate
}

type generatedAVSourceCandidate struct {
	certificate GeneratedAVSourceCertificate
	tracks      []generatedAVSourceTrack
}

func generatedAVSourceError(reason string) error {
	return fmt.Errorf("%w: source MP4 A/V candidate: %s", ErrTimelineProbe, reason)
}

// parseGeneratedMP4AVSourceMetadata is deliberately private: metadata alone
// cannot populate a public certificate's independently observed DemuxOrigin.
// Every enumerated track is checked, including tracks not selected for output.
func parseGeneratedMP4AVSourceMetadata(ctx context.Context, reader io.ReaderAt, size int64, videoIndex, audioIndex int) (generatedAVSourceCandidate, error) {
	var empty generatedAVSourceCandidate
	if ctx == nil || reader == nil || size <= 0 || videoIndex < 0 || audioIndex < 0 || videoIndex == audioIndex ||
		videoIndex >= generatedAVSourceMaxTracks || audioIndex >= generatedAVSourceMaxTracks {
		return empty, generatedAVSourceError("unsupported descriptor or stream selection")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var moov []byte
	var mdats []generatedMP4Extent
	ftypSeen := false
	for offset, count := int64(0), 0; offset < size; count++ {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if count >= generatedMP4MaxTopBoxes {
			return empty, generatedMP4EndpointLimit("A/V top-level box count")
		}
		var header [16]byte
		if size-offset < 8 {
			return empty, generatedAVSourceError("truncated top-level header")
		}
		if _, err := reader.ReadAt(header[:8], offset); err != nil {
			return empty, fmt.Errorf("%w: A/V source header read: %w", ErrTimelineProbe, err)
		}
		boxSize, headerSize := uint64(binary.BigEndian.Uint32(header[:4])), int64(8)
		if boxSize == 1 {
			if size-offset < 16 {
				return empty, generatedAVSourceError("truncated extended top-level header")
			}
			if _, err := reader.ReadAt(header[8:], offset+8); err != nil {
				return empty, fmt.Errorf("%w: A/V source extended header read: %w", ErrTimelineProbe, err)
			}
			boxSize, headerSize = binary.BigEndian.Uint64(header[8:]), 16
		}
		if boxSize < uint64(headerSize) || boxSize > uint64(size-offset) {
			return empty, generatedAVSourceError("invalid explicit top-level extent")
		}
		switch string(header[4:8]) {
		case "ftyp":
			if ftypSeen || boxSize-uint64(headerSize) > 4096 {
				return empty, generatedAVSourceError("duplicate or excessive ftyp")
			}
			body := make([]byte, int(boxSize)-int(headerSize))
			if _, err := reader.ReadAt(body, offset+headerSize); err != nil {
				return empty, fmt.Errorf("%w: A/V source ftyp read: %w", ErrTimelineProbe, err)
			}
			if !generatedMP4OrdinaryBrands(body) {
				return empty, generatedAVSourceError("unsupported ordinary MP4 brands")
			}
			ftypSeen = true
		case "moov":
			if moov != nil {
				return empty, generatedAVSourceError("multiple movie boxes")
			}
			if boxSize > generatedMP4MaxMoov {
				return empty, generatedMP4EndpointLimit("A/V movie metadata bytes")
			}
			moov = make([]byte, int(boxSize))
			if _, err := reader.ReadAt(moov, offset); err != nil {
				return empty, fmt.Errorf("%w: A/V source movie read: %w", ErrTimelineProbe, err)
			}
		case "mdat":
			mdats = append(mdats, generatedMP4Extent{start: uint64(offset + headerSize), end: uint64(offset) + boxSize})
		case "free", "skip":
		default:
			return empty, generatedAVSourceError("unsupported top-level box")
		}
		offset += int64(boxSize)
	}
	if !ftypSeen || moov == nil || len(mdats) == 0 {
		return empty, generatedAVSourceError("missing ordinary MP4 structure")
	}
	p := generatedMP4EndpointParser{ctx: ctx}
	var movie generatedMP4Box
	count := 0
	if err := p.walk(moov, func(box generatedMP4Box) error { count++; movie = box; return nil }); err != nil {
		return empty, err
	}
	if count != 1 || movie.kind != "moov" {
		return empty, generatedAVSourceError("movie framing changed")
	}
	var mvhd, udta []byte
	var trackBodies [][]byte
	if err := p.walk(movie.body, func(box generatedMP4Box) error {
		switch box.kind {
		case "mvhd":
			if mvhd != nil {
				return generatedAVSourceError("duplicate movie clock")
			}
			mvhd = box.body
		case "udta":
			if udta != nil {
				return generatedAVSourceError("duplicate movie metadata")
			}
			udta = box.body
		case "trak":
			if len(trackBodies) == generatedAVSourceMaxTracks {
				return generatedMP4EndpointLimit("A/V track count")
			}
			trackBodies = append(trackBodies, box.body)
		case "free", "skip":
		default:
			return generatedAVSourceError("unsupported movie child " + box.kind)
		}
		return nil
	}); err != nil {
		return empty, err
	}
	movieScale, movieDuration, err := generatedMP4ClockHeader(mvhd, true)
	if err != nil {
		return empty, err
	}
	if udta != nil {
		if err := p.userData(udta); err != nil {
			return empty, err
		}
	}
	if videoIndex >= len(trackBodies) || audioIndex >= len(trackBodies) {
		return empty, generatedAVSourceError("selected stream absent")
	}
	result := generatedAVSourceCandidate{}
	ids := make(map[uint32]bool)
	var extents []generatedMP4Extent
	var totalSamples, maximumMovieEnd int64
	digest := sha256.New()
	_, _ = digest.Write([]byte("goby-generated-av-all-sample-extents-v1\x00"))
	for index, body := range trackBodies {
		track, chunks, err := parseGeneratedAVSourceTrack(&p, body, index, movieScale, mdats)
		if err != nil {
			return empty, err
		}
		if ids[track.common.TrackID] {
			return empty, generatedAVSourceError("duplicate track identity")
		}
		ids[track.common.TrackID] = true
		if track.common.SampleCount > generatedMP4MaxSamples-totalSamples {
			return empty, generatedMP4EndpointLimit("A/V aggregate sample count")
		}
		totalSamples += track.common.SampleCount
		end := track.common.Edit.LeadingEmptyMovieTicks + track.common.Edit.DurationMovieTicks
		if end > maximumMovieEnd {
			maximumMovieEnd = end
		}
		extents = append(extents, chunks...)
		var tuple [16]byte
		binary.BigEndian.PutUint32(tuple[:4], uint32(index))
		binary.BigEndian.PutUint32(tuple[4:8], track.common.TrackID)
		binary.BigEndian.PutUint64(tuple[8:], uint64(track.common.SampleCount))
		_, _ = digest.Write(tuple[:])
		_, _ = digest.Write(track.common.SampleExtentsSHA256[:])
		result.tracks = append(result.tracks, track)
	}
	if movieDuration != maximumMovieEnd {
		return empty, generatedAVSourceError("movie duration differs from complete track edits")
	}
	sort.Slice(extents, func(i, j int) bool { return extents[i].start < extents[j].start })
	for index := 1; index < len(extents); index++ {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if extents[index].start < extents[index-1].end {
			return empty, generatedAVSourceError("sample extents overlap across tracks")
		}
	}
	video, audio := result.tracks[videoIndex], result.tracks[audioIndex]
	if video.kind != "vide" || audio.kind != "soun" {
		return empty, generatedAVSourceError("selected stream kinds differ")
	}
	if video.common.EffectiveFirst != audio.common.EffectiveFirst || video.common.EffectiveEnd != audio.common.EffectiveEnd {
		return empty, generatedAVSourceError("selected effective track endpoints do not align")
	}
	duration := new(big.Rat).Sub(generatedAVSourceRat(video.common.EffectiveEnd), generatedAVSourceRat(video.common.EffectiveFirst))
	ticks := new(big.Rat).Mul(duration, new(big.Rat).SetInt64(ticksPerSecond))
	if !ticks.IsInt() || !ticks.Num().IsInt64() || ticks.Num().Int64() <= 0 || ticks.Num().Int64() > maxDurationTicks {
		return empty, generatedAVSourceError("selected duration is not an exact bounded tick interval")
	}
	result.certificate.Video, result.certificate.Audio = video.video, audio.audio
	result.certificate.PresentationEpoch = video.common.EffectiveFirst
	result.certificate.DurationTicks = ticks.Num().Int64()
	result.certificate.MetadataSHA256 = sha256.Sum256(moov)
	copy(result.certificate.AllExtentsSHA256[:], digest.Sum(nil))
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}

func generatedAVSourceRat(value GeneratedRational) *big.Rat {
	return new(big.Rat).SetFrac64(value.Num, value.Den)
}

func generatedAVExactScale(value, numerator, denominator int64) (int64, error) {
	var product, quotient, remainder big.Int
	product.Mul(big.NewInt(value), big.NewInt(numerator))
	quotient.QuoRem(&product, big.NewInt(denominator), &remainder)
	if remainder.Sign() != 0 || !quotient.IsInt64() {
		return 0, generatedAVSourceError("clock conversion is not exact")
	}
	return quotient.Int64(), nil
}

func parseGeneratedAVSourceTrack(p *generatedMP4EndpointParser, body []byte, index int, movieScale int64, mdats []generatedMP4Extent) (generatedAVSourceTrack, []generatedMP4Extent, error) {
	var empty generatedAVSourceTrack
	track, err := p.children(body, "tkhd", "edts", "mdia")
	if err != nil {
		return empty, nil, err
	}
	mdia, err := p.children(track["mdia"], "mdhd", "hdlr", "minf")
	if err != nil {
		return empty, nil, err
	}
	scale, mediaDuration, err := generatedMP4ClockHeader(mdia["mdhd"], false)
	if err != nil {
		return empty, nil, err
	}
	kind := ""
	if generatedMP4Handler(mdia["hdlr"], "vide") {
		kind = "vide"
	} else if generatedMP4Handler(mdia["hdlr"], "soun") {
		kind = "soun"
	}
	if kind == "" {
		return empty, nil, generatedAVSourceError("unsupported media handler")
	}
	id, trackDuration, headerFlags, alternateGroup, err := generatedAVSourceTrackHeader(track["tkhd"], kind)
	if err != nil {
		return empty, nil, err
	}
	minf, err := p.children(mdia["minf"], "vmhd", "smhd", "dinf", "stbl")
	if err != nil {
		return empty, nil, err
	}
	if kind == "vide" {
		if _, present := minf["smhd"]; present || len(minf["vmhd"]) != 12 || !generatedMP4FullBox(minf["vmhd"], 0, 1) {
			return empty, nil, generatedAVSourceError("unsupported video media header")
		}
	} else {
		if _, present := minf["vmhd"]; present || len(minf["smhd"]) != 8 || !bytes.Equal(minf["smhd"], make([]byte, 8)) {
			return empty, nil, generatedAVSourceError("unsupported audio media header")
		}
	}
	if err := p.dataReference(minf["dinf"]); err != nil {
		return empty, nil, err
	}
	tables, err := p.children(minf["stbl"], "stsd", "stts", "stsz", "stsc", "stco", "co64", "ctts", "stss", "sgpd", "sbgp")
	if err != nil {
		return empty, nil, err
	}
	result := generatedAVSourceTrack{kind: kind}
	var samples, delta, tableDuration int64
	if kind == "vide" {
		if err := p.sampleDescription(tables["stsd"]); err != nil {
			return empty, nil, err
		}
		samples, delta, err = p.timeToSample(tables["stts"])
		if err != nil {
			return empty, nil, err
		}
		if scale%delta != 0 || scale/delta < 1 || scale/delta > 120 {
			return empty, nil, generatedAVSourceError("video cadence is not bounded integer CFR")
		}
		tableDuration = samples * delta
		result.video.FrameRate = int(scale / delta)
		result.video.FrameDuration = generatedMP4Rational(delta, scale)
	} else {
		if scale != generatedAVSourceAudioRate {
			return empty, nil, generatedAVSourceError("audio media scale is not native 48 kHz")
		}
		config, channels, err := generatedAVAACDescription(p, tables["stsd"])
		if err != nil {
			return empty, nil, err
		}
		copy(result.audio.AudioSpecificConfig[:], config)
		result.audio.AudioSpecificConfigLength = uint8(len(config))
		result.audio.Channels = channels
		result.audio.SampleRate = scale
		result.audio.BlockSamples = generatedAVSourceAudioBlock
		samples, tableDuration, err = generatedAVAACSampleTimes(p, tables["stts"])
		if err != nil {
			return empty, nil, err
		}
		delta = generatedAVSourceAudioBlock
	}
	if tableDuration != mediaDuration || tableDuration > generatedMP4MaxSeconds*scale {
		return empty, nil, generatedAVSourceError("media duration differs from full sample table")
	}
	if ctts, present := tables["ctts"]; present {
		if err := p.compositionOffsets(ctts, samples); err != nil {
			return empty, nil, err
		}
	}
	if stss, present := tables["stss"]; present {
		if kind != "vide" {
			return empty, nil, generatedAVSourceError("audio sync table is unsupported")
		}
		if err := p.syncSamples(stss, samples); err != nil {
			return empty, nil, err
		}
	}
	rollPresent, rollSamples, err := generatedAVSourceRollGroups(p, tables, samples, kind)
	if err != nil {
		return empty, nil, err
	}
	if rollPresent {
		result.audio.RollGroupPresent = true
		result.audio.RollDistance = -1
		result.audio.RollMappedSamples = rollSamples
	}
	extentHash, err := p.sampleExtents(tables, samples, mdats)
	if err != nil {
		return empty, nil, err
	}
	chunks, err := generatedAVValidatedChunkExtents(p, tables, samples)
	if err != nil {
		return empty, nil, err
	}
	edit, effectiveUnits, err := generatedAVSourceEdits(p, track["edts"], movieScale, scale, tableDuration)
	if err != nil {
		return empty, nil, err
	}
	if edit.LeadingEmptyMovieTicks > generatedMP4MaxSeconds*movieScale-edit.DurationMovieTicks || trackDuration != edit.LeadingEmptyMovieTicks+edit.DurationMovieTicks {
		return empty, nil, generatedAVSourceError("track duration differs from its supported edit")
	}
	if kind == "vide" {
		if edit.MediaTime%delta != 0 || effectiveUnits%delta != 0 {
			return empty, nil, generatedAVSourceError("video edit does not preserve whole frames")
		}
		result.video.HeadTrimFrames = edit.MediaTime / delta
		result.video.EffectiveFrames = effectiveUnits / delta
		result.video.TailTrimFrames = samples - result.video.HeadTrimFrames - result.video.EffectiveFrames
	} else {
		coded := samples * generatedAVSourceAudioBlock
		tail := coded - edit.MediaTime - effectiveUnits
		if edit.MediaTime > generatedAVSourceAudioBlock || tail < 0 || tail >= generatedAVSourceAudioBlock {
			return empty, nil, generatedAVSourceError("AAC edit exceeds the bounded priming and final-padding profile")
		}
		result.audio.CodedSamples = coded
		result.audio.HeadTrimSamples = edit.MediaTime
		result.audio.TailTrimSamples = tail
		result.audio.EffectiveSamples = effectiveUnits
	}
	first := new(big.Rat).SetFrac64(edit.LeadingEmptyMovieTicks, movieScale)
	origin := new(big.Rat).Sub(new(big.Rat).Set(first), new(big.Rat).SetFrac64(edit.MediaTime, scale))
	end := new(big.Rat).Add(new(big.Rat).Set(first), new(big.Rat).SetFrac64(effectiveUnits, scale))
	lastUnit := delta
	if kind == "soun" {
		lastUnit = 1
	}
	common := GeneratedAVTrackCertificate{StreamIndex: index, TrackID: id, HeaderFlags: headerFlags, AlternateGroup: alternateGroup,
		MediaTimeScale: scale, SampleCount: samples, TableDurationUnits: tableDuration, Edit: edit, SampleExtentsSHA256: extentHash}
	for _, conversion := range []struct {
		value *big.Rat
		field *GeneratedRational
	}{
		{origin, &common.TrackPresentationOrigin}, {new(big.Rat).Set(origin), &common.CodedFirst},
		{new(big.Rat).Add(new(big.Rat).Set(origin), new(big.Rat).SetFrac64((samples-1)*delta, scale)), &common.CodedLast},
		{new(big.Rat).Add(new(big.Rat).Set(origin), new(big.Rat).SetFrac64(samples*delta, scale)), &common.CodedEnd},
		{new(big.Rat).Add(new(big.Rat).Set(origin), new(big.Rat).SetFrac64(tableDuration, scale)), &common.MediaTableEnd},
		{first, &common.EffectiveFirst}, {new(big.Rat).Sub(new(big.Rat).Set(end), new(big.Rat).SetFrac64(lastUnit, scale)), &common.EffectiveLast}, {end, &common.EffectiveEnd},
	} {
		if generatedRatAbs(conversion.value).Cmp(generatedTicksSeconds(maxDurationTicks)) > 0 {
			return empty, nil, generatedAVSourceError("track presentation clock exceeds bounds")
		}
		*conversion.field, err = generatedSourceRangeRational(conversion.value)
		if err != nil {
			return empty, nil, err
		}
	}
	result.common = common
	result.video.GeneratedAVTrackCertificate = common
	result.audio.GeneratedAVTrackCertificate = common
	return result, chunks, nil
}

// Explicit stream selection does not depend on the container's default enabled
// track. Native ordinary MP4 can retain an in-movie disabled alternate video
// track and group AAC tracks under alternate_group=1. Preserve these values;
// do not normalize them or widen the video-only endpoint's separate contract.
func generatedAVSourceTrackHeader(body []byte, kind string) (uint32, int64, uint32, int16, error) {
	if len(body) < 4 || body[0] > 1 || len(body) != 84+int(body[0])*12 ||
		!generatedMP4FullBox(body, body[0], 2) && !generatedMP4FullBox(body, body[0], 3) {
		return 0, 0, 0, 0, generatedAVSourceError("unsupported A/V track header")
	}
	version := int(body[0])
	pos := 12 + version*8
	id := binary.BigEndian.Uint32(body[pos : pos+4])
	var duration uint64
	if version == 0 {
		duration = uint64(binary.BigEndian.Uint32(body[pos+8 : pos+12]))
		if duration == math.MaxUint32 {
			return 0, 0, 0, 0, generatedAVSourceError("unknown A/V track duration")
		}
	} else {
		duration = binary.BigEndian.Uint64(body[pos+8 : pos+16])
	}
	flags := binary.BigEndian.Uint32(body[:4]) & 0x00ffffff
	group := int16(binary.BigEndian.Uint16(body[34+version*12 : 36+version*12]))
	if id == 0 || duration == 0 || duration > math.MaxInt64 ||
		kind == "vide" && group != 0 || kind == "soun" && group != 0 && group != 1 || kind != "vide" && kind != "soun" {
		return 0, 0, 0, 0, generatedAVSourceError("unsupported A/V track identity, duration or alternate group")
	}
	return id, int64(duration), flags, group, nil
}

func generatedAVSourceEdits(p *generatedMP4EndpointParser, body []byte, movieScale, mediaScale, tableDuration int64) (GeneratedAVTrackEdit, int64, error) {
	result := GeneratedAVTrackEdit{MovieTimeScale: movieScale}
	if body == nil {
		duration, err := generatedAVExactScale(tableDuration, movieScale, mediaScale)
		if err != nil {
			return GeneratedAVTrackEdit{}, 0, err
		}
		result.DurationMovieTicks = duration
		return result, tableDuration, nil
	}
	children, err := p.children(body, "elst")
	if err != nil {
		return GeneratedAVTrackEdit{}, 0, err
	}
	elst := children["elst"]
	if len(elst) < 8 || elst[0] > 1 || !generatedMP4FullBox(elst, elst[0], 0) {
		return GeneratedAVTrackEdit{}, 0, generatedAVSourceError("malformed edit list")
	}
	count, width := int(binary.BigEndian.Uint32(elst[4:8])), 12+int(elst[0])*8
	if count < 1 || count > 2 || len(elst) != 8+count*width {
		return GeneratedAVTrackEdit{}, 0, generatedAVSourceError("unsupported edit shape")
	}
	result.Explicit = true
	for index := 0; index < count; index++ {
		entry := elst[8+index*width : 8+(index+1)*width]
		var duration uint64
		var mediaTime int64
		if elst[0] == 0 {
			duration = uint64(binary.BigEndian.Uint32(entry[:4]))
			mediaTime = int64(int32(binary.BigEndian.Uint32(entry[4:8])))
		} else {
			duration = binary.BigEndian.Uint64(entry[:8])
			mediaTime = int64(binary.BigEndian.Uint64(entry[8:16]))
		}
		if duration == 0 || duration > uint64(generatedMP4MaxSeconds*movieScale) || elst[0] == 0 && duration == math.MaxUint32 ||
			binary.BigEndian.Uint16(entry[width-4:width-2]) != 1 || binary.BigEndian.Uint16(entry[width-2:]) != 0 {
			return GeneratedAVTrackEdit{}, 0, generatedAVSourceError("unsupported edit duration or rate")
		}
		if count == 2 && index == 0 {
			if mediaTime != -1 {
				return GeneratedAVTrackEdit{}, 0, generatedAVSourceError("edit lacks a leading empty interval")
			}
			result.LeadingEmptyMovieTicks = int64(duration)
		} else {
			if mediaTime < 0 {
				return GeneratedAVTrackEdit{}, 0, generatedAVSourceError("negative media edit time")
			}
			result.MediaTime = mediaTime
			result.DurationMovieTicks = int64(duration)
		}
	}
	effective, err := generatedAVExactScale(result.DurationMovieTicks, mediaScale, movieScale)
	if err != nil {
		return GeneratedAVTrackEdit{}, 0, err
	}
	if effective <= 0 || result.MediaTime > tableDuration-effective {
		return GeneratedAVTrackEdit{}, 0, generatedAVSourceError("edit trims beyond the complete sample table")
	}
	return result, effective, nil
}

func generatedAVAACSampleTimes(p *generatedMP4EndpointParser, body []byte) (int64, int64, error) {
	entries, err := generatedMP4Table(body, 8, false)
	if err != nil {
		return 0, 0, err
	}
	var samples, duration int64
	for index := 0; index < entries; index++ {
		if err := p.ctx.Err(); err != nil {
			return 0, 0, err
		}
		entry := body[8+index*8 : 16+index*8]
		count, delta := int64(binary.BigEndian.Uint32(entry[:4])), int64(binary.BigEndian.Uint32(entry[4:]))
		if count == 0 || count > generatedMP4MaxSamples-samples || delta < 1 || delta > generatedAVSourceAudioBlock || delta != generatedAVSourceAudioBlock && (index != entries-1 || count != 1) {
			return 0, 0, generatedAVSourceError("AAC STTS is not full blocks with an optional short final declaration")
		}
		samples += count
		duration += count * delta
	}
	if samples == 0 {
		return 0, 0, generatedAVSourceError("empty AAC sample set")
	}
	return samples, duration, nil
}

// This declaration records codec preroll dependencies. It cannot prove decoder
// history preservation, independently restartable segments or audible bounds.
func generatedAVSourceRollGroups(p *generatedMP4EndpointParser, tables map[string][]byte, samples int64, kind string) (bool, int64, error) {
	sgpd, hasDescription := tables["sgpd"]
	sbgp, hasMapping := tables["sbgp"]
	if !hasDescription && !hasMapping {
		return false, 0, nil
	}
	if kind != "soun" || hasDescription != hasMapping || len(sgpd) != 18 || !generatedMP4FullBox(sgpd, 1, 0) || string(sgpd[4:8]) != "roll" ||
		binary.BigEndian.Uint32(sgpd[8:12]) != 2 || binary.BigEndian.Uint32(sgpd[12:16]) != 1 || int16(binary.BigEndian.Uint16(sgpd[16:18])) != -1 {
		return false, 0, generatedAVSourceError("unsupported AAC roll-group description")
	}
	if len(sbgp) < 12 || !generatedMP4FullBox(sbgp, 0, 0) || string(sbgp[4:8]) != "roll" {
		return false, 0, generatedAVSourceError("unsupported AAC roll-group mapping")
	}
	entries := uint64(binary.BigEndian.Uint32(sbgp[8:12]))
	if entries == 0 || entries > generatedMP4MaxSamples || uint64(len(sbgp)) != 12+entries*8 {
		return false, 0, generatedAVSourceError("invalid AAC roll-group extent")
	}
	var total int64
	for offset := 12; offset < len(sbgp); offset += 8 {
		if err := p.ctx.Err(); err != nil {
			return false, 0, err
		}
		count := int64(binary.BigEndian.Uint32(sbgp[offset : offset+4]))
		if count <= 0 || count > samples-total || binary.BigEndian.Uint32(sbgp[offset+4:offset+8]) != 1 {
			return false, 0, generatedAVSourceError("AAC roll mapping is incomplete or ambiguous")
		}
		total += count
	}
	if total != samples {
		return false, 0, generatedAVSourceError("AAC roll mapping count differs from sample set")
	}
	return true, total, nil
}

// The existing extent verifier establishes all indexing preconditions here.
// Chunk bodies are retained only for an aggregate across-track overlap check.
func generatedAVValidatedChunkExtents(p *generatedMP4EndpointParser, tables map[string][]byte, samples int64) ([]generatedMP4Extent, error) {
	chunks, width := tables["stco"], 4
	if value, present := tables["co64"]; present {
		chunks, width = value, 8
	}
	count := int(binary.BigEndian.Uint32(chunks[4:8]))
	mapping, sizes := tables["stsc"], tables["stsz"]
	mapCount := int(binary.BigEndian.Uint32(mapping[4:8]))
	fixed := binary.BigEndian.Uint32(sizes[4:8])
	result := make([]generatedMP4Extent, 0, count)
	mapIndex := 0
	var sample int64
	for chunk := 0; chunk < count; chunk++ {
		if err := p.ctx.Err(); err != nil {
			return nil, err
		}
		if mapIndex+1 < mapCount && binary.BigEndian.Uint32(mapping[8+(mapIndex+1)*12:12+(mapIndex+1)*12]) == uint32(chunk+1) {
			mapIndex++
		}
		chunkSamples := int64(binary.BigEndian.Uint32(mapping[12+mapIndex*12 : 16+mapIndex*12]))
		pos := 8 + chunk*width
		offset := uint64(binary.BigEndian.Uint32(chunks[pos : pos+4]))
		if width == 8 {
			offset = binary.BigEndian.Uint64(chunks[pos : pos+8])
		}
		end := offset
		for index := int64(0); index < chunkSamples; index++ {
			if err := p.ctx.Err(); err != nil {
				return nil, err
			}
			size := fixed
			if size == 0 {
				pos := 12 + int(sample)*4
				size = binary.BigEndian.Uint32(sizes[pos : pos+4])
			}
			end += uint64(size)
			sample++
		}
		result = append(result, generatedMP4Extent{start: offset, end: end})
	}
	if sample != samples {
		return nil, generatedAVSourceError("validated chunk traversal changed its count")
	}
	return result, nil
}

func generatedAVAACDescription(p *generatedMP4EndpointParser, body []byte) ([]byte, int, error) {
	if len(body) < 8 || !generatedMP4FullBox(body, 0, 0) || binary.BigEndian.Uint32(body[4:8]) != 1 {
		return nil, 0, generatedAVSourceError("unsupported AAC description count")
	}
	var config []byte
	channels, count := 0, 0
	err := p.walk(body[8:], func(box generatedMP4Box) error {
		count++
		if count != 1 || box.kind != "mp4a" || len(box.body) < 28 {
			return generatedAVSourceError("unsupported AAC sample entry")
		}
		entry := box.body
		entryChannels := int(binary.BigEndian.Uint16(entry[16:18]))
		if !bytes.Equal(entry[:6], make([]byte, 6)) || binary.BigEndian.Uint16(entry[6:8]) != 1 || !bytes.Equal(entry[8:16], make([]byte, 8)) ||
			(entryChannels != 1 && entryChannels != 2) || binary.BigEndian.Uint16(entry[18:20]) != 16 || !bytes.Equal(entry[20:24], make([]byte, 4)) || binary.BigEndian.Uint32(entry[24:28]) != uint32(generatedAVSourceAudioRate<<16) {
			return generatedAVSourceError("unsupported native AAC sample-entry fields")
		}
		children, err := p.children(entry[28:], "esds", "btrt")
		if err != nil {
			return err
		}
		if btrt, present := children["btrt"]; present && len(btrt) != 12 {
			return generatedAVSourceError("malformed AAC bitrate metadata")
		}
		config, channels, err = generatedAVAACESDS(children["esds"])
		if err != nil {
			return err
		}
		if entryChannels != channels {
			return generatedAVSourceError("AAC channel declarations disagree")
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if count != 1 {
		return nil, 0, generatedAVSourceError("missing native AAC description")
	}
	return config, channels, nil
}

func generatedAVDescriptor(data []byte, offset int) (byte, []byte, int, error) {
	if len(data)-offset < 2 {
		return 0, nil, 0, generatedAVSourceError("truncated AAC descriptor")
	}
	tag := data[offset]
	offset++
	size := 0
	for index := 0; index < 4; index++ {
		if offset >= len(data) {
			break
		}
		value := data[offset]
		offset++
		size = size<<7 | int(value&0x7f)
		if value&0x80 == 0 {
			if size > len(data)-offset {
				return 0, nil, 0, generatedAVSourceError("AAC descriptor exceeds its parent")
			}
			return tag, data[offset : offset+size], offset + size, nil
		}
	}
	return 0, nil, 0, generatedAVSourceError("unbounded AAC descriptor length")
}

func generatedAVAACESDS(data []byte) ([]byte, int, error) {
	if len(data) > 4096 || !generatedMP4FullBox(data, 0, 0) {
		return nil, 0, generatedAVSourceError("unsupported AAC ESDS")
	}
	tag, es, end, err := generatedAVDescriptor(data, 4)
	if err != nil {
		return nil, 0, err
	}
	if tag != 3 || end != len(data) || len(es) < 3 || es[2]&0xe0 != 0 {
		return nil, 0, generatedAVSourceError("external or malformed AAC ES descriptor")
	}
	var config []byte
	decoderSeen, slSeen := false, false
	for offset := 3; offset < len(es); {
		tag, body, next, err := generatedAVDescriptor(es, offset)
		if err != nil {
			return nil, 0, err
		}
		switch tag {
		case 4:
			if decoderSeen || len(body) < 13 || body[0] != 0x40 || body[1] != 0x15 {
				return nil, 0, generatedAVSourceError("unsupported AAC decoder declaration")
			}
			decoderSeen = true
			kind, asc, finish, err := generatedAVDescriptor(body, 13)
			if err != nil {
				return nil, 0, err
			}
			if kind != 5 || finish != len(body) || !generatedAVNativeAACASC(asc) {
				return nil, 0, generatedAVSourceError("AAC is outside native LC 48 kHz mono/stereo 1024-block profile")
			}
			config = bytes.Clone(asc)
		case 6:
			if slSeen || !bytes.Equal(body, []byte{2}) {
				return nil, 0, generatedAVSourceError("unsupported AAC synchronization descriptor")
			}
			slSeen = true
		default:
			return nil, 0, generatedAVSourceError("unsupported AAC descriptor")
		}
		offset = next
	}
	if !decoderSeen || !slSeen {
		return nil, 0, generatedAVSourceError("missing AAC configuration")
	}
	channels := int(config[1] >> 3 & 15)
	return config, channels, nil
}

func generatedAVNativeAACASC(data []byte) bool {
	// Native AAC-LC may append an SBR-absent sync extension. No implicit
	// SBR/PS, PCE, core dependency, 960-sample blocks or arbitrary padding.
	return (len(data) == 2 || len(data) == 5 && bytes.Equal(data[2:], []byte{0x56, 0xe5, 0})) && data[0] == 0x11 && (data[1] == 0x88 || data[1] == 0x90)
}

type generatedAVDemuxDocument struct {
	Streams      []json.RawMessage `json:"streams"`
	Format       json.RawMessage   `json:"format"`
	Programs     []json.RawMessage `json:"programs"`
	StreamGroups []json.RawMessage `json:"stream_groups"`
}

type generatedAVDemuxStream struct {
	Index      *int64          `json:"index"`
	ID         string          `json:"id"`
	Kind       string          `json:"codec_type"`
	Codec      string          `json:"codec_name"`
	TimeBase   string          `json:"time_base"`
	StartPTS   *int64          `json:"start_pts"`
	SampleRate string          `json:"sample_rate"`
	Channels   *int64          `json:"channels"`
	SideData   json.RawMessage `json:"side_data_list"`
}

// bindGeneratedAVDemuxOrigin checks fresh demux stream identities and starts
// against the metadata interpretation, then retains format.start_time as F.
// An unselected earlier track may make F different from selected effective P.
func bindGeneratedAVDemuxOrigin(candidate generatedAVSourceCandidate, data []byte) (GeneratedAVSourceCertificate, error) {
	var empty GeneratedAVSourceCertificate
	if len(data) > generatedAVSourceProjectionBytes {
		return empty, generatedMP4EndpointLimit("A/V demux projection bytes")
	}
	if len(data) == 0 || len(candidate.tracks) < 2 || len(candidate.tracks) > generatedAVSourceMaxTracks {
		return empty, generatedAVSourceError("invalid demux projection extent")
	}
	if err := generatedUniqueJSON(data); err != nil {
		return empty, err
	}
	var document generatedAVDemuxDocument
	if err := generatedBoundsDecodeRecord(data, &document, "streams", "format", "programs", "stream_groups"); err != nil || len(document.Streams) != len(candidate.tracks) || len(document.Programs) != 0 || len(document.StreamGroups) != 0 {
		return empty, generatedAVSourceError("demux track inventory differs")
	}
	var firstDemuxStart *big.Rat
	for index, raw := range document.Streams {
		var stream generatedAVDemuxStream
		if err := generatedBoundsDecodeRecord(raw, &stream, "index", "id", "codec_type", "codec_name", "time_base", "start_pts", "sample_rate", "channels", "side_data_list"); err != nil {
			return empty, err
		}
		track := candidate.tracks[index]
		id, idErr := strconv.ParseUint(stream.ID, 0, 32)
		base, baseErr := generatedBoundsTimeBase(stream.TimeBase)
		if stream.Index == nil || *stream.Index != int64(index) || idErr != nil || id != uint64(track.common.TrackID) || baseErr != nil || stream.StartPTS == nil ||
			base.Num != 1 || base.Den != track.common.MediaTimeScale || generatedClockSeconds(*stream.StartPTS, base.Num, base.Den).Cmp(generatedAVSourceRat(track.common.EffectiveFirst)) != 0 {
			return empty, generatedAVSourceError("demux stream identity or effective start differs")
		}
		start := generatedClockSeconds(*stream.StartPTS, base.Num, base.Den)
		if firstDemuxStart == nil || start.Cmp(firstDemuxStart) < 0 {
			firstDemuxStart = start
		}
		if len(stream.SideData) != 0 {
			// Excluded FFprobe side-data fields can leave empty wrappers. They
			// add no evidence; the shared bounded projection rule rejects data.
			var projection generatedSourceRangeFrame
			wrapped := append([]byte(`{"side_data_list":`), stream.SideData...)
			wrapped = append(wrapped, '}')
			if err := decodeGeneratedSourceFrameProjection(wrapped, &projection); err != nil {
				return empty, err
			}
		}
		if track.kind == "vide" {
			if stream.Kind != "video" || stream.Codec != "h264" || stream.SampleRate != "" || stream.Channels != nil {
				return empty, generatedAVSourceError("demux video declaration differs")
			}
		} else if stream.Kind != "audio" || stream.Codec != "aac" || stream.SampleRate != "48000" || stream.Channels == nil || *stream.Channels != int64(track.audio.Channels) {
			return empty, generatedAVSourceError("demux audio declaration differs")
		}
	}
	var format generatedSourceRangeFormat
	if err := generatedBoundsDecodeRecord(document.Format, &format, "start_time"); err != nil || format.StartTime == nil {
		return empty, generatedAVSourceError("missing fresh demux format origin")
	}
	ticks, valid := generatedSourceRangeOrigin(*format.StartTime)
	if !valid || ticks < -maxDurationTicks || ticks > maxDurationTicks {
		return empty, generatedAVSourceError("demux format origin is not exact and bounded")
	}
	if firstDemuxStart == nil || generatedTicksSeconds(ticks).Cmp(firstDemuxStart) != 0 {
		return empty, generatedAVSourceError("demux format origin differs from its complete stream-start inventory")
	}
	result := candidate.certificate
	result.DemuxOrigin = generatedMP4Rational(ticks, ticksPerSecond)
	return result, nil
}
