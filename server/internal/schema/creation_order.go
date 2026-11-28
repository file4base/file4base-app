package schema

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ensureRecordIdentity gives every user table of the catalog that lacks
// them the column its records are numbered in as they are created
// (dbal.CreationOrderColumn), which orders them as FileMaker does when
// nothing sorts them and is their record ID, and the column their
// modifications are counted in (dbal.ModCountColumn). server/migrations/007
// and 010 describe the same.
//
// The records a table already has are numbered in the order the engine
// reads them (see dbal.CreationOrderType): on PostgreSQL their physical
// order, close to the order they were inserted in, so a migrated file keeps
// FileMaker's record order, since the migration inserts its records in that
// order; on MariaDB their id order, the order InnoDB stores them in, as no
// other is known. Records created afterwards are numbered after them. They
// all count no modification yet, as a record FileMaker migrated has none.
// Neither column rewrites a table that has it: the numbers given stay.
//
// It reads before it writes, so a catalog already upgraded is only read, and
// a table registered without a physical table is left alone. A table another
// session holds is not waited for longer than creationOrderLockWait: it is
// left without the column, and given it at a later sign-in.
func (s *Service) ensureRecordIdentity(ctx context.Context) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()
	schema := "current_schema()"
	if dialect.Engine() == dbal.EngineMariaDB {
		schema = "DATABASE()"
	}
	// Each physical table, and whether it has each column.
	rows, err := db.QueryContext(ctx, `SELECT table_name, MAX(CASE WHEN column_name = `+dialect.Placeholder(1)+
		` THEN 1 ELSE 0 END), MAX(CASE WHEN column_name = `+dialect.Placeholder(2)+
		` THEN 1 ELSE 0 END) FROM information_schema.columns WHERE table_schema = `+schema+` GROUP BY table_name`,
		dbal.CreationOrderColumn, dbal.ModCountColumn)
	if err != nil {
		return fmt.Errorf("failed reading the tables' columns: %w", err)
	}
	type present struct{ creationOrder, modCount bool }
	has := map[string]present{}
	for rows.Next() {
		var table string
		var creationOrder, modCount int
		if err := rows.Scan(&table, &creationOrder, &modCount); err != nil {
			rows.Close()
			return err
		}
		has[strings.ToLower(table)] = present{creationOrder == 1, modCount == 1}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	tables, err := db.QueryContext(ctx, `SELECT name FROM sys_tables`)
	if err != nil {
		return fmt.Errorf("failed reading the tables: %w", err)
	}
	var missingOrder, missingCount []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			tables.Close()
			return err
		}
		columns, exists := has[strings.ToLower(name)]
		if !exists || dbal.IsReservedTableName(name) {
			continue
		}
		if !columns.creationOrder {
			missingOrder = append(missingOrder, name)
		}
		if !columns.modCount {
			missingCount = append(missingCount, name)
		}
	}
	tables.Close()
	if err := tables.Err(); err != nil {
		return err
	}

	added := false
	for _, name := range missingOrder {
		if err := s.addIdentityColumn(ctx, dbal.AddCreationOrderSQL, name); err != nil {
			if ctx.Err() != nil {
				return err
			}
			log.Printf("Warning: the records of %s are ordered by id until a later sign-in numbers them in creation order: %v", name, err)
			continue
		}
		added = true
	}
	for _, name := range missingCount {
		if err := s.addIdentityColumn(ctx, dbal.AddModCountSQL, name); err != nil {
			if ctx.Err() != nil {
				return err
			}
			log.Printf("Warning: the modifications of the records of %s are not counted until a later sign-in: %v", name, err)
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

// addIdentityColumn gives a table one of its record identity columns, the
// statement add makes, waiting no longer than creationOrderLockWait for the
// table.
func (s *Service) addIdentityColumn(ctx context.Context, add func(dbal.Dialect, string, int) string, name string) error {
	dialect := s.driver.Dialect()
	db := s.driver.DB()
	if dialect.Engine() == dbal.EngineMariaDB {
		_, err := db.ExecContext(ctx, add(dialect, name, creationOrderLockWait))
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
	if _, err := tx.ExecContext(ctx, add(dialect, name, 0)); err != nil {
		return err
	}
	return tx.Commit()
}
