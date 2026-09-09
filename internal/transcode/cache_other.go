//go:build !linux

package transcode

import (
	"errors"
	"os"
	"strings"
)

var (
	ErrCacheInvalid = errors.New("invalid transcode cache")
	ErrCacheLocked  = errors.New("transcode cache is already locked")
	ErrCacheUnsafe  = errors.New("unsafe transcode cache content")
)

// Cache management is available only on the Linux deployment target.
type cacheRoot struct{}

func openCacheRoot(string) (*cacheRoot, error) { return nil, ErrUnsupported }
func (*cacheRoot) RootPath() string            { return "" }
func (*cacheRoot) Close() error                { return nil }
func (*cacheRoot) Recover() error              { return ErrUnsupported }
func (*cacheRoot) CreateJob(string) error      { return ErrUnsupported }
func (*cacheRoot) RemoveJob(string) error      { return ErrUnsupported }
func (*cacheRoot) FreeBytes() (int64, error)   { return 0, ErrUnsupported }
func (*cacheRoot) JobPath(string) (string, error) {
	return "", ErrUnsupported
}
func (*cacheRoot) OpenJobFile(string, string) (*os.File, error) {
	return nil, ErrUnsupported
}
func (*cacheRoot) ScanJob(string) (int64, bool, error) {
	return 0, false, ErrUnsupported
}

func validJobID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validOutputName(name string) bool {
	if name == "main.m3u8" {
		return true
	}
	if len(name) > 255 || !strings.HasPrefix(name, "segment-") || !strings.HasSuffix(name, ".ts") {
		return false
	}
	digits := name[len("segment-") : len(name)-len(".ts")]
	if digits == "" {
		return false
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
