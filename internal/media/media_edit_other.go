//go:build !linux

package media

func mediaEditResourceLimiter() (string, error) {
	return "", ErrSubtitleRemovalUnsupported
}

func mediaEditProcessHitFileLimit(error) bool { return false }
