package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnalysisExecutableDescriptorCannotFollowReplacedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'held executable'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	tool, err := analysisOpenTool(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.file.Close()
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'replacement executable'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var output []byte
	sink := &analysisDiscardStderr{}
	err = runAnalysisProcess(context.Background(), "/proc/self/fd/3", nil, nil, nil, 5*time.Second, 128, sink,
		func(reader io.Reader) error { var err error; output, err = io.ReadAll(reader); return err }, tool.file)
	if err != nil || string(output) != "held executable" {
		t.Fatalf("tool path replacement changed executed bytes: %q %v", output, err)
	}
	if err := tool.check(); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatalf("changed tool binding was not retired: %v", err)
	}
}

func TestAnalysisToolRejectsAReplacementWithDifferentAdmittedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	original := []byte("#!/bin/sh\nprintf 'admitted'\n")
	if err := os.WriteFile(path, original, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	expected := hex.EncodeToString(digest[:])
	tool, err := analysisOpenToolExpected(context.Background(), path, expected)
	if err != nil {
		t.Fatal(err)
	}
	tool.file.Close()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'replacement'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if opened, err := analysisOpenToolExpected(context.Background(), path, expected); opened != nil || !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatalf("replacement executable passed admission: %+v %v", opened, err)
	}
	for _, invalid := range []string{"1234", strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		if opened, err := analysisOpenToolExpected(context.Background(), path, invalid); opened != nil || !errors.Is(err, ErrAnalysisUnavailable) {
			t.Fatalf("malformed expected digest was accepted: %+v %v", opened, err)
		}
	}
}
