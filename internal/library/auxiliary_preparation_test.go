package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

func auxiliaryPreparationTestFiles(count int) []*preparedThemeFile {
	state := &scanState{root: libraryRoot{id: "root", path: "/media"}}
	instant := time.Date(2026, 1, 2, 3, 4, 5, 6, time.FixedZone("retained-test-zone", 1234))
	probe := &media.Info{Container: "mp3", Streams: make([]media.Stream, 1, 4)}
	probe.Streams[0].Codec = "mp3"
	metadata := &MetadataValues{ProviderIDs: map[string]string{"provider": "shared-value"}, Genres: make([]string, 0, 7), PremiereDate: &instant}
	files := make([]*preparedThemeFile, 0, count+17)
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("The Theme %03d", index)
		candidate := themeCandidate{relative: fmt.Sprintf("Film/theme-music/%03d.mp3", index), kind: themePathKindSong, layout: themePathLayoutMusic}
		file := &preparedThemeFile{state: state, owner: themeDirectoryOwner{id: "owner", itemType: "Movie"},
			candidate: candidate, id: fmt.Sprintf("resource-%d", index), name: name, sortName: name, itemType: "Audio",
			sourceRow: rootBindingRow{root: state.root, document: make([]byte, 3, 11), boundAt: &instant}}
		file.input = &scannedMediaInput{unchanged: true, probe: probe, stored: storedFile{
			id: file.id, rootID: state.root.id, parentID: file.owner.id, path: filepath.Join(state.root.path, candidate.relative),
			name: name, sortName: name, itemType: "Audio", modified: &instant, media: probe, automatic: metadata}}
		files = append(files, file)
	}
	return files
}

func auxiliaryPreparationTestFacts(files []*preparedThemeFile) []auxiliaryProbeFacts {
	values := make([]auxiliaryProbeFacts, len(files))
	for index, file := range files {
		values[index] = preparedAuxiliaryFacts(file)
	}
	return values
}

func TestAuxiliaryProbeFactsAccountingMatchesCompleteProjection(t *testing.T) {
	for _, count := range []int{0, 1, 2, 256} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			files := auxiliaryPreparationTestFiles(count)
			values := auxiliaryPreparationTestFacts(files)
			exact, fits := scanProbeFactsSize(values, math.MaxInt64)
			if !fits {
				t.Fatal("bounded fixture did not have a complete charge")
			}
			for _, limit := range []int64{exact - 1, exact, exact + 1} {
				budget := newAuxiliaryProbeFactsBudget(limit)
				accepted := budget.remaining >= 0
				for _, file := range files {
					before := budget.remaining
					if !budget.add(file) {
						accepted = false
						if budget.remaining != before {
							t.Fatal("failed append left a reusable partial charge")
						}
						break
					}
				}
				if want := scanProbeFactsFit(values, limit); accepted != want {
					t.Fatalf("incremental acceptance=%t complete=%t count=%d budget=%d", accepted, want, count, limit)
				}
				if accepted && budget.remaining != limit-exact {
					t.Fatalf("incremental charge=%d complete=%d", limit-budget.remaining, exact)
				}
			}
		})
	}
}

func TestScanProbeFactsSizePreservesOriginalCharges(t *testing.T) {
	text := "shared"
	instant := time.Date(2026, 1, 2, 3, 4, 5, 6, time.FixedZone("shared-zone", 1234))
	stringInline := int64(reflect.TypeFor[string]().Size())
	for _, test := range []struct {
		name  string
		value any
		want  int64
	}{
		{"nil", nil, 0},
		{"typed-nil", (*string)(nil), int64(reflect.TypeFor[*string]().Size())},
		{"string", text, stringInline + int64(len(text))},
		{"empty-capacity", make([]string, 0, 7), int64(reflect.TypeFor[[]string]().Size()) + 7*stringInline},
		{"shared-pointer", [2]*string{&text, &text}, int64(reflect.TypeFor[[2]*string]().Size()) + 2*(stringInline+int64(len(text)))},
		{"map", map[string]string{"key": "value"}, int64(reflect.TypeFor[map[string]string]().Size()) + 2*stringInline + 64 + 8},
		{"time", instant, int64(reflect.TypeFor[time.Time]().Size())},
	} {
		t.Run(test.name, func(t *testing.T) {
			if charge, fits := scanProbeFactsSize(test.value, test.want); !fits || charge != test.want {
				t.Fatalf("charge=%d fits=%t want=%d", charge, fits, test.want)
			}
			if _, fits := scanProbeFactsSize(test.value, test.want-1); fits {
				t.Fatal("one byte less than the complete charge was accepted")
			}
		})
	}
	for _, limit := range []int64{math.MinInt64, -1, 0} {
		budget := newAuxiliaryProbeFactsBudget(limit)
		if budget.remaining >= 0 || budget.add(auxiliaryPreparationTestFiles(1)[0]) {
			t.Fatalf("invalid slice budget was accepted: %d", limit)
		}
	}
}

func auxiliarySortTestRows(ordinals []int64, names []string) *ownedCallbackRowsFixture {
	index := -1
	return &ownedCallbackRowsFixture{next: func() bool { index++; return index < len(ordinals) }, scan: func(destinations ...any) error {
		*destinations[0].(*int64) = ordinals[index]
		*destinations[1].(*string) = names[index]
		return nil
	}}
}

func TestAuxiliarySortBatchMappingAndFailures(t *testing.T) {
	errScan, errRows := errors.New("sort scan failed"), errors.New("sort rows failed")
	for _, test := range []struct {
		name     string
		ordinals []int64
		values   []string
		failure  error
		cancel   bool
	}{
		{"complete-duplicates", []int64{1, 2}, []string{"Same", "Same"}, nil, false},
		{"missing-settings", nil, nil, pgx.ErrNoRows, false},
		{"missing-result", []int64{1}, []string{"Same"}, ErrUnavailable, false},
		{"reordered-result", []int64{2, 1}, []string{"B", "A"}, ErrUnavailable, false},
		{"duplicate-ordinal", []int64{1, 1}, []string{"A", "A"}, ErrUnavailable, false},
		{"extra-result", []int64{1, 2, 3}, []string{"A", "B", "C"}, ErrUnavailable, false},
		{"scan-error", []int64{1}, []string{"A"}, errScan, false},
		{"rows-error", []int64{1, 2}, []string{"A", "B"}, errRows, false},
		{"cancelled", []int64{1, 2}, []string{"A", "B"}, context.Canceled, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := auxiliaryPreparationTestFiles(2)
			budget := newAuxiliaryProbeFactsBudget(scanProbeFactsBytes)
			for _, file := range files {
				file.sortName = ""
				if !budget.add(file) {
					t.Fatal("small unsorted fixture exceeded its budget")
				}
			}
			rows := auxiliarySortTestRows(test.ordinals, test.values)
			if test.failure == errScan {
				rows.scan = func(...any) error { return errScan }
			}
			if test.failure == errRows {
				rows.terminal = errRows
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			err := applyAuxiliarySortNames(ctx, files, rows, &budget)
			if test.failure == nil {
				if err != nil || files[0].sortName != "Same" || files[1].sortName != "Same" || !files[0].changed || !files[1].changed {
					t.Fatalf("duplicate names lost ordinal mapping or changed decisions: %v", err)
				}
			} else if test.failure == ErrUnavailable {
				if err == nil {
					t.Fatal("incomplete or invalid result mapping was accepted")
				}
			} else if !errors.Is(err, test.failure) {
				t.Fatalf("failure=%v want=%v", err, test.failure)
			}
		})
	}
}

func TestAuxiliarySortBatchChargesNamesAndAuditsMutatedFacts(t *testing.T) {
	for _, short := range []bool{false, true} {
		files := auxiliaryPreparationTestFiles(1)
		files[0].sortName = ""
		base, _ := scanProbeFactsSize(auxiliaryPreparationTestFacts(files), math.MaxInt64)
		name := "Generated Sort"
		limit := base + int64(len(name))
		if short {
			limit--
		}
		budget := newAuxiliaryProbeFactsBudget(limit)
		if !budget.add(files[0]) {
			t.Fatal("base fixture exceeded a budget reserved for its sort name")
		}
		err := applyAuxiliarySortNames(context.Background(), files, auxiliarySortTestRows([]int64{1}, []string{name}), &budget)
		if short {
			if !errors.Is(err, errScanProbeFactsBudget) {
				t.Fatalf("sort result bypassed the original full-facts limit: %v", err)
			}
			continue
		}
		if err != nil || budget.remaining != 0 || !scanProbeFactsFit(auxiliaryPreparationTestFacts(files), limit) {
			t.Fatalf("exact sort-name charge disagrees with the complete projection: %v", err)
		}
		files[0].sortName = strings.Repeat("x", scanProbeFactsBytes)
		if auxiliaryProbeFactsFit(files) {
			t.Fatal("final full audit trusted a charge after retained facts changed")
		}
		files[0].sortName = ""
		if !scanProbeFactsFit(auxiliaryPreparationTestFacts(files), base) {
			t.Fatal("fresh audit retained the charge of a replaced sort name")
		}
	}
}

func BenchmarkAuxiliaryProbeFactsPreparation(b *testing.B) {
	for _, count := range []int{1, 64, 256} {
		files := auxiliaryPreparationTestFiles(count)
		b.Run(fmt.Sprintf("prefix/%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(count*(count+1)/2+count), "facts/op")
			for range b.N {
				for end := 1; end <= len(files); end++ {
					if !auxiliaryProbeFactsFit(files[:end]) {
						b.Fatal("bounded prefix fixture exceeded its budget")
					}
				}
				if !auxiliaryProbeFactsFit(files) {
					b.Fatal("publication audit rejected the bounded fixture")
				}
			}
		})
		b.Run(fmt.Sprintf("incremental/%d", count), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(3*count), "facts/op")
			for range b.N {
				budget := newAuxiliaryProbeFactsBudget(scanProbeFactsBytes)
				for _, file := range files {
					if !budget.add(file) {
						b.Fatal("incremental fixture exceeded its budget")
					}
				}
				// Include both the new post-sort audit and unchanged publication audit.
				if !auxiliaryProbeFactsFit(files) || !auxiliaryProbeFactsFit(files) {
					b.Fatal("complete audit rejected the bounded fixture")
				}
			}
		})
	}
}
