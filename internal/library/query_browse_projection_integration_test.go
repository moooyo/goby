package library

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestBrowseProjectionPreservesCompleteDefaultItems(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryEntityQueryFixture(t, ctx, store.pool)
	info := media.Info{Container: "mkv", DurationTicks: 600000000, Size: 4096,
		Streams:          []media.Stream{{Index: 0, Codec: "h264", CodecType: "video", Width: 1920, Height: 1080}},
		VideoSeekIndexes: []media.VideoSeekIndex{{Version: 1, Entries: make([]media.VideoSeekPoint, 128)}}}
	for index := range info.VideoSeekIndexes[0].Entries {
		info.VideoSeekIndexes[0].Entries[index] = media.VideoSeekPoint{PTS: int64(index), DTS: int64(index),
			CodedSHA256: "source-packet-evidence", DecodedSHA256: "decoded-frame-evidence"}
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media=$1::jsonb WHERE id='movie-b'`, encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='null'::jsonb WHERE id='episode-b1';
		UPDATE items SET media=NULL WHERE id='audio-b';
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES ('browse-playlist','library-b','library-b','Browse playlist','browse playlist','Playlist',true);
		INSERT INTO media_collections(item_id,owner_id,kind) VALUES ('browse-playlist','restricted','Playlist');
		INSERT INTO media_collection_entries(collection_id,item_id,position)
		VALUES ('browse-playlist','movie-b',0),('browse-playlist','movie-b',1)`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		query Query
	}{
		{name: "typed page", query: Query{Recursive: true, IncludeItemTypes: []string{"Movie"}}},
		{name: "mixed explicit page", query: Query{Ids: []string{"movie-a", "movie-b", "episode-b1", "audio-b"}}},
		{name: "duplicate playlist entries", query: Query{ParentID: "browse-playlist"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := test.query
			query.UserID = "restricted"
			complete, err := store.QueryItems(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			query.Projection.Browse = true
			browse, err := store.QueryItems(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			seekItems := 0
			for index := range complete.Items {
				item := &complete.Items[index]
				if item.ID == "movie-b" {
					seekItems++
					if item.Media == nil || len(item.Media.VideoSeekIndexes) != 1 || len(item.Media.VideoSeekIndexes[0].Entries) != 128 {
						t.Fatal("the default domain query lost complete playback evidence")
					}
				}
				if item.Media != nil {
					item.Media.VideoSeekIndexes = nil
				}
			}
			if seekItems == 0 || !reflect.DeepEqual(complete, browse) {
				t.Fatalf("browse changed fields, null media, visibility, or entry identity: complete=%+v browse=%+v", complete, browse)
			}
		})
	}
	item, err := store.GetItemFor(ctx, Subject{UserID: "restricted"}, "movie-b")
	if err != nil || item.Media == nil || len(item.Media.VideoSeekIndexes) != 1 {
		t.Fatalf("direct item playback facts changed: media=%+v err=%v", item.Media, err)
	}
}
