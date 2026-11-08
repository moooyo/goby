//go:build linux

package backuppg

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/backupformat"
)

func TestPostgreSQLManagedArtworkProjectionRestoresWithoutPayloadsOrOverrides(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedSelectedPhase2ArchiveWitness(t, ctx, source)
	var stateID int64
	if err := source.QueryRow(ctx, `INSERT INTO artwork_state(user_id,revision,managed_types)
		VALUES('backup-admin',7,ARRAY['Primary']) RETURNING id`).Scan(&stateID); err != nil {
		t.Fatal("seed a managed avatar and its manual override")
	}
	content, digest := phase3ArtworkPayload(t, 37)
	if _, err := source.Exec(ctx, `INSERT INTO artwork_images(state_id,image_type,image_index,content,mime_type,width,height,source_hash)
		VALUES($1,'Primary',0,$2,'image/png',2,2,$3)`, stateID, content, digest); err != nil {
		t.Fatal("seed the database-owned image payload")
	}
	if _, err := source.Exec(ctx, `INSERT INTO item_provider_sources(item_id,provider,provider_id,source_url,language,fields)
		VALUES('phase2-review','tmdb','12345','https://example.invalid/retained-provider-metadata','en','{"Overview":"Retain ordinary metadata"}'::jsonb)`); err != nil {
		t.Fatal("seed ordinary provider metadata independent of its images")
	}
	if _, err := source.Exec(ctx, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
		VALUES('phase2-review','Logo',0,'tmdb','12345','retained-logo.png',$1,'image/png',2,2,$2)`, content, digest); err != nil {
		t.Fatal("seed a downloaded library logo and independent provider metadata")
	}
	embeddedContent, embeddedDigest := phase3ArtworkPayload(t, 83)
	if _, err := source.Exec(ctx, `UPDATE item_embedded_artwork SET content=$1,source_hash=$2,width=2,height=2
		WHERE item_id='phase2-cover' AND status='ready'`, embeddedContent, embeddedDigest); err != nil {
		t.Fatal("seed an embedded-cover payload distinct from retained OCR review evidence")
	}
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	projected := options
	projected.OmitManagedArtwork = true
	archive, facts := sourceArchive(t, ctx, source, projected)
	if !factsOmitManagedArtwork(facts) {
		t.Fatal("the export did not declare every omitted library-artwork table empty")
	}
	for index, expected := range before.Tables {
		actual := facts.Tables[index]
		if managedArtworkTable(expected.Name) {
			if actual != emptyManagedArtworkFact(expected.Name) {
				t.Fatal("omitted artwork retained its source row fingerprints")
			}
		} else if actual != expected {
			t.Fatalf("image omission changed unrelated data in %s", expected.Name)
		}
	}
	if err := decodeCommand(ctx, projected, archive, func(reader io.Reader) error {
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		for _, table := range managedArtworkTables {
			if strings.Contains(string(data), "-- Data for Name: "+table+";") {
				return ErrArchive
			}
		}
		if strings.Contains(string(data), hex.EncodeToString(content)) || strings.Contains(string(data), hex.EncodeToString(embeddedContent)) {
			return ErrArchive
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the projected custom archive")
	}
	if err := ValidateDump(ctx, archive, facts, projected); err != nil {
		t.Fatalf("projected archive failed publication preflight: %v", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the projected custom archive for restoration")
	}
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("projected archive failed complete restoration checks: %v", err)
	}
	var valid bool
	if err := target.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM artwork_state)
		AND NOT EXISTS(SELECT 1 FROM artwork_images)
		AND NOT EXISTS(SELECT 1 FROM item_provider_images)
		AND NOT EXISTS(SELECT 1 FROM item_embedded_artwork)
		AND EXISTS(SELECT 1 FROM users WHERE id='backup-admin' AND name='Before snapshot')
		AND EXISTS(SELECT 1 FROM item_provider_sources WHERE item_id='phase2-review' AND fields='{"Overview":"Retain ordinary metadata"}'::jsonb)
		AND EXISTS(SELECT 1 FROM item_metadata_state WHERE item_id='phase2-cover')
		AND EXISTS(SELECT 1 FROM media_operation_cues WHERE operation_id=repeat('a',32) AND ordinal=0
			AND text='Reviewed OCR line' AND octet_length(image_png)>0
			AND image_sha256=encode(sha256(image_png),'hex'))`).Scan(&valid); err != nil || !valid {
		t.Fatalf("restoration retained missing-payload overrides or lost unrelated metadata: %v", err)
	}
	var nextID int64
	if err := target.QueryRow(ctx, `INSERT INTO artwork_state(user_id) VALUES('backup-admin') RETURNING id`).Scan(&nextID); err != nil || nextID <= stateID {
		t.Fatalf("the projected archive lost the artwork sequence state: %v", err)
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLManagedArtworkProjectionRejectsNonemptySourceFacts(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	projected := options
	projected.OmitManagedArtwork = true
	archive, facts := sourceArchive(t, ctx, source, projected)
	for index := range facts.Tables {
		if facts.Tables[index].Name == "artwork_state" {
			facts.Tables[index] = backupformat.TableFact{Name: "artwork_state", Rows: 1, SHA256: strings.Repeat("a", 64)}
		}
	}
	if err := ValidateDump(ctx, archive, facts, options); !errors.Is(err, ErrArchive) {
		t.Fatalf("publication preflight did not reject missing data with nonempty facts: %v", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the invalid projected archive")
	}
	if _, err := Restore(ctx, source, target, archive, facts, options); !errors.Is(err, ErrArchive) {
		t.Fatalf("restoration did not reject missing data with nonempty facts: %v", err)
	}
	var count int
	if err := target.QueryRow(context.Background(), `SELECT count(*) FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, options.Schema).Scan(&count); err != nil || count != 0 {
		t.Fatal("the rejected archive retained target schema objects")
	}
}
