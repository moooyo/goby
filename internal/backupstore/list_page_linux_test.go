//go:build linux

package backupstore

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func listPageStore(t testing.TB, count, tableCount int) *Store {
	t.Helper()
	config := testStoreConfig(filepath.Join(t.TempDir(), "store"))
	config.MaxObjects = max(16, count+1)
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	data := []byte("listed bytes")
	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	next := store.clone()
	for index := range count {
		id := fmt.Sprintf("%032x", index+1)
		if err := os.WriteFile(filepath.Join(config.Directory, basename(id, true)), data, 0600); err != nil {
			t.Fatal(err)
		}
		file, stat, err := store.openFile(basename(id, true), unix.O_RDONLY)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		summary := testSummary()
		summary.Tables = make([]TableCount, tableCount)
		for table := range tableCount {
			summary.Tables[table] = TableCount{Name: fmt.Sprintf("table_%03d", table), Rows: int64(table + 1)}
		}
		next.Entries = append(next.Entries, record{
			Metadata: Metadata{ID: id, Kind: KindImported, State: StateReady,
				CreatedAt: created, UpdatedAt: created, Size: int64(len(data)),
				Digest: testDigest(data), Verified: true, Summary: &summary},
			Identity: identity(stat), Stamp: stamp(stat), Phase: "ready", FinalName: true,
		})
	}
	if err := store.persist(next); err != nil {
		t.Fatal(err)
	}
	if err := store.validateRecords(); err != nil {
		t.Fatalf("invalid list fixture: %v", err)
	}
	return store
}

func TestListPagePreservesOrderingFilteringAndSummaryIsolation(t *testing.T) {
	s := listPageStore(t, 6, 2)
	base := s.registry.Entries[0].Metadata.CreatedAt
	next := s.clone()
	for index, minutes := range []int{1, 3, 3, 0, 2, 2} {
		next.Entries[index].Metadata.CreatedAt = base.Add(time.Duration(minutes) * time.Minute)
		next.Entries[index].Metadata.UpdatedAt = next.Entries[index].Metadata.CreatedAt
	}
	next.Entries[3].Deleting = true
	if err := s.persist(next); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return base.Add(4 * time.Minute) }
	writer := beginTestWriter(t, s, context.Background())
	t.Cleanup(func() { _ = writer.Close() })
	originalOrder := make([]string, len(s.registry.Entries))
	for index, rec := range s.registry.Entries {
		originalOrder[index] = rec.Metadata.ID
	}
	want := []string{writer.id, originalOrder[2], originalOrder[1], originalOrder[5], originalOrder[4], originalOrder[0]}
	var actual []string
	for offset := 0; offset < len(want); offset += 2 {
		page, err := s.List(context.Background(), offset, 2)
		if err != nil || page.TotalRecordCount != len(want) || page.StartIndex != offset || page.Limit != 2 {
			t.Fatalf("page at %d = %+v, %v", offset, page, err)
		}
		for _, item := range page.Items {
			actual = append(actual, item.ID)
		}
	}
	if !slices.Equal(actual, want) {
		t.Fatalf("paginated inventory = %v, want %v", actual, want)
	}
	for index, rec := range s.registry.Entries {
		if rec.Metadata.ID != originalOrder[index] {
			t.Fatal("listing reordered the durable catalog")
		}
	}
	page, err := s.List(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	page.Items[0].Summary.Tables[0].Rows = 999
	page.Items[0].Summary.ServerID = "changed page"
	stored, err := s.Get(context.Background(), page.Items[0].ID)
	if err != nil || stored.Summary.Tables[0].Rows != 1 || stored.Summary.ServerID == "changed page" {
		t.Fatalf("page mutation changed stored summary: %+v, %v", stored, err)
	}
	summary := testSummary()
	summary.Tables[0].Rows = 42
	if _, err := s.Verify(context.Background(), stored.ID, stored.Digest, summary); err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Summary.Tables[0].Rows != 999 || page.Items[0].Summary.ServerID != "changed page" {
		t.Fatal("verification changed an already returned page")
	}
	updated, err := s.List(context.Background(), 1, 1)
	if err != nil || len(updated.Items) != 1 || updated.Items[0].Summary.Tables[0].Rows != 42 {
		t.Fatalf("new page did not contain the complete verified summary: %+v, %v", updated, err)
	}
}

func TestListEmptyPagesPreserveHealthAndCancellation(t *testing.T) {
	for _, scenario := range []string{"empty", "end", "maximum offset", "all deleting"} {
		t.Run(scenario, func(t *testing.T) {
			count := 3
			if scenario == "empty" {
				count = 0
			}
			s := listPageStore(t, count, 1)
			offset, total := count, count
			if scenario == "maximum offset" {
				offset = math.MaxInt
			}
			if scenario == "all deleting" {
				next := s.clone()
				for index := range next.Entries {
					next.Entries[index].Deleting = true
				}
				if err := s.persist(next); err != nil {
					t.Fatal(err)
				}
				offset, total = 0, 0
			}
			page, err := s.List(context.Background(), offset, 200)
			if err != nil || page.Items == nil || len(page.Items) != 0 || page.TotalRecordCount != total || page.StartIndex != offset || page.Limit != 200 {
				t.Fatalf("empty page lost its boundary metadata: %+v, %v", page, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = s.List(ctx, offset, 200)
			requireError(t, err, context.Canceled)
			_, err = s.List(ctx, -1, 200)
			requireError(t, err, ErrInvalid)
			if !s.Status().Healthy {
				t.Fatal("cancelled pagination degraded a healthy store")
			}
			foreign := filepath.Join(s.cfg.Directory, "unregistered-page-file")
			if err := os.WriteFile(foreign, []byte("foreign bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = s.List(context.Background(), offset, 200)
			requireError(t, err, ErrUnavailable)
			if s.Status().Healthy {
				t.Fatal("empty page bypassed the full inventory audit")
			}
			requireFileBytes(t, foreign, []byte("foreign bytes"))
		})
	}
}

func BenchmarkBackupListPage(b *testing.B) {
	for _, page := range []struct {
		name   string
		offset int
		limit  int
	}{{"one", 0, 1}, {"hundred", 0, 100}, {"empty", 128, 1}} {
		b.Run(page.name, func(b *testing.B) {
			s := listPageStore(b, 128, 128)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := s.List(context.Background(), page.offset, page.limit)
				if err != nil || result.TotalRecordCount != 128 {
					b.Fatalf("List = %+v, %v", result, err)
				}
			}
		})
	}
}
