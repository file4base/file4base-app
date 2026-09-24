package schema

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var validIdentifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

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
	ID                string `json:"id"`
	Name              string `json:"name"`
	LeftOccurrenceID  string `json:"left_occurrence_id"`
	LeftColumnID      string `json:"left_column_id"`
	RightOccurrenceID string `json:"right_occurrence_id"`
	RightColumnID     string `json:"right_column_id"`
	Operator          string `json:"operator"`
	AllowCreation     bool   `json:"allow_creation"`
	CascadeDelete     bool   `json:"cascade_delete"`
	SortRelated       string `json:"sort_related,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// Service manages schema metadata and executes dynamic DDL
type Service struct {
	driver dbal.DatabaseDriver
}

func NewService(driver dbal.DatabaseDriver) *Service {
	return &Service{driver: driver}
}

// EnsureSystemTables creates sys_* catalog if not present with default credentials
func (s *Service) EnsureSystemTables(ctx context.Context) error {
	return s.EnsureSystemTablesWithCredentials(ctx, "", "")
}

// EnsureSystemTablesWithCredentials provisions the system tables and the initial owner user matching the database name and password
func (s *Service) EnsureSystemTablesWithCredentials(ctx context.Context, initialOwnerUser, initialOwnerPassword string) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sys_tables (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			display_name VARCHAR(128) NOT NULL,
			description TEXT,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
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
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_table_column UNIQUE (table_id, name)
		);`,
		`CREATE TABLE IF NOT EXISTS sys_table_occurrences (
			id VARCHAR(36) PRIMARY KEY,
			base_table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
			name VARCHAR(64) NOT NULL UNIQUE,
			x_pos DOUBLE PRECISION DEFAULT 100,
			y_pos DOUBLE PRECISION DEFAULT 100,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
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
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS sys_layouts (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			table_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
			definition TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS sys_users (
			id VARCHAR(36) PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(256) NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'user',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS sys_user_permissions (
			id VARCHAR(36) PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
			layout_id VARCHAR(36) NOT NULL REFERENCES sys_layouts(id) ON DELETE CASCADE,
			access_level VARCHAR(16) NOT NULL DEFAULT 'read_write',
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_user_layout UNIQUE (user_id, layout_id)
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

	if strings.TrimSpace(initialOwnerUser) != "" {
		pass := initialOwnerPassword
		if pass == "" {
			pass = "admin"
		}
		provisionUser(initialOwnerUser, pass, "owner")
	} else {
		// Provision standard administrative owner accounts
		provisionUser("admin", "admin", "owner")
		provisionUser("file4base", "dev_password", "owner")
		provisionUser("file4base_dev", "file4base_dev", "owner")
	}

	return nil
}

// CreateTable registers metadata and dynamically generates the physical table via DBAL
func (s *Service) CreateTable(ctx context.Context, displayName, customName string) (*TableMetadata, error) {
	name := strings.ToLower(strings.TrimSpace(customName))
	if name == "" {
		name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(displayName), " ", "_"))
	}

	if !validIdentifier.MatchString(name) {
		return nil, fmt.Errorf("invalid table name '%s'; must start with a letter and contain only alphanumeric/underscore characters", name)
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

	// Insert default Table Occurrence
	insertOccSQL := `INSERT INTO sys_table_occurrences (id, base_table_id, name, created_at) VALUES ($1, $2, $3, $4)`
	if dialect.Engine() == dbal.EngineMariaDB {
		insertOccSQL = `INSERT INTO sys_table_occurrences (id, base_table_id, name, created_at) VALUES (?, ?, ?, ?)`
	}
	if _, err := tx.ExecContext(ctx, insertOccSQL, occID, tableID, name, now); err != nil {
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
	name := strings.ToLower(strings.TrimSpace(col.Name))
	if !validIdentifier.MatchString(name) {
		return nil, fmt.Errorf("invalid column name '%s'", name)
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

	// 1. Build ALTER TABLE ADD COLUMN SQL
	dialect := s.driver.Dialect()
	alterSQL, err := dialect.BuildAddColumnSQL(tableName, dbal.ColumnDefinition{
		Name:         col.Name,
		Type:         col.FieldType,
		IsNullable:   col.IsNullable,
		IsPrimaryKey: false,
		DefaultValue: col.DefaultValue,
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

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &col, nil
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
