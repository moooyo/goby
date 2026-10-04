package library

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func (s *Store) applyTaskProviderSubtitleSnapshot(ctx context.Context, actor *identity.Principal, snapshot *providerSubtitleSnapshot) error {
	if actor != nil {
		return nil
	}
	if hint, granted, err := s.taskSourceRootHint(ctx, snapshot.primary.root); granted {
		if err != nil {
			return err
		}
		snapshot.bindingRevision = hint.bindingRevision
	}
	return nil
}

// Internal publication retains permission approval but always fences the
// manager's live child ownership before business locks and immediately before
// commit. The adapter borrows the already owned transaction and never commits it.
func (s *Store) checkTaskProviderPublication(ctx, protected context.Context, raw pgx.Tx, actor *identity.Principal) error {
	grant := taskSourceGrant(ctx)
	if actor != nil || grant == nil {
		return nil
	}
	if err := grant.check(ctx, s, grant.childID); err != nil {
		return err
	}
	return grant.fence(taskSourcePublicationTx{ctx: protected, tx: raw})
}

type taskSourcePublicationTx struct {
	ctx context.Context
	tx  pgx.Tx
}

func (tx taskSourcePublicationTx) Exec(statement string, args ...any) (pgconn.CommandTag, error) {
	return tx.tx.Exec(tx.ctx, statement, args...)
}

func (tx taskSourcePublicationTx) QueryRow(statement string, args ...any) OwnedRow {
	return tx.tx.QueryRow(tx.ctx, statement, args...)
}

func (tx taskSourcePublicationTx) Query(statement string, args ...any) (OwnedRows, error) {
	return tx.tx.Query(tx.ctx, statement, args...)
}
