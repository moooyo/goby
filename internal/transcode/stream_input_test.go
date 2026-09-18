package transcode

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestStreamInputCannotCrossTheRegularFileBoundary(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "regular-source-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := validateSourceInput(file, Plan{}); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSourceInput(reader, Plan{SourceMode: "stream"}); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSourceInput(file, Plan{SourceMode: "stream"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("stream plan accepted a seekable source snapshot")
	}
	if _, err := validateSourceInput(reader, Plan{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("regular plan accepted an unbounded pipe")
	}
}

func TestEnsureStreamConsumesRejectedDescriptor(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var manager *Manager
	if _, err := manager.EnsureStream(context.Background(), Spec{}, reader); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("regular plan was accepted by stream entry point")
	}
	if _, err := reader.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("rejected stream descriptor was retained")
	}
}
