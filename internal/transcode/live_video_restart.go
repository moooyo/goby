package transcode

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

const (
	maxLiveRestartPacketBytes = 1 << 20
	maxLiveRestartConfigBytes = 256 << 10
	maxLiveRestartProbeBytes  = 8 << 20
	maxLiveRestartNALs        = 4096
)

type liveVideoRestartEntry struct {
	Type        string `json:"type"`
	MediaType   string `json:"media_type"`
	StreamIndex *int   `json:"stream_index"`
	Size        string `json:"size"`
	Flags       string `json:"flags"`
	Data        string `json:"data"`
	KeyFrame    *int   `json:"key_frame"`
	PictureType string `json:"pict_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SideData    []struct {
		Type string `json:"side_data_type"`
	} `json:"side_data_list"`
}

func parseLiveVideoRestartProbe(data []byte, codec string, fragmented bool) error {
	if len(data) == 0 || len(data) > maxLiveRestartProbeBytes {
		return ErrTimelineLimit
	}
	var result struct {
		Entries []liveVideoRestartEntry `json:"packets_and_frames"`
		Streams []struct {
			Index     *int   `json:"index"`
			Codec     string `json:"codec_name"`
			Type      string `json:"codec_type"`
			Extra     string `json:"extradata"`
			ExtraSize int    `json:"extradata_size"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Streams) != 1 || len(result.Entries) != 2 {
		return ErrTimelineProbe
	}
	stream := result.Streams[0]
	if stream.Index == nil || *stream.Index < 0 || stream.Type != "video" || stream.Codec != codec || stream.ExtraSize < 0 || stream.ExtraSize > maxLiveRestartConfigBytes {
		return ErrTimelineProbe
	}
	packet, frame := result.Entries[0], result.Entries[1]
	if packet.Type != "packet" || frame.Type != "frame" || packet.StreamIndex == nil || frame.StreamIndex == nil ||
		*packet.StreamIndex != *stream.Index || *frame.StreamIndex != *stream.Index || frame.MediaType != "video" ||
		frame.KeyFrame == nil || *frame.KeyFrame != 1 || frame.PictureType != "I" || frame.Width <= 0 || frame.Height <= 0 ||
		int64(frame.Width) > media.MaxVideoSeekPixels/int64(frame.Height) || !strings.Contains(packet.Flags, "K") || len(packet.Flags) > 8 || len(packet.SideData) > 1 {
		return ErrTimelineProbe
	}
	for _, flag := range packet.Flags {
		if flag != 'K' && flag != '_' {
			return ErrTimelineProbe
		}
	}
	for _, side := range packet.SideData {
		if fragmented || side.Type != "MPEGTS Stream ID" {
			return ErrTimelineProbe
		}
	}
	size, err := strconv.ParseInt(packet.Size, 10, 64)
	if err != nil || size <= 0 || size > maxLiveRestartPacketBytes {
		return ErrTimelineProbe
	}
	compressed, err := media.DecodeVideoPacketHexdump(packet.Data)
	if err != nil || int64(len(compressed)) != size {
		return ErrTimelineProbe
	}
	var configuration []byte
	if stream.ExtraSize > 0 {
		configuration, err = media.DecodeVideoPacketHexdump(stream.Extra)
		if err != nil || len(configuration) != stream.ExtraSize {
			return ErrTimelineProbe
		}
	} else if stream.Extra != "" {
		return ErrTimelineProbe
	}
	if !liveVideoRestartPacket(codec, configuration, compressed, fragmented) {
		return ErrTimelineProbe
	}
	return nil
}

// Codec syntax is paired with an actual first-packet decode by the probe.
// H.264/HEVC use the same closed NAL scope and IDR types as finite seek proofs;
// a container K flag or HEVC CRA/BLA is not sufficient restart evidence.
func liveVideoRestartPacket(codec string, configuration, packet []byte, fragmented bool) bool {
	if len(packet) == 0 || len(packet) > maxLiveRestartPacketBytes || len(configuration) > maxLiveRestartConfigBytes {
		return false
	}
	if codec == "av1" {
		if !fragmented || len(configuration) < 4 {
			return false
		}
		parser := progressiveVideoParser{}
		if parser.av1Configuration(configuration) != nil {
			return false
		}
		if media.IsAV1RestartPacket(packet) {
			return true
		}
		if !parser.av1Sequence || len(configuration)-4 > maxLiveRestartPacketBytes-len(packet) {
			return false
		}
		var joined []byte
		if bytes.HasPrefix(packet, []byte{0x12, 0}) {
			joined = append(joined, packet[:2]...)
			packet = packet[2:]
		}
		joined = append(joined, configuration[4:]...)
		joined = append(joined, packet...)
		return media.IsAV1RestartPacket(joined)
	}
	if codec != "h264" && codec != "hevc" {
		return false
	}
	var nals [][]byte
	var err error
	configured := false
	if fragmented {
		length := 0
		if codec == "h264" {
			length, err = progressiveVideoAVCC(configuration)
		} else {
			length, err = progressiveVideoHVCC(configuration)
		}
		if err != nil {
			return false
		}
		nals, err = liveRestartLengthNALs(packet, length)
		configured = true
	} else {
		// TS extradata may have been learned by probing later packets. Only
		// parameter sets preceding the first VCL in this packet count here.
		nals, err = liveRestartAnnexBNALs(packet)
	}
	if err != nil {
		return false
	}
	vps, sps, pps, picture := configured, configured, configured, false
	for _, nal := range nals {
		if codec == "h264" {
			if len(nal) < 2 || nal[0]&0x80 != 0 {
				return false
			}
			switch nal[0] & 31 {
			case 7:
				if picture || len(nal) < 4 {
					return false
				}
				sps = true
			case 8:
				if picture {
					return false
				}
				pps = true
			case 5:
				if !sps || !pps || nal[0]&0x60 == 0 || !picture && nal[1]&0x80 == 0 {
					return false
				}
				picture = true
			case 6, 9, 10, 11, 12:
			default:
				return false
			}
		} else {
			if len(nal) < 3 || nal[0]&0x80 != 0 || (nal[0]&1)<<5|nal[1]>>3 != 0 || nal[1]&7 == 0 {
				return false
			}
			switch (nal[0] >> 1) & 63 {
			case 32:
				if picture {
					return false
				}
				vps = true
			case 33:
				if picture {
					return false
				}
				sps = true
			case 34:
				if picture {
					return false
				}
				pps = true
			case 19, 20:
				if !vps || !sps || !pps || nal[1]&7 != 1 || !picture && nal[2]&0x80 == 0 {
					return false
				}
				picture = true
			case 35, 36, 37, 38, 39, 40:
			default:
				return false
			}
		}
	}
	return picture
}

func liveRestartLengthNALs(packet []byte, length int) ([][]byte, error) {
	if length != 1 && length != 2 && length != 4 {
		return nil, ErrTimelineProbe
	}
	var result [][]byte
	for len(packet) != 0 {
		if len(result) >= maxLiveRestartNALs || len(packet) < length {
			return nil, ErrTimelineProbe
		}
		var size uint32
		if length == 4 {
			size = binary.BigEndian.Uint32(packet[:4])
		} else {
			for _, value := range packet[:length] {
				size = size<<8 | uint32(value)
			}
		}
		packet = packet[length:]
		if size == 0 || uint64(size) > uint64(len(packet)) {
			return nil, ErrTimelineProbe
		}
		result = append(result, packet[:int(size)])
		packet = packet[int(size):]
	}
	return result, nil
}

func liveRestartAnnexBNALs(packet []byte) ([][]byte, error) {
	var result [][]byte
	position := bytes.Index(packet, []byte{0, 0, 1})
	if position < 0 {
		return nil, ErrTimelineProbe
	}
	for _, value := range packet[:position] {
		if value != 0 {
			return nil, ErrTimelineProbe
		}
	}
	packet = packet[position+3:]
	for {
		if len(result) >= maxLiveRestartNALs {
			return nil, ErrTimelineProbe
		}
		position = bytes.Index(packet, []byte{0, 0, 1})
		end := len(packet)
		if position >= 0 {
			end = position
		}
		nal := bytes.TrimRight(packet[:end], "\x00")
		if len(nal) == 0 {
			return nil, ErrTimelineProbe
		}
		result = append(result, nal)
		if position < 0 {
			return result, nil
		}
		packet = packet[position+3:]
	}
}
