package dbal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Text sort indexes. SQLite calls a collation, as any function of File4Base,
// through a Go callback of about a microsecond; a sort of a large table by
// the F4B collation is a callback per comparison. A text field sorted on a
// large table is therefore indexed on its sort key (SQLiteSortKey), the way
// FileMaker indexes a field the first time it is sorted: built in the
// background, after which a sort of the whole table reads its first records
// from the index instead of sorting the table. A find is not sorted through
// the index: walking the index to find the records that match costs a read
// of a record per record of the table when few match.
const (
	// SQLiteSortIndexMinRecords is the size of table from which a sorted
	// text field is indexed. Below it, sorting the table is fast enough.
	SQLiteSortIndexMinRecords = 10_000

	sqliteSortIndexBuildTimeout = 30 * time.Minute
	// sqliteSortIndexRetry is how long a sort index whose build failed (a
	// full disk) is not tried again.
	sqliteSortIndexRetry = 10 * time.Minute
)

// sqliteSortIndexMinRecords is SQLiteSortIndexMinRecords, which tests lower.
var sqliteSortIndexMinRecords int64 = SQLiteSortIndexMinRecords

// sqliteSortIndexes holds the sort indexes being built, so a field is
// indexed once however many sorts ask for it meanwhile, and the time a
// build failed, until it is tried again.
var sqliteSortIndexes sync.Map

// SQLiteSortKeySQL is the sort key of a quoted column, as an index on it
// is made and a sort that reads the index orders by. The text is passed as
// its bytes: a function is given a text up to its first zero byte, which
// the collation is not.
func SQLiteSortKeySQL(quotedColumn string) string {
	return SQLiteSortKey + "(CAST(" + quotedColumn + " AS BLOB))"
}

// SQLiteSortIndex is the name of the sort index of column of table, a
// text field, so a sort by it can read the index in the order of
// SQLiteSortKeySQL. When it has none and the table is large, the index is
// built in the background and "" is returned: the sort orders by the F4B
// collation meanwhile, in the same order.
func SQLiteSortIndex(ctx context.Context, db *sql.DB, table, column string) string {
	key := SQLiteSortKeySQL(quoteSQLite(column))
	name, err := sqliteSortIndexOf(ctx, db, table, key)
	if err != sql.ErrNoRows {
		return name
	}
	building := fmt.Sprintf("%p\x00%s\x00%s", db, table, column)
	if v, busy := sqliteSortIndexes.Load(building); busy {
		if failed, ok := v.(time.Time); !ok || time.Since(failed) < sqliteSortIndexRetry {
			return ""
		}
		sqliteSortIndexes.CompareAndDelete(building, v)
	}
	var large int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM `+quoteSQLite(table)+` LIMIT 1 OFFSET $1)`,
		sqliteSortIndexMinRecords-1).Scan(&large); err != nil || large == 0 {
		return ""
	}
	if _, busy := sqliteSortIndexes.LoadOrStore(building, struct{}{}); busy {
		return ""
	}
	// The name only has to be new: the index is found by what it reads. A
	// table renamed keeps its indexes under their names, so a name of the
	// table and column alone could be taken.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", table, column, time.Now().UnixNano())))
	create := fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
		quoteSQLite(sqliteSortIndexPrefix+hex.EncodeToString(sum[:6])), quoteSQLite(table), key)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sqliteSortIndexBuildTimeout)
		defer cancel()
		start := time.Now()
		if err := buildSQLiteSortIndex(ctx, db, table, column, key, create); err != nil {
			log.Printf("sqlite: indexing %s.%s for sorts failed: %v", table, column, err)
			sqliteSortIndexes.Store(building, time.Now())
			return
		}
		sqliteSortIndexes.Delete(building)
		log.Printf("sqlite: indexed %s.%s for sorts in %s", table, column, time.Since(start).Round(time.Millisecond))
	}()
	return ""
}

// sqliteSortIndexPrefix starts the name of every sort index: an index
// someone else made on the same key is not one.
const sqliteSortIndexPrefix = "f4b_sort_"

// sqliteSortIndexOf is the name of the sort index on key of table, or
// sql.ErrNoRows. A partial index cannot serve a list of the whole table.
func sqliteSortIndexOf(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, key string) (string, error) {
	var name string
	err := q.QueryRowContext(ctx,
		`SELECT m.name FROM sqlite_schema AS m, temp.pragma_index_list(m.tbl_name) AS l `+
			`WHERE m.type = 'index' AND m.tbl_name = $1 COLLATE NOCASE AND l.name = m.name AND NOT l.partial `+
			`AND m.name LIKE '`+sqliteSortIndexPrefix+`%' AND instr(lower(m.sql), lower($2)) > 0 LIMIT 1`,
		table, key).Scan(&name)
	return name, err
}

// buildSQLiteSortIndex makes a sort index in a write transaction, after
// checking there that the column is still there and has none: a column
// dropped meanwhile would be read as a text constant ("col"), and two
// builds of the same field in two processes would make two indexes.
func buildSQLiteSortIndex(ctx context.Context, db *sql.DB, table, column, key, create string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM temp.pragma_table_info($1) WHERE name = $2 COLLATE NOCASE`,
		table, column).Scan(&exists); err != nil || exists == 0 {
		return err
	}
	if _, err := sqliteSortIndexOf(ctx, tx, table, key); err != sql.ErrNoRows {
		return err
	}
	if _, err := tx.ExecContext(ctx, create); err != nil {
		return err
	}
	return tx.Commit()
}

func quoteSQLite(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// isSQLiteSortIndex tells a sort index (SQLiteSortIndex) by its name.
func isSQLiteSortIndex(name string) bool {
	return strings.HasPrefix(name, sqliteSortIndexPrefix)
}
