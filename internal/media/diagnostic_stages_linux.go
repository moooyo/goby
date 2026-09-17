package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"
)

// Production uses diagnosticProcessSession; the interface permits finite
// control-flow tests without calling FFmpeg. It is not a pluggable run API.
type diagnosticStageSession interface {
	run(DiagnosticPlan, *os.File, string) (diagnosticCommandObservation, error)
	version() (diagnosticCommandObservation, error)
	contextErr() error
	interrupt()
}

func (s *diagnosticProcessSession) contextErr() error { return s.ctx.Err() }
func (s *diagnosticProcessSession) interrupt()        { s.cancel() }

type diagnosticStageInput struct {
	file *os.File
	kind DiagnosticSampleKind
	hash string
}

type diagnosticReference struct {
	input   diagnosticStageInput
	decoded []byte
	facts   DiagnosticPreparation
}

type diagnosticStageGraph struct {
	ctx           context.Context
	session       diagnosticStageSession
	report        DiagnosticReport
	emit          func(DiagnosticReport)
	inputs        []*os.File
	rawVideo      diagnosticStageInput
	rawAudio      diagnosticStageInput
	video         *diagnosticReference
	audio         *diagnosticReference
	audioCombined *diagnosticReference
	fatal         error
}

// The owner creates one session after current administrator and conversion
// admission, retains it throughout this call, then closes it before finalizing
// the run. This pipeline never discards that ownership or claims final closure.
// emit must synchronously store the bounded snapshot without blocking on I/O.
func executeDiagnosticStages(ctx context.Context, session diagnosticStageSession, selection DiagnosticSelection, emit func(DiagnosticReport)) (report DiagnosticReport, resultErr error) {
	report = diagnosticInitialReport(selection)
	report.SessionClosureRequired = session != nil
	if err := validateDiagnosticSelection(selection); err != nil {
		report.State, report.Code = "unavailable", "diagnostic_profile_unavailable"
		report.FinishedAt = time.Now().UTC()
		return report, err
	}
	if session == nil {
		report.State, report.Code, report.FinishedAt = "unavailable", "diagnostic_resources_unavailable", time.Now().UTC()
		return report, ErrDiagnosticResources
	}
	budget, cancel := context.WithTimeout(ctx, diagnosticRunDeadline)
	defer cancel()
	// The caller's cancellation and this graph's total budget must interrupt
	// the command already in flight, even if the session has a different parent.
	stopInterrupt := context.AfterFunc(budget, session.interrupt)
	defer stopInterrupt()
	g := &diagnosticStageGraph{ctx: budget, session: session, report: report, emit: emit}
	defer func() {
		for _, input := range g.inputs {
			if err := input.Close(); err != nil {
				g.fatal = ErrDiagnosticClosure
			}
		}
		g.finish()
		report = g.snapshot()
		if g.fatal != nil {
			resultErr = g.fatal
		} else if report.State != "stages_complete" {
			resultErr = ErrDiagnosticExecution
		}
	}()
	g.publish()
	if err := g.interrupted(); err != nil {
		g.fatal = err
		return DiagnosticReport{}, nil
	}
	observation, err := session.version()
	fact := g.fact(observation)
	g.report.VersionCommand = fact
	if err == nil {
		err = diagnosticSuccessfulCommand(observation)
	}
	if err == nil {
		g.report.ToolVersion, err = diagnosticVersion(observation.Stdout)
	}
	if err != nil {
		g.fatal = err
		g.report.Code = diagnosticStageCode(err)
		return DiagnosticReport{}, nil
	}
	g.report.ToolSHA256, g.report.EnvironmentSHA256 = fact.ToolSHA256, fact.EnvironmentSHA256
	g.rawVideo, err = g.rawInput(DiagnosticRawVideo)
	if err == nil {
		g.rawAudio, err = g.rawInput(DiagnosticRawAudio)
	}
	if err != nil {
		g.fatal = err
		return DiagnosticReport{}, nil
	}
	g.video = g.prepare(0, g.rawVideo, DiagnosticEncode)
	g.audio = g.prepare(1, g.rawAudio, DiagnosticEncode)
	if g.audio != nil {
		g.audioCombined = g.prepare(2, g.audio.input, DiagnosticCombined)
	} else {
		g.report.Preparations[2].Code = "diagnostic_reference_unavailable"
	}
	for index := range g.report.Stages {
		g.stage(index)
	}
	return DiagnosticReport{}, nil
}

func validateDiagnosticSelection(selection DiagnosticSelection) error {
	if !selection.IncludeConfiguredHardware {
		if selection.ConfiguredProfile != (DiagnosticProfile{}) {
			return ErrDiagnosticPlan
		}
		return nil
	}
	decode, encode, err := diagnosticProfile(selection.ConfiguredProfile)
	if err != nil || decode == "software" && encode == "software" {
		return ErrDiagnosticPlan
	}
	return nil
}

func diagnosticInitialReport(selection DiagnosticSelection) DiagnosticReport {
	r := DiagnosticReport{Version: 1, Selection: selection, StartedAt: time.Now().UTC(), State: "preparing", SessionClosureRequired: true,
		Preparations: []DiagnosticPreparation{{ID: "video-generation-1", Media: "video", Generation: 1, State: "not_run"},
			{ID: "audio-generation-1", Media: "audio", Generation: 1, State: "not_run"},
			{ID: "audio-generation-2", Media: "audio", Generation: 2, State: "not_run"}}}
	for _, media := range []string{"video", "audio"} {
		for _, mode := range []DiagnosticMode{DiagnosticDecode, DiagnosticEncode, DiagnosticCombined} {
			r.Stages = append(r.Stages, DiagnosticStage{ID: "software-" + media + "-" + string(mode), Media: media, Path: "software", Mode: mode, State: "not_run"})
		}
	}
	if selection.IncludeConfiguredHardware {
		for _, mode := range []DiagnosticMode{DiagnosticDecode, DiagnosticEncode, DiagnosticCombined} {
			r.Stages = append(r.Stages, DiagnosticStage{ID: "configured-video-" + string(mode), Media: "video", Path: "configured",
				Mode: mode, Profile: selection.ConfiguredProfile, State: "not_run"})
		}
	}
	return r
}

func (g *diagnosticStageGraph) interrupted() error {
	if g.fatal != nil {
		return g.fatal
	}
	if err := g.ctx.Err(); err != nil {
		return err
	}
	return g.session.contextErr()
}

func (g *diagnosticStageGraph) input(kind DiagnosticSampleKind, data []byte) (diagnosticStageInput, error) {
	file, err := newDiagnosticInput(data)
	if err != nil {
		return diagnosticStageInput{}, err
	}
	g.inputs = append(g.inputs, file)
	return diagnosticStageInput{file: file, kind: kind, hash: diagnosticDigest(data)}, nil
}

func (g *diagnosticStageGraph) rawInput(kind DiagnosticSampleKind) (diagnosticStageInput, error) {
	raw, err := GenerateDiagnosticSample(kind)
	if err != nil {
		return diagnosticStageInput{}, err
	}
	return g.input(kind, raw.Data)
}

func (g *diagnosticStageGraph) command(mode DiagnosticMode, input diagnosticStageInput, profile DiagnosticProfile) (diagnosticCommandObservation, *DiagnosticCommandFact, error) {
	if err := g.interrupted(); err != nil {
		return diagnosticCommandObservation{}, nil, err
	}
	plan, err := BuildDiagnosticPlan(mode, input.kind, profile)
	if err != nil {
		return diagnosticCommandObservation{}, nil, err
	}
	observation, err := g.session.run(plan, input.file, input.hash)
	fact := g.fact(observation)
	if err == nil {
		err = diagnosticSuccessfulCommand(observation)
	}
	if err == nil && (observation.ToolSHA256 != g.report.ToolSHA256 || observation.EnvironmentSHA256 != g.report.EnvironmentSHA256 || observation.InputSHA256 != input.hash) {
		err = ErrDiagnosticTool
	}
	if err == nil {
		evidence, parseErr := ParseDiagnosticFFmpegEvidence(plan, observation.Stderr)
		err = parseErr
		if err == nil {
			fact.Evidence = &evidence
		}
	}
	if errors.Is(err, ErrDiagnosticResources) || errors.Is(err, ErrDiagnosticTool) || errors.Is(err, ErrDiagnosticClosure) || errors.Is(err, ErrDiagnosticAuthority) {
		g.fatal = err
	}
	return observation, fact, err
}

func (g *diagnosticStageGraph) fact(observation diagnosticCommandObservation) *DiagnosticCommandFact {
	g.report.CommandsAttempted++
	if observation.Started {
		g.report.CommandsStarted++
	}
	return &DiagnosticCommandFact{Started: observation.Started, ExitCode: observation.ExitCode, Elapsed: observation.Elapsed,
		ToolSHA256: observation.ToolSHA256, EnvironmentSHA256: observation.EnvironmentSHA256, InputSHA256: observation.InputSHA256,
		OutputSHA256: diagnosticDigest(observation.Stdout), OutputBytes: len(observation.Stdout), ProcessesClosed: observation.ProcessesClosed,
		OutputLimitReached: observation.OutputLimitReached, MemoryLimitEvents: observation.MemoryLimitEvents,
		MemoryOOMEvents: observation.MemoryOOMEvents, MemoryOOMKills: observation.MemoryOOMKills, TaskLimitEvents: observation.TaskLimitEvents}
}

func (g *diagnosticStageGraph) prepare(index int, source diagnosticStageInput, mode DiagnosticMode) *diagnosticReference {
	facts := &g.report.Preparations[index]
	if err := g.interrupted(); err != nil {
		facts.Code = diagnosticStageCode(err)
		return nil
	}
	facts.State, facts.ParentSHA256 = "running", source.hash
	g.publish()
	encoded, command, err := g.command(mode, source, DiagnosticProfile{})
	facts.Encode = command
	if err != nil {
		facts.State, facts.Code = diagnosticFailureState(err), diagnosticStageCode(err)
		g.publish()
		return nil
	}
	kind := DiagnosticH264
	if facts.Media == "audio" {
		kind = DiagnosticAAC
		adts, adtsErr := ValidateDiagnosticADTS(encoded.Stdout)
		if adtsErr != nil {
			facts.State, facts.Code = "unverified", diagnosticStageCode(adtsErr)
			g.publish()
			return nil
		}
		facts.ADTS = &adts
	}
	input, err := g.input(kind, encoded.Stdout)
	if err != nil {
		facts.State, facts.Code = "failed", diagnosticStageCode(err)
		g.publish()
		return nil
	}
	facts.EncodedSHA256, facts.EncodedBytes = input.hash, len(encoded.Stdout)
	decoded, verification, err := g.command(DiagnosticDecode, input, DiagnosticProfile{})
	facts.Decode = verification
	if err == nil {
		facts.Video, facts.Audio, err = diagnosticDecodedContent(decoded.Stdout, verification.Evidence)
	}
	if err == nil {
		err = g.interrupted()
	}
	if err != nil {
		facts.State, facts.Code = diagnosticFailureState(err), diagnosticStageCode(err)
		g.publish()
		return nil
	}
	facts.DecodedSHA256, facts.DecodedBytes, facts.State = diagnosticDigest(decoded.Stdout), len(decoded.Stdout), "ready"
	g.publish()
	return &diagnosticReference{input: input, decoded: decoded.Stdout, facts: *facts}
}

func (g *diagnosticStageGraph) stage(index int) {
	stage := &g.report.Stages[index]
	if err := g.interrupted(); err != nil {
		stage.Code = diagnosticStageCode(err)
		return
	}
	source, reference := g.rawVideo, g.video
	if stage.Media == "audio" {
		source, reference = g.rawAudio, g.audio
	}
	if stage.Mode != DiagnosticEncode {
		if reference == nil {
			stage.Code = "diagnostic_reference_unavailable"
			return
		}
		source = reference.input
	}
	if stage.Media == "audio" && stage.Mode == DiagnosticCombined {
		reference = g.audioCombined
	}
	if stage.Media == "audio" && reference == nil {
		stage.Code = "diagnostic_reference_unavailable"
		return
	}
	stage.State, stage.StartedAt = "running", time.Now().UTC()
	if reference != nil {
		stage.ReferenceGeneration, stage.ReferenceSHA256 = reference.facts.Generation, reference.facts.DecodedSHA256
	}
	if stage.Media == "video" && stage.Mode == DiagnosticEncode {
		stage.ReferenceGeneration, stage.ReferenceSHA256 = 0, g.rawVideo.hash
	}
	g.report.State = "running"
	g.publish()
	output, fact, err := g.command(stage.Mode, source, stage.Profile)
	stage.Command = fact
	decoded, decodeFact := output, fact
	if err == nil && stage.Mode != DiagnosticDecode {
		kind := DiagnosticH264
		if stage.Media == "audio" {
			kind = DiagnosticAAC
			adts, adtsErr := ValidateDiagnosticADTS(output.Stdout)
			err = adtsErr
			if err == nil {
				stage.ADTS = &adts
			}
		}
		if err == nil {
			input, inputErr := g.input(kind, output.Stdout)
			err = inputErr
			if err == nil {
				decoded, decodeFact, err = g.command(DiagnosticDecode, input, DiagnosticProfile{})
				stage.Verification = decodeFact
			}
		}
	}
	if err == nil {
		stage.Video, stage.Audio, err = diagnosticDecodedContent(decoded.Stdout, decodeFact.Evidence)
	}
	if err == nil && reference != nil && !(stage.Media == "video" && stage.Mode == DiagnosticEncode) {
		err = diagnosticMatchReference(decoded.Stdout, stage, reference)
	}
	if err == nil {
		err = g.interrupted()
	}
	stage.FinishedAt = time.Now().UTC()
	if err != nil {
		stage.State, stage.Code = diagnosticFailureState(err), diagnosticStageCode(err)
	} else {
		stage.State, stage.DecodedSHA256, stage.DecodedBytes = "passed", diagnosticDigest(decoded.Stdout), len(decoded.Stdout)
	}
	g.publish()
}

func diagnosticMatchReference(decoded []byte, stage *DiagnosticStage, reference *diagnosticReference) error {
	if len(decoded) != len(reference.decoded) {
		return ErrDiagnosticContent
	}
	if stage.Media == "video" {
		for frame := 0; frame < DiagnosticVideoFrames; frame++ {
			start, end := frame*DiagnosticVideoFrameBytes, (frame+1)*DiagnosticVideoFrameBytes
			if _, ok := diagnosticVideoFrameContent(decoded[start:end], reference.decoded[start:end]); !ok {
				return ErrDiagnosticContent
			}
		}
		return nil
	}
	if stage.Audio == nil || reference.facts.Audio == nil || stage.Audio.SampleFrames != reference.facts.Audio.SampleFrames ||
		stage.Audio.OffsetFrames != reference.facts.Audio.OffsetFrames || stage.Audio.TrailingFrames != reference.facts.Audio.TrailingFrames {
		return ErrDiagnosticContent
	}
	if stage.Mode != DiagnosticDecode && (stage.ADTS == nil || reference.facts.ADTS == nil || stage.ADTS.PacketCount != reference.facts.ADTS.PacketCount) {
		return ErrDiagnosticContent
	}
	observed, original := diagnosticPCM(decoded), diagnosticPCM(reference.decoded)
	var squared, energy [2]int64
	for index, value := range observed {
		channel := index % 2
		difference := int64(value) - int64(original[index])
		squared[channel] += difference * difference
		energy[channel] += int64(original[index]) * int64(original[index])
	}
	if energy[0] == 0 || energy[1] == 0 || !diagnosticAudioGlobalError(squared, energy) {
		return ErrDiagnosticContent
	}
	return nil
}

func diagnosticDecodedContent(data []byte, evidence *DiagnosticFFmpegEvidence) (*DiagnosticVideoContent, *DiagnosticAudioContent, error) {
	if evidence == nil {
		return nil, nil, ErrDiagnosticContent
	}
	if evidence.Encoder == "rawvideo" {
		video, err := ValidateDiagnosticVideoContent(data, evidence.Width, evidence.Height, evidence.PixelFormat)
		return &video, nil, err
	}
	if evidence.Encoder == "pcm_s16le" && evidence.SampleFormat == "s16" {
		audio, err := ValidateDiagnosticAudioContent(data, evidence.SampleRate, evidence.Channels, "s16le")
		return nil, &audio, err
	}
	return nil, nil, ErrDiagnosticContent
}

func diagnosticSuccessfulCommand(observation diagnosticCommandObservation) error {
	if !observation.ProcessesClosed {
		return ErrDiagnosticClosure
	}
	if observation.MemoryLimitEvents != 0 || observation.MemoryOOMEvents != 0 || observation.MemoryOOMKills != 0 || observation.TaskLimitEvents != 0 {
		return ErrDiagnosticResources
	}
	if observation.OutputLimitReached {
		return ErrOutputLimit
	}
	if !observation.Started || observation.ExitCode != 0 || observation.Elapsed < 0 || !videoSeekSHA256(observation.ToolSHA256) || !videoSeekSHA256(observation.EnvironmentSHA256) {
		return ErrDiagnosticExecution
	}
	return nil
}

func diagnosticVersion(data []byte) (string, error) {
	if len(data) == 0 || len(data) >= maxProcessStderr {
		return "", ErrDiagnosticTool
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "ffmpeg" || fields[1] != "version" || len(fields[2]) > 80 {
		return "", ErrDiagnosticTool
	}
	if strings.IndexFunc(fields[2], func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r))
	}) >= 0 {
		return "", ErrDiagnosticTool
	}
	return fields[2], nil
}

func diagnosticDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func diagnosticFailureState(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrDiagnosticAuthority) {
		return "cancelled"
	}
	if errors.Is(err, ErrDiagnosticResources) || errors.Is(err, ErrDiagnosticTool) || errors.Is(err, ErrDiagnosticPlan) {
		return "unavailable"
	}
	if errors.Is(err, ErrDiagnosticContent) || errors.Is(err, ErrDiagnosticADTS) || errors.Is(err, ErrDiagnosticEvidence) {
		return "unverified"
	}
	return "failed"
}

func diagnosticStageCode(err error) string {
	switch {
	case errors.Is(err, ErrDiagnosticAuthority):
		return "diagnostic_authority_lost"
	case errors.Is(err, context.Canceled):
		return "diagnostic_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "diagnostic_deadline_exceeded"
	case errors.Is(err, ErrDiagnosticResources):
		return "diagnostic_resources_unavailable"
	case errors.Is(err, ErrDiagnosticTool):
		return "diagnostic_tool_unavailable"
	case errors.Is(err, ErrDiagnosticInput):
		return "diagnostic_input_invalid"
	case errors.Is(err, ErrDiagnosticClosure):
		return "diagnostic_process_closure_failed"
	case errors.Is(err, ErrOutputLimit):
		return "diagnostic_output_limit"
	case errors.Is(err, ErrDiagnosticPlan):
		return "diagnostic_profile_unavailable"
	case errors.Is(err, ErrDiagnosticContent):
		return "diagnostic_content_unverified"
	case errors.Is(err, ErrDiagnosticADTS):
		return "diagnostic_aac_bitstream_invalid"
	case errors.Is(err, ErrDiagnosticEvidence):
		return "diagnostic_codec_evidence_unverified"
	default:
		return "diagnostic_execution_failed"
	}
}

func (g *diagnosticStageGraph) finish() {
	if g.fatal == nil {
		g.fatal = g.interrupted()
	}
	g.report.State = "stages_complete"
	for _, preparation := range g.report.Preparations {
		if preparation.State != "ready" {
			g.report.State = "incomplete"
		}
	}
	for index := range g.report.Stages {
		stage := &g.report.Stages[index]
		if stage.State != "passed" {
			g.report.State = "incomplete"
		}
		if stage.State == "not_run" && stage.Code == "" {
			stage.Code = "diagnostic_reference_unavailable"
		}
	}
	if g.fatal != nil {
		g.report.State, g.report.Code = diagnosticFailureState(g.fatal), diagnosticStageCode(g.fatal)
		for index := range g.report.Stages {
			if g.report.Stages[index].State == "not_run" {
				g.report.Stages[index].Code = g.report.Code
			}
		}
	}
	g.report.FinishedAt = time.Now().UTC()
	g.publish()
}

func (g *diagnosticStageGraph) snapshot() DiagnosticReport {
	result := g.report
	result.Preparations = append([]DiagnosticPreparation(nil), result.Preparations...)
	result.Stages = append([]DiagnosticStage(nil), result.Stages...)
	result.VersionCommand = diagnosticCloneCommand(result.VersionCommand)
	for index := range result.Preparations {
		row := &result.Preparations[index]
		row.Encode, row.Decode = diagnosticCloneCommand(row.Encode), diagnosticCloneCommand(row.Decode)
		if row.ADTS != nil {
			value := *row.ADTS
			row.ADTS = &value
		}
		if row.Video != nil {
			value := *row.Video
			row.Video = &value
		}
		if row.Audio != nil {
			value := *row.Audio
			row.Audio = &value
		}
	}
	for index := range result.Stages {
		row := &result.Stages[index]
		row.Command, row.Verification = diagnosticCloneCommand(row.Command), diagnosticCloneCommand(row.Verification)
		if row.ADTS != nil {
			value := *row.ADTS
			row.ADTS = &value
		}
		if row.Video != nil {
			value := *row.Video
			row.Video = &value
		}
		if row.Audio != nil {
			value := *row.Audio
			row.Audio = &value
		}
	}
	return result
}

func diagnosticCloneCommand(fact *DiagnosticCommandFact) *DiagnosticCommandFact {
	if fact == nil {
		return nil
	}
	copy := *fact
	if fact.Evidence != nil {
		evidence := *fact.Evidence
		copy.Evidence = &evidence
	}
	return &copy
}

func (g *diagnosticStageGraph) publish() {
	if g.emit != nil {
		g.emit(g.snapshot())
	}
}
