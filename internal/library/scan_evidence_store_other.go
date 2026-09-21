//go:build !linux

package library

import "os"

func scanEvidencePlatformSupported() bool                          { return false }
func scanEvidencePrivateInfo(os.FileInfo, bool) error              { return ErrUnavailable }
func scanEvidenceFileIdentity(os.FileInfo) (uint64, uint64, error) { return 0, 0, ErrUnavailable }
func scanEvidenceOpenFile(*os.Root, string, int, os.FileMode) (*os.File, error) {
	return nil, ErrUnavailable
}
func scanEvidenceLockFile(*os.File) error { return ErrUnavailable }
func scanEvidenceFilesystemIdentity(*os.Root) (scanEvidenceFilesystem, error) {
	return scanEvidenceFilesystem{}, ErrUnavailable
}
func scanEvidencePersistentHandle(*os.Root) (int32, []byte, error) { return 0, nil, ErrUnavailable }
