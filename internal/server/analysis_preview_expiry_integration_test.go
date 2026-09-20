//go:build linux

package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnalysisPreviewKnownExpiryBoundsBlockedInitialIdentityRead(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, true)
	f := fixture.stream.f
	principal := fixture.viewer
	if err := f.pool.QueryRow(f.ctx, `UPDATE sessions SET
		created_at=clock_timestamp()-interval '1 minute',expires_at=clock_timestamp()+interval '2 seconds'
		WHERE id=$1 RETURNING expires_at`, principal.SessionID).Scan(&principal.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	blockOriginalSessionRevalidation(t, f)
	finished := make(chan error, 1)
	go func() {
		lease, err := f.app.mediaAnalysis.OpenPreview(f.ctx, principal, fixture.source.ItemID, fixture.source.MediaSourceID, 400)
		if lease != nil {
			err = errors.Join(err, lease.Close())
		}
		finished <- err
	}()
	timer := time.NewTimer(time.Until(principal.ExpiresAt) + 2*time.Second)
	defer timer.Stop()
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("known expiry did not end a blocked initial preview authorization: %v", err)
		}
	case <-timer.C:
		t.Fatal("initial preview identity read outlived the known credential deadline")
	}
	assertAnalysisProviderIdle(t, fixture)
}

func TestAnalysisPreviewShortenedExpiryRemainsHardDuringLaterIdentityBlock(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, true)
	f := fixture.stream.f
	lease := fixture.open(t)
	var expiresAt time.Time
	if err := f.pool.QueryRow(f.ctx, `UPDATE sessions SET
		created_at=clock_timestamp()-interval '1 minute',expires_at=clock_timestamp()+interval '3 seconds'
		WHERE id=$1 RETURNING expires_at`, fixture.viewer.SessionID).Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	if err := lease.Revalidate(f.ctx); err != nil {
		t.Fatalf("observe the still-valid shortened credential: %v", err)
	}
	blockOriginalSessionRevalidation(t, f)
	timer := time.NewTimer(time.Until(expiresAt) + time.Second)
	defer timer.Stop()
	select {
	case <-lease.Context().Done():
		if time.Now().Before(expiresAt.Add(-100 * time.Millisecond)) {
			t.Fatal("lease was canceled before the observed expiry witness")
		}
	case <-timer.C:
		t.Fatal("a later identity block erased the newly observed credential deadline")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	assertAnalysisProviderIdle(t, fixture)
}
