package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"
)

func previewHoldTestSetup(t *testing.T, end, interval int64) (Info, Stream, analysisVisualPlan, AnalysisLimits, *analysisPreviewHoldBudget) {
	t.Helper()
	info, stream := visualAnalysisTestInfo()
	info.FormatStartTicks, info.DurationTicks = 0, end
	plan := analysisVisualPlan{end: end, interval: interval, frames: int((end-1)/interval + 1), width: 2, height: 2, channels: 3, pixelFormat: "rgb24"}
	limits := DefaultAnalysisLimits()
	return info, stream, plan, limits, &analysisPreviewHoldBudget{limits: limits}
}

func previewHoldTestTranscript(timestamps []int64, proof *analysisPreviewHoldProof) string {
	var text strings.Builder
	text.WriteString(visualAnalysisConfig("source", "1/1000"))
	selected := make(map[int]bool)
	if proof == nil {
		selected[0] = true
	} else {
		for _, point := range proof.points {
			selected[point.ordinal] = true
		}
	}
	for index, pts := range timestamps {
		value := strconv.FormatInt(pts, 10)
		text.WriteString(visualAnalysisPacket(2, value))
		text.WriteString(visualAnalysisFrame("source", index, value, 1920, 1080, "yuv420p"))
		if selected[index] {
			text.WriteString(visualAnalysisOutput(value, "1/1000"))
		}
	}
	return text.String()
}

func previewHoldTestPlan(t *testing.T, timestamps []int64, end, interval int64) (analysisPreviewHoldProof, *analysisPreviewHoldLog, *analysisPreviewHoldBudget) {
	t.Helper()
	info, stream, plan, limits, budget := previewHoldTestSetup(t, end, interval)
	log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, nil)
	if err != nil {
		t.Fatal(err)
	}
	transcript := previewHoldTestTranscript(timestamps, nil)
	for position := 0; position < len(transcript); position += 11 {
		if _, err := log.Write([]byte(transcript[position:min(position+11, len(transcript))])); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := log.finishPlan(); err == nil {
		t.Fatal("unclosed source authorized EOF hold")
	}
	log.Close(nil)
	proof, err := log.finishPlan()
	if err != nil {
		t.Fatal(err)
	}
	return proof, log, budget
}

func TestAnalysisPreviewHoldPlansVFRLateStartAndAudioTail(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		timestamps    []int64
		end, interval int64
		actual        string
	}{
		{"video_eof_before_audio", []int64{0, 9960}, 10100, 10000, "[0 99600000]"},
		{"vfr_long_pictures", []int64{0, 3000, 7000}, 8100, 1000, "[0 0 0 30000000 30000000 30000000 30000000 70000000 70000000]"},
		{"late_first_picture", []int64{1000, 4000, 8000}, 9100, 1000, "[10000000 10000000 10000000 10000000 40000000 40000000 40000000 40000000 80000000 80000000]"},
		{"one_real_picture", []int64{250}, 10100, 10000, "[2500000 2500000]"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			end, interval := fixture.end*TicksPerSecond/1000, fixture.interval*TicksPerSecond/1000
			proof, first, budget := previewHoldTestPlan(t, fixture.timestamps, end, interval)
			info, stream, plan, limits, _ := previewHoldTestSetup(t, end, interval)
			if err := budget.admitRepeat(proof.sourceFrames, proof.packets); err != nil {
				t.Fatal(err)
			}
			second, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, &proof)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := second.Write([]byte(previewHoldTestTranscript(fixture.timestamps, &proof))); err != nil {
				t.Fatal(err)
			}
			second.Close(nil)
			var actual []int64
			err = readAnalysisPreviewHeldFrames(context.Background(), bytes.NewReader(make([]byte, len(proof.points)*12)), second,
				func(point analysisPreviewHoldPoint, pixels []byte) error {
					if len(pixels) != 12 || point.firstSlot != len(actual) {
						return fmt.Errorf("invalid selected raster or slot sequence")
					}
					for index := 0; index < point.slotCount; index++ {
						actual = append(actual, point.actual)
					}
					return nil
				})
			if err != nil || second.result() != nil || fmt.Sprint(actual) != fixture.actual {
				t.Fatalf("held samples = %v, read=%v, audit=%v", actual, err, second.result())
			}
			if budget.frames != 2*len(fixture.timestamps) || first.audit.source.count != len(fixture.timestamps) {
				t.Fatalf("source work was not accumulated across full passes: %+v", budget)
			}
		})
	}
}

func TestAnalysisPreviewHoldOriginAdditionIsExactBeyondInt64(t *testing.T) {
	for _, origin := range []int64{math.MaxInt64 - 1, math.MinInt64 + 1} {
		planner := analysisPreviewHoldPlanner{plan: analysisVisualPlan{end: 3 * TicksPerSecond, interval: TicksPerSecond, frames: 3}, origin: origin}
		timestamp := func(relative int64) *big.Rat {
			absolute := new(big.Int).Add(big.NewInt(origin), big.NewInt(relative))
			return new(big.Rat).SetFrac(absolute, big.NewInt(TicksPerSecond))
		}
		planner.source(analysisPreviewHoldPoint{ordinal: 0, actual: 0, key: "first"}, timestamp(0))
		planner.source(analysisPreviewHoldPoint{ordinal: 1, actual: 2 * TicksPerSecond, key: "second"}, timestamp(2*TicksPerSecond))
		if err := planner.eof(); err != nil {
			t.Fatal(err)
		}
		if len(planner.points) != 2 || planner.points[0].slotCount != 2 || planner.points[1].firstSlot != 2 || planner.points[1].slotCount != 1 {
			t.Fatalf("origin %d overflowed the nominal comparison: %+v", origin, planner.points)
		}
	}
}

func TestAnalysisPreviewFinalSourceCheckRetainsAdmissionAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, finishSource, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- finalizeAnalysisPreview(ctx, func() error {
			close(entered)
			<-finishSource
			return nil
		}, func() { close(released) })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(finishSource)
		t.Fatal("final source check did not start")
	}
	cancel()
	select {
	case <-released:
		t.Error("operation admission was released before final source I/O returned")
	default:
	}
	close(finishSource)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("final deadline decision = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("final source cleanup did not finish")
	}
	select {
	case <-released:
	default:
		t.Fatal("completed source/deadline checks retained operation admission")
	}
}

func TestAnalysisPreviewFinalDeadlineIsCheckedBeforeReleaseCancelsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := finalizeAnalysisPreview(ctx, func() error { return nil }, cancel); err != nil {
		t.Fatalf("release cancellation invalidated an already completed operation: %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("release did not cancel the operation context")
	}
}

func TestAnalysisPreviewHoldDoesNotRoundAFuturePictureIntoPrecedingSlot(t *testing.T) {
	planner := analysisPreviewHoldPlanner{plan: analysisVisualPlan{end: 3 * TicksPerSecond, interval: TicksPerSecond, frames: 3}}
	planner.source(analysisPreviewHoldPoint{ordinal: 0, key: "first"}, big.NewRat(0, 1))
	// The next source picture is 50 ns after slot 1. Flooring ActualTicks must
	// not make it eligible as the preceding picture for that exact slot.
	planner.source(analysisPreviewHoldPoint{ordinal: 1, actual: TicksPerSecond, key: "second"}, big.NewRat(20_000_001, 20_000_000))
	if err := planner.eof(); err != nil {
		t.Fatal(err)
	}
	if len(planner.points) != 2 || planner.points[0].slotCount != 2 || planner.points[1].firstSlot != 2 {
		t.Fatalf("sub-tick future frame was selected: %+v", planner.points)
	}
}

func TestAnalysisPreviewHoldRequiresCleanFirstEOFAndSourceEvidence(t *testing.T) {
	for _, fixture := range []struct {
		name, transcript string
		closeErr         error
	}{
		{"canceled", previewHoldTestTranscript([]int64{0}, nil), context.Canceled},
		{"deadline", previewHoldTestTranscript([]int64{0}, nil), context.DeadlineExceeded},
		{"truncated_diagnostics", strings.TrimSuffix(previewHoldTestTranscript([]int64{0}, nil), "\n"), nil},
		{"zero_pictures", visualAnalysisConfig("source", "1/1000"), nil},
		{"missing_packet_pts", strings.Replace(previewHoldTestTranscript([]int64{0}, nil), "pkt_pts:0 ", "pkt_pts:NOPTS ", 1), nil},
		{"duplicate_source_pts", previewHoldTestTranscript([]int64{0, 0}, nil), nil},
		{"late_first_outside_item", previewHoldTestTranscript([]int64{9000}, nil), nil},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			info, stream, plan, limits, budget := previewHoldTestSetup(t, 8100*TicksPerSecond/1000, TicksPerSecond)
			log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := log.Write([]byte(fixture.transcript))
			log.Close(fixture.closeErr)
			proof, err := log.finishPlan()
			if err == nil && writeErr == nil || len(proof.points) != 0 {
				t.Fatalf("failed source produced EOF hold proof: %+v, %v / %v", proof, writeErr, err)
			}
		})
	}
}

func TestAnalysisPreviewHoldSecondPassAuditsAfterLastSelectedFrame(t *testing.T) {
	timestamps := []int64{0, 500, 1000, 1500, 2000}
	proof, _, _ := previewHoldTestPlan(t, timestamps, 2500*TicksPerSecond/1000, 10*TicksPerSecond)
	if len(proof.points) != 1 || proof.points[0].ordinal != 0 {
		t.Fatal("fixture did not select only its first source picture")
	}
	for _, fixture := range []struct{ name, transcript string }{
		{"truncated_tail", previewHoldTestTranscript(timestamps[:4], &proof)},
		{"changed_unselected_tail", previewHoldTestTranscript([]int64{0, 500, 1000, 1500, 2100}, &proof)},
		{"wrong_selected_pts", strings.Replace(previewHoldTestTranscript(timestamps, &proof), "filter_raw -> pts:0 ", "filter_raw -> pts:1 ", 1)},
		{"duplicate_output", previewHoldTestTranscript(timestamps, &proof) + visualAnalysisOutput("0", "1/1000")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			info, stream, plan, limits, budget := previewHoldTestSetup(t, 2500*TicksPerSecond/1000, 10*TicksPerSecond)
			log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, &proof)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := log.Write([]byte(fixture.transcript))
			log.Close(nil)
			if writeErr == nil && log.err == nil {
				t.Fatal("changed second-pass evidence was accepted")
			}
		})
	}
}

func TestAnalysisPreviewHoldBudgetsAreCumulative(t *testing.T) {
	proof, _, budget := previewHoldTestPlan(t, []int64{0, 3000, 7000}, 8100*TicksPerSecond/1000, TicksPerSecond)
	budget.limits.MaxSourceFrames = 5
	if err := budget.admitRepeat(proof.sourceFrames, proof.packets); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("second full decode admission = %v", err)
	}
	budget.limits.MaxSourceFrames = 6
	if err := budget.admitRepeat(proof.sourceFrames, proof.packets); err != nil {
		t.Fatal(err)
	}
	budget.limits.MaxStderrBytes = budget.bytes + 10
	if err := budget.addBytes(11); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("cumulative diagnostic budget = %v", err)
	}
	if err := budget.addRecords(4, 0); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("cumulative source frame budget = %v", err)
	}
}

func TestAnalysisPreviewHoldAcceptsAtomicRecordsSharingPhysicalLines(t *testing.T) {
	info, stream, plan, limits, budget := previewHoldTestSetup(t, 8100*TicksPerSecond/1000, TicksPerSecond)
	first, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, nil)
	if err != nil {
		t.Fatal(err)
	}
	frame := visualAnalysisFrame("source", 0, "0", 1920, 1080, "yuv420p")
	output := visualAnalysisOutput("0", "1/1000")
	transcript := previewHoldTestTranscript([]int64{0, 3000, 7000}, nil)
	transcript = strings.Replace(transcript, frame+output, strings.TrimSuffix(frame, "\n")+output, 1)
	if _, err := first.Write([]byte(transcript)); err != nil {
		t.Fatal(err)
	}
	first.Close(nil)
	proof, err := first.finishPlan()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, &proof)
	if err != nil {
		t.Fatal(err)
	}
	transcript = previewHoldTestTranscript([]int64{0, 3000, 7000}, &proof)
	transcript = strings.Replace(transcript, frame+output, strings.TrimSuffix(frame, "\n")+output, 1)
	if _, err := second.Write([]byte(transcript)); err != nil {
		t.Fatal(err)
	}
	second.Close(nil)
	if err := readAnalysisPreviewHeldFrames(context.Background(), bytes.NewReader(make([]byte, 36)), second,
		func(analysisPreviewHoldPoint, []byte) error { return nil }); err != nil || second.result() != nil {
		t.Fatalf("interleaved hold records failed: %v / %v", err, second.result())
	}
}

func TestAnalysisPreviewHoldRawBoundsAndMissingMetadataCancellation(t *testing.T) {
	proof, _, _ := previewHoldTestPlan(t, []int64{0, 3000, 7000}, 8100*TicksPerSecond/1000, TicksPerSecond)
	for _, size := range []int{0, 35, 37, 48} {
		info, stream, plan, limits, budget := previewHoldTestSetup(t, 8100*TicksPerSecond/1000, TicksPerSecond)
		log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, &proof)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte(previewHoldTestTranscript([]int64{0, 3000, 7000}, &proof))); err != nil {
			t.Fatal(err)
		}
		log.Close(nil)
		if err := readAnalysisPreviewHeldFrames(context.Background(), bytes.NewReader(make([]byte, size)), log,
			func(analysisPreviewHoldPoint, []byte) error { return nil }); err == nil {
			t.Errorf("raw preview length %d was accepted", size)
		}
	}
	info, stream, plan, limits, budget := previewHoldTestSetup(t, 8100*TicksPerSecond/1000, TicksPerSecond)
	log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, &proof)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := log.next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("metadata wait cancellation: %v", err)
	}
	log.Close(context.DeadlineExceeded)
	if _, err := log.next(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closed metadata wait: %v", err)
	}
}

func TestAnalysisPreviewHoldCommandsPreserveFullEOFAndBoundArgument(t *testing.T) {
	info, stream, plan, limits, _ := previewHoldTestSetup(t, 8100*TicksPerSecond/1000, TicksPerSecond)
	_ = info
	first, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstText := strings.Join(first, " ")
	if !strings.Contains(firstText, "select='eq(n,0)'") || !strings.Contains(firstText, "-c:v wrapped_avframe -f null pipe:1") {
		t.Fatalf("invalid full-audit pass: %s", firstText)
	}
	proof := analysisPreviewHoldProof{points: []analysisPreviewHoldPoint{{ordinal: 0}, {ordinal: 3}, {ordinal: 7}}}
	second, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, &proof)
	if err != nil {
		t.Fatal(err)
	}
	secondText := strings.Join(second, " ")
	if !strings.Contains(secondText, "select='eq(n,0)+eq(n,3)+eq(n,7)'") || !strings.Contains(secondText, "setsar=1,format=rgb24") {
		t.Fatalf("invalid unique-picture pass: %s", secondText)
	}
	for _, text := range []string{firstText, secondText} {
		for _, forbidden := range []string{"trim=", "setpts", "fps=", "tpad", "-frames", "-t ", "-ss "} {
			if strings.Contains(text, forbidden) {
				t.Errorf("full-source audit contains %q", forbidden)
			}
		}
	}
	plan.frames, limits.MaxSourceFrames = 10000, 4_000_000
	proof.points = make([]analysisPreviewHoldPoint, 10000)
	for index := range proof.points {
		proof.points[index].ordinal = 3_000_000 + index
	}
	if _, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, &proof); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("complete -vf argument limit returned %v", err)
	}
}
