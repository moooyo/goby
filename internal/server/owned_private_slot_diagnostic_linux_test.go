//go:build linux

package server

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestOwnedPrivateSlotDiagnostic(t *testing.T) {
	root := os.Getenv("GOBY_OWNED_SLOT_DIAGNOSTIC_ROOT")
	if root == "" {
		t.Skip("an exact owned diagnostic directory is required")
	}
	h, session, jobs, source, info := generatedWindowSlotActualFixture(t)
	stat, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	copy, err := os.OpenFile(filepath.Join(root, "exact-slot-source.mp4"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(copy, io.NewSectionReader(source, 0, stat.Size()))
	closeErr := copy.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("failed to preserve the exact source")
	}
	plan, err := hlsGeneratedWindowSlotPlan(session.windowGraph.basePlan, session.windowGraph.timeline, 0)
	if err != nil {
		t.Fatal(err)
	}
	rangeProof, rangeErr := transcode.MeasureGeneratedSourceRange(session.ctx, h.server.cfg.FFprobePath, source, plan, info.FormatStartTicks)
	_, pin, slotErr := h.generatedWindowSlot(session.ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		_ = pin.Close()
	}
	jobs.mu.Lock()
	ensureCalls := jobs.calls
	jobs.mu.Unlock()
	errorText := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"endpoint": session.windowGraph.endpoint, "source_info": info,
		"slot_plan": plan, "range": rangeProof, "range_error": errorText(rangeErr),
		"slot_error": errorText(slotErr), "ensure_calls": ensureCalls,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "native-stage-results.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("range_error=%q slot_error=%q", errorText(rangeErr), errorText(slotErr))
}
