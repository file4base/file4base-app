package migration

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// Auto-enter field options (docs/specs/auto_enter_options.md).
//
// A field's <AutoEnter> element is read into the options File4Base keeps in
// the column's default_value, with the names the source wrote: a formula
// and a lookup are bound to the imported fields and occurrences once every
// table is known (bindTableAutoEnter), and anything that cannot be applied
// is dropped and reported with the reason.
//
// The element comes in three shapes:
//
//   - Save as XML: the type attribute names the option chosen (SerialNumber,
//     ConstantData, Calculated, Looked_up, CreationDate …
//     ModificationAccountName, or nothing), each option is a child whose
//     enable attribute says whether it is on (a serial and a calculation can
//     both be), and prohibitModification protects the field.
//   - The Database Design Report and the clipboard: flags on the element
//     (constant, calculation, lookup), a value attribute for the stamps,
//     <Serial nextValue increment generate>, <ConstantData>, <Calculation>,
//     and allowEditing="False" for a protected field.
//   - The HTML report: a list of sentences (convertHTMLAutoEnter).

// stampKinds are the spellings of the creation and modification stamps.
var stampKinds = map[string]autoenter.Stamp{
	"date": autoenter.StampDate, "time": autoenter.StampTime, "timestamp": autoenter.StampTimestamp,
	"name": autoenter.StampName, "username": autoenter.StampName, "accountname": autoenter.StampAccountName,
}

// stampOf reads a stamp kind such as CreationTimestamp: whether it is a
// creation or a modification stamp, and which.
func stampOf(kind string) (creation bool, stamp autoenter.Stamp, ok bool) {
	k := strings.ToLower(strings.NewReplacer("_", "", " ", "", "-", "").Replace(kind))
	for prefix, isCreation := range map[string]bool{"creation": true, "modification": false} {
		if rest, found := strings.CutPrefix(k, prefix); found {
			if s, known := stampKinds[rest]; known {
				return isCreation, s, true
			}
		}
	}
	return false, "", false
}

func isTrueText(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "1":
		return true
	}
	return false
}

// readAutoEnter reads a field's <AutoEnter> element.
func readAutoEnter(an *xmlNode) autoenter.Options {
	var o autoenter.Options
	kind := strings.ToLower(firstAttr(an, "type"))
	if kind == "" {
		kind = strings.ToLower(firstAttr(an, "value"))
	}
	// enabled says whether an option is on: its own enable attribute when
	// it has one (Save as XML keeps the options not chosen, disabled), else
	// a flag of the element, else the kind the element names, else whether
	// its element is there.
	enabled := func(child *xmlNode, flag string, kinds ...string) bool {
		if child != nil {
			if e, ok := child.Attrs["enable"]; ok {
				return isTrueText(e)
			}
		}
		if flag != "" {
			if v, ok := an.Attrs[flag]; ok {
				return isTrueText(v)
			}
		}
		for _, k := range kinds {
			if kind == k {
				return true
			}
		}
		// Nothing says which option is chosen: an element for one is the
		// option being set.
		return child != nil && kind == ""
	}

	// Stamps: the kind, or a child element named after one.
	stamps := []string{kind}
	for _, child := range an.Children {
		stamps = append(stamps, child.Name)
	}
	for _, s := range stamps {
		if creation, stamp, ok := stampOf(s); ok {
			if creation {
				o.CreationEnabled, o.CreationType = true, stamp
			} else {
				o.ModificationEnabled, o.ModificationType = true, stamp
			}
		}
	}

	if serial := findFirstChild(an, "SerialNumber", "Serial", "serial", "serialNumber"); serial != nil || kind == "serialnumber" || kind == "serial" {
		if enabled(serial, "", "serialnumber", "serial") {
			o.SerialEnabled = true
			o.SerialGenerate = autoenter.GenerateOnCreation
			o.SerialNext, o.SerialIncrement = "1", 1
			if serial != nil {
				if strings.Contains(strings.ToLower(firstAttr(serial, "generate")), "commit") {
					o.SerialGenerate = autoenter.GenerateOnCommit
				}
				if next := firstAttr(serial, "nextvalue", "nextValue"); next != "" {
					o.SerialNext = next
				}
				if inc, err := strconv.ParseInt(firstAttr(serial, "increment"), 10, 64); err == nil {
					o.SerialIncrement = inc
				}
			}
		}
	}

	if constant := findFirstChild(an, "ConstantData", "constantData"); constant != nil || kind == "constantdata" {
		value := ""
		if constant != nil {
			value = firstAttr(constant, "value", "Value")
			if value == "" {
				value = constant.Text // kept as written, spaces and all
			}
		}
		if value != "" && enabled(constant, "constant", "constantdata", "constant") {
			o.DataEnabled, o.DataValue = true, &value
		}
	}

	if calcNode := findFirstChild(an, "Calculated", "Calculation", "calculation"); calcNode != nil &&
		enabled(calcNode, "calculation", "calculated", "calculation") {
		text := strings.TrimSpace(markNamelessFieldReferences(calcNode, calculationText(calcNode)))
		if text != "" {
			o.CalculatedEnabled, o.CalculatedFormula = true, text
			for _, n := range []*xmlNode{calcNode, findFirstChild(calcNode, "Calculation", "calculation")} {
				if n == nil {
					continue
				}
				if occ := findFirstChild(n, "TableOccurrenceReference"); occ != nil {
					o.CalculatedContext = firstAttr(occ, "name")
				} else if t := firstAttr(n, "table"); t != "" && o.CalculatedContext == "" {
					o.CalculatedContext = t
				}
			}
			// "Do not replace existing value" is FileMaker's default; the
			// source says overwrite when it is off.
			o.DoNotReplace = true
			for _, n := range []*xmlNode{calcNode, an} {
				if v := firstAttr(n, "overwriteExisting", "overwriteExistingValue"); v != "" {
					o.DoNotReplace = !isTrueText(v)
					break
				}
			}
			o.SkipIfEmpty = true
			for _, n := range []*xmlNode{calcNode, an} {
				if v := firstAttr(n, "alwaysEvaluate"); v != "" {
					o.SkipIfEmpty = !isTrueText(v)
					break
				}
			}
		}
	}

	if lookup := findFirstChild(an, "Looked_up", "LookedUp", "Lookup", "lookup"); lookup != nil &&
		enabled(lookup, "lookup", "looked_up", "lookedup", "lookup") {
		o.LookedUpEnabled = true
		if ref := findFirstChild(lookup, "FieldReference", "Field"); ref != nil {
			o.LookedUpField = firstAttr(ref, "name", "Name")
			if occ := findFirstChild(ref, "TableOccurrenceReference"); occ != nil {
				o.LookedUpOccurrence = firstAttr(occ, "name", "Name")
			} else {
				o.LookedUpOccurrence = firstAttr(ref, "table", "Table")
			}
		}
		// The occurrence the lookup starts from (FileMaker's "Starting with
		// table"): an occurrence named beside the field it copies. One that
		// is not of the field's table is not taken (contextOf).
		if ctx := findFirstChild(lookup, "Context", "context"); ctx != nil {
			if occ := findFirstChild(ctx, "TableOccurrenceReference"); occ != nil {
				o.LookedUpContext = firstAttr(occ, "name", "Name")
			} else {
				o.LookedUpContext = firstAttr(ctx, "name", "table")
			}
		} else if occ := findFirstChild(lookup, "TableOccurrenceReference", "StartingTable"); occ != nil {
			o.LookedUpContext = firstAttr(occ, "name", "Name")
		}
		o.LookedUpDontCopyIfEmpty = isTrueText(firstAttr(lookup, "dontCopyIfEmpty", "dontCopyContentsIfEmpty"))
		o.LookedUpNoMatch = autoenter.NoMatchDoNotCopy
		switch option := strings.ToLower(firstAttr(lookup, "noMatchCopyOption", "noMatch")); {
		case strings.Contains(option, "lower"):
			o.LookedUpNoMatch = autoenter.NoMatchNextLower
		case strings.Contains(option, "higher"):
			o.LookedUpNoMatch = autoenter.NoMatchNextHigher
		case strings.Contains(option, "constant"):
			o.LookedUpNoMatch = autoenter.NoMatchConstant
			if constant := findFirstChild(lookup, "ConstantData", "constantData"); constant != nil {
				o.LookedUpNoMatchValue = firstAttr(constant, "value", "Value")
				if o.LookedUpNoMatchValue == "" {
					o.LookedUpNoMatchValue = constant.Text
				}
			}
		}
	}

	switch kind {
	case "lastvisited", "previousrecord", "valuefromlastvisitedrecord", "lastvisitedrecord":
		o.ValueLastVisited = true
	}
	if v, ok := an.Attrs["prohibitModification"]; ok {
		o.ProhibitModification = isTrueText(v)
	} else if v := firstAttr(an, "allowEditing"); v != "" {
		o.ProhibitModification = !isTrueText(v)
	}
	return o
}

// convertFieldAutoEnter turns a field's nested <AutoEnter> element into the
// column's auto-enter options. They are reported once they are bound to the
// imported names (bindTableAutoEnter).
func convertFieldAutoEnter(tableSQL, rawFieldName, colName string, an *xmlNode, col *schema.BundleColumn) []ReportItem {
	if an == nil {
		return nil
	}
	o := readAutoEnter(an)
	if !o.Any() && !o.ValueLastVisited {
		return nil
	}
	stored := autoenter.Encode(o)
	col.DefaultValue = &stored
	return nil
}

// bindTableAutoEnter binds the auto-enter options of a converted table's
// fields to the imported names, and reports each: what was preserved, and
// what was dropped and why.
func bindTableAutoEnter(table *schema.BundleTable, b formulaBinding) []ReportItem {
	var items []ReportItem
	for i := range table.Columns {
		col := &table.Columns[i]
		if col.DefaultValue == nil {
			continue
		}
		o, err := autoenter.Parse(col.DefaultValue)
		if err != nil || (!o.Any() && !o.ValueLastVisited) {
			continue
		}
		source, target := table.Name+"."+col.DisplayName, table.Name+"."+col.Name
		kept, dropped := bindFieldAutoEnter(&o, col.FieldType, table.Name, target, &b)
		for _, d := range dropped {
			items = append(items,
				ReportItem{Category: CategoryUnsupported, Domain: "field", SourceName: source, TargetName: target,
					Detail: fmt.Sprintf("Auto-enter %s was not migrated: %s", d.what, d.why)},
				ReportItem{Category: CategoryManualActionRequired, Domain: "field", SourceName: source, TargetName: target,
					Detail: d.action})
		}
		if len(kept) > 0 {
			items = append(items, ReportItem{Category: CategoryPreserved, Domain: "field", SourceName: source, TargetName: target,
				Detail: "Auto-enter options: " + strings.Join(kept, "; ")})
		}
		if !o.Any() && !o.ValueLastVisited {
			col.DefaultValue = nil
			continue
		}
		stored := autoenter.Encode(o)
		col.DefaultValue = &stored
	}
	return items
}

// droppedOption is an auto-enter option a migration could not keep.
type droppedOption struct {
	what, why, action string
}

// bindFieldAutoEnter checks a field's options against its type and binds
// its formula and lookup, unless b is nil: they are then in the imported
// names already. It turns off what cannot be applied, and says what it kept
// and what it dropped.
func bindFieldAutoEnter(o *autoenter.Options, fieldType, tableName, target string, b *formulaBinding) (kept []string, dropped []droppedOption) {
	drop := func(what, why, action string) { dropped = append(dropped, droppedOption{what, why, action}) }
	switch fieldType {
	case "CALCULATION", "SUMMARY":
		// A formula owns such a field's value; FileMaker has no auto-enter
		// options for one either.
		*o = autoenter.Options{}
		return nil, nil
	}
	typeName := strings.ToLower(fieldType)

	for _, s := range []struct {
		on    *bool
		kind  string
		stamp autoenter.Stamp
	}{{&o.CreationEnabled, "creation", o.CreationType}, {&o.ModificationEnabled, "modification", o.ModificationType}} {
		if !*s.on {
			continue
		}
		what := fmt.Sprintf("%s %s", s.kind, strings.ToLower(string(s.stamp)))
		if !autoenter.StampFits(s.stamp, fieldType) {
			*s.on = false
			drop(what, fmt.Sprintf("a %s field cannot hold it", typeName),
				fmt.Sprintf("Enter the %s in %s with a script, or change the field's type", what, target))
			continue
		}
		kept = append(kept, what)
	}

	if o.SerialEnabled {
		what := "serial number"
		_, err := autoenter.NextSerial(o.SerialNext, 0)
		switch {
		case fieldType != "TEXT" && fieldType != "NUMBER":
			o.SerialEnabled = false
			drop(what, fmt.Sprintf("a %s field cannot hold a serial number", typeName),
				fmt.Sprintf("Number the records of %s with a text or number field", target))
		case err != nil:
			o.SerialEnabled = false
			drop(what, fmt.Sprintf("its next value %q has no number to increment", o.SerialNext),
				fmt.Sprintf("Set a next value for the serial number of %s in Manage Database", target))
		case fieldType == "NUMBER" && !isWholeNumber(o.SerialNext):
			o.SerialEnabled = false
			drop(what, fmt.Sprintf("its next value %q is not a whole number, which a number field needs", o.SerialNext),
				fmt.Sprintf("Set a whole next value for the serial number of %s in Manage Database", target))
		default:
			generate := "on creation"
			if o.SerialGenerate == autoenter.GenerateOnCommit {
				generate = "on commit"
			}
			kept = append(kept, fmt.Sprintf("serial number %s, next value %s, increment %d", generate, o.SerialNext, max(o.SerialIncrement, 1)))
		}
	}

	if o.HasConstant() {
		kept = append(kept, fmt.Sprintf("data %q", *o.DataValue))
	}

	if o.CalculatedEnabled {
		formula := formulaText(o.CalculatedFormula)
		fb := boundFormula{rewritten: formula, context: o.CalculatedContext}
		if b != nil {
			fb = b.bind(tableName, formula, o.CalculatedContext, target)
		}
		switch {
		case fb.why != "":
			o.CalculatedEnabled, o.CalculatedFormula, o.CalculatedContext = false, "", ""
			drop("calculation "+readableFieldRefs(fmt.Sprintf("%q", formula)), readableFieldRefs(fmt.Sprintf("it %s", fb.why)), readableFieldRefs(fb.action))
		case fieldType == "CONTAINER":
			o.CalculatedEnabled, o.CalculatedFormula, o.CalculatedContext = false, "", ""
			drop(fmt.Sprintf("calculation %q", formula), "File4Base does not enter a calculated container",
				fmt.Sprintf("Fill %s with a script, or make it a calculation field", target))
		default:
			o.CalculatedFormula, o.CalculatedContext = fb.rewritten, fb.context
			detail := fmt.Sprintf("calculated value %q", formula)
			if fb.rewritten != formula {
				detail = fmt.Sprintf("calculated value %q, bound to the imported fields as %q", formula, fb.rewritten)
			}
			if o.DoNotReplace {
				detail += ", not replacing an existing value"
			}
			if !o.SkipIfEmpty {
				detail += ", evaluated even when every field it reads is empty"
			}
			kept = append(kept, detail)
		}
	}

	if o.LookedUpEnabled {
		what := fmt.Sprintf("looked-up value from %s::%s", o.LookedUpOccurrence, o.LookedUpField)
		if b == nil {
			b = &formulaBinding{}
		}
		occ, known := b.occByName[strings.ToLower(o.LookedUpOccurrence)]
		contextName := o.LookedUpContext
		from, hasContext := b.contextOf(tableName, contextName)
		named := hasContext && contextName != "" && strings.EqualFold(from.name, contextName)
		if known && !named && (!hasContext || !b.reachable(from.id, occ.id)) {
			// The source names no occurrence of the table to start from, or
			// one that was not migrated: start from one of the table's
			// occurrences a path of relationships leads from, as FileMaker
			// starts from the one the lookup was defined on.
			for _, candidate := range b.occByTable[tableName] {
				if b.reachable(candidate.id, occ.id) {
					from, hasContext, named = candidate, true, true
					break
				}
			}
		}
		column := ""
		switch {
		case o.LookedUpOccurrence == "" || o.LookedUpField == "":
			drop("looked-up value", "the source does not say which field it copies",
				fmt.Sprintf("Set up the lookup of %s in Manage Database", target))
		case !known:
			drop(what, fmt.Sprintf("the table occurrence %q was not migrated", o.LookedUpOccurrence),
				fmt.Sprintf("Set up the lookup of %s in Manage Database", target))
		case fieldType == "CONTAINER":
			drop(what, "File4Base does not copy a container",
				fmt.Sprintf("Copy the container into %s with a script", target))
		case !hasContext || !b.reachable(from.id, occ.id):
			start := tableName
			if hasContext {
				start = from.name
			}
			drop(what, fmt.Sprintf("no relationship leads to %s from %s, directly or through other occurrences", occ.name, start),
				fmt.Sprintf("Relate %s to %s, then set up its lookup in Manage Database", start, occ.name))
		case occ.external:
			column = externalFieldName(o.LookedUpField)
		default:
			if names := b.fields[occ.table]; names != nil {
				column, _ = names.resolve(o.LookedUpField)
			}
			if column == "" {
				drop(what, fmt.Sprintf("%s has no field %q", occ.name, o.LookedUpField),
					fmt.Sprintf("Set up the lookup of %s in Manage Database", target))
			}
		}
		if column == "" {
			o.LookedUpEnabled, o.LookedUpOccurrence, o.LookedUpField, o.LookedUpContext = false, "", "", ""
		} else {
			o.LookedUpOccurrence, o.LookedUpField, o.LookedUpContext = occ.name, column, ""
			if named {
				o.LookedUpContext = from.name
			}
			detail := fmt.Sprintf("looked-up value from %s::%s", occ.name, column)
			switch o.LookedUpNoMatch {
			case autoenter.NoMatchNextLower:
				detail += ", the next lower value when nothing matches"
			case autoenter.NoMatchNextHigher:
				detail += ", the next higher value when nothing matches"
			case autoenter.NoMatchConstant:
				detail += fmt.Sprintf(", %q when nothing matches", o.LookedUpNoMatchValue)
			}
			if o.LookedUpDontCopyIfEmpty {
				detail += ", not copying an empty value"
			}
			kept = append(kept, detail)
		}
	}

	if o.ValueLastVisited {
		drop("value from the last visited record", "File4Base keeps the option but does not enter it",
			fmt.Sprintf("Enter the value of %s with a script trigger if it is needed", target))
	}
	if o.ProhibitModification {
		kept = append(kept, "prohibit modification during data entry")
	}
	return kept, dropped
}

func isWholeNumber(text string) bool {
	_, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err == nil {
		return true
	}
	trimmed := strings.TrimPrefix(strings.TrimSpace(text), "-")
	return trimmed != "" && strings.Trim(trimmed, "0123456789") == ""
}

// HTML report

var (
	reHTMLAutoEnter = regexp.MustCompile(`(?is)<strong>\s*Auto-Enter:\s*</strong>\s*<ul>(.*?)</ul>`)
	reHTMLListItem  = regexp.MustCompile(`(?is)<li>(.*?)</li>`)
	reHTMLSerial    = regexp.MustCompile(`(?i)^Serial:\s*Generate:\s*On\s+(creation|commit)\s*,\s*Next value:\s*(.*?)\s*,\s*Increment:\s*(\d+)`)
	reHTMLContext   = regexp.MustCompile(`(?is)^Context table:\s*(.*?)\s*Calculation:\s*(.*)$`)
)

// htmlAutoEnterItems are the sentences of the Auto-Enter list of a field's
// options cell, nil when the cell has none. The report is read in English.
func htmlAutoEnterItems(cellHTML string) []string {
	m := reHTMLAutoEnter.FindStringSubmatch(cellHTML)
	if m == nil {
		return nil
	}
	items := []string{}
	for _, li := range reHTMLListItem.FindAllStringSubmatch(m[1], -1) {
		items = append(items, cleanHTMLText(li[1]))
	}
	return items
}

// convertHTMLAutoEnter reads the Auto-Enter list of an HTML report into the
// column's options. A calculation is kept when it reads plain fields of its
// own table only, as the report's calculation fields are; a lookup is not
// described by the report, and is reported.
func convertHTMLAutoEnter(tableSQL, rawTableName, rawFieldName string, col *schema.BundleColumn, list []string,
	knownSameTableCols map[string]string, functions formulaBinding,
) []ReportItem {
	if list == nil {
		return nil
	}
	source, target := tableSQL+"."+rawFieldName, tableSQL+"."+col.Name
	var o autoenter.Options
	o.ProhibitModification = true // the report says "Allow editing" when it is allowed
	var items []ReportItem
	for _, item := range list {
		lower := strings.ToLower(strings.TrimSpace(item))
		switch {
		case lower == "allow editing":
			o.ProhibitModification = false
		case strings.HasPrefix(lower, "serial:"):
			if m := reHTMLSerial.FindStringSubmatch(item); m != nil {
				o.SerialEnabled, o.SerialNext = true, strings.TrimSpace(m[2])
				o.SerialGenerate = autoenter.GenerateOnCreation
				if strings.EqualFold(m[1], "commit") {
					o.SerialGenerate = autoenter.GenerateOnCommit
				}
				o.SerialIncrement, _ = strconv.ParseInt(m[3], 10, 64)
			}
		case strings.HasPrefix(lower, "creation ") || strings.HasPrefix(lower, "modification "):
			if creation, stamp, ok := stampOf(lower); ok {
				if creation {
					o.CreationEnabled, o.CreationType = true, stamp
				} else {
					o.ModificationEnabled, o.ModificationType = true, stamp
				}
			}
		case strings.HasPrefix(lower, "constant data:"):
			value := strings.TrimSpace(item[len("constant data:"):])
			if value != "" {
				o.DataEnabled, o.DataValue = true, &value
			}
		case strings.HasPrefix(lower, "do not replace existing value"):
			o.DoNotReplace = true
		case strings.HasPrefix(lower, "context table:"):
			m := reHTMLContext.FindStringSubmatch(item)
			if m == nil {
				continue
			}
			// The list item is the formula itself, after "Calculation:":
			// an "=" in it is the formula's own.
			formula := htmlFormulaNames(strings.TrimSpace(m[2]), tableSQL, rawTableName, knownSameTableCols)
			if expr, ok := ownTableFormula(formula, functions); ok {
				o.CalculatedEnabled, o.CalculatedFormula, o.SkipIfEmpty = true, expr, true
			} else {
				items = append(items,
					ReportItem{Category: CategoryUnsupported, Domain: "field", SourceName: source, TargetName: target,
						Detail: fmt.Sprintf("Auto-enter calculation %q was not migrated: it reads more than plain fields of its own table, which a Database Design Report does not describe well enough", formula)},
					ReportItem{Category: CategoryManualActionRequired, Domain: "field", SourceName: source, TargetName: target,
						Detail: fmt.Sprintf("Set up the auto-enter calculation of %s in Manage Database", target)})
			}
		case strings.HasPrefix(lower, "looked-up value") || strings.HasPrefix(lower, "lookup"):
			items = append(items,
				ReportItem{Category: CategoryUnsupported, Domain: "field", SourceName: source, TargetName: target,
					Detail: fmt.Sprintf("Auto-enter %q was not migrated: the HTML report does not say enough of a lookup", item)},
				ReportItem{Category: CategoryManualActionRequired, Domain: "field", SourceName: source, TargetName: target,
					Detail: fmt.Sprintf("Set up the lookup of %s in Manage Database, or migrate from a Save as XML file", target)})
		}
	}
	if !o.CalculatedEnabled {
		o.DoNotReplace = false
	}
	kept, dropped := bindFieldAutoEnter(&o, col.FieldType, tableSQL, target, nil)
	for _, d := range dropped {
		items = append(items,
			ReportItem{Category: CategoryUnsupported, Domain: "field", SourceName: source, TargetName: target,
				Detail: fmt.Sprintf("Auto-enter %s was not migrated: %s", d.what, d.why)},
			ReportItem{Category: CategoryManualActionRequired, Domain: "field", SourceName: source, TargetName: target,
				Detail: d.action})
	}
	if len(kept) > 0 {
		items = append(items, ReportItem{Category: CategoryPreserved, Domain: "field", SourceName: source, TargetName: target,
			Detail: "Auto-enter options: " + strings.Join(kept, "; ")})
	}
	if o.Any() {
		stored := autoenter.Encode(o)
		col.DefaultValue = &stored
	}
	return items
}
