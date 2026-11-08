package library

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

// ManualOnlineImageResult describes one explicit, missing-only image request.
// Artwork is a fresh, authorized snapshot within the requested library scope.
type ManualOnlineImageResult struct {
	Status    string
	ItemID    string
	LibraryID string
	ImageType string
	Artwork   ArtworkCollection
}

func manualOnlineImageType(imageType string) bool {
	return imageType == "Primary" || imageType == "Backdrop" || imageType == "Logo"
}

func manualOnlineImageStatus(snapshot onlineImageSnapshot, imageType string) string {
	supported := false
	for _, candidate := range onlineImageTypes(snapshot.itemType) {
		supported = supported || candidate == imageType
	}
	if !snapshot.supported || !supported {
		return "unsupported"
	}
	if snapshot.managed.Manages(imageType) {
		return "managed"
	}
	if !snapshot.missing[imageType] {
		return "already_present"
	}
	if snapshot.selection.ID == "" {
		return "unmatched"
	}
	return ""
}

// Manual requests use the same ordinary catalog boundary as worker admission.
// A rootless container or an item attached to another library is not a target.
func readManualOnlineImageTarget(ctx context.Context, tx pgx.Tx, libraryID, itemID string, lock bool) (string, error) {
	statement := `SELECT root.id FROM items i JOIN library_roots root ON root.id=i.root_id AND root.library_id=i.library_id
		WHERE i.id=$1 AND i.library_id=$2 AND i.id<>i.library_id AND i.library_id<>$3
		AND i.type<>'CollectionFolder' AND ` + ordinaryItemSQL("i")
	if lock {
		statement += " FOR SHARE OF root"
	}
	var rootID string
	if err := tx.QueryRow(ctx, statement, itemID, libraryID, CollectionsLibraryID).Scan(&rootID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return rootID, nil
}

func (s *Store) manualOnlineImageSnapshot(ctx context.Context, actor identity.Principal, libraryID, itemID string) (onlineImageSnapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return onlineImageSnapshot{}, err
	}
	defer rollback(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return onlineImageSnapshot{}, err
	}
	result, err := readOnlineImageSnapshotWithPolicy(ctx, tx, libraryID, itemID, false, true)
	if err != nil {
		return result, err
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// manualOnlineImageResult keeps GetArtwork's authorization and artwork snapshot
// boundary while also checking the library scope in that same transaction.
func (s *Store) manualOnlineImageResult(ctx context.Context, actor identity.Principal, libraryID, itemID, imageType, status string, before *onlineImageSnapshot) (ManualOnlineImageResult, error) {
	result := ManualOnlineImageResult{Status: status, ItemID: itemID, LibraryID: libraryID, ImageType: imageType}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return result, err
	}
	if before != nil {
		current, err := readOnlineImageSnapshotWithPolicy(ctx, tx, libraryID, itemID, false, true)
		if err != nil {
			return result, err
		}
		if current.libraryRevision != before.libraryRevision || current.metadataRevision != before.metadataRevision ||
			current.artworkRevision != before.artworkRevision || current.rootID != before.rootID || !sameOnlineImageIdentity(current.selection, before.selection) {
			return result, ErrRevisionConflict
		}
	}
	if _, err := readManualOnlineImageTarget(ctx, tx, libraryID, itemID, false); err != nil {
		return result, err
	}
	target := ArtworkTarget{ItemID: itemID}
	change, err := artworkTargetRecord(ctx, tx, target, false)
	if err != nil {
		return result, err
	}
	if change.LibraryID != libraryID {
		return result, ErrNotFound
	}
	result.Artwork, err = readArtworkCollection(ctx, tx, target)
	if err != nil {
		return result, err
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// DownloadMissingOnlineImage handles one explicit native request without
// enabling automatic downloads. It only uses an existing TMDB identity and
// preserves all existing artwork and deliberate managed-type deletion masks.
func (s *Store) DownloadMissingOnlineImage(ctx context.Context, actor identity.Principal, client OnlineImageProvider, libraryID, itemID, imageType string) (ManualOnlineImageResult, error) {
	if ctx == nil || !validCatalogLibraryIdentifier(libraryID) || !metadataIdentifier(itemID) || !manualOnlineImageType(imageType) {
		return ManualOnlineImageResult{}, ErrInvalidInput
	}
	if !s.Available() {
		return ManualOnlineImageResult{}, ErrUnavailable
	}
	before, err := s.manualOnlineImageSnapshot(ctx, actor, libraryID, itemID)
	if err != nil {
		return ManualOnlineImageResult{}, err
	}
	if status := manualOnlineImageStatus(before, imageType); status != "" {
		return s.manualOnlineImageResult(ctx, actor, libraryID, itemID, imageType, status, &before)
	}
	if client == nil {
		return ManualOnlineImageResult{}, providers.ErrNotConfigured
	}
	images, err := client.Images(ctx, before.selection)
	if err != nil {
		return ManualOnlineImageResult{}, onlineImageProviderError(ctx, err)
	}
	if len(images) > 200 {
		return ManualOnlineImageResult{}, providers.ErrInvalidInput
	}
	var selected *providers.RemoteImage
	for index := range images {
		image := &images[index]
		if image.ImageType == imageType && sameOnlineImageIdentity(image.Selection, before.selection) {
			selected = image
			break
		}
	}
	if selected == nil {
		return s.manualOnlineImageResult(ctx, actor, libraryID, itemID, imageType, "no_candidate", &before)
	}
	guard := onlineImagePublicationGuard{administrator: &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}}
	written, err := s.downloadMissingOnlineImageGuarded(ctx, client, itemID, before, *selected, guard)
	if err != nil {
		return ManualOnlineImageResult{}, err
	}
	if !written {
		return ManualOnlineImageResult{}, ErrRevisionConflict
	}
	return s.manualOnlineImageResult(ctx, actor, libraryID, itemID, imageType, "downloaded", nil)
}
