//go:build linux

package backuppg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
)

// A legal ancillary text chunk reaches the encoded upload boundary while the
// actual image remains four pixels. The PNG decoder still validates its CRC.
func phase3LargeManagedPNG(t *testing.T, size int) []byte {
	t.Helper()
	var small bytes.Buffer
	if err := png.Encode(&small, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("encode the bounded large-artwork pixel witness: %v", err)
	}
	base := small.Bytes()
	const headerEnd = 8 + 4 + 4 + 13 + 4
	const keyword = "Goby backup fixture\x00"
	textSize := size - len(base) - 12
	if len(base) <= headerEnd || string(base[12:16]) != "IHDR" ||
		binary.BigEndian.Uint32(base[8:12]) != 13 || textSize < len(keyword) {
		t.Fatal("the original PNG cannot hold the bounded ancillary text witness")
	}
	content := make([]byte, size)
	copy(content, base[:headerEnd])
	binary.BigEndian.PutUint32(content[headerEnd:headerEnd+4], uint32(textSize))
	copy(content[headerEnd+4:headerEnd+8], "tEXt")
	textStart := headerEnd + 8
	copy(content[textStart:], keyword)
	for index := textStart + len(keyword); index < textStart+textSize; index++ {
		content[index] = 'x'
	}
	crcAt := textStart + textSize
	binary.BigEndian.PutUint32(content[crcAt:crcAt+4], crc32.ChecksumIEEE(content[headerEnd+4:crcAt]))
	copy(content[crcAt+4:], base[headerEnd:])
	return content
}

func TestPostgreSQLPhase3MaximumManagedArtworkDumpValidatesAndRestores(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	// The complete expanded archive must fit independently of its COPY row.
	// A compressible PNG or pg_dump archive does not waive this stream budget.
	options.MaxDumpBytes = 128 << 20
	content := phase3LargeManagedPNG(t, artwork.ManagedUploadBytes)
	prepared, err := artwork.PrepareManagedImage(ctx, "Primary", 0, content)
	if err != nil || prepared.Size != int64(artwork.ManagedUploadBytes) || prepared.Size <= 16<<20 ||
		prepared.Width != 2 || prepared.Height != 2 || prepared.MIMEType != "image/png" ||
		!bytes.Equal(prepared.Content, content) {
		t.Fatalf("the large fixture is not a valid maximum-size managed PNG: %v", err)
	}
	content = nil
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal("begin the maximum-size managed artwork write")
	}
	defer rollback(tx)
	if err := artwork.LockManagedArtwork(ctx, tx); err != nil {
		t.Fatalf("lock the managed artwork quota: %v", err)
	}
	set, err := artwork.ReplaceManagedType(ctx, tx, artwork.Target{Kind: "user", ID: "backup-admin"},
		"Primary", []artwork.StoredImage{prepared}, false)
	if err != nil || len(set.Images) != 1 || set.Images[0].Size != prepared.Size {
		t.Fatalf("persist the accepted maximum-size managed artwork through native storage: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit the maximum-size managed artwork: %v", err)
	}
	var serializedBytes int64
	if err := source.QueryRow(ctx, `SELECT octet_length(to_jsonb(i)::text)
		FROM artwork_images i WHERE state_id=$1 AND image_type='Primary' AND image_index=0`, set.ID).Scan(&serializedBytes); err != nil ||
		serializedBytes <= 32<<20 || serializedBytes > maxSerializedRowBytes {
		t.Fatalf("the real PostgreSQL artwork row did not cross the former serialized limit: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	assertCurrentRecoveryFacts(t, facts)
	if err := ValidateDump(ctx, archive, facts, options); err != nil {
		t.Fatalf("validate the real pg_dump archive containing an accepted maximum-size image: %v", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the real artwork archive for the stricter row limit")
	}
	catalog, _, err := archiveCatalog(facts, options)
	if err != nil {
		t.Fatalf("load the authenticated large-artwork archive catalog: %v", err)
	}
	expectedRows := make(map[string]int64, len(facts.Tables))
	for _, table := range facts.Tables {
		expectedRows[table.Name] = table.Rows
	}
	err = decodeCommand(ctx, options, archive, func(decoded io.Reader) error {
		return Decode(ctx, decoded, catalog, &dumpValidationSink{expected: expectedRows}, DecodeOptions{
			MaxBytes: options.MaxDumpBytes, MaxRowBytes: 32 << 20,
		})
	})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("a lower custom row budget accepted the real large-artwork COPY row: %v", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the real artwork archive for restoration")
	}
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != currentRecoveryVersion(t) ||
		!equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the real maximum-size managed artwork archive: %v", err)
	}
	var restored []byte
	var sourceHash, mimeType string
	var width, height int
	if err := target.QueryRow(ctx, `SELECT i.content,i.source_hash,i.mime_type,i.width,i.height
		FROM artwork_images i JOIN artwork_state s ON s.id=i.state_id
		WHERE s.user_id='backup-admin' AND i.image_type='Primary' AND i.image_index=0`).Scan(
		&restored, &sourceHash, &mimeType, &width, &height); err != nil ||
		!bytes.Equal(restored, prepared.Content) || sourceHash != prepared.Tag || mimeType != prepared.MIMEType ||
		width != prepared.Width || height != prepared.Height {
		t.Fatalf("the native restore changed validated maximum-size image bytes or metadata: %v", err)
	}
	restored, prepared.Content = nil, nil
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, _ := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(after, facts) {
		t.Fatal("the restored maximum-size artwork changed the complete archive fingerprints")
	}
}
