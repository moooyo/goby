//go:build !linux

package transcode

import "os"

// ValidateGeneratedWindowFraming is available only on the Linux deployment target.
func ValidateGeneratedWindowFraming(plan Plan, initialization, segment *os.File) error {
	return ErrUnsupported
}
