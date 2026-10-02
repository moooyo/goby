//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedMP4SourceTailActualDeclaredEndpoint(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	path := generatedClosureMediaSource(t, ctx, ffmpeg, 24, 100, 90, 93)
	source, before := generatedClosureMediaOpenSource(t, path)
	certificate, err := MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
	if err != nil {
		t.Fatalf("actual source lacks a declared sample-set endpoint: %v", err)
	}
	plan := generatedClosureMediaPlan("mpegts", 100, 94, 100, false)
	tail, err := MeasureGeneratedMP4SourceTail(ctx, ffprobe, source, plan, certificate)
	if err != nil || tail.FrameCount != 144 || tail.StartTicks != 94*ticksPerSecond || tail.EndTicks != 100*ticksPerSecond ||
		tail.First != (GeneratedRational{Num: 94, Den: 1}) || tail.Last != (GeneratedRational{Num: 2399, Den: 24}) ||
		tail.End != (GeneratedRational{Num: 100, Den: 1}) || tail.FrameDuration != certificate.FrameDuration {
		t.Fatalf("actual tail did not cover exactly the declared endpoint: certificate=%+v tail=%+v error=%v", certificate, tail, err)
	}
	origin := generatedClockSeconds(1, certificate.Origin.Num, certificate.Origin.Den)
	last := new(big.Rat).Add(origin, generatedClockSeconds(1, tail.Last.Num, tail.Last.Den))
	end := new(big.Rat).Add(origin, generatedClockSeconds(1, tail.End.Num, tail.End.Den))
	if last.Cmp(generatedClockSeconds(1, certificate.Last.Num, certificate.Last.Den)) != 0 ||
		end.Cmp(generatedClockSeconds(1, certificate.End.Num, certificate.End.Den)) != 0 {
		t.Fatal("decoded tail clocks do not belong to the table certificate's absolute epoch")
	}
	if !transcodeSourceUnchanged(source, before) || ValidateGeneratedMP4SourceEndpointIdentity(source, certificate) != nil {
		t.Fatal("source identity changed across certificate and complete observed tail")
	}
	if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("tail inspection changed the borrowed source descriptor: offset=%d error=%v", offset, err)
	}
	t.Logf("actual declared endpoint and complete observed tail: certificate=%+v tail=%+v", certificate, tail)
}

func TestGeneratedMP4SourceTailRejectsStaleIdentityBeforeLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "structural.mp4")
	if err := os.WriteFile(path, generatedEndpointTestFixture(0), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	certificate, err := MeasureGeneratedMP4SourceEndpoint(context.Background(), source, 0)
	if err != nil {
		t.Fatal(err)
	}
	plan := generatedClosureMediaPlan("mpegts", 2, 0, 2, false)
	writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.Write([]byte{0})
	closeErr := writer.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("controlled source mutation failed: write=%v close=%v", writeErr, closeErr)
	}
	// The executable is deliberately absent. Identity rejection must happen
	// before resolving or launching any decoder for the changed source.
	if _, err := MeasureGeneratedMP4SourceTail(context.Background(), "", source, plan, certificate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("changed source reached decoder setup: %v", err)
	}
}

func TestGeneratedMP4SourceTailRejectsCancelledCall(t *testing.T) {
	plan, certificate := generatedMP4SourceTailTestPlanAndCertificate()
	if _, err := MeasureGeneratedMP4SourceTail(nil, "", nil, plan, certificate); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil tail context did not fail closed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureGeneratedMP4SourceTail(ctx, "", nil, plan, certificate); !errors.Is(err, context.Canceled) {
		t.Fatalf("tail cancellation lost error priority: %v", err)
	}
	certificate.SourceIdentity = ""
	if _, err := MeasureGeneratedMP4SourceTail(context.Background(), "", nil, plan, certificate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tail without endpoint identity reached decoder setup: %v", err)
	}
}
