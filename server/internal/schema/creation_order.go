package schema

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ensureCreationOrder gives every user table of the catalog that lacks it
// the column its records are numbered in as they are created
// (dbal.CreationOrderColumn), which orders them as FileMaker does when
// nothing sorts them. server/migrations/007 describes the same.
//
// The records a table already has are numbered in the order the engine
// reads them (see dbal.CreationOrderType): on PostgreSQL their physical
// order, close to the order they were inserted in, so a migrated file keeps
// FileMaker's record order, since the migration inserts its records in that
// order; on MariaDB their id order, the order InnoDB stores them in, as no
// other is known. Records created afterwards are numbered after them.
//
// It reads before it writes, so a catalog already upgraded is only read, and
// a table registered without a physical table is left alone. A table another
// session holds is not waited for longer than creationOrderLockWait: it is
// left ordered by id, and given its column at a later sign-in.
func (s *Service) ensureCreationOrder(ctx context.Context) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()
	if dialect.Engine() == dbal.EngineSQLite {
		// Every SQLite table is created with its creation order, which SQLite
		// cannot add to an existing table (it is the table's rowid).
		return nil
	}
	// Each physical table, and whether it has the column.
	rows, err := db.QueryContext(ctx, `SELECT table_name, MAX(CASE WHEN column_name = `+dialect.Placeholder(1)+
		` THEN 1 ELSE 0 END) FROM `+dbal.CatalogColumnsSQL(dialect.Engine())+` AS c GROUP BY table_name`,
		dbal.CreationOrderColumn)
	if err != nil {
		return fmt.Errorf("failed reading the tables' columns: %w", err)
	}
	has := map[string]bool{}
	for rows.Next() {
		var table string
		var present int
		if err := rows.Scan(&table, &present); err != nil {
			rows.Close()
			return err
		}
		has[strings.ToLower(table)] = present == 1
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	tables, err := db.QueryContext(ctx, `SELECT name FROM sys_tables`)
	if err != nil {
		return fmt.Errorf("failed reading the tables: %w", err)
	}
	var missing []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			tables.Close()
			return err
		}
		present, exists := has[strings.ToLower(name)]
		if exists && !present && !dbal.IsReservedTableName(name) {
			missing = append(missing, name)
		}
	}
	tables.Close()
	if err := tables.Err(); err != nil {
		return err
	}

	added := false
	for _, name := range missing {
		if err := s.addCreationOrder(ctx, name); err != nil {
			if ctx.Err() != nil {
				return err
			}
			log.Printf("Warning: the records of %s are ordered by id until a later sign-in numbers them in creation order: %v", name, err)
			continue
		}
		added = true
	}
	if added {
		data.ForgetCreationOrder(db)
	}
	return nil
}

// creationOrderLockWait bounds how long the upgrade waits for a table another
// session holds, so a sign-in is never held up by a long transaction.
const creationOrderLockWait = 5 // seconds

// addCreationOrder gives a table its creation order column, waiting no
// longer than creationOrderLockWait for the table.
func (s *Service) addCreationOrder(ctx context.Context, name string) error {
	dialect := s.driver.Dialect()
	db := s.driver.DB()
	if dialect.Engine() == dbal.EngineMariaDB {
		_, err := db.ExecContext(ctx, dbal.AddCreationOrderSQL(dialect, name, creationOrderLockWait))
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%ds'", creationOrderLockWait)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, dbal.AddCreationOrderSQL(dialect, name, 0)); err != nil {
		return err
	}
	return tx.Commit()
}
