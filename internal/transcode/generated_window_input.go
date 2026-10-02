package transcode

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
)

const (
	generatedInputLineBytes = 512
	generatedInputMaxFrames = int64(1_000_000)
	generatedInputMaxIndex  = int64(1<<53 - 1)
	generatedInputMaxBase   = int64(1_000_000_000)
)

// GeneratedInputEvidence retains the exact input and encoder clocks observed
// by one output's stats_enc_pre writer. Input frame indexes and timestamps may
// repeat when CFR duplicates a decoded frame, or skip when conversion drops a
// decoded frame. They are not a declaration of source origin, source EOF,
// terminal coverage, or an independently restartable source interval.
type GeneratedInputEvidence struct {
	Rendition                  int
	Frames                     int64
	FirstInputFrame            int64
	LastInputFrame             int64
	InputTimeBaseNumerator     int64
	InputTimeBaseDenominator   int64
	FirstInputPTS              int64
	LastInputPTS               int64
	EncoderTimeBaseNumerator   int64
	EncoderTimeBaseDenominator int64
	FirstEncoderPTS            int64
	LastEncoderPTS             int64
	// These facts are accumulated across every record and cannot recover
	// after a duplicate, drop, or divergent input cadence. Endpoint arithmetic
	// alone cannot establish either fact for unobserved intermediate frames.
	SourceSequential    bool
	InputCadenceAligned bool
	// Exact cadence pairs each record with an independently observed source
	// grid; quantized diagnostic clocks cannot authorize strict publication.
	InputCadenceExact bool
}

type generatedInputWriter struct {
	line      [generatedInputLineBytes]byte
	length    int
	maxFrames int64
	frameRate int64
	step      int64
	evidence  GeneratedInputEvidence
	callback  func(GeneratedInputEvidence)
	cancel    func()
	err       error
}

func generatedInputLimits(plan Plan, rendition int) (int64, int64, error) {
	if ValidatePlan(plan) != nil || !hasHLSWindow(plan) || plan.VideoStreamIndex < 0 ||
		!VideoEncodingSupported(plan.VideoCodec) || plan.SourceMode != "" ||
		rendition < 0 || rendition >= max(1, plan.HLS.RenditionCount) ||
		plan.FrameRate < 1 || plan.FrameRate > 240 || math.Trunc(plan.FrameRate) != plan.FrameRate {
		return 0, 0, ErrInvalidPlan
	}
	rate := int64(plan.FrameRate)
	duration := plan.HLS.Window.EndTicks - plan.StartTicks
	// This is only a bounded observer budget. The padding permits ordinary
	// timestamp rounding and queued frames; it cannot prove actual source EOF
	// or make an output interval valid without independent closure evidence.
	frames := (duration*rate + ticksPerSecond - 1) / ticksPerSecond
	if frames > generatedInputMaxFrames-2*rate-2 {
		return 0, 0, ErrInvalidPlan
	}
	return frames + 2*rate + 2, rate, nil
}

func newGeneratedInputWriter(plan Plan, rendition int, callback func(GeneratedInputEvidence), cancel func()) (*generatedInputWriter, error) {
	limit, rate, err := generatedInputLimits(plan, rendition)
	if err != nil {
		return nil, err
	}
	return &generatedInputWriter{maxFrames: limit, frameRate: rate,
		evidence: GeneratedInputEvidence{Rendition: rendition}, callback: callback, cancel: cancel}, nil
}

func (writer *generatedInputWriter) Write(data []byte) (int, error) {
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

func (writer *generatedInputWriter) fail(err error) error {
	if writer.err == nil {
		writer.err = err
		if writer.cancel != nil {
			// Cancellation is best-effort external code. A panicking callback
			// must not escape an owned reader or hide the original evidence fault.
			func() {
				defer func() { _ = recover() }()
				writer.cancel()
			}()
		}
	}
	return writer.err
}

func generatedInputFields(line []byte) ([9][]byte, bool) {
	var fields [9][]byte
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

func generatedInputTimeBase(value []byte) (int64, int64, error) {
	separator := bytes.IndexByte(value, '/')
	if separator <= 0 || separator >= len(value)-1 {
		return 0, 0, ErrProgress
	}
	numerator, err := strconv.ParseInt(string(value[:separator]), 10, 64)
	if err != nil || numerator <= 0 || numerator > generatedInputMaxBase {
		return 0, 0, ErrProgress
	}
	denominator, err := strconv.ParseInt(string(value[separator+1:]), 10, 64)
	if err != nil || denominator <= 0 || denominator > generatedInputMaxBase {
		return 0, 0, ErrProgress
	}
	gcd := generatedInputGCD(numerator, denominator)
	return numerator / gcd, denominator / gcd, nil
}

func generatedInputGCD(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func generatedInputClockBounded(pts, numerator, denominator int64) bool {
	if numerator <= 0 || numerator > generatedInputMaxBase || denominator <= 0 || denominator > generatedInputMaxBase {
		return false
	}
	// Multiplying the bounded duration in whole seconds by the denominator
	// stays below 2.6e15. Division avoids overflowing on an untrusted PTS.
	limit := (maxDurationTicks / ticksPerSecond) * denominator / numerator
	return pts >= -limit && pts <= limit
}

func generatedInputEncoderStep(numerator, denominator, rate int64) (int64, bool) {
	if numerator <= 0 || numerator > generatedInputMaxBase || denominator <= 0 || denominator > generatedInputMaxBase || rate < 1 || rate > 240 {
		return 0, false
	}
	divisor := numerator * rate
	if denominator%divisor != 0 {
		return 0, false
	}
	step := denominator / divisor
	return step, step > 0
}

func generatedInputCadence(evidence GeneratedInputEvidence, encoderPTS, inputPTS int64) (aligned, exact bool) {
	// Compare both elapsed clocks over their common exact denominator. The
	// error allowance is one input time-base quantum over the whole observed
	// prefix, rather than a per-frame tolerance that could accumulate drift.
	var input, encoder, factor, quantum big.Int
	input.SetInt64(inputPTS - evidence.FirstInputPTS)
	factor.SetInt64(evidence.InputTimeBaseNumerator)
	input.Mul(&input, &factor)
	factor.SetInt64(evidence.EncoderTimeBaseDenominator)
	input.Mul(&input, &factor)
	encoder.SetInt64(encoderPTS - evidence.FirstEncoderPTS)
	factor.SetInt64(evidence.EncoderTimeBaseNumerator)
	encoder.Mul(&encoder, &factor)
	factor.SetInt64(evidence.InputTimeBaseDenominator)
	encoder.Mul(&encoder, &factor)
	input.Sub(&input, &encoder)
	input.Abs(&input)
	quantum.SetInt64(evidence.InputTimeBaseNumerator)
	factor.SetInt64(evidence.EncoderTimeBaseDenominator)
	quantum.Mul(&quantum, &factor)
	return input.Cmp(&quantum) <= 0, input.Sign() == 0
}

func generatedInputCadenceAligned(evidence GeneratedInputEvidence, encoderPTS, inputPTS int64) bool {
	aligned, _ := generatedInputCadence(evidence, encoderPTS, inputPTS)
	return aligned
}

func (writer *generatedInputWriter) consume() error {
	fields, ok := generatedInputFields(writer.line[:writer.length])
	if !ok || string(fields[0]) != "GOBY_INPUT" || writer.evidence.Frames >= writer.maxFrames || writer.evidence.Frames >= generatedInputMaxFrames {
		return ErrProgress
	}
	var values [6]int64
	for index, field := range []int{1, 2, 3, 4, 6, 8} {
		value, err := strconv.ParseInt(string(fields[field]), 10, 64)
		if err != nil {
			return ErrProgress
		}
		values[index] = value
	}
	fileIndex, streamIndex, number, inputFrame, encoderPTS, inputPTS := values[0], values[1], values[2], values[3], values[4], values[5]
	if fileIndex != int64(writer.evidence.Rendition) || streamIndex != 0 || number != writer.evidence.Frames || inputFrame < 0 || inputFrame > generatedInputMaxIndex {
		return ErrProgress
	}
	encoderNumerator, encoderDenominator, err := generatedInputTimeBase(fields[5])
	if err != nil || !generatedInputClockBounded(encoderPTS, encoderNumerator, encoderDenominator) {
		return ErrProgress
	}
	inputNumerator, inputDenominator, err := generatedInputTimeBase(fields[7])
	if err != nil || !generatedInputClockBounded(inputPTS, inputNumerator, inputDenominator) {
		return ErrProgress
	}
	evidence := writer.evidence
	if evidence.Frames == 0 {
		step, ok := generatedInputEncoderStep(encoderNumerator, encoderDenominator, writer.frameRate)
		if !ok {
			return ErrProgress
		}
		writer.step = step
		evidence.FirstInputFrame = inputFrame
		evidence.FirstInputPTS = inputPTS
		evidence.InputTimeBaseNumerator = inputNumerator
		evidence.InputTimeBaseDenominator = inputDenominator
		evidence.FirstEncoderPTS = encoderPTS
		evidence.EncoderTimeBaseNumerator = encoderNumerator
		evidence.EncoderTimeBaseDenominator = encoderDenominator
		evidence.SourceSequential = true
		evidence.InputCadenceAligned = true
		evidence.InputCadenceExact = true
	} else if evidence.InputTimeBaseNumerator != inputNumerator || evidence.InputTimeBaseDenominator != inputDenominator ||
		evidence.EncoderTimeBaseNumerator != encoderNumerator || evidence.EncoderTimeBaseDenominator != encoderDenominator ||
		inputFrame < evidence.LastInputFrame || inputPTS < evidence.LastInputPTS ||
		inputFrame == evidence.LastInputFrame && inputPTS != evidence.LastInputPTS ||
		encoderPTS-evidence.LastEncoderPTS != writer.step {
		return ErrProgress
	}
	if evidence.Frames > 0 {
		aligned, exact := generatedInputCadence(evidence, encoderPTS, inputPTS)
		evidence.SourceSequential = evidence.SourceSequential && inputFrame == evidence.LastInputFrame+1
		evidence.InputCadenceAligned = evidence.InputCadenceAligned && aligned
		evidence.InputCadenceExact = evidence.InputCadenceExact && exact
	}
	evidence.LastInputFrame = inputFrame
	evidence.LastInputPTS = inputPTS
	evidence.LastEncoderPTS = encoderPTS
	evidence.Frames++
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

func (writer *generatedInputWriter) finish() error {
	if writer.err != nil {
		return writer.err
	}
	if writer.length != 0 {
		return writer.fail(fmt.Errorf("%w: incomplete generated input record", ErrProgress))
	}
	if writer.evidence.Frames == 0 {
		return writer.fail(fmt.Errorf("%w: missing generated input evidence", ErrProgress))
	}
	return nil
}

// ValidateGeneratedInputEvidence checks the bounded representation and exact
// encoder cadence of a completed observer snapshot. It does not replace the
// per-record observer, an actual process EOF, or independent source coverage
// and media publication checks.
func ValidateGeneratedInputEvidence(plan Plan, evidence GeneratedInputEvidence) error {
	limit, rate, err := generatedInputLimits(plan, evidence.Rendition)
	if err != nil {
		return err
	}
	step, ok := generatedInputEncoderStep(evidence.EncoderTimeBaseNumerator, evidence.EncoderTimeBaseDenominator, rate)
	_, exact := generatedInputCadence(evidence, evidence.LastEncoderPTS, evidence.LastInputPTS)
	if !ok || evidence.Frames < 1 || evidence.Frames > limit || evidence.Frames > generatedInputMaxFrames ||
		evidence.FirstInputFrame < 0 || evidence.LastInputFrame < evidence.FirstInputFrame || evidence.LastInputFrame > generatedInputMaxIndex ||
		evidence.LastInputPTS < evidence.FirstInputPTS ||
		!generatedInputClockBounded(evidence.FirstInputPTS, evidence.InputTimeBaseNumerator, evidence.InputTimeBaseDenominator) ||
		!generatedInputClockBounded(evidence.LastInputPTS, evidence.InputTimeBaseNumerator, evidence.InputTimeBaseDenominator) ||
		!generatedInputClockBounded(evidence.FirstEncoderPTS, evidence.EncoderTimeBaseNumerator, evidence.EncoderTimeBaseDenominator) ||
		!generatedInputClockBounded(evidence.LastEncoderPTS, evidence.EncoderTimeBaseNumerator, evidence.EncoderTimeBaseDenominator) ||
		generatedInputGCD(evidence.InputTimeBaseNumerator, evidence.InputTimeBaseDenominator) != 1 ||
		generatedInputGCD(evidence.EncoderTimeBaseNumerator, evidence.EncoderTimeBaseDenominator) != 1 ||
		evidence.LastEncoderPTS-evidence.FirstEncoderPTS != (evidence.Frames-1)*step ||
		evidence.Frames == 1 && (evidence.FirstInputFrame != evidence.LastInputFrame || evidence.FirstInputPTS != evidence.LastInputPTS) ||
		evidence.FirstInputFrame == evidence.LastInputFrame && evidence.FirstInputPTS != evidence.LastInputPTS ||
		evidence.SourceSequential && evidence.LastInputFrame-evidence.FirstInputFrame != evidence.Frames-1 ||
		evidence.InputCadenceAligned && !generatedInputCadenceAligned(evidence, evidence.LastEncoderPTS, evidence.LastInputPTS) ||
		evidence.InputCadenceExact && (!evidence.InputCadenceAligned || !exact) {
		return ErrProgress
	}
	return nil
}
