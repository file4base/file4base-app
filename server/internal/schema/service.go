package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrTableExists is returned when a table name is already registered.
var ErrTableExists = errors.New("table already exists")

// tableNameTaken reports whether a table with this name is in the catalog.
// A physical table outside the catalog makes CREATE TABLE itself fail.
func (s *Service) tableNameTaken(ctx context.Context, name string) (bool, error) {
	q := `SELECT COUNT(*) FROM sys_tables WHERE name = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		q = `SELECT COUNT(*) FROM sys_tables WHERE name = ?`
	}
	var n int
	if err := s.driver.DB().QueryRowContext(ctx, q, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// copyName builds "<base><suffix>" within the identifier length limit by
// shortening the base.
func copyName(base, suffix string) string {
	if over := len(base) + len(suffix) - dbal.MaxIdentifierLength; over > 0 {
		base = strings.TrimRight(base[:len(base)-over], "_")
	}
	return base + suffix
}

// ErrInvalidFieldOptions is returned when a column's default_value is not a
// JSON object of field options.
var ErrInvalidFieldOptions = errors.New("invalid field options")

// ValidateFieldOptions checks a column's default_value. It holds the field's
// auto-enter and storage options as a JSON object (see the Fields dialog), or
// nothing. It is metadata only: it is never placed in SQL.
func ValidateFieldOptions(options *string) error {
	if options == nil || strings.TrimSpace(*options) == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(*options), &obj); err != nil || obj == nil {
		return fmt.Errorf("%w: default_value must be a JSON object of field options", ErrInvalidFieldOptions)
	}
	return nil
}

// TableMetadata represents a user database table entry in sys_tables
type TableMetadata struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	DisplayName string           `json:"display_name"`
	Description string           `json:"description,omitempty"`
	Columns     []ColumnMetadata `json:"columns,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// ColumnMetadata represents a column in sys_columns
type ColumnMetadata struct {
	ID                 string                 `json:"id"`
	TableID            string                 `json:"table_id"`
	Name               string                 `json:"name"`
	DisplayName        string                 `json:"display_name"`
	FieldType          dbal.AgnosticFieldType `json:"field_type"`
	IsNullable         bool                   `json:"is_nullable"`
	IsPrimaryKey       bool                   `json:"is_primary_key"`
	DefaultValue       *string                `json:"default_value,omitempty"`
	CalculationFormula *string                `json:"calculation_formula,omitempty"`
	ValidationRules    *string                `json:"validation_rules,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
}

// RelationshipMetadata represents an occurrence join in sys_relationships
type RelationshipMetadata struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	LeftOccurrenceID  string    `json:"left_occurrence_id"`
	LeftColumnID      string    `json:"left_column_id"`
	RightOccurrenceID string    `json:"right_occurrence_id"`
	RightColumnID     string    `json:"right_column_id"`
	Operator          string    `json:"operator"`
	AllowCreation     bool      `json:"allow_creation"`
	CascadeDelete     bool      `json:"cascade_delete"`
	SortRelated       string    `json:"sort_related,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// Service manages schema metadata and executes dynamic DDL
type Service struct {
	driver dbal.DatabaseDriver
}

func NewService(driver dbal.DatabaseDriver) *Service {
	return &Service{driver: driver}
}

// EnsureSystemTables creates the sys_* catalog if it is not present.
// It never creates user accounts: a database only gets its first owner through
// EnsureSystemTablesWithCredentials, with credentials chosen by the caller.
func (s *Service) EnsureSystemTables(ctx context.Context) error {
	return s.EnsureSystemTablesWithCredentials(ctx, "", "")
}

// EnsureSystemTablesWithCredentials provisions the system tables and, when both
// a username and a password are supplied, the initial owner account. There are
// no built-in default credentials.
func (s *Service) EnsureSystemTablesWithCredentials(ctx context.Context, initialOwnerUser, initialOwnerPassword string) error {
	// The catalog is the same on every engine except its timestamp columns,
	// whose type and default differ (TIMESTAMPTZ / DATETIME(6)).
	ts := s.driver.Dialect().TimestampColumn()
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sys_tables (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			display_name VARCHAR(128) NOT NULL,
			description TEXT,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_columns (
			id VARCHAR(36) PRIMARY KEY,
			table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
			name VARCHAR(64) NOT NULL,
			display_name VARCHAR(128) NOT NULL,
			field_type VARCHAR(32) NOT NULL,
			is_nullable BOOLEAN DEFAULT TRUE,
			is_primary_key BOOLEAN DEFAULT FALSE,
			default_value TEXT,
			calculation_formula TEXT,
			validation_rules TEXT,
			created_at ` + ts + `,
			CONSTRAINT uq_table_column UNIQUE (table_id, name)
		);`,
		`CREATE TABLE IF NOT EXISTS sys_table_occurrences (
			id VARCHAR(36) PRIMARY KEY,
			base_table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
			name VARCHAR(64) NOT NULL UNIQUE,
			x_pos DOUBLE PRECISION DEFAULT 100,
			y_pos DOUBLE PRECISION DEFAULT 100,
			created_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_relationships (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			left_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
			left_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
			right_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
			right_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
			operator VARCHAR(8) DEFAULT '=',
			allow_creation BOOLEAN DEFAULT FALSE,
			cascade_delete BOOLEAN DEFAULT FALSE,
			sort_related TEXT,
			created_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_layouts (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			table_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
			definition TEXT NOT NULL,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_value_lists (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL UNIQUE,
			kind VARCHAR(16) NOT NULL DEFAULT 'custom',
			custom_values TEXT,
			source_table_id VARCHAR(36) REFERENCES sys_tables(id) ON DELETE CASCADE,
			source_column_id VARCHAR(36) REFERENCES sys_columns(id) ON DELETE CASCADE,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_users (
			id VARCHAR(36) PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(256) NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'user',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		`ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE;`,
		`CREATE TABLE IF NOT EXISTS sys_user_permissions (
			id VARCHAR(36) PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
			layout_id VARCHAR(36) NOT NULL REFERENCES sys_layouts(id) ON DELETE CASCADE,
			access_level VARCHAR(16) NOT NULL DEFAULT 'read_write',
			created_at ` + ts + `,
			CONSTRAINT uq_user_layout UNIQUE (user_id, layout_id)
		);`,
		// A named connection to another SQL database records can be
		// imported from (#47). It holds no password: one is supplied with
		// the request, or taken from the environment variable named here.
		`CREATE TABLE IF NOT EXISTS sys_data_sources (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL UNIQUE,
			engine VARCHAR(32) NOT NULL,
			host VARCHAR(255) NOT NULL,
			port INT,
			database_name VARCHAR(128),
			username VARCHAR(128),
			schema_name VARCHAR(128),
			use_tls BOOLEAN NOT NULL DEFAULT FALSE,
			password_env VARCHAR(128),
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		// A named set of find requests on one table (#34). The requests
		// are stored as the criteria were typed, so a saved find can be
		// opened, read and changed in Find mode.
		`CREATE TABLE IF NOT EXISTS sys_saved_finds (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			table_name VARCHAR(64) NOT NULL,
			requests TEXT NOT NULL,
			created_by VARCHAR(36),
			created_at ` + ts + `,
			updated_at ` + ts + `,
			CONSTRAINT uq_saved_find_name UNIQUE (table_name, name)
		);`,
		// Capabilities granted per account role (#39). A role without a
		// row holds every capability, so a database created before this
		// table existed keeps working unrestricted.
		`CREATE TABLE IF NOT EXISTS sys_privileges (
			role VARCHAR(32) PRIMARY KEY,
			bulk_export BOOLEAN NOT NULL DEFAULT TRUE,
			bulk_import BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_scripts (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			context_table VARCHAR(128) DEFAULT '',
			folder_id VARCHAR(36),
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		`CREATE TABLE IF NOT EXISTS sys_script_steps (
			id VARCHAR(36) PRIMARY KEY,
			script_id VARCHAR(36) NOT NULL REFERENCES sys_scripts(id) ON DELETE CASCADE,
			sequence_idx INT NOT NULL,
			step_type VARCHAR(64) NOT NULL,
			params TEXT NOT NULL DEFAULT '{}',
			is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			parent_step_id VARCHAR(36),
			created_at ` + ts + `
		);`,
	}

	db := s.driver.DB()
	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed creating system tables: %w", err)
		}
	}

	provisionUser := func(username, password, role string) {
		username = strings.ToLower(strings.TrimSpace(username))
		if username == "" {
			return
		}
		var count int
		var err error
		if s.driver.Dialect().Engine() == dbal.EnginePostgres {
			err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users WHERE LOWER(username) = LOWER($1)`, username).Scan(&count)
		} else {
			err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users WHERE LOWER(username) = LOWER(?)`, username).Scan(&count)
		}
		if err == nil && count == 0 {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err == nil {
				ownerID := uuid.NewString()
				now := time.Now().UTC()
				if s.driver.Dialect().Engine() == dbal.EnginePostgres {
					_, _ = db.ExecContext(ctx,
						`INSERT INTO sys_users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (username) DO NOTHING`,
						ownerID, username, string(hash), role, now, now,
					)
				} else {
					_, _ = db.ExecContext(ctx,
						`INSERT INTO sys_users (id, username, password_hash, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
						ownerID, username, string(hash), role, now, now,
					)
				}
			}
		}
	}

	if strings.TrimSpace(initialOwnerUser) != "" && initialOwnerPassword != "" {
		provisionUser(initialOwnerUser, initialOwnerPassword, "owner")
	}

	return nil
}

// CreateTable registers metadata and dynamically generates the physical table via DBAL
// occurrencePosition lays out table occurrences on the relationships graph in
// rows of four, so a new one never lands on top of an existing card. The
// spacing matches the card size the client draws.
// occupiedPositions reads where the occurrences of the graph already sit, so a
// new one can be put somewhere else.
func occupiedPositions(ctx context.Context, tx *sql.Tx) (map[[2]float64]struct{}, error) {
	rows, err := tx.QueryContext(ctx, `SELECT x_pos, y_pos FROM sys_table_occurrences`)
	if err != nil {
		return nil, fmt.Errorf("failed reading table occurrence positions: %w", err)
	}
	defer rows.Close()

	taken := make(map[[2]float64]struct{})
	for rows.Next() {
		var x, y float64
		if err := rows.Scan(&x, &y); err != nil {
			return nil, err
		}
		taken[[2]float64{x, y}] = struct{}{}
	}
	return taken, rows.Err()
}

// firstFreePosition is the earliest slot of the grid that nothing sits on.
func firstFreePosition(taken map[[2]float64]struct{}) (float64, float64) {
	for index := 0; ; index++ {
		x, y := occurrencePosition(index)
		if _, clash := taken[[2]float64{x, y}]; !clash {
			return x, y
		}
	}
}

func occurrencePosition(index int) (float64, float64) {
	const (
		originX = 100.0
		originY = 100.0
		stepX   = 320.0
		stepY   = 420.0
		perRow  = 4
	)
	col := index % perRow
	row := index / perRow
	return originX + float64(col)*stepX, originY + float64(row)*stepY
}

func (s *Service) CreateTable(ctx context.Context, displayName, customName string) (*TableMetadata, error) {
	name := dbal.NormalizeIdentifier(customName)
	if name == "" {
		name = dbal.NormalizeIdentifier(strings.ReplaceAll(strings.TrimSpace(displayName), " ", "_"))
	}
	if err := dbal.CheckUserTableName(name); err != nil {
		return nil, err
	}
	if taken, err := s.tableNameTaken(ctx, name); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: '%s'", ErrTableExists, name)
	}

	tableID := uuid.NewString()
	occID := uuid.NewString()
	now := time.Now().UTC()

	// Default columns: id (Primary Key)
	pkColumn := ColumnMetadata{
		ID:           uuid.NewString(),
		TableID:      tableID,
		Name:         "id",
		DisplayName:  "ID",
		FieldType:    dbal.FieldTypeText,
		IsNullable:   false,
		IsPrimaryKey: true,
		CreatedAt:    now,
	}

	// 1. Build physical table DDL via Dialect
	dialect := s.driver.Dialect()
	ddlSQL, err := dialect.BuildCreateTableSQL(dbal.TableDefinition{
		Name: name,
		Columns: []dbal.ColumnDefinition{
			{
				Name:         pkColumn.Name,
				Type:         pkColumn.FieldType,
				IsNullable:   pkColumn.IsNullable,
				IsPrimaryKey: pkColumn.IsPrimaryKey,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed generating DDL for table %s: %w", name, err)
	}

	// 2. Transactionally execute DDL and insert system catalog rows
	db := s.driver.DB()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Create physical table
	if _, err := tx.ExecContext(ctx, ddlSQL); err != nil {
		return nil, fmt.Errorf("failed creating physical table %s: %w", name, err)
	}

	// Insert into sys_tables
	insertTableSQL := `INSERT INTO sys_tables (id, name, display_name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`
	if dialect.Engine() == dbal.EngineMariaDB {
		insertTableSQL = `INSERT INTO sys_tables (id, name, display_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	}
	if _, err := tx.ExecContext(ctx, insertTableSQL, tableID, name, displayName, now, now); err != nil {
		return nil, fmt.Errorf("failed registering table in sys_tables: %w", err)
	}

	// Insert into sys_columns
	insertColSQL := `INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	if dialect.Engine() == dbal.EngineMariaDB {
		insertColSQL = `INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	}
	if _, err := tx.ExecContext(ctx, insertColSQL, pkColumn.ID, tableID, pkColumn.Name, pkColumn.DisplayName, string(pkColumn.FieldType), pkColumn.IsNullable, pkColumn.IsPrimaryKey, now); err != nil {
		return nil, fmt.Errorf("failed registering primary key column: %w", err)
	}

	// Insert default Table Occurrence. Its position is staggered: every
	// occurrence used to be created at the column default (100, 100), so in a
	// database with more than one table the cards sat exactly on top of each
	// other and the graph looked as if it held a single table.
	//
	// The slot is the first free one, not the number of occurrences there are:
	// after a table is deleted its place on the graph is free again, and
	// counting would put the next table on top of a card that is still there.
	taken, err := occupiedPositions(ctx, tx)
	if err != nil {
		return nil, err
	}
	xPos, yPos := firstFreePosition(taken)

	insertOccSQL := `INSERT INTO sys_table_occurrences (id, base_table_id, name, x_pos, y_pos, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
	if dialect.Engine() == dbal.EngineMariaDB {
		insertOccSQL = `INSERT INTO sys_table_occurrences (id, base_table_id, name, x_pos, y_pos, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	}
	if _, err := tx.ExecContext(ctx, insertOccSQL, occID, tableID, name, xPos, yPos, now); err != nil {
		return nil, fmt.Errorf("failed creating default table occurrence: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &TableMetadata{
		ID:          tableID,
		Name:        name,
		DisplayName: displayName,
		Columns:     []ColumnMetadata{pkColumn},
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// AddColumn adds a new column to metadata and alters the physical table
func (s *Service) AddColumn(ctx context.Context, tableID string, col ColumnMetadata) (*ColumnMetadata, error) {
	name := dbal.NormalizeIdentifier(col.Name)
	if err := dbal.CheckIdentifier("column", name); err != nil {
		return nil, err
	}
	if err := ValidateFieldOptions(col.DefaultValue); err != nil {
		return nil, err
	}
	rules, err := validation.Parse(col.ValidationRules)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
	}

	// Retrieve target table name
	db := s.driver.DB()
	var tableName string
	qTable := `SELECT name FROM sys_tables WHERE id = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		qTable = `SELECT name FROM sys_tables WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, qTable, tableID).Scan(&tableName); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("table not found: %s", tableID)
		}
		return nil, err
	}

	col.ID = uuid.NewString()
	col.TableID = tableID
	col.Name = name
	col.CreatedAt = time.Now().UTC()

	// A calculation field's formula must parse, read only fields this table
	// has, and not take part in a ring of calculations (#30).
	if err := s.checkCalculation(ctx, tableID, col.ID, col.Name, col.FieldType, col.CalculationFormula); err != nil {
		return nil, err
	}
	// A summary field's definition must name a kind of summary and a field it
	// can be taken over (#32).
	if err := s.checkSummary(ctx, tableID, col.ID, col.Name, col.FieldType, col.CalculationFormula); err != nil {
		return nil, err
	}

	// 1. Build ALTER TABLE ADD COLUMN SQL. A calculation is stored as whatever
	// its formula produces, so a numeric result sorts as a number.
	dialect := s.driver.Dialect()
	alterSQL, err := dialect.BuildAddColumnSQL(tableName, dbal.ColumnDefinition{
		Name:         col.Name,
		Type:         StorageType(col.FieldType, col.CalculationFormula),
		IsNullable:   col.IsNullable,
		IsPrimaryKey: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed generating ALTER TABLE DDL: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Execute physical DDL
	if _, err := tx.ExecContext(ctx, alterSQL); err != nil {
		return nil, fmt.Errorf("failed executing DDL alter table: %w", err)
	}

	// Insert into sys_columns
	insertColSQL := `INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	if dialect.Engine() == dbal.EngineMariaDB {
		insertColSQL = `INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	}
	if _, err := tx.ExecContext(ctx, insertColSQL, col.ID, col.TableID, col.Name, col.DisplayName, string(col.FieldType), col.IsNullable, col.IsPrimaryKey, col.DefaultValue, col.CalculationFormula, col.ValidationRules, col.CreatedAt); err != nil {
		return nil, fmt.Errorf("failed registering column in sys_columns: %w", err)
	}
	if err := s.syncUniqueIndex(ctx, tx, tableName, col.Name, col.FieldType, rules); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &col, nil
}

// syncUniqueIndex creates or drops the database constraint that backs a
// field's "unique" validation rule, so two concurrent writes cannot both
// store the same value (#15). Null and blank values are not constrained: an
// empty field is only rejected by "not empty".
//
// PostgreSQL uses a partial unique index. MariaDB has none, and cannot index
// a LONGTEXT column without a key length, so it indexes a generated column
// holding the SHA-256 of the value, which is NULL when the value is empty
// (MariaDB allows repeated NULLs in a unique index).
func (s *Service) syncUniqueIndex(ctx context.Context, db interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}, table, field string, fieldType dbal.AgnosticFieldType, rules *validation.Rules) error {
	dialect := s.driver.Dialect()
	name := validation.UniqueIndexName(table, field)
	index := dialect.QuoteIdentifier(name)
	qTable, qField := dialect.QuoteIdentifier(table), dialect.QuoteIdentifier(field)
	wanted := rules != nil && rules.Unique

	// Text fields treat an empty value as no value; other types only skip NULL.
	blankIsEmpty := true
	switch fieldType {
	case dbal.FieldTypeNumber, dbal.FieldTypeDate, dbal.FieldTypeTimestamp, dbal.FieldTypeBoolean, dbal.FieldTypeContainer:
		blankIsEmpty = false
	}

	if dialect.Engine() == dbal.EngineMariaDB {
		if !wanted {
			// The index goes with its generated column; neither may exist yet.
			_, _ = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", qTable, index))
			_, _ = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qTable, index))
			return nil
		}
		value := fmt.Sprintf("CAST(%s AS CHAR)", qField)
		if blankIsEmpty {
			value = fmt.Sprintf("NULLIF(%s, '')", value)
		}
		q := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s CHAR(64) AS (SHA2(%s, 256)) VIRTUAL, ADD UNIQUE INDEX %s (%s)",
			qTable, index, value, index, index)
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%w: field %s cannot be unique: some records already share a value (%v)", ErrInvalidFieldOptions, field, err)
		}
		return nil
	}

	if !wanted {
		_, err := db.ExecContext(ctx, "DROP INDEX IF EXISTS "+index)
		return err
	}
	where := qField + " IS NOT NULL"
	if blankIsEmpty {
		where += " AND " + qField + " <> ''"
	}
	q := fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s) WHERE %s", index, qTable, qField, where)
	if _, err := db.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("%w: field %s cannot be unique: some records already share a value (%v)", ErrInvalidFieldOptions, field, err)
	}
	return nil
}

func (s *Service) syncUniqueIndexByColumnID(ctx context.Context, tableID, columnID string, rules *validation.Rules) error {
	q := `SELECT t.name, c.name, c.field_type FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE c.id = $1 AND t.id = $2`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT t.name, c.name, c.field_type FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE c.id = ? AND t.id = ?`
	}
	var table, field, fieldType string
	if err := s.driver.DB().QueryRowContext(ctx, q, columnID, tableID).Scan(&table, &field, &fieldType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("column not found: %s", columnID)
		}
		return err
	}
	return s.syncUniqueIndex(ctx, s.driver.DB(), table, field, dbal.AgnosticFieldType(fieldType), rules)
}

// DeleteColumn removes a column from metadata and drops it from the physical table
func (s *Service) DeleteColumn(ctx context.Context, tableID string, columnID string) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// 1. Get table name
	var tableName string
	qTable := `SELECT name FROM sys_tables WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qTable = `SELECT name FROM sys_tables WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, qTable, tableID).Scan(&tableName); err != nil {
		return fmt.Errorf("table not found: %w", err)
	}

	// 2. Get column name and check primary key
	var colName string
	var isPrimaryKey bool
	qCol := `SELECT name, is_primary_key FROM sys_columns WHERE id = $1 AND table_id = $2`
	if dialect.Engine() == dbal.EngineMariaDB {
		qCol = `SELECT name, is_primary_key FROM sys_columns WHERE id = ? AND table_id = ?`
	}
	if err := db.QueryRowContext(ctx, qCol, columnID, tableID).Scan(&colName, &isPrimaryKey); err != nil {
		return fmt.Errorf("column not found: %w", err)
	}

	if isPrimaryKey {
		return fmt.Errorf("cannot delete primary key column")
	}

	dropSQL, err := dialect.BuildDropColumnSQL(tableName, colName)
	if err != nil {
		return fmt.Errorf("failed generating drop column SQL: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("failed dropping column from physical table: %w", err)
	}

	deleteSQL := `DELETE FROM sys_columns WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		deleteSQL = `DELETE FROM sys_columns WHERE id = ?`
	}
	if _, err := tx.ExecContext(ctx, deleteSQL, columnID); err != nil {
		return fmt.Errorf("failed removing column from sys_columns: %w", err)
	}

	return tx.Commit()
}

// UpdateColumnOptions contains parameters for updating column metadata
type UpdateColumnOptions struct {
	DisplayName        string
	DefaultValue       *string
	UpdateDefaultValue bool
	CalculationFormula *string
	UpdateCalculation  bool
	ValidationRules    *string
	UpdateValidation   bool
}

// UpdateColumn updates a column's metadata (e.g. display name, options)
func (s *Service) UpdateColumn(ctx context.Context, tableID string, columnID string, opts UpdateColumnOptions) (*ColumnMetadata, error) {
	if opts.UpdateDefaultValue {
		if err := ValidateFieldOptions(opts.DefaultValue); err != nil {
			return nil, err
		}
	}
	if opts.UpdateValidation {
		rules, err := validation.Parse(opts.ValidationRules)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
		}
		if err := s.syncUniqueIndexByColumnID(ctx, tableID, columnID, rules); err != nil {
			return nil, err
		}
	}

	// A new formula must be valid before it replaces the old one, and if its
	// result type changed the stored column is re-typed to match (#30).
	var retype func(context.Context) error
	if opts.UpdateCalculation {
		var err error
		if retype, err = s.prepareCalculationChange(ctx, tableID, columnID, opts.CalculationFormula); err != nil {
			return nil, err
		}
		if err := s.prepareSummaryChange(ctx, tableID, columnID, opts.CalculationFormula); err != nil {
			return nil, err
		}
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	if dialect.Engine() == dbal.EngineMariaDB {
		sets := []string{"display_name = ?"}
		args := []interface{}{opts.DisplayName}
		if opts.UpdateDefaultValue {
			sets = append(sets, "default_value = ?")
			args = append(args, opts.DefaultValue)
		}
		if opts.UpdateCalculation {
			sets = append(sets, "calculation_formula = ?")
			args = append(args, opts.CalculationFormula)
		}
		if opts.UpdateValidation {
			sets = append(sets, "validation_rules = ?")
			args = append(args, opts.ValidationRules)
		}
		args = append(args, columnID, tableID)

		qUpdate := fmt.Sprintf("UPDATE sys_columns SET %s WHERE id = ? AND table_id = ?", strings.Join(sets, ", "))
		if _, err := db.ExecContext(ctx, qUpdate, args...); err != nil {
			return nil, err
		}
		var col ColumnMetadata
		qGet := `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at FROM sys_columns WHERE id = ?`
		if err := db.QueryRowContext(ctx, qGet, columnID).Scan(&col.ID, &col.TableID, &col.Name, &col.DisplayName, &col.FieldType, &col.IsNullable, &col.IsPrimaryKey, &col.DefaultValue, &col.CalculationFormula, &col.ValidationRules, &col.CreatedAt); err != nil {
			return nil, err
		}
		if retype != nil {
			if err := retype(ctx); err != nil {
				return nil, err
			}
		}
		return &col, nil
	}

	sets := []string{"display_name = $1"}
	args := []interface{}{opts.DisplayName}
	argIdx := 2
	if opts.UpdateDefaultValue {
		sets = append(sets, fmt.Sprintf("default_value = $%d", argIdx))
		args = append(args, opts.DefaultValue)
		argIdx++
	}
	if opts.UpdateCalculation {
		sets = append(sets, fmt.Sprintf("calculation_formula = $%d", argIdx))
		args = append(args, opts.CalculationFormula)
		argIdx++
	}
	if opts.UpdateValidation {
		sets = append(sets, fmt.Sprintf("validation_rules = $%d", argIdx))
		args = append(args, opts.ValidationRules)
		argIdx++
	}
	args = append(args, columnID, tableID)

	q := fmt.Sprintf(`UPDATE sys_columns SET %s WHERE id = $%d AND table_id = $%d RETURNING id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at`,
		strings.Join(sets, ", "), argIdx, argIdx+1)

	var col ColumnMetadata
	if err := db.QueryRowContext(ctx, q, args...).Scan(&col.ID, &col.TableID, &col.Name, &col.DisplayName, &col.FieldType, &col.IsNullable, &col.IsPrimaryKey, &col.DefaultValue, &col.CalculationFormula, &col.ValidationRules, &col.CreatedAt); err != nil {
		return nil, err
	}
	if retype != nil {
		if err := retype(ctx); err != nil {
			return nil, err
		}
	}
	return &col, nil
}

// ListTables retrieves all registered tables with their columns
func (s *Service) ListTables(ctx context.Context) ([]TableMetadata, error) {
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, name, display_name, COALESCE(description, ''), created_at, updated_at FROM sys_tables ORDER BY display_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make([]TableMetadata, 0)
	for rows.Next() {
		var t TableMetadata
		if err := rows.Scan(&t.ID, &t.Name, &t.DisplayName, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}

	// Fetch columns for each table
	for i := range tables {
		colQuery := `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at FROM sys_columns WHERE table_id = $1 ORDER BY created_at ASC`
		if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
			colQuery = `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at FROM sys_columns WHERE table_id = ? ORDER BY created_at ASC`
		}
		cRows, err := db.QueryContext(ctx, colQuery, tables[i].ID)
		if err != nil {
			return nil, err
		}

		cols := make([]ColumnMetadata, 0)
		for cRows.Next() {
			var c ColumnMetadata
			var fType string
			if err := cRows.Scan(&c.ID, &c.TableID, &c.Name, &c.DisplayName, &fType, &c.IsNullable, &c.IsPrimaryKey, &c.DefaultValue, &c.CalculationFormula, &c.ValidationRules, &c.CreatedAt); err != nil {
				cRows.Close()
				return nil, err
			}
			c.FieldType = dbal.AgnosticFieldType(fType)
			cols = append(cols, c)
		}
		cRows.Close()
		tables[i].Columns = cols
	}

	return tables, nil
}

// DeleteTable removes a table entirely: drops the physical table and deletes sys_* metadata.
func (s *Service) DeleteTable(ctx context.Context, tableID string) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// 1. Look up the physical table name
	var tableName string
	qTable := `SELECT name FROM sys_tables WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qTable = `SELECT name FROM sys_tables WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, qTable, tableID).Scan(&tableName); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("table not found: %s", tableID)
		}
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 2. Drop physical table
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", dialect.QuoteIdentifier(tableName))); err != nil {
		return fmt.Errorf("failed dropping physical table %s: %w", tableName, err)
	}

	// 3. Delete from sys_tables (cascades to sys_columns and sys_table_occurrences via FK)
	delSQL := `DELETE FROM sys_tables WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		delSQL = `DELETE FROM sys_tables WHERE id = ?`
	}
	if _, err := tx.ExecContext(ctx, delSQL, tableID); err != nil {
		return fmt.Errorf("failed deleting table metadata: %w", err)
	}

	return tx.Commit()
}

// RenameTable updates the display_name of a table in the system catalog.
func (s *Service) RenameTable(ctx context.Context, tableID string, newDisplayName string) (*TableMetadata, error) {
	newDisplayName = strings.TrimSpace(newDisplayName)
	if newDisplayName == "" {
		return nil, fmt.Errorf("display_name cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	now := time.Now().UTC()

	updSQL := `UPDATE sys_tables SET display_name = $1, updated_at = $2 WHERE id = $3`
	if dialect.Engine() == dbal.EngineMariaDB {
		updSQL = `UPDATE sys_tables SET display_name = ?, updated_at = ? WHERE id = ?`
	}
	res, err := db.ExecContext(ctx, updSQL, newDisplayName, now, tableID)
	if err != nil {
		return nil, fmt.Errorf("failed updating table display_name: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil, fmt.Errorf("table not found: %s", tableID)
	}

	// Return updated record
	var t TableMetadata
	selSQL := `SELECT id, name, display_name, COALESCE(description,''), created_at, updated_at FROM sys_tables WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		selSQL = `SELECT id, name, display_name, COALESCE(description,''), created_at, updated_at FROM sys_tables WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, selSQL, tableID).Scan(&t.ID, &t.Name, &t.DisplayName, &t.Description, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// DuplicateTable creates a new table with the same structure as an existing table.
func (s *Service) DuplicateTable(ctx context.Context, tableID string) (*TableMetadata, error) {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// 1. Fetch original table metadata
	var origName, origDisplayName string
	qTable := `SELECT name, display_name FROM sys_tables WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qTable = `SELECT name, display_name FROM sys_tables WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, qTable, tableID).Scan(&origName, &origDisplayName); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("table not found: %s", tableID)
		}
		return nil, err
	}

	// 2. Fetch columns of the original table (excluding PK)
	colQuery := `SELECT name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules FROM sys_columns WHERE table_id = $1 ORDER BY created_at ASC`
	if dialect.Engine() == dbal.EngineMariaDB {
		colQuery = `SELECT name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules FROM sys_columns WHERE table_id = ? ORDER BY created_at ASC`
	}
	cRows, err := db.QueryContext(ctx, colQuery, tableID)
	if err != nil {
		return nil, err
	}
	defer cRows.Close()

	type srcCol struct {
		name, displayName, fieldType               string
		isNullable, isPrimaryKey                   bool
		defaultValue, calcFormula, validationRules *string
	}
	var cols []srcCol
	for cRows.Next() {
		var c srcCol
		var fType string
		if err := cRows.Scan(&c.name, &c.displayName, &fType, &c.isNullable, &c.isPrimaryKey, &c.defaultValue, &c.calcFormula, &c.validationRules); err != nil {
			return nil, err
		}
		c.fieldType = fType
		cols = append(cols, c)
	}
	cRows.Close()

	// 3. Generate a unique new name
	newName := copyName(origName, "_copy")
	newDisplayName := origDisplayName + " (Copy)"

	// Create the duplicate table using CreateTable (which adds the PK column)
	newTbl, err := s.CreateTable(ctx, newDisplayName, newName)
	if err != nil {
		// If name conflict, append timestamp
		newName = copyName(origName, fmt.Sprintf("_copy_%d", time.Now().UnixMilli()))
		newDisplayName = fmt.Sprintf("%s (Copy %d)", origDisplayName, time.Now().UnixMilli())
		newTbl, err = s.CreateTable(ctx, newDisplayName, newName)
		if err != nil {
			return nil, fmt.Errorf("failed creating duplicate table: %w", err)
		}
	}

	// 4. Add non-PK columns to the duplicate. A failure removes the copy, so
	// no incomplete table is left behind.
	for _, c := range cols {
		if c.isPrimaryKey {
			continue
		}
		_, err := s.AddColumn(ctx, newTbl.ID, ColumnMetadata{
			Name:               c.name,
			DisplayName:        c.displayName,
			FieldType:          dbal.AgnosticFieldType(c.fieldType),
			IsNullable:         c.isNullable,
			DefaultValue:       c.defaultValue,
			CalculationFormula: c.calcFormula,
			ValidationRules:    c.validationRules,
		})
		if err != nil {
			if delErr := s.DeleteTable(ctx, newTbl.ID); delErr != nil {
				return nil, fmt.Errorf("failed copying column %s: %w (and removing the incomplete copy failed: %v)", c.name, err, delErr)
			}
			return nil, fmt.Errorf("failed copying column %s: %w", c.name, err)
		}
	}

	return newTbl, nil
}

// TruncateTable deletes all data rows from the physical table.
func (s *Service) TruncateTable(ctx context.Context, tableID string) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	var tableName string
	qTable := `SELECT name FROM sys_tables WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qTable = `SELECT name FROM sys_tables WHERE id = ?`
	}
	if err := db.QueryRowContext(ctx, qTable, tableID).Scan(&tableName); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("table not found: %s", tableID)
		}
		return err
	}

	// Use DELETE instead of TRUNCATE to avoid DDL-in-transaction issues on some drivers
	if _, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", dialect.QuoteIdentifier(tableName))); err != nil {
		return fmt.Errorf("failed truncating table %s: %w", tableName, err)
	}
	return nil
}
