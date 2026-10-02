package transcode

import (
	"context"
	"crypto/sha256"
	"os"
	"sync"
)

const generatedAVEffectiveCaptureCalls = 8

// GeneratedAVEffectiveJSONCaptureRequest describes the already bounded stdout
// of one joined effective-frame probe. Ordinal is local to this capability;
// it is not a stream timestamp, packet number or qualification fact.
type GeneratedAVEffectiveJSONCaptureRequest struct {
	Ordinal           int
	Bytes, LimitBytes int
	SHA256            [32]byte
}

type generatedAVEffectiveCaptureKey struct{}
type generatedAVEffectiveCaptureState struct {
	mu      sync.Mutex
	calls   int
	factory func(GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error)
}

// WithGeneratedAVEffectiveJSONCapture installs an opt-in trusted diagnostic
// capability. It is not an HTTP pathname API. The caller chooses an exclusively
// created private artifact and transfers every nonnil file, even with an error; nil skips
// that capture. At most eight factories are called, with the unchanged 4 MiB
// probe ceiling. No stderr, arguments, extra process or process pool is added.
func WithGeneratedAVEffectiveJSONCapture(ctx context.Context, factory func(GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error)) context.Context {
	if ctx == nil || factory == nil {
		return ctx
	}
	return context.WithValue(ctx, generatedAVEffectiveCaptureKey{}, &generatedAVEffectiveCaptureState{factory: factory})
}

func generatedAVEffectiveCaptureFile(ctx context.Context, data []byte) (*os.File, error) {
	state, _ := ctx.Value(generatedAVEffectiveCaptureKey{}).(*generatedAVEffectiveCaptureState)
	if state == nil {
		return nil, nil
	}
	if len(data) == 0 || len(data) > generatedAVEffectiveJSONBytes {
		return nil, ErrTimelineLimit
	}
	state.mu.Lock()
	if state.calls >= generatedAVEffectiveCaptureCalls {
		state.mu.Unlock()
		return nil, nil
	}
	state.calls++
	ordinal := state.calls
	state.mu.Unlock()
	return state.factory(GeneratedAVEffectiveJSONCaptureRequest{Ordinal: ordinal, Bytes: len(data), LimitBytes: generatedAVEffectiveJSONBytes, SHA256: sha256.Sum256(data)})
}
