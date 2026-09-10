package database

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProtectsTransaction additionally binds a borrowed transaction to the leased
// pool's declared connection and actual server peer. Equal database/role names
// on a different server are insufficient. It reads immutable connection metadata
// only, so it does not race the lease watcher's SQL connection use.
// As with the deployment lease itself, routing proxies that send one endpoint
// to different PostgreSQL instances are outside the single-host contract.
func (lease *Lease) ProtectsTransaction(pool *pgxpool.Pool, tx pgx.Tx) bool {
	if !lease.Protects(pool) || tx == nil || lease.connection == nil || tx.Conn() == nil {
		return false
	}
	expected := pool.Config().ConnConfig
	actual := tx.Conn().Config()
	if actual == nil || actual.ConnString() != expected.ConnString() || actual.Host != expected.Host || actual.Port != expected.Port || actual.Database != expected.Database || actual.User != expected.User {
		return false
	}
	leasedConnection := lease.connection.PgConn().Conn()
	transactionConnection := tx.Conn().PgConn().Conn()
	if leasedConnection == nil || transactionConnection == nil {
		return false
	}
	leasingPeer, transactionPeer := leasedConnection.RemoteAddr(), transactionConnection.RemoteAddr()
	if leasingPeer == nil || transactionPeer == nil || leasingPeer.Network() != transactionPeer.Network() || leasingPeer.String() != transactionPeer.String() {
		return false
	}
	return lease.Protects(pool)
}
