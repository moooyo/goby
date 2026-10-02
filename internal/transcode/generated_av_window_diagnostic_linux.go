//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/moooyo/goby/internal/media"
)

type generatedAVDiagnosticHeld struct {
	file     *os.File
	close    func() error
	before   os.FileInfo
	identity string
	offset   int64
}

func generatedAVDiagnosticWindowPlan(options GeneratedAVWindowDiagnosticOptions) (Plan, error) {
	plan := options.NegotiatedPlan
	decode, encode := hardwareSelection(plan.Hardware)
	if ValidatePlan(plan) != nil || plan.OutputMode != "" || plan.SourceMode != "" || plan.SegmentMode != "" || !GeneratedHLS(plan) ||
		plan.Container != "ts" || plan.HLS.SegmentType != "mpegts" || plan.HLS.RenditionCount < 2 || plan.HLS.RenditionCount > MaxHLSRenditions ||
		plan.HLS.Window != (HLSWindow{}) || plan.StartTicks != 0 || plan.SegmentSeconds != 6 || plan.VideoCodec != "h264" || plan.AudioCodec != "aac" ||
		plan.VideoStreamIndex < 0 || plan.AudioStreamIndex < 0 || plan.VideoStreamIndex == plan.AudioStreamIndex || plan.FrameRate != 24 ||
		plan.AudioSampleRate != 48000 || plan.AudioChannels < 1 || plan.AudioChannels > 2 || decode != "software" || encode != "software" ||
		plan.Subtitle != (SubtitlePlan{}) || HasHLSSubtitles(plan) || plan.VideoFilters != (VideoFilters{}) || plan.AudioSampleSeek || plan.CopyTimestamps ||
		plan.VideoSeekCandidate != "" || plan.VideoCopySeekCandidate != "" || options.StartTicks < 0 || options.EndTicks-options.StartTicks != 24*ticksPerSecond ||
		options.EndTicks > plan.DurationTicks || options.StartNumber < 0 || options.StartNumber > MaxPlaylistSegments-8 || options.AcquireProbe == nil || options.Baseline == nil {
		return Plan{}, ErrInvalidPlan
	}
	plan.StartTicks = options.StartTicks
	plan.HLS.Window = HLSWindow{EndTicks: options.EndTicks, StartNumber: options.StartNumber}
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// RunGeneratedAVWindowDiagnostic combines a real manager baseline with a
// separately governed stats-only observation. It holds every baseline/observer
// playlist and media file until source, input, native transport, full packets,
// decoded frames, streamed PCM, comparisons and final identities have finished.
// Every outcome remains Qualified=false; actual cut disagreements are data.
func RunGeneratedAVWindowDiagnostic(ctx context.Context, source *os.File, options GeneratedAVWindowDiagnosticOptions) (result GeneratedAVWindowDiagnostic, failure error) {
	stage, stageVariant, stageNumber := "options", -1, int64(-1)
	defer func() { failure = generatedAVDiagnosticStageFailure(stage, stageVariant, stageNumber, failure) }()
	result.NegotiatedPlan = options.NegotiatedPlan
	if ctx == nil || source == nil {
		return result, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	plan, err := generatedAVDiagnosticWindowPlan(options)
	if err != nil {
		return result, err
	}
	result.WindowPlan = plan
	if !filepath.IsAbs(options.Directory) || !emptyOutputDirectory(options.Directory) {
		return result, ErrInvalidDirectory
	}
	sourceFence, err := generatedAVDiagnosticOuterFence([]*os.File{source})
	if err != nil {
		return result, err
	}
	probe := func(run func() error) error {
		release, err := options.AcquireProbe(ctx)
		if err != nil {
			return err
		}
		if release == nil {
			return ErrInvalidOptions
		}
		defer release()
		return run()
	}
	stage = "source_certificate"
	err = probe(func() error {
		var err error
		result.SourceCertificate, err = MeasureGeneratedMP4AVSourceEndpoint(ctx, options.FFprobePath, source, plan.VideoStreamIndex, plan.AudioStreamIndex)
		return err
	})
	if err != nil {
		return result, err
	}
	cert := result.SourceCertificate
	if cert.DurationTicks != plan.DurationTicks || cert.DemuxOrigin != (GeneratedRational{Den: 1}) || cert.PresentationEpoch != (GeneratedRational{Den: 1}) ||
		cert.Audio.SampleRate != 48000 || cert.Audio.Channels != plan.AudioChannels || cert.Video.FrameRate != 24 {
		return result, ErrUnsupportedTimeline
	}
	stage = "source_effective"
	err = probe(func() error {
		var err error
		result.SourceEffective, err = MeasureGeneratedAVEffectiveDecodeDiagnostic(ctx, options.FFprobePath, []*os.File{source})
		return err
	})
	if err != nil {
		return result, err
	}
	stage = "source_pcm"
	err = probe(func() error {
		var err error
		result.SourcePCM, err = MeasureGeneratedAVPCMDiagnostic(ctx, options.FFmpegPath, []*os.File{source}, result.SourceEffective.Audio.Channels, generatedAVPCMBytes)
		return err
	})
	if err != nil {
		return result, err
	}
	if result.SourceEffective.Audio.Samples != result.SourcePCM.Samples {
		return result, generatedAVTransportInvalid("source frame and PCM sample counts differ")
	}
	if err := sourceFence(); err != nil {
		return result, err
	}
	stage = "baseline_production"
	baseline, err := options.Baseline(ctx, source, plan)
	if err != nil {
		return result, err
	}
	// The successful callback transferred ownership, even if its returned
	// structure proves invalid. Close every transferred handle on all exits.
	defer func() {
		var closeErrors []error
		for variant := 0; variant < MaxHLSRenditions; variant++ {
			if handle := baseline.PlaylistHandles[variant]; handle != nil {
				closeErrors = append(closeErrors, handle.Close())
			}
			for _, handle := range baseline.MediaHandles[variant] {
				if handle != nil {
					closeErrors = append(closeErrors, handle.Close())
				}
			}
		}
		if err := errors.Join(closeErrors...); err != nil {
			result.Complete = false
			failure = errors.Join(failure, err)
		}
	}()
	stage = "baseline_resources"
	actual := baseline.Record.Spec.Plan
	semantic := actual
	if plan.ExecutionVersion == 0 && plan.Execution == (ExecutionOptions{}) {
		semantic.ExecutionVersion, semantic.Execution = 0, ExecutionOptions{}
	}
	if baseline.Record.ID == "" || baseline.Record.State != "completed" || semantic != plan {
		return result, ErrOutputUnavailable
	}
	threads, err := ExecutionThreads(actual, options.Threads)
	if err != nil {
		return result, err
	}
	arguments, err := BuildArgs(actual, threads)
	if err != nil {
		return result, err
	}
	result.Baseline = GeneratedAVDiagnosticRun{Plan: actual, Arguments: arguments, BaselineManagerCompleted: true, Record: baseline.Record, MuxClocks: baseline.MuxClocks}
	var held []*generatedAVDiagnosticHeld
	hold := func(file *os.File, close func() error) (*generatedAVDiagnosticHeld, error) {
		if file == nil {
			return nil, ErrInvalidInput
		}
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		identity, err := media.VideoSeekSourceIdentity(info)
		if err != nil || info.Size() < 1 {
			return nil, ErrInvalidInput
		}
		original, err := source.Stat()
		if err != nil || os.SameFile(original, info) {
			return nil, ErrInvalidInput
		}
		for _, prior := range held {
			if os.SameFile(prior.before, info) {
				return nil, ErrInvalidInput
			}
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		value := &generatedAVDiagnosticHeld{file: file, close: close, before: info, identity: identity, offset: offset}
		held = append(held, value)
		return value, nil
	}
	count := actual.HLS.RenditionCount
	var baselineMedia [MaxHLSRenditions][]*generatedAVDiagnosticHeld
	var mediaBytes int64
	for variant := 0; variant < MaxHLSRenditions; variant++ {
		if variant >= count {
			if baseline.PlaylistHandles[variant] != nil || len(baseline.MediaHandles[variant]) != 0 || baseline.MuxClocks[variant] != (HLSMuxClock{}) || len(baseline.Playlists[variant].Segments) != 0 {
				return result, ErrInvalidInput
			}
			continue
		}
		playlist := baseline.PlaylistHandles[variant]
		if playlist == nil || playlist.EncodingID() != baseline.Record.ID {
			return result, ErrInvalidInput
		}
		if _, err := hold(playlist.File, nil); err != nil {
			return result, err
		}
		if err := generatedAVDiagnosticList(actual, baseline.Playlists[variant], variant); err != nil {
			return result, err
		}
		if len(baseline.MediaHandles[variant]) != len(baseline.Playlists[variant].Segments) {
			return result, ErrInvalidInput
		}
		for _, handle := range baseline.MediaHandles[variant] {
			if handle == nil || handle.EncodingID() != baseline.Record.ID {
				return result, ErrInvalidInput
			}
			value, err := hold(handle.File, nil)
			if err != nil {
				return result, err
			}
			if value.before.Size() > generatedAVTransportBytes-mediaBytes {
				return result, ErrTimelineLimit
			}
			mediaBytes += value.before.Size()
			baselineMedia[variant] = append(baselineMedia[variant], value)
		}
	}
	if mediaBytes >= generatedAVTransportBytes {
		return result, ErrTimelineLimit
	}
	observerDirectory := filepath.Join(options.Directory, "observer")
	if err := os.Mkdir(observerDirectory, 0700); err != nil {
		return result, err
	}
	stage = "observer_production"
	err = probe(func() error {
		var err error
		result.Observer, result.ObserverDelta, err = runGeneratedAVStatsDiagnostic(ctx, options.FFmpegPath, observerDirectory, source, actual, threads, generatedAVTransportBytes-mediaBytes)
		return err
	})
	if err != nil {
		return result, err
	}
	stage = "observer_resources"
	observerRoot, err := os.OpenRoot(observerDirectory)
	if err != nil {
		return result, err
	}
	defer observerRoot.Close()
	// Observer files are independent of the manager's ReadHandle charge, but
	// remain held through the exact same outer proof and final identity fence.
	var observerFiles []*os.File
	defer func() {
		var closeErrors []error
		for _, file := range observerFiles {
			closeErrors = append(closeErrors, file.Close())
		}
		if err := errors.Join(closeErrors...); err != nil {
			result.Complete = false
			failure = errors.Join(failure, err)
		}
	}()
	var observerMedia [MaxHLSRenditions][]*generatedAVDiagnosticHeld
	var observerLists [MaxHLSRenditions]MediaPlaylist
	for variant := 0; variant < count; variant++ {
		playlist, err := observerRoot.Open(HLSPlaylistName(variant, count))
		if err != nil {
			return result, err
		}
		observerFiles = append(observerFiles, playlist)
		if _, err := hold(playlist, nil); err != nil {
			return result, err
		}
		data, err := io.ReadAll(io.LimitReader(playlist, MaxPlaylistBytes+1))
		if err != nil {
			return result, err
		}
		if len(data) > MaxPlaylistBytes {
			return result, ErrTimelineLimit
		}
		list, err := ParseMediaPlaylist(data)
		if err != nil {
			return result, err
		}
		if err := generatedAVDiagnosticList(actual, list, variant); err != nil {
			return result, err
		}
		observerLists[variant] = list
		// The playlist reader's real parse advances only this held file; update
		// its captured offset after the bounded read, before any observation.
		for _, value := range held {
			if value.file == playlist {
				value.offset, err = playlist.Seek(0, io.SeekCurrent)
				if err != nil {
					return result, err
				}
				break
			}
		}
		for _, segment := range list.Segments {
			file, err := observerRoot.Open(segment.Name)
			if err != nil {
				return result, err
			}
			observerFiles = append(observerFiles, file)
			value, err := hold(file, nil)
			if err != nil {
				return result, err
			}
			if value.before.Size() > generatedAVTransportBytes-mediaBytes {
				return result, ErrTimelineLimit
			}
			mediaBytes += value.before.Size()
			observerMedia[variant] = append(observerMedia[variant], value)
		}
	}
	observe := func(run *GeneratedAVDiagnosticRun, lists [MaxHLSRenditions]MediaPlaylist, mediaFiles [MaxHLSRenditions][]*generatedAVDiagnosticHeld, role string) error {
		for variant := 0; variant < count; variant++ {
			stageVariant, stageNumber = variant, -1
			rendition := GeneratedAVDiagnosticRendition{Variant: variant, Playlist: lists[variant]}
			var group []*os.File
			for index, value := range mediaFiles[variant] {
				segment := lists[variant].Segments[index]
				stageNumber = segment.Number
				cut := GeneratedAVDiagnosticCut{Name: segment.Name, Number: segment.Number, DurationTicks: segment.DurationTicks, Identity: value.identity}
				var err error
				stage = role + "_transport"
				cut.Transport, err = MeasureGeneratedAVTransportDiagnostic(ctx, value.file)
				if err != nil {
					return err
				}
				stage = role + "_packets"
				err = probe(func() error {
					var err error
					cut.Packets, err = MeasureGeneratedSegmentBounds(ctx, options.FFprobePath, nil, value.file, true)
					return err
				})
				if err != nil {
					return err
				}
				stage = role + "_effective"
				err = probe(func() error {
					var err error
					cut.Observation, err = MeasureGeneratedAVDecodedObservation(ctx, options.FFprobePath, []*os.File{value.file})
					return err
				})
				if err != nil {
					return err
				}
				stage = role + "_pcm"
				err = probe(func() error {
					var err error
					cut.PCM, err = MeasureGeneratedAVPCMDiagnostic(ctx, options.FFmpegPath, []*os.File{value.file}, cut.Observation.Audio.Channels, 8<<20)
					return err
				})
				if err != nil {
					return err
				}
				if cut.Transport.SHA256 != cut.Packets.SegmentSHA256 || cut.Observation.Audio.Samples != cut.PCM.Samples || len(cut.Observation.InputSHA256) != 1 || cut.Observation.InputSHA256[0] != cut.Transport.SHA256 {
					return generatedAVTransportInvalid("held output observation association differs")
				}
				rendition.Cuts = append(rendition.Cuts, cut)
				group = append(group, value.file)
			}
			stage, stageNumber = role+"_continuous_effective", -1
			err := probe(func() error {
				var err error
				rendition.ContinuousObservation, err = MeasureGeneratedAVDecodedObservation(ctx, options.FFprobePath, group)
				return err
			})
			if err != nil {
				return err
			}
			stage = role + "_continuous_pcm"
			err = probe(func() error {
				var err error
				rendition.ContinuousPCM, err = MeasureGeneratedAVPCMDiagnostic(ctx, options.FFmpegPath, group, rendition.ContinuousObservation.Audio.Channels, 8<<20)
				return err
			})
			if err != nil {
				return err
			}
			if rendition.ContinuousObservation.Audio.Samples != rendition.ContinuousPCM.Samples {
				return generatedAVTransportInvalid("continuous frame and PCM sample counts differ")
			}
			run.Renditions = append(run.Renditions, rendition)
		}
		return nil
	}
	if err := observe(&result.Baseline, baseline.Playlists, baselineMedia, "baseline"); err != nil {
		return result, err
	}
	if err := observe(&result.Observer, observerLists, observerMedia, "observer"); err != nil {
		return result, err
	}
	stage, stageVariant, stageNumber = "comparison", -1, -1
	result.Comparison = generatedAVDiagnosticCompare(result.Baseline, result.Observer, options.NegotiatedPlan)
	stage = "final_fences"
	for _, value := range held {
		info, err := value.file.Stat()
		offset, offsetErr := value.file.Seek(0, io.SeekCurrent)
		if err != nil || offsetErr != nil || offset != value.offset || !transcodeSourceUnchanged(value.file, value.before) {
			return result, ErrInvalidInput
		}
		identity, err := media.VideoSeekSourceIdentity(info)
		if err != nil || identity != value.identity {
			return result, ErrInvalidInput
		}
	}
	if err := sourceFence(); err != nil {
		return result, err
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(source, cert); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Baseline.SourceIdentityUnchanged, result.Observer.SourceIdentityUnchanged = true, true
	result.Baseline.OutputIdentitiesUnchanged, result.Observer.OutputIdentitiesUnchanged = true, true
	result.NativeClockComplete = generatedAVObservationClocksComplete(result.Baseline, count) && generatedAVObservationClocksComplete(result.Observer, count)
	result.Complete = true
	return result, nil
}

func generatedAVObservationClocksComplete(run GeneratedAVDiagnosticRun, count int) bool {
	if count <= 0 || count > MaxHLSRenditions || len(run.Renditions) != count {
		return false
	}
	for _, rendition := range run.Renditions {
		if len(rendition.Cuts) == 0 || !rendition.ContinuousObservation.Complete || !rendition.ContinuousObservation.NativeClockComplete {
			return false
		}
		for _, cut := range rendition.Cuts {
			if !cut.Observation.Complete || !cut.Observation.NativeClockComplete {
				return false
			}
		}
	}
	return true
}

func generatedAVDiagnosticList(plan Plan, list MediaPlaylist, variant int) error {
	if !list.Ended || list.Type != "EVENT" || list.InitName != "" || list.Sequence != int64(plan.HLS.Window.StartNumber) || len(list.Segments) < 1 || len(list.Segments) > 8 {
		return ErrInvalidTimeline
	}
	for index, segment := range list.Segments {
		number := plan.HLS.Window.StartNumber + index
		if segment.Number != int64(number) || segment.Name != fmt.Sprintf("v%d-segment-%06d.ts", variant, number) || segment.DurationTicks <= 0 {
			return ErrInvalidTimeline
		}
	}
	return nil
}

func generatedAVDiagnosticCompare(baseline, observer GeneratedAVDiagnosticRun, negotiated Plan) GeneratedAVDiagnosticComparison {
	result := GeneratedAVDiagnosticComparison{MediaBytesEqual: true, PacketClocksEqual: true, PhysicalCutsEqual: true,
		NegotiatedLadderPreserved: baseline.Plan.HLS.RenditionCount == negotiated.HLS.RenditionCount && baseline.Plan.HLS.Renditions == negotiated.HLS.Renditions && observer.Plan.HLS.RenditionCount == negotiated.HLS.RenditionCount && observer.Plan.HLS.Renditions == negotiated.HLS.Renditions}
	if len(baseline.Renditions) != len(observer.Renditions) {
		result.MediaBytesEqual, result.PacketClocksEqual, result.PhysicalCutsEqual = false, false, false
		return result
	}
	for variant, left := range baseline.Renditions {
		right := observer.Renditions[variant]
		if left.Variant != right.Variant || !reflect.DeepEqual(left.Playlist, right.Playlist) || len(left.Cuts) != len(right.Cuts) {
			result.PhysicalCutsEqual = false
		}
		if len(left.Cuts) != len(right.Cuts) {
			result.MediaBytesEqual, result.PacketClocksEqual = false, false
			continue
		}
		for index, cut := range left.Cuts {
			other := right.Cuts[index]
			result.MediaBytesEqual = result.MediaBytesEqual && cut.Transport.SHA256 == other.Transport.SHA256
			leftPackets, rightPackets := cut.Packets, other.Packets
			leftPackets.InitializationSHA256, leftPackets.SegmentSHA256 = [32]byte{}, [32]byte{}
			rightPackets.InitializationSHA256, rightPackets.SegmentSHA256 = [32]byte{}, [32]byte{}
			result.PacketClocksEqual = result.PacketClocksEqual && leftPackets == rightPackets
			result.PhysicalCutsEqual = result.PhysicalCutsEqual && cut.Name == other.Name && cut.Number == other.Number && cut.DurationTicks == other.DurationTicks
		}
	}
	return result
}
