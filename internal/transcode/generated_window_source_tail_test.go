package transcode

import (
	"errors"
	"strings"
	"testing"
)

func generatedMP4SourceTailTestPlanAndCertificate() (Plan, GeneratedSourceEndpointCertificate) {
	plan := generatedSourceRangeTestPlan()
	plan.StartTicks, plan.HLS.Window.EndTicks = 94*ticksPerSecond, 100*ticksPerSecond
	cert := GeneratedSourceEndpointCertificate{
		SourceIdentity: "held-source-identity", StreamIndex: 0, TrackID: 1, SampleCount: 2500,
		FrameDuration: GeneratedRational{Num: 1, Den: 25}, Origin: GeneratedRational{Num: 2, Den: 1},
		Last: GeneratedRational{Num: 2549, Den: 25}, End: GeneratedRational{Num: 102, Den: 1},
		DurationTicks: 100 * ticksPerSecond, DurationTicksExact: true,
		MetadataSHA256: [32]byte{1}, SampleExtentsSHA256: [32]byte{2},
	}
	return plan, cert
}

func TestGeneratedMP4SourceTailUsesAbsoluteEndpointAndRelativeCoverage(t *testing.T) {
	plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
	origin, err := generatedMP4SourceTailPlan(plan, cert)
	if err != nil || origin != 2*ticksPerSecond || plan.DurationTicks == cert.DurationTicks {
		t.Fatalf("tail plan changed the independently declared endpoint: origin=%d, %v", origin, err)
	}
	want := GeneratedSourceRange{StartTicks: 94 * ticksPerSecond, EndTicks: 100 * ticksPerSecond, FrameCount: 150,
		First: GeneratedRational{Num: 94, Den: 1}, Last: GeneratedRational{Num: 2499, Den: 25},
		End: GeneratedRational{Num: 100, Den: 1}, FrameDuration: GeneratedRational{Num: 1, Den: 25}}
	for name, frames := range map[string]string{
		"exact tail": generatedSourceRangeFrames(96_000, 40, 150),
		"preroll":    generatedSourceRangeFrames(95_920, 40, 152),
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte(generatedSourceRangeDocumentJSON(frames, "1/1000", "2.000000"))
			got, err := parseGeneratedMP4SourceTail(data, plan, cert)
			if err != nil || got != want {
				t.Fatalf("absolute source endpoint or relative coverage changed: %+v, %v", got, err)
			}
		})
	}
	// Mathematical equality does not require the caller to pre-reduce clocks.
	cert.Origin, cert.Last, cert.End, cert.FrameDuration = GeneratedRational{Num: 4, Den: 2}, GeneratedRational{Num: 5098, Den: 50}, GeneratedRational{Num: 204, Den: 2}, GeneratedRational{Num: 2, Den: 50}
	if origin, err := generatedMP4SourceTailPlan(plan, cert); err != nil || origin != 2*ticksPerSecond {
		t.Fatalf("equivalent rational certificate was rejected: %d, %v", origin, err)
	}
}

func TestGeneratedMP4SourceTailRejectsFramesOutsideDeclaredSampleSet(t *testing.T) {
	plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
	frames := generatedSourceRangeFrames(96_000, 40, 150)
	extra := []byte(generatedSourceRangeDocumentJSON(frames+","+generatedSourceRangeFrameJSON(102_000, 40), "1/1000", "2.000000"))
	// The interval-only parser may ignore a complete postroll record. The tail
	// parser must still inspect it against the independent finite endpoint.
	if _, err := parseGeneratedSourceRange(extra, plan, 2*ticksPerSecond); err != nil {
		t.Fatalf("fixture does not exercise ignored interval postroll: %v", err)
	}
	if got, err := parseGeneratedMP4SourceTail(extra, plan, cert); got != (GeneratedSourceRange{}) || !errors.Is(err, ErrInvalidTimeline) {
		t.Fatalf("frame at the declared endpoint was silently ignored: %+v, %v", got, err)
	}
	for name, data := range map[string]string{
		"extra frame beyond endpoint": generatedSourceRangeDocumentJSON(frames+","+generatedSourceRangeFrameJSON(102_040, 40), "1/1000", "2.000000"),
		"missing final frame":         generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(96_000, 40, 149), "1/1000", "2.000000"),
		"partial final duration":      generatedSourceRangeDocumentJSON(strings.Replace(frames, `"pts":101960,"duration":40`, `"pts":101960,"duration":20`, 1), "1/1000", "2.000000"),
		"extended final duration":     generatedSourceRangeDocumentJSON(strings.Replace(frames, `"pts":101960,"duration":40`, `"pts":101960,"duration":80`, 1), "1/1000", "2.000000"),
		"preroll before source epoch": generatedSourceRangeDocumentJSON(generatedSourceRangeFrameJSON(1960, 40)+","+frames, "1/1000", "2.000000"),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := parseGeneratedMP4SourceTail([]byte(data), plan, cert); got != (GeneratedSourceRange{}) || !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("inconsistent decoded tail was accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestGeneratedMP4SourceTailRetainsWholeStrictSourceSchema(t *testing.T) {
	plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
	frames := generatedSourceRangeFrames(96_000, 40, 150)
	valid := generatedSourceRangeDocumentJSON(frames, "1/1000", "2.000000")
	for name, data := range map[string]string{
		"extra access unit missing pts": generatedSourceRangeDocumentJSON(frames+`,{"media_type":"video","stream_index":0,"duration":40}`, "1/1000", "2.000000"),
		"extra access unit null pts":    generatedSourceRangeDocumentJSON(frames+`,{"media_type":"video","stream_index":0,"pts":null,"duration":40}`, "1/1000", "2.000000"),
		"duplicate raw pts":             strings.Replace(valid, `"pts":96000`, `"pts":96000,"pts":96000`, 1),
		"best effort substitute":        strings.Replace(valid, `"pts":96000`, `"best_effort_timestamp":96000`, 1),
		"unknown root":                  strings.TrimSuffix(valid, "}") + `,"source_eof":true}`,
		"multiple documents":            valid + valid,
		"unknown frame field":           strings.Replace(valid, `"pts":96000`, `"pts":96000,"eof":true`, 1),
		"extra stream":                  strings.Replace(valid, `"streams":[`, `"streams":[{"index":1,"codec_type":"video","time_base":"1/1000"},`, 1),
		"truncated document":            strings.TrimSuffix(valid, "}"),
		"foreign access unit":           generatedSourceRangeDocumentJSON(frames+`,{"media_type":"audio","stream_index":1,"pts":102000,"duration":40}`, "1/1000", "2.000000"),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := parseGeneratedMP4SourceTail([]byte(data), plan, cert); got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("noncanonical or incomplete source schema was accepted: %+v, %v", got, err)
			}
		})
	}
	if _, err := parseGeneratedMP4SourceTail([]byte(strings.Repeat(" ", maxGeneratedSourceRangeBytes+1)), plan, cert); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("tail output escaped the shared byte budget: %v", err)
	}
	excess := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(2000, 40, maxGeneratedSourceRangeFrames+1), "1/1000", "2.000000")
	if _, err := parseGeneratedMP4SourceTail([]byte(excess), plan, cert); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("all-record frame budget was not retained: %v", err)
	}
}

func TestGeneratedMP4SourceTailRejectsInconsistentEndpointCertificate(t *testing.T) {
	for name, mutate := range map[string]func(*GeneratedSourceEndpointCertificate){
		"wrong stream":       func(c *GeneratedSourceEndpointCertificate) { c.StreamIndex = 1 },
		"absent track":       func(c *GeneratedSourceEndpointCertificate) { c.TrackID = 0 },
		"absent metadata":    func(c *GeneratedSourceEndpointCertificate) { c.MetadataSHA256 = [32]byte{} },
		"absent extents":     func(c *GeneratedSourceEndpointCertificate) { c.SampleExtentsSHA256 = [32]byte{} },
		"empty sample set":   func(c *GeneratedSourceEndpointCertificate) { c.SampleCount = 0 },
		"excess samples":     func(c *GeneratedSourceEndpointCertificate) { c.SampleCount = generatedMP4MaxSamples + 1 },
		"wrong sample count": func(c *GeneratedSourceEndpointCertificate) { c.SampleCount++ },
		"inexact ticks":      func(c *GeneratedSourceEndpointCertificate) { c.DurationTicksExact = false },
		"zero duration":      func(c *GeneratedSourceEndpointCertificate) { c.DurationTicks = 0 },
		"wrong duration":     func(c *GeneratedSourceEndpointCertificate) { c.DurationTicks-- },
		"wrong endpoint":     func(c *GeneratedSourceEndpointCertificate) { c.End.Num++ },
		"wrong last":         func(c *GeneratedSourceEndpointCertificate) { c.Last.Num-- },
		"wrong period":       func(c *GeneratedSourceEndpointCertificate) { c.FrameDuration.Den = 24 },
		"zero period":        func(c *GeneratedSourceEndpointCertificate) { c.FrameDuration.Num = 0 },
		"zero origin base":   func(c *GeneratedSourceEndpointCertificate) { c.Origin.Den = 0 },
		"negative last base": func(c *GeneratedSourceEndpointCertificate) { c.Last.Den = -25 },
		"zero end base":      func(c *GeneratedSourceEndpointCertificate) { c.End.Den = 0 },
		"zero period base":   func(c *GeneratedSourceEndpointCertificate) { c.FrameDuration.Den = 0 },
		"inexact origin ticks": func(c *GeneratedSourceEndpointCertificate) {
			c.Origin, c.Last, c.End = GeneratedRational{Num: 7, Den: 3}, GeneratedRational{Num: 7672, Den: 75}, GeneratedRational{Num: 307, Den: 3}
		},
		"negative origin": func(c *GeneratedSourceEndpointCertificate) {
			c.Origin, c.Last, c.End = GeneratedRational{Num: -1, Den: 1}, GeneratedRational{Num: 2474, Den: 25}, GeneratedRational{Num: 99, Den: 1}
		},
		"absolute endpoint exceeds profile": func(c *GeneratedSourceEndpointCertificate) {
			origin := generatedMP4MaxSeconds - 99
			c.Origin, c.Last, c.End = GeneratedRational{Num: origin, Den: 1}, GeneratedRational{Num: (origin+100)*25 - 1, Den: 25}, GeneratedRational{Num: origin + 100, Den: 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
			mutate(&cert)
			if _, err := generatedMP4SourceTailPlan(plan, cert); !errors.Is(err, ErrInvalidTimeline) {
				t.Fatalf("inconsistent endpoint certificate was accepted: %v", err)
			}
		})
	}
	plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
	cert.SourceIdentity = ""
	if _, err := generatedMP4SourceTailPlan(plan, cert); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("absent held-source identity was accepted: %v", err)
	}
}

func TestGeneratedMP4SourceTailRequiresSupportedBoundedEvidencePlan(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"no input evidence": func(p *Plan) { p.HLS.Window.RequireInputEvidence = false },
		"wrong stream":      func(p *Plan) { p.VideoStreamIndex = 1 },
		"copied video":      func(p *Plan) { p.VideoCodec, p.VideoCopyCodec = "copy", "h264" },
		"audio":             func(p *Plan) { p.AudioStreamIndex, p.AudioCodec = 1, "aac" },
		"fractional period": func(p *Plan) { p.FrameRate = 24_000.0 / 1001 },
		"invalid duration":  func(p *Plan) { p.DurationTicks = 99 * ticksPerSecond },
	} {
		t.Run(name, func(t *testing.T) {
			plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
			mutate(&plan)
			if _, err := generatedMP4SourceTailPlan(plan, cert); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unsupported tail evidence plan was accepted: %v", err)
			}
		})
	}
	plan, cert := generatedMP4SourceTailTestPlanAndCertificate()
	plan.HLS.Window.EndTicks = 99 * ticksPerSecond
	if _, err := generatedMP4SourceTailPlan(plan, cert); !errors.Is(err, ErrInvalidTimeline) {
		t.Fatalf("window not ending at the declared sample endpoint was accepted: %v", err)
	}
	plan, cert = generatedMP4SourceTailTestPlanAndCertificate()
	plan.StartTicks = 0
	if _, err := generatedMP4SourceTailPlan(plan, cert); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("unbounded decoded-source interval was accepted: %v", err)
	}
}
