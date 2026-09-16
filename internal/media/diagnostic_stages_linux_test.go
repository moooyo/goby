package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// These tests simulate the command boundary with source-derived grammar and
// fixed sample bytes. They test orchestration only, never an installed codec,
// hardware device, real process, or media-acceptance result.
type diagnosticFakeStageSession struct {
	t       *testing.T
	ctx     context.Context
	calls   int
	plans   []DiagnosticPlan
	failAt  int
	mutate  func(int, *diagnosticCommandObservation)
	cancel  context.CancelFunc
	blockAt int
	entered chan struct{}
}

func (s *diagnosticFakeStageSession) contextErr() error { return s.ctx.Err() }
func (s *diagnosticFakeStageSession) interrupt() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *diagnosticFakeStageSession) observation(data []byte) diagnosticCommandObservation {
	return diagnosticCommandObservation{Started: true, ExitCode: 0, Elapsed: time.Millisecond,
		Stdout: data, ToolSHA256: strings.Repeat("1", 64), EnvironmentSHA256: strings.Repeat("2", 64), ProcessesClosed: true}
}

func (s *diagnosticFakeStageSession) version() (diagnosticCommandObservation, error) {
	s.calls++
	return s.observation([]byte("ffmpeg version 9.0.1 Copyright (c) FFmpeg developers\n")), nil
}

func (s *diagnosticFakeStageSession) run(plan DiagnosticPlan, input *os.File, inputHash string) (diagnosticCommandObservation, error) {
	s.t.Helper()
	if _, err := diagnosticCheckInput(input, plan, inputHash); err != nil {
		s.t.Fatal(err)
	}
	s.calls++
	s.plans = append(s.plans, plan)
	if s.calls == s.blockAt {
		close(s.entered)
		<-s.ctx.Done()
		return s.observation(nil), s.ctx.Err()
	}
	var data []byte
	log := ""
	if plan.Input.Width != 0 {
		decoder, encoder, pixel, decodePixel := "h264", "libx264", "yuv420p", "yuv420p"
		if plan.ActiveDecodeBackend == "qsv" {
			decoder = "h264_qsv"
		}
		if plan.ActiveEncodeBackend != "software" && plan.ActiveEncodeBackend != "none" {
			encoder, pixel = "h264_"+plan.ActiveEncodeBackend, plan.ActiveEncodeBackend
		}
		if plan.ActiveEncodeBackend == "nvenc" {
			pixel = "cuda"
		}
		if plan.ActiveDecodeBackend != "software" && plan.ActiveDecodeBackend != "none" {
			decodePixel = plan.ActiveDecodeBackend
		}
		if plan.Mode == DiagnosticEncode {
			decoder, decodePixel = "rawvideo", ""
		}
		if plan.Mode == DiagnosticDecode {
			encoder, pixel = "rawvideo", "yuv420p"
			raw, err := GenerateDiagnosticSample(DiagnosticRawVideo)
			if err != nil {
				s.t.Fatal(err)
			}
			data = raw.Data
		} else {
			data = []byte{0, 0, 1, 0x67, 1}
			if plan.Mode == DiagnosticCombined {
				data[4] = 2
			}
		}
		log = diagnosticEvidenceVideoLog(plan.Mode, decoder, encoder, pixel, decodePixel)
	} else {
		if plan.Mode == DiagnosticDecode {
			marker := make([]byte, 8)
			if _, err := input.ReadAt(marker, 0); err != nil {
				s.t.Fatal(err)
			}
			delay := 1024
			if marker[7] == 2 {
				delay = 2048
			}
			raw, err := GenerateDiagnosticSample(DiagnosticRawAudio)
			if err != nil {
				s.t.Fatal(err)
			}
			data = append(make([]byte, delay*4), raw.Data...)
			data = append(data, make([]byte, 1024*4)...)
		} else {
			marker, packets := byte(1), 10
			if plan.Mode == DiagnosticCombined {
				marker, packets = 2, 11
			}
			packet := diagnosticADTSTestPacket(s.t, []byte{marker})
			data = bytes.Repeat(packet, packets)
		}
		log = diagnosticEvidenceAudioLog(plan.Mode)
	}
	observation := s.observation(data)
	observation.Stderr, observation.InputSHA256 = []byte(log), inputHash
	if s.mutate != nil {
		s.mutate(s.calls, &observation)
	}
	if s.calls == s.failAt {
		observation.ExitCode = 1
		return observation, ErrDiagnosticExecution
	}
	return observation, nil
}

func TestDiagnosticStagesSeparatePreparedReferencesFromSeventeenSoftwareCommands(t *testing.T) {
	session := &diagnosticFakeStageSession{t: t, ctx: context.Background()}
	report, err := executeDiagnosticStages(session.ctx, session, DiagnosticSelection{}, nil)
	if err != nil || report.State != "stages_complete" || !report.SessionClosureRequired || session.calls != 17 || report.CommandsStarted != 17 || len(report.Stages) != 6 {
		t.Fatalf("software control flow incomplete: %s, %v, %d calls", report.State, err, session.calls)
	}
	for _, stage := range report.Stages {
		if stage.State != "passed" || stage.Command == nil || stage.Command.Evidence == nil {
			t.Fatal("formal stage missing independent command evidence")
		}
	}
	first, second := report.Preparations[1], report.Preparations[2]
	if first.Audio.SampleFrames != 10240 || second.Audio.SampleFrames != 11264 || first.EncodedSHA256 == second.EncodedSHA256 || second.ParentSHA256 != first.EncodedSHA256 {
		t.Fatal("AAC generation evidence was guessed or conflated")
	}
	if session.plans[4].Mode != DiagnosticCombined || session.plans[4].Input.Kind != DiagnosticAAC {
		t.Fatal("second reference bypassed the real combined input contract")
	}
	for index, generation := range []int{1, 1, 2} {
		stage := report.Stages[3+index]
		if stage.ReferenceGeneration != generation || stage.DecodedBytes != report.Preparations[generation].DecodedBytes {
			t.Fatal("stage used the wrong AAC reference")
		}
	}
}

func TestDiagnosticStagesConfiguredVideoUsesTwentyTwoCommands(t *testing.T) {
	session := &diagnosticFakeStageSession{t: t, ctx: context.Background()}
	selection := DiagnosticSelection{IncludeConfiguredHardware: true, ConfiguredProfile: DiagnosticProfile{Decode: "cuda", Encode: "nvenc"}}
	report, err := executeDiagnosticStages(session.ctx, session, selection, nil)
	if err != nil || report.State != "stages_complete" || session.calls != 22 || len(report.Stages) != 9 {
		t.Fatalf("configured graph: %s, %v, %d", report.State, err, session.calls)
	}
	for _, index := range []int{6, 8} {
		if report.Stages[index].Command.Evidence.HardwarePixelFormat != "cuda" {
			t.Fatal("configured decode lost actual hardware evidence")
		}
	}
}

func TestDiagnosticStagesCancellationPreservesCompletedStageWithoutFurtherDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &diagnosticFakeStageSession{t: t, ctx: ctx}
	report, err := executeDiagnosticStages(ctx, session, DiagnosticSelection{}, func(progress DiagnosticReport) {
		if progress.Stages[0].State == "passed" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || report.State != "cancelled" || session.calls != 8 || report.Stages[0].State != "passed" {
		t.Fatal("cancellation discarded completed facts or dispatched new work")
	}
	for _, stage := range report.Stages[1:] {
		if stage.State != "not_run" || stage.Code != "diagnostic_cancelled" {
			t.Fatal("unattempted stage was presented as an execution")
		}
	}
}

func TestDiagnosticStagesUnclosedOrChangedCommandCannotPass(t *testing.T) {
	for _, mode := range []string{"closure", "tool", "memory"} {
		t.Run(mode, func(t *testing.T) {
			session := &diagnosticFakeStageSession{t: t, ctx: context.Background(), mutate: func(call int, observation *diagnosticCommandObservation) {
				if call != 8 {
					return
				}
				switch mode {
				case "closure":
					observation.ProcessesClosed = false
				case "tool":
					observation.ToolSHA256 = strings.Repeat("3", 64)
				case "memory":
					observation.MemoryOOMKills = 1
				}
			}}
			report, err := executeDiagnosticStages(session.ctx, session, DiagnosticSelection{}, nil)
			if err == nil || session.calls != 8 || report.Stages[0].State == "passed" {
				t.Fatal("broken process evidence was accepted or continued")
			}
		})
	}
}

func TestDiagnosticStagesCodecFailureDoesNotEraseOrBlockIndependentWork(t *testing.T) {
	session := &diagnosticFakeStageSession{t: t, ctx: context.Background(), failAt: 9}
	report, err := executeDiagnosticStages(session.ctx, session, DiagnosticSelection{}, nil)
	if err == nil || report.State != "incomplete" || report.Stages[0].State != "passed" || report.Stages[1].State != "failed" || report.Stages[2].State != "passed" || report.Stages[5].State != "passed" || session.calls != 16 {
		t.Fatal("codec failure hid independent stage outcomes or retried failed input")
	}
}

func TestDiagnosticStagesRequireActualHardwareEvidenceAndDecodedContent(t *testing.T) {
	for _, mode := range []string{"hardware", "content", "audio_reference"} {
		t.Run(mode, func(t *testing.T) {
			session := &diagnosticFakeStageSession{t: t, ctx: context.Background(), mutate: func(call int, observation *diagnosticCommandObservation) {
				switch {
				case mode == "hardware" && call == 18:
					observation.Stderr = bytes.ReplaceAll(observation.Stderr, []byte("[h264 @ 0x2222] [debug] Format cuda chosen by get_format().\n"), nil)
				case mode == "content" && call == 10:
					observation.Stdout = make([]byte, DiagnosticVideoBytes)
				case mode == "audio_reference" && call == 17:
					observation.Stdout = observation.Stdout[1024*4:]
				}
			}}
			selection := DiagnosticSelection{IncludeConfiguredHardware: mode == "hardware"}
			if selection.IncludeConfiguredHardware {
				selection.ConfiguredProfile = DiagnosticProfile{Decode: "cuda", Encode: "nvenc"}
			}
			report, err := executeDiagnosticStages(session.ctx, session, selection, nil)
			index := map[string]int{"hardware": 6, "content": 1, "audio_reference": 5}[mode]
			if err == nil || report.Stages[index].State != "unverified" {
				t.Fatal("configuration, zero output, or wrong-generation AAC was treated as verified")
			}
		})
	}
}

func TestDiagnosticStageSnapshotsCannotMutatePrivateReferences(t *testing.T) {
	session := &diagnosticFakeStageSession{t: t, ctx: context.Background()}
	report, err := executeDiagnosticStages(session.ctx, session, DiagnosticSelection{}, func(snapshot DiagnosticReport) {
		for index := range snapshot.Preparations {
			preparation := &snapshot.Preparations[index]
			preparation.State = "observer-mutated"
			if preparation.Audio != nil {
				preparation.Audio.SampleFrames = 1
			}
			if preparation.Decode != nil && preparation.Decode.Evidence != nil {
				preparation.Decode.Evidence.Encoder = "observer-mutated"
			}
		}
		for index := range snapshot.Stages {
			if snapshot.Stages[index].Video != nil {
				snapshot.Stages[index].Video.FrameMetrics[0].BestReferenceFrame = 99
			}
		}
	})
	if err != nil || report.State != "stages_complete" || report.Stages[0].Video.FrameMetrics[0].BestReferenceFrame != 0 {
		t.Fatal("progress callback changed private acceptance evidence")
	}
}

func TestDiagnosticStagesRejectInvalidSelectionBeforeAnyCommand(t *testing.T) {
	for _, selection := range []DiagnosticSelection{
		{IncludeConfiguredHardware: true}, {ConfiguredProfile: DiagnosticProfile{Decode: "cuda"}},
		{IncludeConfiguredHardware: true, ConfiguredProfile: DiagnosticProfile{Decode: "cuda", Encode: "vaapi"}},
	} {
		session := &diagnosticFakeStageSession{t: t, ctx: context.Background()}
		report, err := executeDiagnosticStages(session.ctx, session, selection, nil)
		if !errors.Is(err, ErrDiagnosticPlan) || session.calls != 0 || report.State != "unavailable" {
			t.Fatal("invalid selection reached command dispatch")
		}
	}
}

func TestDiagnosticStagesCancellationInterruptsAnInFlightDifferentParentSession(t *testing.T) {
	graphCtx, cancelGraph := context.WithCancel(context.Background())
	defer cancelGraph()
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	defer cancelSession()
	session := &diagnosticFakeStageSession{t: t, ctx: sessionCtx, cancel: cancelSession, blockAt: 8, entered: make(chan struct{})}
	type result struct {
		report DiagnosticReport
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := executeDiagnosticStages(graphCtx, session, DiagnosticSelection{}, nil)
		done <- result{report: report, err: err}
	}()
	select {
	case <-session.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("synthetic stage did not enter")
	}
	cancelGraph()
	select {
	case actual := <-done:
		if !errors.Is(actual.err, context.Canceled) || actual.report.State != "cancelled" || actual.report.CommandsAttempted != 8 {
			t.Fatal("in-flight cancellation lost its result or dispatched another stage")
		}
	case <-time.After(time.Second):
		t.Fatal("graph cancellation did not interrupt the session's active command")
	}
}
