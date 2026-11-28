package diagnostics

import "context"

// PlaybackFailure is an observation, not an error or an authorization decision.
// Its stage and class are fixed labels; raw errors and request data do not travel
// with it. The diagnostic handler validates the labels on emission.
type PlaybackFailure struct {
	Stage      string
	ErrorClass string
	JobID      string
}

type playbackFailureKey struct{}

// WithPlaybackFailureCapture binds a single synchronous operation's capture.
// A worker must use its own capture and publish the value with its result; it
// must not write into the initiating request's capture.
func WithPlaybackFailureCapture(ctx context.Context, failure *PlaybackFailure) context.Context {
	return context.WithValue(ctx, playbackFailureKey{}, failure)
}

func CapturesPlaybackFailure(ctx context.Context) bool {
	return playbackFailureCapture(ctx) != nil
}

// CapturePlaybackFailure keeps the first, most specific observation from the
// operation that failed. It does not log or alter the returned error.
func CapturePlaybackFailure(ctx context.Context, observed PlaybackFailure) {
	if failure := playbackFailureCapture(ctx); failure != nil && failure.Stage == "" {
		*failure = observed
	}
}

func playbackFailureCapture(ctx context.Context) *PlaybackFailure {
	if ctx == nil {
		return nil
	}
	failure, _ := ctx.Value(playbackFailureKey{}).(*PlaybackFailure)
	return failure
}
