//go:build !linux

package library

import "context"

func CheckTranscodingTempPathMountIsolation(ctx context.Context, selected string, reserved []string) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrRootTopologyUnavailable
}
