//go:build !linux

package analysiscache

import "os"

const (
	diskMarkerName    = ".goby-analysis-cache"
	diskMarkerVersion = "goby-analysis-cache-v1"
	diskLockName      = ".writer-lock"
	diskMarkerLimit   = 256
)

type diskRoot struct {
	root           *os.Root
	path           string
	info           os.FileInfo
	owner          string
	lock           *os.File
	markerInfo     os.FileInfo
	markerIdentity string
}

func openDiskRoot(string) (*diskRoot, error) { return nil, ErrUnsupported }
func (d *diskRoot) check() error             { return ErrUnsupported }
func (d *diskRoot) close() error             { return nil }

func openChild(*os.Root, string) (*os.Root, error) { return nil, ErrUnsupported }

func openRegular(*os.Root, string, int, os.FileMode) (*os.File, error) {
	return nil, ErrUnsupported
}

func fileIdentity(os.FileInfo) (string, error) { return "", ErrUnsupported }

func renameNoReplace(*os.Root, string, string) error { return ErrUnsupported }
func syncDirectory(*os.Root) error                   { return ErrUnsupported }
