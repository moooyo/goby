//go:build !linux

package transcode

import "context"

func ExtractLiveCaption(context.Context, string, LiveCaptionSegment) ([]byte, error) {
	return nil, ErrUnsupported
}
