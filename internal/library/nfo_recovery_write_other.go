//go:build !linux || android

package library

import (
	"context"
	"os"
)

type nfoRecoveryStage struct{}

func stageNFORecovery(context.Context, *os.Root, []byte) (*nfoRecoveryStage, error) {
	return nil, ErrUnavailable
}

func (*nfoRecoveryStage) checkReplacement(context.Context, string, func() error) error {
	return ErrUnavailable
}

func (*nfoRecoveryStage) replace(context.Context, string) error { return ErrUnavailable }

func (*nfoRecoveryStage) verifyPublished(context.Context, string) error { return ErrUnavailable }

func (*nfoRecoveryStage) close(context.Context) error { return nil }
