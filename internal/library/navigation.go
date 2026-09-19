package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

// Ancestors returns nearest parents first. Every link must stay inside the
// seed's library and remain visible in the same current-policy snapshot.
func (s *Store) Ancestors(ctx context.Context, subject Subject, itemID string) ([]Item, error) {
	if !metadataIdentifier(itemID) {
		return nil, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	result := make([]Item, 0)
	if itemID == VirtualRootItemID {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return result, nil
	}
	var seed string
	if err := tx.QueryRow(ctx, "SELECT i.id FROM items i WHERE i.id=$1 AND "+access.directSQL("i"), itemID).Scan(&seed); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("authorize ancestor seed: %w", err)
	}
	rows, err := tx.Query(ctx, `WITH RECURSIVE ancestry AS (
		SELECT i.id,i.parent_id,i.library_id,0 AS depth,ARRAY[i.id] AS visited FROM items i WHERE i.id=$1
		UNION ALL SELECT parent.id,parent.parent_id,parent.library_id,child.depth+1,child.visited||parent.id
		FROM ancestry child JOIN items parent ON parent.id=child.parent_id AND parent.library_id=child.library_id
		WHERE child.depth < 128 AND parent.is_folder AND NOT parent.id=ANY(child.visited)
		AND `+access.ordinarySQL("parent")+`
	) SELECT `+access.itemColumnsSQL()+` FROM ancestry ancestor JOIN items i ON i.id=ancestor.id
		AND i.library_id=ancestor.library_id WHERE ancestor.depth>0 ORDER BY ancestor.depth`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query item ancestors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		item.CanPlay = access.canPlay
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := attachUserData(ctx, tx, subject.UserID, result, access); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// ItemCounts uses the pinned ItemCounts field names. ItemCount is the actual
// authorized recursive ordinary catalog count, including its visible containers.
// Individual kind counts are not advertised as a disjoint sum of that total.
type ItemCounts struct {
	MovieCount      int `json:"MovieCount"`
	SeriesCount     int `json:"SeriesCount"`
	EpisodeCount    int `json:"EpisodeCount"`
	GameCount       int `json:"GameCount"`
	ArtistCount     int `json:"ArtistCount"`
	ProgramCount    int `json:"ProgramCount"`
	GameSystemCount int `json:"GameSystemCount"`
	TrailerCount    int `json:"TrailerCount"`
	SongCount       int `json:"SongCount"`
	AlbumCount      int `json:"AlbumCount"`
	MusicVideoCount int `json:"MusicVideoCount"`
	BoxSetCount     int `json:"BoxSetCount"`
	BookCount       int `json:"BookCount"`
	ItemCount       int `json:"ItemCount"`
}

// CountItems applies authorization and the optional personal favorite predicate
// before grouping, using exactly one snapshot for the complete result.
func (s *Store) CountItems(ctx context.Context, subject Subject, favorite *bool) (ItemCounts, error) {
	if favorite != nil && subject.UserID == "" {
		return ItemCounts{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return ItemCounts{}, err
	}
	defer rollback(tx)
	query := Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID, Recursive: true, IsFavorite: favorite}
	prefix, conditions, args := itemQuerySQL(query, access, "")
	rows, err := tx.Query(ctx, prefix+`SELECT i.type,count(*) FROM items i WHERE `+conditions+` GROUP BY i.type`, args...)
	if err != nil {
		return ItemCounts{}, err
	}
	defer rows.Close()
	result := ItemCounts{}
	fields := map[string]*int{"Movie": &result.MovieCount, "Series": &result.SeriesCount, "Episode": &result.EpisodeCount,
		"Game": &result.GameCount, "MusicArtist": &result.ArtistCount, "Program": &result.ProgramCount, "GameSystem": &result.GameSystemCount,
		"Trailer": &result.TrailerCount, "Audio": &result.SongCount, "MusicAlbum": &result.AlbumCount, "MusicVideo": &result.MusicVideoCount,
		"BoxSet": &result.BoxSetCount, "Book": &result.BookCount}
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return ItemCounts{}, err
		}
		if count < 0 || count > math.MaxInt32-int64(result.ItemCount) {
			return ItemCounts{}, ErrUnavailable
		}
		result.ItemCount += int(count)
		if field := fields[kind]; field != nil {
			*field = int(count)
		}
	}
	if err := rows.Err(); err != nil {
		return ItemCounts{}, err
	}
	rows.Close()
	artistPrefix, artistArgs, err := musicEntityQuerySQL(query, access, "", "allartists")
	if err != nil {
		return ItemCounts{}, err
	}
	var artists int64
	if err := tx.QueryRow(ctx, artistPrefix+"SELECT count(*) FROM eligible_entities", artistArgs...).Scan(&artists); err != nil {
		return ItemCounts{}, err
	}
	if artists < 0 || artists > math.MaxInt32 {
		return ItemCounts{}, ErrUnavailable
	}
	result.ArtistCount = int(artists)
	trailerConditions := []string{database.ExtraResourceItemSQL("i", true), access.directSQL("i"),
		"EXISTS(SELECT 1 FROM item_extra_resources trailer WHERE trailer.resource_item_id=i.id AND trailer.active AND trailer.kind='trailer')"}
	trailerConditions, trailerArgs := addUserDataConditions(query, trailerConditions, nil, access)
	var trailers int64
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM items i WHERE "+strings.Join(trailerConditions, " AND "), trailerArgs...).Scan(&trailers); err != nil {
		return ItemCounts{}, err
	}
	if trailers < 0 || trailers > math.MaxInt32-int64(result.TrailerCount) {
		return ItemCounts{}, ErrUnavailable
	}
	result.TrailerCount += int(trailers)
	if err := tx.Commit(ctx); err != nil {
		return ItemCounts{}, err
	}
	return result, nil
}
