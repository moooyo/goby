//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func audioWaveformReadFixture(t *testing.T) (mediaSourceFixture, media.AudioWaveformData, string) {
	t.Helper()
	fixture := mediaSourceTestCatalog(t, waveformFixtureProber{})
	snapshot, err := fixture.store.readMediaSourceFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := audioWaveformSourceStamp(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var streams []media.Stream
	for _, stream := range snapshot.mediaFile.Item.Media.Streams {
		if stream.CodecType == "audio" && !stream.IsExternal && !stream.IsAttachedPicture {
			streams = append(streams, stream)
		}
	}
	data := audioWaveformTestData(len(streams))
	data.DurationTicks = snapshot.mediaFile.Item.Media.DurationTicks
	for index, stream := range streams {
		data.Tracks[index].AudioWaveformTrackSummary = media.AudioWaveformTrackSummary{
			StreamIndex: stream.Index, Channels: stream.Channels, SampleRate: stream.SampleRate, ChannelLayout: stream.ChannelLayout,
			SampleCount: data.DurationTicks * int64(stream.SampleRate) / media.TicksPerSecond, CoverageEndTicks: data.DurationTicks,
		}
	}
	encoded, err := media.MarshalAudioWaveforms(data)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.OpenRoot(filepath.Dir(fixture.path))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	name := filepath.Base(fixture.path)
	directory, err := openAudioWaveformDirectory(parent, name, true)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := claimAudioWaveformDirectory(directory, name); err != nil {
		t.Fatal(err)
	}
	generation := "gen-" + strings.Repeat("a", 32) + ".gawf"
	file, err := directory.OpenFile(generation, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(encoded)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	manifest := audioWaveformManifest{
		Format: audioWaveformFileFormat, SourceName: name, SourceRevision: snapshot.mediaFile.Item.AnalysisSourceRevision,
		SourceStamp: stamp, OperationID: strings.Repeat("b", 32), Generation: generation, SHA256: hex.EncodeToString(digest[:]),
		Summary: data.Summary(int64(len(encoded))), CreatedAt: time.Now().UTC(),
	}
	if _, err := writeAudioWaveformJSON(directory, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	return fixture, data, filepath.Join(directory.Name(), generation)
}

func TestAudioWaveformReadReturnsOwnedDataAfterDescriptorClose(t *testing.T) {
	fixture, expected, path := audioWaveformReadFixture(t)
	events := analysisPreviewWatchSource(t, path)
	data, artifact, err := fixture.store.ReadAudioWaveformFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID)
	if err != nil || !artifact.Available || artifact.Stale || !reflect.DeepEqual(data, expected) {
		t.Fatalf("decoded read lost validated data: artifact=%+v error=%v", artifact, err)
	}
	if opens, closes, reads := events(); opens != 1 || closes != 1 || reads == 0 {
		t.Fatalf("decoded read did not retire its payload descriptor: opens=%d closes=%d reads=%d", opens, closes, reads)
	}
	if err := fixture.store.Close(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, expected) {
		t.Fatal("retiring the store changed the caller-owned waveform arrays")
	}
}

type audioWaveformReadTraceKey struct{}

type audioWaveformReadTrace struct {
	authorizations atomic.Int32
	beforeFinal    func(context.Context)
}

func (trace *audioWaveformReadTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(audioWaveformReadTraceKey{}) == trace && strings.Contains(data.SQL, "SELECT is_administrator, is_disabled, policy") &&
		strings.Contains(data.SQL, "FROM users WHERE id = $1") && trace.authorizations.Add(1) == 2 && trace.beforeFinal != nil {
		trace.beforeFinal(ctx)
	}
	return ctx
}

func (*audioWaveformReadTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func audioWaveformTracedReader(t *testing.T, fixture *mediaSourceFixture, trace *audioWaveformReadTrace) context.Context {
	t.Helper()
	if err := fixture.store.Close(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	configuration := fixture.pool.Config().Copy()
	configuration.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, reader)
	store, err := New(reader, fixture.store.prober, []string{fixture.allowedRoot})
	if err != nil {
		t.Fatal(err)
	}
	entitiesStoreCleanup(t, store)
	fixture.store = store
	return context.WithValue(fixture.ctx, audioWaveformReadTraceKey{}, trace)
}

func TestAudioWaveformDecodedReadStillChecksFinalAccessAndSource(t *testing.T) {
	for _, change := range []string{"access", "source"} {
		t.Run(change, func(t *testing.T) {
			fixture, _, path := audioWaveformReadFixture(t)
			mutated := make(chan error, 1)
			trace := &audioWaveformReadTrace{beforeFinal: func(context.Context) {
				if change == "access" {
					_, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, fixture.userID)
					mutated <- err
					return
				}
				staged := fixture.path + ".replacement"
				if err := os.WriteFile(staged, []byte(fixture.contents), 0600); err != nil {
					mutated <- err
					return
				}
				mutated <- os.Rename(staged, fixture.path)
			}}
			ctx := audioWaveformTracedReader(t, &fixture, trace)
			events := analysisPreviewWatchSource(t, path)
			data, artifact, err := fixture.store.ReadAudioWaveformFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID)
			want := ErrForbidden
			if change == "source" {
				want = ErrAudioWaveformStale
			}
			if !errors.Is(err, want) || !reflect.DeepEqual(data, media.AudioWaveformData{}) || !reflect.DeepEqual(artifact, AudioWaveformArtifact{}) {
				t.Fatalf("decoded data escaped final %s check: artifact=%+v error=%v", change, artifact, err)
			}
			if trace.authorizations.Load() != 2 {
				t.Fatalf("final authorization did not run: %d reads", trace.authorizations.Load())
			}
			if err := <-mutated; err != nil {
				t.Fatal(err)
			}
			if opens, closes, reads := events(); opens != 1 || closes != 1 || reads == 0 {
				t.Fatalf("final rejection failed to retire the decoded payload: opens=%d closes=%d reads=%d", opens, closes, reads)
			}
		})
	}
}

func TestAudioWaveformReadCancellationRetainsWorkerUntilDescriptorClose(t *testing.T) {
	fixture, _, path := audioWaveformReadFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	trace := &audioWaveformReadTrace{beforeFinal: func(context.Context) {
		close(entered)
		<-release
	}}
	ctx := audioWaveformTracedReader(t, &fixture, trace)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := analysisPreviewWatchSource(t, path)
	type outcome struct {
		data     media.AudioWaveformData
		artifact AudioWaveformArtifact
		err      error
	}
	returned := make(chan outcome, 1)
	go func() {
		data, artifact, err := fixture.store.ReadAudioWaveformFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID)
		returned <- outcome{data, artifact, err}
	}()
	mediaSourceAdmissionTestWait(t, entered, "waveform final access check")
	if opens, closes, reads := events(); opens != 1 || closes != 0 || reads == 0 {
		t.Fatalf("cancellation barrier did not hold a decoded payload: opens=%d closes=%d reads=%d", opens, closes, reads)
	}
	cancel()
	select {
	case result := <-returned:
		if !errors.Is(result.err, context.Canceled) || !reflect.DeepEqual(result.data, media.AudioWaveformData{}) || !reflect.DeepEqual(result.artifact, AudioWaveformArtifact{}) {
			t.Fatalf("canceled read returned data owned by its active worker: artifact=%+v error=%v", result.artifact, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled waveform caller waited for the blocked worker")
	}
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if err := fixture.store.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("store retired a worker still holding the waveform: %v", err)
	}
	select {
	case <-fixture.store.done:
		t.Fatal("store finished before the waveform worker returned")
	default:
	}
	once.Do(func() { close(release) })
	mediaSourceAdmissionTestWait(t, fixture.store.done, "waveform descriptor and worker retirement")
	if opens, closes, _ := events(); opens != 0 || closes != 1 {
		t.Fatalf("undelivered waveform descriptor outlived worker retirement: opens=%d closes=%d", opens, closes)
	}
}
