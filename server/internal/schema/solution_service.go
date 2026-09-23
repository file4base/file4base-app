package schema

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
)

const credentialSecretKey = "file4base_secure_credentials_key_v1"

// EncodeCredential encodes sensitive credentials using reversible key-stream obfuscation
func EncodeCredential(plain string) string {
	if plain == "" {
		return ""
	}
	key := []byte(credentialSecretKey)
	data := []byte(plain)
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[i] ^ key[i%len(key)]
	}
	return "enc:" + base64.StdEncoding.EncodeToString(out)
}

// DecodeCredential decodes sensitive credentials back to plain text
func DecodeCredential(encoded string) string {
	if encoded == "" {
		return ""
	}
	if !strings.HasPrefix(encoded, "enc:") {
		return encoded
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:"))
	if err != nil {
		return encoded
	}
	key := []byte(credentialSecretKey)
	out := make([]byte, len(raw))
	for i := range raw {
		out[i] = raw[i] ^ key[i%len(key)]
	}
	return string(out)
}

// DatabaseConnectionConfig represents DB connection settings stored inside the MessagePack .f4b file
type DatabaseConnectionConfig struct {
	Engine   string `json:"engine" msgpack:"engine"`
	Host     string `json:"host" msgpack:"host"`
	Port     int    `json:"port" msgpack:"port"`
	Database string `json:"database" msgpack:"database"`
	User     string `json:"user" msgpack:"user"`
	Password string `json:"password" msgpack:"password"`
	SSLMode  string `json:"ssl_mode" msgpack:"ssl_mode"`
}

// UserAccount represents a user account stored inside the .f4b file
type UserAccount struct {
	ID        string    `json:"id" msgpack:"id"`
	Username  string    `json:"username" msgpack:"username"`
	Role      string    `json:"role" msgpack:"role"`
	CreatedAt time.Time `json:"created_at" msgpack:"created_at"`
}

// SolutionBundle represents the full .f4b file contents
type SolutionBundle struct {
	Format             string                   `json:"format" msgpack:"format"`
	Version            string                   `json:"version" msgpack:"version"`
	SolutionName       string                   `json:"solution_name" msgpack:"solution_name"`
	DatabaseConnection DatabaseConnectionConfig `json:"database_connection" msgpack:"database_connection"`
	Tables             []TableMetadata          `json:"tables" msgpack:"tables"`
	TableOccurrences   []TableOccurrence        `json:"table_occurrences" msgpack:"table_occurrences"`
	Relationships      []RelationshipMetadata   `json:"relationships" msgpack:"relationships"`
	Layouts            []LayoutMetadata         `json:"layouts" msgpack:"layouts"`
	Users              []UserAccount            `json:"users" msgpack:"users"`
	CreatedAt          time.Time                `json:"created_at" msgpack:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at" msgpack:"updated_at"`
}

// DatabaseDataBundle represents the .f4data file contents (all records in the database)
type DatabaseDataBundle struct {
	Format       string                            `json:"format" msgpack:"format"`
	Version      string                            `json:"version" msgpack:"version"`
	DatabaseName string                            `json:"database_name" msgpack:"database_name"`
	ExportedAt   time.Time                         `json:"exported_at" msgpack:"exported_at"`
	TablesData   map[string][]map[string]interface{} `json:"tables_data" msgpack:"tables_data"`
}

// ExportSolution serializes all schemas, layouts, occurrences, and connection parameters to MessagePack
func (s *Service) ExportSolution(ctx context.Context, solutionName string, dbConfig DatabaseConnectionConfig) ([]byte, error) {
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing tables: %w", err)
	}

	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing occurrences: %w", err)
	}

	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing layouts: %w", err)
	}

	now := time.Now().UTC()
	encodedDBConfig := dbConfig
	encodedDBConfig.User = EncodeCredential(dbConfig.User)
	encodedDBConfig.Password = EncodeCredential(dbConfig.Password)

	bundle := SolutionBundle{
		Format:             "file4base_solution",
		Version:            "1.0",
		SolutionName:       solutionName,
		DatabaseConnection: encodedDBConfig,
		Tables:             tables,
		TableOccurrences:   occurrences,
		Relationships:      make([]RelationshipMetadata, 0),
		Layouts:            layouts,
		Users: []UserAccount{
			{
				ID:        uuid.NewString(),
				Username:  "admin",
				Role:      "Full Access",
				CreatedAt: now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	return msgpack.Marshal(bundle)
}

// ImportSolution imports a solution bundle from MessagePack bytes into the active database
func (s *Service) ImportSolution(ctx context.Context, data []byte) (*SolutionBundle, error) {
	var bundle SolutionBundle
	if err := msgpack.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("invalid MessagePack solution bundle: %w", err)
	}

	// Decode credentials
	bundle.DatabaseConnection.User = DecodeCredential(bundle.DatabaseConnection.User)
	bundle.DatabaseConnection.Password = DecodeCredential(bundle.DatabaseConnection.Password)

	// 1. Ensure system tables exist
	if err := s.EnsureSystemTables(ctx); err != nil {
		return nil, fmt.Errorf("failed ensuring system tables: %w", err)
	}

	// 2. Fetch current tables to compare
	currentTables, err := s.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing current tables: %w", err)
	}
	existingTablesByName := make(map[string]*TableMetadata)
	for i := range currentTables {
		existingTablesByName[currentTables[i].Name] = &currentTables[i]
	}

	// 3. Re-create physical tables and columns if not present
	for _, tbl := range bundle.Tables {
		existingTable := existingTablesByName[tbl.Name]
		var currentTableID string
		if existingTable == nil {
			created, err := s.CreateTable(ctx, tbl.DisplayName, tbl.Name)
			if err != nil {
				return nil, fmt.Errorf("failed creating table %s: %w", tbl.Name, err)
			}
			currentTableID = created.ID
		} else {
			currentTableID = existingTable.ID
		}

		// Ensure columns
		existingCols := make(map[string]bool)
		if existingTable != nil {
			for _, col := range existingTable.Columns {
				existingCols[col.Name] = true
			}
		}

		for _, col := range tbl.Columns {
			if col.IsPrimaryKey || existingCols[col.Name] {
				continue
			}
			_, err := s.AddColumn(ctx, currentTableID, col)
			if err != nil {
				return nil, fmt.Errorf("failed adding column %s to table %s: %w", col.Name, tbl.Name, err)
			}
		}
	}

	// 4. Import layouts
	for _, lay := range bundle.Layouts {
		_, err := s.GetLayout(ctx, lay.ID)
		if err != nil {
			_, _ = s.CreateLayout(ctx, lay.Name, lay.TableOccurrenceID, lay.Definition)
		} else {
			_, _ = s.UpdateLayout(ctx, lay.ID, lay.Name, lay.Definition)
		}
	}

	return &bundle, nil
}

// ExportDatabaseData exports all rows from all user tables into a MessagePack data file (.f4data)
func (s *Service) ExportDatabaseData(ctx context.Context, dbName string) ([]byte, error) {
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed listing tables: %w", err)
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	tablesData := make(map[string][]map[string]interface{})

	for _, tbl := range tables {
		q := fmt.Sprintf(`SELECT * FROM %s`, dialect.QuoteIdentifier(tbl.Name))
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			continue
		}

		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			continue
		}

		tableRows := make([]map[string]interface{}, 0)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			valPtrs := make([]interface{}, len(cols))
			for i := range vals {
				valPtrs[i] = &vals[i]
			}

			if err := rows.Scan(valPtrs...); err != nil {
				continue
			}

			rowMap := make(map[string]interface{})
			for i, colName := range cols {
				val := vals[i]
				if b, ok := val.([]byte); ok {
					rowMap[colName] = string(b)
				} else {
					rowMap[colName] = val
				}
			}
			tableRows = append(tableRows, rowMap)
		}
		rows.Close()
		tablesData[tbl.Name] = tableRows
	}

	bundle := DatabaseDataBundle{
		Format:       "file4base_data",
		Version:      "1.0",
		DatabaseName: dbName,
		ExportedAt:   time.Now().UTC(),
		TablesData:   tablesData,
	}

	return msgpack.Marshal(bundle)
}

// ImportDatabaseData restores records from a MessagePack data file into the active database
func (s *Service) ImportDatabaseData(ctx context.Context, data []byte) (*DatabaseDataBundle, error) {
	var bundle DatabaseDataBundle
	if err := msgpack.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("invalid MessagePack database data file: %w", err)
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	for tableName, rows := range bundle.TablesData {
		for _, row := range rows {
			if len(row) == 0 {
				continue
			}

			colNames := make([]string, 0, len(row))
			placeholders := make([]string, 0, len(row))
			vals := make([]interface{}, 0, len(row))

			idx := 1
			for k, v := range row {
				colNames = append(colNames, dialect.QuoteIdentifier(k))
				placeholders = append(placeholders, dialect.Placeholder(idx))
				vals = append(vals, v)
				idx++
			}

			var insertSQL string
			if dialect.Engine() == dbal.EnginePostgres {
				// Upsert with ON CONFLICT (id) DO NOTHING if id column present
				if _, hasID := row["id"]; hasID {
					insertSQL = fmt.Sprintf(
						`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (id) DO NOTHING`,
						dialect.QuoteIdentifier(tableName),
						joinStrings(colNames, ", "),
						joinStrings(placeholders, ", "),
					)
				} else {
					insertSQL = fmt.Sprintf(
						`INSERT INTO %s (%s) VALUES (%s)`,
						dialect.QuoteIdentifier(tableName),
						joinStrings(colNames, ", "),
						joinStrings(placeholders, ", "),
					)
				}
			} else {
				insertSQL = fmt.Sprintf(
					`INSERT IGNORE INTO %s (%s) VALUES (%s)`,
					dialect.QuoteIdentifier(tableName),
					joinStrings(colNames, ", "),
					joinStrings(placeholders, ", "),
				)
			}

			_, _ = db.ExecContext(ctx, insertSQL, vals...)
		}
	}

	return &bundle, nil
}

func joinStrings(items []string, sep string) string {
	res := ""
	for i, s := range items {
		if i > 0 {
			res += sep
		}
		res += s
	}
	return res
}
