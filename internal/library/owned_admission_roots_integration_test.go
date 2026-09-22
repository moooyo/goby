//go:build linux

package library

import (
	"context"
	"testing"
)

func TestOwnedAdmissionRootWaitDoesNotBlockIndependentWork(t *testing.T) {
	fixture := newRootBindingScanFixture(t)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	const userID, itemID = "owned-admission-root-viewer", "owned-admission-root-movie"
	libraryIntegrationUser(t, fixture.ctx, fixture.pool, userID, false, true, nil)
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES ($1,$2,$2,'Independent movie','independent movie','Movie',false)`, itemID, fixture.library.ID); err != nil {
		t.Fatalf("create the independent user-state item: %v", err)
	}
	input := rootBindingWriteTestInput(t, fixture.rootBindingReadFixture, "13")
	for _, operation := range []struct {
		name, caller string
		run          func(context.Context) error
	}{
		{"binding update", "updateRootBinding", func(ctx context.Context) error {
			observation := &rootBindingWriteTestCapture{snapshot: fixture.snapshot}
			_, err := fixture.store.updateRootBinding(ctx, fixture.actor, fixture.library.ID, fixture.root.RootID,
				input, rootBindingWriteTestFactory(t, fixture.rootBindingReadFixture, observation))
			return err
		}},
		{"scan binding", "admitRootBindingScan", func(ctx context.Context) error {
			original := fixture.task.ctx
			fixture.task.ctx = ctx
			defer func() { fixture.task.ctx = original }()
			_, err := fixture.store.admitRootBindingScan(fixture.task, fixture.scanRoot, &capture.row, capture, nil)
			return err
		}},
		{"reconciliation", "reconcileMissingScanItems", func(ctx context.Context) error {
			original := fixture.task.ctx
			fixture.task.ctx = ctx
			defer func() { fixture.task.ctx = original }()
			_, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
				[]*rootBindingScanCapture{capture}, evidence, nil)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			ownedAdmissionCheckWait(t, fixture.ctx, fixture.store, fixture.scanRoot, userID, itemID, operation.caller, operation.run)
		})
	}
}
