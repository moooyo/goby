//go:build linux

package library

import (
	"reflect"
	"testing"
)

func TestItemAnalysisRevisionRequiresMediaObject(t *testing.T) {
	f := newAnalysisWorkFixture(t, 1)
	subject := Subject{UserID: f.viewer}
	id := f.ids[0]
	var encoded []byte
	var expectedRevision string
	if err := f.pool.QueryRow(f.ctx, `SELECT i.media,`+introSourceRevisionSQL+` FROM items i WHERE i.id=$1`, id).
		Scan(&encoded, &expectedRevision); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		media    any
		hasMedia bool
	}{
		{name: "metadata only SQL NULL"},
		{name: "metadata only JSON null", media: []byte("null")},
		{name: "scanned media object", media: encoded, hasMedia: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=$2::jsonb WHERE id=$1`, id, test.media); err != nil {
				t.Fatal(err)
			}
			item, err := f.store.GetItemFor(f.ctx, subject, id)
			if err != nil {
				t.Fatal(err)
			}
			if test.hasMedia {
				if item.Media == nil || item.AnalysisSourceRevision == "" || item.AnalysisSourceRevision != expectedRevision {
					t.Fatal("a scanned media object lost its unchanged source revision")
				}
			} else if item.Media != nil || item.AnalysisSourceRevision != "" {
				t.Fatal("an item without a media object acquired an analysis source revision")
			}
			listed, err := f.store.QueryItems(f.ctx, Query{UserID: f.viewer, Ids: []string{id}})
			if err != nil || len(listed.Items) != 1 || listed.Items[0].AnalysisSourceRevision != "" {
				t.Fatalf("the list acquired detail-only source revision work: items=%d err=%v", len(listed.Items), err)
			}
			if !test.hasMedia && !reflect.DeepEqual(item, listed.Items[0]) {
				t.Fatal("metadata-only direct and listed item projections diverged")
			}
		})
	}
	file, source, err := f.store.OpenMediaFor(f.ctx, subject, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if source.Item.AnalysisSourceRevision != expectedRevision {
		t.Fatal("single-item and playback reads disagreed on the restored media object revision")
	}
}
