package library

import "io"

const (
	scanSpoolReadBlockEntries    = 8
	scanSpoolReadCacheBlocks     = 16
	scanSpoolReadBlockBytes      = scanSpoolReadBlockEntries * scanSpoolEntrySize
	scanSpoolReadCacheMinEntries = 16
	scanSpoolReadCacheMaxEntries = 1024
)

func scanSpoolEntryCacheEligible(count int) bool {
	return count > scanSpoolReadCacheMinEntries && count <= scanSpoolReadCacheMaxEntries
}

type scanSpoolEntryReadBlock struct {
	data  [scanSpoolReadBlockBytes]byte
	start int
	count int
	used  uint64
}

// A verification retains at most 48 KiB of raw record blocks. Reusing the upper
// binary-search blocks reduces small reads without retaining a directory name
// map. Checksums and source facts are decoded on every access. This reader is
// never shared across verification passes, directories or absence witnesses.
type scanSpoolEntryBlockReader struct {
	source io.ReaderAt
	offset int64
	count  int
	clock  uint64
	blocks [scanSpoolReadCacheBlocks]scanSpoolEntryReadBlock
}

func (reader *scanSpoolEntryBlockReader) entry(index int) (scanSpoolEntry, error) {
	if index < 0 || index >= reader.count {
		return scanSpoolEntry{}, errScanReconciliationEvidenceUnavailable
	}
	start := index / scanSpoolReadBlockEntries * scanSpoolReadBlockEntries
	oldest := &reader.blocks[0]
	reader.clock++
	for slot := range reader.blocks {
		block := &reader.blocks[slot]
		if block.count != 0 && block.start == start {
			block.used = reader.clock
			return block.entry(index)
		}
		if block.used < oldest.used {
			oldest = block
		}
	}
	if err := oldest.read(reader.source, reader.offset, start, reader.count); err != nil {
		return scanSpoolEntry{}, err
	}
	oldest.used = reader.clock
	return oldest.entry(index)
}

func (block *scanSpoolEntryReadBlock) read(source io.ReaderAt, offset int64, start, count int) error {
	// A failed refill cannot publish a partially read block or preserve a stale
	// accepted range in this slot.
	block.count, block.used = 0, 0
	length := min(scanSpoolReadBlockEntries, count-start) * scanSpoolEntrySize
	n, err := source.ReadAt(block.data[:length], offset+int64(start)*scanSpoolEntrySize)
	if err != nil {
		return err
	}
	if n != length {
		return io.ErrUnexpectedEOF
	}
	block.start, block.count = start, length/scanSpoolEntrySize
	return nil
}

func (block *scanSpoolEntryReadBlock) entry(index int) (scanSpoolEntry, error) {
	start := (index - block.start) * scanSpoolEntrySize
	return scanSpoolDecodeEntry(block.data[start : start+scanSpoolEntrySize])
}
