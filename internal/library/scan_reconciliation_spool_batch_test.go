package library

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"testing"
)

type scanSpoolBatchTestWrite struct {
	offset int64
	data   []byte
}

type scanSpoolBatchTestWriter struct {
	writes []scanSpoolBatchTestWrite
	short  bool
	err    error
}

func (writer *scanSpoolBatchTestWriter) WriteAt(data []byte, offset int64) (int, error) {
	writer.writes = append(writer.writes, scanSpoolBatchTestWrite{offset: offset, data: append([]byte(nil), data...)})
	if writer.err != nil {
		return 0, writer.err
	}
	if writer.short {
		return len(data) - 1, nil
	}
	return len(data), nil
}

func TestScanSpoolEntryBatchPreservesBytesAndOffsets(t *testing.T) {
	for _, count := range []int{0, 1, 63, 64, 65, 128, 135} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			destination := &scanSpoolBatchTestWriter{}
			writer := scanSpoolEntryBatchWriter{writer: destination, offset: 97}
			var expected []byte
			for index := 0; index < count; index++ {
				var entry [scanSpoolEntrySize]byte
				for position := range entry {
					entry[position] = byte(index + position)
				}
				expected = append(expected, entry[:]...)
				if err := writer.write(entry); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.flush(); err != nil {
				t.Fatal(err)
			}
			if err := writer.flush(); err != nil {
				t.Fatal(err)
			}
			if want := (count + scanSpoolWriteBatchEntries - 1) / scanSpoolWriteBatchEntries; len(destination.writes) != want {
				t.Fatalf("entry writes = %d, want %d", len(destination.writes), want)
			}
			var actual []byte
			for _, written := range destination.writes {
				if written.offset != 97+int64(len(actual)) || len(written.data) > len(writer.buffer) || len(written.data)%scanSpoolEntrySize != 0 {
					t.Fatalf("invalid bounded write at %d with %d bytes", written.offset, len(written.data))
				}
				actual = append(actual, written.data...)
			}
			if !bytes.Equal(actual, expected) || writer.offset != 97+int64(len(expected)) || writer.used != 0 {
				t.Fatal("batching changed entry bytes or final write position")
			}
		})
	}
}

func TestScanSpoolEntryBatchRejectsShortAndFailedWrites(t *testing.T) {
	failure := errors.New("entry write failed")
	for _, scenario := range []struct {
		name  string
		short bool
		err   error
		want  error
	}{{"short write", true, nil, io.ErrShortWrite}, {"write failure", false, failure, failure}} {
		for _, count := range []int{1, scanSpoolWriteBatchEntries} {
			t.Run(scenario.name+"/"+strconv.Itoa(count), func(t *testing.T) {
				destination := &scanSpoolBatchTestWriter{short: scenario.short, err: scenario.err}
				writer := scanSpoolEntryBatchWriter{writer: destination, offset: 97}
				var err error
				for index := 0; index < count && err == nil; index++ {
					err = writer.write([scanSpoolEntrySize]byte{})
				}
				if err == nil {
					err = writer.flush()
				}
				if !errors.Is(err, scenario.want) || len(destination.writes) != 1 || writer.offset != 97 {
					t.Fatalf("failed write advanced its accepted position: offset=%d, writes=%d, error=%v", writer.offset, len(destination.writes), err)
				}
			})
		}
	}
}
