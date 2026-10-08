//go:build linux

package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestRemotePlayBatchPreservesCompleteCoverageAndErrorOrder(t *testing.T) {
	p := newPlaybackHTTPFixture(t)
	f := p.s.f
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "batch-controller")
	actor, target := library.Subject{UserID: admin.userID}, library.Subject{UserID: p.s.viewerID}
	ids := make([]string, maxRemotePlayItems)
	for index := range ids {
		ids[index] = fmt.Sprintf("remote-batch-item-%03d", index)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		SELECT id,$2,$2,id,id,CASE WHEN ordinal%2=0 THEN 'Folder' ELSE 'Movie' END,ordinal%2=0
		FROM unnest($1::text[]) WITH ORDINALITY input(id,ordinal)`, ids, p.s.video.libraryID); err != nil {
		t.Fatal(err)
	}
	if err := f.app.authorizeRemotePlayItems(f.ctx, actor, target, ids); err != nil {
		t.Fatalf("legal maximum command with folder and metadata-only items was rejected: %v", err)
	}
	p.s.setPolicy(t, p.s.viewerID, false, []string{p.s.video.libraryID})
	if err := f.app.authorizeRemotePlayItems(f.ctx, actor, target, []string{ids[0], "missing-later"}); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("later missing controller item displaced first target playback denial: %v", err)
	}
	if err := f.app.authorizeRemotePlayItems(f.ctx, actor, target, []string{"missing-first", ids[0]}); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("target playback denial displaced first missing controller item: %v", err)
	}
	p.s.setPolicy(t, p.s.viewerID, true, []string{p.s.video.libraryID})
	ids[len(ids)-1] = "missing-last"
	if err := f.app.authorizeRemotePlayItems(f.ctx, actor, target, ids); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("batch silently dropped a missing requested item: %v", err)
	}
}
