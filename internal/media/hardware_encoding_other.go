//go:build !linux

package media

import (
	"context"
	"fmt"
)

func HardwareEncodingIdentity(context.Context, string, string, string) (string, error) {
	return "", fmt.Errorf("hardware encoding admission requires Linux")
}

func EncodingToolIdentity(context.Context, string) (string, error) {
	return "", fmt.Errorf("hardware encoding admission requires Linux")
}

func ProbeHardwareEncoding(ctx context.Context, _, _ string, _ HardwareEncodingRequest) (HardwareEncodingResult, error) {
	return hardwareEncodingRejected(ctx, "hardware_encoding_platform_unsupported")
}
