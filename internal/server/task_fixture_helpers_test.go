package server

import (
	"context"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

// A fixture replacing its catalog must first stop the manager that owns task
// operations on that catalog. The server's production shutdown path is left
// intact for tests that deliberately exercise shutdown or ownership loss.
func closeFixtureCatalogForReplacement(t *testing.T, f *serverFixture) {
	t.Helper()
	if err := f.app.mediaOperations.Close(f.ctx); err != nil {
		t.Fatalf("close media operations before replacing fixture catalog: %v", err)
	}
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatalf("close task manager before replacing fixture catalog: %v", err)
	}
	if err := f.app.mediaAnalysis.Close(f.ctx); err != nil {
		t.Fatalf("close media analysis before replacing fixture catalog: %v", err)
	}
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close previous fixture catalog: %v", err)
	}
}

// Each installed catalog receives the same settings initialization, task
// reconciliation, run recovery, and manager construction as a new server.
// Capture the resulting pair so later replacements cannot redirect cleanup to
// a different owner generation.
func installFixtureCatalog(t *testing.T, f *serverFixture, catalog *library.Store) {
	t.Helper()
	f.app.library = catalog
	var manager *tasks.Manager
	var operations *mediaOperationsRuntime
	var analysis *mediaAnalysisRuntime
	// Register before initialization so an initialization failure still retires
	// the new catalog before its temporary media root is removed.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := operations.Close(ctx); err != nil {
			t.Errorf("close replacement fixture media operations: %v", err)
			return
		}
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close replacement fixture task manager: %v", err)
			return
		}
		if err := analysis.Close(ctx); err != nil {
			t.Errorf("close replacement fixture media analysis: %v", err)
			return
		}
		if err := catalog.Close(ctx); err != nil {
			t.Errorf("close replacement fixture catalog: %v", err)
		}
	})
	initializeFixtureSettings(t, f)
	var err error
	operations, err = newMediaOperationsRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatalf("initialize media operations for replacement fixture catalog: %v", err)
	}
	f.app.mediaOperations = operations
	analysis, err = newMediaAnalysisRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatalf("initialize media analysis for replacement fixture catalog: %v", err)
	}
	f.app.mediaAnalysis = analysis
	if err := f.app.initializeTasks(f.ctx); err != nil {
		t.Fatalf("initialize tasks for replacement fixture catalog: %v", err)
	}
	manager = f.app.taskManager
}

// Fixtures that replace startup configuration explicitly rebuild its frozen
// defaults on the current owner before constructing or exercising media plans.
func initializeFixtureSettings(t *testing.T, f *serverFixture) {
	t.Helper()
	if err := f.app.initializeSettings(f.ctx); err != nil {
		t.Fatalf("initialize settings for fixture configuration and catalog owner: %v", err)
	}
}
