package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	ManagedUploadBytes        = 20 << 20
	ManagedTypeBytes          = 128 << 20
	ManagedOwnerBytes         = 256 << 20
	ManagedGlobalBytes        = 512 << 20
	ManagedGlobalImages       = 10000
	managedArtworkLock  int64 = 5138411729813525553
)

var (
	ErrManagedStorage  = errors.New("managed artwork storage unavailable")
	ErrManagedConflict = errors.New("managed artwork revision conflict")
	ErrManagedTarget   = errors.New("invalid managed artwork target")
	ErrManagedLimit    = errors.New("managed artwork capacity exceeded")
)

var ManagedTypes = []string{"Primary", "Backdrop", "Thumb", "Banner", "Logo", "Art", "Disc", "Box", "BoxRear", "Menu", "Screenshot"}

// Target is a closed SQL selector. These storage helpers do not authorize a
// caller: the owning identity/catalog transaction must do so before using them.
type Target struct {
	Kind string
	ID   string
}

func (target Target) column() (string, any, error) {
	if target.ID == "" || len(target.ID) > 256 || strings.TrimSpace(target.ID) != target.ID || !utf8.ValidString(target.ID) || strings.ContainsRune(target.ID, 0) {
		return "", nil, ErrManagedTarget
	}
	switch target.Kind {
	case "item":
		return "item_id", target.ID, nil
	case "user":
		return "user_id", target.ID, nil
	case "entity":
		id, err := strconv.ParseInt(target.ID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != target.ID {
			return "", nil, ErrManagedTarget
		}
		return "entity_id", id, nil
	default:
		return "", nil, ErrManagedTarget
	}
}

func NormalizeManagedType(value string) (string, error) {
	for _, candidate := range ManagedTypes {
		if strings.EqualFold(value, candidate) {
			return candidate, nil
		}
	}
	return "", ErrInvalidOptions
}

func MultipleManagedImages(value string) bool { return value == "Backdrop" || value == "Screenshot" }

type StoredImage struct {
	ImageType     string
	ImageIndex    int
	Tag, MIMEType string
	Width, Height int
	Size          int64
	ModifiedAt    time.Time
	Content       []byte
}

type ManagedSet struct {
	ID       int64
	Revision int64
	Types    []string
	Images   []StoredImage
}

func PrepareManagedImage(ctx context.Context, imageType string, index int, content []byte) (StoredImage, error) {
	canonical, err := NormalizeManagedType(imageType)
	if err != nil || index < 0 || index >= 32 || !MultipleManagedImages(canonical) && index != 0 || len(content) == 0 || len(content) > ManagedUploadBytes {
		return StoredImage{}, ErrInvalidImage
	}
	data := append([]byte(nil), content...)
	info, err := InspectContext(ctx, bytes.NewReader(data))
	if err != nil {
		return StoredImage{}, err
	}
	return StoredImage{ImageType: canonical, ImageIndex: index, Tag: info.Tag, MIMEType: info.MIMEType,
		Width: info.Width, Height: info.Height, Size: int64(len(data)), Content: data}, nil
}

// RevisionToken is opaque decimal text, including all effective source images.
// Automatic-source changes invalidate an editor even before its first override.
func RevisionToken(target Target, revision int64, images []StoredImage) string {
	values := append([]StoredImage(nil), images...)
	for index := range values {
		values[index].Content = nil
		values[index].ModifiedAt = time.Time{}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ImageType == values[j].ImageType {
			return values[i].ImageIndex < values[j].ImageIndex
		}
		return values[i].ImageType < values[j].ImageType
	})
	encoded, _ := json.Marshal(struct {
		Target   Target
		Revision int64
		Images   []StoredImage
	}{target, revision, values})
	digest := sha256.Sum256(encoded)
	return new(big.Int).SetBytes(digest[:]).String()
}

func (set ManagedSet) Manages(imageType string) bool {
	for _, candidate := range set.Types {
		if candidate == imageType {
			return true
		}
	}
	return false
}

func LockManagedArtwork(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", managedArtworkLock)
	return err
}

func ReadManagedSet(ctx context.Context, tx pgx.Tx, target Target, lock, content bool) (ManagedSet, error) {
	column, id, err := target.column()
	if err != nil {
		return ManagedSet{}, err
	}
	query := "SELECT id, revision, managed_types FROM artwork_state WHERE " + column + "=$1"
	if lock {
		query += " FOR UPDATE"
	}
	set := ManagedSet{Types: []string{}, Images: []StoredImage{}}
	err = tx.QueryRow(ctx, query, id).Scan(&set.ID, &set.Revision, &set.Types)
	if errors.Is(err, pgx.ErrNoRows) {
		return set, nil
	}
	if err != nil {
		return ManagedSet{}, fmt.Errorf("%w: read artwork set", ErrManagedStorage)
	}
	seen := make(map[string]bool)
	for _, name := range set.Types {
		canonical, err := NormalizeManagedType(name)
		if err != nil || canonical != name || seen[name] || target.Kind == "user" && name != "Primary" {
			return ManagedSet{}, ErrManagedStorage
		}
		seen[name] = true
	}
	projection := "NULL::bytea"
	if content {
		projection = "content"
	}
	rows, err := tx.Query(ctx, `SELECT image_type,image_index,source_hash,mime_type,width,height,octet_length(content),modified_at,`+projection+`
		FROM artwork_images WHERE state_id=$1 ORDER BY image_type,image_index`, set.ID)
	if err != nil {
		return ManagedSet{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var image StoredImage
		if err := rows.Scan(&image.ImageType, &image.ImageIndex, &image.Tag, &image.MIMEType, &image.Width, &image.Height, &image.Size, &image.ModifiedAt, &image.Content); err != nil {
			return ManagedSet{}, err
		}
		if !seen[image.ImageType] || !validStoredManagedImage(image, content) {
			return ManagedSet{}, ErrManagedStorage
		}
		set.Images = append(set.Images, image)
	}
	if err := rows.Err(); err != nil {
		return ManagedSet{}, err
	}
	return set, nil
}

func ReadManagedImage(ctx context.Context, tx pgx.Tx, target Target, imageType string, index int) (StoredImage, bool, error) {
	set, err := ReadManagedSet(ctx, tx, target, false, false)
	if err != nil {
		return StoredImage{}, false, err
	}
	if !set.Manages(imageType) {
		return StoredImage{}, false, nil
	}
	for _, image := range set.Images {
		if image.ImageType != imageType || image.ImageIndex != index {
			continue
		}
		if err := tx.QueryRow(ctx, `SELECT content FROM artwork_images WHERE state_id=$1 AND image_type=$2 AND image_index=$3`, set.ID, imageType, index).Scan(&image.Content); err != nil {
			return StoredImage{}, true, err
		}
		if !validStoredManagedImage(image, true) {
			return StoredImage{}, true, ErrManagedStorage
		}
		return image, true, nil
	}
	return StoredImage{}, true, nil
}

func validStoredManagedImage(image StoredImage, content bool) bool {
	if image.ImageIndex < 0 || image.ImageIndex >= 32 || !MultipleManagedImages(image.ImageType) && image.ImageIndex != 0 ||
		image.Size <= 0 || image.Size > ManagedUploadBytes || image.Width <= 0 || image.Height <= 0 || image.Width > 16384 || image.Height > 16384 ||
		int64(image.Width)*int64(image.Height) > 26214400 || len(image.Tag) != 64 {
		return false
	}
	if image.MIMEType != "image/jpeg" && image.MIMEType != "image/png" && image.MIMEType != "image/gif" {
		return false
	}
	if _, err := hex.DecodeString(image.Tag); err != nil || strings.ToLower(image.Tag) != image.Tag {
		return false
	}
	if content {
		digest := sha256.Sum256(image.Content)
		return int64(len(image.Content)) == image.Size && hex.EncodeToString(digest[:]) == image.Tag
	}
	return true
}

// ReplaceManagedType takes the quota lock before this call. It atomically owns
// a complete type, including an empty deletion mask, and never writes media roots.
func ReplaceManagedType(ctx context.Context, tx pgx.Tx, target Target, imageType string, images []StoredImage, reset bool) (ManagedSet, error) {
	column, id, err := target.column()
	if err != nil {
		return ManagedSet{}, err
	}
	canonical, err := NormalizeManagedType(imageType)
	if err != nil || canonical != imageType || target.Kind == "user" && imageType != "Primary" ||
		len(images) > 32 || !MultipleManagedImages(imageType) && len(images) > 1 || reset && len(images) != 0 {
		return ManagedSet{}, ErrInvalidOptions
	}
	var incoming int64
	seen := make(map[int]bool)
	for _, image := range images {
		if image.ImageType != imageType || seen[image.ImageIndex] || !validStoredManagedImage(image, true) {
			return ManagedSet{}, ErrInvalidImage
		}
		seen[image.ImageIndex] = true
		incoming += image.Size
	}
	if incoming > ManagedTypeBytes {
		return ManagedSet{}, ErrManagedLimit
	}
	if _, err := tx.Exec(ctx, "INSERT INTO artwork_state("+column+") VALUES($1) ON CONFLICT("+column+") DO NOTHING", id); err != nil {
		return ManagedSet{}, err
	}
	set, err := ReadManagedSet(ctx, tx, target, true, false)
	if err != nil {
		return ManagedSet{}, err
	}
	if set.Revision == math.MaxInt64 {
		return ManagedSet{}, ErrManagedStorage
	}
	var total, count, owner, replaced int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(octet_length(content)),0),count(*),
		COALESCE(sum(octet_length(content)) FILTER (WHERE state_id=$1),0),
		COALESCE(sum(octet_length(content)) FILTER (WHERE state_id=$1 AND image_type=$2),0)
		FROM artwork_images`, set.ID, imageType).Scan(&total, &count, &owner, &replaced); err != nil {
		return ManagedSet{}, err
	}
	oldCount := 0
	for _, image := range set.Images {
		if image.ImageType == imageType {
			oldCount++
		}
	}
	if total-replaced+incoming > ManagedGlobalBytes || owner-replaced+incoming > ManagedOwnerBytes || count-int64(oldCount)+int64(len(images)) > ManagedGlobalImages {
		return ManagedSet{}, ErrManagedLimit
	}
	if _, err := tx.Exec(ctx, "DELETE FROM artwork_images WHERE state_id=$1 AND image_type=$2", set.ID, imageType); err != nil {
		return ManagedSet{}, err
	}
	for _, image := range images {
		if _, err := tx.Exec(ctx, `INSERT INTO artwork_images(state_id,image_type,image_index,content,mime_type,width,height,source_hash)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, set.ID, imageType, image.ImageIndex, image.Content, image.MIMEType, image.Width, image.Height, image.Tag); err != nil {
			return ManagedSet{}, err
		}
	}
	marked := make([]string, 0, len(set.Types)+1)
	for _, name := range set.Types {
		if name != imageType {
			marked = append(marked, name)
		}
	}
	if !reset {
		marked = append(marked, imageType)
	}
	sort.Strings(marked)
	if _, err := tx.Exec(ctx, `UPDATE artwork_state SET revision=revision+1,managed_types=$2,updated_at=clock_timestamp() WHERE id=$1`, set.ID, marked); err != nil {
		return ManagedSet{}, err
	}
	return ReadManagedSet(ctx, tx, target, false, false)
}
