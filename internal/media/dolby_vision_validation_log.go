package media

import "bytes"

// An hvcC record may leave parameter sets in-band. FFmpeg's Annex B filter
// reports this structural warning but continues successfully. Only the syntax
// validation pass may accept it, after the complete access-unit and CRC scan.
// Every other diagnostic, partial line, or mixed message remains a failure.
func dolbyVisionRPUValidationStderrAllowed(stderr []byte) bool {
	const prefix = "[hevc_mp4toannexb @ 0x"
	const suffix = "] No parameter sets in the extradata"
	for len(stderr) != 0 {
		line, rest, complete := bytes.Cut(stderr, []byte{'\n'})
		if !complete || len(line) <= len(prefix)+len(suffix) || !bytes.HasPrefix(line, []byte(prefix)) || !bytes.HasSuffix(line, []byte(suffix)) {
			return false
		}
		address := line[len(prefix) : len(line)-len(suffix)]
		if len(address) == 0 || len(address) > 16 {
			return false
		}
		for _, digit := range address {
			if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
				return false
			}
		}
		stderr = rest
	}
	return true
}
