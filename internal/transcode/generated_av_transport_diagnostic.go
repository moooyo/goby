package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	generatedAVTransportBytes    = 64 << 20
	generatedAVTransportPIDs     = 16
	generatedAVTransportRecords  = 4096
	generatedAVTransportPESBytes = 1 << 20
)

// GeneratedAVTransportPID records every observed PID, including permitted PSI
// and null packets. Continuity is checked independently for each payload PID.
type GeneratedAVTransportPID struct {
	PID                                        uint16
	Packets, PayloadPackets, AdaptationPackets int64
}

// GeneratedAVTransportAdaptation retains actual transport adaptation clocks.
// PCRBase33 and PCRExtension are separate encoded fields, never unwrapped PTS.
type GeneratedAVTransportAdaptation struct {
	Offset       int64
	PID          uint16
	Flags        byte
	PCRKnown     bool
	PCRBase33    uint64
	PCRExtension uint16
}

// GeneratedAVTransportPES describes one bounded PES envelope. A zero-length
// video PES ends at the next PUSI or the held file end. That observed boundary
// does not certify AVC payload completeness or decoder EOF. Absent DTS remains
// absent even when another demuxer can synthesize DTS from PTS.
type GeneratedAVTransportPES struct {
	PID                                       uint16
	Kind                                      string
	StreamID                                  byte
	FirstTransportOffset, LastTransportOffset int64
	DeclaredLength                            uint16
	Boundary                                  string
	Bytes, PayloadBytes                       int
	PTSKnown, DTSKnown                        bool
	PTS33, DTS33                              uint64
	SHA256                                    [32]byte
	PayloadSHA256                             [32]byte
	FirstAccessUnit, AccessUnits              int
}

// GeneratedAVTransportADTS preserves a complete physical ADTS access unit and
// its owning PES. No access unit contains a native timestamp in this profile.
// The sample offset from the timestamp-bearing PES is explicitly derived from
// header-declared 1024-sample blocks, not observed decoded or audible samples.
type GeneratedAVTransportADTS struct {
	PESIndex, IndexInPES, OffsetInPES, Bytes                int
	Header                                                  [7]byte
	MPEGID, ObjectType, SamplingIndex, ChannelConfiguration int
	SampleRate                                              int64
	BlockSamples                                            int64
	PESPTS33                                                uint64
	Derived                                                 bool
	DerivedSampleOffset                                     int64
	SHA256                                                  [32]byte
}

// GeneratedAVTransportDiagnostic is structural diagnostic data only. Complete
// means the bounded held extent was consumed and its admitted PSI/PES/ADTS
// framing finished. It never establishes codec validity, effective audio, source
// coverage, an absolute presentation epoch, restart or publication.
type GeneratedAVTransportDiagnostic struct {
	Qualified                                                      bool
	Complete                                                       bool
	Bytes, TransportPackets                                        int64
	SHA256                                                         [32]byte
	TransportStreamID, Program, PMTPID, PCRPID, VideoPID, AudioPID uint16
	PATSections, PMTSections, SDTSections                          int64
	PAT, PMT, SDT                                                  []byte
	PIDs                                                           []GeneratedAVTransportPID
	Adaptations                                                    []GeneratedAVTransportAdaptation
	PES                                                            []GeneratedAVTransportPES
	ADTS                                                           []GeneratedAVTransportADTS
	DeclaredAudioSamples                                           int64
}

type generatedAVTransportPIDState struct {
	facts      GeneratedAVTransportPID
	seen       bool
	continuity byte
}

type generatedAVTransportPESState struct {
	pid         uint16
	kind        string
	data        []byte
	first, last int64
}

type generatedAVTransportParser struct {
	ctx    context.Context
	result GeneratedAVTransportDiagnostic
	pids   []generatedAVTransportPIDState
	psi    [3][]byte
	pes    [2]generatedAVTransportPESState
}

func generatedAVTransportInvalid(reason string) error {
	return fmt.Errorf("%w: A/V transport diagnostic: %s", ErrTimelineProbe, reason)
}

// ParseGeneratedAVTransportDiagnostic consumes an explicit complete extent
// through ReaderAt. The narrow profile is one AVC/AAC-LC 48 kHz mono/stereo
// program, one-section stable PAT/PMT and FFmpeg's optional service SDT. Unknown
// PIDs, stream types, descriptors, optional PES fields and ADTS profiles fail.
// Sources for the native mux/header layouts (not runtime acceptance):
// https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/mpegtsenc.c
// https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/adtsenc.c
// https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/adts_header.c
func ParseGeneratedAVTransportDiagnostic(ctx context.Context, reader io.ReaderAt, size int64) (GeneratedAVTransportDiagnostic, error) {
	var empty GeneratedAVTransportDiagnostic
	if ctx == nil || reader == nil || size < 3*188 || size%188 != 0 {
		return empty, generatedAVTransportInvalid("invalid explicit transport extent")
	}
	if size > generatedAVTransportBytes {
		return empty, ErrTimelineLimit
	}
	p := generatedAVTransportParser{ctx: ctx}
	digest := sha256.New()
	var packet [188]byte
	for offset := int64(0); offset < size; offset += 188 {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if n, err := reader.ReadAt(packet[:], offset); err != nil || n != len(packet) {
			return empty, generatedAVTransportInvalid("incomplete transport read")
		}
		if err := p.packet(packet[:], offset); err != nil {
			return empty, err
		}
		_, _ = digest.Write(packet[:])
	}
	for _, pending := range p.psi {
		if len(pending) != 0 {
			return empty, generatedAVTransportInvalid("unfinished PSI section")
		}
	}
	for index := range p.pes {
		if err := p.finishPES(index, "held_file_end"); err != nil {
			return empty, err
		}
	}
	if p.result.PATSections == 0 || p.result.PMTSections == 0 || len(p.result.PES) == 0 || len(p.result.ADTS) == 0 {
		return empty, generatedAVTransportInvalid("missing complete program or access units")
	}
	if p.result.SDT != nil && (binary.BigEndian.Uint16(p.result.SDT[11:13]) != p.result.Program ||
		binary.BigEndian.Uint16(p.result.SDT[3:5]) != p.result.TransportStreamID) {
		return empty, generatedAVTransportInvalid("SDT program differs from final PAT")
	}
	video, audio := false, false
	for _, pes := range p.result.PES {
		video = video || pes.Kind == "video"
		audio = audio || pes.Kind == "audio"
	}
	if !video || !audio {
		return empty, generatedAVTransportInvalid("missing selected PES track")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	p.result.Bytes, p.result.TransportPackets = size, size/188
	p.result.Complete = true
	copy(p.result.SHA256[:], digest.Sum(nil))
	for _, pid := range p.pids {
		p.result.PIDs = append(p.result.PIDs, pid.facts)
	}
	return p.result, nil
}

func (p *generatedAVTransportParser) packet(data []byte, offset int64) error {
	if data[0] != 0x47 || data[1]&0x80 != 0 || data[3]&0xc0 != 0 || data[3]&0x30 == 0 {
		return generatedAVTransportInvalid("sync, error, scrambling or control")
	}
	pid := uint16(data[1]&0x1f)<<8 | uint16(data[2])
	if pid != 0 && pid != 17 && pid != 0x1fff &&
		!(p.result.PATSections > 0 && pid == p.result.PMTPID) &&
		!(p.result.PMTSections > 0 && (pid == p.result.VideoPID || pid == p.result.AudioPID)) {
		return generatedAVTransportInvalid("unmapped PID")
	}
	control, cc, start := data[3]>>4&3, data[3]&15, data[1]&0x40 != 0
	cursor, discontinuity := 4, false
	if control&2 != 0 {
		length := int(data[4])
		if control == 2 && length != 183 || control == 3 && length > 182 {
			return generatedAVTransportInvalid("adaptation length")
		}
		cursor = 5 + length
		if length > 0 {
			fact := GeneratedAVTransportAdaptation{Offset: offset, PID: pid, Flags: data[5]}
			// Admit PCR, random-access, elementary priority and discontinuity.
			// Other extension fields need independent bounded interpretations.
			if fact.Flags&0x0f != 0 {
				return generatedAVTransportInvalid("unsupported adaptation field")
			}
			discontinuity = fact.Flags&0x80 != 0
			at := 6
			if fact.Flags&0x10 != 0 {
				if cursor-at < 6 || data[at+4]&0x7e != 0x7e {
					return generatedAVTransportInvalid("PCR field")
				}
				fact.PCRKnown = true
				fact.PCRBase33 = uint64(data[at])<<25 | uint64(data[at+1])<<17 | uint64(data[at+2])<<9 | uint64(data[at+3])<<1 | uint64(data[at+4]>>7)
				fact.PCRExtension = uint16(data[at+4]&1)<<8 | uint16(data[at+5])
				if fact.PCRExtension > 299 {
					return generatedAVTransportInvalid("PCR extension")
				}
				at += 6
			}
			if !generatedAVAllFF(data[at:cursor]) {
				return generatedAVTransportInvalid("adaptation stuffing")
			}
			if len(p.result.Adaptations) >= generatedAVTransportRecords {
				return ErrTimelineLimit
			}
			p.result.Adaptations = append(p.result.Adaptations, fact)
		}
	}
	index := -1
	for i := range p.pids {
		if p.pids[i].facts.PID == pid {
			index = i
			break
		}
	}
	if index < 0 {
		if len(p.pids) >= generatedAVTransportPIDs {
			return ErrTimelineLimit
		}
		p.pids = append(p.pids, generatedAVTransportPIDState{facts: GeneratedAVTransportPID{PID: pid}})
		index = len(p.pids) - 1
	}
	state := &p.pids[index]
	state.facts.Packets++
	if control&2 != 0 {
		state.facts.AdaptationPackets++
	}
	if discontinuity && state.seen && pid != 0x1fff {
		return generatedAVTransportInvalid("mid-file discontinuity requires another contract")
	}
	if control&1 == 0 {
		if start || state.seen && cc != state.continuity {
			return generatedAVTransportInvalid("adaptation-only continuity")
		}
		return nil
	}
	state.facts.PayloadPackets++
	if pid != 0x1fff && state.seen && cc != (state.continuity+1)&15 {
		return generatedAVTransportInvalid("payload continuity")
	}
	state.seen, state.continuity = true, cc
	if cursor >= len(data) {
		return generatedAVTransportInvalid("empty advertised payload")
	}
	payload := data[cursor:]
	if pid == 0x1fff {
		if start || !generatedAVAllFF(payload) {
			return generatedAVTransportInvalid("null payload")
		}
		return nil
	}
	if pid == 0 {
		return p.section(0, start, payload)
	}
	if pid == 17 {
		return p.section(2, start, payload)
	}
	if p.result.PATSections > 0 && pid == p.result.PMTPID {
		return p.section(1, start, payload)
	}
	if p.result.PMTSections == 0 {
		return generatedAVTransportInvalid("payload before program mapping")
	}
	track := -1
	if pid == p.result.VideoPID {
		track = 0
	}
	if pid == p.result.AudioPID {
		track = 1
	}
	if track < 0 {
		return generatedAVTransportInvalid("unmapped PID")
	}
	if start {
		if err := p.finishPES(track, "next_payload_start"); err != nil {
			return err
		}
		kind := "video"
		if track == 1 {
			kind = "audio"
		}
		p.pes[track] = generatedAVTransportPESState{pid: pid, kind: kind, first: offset}
	} else if len(p.pes[track].data) == 0 {
		return generatedAVTransportInvalid("PES continuation without start")
	}
	current := &p.pes[track]
	if len(payload) > generatedAVTransportPESBytes-len(current.data) {
		return ErrTimelineLimit
	}
	current.data = append(current.data, payload...)
	current.last = offset
	return nil
}

func generatedAVAllFF(data []byte) bool {
	for _, value := range data {
		if value != 0xff {
			return false
		}
	}
	return true
}

// PSI CRC uses the non-reflected MPEG-2 polynomial and the complete section,
// including its CRC bytes. Valid sections have a zero remainder.
func generatedAVPSICRC(data []byte) uint32 {
	crc := ^uint32(0)
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

func (p *generatedAVTransportParser) section(index int, start bool, payload []byte) error {
	if start {
		if len(payload) == 0 || int(payload[0])+1 > len(payload) {
			return generatedAVTransportInvalid("PSI pointer")
		}
		pointer := int(payload[0])
		prefix := payload[1 : 1+pointer]
		if len(p.psi[index]) != 0 {
			if len(prefix) > 1024-len(p.psi[index]) {
				return ErrTimelineLimit
			}
			p.psi[index] = append(p.psi[index], prefix...)
			if err := p.consumeSections(index, false); err != nil {
				return err
			}
			if len(p.psi[index]) != 0 {
				return generatedAVTransportInvalid("PSI pointer does not finish previous section")
			}
		} else if !generatedAVAllFF(prefix) {
			return generatedAVTransportInvalid("PSI prefix without previous section")
		}
		payload = payload[1+pointer:]
	} else if len(p.psi[index]) == 0 {
		return generatedAVTransportInvalid("PSI continuation without section")
	}
	if len(payload) > 1024-len(p.psi[index]) {
		return ErrTimelineLimit
	}
	p.psi[index] = append(p.psi[index], payload...)
	return p.consumeSections(index, true)
}

func (p *generatedAVTransportParser) consumeSections(index int, allowPartial bool) error {
	for len(p.psi[index]) > 0 {
		data := p.psi[index]
		if data[0] == 0xff {
			if !generatedAVAllFF(data) {
				return generatedAVTransportInvalid("PSI trailing bytes")
			}
			p.psi[index] = nil
			return nil
		}
		if len(data) < 3 {
			if allowPartial {
				return nil
			}
			return generatedAVTransportInvalid("partial PSI header")
		}
		length := 3 + (int(data[1]&15)<<8 | int(data[2]))
		if length < 12 || length > 1024 {
			return generatedAVTransportInvalid("PSI extent")
		}
		if len(data) < length {
			if allowPartial {
				return nil
			}
			return generatedAVTransportInvalid("partial PSI section")
		}
		if err := p.bindSection(index, data[:length]); err != nil {
			return err
		}
		p.psi[index] = data[length:]
	}
	return nil
}

func (p *generatedAVTransportParser) bindSection(index int, data []byte) error {
	if data[1]&0xf0 != 0xb0 && index != 2 || data[1]&0xf0 != 0xf0 && index == 2 || data[5]&0xc1 != 0xc1 ||
		data[6] != 0 || data[7] != 0 || generatedAVPSICRC(data) != 0 {
		return generatedAVTransportInvalid("PSI syntax, version or CRC")
	}
	if index == 0 {
		if data[0] != 0 || len(data) != 16 || data[10]&0xe0 != 0xe0 {
			return generatedAVTransportInvalid("single-program PAT")
		}
		program, pid := binary.BigEndian.Uint16(data[8:10]), binary.BigEndian.Uint16(data[10:12])&0x1fff
		if program == 0 || pid < 32 || pid == 0x1fff {
			return generatedAVTransportInvalid("PAT program/PID")
		}
		if p.result.PAT != nil && !bytes.Equal(p.result.PAT, data) {
			return generatedAVTransportInvalid("changing PAT")
		}
		p.result.TransportStreamID, p.result.Program, p.result.PMTPID = binary.BigEndian.Uint16(data[3:5]), program, pid
		p.result.PAT = bytes.Clone(data)
		p.result.PATSections++
		return nil
	}
	if index == 1 {
		if data[0] != 2 || binary.BigEndian.Uint16(data[3:5]) != p.result.Program || len(data) != 26 ||
			data[8]&0xe0 != 0xe0 || data[10] != 0xf0 || data[11] != 0 {
			return generatedAVTransportInvalid("single AVC/AAC PMT")
		}
		var video, audio uint16
		for at := 12; at < len(data)-4; at += 5 {
			if data[at+1]&0xe0 != 0xe0 || data[at+3] != 0xf0 || data[at+4] != 0 {
				return generatedAVTransportInvalid("PMT stream descriptor")
			}
			pid := binary.BigEndian.Uint16(data[at+1:at+3]) & 0x1fff
			if pid < 32 || pid == 0x1fff || pid == p.result.PMTPID {
				return generatedAVTransportInvalid("PMT elementary PID")
			}
			switch data[at] {
			case 0x1b:
				if video != 0 {
					return generatedAVTransportInvalid("duplicate AVC")
				}
				video = pid
			case 0x0f:
				if audio != 0 {
					return generatedAVTransportInvalid("duplicate AAC")
				}
				audio = pid
			default:
				return generatedAVTransportInvalid("unsupported PMT stream type")
			}
		}
		pcr := binary.BigEndian.Uint16(data[8:10]) & 0x1fff
		if video == 0 || audio == 0 || video == audio || pcr != video {
			return generatedAVTransportInvalid("PMT track/PCR mapping")
		}
		if p.result.PMT != nil && !bytes.Equal(p.result.PMT, data) {
			return generatedAVTransportInvalid("changing PMT")
		}
		p.result.PCRPID, p.result.VideoPID, p.result.AudioPID = pcr, video, audio
		p.result.PMT = bytes.Clone(data)
		p.result.PMTSections++
		return nil
	}
	// Admit exactly one FFmpeg DVB service descriptor and retain its bytes.
	if data[0] != 0x42 || len(data) < 20 || data[10] != 0xff || data[13]&0xfc != 0xfc || data[14]&0xf0 != 0x80 {
		return generatedAVTransportInvalid("SDT shape")
	}
	service := binary.BigEndian.Uint16(data[11:13])
	if p.result.PATSections > 0 && (service != p.result.Program || binary.BigEndian.Uint16(data[3:5]) != p.result.TransportStreamID) {
		return generatedAVTransportInvalid("SDT program differs")
	}
	desc := data[16 : len(data)-4]
	if len(desc) < 5 || int(binary.BigEndian.Uint16(data[14:16])&0x0fff) != len(desc) || desc[0] != 0x48 || int(desc[1])+2 != len(desc) || desc[2] != 1 {
		return generatedAVTransportInvalid("SDT service descriptor")
	}
	provider := int(desc[3])
	if provider+5 > len(desc) || int(desc[4+provider])+5+provider != len(desc) {
		return generatedAVTransportInvalid("SDT service strings")
	}
	if p.result.SDT != nil && !bytes.Equal(p.result.SDT, data) {
		return generatedAVTransportInvalid("changing SDT")
	}
	p.result.SDT = bytes.Clone(data)
	p.result.SDTSections++
	return nil
}

func generatedAVPESClock(data []byte, prefix byte) (uint64, bool) {
	if len(data) != 5 || data[0]>>4 != prefix || data[0]&1 == 0 || data[2]&1 == 0 || data[4]&1 == 0 {
		return 0, false
	}
	return uint64(data[0]>>1&7)<<30 | uint64(data[1])<<22 | uint64(data[2]>>1)<<15 | uint64(data[3])<<7 | uint64(data[4]>>1), true
}

func (p *generatedAVTransportParser) finishPES(track int, boundary string) error {
	state := &p.pes[track]
	data := state.data
	if len(data) == 0 {
		return nil
	}
	if len(p.result.PES) >= generatedAVTransportRecords {
		return ErrTimelineLimit
	}
	if len(data) < 14 || !bytes.Equal(data[:3], []byte{0, 0, 1}) || data[6] != 0x80 || data[7]&0x3f != 0 {
		return generatedAVTransportInvalid("PES header")
	}
	if track == 0 && data[3]&0xf0 != 0xe0 || track == 1 && data[3]&0xe0 != 0xc0 {
		return generatedAVTransportInvalid("PES stream ID")
	}
	fact := GeneratedAVTransportPES{PID: state.pid, Kind: state.kind, StreamID: data[3], FirstTransportOffset: state.first,
		LastTransportOffset: state.last, DeclaredLength: binary.BigEndian.Uint16(data[4:6]), Boundary: boundary, Bytes: len(data), SHA256: sha256.Sum256(data)}
	if fact.DeclaredLength != 0 && int(fact.DeclaredLength)+6 != len(data) || fact.DeclaredLength == 0 && track != 0 {
		return generatedAVTransportInvalid("PES declared extent")
	}
	flags, header := data[7]>>6, int(data[8])
	if flags != 2 && flags != 3 || flags == 2 && header != 5 || flags == 3 && header != 10 || 9+header >= len(data) {
		return generatedAVTransportInvalid("PES timestamp fields")
	}
	var valid bool
	fact.PTS33, valid = generatedAVPESClock(data[9:14], flags)
	if !valid {
		return generatedAVTransportInvalid("PTS marker bits")
	}
	fact.PTSKnown = true
	if flags == 3 {
		fact.DTS33, valid = generatedAVPESClock(data[14:19], 1)
		if !valid {
			return generatedAVTransportInvalid("DTS marker bits")
		}
		fact.DTSKnown = true
	}
	payload := data[9+header:]
	fact.PayloadBytes, fact.PayloadSHA256 = len(payload), sha256.Sum256(payload)
	fact.FirstAccessUnit = len(p.result.ADTS)
	if track == 1 {
		if err := p.adts(payload, len(p.result.PES), fact.PTS33); err != nil {
			return err
		}
		fact.AccessUnits = len(p.result.ADTS) - fact.FirstAccessUnit
	}
	p.result.PES = append(p.result.PES, fact)
	state.data = nil
	return nil
}

func (p *generatedAVTransportParser) adts(data []byte, pes int, pts uint64) error {
	for offset, index := 0, 0; offset < len(data); index++ {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		if len(p.result.ADTS) >= generatedAVTransportRecords {
			return ErrTimelineLimit
		}
		if len(data)-offset < 7 {
			return generatedAVTransportInvalid("partial ADTS header")
		}
		h := data[offset : offset+7]
		object, rate, channels := int(h[2]>>6)+1, int(h[2]>>2&15), int(h[2]&1)<<2|int(h[3]>>6)
		if h[0] != 0xff || h[1] != 0xf1 || object != 2 || rate != 3 || channels < 1 || channels > 2 || h[2]&2 != 0 ||
			h[3]&0x3c != 0 || h[5]&31 != 31 || h[6] != 0xfc {
			return generatedAVTransportInvalid("unsupported ADTS header")
		}
		length := int(h[3]&3)<<11 | int(h[4])<<3 | int(h[5]>>5)
		if length <= 7 || length > len(data)-offset {
			return generatedAVTransportInvalid("partial or empty ADTS access unit")
		}
		if len(p.result.ADTS) > 0 && p.result.ADTS[0].ChannelConfiguration != channels {
			return generatedAVTransportInvalid("changing ADTS layout")
		}
		fact := GeneratedAVTransportADTS{PESIndex: pes, IndexInPES: index, OffsetInPES: offset, Bytes: length, ObjectType: object,
			SamplingIndex: rate, ChannelConfiguration: channels, SampleRate: 48000, BlockSamples: 1024, PESPTS33: pts,
			Derived: true, DerivedSampleOffset: int64(index) * 1024, SHA256: sha256.Sum256(data[offset : offset+length])}
		copy(fact.Header[:], h)
		p.result.ADTS = append(p.result.ADTS, fact)
		p.result.DeclaredAudioSamples += 1024
		offset += length
	}
	return nil
}
