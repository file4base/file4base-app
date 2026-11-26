package fmdapi

import (
	"fmt"
	"strings"
)

// DefaultProductName is the name productInfo gives File4Base by.
const DefaultProductName = "File4Base Data API"

// Config is what the operator decides about the Data API.
type Config struct {
	// Databases are the databases the Data API serves, under the names
	// clients use. None means the Data API is off.
	Databases []DatabaseAlias
	// ProductName, ProductVersion and ProductBuildDate are what productInfo
	// reports. The name defaults to DefaultProductName; the version and
	// build date are left out unless set, as FileMaker Server 26 does.
	ProductName      string
	ProductVersion   string
	ProductBuildDate string
	// MaxBodyBytes bounds a JSON request body; 0 selects DefaultMaxBodyBytes.
	MaxBodyBytes int64
}

// DatabaseAlias exposes a File4Base database to the Data API under the
// name FileMaker clients know it by: the name of the FileMaker file it was
// migrated from, without extension.
type DatabaseAlias struct {
	// Name is the FileMaker database name. Clients may sign in with it in
	// any case; the session is then bound to the name as they wrote it.
	Name string
	// Database is the File4Base database.
	Database string
}

// ParseDatabaseAliases reads a comma-separated list whose entries are
// either a database ("sales") or a FileMaker name for it ("Sales 2024=sales").
func ParseDatabaseAliases(raw string) ([]DatabaseAlias, error) {
	var out []DatabaseAlias
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, database, found := strings.Cut(entry, "=")
		name, database = strings.TrimSpace(name), strings.TrimSpace(database)
		if !found {
			database = name
		}
		if name == "" || database == "" {
			return nil, fmt.Errorf("data API database %q: both the name and the database are required", entry)
		}
		// Sign-in matches names as alias does, so must the check.
		if (Config{Databases: out}).hasAlias(name) {
			return nil, fmt.Errorf("data API database name %q is listed twice", name)
		}
		out = append(out, DatabaseAlias{Name: name, Database: database})
	}
	return out, nil
}

func (c Config) hasAlias(name string) bool {
	_, ok := c.alias(name)
	return ok
}

// alias finds the database a client names, matching the name in any case
// as FileMaker Server does.
func (c Config) alias(name string) (DatabaseAlias, bool) {
	for _, a := range c.Databases {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return DatabaseAlias{}, false
}
