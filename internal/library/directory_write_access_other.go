//go:build !linux

package library

import (
	"context"
	"os"
)

func observeDirectoryWriteAccess(ctx context.Context, directory *os.File) (*bool, error) {
	return nil, ctx.Err()
}
