package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

type extraCandidate struct {
	relative string
	kind     string
}

type extraDirectoryScan struct {
	relative   string
	candidates []extraCandidate
	failed     bool
	overflow   bool
}

type extraScan struct {
	markers map[string]bool
	groups  map[string]*extraDirectoryScan
}

func (state *scanState) startExtraScan() error {
	state.extras = &extraScan{markers: make(map[string]bool), groups: make(map[string]*extraDirectoryScan)}
	rows, err := state.store.pool.Query(state.task.ctx, `SELECT relative_path,is_directory FROM extra_reserved_paths WHERE root_id=$1
		UNION ALL SELECT i.relative_path,i.is_folder FROM items i JOIN item_extra_resources r ON r.resource_item_id=i.id WHERE i.root_id=$1`, state.root.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var relative string
		var directory bool
		if err := rows.Scan(&relative, &directory); err != nil {
			return err
		}
		if _, err := classifyExtraPath(relative, 0); err != nil {
			return fmt.Errorf("%w: a retained extra path is invalid", ErrUnavailable)
		}
		state.extras.markers[relative] = state.extras.markers[relative] || directory
	}
	return rows.Err()
}

func (state *scanState) extraPathReserved(relative string) bool {
	if state.extras == nil {
		return false
	}
	if _, exists := state.extras.markers[relative]; exists {
		return true
	}
	for index := strings.LastIndexByte(relative, '/'); index >= 0; index = strings.LastIndexByte(relative, '/') {
		relative = relative[:index]
		if state.extras.markers[relative] {
			return true
		}
	}
	return false
}

func (state *scanState) extraGroup(relative string) *extraDirectoryScan {
	relative = filepath.ToSlash(filepath.Clean(relative))
	group := state.extras.groups[relative]
	if group == nil {
		group = &extraDirectoryScan{relative: relative}
		state.extras.groups[relative] = group
	}
	return group
}

// Filter extras before the theme classifier and ordinary folder/movie walk.
// A directory marker protects nested and nonvideo contents without indexing
// them. Retained markers remain effective after collection-type changes.
func (state *scanState) classifyExtraDirectory(relative string, entries []os.DirEntry, info os.FileInfo) ([]os.DirEntry, error) {
	if state.extras == nil {
		return entries, nil
	}
	ordinary := make([]os.DirEntry, 0, len(entries))
	markers := make(map[string]bool)
	directories := []string{}
	for _, entry := range entries {
		name := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		classification := extraPathClassification{}
		var err error
		if state.library.CollectionType == "movies" {
			classification, err = classifyExtraPath(name, entry.Type())
		}
		retained := state.extraPathReserved(name)
		if err != nil {
			shape, _ := classifyExtraPath(name, os.ModeDir)
			if retained || shape.Reserved {
				state.warnings++
				state.extraGroup(relative).failed = true
				continue
			}
			ordinary = append(ordinary, entry)
			continue
		}
		if !retained && !classification.Reserved {
			ordinary = append(ordinary, entry)
			continue
		}
		observed, observeErr := state.opened.Lstat(filepath.FromSlash(name))
		if observeErr != nil || (!observed.Mode().IsRegular() && !observed.IsDir()) || observed.IsDir() != entry.IsDir() {
			state.warnings++
			state.extraGroup(relative).failed = true
			continue
		}
		// Existing theme reservations retain their role. Reserving the outer
		// extra boundary may hide an old owner, but does not convert its items.
		markers[name] = entry.IsDir()
		if entry.IsDir() && classification.Reserved && classification.OwnerDirectory == filepath.ToSlash(relative) {
			directories = append(directories, name)
		}
	}
	after, err := state.opened.Lstat(relative)
	if err != nil || !themeSnapshotEqual(info, after) {
		return nil, fmt.Errorf("%w: extra classification directory changed during enumeration", ErrUnavailable)
	}
	if err := state.persistAuxiliaryMarkers(markers, true); err != nil {
		return nil, err
	}
	for _, directory := range directories {
		group := state.extraGroup(relative)
		if err := state.enumerateExtraDirectory(group, directory); err != nil {
			if state.task.ctx.Err() != nil {
				return nil, state.task.ctx.Err()
			}
			state.warnings++
			group.failed = true
		}
	}
	return ordinary, nil
}

func (state *scanState) enumerateExtraDirectory(group *extraDirectoryScan, relative string) error {
	parent, err := openRegisteredRoot(state.opened, filepath.Dir(relative))
	if err != nil {
		return err
	}
	defer parent.Close()
	directory, err := openScanFile(parent, filepath.Base(relative))
	if err != nil {
		return err
	}
	defer directory.Close()
	before, err := directory.Stat()
	if err != nil || !before.IsDir() {
		return fmt.Errorf("extra directory is unavailable")
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		classification, err := classifyExtraPath(name, entry.Type())
		if err != nil {
			return err
		}
		if classification.Kind == "" {
			continue
		}
		if len(group.candidates) >= MaxExtraResourcesPerOwner {
			group.overflow = true
			continue
		}
		group.candidates = append(group.candidates, extraCandidate{relative: name, kind: classification.Kind})
	}
	after, err := directory.Stat()
	current, currentErr := parent.Lstat(filepath.Base(relative))
	if err != nil || currentErr != nil || !themeSnapshotEqual(before, after) || !themeSnapshotEqual(before, current) {
		return fmt.Errorf("extra directory changed during enumeration")
	}
	state.themes.directories[relative] = before
	return nil
}

// Invalid children of precisely the changed owners become inactive in the
// same transaction. No inactive history, identity, metadata, or UserData moves.
func deactivateInvalidExtraChildren(ctx context.Context, tx pgx.Tx, ownerIDs []string) error {
	if len(ownerIDs) == 0 {
		return nil
	}
	var locked []string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id),'{}'::text[]) FROM (
		SELECT resource.id FROM items resource JOIN item_extra_resources relationship ON relationship.resource_item_id=resource.id
		WHERE relationship.active AND relationship.owner_item_id=ANY($1::text[])
		ORDER BY resource.id FOR UPDATE OF resource) locked`, ownerIDs).Scan(&locked); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE item_extra_resources relationship SET active=false
		WHERE relationship.active AND relationship.owner_item_id=ANY($1::text[])
		AND NOT EXISTS(SELECT 1 FROM items resource WHERE resource.id=relationship.resource_item_id AND `+
		database.ExtraResourceItemSQL("resource", true)+`)`, ownerIDs)
	return err
}

func readExtraActiveResources(ctx context.Context, source themeRowQuerier, ownerID string) ([]themeActiveResource, error) {
	var raw []byte
	if err := source.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'root',root_id,
		'path',relative_path,'kind',kind) ORDER BY id),'[]'::jsonb) FROM (
		SELECT i.id,i.root_id,i.relative_path,r.kind FROM item_extra_resources r JOIN items i ON i.id=r.resource_item_id
		WHERE r.owner_item_id=$1 AND r.active ORDER BY i.id LIMIT $2) active`, ownerID, MaxExtraResourcesPerOwner+1).Scan(&raw); err != nil {
		return nil, err
	}
	var resources []themeActiveResource
	if err := json.Unmarshal(raw, &resources); err != nil {
		return nil, err
	}
	if len(resources) > MaxExtraResourcesPerOwner {
		return nil, fmt.Errorf("%w: existing extra owner population exceeds its bound", ErrInvalidInput)
	}
	return resources, nil
}

func (state *scanState) publishExtraOwner(ownerID string, files []*preparedThemeFile, kinds map[string]string) error {
	if err := state.verifyThemeDirectories(".", true); err != nil {
		return fmt.Errorf("%w: extra source directories changed before publication", ErrUnavailable)
	}
	if err := verifyPreparedThemeFiles(files); err != nil {
		return fmt.Errorf("%w: extra sources changed before publication", ErrUnavailable)
	}
	active, err := readExtraActiveResources(state.task.ctx, state.store.pool, ownerID)
	if err != nil {
		return err
	}
	expected := make([]string, 0, len(files))
	incoming := make(map[string]bool)
	for _, file := range files {
		if file.owner.id != ownerID || file.state != state || file.role != scannedRoleExtra || kinds[file.id] == "" || incoming[file.id] {
			return fmt.Errorf("%w: extra publication mixes identities, roles, or owners", ErrInvalidInput)
		}
		expected = append(expected, file.id)
		incoming[file.id] = true
	}
	if len(expected) > MaxExtraResourcesPerOwner {
		return fmt.Errorf("%w: extra owner candidate count exceeds its bound", ErrInvalidInput)
	}
	retire := []string{}
	for _, resource := range active {
		if incoming[resource.ID] {
			continue
		}
		classification, err := classifyExtraPath(resource.Relative, 0)
		if err != nil || classification.Kind == "" || classification.Kind != resource.Kind || resource.RootID != state.root.id {
			return fmt.Errorf("%w: an unobserved extra is outside the accepted root or layouts", ErrUnavailable)
		}
		absent, err := themePathAbsent(state.opened, resource.Relative)
		if err != nil || !absent {
			return fmt.Errorf("%w: an unobserved extra is present or its absence is uncertain", ErrUnavailable)
		}
		retire = append(retire, resource.ID)
	}
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var status string
	var cancelled bool
	if err := tx.QueryRow(state.task.ctx, `SELECT status,cancel_requested FROM scan_jobs WHERE id=$1 AND library_id=$2 FOR UPDATE`,
		state.task.job.ID, state.library.ID).Scan(&status, &cancelled); err != nil {
		return err
	}
	if cancelled || status != "Running" || state.task.ctx.Err() != nil {
		return context.Canceled
	}
	var locked []string
	if err := tx.QueryRow(state.task.ctx, `SELECT COALESCE(array_agg(id),'{}'::text[]) FROM (
		SELECT i.id FROM items i WHERE i.id=$1 OR i.id=ANY($2::text[]) OR i.id IN
		(SELECT resource_item_id FROM item_extra_resources WHERE owner_item_id=$1 AND active)
		ORDER BY i.id FOR UPDATE OF i) locked`, ownerID, expected).Scan(&locked); err != nil {
		return err
	}
	var validOwner bool
	if err := tx.QueryRow(state.task.ctx, `SELECT EXISTS(SELECT 1 FROM items owner WHERE owner.id=$1 AND owner.library_id=$2
		AND owner.root_id=$3 AND owner.type='Movie' AND NOT owner.is_folder AND `+ordinaryItemSQL("owner")+`)`,
		ownerID, state.library.ID, state.root.id).Scan(&validOwner); err != nil {
		return err
	}
	if !validOwner {
		return fmt.Errorf("%w: the extra Movie owner changed before publication", ErrUnavailable)
	}
	activeNow, err := readExtraActiveResources(state.task.ctx, tx, ownerID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(active, activeNow) {
		return fmt.Errorf("%w: the extra population changed after source verification", ErrUnavailable)
	}
	var invalidBefore int
	if err := tx.QueryRow(state.task.ctx, `SELECT count(*) FROM item_extra_resources r JOIN items i ON i.id=r.resource_item_id
		WHERE r.owner_item_id=$1 AND r.active AND NOT `+database.ExtraResourceItemSQL("i", true), ownerID).Scan(&invalidBefore); err != nil {
		return err
	}
	if invalidBefore != 0 {
		return fmt.Errorf("%w: the existing active extra population has an invalid shape", ErrUnavailable)
	}
	if len(retire) != 0 {
		tag, err := tx.Exec(state.task.ctx, `UPDATE item_extra_resources SET active=false
			WHERE owner_item_id=$1 AND active AND resource_item_id=ANY($2::text[])`, ownerID, retire)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != int64(len(retire)) {
			return fmt.Errorf("%w: the proved missing extra population changed", ErrUnavailable)
		}
	}
	roleChanged := make(map[string]bool)
	for _, file := range files {
		if err := persistThemeFile(state.task.ctx, tx, file); err != nil {
			return err
		}
		tag, err := tx.Exec(state.task.ctx, `INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
			VALUES($1,$2,$3,true) ON CONFLICT(resource_item_id) DO UPDATE
			SET owner_item_id=EXCLUDED.owner_item_id,kind=EXCLUDED.kind,active=true
			WHERE item_extra_resources.owner_item_id<>EXCLUDED.owner_item_id OR item_extra_resources.kind<>EXCLUDED.kind OR NOT item_extra_resources.active`,
			file.id, ownerID, kinds[file.id])
		if err != nil {
			return err
		}
		roleChanged[file.id] = tag.RowsAffected() != 0
	}
	if err := deactivateInvalidThemeChildren(state.task.ctx, tx, expected); err != nil {
		return err
	}
	var count, invalid int
	if err := tx.QueryRow(state.task.ctx, `SELECT count(*),count(*) FILTER(WHERE NOT `+database.ExtraResourceItemSQL("i", true)+`)
		FROM item_extra_resources r JOIN items i ON i.id=r.resource_item_id WHERE r.owner_item_id=$1 AND r.active`, ownerID).Scan(&count, &invalid); err != nil {
		return err
	}
	if count != len(expected) || invalid != 0 {
		return fmt.Errorf("%w: atomic extra publication did not preserve its complete valid shape", ErrUnavailable)
	}
	if err := state.verifyThemeDirectories(".", true); err != nil {
		return fmt.Errorf("%w: extra source directories changed before commit", ErrUnavailable)
	}
	if err := verifyPreparedThemeFiles(files); err != nil {
		return fmt.Errorf("%w: extra sources changed before commit", ErrUnavailable)
	}
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return err
	}
	for _, file := range files {
		if file.input.stored.itemType == "Audio" {
			state.queueMusicParent(file.input.stored.parentID)
		}
		if file.input.stored.id == "" {
			state.task.job.Added++
		} else if file.changed || roleChanged[file.id] {
			state.task.job.Updated++
		}
	}
	return state.store.persistProgress(state.task)
}

// Each Movie belongs to exactly one registered root, so its complete extra
// population can be decided after that root walk. No cross-root partial owner
// set is published. A failed primary/probe, ambiguous owner, overflow, changed
// directory, or unknown present historical source retains its accepted set.
func (state *scanState) finishExtraScan() error {
	if state.extras == nil || state.library.CollectionType != "movies" || state.warnings != 0 || state.themes == nil || !state.themes.walkComplete {
		return nil
	}
	if err := state.verifyThemeDirectories(".", true); err != nil {
		state.warnings++
		return nil
	}
	groups := make(map[string]*extraDirectoryScan)
	for _, group := range state.extras.groups {
		if group.failed || group.overflow || state.themeGroup(group.relative).failed {
			state.warnings++
			return nil
		}
		if len(group.candidates) == 0 {
			continue
		}
		movies := state.themes.movies[group.relative]
		if len(movies) != 1 {
			state.warnings++
			return nil
		}
		for ownerID := range movies {
			if groups[ownerID] != nil {
				state.warnings++
				return nil
			}
			groups[ownerID] = group
		}
	}
	rows, err := state.store.pool.Query(state.task.ctx, `SELECT DISTINCT r.owner_item_id FROM item_extra_resources r
		JOIN items i ON i.id=r.resource_item_id WHERE i.root_id=$1 AND r.active`, state.root.id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if groups[id] == nil {
			groups[id] = &extraDirectoryScan{}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	owners := make([]string, 0, len(groups))
	for ownerID := range groups {
		owners = append(owners, ownerID)
	}
	sort.Strings(owners)
	for _, ownerID := range owners {
		group := groups[ownerID]
		preparedGroup := &themeDirectoryScan{relative: group.relative, owner: themeDirectoryOwner{id: ownerID, itemType: "Movie"}}
		pathKinds := make(map[string]string)
		for _, candidate := range group.candidates {
			preparedGroup.candidates = append(preparedGroup.candidates, themeCandidate{relative: candidate.relative, kind: themePathKindVideo})
			pathKinds[candidate.relative] = candidate.kind
		}
		files, err := state.prepareAuxiliaryFiles(preparedGroup, scannedRoleExtra)
		if err != nil {
			return err
		}
		if files == nil {
			return nil
		}
		kinds := make(map[string]string)
		for _, file := range files {
			kinds[file.id] = pathKinds[file.candidate.relative]
		}
		err = state.publishExtraOwner(ownerID, files, kinds)
		closeThemeFiles(files)
		if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInvalidInput) {
			state.warnings++
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}
