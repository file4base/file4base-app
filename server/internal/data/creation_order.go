package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Creation order.
//
// FileMaker lists records in the order they were created when nothing sorts
// them — a found set, the records of a relationship without "Sort records",
// a portal on one — and its First, Last and GetNthRecord read that order.
// Every user table numbers its records as they are created
// (dbal.CreationOrderColumn), and that number orders them here the same way:
// alone when there is no sort, and after the sort otherwise, so records that
// sort alike stay in the order they were created and a page never depends on
// the engine's whim.
//
// A table created before the column existed gets it when its catalog is
// brought up to date (schema.EnsureSystemTables). Until then its records are
// ordered by id, as before: a read never alters a table.

// createdKey is where a record read across a path of relationships holds
// the number it was created with, or its id in a table not numbered yet, so
// the records the path reaches can be ordered all together. It holds a $, so
// no field has that name.
const createdKey = "f4b$created"

// knownCreationOrder keeps, by connection pool and table, what was found of
// the table's creation order column: true once the table has it, which it
// keeps, or the time it was found without it. That is only believed for
// missingCreationOrderTTL, as the column may be added at any
// time: by this server's next catalog upgrade, which forgets what it knew
// (ForgetCreationOrder), or by another server sharing the database.
var knownCreationOrder sync.Map

// missingCreationOrderTTL is how long a table found without its creation
// order column is taken not to have it before it is looked up again.
var missingCreationOrderTTL = 30 * time.Second

type creationOrderKey struct {
	db    *sql.DB
	table string
}

// ForgetCreationOrder drops what is known of the creation order columns of a
// database's tables, so that tables just given one are ordered by it.
func ForgetCreationOrder(db *sql.DB) {
	knownCreationOrder.Range(func(key, _ any) bool {
		if k, ok := key.(creationOrderKey); ok && k.db == db {
			knownCreationOrder.Delete(key)
		}
		return true
	})
}

// HasCreationOrder reports whether a table has its creation order column.
func HasCreationOrder(ctx context.Context, driver dbal.DatabaseDriver, tableName string) (bool, error) {
	key := creationOrderKey{driver.DB(), tableName}
	if known, ok := knownCreationOrder.Load(key); ok {
		if found, missing := known.(time.Time); !missing || time.Since(found) < missingCreationOrderTTL {
			return !missing, nil
		}
	}
	dialect := driver.Dialect()
	schema := "current_schema()"
	if dialect.Engine() == dbal.EngineMariaDB {
		schema = "DATABASE()"
	}
	var one int
	err := driver.DB().QueryRowContext(ctx, `SELECT 1 FROM information_schema.columns WHERE table_schema = `+schema+
		` AND table_name = `+dialect.Placeholder(1)+` AND column_name = `+dialect.Placeholder(2),
		tableName, dbal.CreationOrderColumn).Scan(&one)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		knownCreationOrder.Store(key, true)
		return true, nil
	}
	knownCreationOrder.Store(key, time.Now())
	return false, nil
}

// creationOrder is the column a table's records are ordered by when nothing
// else orders them: their creation order, or their id in a table that has
// none yet.
func (s *Service) creationOrder(ctx context.Context, tableName string) (string, error) {
	has, err := HasCreationOrder(ctx, s.driver, tableName)
	if err != nil {
		return "", err
	}
	if has {
		return dbal.CreationOrderColumn, nil
	}
	return "id", nil
}

// thenBy appends a last level to an ORDER BY clause, or makes one of it.
func thenBy(dialect dbal.Dialect, order, column string, descending bool) string {
	level := dialect.QuoteIdentifier(column) + " ASC"
	if descending {
		level = dialect.QuoteIdentifier(column) + " DESC"
	}
	if strings.TrimSpace(order) == "" {
		return "ORDER BY " + level
	}
	return order + ", " + level
}

// withCreationOrder adds the creation order column to a select list, so a
// statement reading the rows of a subquery can order them by it.
func withCreationOrder(dialect dbal.Dialect, list, column string) string {
	if column != dbal.CreationOrderColumn {
		return list // the id, which every select list reads
	}
	return list + ", " + dialect.QuoteIdentifier(column)
}
