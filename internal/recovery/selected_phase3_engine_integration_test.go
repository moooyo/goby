//go:build linux

package recovery

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const selectedPhase3RecoveryStateSQL = `SELECT jsonb_build_object(
	'rosters',(SELECT jsonb_agg(to_jsonb(r) ORDER BY series_id) FROM series_episode_rosters r),
	'imports',(SELECT jsonb_agg(to_jsonb(i) ORDER BY series_id,revision) FROM episode_roster_imports i),
	'facts',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM expected_episodes e),
	'physical',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i WHERE id LIKE 'roster-recovery-%'))::text`

// The source roster is written through the administrator API owner. The real
// encrypted archive, authenticated fingerprints and application finalizer must
// preserve it without discovering episodes, reopening media or consulting a
// metadata provider. Dormant owner classifications retain history without
// permitting imports or discovery. Imported credentials expire independently.
func TestEngineSelectedPhase3ArchiveRestorePreservesExplicitRosterAuthority(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder)
		VALUES('roster-recovery-active',$1,$1,'Retained active series','retained active series','Series',true),
		('roster-recovery-withdrawn',$1,$1,'Retained withdrawn series','retained withdrawn series','Series',true),
		('roster-recovery-dormant',$1,$1,'Retained dormant series','retained dormant series','Series',true)`, f.libraryID); err != nil {
		t.Fatal("seed real series identities without inferring expected episodes")
	}
	openCatalog := func(target bool) *library.Store {
		t.Helper()
		pool, configuration := f.source, f.configuration
		if target {
			pool, configuration = f.target, f.targetConfig
		}
		catalog, err := library.New(pool, media.Prober{FFprobePath: configuration.FFprobePath, FFmpegPath: configuration.FFmpegPath}, configuration.MediaRoots)
		if err != nil {
			t.Fatalf("open the explicit roster owner: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := catalog.Close(ctx); err != nil {
				t.Error("close the explicit roster owner")
			}
		})
		return catalog
	}
	catalog := openCatalog(false)
	first := library.EpisodeRosterEdit{Revision: "0", Source: library.EpisodeRosterSourceInput{Key: "recovery-local-source", Label: "Retained explicit source", Revision: "edition-one"}, Entries: []library.EpisodeRosterEntryInput{
		{Key: "unknown", SeasonNumber: 1, EpisodeNumber: 1, Name: ""},
		{Key: "retired", SeasonNumber: 1, EpisodeNumber: 3, Name: "Declared retired entry", PremiereDate: "2020-01-01"},
	}}
	active, err := catalog.ReplaceEpisodeRoster(f.ctx, f.actor, "roster-recovery-active", first)
	if err != nil || active.Revision != "1" || len(active.Entries) != 2 {
		t.Fatalf("create explicit source authority through the native owner: %v", err)
	}
	stableID := active.Entries[0].ID
	second := first
	second.Revision = active.Revision
	second.Source.Revision = "edition-two"
	second.Entries = []library.EpisodeRosterEntryInput{first.Entries[0], {Key: "future", SeasonNumber: 1, EpisodeNumber: 4, Name: "Declared future entry", PremiereDate: "9999-12-31"}}
	active, err = catalog.ReplaceEpisodeRoster(f.ctx, f.actor, "roster-recovery-active", second)
	if err != nil || active.Revision != "2" || active.RetiredCount != 1 || active.Entries[0].ID != stableID {
		t.Fatalf("replace source facts while retaining stable and retired identities: %v", err)
	}
	withdrawn, err := catalog.ReplaceEpisodeRoster(f.ctx, f.actor, "roster-recovery-withdrawn", first)
	if err != nil {
		t.Fatalf("create the later withdrawn source witness: %v", err)
	}
	withdrawn, err = catalog.WithdrawEpisodeRoster(f.ctx, f.actor, "roster-recovery-withdrawn", withdrawn.Revision)
	if err != nil || withdrawn.State != "withdrawn" || len(withdrawn.Entries) != 0 || withdrawn.RetiredCount != 2 {
		t.Fatalf("retain an explicit withdrawn source: %v", err)
	}
	dormant, err := catalog.ReplaceEpisodeRoster(f.ctx, f.actor, "roster-recovery-dormant", first)
	if err != nil || len(dormant.Entries) != 2 {
		t.Fatalf("create the explicit roster before its owner becomes dormant: %v", err)
	}
	dormantIDs := []string{dormant.Entries[0].ID, dormant.Entries[1].ID}
	placeholder := true
	dormantQuery := library.Query{UserID: f.actor.User.ID, Ids: dormantIDs, IsPlaceHolder: &placeholder, Limit: 100}
	if visible, err := catalog.QueryItems(f.ctx, dormantQuery); err != nil || visible.TotalRecordCount != 2 || len(visible.Items) != 2 {
		t.Fatalf("the source fixture must expose both facts while its owner is a Series: %v", err)
	}
	// This is the persisted boundary after a same-identity classification change.
	// Scanner integration owns the filesystem transition; restoration must not
	// infer a withdrawal, delete provenance, or reverse this physical owner type.
	if tag, err := f.source.Exec(f.ctx, `UPDATE items SET type='Movie',is_folder=false WHERE id='roster-recovery-dormant'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("retain the source roster under a dormant physical owner: %v", err)
	}
	if err := catalog.Close(f.ctx); err != nil {
		t.Fatal("join the source owner before archive capture")
	}
	want := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase3RecoveryStateSQL)
	var credentials int64
	if err := f.source.QueryRow(f.ctx, `SELECT count(*) FROM sessions WHERE revoked_at IS NULL`).Scan(&credentials); err != nil {
		t.Fatal("count imported source credentials")
	}
	manifest, metadata := f.create(t)
	for _, name := range []string{"series_episode_rosters", "episode_roster_imports", "expected_episodes"} {
		found := false
		for _, fact := range manifest.Source.Tables {
			found = found || fact.Name == name && fact.Rows > 0
		}
		if !found {
			t.Fatalf("the authenticated archive omitted populated roster table %s", name)
		}
	}
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open the encrypted explicit roster archive")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatalf("authenticate the real explicit roster archive: %v", err)
	}
	defer archive.Close()
	releaseRecoveryEngineTestMemory()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("lease the independently owned roster restore target")
	}
	defer lease.Close()
	refused := errors.New("exact roster raw restore witness completed")
	called := false
	result, err := backuppg.RestoreFinalized(f.ctx, f.source, f.target, archive.Database(), manifest.Source, f.engine.options,
		func(ctx context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
			called = true
			if raw.SourceVersion != manifest.Source.SchemaVersion || raw.CurrentVersion != manifest.Source.SchemaVersion || !reflect.DeepEqual(raw.Tables, manifest.Source.Tables) {
				t.Error("roster raw fingerprints changed before normalization")
			}
			var state string
			if err := tx.QueryRow(ctx, selectedPhase3RecoveryStateSQL).Scan(&state); err != nil || state != want {
				t.Errorf("raw restoration changed explicit source evidence before the finalizer: %v", err)
			}
			return refused
		})
	if !called || !errors.Is(err, refused) || result.CurrentVersion != 0 {
		t.Fatalf("the raw roster witness did not roll back atomically: %v", err)
	}
	var empty bool
	if err := f.target.QueryRow(f.ctx, `SELECT to_regclass('series_episode_rosters') IS NULL`).Scan(&empty); err != nil || !empty {
		t.Fatal("a refused finalizer committed roster state")
	}
	if _, err := archive.Database().Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the same authenticated roster archive")
	}
	restoredResult, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil || restoredResult.RevokedCredentials != credentials ||
		restoredResult.SourceVersion != manifest.Source.SchemaVersion || restoredResult.CurrentVersion != manifest.Source.SchemaVersion {
		t.Fatalf("restore explicit roster authority while revoking imported credentials: %v", err)
	}
	if got := recoveryEngineJSONState(t, f.ctx, f.target, selectedPhase3RecoveryStateSQL); got != want {
		t.Fatal("recovery normalized or regenerated explicit roster evidence")
	}
	identities := identity.New(f.target)
	if _, err := identities.Resolve(f.ctx, f.adminLogin.Token, "admin"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("an imported native credential survived roster recovery")
	}
	login, err := identities.Authenticate(f.ctx, "Recovery administrator", "recovery-administrator-password", identity.Client{Name: "Recovered roster owner"}, "admin")
	if err != nil {
		t.Fatal("issue a fresh restored administrator login")
	}
	actor, err := identities.Resolve(f.ctx, login.Token, "admin")
	if err != nil {
		t.Fatal("resolve the fresh restored administrator")
	}
	restored := openCatalog(true)
	for _, expected := range []library.EpisodeRosterDetail{active, withdrawn} {
		actual, err := restored.GetEpisodeRoster(f.ctx, actor, expected.SeriesID)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("restored native reads changed explicit facts, unknown dates or withdrawal: %v", err)
		}
	}
	if _, err := restored.GetEpisodeRoster(f.ctx, actor, dormant.SeriesID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("restoration exposed dormant roster management: %v", err)
	}
	dormantEdit := first
	dormantEdit.Revision = dormant.Revision
	dormantEdit.Source.Revision = "dormant-replacement"
	if _, err := restored.ReplaceEpisodeRoster(f.ctx, actor, dormant.SeriesID, dormantEdit); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("restoration authorized an import under a non-Series owner: %v", err)
	}
	if _, err := restored.WithdrawEpisodeRoster(f.ctx, actor, dormant.SeriesID, dormant.Revision); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("restoration authorized a withdrawal under a non-Series owner: %v", err)
	}
	dormantQuery.UserID = actor.User.ID
	if hidden, err := restored.QueryItems(f.ctx, dormantQuery); err != nil || hidden.TotalRecordCount != 0 || len(hidden.Items) != 0 {
		t.Fatalf("restoration projected expected episodes from a dormant owner: %v", err)
	}
	for _, id := range dormantIDs {
		if _, err := restored.GetItemFor(f.ctx, library.Subject{UserID: actor.User.ID, Actor: &actor}, id); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("restoration exposed a dormant fact by stable ID: %v", err)
		}
	}
	if got := recoveryEngineJSONState(t, f.ctx, f.target, selectedPhase3RecoveryStateSQL); got != want {
		t.Fatal("owner startup or native reads reconstructed the retained roster")
	}
	if got := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase3RecoveryStateSQL); got != want {
		t.Fatal("roster backup and recovery changed the original source state")
	}
}
