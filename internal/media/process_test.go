package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMediaProcessHelper(t *testing.T) {
	if os.Getenv("GOBY_MEDIA_HELPER_PROCESS") != "1" {
		return
	}
	for index, argument := range os.Args {
		if argument != "--" || index+1 >= len(os.Args) {
			continue
		}
		switch os.Args[index+1] {
		case "stdout":
			fmt.Print(strings.Repeat("x", 4096))
		case "stderr":
			fmt.Fprint(os.Stderr, strings.Repeat("x", maxProcessStderr+1))
		case "failure":
			fmt.Fprint(os.Stderr, "fixture process failed")
			os.Exit(7)
		case "wait":
			time.Sleep(30 * time.Second)
		case "echo":
			fmt.Print(os.Args[index+2])
		default:
			os.Exit(8)
		}
		os.Exit(0)
	}
	os.Exit(9)
}

func TestRunLimitedEnforcesBudgetsAndArguments(t *testing.T) {
	t.Setenv("GOBY_MEDIA_HELPER_PROCESS", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := func(mode string) []string { return []string{"-test.run=^TestMediaProcessHelper$", "--", mode} }
	for _, mode := range []string{"stdout", "stderr"} {
		t.Run(mode+" limit", func(t *testing.T) {
			_, err := runLimited(context.Background(), 5*time.Second, 2048, executable, args(mode)...)
			if !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("output limit returned %v", err)
			}
		})
	}
	_, err = runLimited(context.Background(), 100*time.Millisecond, 2048, executable, args("wait")...)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout returned %v", err)
	}
	_, err = runLimited(context.Background(), 5*time.Second, 2048, executable, args("failure")...)
	if err == nil || !strings.Contains(err.Error(), "fixture process failed") {
		t.Fatalf("nonzero exit did not retain its bounded diagnostic: %v", err)
	}
	literal := `filename with spaces; $(printf injected) & "quoted"`
	output, err := runLimited(context.Background(), 5*time.Second, 2048, executable, append(args("echo"), literal)...)
	if err != nil || string(output) != literal {
		t.Fatalf("arguments were not passed literally: %q, %v", output, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runLimited(ctx, time.Second, 10, executable); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled process returned %v", err)
	}
}

func TestMediaProcessCancelErrorsRetainsBoundedFirstAndLatestFailures(t *testing.T) {
	first := errors.New("first cancellation failure")
	discarded := errors.New("intermediate cancellation failure")
	latest := errors.New("latest cancellation failure")
	var summary mediaProcessCancelErrors
	summary.observe(first)
	for attempt := 0; attempt < 100000; attempt++ {
		summary.observe(discarded)
	}
	summary.observe(latest)
	for attempt := 0; attempt < 100000; attempt++ {
		summary.observe(nil)
	}
	err := summary.err()
	if !errors.Is(err, first) || !errors.Is(err, latest) || errors.Is(err, discarded) {
		t.Fatalf("cleanup did not retain only its first and latest failures: %v", err)
	}
	if !strings.Contains(err.Error(), "200002 attempts") || len(err.Error()) > 256 {
		t.Fatalf("cleanup diagnostics grew with retry history or lost the attempt count: %v", err)
	}
	// Later retry observations cannot mutate a diagnostic already returned to
	// an observer, and a successful signal does not erase an earlier failure.
	summary.observe(errors.New("later cancellation failure"))
	if !errors.Is(err, latest) || !strings.Contains(err.Error(), "200002 attempts") {
		t.Fatalf("an observed cleanup result changed after another attempt: %v", err)
	}
}

func TestMediaProcessCancelErrorsNilAttemptsRemainSuccessful(t *testing.T) {
	var summary mediaProcessCancelErrors
	for attempt := 0; attempt < 100000; attempt++ {
		summary.observe(nil)
	}
	if err := summary.err(); err != nil {
		t.Fatalf("successful cancellation attempts invented an error: %v", err)
	}
}
