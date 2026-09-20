package library

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// MusicMixSeed keeps catalog item identities separate from numeric entities.
// Item is the compatibility dispatch route: an existing item reserves its ID,
// even when hidden; only absent items may resolve to a music artist or genre.
type MusicMixSeed struct {
	Kind string
	ID   string
	Name string
}

type InstantMixQuery struct {
	Query
	ExcludeArtistIds []int64
	ExplicitSort     bool
}

type resolvedMusicMixSeed struct {
	kind, itemID string
	entityID     int64
}

func normalizeMusicMixSeed(seed MusicMixSeed) (MusicMixSeed, error) {
	if seed.Kind == "GenreName" {
		if seed.ID != "" || len(seed.Name) > 1024 || strings.TrimSpace(seed.Name) == "" || !utf8.ValidString(seed.Name) || strings.IndexFunc(seed.Name, unicode.IsControl) >= 0 {
			return MusicMixSeed{}, ErrInvalidInput
		}
		return seed, nil
	}
	if seed.Name != "" || seed.ID == "" || len(seed.ID) > 256 || strings.TrimSpace(seed.ID) != seed.ID || !utf8.ValidString(seed.ID) || strings.IndexFunc(seed.ID, unicode.IsControl) >= 0 {
		return MusicMixSeed{}, ErrInvalidInput
	}
	switch seed.Kind {
	case "Item", "Song", "Album", "Playlist":
	case "Artist", "Genre":
		id, err := strconv.ParseInt(seed.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != seed.ID {
			return MusicMixSeed{}, ErrInvalidInput
		}
	default:
		return MusicMixSeed{}, ErrInvalidInput
	}
	return seed, nil
}

// QueryInstantMix creates a stateless queue from the current authorized music
// catalog. Metadata weights, deterministic ties and unrelated fallback are a
// Goby policy, not a reconstruction of another server's private algorithm.
// It never creates playback sessions or changes played/favorite history.
func (s *Store) QueryInstantMix(ctx context.Context, seed MusicMixSeed, query InstantMixQuery) (ItemResult, error) {
	seed, err := normalizeMusicMixSeed(seed)
	if err != nil || len(query.ExcludeArtistIds) > 1024 {
		return ItemResult{}, ErrInvalidInput
	}
	for _, id := range query.ExcludeArtistIds {
		if id <= 0 {
			return ItemResult{}, ErrInvalidInput
		}
	}
	limit := query.Limit
	if query.ParentID == "" {
		query.Recursive = true
	}
	query.Query, err = normalizeItemQuery(query.Query)
	if err != nil {
		return ItemResult{}, err
	}
	query.Limit = limit
	subject := Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return ItemResult{}, err
	}
	defer rollback(tx)
	resolved, err := resolveMusicMixSeed(ctx, tx, access, subject, seed)
	if err != nil {
		return ItemResult{}, err
	}
	if !access.canPlay {
		return ItemResult{}, ErrForbidden
	}
	parent, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return ItemResult{}, err
	}
	prefix, args := instantMixRelationSQL(resolved, query, access, parent)
	result := ItemResult{Items: []Item{}}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM mix_candidates", args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, fmt.Errorf("count instant mix: %w", err)
	}
	order := "ranked.score DESC, lower(i.sort_name), i.id"
	if query.ExplicitSort {
		userParameter := 0
		if itemSortUsesUserData(query.SortBy) {
			args = append(args, query.UserID)
			userParameter = len(args)
		}
		order = access.scopeSQL(itemOrderSQL(query.Query, userParameter))
	}
	args = append(args, query.Limit, query.StartIndex)
	prefix += `, mix_page AS MATERIALIZED (
		SELECT i.id,row_number() OVER (ORDER BY ` + order + `) AS ordinal
		FROM mix_scores ranked JOIN items i ON i.id=ranked.id
		ORDER BY ordinal` + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)) + ") "
	rows, err := tx.Query(ctx, prefix+`SELECT `+access.itemColumnsSQL()+` FROM mix_page page JOIN items i ON i.id=page.id ORDER BY page.ordinal`, args...)
	if err != nil {
		return ItemResult{}, fmt.Errorf("query instant mix: %w", err)
	}
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return ItemResult{}, fmt.Errorf("read instant mix: %w", err)
		}
		item.CanPlay = true
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ItemResult{}, fmt.Errorf("finish instant mix rows: %w", err)
	}
	if err := attachUserData(ctx, tx, query.UserID, result.Items, access); err != nil {
		return ItemResult{}, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete instant mix: %w", err)
	}
	return result, nil
}

func resolveMusicMixSeed(ctx context.Context, tx pgx.Tx, access libraryAccess, subject Subject, seed MusicMixSeed) (resolvedMusicMixSeed, error) {
	if seed.Kind == "Artist" || seed.Kind == "Genre" || seed.Kind == "GenreName" {
		return resolveMusicMixEntity(ctx, tx, access, subject, seed)
	}
	var kind string
	var folder, visible bool
	err := tx.QueryRow(ctx, `SELECT i.type,i.is_folder,`+access.ordinarySQL("i")+` FROM items i WHERE i.id=$1`, seed.ID).Scan(&kind, &folder, &visible)
	if errors.Is(err, pgx.ErrNoRows) {
		if seed.Kind == "Item" {
			id, parseErr := strconv.ParseInt(seed.ID, 10, 64)
			if parseErr == nil && id > 0 && strconv.FormatInt(id, 10) == seed.ID {
				return resolveMusicMixEntity(ctx, tx, access, subject, MusicMixSeed{Kind: "Entity", ID: seed.ID})
			}
		}
		return resolvedMusicMixSeed{}, ErrNotFound
	}
	if err != nil {
		return resolvedMusicMixSeed{}, fmt.Errorf("authorize instant mix item: %w", err)
	}
	if !visible {
		return resolvedMusicMixSeed{}, ErrNotFound
	}
	resolved := resolvedMusicMixSeed{itemID: seed.ID}
	switch {
	case kind == "Audio" && !folder && (seed.Kind == "Item" || seed.Kind == "Song"):
		resolved.kind = "Song"
	case kind == "MusicAlbum" && folder && (seed.Kind == "Item" || seed.Kind == "Album"):
		resolved.kind = "Album"
	case kind == PlaylistKind && (seed.Kind == "Item" || seed.Kind == "Playlist"):
		if _, err := readCollection(ctx, tx, access, seed.ID, PlaylistKind, false); err != nil {
			return resolvedMusicMixSeed{}, err
		}
		resolved.kind = "Playlist"
	default:
		return resolvedMusicMixSeed{}, ErrNotFound
	}
	return resolved, nil
}

func resolveMusicMixEntity(ctx context.Context, tx pgx.Tx, access libraryAccess, subject Subject, seed MusicMixSeed) (resolvedMusicMixSeed, error) {
	family, kind := "allartists", "Artist"
	if seed.Kind == "Genre" || seed.Kind == "GenreName" {
		family, kind = "genres", "Genre"
	} else if seed.Kind == "Entity" {
		var storedKind string
		err := tx.QueryRow(ctx, `SELECT kind FROM catalog_entities WHERE id=$1::bigint`, seed.ID).Scan(&storedKind)
		if errors.Is(err, pgx.ErrNoRows) {
			return resolvedMusicMixSeed{}, ErrNotFound
		}
		if err != nil {
			return resolvedMusicMixSeed{}, fmt.Errorf("resolve instant mix entity kind: %w", err)
		}
		switch storedKind {
		case "MusicArtist":
		case "Genre":
			family, kind = "genres", "Genre"
		default:
			return resolvedMusicMixSeed{}, ErrNotFound
		}
	}
	query := Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID, Recursive: true}
	prefix, args, err := musicEntityQuerySQL(query, access, "", family)
	if err != nil {
		return resolvedMusicMixSeed{}, err
	}
	predicate := "id=$%d::bigint"
	value := seed.ID
	if seed.Kind == "GenreName" {
		predicate, value = "lower(btrim(name))=lower(btrim($%d::text))", seed.Name
	}
	args = append(args, value)
	var id int64
	err = tx.QueryRow(ctx, prefix+"SELECT id FROM eligible_entities WHERE "+fmt.Sprintf(predicate, len(args)), args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedMusicMixSeed{}, ErrNotFound
	}
	if err != nil {
		return resolvedMusicMixSeed{}, fmt.Errorf("authorize instant mix entity: %w", err)
	}
	return resolvedMusicMixSeed{kind: kind, entityID: id}, nil
}

func musicMixPlayableSQL(alias string) string {
	return alias + `.type='Audio' AND NOT ` + alias + `.is_folder AND ` + alias + `.media->>'ProbeVersion'=` + policySQLString(strconv.Itoa(media.CurrentProbeVersion)) +
		` AND ` + navigationNumberSQL(alias+".media", "FileChangeTimeNs") + `>0 AND ` + navigationNumberSQL(alias+".media", "DurationTicks") + `>0
		AND ` + alias + `.file_identity<>'' AND ` + alias + `.file_size>0 AND ` + alias + `.modified_at IS NOT NULL
		AND ` + alias + `.modified_at>'0001-01-01 00:00:00+00'::timestamptz AND ` + alias + `.path<>'' AND ` + alias + `.relative_path<>''
		AND EXISTS(SELECT 1 FROM library_roots mix_root WHERE mix_root.id=` + alias + `.root_id AND mix_root.library_id=` + alias + `.library_id)
		AND (COALESCE(` + navigationNumberSQL(alias+".media", "Size") + `,0)<=0 OR ` + navigationNumberSQL(alias+".media", "Size") + `=` + alias + `.file_size)
		AND EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(` + alias + `.media->'Streams')='array' THEN ` + alias + `.media->'Streams' ELSE '[]'::jsonb END) stream WHERE stream->>'CodecType'='audio')
		AND NOT EXISTS(SELECT 1 FROM media_operations publication WHERE publication.source_item_id=` + alias + `.id AND publication.publication_phase IN ('prepared','catalog_committed'))`
}

func instantMixRelationSQL(seed resolvedMusicMixSeed, query InstantMixQuery, access libraryAccess, parent string) (string, []any) {
	prefix, filter, args := itemQuerySQL(query.Query, access, parent)
	if prefix == "" {
		prefix = "WITH "
	} else {
		prefix = strings.TrimSpace(prefix) + ", "
	}
	args = append(args, seed.itemID, seed.entityID)
	itemParameter, entityParameter := len(args)-1, len(args)
	prefix += fmt.Sprintf(`mix_input AS (SELECT $%d::text AS item_id,$%d::bigint AS entity_id), `, itemParameter, entityParameter)
	prefix += `mix_scope AS MATERIALIZED (
		SELECT i.id,i.type,` + access.scopeSQL(physicalMusicAlbumIDSQL) + ` AS album_id,
		` + access.scopeSQL(effectiveMusicAlbumArtistItemSQL) + ` AS album_artist_item_id,
		(` + musicMixPlayableSQL("i") + `) AS playable FROM items i
		WHERE i.type IN ('Audio','MusicAlbum','MusicVideo') AND ` + access.ordinarySQL("i") + `
	), mix_credits AS MATERIALIZED (
		SELECT DISTINCT scope.id AS item_id,entity.id AS entity_id,entity.kind FROM mix_scope scope
		JOIN item_entities association ON association.item_id=scope.id JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE ` + similarOrdinaryCreditSQL + ` OR ` + similarArtistCreditSQL + `
		UNION SELECT scope.id,entity.id,entity.kind FROM mix_scope scope
		JOIN item_entities association ON association.item_id=scope.album_artist_item_id JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE ` + similarAlbumArtistCreditSQL + `
	), mix_seed_tracks AS MATERIALIZED (
		SELECT scope.id FROM mix_scope scope WHERE scope.playable AND `
	switch seed.kind {
	case "Song":
		prefix += fmt.Sprintf("scope.id=$%d::text", itemParameter)
	case "Album":
		prefix += fmt.Sprintf("scope.album_id=$%d::text", itemParameter)
	case "Playlist":
		prefix += fmt.Sprintf(`EXISTS(SELECT 1 FROM media_collection_entries membership WHERE membership.collection_id=$%d::text AND membership.item_id=scope.id)`, itemParameter)
	case "Artist", "Genre":
		prefix += fmt.Sprintf(`EXISTS(SELECT 1 FROM mix_credits credit WHERE credit.item_id=scope.id AND credit.entity_id=$%d::bigint)`, entityParameter)
	}
	prefix += `
	), mix_seed_items AS (
		SELECT id FROM mix_seed_tracks`
	if seed.kind == "Album" {
		prefix += fmt.Sprintf(" UNION SELECT $%d::text", itemParameter)
	}
	prefix += `
	), mix_seed_entities AS MATERIALIZED (
		SELECT DISTINCT credit.entity_id,credit.kind FROM mix_credits credit JOIN mix_seed_items seed ON seed.id=credit.item_id
	), mix_seed_albums AS MATERIALIZED (
		SELECT DISTINCT scope.album_id FROM mix_scope scope JOIN mix_seed_tracks seed ON seed.id=scope.id WHERE scope.album_id IS NOT NULL
	), mix_candidates AS MATERIALIZED (
		SELECT i.id FROM items i JOIN mix_scope scope ON scope.id=i.id WHERE scope.playable AND ` + filter + ` AND EXISTS(SELECT 1 FROM mix_seed_tracks)`
	if len(query.ExcludeArtistIds) != 0 {
		args = append(args, query.ExcludeArtistIds)
		prefix += fmt.Sprintf(` AND NOT EXISTS(SELECT 1 FROM mix_credits credit WHERE credit.item_id=i.id AND credit.kind='MusicArtist' AND credit.entity_id=ANY($%d::bigint[]))`, len(args))
	}
	prefix += `
	), mix_scores AS (
		SELECT candidate.id,
		CASE WHEN EXISTS(SELECT 1 FROM mix_seed_tracks seed WHERE seed.id=candidate.id) THEN 16 ELSE 0 END
		+ CASE WHEN EXISTS(SELECT 1 FROM mix_seed_albums album WHERE album.album_id=scope.album_id) THEN 8 ELSE 0 END
		+ COALESCE((SELECT sum(CASE credit.kind WHEN 'MusicArtist' THEN 4 WHEN 'Genre' THEN 2 ELSE 1 END)
			FROM mix_credits credit JOIN mix_seed_entities seed ON seed.entity_id=credit.entity_id AND seed.kind=credit.kind
			WHERE credit.item_id=candidate.id),0) AS score
		FROM mix_candidates candidate JOIN mix_scope scope ON scope.id=candidate.id
	) `
	return prefix, args
}
