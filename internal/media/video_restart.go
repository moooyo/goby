package media

// DecodeVideoPacketHexdump exposes the existing bounded ffprobe decoder for
// first-packet validators. The ASCII column never becomes packet bytes.
func DecodeVideoPacketHexdump(encoded string) ([]byte, error) {
	return videoCopySeekPacketData(encoded)
}

// IsAV1RestartPacket checks the same complete sequence and displayed-key-frame
// syntax used by finite copy-seek evidence. It is not an entropy decoder; callers
// must independently require successful decoding of the classified packet.
func IsAV1RestartPacket(data []byte) bool {
	return videoCopySeekAV1RestartPacket(data)
}
