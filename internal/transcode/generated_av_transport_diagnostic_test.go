package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func generatedAVTransportTestSection(data []byte) []byte {
	data = append(bytes.Clone(data), make([]byte, 4)...)
	length := len(data) - 3
	data[1], data[2] = data[1]|byte(length>>8), byte(length)
	binary.BigEndian.PutUint32(data[len(data)-4:], generatedAVPSICRC(data[:len(data)-4]))
	return data
}

func generatedAVTransportTestPacket(pid uint16, start bool, cc byte, payload []byte) []byte {
	packet := bytes.Repeat([]byte{0xff}, 188)
	packet[0], packet[1], packet[2] = 0x47, byte(pid>>8), byte(pid)
	if start {
		packet[1] |= 0x40
	}
	packet[3] = 0x30 | cc
	adaptation := 183 - len(payload)
	packet[4] = byte(adaptation)
	if adaptation > 0 {
		packet[5] = 0
	}
	copy(packet[5+adaptation:], payload)
	return packet
}

func generatedAVTransportTestClock(prefix byte, value uint64) []byte {
	return []byte{prefix<<4 | byte(value>>29)&0x0e | 1, byte(value >> 22), byte(value>>14)&0xfe | 1, byte(value >> 7), byte(value<<1) | 1}
}

func generatedAVTransportTestPES(stream byte, pts uint64, dts *uint64, payload []byte, zeroLength bool) []byte {
	flags, header := byte(0x80), generatedAVTransportTestClock(2, pts)
	if dts != nil {
		flags = 0xc0
		header = append(generatedAVTransportTestClock(3, pts), generatedAVTransportTestClock(1, *dts)...)
	}
	data := []byte{0, 0, 1, stream, 0, 0, 0x80, flags, byte(len(header))}
	data = append(append(data, header...), payload...)
	if !zeroLength {
		binary.BigEndian.PutUint16(data[4:6], uint16(len(data)-6))
	}
	return data
}

func generatedAVTransportTestADTS(channels int) []byte {
	data := []byte{0xff, 0xf1, 0x4c, byte(channels << 6), 1, 0x1f, 0xfc, 0x55}
	return data
}

func generatedAVTransportTestFixture() []byte {
	pat := generatedAVTransportTestSection([]byte{0, 0xb0, 0, 0x12, 0x34, 0xc1, 0, 0, 0, 1, 0xf0, 0})
	pmt := generatedAVTransportTestSection([]byte{2, 0xb0, 0, 0, 1, 0xc1, 0, 0, 0xe1, 0, 0xf0, 0, 0x1b, 0xe1, 0, 0xf0, 0, 0x0f, 0xe1, 1, 0xf0, 0})
	data := generatedAVTransportTestPacket(0, true, 0, append([]byte{0}, pat...))
	data = append(data, generatedAVTransportTestPacket(0x1000, true, 0, append([]byte{0}, pmt...))...)
	video := generatedAVTransportTestPES(0xe0, (1<<33)-2, nil, []byte{0, 0, 1, 0x65, 0x88}, true)
	data = append(data, generatedAVTransportTestPacket(0x100, true, 0, video)...)
	dts := uint64(90000)
	audio := generatedAVTransportTestPES(0xc0, 90000, &dts, append(generatedAVTransportTestADTS(2), generatedAVTransportTestADTS(2)...), false)
	return append(data, generatedAVTransportTestPacket(0x101, true, 0, audio)...)
}

func TestGeneratedAVTransportDiagnosticKeepsRawClocksAndDerivedAssociations(t *testing.T) {
	data := generatedAVTransportTestFixture()
	got, err := ParseGeneratedAVTransportDiagnostic(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Qualified || !got.Complete || got.Program != 1 || got.VideoPID != 0x100 || got.AudioPID != 0x101 ||
		got.DeclaredAudioSamples != 2048 || len(got.PES) != 2 || len(got.ADTS) != 2 || got.SHA256 == ([32]byte{}) {
		t.Fatalf("lost complete structural observation: %+v", got)
	}
	if got.PES[0].PTS33 != (1<<33)-2 || !got.PES[0].PTSKnown || got.PES[0].DTSKnown || got.PES[0].Boundary != "held_file_end" ||
		got.PES[0].DeclaredLength != 0 || got.PES[1].DTS33 != 90000 || !got.PES[1].DTSKnown {
		t.Fatal("raw timestamps were repaired, unwrapped or dropped")
	}
	for index, unit := range got.ADTS {
		if unit.PESIndex != 1 || unit.IndexInPES != index || !unit.Derived || unit.PESPTS33 != 90000 ||
			unit.DerivedSampleOffset != int64(index)*1024 || unit.BlockSamples != 1024 || unit.Bytes != 8 || unit.SHA256 == ([32]byte{}) {
			t.Fatal("PES association became an unlabelled native AU clock")
		}
	}
}

func TestGeneratedAVTransportDiagnosticRejectsMissingOrMalformedEvidence(t *testing.T) {
	for name, mutate := range map[string]func([]byte) []byte{
		"sync":                  func(data []byte) []byte { data[0] = 0; return data },
		"PSI CRC":               func(data []byte) []byte { data[187] ^= 1; return data },
		"unknown PID":           func(data []byte) []byte { data[2*188+2] = 2; return data },
		"PES missing PTS":       func(data []byte) []byte { data[3*188+188-35+7] = 0; return data },
		"audio declared length": func(data []byte) []byte { data[len(data)-35+5]++; return data },
		"ADTS rate":             func(data []byte) []byte { data[len(data)-16+2] ^= 4; return data },
		"ADTS partial frame": func(data []byte) []byte {
			// Two original eight-byte units leave sixteen payload bytes.
			// Declare twenty-four bytes so the first unit exceeds the held PES;
			// sixteen bytes would form one structurally complete unit instead.
			data[len(data)-16+4] = 3
			return data
		},
		"transport truncation": func(data []byte) []byte { return data[:len(data)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			data := mutate(generatedAVTransportTestFixture())
			got, err := ParseGeneratedAVTransportDiagnostic(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err == nil || got.Complete || got.Qualified || len(got.PES) != 0 || len(got.ADTS) != 0 {
				t.Fatalf("invalid bytes returned evidence: %+v %v", got, err)
			}
		})
	}
}

type generatedAVTransportShortReader struct{ io.ReaderAt }

func (reader generatedAVTransportShortReader) ReadAt(data []byte, offset int64) (int, error) {
	n, err := reader.ReaderAt.ReadAt(data, offset)
	if n > 0 {
		n--
	}
	return n, err
}

func TestGeneratedAVTransportDiagnosticWholeExtentAndBudgets(t *testing.T) {
	data := generatedAVTransportTestFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseGeneratedAVTransportDiagnostic(ctx, bytes.NewReader(data), int64(len(data))); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not invalidate observation")
	}
	if _, err := ParseGeneratedAVTransportDiagnostic(context.Background(), generatedAVTransportShortReader{bytes.NewReader(data)}, int64(len(data))); err == nil {
		t.Fatal("short ReaderAt count became complete evidence")
	}
	size := int64(generatedAVTransportBytes/188+1) * 188
	if _, err := ParseGeneratedAVTransportDiagnostic(context.Background(), bytes.NewReader(data), size); !errors.Is(err, ErrTimelineLimit) {
		t.Fatal("explicit extent exceeded budget")
	}
}
