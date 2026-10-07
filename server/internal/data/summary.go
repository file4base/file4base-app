package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrInvalidSummary is returned when a summary field's definition cannot be
// used: an unknown kind of summary, a field it cannot be taken over, or a
// field that is not there.
var ErrInvalidSummary = errors.New("invalid summary field")

// SummaryType is what a summary field works out over the found set.
type SummaryType string

const (
	SummaryTotal             SummaryType = "total"
	SummaryAverage           SummaryType = "average"
	SummaryCount             SummaryType = "count"
	SummaryMinimum           SummaryType = "minimum"
	SummaryMaximum           SummaryType = "maximum"
	SummaryStandardDeviation SummaryType = "standard_deviation"
	SummaryFractionOfTotal   SummaryType = "fraction_of_total"
)

// SummaryTypes are the kinds a summary field may be, in the order the New
// Field dialog offers them.
var SummaryTypes = []SummaryType{
	SummaryTotal, SummaryAverage, SummaryCount,
	SummaryMinimum, SummaryMaximum, SummaryStandardDeviation, SummaryFractionOfTotal,
}

// needsNumber reports whether this kind of summary can only be taken over a
// field that holds numbers. Count, Minimum and Maximum work on anything.
func (t SummaryType) needsNumber() bool {
	switch t {
	case SummaryTotal, SummaryAverage, SummaryStandardDeviation, SummaryFractionOfTotal:
		return true
	}
	return false
}

// Valid reports whether this is one of the kinds File4Base computes.
func (t SummaryType) Valid() bool {
	for _, known := range SummaryTypes {
		if known == t {
			return true
		}
	}
	return false
}

// SummarySpec is a summary field's definition: what it works out, and over
// which field.
//
// It is stored as JSON in the field's `calculation_formula`, the column that
// already holds "how this field is computed" for calculation fields.
type SummarySpec struct {
	Type SummaryType `json:"summary_type"`

	// Field the summary is taken over. A summary cannot be taken over another
	// summary: there is no per-record value to aggregate.
	Field string `json:"field"`

	// Running makes the field a running total down the sorted records rather
	// than one figure for the whole group. It is worked out by the client as
	// it draws the rows, because it depends on the order they are drawn in.
	Running bool `json:"running"`
}

// legacySummarySpec is the shape the Field Options dialog saved before
// summary fields were computed: a SQL aggregate name and a target column.
// Nothing ever read it, but databases hold it, so it is read rather than
// ignored — a summary field defined before 0.9.4 starts working instead of
// having to be set up again.
type legacySummarySpec struct {
	Operation    string `json:"operation"`
	TargetColumn string `json:"target_column"`
	RunningTotal bool   `json:"running_total"`
}

// legacyOperations maps those aggregate names to the kinds of summary.
var legacyOperations = map[string]SummaryType{
	"SUM": SummaryTotal, "AVG": SummaryAverage, "COUNT": SummaryCount,
	"MIN": SummaryMinimum, "MAX": SummaryMaximum, "STDDEV": SummaryStandardDeviation,
}

// ParseSummarySpec reads a summary field's stored definition.
func ParseSummarySpec(stored string) (*SummarySpec, error) {
	text := strings.TrimSpace(stored)
	if text == "" {
		return nil, fmt.Errorf("%w: no definition", ErrInvalidSummary)
	}
	var spec SummarySpec
	if err := json.Unmarshal([]byte(text), &spec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSummary, err)
	}
	spec.Type = SummaryType(strings.ToLower(strings.TrimSpace(string(spec.Type))))
	spec.Field = strings.TrimSpace(spec.Field)

	if spec.Type == "" && spec.Field == "" {
		var legacy legacySummarySpec
		if err := json.Unmarshal([]byte(text), &legacy); err == nil && legacy.Operation != "" {
			spec = SummarySpec{
				Type:    legacyOperations[strings.ToUpper(strings.TrimSpace(legacy.Operation))],
				Field:   strings.TrimSpace(legacy.TargetColumn),
				Running: legacy.RunningTotal,
			}
		}
	}

	if !spec.Type.Valid() {
		return nil, fmt.Errorf("%w: unknown summary %q", ErrInvalidSummary, spec.Type)
	}
	if spec.Field == "" {
		return nil, fmt.Errorf("%w: no field to summarize", ErrInvalidSummary)
	}
	return &spec, nil
}

// Encode writes the definition back out for storage.
func (s SummarySpec) Encode() (string, error) {
	raw, err := json.Marshal(s)
	return string(raw), err
}

// CheckSummarySpec validates a definition against the fields of its table.
func CheckSummarySpec(spec SummarySpec, fields map[string]dbal.AgnosticFieldType, ownName string) error {
	if !spec.Type.Valid() {
		return fmt.Errorf("%w: unknown summary %q", ErrInvalidSummary, spec.Type)
	}
	if spec.Field == ownName {
		return fmt.Errorf("%w: a summary cannot be taken over itself", ErrInvalidSummary)
	}
	fieldType, known := fields[spec.Field]
	if !known {
		return fmt.Errorf("%w: no field named %q", ErrInvalidSummary, spec.Field)
	}
	if fieldType == dbal.FieldTypeSummary {
		return fmt.Errorf("%w: %q is itself a summary, which has no value in a record to total",
			ErrInvalidSummary, spec.Field)
	}
	if spec.Type.needsNumber() && fieldType != dbal.FieldTypeNumber {
		return fmt.Errorf("%w: %s needs a number field, and %q holds %s",
			ErrInvalidSummary, spec.Type, spec.Field, strings.ToLower(string(fieldType)))
	}
	return nil
}

// aggregateSQL is the expression that works out one summary over a set of
// rows. A fraction of the total is the group's own total; the dividing is done
// afterwards, where the grand total is known.
func aggregateSQL(dialect dbal.Dialect, spec SummarySpec) string {
	col := dialect.QuoteIdentifier(spec.Field)
	switch spec.Type {
	case SummaryTotal, SummaryFractionOfTotal:
		return fmt.Sprintf("SUM(%s)", col)
	case SummaryAverage:
		return fmt.Sprintf("AVG(%s)", col)
	case SummaryCount:
		return fmt.Sprintf("COUNT(%s)", col)
	case SummaryMinimum:
		return fmt.Sprintf("MIN(%s)", col)
	case SummaryMaximum:
		return fmt.Sprintf("MAX(%s)", col)
	case SummaryStandardDeviation:
		return fmt.Sprintf("STDDEV_SAMP(%s)", col)
	}
	return "NULL"
}

// SummaryGroup is one group of a report: the values its break fields hold, how
// many records are in it, and what each summary field works out to over them.
type SummaryGroup struct {
	// Break field values, in the order the caller asked to group by.
	Values map[string]interface{} `json:"values"`

	Count int `json:"count"`

	// Each summary field's value, by field name.
	Summaries map[string]interface{} `json:"summaries"`
}

// SummaryResult is what a report needs: one figure per summary field over the
// whole found set, and one per group.
type SummaryResult struct {
	Count     int                    `json:"count"`
	Grand     map[string]interface{} `json:"grand"`
	GroupBy   []string               `json:"group_by,omitempty"`
	Groups    []SummaryGroup         `json:"groups"`
	Summaries map[string]SummarySpec `json:"summaries"`
}

// SummaryRequest asks for the figures of a report.
type SummaryRequest struct {
	// The found set to summarize, in the same form as a find. Empty means
	// every record of the table.
	Requests []FindRequest `json:"requests"`

	// The summary fields to work out. Empty means every summary field the
	// table has.
	Fields []string `json:"fields"`

	// The break fields to group by, outermost first. Empty returns the grand
	// totals alone.
	GroupBy []string `json:"group_by"`
}

// tableSummaries returns the summary fields of a table and their definitions.
// A field whose definition cannot be read is left out rather than failing the
// report: one broken field must not make a layout unusable.
func (s *Service) tableSummaries(ctx context.Context, tableName string) (map[string]SummarySpec, error) {
	dialect := s.driver.Dialect()
	q := `SELECT c.name, c.calculation_formula FROM sys_columns c
		JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = ` + dialect.Placeholder(1) + ` AND c.field_type = ` + dialect.Placeholder(2)

	rows, err := s.driver.DB().QueryContext(ctx, q, tableName, string(dbal.FieldTypeSummary))
	if err != nil {
		return nil, fmt.Errorf("failed reading the summary fields of %s: %w", tableName, err)
	}
	defer rows.Close()

	specs := make(map[string]SummarySpec)
	for rows.Next() {
		var name string
		var stored sql.NullString
		if err := rows.Scan(&name, &stored); err != nil {
			return nil, err
		}
		if !stored.Valid {
			continue
		}
		spec, err := ParseSummarySpec(stored.String)
		if err != nil {
			continue
		}
		specs[name] = *spec
	}
	return specs, rows.Err()
}

// Summarize works out the figures of a report: what each summary field comes
// to over the found set, and over each group of it (#32).
//
// The found set is defined exactly as a find defines it, so a report totals
// the records the find returned.
func (s *Service) Summarize(ctx context.Context, tableName string, req SummaryRequest) (*SummaryResult, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	all, err := s.tableSummaries(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// The summary fields asked for, or every one the table has.
	wanted := make(map[string]SummarySpec)
	if len(req.Fields) == 0 {
		wanted = all
	} else {
		for _, name := range req.Fields {
			spec, known := all[name]
			if !known {
				return nil, fmt.Errorf("%w: %s.%s is not a summary field", ErrInvalidSummary, tableName, name)
			}
			wanted[name] = spec
		}
	}
	for name, spec := range wanted {
		if err := checkField(fields, tableName, spec.Field); err != nil {
			return nil, fmt.Errorf("%w: %s summarizes %q: %v", ErrInvalidSummary, name, spec.Field, err)
		}
	}
	for _, field := range req.GroupBy {
		if err := checkField(fields, tableName, field); err != nil {
			return nil, err
		}
	}

	dialect := s.driver.Dialect()
	where, values, err := buildFindClauses(dialect, fields, tableName, req.Requests)
	if err != nil {
		return nil, err
	}

	// A stable order for the columns, so the SQL and the scan agree.
	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)

	result := &SummaryResult{
		Grand:     map[string]interface{}{},
		Groups:    []SummaryGroup{},
		GroupBy:   req.GroupBy,
		Summaries: wanted,
	}

	grandCount, grand, err := s.summaryRow(ctx, dialect, tableName, where, values, names, wanted, nil)
	if err != nil {
		return nil, err
	}
	result.Count = grandCount
	result.Grand = grand

	if len(req.GroupBy) > 0 {
		groups, err := s.summaryGroups(ctx, dialect, tableName, where, values, names, wanted, req.GroupBy)
		if err != nil {
			return nil, err
		}
		result.Groups = groups
	}

	// A fraction of the total needs the grand total, which only exists once
	// every group has been read.
	applyFractions(result, names, wanted)
	return result, nil
}

// summaryRow works out the grand figures: one row, over the whole found set.
func (s *Service) summaryRow(
	ctx context.Context, dialect dbal.Dialect, tableName, where string, values []interface{},
	names []string, specs map[string]SummarySpec, _ []string,
) (int, map[string]interface{}, error) {
	selects := []string{"COUNT(*)"}
	for _, name := range names {
		selects = append(selects, aggregateSQL(dialect, specs[name]))
	}

	q := fmt.Sprintf("SELECT %s FROM %s %s",
		strings.Join(selects, ", "), dialect.QuoteIdentifier(tableName), where)

	count := 0
	cells := make([]sql.NullString, len(names))
	targets := make([]interface{}, 0, len(names)+1)
	targets = append(targets, &count)
	for i := range cells {
		targets = append(targets, &cells[i])
	}

	if err := s.driver.DB().QueryRowContext(ctx, q, values...).Scan(targets...); err != nil {
		return 0, nil, fmt.Errorf("failed summarizing %s: %w", tableName, err)
	}

	out := make(map[string]interface{}, len(names))
	for i, name := range names {
		out[name] = summaryValue(cells[i])
	}
	return count, out, nil
}

// summaryGroups works out one row per group, in the order the break fields
// sort, so the report reads top to bottom.
func (s *Service) summaryGroups(
	ctx context.Context, dialect dbal.Dialect, tableName, where string, values []interface{},
	names []string, specs map[string]SummarySpec, groupBy []string,
) ([]SummaryGroup, error) {
	groupCols := make([]string, 0, len(groupBy))
	for _, field := range groupBy {
		groupCols = append(groupCols, dialect.QuoteIdentifier(field))
	}

	selects := append([]string{}, groupCols...)
	selects = append(selects, "COUNT(*)")
	for _, name := range names {
		selects = append(selects, aggregateSQL(dialect, specs[name]))
	}

	q := fmt.Sprintf("SELECT %s FROM %s %s GROUP BY %s ORDER BY %s",
		strings.Join(selects, ", "),
		dialect.QuoteIdentifier(tableName),
		where,
		strings.Join(groupCols, ", "),
		strings.Join(groupCols, ", "),
	)

	rows, err := s.driver.DB().QueryContext(ctx, q, values...)
	if err != nil {
		return nil, fmt.Errorf("failed grouping %s: %w", tableName, err)
	}
	defer rows.Close()

	groups := make([]SummaryGroup, 0)
	for rows.Next() {
		breaks := make([]sql.NullString, len(groupBy))
		count := 0
		cells := make([]sql.NullString, len(names))

		targets := make([]interface{}, 0, len(groupBy)+len(names)+1)
		for i := range breaks {
			targets = append(targets, &breaks[i])
		}
		targets = append(targets, &count)
		for i := range cells {
			targets = append(targets, &cells[i])
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}

		group := SummaryGroup{
			Values:    make(map[string]interface{}, len(groupBy)),
			Count:     count,
			Summaries: make(map[string]interface{}, len(names)),
		}
		for i, field := range groupBy {
			if breaks[i].Valid {
				group.Values[field] = dbal.TrimNumericText(breaks[i].String)
			} else {
				group.Values[field] = nil
			}
		}
		for i, name := range names {
			group.Summaries[name] = summaryValue(cells[i])
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

// applyFractions turns the totals of the fraction-of-total fields into their
// share of the grand total, which is what the field means.
func applyFractions(result *SummaryResult, names []string, specs map[string]SummarySpec) {
	for _, name := range names {
		if specs[name].Type != SummaryFractionOfTotal {
			continue
		}
		total, ok := numberOf(result.Grand[name])
		if !ok || total == 0 {
			// Nothing to take a share of: every share is empty rather than a
			// division by zero reported as a figure.
			result.Grand[name] = nil
			for i := range result.Groups {
				result.Groups[i].Summaries[name] = nil
			}
			continue
		}
		result.Grand[name] = "1"
		for i := range result.Groups {
			part, ok := numberOf(result.Groups[i].Summaries[name])
			if !ok {
				result.Groups[i].Summaries[name] = nil
				continue
			}
			result.Groups[i].Summaries[name] = strconv.FormatFloat(part/total, 'f', -1, 64)
		}
	}
}

// summaryValue reads an aggregate back. Both engines return these as decimal
// text; it is kept as text so a number never passes through a float on its way
// to the client (#18), with the trailing zeros a DECIMAL carries trimmed off.
func summaryValue(cell sql.NullString) interface{} {
	if !cell.Valid {
		return nil
	}
	return dbal.TrimNumericText(cell.String)
}

func numberOf(v interface{}) (float64, bool) {
	text, ok := v.(string)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(text, 64)
	return n, err == nil
}

// dropSummaryValues removes from a record anything sent for a summary field.
//
// A summary has no value in a record: it is worked out over the found set when
// a report is drawn. Its column exists but is never written, so a value sent
// for one is dropped rather than stored where nothing would ever read it.
func (s *Service) dropSummaryValues(ctx context.Context, tableName string, record map[string]interface{}) error {
	if len(record) == 0 {
		return nil
	}
	// The field type decides this, not the definition: a summary field that
	// has not been defined yet is still one.
	fields, err := s.summaryFieldNames(ctx, tableName)
	if err != nil {
		return err
	}
	for name := range fields {
		delete(record, name)
	}
	return nil
}

// summaryFieldNames are the names of the table's summary fields, defined or
// not.
func (s *Service) summaryFieldNames(ctx context.Context, tableName string) (map[string]struct{}, error) {
	dialect := s.driver.Dialect()
	q := `SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = ` + dialect.Placeholder(1) + ` AND c.field_type = ` + dialect.Placeholder(2)

	rows, err := s.driver.DB().QueryContext(ctx, q, tableName, string(dbal.FieldTypeSummary))
	if err != nil {
		return nil, fmt.Errorf("failed reading the summary fields of %s: %w", tableName, err)
	}
	defer rows.Close()

	names := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = struct{}{}
	}
	return names, rows.Err()
}
