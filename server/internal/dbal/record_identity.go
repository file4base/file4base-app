package dbal

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// A record's identity, as FileMaker gives it (#208): its record ID, the
// number it was created with (CreationOrderColumn), and its modification
// count, how many writes set a field stored with it (ModCountColumn). The
// Data API calls them recordId and modId, and Get ( RecordID ) and
// Get ( RecordModificationCount ) read them.
//
// A record ID is per table, ascending in creation order, and never given
// twice, even once the record that had it is deleted. Every insert takes it
// from the table's allocator before the record exists (data.ReserveRecordIDs),
// so a formula computed for a new record knows it and a create that fails
// spends it, as FileMaker's do: on PostgreSQL the column's identity
// sequence, on MariaDB a row of RecordIDsTable, as the AUTO_INCREMENT counter
// cannot be read and advanced atomically. The engine's own default (the
// identity, AUTO_INCREMENT) only numbers the records a table holds when it
// is given the column.

// ModCountColumn is the column a user table counts each record's committed
// changes in, from 0 for a record never modified. Like CreationOrderColumn it
// is not a field: it is not in the catalog, and "$" is in no field name.
const ModCountColumn = "f4b$mod_count"

// ModCountType is the definition of the modification count column. A table
// given it holds 0 in every record: on PostgreSQL 11 and later and on MariaDB
// 10.3 and later the column is added without rewriting the table.
const ModCountType = "BIGINT NOT NULL DEFAULT 0"

// AddModCountSQL gives an existing table the modification count column, with
// the bounded lock wait of AddCreationOrderSQL. On MariaDB it insists on an
// instant change: a table that would have to be rebuilt (a compressed one)
// is left without the column rather than rebuilt at sign-in.
func AddModCountSQL(dialect Dialect, tableName string, lockWait int) string {
	wait, instant := "", ""
	if dialect.Engine() == EngineMariaDB {
		if lockWait > 0 {
			wait = fmt.Sprintf(" WAIT %d", lockWait)
		}
		instant = ", ALGORITHM=INSTANT"
	}
	return fmt.Sprintf("ALTER TABLE %s%s ADD COLUMN %s %s%s", dialect.QuoteIdentifier(tableName), wait,
		dialect.QuoteIdentifier(ModCountColumn), ModCountType, instant)
}

// MaxRecordIdentity bounds the record IDs and modification counts a record
// can be given from outside (a restore, a migration), so that the allocator
// and the count always have room to go on.
const MaxRecordIdentity int64 = 1 << 62

// IsRecordIDConflict reports whether err is an insert refused because
// another record holds the record ID it gives (CreationOrderColumn).
func IsRecordIDConflict(err error) bool {
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		// "Duplicate entry '12' for key 'f4b$creation_order'": the key is
		// last, after the value, which may hold anything.
		return my.Number == 1062 && strings.HasSuffix(strings.TrimSpace(my.Message), CreationOrderColumn+"'")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23505" && strings.Contains(pg.ConstraintName, CreationOrderColumn)
	}
	return false
}

// RecordIDsTable is, on MariaDB, the allocator of each user table's record
// IDs: the last ID given, by table name. A table has no row until its first
// ID is reserved, which seeds it past every ID the table already holds or
// its AUTO_INCREMENT counter has passed.
const RecordIDsTable = "sys_record_ids"

// RecordIDsTableSQL creates RecordIDsTable. PostgreSQL has no use for it.
const RecordIDsTableSQL = `CREATE TABLE IF NOT EXISTS sys_record_ids (
	table_name VARCHAR(64) PRIMARY KEY,
	last_id BIGINT NOT NULL
)`

// RecordIDSequenceSQL is, on PostgreSQL, the expression naming the sequence
// of a table's creation order column, for nextval and setval. The table is
// bound as its quoted identifier ($1, QuoteIdentifier), as
// pg_get_serial_sequence parses it as SQL.
func RecordIDSequenceSQL(placeholder string) string {
	return "pg_get_serial_sequence(" + placeholder + ", '" + CreationOrderColumn + "')"
}

// OverridingSystemValue is what a PostgreSQL INSERT that gives the creation
// order column its value says between its column list and VALUES: the
// column is GENERATED ALWAYS. Empty on MariaDB, which takes the value given
// to an AUTO_INCREMENT column and moves its counter past it.
func OverridingSystemValue(engine EngineType) string {
	if engine == EnginePostgres {
		return " OVERRIDING SYSTEM VALUE"
	}
	return ""
}
