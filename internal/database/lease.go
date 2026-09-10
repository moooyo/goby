package database

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This namespace is separate from schema migrations and catalog ownership.
// The single bigint form uses objsubid=1 in pg_locks.
const deploymentLeaseID int64 = 4919415424202458201

var (
	ErrLeaseBusy        = errors.New("database deployment lease is already held")
	ErrLeaseUnavailable = errors.New("database deployment lease unavailable")
)

// Lease prevents another cooperating process from starting on the same
// database. Acquire it before Migrate and retain it until application shutdown.
// A local lifecycle lock additionally binds the database generation and vault.
// This is single-host startup fencing, not a distributed per-write lease or an
// HA guarantee. On Done, the owner must stop ingress and drain the application.
type Lease struct {
	connection *pgx.Conn
	pool       *pgxpool.Pool
	cancel     context.CancelFunc
	done       chan struct{}
	watchDone  chan struct{}
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
	signalOnce sync.Once
}

func AcquireLease(ctx context.Context, pool *pgxpool.Pool) (*Lease, error) {
	if pool == nil {
		return nil, ErrLeaseUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	acquired, err := pool.Acquire(bounded)
	if err != nil {
		return nil, ErrLeaseUnavailable
	}
	// A dedicated physical connection must never return a session-level lock
	// to the ordinary request pool, including every failed acquisition path.
	connection := acquired.Hijack()
	var locked bool
	err = connection.QueryRow(bounded, "SELECT pg_try_advisory_lock($1)", deploymentLeaseID).Scan(&locked)
	if err != nil || !locked {
		closeLeaseConnection(connection)
		if err == nil {
			return nil, ErrLeaseBusy
		}
		return nil, ErrLeaseUnavailable
	}
	if ctx.Err() != nil {
		closeLeaseConnection(connection)
		return nil, ctx.Err()
	}
	watch, stop := context.WithCancel(context.Background())
	lease := &Lease{connection: connection, pool: pool, cancel: stop, done: make(chan struct{}), watchDone: make(chan struct{}), closeDone: make(chan struct{})}
	go lease.watch(watch)
	return lease, nil
}

func (l *Lease) Done() <-chan struct{} { return l.done }

// Protects is an instantaneous startup-ownership check, not authorization for
// a later write. Long operations must additionally stop when Done is closed.
func (l *Lease) Protects(pool *pgxpool.Pool) bool {
	if l == nil || pool == nil || l.pool != pool {
		return false
	}
	select {
	case <-l.done:
		return false
	default:
		return true
	}
}

func (l *Lease) signal() { l.signalOnce.Do(func() { close(l.done) }) }

func (l *Lease) watch(ctx context.Context) {
	defer close(l.watchDone)
	defer l.signal()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		var held bool
		err := l.connection.QueryRow(probe, `SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_locks WHERE locktype='advisory'
			AND pid=pg_backend_pid() AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
			AND classid=$1::oid AND objid=$2::oid AND objsubid=1
			AND mode='ExclusiveLock' AND granted
		)`, uint32(uint64(deploymentLeaseID)>>32), uint32(uint64(deploymentLeaseID)&0xffffffff)).Scan(&held)
		cancel()
		if err != nil || !held {
			l.signal()
			// Close the physical session on ownership loss. Reacquiring a lock
			// here would hide a period when another process could have started.
			closeLeaseConnection(l.connection)
			return
		}
	}
}

func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		l.cancel()
		<-l.watchDone
		if !l.connection.IsClosed() {
			// Closing TCP only queues session termination; PostgreSQL may still
			// hold the advisory lock when Close returns. Await an explicit unlock
			// before reporting a clean handoff to a successor process.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			var unlocked bool
			err := l.connection.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", deploymentLeaseID).Scan(&unlocked)
			cancel()
			if err != nil || !unlocked {
				l.closeErr = ErrLeaseUnavailable
			}
		}
		if err := closeLeaseConnection(l.connection); err != nil {
			l.closeErr = ErrLeaseUnavailable
		}
		close(l.closeDone)
	})
	<-l.closeDone
	return l.closeErr
}

func closeLeaseConnection(connection *pgx.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return connection.Close(ctx)
}
