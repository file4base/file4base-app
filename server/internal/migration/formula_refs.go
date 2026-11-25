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
// gave the fields of every table, its table occurrences and the relationships
// between them, and the locale it writes numbers in.
type formulaBinding struct {
	fields      map[string]*fieldNameMap // imported table name -> its field names
	occurrences map[string][]string      // occurrence name -> source names of its table's fields
	occByName   map[string]boundOccurrence
	occByTable  map[string][]boundOccurrence // in the order the graph declares them
	links       map[string][]string          // occurrence id -> the occurrences related to it
	// matchColumns are the columns (by id) a relationship matches on.
	matchColumns map[string]bool
	locale       *calc.Locale // nil when source_locale was not named
	// functions are the solution's custom functions, as the source wrote
	// them, which its formulas are read with, and stored the same functions
	// as they were imported (see bindCustomFunctions). dropped are the ones
	// that could not be, by upper-case name.
	functions, stored *calc.Library
	dropped           map[string]string
}

// boundOccurrence is an imported table occurrence: its name and its table.
// An occurrence of another file's table has no table of this file.
type boundOccurrence struct {
	id, name, table string
	external        bool
}

// newFormulaBinding reads the converted bundle's occurrences and
// relationships. externalFields are the source names of the fields used
// through each occurrence of another file's table (by lower-cased name),
// which is all the document says of that table.
func newFormulaBinding(bundle *schema.SolutionBundle, fields map[string]*fieldNameMap, loc *sourceLocale, externalFields map[string][]string) formulaBinding {
	tableByID := make(map[string]string, len(bundle.Tables))
	for _, t := range bundle.Tables {
		tableByID[t.ID] = t.Name
	}
	b := formulaBinding{
		fields: fields, occurrences: map[string][]string{}, locale: formulaLocale(loc),
		occByName: map[string]boundOccurrence{}, occByTable: map[string][]boundOccurrence{}, links: map[string][]string{},
		matchColumns: map[string]bool{},
	}
	for _, occ := range bundle.TableOccurrences {
		if occ.External != nil {
			// Its fields are named as the other file's migration names
			// them; the binding checks they are there.
			b.occurrences[occ.Name] = externalFields[strings.ToLower(occ.Name)]
			b.occByName[strings.ToLower(occ.Name)] = boundOccurrence{id: occ.ID, name: occ.Name, external: true}
			continue
		}
		table := tableByID[occ.BaseTableID]
		if names := fields[table]; names != nil {
			b.occurrences[occ.Name] = names.sources
		}
		o := boundOccurrence{id: occ.ID, name: occ.Name, table: table}
		b.occByName[strings.ToLower(occ.Name)] = o
		b.occByTable[table] = append(b.occByTable[table], o)
	}
	for _, rel := range bundle.Relationships {
		b.links[rel.LeftOccurrenceID] = append(b.links[rel.LeftOccurrenceID], rel.RightOccurrenceID)
		b.links[rel.RightOccurrenceID] = append(b.links[rel.RightOccurrenceID], rel.LeftOccurrenceID)
		for _, p := range rel.PredicateList() {
			if p.Operator != "×" { // a Cartesian criterion reads no field
				b.matchColumns[p.LeftColumnID], b.matchColumns[p.RightColumnID] = true, true
			}
		}
	}
	return b
}

// contextOf is the occurrence a calculation of table is evaluated from: the
// one FileMaker names, else the one named like the table, else its first.
// File4Base resolves it the same way when the calculation is computed.
func (b formulaBinding) contextOf(table, name string) (boundOccurrence, bool) {
	for _, n := range []string{name, table} {
		if o, ok := b.occByName[strings.ToLower(n)]; ok && n != "" && o.table == table {
			return o, true
		}
	}
	if list := b.occByTable[table]; len(list) > 0 {
		return list[0], true
	}
	return boundOccurrence{}, false
}

// reachable reports whether relationships lead from one occurrence to another.
func (b formulaBinding) reachable(from, to string) bool {
	seen := map[string]bool{from: true}
	for queue := []string{from}; len(queue) > 0; queue = queue[1:] {
		if queue[0] == to {
			return true
		}
		for _, next := range b.links[queue[0]] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
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
// "Prix HT" or a French 1,5 is understood. A related field Occurrence::Field
// is bound to the imported occurrence and column; a calculation reading one is
// unstored, computed each time a record is read, as FileMaker requires — and
// so is a calculation reading an unstored one. A formula that cannot be
// applied — it does not read, it names a field or occurrence that is not
// there or was deleted in FileMaker, or one no relationship leads to — is
// **quarantined**: the column keeps the type the calculation would have
// produced and stores a plain value, and the report says why.
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
	opts := calc.Options{Fields: local, Tables: b.occurrences, Locale: b.locale, Library: b.functions}

	var items []ReportItem
	var bound []boundCalculation
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

		this := boundCalculation{col: col, payload: payload, formula: formula, resultType: resultType, source: source, target: target}
		quarantine := func(why, action string) {
			items = append(items, this.quarantine(why, action, colTypeByTableCol)...)
		}

		expr, err := calc.ParseWith(formula, opts)
		if err != nil {
			why := b.unreadable(formula, err)
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
		if name, ok := b.callsDropped(expr); ok {
			quarantine(fmt.Sprintf("calls the custom function %s, which could not be migrated", name),
				fmt.Sprintf("Rewrite the calculation of %s in File4Base, or correct it in FileMaker and migrate again", target))
			continue
		}
		if slices.Contains(expr.Functions(), "GETLAYOUTOBJECTATTRIBUTE") {
			quarantine("reads a layout object with GetLayoutObjectAttribute, which a field stored with its record cannot do",
				fmt.Sprintf("Compute %s from fields instead of from a layout object, or fill it with a script", target))
			continue
		}

		// The occurrence it is evaluated from, kept only when it is one of
		// this table's: anything else would read the wrong records.
		contextName, _ := payload["context"].(string)
		from, hasContext := b.contextOf(table.Name, contextName)
		if hasContext && strings.EqualFold(from.name, contextName) {
			payload["context"] = from.name
		} else {
			delete(payload, "context")
		}
		var unreachable []string
		for _, ref := range expr.RelatedFields() {
			if to, ok := b.occByName[strings.ToLower(ref.Table)]; ok && (!hasContext || !b.reachable(from.id, to.id)) {
				unreachable = append(unreachable, ref.Table+"::"+ref.Field)
			}
		}
		if len(unreachable) > 0 {
			quarantine(fmt.Sprintf("reads %s, but no relationship leads there from %s", quotedList(unreachable), table.Name),
				fmt.Sprintf("Relate %s to the table it reads, or rewrite its calculation", target))
			continue
		}

		rewritten, unresolved, err := calc.Rewrite(formula, opts, b.renamer(names))
		if err != nil {
			quarantine(fmt.Sprintf("could not be read (%v)", err),
				fmt.Sprintf("Rewrite the calculation of %s in File4Base", target))
			continue
		}
		if len(unresolved) > 0 {
			var refs []string
			seen := map[string]bool{}
			for _, ref := range unresolved {
				name := ref.Field
				if ref.Table != "" {
					name = ref.Table + "::" + ref.Field
				}
				if !seen[name] {
					seen[name] = true
					refs = append(refs, name)
				}
			}
			quarantine(fmt.Sprintf("reads %s, which the imported tables do not have", quotedList(refs)),
				fmt.Sprintf("Repoint the calculation of %s at the fields it should read, or add the missing %s", target, quotedList(refs)))
			continue
		}

		// A relationship matches on what the database holds, and an unstored
		// calculation holds nothing there.
		matched := b.matchColumns[col.ID]
		if matched && len(related) > 0 {
			quarantine(fmt.Sprintf("reads the related %s, so it must be computed when shown, but a relationship matches records on it", quotedList(related)),
				fmt.Sprintf("Match the relationship on a stored field, or fill %s with a script", target))
			continue
		}

		payload["formula"] = rewritten
		item := ReportItem{Category: CategoryPreserved, Domain: "field", SourceName: source, TargetName: target,
			Detail: fmt.Sprintf("Calculation field (%s): %s", resultType, formula)}
		if rewritten != formula {
			item.Category = CategoryTranslated
			item.Detail = fmt.Sprintf("Calculation bound to the imported fields: %q now reads %q", formula, rewritten)
		}
		switch {
		case len(related) > 0:
			payload["stored"] = false
			item.Detail += fmt.Sprintf("; it reads the related %s, so it is computed each time a record is shown (unstored), as in FileMaker",
				quotedList(related))
		case matched && payload["stored"] == false:
			delete(payload, "stored")
			item.Category = CategoryTranslated
			item.Detail += "; stored, although FileMaker does not store it, because a relationship matches records on it"
		}
		bound = append(bound, this)
		items = append(items, item)
	}
	return append(items, finishCalculations(bound, b.matchColumns, colTypeByTableCol, b.stored)...)
}

// renamer names a field reference of a formula as the import named it: a
// field of the formula's own table (names, nil for a formula of no table)
// by its column, one read through an occurrence by the occurrence and the
// column, a field of another file's table by the name its migration gives it.
func (b formulaBinding) renamer(names *fieldNameMap) func(occurrence, field string) (string, bool) {
	return func(occurrence, field string) (string, bool) {
		if occurrence == "" {
			if names == nil {
				return "", false
			}
			// A column named like a custom function is quoted, so the
			// stored formula still reads the field, not the function.
			column, ok := names.resolve(field)
			return calc.QuoteFieldName(column, b.stored), ok
		}
		occ, ok := b.occByName[strings.ToLower(occurrence)]
		if ok && occ.external {
			return calc.QuoteName(occ.name) + "::" + calc.QuoteName(externalFieldName(field)), true
		}
		if !ok || b.fields[occ.table] == nil {
			return "", false
		}
		column, ok := b.fields[occ.table].resolve(field)
		return calc.QuoteName(occ.name) + "::" + calc.QuoteName(column), ok
	}
}

// boundCalculation is a calculation whose formula now reads the imported
// names, still to be written back into its column.
type boundCalculation struct {
	col                 *schema.BundleColumn
	payload             map[string]interface{}
	formula, resultType string // as the source wrote them
	source, target      string
}

// quarantine keeps the column with the type the calculation would have
// produced, storing a plain value, and reports why.
func (c boundCalculation) quarantine(why, action string, colTypes map[string]string) []ReportItem {
	c.col.FieldType = mapBasicFieldType(c.resultType)
	c.col.CalculationFormula = nil
	colTypes[c.target] = c.col.FieldType
	return []ReportItem{
		{Category: CategoryUnsupported, Domain: "field", SourceName: c.source, TargetName: c.target,
			Detail: fmt.Sprintf("Calculation %q %s; converted to a stored %s column instead of being applied", readableFieldRefs(c.formula), readableFieldRefs(why), c.col.FieldType)},
		{Category: CategoryManualActionRequired, Domain: "field", SourceName: c.source, TargetName: c.target, Detail: readableFieldRefs(action)},
	}
}

// finishCalculations makes unstored every calculation that reads an unstored
// one, as FileMaker does: a stored result would go stale when the value it
// was computed from changes. One a relationship matches on cannot be, and is
// quarantined. Then it writes each formula into its column; an unstored
// calculation's column holds nothing, so it is never required.
func finishCalculations(bound []boundCalculation, matchColumns map[string]bool, colTypes map[string]string, lib *calc.Library) []ReportItem {
	unstored := map[string]bool{}
	reads := map[string][]string{}
	for _, c := range bound {
		unstored[c.col.Name] = c.payload["stored"] == false
		formula, _ := c.payload["formula"].(string)
		if expr, err := calc.ParseWith(formula, calc.Options{Library: lib}); err == nil {
			reads[c.col.Name] = expr.Fields()
		}
	}
	var items []ReportItem
	quarantined := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, c := range bound {
			if unstored[c.col.Name] || quarantined[c.col.Name] {
				continue
			}
			for _, read := range reads[c.col.Name] {
				if !unstored[read] {
					continue
				}
				changed = true
				if matchColumns[c.col.ID] {
					quarantined[c.col.Name] = true
					items = append(items, c.quarantine(
						fmt.Sprintf("reads %q, which is computed when shown, but a relationship matches records on it", read),
						fmt.Sprintf("Match the relationship on a stored field, or fill %s with a script", c.target), colTypes)...)
					break
				}
				unstored[c.col.Name] = true
				c.payload["stored"] = false
				items = append(items, ReportItem{Category: CategoryTranslated, Domain: "field", SourceName: c.source, TargetName: c.target,
					Detail: fmt.Sprintf("Computed each time a record is shown (unstored), because it reads %q, which is", read)})
				break
			}
		}
	}
	for _, c := range bound {
		if quarantined[c.col.Name] {
			continue
		}
		if unstored[c.col.Name] {
			c.col.IsNullable = true
		}
		encoded, err := json.Marshal(c.payload)
		if err != nil {
			continue
		}
		stored := string(encoded)
		c.col.CalculationFormula = &stored
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
// route can apply. lib holds the solution's custom functions.
func ownTableFormula(raw string, functions formulaBinding) (string, bool) {
	formula := formulaText(raw)
	if formula == "" {
		return "", false
	}
	expr, err := calc.ParseWith(formula, calc.Options{Library: functions.stored})
	if err != nil || len(expr.RelatedFields()) > 0 {
		return formula, false
	}
	if _, ok := functions.callsDropped(expr); ok {
		return formula, false
	}
	return formula, true
}

// callsDropped names a custom function the formula calls that could not be
// migrated. A formula that reads at all calls one only when it is named
// like a built-in function, which it would otherwise silently call instead.
func (b formulaBinding) callsDropped(expr *calc.Expr) (string, bool) {
	for _, fn := range expr.Functions() {
		if name, ok := b.dropped[fn]; ok {
			return name, true
		}
	}
	return "", false
}

// quotedList renders names as "a", "b" and "c" for a report a person reads.
func quotedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", readableFieldRefs(name)))
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
