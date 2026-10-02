//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGeneratedMP4SourceEndpointBorrowedIdentityFence(t *testing.T) {
	fixture := generatedEndpointTestFixture(2 * ticksPerSecond)
	path := filepath.Join(t.TempDir(), "structural.mp4")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	certificate, err := MeasureGeneratedMP4SourceEndpoint(context.Background(), source, 0)
	if err != nil || certificate.SampleCount != 48 || certificate.DurationTicks != 2*ticksPerSecond {
		t.Fatalf("held structural source did not produce its endpoint: %+v, %v", certificate, err)
	}
	if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("endpoint inspection changed a borrowed source offset: %d, %v", offset, err)
	}
	if err := ValidateGeneratedMP4SourceEndpointIdentity(source, certificate); err != nil {
		t.Fatalf("unchanged source lost its endpoint identity: %v", err)
	}
	before, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("source lacks the Linux change-time fact")
	}
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	payload := int64(bytes.Index(fixture, []byte("mdat")) + 4)
	deadline := time.Now().Add(2 * time.Second)
	var after os.FileInfo
	for attempt := 0; ; attempt++ {
		if _, err := writer.WriteAt([]byte{byte(attempt)}, payload); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
			t.Fatal(err)
		}
		after, err = source.Stat()
		if err != nil {
			t.Fatal(err)
		}
		afterStat, ok := after.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("mutated source lacks the Linux change-time fact")
		}
		if afterStat.Ctim != beforeStat.Ctim {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("controlled same-size source mutation did not advance change time")
		}
		// Filesystems can expose coarse clock quanta. Wait only until the
		// controlled fixture records the ctime change this test must exercise.
		time.Sleep(time.Millisecond)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("controlled mutation changed an identity fact other than ctime")
	}
	if err := ValidateGeneratedMP4SourceEndpointIdentity(source, certificate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("restored size and mtime concealed a source mutation: %v", err)
	}
}

func TestGeneratedMP4SourceEndpointRejectsInvalidBorrowers(t *testing.T) {
	if _, err := MeasureGeneratedMP4SourceEndpoint(nil, nil, 0); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil context did not fail closed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureGeneratedMP4SourceEndpoint(ctx, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection did not preserve cancellation: %v", err)
	}
	if _, err := MeasureGeneratedMP4SourceEndpoint(context.Background(), nil, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing source descriptor was admitted: %v", err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if _, err := MeasureGeneratedMP4SourceEndpoint(context.Background(), read, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nonregular source descriptor was admitted: %v", err)
	}
	if err := ValidateGeneratedMP4SourceEndpointIdentity(nil, GeneratedSourceEndpointCertificate{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing certificate source identity was admitted: %v", err)
	}
}

func TestGeneratedMP4SourceEndpointActualFiniteSampleSet(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	path := generatedClosureMediaSource(t, ctx, ffmpeg, 24, 100, 90, 93)
	source, before := generatedClosureMediaOpenSource(t, path)
	certificate, err := MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
	if err != nil {
		t.Fatalf("ordinary MP4 sample tables did not establish a finite endpoint: %v", err)
	}
	if certificate.SourceIdentity == "" || certificate.StreamIndex != 0 || certificate.TrackID == 0 || certificate.SampleCount != 2400 ||
		certificate.FrameDuration != (GeneratedRational{Num: 1, Den: 24}) || certificate.Origin != (GeneratedRational{Num: 2, Den: 1}) ||
		certificate.Last != (GeneratedRational{Num: 2447, Den: 24}) || certificate.End != (GeneratedRational{Num: 102, Den: 1}) ||
		!certificate.DurationTicksExact || certificate.DurationTicks != 100*ticksPerSecond ||
		certificate.MetadataSHA256 == ([32]byte{}) || certificate.SampleExtentsSHA256 == ([32]byte{}) {
		t.Fatalf("source origin or table-derived endpoint changed: %+v", certificate)
	}
	plan := generatedClosureMediaPlan("mpegts", 100, 94, 100, false)
	tail, err := MeasureGeneratedSourceRange(ctx, ffprobe, source, plan, 2*ticksPerSecond)
	if err != nil || tail.FrameCount != 144 {
		t.Fatalf("independent actual tail frames did not cover the final table interval: %+v, %v", tail, err)
	}
	origin := generatedClockSeconds(1, certificate.Origin.Num, certificate.Origin.Den)
	last := new(big.Rat).Add(origin, generatedClockSeconds(1, tail.Last.Num, tail.Last.Den))
	end := new(big.Rat).Add(origin, generatedClockSeconds(1, tail.End.Num, tail.End.Den))
	if last.Cmp(generatedClockSeconds(1, certificate.Last.Num, certificate.Last.Den)) != 0 ||
		end.Cmp(generatedClockSeconds(1, certificate.End.Num, certificate.End.Den)) != 0 || tail.FrameDuration != certificate.FrameDuration {
		t.Fatalf("decoded tail belongs to a different sample-table grid: certificate=%+v tail=%+v", certificate, tail)
	}
	if !transcodeSourceUnchanged(source, before) || ValidateGeneratedMP4SourceEndpointIdentity(source, certificate) != nil {
		t.Fatal("source identity changed across sample-table and actual tail evidence")
	}
	if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("source certificate or tail probe changed the borrowed descriptor: offset=%d error=%v", offset, err)
	}
	t.Logf("actual ordinary MP4 finite sample set and decoded tail: certificate=%+v tail=%+v", certificate, tail)

	// A different inode containing identical bytes cannot reuse the certificate.
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replacementPath := filepath.Join(t.TempDir(), "replacement.mp4")
	if err := os.WriteFile(replacementPath, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Open(replacementPath)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := ValidateGeneratedMP4SourceEndpointIdentity(replacement, certificate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("another inode borrowed the sample endpoint: %v", err)
	}

	// Modifying the held inode invalidates the original identity even when the
	// complete table certificate already exists in memory.
	writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.Write([]byte{0})
	closeErr := writer.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("controlled source mutation failed: write=%v close=%v", writeErr, closeErr)
	}
	if err := ValidateGeneratedMP4SourceEndpointIdentity(source, certificate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mutated source retained its earlier sample endpoint: %v", err)
	}
}
