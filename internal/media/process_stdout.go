package media

import (
	"context"
	"io"
	"os/exec"
)

// RunProcessStdout owns admission, the stdout pipe, one synchronous parser call
// and the actual child/copier join for a fresh command. The parser must bound
// its own output and return after reading EOF, or return an error to request
// cancellation. It must not retain the reader or launch unjoined consumers.
//
// An explicitly bound CommandScope uses the native pipe owner and its actual
// leaf retirement. Missing or invalid required native ownership fails closed.
// Capacity is retained until the parser returns and the native owner completes
// its Wait, pipe close and whole leaf retirement. Abnormal parser exits retain
// ownership and charge. Absence of a native binding preserves legacy routing.
//
// The distinct errors preserve caller policy: startErr reports construction or
// Start, parseErr reports the parser, and waitErr reports actual join/retirement.
// A non-nil Start owner is joined before returning. This wrapper adds no parser
// goroutine, admission budget, cancellation fallback or readiness authority.
func RunProcessStdout(ctx context.Context, command *exec.Cmd, parse func(io.Reader) error) (parseErr, waitErr, startErr error) {
	return runMediaStdout(ctx, command, parse)
}
