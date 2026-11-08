package library

import (
	"math"
	"testing"
)

func TestDirectoryCapacityBytesPreservesFilesystemCounters(t *testing.T) {
	for _, test := range []struct {
		name                               string
		blocks, free, available, blockSize uint64
		want                               directoryCapacity
	}{
		{
			name: "ordinary filesystem", blocks: 100, free: 40, available: 30, blockSize: 4096,
			want: directoryCapacity{totalBytes: 409600, usedBytes: 245760, availableBytes: 122880},
		},
		{
			name: "full filesystem", blocks: 100, blockSize: 4096,
			want: directoryCapacity{totalBytes: 409600, usedBytes: 409600},
		},
		{
			name: "empty filesystem", blocks: 100, free: 100, available: 100, blockSize: 4096,
			want: directoryCapacity{totalBytes: 409600, availableBytes: 409600},
		},
		{
			name: "available below free", blocks: 100, free: 60, available: 10, blockSize: 4,
			want: directoryCapacity{totalBytes: 400, usedBytes: 160, availableBytes: 40},
		},
		{
			name: "available above free", blocks: 100, free: 10, available: 60, blockSize: 4,
			want: directoryCapacity{totalBytes: 400, usedBytes: 360, availableBytes: 240},
		},
		{
			name: "largest representable capacity", blocks: math.MaxUint64, available: math.MaxUint64, blockSize: 1,
			want: directoryCapacity{totalBytes: math.MaxUint64, usedBytes: math.MaxUint64, availableBytes: math.MaxUint64},
		},
		{
			name: "largest representable block", blocks: 1, free: 1, available: 1, blockSize: math.MaxUint64,
			want: directoryCapacity{totalBytes: math.MaxUint64, availableBytes: math.MaxUint64},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := directoryCapacityBytes(test.blocks, test.free, test.available, test.blockSize)
			if err != nil || got == nil || *got != test.want {
				t.Fatalf("filesystem counters changed: got=%+v want=%+v err=%v", got, test.want, err)
			}
		})
	}
}

func TestDirectoryCapacityBytesRejectsInvalidCounters(t *testing.T) {
	for _, test := range []struct {
		name                               string
		blocks, free, available, blockSize uint64
	}{
		{name: "zero blocks", blockSize: 4096},
		{name: "zero block size", blocks: 100, free: 10, available: 10},
		{name: "free exceeds total", blocks: 100, free: 101, available: 10, blockSize: 4096},
		{name: "available exceeds total", blocks: 100, free: 10, available: 101, blockSize: 4096},
		{name: "capacity multiplication overflow", blocks: math.MaxUint64/4096 + 1, blockSize: 4096},
		{name: "overflow with no used blocks", blocks: 2, free: 2, available: 2, blockSize: math.MaxUint64},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := directoryCapacityBytes(test.blocks, test.free, test.available, test.blockSize)
			if got != nil || err == nil {
				t.Fatalf("invalid filesystem counters invented capacity: got=%+v err=%v", got, err)
			}
		})
	}
}
