package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
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
// auto-enter and storage options as a JSON object (see the Fields dialog and
// docs/specs/auto_enter_options.md), or nothing. It is metadata only: it is
// never placed in SQL.
//
// It checks the shape only, so a solution bundle saved before an option was
// checked more strictly still imports; the server skips an option it cannot
// apply. A field saved through the Fields dialog is checked with
// ValidateFieldOptionsFor.
func ValidateFieldOptions(options *string) error {
	if options == nil || strings.TrimSpace(*options) == "" {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(*options), &obj); err != nil || obj == nil {
		return fmt.Errorf("%w: default_value must be a JSON object of field options", ErrInvalidFieldOptions)
	}
	if _, err := autoenter.Parse(options); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
	}
	return nil
}

// ValidateFieldOptionsFor is ValidateFieldOptions for a field of fieldType,
// whose auto-enter options must also be ones such a field can hold. Its
// auto-enter calculation may call the custom functions of functions.
func ValidateFieldOptionsFor(fieldType string, options *string, functions *calc.Library) error {
	if err := ValidateFieldOptions(options); err != nil {
		return err
	}
	if err := autoenter.ValidateWith(fieldType, options, functions); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
	}
	return nil
}

// autoEnterLibrary is the custom functions an auto-enter calculation in
// options may call; options without one need none, and are read without
// reading them.
func (s *Service) autoEnterLibrary(ctx context.Context, options *string) (*calc.Library, error) {
	if o, err := autoenter.Parse(options); err != nil || !o.CalculatedEnabled {
		return nil, nil
	}
	return s.library(ctx)
}

// writeFieldOptions stores a column's options. Options without serial_next
// keep the next serial value the column has now, which records may have
// moved on since the options were read: only options that name it set it
// (see docs/specs/auto_enter_options.md). The row is locked while it is
// read and written, as a serial number is allocated under the same lock.
func (s *Service) writeFieldOptions(ctx context.Context, tableID, columnID string, options *string) error {
	dialect := s.driver.Dialect()
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lock := " FOR UPDATE"
	if dialect.Engine() == dbal.EnginePostgres {
		lock = " FOR NO KEY UPDATE"
	}
	var current sql.NullString
	q := fmt.Sprintf("SELECT default_value FROM sys_columns WHERE id = %s AND table_id = %s%s",
		dialect.Placeholder(1), dialect.Placeholder(2), lock)
	if err := tx.QueryRowContext(ctx, q, columnID, tableID).Scan(&current); err != nil {
		return err
	}
	value := options
	if options != nil && current.Valid && !autoenter.HasKey(options, "serial_next") && autoenter.HasKey(&current.String, "serial_next") {
		if live, err := autoenter.Parse(&current.String); err == nil {
			merged, err := autoenter.WithSerialNext(options, live.SerialNext)
			if err != nil {
				return err
			}
			value = &merged
		}
	}
	q = fmt.Sprintf("UPDATE sys_columns SET default_value = %s WHERE id = %s AND table_id = %s",
		dialect.Placeholder(1), dialect.Placeholder(2), dialect.Placeholder(3))
	if _, err := tx.ExecContext(ctx, q, value, columnID, tableID); err != nil {
		return err
	}
	return tx.Commit()
}

// columnFieldType is the declared type of a column, "" when there is none.
func (s *Service) columnFieldType(ctx context.Context, tableID, columnID string) (string, error) {
	dialect := s.driver.Dialect()
	q := fmt.Sprintf("SELECT field_type FROM sys_columns WHERE id = %s AND table_id = %s",
		dialect.Placeholder(1), dialect.Placeholder(2))
	var fieldType string
	err := s.driver.DB().QueryRowContext(ctx, q, columnID, tableID).Scan(&fieldType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return fieldType, err
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
	// MaxRepetitions is how many values the field holds, as a FileMaker
	// repeating field does: 1 for a field that does not repeat (see the
	// repetition package for how the others are stored).
	MaxRepetitions int       `json:"max_repetitions"`
	CreatedAt      time.Time `json:"created_at"`
	// SourceFieldID is the id FileMaker gave the field, kept by the
	// migration, so another solution's occurrences that name the field only
	// by id find it (#107). Empty for a field not migrated from FileMaker.
	SourceFieldID string `json:"source_field_id,omitempty"`
}

// RelationshipMetadata represents an occurrence join in sys_relationships.
//
// Predicates is every match criterion, in order, and a record is related when
// it meets all of them. The first one is also given as LeftColumnID, Operator
// and RightColumnID, which is all a relationship had before it could have
// several.
//
// Each side has its own options, as in FileMaker's Edit Relationship dialog,
// and each acts on the side it is set on. AllowCreation, CascadeDelete and
// SortRelated are the right side's, which is all a relationship had before
// the left side's could be kept: LeftAllowCreation, LeftCascadeDelete and
// LeftSortRelated.
type RelationshipMetadata struct {
	ID                string                  `json:"id"`
	Name              string                  `json:"name"`
	LeftOccurrenceID  string                  `json:"left_occurrence_id"`
	LeftColumnID      string                  `json:"left_column_id"`
	RightOccurrenceID string                  `json:"right_occurrence_id"`
	RightColumnID     string                  `json:"right_column_id"`
	Operator          string                  `json:"operator"`
	Predicates        []RelationshipPredicate `json:"predicates"`
	AllowCreation     bool                    `json:"allow_creation"`
	CascadeDelete     bool                    `json:"cascade_delete"`
	SortRelated       string                  `json:"sort_related,omitempty"`
	LeftAllowCreation bool                    `json:"left_allow_creation"`
	LeftCascadeDelete bool                    `json:"left_cascade_delete"`
	LeftSortRelated   string                  `json:"left_sort_related,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
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
	// The same type with no default, for a time that may not have happened
	// yet, such as a schedule's next or last run.
	tsNullable := strings.TrimSpace(strings.SplitN(ts, " DEFAULT", 2)[0])
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
			max_repetitions INT NOT NULL DEFAULT 1,
			created_at ` + ts + `,
			source_field_id VARCHAR(32),
			CONSTRAINT uq_table_column UNIQUE (table_id, name)
		);`,
		// Repeating fields (see migrations/003_repeating_fields.sql).
		`ALTER TABLE sys_columns ADD COLUMN IF NOT EXISTS max_repetitions INT NOT NULL DEFAULT 1;`,
		// FileMaker field ids (see migrations/005_external_occurrences.sql).
		`ALTER TABLE sys_columns ADD COLUMN IF NOT EXISTS source_field_id VARCHAR(32);`,
		`CREATE TABLE IF NOT EXISTS sys_table_occurrences (
			id VARCHAR(36) PRIMARY KEY,
			base_table_id VARCHAR(36) REFERENCES sys_tables(id) ON DELETE CASCADE,
			name VARCHAR(64) NOT NULL UNIQUE,
			x_pos DOUBLE PRECISION DEFAULT 100,
			y_pos DOUBLE PRECISION DEFAULT 100,
			created_at ` + ts + `,
			file_reference_id VARCHAR(36),
			external_table VARCHAR(64)
		);`,
		`CREATE TABLE IF NOT EXISTS sys_relationships (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			left_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
			left_column_id VARCHAR(36) REFERENCES sys_columns(id) ON DELETE CASCADE,
			right_occurrence_id VARCHAR(36) NOT NULL REFERENCES sys_table_occurrences(id) ON DELETE CASCADE,
			right_column_id VARCHAR(36) REFERENCES sys_columns(id) ON DELETE CASCADE,
			operator VARCHAR(8) DEFAULT '=',
			allow_creation BOOLEAN DEFAULT FALSE,
			cascade_delete BOOLEAN DEFAULT FALSE,
			sort_related TEXT,
			left_column_name VARCHAR(64),
			right_column_name VARCHAR(64),
			created_at ` + ts + `,
			left_allow_creation BOOLEAN NOT NULL DEFAULT FALSE,
			left_cascade_delete BOOLEAN NOT NULL DEFAULT FALSE,
			left_sort_related TEXT
		);`,
		// The match criteria of a relationship after its first.
		relationshipPredicatesSQL(ts),
		// Deleted FileMaker fields (see migrations/011_deleted_source_fields.sql).
		deletedSourceFieldsSQL,
		relationshipColumnNamesSQL[0],
		relationshipColumnNamesSQL[1],
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
			updated_at ` + ts + `,
			source_column_name VARCHAR(64),
			second_column_name VARCHAR(64)
		);`,
		`CREATE TABLE IF NOT EXISTS sys_users (
			id VARCHAR(36) PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(256) NOT NULL,
			role VARCHAR(32) NOT NULL DEFAULT 'user',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			mfa_secret VARCHAR(128) NOT NULL DEFAULT '',
			super_owner BOOLEAN NOT NULL DEFAULT FALSE,
			password_pending BOOLEAN NOT NULL DEFAULT FALSE,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		`ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE;`,
		`ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN DEFAULT FALSE;`,
		`ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS mfa_secret VARCHAR(128) DEFAULT '';`,
		`ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS super_owner BOOLEAN DEFAULT FALSE;`,
		// An account a solution import or a FileMaker migration created
		// without a usable password (#198): it cannot sign in until an
		// administrator sets one, which clears the flag.
		`ALTER TABLE sys_users ADD COLUMN IF NOT EXISTS password_pending BOOLEAN DEFAULT FALSE;`,
		`CREATE TABLE IF NOT EXISTS sys_user_permissions (
			id VARCHAR(36) PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
			layout_id VARCHAR(36) NOT NULL REFERENCES sys_layouts(id) ON DELETE CASCADE,
			access_level VARCHAR(16) NOT NULL DEFAULT 'read_write',
			created_at ` + ts + `,
			CONSTRAINT uq_user_layout UNIQUE (user_id, layout_id)
		);`,
		`CREATE TABLE IF NOT EXISTS sys_table_permissions (
			id VARCHAR(36) PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
			table_name VARCHAR(64) NOT NULL,
			access_level VARCHAR(16) NOT NULL DEFAULT 'read_write',
			can_create BOOLEAN NOT NULL DEFAULT TRUE,
			can_edit BOOLEAN NOT NULL DEFAULT TRUE,
			can_delete BOOLEAN NOT NULL DEFAULT TRUE,
			can_modify_schema BOOLEAN NOT NULL DEFAULT FALSE,
			created_at ` + ts + `,
			CONSTRAINT uq_user_table UNIQUE (user_id, table_name)
		);`,
		// Menu sets a solution installs over the standard menu bar (#52).
		// A set is a convenience, never a boundary: what an account may do
		// is the role, the table and layout access and the privileges, and
		// the server enforces those whatever the bar shows.
		`CREATE TABLE IF NOT EXISTS sys_menu_sets (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL UNIQUE,
			definition TEXT NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`,
		// Solution-wide metadata and irreversible design lock (#53).
		`CREATE TABLE IF NOT EXISTS sys_solution_meta (
			id VARCHAR(36) PRIMARY KEY,
			solution_name VARCHAR(128) NOT NULL DEFAULT '',
			design_locked BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at ` + ts + `
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
		// A named record transfer between two solutions (#96). It holds the
		// mappings, the match keys and the policies, and no credentials
		// whatever: which accounts may run it is decided when it runs, by
		// the session tokens presented for each end.
		`CREATE TABLE IF NOT EXISTS sys_transfers (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			source_database VARCHAR(128),
			source_table VARCHAR(64) NOT NULL,
			destination_database VARCHAR(128),
			destination_table VARCHAR(64) NOT NULL,
			definition TEXT NOT NULL,
			created_by VARCHAR(36),
			created_at ` + ts + `,
			updated_at ` + ts + `,
			CONSTRAINT uq_transfer_name UNIQUE (name)
		);`,
		// A named reference to another solution that this solution's
		// scripts reach (#107): a Perform Script step names the reference,
		// and the reference names the solution. An empty database_name is a
		// reference that is not mapped yet, which a step refuses to use.
		`CREATE TABLE IF NOT EXISTS sys_file_references (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			database_name VARCHAR(128),
			created_at ` + ts + `,
			updated_at ` + ts + `,
			paths TEXT,
			CONSTRAINT uq_file_reference_name UNIQUE (name)
		);`,
		// A transfer run on a cron schedule with nobody signed in (#108).
		// It names an account in each solution to run as and stores no
		// password or token; the accounts are checked again on every run.
		// The two solutions it was authorized for are kept, so a definition
		// changed to point elsewhere cannot inherit the authorization.
		`CREATE TABLE IF NOT EXISTS sys_transfer_schedules (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			transfer_id VARCHAR(36) NOT NULL REFERENCES sys_transfers(id) ON DELETE CASCADE,
			cron_expr VARCHAR(128) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			source_account VARCHAR(64) NOT NULL,
			destination_account VARCHAR(64) NOT NULL,
			source_database VARCHAR(128),
			destination_database VARCHAR(128),
			created_by VARCHAR(36),
			created_at ` + ts + `,
			updated_at ` + ts + `,
			next_run_at ` + tsNullable + `,
			last_run_at ` + tsNullable + `,
			last_status VARCHAR(16),
			last_error TEXT,
			CONSTRAINT uq_transfer_schedule_name UNIQUE (name)
		);`,
		`CREATE TABLE IF NOT EXISTS sys_transfer_schedule_runs (
			id VARCHAR(36) PRIMARY KEY,
			schedule_id VARCHAR(36) NOT NULL REFERENCES sys_transfer_schedules(id) ON DELETE CASCADE,
			trigger_kind VARCHAR(16) NOT NULL,
			started_at ` + tsNullable + ` NOT NULL,
			finished_at ` + tsNullable + ` NOT NULL,
			status VARCHAR(16) NOT NULL,
			error TEXT,
			report TEXT
		);`,
		// Capabilities granted per account role (#39). A role without a
		// row holds every bulk capability, so a database created before
		// this table existed keeps working unrestricted; the Data API
		// (fmrest) is held only once granted.
		`CREATE TABLE IF NOT EXISTS sys_privileges (
			role VARCHAR(32) PRIMARY KEY,
			bulk_export BOOLEAN NOT NULL DEFAULT TRUE,
			bulk_import BOOLEAN NOT NULL DEFAULT TRUE,
			fmrest BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at ` + ts + `
		);`,
		// Data API access (see migrations/009_privilege_fmrest.sql).
		`ALTER TABLE sys_privileges ADD COLUMN IF NOT EXISTS fmrest BOOLEAN NOT NULL DEFAULT FALSE;`,
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
		// The original text of values a typed field cannot hold as they were
		// written, such as "12-A" in a migrated FileMaker Number field (#124).
		sourcetext.CreateTableSQL,
		// Original filename, MIME type and SHA-256 metadata for migrated
		// container fields (#125).
		containermeta.CreateTableSQL,
		// The solution's custom functions (see
		// migrations/007_custom_functions.sql).
		customFunctionsSQL(ts),
	}
	// The options of a value list taken from a field (#31) are added to
	// sys_value_lists rather than declared in it, so a new database and an
	// older one end up with the same columns and foreign keys.
	queries = append(queries, valueListOptionsSQL(s.driver.Dialect().Engine())...)
	queries = append(queries, customFunctionNameIndexSQL(s.driver.Dialect().Engine())...)
	// MariaDB's record ID allocators (see dbal.RecordIDsTable).
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		queries = append(queries, dbal.RecordIDsTableSQL)
	}

	db := s.driver.DB()
	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed creating system tables: %w", err)
		}
	}
	// A catalog created before external table occurrences existed is
	// brought to their shape, every time: a restored backup may be older.
	if err := s.ensureExternalOccurrenceColumns(ctx); err != nil {
		return err
	}
	upgradedCatalogs.Store(db, true)
	// Every user table numbers its records in creation order and counts
	// their modifications.
	if err := s.ensureRecordIdentity(ctx); err != nil {
		return err
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
		Name:          name,
		CreationOrder: true,
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
	return s.addColumn(ctx, tableID, col, false)
}

// addColumn is AddColumn. A solution import adds a table's calculations in
// the order the file declares them, before the fields of later tables and the
// relationships they read through, so it passes deferCheck and checks each
// formula once everything is in place (checkCalculation).
func (s *Service) addColumn(ctx context.Context, tableID string, col ColumnMetadata, deferCheck bool) (*ColumnMetadata, error) {
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}
	name := dbal.NormalizeIdentifier(col.Name)
	if err := dbal.CheckIdentifier("column", name); err != nil {
		return nil, err
	}
	functions, err := s.autoEnterLibrary(ctx, col.DefaultValue)
	if err != nil {
		return nil, err
	}
	if err := ValidateFieldOptionsFor(string(col.FieldType), col.DefaultValue, functions); err != nil {
		return nil, err
	}
	rules, err := validation.Parse(col.ValidationRules)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
	}
	if err := s.checkValidationFormula(ctx, name, rules); err != nil {
		return nil, err
	}
	if col.MaxRepetitions == 0 {
		col.MaxRepetitions = 1
	}
	if err := checkRepetitions(col.FieldType, col.MaxRepetitions); err != nil {
		return nil, err
	}
	if col.MaxRepetitions > 1 && col.IsPrimaryKey {
		return nil, fmt.Errorf("%w: the primary key cannot have repetitions", ErrInvalidFieldOptions)
	}
	col.SourceFieldID = strings.TrimSpace(col.SourceFieldID)
	if !ValidSourceFieldID(col.SourceFieldID) {
		return nil, fmt.Errorf("%w: source_field_id is a FileMaker field id, digits only", ErrInvalidFieldOptions)
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
	if !deferCheck {
		if err := s.checkCalculation(ctx, tableID, col.ID, col.Name, col.FieldType, col.CalculationFormula); err != nil {
			return nil, err
		}
	}
	// A new column is in no relationship yet: only its being required matters.
	newCol := col
	newCol.ID = ""
	if err := s.checkUnstorable(ctx, newCol, col.CalculationFormula); err != nil {
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

	if col.MaxRepetitions > 1 {
		if _, err := tx.ExecContext(ctx, s.addRepetitionsColumnSQL(tableName, col.Name)); err != nil {
			return nil, fmt.Errorf("failed adding the repetitions of %s: %w", col.Name, err)
		}
	}

	// Insert into sys_columns
	insertColSQL := `INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at, max_repetitions, source_field_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	if dialect.Engine() == dbal.EngineMariaDB {
		insertColSQL = `INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at, max_repetitions, source_field_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	}
	if _, err := tx.ExecContext(ctx, insertColSQL, col.ID, col.TableID, col.Name, col.DisplayName, string(col.FieldType), col.IsNullable, col.IsPrimaryKey, col.DefaultValue, col.CalculationFormula, col.ValidationRules, col.CreatedAt, col.MaxRepetitions, nullable(col.SourceFieldID)); err != nil {
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
	// A rule the person may override during data entry is checked on each
	// write only: an index would refuse the value they chose to keep.
	wanted := rules != nil && rules.Unique && !rules.AllowOverride

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
	var colName, sourceFieldID string
	var isPrimaryKey bool
	qCol := `SELECT name, is_primary_key, COALESCE(source_field_id, '') FROM sys_columns WHERE id = $1 AND table_id = $2`
	if dialect.Engine() == dbal.EngineMariaDB {
		qCol = `SELECT name, is_primary_key, COALESCE(source_field_id, '') FROM sys_columns WHERE id = ? AND table_id = ?`
	}
	if err := db.QueryRowContext(ctx, qCol, columnID, tableID).Scan(&colName, &isPrimaryKey, &sourceFieldID); err != nil {
		return fmt.Errorf("column not found: %w", err)
	}

	if isPrimaryKey {
		return fmt.Errorf("cannot delete primary key column")
	}

	dropSQL, err := dialect.BuildDropColumnSQL(tableName, colName)
	if err != nil {
		return fmt.Errorf("failed generating drop column SQL: %w", err)
	}
	// The field's original texts go with it (#124). The table they live in
	// must exist before the transaction: a failed statement would abort it.
	if err := sourcetext.Ensure(ctx, db); err != nil {
		return err
	}
	if err := containermeta.Ensure(ctx, db); err != nil {
		return err
	}
	if err := s.ensureRelationshipPredicates(ctx); err != nil {
		return err
	}
	if sourceFieldID != "" {
		if err := s.ensureDeletedSourceFields(ctx); err != nil {
			return err
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// A migrated field keeps its place among its table's FileMaker fields,
	// which Import Records steps read by position (#191).
	if err := s.recordDeletedSourceField(ctx, tx, tableID, sourceFieldID); err != nil {
		return err
	}

	// A relationship matching on the field goes with it, whichever of its
	// criteria names the field: dropping only that criterion would leave a
	// relationship that relates, and cascades deletes to, more records than
	// it was defined to.
	relIDs, err := s.relationshipsUsingColumn(ctx, tx, columnID)
	if err != nil {
		return fmt.Errorf("failed reading the relationships matching on the field: %w", err)
	}
	for _, relID := range relIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sys_relationships WHERE id = `+dialect.Placeholder(1), relID); err != nil {
			return fmt.Errorf("failed removing a relationship matching on the field: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("failed dropping column from physical table: %w", err)
	}
	// The repetitions go with the field. The column exists only for a field
	// that repeats, or did once, hence IF EXISTS.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s",
		dialect.QuoteIdentifier(tableName), dialect.QuoteIdentifier(repetition.Column(colName)))); err != nil {
		return fmt.Errorf("failed dropping the repetitions of %s: %w", colName, err)
	}
	if err := sourcetext.DeleteField(ctx, tx, dialect, tableName, colName); err != nil {
		return err
	}
	if err := containermeta.DeleteField(ctx, tx, dialect, tableName, colName); err != nil {
		return err
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
	// MaxRepetitions changes how many repetitions the field has. Fewer keeps
	// the values of the repetitions beyond the new maximum, hidden, as
	// FileMaker does: raising it again shows them.
	MaxRepetitions    int
	UpdateRepetitions bool
}

// UpdateColumn updates a column's metadata (e.g. display name, options)
func (s *Service) UpdateColumn(ctx context.Context, tableID string, columnID string, opts UpdateColumnOptions) (*ColumnMetadata, error) {
	if opts.UpdateDefaultValue {
		fieldType, err := s.columnFieldType(ctx, tableID, columnID)
		if err != nil {
			return nil, err
		}
		functions, err := s.autoEnterLibrary(ctx, opts.DefaultValue)
		if err != nil {
			return nil, err
		}
		if err := ValidateFieldOptionsFor(fieldType, opts.DefaultValue, functions); err != nil {
			return nil, err
		}
	}
	if opts.UpdateValidation {
		rules, err := validation.Parse(opts.ValidationRules)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidFieldOptions, err)
		}
		if err := s.checkValidationFormula(ctx, opts.DisplayName, rules); err != nil {
			return nil, err
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

	if opts.UpdateRepetitions {
		if err := s.prepareRepetitionsChange(ctx, tableID, columnID, opts.MaxRepetitions); err != nil {
			return nil, err
		}
	}

	if opts.UpdateDefaultValue {
		if err := s.writeFieldOptions(ctx, tableID, columnID, opts.DefaultValue); err != nil {
			return nil, err
		}
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	if dialect.Engine() == dbal.EngineMariaDB {
		sets := []string{"display_name = ?"}
		args := []interface{}{opts.DisplayName}
		if opts.UpdateRepetitions {
			sets = append(sets, "max_repetitions = ?")
			args = append(args, opts.MaxRepetitions)
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
		qGet := `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at, max_repetitions FROM sys_columns WHERE id = ?`
		if err := db.QueryRowContext(ctx, qGet, columnID).Scan(&col.ID, &col.TableID, &col.Name, &col.DisplayName, &col.FieldType, &col.IsNullable, &col.IsPrimaryKey, &col.DefaultValue, &col.CalculationFormula, &col.ValidationRules, &col.CreatedAt, &col.MaxRepetitions); err != nil {
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
	if opts.UpdateRepetitions {
		sets = append(sets, fmt.Sprintf("max_repetitions = $%d", argIdx))
		args = append(args, opts.MaxRepetitions)
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

	q := fmt.Sprintf(`UPDATE sys_columns SET %s WHERE id = $%d AND table_id = $%d RETURNING id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at, max_repetitions`,
		strings.Join(sets, ", "), argIdx, argIdx+1)

	var col ColumnMetadata
	if err := db.QueryRowContext(ctx, q, args...).Scan(&col.ID, &col.TableID, &col.Name, &col.DisplayName, &col.FieldType, &col.IsNullable, &col.IsPrimaryKey, &col.DefaultValue, &col.CalculationFormula, &col.ValidationRules, &col.CreatedAt, &col.MaxRepetitions); err != nil {
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
	// Columns are read with their source_field_id (#107).
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}
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

	for i := range tables {
		cols, err := s.tableColumns(ctx, tables[i].ID)
		if err != nil {
			return nil, err
		}
		tables[i].Columns = cols
	}

	return tables, nil
}

// TableByName reads one registered table with its columns, by its exact
// name. found is false when the catalog has no such table. Unlike
// ListTables it reads the columns of that table only.
func (s *Service) TableByName(ctx context.Context, name string) (table *TableMetadata, found bool, err error) {
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, false, err
	}
	query := `SELECT id, name, display_name, COALESCE(description, ''), created_at, updated_at FROM sys_tables WHERE name = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		query = `SELECT id, name, display_name, COALESCE(description, ''), created_at, updated_at FROM sys_tables WHERE name = ?`
	}
	var t TableMetadata
	err = s.driver.DB().QueryRowContext(ctx, query, name).Scan(&t.ID, &t.Name, &t.DisplayName, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// MariaDB compares names without case: the table is the one named so.
	if t.Name != name {
		return nil, false, nil
	}
	if t.Columns, err = s.tableColumns(ctx, t.ID); err != nil {
		return nil, false, err
	}
	return &t, true, nil
}

// tableColumns reads the columns of a registered table, with their
// source_field_id (#107), in the order they were created.
func (s *Service) tableColumns(ctx context.Context, tableID string) ([]ColumnMetadata, error) {
	colQuery := `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at, max_repetitions, COALESCE(source_field_id, '') FROM sys_columns WHERE table_id = $1 ORDER BY created_at ASC`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		colQuery = `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at, max_repetitions, COALESCE(source_field_id, '') FROM sys_columns WHERE table_id = ? ORDER BY created_at ASC`
	}
	rows, err := s.driver.DB().QueryContext(ctx, colQuery, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := make([]ColumnMetadata, 0)
	for rows.Next() {
		var c ColumnMetadata
		var fType string
		if err := rows.Scan(&c.ID, &c.TableID, &c.Name, &c.DisplayName, &fType, &c.IsNullable, &c.IsPrimaryKey, &c.DefaultValue, &c.CalculationFormula, &c.ValidationRules, &c.CreatedAt, &c.MaxRepetitions, &c.SourceFieldID); err != nil {
			return nil, err
		}
		c.FieldType = dbal.AgnosticFieldType(fType)
		cols = append(cols, c)
	}
	return cols, rows.Err()
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

	if err := sourcetext.Ensure(ctx, db); err != nil {
		return err
	}
	if err := containermeta.Ensure(ctx, db); err != nil {
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
	if err := sourcetext.DeleteTable(ctx, tx, dialect, tableName); err != nil {
		return err
	}
	if err := containermeta.DeleteTable(ctx, tx, dialect, tableName); err != nil {
		return err
	}
	if err := data.ForgetRecordIDs(ctx, tx, dialect, tableName); err != nil {
		return fmt.Errorf("failed dropping the record ID allocator of %s: %w", tableName, err)
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
	colQuery := `SELECT name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, max_repetitions FROM sys_columns WHERE table_id = $1 ORDER BY created_at ASC`
	if dialect.Engine() == dbal.EngineMariaDB {
		colQuery = `SELECT name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, max_repetitions FROM sys_columns WHERE table_id = ? ORDER BY created_at ASC`
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
		maxRepetitions                             int
	}
	var cols []srcCol
	for cRows.Next() {
		var c srcCol
		var fType string
		if err := cRows.Scan(&c.name, &c.displayName, &fType, &c.isNullable, &c.isPrimaryKey, &c.defaultValue, &c.calcFormula, &c.validationRules, &c.maxRepetitions); err != nil {
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
			MaxRepetitions:     c.maxRepetitions,
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
	if err := sourcetext.DeleteTable(ctx, db, dialect, tableName); err != nil {
		return err
	}
	return containermeta.DeleteTable(ctx, db, dialect, tableName)
}
