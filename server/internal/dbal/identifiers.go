package dbal

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxIdentifierLength is the longest table or column name accepted. PostgreSQL
// silently truncates identifiers to 63 bytes (MariaDB allows 64), so two longer
// names could otherwise map to the same physical object.
const MaxIdentifierLength = 63

// ErrInvalidIdentifier is returned for a table or column name that breaks the
// naming policy.
var ErrInvalidIdentifier = errors.New("invalid identifier")

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// reservedTablePrefixes and reservedTableNames belong to File4Base's own
// catalog (sys_*) and to the database engines; user tables may not use them.
var (
	reservedTablePrefixes = []string{"sys_", "pg_"}
	reservedTableNames    = map[string]bool{
		"information_schema": true, "mysql": true, "performance_schema": true, "sys": true,
	}
)

// NormalizeIdentifier lower-cases and trims a user-supplied name.
func NormalizeIdentifier(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// CheckIdentifier validates a normalized table or column name: a letter
// followed by letters, digits or underscores, at most MaxIdentifierLength long.
func CheckIdentifier(kind, name string) error {
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("%w: %s name '%s' must start with a letter and contain only letters, digits and underscores", ErrInvalidIdentifier, kind, name)
	}
	if len(name) > MaxIdentifierLength {
		return fmt.Errorf("%w: %s name '%s' is longer than %d characters", ErrInvalidIdentifier, kind, name, MaxIdentifierLength)
	}
	return nil
}

// IsReservedTableName reports whether a table name belongs to the system
// catalog or to the database engine, whatever its case or surrounding spaces.
func IsReservedTableName(name string) bool {
	n := NormalizeIdentifier(name)
	if reservedTableNames[n] {
		return true
	}
	for _, p := range reservedTablePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// CheckUserTableName validates a normalized name for a user table.
func CheckUserTableName(name string) error {
	if err := CheckIdentifier("table", name); err != nil {
		return err
	}
	if IsReservedTableName(name) {
		return fmt.Errorf("%w: table name '%s' is reserved for the system catalog", ErrInvalidIdentifier, name)
	}
	return nil
}
