//go:build !linux

package timeshift

import (
	"context"
	"os"
)

type storageRoot struct{}
type storageWindow struct{}

func openStorage(string) (*storageRoot, error)             { return nil, ErrUnsupported }
func (*storageRoot) create(string) (*storageWindow, error) { return nil, ErrUnsupported }
func (*storageRoot) charge(size int64) int64               { return size }
func (*storageRoot) close() error                          { return nil }
func (*storageWindow) copy(context.Context, string, *os.File, os.FileInfo) (int64, error) {
	return 0, ErrUnsupported
}
func (*storageWindow) open(string) (*os.File, error) { return nil, ErrUnsupported }
func (*storageWindow) remove(string) error           { return ErrUnsupported }
func (*storageWindow) destroy() error                { return ErrUnsupported }
func (*storageWindow) close() error                  { return nil }
