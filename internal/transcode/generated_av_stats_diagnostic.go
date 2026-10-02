package transcode

import (
	"bytes"
	"strconv"
)

// This fixed format uses only documented packet directives. A pre-mux packet
// duration is not supplied by these directives and is never fabricated here.
// https://ffmpeg.org/ffmpeg.html#stats_005fenc_005foptions
const generatedAVPreMuxFormat = "GOBY_AV_MUX {fidx} {sidx} {n} {tb} {pts} {dts} {size} {key}"

type GeneratedAVPreMuxPacket struct {
	Number          int64
	TimeBase        GeneratedRational
	PTS, DTS, Bytes int64
	Key             bool
}

// GeneratedAVPreMuxDiagnostic retains the complete actual packet record set.
// ReaderEOF and RecordsParsed are separate from process retirement, source
// identity, native container emission, decoded samples and qualification.
type GeneratedAVPreMuxDiagnostic struct {
	Qualified                bool
	Rendition, StreamIndex   int
	RecordsParsed, ReaderEOF bool
	Packets                  []GeneratedAVPreMuxPacket
}

type generatedAVPreMuxWriter struct {
	line   [512]byte
	length int
	result GeneratedAVPreMuxDiagnostic
	err    error
	cancel func()
}

func newGeneratedAVPreMuxWriter(rendition, stream int, cancel func()) (*generatedAVPreMuxWriter, error) {
	if rendition < 0 || rendition >= MaxHLSRenditions || stream < 0 || stream > 1 {
		return nil, ErrInvalidOptions
	}
	return &generatedAVPreMuxWriter{result: GeneratedAVPreMuxDiagnostic{Rendition: rendition, StreamIndex: stream}, cancel: cancel}, nil
}

func (writer *generatedAVPreMuxWriter) fail(err error) error {
	if writer.err == nil {
		writer.err = err
		if writer.cancel != nil {
			func() { defer func() { _ = recover() }(); writer.cancel() }()
		}
	}
	return writer.err
}

func (writer *generatedAVPreMuxWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	for index, value := range data {
		if value == '\n' {
			if err := writer.consume(); err != nil {
				return index + 1, writer.fail(err)
			}
			writer.length = 0
			continue
		}
		if writer.length == len(writer.line) {
			return index, writer.fail(ErrProgress)
		}
		writer.line[writer.length] = value
		writer.length++
	}
	return len(data), nil
}

func (writer *generatedAVPreMuxWriter) consume() error {
	fields := bytes.Fields(writer.line[:writer.length])
	if len(fields) != 9 || string(fields[0]) != "GOBY_AV_MUX" || len(writer.result.Packets) >= generatedAVTransportRecords {
		return ErrProgress
	}
	var values [6]int64
	for index, field := range []int{1, 2, 3, 5, 6, 7} {
		value, err := strconv.ParseInt(string(fields[field]), 10, 64)
		if err != nil {
			return ErrProgress
		}
		values[index] = value
	}
	if values[0] != int64(writer.result.Rendition) || values[1] != int64(writer.result.StreamIndex) || values[2] != int64(len(writer.result.Packets)) || values[5] < 1 || values[5] > generatedAVTransportPESBytes {
		return ErrProgress
	}
	num, den, err := generatedInputTimeBase(fields[4])
	if err != nil || !generatedInputClockBounded(values[3], num, den) || !generatedInputClockBounded(values[4], num, den) {
		return ErrProgress
	}
	if string(fields[8]) != "K" && string(fields[8]) != "N" {
		return ErrProgress
	}
	writer.result.Packets = append(writer.result.Packets, GeneratedAVPreMuxPacket{Number: values[2], TimeBase: GeneratedRational{Num: num, Den: den}, PTS: values[3], DTS: values[4], Bytes: values[5], Key: string(fields[8]) == "K"})
	return nil
}

func (writer *generatedAVPreMuxWriter) finish() (GeneratedAVPreMuxDiagnostic, error) {
	if writer.err != nil {
		return GeneratedAVPreMuxDiagnostic{}, writer.err
	}
	if writer.length != 0 || len(writer.result.Packets) == 0 {
		return GeneratedAVPreMuxDiagnostic{}, writer.fail(ErrProgress)
	}
	writer.result.RecordsParsed = true
	return writer.result, nil
}
