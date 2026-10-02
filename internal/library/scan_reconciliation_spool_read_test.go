package library

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
)

type scanSpoolReadTestSource struct {
	data  []byte
	reads int
	bytes int
	short bool
	err   error
}

func (source *scanSpoolReadTestSource) ReadAt(data []byte, offset int64) (int, error) {
	source.reads++
	source.bytes += len(data)
	if source.err != nil {
		return 0, source.err
	}
	if source.short {
		return copy(data[:len(data)-1], source.data[offset:]), nil
	}
	return bytes.NewReader(source.data).ReadAt(data, offset)
}

func scanSpoolReadTestEntries(names []string, offset int) *scanSpoolReadTestSource {
	source := &scanSpoolReadTestSource{data: make([]byte, offset)}
	for index, name := range names {
		data := scanSpoolEncodeEntry(scanSpoolEntry{name: name, version: scanSpoolVersion{
			identity: fmt.Sprintf("1:%d", index), size: int64(index), ctime: 1, mtimeSeconds: 1,
		}})
		source.data = append(source.data, data[:]...)
	}
	return source
}

func TestScanSpoolEntryReadCacheEligibilityBoundaries(t *testing.T) {
	for _, scenario := range []struct {
		count    int
		eligible bool
	}{{16, false}, {17, true}, {1024, true}, {1025, false}} {
		if eligible := scanSpoolEntryCacheEligible(scenario.count); eligible != scenario.eligible {
			t.Fatalf("cache eligibility for %d members = %t, want %t", scenario.count, eligible, scenario.eligible)
		}
	}
}

func TestScanSpoolEntryReadCachePreservesNamesAndBoundaries(t *testing.T) {
	for _, count := range []int{1, 7, 8, 9, 127, 128, 129, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			names := make([]string, count)
			for index := range names {
				names[index] = fmt.Sprintf("Film-%04d.mp4", index)
			}
			source := scanSpoolReadTestEntries(names, 97)
			reader := scanSpoolEntryBlockReader{source: source, offset: 97, count: count}
			for _, index := range []int{0, count - 1, count / 2, 0} {
				entry, position, found, err := scanSpoolLookup(names[index], count, reader.entry)
				if err != nil || !found || position != index || entry.name != names[index] || entry.version.size != int64(index) {
					t.Fatalf("cached member %q differs: entry=%+v position=%d found=%t err=%v", names[index], entry, position, found, err)
				}
			}
			for _, name := range []string{"Before", "Film-0000.mp3", "ZZZ"} {
				if _, _, found, err := scanSpoolLookup(name, count, reader.entry); err != nil || found {
					t.Fatalf("cached lookup accepted an absent member %q: %t, %v", name, found, err)
				}
			}
			for _, index := range []int{-1, count} {
				if _, err := reader.entry(index); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
					t.Fatalf("cached lookup accepted out-of-range index %d: %v", index, err)
				}
			}
		})
	}
	names := []string{"a\xff", "a\xfe", "a\ufffd", "a\\b", "line\nbreak", strings.Repeat("n", 255)}
	sort.Strings(names)
	source := scanSpoolReadTestEntries(names, 97)
	reader := scanSpoolEntryBlockReader{source: source, offset: 97, count: len(names)}
	for index, name := range names {
		entry, position, found, err := scanSpoolLookup(name, len(names), reader.entry)
		if err != nil || !found || position != index || entry.name != name {
			t.Fatalf("cached raw name %q changed: %q, %d, %t, %v", name, entry.name, position, found, err)
		}
	}
}

func TestScanSpoolEntryReadCacheRejectsFailedRefillsAndCorruption(t *testing.T) {
	failure := errors.New("record read failed")
	for _, scenario := range []struct {
		name  string
		short bool
		err   error
		want  error
	}{{"short read", true, nil, io.ErrUnexpectedEOF}, {"read error", false, failure, failure}} {
		t.Run(scenario.name, func(t *testing.T) {
			source := scanSpoolReadTestEntries([]string{"first", "second"}, 97)
			source.short, source.err = scenario.short, scenario.err
			reader := scanSpoolEntryBlockReader{source: source, offset: 97, count: 2}
			if _, err := reader.entry(0); !errors.Is(err, scenario.want) {
				t.Fatalf("failed refill was accepted: %v", err)
			}
			source.short, source.err = false, nil
			entry, err := reader.entry(1)
			if err != nil || entry.name != "second" || source.reads != 2 {
				t.Fatalf("failed refill retained a cache hit: entry=%+v reads=%d err=%v", entry, source.reads, err)
			}
		})
		t.Run(scenario.name+"/occupied slot", func(t *testing.T) {
			count := (scanSpoolReadCacheBlocks + 1) * scanSpoolReadBlockEntries
			names := make([]string, count)
			for index := range names {
				names[index] = fmt.Sprintf("Film-%04d.mp4", index)
			}
			source := scanSpoolReadTestEntries(names, 97)
			reader := scanSpoolEntryBlockReader{source: source, offset: 97, count: count}
			for slot := 0; slot < scanSpoolReadCacheBlocks; slot++ {
				if _, err := reader.entry(slot * scanSpoolReadBlockEntries); err != nil {
					t.Fatal(err)
				}
			}
			source.short, source.err = scenario.short, scenario.err
			if _, err := reader.entry(count - 1); !errors.Is(err, scenario.want) {
				t.Fatalf("failed occupied-slot refill was accepted: %v", err)
			}
			source.short, source.err = false, nil
			entry, err := reader.entry(0)
			if err != nil || entry.name != names[0] || source.reads != scanSpoolReadCacheBlocks+2 {
				t.Fatalf("failed eviction retained its previous accepted range: entry=%+v reads=%d err=%v", entry, source.reads, err)
			}
		})
	}
	source := scanSpoolReadTestEntries([]string{"first", "second"}, 97)
	source.data[97+scanSpoolEntrySize-1] ^= 0x80
	reader := scanSpoolEntryBlockReader{source: source, offset: 97, count: 2}
	if _, err := reader.entry(0); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("corrupt cached record was accepted: %v", err)
	}
	if _, err := reader.entry(0); !errors.Is(err, errScanReconciliationEvidenceUnavailable) || source.reads != 1 {
		t.Fatalf("cache hit bypassed checksum decoding: reads=%d err=%v", source.reads, err)
	}
}

func TestScanSpoolEntryReadCacheReducesBinarySearchReads(t *testing.T) {
	const count = scanSpoolReadCacheMaxEntries
	names := make([]string, count)
	for index := range names {
		names[index] = fmt.Sprintf("Film-%05d.mp4", index)
	}
	cachedSource := scanSpoolReadTestEntries(names, 97)
	directSource := &scanSpoolReadTestSource{data: cachedSource.data}
	reader := scanSpoolEntryBlockReader{source: cachedSource, offset: 97, count: count}
	direct := func(index int) (scanSpoolEntry, error) {
		var data [scanSpoolEntrySize]byte
		if _, err := directSource.ReadAt(data[:], 97+int64(index)*scanSpoolEntrySize); err != nil {
			return scanSpoolEntry{}, err
		}
		return scanSpoolDecodeEntry(data[:])
	}
	for visit := 0; visit < count; visit++ {
		// An odd stride permutes every member without granting sorted-enumeration
		// locality to the cache.
		index := visit * 4051 % count
		cached, cachedPosition, cachedFound, cachedErr := scanSpoolLookup(names[index], count, reader.entry)
		fresh, freshPosition, freshFound, freshErr := scanSpoolLookup(names[index], count, direct)
		if cachedErr != nil || freshErr != nil || cached != fresh || cachedPosition != freshPosition ||
			!cachedFound || !freshFound || cachedPosition != index {
			t.Fatalf("cached binary search changed member %d", index)
		}
	}
	if cachedSource.reads*5 >= directSource.reads*4 {
		t.Fatalf("bounded cache did not reduce reads by 20%%: cached=%d direct=%d", cachedSource.reads, directSource.reads)
	}
	t.Logf("spool lookup members=%d cached_reads=%d direct_reads=%d cached_bytes=%d direct_bytes=%d cache_bytes=%d",
		count, cachedSource.reads, directSource.reads, cachedSource.bytes, directSource.bytes,
		scanSpoolReadCacheBlocks*scanSpoolReadBlockBytes)
}
