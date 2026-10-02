//go:build linux

package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedAVEffectiveCaptureKeepsExactBoundedJSONAndOwnsFile(t *testing.T) {
	directory := t.TempDir()
	data := []byte(`{"frames":[],"streams":[]}`)
	var captured *os.File
	var request GeneratedAVEffectiveJSONCaptureRequest
	ctx := WithGeneratedAVEffectiveJSONCapture(context.Background(), func(value GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error) {
		request = value
		var err error
		captured, err = os.OpenFile(filepath.Join(directory, "raw.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		return captured, err
	})
	if err := generatedAVCaptureEffectiveJSON(ctx, data, nil); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(directory, "raw.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) || request.Ordinal != 1 || request.Bytes != len(data) || request.LimitBytes != 4<<20 || request.SHA256 != sha256.Sum256(data) {
		t.Fatal("capture changed bytes or enlarged the existing probe cap")
	}
	if _, err := captured.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("capture did not consume transferred file ownership")
	}
}

func TestGeneratedAVEffectiveCaptureBoundsFactoryAndRejectsNonemptyTarget(t *testing.T) {
	calls := 0
	ctx := WithGeneratedAVEffectiveJSONCapture(context.Background(), func(value GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error) { calls++; return nil, nil })
	for index := 0; index < 10; index++ {
		if err := generatedAVCaptureEffectiveJSON(ctx, []byte(`{}`), nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 8 {
		t.Fatal("capture factory calls exceeded the fixed diagnostic bound")
	}
	if err := generatedAVCaptureEffectiveJSON(ctx, bytes.Repeat([]byte{'x'}, (4<<20)+1), nil); !errors.Is(err, ErrTimelineLimit) {
		t.Fatal("capture enlarged the existing stdout bound")
	}
	directory := t.TempDir()
	var transferred *os.File
	bad := WithGeneratedAVEffectiveJSONCapture(context.Background(), func(value GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error) {
		var err error
		transferred, err = os.OpenFile(filepath.Join(directory, fmt.Sprintf("raw-%d.json", value.Ordinal)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, err = transferred.Write([]byte("existing"))
		}
		return transferred, err
	})
	if err := generatedAVCaptureEffectiveJSON(bad, []byte(`{}`), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("capture appended to a nonexclusive existing target")
	}
	if _, err := transferred.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("rejected capture retained transferred ownership")
	}
	factoryFailure := errors.New("capture factory failed after opening")
	failed := WithGeneratedAVEffectiveJSONCapture(context.Background(), func(value GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error) {
		var err error
		transferred, err = os.OpenFile(filepath.Join(directory, "failed-factory.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		return transferred, factoryFailure
	})
	if err := generatedAVCaptureEffectiveJSON(failed, []byte(`{}`), nil); !errors.Is(err, factoryFailure) {
		t.Fatal("capture lost its original factory error")
	}
	if _, err := transferred.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("factory error leaked a nonnil transferred file")
	}
}
