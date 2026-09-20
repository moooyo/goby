//go:build linux

package recovery

import (
	"testing"

	"github.com/moooyo/goby/internal/database"
)

const compatibilityLongTailRecoverySQL = `SELECT jsonb_build_object(
	'sorting',(SELECT to_jsonb(sort_remove_words) FROM managed_settings WHERE id=1),
	'library_options',(SELECT jsonb_agg(jsonb_build_object('Id',id,'Options',options) ORDER BY id) FROM libraries),
	'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i WHERE id LIKE 'long-tail-recovery-%'),
	'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m WHERE item_id LIKE 'long-tail-recovery-%'))::text`

func TestEngineCompatibilityLongTailArchivePreservesTelevisionSourceOverridesAndLocks(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	if _, err := f.source.Exec(f.ctx, `UPDATE managed_settings SET sort_remove_words=ARRAY['The','A'] WHERE id=1`); err != nil {
		t.Fatal("seed the portable sorting policy")
	}
	if _, err := f.source.Exec(f.ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableEmbeddedArtwork}','false') WHERE id=$1`, f.libraryID); err != nil {
		t.Fatal("seed a disabled embedded-artwork importer independently from sidecars")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder,local_metadata)
		VALUES('long-tail-recovery-series',$1,$1,'Retained TV series','retained tv series','Series',true,
		'{"SortName":"retained tv series","Status":"Ended","EndDate":"2025-01-02T03:04:05Z"}')`, f.libraryID); err != nil {
		t.Fatalf("seed the retained series source: %v", err)
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,parent_id,name,sort_name,type,index_number,parent_index_number,local_metadata)
		VALUES('long-tail-recovery-special',$1,'long-tail-recovery-series','Retained special','retained special','Episode',1,0,
		'{"AirsBeforeSeasonNumber":1,"AirsBeforeEpisodeNumber":2}')`, f.libraryID); err != nil {
		t.Fatalf("seed the separately placed special: %v", err)
	}
	if _, err := f.source.Exec(f.ctx, `UPDATE item_metadata_state SET overrides='{"Status":"Continuing"}',
		effective=jsonb_set(effective,'{Status}','"Continuing"'),revision=9007199254740993 WHERE item_id='long-tail-recovery-series';
		UPDATE item_metadata_state SET overrides='{"AirsBeforeSeasonNumber":null}',locked_values='{"AirsAfterSeasonNumber":2}',
		effective=effective || '{"AirsBeforeSeasonNumber":null,"AirsAfterSeasonNumber":2}',revision=9007199254740993
		WHERE item_id='long-tail-recovery-special'`); err != nil {
		t.Fatalf("seed separate current and dormant metadata layers: %v", err)
	}
	want := recoveryEngineJSONState(t, f.ctx, f.source, compatibilityLongTailRecoverySQL)
	_, object := f.create(t)
	reader, err := f.objects.Snapshot(f.ctx, object.ID)
	if err != nil {
		t.Fatal("open the encrypted compatibility archive")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatalf("authenticate the compatibility archive: %v", err)
	}
	defer archive.Close()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("lease the owned compatibility recovery target")
	}
	defer lease.Close()
	result, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil || result.RevokedCredentials == 0 {
		t.Fatalf("restore retained metadata with imported credentials revoked: %v", err)
	}
	if got := recoveryEngineJSONState(t, f.ctx, f.target, compatibilityLongTailRecoverySQL); got != want {
		t.Fatal("recovery changed retained TV source facts, explicit null, locked placement or exact revision")
	}
	if got := recoveryEngineJSONState(t, f.ctx, f.source, compatibilityLongTailRecoverySQL); got != want {
		t.Fatal("archive creation or recovery changed source metadata")
	}
}
