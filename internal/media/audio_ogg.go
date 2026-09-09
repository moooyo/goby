package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sort"
	"strings"
)

const (
	maxOggAudioBytes         int64 = 1 << 30
	maxOggAudioStreams             = 32
	maxOggAudioDataPackets         = 1_000_000
	maxOggAudioHeaderPackets       = 65536
	maxOggAudioPageBytes           = 65307
	oggAudioPrefixBytes            = 64
)

type oggAudioEvidence struct {
	PacketCounts           map[int]int64
	OpusSamples            map[int][]uint16
	OpusPreSkip            map[int]int64
	VorbisMaxPacketSamples map[int]uint16
}

type oggAudioStream struct {
	stream           Stream
	nextSequence     uint32
	ended            bool
	packetOpen       bool
	prefix           [oggAudioPrefixBytes]byte
	prefixLength     int
	packetLength     int64
	lastByte         byte
	headers          int
	flacHeaderCount  int
	flacMetadataDone bool
	flacBits         int
	dataPackets      int64
}

type oggAudioInput struct {
	ctx   context.Context
	input io.LimitedReader
	read  int64
}

// readOggAudioEvidence verifies the physical stream topology independently of
// libavformat, which can reuse an AVStream across chained logical streams.
// Codec payload decoding remains the responsibility of the exhaustive probe.
func readOggAudioEvidence(ctx context.Context, reader io.Reader, streams []Stream) (*oggAudioEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	audio := make([]Stream, 0, len(streams))
	indices := make(map[int]bool, len(streams))
	for _, stream := range streams {
		if stream.Index < 0 || indices[stream.Index] {
			return nil, unprovenAudioTiming("ogg_stream_mismatch")
		}
		indices[stream.Index] = true
		if stream.CodecType != "audio" {
			if stream.CodecType != "video" || !stream.IsAttachedPicture {
				return nil, unprovenAudioTiming("ogg_unsupported_stream")
			}
			continue
		}
		stream.Codec = strings.ToLower(stream.Codec)
		if stream.Codec != "opus" && stream.Codec != "vorbis" && stream.Codec != "flac" {
			return nil, unprovenAudioTiming("ogg_unsupported_codec")
		}
		if stream.Channels <= 0 || stream.Channels > 255 || stream.SampleRate <= 0 || stream.SampleRate > 768000 {
			return nil, unprovenAudioTiming("ogg_stream_mismatch")
		}
		audio = append(audio, stream)
	}
	if len(audio) == 0 || len(audio) > maxOggAudioStreams {
		return nil, unprovenAudioTiming("ogg_stream_limit")
	}
	sort.Slice(audio, func(i, j int) bool { return audio[i].Index < audio[j].Index })
	evidence := &oggAudioEvidence{
		PacketCounts:           make(map[int]int64, len(audio)),
		OpusSamples:            make(map[int][]uint16),
		OpusPreSkip:            make(map[int]int64),
		VorbisMaxPacketSamples: make(map[int]uint16),
	}
	input := oggAudioInput{ctx: ctx, input: io.LimitedReader{R: reader, N: maxOggAudioBytes + 1}}
	physical := make(map[uint32]*oggAudioStream, len(audio))
	var page [maxOggAudioPageBytes]byte
	var totalPackets int64
	var afterBOS, seenEOS bool
	for {
		n, err := input.readFull(page[:27])
		if err == io.EOF && n == 0 {
			break
		}
		if err != nil {
			return nil, oggAudioReadError(err)
		}
		if !bytes.Equal(page[:4], []byte("OggS")) || page[4] != 0 || page[5]&^byte(7) != 0 {
			return nil, unprovenAudioTiming("ogg_invalid_page")
		}
		segments := int(page[26])
		if _, err = input.readFull(page[27 : 27+segments]); err != nil {
			return nil, oggAudioReadError(err)
		}
		bodyLength := 0
		for _, length := range page[27 : 27+segments] {
			bodyLength += int(length)
		}
		pageLength := 27 + segments + bodyLength
		if _, err = input.readFull(page[27+segments : pageLength]); err != nil {
			return nil, oggAudioReadError(err)
		}
		checksum := binary.LittleEndian.Uint32(page[22:26])
		clear(page[22:26])
		if oggAudioChecksum(page[:pageLength]) != checksum {
			return nil, unprovenAudioTiming("ogg_crc_mismatch")
		}
		flags := page[5]
		serial := binary.LittleEndian.Uint32(page[14:18])
		sequence := binary.LittleEndian.Uint32(page[18:22])
		state := physical[serial]
		if state == nil {
			if flags&2 == 0 || afterBOS || seenEOS || len(physical) >= len(audio) {
				return nil, unprovenAudioTiming("ogg_invalid_topology")
			}
			state = &oggAudioStream{stream: audio[len(physical)]}
			physical[serial] = state
		} else if flags&2 != 0 || state.ended {
			return nil, unprovenAudioTiming("ogg_chained_stream")
		}
		if sequence != state.nextSequence || (flags&1 != 0) != state.packetOpen {
			return nil, unprovenAudioTiming("ogg_discontinuous_page")
		}
		state.nextSequence++
		if flags&2 == 0 {
			afterBOS = true
		}
		completed := 0
		bodyOffset := 27 + segments
		for _, lace := range page[27 : 27+segments] {
			length := int(lace)
			part := page[bodyOffset : bodyOffset+length]
			bodyOffset += length
			state.prefixLength += copy(state.prefix[state.prefixLength:], part)
			state.packetLength += int64(length)
			if length > 0 {
				state.lastByte = part[length-1]
			}
			state.packetOpen = lace == 255
			if state.packetOpen {
				continue
			}
			wasHeader := state.headers
			if state.headersComplete() && totalPackets >= maxOggAudioDataPackets {
				return nil, unprovenAudioTiming("ogg_packet_limit")
			}
			if err := state.completePacket(evidence); err != nil {
				return nil, err
			}
			if state.headers == wasHeader {
				totalPackets++
			}
			state.prefixLength = 0
			state.packetLength = 0
			completed++
		}
		if flags&2 != 0 && (completed != 1 || state.headers != 1 || state.packetOpen || flags&4 != 0 || binary.LittleEndian.Uint64(page[6:14]) != 0) {
			return nil, unprovenAudioTiming("ogg_invalid_headers")
		}
		if flags&4 != 0 {
			if state.packetOpen || !state.headersComplete() || state.dataPackets == 0 {
				return nil, unprovenAudioTiming("ogg_incomplete_stream")
			}
			state.ended = true
			seenEOS = true
		}
	}
	if len(physical) != len(audio) {
		return nil, unprovenAudioTiming("ogg_stream_mismatch")
	}
	for _, state := range physical {
		if !state.ended || state.packetOpen || !state.headersComplete() || state.dataPackets == 0 {
			return nil, unprovenAudioTiming("ogg_incomplete_stream")
		}
		evidence.PacketCounts[state.stream.Index] = state.dataPackets
	}
	return evidence, nil
}

func (input *oggAudioInput) readFull(buffer []byte) (int, error) {
	n, empty := 0, 0
	for n < len(buffer) {
		if err := input.ctx.Err(); err != nil {
			return n, err
		}
		count, err := input.input.Read(buffer[n:])
		n += count
		input.read += int64(count)
		if ctxErr := input.ctx.Err(); ctxErr != nil {
			return n, ctxErr
		}
		if input.read > maxOggAudioBytes {
			return n, unprovenAudioTiming("ogg_input_limit")
		}
		if err != nil {
			if err == io.EOF && n == len(buffer) {
				return n, nil
			}
			return n, err
		}
		if count == 0 {
			empty++
			if empty >= 100 {
				return n, io.ErrNoProgress
			}
		} else {
			empty = 0
		}
	}
	return n, nil
}

func oggAudioReadError(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return unprovenAudioTiming("ogg_truncated_page")
	}
	return err
}

func (state *oggAudioStream) headersComplete() bool {
	switch state.stream.Codec {
	case "opus":
		return state.headers == 2
	case "vorbis":
		return state.headers == 3
	case "flac":
		return state.flacMetadataDone
	}
	return false
}

func (state *oggAudioStream) completePacket(evidence *oggAudioEvidence) error {
	prefix := state.prefix[:state.prefixLength]
	if state.packetLength == 0 {
		return unprovenAudioTiming("ogg_empty_packet")
	}
	if !state.headersComplete() {
		if state.headers >= maxOggAudioHeaderPackets {
			return unprovenAudioTiming("ogg_header_limit")
		}
		if err := state.headerPacket(prefix, evidence); err != nil {
			return err
		}
		state.headers++
		return nil
	}
	switch state.stream.Codec {
	case "opus":
		if bytes.HasPrefix(prefix, []byte("OpusHead")) || bytes.HasPrefix(prefix, []byte("OpusTags")) {
			return unprovenAudioTiming("ogg_parameter_change")
		}
		samples, err := oggOpusPacketSamples(prefix, state.packetLength)
		if err != nil {
			return err
		}
		evidence.OpusSamples[state.stream.Index] = append(evidence.OpusSamples[state.stream.Index], samples)
	case "vorbis":
		if prefix[0]&1 != 0 {
			return unprovenAudioTiming("ogg_parameter_change")
		}
	case "flac":
		if err := state.flacFrameHeader(prefix); err != nil {
			return err
		}
	}
	state.dataPackets++
	return nil
}

func (state *oggAudioStream) headerPacket(prefix []byte, evidence *oggAudioEvidence) error {
	switch state.stream.Codec {
	case "opus":
		if state.headers == 0 {
			return state.opusHeader(prefix, evidence)
		}
		if state.packetLength < 16 || !bytes.HasPrefix(prefix, []byte("OpusTags")) ||
			int64(binary.LittleEndian.Uint32(prefix[8:12])) > state.packetLength-16 {
			return unprovenAudioTiming("ogg_invalid_headers")
		}
	case "vorbis":
		if len(prefix) < 7 || prefix[0] != byte(1+state.headers*2) || !bytes.Equal(prefix[1:7], []byte("vorbis")) {
			return unprovenAudioTiming("ogg_invalid_headers")
		}
		if state.headers == 0 {
			if state.packetLength != 30 || binary.LittleEndian.Uint32(prefix[7:11]) != 0 ||
				int(prefix[11]) != state.stream.Channels || int(binary.LittleEndian.Uint32(prefix[12:16])) != state.stream.SampleRate ||
				prefix[29] != 1 {
				return unprovenAudioTiming("ogg_stream_mismatch")
			}
			small, large := prefix[28]&15, prefix[28]>>4
			if small < 6 || large > 13 || small > large {
				return unprovenAudioTiming("ogg_invalid_headers")
			}
			evidence.VorbisMaxPacketSamples[state.stream.Index] = uint16(1 << (large - 1))
		} else if state.headers == 1 {
			if state.packetLength < 16 || state.lastByte != 1 ||
				int64(binary.LittleEndian.Uint32(prefix[7:11])) > state.packetLength-16 {
				return unprovenAudioTiming("ogg_invalid_headers")
			}
		} else if state.packetLength < 8 {
			return unprovenAudioTiming("ogg_invalid_headers")
		}
	case "flac":
		return state.flacHeader(prefix)
	default:
		return unprovenAudioTiming("ogg_unsupported_codec")
	}
	return nil
}

func (state *oggAudioStream) opusHeader(prefix []byte, evidence *oggAudioEvidence) error {
	if state.packetLength < 19 || !bytes.HasPrefix(prefix, []byte("OpusHead")) || prefix[8] != 1 {
		return unprovenAudioTiming("ogg_invalid_headers")
	}
	channels := int(prefix[9])
	if state.stream.SampleRate != 48000 || channels != state.stream.Channels {
		return unprovenAudioTiming("ogg_stream_mismatch")
	}
	switch prefix[18] {
	case 0:
		if channels < 1 || channels > 2 || state.packetLength != 19 {
			return unprovenAudioTiming("ogg_unsupported_layout")
		}
	case 1:
		// These are the standard Vorbis-order layouts from libopus.
		layouts := [...][]byte{
			{1, 0, 0}, {1, 1, 0, 1}, {2, 1, 0, 2, 1}, {2, 2, 0, 1, 2, 3},
			{3, 2, 0, 4, 1, 2, 3}, {4, 2, 0, 4, 1, 2, 3, 5},
			{4, 3, 0, 4, 1, 2, 3, 5, 6}, {5, 3, 0, 6, 1, 2, 3, 4, 5, 7},
		}
		if channels < 1 || channels > len(layouts) || state.packetLength != int64(21+channels) ||
			!bytes.Equal(prefix[19:21+channels], layouts[channels-1]) {
			return unprovenAudioTiming("ogg_unsupported_layout")
		}
	default:
		return unprovenAudioTiming("ogg_unsupported_layout")
	}
	evidence.OpusPreSkip[state.stream.Index] = int64(binary.LittleEndian.Uint16(prefix[10:12]))
	return nil
}

func (state *oggAudioStream) flacHeader(prefix []byte) error {
	if state.headers == 0 {
		if bytes.HasPrefix(prefix, []byte("fLaC")) {
			return unprovenAudioTiming("ogg_unsupported_codec")
		}
		if state.packetLength != 51 || !bytes.Equal(prefix[:5], []byte("\x7fFLAC")) ||
			prefix[5] != 1 || prefix[6] != 0 || !bytes.Equal(prefix[9:13], []byte("fLaC")) ||
			prefix[13] != 0 || prefix[14] != 0 || prefix[15] != 0 || prefix[16] != 34 {
			return unprovenAudioTiming("ogg_invalid_headers")
		}
		packed := binary.BigEndian.Uint64(prefix[27:35])
		if int(packed>>44) != state.stream.SampleRate || int(packed>>41&7)+1 != state.stream.Channels {
			return unprovenAudioTiming("ogg_stream_mismatch")
		}
		state.flacBits = int(packed>>36&31) + 1
		minimum, maximum := binary.BigEndian.Uint16(prefix[17:19]), binary.BigEndian.Uint16(prefix[19:21])
		if minimum < 16 || maximum < minimum || state.flacBits < 4 || state.stream.BitDepth > 0 && state.stream.BitDepth != state.flacBits {
			return unprovenAudioTiming("ogg_invalid_headers")
		}
		state.flacHeaderCount = int(binary.BigEndian.Uint16(prefix[7:9]))
		return nil
	}
	if len(prefix) < 4 {
		return unprovenAudioTiming("ogg_invalid_headers")
	}
	kind := prefix[0] & 0x7f
	length := int64(prefix[1])<<16 | int64(prefix[2])<<8 | int64(prefix[3])
	if kind == 0 || kind == 127 || state.packetLength != length+4 || state.headers == 1 && kind != 4 {
		return unprovenAudioTiming("ogg_invalid_headers")
	}
	if kind == 4 && (length < 8 || int64(binary.LittleEndian.Uint32(prefix[4:8])) > length-8) {
		return unprovenAudioTiming("ogg_invalid_headers")
	}
	last := prefix[0]&0x80 != 0
	if state.flacHeaderCount != 0 && (state.headers > state.flacHeaderCount || last != (state.headers == state.flacHeaderCount)) {
		return unprovenAudioTiming("ogg_invalid_headers")
	}
	state.flacMetadataDone = last
	return nil
}

// FLAC frames may override STREAMINFO audio parameters. FFprobe does not expose
// the decoded frame sample rate, so reject changes directly from the header.
// Header and payload CRCs are additionally checked by the decoder.
func (state *oggAudioStream) flacFrameHeader(prefix []byte) error {
	if len(prefix) < 6 || prefix[0] != 0xff || prefix[1]&0xfe != 0xf8 || prefix[2]>>4 == 0 || prefix[3]&1 != 0 {
		return unprovenAudioTiming("ogg_invalid_flac_packet")
	}
	assignment := int(prefix[3] >> 4)
	channels := assignment + 1
	if assignment >= 8 && assignment <= 10 {
		channels = 2
	} else if assignment > 10 {
		return unprovenAudioTiming("ogg_invalid_flac_packet")
	}
	depths := [...]int{0, 8, 12, 0, 16, 20, 24, 32}
	depthCode := (prefix[3] >> 1) & 7
	if depthCode == 3 || channels != state.stream.Channels || depthCode != 0 && depths[depthCode] != state.flacBits {
		return unprovenAudioTiming("ogg_parameter_change")
	}
	// The frame/sample number uses FLAC's extended UTF-8 integer coding.
	numberBytes := 1
	if prefix[4]&0x80 != 0 {
		numberBytes = 0
		for bit := byte(0x80); bit != 0 && prefix[4]&bit != 0; bit >>= 1 {
			numberBytes++
		}
		if numberBytes < 2 || numberBytes > 7 || len(prefix) < 4+numberBytes {
			return unprovenAudioTiming("ogg_invalid_flac_packet")
		}
		for _, part := range prefix[5 : 4+numberBytes] {
			if part&0xc0 != 0x80 {
				return unprovenAudioTiming("ogg_invalid_flac_packet")
			}
		}
	}
	offset := 4 + numberBytes
	switch prefix[2] >> 4 {
	case 6:
		offset++
	case 7:
		offset += 2
	}
	rateCode := prefix[2] & 15
	rates := [...]int{0, 88200, 176400, 192000, 8000, 16000, 22050, 24000, 32000, 44100, 48000, 96000}
	rate := state.stream.SampleRate
	switch {
	case rateCode == 15:
		return unprovenAudioTiming("ogg_invalid_flac_packet")
	case rateCode == 12:
		if len(prefix) < offset+2 {
			return unprovenAudioTiming("ogg_invalid_flac_packet")
		}
		rate = int(prefix[offset]) * 1000
		offset++
	case rateCode == 13 || rateCode == 14:
		if len(prefix) < offset+3 {
			return unprovenAudioTiming("ogg_invalid_flac_packet")
		}
		rate = int(binary.BigEndian.Uint16(prefix[offset : offset+2]))
		if rateCode == 14 {
			rate *= 10
		}
		offset += 2
	case rateCode != 0:
		rate = rates[rateCode]
	}
	if len(prefix) <= offset || rate != state.stream.SampleRate {
		return unprovenAudioTiming("ogg_parameter_change")
	}
	return nil
}

// oggOpusPacketSamples follows opus_packet_get_nb_samples at 48 kHz. It reads
// only the TOC and optional frame-count byte; decoding validates the payload.
func oggOpusPacketSamples(prefix []byte, length int64) (uint16, error) {
	if len(prefix) == 0 || length <= 0 {
		return 0, unprovenAudioTiming("ogg_invalid_opus_packet")
	}
	toc := prefix[0]
	var frameSamples int
	if toc&0x80 != 0 {
		frameSamples = 120 << ((toc >> 3) & 3)
	} else if toc&0x60 == 0x60 {
		frameSamples = 480 << ((toc >> 3) & 1)
	} else if (toc>>3)&3 == 3 {
		frameSamples = 2880
	} else {
		frameSamples = 480 << ((toc >> 3) & 3)
	}
	frames := 1
	switch toc & 3 {
	case 1, 2:
		frames = 2
	case 3:
		if length < 2 || len(prefix) < 2 {
			return 0, unprovenAudioTiming("ogg_invalid_opus_packet")
		}
		frames = int(prefix[1] & 0x3f)
	}
	samples := frames * frameSamples
	if samples <= 0 || samples > 5760 {
		return 0, unprovenAudioTiming("ogg_invalid_opus_packet")
	}
	return uint16(samples), nil
}

var oggAudioCRCTable = func() [256]uint32 {
	var table [256]uint32
	for index := range table {
		value := uint32(index) << 24
		for range 8 {
			if value&0x80000000 != 0 {
				value = value<<1 ^ 0x04c11db7
			} else {
				value <<= 1
			}
		}
		table[index] = value
	}
	return table
}()

func oggAudioChecksum(data []byte) uint32 {
	var value uint32
	for _, part := range data {
		value = value<<8 ^ oggAudioCRCTable[byte(value>>24)^part]
	}
	return value
}
