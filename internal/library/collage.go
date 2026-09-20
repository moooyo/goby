package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
)

// A manifest is computed from the same policy snapshot as its selected member
// images. It never contains paths, user secrets, or another subject's members.
// Changing membership, source precedence, bytes, or source version changes Tag.
const collageManifestVersion = "collage-v1-512-center-cover"

type collageMember struct {
	ItemID   string
	Source   string
	Tag      string
	Revision string
}

type collageManifest struct {
	Version string
	Kind    string
	ID      string
	Members []collageMember
	Tag     string    `json:"-"`
	Changed time.Time `json:"-"`
}

func (manifest *collageManifest) image() Image {
	return Image{ImageType: "Primary", MIMEType: "image/png", Width: 512, Height: 512,
		Tag: manifest.Tag, Source: "generated", SourceRevision: manifest.Tag, ModifiedAt: manifest.Changed}
}

func hasPrimaryImage(images []Image) bool {
	for _, image := range images {
		if image.ImageType == "Primary" && image.ImageIndex == 0 {
			return true
		}
	}
	return false
}

// Image selection remains in SQL so an arbitrarily long prefix of items with
// no artwork cannot starve later usable members. Managed Primary ownership,
// including a deletion tombstone, suppresses every automatic member source.
const collageMemberImageSQL = ` JOIN LATERAL (
	SELECT source,source_hash,version,modified_at FROM (
		SELECT 0 AS priority,'managed' AS source,im.source_hash,
			jsonb_build_array(state.revision,im.modified_at,im.source_hash)::text AS version,im.modified_at
		FROM artwork_state state JOIN artwork_images im ON im.state_id=state.id
		WHERE state.item_id=i.id AND 'Primary'=ANY(state.managed_types) AND im.image_type='Primary' AND im.image_index=0
		UNION ALL
		SELECT 1,'provider',im.source_hash,jsonb_build_array(im.provider,im.provider_id,im.image_id,im.source_hash,im.mime_type,im.width,im.height,im.fetched_at)::text,im.fetched_at
		FROM item_provider_images im WHERE im.item_id=i.id AND im.image_type='Primary' AND im.image_index=0
		AND NOT EXISTS (SELECT 1 FROM artwork_state state WHERE state.item_id=i.id AND 'Primary'=ANY(state.managed_types))
		UNION ALL
		SELECT 2,'sidecar',im.source_hash,jsonb_build_array(to_jsonb(im),r.binding_revision,r.storage_binding)::text,im.modified_at
		FROM item_images im JOIN library_roots r ON r.id=im.root_id AND r.id=i.root_id AND r.library_id=i.library_id
		WHERE im.item_id=i.id AND im.image_type='Primary' AND im.image_index=0
		AND NOT EXISTS (SELECT 1 FROM artwork_state state WHERE state.item_id=i.id AND 'Primary'=ANY(state.managed_types))
		UNION ALL
		SELECT 3,'embedded',e.source_hash,jsonb_build_array(e.source_revision,e.extraction_version,e.stream_index,e.picture_type,e.source_hash,e.mime_type,e.width,e.height,e.inspected_at)::text,e.inspected_at
		FROM item_embedded_artwork e WHERE e.item_id=i.id AND ` + embeddedArtworkCurrentSQL + `
		AND NOT EXISTS (SELECT 1 FROM artwork_state state WHERE state.item_id=i.id AND 'Primary'=ANY(state.managed_types))
	) choices ORDER BY priority LIMIT 1
) selected ON true `

func readCollageManifest(ctx context.Context, tx pgx.Tx, access libraryAccess, target ArtworkTarget) (*collageManifest, error) {
	stored, err := target.stored()
	if err != nil {
		return nil, err
	}
	manifest := &collageManifest{Version: collageManifestVersion, Kind: stored.Kind, ID: stored.ID, Members: []collageMember{}}
	var filter string
	var argument any
	if target.ItemID != "" {
		var libraryID string
		err := tx.QueryRow(ctx, `SELECT i.library_id,i.updated_at FROM items i WHERE i.id=$1 AND i.type='CollectionFolder' AND `+access.directSQL("i"), target.ItemID).Scan(&libraryID, &manifest.Changed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var automatic bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM item_provider_images WHERE item_id=$1 AND image_type='Primary' AND image_index=0)
			OR EXISTS (SELECT 1 FROM item_images im JOIN items i ON i.id=im.item_id JOIN library_roots r ON r.id=im.root_id AND r.id=i.root_id AND r.library_id=i.library_id
			WHERE i.id=$1 AND im.image_type='Primary' AND im.image_index=0)`, target.ItemID).Scan(&automatic); err != nil {
			return nil, err
		}
		if automatic {
			return nil, nil
		}
		filter, argument = "i.library_id=$1", libraryID
	} else {
		var kind string
		if err := tx.QueryRow(ctx, `SELECT kind FROM catalog_entities WHERE id=$1`, target.EntityID).Scan(&kind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		if kind != "Genre" {
			return nil, nil
		}
		if err := visibleEntityForState(ctx, tx, access, target.EntityID, false); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		filter, argument = "EXISTS (SELECT 1 FROM item_entities association WHERE association.item_id=i.id AND association.entity_id=$1)", target.EntityID
	}
	set, err := artwork.ReadManagedSet(ctx, tx, stored, false, false)
	if err != nil {
		return nil, err
	}
	if set.Manages("Primary") {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `WITH eligible AS (
		SELECT i.id,lower(i.sort_name) COLLATE "C" AS sort_name,selected.source,selected.source_hash,
		jsonb_build_array(i.updated_at,selected.version)::text AS version,selected.modified_at
		FROM items i `+collageMemberImageSQL+` WHERE `+filter+` AND i.type<>'CollectionFolder' AND `+access.ordinarySQL("i")+`
	), representatives AS (
		SELECT DISTINCT ON (source_hash) * FROM eligible ORDER BY source_hash,sort_name,id COLLATE "C"
	) SELECT id,source,source_hash,version,modified_at FROM representatives ORDER BY sort_name,id COLLATE "C" LIMIT 4`, argument)
	if err != nil {
		return nil, fmt.Errorf("select authorized collage members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member collageMember
		var version string
		var modified time.Time
		if err := rows.Scan(&member.ItemID, &member.Source, &member.Tag, &version, &modified); err != nil {
			return nil, err
		}
		digest := sha256.Sum256([]byte(version))
		member.Revision = hex.EncodeToString(digest[:])
		if modified.After(manifest.Changed) {
			manifest.Changed = modified
		}
		manifest.Members = append(manifest.Members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	manifest.Tag = hex.EncodeToString(digest[:])
	return manifest, nil
}

func mergeCollageImageListing(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string, result map[string][]Image) error {
	if len(ids) == 0 {
		return nil
	}
	// Resolve only aggregate targets in one query; ordinary image-less pages
	// must not trigger a new database query for each movie or music track.
	rows, err := tx.Query(ctx, `SELECT i.id,'item' FROM items i WHERE i.id=ANY($1::text[]) AND i.type='CollectionFolder' AND `+access.directSQL("i")+`
		UNION ALL SELECT entity.id::text,'entity' FROM catalog_entities entity WHERE entity.id::text=ANY($1::text[]) AND entity.kind='Genre'
		AND NOT EXISTS(SELECT 1 FROM items collision WHERE collision.id=entity.id::text)
		AND EXISTS(SELECT 1 FROM item_entities association JOIN items i ON i.id=association.item_id WHERE association.entity_id=entity.id
		AND i.type<>'CollectionFolder' AND `+access.ordinarySQL("i")+`) ORDER BY 1`, ids)
	if err != nil {
		return err
	}
	var targets []ArtworkTarget
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			rows.Close()
			return err
		}
		if hasPrimaryImage(result[id]) {
			continue
		}
		target := ArtworkTarget{ItemID: id}
		if kind == "entity" {
			entityID, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				rows.Close()
				return err
			}
			target = ArtworkTarget{EntityID: entityID}
		}
		targets = append(targets, target)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, target := range targets {
		manifest, err := readCollageManifest(ctx, tx, access, target)
		if err != nil {
			return err
		}
		if manifest != nil {
			result[manifest.ID] = append([]Image{manifest.image()}, result[manifest.ID]...)
		}
	}
	return nil
}

func (s *Store) openCollageFor(ctx context.Context, subject Subject, target ArtworkTarget) (io.ReadCloser, Image, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, Image{}, err
	}
	defer rollback(tx)
	return s.openCollageInSnapshot(ctx, tx, access, target)
}

func (s *Store) openCollageInSnapshot(ctx context.Context, tx pgx.Tx, access libraryAccess, target ArtworkTarget) (io.ReadCloser, Image, error) {
	manifest, err := readCollageManifest(ctx, tx, access, target)
	if err != nil {
		return nil, Image{}, err
	}
	if manifest == nil {
		return nil, Image{}, ErrNotFound
	}
	sources := make([][]byte, 0, len(manifest.Members))
	for _, member := range manifest.Members {
		data, source, err := s.readCollageMember(ctx, tx, member)
		if err != nil {
			return nil, Image{}, err
		}
		if source.Tag != member.Tag {
			return nil, Image{}, ErrRevisionConflict
		}
		sources = append(sources, data)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, Image{}, err
	}
	// Every source descriptor and digest has been checked before this lookup.
	// The response handler repeats authorization/selection after rendering.
	rendered, err := renderCachedCollage(ctx, manifest.Tag, sources)
	if err != nil {
		return nil, Image{}, err
	}
	image := manifest.image()
	image.ContentTag, image.Size = rendered.Source.Tag, int64(len(rendered.Bytes))
	return io.NopCloser(bytes.NewReader(rendered.Bytes)), image, nil
}

func (s *Store) readCollageMember(ctx context.Context, tx pgx.Tx, member collageMember) ([]byte, Image, error) {
	switch member.Source {
	case "managed":
		image, owned, err := artwork.ReadManagedImage(ctx, tx, artwork.Target{Kind: "item", ID: member.ItemID}, "Primary", 0)
		if err != nil {
			return nil, Image{}, err
		}
		if !owned || image.Tag == "" {
			return nil, Image{}, ErrRevisionConflict
		}
		return image.Content, imageFromManaged(image), nil
	case "embedded":
		return s.readEmbeddedImageContent(ctx, tx, member.ItemID)
	case "provider":
		var data []byte
		image := Image{ImageType: "Primary", Source: "provider"}
		err := tx.QueryRow(ctx, `SELECT content,mime_type,width,height,source_hash,fetched_at FROM item_provider_images WHERE item_id=$1 AND image_type='Primary' AND image_index=0`, member.ItemID).
			Scan(&data, &image.MIMEType, &image.Width, &image.Height, &image.Tag, &image.ModifiedAt)
		if err != nil {
			return nil, Image{}, err
		}
		if len(data) == 0 || int64(len(data)) > storedImageReadLimit {
			return nil, Image{}, ErrUnavailable
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != image.Tag {
			return nil, Image{}, ErrUnavailable
		}
		image.Size = int64(len(data))
		return data, image, nil
	case "sidecar":
		stored, err := scanStoredImage(tx.QueryRow(ctx, "SELECT "+storedImageColumns+storedImageSource+` WHERE i.id=$1 AND im.image_type='Primary' AND im.image_index=0`, member.ItemID))
		if err != nil {
			return nil, Image{}, err
		}
		var data []byte
		_, _, err = runPublicImageWorker(ctx, publicImageWorkers, func() (*os.File, Image, error) {
			file, err := s.openStoredImage(ctx, stored)
			if err != nil {
				return nil, Image{}, err
			}
			defer file.Close()
			data, err = io.ReadAll(io.LimitReader(imageContextReader{ctx: ctx, reader: file}, storedImageReadLimit+1))
			return nil, stored.Image, err
		})
		if err != nil {
			return nil, Image{}, err
		}
		digest := sha256.Sum256(data)
		if int64(len(data)) != stored.Size || hex.EncodeToString(digest[:]) != stored.Tag {
			return nil, Image{}, ErrUnavailable
		}
		return data, stored.Image, nil
	default:
		return nil, Image{}, ErrUnavailable
	}
}

// This cache contains only bounded, immutable rendered pixels, not a retained
// authorization decision. Shared subjects can reuse identical manifests only
// after selecting and validating those members in their own current snapshot.
var collagePixels = struct {
	sync.Mutex
	entries map[string]artwork.Result
	order   []string
	bytes   int
}{entries: make(map[string]artwork.Result)}

func renderCachedCollage(ctx context.Context, tag string, sources [][]byte) (artwork.Result, error) {
	if err := ctx.Err(); err != nil {
		return artwork.Result{}, err
	}
	collagePixels.Lock()
	result, found := collagePixels.entries[tag]
	collagePixels.Unlock()
	if found {
		return result, nil
	}
	result, err := artwork.Collage(ctx, sources)
	if err != nil {
		return artwork.Result{}, err
	}
	if cap(result.Bytes) > 2<<20 {
		return result, nil
	}
	collagePixels.Lock()
	defer collagePixels.Unlock()
	if _, exists := collagePixels.entries[tag]; !exists {
		for len(collagePixels.order) >= 32 || collagePixels.bytes+cap(result.Bytes) > 16<<20 {
			oldest := collagePixels.order[0]
			collagePixels.order = collagePixels.order[1:]
			collagePixels.bytes -= cap(collagePixels.entries[oldest].Bytes)
			delete(collagePixels.entries, oldest)
		}
		collagePixels.entries[tag] = result
		collagePixels.order = append(collagePixels.order, tag)
		collagePixels.bytes += cap(result.Bytes)
	}
	return result, nil
}
