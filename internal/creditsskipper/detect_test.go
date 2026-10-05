// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type testProbe struct {
	evidence                                                                    KeyframeEvidence
	intervals                                                                   []Range
	boundary                                                                    []BlackFrame
	silence                                                                     []Range
	keyframes                                                                   []float64
	err                                                                         error
	keyframeCalls, intervalCalls, boundaryCalls, silenceCalls, endKeyframeCalls int
	onKeyframes                                                                 func()
}

func (p *testProbe) ScanKeyframes(context.Context, Range, int) (KeyframeEvidence, error) {
	p.keyframeCalls++
	if p.onKeyframes != nil {
		p.onKeyframes()
	}
	return p.evidence, p.err
}
func (p *testProbe) ScanBlackIntervals(context.Context, Range, int, int) ([]Range, error) {
	p.intervalCalls++
	return p.intervals, p.err
}
func (p *testProbe) ScanBoundary(context.Context, Range, int, int) ([]BlackFrame, error) {
	p.boundaryCalls++
	return p.boundary, p.err
}
func (p *testProbe) ScanSilence(context.Context, Range) ([]Range, error) {
	p.silenceCalls++
	return p.silence, p.err
}
func (p *testProbe) ScanKeyframesAtBoundary(context.Context, Range) ([]float64, error) {
	p.endKeyframeCalls++
	return p.keyframes, p.err
}

func TestMovieChapterPassDoesNotRequireSharedAudio(t *testing.T) {
	probe := &testProbe{}
	got, err := Detect(context.Background(), Request{DurationSeconds: 2000, IsMovie: true, Chapters: []Chapter{{"Main", 0}, {"Credits", 1400}}}, probe)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != Version || got.UpstreamCommit != UpstreamCommit || !reflect.DeepEqual(got.Segments, []Segment{{1400, 2000, ChapterSource}}) || probe.keyframeCalls != 1 {
		t.Fatalf("movie pass: %+v %+v", got, probe)
	}
	_, err = Detect(context.Background(), Request{DurationSeconds: 2000, IsMovie: true, AudioSegments: []Segment{{1800, 2000, ChromaprintSource}}}, probe)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("movie accepted cross-item audio: %v", err)
	}
}

func TestPassKeepsSeparatedNativeSegmentsAndEvidence(t *testing.T) {
	probe := &testProbe{evidence: KeyframeEvidence{Visuals: visualSequence(401, 449, 2, true)}}
	request := Request{DurationSeconds: 450, Chapters: []Chapter{{"Main", 0}, {"Ending", 90}, {"Epilogue", 179}}}
	got, err := Detect(context.Background(), request, probe)
	if err != nil {
		t.Fatal(err)
	}
	want := []Segment{{90, 179, ChapterSource}, {401, 450, BlackFrameSource}}
	if !reflect.DeepEqual(got.Segments, want) || len(got.Evidence.RawCandidates) != 2 || !reflect.DeepEqual(got.Evidence.HardBoundaries, []float64{179}) || got.Evidence.VisualMethod != "Entropy" || probe.silenceCalls != 1 || probe.endKeyframeCalls != 1 {
		t.Fatalf("native intervals/evidence lost: %+v %+v", got, probe)
	}
}

func TestPassFailureAndCancellationNeverReturnNoMatch(t *testing.T) {
	sentinel := errors.New("source read failed")
	result, err := Detect(context.Background(), Request{DurationSeconds: 600}, &testProbe{err: sentinel})
	if !errors.Is(err, sentinel) || result.Version != "" {
		t.Fatalf("probe failure became result: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, err = Detect(ctx, Request{DurationSeconds: 600}, &testProbe{onKeyframes: cancel})
	if !errors.Is(err, context.Canceled) || result.Version != "" {
		t.Fatalf("cancellation became result: %+v %v", result, err)
	}
}

func TestPassRetainsNativeBoundaryKeyframeOutsideRequestedWindow(t *testing.T) {
	probe := &testProbe{keyframes: []float64{441}}
	got, err := Detect(context.Background(), Request{DurationSeconds: 500, Chapters: []Chapter{{"Main", 0}, {"Ending", 350}, {"Preview", 438}}}, probe)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Segments) != 1 || got.Segments[0].End != 441 || got.Evidence.Adjustments[0].SearchRange.End != 440 {
		t.Fatalf("native nokey overshoot changed: %+v", got)
	}
}

func TestPassRejectsUnorderedOrOutOfSourceEvidence(t *testing.T) {
	for _, evidence := range []KeyframeEvidence{
		{BlackFrames: []BlackFrame{{0, 99, 20}, {1, 99, 10}}},
		{BlackFrames: []BlackFrame{{0, 99, 451}}},
		{Visuals: []KeyframeVisual{{10, 1.1, 0}}},
	} {
		if _, err := Detect(context.Background(), Request{DurationSeconds: 600}, &testProbe{evidence: evidence}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %+v: %v", evidence, err)
		}
	}
}
