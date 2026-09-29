package transcode

import (
	"context"
	"path/filepath"
	"testing"
)

func TestManagerMetricsHardwareClassification(t *testing.T) {
	tests := []struct {
		name     string
		plan     Plan
		hardware bool
	}{
		{name: "default software"},
		{name: "explicit software", plan: Plan{Hardware: Hardware{Decode: "software", Encode: "software"}}},
		{name: "hardware decode", plan: Plan{Hardware: Hardware{Decode: "vaapi"}}, hardware: true},
		{name: "hardware encode", plan: Plan{Hardware: Hardware{Encode: "vaapi"}}, hardware: true},
		{name: "hardware decode and encode", plan: Plan{Hardware: Hardware{Decode: "vaapi", Encode: "vaapi"}}, hardware: true},
		{name: "VAAPI filters", plan: Plan{VideoFilters: VideoFilters{Backend: "vaapi"}}, hardware: true},
		{name: "Vulkan filters", plan: Plan{VideoFilters: VideoFilters{Backend: "vulkan"}}, hardware: true},
		{name: "device alone", plan: Plan{Hardware: Hardware{Device: "/dev/dri/renderD128"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := &Manager{options: Options{MaxJobs: 4}, jobs: map[string]*managedJob{
				"active": {record: Record{State: "running", Spec: Spec{Plan: test.plan}}, running: true},
			}}
			want := Metrics{Running: 1, Software: 1, MaxJobs: 4}
			if test.hardware {
				want.Hardware, want.Software = 1, 0
			}
			if got := manager.Metrics(); got != want {
				t.Fatalf("Metrics() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestManagerMetricsCountsOccupiedSlots(t *testing.T) {
	manager := &Manager{
		options:               Options{MaxJobs: 8},
		diagnosticReservation: 1,
		jobs: map[string]*managedJob{
			"software":  {record: Record{State: "running"}, running: true},
			"hardware":  {record: Record{State: "running", Spec: Spec{Plan: Plan{Hardware: Hardware{Encode: "vaapi"}}}}, running: true},
			"stopping":  {record: Record{State: "running"}, running: true, stopCode: "cancelled"},
			"queued":    {record: Record{State: "queued"}},
			"completed": {record: Record{State: "completed"}, finished: true},
			"failed":    {record: Record{State: "failed"}, finished: true},
			"cancelled": {record: Record{State: "cancelled"}, finished: true},
		},
	}
	want := Metrics{Running: 3, Hardware: 1, Software: 2, MaxJobs: 8}
	first := manager.Metrics()
	if first != want {
		t.Fatalf("Metrics() = %+v, want %+v", first, want)
	}

	manager.mu.Lock()
	manager.jobs["stopping"].running = false
	manager.jobs["stopping"].record.State = "cancelled"
	manager.mu.Unlock()
	if got := manager.Metrics(); got != (Metrics{Running: 2, Hardware: 1, Software: 1, MaxJobs: 8}) {
		t.Fatalf("Metrics() after slot release = %+v", got)
	}
	if first != want {
		t.Fatalf("previous snapshot changed: %+v", first)
	}
}

func TestManagerMetricsEffectiveCapacity(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, requested := range []int{0, 7} {
		options, err := normalizeManagerOptions(Options{
			Root: root, FFmpegPath: "ffmpeg", Repository: metricsTestRepository{}, MaxJobs: requested,
		})
		if err != nil {
			t.Fatal(err)
		}
		manager := &Manager{options: options}
		want := requested
		if want == 0 {
			want = 2
		}
		if got := manager.Metrics(); got != (Metrics{MaxJobs: want}) {
			t.Fatalf("Metrics() with requested capacity %d = %+v, want capacity %d", requested, got, want)
		}
	}
}

type metricsTestRepository struct{}

func (metricsTestRepository) Recover(context.Context) error        { return nil }
func (metricsTestRepository) Create(context.Context, Record) error { return nil }
func (metricsTestRepository) Update(context.Context, Record) error { return nil }
