//go:build linux

package server

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

// This fixture intentionally uses only pre-admission public runtime methods so
// the exact same file can profile the previous and candidate implementations.
type hlsAdmissionProfileJobs struct {
	*hlsRuntimeTestJobs
	entered  chan struct{}
	finished chan struct{}
	delay    time.Duration
}

func (jobs *hlsAdmissionProfileJobs) Ensure(_ context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	if err := input.Close(); err != nil {
		return transcode.Record{}, err
	}
	close(jobs.entered)
	timer := time.NewTimer(jobs.delay)
	<-timer.C
	close(jobs.finished)
	return transcode.Record{ID: "profile-admission-producer", Spec: spec, State: "running"}, nil
}

func TestHLSAdmissionPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_HLS_ADMISSION_PERFORMANCE") != "1" {
		t.Skip("set GOBY_TEST_HLS_ADMISSION_PERFORMANCE=1 to measure controlled admission contention")
	}
	const samples = 20
	for _, generated := range []bool{false, true} {
		for _, operation := range []string{"stop", "request_cancel"} {
			for _, delay := range []time.Duration{5 * time.Millisecond, 50 * time.Millisecond} {
				elapsed := make([]int64, 0, samples)
				for range samples {
					h, base := hlsRuntimeTestFixture(t)
					jobs := &hlsAdmissionProfileJobs{hlsRuntimeTestJobs: base, entered: make(chan struct{}), finished: make(chan struct{}), delay: delay}
					h.manager = jobs
					session := hlsRuntimeTestSession(t, h, "profile-admission", false)
					if generated {
						session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					stop := context.AfterFunc(session.ctx, cancel)
					input := hlsRuntimeInput(t)
					result := make(chan error, 1)
					go func() {
						var err error
						if generated {
							_, err = h.generatedArtifact(ctx, session, input, "main.m3u8")
						} else {
							_, err = h.segment(ctx, session, input, 0)
						}
						result <- err
					}()
					select {
					case <-jobs.entered:
					case <-ctx.Done():
						t.Fatal("controlled producer did not enter Ensure")
					}
					start := time.Now()
					if operation == "stop" {
						h.retire(session)
						elapsed = append(elapsed, time.Since(start).Nanoseconds())
					} else {
						cancel()
					}
					select {
					case <-result:
						if operation == "request_cancel" {
							elapsed = append(elapsed, time.Since(start).Nanoseconds())
						}
					case <-time.After(5 * time.Second):
						t.Fatal("controlled request did not respond to cancellation")
					}
					<-jobs.finished
					h.workers.Wait()
					h.retire(session)
					base.releaseAll()
					stop()
					cancel()
				}
				sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
				payload := struct {
					Operation       string `json:"operation"`
					Generated       bool   `json:"generated"`
					InjectedDelayNS int64  `json:"injected_ensure_delay_ns"`
					Samples         int    `json:"samples"`
					MedianNS        int64  `json:"median_ns"`
					P95NS           int64  `json:"p95_ns"`
					MaxNS           int64  `json:"max_ns"`
				}{operation, generated, delay.Nanoseconds(), samples, elapsed[len(elapsed)/2], elapsed[(len(elapsed)*95-1)/100], elapsed[len(elapsed)-1]}
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("hls_admission_performance=%s", encoded)
			}
		}
	}
}
