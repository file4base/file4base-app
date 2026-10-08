package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/extsource"
	"github.com/google/uuid"
)

// ErrDataSourceNotFound is returned when no data source has the given id.
var ErrDataSourceNotFound = errors.New("data source not found")

// ErrDataSourceExists is returned when another source already has that name.
var ErrDataSourceExists = errors.New("a data source with that name already exists")

// ErrInvalidDataSource is returned for a connection that cannot be stored.
var ErrInvalidDataSource = errors.New("invalid data source")

// DataSource is a named connection to another SQL database that records can
// be imported from (#47).
//
// **It holds no password.** A password is supplied with the request that
// reads, or taken from an environment variable of the server named by
// PasswordEnv, so that no secret is written to the catalog, to a solution
// file or to a log.
type DataSource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Schema   string `json:"schema,omitempty"`
	TLS      bool   `json:"tls"`

	// PasswordEnv names an environment variable of the **server** holding the
	// password, for an import that runs without anyone typing one.
	PasswordEnv string `json:"password_env,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Connection is the source as the reader takes it.
func (d DataSource) Connection() extsource.Connection {
	return extsource.Connection{
		Engine:   extsource.Engine(d.Engine),
		Host:     d.Host,
		Port:     d.Port,
		Database: d.Database,
		Username: d.Username,
		Schema:   d.Schema,
		TLS:      d.TLS,
	}
}

// Password resolves the password for a read: the one given with the request,
// otherwise the environment variable the source names.
func (d DataSource) Password(given string) string {
	if given != "" {
		return given
	}
	if env := strings.TrimSpace(d.PasswordEnv); env != "" {
		return os.Getenv(env)
	}
	return ""
}

// DataSourceInput is what a caller supplies to create or update a source.
type DataSourceInput struct {
	Name        string `json:"name"`
	Engine      string `json:"engine"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	Username    string `json:"username"`
	Schema      string `json:"schema"`
	TLS         bool   `json:"tls"`
	PasswordEnv string `json:"password_env"`
}

func (s *Service) dataSourcePlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// checkDataSource validates a connection before it is stored.
func checkDataSource(in DataSourceInput) (DataSourceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("%w: a data source needs a name", ErrInvalidDataSource)
	}
	if len([]rune(in.Name)) > 128 {
		return in, fmt.Errorf("%w: the name is at most 128 characters", ErrInvalidDataSource)
	}

	in.Engine = strings.ToLower(strings.TrimSpace(in.Engine))
	known := false
	for _, engine := range extsource.Engines {
		if string(engine) == in.Engine {
			known = true
		}
	}
	if !known {
		names := make([]string, 0, len(extsource.Engines))
		for _, engine := range extsource.Engines {
			names = append(names, string(engine))
		}
		return in, fmt.Errorf("%w: the engine is one of %s", ErrInvalidDataSource, strings.Join(names, ", "))
	}

	in.Host = strings.TrimSpace(in.Host)
	if in.Host == "" {
		return in, fmt.Errorf("%w: the host is missing", ErrInvalidDataSource)
	}
	if in.Port < 0 || in.Port > 65535 {
		return in, fmt.Errorf("%w: %d is not a port", ErrInvalidDataSource, in.Port)
	}
	if in.Port == 0 {
		in.Port = extsource.Engine(in.Engine).DefaultPort()
	}
	in.Database = strings.TrimSpace(in.Database)
	in.Username = strings.TrimSpace(in.Username)
	in.Schema = strings.TrimSpace(in.Schema)

	// A password in the connection itself is refused rather than ignored, so
	// nobody believes File4Base is keeping one.
	in.PasswordEnv = strings.TrimSpace(in.PasswordEnv)
	if in.PasswordEnv != "" && !environmentName(in.PasswordEnv) {
		return in, fmt.Errorf("%w: %q is not the name of an environment variable",
			ErrInvalidDataSource, in.PasswordEnv)
	}
	return in, nil
}

// environmentName accepts the usual spelling of an environment variable.
func environmentName(s string) bool {
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return s != ""
}

// CreateDataSource registers a new connection.
func (s *Service) CreateDataSource(ctx context.Context, in DataSourceInput) (*DataSource, error) {
	in, err := checkDataSource(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.dataSourceNameTaken(ctx, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrDataSourceExists, in.Name)
	}

	now := time.Now().UTC()
	source := &DataSource{
		ID: uuid.NewString(), Name: in.Name, Engine: in.Engine, Host: in.Host, Port: in.Port,
		Database: in.Database, Username: in.Username, Schema: in.Schema, TLS: in.TLS,
		PasswordEnv: in.PasswordEnv, CreatedAt: now, UpdatedAt: now,
	}
	q := fmt.Sprintf(`INSERT INTO sys_data_sources
	        (id, name, engine, host, port, database_name, username, schema_name, use_tls, password_env, created_at, updated_at)
	        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		s.dataSourcePlaceholder(1), s.dataSourcePlaceholder(2), s.dataSourcePlaceholder(3), s.dataSourcePlaceholder(4),
		s.dataSourcePlaceholder(5), s.dataSourcePlaceholder(6), s.dataSourcePlaceholder(7), s.dataSourcePlaceholder(8),
		s.dataSourcePlaceholder(9), s.dataSourcePlaceholder(10), s.dataSourcePlaceholder(11), s.dataSourcePlaceholder(12))
	if _, err := s.driver.DB().ExecContext(ctx, q, source.ID, source.Name, source.Engine, source.Host,
		source.Port, source.Database, source.Username, source.Schema, source.TLS, source.PasswordEnv,
		source.CreatedAt, source.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed creating data source %q: %w", source.Name, err)
	}
	return source, nil
}

// UpdateDataSource replaces a connection's settings.
func (s *Service) UpdateDataSource(ctx context.Context, id string, in DataSourceInput) (*DataSource, error) {
	in, err := checkDataSource(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.dataSourceNameTaken(ctx, in.Name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrDataSourceExists, in.Name)
	}

	q := fmt.Sprintf(`UPDATE sys_data_sources SET name = %s, engine = %s, host = %s, port = %s,
	        database_name = %s, username = %s, schema_name = %s, use_tls = %s, password_env = %s, updated_at = %s
	        WHERE id = %s`,
		s.dataSourcePlaceholder(1), s.dataSourcePlaceholder(2), s.dataSourcePlaceholder(3), s.dataSourcePlaceholder(4),
		s.dataSourcePlaceholder(5), s.dataSourcePlaceholder(6), s.dataSourcePlaceholder(7), s.dataSourcePlaceholder(8),
		s.dataSourcePlaceholder(9), s.dataSourcePlaceholder(10), s.dataSourcePlaceholder(11))
	res, err := s.driver.DB().ExecContext(ctx, q, in.Name, in.Engine, in.Host, in.Port, in.Database,
		in.Username, in.Schema, in.TLS, in.PasswordEnv, time.Now().UTC(), id)
	if err != nil {
		return nil, fmt.Errorf("failed updating data source %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, fmt.Errorf("%w: %s", ErrDataSourceNotFound, id)
	}
	return s.GetDataSource(ctx, id)
}

// DeleteDataSource removes a connection.
func (s *Service) DeleteDataSource(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_data_sources WHERE id = %s`, s.dataSourcePlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting data source %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrDataSourceNotFound, id)
	}
	return nil
}

const dataSourceColumns = `id, name, engine, host, port, database_name, username, schema_name, use_tls, password_env, created_at, updated_at`

func scanDataSource(row rowScanner) (*DataSource, error) {
	var d DataSource
	var database, username, schemaName, passwordEnv sql.NullString
	var port sql.NullInt64
	if err := row.Scan(&d.ID, &d.Name, &d.Engine, &d.Host, &port, &database, &username,
		&schemaName, &d.TLS, &passwordEnv, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.Port = int(port.Int64)
	d.Database, d.Username, d.Schema, d.PasswordEnv =
		database.String, username.String, schemaName.String, passwordEnv.String
	return &d, nil
}

// GetDataSource reads one connection.
func (s *Service) GetDataSource(ctx context.Context, id string) (*DataSource, error) {
	q := fmt.Sprintf(`SELECT %s FROM sys_data_sources WHERE id = %s`, dataSourceColumns, s.dataSourcePlaceholder(1))
	source, err := scanDataSource(s.driver.DB().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrDataSourceNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return source, nil
}

// ListDataSources reads every connection, by name.
func (s *Service) ListDataSources(ctx context.Context) ([]DataSource, error) {
	rows, err := s.driver.DB().QueryContext(ctx,
		fmt.Sprintf(`SELECT %s FROM sys_data_sources ORDER BY name ASC`, dataSourceColumns))
	if err != nil {
		return nil, fmt.Errorf("failed reading the data sources: %w", err)
	}
	defer rows.Close()

	sources := []DataSource{}
	for rows.Next() {
		source, err := scanDataSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, *source)
	}
	return sources, rows.Err()
}

func (s *Service) dataSourceNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_data_sources WHERE LOWER(name) = LOWER(%s) AND id <> %s`,
		s.dataSourcePlaceholder(1), s.dataSourcePlaceholder(2))
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, q, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed checking the name %q: %w", name, err)
	}
	return count > 0, nil
}
