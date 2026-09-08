package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const storedImageReadLimit int64 = 20 * 1024 * 1024

// These slots are separate from HTTP request slots. A canceled request cannot
// release a worker whose filesystem operation is still blocked on storage.
var publicImageWorkers = make(chan struct{}, 4)

type publicImageWorkResult struct {
	file  *os.File
	image Image
	err   error
}

func runPublicImageWorker(ctx context.Context, slots chan struct{}, work func() (*os.File, Image, error)) (*os.File, Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, Image{}, err
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, Image{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slots
		return nil, Image{}, err
	}
	result := make(chan publicImageWorkResult)
	go func() {
		defer func() { <-slots }()
		if ctx.Err() != nil {
			return
		}
		file, image, err := work()
		if err != nil && file != nil {
			_ = file.Close()
			file = nil
		}
		select {
		case result <- publicImageWorkResult{file: file, image: image, err: err}:
			// Ownership moves to the receiver only after the unbuffered handoff.
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	select {
	case outcome := <-result:
		if err := ctx.Err(); err != nil {
			if outcome.file != nil {
				_ = outcome.file.Close()
			}
			return nil, Image{}, err
		}
		return outcome.file, outcome.image, outcome.err
	case <-ctx.Done():
		return nil, Image{}, ctx.Err()
	}
}

// Image describes a validated, indexed local image. Path and Filename are
// authorized item metadata and must not be included in public image responses.
type Image struct {
	ImageType           string
	ImageIndex          int
	Path, Filename, Tag string
	MIMEType            string
	Width, Height       int
	Size                int64
	ModifiedAt          time.Time
}

type storedImage struct {
	Image
	root                   libraryRoot
	relativePath, identity string
}

const storedImageColumns = `im.image_type, im.image_index, im.relative_path,
	im.file_identity, im.source_hash, im.file_size, im.modified_at,
	im.width, im.height, im.mime_type,
	r.id, r.library_id, r.path, r.allowed_path, r.relative_path`

// Both root relationships are checked explicitly because their foreign keys
// alone do not prevent a corrupt image or item from referencing another root.
const storedImageSource = ` FROM item_images im
	JOIN items i ON i.id = im.item_id
	JOIN library_roots r ON r.id = im.root_id AND r.id = i.root_id
		AND r.library_id = i.library_id `

const storedImageOrder = ` ORDER BY CASE im.image_type
	WHEN 'Primary' THEN 0 WHEN 'Backdrop' THEN 1 WHEN 'Thumb' THEN 2
	WHEN 'Banner' THEN 3 WHEN 'Logo' THEN 4 WHEN 'Art' THEN 5 ELSE 6 END,
	im.image_index`

// ListImages authorizes the item and reads its images in one policy snapshot.
// Missing and unauthorized items return the same error, including empty items.
func (s *Store) ListImages(ctx context.Context, userID, itemID string) ([]Image, error) {
	if !validImageItemID(itemID) {
		return nil, ErrInvalidInput
	}
	tx, access, err := s.beginUserRead(ctx, userID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := readQueryParent(ctx, tx, itemID, access); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT "+storedImageColumns+storedImageSource+`
		WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[]))`+storedImageOrder,
		itemID, access.all, access.folders)
	if err != nil {
		return nil, fmt.Errorf("%w: query item images: %w", ErrUnavailable, err)
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
		return nil, fmt.Errorf("%w: read item images: %w", ErrUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%w: complete item image read: %w", ErrUnavailable, err)
	}
	return images, nil
}

// ImagesForItems batches at most 1000 identifiers and returns image entries only
// for accessible catalog items, using the current database policy snapshot.
func (s *Store) ImagesForItems(ctx context.Context, userID string, ids []string) (map[string][]Image, error) {
	if len(ids) > 1000 {
		return nil, ErrInvalidInput
	}
	for _, id := range ids {
		if !validImageItemID(id) {
			return nil, ErrInvalidInput
		}
	}
	tx, access, err := s.beginUserRead(ctx, userID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	result := make(map[string][]Image)
	if len(ids) != 0 {
		rows, err := tx.Query(ctx, "SELECT i.id, "+storedImageColumns+storedImageSource+`
			WHERE i.id = ANY($1::text[]) AND ($2::boolean OR i.library_id = ANY($3::text[]))`+
			strings.Replace(storedImageOrder, " ORDER BY ", " ORDER BY i.id, ", 1),
			ids, access.all, access.folders)
		if err != nil {
			return nil, fmt.Errorf("%w: query item image batch: %w", ErrUnavailable, err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			stored, err := scanStoredImage(rows, &id)
			if err != nil {
				return nil, err
			}
			result[id] = append(result[id], stored.Image)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("%w: read item image batch: %w", ErrUnavailable, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%w: complete item image batch: %w", ErrUnavailable, err)
	}
	return result, nil
}

// OpenPublicImage deliberately does not require a user or token: indexed image
// binary routes are public. The caller owns the returned descriptor, positioned
// at offset zero. It must hash its bounded read against Image.Tag before serving
// or rendering those bytes because another process can modify a regular file
// after this function returns.
func (s *Store) OpenPublicImage(ctx context.Context, itemID, imageType string, index int) (*os.File, Image, error) {
	if !validImageItemID(itemID) {
		return nil, Image{}, ErrInvalidInput
	}
	imageType, err := normalizeStoredImageType(imageType, index)
	if err != nil {
		return nil, Image{}, err
	}
	if s == nil || s.pool == nil {
		return nil, Image{}, ErrUnavailable
	}
	return runPublicImageWorker(ctx, publicImageWorkers, func() (*os.File, Image, error) {
		stored, err := scanStoredImage(s.pool.QueryRow(ctx, "SELECT "+storedImageColumns+storedImageSource+`
			WHERE i.id = $1 AND im.image_type = $2 AND im.image_index = $3`, itemID, imageType, index))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, Image{}, ErrNotFound
		}
		if err != nil {
			return nil, Image{}, err
		}
		file, err := s.openStoredImage(ctx, stored)
		if err != nil {
			return nil, Image{}, err
		}
		return file, stored.Image, nil
	})
}

func scanStoredImage(row rowScanner, prefix ...any) (storedImage, error) {
	var image storedImage
	fields := []any{&image.ImageType, &image.ImageIndex, &image.relativePath,
		&image.identity, &image.Tag, &image.Size, &image.ModifiedAt,
		&image.Width, &image.Height, &image.MIMEType,
		&image.root.id, &image.root.libraryID, &image.root.path,
		&image.root.allowedPath, &image.root.relativePath}
	if err := row.Scan(append(prefix, fields...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedImage{}, err
		}
		return storedImage{}, fmt.Errorf("%w: read stored image: %w", ErrUnavailable, err)
	}
	image.ModifiedAt = image.ModifiedAt.UTC()
	if err := validateStoredImage(&image); err != nil {
		return storedImage{}, err
	}
	return image, nil
}

func validateStoredImage(image *storedImage) error {
	canonical, err := normalizeStoredImageType(image.ImageType, image.ImageIndex)
	if err != nil || canonical != image.ImageType || image.identity == "" ||
		image.Size <= 0 || image.Size > storedImageReadLimit || image.ModifiedAt.IsZero() ||
		image.Width <= 0 || image.Height <= 0 || image.Width > 16384 || image.Height > 16384 ||
		int64(image.Width)*int64(image.Height) > 25*1024*1024 {
		return fmt.Errorf("%w: invalid stored image metadata", ErrUnavailable)
	}
	switch image.ImageType {
	case "Primary", "Thumb", "Banner", "Logo", "Art":
		if image.ImageIndex != 0 {
			return fmt.Errorf("%w: invalid stored image index", ErrUnavailable)
		}
	case "Backdrop":
	default:
		return fmt.Errorf("%w: unsupported stored image type", ErrUnavailable)
	}
	digest, err := hex.DecodeString(image.Tag)
	if err != nil || len(digest) != sha256.Size || strings.ToLower(image.Tag) != image.Tag {
		return fmt.Errorf("%w: invalid stored image hash", ErrUnavailable)
	}
	switch image.MIMEType {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return fmt.Errorf("%w: unsupported stored image format", ErrUnavailable)
	}
	path := filepath.FromSlash(image.relativePath)
	if strings.Contains(image.relativePath, `\`) || !validStoredImageRelativePath(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: invalid stored image path", ErrUnavailable)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif":
	default:
		return fmt.Errorf("%w: unsupported stored image extension", ErrUnavailable)
	}
	root := image.root
	if !filepath.IsAbs(root.path) || !filepath.IsAbs(root.allowedPath) ||
		strings.ContainsRune(root.path, '\x00') || strings.ContainsRune(root.allowedPath, '\x00') ||
		hasTraversal(root.path) || hasTraversal(root.allowedPath) ||
		!validStoredImageRelativePath(root.relativePath) ||
		filepath.Clean(root.path) != filepath.Join(root.allowedPath, root.relativePath) {
		return fmt.Errorf("%w: invalid stored image root", ErrUnavailable)
	}
	image.Path = filepath.Join(root.path, path)
	if !pathWithin(root.path, image.Path) {
		return fmt.Errorf("%w: stored image is outside its root", ErrUnavailable)
	}
	image.Filename = filepath.Base(path)
	return nil
}

func validStoredImageRelativePath(path string) bool {
	return path != "" && !strings.ContainsRune(path, '\x00') &&
		filepath.IsLocal(path) && !hasTraversal(path)
}

func validImageItemID(id string) bool {
	return strings.TrimSpace(id) != "" && !strings.ContainsRune(id, '\x00')
}

func normalizeStoredImageType(value string, index int) (string, error) {
	if index < 0 || index > 31 {
		return "", ErrInvalidInput
	}
	var canonical string
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "primary":
		canonical = "Primary"
	case "backdrop":
		canonical = "Backdrop"
	case "thumb":
		canonical = "Thumb"
	case "banner":
		canonical = "Banner"
	case "logo":
		canonical = "Logo"
	case "art":
		canonical = "Art"
	case "disc":
		canonical = "Disc"
	case "box":
		canonical = "Box"
	case "boxrear":
		canonical = "BoxRear"
	case "menu":
		canonical = "Menu"
	case "screenshot":
		canonical = "Screenshot"
	default:
		return "", ErrInvalidInput
	}
	return canonical, nil
}

func (s *Store) openStoredImage(ctx context.Context, image storedImage) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.openLibraryRoot(image.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	path := filepath.FromSlash(image.relativePath)
	parent, err := openRegisteredRoot(root, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: image directory cannot be opened safely", ErrUnavailable)
	}
	defer parent.Close()
	name := filepath.Base(path)
	before, err := parent.Lstat(name)
	if err != nil || !image.matchesFile(before) {
		return nil, fmt.Errorf("%w: indexed image changed before opening", ErrUnavailable)
	}
	file, err := openScanFile(parent, name)
	if err != nil {
		return nil, fmt.Errorf("%w: indexed image cannot be opened safely", ErrUnavailable)
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()
	opened, err := file.Stat()
	if err != nil || !image.matchesFile(opened) || !sameImageFile(before, opened) {
		return nil, fmt.Errorf("%w: indexed image changed while opening", ErrUnavailable)
	}
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(imageContextReader{ctx: ctx, reader: file}, storedImageReadLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: indexed image cannot be read", ErrUnavailable)
	}
	if count != image.Size || count > storedImageReadLimit || hex.EncodeToString(digest.Sum(nil)) != image.Tag {
		return nil, fmt.Errorf("%w: indexed image content changed", ErrUnavailable)
	}
	after, err := file.Stat()
	if err != nil || !image.matchesFile(after) || !sameImageFile(opened, after) {
		return nil, fmt.Errorf("%w: indexed image changed while reading", ErrUnavailable)
	}
	// Reopen from the approved root descriptor to detect a registered root or
	// image directory replacement after the initial handles were acquired.
	currentRoot, err := s.openLibraryRoot(image.root)
	if err != nil {
		return nil, err
	}
	defer currentRoot.Close()
	if !sameImageDirectory(root, currentRoot) {
		return nil, fmt.Errorf("%w: registered image root changed while reading", ErrUnavailable)
	}
	currentParent, err := openRegisteredRoot(currentRoot, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: image directory changed while reading", ErrUnavailable)
	}
	defer currentParent.Close()
	if !sameImageDirectory(parent, currentParent) {
		return nil, fmt.Errorf("%w: image directory changed while reading", ErrUnavailable)
	}
	current, err := currentParent.Lstat(name)
	if err != nil || !image.matchesFile(current) || !sameImageFile(after, current) {
		return nil, fmt.Errorf("%w: image pathname changed while reading", ErrUnavailable)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: indexed image cannot be rewound", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return file, nil
}

func (image storedImage) matchesFile(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Size() > 0 &&
		info.Size() <= storedImageReadLimit && info.Size() == image.Size &&
		image.identity != "" && fileIdentity(info) == image.identity &&
		catalogModifiedTime(info).Equal(image.ModifiedAt)
}

func sameImageFile(first, second os.FileInfo) bool {
	return first != nil && second != nil && os.SameFile(first, second) &&
		first.Size() == second.Size() && first.ModTime().Equal(second.ModTime())
}

func sameImageDirectory(first, second *os.Root) bool {
	before, err := first.Stat(".")
	if err != nil || !before.IsDir() {
		return false
	}
	after, err := second.Stat(".")
	return err == nil && after.IsDir() && os.SameFile(before, after)
}

type imageContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader imageContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}
