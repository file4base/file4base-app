package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/containermeta"
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
// (fieldText), and a container as the name of its file, which is the text
// FileMaker gives a container: a container whose file name is not known
// offers nothing. A table the caller may not read is ErrNoAccess.
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
	source := dialect.QuoteIdentifier("src")
	var joins []string
	var args []interface{}
	// read is the type each field is read back as: a container is read as
	// its file name, which is text.
	read := fieldSet{}
	// column is the expression a field is read with.
	column := func(name string) string {
		if fields[name] != dbal.FieldTypeContainer {
			read[name] = fields[name]
			return source + "." + dialect.QuoteIdentifier(name)
		}
		read[name] = dbal.FieldTypeText
		meta := dialect.QuoteIdentifier(fmt.Sprintf("meta%d", len(joins)+1))
		joins = append(joins, fmt.Sprintf(" LEFT JOIN %s %s ON %s.table_name = %s AND %s.field_name = %s AND %s.record_id = %s.%s",
			containermeta.Table, meta, meta, dialect.Placeholder(len(args)+1), meta, dialect.Placeholder(len(args)+2),
			meta, source, dialect.QuoteIdentifier("id")))
		args = append(args, tableName, name)
		return meta + ".filename"
	}

	value := column(q.Field)
	where := value + " IS NOT NULL"
	if read[q.Field] == dbal.FieldTypeText {
		where += fmt.Sprintf(" AND TRIM(%s) <> ''", value)
	}
	if fields[q.Field] == dbal.FieldTypeContainer {
		where = fmt.Sprintf("%s.%s IS NOT NULL AND %s", source, dialect.QuoteIdentifier(q.Field), where)
	}
	as := func(expr, name string) string { return expr + " AS " + dialect.QuoteIdentifier(name) }
	selected, group, order := as(value, q.Field), value, "1"
	if q.Label != "" {
		label := column(q.Label)
		selected, group, order = as(value, q.Field)+", "+as(label, q.Label), value+", "+label, "1, 2"
		if q.LabelOnly {
			selected, group = as(leastValue(fields[q.Field], value), q.Field)+", "+as(label, q.Label), label
		}
		if q.ByLabel {
			order = "2, 1"
		}
	}
	sqlText := fmt.Sprintf("SELECT %s FROM %s %s%s WHERE %s GROUP BY %s ORDER BY %s LIMIT %d",
		selected, dialect.QuoteIdentifier(tableName), source, strings.Join(joins, ""), where, group, order, q.Limit+1)

	result, err := s.driver.DB().QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed reading the values of %s.%s: %w", tableName, q.Field, err)
	}
	defer result.Close()
	found, err := rowsToMaps(result, read)
	if err != nil {
		return nil, false, err
	}
	if len(found) > q.Limit {
		found, more = found[:q.Limit], true
	}
	rows = make([]map[string]string, len(found))
	for i, row := range found {
		rows[i] = make(map[string]string, len(names))
		for _, name := range names {
			rows[i][name] = fieldText(read[name], row[name])
		}
	}
	return rows, more, nil
}

// leastValue is the least of the values of a group, the one an entry of a
// list showing only its second field stores. PostgreSQL has no MIN of a
// boolean, so a Boolean is ranked as 0 or 1, which reads back as false or
// true.
func leastValue(fieldType dbal.AgnosticFieldType, expr string) string {
	if fieldType == dbal.FieldTypeBoolean {
		return fmt.Sprintf("MIN(CASE WHEN %s THEN 1 ELSE 0 END)", expr)
	}
	return fmt.Sprintf("MIN(%s)", expr)
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
