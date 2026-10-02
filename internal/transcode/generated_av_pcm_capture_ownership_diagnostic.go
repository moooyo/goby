package transcode

import "os"

// Borrowed-descriptor ownership takes precedence over an invalid artifact
// transfer. A second os.File value sharing the same valid OS descriptor also
// aliases the borrowed capability, even when its Go pointer differs.
func generatedAVPCMCaptureAliasesBorrowed(source *os.File, files []*os.File, capture *os.File) bool {
	if capture == nil {
		return false
	}
	aliases := func(borrowed *os.File) bool {
		if borrowed == nil {
			return false
		}
		if capture == borrowed {
			return true
		}
		captureFD := capture.Fd()
		return captureFD != ^uintptr(0) && captureFD == borrowed.Fd()
	}
	if aliases(source) {
		return true
	}
	for _, file := range files {
		if aliases(file) {
			return true
		}
	}
	return false
}
