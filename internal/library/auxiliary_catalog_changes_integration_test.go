//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type auxiliaryCatalogFixture struct {
	ctx                  context.Context
	pool                 *pgxpool.Pool
	store                *Store
	prober               *libraryFixtureProber
	root, userID, source string
	role                 string
	library              Library
	owner                Item
}

func newAuxiliaryCatalogFixture(t *testing.T, role string, populated bool) auxiliaryCatalogFixture {
	t.Helper()
	prober := &libraryFixtureProber{}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	main := libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:ordinary-owner")
	relative := "movies/Film/theme.mp3"
	if role == "extra" {
		relative = "movies/Film/featurettes/Clip.mp4"
	}
	source := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if populated {
		libraryIntegrationFile(t, root, relative, auxiliaryCatalogContents(role, "accepted"))
	}
	lib := libraryIntegrationCreate(t, ctx, store, "Auxiliary catalog facts", "movies", filepath.Join(root, "movies"))
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("prepare the ordinary auxiliary owner: %+v", job)
	}
	owner := nfoCatalogItem(t, ctx, store, userID, lib.ID, main)
	return auxiliaryCatalogFixture{ctx: ctx, pool: pool, store: store, prober: prober, root: root,
		userID: userID, source: source, role: role, library: lib, owner: owner}
}

func auxiliaryCatalogContents(role, label string) string {
	if role == "theme" {
		return "audio:" + label
	}
	return "video:" + label
}

func (f auxiliaryCatalogFixture) resource(t *testing.T) themeScanTestResource {
	t.Helper()
	resources := extraScanTestResources(t, f.ctx, f.pool, f.library.ID)
	if f.role == "theme" {
		resources = themeScanTestResources(t, f.ctx, f.pool, f.library.ID)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one retained %s resource, got %d", f.role, len(resources))
	}
	return resources[0]
}

func (f auxiliaryCatalogFixture) snapshot(t *testing.T) string {
	t.Helper()
	if f.role == "theme" {
		return themeScanTestSnapshot(t, f.ctx, f.pool, f.library.ID)
	}
	return extraScanTestSnapshot(t, f.ctx, f.pool, f.library.ID)
}

func TestAuxiliaryCatalogChangesLifecyclePreservesIdentityAndUserData(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		t.Run(role, func(t *testing.T) {
			f := newAuxiliaryCatalogFixture(t, role, false)
			notifications := catalogChangesTestListener(t, f.store)
			if err := os.WriteFile(f.source, []byte(auxiliaryCatalogContents(role, "new-resource")), 0o600); err != nil {
				t.Fatal(err)
			}
			if job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" {
				t.Fatalf("add the auxiliary source: %+v", job)
			}
			resource := f.resource(t)
			if !resource.active || resource.ownerID != f.owner.ID {
				t.Fatal("the committed resource lost its active Movie owner")
			}
			batches := auxiliaryCatalogBatches(t, notifications)
			assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogAdded)
			requireAuxiliaryCatalogResource(t, batches, CatalogAdded, resource.id, f.library.ID, f.owner.ID)
			userDataSeed(t, f.ctx, f.pool, f.userID, UserData{ItemID: resource.id, IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 91})
			userData := forceProbeUserDataSnapshot(t, f.ctx, f.pool, resource.id)
			before := f.snapshot(t)
			calls := len(f.prober.calls())
			if job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" || job.Updated != 0 {
				t.Fatalf("cached auxiliary scan changed accepted data: %+v", job)
			}
			assertNoCatalogTestNotification(t, notifications)
			if f.snapshot(t) != before || len(f.prober.calls()) != calls {
				t.Fatal("cached auxiliary scanning rewrote its accepted snapshot or repeated probing")
			}
			if err := os.WriteFile(f.source, []byte(auxiliaryCatalogContents(role, "changed-resource-with-new-size")), 0o600); err != nil {
				t.Fatal(err)
			}
			if job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" {
				t.Fatalf("update auxiliary properties: %+v", job)
			}
			batches = auxiliaryCatalogBatches(t, notifications)
			assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogUpdated)
			requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, resource.id, f.library.ID, f.owner.ID)
			if updated := f.resource(t); updated.id != resource.id || updated.probe.Size == resource.probe.Size {
				t.Fatal("the notified media update did not retain its identity and change its committed source facts")
			}
			calls = len(f.prober.calls())
			if job := forceProbeTestScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" {
				t.Fatalf("force an unchanged auxiliary source refresh: %+v", job)
			}
			batches = auxiliaryCatalogBatches(t, notifications)
			assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogUpdated)
			requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, resource.id, f.library.ID, f.owner.ID)
			if len(f.prober.calls()) != calls+2 {
				t.Fatal("the explicit refresh did not successfully probe both the ordinary Movie and its active resource")
			}
			if err := os.Remove(f.source); err != nil {
				t.Fatal(err)
			}
			if job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" {
				t.Fatalf("retire the absent auxiliary source: %+v", job)
			}
			batches = auxiliaryCatalogBatches(t, notifications)
			assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogRemoved)
			requireAuxiliaryCatalogResource(t, batches, CatalogRemoved, resource.id, f.library.ID, f.owner.ID)
			if retired := f.resource(t); retired.id != resource.id || retired.active || retired.ownerID != f.owner.ID {
				t.Fatal("auxiliary retirement discarded its historical identity or semantic owner")
			}
			if _, err := f.store.GetItem(f.ctx, f.userID, resource.id); !errors.Is(err, ErrNotFound) {
				t.Fatal("the notified inactive auxiliary resource remained directly visible")
			}
			if forceProbeUserDataSnapshot(t, f.ctx, f.pool, resource.id) != userData {
				t.Fatal("auxiliary notification lifecycle changed historical user data")
			}
			libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed")
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

func TestAuxiliaryCatalogChangesMoveInvalidatesBothSemanticOwners(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	firstPath := libraryIntegrationFile(t, root, "movies/First/Main.mp4", "video:first-owner")
	secondPath := libraryIntegrationFile(t, root, "movies/Second/Main.mp4", "video:second-owner")
	source := libraryIntegrationFile(t, root, "movies/First/featurettes/Clip.mp4", "video:moving-extra")
	destination := filepath.Join(root, "movies", "Second", "featurettes", "Clip.mp4")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	lib := libraryIntegrationCreate(t, ctx, store, "Changing semantic owner", "movies", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, lib.ID, firstPath)
	second := nfoCatalogItem(t, ctx, store, userID, lib.ID, secondPath)
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 1 || !resources[0].active {
		t.Fatal("prepare one active extra for its owner move")
	}
	resource := resources[0]
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: resource.id, IsFavorite: true, PlayCount: 4})
	userData := forceProbeUserDataSnapshot(t, ctx, pool, resource.id)
	notifications := catalogChangesTestListener(t, store)
	if err := os.Rename(source, destination); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("move the extra between its Movie owners: %+v", job)
	}
	batches := auxiliaryCatalogBatches(t, notifications)
	kinds := auxiliaryCatalogKinds(batches, resource.id)
	if !slices.Equal(kinds, []CatalogChangeKind{CatalogUpdated}) && !slices.Equal(kinds, []CatalogChangeKind{CatalogRemoved, CatalogAdded}) {
		t.Fatalf("owner move did not describe its actual committed visibility transitions: %v", kinds)
	}
	for _, batch := range batches {
		for _, change := range batch.Changes {
			if change.ItemID == resource.id {
				assertAuxiliaryCatalogScope(t, change, lib.ID)
			}
		}
	}
	if kinds[0] == CatalogUpdated {
		requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, resource.id, lib.ID, first.ID)
		requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, resource.id, lib.ID, second.ID)
	} else {
		requireAuxiliaryCatalogResource(t, batches, CatalogRemoved, resource.id, lib.ID, first.ID)
		requireAuxiliaryCatalogResource(t, batches, CatalogAdded, resource.id, lib.ID, second.ID)
	}
	resources = extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 1 || resources[0].id != resource.id || !resources[0].active || resources[0].ownerID != second.ID || resources[0].path != destination {
		t.Fatal("semantic owner notification did not retain the committed resource identity and destination")
	}
	if forceProbeUserDataSnapshot(t, ctx, pool, resource.id) != userData {
		t.Fatal("semantic owner movement changed existing user data")
	}
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	assertNoCatalogTestNotification(t, notifications)
}

func TestAuxiliaryCatalogChangesPermanentMarkerRemovesOrdinaryBeforeActivation(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mainPath := libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:owner")
	clipPath := libraryIntegrationFile(t, root, "movies/Film/featurettes/Clip.mp4", "video:ordinary-before-reservation")
	lib := libraryIntegrationCreate(t, ctx, store, "Permanent role reservation", "mixed", filepath.Join(root, "movies"))
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	owner := nfoCatalogItem(t, ctx, store, userID, lib.ID, mainPath)
	ordinary := nfoCatalogItem(t, ctx, store, userID, lib.ID, clipPath)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: ordinary.ID, IsFavorite: true, PlayCount: 6, PlaybackPositionTicks: 73})
	userData := forceProbeUserDataSnapshot(t, ctx, pool, ordinary.ID)
	notifications := catalogChangesTestListener(t, store)
	if _, err := pool.Exec(ctx, "UPDATE libraries SET collection_type='movies' WHERE id=$1", lib.ID); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("publish the new permanent auxiliary reservation: %+v", job)
	}
	batches := auxiliaryCatalogBatches(t, notifications)
	assertAuxiliaryCatalogKinds(t, batches, ordinary.ID, CatalogRemoved, CatalogAdded)
	removed, removedBatch := requireAuxiliaryCatalogFact(t, batches, CatalogRemoved, ordinary.ID, lib.ID)
	if removed.ParentID != ordinary.ParentID || removed.IsFolder {
		t.Fatal("ordinary removal did not retain its original browse container")
	}
	addedBatch := requireAuxiliaryCatalogResource(t, batches, CatalogAdded, ordinary.ID, lib.ID, owner.ID)
	if removedBatch >= addedBatch {
		t.Fatal("auxiliary activation did not follow the separately committed ordinary reservation removal")
	}
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 1 || resources[0].id != ordinary.ID || !resources[0].active || resources[0].ownerID != owner.ID {
		t.Fatal("the permanent role transition replaced the original ordinary identity")
	}
	if forceProbeUserDataSnapshot(t, ctx, pool, ordinary.ID) != userData {
		t.Fatal("permanent classification changed user data for the retained identity")
	}
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	assertNoCatalogTestNotification(t, notifications)
}

func TestAuxiliaryCatalogChangesOrdinaryOwnerMoveRetiresChildrenInItsCommit(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	destinationRoot := filepath.Join(root, "a-destination")
	destination := filepath.Join(destinationRoot, "Target", "Main.mp4")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	source := libraryIntegrationFile(t, root, "z-source/Film/Main.mp4", "video:moving-owner")
	song := libraryIntegrationFile(t, root, "z-source/Film/theme.mp3", "audio:retained-theme")
	clip := libraryIntegrationFile(t, root, "z-source/Film/featurettes/Clip.mp4", "video:retained-extra")
	lib, err := store.CreateLibrary(ctx, "Moving ordinary owner", "movies", []string{destinationRoot, filepath.Join(root, "z-source")})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	owner := nfoCatalogItem(t, ctx, store, userID, lib.ID, source)
	target := nfoCatalogItem(t, ctx, store, userID, lib.ID, filepath.Dir(destination))
	resources := append(themeScanTestResources(t, ctx, pool, lib.ID), extraScanTestResources(t, ctx, pool, lib.ID)...)
	if len(resources) != 2 {
		t.Fatal("prepare both active auxiliary roles on the moving Movie")
	}
	userData := make(map[string]string)
	for _, resource := range resources {
		if !resource.active || resource.ownerID != owner.ID {
			t.Fatal("the original Movie did not own both active auxiliary resources")
		}
		userDataSeed(t, ctx, pool, userID, UserData{ItemID: resource.id, IsFavorite: true, PlayCount: 3})
		userData[resource.id] = forceProbeUserDataSnapshot(t, ctx, pool, resource.id)
	}
	retained := filepath.Join(root, "retained")
	if err := os.Mkdir(retained, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{song, clip} {
		if err := os.Rename(path, filepath.Join(retained, filepath.Base(path))); err != nil {
			t.Fatal(err)
		}
	}
	notifications := catalogChangesTestListener(t, store)
	if err := os.Rename(source, destination); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("move the ordinary owner before scanning its old root: %+v", job)
	}
	batches := auxiliaryCatalogBatches(t, notifications)
	moved, ownerBatch := requireAuxiliaryCatalogFact(t, batches, CatalogUpdated, owner.ID, lib.ID)
	if moved.ParentID != target.ID || moved.PreviousParentID != owner.ParentID {
		t.Fatal("ordinary owner movement lost its previous and current browse containers")
	}
	for _, resource := range resources {
		assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogRemoved)
		if batch := requireAuxiliaryCatalogResource(t, batches, CatalogRemoved, resource.id, lib.ID, owner.ID); batch != ownerBatch {
			t.Fatal("invalid auxiliary children were removed after their ordinary owner's move commit")
		}
		if _, err := store.GetItem(ctx, userID, resource.id); !errors.Is(err, ErrNotFound) {
			t.Fatal("an invalidated auxiliary child remained directly visible")
		}
		if forceProbeUserDataSnapshot(t, ctx, pool, resource.id) != userData[resource.id] {
			t.Fatal("ordinary owner movement changed the child's historical user data")
		}
	}
	retired := append(themeScanTestResources(t, ctx, pool, lib.ID), extraScanTestResources(t, ctx, pool, lib.ID)...)
	if len(retired) != len(resources) {
		t.Fatal("ordinary owner movement deleted accepted auxiliary relationship rows")
	}
	for _, resource := range retired {
		if resource.active || resource.ownerID != owner.ID || userData[resource.id] == "" {
			t.Fatal("child invalidation discarded or reassigned an accepted historical identity")
		}
	}
}

func TestAuxiliaryCatalogChangesTrackInheritedThemeGenresAndHonorEmptyControls(t *testing.T) {
	f := newAuxiliaryCatalogFixture(t, "theme", true)
	resource := f.resource(t)
	nfo := filepath.Join(f.root, "movies", "Film", "Main.nfo")
	writeOwnerGenres := func(genre string) {
		t.Helper()
		if err := os.WriteFile(nfo, []byte("<movie><genre>"+genre+"</genre></movie>"), 0o600); err != nil {
			t.Fatal(err)
		}
		if job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" {
			t.Fatalf("scan the ordinary owner's genre source: %+v", job)
		}
	}
	assertThemeGenres := func(want ...string) {
		t.Helper()
		result := themeScanTestQuery(t, f.ctx, f.store, f.userID, f.owner.ID)
		if len(result.ThemeSongsResult.Items) != 1 {
			t.Fatal("genre inheritance lost the active theme resource")
		}
		item := result.ThemeSongsResult.Items[0]
		if item.ID != resource.id || item.Metadata == nil || !slices.Equal(item.Metadata.Genres, want) || len(item.Entities.Genres) != len(want) {
			t.Fatalf("the active theme genre projection did not match the accepted controls: %+v", item)
		}
		for index, genre := range item.Entities.Genres {
			if genre.ID <= 0 || genre.Name != want[index] {
				t.Fatal("theme genre inheritance lost its real catalog entity identity")
			}
		}
	}
	writeOwnerGenres("Initial Drama")
	assertThemeGenres("Initial Drama")
	notifications := catalogChangesTestListener(t, f.store)
	writeOwnerGenres("Changed Adventure")
	batches := auxiliaryCatalogBatches(t, notifications)
	assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogUpdated)
	requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, resource.id, f.library.ID, f.owner.ID)
	assertThemeGenres("Changed Adventure")
	actor := metadataEditTestActor(t, f.ctx, f.pool, "auxiliary-genre-editor")
	detail := metadataEditTestDetail(t, f.ctx, f.store, actor, resource.id)
	detail = metadataEditTestUpdate(t, f.ctx, f.store, actor, detail,
		map[string]json.RawMessage{"Genres": json.RawMessage(`[]`)}, nil)
	if string(detail.Overrides["Genres"]) != "[]" {
		t.Fatal("the native metadata edit did not retain its explicit empty genre control")
	}
	// The native control edit has its own committed notification. Only the
	// following owner scan is expected to leave this resource unchanged.
	_ = auxiliaryCatalogBatches(t, notifications)
	assertThemeGenres()
	writeOwnerGenres("Later Comedy")
	batches = auxiliaryCatalogBatches(t, notifications)
	assertAuxiliaryCatalogKinds(t, batches, resource.id)
	requireAuxiliaryCatalogFact(t, batches, CatalogUpdated, f.owner.ID, f.library.ID)
	assertThemeGenres()
}

func TestAuxiliaryCatalogChangesDeferredFailureIsQuietAndRecoverable(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		t.Run(role, func(t *testing.T) {
			f := newAuxiliaryCatalogFixture(t, role, true)
			resource := f.resource(t)
			userDataSeed(t, f.ctx, f.pool, f.userID, UserData{ItemID: resource.id, IsFavorite: true, PlayCount: 2})
			userData := forceProbeUserDataSnapshot(t, f.ctx, f.pool, resource.id)
			before := f.snapshot(t)
			if _, err := f.pool.Exec(f.ctx, `CREATE FUNCTION reject_auxiliary_catalog_commit() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN IF NEW.relative_path IN ('Film/theme.mp3', 'Film/featurettes/Clip.mp4')
				THEN RAISE EXCEPTION 'auxiliary catalog commit rejected'; END IF; RETURN NEW; END; $$;
				CREATE CONSTRAINT TRIGGER reject_auxiliary_catalog_commit AFTER UPDATE ON items
				DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_auxiliary_catalog_commit()`); err != nil {
				t.Fatal(err)
			}
			notifications := catalogChangesTestListener(t, f.store)
			if err := os.WriteFile(f.source, []byte(auxiliaryCatalogContents(role, "rejected-source-with-new-size")), 0o600); err != nil {
				t.Fatal(err)
			}
			job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Failed")
			if job.Error == "" {
				t.Fatal("the delayed auxiliary commit failure was not observed")
			}
			assertNoCatalogTestNotification(t, notifications)
			if f.snapshot(t) != before || forceProbeUserDataSnapshot(t, f.ctx, f.pool, resource.id) != userData {
				t.Fatal("a rolled-back auxiliary notification changed accepted resource or user state")
			}
			if _, err := f.pool.Exec(f.ctx, "DROP TRIGGER reject_auxiliary_catalog_commit ON items"); err != nil {
				t.Fatal(err)
			}
			if job := libraryIntegrationScan(t, f.ctx, f.store, f.library.ID, "Completed"); job.Error != "" {
				t.Fatalf("recover the rejected auxiliary update: %+v", job)
			}
			batches := auxiliaryCatalogBatches(t, notifications)
			assertAuxiliaryCatalogKinds(t, batches, resource.id, CatalogUpdated)
			requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, resource.id, f.library.ID, f.owner.ID)
			if f.resource(t).id != resource.id || forceProbeUserDataSnapshot(t, f.ctx, f.pool, resource.id) != userData {
				t.Fatal("recovery changed the retained auxiliary identity or user data")
			}
		})
	}
}

func TestAuxiliaryCatalogChangesEarlierRootSurvivesLaterCommitFailure(t *testing.T) {
	var block atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	fixture := &libraryFixtureProber{}
	prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if filepath.Base(file.Name()) == "Second.mp4" && block.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return media.Info{}, ctx.Err()
			}
		}
		return fixture.ProbeFile(ctx, file)
	})
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	libraryIntegrationFile(t, root, "first/Film/Main.mp4", "video:first-owner")
	firstPath := libraryIntegrationFile(t, root, "first/Film/featurettes/First.mp4", "video:first-before")
	libraryIntegrationFile(t, root, "second/Film/Main.mp4", "video:second-owner")
	secondPath := libraryIntegrationFile(t, root, "second/Film/featurettes/Second.mp4", "video:second-before")
	lib, err := store.CreateLibrary(ctx, "Partial auxiliary notification scan", "movies", []string{filepath.Join(root, "first"), filepath.Join(root, "second")})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
	resources := extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 2 || resources[0].path != firstPath || resources[1].path != secondPath {
		t.Fatal("prepare the two ordered auxiliary roots")
	}
	first, second := resources[0], resources[1]
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: second.id, IsFavorite: true, PlayCount: 5})
	secondData := forceProbeUserDataSnapshot(t, ctx, pool, second.id)
	secondSnapshot := metadataEditTestSnapshot(t, ctx, pool, second.id)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_second_auxiliary_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.relative_path = 'Film/featurettes/Second.mp4' THEN RAISE EXCEPTION 'second auxiliary commit rejected'; END IF; RETURN NEW; END; $$;
		CREATE CONSTRAINT TRIGGER reject_second_auxiliary_commit AFTER UPDATE ON items
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_second_auxiliary_commit()`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte("video:changed-auxiliary-source-with-new-size"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	notifications := catalogChangesTestListener(t, store)
	block.Store(true)
	job, err := store.StartScan(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("the second auxiliary root did not reach its controlled probe")
	}
	batches := auxiliaryCatalogBatches(t, notifications)
	assertAuxiliaryCatalogKinds(t, batches, first.id, CatalogUpdated)
	requireAuxiliaryCatalogResource(t, batches, CatalogUpdated, first.id, lib.ID, first.ownerID)
	if len(auxiliaryCatalogKinds(batches, second.id)) != 0 {
		t.Fatal("the blocked second auxiliary source published before its transaction")
	}
	resources = extraScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 2 || resources[0].probe.Size == first.probe.Size {
		t.Fatal("the first notification was not independently visible while the second root remained blocked")
	}
	releaseOnce.Do(func() { close(release) })
	finished := libraryIntegrationWaitJob(t, ctx, store, job.ID, "Failed")
	if finished.Error == "" {
		t.Fatal("the second auxiliary transaction did not fail")
	}
	assertNoCatalogTestNotification(t, notifications)
	if metadataEditTestSnapshot(t, ctx, pool, second.id) != secondSnapshot || forceProbeUserDataSnapshot(t, ctx, pool, second.id) != secondData {
		t.Fatal("the failed second root changed its accepted auxiliary snapshot or user data")
	}
}

func auxiliaryCatalogBatches(t *testing.T, notifications <-chan CatalogNotification) []CatalogNotification {
	t.Helper()
	var batches []CatalogNotification
	for {
		select {
		case notification := <-notifications:
			if notification.Resync || len(notification.Changes) == 0 {
				t.Fatal("a bounded auxiliary transaction replaced its exact facts with an empty batch or resynchronization")
			}
			batches = append(batches, notification)
		default:
			return batches
		}
	}
}

func auxiliaryCatalogKinds(batches []CatalogNotification, itemID string) []CatalogChangeKind {
	var kinds []CatalogChangeKind
	for _, batch := range batches {
		for _, change := range batch.Changes {
			if change.ItemID == itemID {
				kinds = append(kinds, change.Kind)
			}
		}
	}
	return kinds
}

func assertAuxiliaryCatalogKinds(t *testing.T, batches []CatalogNotification, itemID string, want ...CatalogChangeKind) {
	t.Helper()
	if got := auxiliaryCatalogKinds(batches, itemID); !slices.Equal(got, want) {
		t.Fatalf("auxiliary identity %s received change kinds %v, want %v", itemID, got, want)
	}
}

func requireAuxiliaryCatalogFact(t *testing.T, batches []CatalogNotification, kind CatalogChangeKind, itemID, libraryID string) (CatalogChange, int) {
	t.Helper()
	var found CatalogChange
	index := -1
	for batchIndex, batch := range batches {
		for _, change := range batch.Changes {
			if change.ItemID != itemID || change.Kind != kind {
				continue
			}
			if change.LibraryID != libraryID {
				t.Fatal("auxiliary publication used a foreign library scope")
			}
			if index != -1 {
				t.Fatalf("auxiliary identity %s repeated change kind %d in separate or duplicate facts", itemID, kind)
			}
			found, index = change, batchIndex
		}
	}
	if index == -1 {
		t.Fatalf("auxiliary publication omitted change kind %d for identity %s: %+v", kind, itemID, batches)
	}
	return found, index
}

func requireAuxiliaryCatalogResource(t *testing.T, batches []CatalogNotification, kind CatalogChangeKind, itemID, libraryID, ownerID string) int {
	t.Helper()
	change, index := requireAuxiliaryCatalogFact(t, batches, kind, itemID, libraryID)
	assertAuxiliaryCatalogScope(t, change, libraryID)
	// The resource transition and its visible semantic owner's invalidation
	// must describe the same committed transaction, including forced refreshes.
	requireAuxiliaryCatalogFact(t, batches[index:index+1], CatalogUpdated, ownerID, libraryID)
	return index
}

func assertAuxiliaryCatalogScope(t *testing.T, change CatalogChange, libraryID string) {
	t.Helper()
	if change.LibraryID != libraryID || change.ParentID != "" || change.PreviousParentID != "" ||
		change.IsFolder || change.IsCollectionFolder || change.ChildrenAdded || change.ChildrenRemoved {
		t.Fatalf("auxiliary resource treated its semantic Movie owner as a browse folder: %+v", change)
	}
}
