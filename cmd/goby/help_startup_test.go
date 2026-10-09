package main

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func captureStartupHelp(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "help-output-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousArgs, previousOutput := os.Args, os.Stdout
	previousLogger, previousLogOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	defer func() {
		os.Args, os.Stdout = previousArgs, previousOutput
		slog.SetDefault(previousLogger)
		log.SetOutput(previousLogOutput)
		log.SetFlags(previousFlags)
	}()
	os.Args, os.Stdout = append([]string{"goby"}, args...), output
	var logs bytes.Buffer
	runErr := run(slog.NewJSONHandler(&logs, nil))
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), logs.String(), runErr
}

func TestStartupHelpDoesNotRequireServiceConfiguration(t *testing.T) {
	for _, configuration := range []struct{ name, database, cookie string }{
		{"missing database", "", "true"},
		{"invalid service option", "postgres://invalid.example/goby", "invalid"},
	} {
		t.Run(configuration.name, func(t *testing.T) {
			cfg := cliTestConfig(t)
			t.Setenv("GOBY_DATABASE_URL", configuration.database)
			t.Setenv("GOBY_COOKIE_SECURE", configuration.cookie)
			for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {"recovery", "help"}} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					output, logs, err := captureStartupHelp(t, args)
					if err != nil || output != recoveryCLIHelp || logs != "" {
						t.Fatalf("static help depended on service startup: error=%v output=%q logs=%q", err, output, logs)
					}
					assertCLIStoresUnopened(t, cfg)
				})
			}
		})
	}
}

func TestStartupOperationsStillRequireValidConfiguration(t *testing.T) {
	cfg := cliTestConfig(t)
	t.Setenv("GOBY_DATABASE_URL", "")
	t.Setenv("GOBY_COOKIE_SECURE", "true")
	for _, args := range [][]string{nil, {"serve"}, {"recovery", "status"}, {"backup", "list"}, {"help", "extra"}, {"recovery", "help", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			output, logs, err := captureStartupHelp(t, args)
			if err == nil || output != "" || !strings.Contains(logs, "server.stopped") {
				t.Fatalf("an operation bypassed configuration: error=%v output=%q logs=%q", err, output, logs)
			}
			assertCLIStoresUnopened(t, cfg)
		})
	}
}

func TestStartupHelpReportsOutputFailure(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "")
	output, err := os.CreateTemp(t.TempDir(), "closed-help-output-")
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	diagnostic, err := os.CreateTemp(t.TempDir(), "help-diagnostic-")
	if err != nil {
		t.Fatal(err)
	}
	defer diagnostic.Close()
	previousArgs, previousOutput, previousError := os.Args, os.Stdout, os.Stderr
	previousLogger, previousLogOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	defer func() {
		os.Args, os.Stdout, os.Stderr = previousArgs, previousOutput, previousError
		slog.SetDefault(previousLogger)
		log.SetOutput(previousLogOutput)
		log.SetFlags(previousFlags)
	}()
	os.Args, os.Stdout, os.Stderr = []string{"goby", "--help"}, output, diagnostic
	var logs bytes.Buffer
	runErr := run(slog.NewJSONHandler(&logs, nil))
	if runErr == nil || logs.Len() != 0 {
		t.Fatalf("failed help write did not remain a CLI error: error=%v logs=%q", runErr, logs.String())
	}
	if _, err := diagnostic.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != safeCLIError(runErr).Error()+"\n" || strings.Contains(string(data), output.Name()) {
		t.Fatalf("failed help write lost its safe diagnostic: %q", data)
	}
}
