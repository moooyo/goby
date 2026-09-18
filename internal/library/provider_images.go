package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

const maxProviderImageCacheBytes int64 = 512 << 20
const maxProviderImageCacheEntries int64 = 10000

type ProviderImageResult struct {
	Provider   string `json:"Provider"`
	ProviderID string `json:"ProviderId"`
	ImageID    string `json:"ImageId"`
	ImageType  string `json:"ImageType"`
	ImageIndex int    `json:"ImageIndex"`
	Width      int    `json:"Width"`
	Height     int    `json:"Height"`
	Tag        string `json:"Tag"`
}

func (s *Store) SelectProviderImage(ctx context.Context, actor identity.Principal, itemID, revision string, selected providers.RemoteImage, index int, data []byte) (ProviderImageResult, error) {
	if !validMetadataActor(actor) {
		return ProviderImageResult{}, ErrForbidden
	}
	imageType, err := normalizeStoredImageType(selected.ImageType, index)
	if err != nil || (imageType != "Backdrop" && index != 0) || selected.Provider != "tmdb" || !metadataIdentifier(selected.ID) || !metadataIdentifier(selected.ImageID) || len(data) == 0 || int64(len(data)) > storedImageReadLimit {
		return ProviderImageResult{}, ErrInvalidInput
	}
	info, err := artwork.InspectContext(ctx, bytes.NewReader(data))
	if err != nil {
		return ProviderImageResult{}, fmt.Errorf("%w: provider image is invalid", ErrInvalidInput)
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return ProviderImageResult{}, err
	}
	defer rollback(tx)
	if err := lockMetadataActor(ctx, tx, actor); err != nil {
		return ProviderImageResult{}, err
	}
	record, err := readMetadataRecord(ctx, tx, itemID, true)
	if err != nil {
		return ProviderImageResult{}, err
	}
	if revision != strconv.FormatInt(record.revision, 10) || selected.Type != record.itemType {
		return ProviderImageResult{}, ErrRevisionConflict
	}
	var bytesUsed, entries, previous int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(octet_length(content)),0),count(*) FROM item_provider_images`).Scan(&bytesUsed, &entries); err != nil {
		return ProviderImageResult{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT octet_length(content) FROM item_provider_images WHERE item_id=$1 AND image_type=$2 AND image_index=$3),0)`, itemID, imageType, index).Scan(&previous); err != nil {
		return ProviderImageResult{}, err
	}
	if bytesUsed-previous+int64(len(data)) > maxProviderImageCacheBytes || (previous == 0 && entries >= maxProviderImageCacheEntries) {
		return ProviderImageResult{}, fmt.Errorf("%w: provider image cache is full; run cache maintenance", ErrUnavailable)
	}
	before, err := readAuxiliaryCatalogSnapshot(ctx, tx, []string{itemID})
	if err != nil {
		return ProviderImageResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(item_id,image_type,image_index) DO UPDATE SET provider=excluded.provider,provider_id=excluded.provider_id,image_id=excluded.image_id,content=excluded.content,mime_type=excluded.mime_type,width=excluded.width,height=excluded.height,source_hash=excluded.source_hash,fetched_at=clock_timestamp()`, itemID, imageType, index, selected.Provider, selected.ID, selected.ImageID, data, info.MIMEType, info.Width, info.Height, info.Tag); err != nil {
		return ProviderImageResult{}, err
	}
	change := CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: record.libraryID, ParentID: record.parentID, IsFolder: record.isFolder}
	if item, exists := before.items[itemID]; exists && !item.ordinary {
		change.ParentID = ""
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		return ProviderImageResult{}, err
	}
	if err := before.record(ctx, tx, nil); err != nil {
		return ProviderImageResult{}, err
	}
	if err := authorizeMetadataActor(ctx, tx, actor); err != nil {
		return ProviderImageResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProviderImageResult{}, err
	}
	return ProviderImageResult{Provider: selected.Provider, ProviderID: selected.ID, ImageID: selected.ImageID, ImageType: imageType, ImageIndex: index, Width: info.Width, Height: info.Height, Tag: info.Tag}, nil
}

// OpenImageContentFor checks current policy in the same transaction as reading
// selected online bytes; local artwork continues through its descriptor checks.
func (s *Store) OpenImageContentFor(ctx context.Context, subject Subject, itemID, imageType string, index int) (io.ReadCloser, Image, error) {
	if !validImageItemID(itemID) {
		return nil, Image{}, ErrInvalidInput
	}
	imageType, err := normalizeStoredImageType(imageType, index)
	if err != nil {
		return nil, Image{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, Image{}, err
	}
	defer rollback(tx)
	if _, err := readQueryParent(ctx, tx, itemID, access); err != nil {
		return nil, Image{}, err
	}
	var data []byte
	var source Image
	err = tx.QueryRow(ctx, `SELECT im.content,im.image_type,im.image_index,im.mime_type,im.width,im.height,im.source_hash,im.fetched_at FROM item_provider_images im JOIN items i ON i.id=im.item_id WHERE i.id=$1 AND im.image_type=$2 AND im.image_index=$3 AND ($4::boolean OR i.library_id=ANY($5::text[])) AND `+access.directSQL("i"), itemID, imageType, index, access.all, access.folders).Scan(&data, &source.ImageType, &source.ImageIndex, &source.MIMEType, &source.Width, &source.Height, &source.Tag, &source.ModifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		rollback(tx)
		return s.OpenImageFor(ctx, subject, itemID, imageType, index)
	}
	if err != nil {
		return nil, Image{}, fmt.Errorf("%w: read selected artwork", ErrUnavailable)
	}
	if len(data) == 0 || int64(len(data)) > storedImageReadLimit {
		return nil, Image{}, ErrUnavailable
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != source.Tag {
		return nil, Image{}, ErrUnavailable
	}
	source.Size = int64(len(data))
	if err := tx.Commit(ctx); err != nil {
		return nil, Image{}, err
	}
	return io.NopCloser(bytes.NewReader(data)), source, nil
}

func mergeProviderImageListing(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string, result map[string][]Image) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT i.id,im.image_type,im.image_index,im.mime_type,im.width,im.height,octet_length(im.content),im.source_hash,im.fetched_at FROM item_provider_images im JOIN items i ON i.id=im.item_id WHERE i.id=ANY($1::text[]) AND ($2::boolean OR i.library_id=ANY($3::text[])) AND `+access.directSQL("i"), ids, access.all, access.folders)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var source Image
		if err := rows.Scan(&id, &source.ImageType, &source.ImageIndex, &source.MIMEType, &source.Width, &source.Height, &source.Size, &source.Tag, &source.ModifiedAt); err != nil {
			return err
		}
		images := result[id]
		replaced := false
		for index := range images {
			if images[index].ImageType == source.ImageType && images[index].ImageIndex == source.ImageIndex {
				images[index] = source
				replaced = true
				break
			}
		}
		if !replaced {
			images = append(images, source)
		}
		sort.Slice(images, func(i, j int) bool {
			if images[i].ImageType == images[j].ImageType {
				return images[i].ImageIndex < images[j].ImageIndex
			}
			rank := map[string]int{"Primary": 0, "Backdrop": 1, "Thumb": 2, "Banner": 3, "Logo": 4, "Art": 5}
			return rank[images[i].ImageType] < rank[images[j].ImageType]
		})
		result[id] = images
	}
	return rows.Err()
}
