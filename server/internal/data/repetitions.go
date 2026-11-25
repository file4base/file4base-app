package data

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
)

// Repeating fields.
//
// A record carries repetition n (from 2) of a repeating field under the key
// "field[n]"; repetition 1 is the field itself, so a client that knows
// nothing of repetitions reads and writes what it always did. Only the
// repetitions that hold a value are returned. Repetitions 2 to N are stored
// together in the field's companion column (see the repetition package).
//
// In a fieldSet, a repeating field has a second entry under
// repetitionsKey(field), whose type carries the maximum: selectList reads the
// companion column, and rowsToMaps spreads it into the record's "field[n]"
// keys. Code that only knows plain fields never sees it:
// checkField refuses it, and nothing else names it.

// repetitionsKey is the fieldSet and record key of a repeating field's
// repetitions 2 to N. "[]" is in no field name, so it cannot clash with one.
func repetitionsKey(field string) string { return field + "[]" }

const repeatsPrefix = "REPETITIONS:"

// repeatsType is the type of a repetitionsKey entry: the field's maximum.
func repeatsType(max int) dbal.AgnosticFieldType {
	return dbal.AgnosticFieldType(repeatsPrefix + strconv.Itoa(max))
}

// repeatsOf reads a repetitionsKey entry's maximum.
func repeatsOf(t dbal.AgnosticFieldType) (int, bool) {
	rest, ok := strings.CutPrefix(string(t), repeatsPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// maxRepetitions is how many repetitions a field has: 1 when it does not
// repeat.
func (f fieldSet) maxRepetitions(field string) int {
	if n, ok := repeatsOf(f[repetitionsKey(field)]); ok && n > 1 {
		return n
	}
	return 1
}

// repeating lists the repeating fields and their maximum.
func (f fieldSet) repeating() map[string]int {
	var out map[string]int
	for key, t := range f {
		if n, ok := repeatsOf(t); ok && n > 1 {
			if out == nil {
				out = map[string]int{}
			}
			out[strings.TrimSuffix(key, "[]")] = n
		}
	}
	return out
}

// companionColumns maps the physical companion column of each repeating
// field, as a query reads it, to its repetitionsKey. "$" is in no field
// name, so a companion column cannot be taken for a field.
func (f fieldSet) companionColumns() map[string]string {
	out := map[string]string{}
	for key := range f {
		if f.isInternal(key) {
			out[repetition.Column(strings.TrimSuffix(key, "[]"))] = key
		}
	}
	return out
}

// isInternal reports whether a fieldSet key is a repetitions entry rather
// than a field.
func (f fieldSet) isInternal(key string) bool {
	_, ok := repeatsOf(f[key])
	return ok
}

// typeOf is the type of the value under a record key: the field's own, for
// a repetition of it.
func (f fieldSet) typeOf(key string) dbal.AgnosticFieldType {
	if t, ok := f[key]; ok {
		return t
	}
	if base, _, ok := repetition.Split(key); ok {
		return f[base]
	}
	return ""
}

// checkValueKey validates a key of a record: a field, or a repetition of a
// repeating field within its maximum.
func checkValueKey(fields fieldSet, tableName, key string) error {
	if err := checkField(fields, tableName, key); err == nil {
		return nil
	}
	if base, n, ok := repetition.Split(key); ok && !fields.isInternal(base) {
		if _, known := fields[base]; known && n <= fields.maxRepetitions(base) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s.%s", ErrUnknownField, tableName, key)
}

// normalizeRepetitionKeys checks the keys of a record a client sent and makes
// "field[1]" the field itself.
func normalizeRepetitionKeys(fields fieldSet, tableName string, record map[string]interface{}) error {
	for key, value := range record {
		if err := checkValueKey(fields, tableName, key); err != nil {
			return err
		}
		if base, n, ok := repetition.Split(key); ok && n == 1 {
			if _, both := record[base]; both {
				return fmt.Errorf("%w: %s.%s and %s.%s are the same repetition", ErrInvalidValue, tableName, base, tableName, key)
			}
			delete(record, key)
			record[base] = value
		}
	}
	return nil
}

// columnSQL is the quoted column a record key is written to.
func columnSQL(dialect dbal.Dialect, key string) string {
	if base, ok := strings.CutSuffix(key, "[]"); ok {
		return dialect.QuoteIdentifier(repetition.Column(base))
	}
	return dialect.QuoteIdentifier(key)
}

// decodeRepetitions spreads the companion column of repetitionsKey key
// into the record's "field[n]" keys, the values typed as the field reads.
// Repetitions past the field's maximum, kept from a time it was higher, are
// not shown.
func decodeRepetitions(fields fieldSet, key string, raw interface{}, record map[string]interface{}) error {
	max, _ := repeatsOf(fields[key])
	base := strings.TrimSuffix(key, "[]")
	values, err := repetition.Unpack(fields[base], max, raw)
	if err != nil {
		return fmt.Errorf("%w: the repetitions of %s cannot be read: %v", ErrInvalidValue, base, err)
	}
	for n, value := range values {
		record[repetition.Key(base, n)] = value
	}
	return nil
}

// packRepetitions is the record as its statement writes it: the "field[n]"
// keys of each repeating field are gathered into its companion column, under
// repetitionsKey, and a date or timestamp given as text is bound as the value
// every engine stores (dbal.TemporalValue). The record itself is not changed.
//
// An update changes only the repetitions it names, so for a record that
// exists (id not empty) the others are read from it first, through q.
func packRepetitions(
	ctx context.Context, q execer, dialect dbal.Dialect, fields fieldSet, tableName, id string,
	record map[string]interface{},
) (map[string]interface{}, error) {
	written := map[string]map[int]interface{}{}
	out := make(map[string]interface{}, len(record))
	for key, value := range record {
		base, n, ok := repetition.Split(key)
		if !ok || fields.isInternal(base) {
			out[key] = dbal.TemporalValue(fields[key], value)
			continue
		}
		if written[base] == nil {
			written[base] = map[int]interface{}{}
		}
		written[base][n] = value
	}
	if len(written) == 0 {
		return out, nil
	}

	var stored map[string]interface{}
	if id != "" {
		var err error
		if stored, err = storedRepetitions(ctx, q, dialect, tableName, id, written); err != nil {
			return nil, err
		}
	}
	for base, values := range written {
		delete(values, 1) // repetition 1 is the field's own column, not the companion's
		encoded, err := repetition.Merge(fields[base], fields.maxRepetitions(base), stored[base], values)
		if err != nil {
			return nil, fmt.Errorf("%w: %s.%s: %v", ErrInvalidValue, tableName, base, err)
		}
		out[repetitionsKey(base)] = encoded
	}
	return out, nil
}

// storedRepetitions reads the companion columns of the given fields as the
// record holds them now, by field: whole, including repetitions past the
// current maximum, which an update keeps.
func storedRepetitions(
	ctx context.Context, q execer, dialect dbal.Dialect, tableName, id string,
	of map[string]map[int]interface{},
) (map[string]interface{}, error) {
	bases := make([]string, 0, len(of))
	for base := range of {
		bases = append(bases, base)
	}
	sort.Strings(bases)
	cols := make([]string, len(bases))
	for i, base := range bases {
		cols[i] = columnSQL(dialect, repetitionsKey(base))
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		strings.Join(cols, ", "), dialect.QuoteIdentifier(tableName), dialect.QuoteIdentifier("id"), dialect.Placeholder(1))
	raw := make([]interface{}, len(bases))
	ptrs := make([]interface{}, len(bases))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := q.QueryRowContext(ctx, query, id).Scan(ptrs...); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil // an update of nothing reports that itself
		}
		return nil, fmt.Errorf("failed reading the repetitions of %s: %w", tableName, err)
	}
	stored := make(map[string]interface{}, len(bases))
	for i, base := range bases {
		stored[base] = raw[i]
	}
	return stored, nil
}

// splitRepetitionText spreads a value imported into a repeating field over
// its repetitions: FileMaker separates repetitions with the group separator
// (0x1D) in tab-separated and comma-separated files, and File4Base's XML
// reader joins a field's DATA elements with it.
func splitRepetitionText(text string, max int) []string {
	parts := strings.Split(text, "\x1d")
	if len(parts) > max {
		parts = parts[:max]
	}
	return parts
}

// joinRepetitionText writes every repetition of a field as one exported
// cell, the repetitions separated by the group separator as FileMaker
// exports them; trailing empty repetitions are left out. A repetition kept
// with its original text (#124) is written as that text.
func joinRepetitionText(fieldType dbal.AgnosticFieldType, field string, reps int, row map[string]interface{}, originals map[string]string) string {
	parts := make([]string, reps)
	last := 0
	for n := 1; n <= reps; n++ {
		key := repetition.Key(field, n)
		if text, ok := originals[key]; ok {
			parts[n-1] = text
		} else {
			parts[n-1] = exportText(fieldType, row[key])
		}
		if parts[n-1] != "" {
			last = n
		}
	}
	return strings.Join(parts[:max(last, 1)], "\x1d")
}

// fillRepetitions computes repetitions 2 to N of an unstored repeating
// calculation into row, the first being computed by the caller, and keeps
// them with it under key so a record reached again gets them too. A
// repetition that fails is empty, as the first is.
func (r *relatedReader) fillRepetitions(key string, c calculation, env calc.Context, row map[string]interface{}) {
	for rep := 2; rep <= c.reps; rep++ {
		k := repetition.Key(c.field, rep)
		answer, err := calc.EvaluateRepetition(c.expr, env, c.result, rep)
		if err != nil || answer == nil {
			delete(row, k)
			r.values[key+"["+strconv.Itoa(rep)+"]"] = nil
			continue
		}
		answer = rowValue(answer, c.types[c.field])
		row[k] = answer
		r.values[key+"["+strconv.Itoa(rep)+"]"] = answer
	}
}

// restoreRepetitions puts back the repetitions fillRepetitions kept.
func (r *relatedReader) restoreRepetitions(key string, c calculation, row map[string]interface{}) {
	for rep := 2; rep <= c.reps; rep++ {
		if v := r.values[key+"["+strconv.Itoa(rep)+"]"]; v != nil {
			row[repetition.Key(c.field, rep)] = v
		}
	}
}
