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
