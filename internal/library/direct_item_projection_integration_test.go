package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type directItemProjectionTrace struct {
	mu         sync.Mutex
	statements []string
}

func (trace *directItemProjectionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	trace.statements = append(trace.statements, data.SQL)
	trace.mu.Unlock()
	return ctx
}

func (*directItemProjectionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *directItemProjectionTrace) reset() {
	trace.mu.Lock()
	trace.statements = nil
	trace.mu.Unlock()
}

func (trace *directItemProjectionTrace) count(fragment string) int {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	count := 0
	for _, statement := range trace.statements {
		if strings.Contains(statement, fragment) {
			count++
		}
	}
	return count
}

func tracedDirectItemStore(t *testing.T, ctx context.Context, fixture *Store) (*Store, *directItemProjectionTrace) {
	t.Helper()
	trace := &directItemProjectionTrace{}
	config := fixture.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Store{pool: pool}, trace
}

func TestNowPlayingItemsProjectionPreservesDirectPresentation(t *testing.T) {
	ctx, fixture := extraTestFixture(t)
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ($1,'Collections','mixed')`, collectionLibraryID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES ('direct-projection-playlist',$1,'Projection playlist','Projection playlist','Playlist',true)`, collectionLibraryID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO media_collections(item_id,owner_id,kind,media_type)
		VALUES ('direct-projection-playlist','restricted','Playlist','Video');
		INSERT INTO media_collection_entries(collection_id,item_id,position)
		VALUES ('direct-projection-playlist','theme-seed',0),('direct-projection-playlist','theme-seed',1);
		INSERT INTO media_collection_shares(collection_id,user_id,can_edit)
		VALUES ('direct-projection-playlist','default',true);
		UPDATE items SET media='{"Container":"mp4","DurationTicks":120000000,
		"Streams":[{"Index":0,"CodecType":"video","Codec":"h264"}]}' WHERE id='theme-video';
		INSERT INTO item_subtitles(item_id,root_id,stream_index,relative_path,file_identity,source_hash,file_size,
		modified_at,change_time_ns,codec,language,title,mime_type)
		VALUES ('theme-video','root-b',2,'Video/backdrops/video.en.vtt','fixture-only',repeat('a',64),20,
		clock_timestamp(),1,'vtt','en','English','text/vtt')`); err != nil {
		t.Fatal(err)
	}
	indexes := []media.VideoSeekIndex{{Version: media.VideoSeekIndexVersion, Entries: make([]media.VideoSeekPoint, 128)}}
	encoded, err := json.Marshal(indexes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE items SET media=media || jsonb_build_object('VideoSeekIndexes',$1::jsonb)
		WHERE id IN ('theme-video','extra-alpha')`, encoded); err != nil {
		t.Fatal(err)
	}
	userDataSeed(t, ctx, fixture.pool, "restricted", UserData{ItemID: "theme-video", PlayCount: 4, IsFavorite: true})
	key := seedCatalogApplicationKey(t, ctx, fixture.pool, "direct-projection-key", true)
	store, trace := tracedDirectItemStore(t, ctx, fixture)
	ids := []string{"theme-video", "movie-a", "extra-alpha", "direct-projection-playlist", "theme-episode1", "library-b", "missing"}
	for _, subject := range []Subject{{UserID: "restricted"}, {UserID: "default"}, {UserID: "none"}, key,
		{UserID: "restricted", ApplicationCredentialID: key.ApplicationCredentialID}} {
		t.Run(subject.UserID+"/"+subject.ApplicationCredentialID, func(t *testing.T) {
			complete, err := store.GetItemsByIDFor(ctx, subject, ids)
			if err != nil {
				t.Fatal(err)
			}
			trace.reset()
			nowPlaying, err := store.GetNowPlayingItemsByIDFor(ctx, subject, ids)
			if err != nil {
				t.Fatal(err)
			}
			if trace.count("i.media - 'VideoSeekIndexes'") != 1 || trace.count("user_item_data") != 0 {
				t.Fatal("NowPlaying must omit private seek evidence and user-data SQL at the query boundary")
			}
			for index := range complete {
				item := &complete[index]
				if item.ID == "theme-video" {
					if item.Media == nil || len(item.Media.VideoSeekIndexes) != 1 || item.ThemeKind != "video" || len(item.Subtitles) != 1 {
						t.Fatal("the complete baseline lost seek evidence, theme identity, or subtitles")
					}
					if subject.UserID == "restricted" && (item.UserData == nil || !item.UserData.IsFavorite || item.UserData.PlayCount != 4) {
						t.Fatal("the default direct-item query lost its complete user-data contract")
					}
				}
				if item.ID == "extra-alpha" && (item.ExtraKind != ExtraKindClip || item.ExtraOwnerName != "Feature Movie") {
					t.Fatal("the complete baseline lost the extra's owner projection")
				}
				if item.ID == "direct-projection-playlist" && (item.Collection == nil || item.Collection.ItemCount != 2) {
					t.Fatal("the complete baseline lost collection metadata")
				}
				item.UserData = nil
				if item.Collection != nil {
					item.Collection.UserData = nil
				}
				if item.Media != nil {
					item.Media.VideoSeekIndexes = nil
				}
			}
			if !reflect.DeepEqual(complete, nowPlaying) {
				t.Fatal("NowPlaying changed input order, current visibility, or retained presentation fields")
			}
			trace.reset()
			permissions, err := store.StoredItemPermissionsFor(ctx, subject, ids)
			if err != nil || len(permissions) != len(complete) {
				t.Fatalf("stored permissions differ from direct reads: visible=%d direct=%d error=%v", len(permissions), len(complete), err)
			}
			for _, item := range complete {
				permission, found := permissions[item.ID]
				if !found || permission.CanPlay != item.CanPlay {
					t.Fatalf("stored permission differs for %s", item.ID)
				}
			}
			if trace.count("SELECT i.id FROM items i WHERE i.id=ANY") != 1 || trace.count("i.created_at,") != 0 || trace.count("user_item_data") != 0 {
				t.Fatal("visibility-only reads loaded complete item or user-data projections")
			}
		})
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE users SET policy=policy || '{"ExcludedSubFolders":["theme-seed","theme-video-owner"]}'::jsonb
		WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	permissions, err := store.StoredItemPermissionsFor(ctx, Subject{UserID: "restricted"}, []string{"theme-video", "extra-alpha", "library-b"})
	if err != nil || len(permissions) != 1 {
		t.Fatalf("stored permissions ignored current auxiliary-owner policy: %+v %v", permissions, err)
	}
	if _, found := permissions["library-b"]; !found {
		t.Fatal("owner restrictions hid an independently visible collection folder")
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetNowPlayingItemsByIDFor(ctx, key, ids); !errors.Is(err, ErrForbidden) {
		t.Fatalf("NowPlaying reused a revoked application credential: %v", err)
	}
	if _, err := store.StoredItemPermissionsFor(ctx, key, ids); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stored permissions reused a revoked application credential: %v", err)
	}
}

func TestStoredItemPermissionsDoNotExpandExpectedEpisodes(t *testing.T) {
	f := newEpisodeRosterFixture(t)
	f.replace(t, f.edit)
	var virtualIDs []string
	rows, err := f.pool.Query(f.ctx, "SELECT id FROM expected_episodes ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		virtualIDs = append(virtualIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(virtualIDs) == 0 {
		t.Fatalf("expected-episode fixture: ids=%v error=%v", virtualIDs, err)
	}
	literalID := "missing-" + strings.Repeat("d", 32)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES ($1,'library-b','library-b','Literal stored item','Literal stored item','Movie',false)`, literalID); err != nil {
		t.Fatal(err)
	}
	ids := append([]string{"movie-a", "movie-b", "library-b", VirtualRootItemID, literalID}, virtualIDs...)
	for _, userID := range []string{"admin", "default", "restricted", "none"} {
		subject := Subject{UserID: userID}
		complete, err := f.store.GetItemsByIDFor(f.ctx, subject, ids)
		if err != nil {
			t.Fatal(err)
		}
		permissions, err := f.store.StoredItemPermissionsFor(f.ctx, subject, ids)
		if err != nil || len(permissions) != len(complete) {
			t.Fatalf("stored-only permission population differs for %s: %v", userID, err)
		}
		for _, item := range complete {
			if permission, found := permissions[item.ID]; !found || permission.CanPlay != item.CanPlay {
				t.Fatalf("stored-only permission differs for %s/%s", userID, item.ID)
			}
		}
		for _, id := range virtualIDs {
			if _, found := permissions[id]; found {
				t.Fatal("a virtual expected episode became a current stored notification item")
			}
		}
		if _, found := permissions[literalID]; found != (userID != "none") {
			t.Fatal("a stored identifier was interpreted as a virtual-episode lookup")
		}
	}
	permissions, err := f.store.ItemPermissionsFor(f.ctx, Subject{UserID: "restricted"}, virtualIDs)
	if err != nil || len(permissions) == 0 {
		t.Fatalf("the existing remote-control projection lost expected episodes: %+v %v", permissions, err)
	}
}

func TestNowPlayingItemsRejectDuplicateInputLikeCompleteDirectReads(t *testing.T) {
	store := &Store{}
	for _, ids := range [][]string{{"movie", "movie"}, {""}, {"bad\x00id"}, {strings.Repeat("x", 257)}, make([]string, 1001)} {
		if _, err := store.GetNowPlayingItemsByIDFor(context.Background(), Subject{UserID: "user"}, ids); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid direct input was accepted: %v", err)
		}
	}
}
