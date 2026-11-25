package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"

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

// knownCreationOrder keeps, by connection pool and table, whether the table
// has its creation order column, once it was looked up.
var knownCreationOrder sync.Map

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
		return known.(bool), nil
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
	has := err == nil
	knownCreationOrder.Store(key, has)
	return has, nil
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
