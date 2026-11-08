//go:build !linux

package library

import (
	"context"
	"os"
)

func observeDirectoryCapacity(ctx context.Context, directory *os.File) (*directoryCapacity, error) {
	return nil, ctx.Err()
}

func observeDirectoryReadAccess(ctx context.Context, directory *os.File) (*bool, error) {
	return nil, ctx.Err()
}
