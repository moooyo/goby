package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/diagnostics"
)

func TestRunSanitizesBootstrapFailuresAndDefaultLogging(t *testing.T) {
	_ = cliTestConfig(t)
	previousLogger := slog.Default()
	previousOutput, previousFlags := log.Writer(), log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
	const secret = "bootstrap-secret-postgres://user:password@private.invalid/media"
	t.Setenv("GOBY_TRUSTED_PROXIES", secret)
	var output bytes.Buffer
	if err := run(slog.NewJSONHandler(&output, nil)); err == nil {
		t.Fatal("run accepted invalid configuration")
	}
	records := readProcessLogRecords(t, output.String())
	if len(records) != 1 || records[0]["event"] != "server.stopped" || records[0]["level"] != "ERROR" {
		t.Fatalf("expected one bootstrap failure event, got %#v", records)
	}
	if records[0]["error_class"] != "unclassified" {
		t.Fatalf("expected a classified error without its text, got %#v", records[0])
	}
	slog.Error(secret, "error", errors.New(secret), "address", secret)
	log.Printf("library failure: %s", secret)
	records = readProcessLogRecords(t, output.String())
	if len(records) != 3 {
		t.Fatalf("expected bootstrap and two default logger events, got %#v", records)
	}
	for _, record := range records[1:] {
		if record["event"] != "unclassified" || record["msg"] != "unclassified event" {
			t.Fatalf("default logger retained a dynamic message: %#v", record)
		}
		if _, present := record["error"]; present {
			t.Fatalf("default logger retained a raw error: %#v", record)
		}
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("bootstrap or default logging exposed configuration data")
	}
}

func TestHTTPServerErrorLogSanitizesFormattedMessages(t *testing.T) {
	const secret = "private-client.invalid/private/media?api_key=secret"
	var output bytes.Buffer
	logger := slog.New(diagnostics.NewHandler(nil, slog.NewJSONHandler(&output, nil)))
	srv := newHTTPServer(secret, http.NotFoundHandler(), logger)
	if srv.ErrorLog == nil {
		t.Fatal("HTTP server is missing its safe error logger")
	}
	srv.ErrorLog.Printf("http: panic serving %s: %s", secret, secret)
	records := readProcessLogRecords(t, output.String())
	if len(records) != 1 || records[0]["event"] != "unclassified" || records[0]["msg"] != "unclassified event" || records[0]["level"] != "ERROR" {
		t.Fatalf("HTTP server error log was not sanitized: %#v", records)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("HTTP server error log exposed a formatted message")
	}
}

func readProcessLogRecords(t *testing.T, output string) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var records []map[string]any
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			return records
		} else if err != nil {
			t.Fatalf("decode process log: %v", err)
		}
		records = append(records, record)
	}
}
