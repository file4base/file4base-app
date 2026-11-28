package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// The FileMaker ids of migrated fields deleted since (#191). A FileMaker
// Import Records step reads its source fields by position, the field's place
// among its table's fields in creation order, and a field deleted after the
// migration would shift every later position onto the next field: kept here,
// it keeps its place, and a step that reads it is told so.
const deletedSourceFieldsSQL = `CREATE TABLE IF NOT EXISTS sys_deleted_source_fields (
			table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
			source_field_id VARCHAR(32) NOT NULL,
			PRIMARY KEY (table_id, source_field_id)
		);`

func (s *Service) ensureDeletedSourceFields(ctx context.Context) error {
	if _, err := s.driver.DB().ExecContext(ctx, deletedSourceFieldsSQL); err != nil {
		return fmt.Errorf("failed creating sys_deleted_source_fields: %w", err)
	}
	return nil
}

// recordDeletedSourceField keeps the FileMaker id of a field being deleted.
func (s *Service) recordDeletedSourceField(ctx context.Context, tx *sql.Tx, tableID, sourceFieldID string) error {
	if strings.TrimSpace(sourceFieldID) == "" {
		return nil
	}
	dialect := s.driver.Dialect()
	del := fmt.Sprintf(`DELETE FROM sys_deleted_source_fields WHERE table_id = %s AND source_field_id = %s`,
		dialect.Placeholder(1), dialect.Placeholder(2))
	if _, err := tx.ExecContext(ctx, del, tableID, sourceFieldID); err != nil {
		return fmt.Errorf("failed keeping the FileMaker id of the deleted field: %w", err)
	}
	ins := fmt.Sprintf(`INSERT INTO sys_deleted_source_fields (table_id, source_field_id) VALUES (%s, %s)`,
		dialect.Placeholder(1), dialect.Placeholder(2))
	if _, err := tx.ExecContext(ctx, ins, tableID, sourceFieldID); err != nil {
		return fmt.Errorf("failed keeping the FileMaker id of the deleted field: %w", err)
	}
	return nil
}

// DeletedSourceFieldIDs are the FileMaker ids of the fields of tableName
// deleted since the migration, keyed by nothing else: none for a table that
// was not migrated or lost no field.
func (s *Service) DeletedSourceFieldIDs(ctx context.Context, tableName string) ([]string, error) {
	if err := s.ensureDeletedSourceFields(ctx); err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	q := fmt.Sprintf(`SELECT d.source_field_id FROM sys_deleted_source_fields d
		JOIN sys_tables t ON t.id = d.table_id WHERE t.name = %s`, dialect.Placeholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the deleted fields of %s: %w", tableName, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
