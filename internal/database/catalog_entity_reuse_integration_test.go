package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

const entityReuseMetadata = `{"Genres":[" Shared Genre ","shared genre"],"Tags":["Shared Tag"],
	"Studios":["Shared Studio"],"People":[
		{"Name":"Shared Person","Role":"Voice","Type":"Actor","SortOrder":9},
		{"Name":" shared person ","Role":"Direction","Type":"Director","SortOrder":0}],
	"Artists":[" First Artist ","first artist",null," Second Artist "],
	"AlbumArtists":["SECOND ARTIST","FIRST ARTIST"," Album Only "]}`

func seedEntityReuseItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('entity-reuse-library','Entity reuse','mixed');
		INSERT INTO items(id,library_id,name,sort_name,type)
		SELECT id,'entity-reuse-library',id,id,'Audio'
		FROM (VALUES('entity-reuse-source'),('entity-reuse-peer')) fixture(id)`); err != nil {
		t.Fatal(err)
	}
}

func entityReuseState(t *testing.T, ctx context.Context, query themeOwnerQuery) string {
	t.Helper()
	var state string
	if err := query.QueryRow(ctx, `SELECT jsonb_build_object(
		'entities',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e),
		'associations',(SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id,entity_id,credit_group,position)
			FROM item_entities a))::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func assertEntityReuseCredits(t *testing.T, ctx context.Context, query themeOwnerQuery, item string) {
	t.Helper()
	const expected = `[
		["Genre","Shared Genre","Shared Genre",0,1,"","",null],
		["MusicArtist","First Artist","First Artist",1,1,"Artist","",null],
		["MusicArtist","Second Artist","Second Artist",1,4,"Artist","",null],
		["MusicArtist","Second Artist","SECOND ARTIST",2,1,"AlbumArtist","",null],
		["MusicArtist","First Artist","FIRST ARTIST",2,2,"AlbumArtist","",null],
		["MusicArtist","Album Only","Album Only",2,3,"AlbumArtist","",null],
		["Person","Shared Person","Shared Person",0,1,"Actor","Voice",9],
		["Person","Shared Person","shared person",0,2,"Director","Direction",0],
		["Studio","Shared Studio","Shared Studio",0,1,"","",null],
		["Tag","Shared Tag","Shared Tag",0,1,"","",null]]`
	var matches bool
	if err := query.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(e.kind,e.name,a.display_name,
		a.credit_group,a.position,a.credit_type,a.role,a.sort_order)
		ORDER BY e.kind,a.credit_group,a.position)=$2::jsonb
		FROM item_entities a JOIN catalog_entities e ON e.id=a.entity_id WHERE a.item_id=$1`, item, expected).Scan(&matches); err != nil || !matches {
		t.Fatalf("entity synchronization changed spelling, duplicate credits, groups or ordering: %v", err)
	}
}

func TestCatalogEntityReuseAvoidsWritesAndPreservesAssociations(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedEntityReuseItems(t, ctx, pool)
	if _, err := pool.Exec(ctx, `CREATE TABLE entity_write_audit(operation text NOT NULL);
		CREATE FUNCTION audit_entity_write() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN INSERT INTO entity_write_audit VALUES(TG_OP); RETURN NEW; END; $$;
		CREATE TRIGGER audit_entity_write BEFORE INSERT OR UPDATE ON catalog_entities
		FOR EACH ROW EXECUTE FUNCTION audit_entity_write()`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source',$1::jsonb)`, entityReuseMetadata); err != nil {
		t.Fatal(err)
	}
	assertEntityReuseCredits(t, ctx, pool, "entity-reuse-source")
	var initialInserts, initialUpdates int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE operation='INSERT'),
		count(*) FILTER(WHERE operation='UPDATE') FROM entity_write_audit`).Scan(&initialInserts, &initialUpdates); err != nil || initialInserts != 7 || initialUpdates != 0 {
		t.Fatalf("all-new synchronization attempted unexpected writes: inserts=%d updates=%d error=%v", initialInserts, initialUpdates, err)
	}
	var firstSequence int64
	if err := pool.QueryRow(ctx, `SELECT last_value FROM catalog_entities_id_seq`).Scan(&firstSequence); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE entity_write_audit`); err != nil {
		t.Fatal(err)
	}
	// New imports, metadata edits and forced repairs all reach the same owned
	// synchronization function. Every invocation still rebuilds associations.
	for _, item := range []string{"entity-reuse-peer", "entity-reuse-source", "entity-reuse-peer"} {
		if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities($1,$2::jsonb)`, item, entityReuseMetadata); err != nil {
			t.Fatal(err)
		}
		assertEntityReuseCredits(t, ctx, pool, item)
	}
	var writes int
	var sequence int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM entity_write_audit),
		(SELECT last_value FROM catalog_entities_id_seq)`).Scan(&writes, &sequence); err != nil || writes != 0 || sequence != firstSequence {
		t.Fatalf("shared entity sync attempted writes or allocated identities: writes=%d sequence=%d want=%d error=%v", writes, sequence, firstSequence, err)
	}
	// A mixed set inserts only the new dimension and preserves the first spelling.
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-peer',
		'{"Genres":["shared GENRE","New Genre"]}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	var inserts, updates int
	var canonical string
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE operation='INSERT'),
		count(*) FILTER(WHERE operation='UPDATE'),
		(SELECT name FROM catalog_entities WHERE kind='Genre' AND normalized_name='shared genre')
		FROM entity_write_audit`).Scan(&inserts, &updates, &canonical); err != nil || inserts != 1 || updates != 0 || canonical != "Shared Genre" {
		t.Fatalf("mixed synchronization rewrote a shared entity: inserts=%d updates=%d canonical=%q error=%v", inserts, updates, canonical, err)
	}
	before := entityReuseState(t, ctx, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source',
		'{"Genres":["Shared Genre","Rollback Genre"]}');
		SELECT sync_catalog_item_entities('entity-reuse-peer','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if entityReuseState(t, ctx, pool) != before {
		t.Fatal("rolled-back synchronization changed entities or associations")
	}
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-peer','{}')`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM item_entities WHERE item_id='entity-reuse-peer'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("empty metadata retained associations: %d %v", remaining, err)
	}
	assertEntityReuseCredits(t, ctx, pool, "entity-reuse-source")
}

func TestCatalogEntityReuseProtectsMatchedRowsUntilOwnerFinishes(t *testing.T) {
	for _, change := range []string{"UPDATE catalog_entities SET name='SHARED'", "DELETE FROM catalog_entities"} {
		t.Run(change, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			if err := database.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			seedEntityReuseItems(t, ctx, pool)
			if _, err := pool.Exec(ctx, `INSERT INTO catalog_entities(kind,name) VALUES('Genre','Shared')`); err != nil {
				t.Fatal(err)
			}
			owner, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Rollback(ctx)
			if _, err := owner.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source','{"Genres":["shared"]}')`); err != nil {
				t.Fatal(err)
			}
			writer, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback(ctx)
			work, cancel := context.WithCancel(ctx)
			result := make(chan error, 1)
			pending := true
			go func() {
				_, err := writer.Exec(work, change)
				result <- err
			}()
			defer func() {
				cancel()
				if pending {
					<-result
				}
			}()
			themeOwnersWaitBlocked(t, ctx, pool, writer.Conn().PgConn().PID(), owner.Conn().PgConn().PID())
			if err := owner.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-result
			pending = false
			if err != nil {
				t.Fatalf("writer could not continue after the synchronization owner exited: %v", err)
			}
		})
	}
}

func TestCatalogEntityReuseConcurrentChangesRespectIsolation(t *testing.T) {
	for _, isolation := range []pgx.TxIsoLevel{pgx.ReadCommitted, pgx.RepeatableRead, pgx.Serializable} {
		for _, change := range []string{"insert", "rename", "delete"} {
			t.Run(string(isolation)+"/"+change, func(t *testing.T) {
				ctx, pool := migrationTestPool(t)
				if err := database.Migrate(ctx, pool); err != nil {
					t.Fatal(err)
				}
				seedEntityReuseItems(t, ctx, pool)
				if change != "insert" {
					if _, err := pool.Exec(ctx, `INSERT INTO catalog_entities(kind,name) VALUES('Genre','Shared')`); err != nil {
						t.Fatal(err)
					}
				}
				owner, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Rollback(ctx)
				var count int
				if err := owner.QueryRow(ctx, `SELECT count(*) FROM catalog_entities`).Scan(&count); err != nil {
					t.Fatal(err)
				}
				writer, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Rollback(ctx)
				statements := map[string]string{
					"insert": `INSERT INTO catalog_entities(kind,name) VALUES('Genre','Shared')`,
					"rename": `UPDATE catalog_entities SET name='Renamed' WHERE kind='Genre'`,
					"delete": `DELETE FROM catalog_entities WHERE kind='Genre'`,
				}
				if _, err := writer.Exec(ctx, statements[change]); err != nil {
					t.Fatal(err)
				}
				work, cancel := context.WithCancel(ctx)
				result := make(chan error, 1)
				pending := true
				go func() {
					_, err := owner.Exec(work, `SELECT sync_catalog_item_entities('entity-reuse-source','{"Genres":["shared"]}')`)
					result <- err
				}()
				defer func() {
					cancel()
					if pending {
						<-result
					}
				}()
				themeOwnersWaitBlocked(t, ctx, pool, owner.Conn().PgConn().PID(), writer.Conn().PgConn().PID())
				if err := writer.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				err = <-result
				pending = false
				if isolation != pgx.ReadCommitted {
					var postgres *pgconn.PgError
					if !errors.As(err, &postgres) || postgres.Code != "40001" {
						t.Fatalf("concurrent %s at %s returned %v, want serialization failure", change, isolation, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("read committed synchronization after concurrent %s: %v", change, err)
				}
				if err := owner.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				var exact bool
				if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(e.normalized_name='shared')
					FROM item_entities a JOIN catalog_entities e ON e.id=a.entity_id
					WHERE a.item_id='entity-reuse-source'`).Scan(&exact); err != nil || !exact {
					t.Fatalf("concurrent %s lost the requested association or reused a renamed identity: %v", change, err)
				}
			})
		}
	}
}

func TestCatalogEntityReuseRejectsDigestCollision(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	seedEntityReuseItems(t, ctx, pool)
	if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source','{"Tags":["Original"]}');
		DO $$ DECLARE hash_check text; BEGIN
		SELECT conname INTO STRICT hash_check FROM pg_constraint
		WHERE conrelid='catalog_entities'::regclass AND contype='c'
		AND pg_get_constraintdef(oid) LIKE '%normalized_hash = %';
		EXECUTE format('ALTER TABLE catalog_entities DROP CONSTRAINT %I',hash_check);
		END; $$;
		ALTER TABLE catalog_entities DISABLE TRIGGER catalog_entities_name_hash;
		INSERT INTO catalog_entities(kind,name,normalized_hash)
		VALUES('Genre','Different identity',sha256(convert_to('collision','UTF8')));
		ALTER TABLE catalog_entities ENABLE TRIGGER catalog_entities_name_hash`); err != nil {
		t.Fatal("construct a synthetic digest collision in the owned test schema", err)
	}
	before := entityReuseState(t, ctx, pool)
	_, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source','{"Genres":["Collision"]}')`)
	var postgres *pgconn.PgError
	if !errors.As(err, &postgres) || postgres.Code != "23502" {
		t.Fatalf("digest collision returned %v, want the existing null-name rejection", err)
	}
	if entityReuseState(t, ctx, pool) != before {
		t.Fatal("digest collision changed the original associations or entity identity")
	}
}

func TestCatalogEntityReuseUpgradePreservesSchema67(t *testing.T) {
	for _, runner := range []string{"normal", "recovery"} {
		t.Run(runner, func(t *testing.T) {
			ctx, pool := migrationTestPool(t)
			themeOwnersMigrateTo(t, ctx, pool, 67)
			seedEntityReuseItems(t, ctx, pool)
			if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source',$1::jsonb)`, entityReuseMetadata); err != nil {
				t.Fatal(err)
			}
			before := entityReuseState(t, ctx, pool)
			var originalOID uint32
			if err := pool.QueryRow(ctx, `SELECT 'sync_catalog_item_entities(text,jsonb)'::regprocedure::oid`).Scan(&originalOID); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if runner == "normal" {
					if err := database.Migrate(ctx, pool); err != nil {
						t.Fatal(err)
					}
				} else {
					themeOwnersMigrateTo(t, ctx, pool, 68)
				}
				if entityReuseState(t, ctx, pool) != before {
					t.Fatal("entity reuse migration changed historical entities or associations")
				}
				var sameOID bool
				if err := pool.QueryRow(ctx, `SELECT 'sync_catalog_item_entities(text,jsonb)'::regprocedure::oid=$1`, originalOID).Scan(&sameOID); err != nil || !sameOID {
					t.Fatalf("entity reuse migration replaced the function identity: %v", err)
				}
			}
			if _, err := pool.Exec(ctx, `SELECT sync_catalog_item_entities('entity-reuse-source',$1::jsonb)`, entityReuseMetadata); err != nil {
				t.Fatal(err)
			}
			if entityReuseState(t, ctx, pool) != before {
				t.Fatal("upgraded entity synchronization changed IDs, canonical spelling or creation times")
			}
			assertEntityReuseCredits(t, ctx, pool, "entity-reuse-source")
		})
	}
}

func TestCatalogEntityReuseRecoveryMigrationRollsBackWithCaller(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 67)
	var original string
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('sync_catalog_item_entities(text,jsonb)'::regprocedure)`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := database.RecoveryMigrateTo(ctx, tx, 68); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var restored string
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('sync_catalog_item_entities(text,jsonb)'::regprocedure)`).Scan(&restored); err != nil || restored != original {
		t.Fatalf("rolled-back migration retained its function body: %v", err)
	}
	if version, err := database.SchemaVersion(ctx, pool); err != nil || version != 67 {
		t.Fatalf("rolled-back entity migration version=%d, want 67: %v", version, err)
	}
}
