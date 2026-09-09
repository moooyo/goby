//go:build !linux

package media

import (
	"context"
	"fmt"
	"os"
)

func probeOggAudioEvidence(context.Context, *os.File, []Stream) (*oggAudioEvidence, error) {
	return nil, fmt.Errorf("Ogg descriptor evidence requires Linux")
}
