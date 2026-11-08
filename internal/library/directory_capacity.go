package library

import (
	"errors"
	"math"
)

var errDirectoryCapacityUnavailable = errors.New("directory filesystem capacity is unavailable")

// These counters describe the backing filesystem, not directory contents or a
// quota. Available blocks exclude reservations which may remain in free blocks.
type directoryCapacity struct {
	totalBytes     uint64
	usedBytes      uint64
	availableBytes uint64
}

func directoryCapacityBytes(blocks, free, available, blockSize uint64) (*directoryCapacity, error) {
	if blocks == 0 || blockSize == 0 || free > blocks || available > blocks || blocks > math.MaxUint64/blockSize {
		return nil, errDirectoryCapacityUnavailable
	}
	return &directoryCapacity{
		totalBytes: blocks * blockSize, usedBytes: (blocks - free) * blockSize,
		availableBytes: available * blockSize,
	}, nil
}
