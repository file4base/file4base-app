package dbal_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/testdb"
)

// scratchDatabase creates a database with a table t (one row) for a test.
func scratchDatabase(t *testing.T) (*dbal.MultiDatabaseManager, string, dbal.DatabaseDriver) {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	require.NoError(t, err)
	ctx := context.Background()
	if err := mgr.Ping(ctx); err != nil {
		testdb.Unavailable(t, "no database server:", err)
	}
	name := fmt.Sprintf("f4b_pinned_%d", rand.New(rand.NewSource(time.Now().UnixNano())).Int63()%1_000_000_000)
	require.NoError(t, mgr.CreateDatabase(ctx, name))
	t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name); _ = mgr.Close() })
	live, err := mgr.DriverFor(ctx, name)
	require.NoError(t, err)
	_, err = live.DB().ExecContext(ctx, `CREATE TABLE t (n INT)`)
	require.NoError(t, err)
	_, err = live.DB().ExecContext(ctx, `INSERT INTO t (n) VALUES (1)`)
	require.NoError(t, err)
	return mgr, name, live
}

func count(t *testing.T, ctx context.Context, d dbal.DatabaseDriver, table string) int {
	t.Helper()
	var n int
	require.NoError(t, d.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

// Everything read through a snapshot is the database as it was at its first
// read, even through the transactions and failed statements of the code
// reading it, and it never reads through a second connection (#203).
func TestTxDriver_SnapshotReadsOneMomentOfTheDatabase(t *testing.T) {
	mgr, name, live := scratchDatabase(t)
	ctx := context.Background()
	snap, err := mgr.OpenSnapshot(ctx, name)
	require.NoError(t, err)
	defer snap.Rollback()

	assert.Equal(t, 1, count(t, ctx, snap, "t"))
	_, err = live.DB().ExecContext(ctx, `INSERT INTO t (n) VALUES (2)`)
	require.NoError(t, err)
	assert.Equal(t, 2, count(t, ctx, live, "t"))
	assert.Equal(t, 1, count(t, ctx, snap, "t"), "a write made since is not seen")

	// A transaction of the reading code is a savepoint of the snapshot.
	tx, err := snap.DB().BeginTx(ctx, nil)
	require.NoError(t, err)
	var n int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, tx.Commit())
	assert.Equal(t, 1, count(t, ctx, snap, "t"), "committing it did not end the snapshot")

	// An error the reading code tolerates does not end the snapshot either.
	_, err = snap.DB().QueryContext(ctx, `SELECT * FROM no_such_table`)
	require.Error(t, err)
	assert.Equal(t, 1, count(t, ctx, snap, "t"))

	// A statement that would need a second connection fails.
	rows, err := snap.DB().QueryContext(ctx, `SELECT n FROM t`)
	require.NoError(t, err)
	_, err = snap.DB().QueryContext(ctx, `SELECT n FROM t`)
	assert.ErrorContains(t, err, "one connection")
	require.NoError(t, rows.Close())

	require.NoError(t, snap.Check(ctx))
	require.NoError(t, snap.Rollback())
}

// On MariaDB a DDL statement commits the snapshot: Check says so.
func TestTxDriver_CheckSeesAnImplicitCommit(t *testing.T) {
	if testdb.Engine() != dbal.EngineMariaDB {
		t.Skip("MariaDB commits implicitly before DDL")
	}
	mgr, name, _ := scratchDatabase(t)
	ctx := context.Background()
	snap, err := mgr.OpenSnapshot(ctx, name)
	require.NoError(t, err)
	defer snap.Rollback()
	_, err = mgr.OpenTransaction(ctx, name)
	assert.Error(t, err, "no read-write transaction on MariaDB")
	_, _ = snap.DB().ExecContext(ctx, `CREATE TABLE u (n INT)`)
	assert.ErrorContains(t, snap.Check(ctx), "committed implicitly")
}

// A read-write transaction commits or rolls back as a whole, DDL included,
// and the reading code's own transactions roll back on their own.
func TestTxDriver_TransactionRollsBackAsAWhole(t *testing.T) {
	if testdb.Engine() != dbal.EnginePostgres {
		t.Skip("transactional DDL is PostgreSQL's")
	}
	mgr, name, live := scratchDatabase(t)
	ctx := context.Background()

	tx, err := mgr.OpenTransaction(ctx, name)
	require.NoError(t, err)
	_, err = tx.DB().ExecContext(ctx, `CREATE TABLE u (n INT)`)
	require.NoError(t, err)
	_, err = tx.DB().ExecContext(ctx, `INSERT INTO u (n) VALUES (1)`)
	require.NoError(t, err)
	inner, err := tx.DB().BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = inner.ExecContext(ctx, `INSERT INTO u (n) VALUES (2)`)
	require.NoError(t, err)
	require.NoError(t, inner.Rollback())
	assert.Equal(t, 1, count(t, ctx, tx, "u"), "only the inner transaction was rolled back")
	_, err = tx.DB().ExecContext(ctx, `INSERT INTO no_such_table (n) VALUES (1)`)
	require.Error(t, err)
	_, err = tx.DB().ExecContext(ctx, `INSERT INTO t (n) VALUES (3)`)
	require.NoError(t, err, "a failed statement did not abort the transaction")
	require.NoError(t, tx.Rollback())

	assert.Equal(t, 1, count(t, ctx, live, "t"), "the insert was rolled back")
	_, err = live.DB().ExecContext(ctx, `SELECT 1 FROM u`)
	assert.Error(t, err, "so was the table")

	tx, err = mgr.OpenTransaction(ctx, name)
	require.NoError(t, err)
	_, err = tx.DB().ExecContext(ctx, `INSERT INTO t (n) VALUES (4)`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	assert.Equal(t, 2, count(t, ctx, live, "t"))
}
