// Package validation enforces the field validation rules configured in the
// Fields dialog (sys_columns.validation_rules) on every record write: the
// data API and data import.
//
// Rules and their meaning:
//
//	not_empty           the field must have a value (not null, not blank text)
//	unique              no other record has the same value (empty values are
//	                    not compared); backed by a unique index on PostgreSQL
//	existing_value      another record already has the same value
//	strict_type         the value is "Numeric Only", a "Date" (YYYY-MM-DD),
//	                    a "4-Digit Year" date, a "Time of Day" (HH:MM[:SS]) or
//	                    "Text Only" (not a number)
//	range_min/range_max the value is within the bounds (numbers compared as
//	                    numbers, other values as text, e.g. ISO dates)
//	max_length          at most that many characters
//	custom_message      replaces the default error message
//
// not_empty_timing is the timing of the whole rule set: "always" applies the
// rules to every write, including imports; "entry" applies them to data
// entry (the data API) only. Empty values are only rejected by not_empty.
package validation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrValidation is wrapped by every *Error.
var ErrValidation = errors.New("validation failed")

// ErrInvalidRules is returned for validation_rules that are not a JSON object
// of rules.
var ErrInvalidRules = errors.New("invalid validation rules")

// Error reports the field and the rule a value broke.
type Error struct {
	Table   string `json:"table"`
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return ErrValidation }

// Rules is the validation_rules JSON written by the Fields dialog.
type Rules struct {
	NotEmpty             bool   `json:"not_empty"`
	Timing               string `json:"not_empty_timing"`
	Unique               bool   `json:"unique"`
	ExistingValue        bool   `json:"existing_value"`
	StrictTypeEnabled    bool   `json:"strict_type_enabled"`
	StrictType           string `json:"strict_type"`
	RangeEnabled         bool   `json:"range_enabled"`
	RangeMin             string `json:"range_min"`
	RangeMax             string `json:"range_max"`
	MaxLengthEnabled     bool   `json:"max_length_enabled"`
	MaxLength            *int   `json:"max_length"`
	CustomMessageEnabled bool   `json:"custom_message_enabled"`
	CustomMessage        string `json:"custom_message"`
}

// Parse reads validation_rules. Nil, blank or "null" mean no rules.
func Parse(raw *string) (*Rules, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" || strings.TrimSpace(*raw) == "null" {
		return nil, nil
	}
	var r Rules
	if err := json.Unmarshal([]byte(*raw), &r); err != nil {
		return nil, fmt.Errorf("%w: validation_rules must be a JSON object of rules", ErrInvalidRules)
	}
	if r.MaxLengthEnabled && (r.MaxLength == nil || *r.MaxLength < 0) {
		return nil, fmt.Errorf("%w: max_length must be a non-negative number when max_length_enabled is set", ErrInvalidRules)
	}
	if r.StrictTypeEnabled && !knownStrictType(r.StrictType) {
		return nil, fmt.Errorf("%w: unknown strict_type %q", ErrInvalidRules, r.StrictType)
	}
	return &r, nil
}

func knownStrictType(t string) bool {
	switch t {
	case "Numeric Only", "Date", "4-Digit Year", "Time of Day", "Text Only":
		return true
	}
	return false
}

// AppliesTo reports whether the rules apply to a write: data entry (the data
// API) always; imports only when the timing is "always".
func (r *Rules) AppliesTo(dataEntry bool) bool {
	return dataEntry || r.Timing == "" || r.Timing == "always"
}

// Field is a field of a table with its rules (nil when it has none).
type Field struct {
	Name  string
	Type  dbal.AgnosticFieldType
	Rules *Rules
}

// Querier runs the lookups of the unique and existing-value rules, on the
// database or inside a transaction.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Checker validates records of one table.
type Checker struct {
	Dialect dbal.Dialect
	DB      Querier
	Table   string
	Fields  map[string]Field
}

// HasRules reports whether any field of the table has rules.
func (c *Checker) HasRules() bool {
	for _, f := range c.Fields {
		if f.Rules != nil {
			return true
		}
	}
	return false
}

// Check validates the fields named in fields (all ruled fields when nil) of a
// record, using values (the record's values after the write). recordID is
// excluded from the unique and existing-value lookups.
func (c *Checker) Check(ctx context.Context, recordID string, values map[string]interface{}, fields map[string]bool, dataEntry bool) error {
	for name, f := range c.Fields {
		if f.Rules == nil || !f.Rules.AppliesTo(dataEntry) || (fields != nil && !fields[name]) {
			continue
		}
		if err := c.checkField(ctx, recordID, f, values[name]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Checker) fail(f Field, rule, message string) error {
	if f.Rules.CustomMessageEnabled && strings.TrimSpace(f.Rules.CustomMessage) != "" {
		message = f.Rules.CustomMessage
	}
	return &Error{Table: c.Table, Field: f.Name, Rule: rule, Message: message}
}

func (c *Checker) checkField(ctx context.Context, recordID string, f Field, value interface{}) error {
	r := f.Rules
	text, empty := asText(value)
	if empty {
		if r.NotEmpty {
			return c.fail(f, "not_empty", fmt.Sprintf("%s requires a value", f.Name))
		}
		return nil
	}
	if r.StrictTypeEnabled {
		if !matchesStrictType(r.StrictType, text) {
			return c.fail(f, "strict_type", fmt.Sprintf("%s must be %s", f.Name, strictTypeDescription(r.StrictType)))
		}
	}
	if r.MaxLengthEnabled && r.MaxLength != nil && utf8.RuneCountInString(text) > *r.MaxLength {
		return c.fail(f, "max_length", fmt.Sprintf("%s accepts at most %d characters", f.Name, *r.MaxLength))
	}
	if r.RangeEnabled {
		if strings.TrimSpace(r.RangeMin) != "" && compare(text, r.RangeMin) < 0 {
			return c.fail(f, "range", fmt.Sprintf("%s must be at least %s", f.Name, r.RangeMin))
		}
		if strings.TrimSpace(r.RangeMax) != "" && compare(text, r.RangeMax) > 0 {
			return c.fail(f, "range", fmt.Sprintf("%s must be at most %s", f.Name, r.RangeMax))
		}
	}
	if r.Unique || r.ExistingValue {
		others, err := c.countOthers(ctx, f.Name, value, recordID)
		if err != nil {
			return err
		}
		if r.Unique && others > 0 {
			return c.fail(f, "unique", fmt.Sprintf("another record already has this %s", f.Name))
		}
		if r.ExistingValue && others == 0 {
			return c.fail(f, "existing_value", fmt.Sprintf("%s must be a value another record already has", f.Name))
		}
	}
	return nil
}

func (c *Checker) countOthers(ctx context.Context, field string, value interface{}, recordID string) (int, error) {
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = %s AND %s <> %s",
		c.Dialect.QuoteIdentifier(c.Table), c.Dialect.QuoteIdentifier(field), c.Dialect.Placeholder(1),
		c.Dialect.QuoteIdentifier("id"), c.Dialect.Placeholder(2))
	var n int
	if err := c.DB.QueryRowContext(ctx, q, value, recordID).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed checking %s.%s: %w", c.Table, field, err)
	}
	return n, nil
}

// asText returns the value as text, and whether it is empty (null or blank).
func asText(v interface{}) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return t, strings.TrimSpace(t) == ""
	case []byte:
		return string(t), len(t) == 0
	case time.Time:
		return t.Format(time.RFC3339), false
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), false
	case json.Number:
		return t.String(), false
	}
	return fmt.Sprint(v), false
}

var (
	isoDate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
	timeOfDay = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d+)?)?$`)
)

func matchesStrictType(kind, text string) bool {
	text = strings.TrimSpace(text)
	switch kind {
	case "Numeric Only":
		_, err := strconv.ParseFloat(text, 64)
		return err == nil
	case "Date", "4-Digit Year":
		if !isoDate.MatchString(text) {
			return false
		}
		_, err := time.Parse("2006-01-02", text[:10])
		return err == nil
	case "Time of Day":
		return timeOfDay.MatchString(text)
	case "Text Only":
		_, err := strconv.ParseFloat(text, 64)
		return err != nil
	}
	return true
}

func strictTypeDescription(kind string) string {
	switch kind {
	case "Numeric Only":
		return "a number"
	case "Date":
		return "a date (YYYY-MM-DD)"
	case "4-Digit Year":
		return "a date with a 4-digit year (YYYY-MM-DD)"
	case "Time of Day":
		return "a time of day (HH:MM or HH:MM:SS)"
	case "Text Only":
		return "text, not a number"
	}
	return kind
}

// compare orders a value and a bound: as numbers when both are numbers,
// otherwise as text (which orders ISO dates and times correctly).
func compare(value, bound string) int {
	a, errA := strconv.ParseFloat(strings.TrimSpace(value), 64)
	b, errB := strconv.ParseFloat(strings.TrimSpace(bound), 64)
	if errA == nil && errB == nil {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		}
		return 0
	}
	return strings.Compare(strings.TrimSpace(value), strings.TrimSpace(bound))
}

// LoadFields reads the fields of a user table with their rules.
func LoadFields(ctx context.Context, db interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}, dialect dbal.Dialect, table string) (map[string]Field, error) {
	q := `SELECT c.name, c.field_type, c.validation_rules FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `SELECT c.name, c.field_type, c.validation_rules FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = ?`
	}
	rows, err := db.QueryContext(ctx, q, table)
	if err != nil {
		return nil, fmt.Errorf("failed reading the fields of %s: %w", table, err)
	}
	defer rows.Close()
	fields := map[string]Field{}
	for rows.Next() {
		var name, fieldType string
		var raw sql.NullString
		if err := rows.Scan(&name, &fieldType, &raw); err != nil {
			return nil, err
		}
		f := Field{Name: name, Type: dbal.AgnosticFieldType(fieldType)}
		if raw.Valid {
			// Rules saved before validation was enforced may be malformed:
			// they enforce nothing rather than blocking every write.
			if r, err := Parse(&raw.String); err == nil {
				f.Rules = r
			}
		}
		fields[name] = f
	}
	return fields, rows.Err()
}

// UniqueIndexName is the name of the index that backs a unique rule.
func UniqueIndexName(table, field string) string {
	sum := sha256.Sum256([]byte(table + "\x00" + field))
	return "uq_f4b_" + hex.EncodeToString(sum[:8])
}

// IsUniqueIndexViolation returns the field of a unique-rule index named in a
// database error, so a concurrent duplicate is reported like any other
// unique violation.
func IsUniqueIndexViolation(err error, table string, fields map[string]Field) (Field, bool) {
	if err == nil {
		return Field{}, false
	}
	msg := err.Error()
	if !strings.Contains(msg, "uq_f4b_") {
		return Field{}, false
	}
	for name, f := range fields {
		if strings.Contains(msg, UniqueIndexName(table, name)) {
			return f, true
		}
	}
	return Field{}, false
}

// UniqueViolation builds the error of a unique rule broken by a concurrent
// write (detected by the index).
func UniqueViolation(table string, f Field) error {
	c := &Checker{Table: table}
	if f.Rules == nil {
		f.Rules = &Rules{}
	}
	return c.fail(f, "unique", fmt.Sprintf("another record already has this %s", f.Name))
}
