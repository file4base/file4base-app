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
)

// sqliteSortIndexMinRecords is SQLiteSortIndexMinRecords, which tests lower.
var sqliteSortIndexMinRecords int64 = SQLiteSortIndexMinRecords

// sqliteSortIndexes holds the sort indexes being built, so a field is
// indexed once however many sorts ask for it meanwhile.
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
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_schema WHERE type = 'index' AND tbl_name = $1 AND instr(sql, $2) > 0 LIMIT 1`,
		table, key).Scan(&name)
	if err != sql.ErrNoRows {
		return name
	}
	building := fmt.Sprintf("%p\x00%s\x00%s", db, table, column)
	if _, busy := sqliteSortIndexes.Load(building); busy {
		return ""
	}
	var last sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT max(rowid) FROM `+quoteSQLite(table)).Scan(&last); err != nil || last.Int64 < sqliteSortIndexMinRecords {
		return ""
	}
	if _, busy := sqliteSortIndexes.LoadOrStore(building, struct{}{}); busy {
		return ""
	}
	// The name only has to be new: the index is found by what it reads. A
	// table renamed keeps its indexes under their names, so a name of the
	// table and column alone could be taken.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", table, column, time.Now().UnixNano())))
	create := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		quoteSQLite("f4b_sort_"+hex.EncodeToString(sum[:6])), quoteSQLite(table), key)
	go func() {
		defer sqliteSortIndexes.Delete(building)
		ctx, cancel := context.WithTimeout(context.Background(), sqliteSortIndexBuildTimeout)
		defer cancel()
		start := time.Now()
		if _, err := db.ExecContext(ctx, create); err != nil {
			log.Printf("sqlite: indexing %s.%s for sorts failed: %v", table, column, err)
			return
		}
		log.Printf("sqlite: indexed %s.%s for sorts in %s", table, column, time.Since(start).Round(time.Millisecond))
	}()
	return ""
}

func quoteSQLite(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// isSQLiteSortIndex tells a sort index (SQLiteSortIndex) by the statement
// that made it.
func isSQLiteSortIndex(create string) bool {
	return strings.Contains(create, SQLiteSortKey+"(")
}
