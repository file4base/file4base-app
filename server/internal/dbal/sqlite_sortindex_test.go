package dbal

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sortIndexes lists the sort indexes of a table.
func sortIndexes(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT sql FROM sqlite_schema WHERE type = 'index' AND tbl_name = $1 AND instr(sql, 'f4b_sortkey(') > 0`, table)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// waitSortIndex waits for the background build of a column's sort index.
func waitSortIndex(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	require.Eventually(t, func() bool { return SQLiteSortIndex(context.Background(), db, table, column) != "" },
		10*time.Second, 10*time.Millisecond)
}

func lowerSortIndexThreshold(t *testing.T, n int64) {
	old := sqliteSortIndexMinRecords
	sqliteSortIndexMinRecords = n
	t.Cleanup(func() { sqliteSortIndexMinRecords = old })
}

var sortTexts = []string{"", "a", "A", "á", "Á", "ab", "aB", "Ab", "b", "é", "e", "E", "ê", "z", "Z", "ß", "ss",
	"œ", "oe", "a\x00", "a\x00b", "1", "10", "2", "  a", "-a", "Ölgemälde", "olgemalde", "straße", "strasse", "日本"}

// A sort by the sort key gives the order of the F4B collation, so a sort
// gives the same records whether the index is there yet or not.
func TestSQLite_SortKeyOrdersAsTheCollation(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	r := rand.New(rand.NewSource(1))
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	for i := range 3000 {
		var v any
		switch {
		case i%97 == 0:
			v = nil
		case i < len(sortTexts):
			v = sortTexts[i]
		default:
			v = sortTexts[r.Intn(len(sortTexts))] + sortTexts[r.Intn(len(sortTexts))]
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ($1)`, v)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())

	order := func(by string) []int64 {
		rows, err := db.QueryContext(ctx, `SELECT id FROM t ORDER BY `+by+`, id`)
		require.NoError(t, err)
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		require.NoError(t, rows.Err())
		return ids
	}
	byCollation := order(`v COLLATE F4B`)
	assert.Equal(t, byCollation, order(SQLiteSortKeySQL(`"v"`)))
	_, err = db.ExecContext(ctx, `CREATE INDEX t_v_key ON t (`+SQLiteSortKeySQL(`"v"`)+`)`)
	require.NoError(t, err)
	assert.Equal(t, byCollation, order(SQLiteSortKeySQL(`"v"`)), "through the index")
	rows, err := db.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT id FROM t ORDER BY `+SQLiteSortKeySQL(`"v"`)+` LIMIT 10`)
	require.NoError(t, err)
	var detail string
	for rows.Next() {
		var id, parent, unused int
		var d string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &d))
		detail += d + "\n"
	}
	require.NoError(t, rows.Close())
	assert.Contains(t, detail, "t_v_key", "the sort reads the index")
}

// A sort of a large table's text field indexes it once, in the background;
// a small table is not indexed.
func TestSQLite_SortIndexIsBuiltOnceForALargeTable(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	lowerSortIndexThreshold(t, 100)
	_, err := db.ExecContext(ctx, `CREATE TABLE "Big Table" (id INTEGER PRIMARY KEY, "na""me" TEXT); CREATE TABLE small (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 150)
		INSERT INTO "Big Table" ("na""me") SELECT 'v' || i FROM n; INSERT INTO small (v) VALUES ('x')`)
	require.NoError(t, err)

	assert.Empty(t, SQLiteSortIndex(ctx, db, "small", "v"))
	for range 5 {
		SQLiteSortIndex(ctx, db, "Big Table", `na"me`)
	}
	waitSortIndex(t, db, "Big Table", `na"me`)
	assert.Len(t, sortIndexes(t, db, "Big Table"), 1)
	assert.Empty(t, sortIndexes(t, db, "small"))

	// The file stays sound and a backup takes the index along.
	var check string
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check))
	assert.Equal(t, "ok", check)
	backup := t.TempDir() + "/backup" + SQLiteFileExtension
	require.NoError(t, BackupSQLite(ctx, db, backup))
	b, err := openSQLite(ctx, backup)
	require.NoError(t, err)
	defer b.Close()
	require.NoError(t, b.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&check))
	assert.Equal(t, "ok", check)
	assert.Len(t, sortIndexes(t, b, "Big Table"), 1)
}

// Dropping, changing the type of and renaming a column with a sort index
// takes the index along.
func TestSQLite_ColumnChangesTakeTheSortIndexAlong(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	lowerSortIndexThreshold(t, 1)
	_, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT, b TEXT, c TEXT);
		CREATE INDEX t_b ON t (b);
		INSERT INTO t (a, b, c) VALUES ('x', '1', 'y'), ('y', '2', 'z')`)
	require.NoError(t, err)
	for _, col := range []string{"a", "b", "c"} {
		SQLiteSortIndex(ctx, db, "t", col)
		waitSortIndex(t, db, "t", col)
	}
	require.Len(t, sortIndexes(t, db, "t"), 3)

	_, err = db.ExecContext(ctx, `ALTER TABLE "t" DROP COLUMN "a"`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE "t" ALTER COLUMN "b" TYPE NUMERIC`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE "t" RENAME COLUMN "c" TO "d"`)
	require.NoError(t, err)

	indexes := sortIndexes(t, db, "t")
	require.Len(t, indexes, 1, "only the renamed column's is left")
	assert.Contains(t, indexes[0], SQLiteSortKeySQL(`"d"`))
	assert.NotEmpty(t, SQLiteSortIndex(ctx, db, "t", "d"))
	var plain int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name = 't_b'`).Scan(&plain))
	assert.Equal(t, 1, plain, "the column's own index is made again")
	var sum float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sum(b) FROM t`).Scan(&sum))
	assert.Equal(t, 3.0, sum)
}

// A new database is made with the page size of SQLitePageSize.
func TestSQLite_NewDatabasesHaveLargePages(t *testing.T) {
	db, _ := newSQLiteDB(t)
	var size int
	require.NoError(t, db.QueryRow(`PRAGMA page_size`).Scan(&size))
	assert.Equal(t, SQLitePageSize, size)
	var mode string
	require.NoError(t, db.QueryRow(`PRAGMA journal_mode`).Scan(&mode))
	assert.Equal(t, "wal", mode, fmt.Sprint("page size ", size))
}
