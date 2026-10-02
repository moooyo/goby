package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
)

func TestPlaybackControlPoolPreservesBoundIdentityAndApplicationBudget(t *testing.T) {
	url := os.Getenv("GOBY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal("open the production data pool")
	}
	defer data.Close()
	lease, err := database.AcquireLease(ctx, data)
	if err != nil {
		t.Fatal("acquire the production deployment lease")
	}
	defer func() {
		if err := lease.Close(); err != nil {
			t.Error("release the owned deployment lease")
		}
	}()
	control, err := database.OpenPlaybackControlFor(ctx, data)
	if err != nil {
		t.Fatal("open the bound playback-control pool")
	}
	defer control.Close()
	dataConfig, controlConfig := data.Config(), control.Config()
	if dataConfig.MaxConns != database.DataMaxConns || controlConfig.MaxConns != database.PlaybackControlMaxConns ||
		dataConfig.MaxConns+controlConfig.MaxConns != database.ApplicationMaxConns ||
		dataConfig.ConnConfig.Host != controlConfig.ConnConfig.Host || dataConfig.ConnConfig.Port != controlConfig.ConnConfig.Port ||
		dataConfig.ConnConfig.Database != controlConfig.ConnConfig.Database || dataConfig.ConnConfig.User != controlConfig.ConnConfig.User ||
		dataConfig.ConnConfig.RuntimeParams["search_path"] != controlConfig.ConnConfig.RuntimeParams["search_path"] ||
		controlConfig.ConnConfig.RuntimeParams["jit"] != "off" || controlConfig.ConnConfig.RuntimeParams["application_name"] != "goby-playback-control" {
		t.Fatal("reserved capacity changed the bound storage identity or application budget")
	}
	var held []*pgxpool.Conn
	defer func() {
		for _, connection := range held {
			connection.Release()
		}
	}()
	pids := make([]int64, 0, database.ApplicationMaxConns)
	for _, pool := range []*pgxpool.Pool{data, control} {
		for range pool.Config().MaxConns {
			connection, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal("reserve the bounded application capacity")
			}
			held = append(held, connection)
			pids = append(pids, int64(connection.Conn().PgConn().PID()))
		}
	}
	var poolBackends, deploymentBackends int
	err = held[0].QueryRow(ctx, `SELECT
		(SELECT count(*) FROM pg_stat_activity WHERE pid=ANY($1::bigint[]) AND datname=current_database()),
		(SELECT count(DISTINCT pid) FROM pg_locks WHERE locktype='advisory' AND granted
		 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
		 AND classid=$2::oid AND objid=$3::oid AND objsubid=1 AND NOT pid=ANY($1::bigint[]))`,
		pids, uint32(uint64(4919415424202458201)>>32), uint32(uint64(4919415424202458201)&0xffffffff)).Scan(&poolBackends, &deploymentBackends)
	if err != nil || poolBackends != int(database.ApplicationMaxConns) || deploymentBackends != 1 || !lease.Protects(data) {
		t.Fatal("physical capacity must be sixteen application backends plus one hijacked generation lease")
	}
	t.Logf("application_backends=%d deployment_lease_backends=%d data_max=%d control_max=%d", poolBackends, deploymentBackends, dataConfig.MaxConns, controlConfig.MaxConns)
}
