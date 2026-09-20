package media

import (
	"bytes"
	"errors"
	"image/jpeg"
	"strings"
	"testing"
)

func TestAnalysisPreviewPlanAdmitsOnlyFixedBoundedVariants(t *testing.T) {
	info, stream := visualAnalysisTestInfo()
	info.DurationTicks = 61 * TicksPerSecond
	limits := DefaultAnalysisLimits()
	for _, width := range []int{0, 240, 320, 400} {
		plan, err := analysisPreviewOptions(info, stream, PreviewAnalysisOptions{Width: width}, limits)
		if err != nil || plan.frames != 7 || plan.interval != 10*TicksPerSecond || plan.channels != 3 || plan.pixelFormat != "rgb24" {
			t.Fatalf("width %d plan = %+v, %v", width, plan, err)
		}
		if plan.width == 320 && plan.height != 180 {
			t.Fatalf("aspect ratio changed: %+v", plan)
		}
	}
	for _, options := range []PreviewAnalysisOptions{{Width: 1}, {Width: 319}, {Width: 401}, {IntervalTicks: -1}, {IntervalTicks: TicksPerSecond - 1}, {IntervalTicks: 601 * TicksPerSecond}} {
		if _, err := analysisPreviewOptions(info, stream, options, limits); err == nil {
			t.Fatalf("invalid preview options %+v were admitted", options)
		}
	}
	for _, kind := range []string{"frames", "pixels", "raw_bytes", "duration"} {
		t.Run(kind, func(t *testing.T) {
			bounded := limits
			copyInfo := info
			switch kind {
			case "frames":
				bounded.MaxPreviewFrames = 6
			case "pixels":
				bounded.MaxFramePixels = 320*180 - 1
			case "raw_bytes":
				bounded.MaxRawBytes = 7*320*180*3 - 1
			case "duration":
				copyInfo.DurationTicks = MaxAnalysisDurationTicks + 1
			}
			if _, err := analysisPreviewOptions(copyInfo, stream, PreviewAnalysisOptions{}, bounded); err == nil {
				t.Fatal("preview budget was ignored")
			}
		})
	}
}

func TestAnalysisVisualWindowPlanAndDescriptorOnlyArguments(t *testing.T) {
	info, stream := visualAnalysisTestInfo()
	info.DurationTicks = 1000 * TicksPerSecond
	limits := DefaultAnalysisLimits()
	plan, err := analysisVisualOptions(info, VisualAnalysisOptions{}, limits)
	if err != nil || plan.end != 600*TicksPerSecond || plan.frames != 1200 {
		t.Fatalf("default prefix = %+v, %v", plan, err)
	}
	plan, err = analysisVisualOptions(info, VisualAnalysisOptions{StartTicks: 125 * TicksPerSecond, EndTicks: 130 * TicksPerSecond, IntervalTicks: TicksPerSecond / 10}, limits)
	if err != nil || plan.frames != 50 {
		t.Fatalf("refinement window = %+v, %v", plan, err)
	}
	args := strings.Join(buildAnalysisVisualArgs(info, stream, plan, limits), " ")
	for _, fragment := range []string{"-copyts", "-debug_ts", "+nofillin-genpts", "-i /proc/self/fd/3", "-map 0:2", "trim=end=131.0000000", "gte(t,126.0000000+selected_n*0.1000000)", "-fps_mode:v passthrough", "-enc_time_base:v filter", "-threads 1", "-threads:v 1"} {
		if !strings.Contains(args, fragment) {
			t.Errorf("missing command constraint %q: %s", fragment, args)
		}
	}
	for _, forbidden := range []string{"setpts", "fps=", "-start_at_zero", "-ss ", "-r "} {
		if strings.Contains(args, forbidden) {
			t.Errorf("timestamp-changing argument %q is present", forbidden)
		}
	}
	for _, options := range []VisualAnalysisOptions{{StartTicks: -1}, {EndTicks: 601 * TicksPerSecond}, {StartTicks: TicksPerSecond, EndTicks: TicksPerSecond}, {IntervalTicks: TicksPerSecond/10 - 1}} {
		if _, err := analysisVisualOptions(info, options, limits); err == nil {
			t.Fatalf("invalid visual options %+v were admitted", options)
		}
	}
}

func TestAnalysisPreviewEncodesOneBoundedRGBRaster(t *testing.T) {
	pixels := make([]byte, 8*6*3)
	for index := 0; index < len(pixels); index += 3 {
		pixels[index], pixels[index+1], pixels[index+2] = 230, 30, 10
	}
	frame := analysisRGBImage{pixels: pixels, width: 8, height: 6}
	buffer := analysisJPEGBuffer{limit: 4096}
	if err := jpeg.Encode(&buffer, frame, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(buffer.buffer.Bytes()))
	if err != nil || config.Width != 8 || config.Height != 6 {
		t.Fatalf("JPEG raster = %+v, %v", config, err)
	}
	bounded := analysisJPEGBuffer{limit: int64(buffer.buffer.Len() - 1)}
	if err := jpeg.Encode(&bounded, frame, &jpeg.Options{Quality: 80}); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("single JPEG budget returned %v", err)
	}
	if int64(bounded.buffer.Len()) > bounded.limit {
		t.Fatal("the JPEG writer retained excess bytes")
	}
	zero := analysisJPEGBuffer{limit: 0}
	if _, err := zero.Write([]byte{1}); !errors.Is(err, ErrAnalysisBudget) || zero.buffer.Len() != 0 {
		t.Fatalf("exhausted total JPEG budget returned %v", err)
	}
}
