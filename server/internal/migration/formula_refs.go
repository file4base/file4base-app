package migration

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// #111 — a calculation has to keep pointing at the fields it was written
// against, even when their names had to change on the way in.
//
// A source field named `_code` is imported as the column `code`, because
// sanitizeIdentifier strips the leading underscore, and two names that
// normalize alike are told apart as `code` and `code_2` (#89). The formula of
// a calculation field was stored exactly as the source wrote it, so
// `Mirror = _code` arrived naming a column the imported table does not have:
// preflight reported two preserved fields and nothing unsupported, and the
// apply then failed with "the formula of \"mirror\" reads \"_code\", which
// this table does not have" — a conversion reported as faithful that cannot
// be applied.
//
// Parsing the formula is not enough to know it will work: the parser accepts
// any identifier, so a reference to a field that does not exist parses
// perfectly. The names a formula reads are therefore resolved against the
// table's complete source-to-target field map, and a formula naming something
// that map cannot place is quarantined with the reference in the report rather
// than being carried to an apply that will fail.

// fieldNameMap is every name a table's fields can be named by in a formula,
// mapped to the column the import gave them.
//
// Two source names can differ only in case — `Code` and `code` — and still be
// two fields, the second of which is imported as `code_2` (#89). Matching
// case-insensitively alone would make such a pair ambiguous, so a name is
// resolved by its exact spelling first and only then case-insensitively.
type fieldNameMap struct {
	exact map[string]string
	fold  map[string]string
}

func newFieldNameMap() *fieldNameMap {
	return &fieldNameMap{exact: map[string]string{}, fold: map[string]string{}}
}

// claim records that source is imported as column. A name a source field
// already claims is never taken over by another field's imported name.
func (m *fieldNameMap) claim(source, column string, authoritative bool) {
	if _, taken := m.exact[source]; !taken || authoritative {
		m.exact[source] = column
	}
	lower := strings.ToLower(source)
	if _, taken := m.fold[lower]; !taken || authoritative {
		m.fold[lower] = column
	}
}

func (m *fieldNameMap) resolve(name string) (string, bool) {
	if column, ok := m.exact[name]; ok {
		return column, true
	}
	column, ok := m.fold[strings.ToLower(name)]
	return column, ok
}

// remapTableCalculations resolves every calculation of a converted table
// against the names the table ended up with, once all of its fields are known.
//
// A formula whose references all resolve is rewritten in place, and reported
// as translated when that changed it. A formula naming something the table
// does not have is **quarantined**: the column keeps the type the calculation
// would have produced and stores a plain value, and the report says which
// reference could not be placed — the alternative is an apply that fails on a
// field reported as preserved.
func remapTableCalculations(
	tableSQL string,
	table *schema.BundleTable,
	renames *fieldNameMap,
	colTypeByTableCol map[string]string,
) []ReportItem {
	var items []ReportItem
	for i := range table.Columns {
		col := &table.Columns[i]
		if col.FieldType != "CALCULATION" || col.CalculationFormula == nil {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(*col.CalculationFormula), &payload); err != nil {
			continue
		}
		formula, _ := payload["formula"].(string)
		if strings.TrimSpace(formula) == "" {
			continue
		}
		resultType, _ := payload["result_type"].(string)

		rewritten, unresolved := remapFormulaFields(formula, renames)
		source := tableSQL + "." + col.DisplayName
		target := tableSQL + "." + col.Name

		if len(unresolved) > 0 {
			col.FieldType = mapBasicFieldType(resultType)
			col.CalculationFormula = nil
			colTypeByTableCol[tableSQL+"."+col.Name] = col.FieldType
			items = append(items,
				ReportItem{
					Category:   CategoryUnsupported,
					Domain:     "field",
					SourceName: source,
					TargetName: target,
					Detail: fmt.Sprintf(
						"Calculation %q reads %s, which this table does not have; converted to a stored %s column instead of being applied",
						formula, quotedList(unresolved), col.FieldType),
				},
				ReportItem{
					Category:   CategoryManualActionRequired,
					Domain:     "field",
					SourceName: source,
					TargetName: target,
					Detail: fmt.Sprintf(
						"Repoint the calculation of %s at the fields it should read, or add the missing %s",
						target, quotedList(unresolved)),
				})
			continue
		}
		if rewritten == formula {
			continue
		}
		payload["formula"] = rewritten
		encoded, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		stored := string(encoded)
		col.CalculationFormula = &stored
		items = append(items, ReportItem{
			Category:   CategoryTranslated,
			Domain:     "field",
			SourceName: source,
			TargetName: target,
			Detail: fmt.Sprintf(
				"Calculation references repointed at the imported field names: %q now reads %q",
				formula, rewritten),
		})
	}
	return items
}

// quotedList renders names as "a", "b" and "c" for a report a person reads.
func quotedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}

// remapFormulaFields rewrites the field references of a formula from the names
// its source used to the names the imported table has.
//
// Only the identifiers the formula reads as fields are rewritten. A function
// name is left alone, and so is everything inside quoted text, so
// `If(_code = "_code"; _code; "")` keeps its literal and renames only the two
// references. The formula is returned unchanged when there is nothing to
// rewrite, and [unresolved] lists, sorted and without duplicates, every
// reference the map could not place — in which case the formula is returned as
// it came, for the caller to quarantine.
func remapFormulaFields(formula string, renames *fieldNameMap) (rewritten string, unresolved []string) {
	expr, err := calc.Parse(formula)
	if err != nil {
		// The caller only remaps a formula the parser already accepted; a
		// formula that does not parse is quarantined before it gets here.
		return formula, nil
	}

	rename := map[string]string{}
	seenUnresolved := map[string]bool{}
	for _, name := range expr.Fields() {
		target, ok := renames.resolve(name)
		if !ok {
			if !seenUnresolved[name] {
				seenUnresolved[name] = true
				unresolved = append(unresolved, name)
			}
			continue
		}
		if target != name {
			rename[name] = target
		}
	}
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return formula, unresolved
	}
	if len(rename) == 0 {
		return formula, nil
	}
	return rewriteIdentifiers(formula, rename), nil
}

// rewriteIdentifiers replaces the identifiers named in rename — keyed by the
// exact spelling the formula uses — wherever the formula reads them as a
// value, leaving quoted text and function names as they are.
func rewriteIdentifiers(formula string, rename map[string]string) string {
	runes := []rune(formula)
	var b strings.Builder
	b.Grow(len(formula))

	for i := 0; i < len(runes); {
		ch := runes[i]
		switch {
		case ch == '"' || ch == '\'':
			// A text literal, where a doubled quote is an escaped quote and
			// not the end of the literal, exactly as the lexer reads it.
			quote := ch
			b.WriteRune(ch)
			i++
			for i < len(runes) {
				if runes[i] == quote {
					if i+1 < len(runes) && runes[i+1] == quote {
						b.WriteRune(quote)
						b.WriteRune(quote)
						i += 2
						continue
					}
					b.WriteRune(quote)
					i++
					break
				}
				b.WriteRune(runes[i])
				i++
			}

		case ch == '_' || unicode.IsLetter(ch):
			start := i
			for i < len(runes) && (runes[i] == '_' || unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			word := string(runes[start:i])
			// An identifier a "(" follows is the name of a function, not a
			// field: TODAY() must stay TODAY() whatever the fields are called.
			j := i
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			if j < len(runes) && runes[j] == '(' {
				b.WriteString(word)
				continue
			}
			if target, ok := rename[word]; ok {
				b.WriteString(target)
				continue
			}
			b.WriteString(word)

		default:
			b.WriteRune(ch)
			i++
		}
	}
	return b.String()
}
