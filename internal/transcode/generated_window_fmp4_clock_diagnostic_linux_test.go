//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// Candidate argument changes are confined to this opt-in diagnostic. They
// never change BuildArgs, NativeClockV1 eligibility or published graph plans.
// A successful observation does not qualify fMP4 for full-VOD playback.
type generatedFMP4ClockProfile struct {
	name         string
	childOptions string
}

var generatedFMP4ClockProfiles = []generatedFMP4ClockProfile{
	{name: "ordinary"},
	{name: "offset", childOptions: "use_editlist=0:avoid_negative_ts=disabled"},
	{name: "offset_frag_discont", childOptions: "movflags=+frag_discont:use_editlist=0:avoid_negative_ts=disabled"},
}

func generatedFMP4ClockDiagnosticArgs(plan Plan, profile generatedFMP4ClockProfile) ([]string, error) {
	args, err := BuildArgs(plan, 1)
	if err != nil || plan.HLS.SegmentType != "fmp4" || plan.HLS.Window.NativeClockVersion != 0 ||
		!plan.HLS.Window.RequireInputEvidence || !GeneratedWindowClosureEligible(plan) {
		return nil, ErrInvalidPlan
	}
	if profile.name == "ordinary" && profile.childOptions == "" {
		return args, nil
	}
	if profile != generatedFMP4ClockProfiles[1] && profile != generatedFMP4ClockProfiles[2] {
		return nil, ErrInvalidPlan
	}
	var decorated []string
	var offsets, children int
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-avoid_negative_ts":
			if index+1 >= len(args) || args[index+1] != "make_zero" {
				return nil, ErrInvalidPlan
			}
			decorated = append(decorated, "-avoid_negative_ts", "disabled", "-output_ts_offset", tickSeconds(plan.StartTicks))
			offsets++
			index++
		case "-hls_fmp4_init_filename":
			decorated = append(decorated, "-hls_segment_options", profile.childOptions, args[index])
			children++
		default:
			decorated = append(decorated, args[index])
		}
	}
	if offsets != max(1, plan.HLS.RenditionCount) || children != offsets {
		return nil, ErrInvalidPlan
	}
	return decorated, nil
}

type generatedFMP4DiagnosticBuffer struct {
	bytes.Buffer
	limit  int
	err    error
	cancel context.CancelFunc
}

func (buffer *generatedFMP4DiagnosticBuffer) Write(data []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	if len(data) > buffer.limit-buffer.Len() {
		buffer.err = ErrTimelineLimit
		buffer.cancel()
		return 0, buffer.err
	}
	return buffer.Buffer.Write(data)
}

// Every child, including fixture creation and independent decoding, uses the
// shared foreground governor and retires its owned group before Wait reaps.
// Observer writers are closed even when admission or Start fails.
func generatedFMP4DiagnosticCommand(ctx context.Context, executable, directory string, source *os.File, plan *Plan, args []string) ([]byte, []byte,
	[MaxHLSRenditions]HLSMuxClock, [MaxHLSRenditions]GeneratedInputEvidence, error) {
	var clocks [MaxHLSRenditions]HLSMuxClock
	var input [MaxHLSRenditions]GeneratedInputEvidence
	processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var owned *os.File
	var before os.FileInfo
	var offset int64
	if source != nil {
		var err error
		before, err = source.Stat()
		if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > 16<<20 {
			return nil, nil, clocks, input, ErrInvalidInput
		}
		offset, err = source.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, nil, clocks, input, ErrInvalidInput
		}
		owned, err = DuplicateInput(source)
		if err != nil {
			return nil, nil, clocks, input, err
		}
		defer owned.Close()
	}
	var clockObserver *hlsClockObserver
	var inputObserver *generatedInputObserver
	var clockMu sync.Mutex
	var seen [MaxHLSRenditions]bool
	if plan != nil {
		if source == nil {
			return nil, nil, clocks, input, ErrInvalidInput
		}
		var err error
		clockObserver, err = newHLSClockObserver(*plan, func(progress Progress) {
			if progress.HLSClock != nil {
				index := progress.HLSClock.Rendition
				if index >= 0 && index < max(1, plan.HLS.RenditionCount) {
					clockMu.Lock()
					clocks[index], seen[index] = *progress.HLSClock, true
					clockMu.Unlock()
				}
			}
		}, cancel)
		if err != nil {
			return nil, nil, clocks, input, err
		}
		defer clockObserver.close()
		inputObserver, err = newGeneratedInputObserver(processCtx, *plan, nil, cancel)
		if err != nil {
			return nil, nil, clocks, input, err
		}
		defer inputObserver.close()
	}
	var startOnce sync.Once
	startObservers := func() {
		startOnce.Do(func() {
			if clockObserver != nil {
				clockObserver.start()
				inputObserver.start()
			}
		})
	}
	stopObserverContext := context.AfterFunc(processCtx, func() {
		if clockObserver != nil {
			clockObserver.close()
			inputObserver.close()
		}
	})
	defer stopObserverContext()
	command := exec.CommandContext(processCtx, executable, args...)
	command.Dir, command.Env = directory, processEnvironment()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	if owned != nil {
		command.ExtraFiles = append(command.ExtraFiles, owned)
	}
	if clockObserver != nil {
		for _, pipe := range clockObserver.pipes {
			command.ExtraFiles = append(command.ExtraFiles, pipe.write)
		}
		for _, pipe := range inputObserver.pipes {
			command.ExtraFiles = append(command.ExtraFiles, pipe.write)
		}
	}
	stdout := &generatedFMP4DiagnosticBuffer{limit: 16 << 20, cancel: cancel}
	stderr := &generatedFMP4DiagnosticBuffer{limit: 64 << 10, cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
	var groupMu sync.Mutex
	retired := false
	retirementUnknown := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			if retirementUnknown {
				// The governed owner kills only its still-owned direct child.
				// Unknown WNOWAIT cannot authorize another numeric PGID signal.
				return media.ErrProcessRetirementUnknown
			}
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		startObservers()
		waitErr := waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			retirementUnknown = true
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	startObservers()
	var observerErr error
	if clockObserver != nil {
		observerErr = errors.Join(clockObserver.finish(), inputObserver.finish())
		input = inputObserver.snapshot()
		for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
			if !seen[index] {
				observerErr = errors.Join(observerErr, ErrInvalidTimeline)
			}
		}
	}
	var sourceErr error
	if source != nil {
		afterOffset, err := source.Seek(0, io.SeekCurrent)
		if err != nil || afterOffset != offset || !transcodeSourceUnchanged(source, before) {
			sourceErr = ErrInvalidInput
		}
	}
	return stdout.Bytes(), stderr.Bytes(), clocks, input, errors.Join(runErr, observerErr, stdout.err, stderr.err, processCtx.Err(), sourceErr)
}

type generatedFMP4DiagnosticTFDT struct {
	TrackID    uint32 `json:"track_id"`
	Version    byte   `json:"version"`
	DecodeTime uint64 `json:"decode_time_units"`
}

// Raw tfdt units are retained without guessing a track time base or normalizing
// them into a source epoch. Packet seconds are measured independently below.
func generatedFMP4DiagnosticDecodeTimes(segment *os.File) ([]generatedFMP4DiagnosticTFDT, error) {
	info, err := segment.Stat()
	if err != nil || info.Size() < 1 || info.Size() > 16<<20 {
		return nil, ErrInvalidInput
	}
	var result []generatedFMP4DiagnosticTFDT
	for offset, count := int64(0), 0; offset < info.Size(); count++ {
		if count >= 128 {
			return nil, ErrTimelineLimit
		}
		box, err := generatedFramingReadBox(segment, info.Size(), offset)
		if err != nil {
			return nil, err
		}
		if box.kind == "moof" {
			metadata, err := generatedFramingMetadata(segment, box)
			if err != nil {
				return nil, err
			}
			parser := progressiveVideoParser{}
			children, err := parser.children(metadata)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if child.kind != "traf" {
					continue
				}
				trackChildren, err := parser.children(child.payload)
				if err != nil {
					return nil, err
				}
				header, headerErr := progressiveVideoOne(trackChildren, "tfhd", true)
				decode, decodeErr := progressiveVideoOne(trackChildren, "tfdt", true)
				if headerErr != nil || decodeErr != nil || len(header.payload) < 8 || len(decode.payload) < 8 ||
					decode.payload[1] != 0 || decode.payload[2] != 0 || decode.payload[3] != 0 {
					return nil, ErrTimelineProbe
				}
				fact := generatedFMP4DiagnosticTFDT{TrackID: binary.BigEndian.Uint32(header.payload[4:8]), Version: decode.payload[0]}
				switch {
				case fact.Version == 0 && len(decode.payload) == 8:
					fact.DecodeTime = uint64(binary.BigEndian.Uint32(decode.payload[4:]))
				case fact.Version == 1 && len(decode.payload) == 12:
					fact.DecodeTime = binary.BigEndian.Uint64(decode.payload[4:])
				default:
					return nil, ErrTimelineProbe
				}
				if fact.TrackID == 0 || len(result) >= 64 {
					return nil, ErrTimelineLimit
				}
				result = append(result, fact)
			}
		}
		offset = box.end
	}
	if len(result) == 0 {
		return nil, ErrTimelineProbe
	}
	return result, nil
}

type generatedFMP4DiagnosticFacts struct {
	Profile             string                             `json:"profile"`
	Rate                int                                `json:"rate"`
	SourceOrigin        int                                `json:"source_origin"`
	StartTicks          int64                              `json:"start_ticks"`
	EndTicks            int64                              `json:"end_ticks"`
	Rendition           int                                `json:"rendition"`
	Mux                 HLSMuxClock                        `json:"mux"`
	Input               GeneratedInputEvidence             `json:"input"`
	Packets             GeneratedSegmentBounds             `json:"packets"`
	NativeClock         GeneratedFMP4NativeClock           `json:"native_clock"`
	SourceRange         GeneratedSourceRange               `json:"source_range"`
	SourceEndpoint      GeneratedSourceEndpointCertificate `json:"source_endpoint"`
	SourceTailObserved  bool                               `json:"source_tail_observed"`
	TFDT                []generatedFMP4DiagnosticTFDT      `json:"tfdt"`
	NativePacketAligned bool                               `json:"native_packet_aligned"`
	IntervalClosure     bool                               `json:"interval_closure"`
	IndependentDecoded  bool                               `json:"independent_decoded"`
	Qualified           bool                               `json:"qualified"`
}

func generatedFMP4DiagnosticSave(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func generatedFMP4DiagnosticSegment(ctx context.Context, ffprobe, directory string, root *os.Root, label string, plan Plan, variant int, native *GeneratedFMP4NativeClock) (MediaPlaylist, GeneratedSegmentBounds, []generatedFMP4DiagnosticTFDT, string, error) {
	var list MediaPlaylist
	var bounds GeneratedSegmentBounds
	if native == nil {
		return list, bounds, nil, "", ErrInvalidInput
	}
	*native = GeneratedFMP4NativeClock{}
	data, err := os.ReadFile(filepath.Join(directory, HLSPlaylistName(variant, plan.HLS.RenditionCount)))
	if err != nil || len(data) > 512<<10 {
		return list, bounds, nil, "", ErrTimelineProbe
	}
	list, err = ParseMediaPlaylist(data)
	prefix := hlsPrefix(variant, plan.HLS.RenditionCount)
	if err != nil || !list.Ended || list.Type != "EVENT" || list.Sequence != int64(plan.HLS.Window.StartNumber) || len(list.Segments) != 1 ||
		list.InitName != prefix+"init.mp4" || list.Segments[0].Name != fmt.Sprintf("%ssegment-%06d.m4s", prefix, plan.HLS.Window.StartNumber) ||
		!list.Segments[0].Discontinuity || list.Segments[0].DurationTicks != plan.HLS.Window.EndTicks-plan.StartTicks {
		return list, bounds, nil, "", ErrInvalidTimeline
	}
	initialization, err := os.Open(filepath.Join(directory, list.InitName))
	if err != nil {
		return list, bounds, nil, "", ErrTimelineProbe
	}
	defer initialization.Close()
	segment, err := os.Open(filepath.Join(directory, list.Segments[0].Name))
	if err != nil {
		return list, bounds, nil, "", ErrTimelineProbe
	}
	defer segment.Close()
	initStamp, initErr := initialization.Stat()
	mediaStamp, mediaErr := segment.Stat()
	if initErr != nil || mediaErr != nil {
		return list, bounds, nil, "", ErrInvalidInput
	}
	if _, err := initialization.Seek(3, io.SeekStart); err != nil {
		return list, bounds, nil, "", ErrInvalidInput
	}
	if _, err := segment.Seek(5, io.SeekStart); err != nil {
		return list, bounds, nil, "", ErrInvalidInput
	}
	bounds, err = MeasureGeneratedWindowSegment(ctx, ffprobe, plan, initialization, segment)
	if err != nil {
		return list, bounds, nil, "", err
	}
	measuredNative, err := MeasureGeneratedFMP4NativeClock(ctx, plan, initialization, segment)
	if err != nil || measuredNative.TotalSamples != bounds.Video.PacketCount || measuredNative.InitializationSHA256 != bounds.InitializationSHA256 ||
		measuredNative.SegmentSHA256 != bounds.SegmentSHA256 || bounds.Video.MinPacketDuration <= 0 ||
		bounds.Video.MinPacketDuration != bounds.Video.MaxPacketDuration || !bounds.Video.PresentationDecodeAligned ||
		generatedClockSeconds(1, 1, measuredNative.MediaTimeScale).Cmp(generatedClockSeconds(1, bounds.Video.TimeBase.Num, bounds.Video.TimeBase.Den)) != 0 {
		return list, bounds, nil, "", errors.Join(ErrInvalidTimeline, err)
	}
	var samples int64
	for _, fragment := range measuredNative.Fragments[:measuredNative.FragmentCount] {
		if fragment.TrackID != measuredNative.TrackID || fragment.DecodeUnits > math.MaxInt64 ||
			int64(fragment.DecodeUnits) != bounds.Video.FirstDTS+samples*bounds.Video.MinPacketDuration {
			return list, bounds, nil, "", ErrInvalidTimeline
		}
		samples += fragment.SampleCount
	}
	tfdt, err := generatedFMP4DiagnosticDecodeTimes(segment)
	if err != nil {
		return list, bounds, nil, "", err
	}
	var combined []byte
	for _, file := range []*os.File{initialization, segment} {
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64((16<<20)-len(combined)) {
			return list, bounds, nil, "", ErrTimelineLimit
		}
		data := make([]byte, int(info.Size()))
		if _, err := io.ReadFull(io.NewSectionReader(file, 0, info.Size()), data); err != nil {
			return list, bounds, nil, "", ErrTimelineProbe
		}
		combined = append(combined, data...)
	}
	initOffset, initErr := initialization.Seek(0, io.SeekCurrent)
	mediaOffset, mediaErr := segment.Seek(0, io.SeekCurrent)
	if initErr != nil || mediaErr != nil || initOffset != 3 || mediaOffset != 5 {
		return list, bounds, nil, "", ErrInvalidInput
	}
	name := fmt.Sprintf("decode-v%d.mp4", variant)
	if generatedFMP4DiagnosticSave(root, label+"/"+name, combined) != nil {
		return list, bounds, nil, "", ErrTimelineProbe
	}
	if !transcodeSourceUnchanged(initialization, initStamp) || !transcodeSourceUnchanged(segment, mediaStamp) {
		return list, bounds, nil, "", ErrInvalidInput
	}
	*native = measuredNative
	return list, bounds, tfdt, filepath.Join(directory, name), nil
}

func generatedFMP4DiagnosticErrorCode(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, media.ErrProcessRetirementUnknown):
		return "retirement_unknown"
	case errors.Is(err, ErrTimelineLimit):
		return "limit"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, ErrInvalidInput):
		return "source_changed"
	default:
		return "observation_failed"
	}
}

// This is an explicitly experimental emission diagnostic. Complete generic
// interval closure and independent decoding are required observations; native
// offset alignment is recorded, never assumed or used to publish a graph.
func TestGeneratedFMP4NativeClockEmissionDiagnostic(t *testing.T) {
	if os.Getenv("GOBY_GENERATED_FMP4_CLOCK_DIAGNOSTIC") != "1" {
		t.Skip("GOBY_GENERATED_FMP4_CLOCK_DIAGNOSTIC=1 is required")
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	directory := os.Getenv("GOBY_GENERATED_FMP4_CLOCK_DIAGNOSTIC_DIR")
	if ffmpeg == "" || ffprobe == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || strings.ContainsAny(directory, "\x00\r\n") {
		t.Fatal("diagnostic requires pinned tools and a fresh absolute private directory")
	}
	if os.Mkdir(directory, 0700) != nil {
		t.Fatal("diagnostic directory could not be created exclusively")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("diagnostic directory is not private")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		t.Fatal("diagnostic directory has a foreign owner")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal("diagnostic directory could not be held")
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, rate := range []int{24, 25} {
		for _, origin := range []int{0, 2} {
			sourceName := fmt.Sprintf("source-r%d-o%d.mp4", rate, origin)
			sourcePath := filepath.Join(directory, sourceName)
			args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1", "-n", "-f", "lavfi", "-i",
				fmt.Sprintf("color=c=red:s=160x96:r=%d:d=100", rate), "-vf",
				"drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='gte(t,90)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,93)'",
				"-an", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-crf", "0", "-pix_fmt", "yuv420p", "-bf", "0",
				"-g", strconv.Itoa(rate), "-keyint_min", strconv.Itoa(rate), "-sc_threshold", "0", "-output_ts_offset", strconv.Itoa(origin)}
			if origin == 0 {
				args = append(args, "-use_editlist", "0")
			}
			args = append(args, "-movflags", "+faststart", sourcePath)
			_, stderr, _, _, runErr := generatedFMP4DiagnosticCommand(ctx, ffmpeg, directory, nil, nil, args)
			if generatedFMP4DiagnosticSave(root, sourceName+".stderr", stderr) != nil || runErr != nil || len(stderr) != 0 {
				t.Fatalf("source generation observation failed: rate=%d origin=%d error_class=%s stderr_bytes=%d", rate, origin, generatedFMP4DiagnosticErrorCode(runErr), len(stderr))
			}
			source, err := root.Open(sourceName)
			if err != nil {
				t.Fatal("diagnostic source could not be held")
			}
			func() {
				defer source.Close()
				if source.Chmod(0600) != nil {
					t.Fatal("diagnostic source could not be restricted")
				}
				certificate, err := MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
				if err != nil || !certificate.DurationTicksExact || certificate.DurationTicks != 100*ticksPerSecond || certificate.SampleCount != int64(100*rate) ||
					certificate.Origin != (GeneratedRational{Num: int64(origin), Den: 1}) {
					t.Fatal("diagnostic source lacks its independent exact native endpoint")
				}
				if _, err := source.Seek(37, io.SeekStart); err != nil {
					t.Fatal("diagnostic source offset could not be initialized")
				}
				for _, start := range []int64{0, 6, 90, 96} {
					for _, count := range []int{0, 2} {
						for _, profile := range generatedFMP4ClockProfiles {
							label := fmt.Sprintf("r%d-o%d-s%d-n%d-%s", rate, origin, start, max(1, count), profile.name)
							t.Run(label, func(t *testing.T) {
								if root.Mkdir(label, 0700) != nil {
									t.Fatal("private output directory could not be created exclusively")
								}
								outputDirectory := filepath.Join(directory, label)
								end := min(start+6, int64(100))
								plan := generatedClosureMediaPlan("fmp4", 100, start, end, count != 0)
								plan.FrameRate, plan.HLS.Window.StartNumber = float64(rate), int(start/6)
								arguments, err := generatedFMP4ClockDiagnosticArgs(plan, profile)
								if err != nil {
									t.Fatal("candidate diagnostic command could not be constructed")
								}
								progress, stderr, clocks, input, runErr := generatedFMP4DiagnosticCommand(ctx, ffmpeg, outputDirectory, source, &plan, arguments)
								if generatedFMP4DiagnosticSave(root, label+"/progress.txt", progress) != nil || generatedFMP4DiagnosticSave(root, label+"/stderr.txt", stderr) != nil {
									t.Fatal("private producer diagnostics could not be retained")
								}
								if runErr != nil || len(stderr) != 0 {
									t.Fatalf("finite diagnostic producer failed: error_class=%s stderr_bytes=%d", generatedFMP4DiagnosticErrorCode(runErr), len(stderr))
								}
								coverage, err := MeasureGeneratedSourceRange(ctx, ffprobe, source, plan, int64(origin)*ticksPerSecond)
								if err != nil {
									t.Fatal("diagnostic source range could not be independently observed")
								}
								tailObserved := false
								if plan.HLS.Window.EndTicks == certificate.DurationTicks {
									tail, err := MeasureGeneratedMP4SourceTail(ctx, ffprobe, source, plan, certificate)
									if err != nil || tail != coverage {
										t.Fatal("actual terminal source range did not agree with the independent endpoint and last-sample facts")
									}
									tailObserved = true
								}
								var outputs [MaxHLSRenditions]GeneratedSegmentBounds
								var lists [MaxHLSRenditions]MediaPlaylist
								var facts []generatedFMP4DiagnosticFacts
								for variant := 0; variant < max(1, count); variant++ {
									var native GeneratedFMP4NativeClock
									list, bounds, tfdt, combined, err := generatedFMP4DiagnosticSegment(ctx, ffprobe, outputDirectory, root, label, plan, variant, &native)
									if err != nil {
										t.Fatalf("diagnostic media/init observations incomplete: rendition=%d error_class=%s", variant, generatedFMP4DiagnosticErrorCode(err))
									}
									width, height := plan.Width, plan.Height
									if count != 0 {
										width, height = plan.HLS.Renditions[variant].Width, plan.HLS.Renditions[variant].Height
									}
									first := generatedClockSeconds(bounds.Video.FirstPTS, bounds.Video.TimeBase.Num, bounds.Video.TimeBase.Den)
									packetEnd := generatedClockSeconds(bounds.Video.EndPTS, bounds.Video.TimeBase.Num, bounds.Video.TimeBase.Den)
									fact := generatedFMP4DiagnosticFacts{Profile: profile.name, Rate: rate, SourceOrigin: origin,
										StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks, Rendition: variant, Mux: clocks[variant], Input: input[variant], Packets: bounds,
										TFDT: tfdt, NativePacketAligned: first.Cmp(generatedTicksSeconds(plan.StartTicks)) == 0 && packetEnd.Cmp(generatedTicksSeconds(plan.HLS.Window.EndTicks)) == 0}
									fact.NativeClock = native
									fact.SourceRange, fact.SourceEndpoint, fact.SourceTailObserved = coverage, certificate, tailObserved
									if native.HeaderDurationKnown || native.HeaderDurationUnits != 0 || native.TrackID == 0 ||
										profile.name != "ordinary" && native.EditPresent {
										t.Fatal("initialization duration or edit state differs from the observed emission profile")
									}
									stage, stageErr := json.MarshalIndent(fact, "", "  ")
									if stageErr != nil || generatedFMP4DiagnosticSave(root, fmt.Sprintf("%s/packet-v%d-facts.json", label, variant), stage) != nil {
										t.Fatal("safe partial packet observations could not be retained")
									}
									decoded, decodeStderr, _, _, decodeErr := generatedFMP4DiagnosticCommand(ctx, ffmpeg, directory, nil, nil,
										[]string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", combined, "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1"})
									if generatedFMP4DiagnosticSave(root, fmt.Sprintf("%s/decode-v%d.stderr", label, variant), decodeStderr) != nil {
										t.Fatal("private independent-decode diagnostics could not be retained")
									}
									if decodeErr != nil || len(decodeStderr) != 0 || !generatedFMP4DiagnosticContent(decoded, width, height, rate, start, end) {
										t.Fatal("diagnostic media did not independently decode the complete requested source content")
									}
									outputs[variant], lists[variant] = bounds, list
									fact.IndependentDecoded = true
									facts = append(facts, fact)
								}
								_, closureErr := ValidateGeneratedWindowClosure(plan, coverage, input, clocks, outputs, lists)
								for index := range facts {
									facts[index].IntervalClosure = closureErr == nil
								}
								if err := ValidateGeneratedMP4SourceEndpointIdentity(source, certificate); err != nil {
									t.Fatal("diagnostic source identity no longer matches its independently observed endpoint")
								}
								if sourceOffset, err := source.Seek(0, io.SeekCurrent); err != nil || sourceOffset != 37 {
									t.Fatal("diagnostic source offset changed across input, source and output closure")
								}
								encoded, err := json.MarshalIndent(facts, "", "  ")
								if err != nil || generatedFMP4DiagnosticSave(root, label+"/facts.json", encoded) != nil {
									t.Fatal("safe diagnostic facts could not be retained exclusively")
								}
								if closureErr != nil {
									t.Fatal("normally completed output did not establish generic interval closure")
								}
								for _, fact := range facts {
									if profile.name == "offset_frag_discont" && !fact.NativePacketAligned {
										t.Fatal("extended fragment-discontinuity candidate failed its independently observed source-global packet interval")
									}
									t.Logf("fMP4 emission observation: profile=%s rate=%d origin=%d start=%d rendition=%d premux_pts=%d packet_first=%d packet_end=%d timebase=%d/%d native_aligned=%t decoded=true qualified=false",
										fact.Profile, rate, origin, start, fact.Rendition, fact.Mux.PTS, fact.Packets.Video.FirstPTS, fact.Packets.Video.EndPTS,
										fact.Packets.Video.TimeBase.Num, fact.Packets.Video.TimeBase.Den, fact.NativePacketAligned)
								}
							})
						}
					}
				}
			}()
		}
	}
}

func generatedFMP4DiagnosticContent(data []byte, width, height, rate int, start, end int64) bool {
	if end <= start || end-start > 6 {
		return false
	}
	frameBytes, frames := width*height*3, rate*int(end-start)
	if frameBytes <= 0 || len(data) != frameBytes*frames {
		return false
	}
	for index := 0; index < frames; index++ {
		var totals [3]int64
		frame := data[index*frameBytes : (index+1)*frameBytes]
		for pixel := 0; pixel < len(frame); pixel += 3 {
			for channel := range totals {
				totals[channel] += int64(frame[pixel+channel])
			}
		}
		wanted := 0
		if start >= 93 {
			wanted = 2
		} else if start >= 90 {
			wanted = 1
			if index >= rate*int(93-start) {
				wanted = 2
			}
		}
		for channel, total := range totals {
			average := total / int64(width*height)
			if channel == wanted && average < 170 || channel != wanted && average > 60 {
				return false
			}
		}
	}
	return true
}
