package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
)

type ArtworkTarget struct {
	ItemID   string
	EntityID int64
}

func (target ArtworkTarget) stored() (artwork.Target, error) {
	if target.EntityID > 0 && target.ItemID == "" {
		return artwork.Target{Kind: "entity", ID: strconv.FormatInt(target.EntityID, 10)}, nil
	}
	if target.EntityID == 0 && metadataIdentifier(target.ItemID) {
		return artwork.Target{Kind: "item", ID: target.ItemID}, nil
	}
	return artwork.Target{}, ErrInvalidInput
}

type ArtworkCollection struct {
	Revision string
	Items    []Image
}

var artworkMutationSlots = make(chan struct{}, 2)

func (s *Store) GetArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget) (ArtworkCollection, error) {
	if _, err := target.stored(); err != nil {
		return ArtworkCollection{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ArtworkCollection{}, err
	}
	defer rollback(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, audience, false); err != nil {
		return ArtworkCollection{}, err
	}
	if _, err := artworkTargetRecord(ctx, tx, target, false); err != nil {
		return ArtworkCollection{}, err
	}
	result, err := readArtworkCollection(ctx, tx, target)
	if err != nil {
		return ArtworkCollection{}, err
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, audience, false); err != nil {
		return ArtworkCollection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ArtworkCollection{}, err
	}
	return result, nil
}

func artworkTargetRecord(ctx context.Context, tx pgx.Tx, target ArtworkTarget, lock bool) (CatalogChange, error) {
	if target.EntityID > 0 {
		return CatalogChange{}, visibleEntityForState(ctx, tx, unrestrictedLibraryAccess(), target.EntityID, lock)
	}
	query := `SELECT id,library_id,COALESCE(parent_id,''),is_folder,type='CollectionFolder' FROM items WHERE id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	change := CatalogChange{Kind: CatalogUpdated}
	err := tx.QueryRow(ctx, query, target.ItemID).Scan(&change.ItemID, &change.LibraryID, &change.ParentID, &change.IsFolder, &change.IsCollectionFolder)
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogChange{}, ErrNotFound
	}
	return change, err
}

func imageFromManaged(image artwork.StoredImage) Image {
	return Image{ImageType: image.ImageType, ImageIndex: image.ImageIndex, Tag: image.Tag, MIMEType: image.MIMEType,
		Width: image.Width, Height: image.Height, Size: image.Size, ModifiedAt: image.ModifiedAt, Source: "managed"}
}

func managedImageMetadata(image Image) artwork.StoredImage {
	return artwork.StoredImage{ImageType: image.ImageType, ImageIndex: image.ImageIndex, Tag: image.Tag, MIMEType: image.MIMEType,
		Width: image.Width, Height: image.Height, Size: image.Size, ModifiedAt: image.ModifiedAt}
}

func readArtworkCollection(ctx context.Context, tx pgx.Tx, target ArtworkTarget) (ArtworkCollection, error) {
	stored, err := target.stored()
	if err != nil {
		return ArtworkCollection{}, err
	}
	set, err := artwork.ReadManagedSet(ctx, tx, stored, false, false)
	if err != nil {
		return ArtworkCollection{}, err
	}
	images := make([]Image, 0)
	if target.ItemID != "" {
		images, err = automaticItemImages(ctx, tx, target.ItemID)
		if err != nil {
			return ArtworkCollection{}, err
		}
	}
	if !set.Manages("Primary") && !hasPrimaryImage(images) {
		manifest, err := readCollageManifest(ctx, tx, unrestrictedLibraryAccess(), target)
		if err != nil {
			return ArtworkCollection{}, err
		}
		if manifest != nil {
			images = append(images, manifest.image())
		}
	}
	images = mergeArtworkSet(images, set)
	metadata := make([]artwork.StoredImage, len(images))
	for index, image := range images {
		metadata[index] = managedImageMetadata(image)
	}
	revision := artwork.RevisionToken(stored, set.Revision, metadata)
	// A new media descriptor may contain identical embedded cover bytes. Editing
	// must still reject a stale automatic-source snapshot in that case.
	var sources []struct {
		Type     string
		Index    int
		Revision string
	}
	for _, image := range images {
		if image.SourceRevision != "" {
			sources = append(sources, struct {
				Type     string
				Index    int
				Revision string
			}{image.ImageType, image.ImageIndex, image.SourceRevision})
		}
	}
	if len(sources) != 0 {
		encoded, err := json.Marshal(sources)
		if err != nil {
			return ArtworkCollection{}, err
		}
		digest := sha256.Sum256(append([]byte("artwork-source-revision-v1/"+revision+"/"), encoded...))
		revision = new(big.Int).SetBytes(digest[:]).String()
	}
	return ArtworkCollection{Revision: revision, Items: images}, nil
}

func automaticItemImages(ctx context.Context, tx pgx.Tx, itemID string) ([]Image, error) {
	rows, err := tx.Query(ctx, "SELECT "+storedImageColumns+storedImageSource+" WHERE i.id=$1"+storedImageOrder, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	images := make([]Image, 0)
	for rows.Next() {
		stored, err := scanStoredImage(rows)
		if err != nil {
			return nil, err
		}
		images = append(images, stored.Image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	result := map[string][]Image{itemID: images}
	if err := mergeEmbeddedImageListing(ctx, tx, unrestrictedLibraryAccess(), []string{itemID}, result); err != nil {
		return nil, err
	}
	if err := mergeProviderImageListing(ctx, tx, unrestrictedLibraryAccess(), []string{itemID}, result); err != nil {
		return nil, err
	}
	return result[itemID], nil
}

func mergeArtworkSet(images []Image, set artwork.ManagedSet) []Image {
	result := make([]Image, 0, len(images)+len(set.Images))
	for _, image := range images {
		if !set.Manages(image.ImageType) {
			result = append(result, image)
		}
	}
	for _, image := range set.Images {
		result = append(result, imageFromManaged(image))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ImageType == result[j].ImageType {
			return result[i].ImageIndex < result[j].ImageIndex
		}
		return artworkTypeRank(result[i].ImageType) < artworkTypeRank(result[j].ImageType)
	})
	return result
}

func artworkTypeRank(value string) int {
	for index, candidate := range artwork.ManagedTypes {
		if value == candidate {
			return index
		}
	}
	return len(artwork.ManagedTypes)
}

func (s *Store) UploadArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, revision, imageType string, index int, data []byte) (ArtworkCollection, error) {
	return s.mutateArtwork(ctx, actor, audience, target, revision, imageType, "upload", index, nil, data)
}

func (s *Store) DeleteArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, revision, imageType string, index int) (ArtworkCollection, error) {
	return s.mutateArtwork(ctx, actor, audience, target, revision, imageType, "delete", index, nil, nil)
}

func (s *Store) ReorderArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, revision, imageType string, indexes []int) (ArtworkCollection, error) {
	return s.mutateArtwork(ctx, actor, audience, target, revision, imageType, "reorder", 0, append([]int(nil), indexes...), nil)
}

func (s *Store) ResetArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, revision, imageType string) (ArtworkCollection, error) {
	return s.mutateArtwork(ctx, actor, audience, target, revision, imageType, "reset", 0, nil, nil)
}

func (s *Store) mutateArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, revision, imageType, operation string, index int, indexes []int, data []byte) (ArtworkCollection, error) {
	stored, err := target.stored()
	if err != nil {
		return ArtworkCollection{}, err
	}
	imageType, err = artwork.NormalizeManagedType(imageType)
	if err != nil || index < 0 || index >= 32 || !artwork.MultipleManagedImages(imageType) && index != 0 {
		return ArtworkCollection{}, ErrInvalidInput
	}
	// Authorize before decoding bytes or reading any baseline source content.
	before, err := s.GetArtwork(ctx, actor, audience, target)
	if err != nil {
		return ArtworkCollection{}, err
	}
	if revision != "" && revision != before.Revision {
		return ArtworkCollection{}, ErrRevisionConflict
	}
	select {
	case artworkMutationSlots <- struct{}{}:
		defer func() { <-artworkMutationSlots }()
	case <-ctx.Done():
		return ArtworkCollection{}, ctx.Err()
	}
	var replacement artwork.StoredImage
	if operation == "upload" {
		replacement, err = artwork.PrepareManagedImage(ctx, imageType, index, data)
		if err != nil {
			return ArtworkCollection{}, err
		}
	}
	var sources []Image
	for _, image := range before.Items {
		if image.ImageType == imageType {
			sources = append(sources, image)
		}
	}
	if operation == "delete" {
		found := false
		for _, image := range sources {
			found = found || image.ImageIndex == index
		}
		if !found {
			return ArtworkCollection{}, ErrNotFound
		}
	}
	if operation == "reorder" {
		if !artwork.MultipleManagedImages(imageType) || len(indexes) != len(sources) {
			return ArtworkCollection{}, ErrInvalidInput
		}
		seen := make(map[int]bool)
		for _, selected := range indexes {
			found := false
			for _, image := range sources {
				found = found || image.ImageIndex == selected
			}
			if !found || seen[selected] {
				return ArtworkCollection{}, ErrInvalidInput
			}
			seen[selected] = true
		}
	}
	prepared := make([]artwork.StoredImage, 0, len(sources)+1)
	var bytesUsed int64
	for _, image := range sources {
		if operation == "reset" || operation == "delete" && image.ImageIndex == index || operation == "upload" && image.ImageIndex == index {
			continue
		}
		if image.Size > artwork.ManagedTypeBytes-bytesUsed {
			return ArtworkCollection{}, artwork.ErrManagedLimit
		}
		reader, actual, err := s.openArtworkAsAdministrator(ctx, actor, audience, target, image.ImageType, image.ImageIndex)
		if err != nil {
			return ArtworkCollection{}, err
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, artwork.ManagedUploadBytes+1))
		_ = reader.Close()
		if readErr != nil || len(content) > artwork.ManagedUploadBytes || actual.Tag != image.Tag {
			return ArtworkCollection{}, ErrRevisionConflict
		}
		copy, err := artwork.PrepareManagedImage(ctx, imageType, image.ImageIndex, content)
		if err != nil || copy.Tag != image.Tag {
			return ArtworkCollection{}, ErrRevisionConflict
		}
		bytesUsed += copy.Size
		prepared = append(prepared, copy)
	}
	if operation == "upload" {
		prepared = append(prepared, replacement)
	}
	if operation == "reorder" {
		ordered := make([]artwork.StoredImage, 0, len(prepared))
		for position, old := range indexes {
			for _, image := range prepared {
				if image.ImageIndex == old {
					image.ImageIndex = position
					ordered = append(ordered, image)
					break
				}
			}
		}
		prepared = ordered
	} else {
		sort.Slice(prepared, func(i, j int) bool { return prepared[i].ImageIndex < prepared[j].ImageIndex })
		if operation == "delete" {
			for position := range prepared {
				prepared[position].ImageIndex = position
			}
		}
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return ArtworkCollection{}, err
	}
	defer rollback(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, audience, true); err != nil {
		return ArtworkCollection{}, err
	}
	if err := artwork.LockManagedArtwork(ctx, tx); err != nil {
		return ArtworkCollection{}, err
	}
	change, err := artworkTargetRecord(ctx, tx, target, true)
	if err != nil {
		return ArtworkCollection{}, err
	}
	current, err := readArtworkCollection(ctx, tx, target)
	if err != nil {
		return ArtworkCollection{}, err
	}
	if current.Revision != before.Revision {
		return ArtworkCollection{}, ErrRevisionConflict
	}
	if _, err := artwork.ReplaceManagedType(ctx, tx, stored, imageType, prepared, operation == "reset"); err != nil {
		return ArtworkCollection{}, err
	}
	if target.ItemID != "" {
		if err := recordCatalogChanges(tx, change); err != nil {
			return ArtworkCollection{}, err
		}
	}
	result, err := readArtworkCollection(ctx, tx, target)
	if err != nil {
		return ArtworkCollection{}, err
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, audience, false); err != nil {
		return ArtworkCollection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ArtworkCollection{}, err
	}
	return result, nil
}

func (s *Store) openArtworkAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, imageType string, index int) (io.ReadCloser, Image, error) {
	if _, err := s.GetArtwork(ctx, actor, audience, target); err != nil {
		return nil, Image{}, err
	}
	subject := Subject{UserID: actor.User.ID}
	if actor.IsApplicationKey() {
		subject = Subject{ApplicationCredentialID: actor.SessionID}
	}
	if target.EntityID > 0 {
		return s.OpenEntityImageFor(ctx, subject, target.EntityID, imageType, index)
	}
	return s.OpenImageContentFor(ctx, subject, target.ItemID, imageType, index)
}

// OpenArtwork provides administrator-only native previews with the same source
// checks as compatibility image delivery; it does not turn media images public.
func (s *Store) OpenArtwork(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, target ArtworkTarget, imageType string, index int) (io.ReadCloser, Image, error) {
	return s.openArtworkAsAdministrator(ctx, actor, audience, target, imageType, index)
}

func (s *Store) OpenEntityImageFor(ctx context.Context, subject Subject, entityID int64, imageType string, index int) (io.ReadCloser, Image, error) {
	imageType, err := normalizeStoredImageType(imageType, index)
	if err != nil || entityID <= 0 {
		return nil, Image{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, Image{}, err
	}
	defer rollback(tx)
	if err := visibleEntityForState(ctx, tx, access, entityID, false); err != nil {
		return nil, Image{}, err
	}
	image, owned, err := artwork.ReadManagedImage(ctx, tx, artwork.Target{Kind: "entity", ID: strconv.FormatInt(entityID, 10)}, imageType, index)
	if err != nil {
		return nil, Image{}, err
	}
	if image.Tag == "" {
		if !owned && imageType == "Primary" && index == 0 {
			return s.openCollageInSnapshot(ctx, tx, access, ArtworkTarget{EntityID: entityID})
		}
		return nil, Image{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, Image{}, err
	}
	return io.NopCloser(bytes.NewReader(image.Content)), imageFromManaged(image), nil
}

func (s *Store) ListEntityImagesFor(ctx context.Context, subject Subject, entityID int64) ([]Image, error) {
	entity, err := s.GetEntityByIDFor(ctx, subject, entityID)
	if err != nil {
		return nil, err
	}
	return entity.Images, nil
}

// This map is updated only after the caller has selected currently visible
// items. The query repeats their ACL rather than treating a requested ID as proof.
func mergeManagedImageListing(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string, result map[string][]Image) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT state.item_id,state.managed_types FROM artwork_state state JOIN items i ON i.id=state.item_id WHERE i.id=ANY($1::text[]) AND `+access.directSQL("i"), ids)
	if err != nil {
		return err
	}
	sets := make(map[string]artwork.ManagedSet)
	visibleIDs := make([]string, 0)
	for rows.Next() {
		var id string
		var names []string
		if err := rows.Scan(&id, &names); err != nil {
			rows.Close()
			return err
		}
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			canonical, err := artwork.NormalizeManagedType(name)
			if err != nil || canonical != name || seen[name] {
				rows.Close()
				return artwork.ErrManagedStorage
			}
			seen[name] = true
		}
		sets[id] = artwork.ManagedSet{Types: names, Images: []artwork.StoredImage{}}
		visibleIDs = append(visibleIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(visibleIDs) == 0 {
		return nil
	}
	rows, err = tx.Query(ctx, `SELECT state.item_id,im.image_type,im.image_index,im.source_hash,im.mime_type,im.width,im.height,octet_length(im.content),im.modified_at
		FROM artwork_state state JOIN artwork_images im ON im.state_id=state.id
		WHERE state.item_id=ANY($1::text[]) ORDER BY state.item_id,im.image_type,im.image_index`, visibleIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var image artwork.StoredImage
		if err := rows.Scan(&id, &image.ImageType, &image.ImageIndex, &image.Tag, &image.MIMEType, &image.Width, &image.Height, &image.Size, &image.ModifiedAt); err != nil {
			rows.Close()
			return err
		}
		set := sets[id]
		if !set.Manages(image.ImageType) {
			rows.Close()
			return artwork.ErrManagedStorage
		}
		set.Images = append(set.Images, image)
		sets[id] = set
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for id, set := range sets {
		result[id] = mergeArtworkSet(result[id], set)
	}
	return nil
}

func mergeEntityImageListing(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string, result map[string][]Image) error {
	var entities []int64
	for _, id := range ids {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == id {
			entities = append(entities, parsed)
		}
	}
	if len(entities) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT entity.id::text,im.image_type,im.image_index,im.source_hash,im.mime_type,im.width,im.height,octet_length(im.content),im.modified_at
		FROM artwork_state state JOIN catalog_entities entity ON entity.id=state.entity_id JOIN artwork_images im ON im.state_id=state.id
		WHERE entity.id=ANY($1::bigint[]) AND NOT EXISTS (SELECT 1 FROM items collision WHERE collision.id=entity.id::text)
		AND EXISTS (SELECT 1 FROM item_entities association JOIN items i ON i.id=association.item_id WHERE association.entity_id=entity.id
		AND i.type<>'CollectionFolder' AND `+access.ordinarySQL("i")+" AND "+validEntityAssociationSQL+`) ORDER BY entity.id,im.image_type,im.image_index`, entities)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var image artwork.StoredImage
		if err := rows.Scan(&id, &image.ImageType, &image.ImageIndex, &image.Tag, &image.MIMEType, &image.Width, &image.Height, &image.Size, &image.ModifiedAt); err != nil {
			return err
		}
		result[id] = append(result[id], imageFromManaged(image))
	}
	return rows.Err()
}
