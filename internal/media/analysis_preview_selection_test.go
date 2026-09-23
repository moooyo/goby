package media

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func previewSelectionTestExpression(t *testing.T, points []analysisPreviewHoldPoint) (string, string) {
	t.Helper()
	_, stream, plan, limits, _ := previewHoldTestSetup(t, int64(len(points))*TicksPerSecond, TicksPerSecond)
	plan.frames, limits.MaxSourceFrames = len(points), 4_000_000
	proof := analysisPreviewHoldProof{points: points}
	arguments, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, &proof)
	if err != nil {
		t.Fatal(err)
	}
	for index, argument := range arguments {
		if argument != "-vf" || index+1 == len(arguments) {
			continue
		}
		filter := arguments[index+1]
		_, selected, present := strings.Cut(filter, "select='")
		expression, _, closed := strings.Cut(selected, "'")
		if !present || !closed || expression == "" {
			t.Fatal("production filter did not contain its complete selection expression")
		}
		return expression, filter
	}
	t.Fatal("production command did not contain a filter")
	return "", ""
}

func TestAnalysisPreviewSelectionRetainsMaximumOrdinalSetsWithinFilterBudget(t *testing.T) {
	for _, count := range []int{1, 16, 17, 100, 101, 164, 168, 4096, 8192} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			points := make([]analysisPreviewHoldPoint, count)
			for index := range points {
				points[index].ordinal = 1_000_000 + index*2
			}
			expression, filter := previewSelectionTestExpression(t, points)
			if len(filter) >= maxAnalysisPreviewFilterBytes {
				t.Fatalf("%d selected ordinals exceed the unchanged complete-filter budget: %d", count, len(filter))
			}
			// Strip only grouping and separators, then compare every original
			// equality in order. This catches omitted, approximated or duplicated
			// ordinal predicates without reimplementing the expression parser.
			remaining := expression
			for _, point := range points {
				remaining = strings.TrimLeft(remaining, "()+")
				want := fmt.Sprintf("eq(n,%d)", point.ordinal)
				if !strings.HasPrefix(remaining, want) {
					t.Fatalf("selection changed source ordinal %d", point.ordinal)
				}
				remaining = strings.TrimPrefix(remaining, want)
			}
			if strings.Trim(remaining, "()+") != "" {
				t.Fatal("selection added predicates after the complete source set")
			}
		})
	}
}

func TestAnalysisPreviewSelectionPreservesOrdinalRejections(t *testing.T) {
	_, stream, plan, limits, _ := previewHoldTestSetup(t, 3*TicksPerSecond, TicksPerSecond)
	for _, ordinals := range [][]int{{}, {-1}, {0, 0}, {2, 1}, {limits.MaxSourceFrames}, {0, 1, 2, 3}} {
		points := make([]analysisPreviewHoldPoint, len(ordinals))
		for index, ordinal := range ordinals {
			points[index].ordinal = ordinal
		}
		if _, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, &analysisPreviewHoldProof{points: points}); !errors.Is(err, ErrAnalysisUnproven) {
			t.Errorf("invalid ordinal set %v returned %v", ordinals, err)
		}
	}
}

func previewSelectionActualFrames(ffmpeg, expression string, frames int) (mediaProcessOutput, error) {
	return runLimitedFilesOutput(context.Background(), 2*time.Minute, 2<<20, ffmpeg, nil,
		"-hide_banner", "-nostdin", "-nostats", "-v", "error", "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:size=2x2:rate=1:duration=%d", frames),
		"-vf", "select='"+expression+"'", "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter",
		"-c:v", "rawvideo", "-pix_fmt", "rgb24", "-threads:v", "1", "-f", "framecrc", "pipe:1")
}

func previewSelectionAssertFrameOrdinals(t *testing.T, output mediaProcessOutput, want []int, checksum string) {
	t.Helper()
	if len(output.stderr) != 0 {
		t.Fatalf("selection emitted decoder diagnostics: %s", output.stderr)
	}
	index, exactClock := 0, false
	for _, line := range strings.Split(string(output.stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "#tb 0: 1/1" {
			exactClock = true
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if !exactClock || len(fields) != 6 || index >= len(want) {
			t.Fatalf("unexpected selected frame record %d: %q", index, line)
		}
		values := make([]int, 5)
		for field := range values {
			var err error
			values[field], err = strconv.Atoi(strings.TrimSpace(fields[field]))
			if err != nil {
				t.Fatalf("invalid selected frame field: %q", line)
			}
		}
		if values[0] != 0 || values[1] != want[index] || values[2] != want[index] || values[3] != 1 || values[4] != 12 {
			t.Fatalf("selected frame %d has fields %v, want original ordinal %d", index, values, want[index])
		}
		if strings.TrimSpace(fields[5]) != checksum {
			t.Fatalf("selected frame %d changed the independently observed source payload checksum", index)
		}
		index++
	}
	if !exactClock || index != len(want) {
		t.Fatalf("selection emitted %d frames, want %d with the original 1/1 clock", index, len(want))
	}
}

// Exercise the actual pinned evaluator and select filter. Sparse source frame
// ordinals prove exact selection on both sides of every selected frame; the
// maximum seven-digit case separately proves the unchanged argument budget.
func TestAnalysisPreviewSelectionActualFFmpegDepthAndMaximumPlans(t *testing.T) {
	_, ffmpeg := audioProbeIntegrationTools(t)
	version, err := runLimited(context.Background(), 5*time.Second, 128<<10, ffmpeg, "-version")
	if err != nil || !analysisFFmpegVersion(string(version)) {
		t.Fatalf("FFmpeg 9.0.1 is required: %v", err)
	}
	// A constant select passes the complete small source without using the
	// production expression generator. Every black raster in the larger
	// fixtures must retain this independent raw-payload checksum.
	reference, err := previewSelectionActualFrames(ffmpeg, "1", 3)
	if err != nil {
		t.Fatalf("read the unselected fixture's payload oracle: %v", err)
	}
	checksum := ""
	for _, line := range strings.Split(string(reference.stdout), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) == 6 && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			checksum = strings.TrimSpace(fields[5])
			break
		}
	}
	if !strings.HasPrefix(checksum, "0x") {
		t.Fatal("the independent source frame checksum is absent")
	}
	if _, err := strconv.ParseUint(strings.TrimPrefix(checksum, "0x"), 16, 32); err != nil {
		t.Fatalf("the independent source frame checksum is invalid: %v", err)
	}
	previewSelectionAssertFrameOrdinals(t, reference, []int{0, 1, 2}, checksum)
	t.Run("original_flat_tree_boundary", func(t *testing.T) {
		var flat strings.Builder
		for ordinal := 0; ordinal < 100; ordinal++ {
			if ordinal > 0 {
				flat.WriteByte('+')
			}
			fmt.Fprintf(&flat, "eq(n,%d)", ordinal)
		}
		output, err := previewSelectionActualFrames(ffmpeg, flat.String(), 3)
		if err != nil {
			t.Fatalf("the original depth-100 control did not parse: %v", err)
		}
		previewSelectionAssertFrameOrdinals(t, output, []int{0, 1, 2}, checksum)
		flat.WriteString("+eq(n,100)")
		if _, err := previewSelectionActualFrames(ffmpeg, flat.String(), 3); err == nil || !strings.Contains(err.Error(), "Error while parsing expression") {
			t.Fatalf("the original excessive-depth control did not reproduce the select parser failure: %v", err)
		}
	})
	for _, count := range []int{17, 100, 101, 164, 168, 4096, 8192} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			points, want := make([]analysisPreviewHoldPoint, count), make([]int, count)
			for index := range points {
				points[index].ordinal = index*2 + 1
				want[index] = points[index].ordinal
			}
			expression, _ := previewSelectionTestExpression(t, points)
			output, err := previewSelectionActualFrames(ffmpeg, expression, count*2+1)
			if err != nil {
				t.Fatalf("production selection of %d source frames failed: %v", count, err)
			}
			previewSelectionAssertFrameOrdinals(t, output, want, checksum)
		})
	}
	t.Run("8192_seven_digit_ordinals", func(t *testing.T) {
		points := make([]analysisPreviewHoldPoint, 8192)
		points[0].ordinal, points[1].ordinal = 0, 2
		for index := 2; index < len(points); index++ {
			points[index].ordinal = 1_000_000 + index*2
		}
		expression, _ := previewSelectionTestExpression(t, points)
		output, err := previewSelectionActualFrames(ffmpeg, expression, 4)
		if err != nil {
			t.Fatalf("maximum production expression did not parse and evaluate: %v", err)
		}
		previewSelectionAssertFrameOrdinals(t, output, []int{0, 2}, checksum)
	})
}
