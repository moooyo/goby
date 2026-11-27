package library

import "fmt"

// WithBasicVideoScanProbe uses a probe without optional seek analysis for
// ordinary video imports. The prober supplied to New still handles explicit
// media refreshes, audio, auxiliary resources, and other source operations.
func WithBasicVideoScanProbe(prober Prober) Option {
	return func(settings *storeOptions) error {
		if prober == nil || settings.basicVideoScanProbe != nil {
			return fmt.Errorf("%w: a single basic video scan prober is required", ErrInvalidInput)
		}
		settings.basicVideoScanProbe = prober
		return nil
	}
}

func scanIngestVideoLibrary(library Library) bool {
	return library.CollectionType == "movies" || library.CollectionType == "tvshows"
}

func (state *scanState) scanProber(kind string, role scannedMediaRole) Prober {
	if kind == "video" && role == scannedRoleOrdinary && !state.task.job.ForceProbe &&
		scanIngestVideoLibrary(state.library) && state.store.basicVideoScanProbe != nil {
		return state.store.basicVideoScanProbe
	}
	return state.store.prober
}
