//go:build linux

package library

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type analysisPreviewRevalidationTraceKey struct{}
type analysisPreviewRevalidationCommitKey struct{}

type analysisPreviewRevalidationTrace struct {
	mu             sync.Mutex
	sources, refs  int
	afterProof     func(context.Context) error
	injected       bool
	injectionError error
}

func (trace *analysisPreviewRevalidationTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(analysisPreviewRevalidationTraceKey{}) != trace {
		return ctx
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if strings.Contains(data.SQL, "SELECT "+analysisSourceColumns+" FROM items i ") {
		trace.sources++
	}
	if strings.Contains(data.SQL, "FROM analysis_previews p CROSS JOIN analysis_settings settings") {
		trace.refs++
	}
	// The second analysis source read follows the real descriptor's close. Its
	// commit ends that proof's snapshot, before reference authorization begins.
	if trace.sources == 2 && !trace.injected && trace.afterProof != nil && strings.EqualFold(strings.TrimSpace(data.SQL), "commit") {
		return context.WithValue(ctx, analysisPreviewRevalidationCommitKey{}, true)
	}
	return ctx
}

func (trace *analysisPreviewRevalidationTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil || ctx.Value(analysisPreviewRevalidationCommitKey{}) != true {
		return
	}
	trace.mu.Lock()
	if trace.injected {
		trace.mu.Unlock()
		return
	}
	trace.injected = true
	hook := trace.afterProof
	trace.mu.Unlock()
	err := hook(ctx)
	trace.mu.Lock()
	trace.injectionError = err
	trace.mu.Unlock()
}

func analysisPreviewRevalidationTraceFixture(t *testing.T, fixture *mediaSourceFixture, trace *analysisPreviewRevalidationTrace) context.Context {
	t.Helper()
	if err := fixture.store.Close(fixture.ctx); err != nil {
		t.Fatalf("retire original catalog before installing its traced reader: %v", err)
	}
	configuration := fixture.pool.Config()
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
	return context.WithValue(fixture.ctx, analysisPreviewRevalidationTraceKey{}, trace)
}

// Alternating open and close events prevent successive opens from coalescing.
// This observes actual source descriptors, independently of SQL projections.
func analysisPreviewWatchSource(t *testing.T, path string) func() (int, int, int) {
	t.Helper()
	descriptor, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Close(descriptor); err != nil {
			t.Errorf("close preview source watch: %v", err)
		}
	})
	if _, err := syscall.InotifyAddWatch(descriptor, path, syscall.IN_OPEN|syscall.IN_CLOSE_NOWRITE|syscall.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	return func() (opens, closes, reads int) {
		t.Helper()
		var buffer [4096]byte
		for {
			count, err := syscall.Read(descriptor, buffer[:])
			if errors.Is(err, syscall.EAGAIN) {
				return
			}
			if err != nil || count == 0 {
				t.Fatalf("read preview source events: bytes=%d error=%v", count, err)
			}
			for offset := 0; offset < count; {
				if count-offset < syscall.SizeofInotifyEvent {
					t.Fatal("preview source watch returned a partial event")
				}
				mask := binary.NativeEndian.Uint32(buffer[offset+4 : offset+8])
				length := int(binary.NativeEndian.Uint32(buffer[offset+12 : offset+16]))
				if mask&(syscall.IN_Q_OVERFLOW|syscall.IN_IGNORED|syscall.IN_UNMOUNT) != 0 || length > count-offset-syscall.SizeofInotifyEvent {
					t.Fatal("preview source watch lost its observation")
				}
				if mask&syscall.IN_OPEN != 0 {
					opens++
				}
				if mask&syscall.IN_CLOSE_NOWRITE != 0 {
					closes++
				}
				if mask&syscall.IN_ACCESS != 0 {
					reads++
				}
				offset += syscall.SizeofInotifyEvent + length
			}
		}
	}
}

func TestAnalysisPreviewCombinedReadProvesSourceOnceWithoutReadingContent(t *testing.T) {
	fixture, value := analysisPreviewCatalogFixture(t)
	trace := &analysisPreviewRevalidationTrace{}
	ctx := analysisPreviewRevalidationTraceFixture(t, &fixture, trace)
	events := analysisPreviewWatchSource(t, fixture.path)
	source, references, err := fixture.store.GetCurrentAnalysisSourceAndPreviewsFor(ctx, Subject{UserID: fixture.userID},
		value.ItemID, media.SourceID(value.ItemID), value.SourceRevision)
	if err != nil || source.ItemID != value.ItemID || source.SourceRevision != value.SourceRevision ||
		!reflect.DeepEqual(references, []AnalysisPreview{value}) {
		t.Fatalf("combined read lost its current source or references: source=%+v references=%+v error=%v", source, references, err)
	}
	if opens, closes, reads := events(); opens != 1 || closes != 1 || reads != 0 {
		t.Fatalf("preview source proof did not open and close exactly one unread descriptor: opens=%d closes=%d reads=%d", opens, closes, reads)
	}
	if trace.refs != 1 {
		t.Fatalf("combined source proof did not read exactly one reference set: %d", trace.refs)
	}
}

func TestAnalysisPreviewExpectedSourceFailurePrecedesInvalidReference(t *testing.T) {
	fixture, value := analysisPreviewCatalogFixture(t)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET timeline=decode(repeat('ff',octet_length(timeline)),'hex') WHERE item_id=$1`, value.ItemID); err != nil {
		t.Fatal(err)
	}
	trace := &analysisPreviewRevalidationTrace{}
	ctx := analysisPreviewRevalidationTraceFixture(t, &fixture, trace)
	for _, expected := range []struct{ sourceID, revision string }{
		{media.SourceID(value.ItemID), "stale-preview-source"},
		{media.SourceID(value.ItemID), ""},
		{"", value.SourceRevision},
	} {
		source, references, err := fixture.store.GetCurrentAnalysisSourceAndPreviewsFor(ctx, Subject{UserID: fixture.userID},
			value.ItemID, expected.sourceID, expected.revision)
		if !errors.Is(err, ErrAnalysisSourceChanged) || source != (AnalysisSource{}) || references != nil || trace.refs != 0 {
			t.Fatalf("stale source reached reference parsing: source=%+v references=%v queries=%d error=%v", source, references, trace.refs, err)
		}
	}
	// The same durable corruption must fail when the expected source is current;
	// otherwise the stale-source assertion would not establish failure ordering.
	_, references, err := fixture.store.GetCurrentAnalysisSourceAndPreviewsFor(ctx, Subject{UserID: fixture.userID},
		value.ItemID, media.SourceID(value.ItemID), value.SourceRevision)
	if !errors.Is(err, ErrUnavailable) || references != nil || trace.refs != 1 {
		t.Fatalf("current source did not reject the invalid reference: references=%v queries=%d error=%v", references, trace.refs, err)
	}
}

func TestAnalysisPreviewReferencesObserveChangesAfterPhysicalProof(t *testing.T) {
	for _, test := range []struct {
		name, mutation string
		wantError      error
	}{
		{"source", `UPDATE items SET media=jsonb_set(media,'{DurationTicks}',to_jsonb((media->>'DurationTicks')::bigint+1)) WHERE id=$1`, ErrAnalysisSourceChanged},
		{"settings_revision", `UPDATE analysis_settings SET revision=revision+1 WHERE id=1`, nil},
		{"publication_epoch", `UPDATE analysis_settings SET publication_epoch=publication_epoch+1 WHERE id=1`, nil},
		{"playback_policy", `UPDATE users SET policy='{"EnableAllFolders":true,"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, ErrForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, value := analysisPreviewCatalogFixture(t)
			trace := &analysisPreviewRevalidationTrace{afterProof: func(ctx context.Context) error {
				// A separate physical connection commits only after the source
				// proof's last transaction has released its snapshot.
				var err error
				switch test.name {
				case "source":
					_, err = fixture.pool.Exec(ctx, test.mutation, value.ItemID)
				case "playback_policy":
					_, err = fixture.pool.Exec(ctx, test.mutation, fixture.userID)
				default:
					_, err = fixture.pool.Exec(ctx, test.mutation)
				}
				return err
			}}
			ctx := analysisPreviewRevalidationTraceFixture(t, &fixture, trace)
			source, references, err := fixture.store.GetCurrentAnalysisSourceAndPreviewsFor(ctx, Subject{UserID: fixture.userID},
				value.ItemID, media.SourceID(value.ItemID), value.SourceRevision)
			if !trace.injected || trace.injectionError != nil {
				t.Fatalf("reference boundary mutation did not commit: injected=%t error=%v", trace.injected, trace.injectionError)
			}
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) || source != (AnalysisSource{}) || references != nil || trace.refs != 0 {
					t.Fatalf("reference transaction reused earlier source authority: source=%+v references=%v queries=%d error=%v", source, references, trace.refs, err)
				}
				return
			}
			if err != nil || source.SourceRevision != value.SourceRevision || references == nil || len(references) != 0 || trace.refs != 1 {
				t.Fatalf("reference transaction reused earlier preview settings: source=%+v references=%v queries=%d error=%v", source, references, trace.refs, err)
			}
		})
	}
}
