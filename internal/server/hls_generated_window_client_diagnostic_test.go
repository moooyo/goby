//go:build linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type generatedWindowSeekDiagnosticHTTP struct {
	Kind, ProducerID string
	Number, Variant  int
	Status           int
}

type generatedWindowSeekDiagnosticResponse struct {
	http.ResponseWriter
	status int
}

func (writer *generatedWindowSeekDiagnosticResponse) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
		writer.ResponseWriter.WriteHeader(status)
	}
}

func (writer *generatedWindowSeekDiagnosticResponse) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *generatedWindowSeekDiagnosticResponse) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// This owned diagnostic preserves the unmodified full-VOD -ss 90 invocation.
// A completed observation is not a playback compatibility assertion: the
// separate client acceptance continues to require 144 correct decoded frames.
func TestHTTPGeneratedWindowFFmpegNativeSeekDiagnostic(t *testing.T) {
	directory := os.Getenv("GOBY_GENERATED_WINDOW_DIAGNOSTIC_DIR")
	if directory == "" {
		t.Skip("GOBY_GENERATED_WINDOW_DIAGNOSTIC_DIR admits the private native-seek diagnostic")
	}
	info, err := os.Stat(directory)
	if err != nil || !filepath.IsAbs(directory) || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatal("native-seek diagnostic requires an absolute private evidence directory")
	}
	output, err := os.MkdirTemp(directory, "native-seek-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create native-seek private evidence directory")
	}
	h := newHLSGeneratedWindowHTTPFixture(t)
	graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	var mu sync.Mutex
	var requests []generatedWindowSeekDiagnosticHTTP
	overflow := false
	clientServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		fact := generatedWindowSeekDiagnosticHTTP{Number: -1, Variant: -1}
		if number, variant, logical := hlsGeneratedWindowVariantNumber(name, len(graph.mainURLs)); logical {
			fact.Kind, fact.Number, fact.Variant = "logical", number, variant
		} else if id := r.URL.Query().Get(hlsProducerQuery); id != "" && hlsPlanArtifact(graph.session.key.plan, name) {
			fact.Kind, fact.ProducerID = "exact", id
		}
		writer := &generatedWindowSeekDiagnosticResponse{ResponseWriter: w}
		h.f.handler.ServeHTTP(writer, r)
		if fact.Kind != "" {
			fact.Status = writer.status
			mu.Lock()
			if len(requests) < 256 {
				requests = append(requests, fact)
			} else {
				overflow = true
			}
			mu.Unlock()
		}
	}))
	defer clientServer.Close()
	ctx, cancel := context.WithTimeout(h.f.ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-filter_threads", "1",
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto", "-ss", "90", "-i", clientServer.URL+graph.mainURLs[0],
		"-t", "6", "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	primary := graph.session.key.plan.HLS.Renditions[0]
	decoded := &hlsGeneratedWindowHTTPClientOutput{limit: 168 * primary.Width * primary.Height * 3, cancel: cancel}
	diagnostics := &hlsGeneratedWindowHTTPClientOutput{limit: 64 << 10, cancel: cancel}
	command.Stdout, command.Stderr = decoded, diagnostics
	commandErr := command.Run()
	mu.Lock()
	trace := append([]generatedWindowSeekDiagnosticHTTP(nil), requests...)
	traceOverflow := overflow
	mu.Unlock()
	type jobObservation struct {
		ID, State            string
		StartTicks, EndTicks int64
		Number               int
		Premux               []transcode.HLSMuxClock
		Media                []transcode.GeneratedSegmentBounds
	}
	var jobs []jobObservation
	for _, id := range hlsGeneratedWindowHTTPJobIDs(t, h, graph) {
		state, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, id)
		if err != nil || state.State != "completed" {
			t.Fatal("native-seek diagnostic encountered an unfinished private producer")
		}
		job := jobObservation{ID: id, State: state.State, StartTicks: state.Spec.Plan.StartTicks,
			EndTicks: state.Spec.Plan.HLS.Window.EndTicks, Number: state.Spec.Plan.HLS.Window.StartNumber}
		clockJobs, ok := h.f.app.hls.manager.(hlsGeneratedWindowJobs)
		if !ok {
			t.Fatal("native-seek diagnostic lacks actual premux clocks")
		}
		for variant := 0; variant < state.Spec.Plan.HLS.RenditionCount; variant++ {
			clock, err := clockJobs.HLSClock(ctx, graph.session.key.scope, id, variant)
			if err != nil {
				t.Fatal("read native-seek diagnostic premux clock")
			}
			name := fmt.Sprintf("v%d-segment-%06d.ts", variant, job.Number)
			reader, err := h.f.app.hls.manager.TryOpen(graph.session.key.scope, id, name)
			if err != nil {
				t.Fatal("pin native-seek diagnostic actual media")
			}
			bounds, measureErr := transcode.MeasureGeneratedSegmentBounds(ctx, h.ffprobe, nil, reader.File, true)
			closeErr := reader.Close()
			if measureErr != nil || closeErr != nil {
				t.Fatal("observe native-seek diagnostic actual media bounds")
			}
			job.Premux, job.Media = append(job.Premux, clock), append(job.Media, bounds)
		}
		jobs = append(jobs, job)
	}
	result := map[string]any{"Marker": "goby-generated-window-native-seek-diagnostic-v1", "PlaybackCompatibilityAsserted": false,
		"CompleteObservation": true, "DecodedBytes": decoded.buffer.Len(), "DecodedFrames": decoded.buffer.Len() / (primary.Width * primary.Height * 3),
		"DiagnosticBytes": diagnostics.buffer.Len(), "CommandErrorType": fmt.Sprintf("%T", commandErr), "Requests": trace, "RequestOverflow": traceOverflow,
		"NativeDurationTicks": 100 * media.TicksPerSecond, "SourceOriginTicks": h.item.Media.FormatStartTicks, "Jobs": jobs}
	// Token-bearing command errors and diagnostics are deliberately excluded.
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil || len(data) > 256<<10 || os.WriteFile(filepath.Join(output, "native-seek-facts.json"), append(data, '\n'), 0o600) != nil {
		t.Fatal("preserve bounded native-seek diagnostic facts")
	}
	t.Logf("native_seek_observation_complete=true playback_compatibility_asserted=false decoded_bytes=%d decoded_frames=%d request_count=%d evidence_directory=%s",
		decoded.buffer.Len(), decoded.buffer.Len()/(primary.Width*primary.Height*3), len(trace), output)
}
