package transcode

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

const (
	generatedAudioInputLineBytes       = 512
	generatedAudioInputMaxFrames       = int64(65_536)
	generatedAudioInputMaxSamples      = int64(12_288_000)
	generatedAudioInputMaxFrameSamples = int64(65_536)
)

// RawGeneratedAudioInputOptions bounds a diagnostic observer independently of
// generated-window eligibility. SampleRate is the caller's declared encoder
// input rate, against which clock facts are compared. The stats fields do not
// independently report that rate or prove that a decoder or filter preserved it.
type RawGeneratedAudioInputOptions struct {
	Rendition         int
	OutputStreamIndex int
	SampleRate        int64
	MaxFrames         int64
	MaxSamples        int64
}

// RawGeneratedAudioInputClock retains one exact diagnostic timestamp. TimeBase
// is seconds per clock unit, rather than a rounded presentation timestamp.
type RawGeneratedAudioInputClock struct {
	PTS      int64
	TimeBase GeneratedRational
}

// RawGeneratedAudioInputSource retains the decoder association reported by
// FFmpeg. Available requires both a frame index and a real source timestamp;
// unavailable fields retain their documented -1, 0/1 and MaxInt64 sentinels.
type RawGeneratedAudioInputSource struct {
	FrameIndex int64
	Clock      RawGeneratedAudioInputClock
	Available  bool
}

// RawGeneratedAudioInputEvidence describes every observed stats_enc_pre record.
// TotalSamples counts samples actually submitted at that observation point;
// these may include upstream padding. Neither sample counts nor source frame
// associations establish audible output bounds, preserved decoder history,
// source EOF, priming removal, independent segment restart or publication.
// A snapshot remains progress data until its owned reader reaches real EOF,
// finish succeeds, and the caller separately proves process retirement.
type RawGeneratedAudioInputEvidence struct {
	Rendition         int
	OutputStreamIndex int
	SampleRate        int64
	Frames            int64
	TotalSamples      int64
	FirstSampleNumber int64
	LastSampleNumber  int64
	FirstFrameSamples int64
	LastFrameSamples  int64
	MinFrameSamples   int64
	MaxFrameSamples   int64
	FirstEncoder      RawGeneratedAudioInputClock
	LastEncoder       RawGeneratedAudioInputClock
	FirstInput        RawGeneratedAudioInputSource
	LastInput         RawGeneratedAudioInputSource
	// These are cumulative facts. A later matching endpoint cannot erase an
	// intermediate mismatch or unavailable decoder association.
	SamplesContiguous       bool
	EncoderTimeBaseStable   bool
	EncoderSampleClockExact bool
	InputAssociationKnown   bool
	InputTimeBaseStable     bool
	SourceSequential        bool
	InputClockNondecreasing bool
	InputCadenceExact       bool
}

type generatedAudioInputWriter struct {
	options  RawGeneratedAudioInputOptions
	line     [generatedAudioInputLineBytes]byte
	length   int
	evidence RawGeneratedAudioInputEvidence
	callback func(RawGeneratedAudioInputEvidence)
	cancel   func()
	err      error
}

func validateRawGeneratedAudioInputOptions(options RawGeneratedAudioInputOptions) error {
	if options.Rendition < 0 || options.Rendition >= MaxHLSRenditions ||
		options.OutputStreamIndex < 0 || options.OutputStreamIndex > 1 ||
		options.SampleRate < 8_000 || options.SampleRate > 384_000 ||
		options.MaxFrames < 1 || options.MaxFrames > generatedAudioInputMaxFrames ||
		options.MaxSamples < 1 || options.MaxSamples > generatedAudioInputMaxSamples {
		return ErrInvalidOptions
	}
	return nil
}

func newGeneratedAudioInputWriter(options RawGeneratedAudioInputOptions, callback func(RawGeneratedAudioInputEvidence), cancel func()) (*generatedAudioInputWriter, error) {
	if err := validateRawGeneratedAudioInputOptions(options); err != nil {
		return nil, err
	}
	return &generatedAudioInputWriter{options: options, callback: callback, cancel: cancel,
		evidence: RawGeneratedAudioInputEvidence{Rendition: options.Rendition, OutputStreamIndex: options.OutputStreamIndex, SampleRate: options.SampleRate}}, nil
}

func (writer *generatedAudioInputWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	for index, char := range data {
		if char == '\n' {
			if err := writer.consume(); err != nil {
				return index + 1, writer.fail(err)
			}
			writer.length = 0
			continue
		}
		if writer.length == len(writer.line) {
			return index, writer.fail(ErrProgress)
		}
		writer.line[writer.length] = char
		writer.length++
	}
	return len(data), nil
}

func (writer *generatedAudioInputWriter) fail(err error) error {
	if writer.err == nil {
		writer.err = err
		if writer.cancel != nil {
			// Set the fault before external code. The owned-reader contract
			// handles Goexit; a panicking cancellation must preserve this fault.
			func() {
				defer func() { _ = recover() }()
				writer.cancel()
			}()
		}
	}
	return writer.err
}

func generatedAudioInputFields(line []byte) ([11][]byte, bool) {
	var fields [11][]byte
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	count := 0
	for index := 0; index < len(line); {
		if line[index] == ' ' || line[index] == '\t' {
			index++
			continue
		}
		if count == len(fields) {
			return fields, false
		}
		start := index
		for index < len(line) && line[index] != ' ' && line[index] != '\t' {
			index++
		}
		fields[count] = line[start:index]
		count++
	}
	return fields, count == len(fields)
}

func generatedAudioInputClockSeconds(clock RawGeneratedAudioInputClock) *big.Rat {
	return generatedClockSeconds(clock.PTS, clock.TimeBase.Num, clock.TimeBase.Den)
}

func generatedAudioInputElapsedExact(first, current RawGeneratedAudioInputClock, samples, rate int64) bool {
	elapsed := new(big.Rat).Sub(generatedAudioInputClockSeconds(current), generatedAudioInputClockSeconds(first))
	return elapsed.Cmp(new(big.Rat).SetFrac64(samples, rate)) == 0
}

func generatedAudioInputSource(fields [11][]byte) (RawGeneratedAudioInputSource, error) {
	var source RawGeneratedAudioInputSource
	index, err := strconv.ParseInt(string(fields[8]), 10, 64)
	if err != nil || index < -1 || index > generatedInputMaxIndex {
		return source, ErrProgress
	}
	pts, err := strconv.ParseInt(string(fields[10]), 10, 64)
	if err != nil {
		return source, ErrProgress
	}
	base := GeneratedRational{Den: 1}
	if bytes.Equal(fields[9], []byte("0/1")) {
		if pts != math.MaxInt64 {
			return source, ErrProgress
		}
	} else {
		base.Num, base.Den, err = generatedInputTimeBase(fields[9])
		if err != nil || !generatedInputClockBounded(pts, base.Num, base.Den) {
			return source, ErrProgress
		}
	}
	source.FrameIndex = index
	source.Clock = RawGeneratedAudioInputClock{PTS: pts, TimeBase: base}
	source.Available = index >= 0 && base.Num > 0
	return source, nil
}

func (writer *generatedAudioInputWriter) consume() error {
	fields, ok := generatedAudioInputFields(writer.line[:writer.length])
	if !ok || string(fields[0]) != "GOBY_AUDIO" || writer.evidence.Frames >= writer.options.MaxFrames {
		return ErrProgress
	}
	var values [6]int64
	for index, field := range []int{1, 2, 3, 4, 5, 7} {
		value, err := strconv.ParseInt(string(fields[field]), 10, 64)
		if err != nil {
			return ErrProgress
		}
		values[index] = value
	}
	file, stream, number, samplesBefore, samples, pts := values[0], values[1], values[2], values[3], values[4], values[5]
	if file != int64(writer.options.Rendition) || stream != int64(writer.options.OutputStreamIndex) || number != writer.evidence.Frames ||
		samplesBefore != writer.evidence.TotalSamples || samples < 1 || samples > generatedAudioInputMaxFrameSamples ||
		samples > writer.options.MaxSamples-writer.evidence.TotalSamples {
		return ErrProgress
	}
	baseNum, baseDen, err := generatedInputTimeBase(fields[6])
	if err != nil || !generatedInputClockBounded(pts, baseNum, baseDen) {
		return ErrProgress
	}
	encoder := RawGeneratedAudioInputClock{PTS: pts, TimeBase: GeneratedRational{Num: baseNum, Den: baseDen}}
	source, err := generatedAudioInputSource(fields)
	if err != nil {
		return err
	}
	evidence := writer.evidence
	if evidence.Frames == 0 {
		evidence.FirstSampleNumber, evidence.FirstFrameSamples = samplesBefore, samples
		evidence.MinFrameSamples, evidence.MaxFrameSamples = samples, samples
		evidence.FirstEncoder, evidence.FirstInput = encoder, source
		evidence.SamplesContiguous = true
		evidence.EncoderTimeBaseStable, evidence.EncoderSampleClockExact = true, true
		evidence.InputAssociationKnown, evidence.InputTimeBaseStable = source.Available, source.Available
		evidence.SourceSequential, evidence.InputClockNondecreasing, evidence.InputCadenceExact = source.Available, source.Available, source.Available
	} else {
		evidence.EncoderTimeBaseStable = evidence.EncoderTimeBaseStable && encoder.TimeBase == evidence.FirstEncoder.TimeBase
		evidence.EncoderSampleClockExact = evidence.EncoderSampleClockExact &&
			generatedAudioInputElapsedExact(evidence.FirstEncoder, encoder, samplesBefore, evidence.SampleRate)
		evidence.InputAssociationKnown = evidence.InputAssociationKnown && source.Available
		evidence.InputTimeBaseStable = evidence.InputTimeBaseStable && source.Available && source.Clock.TimeBase == evidence.FirstInput.Clock.TimeBase
		evidence.SourceSequential = evidence.SourceSequential && source.Available && evidence.LastInput.Available && source.FrameIndex == evidence.LastInput.FrameIndex+1
		if source.Available && evidence.LastInput.Available {
			evidence.InputClockNondecreasing = evidence.InputClockNondecreasing &&
				generatedAudioInputClockSeconds(source.Clock).Cmp(generatedAudioInputClockSeconds(evidence.LastInput.Clock)) >= 0
		} else {
			evidence.InputClockNondecreasing = false
		}
		evidence.InputCadenceExact = evidence.InputCadenceExact && source.Available && evidence.FirstInput.Available &&
			generatedAudioInputElapsedExact(evidence.FirstInput.Clock, source.Clock, samplesBefore, evidence.SampleRate)
	}
	evidence.LastSampleNumber, evidence.LastFrameSamples = samplesBefore, samples
	evidence.MinFrameSamples, evidence.MaxFrameSamples = min(evidence.MinFrameSamples, samples), max(evidence.MaxFrameSamples, samples)
	evidence.LastEncoder, evidence.LastInput = encoder, source
	evidence.Frames++
	evidence.TotalSamples += samples
	writer.evidence = evidence
	if writer.callback != nil {
		callbackErr := func() (err error) {
			completed := false
			defer func() {
				_ = recover()
				if !completed {
					err = ErrProgress
				}
			}()
			writer.callback(evidence)
			completed = true
			return nil
		}()
		if callbackErr != nil {
			return callbackErr
		}
	}
	return nil
}

func (writer *generatedAudioInputWriter) finish() error {
	if writer.err != nil {
		return writer.err
	}
	if writer.length != 0 {
		return writer.fail(fmt.Errorf("%w: incomplete generated audio input record", ErrProgress))
	}
	if writer.evidence.Frames == 0 {
		return writer.fail(fmt.Errorf("%w: missing generated audio input evidence", ErrProgress))
	}
	return nil
}
