package schema

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
)

// checkRepetitions validates a field's maximum number of repetitions. Every
// kind of field may repeat except a summary, which has no value in a record,
// as in FileMaker.
func checkRepetitions(fieldType dbal.AgnosticFieldType, max int) error {
	if err := repetition.Check(max); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
	}
	if max > 1 && fieldType == dbal.FieldTypeSummary {
		return fmt.Errorf("%w: a summary field cannot have repetitions", ErrInvalidFieldOptions)
	}
	return nil
}

// addRepetitionsColumnSQL adds the column that holds repetitions 2 to N of a
// field (see the repetition package). It is text on every engine; IF NOT
// EXISTS lets a field whose maximum went down and up again keep the values it
// had.
func (s *Service) addRepetitionsColumnSQL(tableName, field string) string {
	dialect := s.driver.Dialect()
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		dialect.QuoteIdentifier(tableName), dialect.QuoteIdentifier(repetition.Column(field)),
		dialect.MapType(dbal.FieldTypeText))
}

// prepareRepetitionsChange checks a new maximum number of repetitions for a
// field and gives the field somewhere to keep them. Lowering the maximum
// deletes nothing: the repetitions past it are hidden, and come back if it is
// raised again, as in FileMaker.
func (s *Service) prepareRepetitionsChange(ctx context.Context, tableID, columnID string, max int) error {
	q := `SELECT t.name, c.name, c.field_type, c.is_primary_key FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE c.id = $1 AND t.id = $2`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT t.name, c.name, c.field_type, c.is_primary_key FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE c.id = ? AND t.id = ?`
	}
	var table, field, fieldType string
	var primaryKey bool
	if err := s.driver.DB().QueryRowContext(ctx, q, columnID, tableID).Scan(&table, &field, &fieldType, &primaryKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("column not found: %s", columnID)
		}
		return err
	}
	if err := checkRepetitions(dbal.AgnosticFieldType(fieldType), max); err != nil {
		return err
	}
	if max > 1 && primaryKey {
		return fmt.Errorf("%w: the primary key cannot have repetitions", ErrInvalidFieldOptions)
	}
	if max <= 1 {
		return nil
	}
	if _, err := s.driver.DB().ExecContext(ctx, s.addRepetitionsColumnSQL(table, field)); err != nil {
		return fmt.Errorf("failed adding the repetitions of %s: %w", field, err)
	}
	return nil
}

// companionColumns lists the companion columns table physically has. A field
// whose maximum went down, even to 1, keeps its companion column and the
// repetitions in it, so a data bundle has to carry them too.
func (s *Service) companionColumns(ctx context.Context, table string) (map[string]bool, error) {
	q := `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name LIKE '%$reps'`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name LIKE '%$reps'`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, table)
	if err != nil {
		return nil, fmt.Errorf("failed listing the repetitions of %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// exportRepetitions puts every repetition from 2 a companion column holds
// into a data bundle row, under "field[n]", with the type the field's own
// column has there: a container's bytes, not their base64 text. Repetitions
// past the field's current maximum, hidden from records, are exported too,
// so a restore loses none of them.
func exportRepetitions(row map[string]interface{}, c ColumnMetadata, storage dbal.AgnosticFieldType, raw interface{}) error {
	values, err := repetition.Unpack(storage, repetition.Max, raw)
	if err != nil {
		return fmt.Errorf("the repetitions of %s cannot be read: %w", c.Name, err)
	}
	for n, v := range values {
		if text, ok := v.(string); ok && storage == dbal.FieldTypeContainer {
			b, err := base64.StdEncoding.DecodeString(text)
			if err != nil {
				return fmt.Errorf("repetition %d of %s is not a container value: %w", n, c.Name, err)
			}
			v = b
		}
		row[repetition.Key(c.Name, n)] = v
	}
	return nil
}

// packBundleRepetitions takes the "field[n]" keys out of a data bundle row
// and returns each field's companion column content. repeating holds every
// field that may have a companion column: a field whose maximum is lower than
// the repetitions it carries keeps them, hidden, as it did when exported.
func packBundleRepetitions(row map[string]interface{}, repeating map[string]ColumnMetadata) (map[string]interface{}, error) {
	if len(repeating) == 0 {
		return nil, nil
	}
	given := map[string]map[int]interface{}{}
	for key, v := range row {
		base, n, ok := repetition.Split(key)
		if !ok {
			continue
		}
		if _, repeats := repeating[base]; !repeats {
			continue
		}
		delete(row, key)
		if n == 1 {
			if _, both := row[base]; !both {
				row[base] = v
			}
			continue
		}
		if given[base] == nil {
			given[base] = map[int]interface{}{}
		}
		given[base][n] = v
	}
	out := make(map[string]interface{}, len(given))
	for base, values := range given {
		c := repeating[base]
		bound := c.MaxRepetitions
		for n := range values {
			bound = max(bound, n)
		}
		packed, err := repetition.Pack(StorageType(c.FieldType, c.CalculationFormula), bound, values)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", base, err)
		}
		out[base] = packed
	}
	return out, nil
}
