package migration

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

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
	// sources are the names the source gave the table's fields, in order.
	sources []string
}

func newFieldNameMap() *fieldNameMap {
	return &fieldNameMap{exact: map[string]string{}, fold: map[string]string{}}
}

// claim records that source is imported as column. A name a source field
// already claims is never taken over by another field's imported name.
func (m *fieldNameMap) claim(source, column string, authoritative bool) {
	if authoritative {
		m.sources = append(m.sources, source)
	}
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

// formulaBinding is what a calculation is read against: the names the source
// gave the fields of every table, its table occurrences and the locale it
// writes numbers in.
type formulaBinding struct {
	fields      map[string]*fieldNameMap // imported table name -> its field names
	occurrences map[string][]string      // occurrence name -> source names of its table's fields
	locale      *calc.Locale             // nil when source_locale was not named
}

func newFormulaBinding(bundle *schema.SolutionBundle, fields map[string]*fieldNameMap, loc *sourceLocale) formulaBinding {
	tableByID := make(map[string]string, len(bundle.Tables))
	for _, t := range bundle.Tables {
		tableByID[t.ID] = t.Name
	}
	b := formulaBinding{fields: fields, occurrences: map[string][]string{}, locale: formulaLocale(loc)}
	for _, occ := range bundle.TableOccurrences {
		if names := fields[tableByID[occ.BaseTableID]]; names != nil {
			b.occurrences[occ.Name] = names.sources
		}
	}
	return b
}

// formulaLocale is the locale a formula of the source file is written in,
// which decides whether 1,5 is one number or two arguments.
func formulaLocale(loc *sourceLocale) *calc.Locale {
	if loc == nil {
		return nil
	}
	if l, ok := calc.LocaleFor(loc.Tag); ok {
		return &l
	}
	l := calc.ISOLocale
	l.DecimalSeparator = string(loc.Decimal)
	return &l
}

// bindTableCalculations reads every calculation of a converted table as
// FileMaker wrote it, once all tables and table occurrences are known, and
// rewrites its field references to the names the import gave them (#111).
//
// The formula is read with the source's own names, so a field called
// "Prix HT" or a French 1,5 is understood. A formula that cannot be applied —
// it does not read, it names a field this table does not have, a field that
// was deleted in FileMaker, or a related field — is **quarantined**: the
// column keeps the type the calculation would have produced and stores a
// plain value, and the report says why.
func bindTableCalculations(table *schema.BundleTable, b formulaBinding, colTypeByTableCol map[string]string) []ReportItem {
	names := b.fields[table.Name]
	if names == nil {
		names = newFieldNameMap()
	}
	var local []string
	for _, name := range names.sources {
		// A plain word is read as written, which keeps "Code" and "code"
		// apart (#89); only a name a plain word cannot hold is declared.
		if calc.QuoteName(name) != name {
			local = append(local, name)
		}
	}
	opts := calc.Options{Fields: local, Tables: b.occurrences, Locale: b.locale}

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
		resultType, _ := payload["result_type"].(string)
		source := table.Name + "." + col.DisplayName
		target := table.Name + "." + col.Name

		quarantine := func(why, action string) {
			col.FieldType = mapBasicFieldType(resultType)
			col.CalculationFormula = nil
			colTypeByTableCol[target] = col.FieldType
			items = append(items,
				ReportItem{Category: CategoryUnsupported, Domain: "field", SourceName: source, TargetName: target,
					Detail: fmt.Sprintf("Calculation %q %s; converted to a stored %s column instead of being applied", formula, why, col.FieldType)},
				ReportItem{Category: CategoryManualActionRequired, Domain: "field", SourceName: source, TargetName: target,
					Detail: action})
		}

		expr, err := calc.ParseWith(formula, opts)
		if err != nil {
			why := fmt.Sprintf("could not be read (%v)", err)
			if b.locale == nil && reCommaDecimal.MatchString(formula) {
				why += "; it seems to write decimals with a comma, so name the locale of the source file (source_locale, such as fr-FR) and migrate again"
			}
			quarantine(why, fmt.Sprintf("Rewrite the calculation of %s in File4Base, or correct it in FileMaker and migrate again", target))
			continue
		}
		var missing, related []string
		for _, ref := range expr.RelatedFields() {
			name := ref.Table + "::" + ref.Field
			if ref.Missing {
				missing = append(missing, name)
			} else {
				related = append(related, name)
			}
		}
		if len(missing) > 0 {
			quarantine(fmt.Sprintf("reads %s, a table or field that was deleted in FileMaker", quotedList(missing)),
				fmt.Sprintf("Point the calculation of %s at an existing field, or remove that part of it", target))
			continue
		}
		if slices.Contains(expr.Functions(), "GETLAYOUTOBJECTATTRIBUTE") {
			quarantine("reads a layout object with GetLayoutObjectAttribute, which a field stored with its record cannot do",
				fmt.Sprintf("Compute %s from fields instead of from a layout object, or fill it with a script", target))
			continue
		}
		if len(related) > 0 {
			quarantine(fmt.Sprintf("reads the related field %s, and calculations cannot read related fields in File4Base yet", quotedList(related)),
				fmt.Sprintf("Recreate the calculation of %s once related fields can be read, or fill it with a script", target))
			continue
		}

		rewritten, unresolved, err := calc.Rewrite(formula, opts, func(_, field string) (string, bool) {
			column, ok := names.resolve(field)
			return calc.QuoteName(column), ok
		})
		if err != nil {
			quarantine(fmt.Sprintf("could not be read (%v)", err),
				fmt.Sprintf("Rewrite the calculation of %s in File4Base", target))
			continue
		}
		if len(unresolved) > 0 {
			var refs []string
			seen := map[string]bool{}
			for _, ref := range unresolved {
				if !seen[ref.Field] {
					seen[ref.Field] = true
					refs = append(refs, ref.Field)
				}
			}
			quarantine(fmt.Sprintf("reads %s, which this table does not have", quotedList(refs)),
				fmt.Sprintf("Repoint the calculation of %s at the fields it should read, or add the missing %s", target, quotedList(refs)))
			continue
		}

		payload["formula"] = rewritten
		encoded, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		stored := string(encoded)
		col.CalculationFormula = &stored
		if rewritten == formula {
			items = append(items, ReportItem{Category: CategoryPreserved, Domain: "field", SourceName: source, TargetName: target,
				Detail: fmt.Sprintf("Calculation field (%s): %s", resultType, formula)})
			continue
		}
		items = append(items, ReportItem{Category: CategoryTranslated, Domain: "field", SourceName: source, TargetName: target,
			Detail: fmt.Sprintf("Calculation bound to the imported fields: %q now reads %q", formula, rewritten)})
	}
	return items
}

// reCommaDecimal finds a number written with a decimal comma, such as 1,5.
var reCommaDecimal = regexp.MustCompile(`\d,\d`)

// formulaText is the formula of a calculation as the source wrote it. A
// File4Base calculation specification ({"formula": ...}) is unwrapped.
func formulaText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		for _, key := range []string{"formula", "expression"} {
			if expr, ok := parsed[key].(string); ok && strings.TrimSpace(expr) != "" {
				return strings.TrimSpace(expr)
			}
		}
	}
	return trimmed
}

// ownTableFormula is the formula of a Database Design Report calculation when
// it reads only plain fields of its own table, the only kind that report's
// route can apply.
func ownTableFormula(raw string) (string, bool) {
	formula := formulaText(raw)
	if formula == "" {
		return "", false
	}
	expr, err := calc.Parse(formula)
	if err != nil || len(expr.RelatedFields()) > 0 {
		return formula, false
	}
	return formula, true
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
