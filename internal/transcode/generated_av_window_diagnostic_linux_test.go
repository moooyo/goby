//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGeneratedAVDiagnosticOutputMonitorChecksAfterSamplerExit(t *testing.T) {
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	monitor, err := newGeneratedAVDiagnosticOutputMonitor(ctx, directory, 16, cancel)
	if err != nil {
		t.Fatal(err)
	}
	<-monitor.done
	// Simulate a final child write after ctx stopped the sampling goroutine.
	if err := os.WriteFile(filepath.Join(directory, "segment.ts"), make([]byte, 17), 0600); err != nil {
		t.Fatal(err)
	}
	monitor.finish()
	if !errors.Is(monitor.failure(), ErrTimelineLimit) {
		t.Fatal("post-join output extent escaped final validation")
	}
	if err := generatedAVDiagnosticOutputExtent(directory, 0); !errors.Is(err, ErrTimelineLimit) {
		t.Fatal("zero remaining diagnostic budget passed preflight")
	}
}

func generatedAVDiagnosticTestPlan() Plan {
	plan := Plan{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", VideoStreamIndex: 0, AudioStreamIndex: 1, Width: 160, Height: 96,
		FrameRate: 24, VideoBitrate: 512000, AudioBitrate: 96000, AudioChannels: 2, AudioSampleRate: 48000, DurationTicks: 48 * ticksPerSecond,
		SegmentSeconds: 6, HLS: HLSPlan{SegmentType: "mpegts", RenditionCount: 4}}
	for index, rendition := range []HLSRendition{{Width: 160, Height: 96, VideoBitrate: 512000}, {Width: 120, Height: 72, VideoBitrate: 288000}, {Width: 80, Height: 48, VideoBitrate: 128000}, {Width: 40, Height: 24, VideoBitrate: 64000}} {
		plan.HLS.Renditions[index] = rendition
	}
	return plan
}

func generatedAVDiagnosticTestOptions() GeneratedAVWindowDiagnosticOptions {
	return GeneratedAVWindowDiagnosticOptions{NegotiatedPlan: generatedAVDiagnosticTestPlan(), EndTicks: 24 * ticksPerSecond,
		AcquireProbe: func(context.Context) (func(), error) { return func() {}, nil }, Baseline: func(context.Context, *os.File, Plan) (GeneratedAVDiagnosticBaseline, error) {
			return GeneratedAVDiagnosticBaseline{}, ErrUnsupported
		}}
}

func TestGeneratedAVDiagnosticWindowPlanKeepsRealNegotiatedLadder(t *testing.T) {
	options := generatedAVDiagnosticTestOptions()
	window, err := generatedAVDiagnosticWindowPlan(options)
	if err != nil {
		t.Fatal(err)
	}
	if window.HLS.RenditionCount != 4 || window.HLS.Renditions != options.NegotiatedPlan.HLS.Renditions || window.HLS.Window.RequireInputEvidence || window.HLS.Window.NativeClockVersion != 0 {
		t.Fatal("diagnostic changed ladder or granted silent qualification")
	}
	window.StartTicks, window.HLS.Window = 0, HLSWindow{}
	if window != options.NegotiatedPlan {
		t.Fatal("diagnostic normalized original metadata or execution")
	}
	for _, mutate := range []func(*GeneratedAVWindowDiagnosticOptions){
		func(o *GeneratedAVWindowDiagnosticOptions) { o.NegotiatedPlan.AudioCodec = "copy" },
		func(o *GeneratedAVWindowDiagnosticOptions) { o.NegotiatedPlan.HLS.Window.RequireInputEvidence = true },
		func(o *GeneratedAVWindowDiagnosticOptions) { o.NegotiatedPlan.AudioSampleRate = 44100 },
		func(o *GeneratedAVWindowDiagnosticOptions) { o.NegotiatedPlan.SegmentSeconds = 24 },
		func(o *GeneratedAVWindowDiagnosticOptions) { o.AcquireProbe = nil },
	} {
		bad := generatedAVDiagnosticTestOptions()
		mutate(&bad)
		if _, err := generatedAVDiagnosticWindowPlan(bad); err == nil {
			t.Fatal("unsupported actual diagnostic domain was accepted")
		}
	}
}

func TestGeneratedAVObservedArgumentsOnlyAddsStatsObservers(t *testing.T) {
	options := generatedAVDiagnosticTestOptions()
	plan, err := generatedAVDiagnosticWindowPlan(options)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := BuildArgs(plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	observed, delta, err := generatedAVObservedArguments(plan, baseline)
	if err != nil || len(delta) == 0 {
		t.Fatal("observation delta is absent")
	}
	var recovered []string
	for index := 0; index < len(observed); index++ {
		flag := observed[index]
		switch flag {
		case "-stats_enc_pre:v:0", "-stats_enc_pre_fmt:v:0", "-stats_enc_pre:a:0", "-stats_enc_pre_fmt:a:0", "-stats_mux_pre:a:0", "-stats_mux_pre_fmt:a:0":
			index++
			continue
		case "-stats_mux_pre_fmt:v:0":
			if index+1 >= len(observed) || observed[index+1] != generatedAVPreMuxFormat {
				t.Fatal("unexpected reference stats delta")
			}
			recovered = append(recovered, flag, "GOBY {fidx} {n} {tb} {pts}")
			index++
			continue
		}
		recovered = append(recovered, flag)
	}
	if !reflect.DeepEqual(recovered, baseline) {
		t.Fatal("stats delta changed media parameters, order or natural ladder")
	}
}

func TestGeneratedAVDiagnosticComparisonSeparatesBytesFromPacketClocks(t *testing.T) {
	plan := generatedAVDiagnosticTestPlan()
	left := GeneratedAVDiagnosticRun{Plan: plan, Renditions: []GeneratedAVDiagnosticRendition{{Variant: 0, Cuts: []GeneratedAVDiagnosticCut{{Name: "v0-segment-000000.ts", Number: 0, DurationTicks: 6 * ticksPerSecond}}}}}
	right := left
	right.Renditions = append([]GeneratedAVDiagnosticRendition(nil), left.Renditions...)
	right.Renditions[0].Cuts = append([]GeneratedAVDiagnosticCut(nil), left.Renditions[0].Cuts...)
	right.Renditions[0].Cuts[0].Transport.SHA256[0] = 1
	right.Renditions[0].Cuts[0].Packets.SegmentSHA256[0] = 1
	got := generatedAVDiagnosticCompare(left, right, plan)
	if got.MediaBytesEqual || !got.PacketClocksEqual || !got.PhysicalCutsEqual || !got.NegotiatedLadderPreserved {
		t.Fatal("media digest difference was misreported as a packet clock change")
	}
	right.Renditions[0].Cuts[0].Packets.Audio.FirstPTS = 1
	got = generatedAVDiagnosticCompare(left, right, plan)
	if got.PacketClocksEqual {
		t.Fatal("actual packet clock mismatch was ignored")
	}
}
