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
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatalf("close task manager before replacing fixture catalog: %v", err)
	}
	if err := f.app.library.Close(f.ctx); err != nil {
		t.Fatalf("close previous fixture catalog: %v", err)
	}
}

// Each installed catalog receives the same task reconciliation, run recovery,
// and manager construction as a new server. Capture the resulting pair so later
// replacements cannot redirect cleanup to a different owner generation.
func installFixtureCatalog(t *testing.T, f *serverFixture, catalog *library.Store) {
	t.Helper()
	f.app.library = catalog
	var manager *tasks.Manager
	// Register before initialization so an initialization failure still retires
	// the new catalog before its temporary media root is removed.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close replacement fixture task manager: %v", err)
			return
		}
		if err := catalog.Close(ctx); err != nil {
			t.Errorf("close replacement fixture catalog: %v", err)
		}
	})
	if err := f.app.initializeTasks(f.ctx); err != nil {
		t.Fatalf("initialize tasks for replacement fixture catalog: %v", err)
	}
	manager = f.app.taskManager
}
