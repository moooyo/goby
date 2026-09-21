package server

import (
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
)

func scanEvidenceLibraryOptions(cfg config.Config, serverID string) ([]library.Option, error) {
	if err := cfg.ScanEvidence.Validate(); err != nil {
		return nil, err
	}
	if !cfg.ScanEvidence.Enabled {
		return nil, nil
	}
	inventory := cfg.ScanEvidence
	return []library.Option{library.WithScanEvidence(library.ScanEvidenceOptions{
		Directory: inventory.Directory, ServerID: serverID, ExcludedRoots: cfg.ScanEvidenceExcludedRoots(),
		MaxBytes: inventory.MaxBytes, MaxDirectories: inventory.MaxDirectories,
		MaxEntries: inventory.MaxEntries, MaxFallbackHandles: inventory.MaxFallbackHandles,
	})}, nil
}
