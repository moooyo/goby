package transcode

// Metrics is a point-in-time snapshot of occupied conversion slots. Hardware
// includes jobs using hardware decoding, encoding, or GPU video filters.
type Metrics struct {
	Running  int
	Hardware int
	Software int
	MaxJobs  int
}

// Metrics reports active conversion slots and the manager's effective capacity.
// A cancellation request retains its slot until the running job releases it.
func (m *Manager) Metrics() Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := Metrics{MaxJobs: m.options.MaxJobs}
	for _, job := range m.jobs {
		if !job.running {
			continue
		}
		metrics.Running++
		plan := job.record.Spec.Plan
		decode, encode := hardwareSelection(plan.Hardware)
		if decode != "software" || encode != "software" ||
			plan.VideoFilters.Backend == "vaapi" || plan.VideoFilters.Backend == "vulkan" {
			metrics.Hardware++
		} else {
			metrics.Software++
		}
	}
	return metrics
}
