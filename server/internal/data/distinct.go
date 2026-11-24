package data

import (
	"context"
	"fmt"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// DistinctQuery is what a value list taken from a field reads from every
// record of its table (#31).
type DistinctQuery struct {
	// Field holds the values an entry stores. A record without one offers
	// nothing.
	Field string
	// Label is an optional second field of the same table, read beside Field.
	Label string
	// LabelOnly gives one entry per label, holding the least value of the
	// records that show it: what a list showing only its second field offers.
	// Without it each distinct pair of values is an entry.
	LabelOnly bool
	// ByLabel orders the entries by their label, then their value; otherwise
	// by their value, then their label.
	ByLabel bool
	// Limit is the most entries read.
	Limit int
}

// DistinctValues reads, as the caller, the distinct entries q describes from
// the records of tableName, the first q.Limit of them in q's order. more is
// true when there are others past those. The grouping, the order and the limit
// are applied by the database, so a value repeated across many records never
// hides another. Each value is given as the text a client shows for its field
// (fieldText). A table the caller may not read is ErrNoAccess.
func (s *Service) DistinctValues(ctx context.Context, tableName string, q DistinctQuery) (rows []map[string]string, more bool, err error) {
	if err := s.newRelatedReader().checkRead(ctx, tableName); err != nil {
		return nil, false, err
	}
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, false, err
	}
	names := []string{q.Field}
	if q.Label != "" {
		names = append(names, q.Label)
	}
	for _, name := range names {
		if err := checkField(fields, tableName, name); err != nil {
			return nil, false, err
		}
	}

	dialect := s.driver.Dialect()
	value := dialect.QuoteIdentifier(q.Field)
	where := value + " IS NOT NULL"
	if fields[q.Field] == dbal.FieldTypeText {
		where += fmt.Sprintf(" AND TRIM(%s) <> ''", value)
	}
	selected, group, order := value, value, "1"
	if q.Label != "" {
		label := dialect.QuoteIdentifier(q.Label)
		selected, group, order = value+", "+label, value+", "+label, "1, 2"
		if q.LabelOnly {
			selected, group = fmt.Sprintf("MIN(%s) AS %s, %s", value, value, label), label
		}
		if q.ByLabel {
			order = "2, 1"
		}
	}
	sqlText := fmt.Sprintf("SELECT %s FROM %s WHERE %s GROUP BY %s ORDER BY %s LIMIT %d",
		selected, dialect.QuoteIdentifier(tableName), where, group, order, q.Limit+1)

	result, err := s.driver.DB().QueryContext(ctx, sqlText)
	if err != nil {
		return nil, false, fmt.Errorf("failed reading the values of %s.%s: %w", tableName, q.Field, err)
	}
	defer result.Close()
	read, err := rowsToMaps(result, fields)
	if err != nil {
		return nil, false, err
	}
	if len(read) > q.Limit {
		read, more = read[:q.Limit], true
	}
	rows = make([]map[string]string, len(read))
	for i, row := range read {
		rows[i] = make(map[string]string, len(names))
		for _, name := range names {
			rows[i][name] = fieldText(fields[name], row[name])
		}
	}
	return rows, more, nil
}

// fieldText is a value read for the JSON API as the text a client shows for
// its field and writes back when it is chosen: a date as YYYY-MM-DD, as a
// date field shows it, a timestamp in RFC 3339 as the API writes it, a number
// without the zeros its column pads it with.
func fieldText(fieldType dbal.AgnosticFieldType, v interface{}) string {
	var text string
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		if fieldType == dbal.FieldTypeDate {
			return t.Format("2006-01-02")
		}
		return t.Format(time.RFC3339Nano)
	case []byte:
		text = string(t)
	default:
		text = fmt.Sprint(t)
	}
	switch fieldType {
	case dbal.FieldTypeDate:
		// A DATE can come back as a full timestamp.
		if len(text) > 10 && text[10] == 'T' {
			text = text[:10]
		}
	case dbal.FieldTypeNumber:
		text = dbal.TrimNumericText(text)
	}
	return text
}
