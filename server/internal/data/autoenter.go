package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
)

// Auto-enter field options: what FileMaker's Field Options > Auto-Enter tab
// enters into a record, applied by every write path. The options and the
// rules are described in docs/specs/auto_enter_options.md.

// ErrProhibitedModification is returned when data entry gives a value to a
// field whose options prohibit modifying it during data entry.
var ErrProhibitedModification = errors.New("prohibited modification")

// ErrNotSerial is returned for a serial number request on a field that has
// no serial number option.
var ErrNotSerial = errors.New("not a serial number field")

type scriptEntryKey struct{}

type entryExemptKey struct{}

// AsScript marks the writes made with ctx as a script's: a script step may
// set a field whose options prohibit modification during data entry, as in
// FileMaker. It decides nothing about privileges.
func AsScript(ctx context.Context) context.Context {
	return context.WithValue(ctx, scriptEntryKey{}, true)
}

func isScript(ctx context.Context) bool {
	script, _ := ctx.Value(scriptEntryKey{}).(bool)
	return script
}

// exemptFromEntry marks fields the server itself gives a value in a data
// entry write, such as the match fields of a related record created through
// a portal: they are not the user's to modify, so prohibit-modification does
// not stop them.
func exemptFromEntry(ctx context.Context, fields ...string) context.Context {
	exempt := map[string]bool{}
	for _, f := range fields {
		exempt[f] = true
	}
	return context.WithValue(ctx, entryExemptKey{}, exempt)
}

func entryExempt(ctx context.Context, field string) bool {
	exempt, _ := ctx.Value(entryExemptKey{}).(map[string]bool)
	return exempt[field]
}

// WithAccount names the account the service writes as: what the Name and
// Account Name stamps enter, and what Get ( AccountName ) answers.
func (s *Service) WithAccount(name string) *Service {
	s.account = name
	return s
}

// autoMode is how a write applies the options.
type autoMode struct {
	// off enters nothing: an import without auto-enter, a recalculation.
	off bool
	// entry is data entry, where prohibit-modification applies.
	entry bool
	// overwrite enters values over the ones the write gives, as an import
	// performing auto-enter options does.
	overwrite bool
	// tx is the transaction serial numbers are taken in; nil takes each in
	// a transaction of its own.
	tx execer
}

// writeMode is the mode of a write through InsertRow and UpdateRow.
func writeMode(ctx context.Context) autoMode {
	return autoMode{entry: !isScript(ctx)}
}

// autoField is a field whose options enter a value.
type autoField struct {
	id, name  string
	fieldType dbal.AgnosticFieldType
	opts      autoenter.Options
	// expr is the auto-enter formula, nil when there is none or it does not
	// parse: a broken formula enters nothing rather than failing the write,
	// as a calculation field's does.
	expr *calc.Expr
	// reads are the fields of the record the formula reads, related
	// references excluded.
	reads []string
}

// enters reports whether the field enters a value at all.
func (f *autoField) enters() bool {
	o := f.opts
	return o.CreationEnabled || o.ModificationEnabled || o.SerialEnabled || o.HasConstant() ||
		f.expr != nil || (o.LookedUpEnabled && o.LookedUpOccurrence != "" && o.LookedUpField != "")
}

// reentersOnDuplicate reports whether a duplicate gets a new value rather
// than a copy: everything but a looked-up value and a calculated value that
// does not replace.
func (f *autoField) reentersOnDuplicate() bool {
	o := f.opts
	return o.CreationEnabled || o.ModificationEnabled || o.SerialEnabled || o.HasConstant() ||
		(f.expr != nil && !o.DoNotReplace)
}

// autoEnter is the auto-enter options of a table, read for one write.
type autoEnter struct {
	s          *Service
	table      string
	types      fieldSet
	fields     []*autoField // in the order the fields were created
	prohibited map[string]bool
	global     map[string]bool
	// calcs are the table's calculation fields; reads is what each reads,
	// so a field read through a calculation counts as read.
	calcs     []calculation
	calcReads map[string][]string
	reader    *relatedReader
	// firstHop keeps the match fields a reference through an occurrence
	// starts from, by context and occurrence.
	firstHop map[string][]string
}

// loadAutoEnter reads the options of a table's fields.
func (s *Service) loadAutoEnter(ctx context.Context, tableName string, types fieldSet) (*autoEnter, error) {
	dialect := s.driver.Dialect()
	q := `SELECT c.id, c.name, c.field_type, c.default_value FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = ` + dialect.Placeholder(1) + ` AND c.default_value IS NOT NULL ORDER BY c.created_at, c.name`
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the field options of %s: %w", tableName, err)
	}
	defer rows.Close()

	a := &autoEnter{s: s, table: tableName, types: types, prohibited: map[string]bool{}, global: map[string]bool{},
		firstHop: map[string][]string{}}
	for rows.Next() {
		var id, name, declared, raw string
		if err := rows.Scan(&id, &name, &declared, &raw); err != nil {
			return nil, err
		}
		opts, err := autoenter.Parse(&raw)
		if err != nil {
			continue // options the server cannot read enter nothing
		}
		if opts.StorageGlobal {
			a.global[name] = true
		}
		switch dbal.AgnosticFieldType(declared) {
		case dbal.FieldTypeCalculation, dbal.FieldTypeSummary:
			continue // a formula owns the value
		}
		if opts.ProhibitModification {
			a.prohibited[name] = true
		}
		f := &autoField{id: id, name: name, fieldType: types[name], opts: opts}
		if opts.CalculatedEnabled && strings.TrimSpace(opts.CalculatedFormula) != "" {
			if expr, err := calc.Parse(opts.CalculatedFormula); err == nil {
				f.expr, f.reads = expr, expr.Fields()
			}
		}
		if f.enters() {
			a.fields = append(a.fields, f)
		}
	}
	return a, rows.Err()
}

// active reports whether the table has an option a write has to apply.
func (a *autoEnter) active() bool {
	return len(a.fields) > 0 || len(a.prohibited) > 0
}

// withCalculations gives the options the table's calculation fields, which
// the formulas and lookups read as the record holds them.
func (a *autoEnter) withCalculations(calcs []calculation) {
	a.calcs = calcs
	a.calcReads = map[string][]string{}
	for _, c := range calcs {
		a.calcReads[c.field] = c.expr.Fields()
	}
}

func (a *autoEnter) relatedReader() *relatedReader {
	if a.reader == nil {
		a.reader = a.s.newRelatedReader()
	}
	return a.reader
}

// matchFields are the fields of the record a reference through occurrence,
// from the occurrence contextName, starts from: the match fields of the
// first relationship of the path. They are nil when no path leads there.
func (a *autoEnter) matchFields(ctx context.Context, contextName, occurrence string) []string {
	key := strings.ToLower(contextName) + "\x00" + strings.ToLower(occurrence)
	if fields, ok := a.firstHop[key]; ok {
		return fields
	}
	var fields []string
	if path, err := a.path(ctx, contextName, occurrence); err == nil && len(path) > 0 {
		if terms, err := path[0].rel.matchTerms(path[0].from); err == nil {
			for _, t := range terms {
				if t.Op != cartesian {
					fields = append(fields, t.SourceColumn)
				}
			}
		}
	}
	a.firstHop[key] = fields
	return fields
}

// path is the chain of relationships from the occurrence contextName of the
// table to occurrence.
func (a *autoEnter) path(ctx context.Context, contextName, occurrence string) ([]hop, error) {
	graph, err := a.relatedReader().occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	from, err := graph.contextOf(a.table, contextName)
	if err != nil {
		return nil, err
	}
	to, ok := graph.byName[strings.ToLower(occurrence)]
	if !ok {
		return nil, fmt.Errorf("there is no table occurrence %q", occurrence)
	}
	return graph.path(from, to)
}

// expand adds to fields every field a calculation among them reads, down
// the chain, so a formula reading a calculation is triggered by what the
// calculation reads.
func (a *autoEnter) expand(fields []string) map[string]bool {
	out := map[string]bool{}
	var add func(string)
	add = func(name string) {
		if out[name] {
			return
		}
		out[name] = true
		for _, read := range a.calcReads[name] {
			add(read)
		}
	}
	for _, f := range fields {
		add(f)
	}
	return out
}

// triggers are the fields whose change makes the field's formula be
// evaluated again, and whose emptiness "Do not evaluate if all referenced
// fields are empty" looks at: the fields it reads, through calculations
// too, and the match fields its related references start from. The field
// itself is left out.
func (a *autoEnter) triggers(ctx context.Context, f *autoField) map[string]bool {
	names := append([]string(nil), f.reads...)
	for _, ref := range f.expr.RelatedFields() {
		names = append(names, a.matchFields(ctx, f.opts.CalculatedContext, ref.Table)...)
	}
	out := a.expand(names)
	delete(out, f.name)
	return out
}

// lookupTriggers are the fields whose change makes the field be looked up
// again: the match fields of the first relationship of the lookup's path,
// and what those read when they are calculations.
func (a *autoEnter) lookupTriggers(ctx context.Context, f *autoField) map[string]bool {
	return a.expand(a.matchFields(ctx, f.opts.LookedUpContext, f.opts.LookedUpOccurrence))
}

// withCalculated is the record a formula or a lookup reads: the record with
// its calculation fields computed over what it holds now. The record itself
// is left as it is.
func (a *autoEnter) withCalculated(record map[string]interface{}) map[string]interface{} {
	view := make(map[string]interface{}, len(record))
	for k, v := range record {
		view[k] = v
	}
	for _, c := range a.calcs {
		if c.unstored {
			continue
		}
		if answers, err := c.evaluate(view); err == nil {
			for k, v := range answers {
				view[k] = v
			}
		}
	}
	return view
}

// change is one write a set of options is applied to.
type change struct {
	a      *autoEnter
	mode   autoMode
	record map[string]interface{} // the values written; entered values are added
	// current is the record as it is stored, nil for a new one.
	current map[string]interface{}
	// given are the fields the write gives a value, before any is entered.
	given map[string]bool
	// changed are the fields whose value the write changes, and the ones an
	// option entered since.
	changed map[string]bool
}

// merged is the record as it will be once the change is written.
func (c *change) merged() map[string]interface{} {
	out := make(map[string]interface{}, len(c.current)+len(c.record))
	for k, v := range c.current {
		out[k] = v
	}
	for k, v := range c.record {
		out[k] = v
	}
	return out
}

// keeps reports whether a value the write gives the field stays: it does,
// unless the write performs auto-enter options over it.
func (c *change) keeps(field string) bool {
	return c.given[field] && !c.mode.overwrite
}

func (c *change) enter(field string, value interface{}) {
	c.record[field] = c.a.storable(field, value)
	c.changed[field] = true
}

// storable is a value entered in a field as the engine takes it: MariaDB's
// DATETIME refuses RFC 3339 text with a zone, so a timestamp is written
// there as a time, which the driver sends in UTC.
func (a *autoEnter) storable(field string, value interface{}) interface{} {
	text, ok := value.(string)
	if !ok || a.types[field] != dbal.FieldTypeTimestamp || a.s.driver.Dialect().Engine() != dbal.EngineMariaDB {
		return value
	}
	if at, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return at.UTC()
	}
	return value
}

// newChange prepares the options' work on a write.
func (a *autoEnter) newChange(mode autoMode, record, current map[string]interface{}) *change {
	c := &change{a: a, mode: mode, record: record, current: current, given: map[string]bool{}, changed: map[string]bool{}}
	for key, value := range record {
		switch {
		case current == nil && isEmptyValue(value):
			continue // an empty value given to a new record is no value
		case current != nil && a.same(a.baseOf(key), value, current[key]):
			continue // a value sent back as it is changes nothing
		}
		c.given[a.baseOf(key)] = true
		c.changed[a.baseOf(key)] = true
	}
	return c
}

// baseOf is the field a record key is a repetition of.
func (a *autoEnter) baseOf(key string) string {
	if _, ok := a.types[key]; ok {
		return key
	}
	if base, _, ok := repetition.Split(key); ok {
		return base
	}
	return key
}

// checkProhibited refuses a data entry write that gives a value to a field
// whose options prohibit it: a value to a new record, a different one to an
// existing record.
func (a *autoEnter) checkProhibited(ctx context.Context, c *change) error {
	if !c.mode.entry || len(a.prohibited) == 0 {
		return nil
	}
	for key, value := range c.record {
		field := a.baseOf(key)
		if !a.prohibited[field] || entryExempt(ctx, field) {
			continue
		}
		if c.current == nil {
			if isEmptyValue(value) {
				continue
			}
		} else if a.same(field, value, c.current[key]) {
			continue
		}
		return fmt.Errorf("%w: the options of %s.%s prohibit modifying its value during data entry",
			ErrProhibitedModification, a.table, field)
	}
	return nil
}

// enterNew applies the options to a new record.
func (a *autoEnter) enterNew(ctx context.Context, mode autoMode, record map[string]interface{}) error {
	c := a.newChange(mode, record, nil)
	if err := a.checkProhibited(ctx, c); err != nil {
		return err
	}
	now := calc.Now()
	for _, f := range a.fields {
		o := f.opts
		switch {
		case c.keeps(f.name):
		case o.CreationEnabled:
			c.enter(f.name, a.stamp(o.CreationType, f.fieldType, now))
		case o.ModificationEnabled:
			c.enter(f.name, a.stamp(o.ModificationType, f.fieldType, now))
		}
	}
	for _, f := range a.fields {
		if f.opts.SerialEnabled && !c.keeps(f.name) {
			next, err := a.s.takeSerial(ctx, mode.tx, f.id)
			if err != nil {
				return err
			}
			if f.fieldType == dbal.FieldTypeNumber {
				// A number, exactly, as a calculation's number answer is.
				c.enter(f.name, json.Number(next))
			} else {
				c.enter(f.name, next)
			}
		}
	}
	for _, f := range a.fields {
		if f.opts.HasConstant() && !c.keeps(f.name) {
			c.enter(f.name, *f.opts.DataValue)
		}
	}
	return a.enterFormulasAndLookups(ctx, c, true)
}

// enterModified applies the options to an existing record a write changes.
// The record is modified when the write changes a field stored with it: not
// a calculation, a summary or a global field.
func (a *autoEnter) enterModified(ctx context.Context, mode autoMode, updates, current map[string]interface{}) error {
	c := a.newChange(mode, updates, current)
	if err := a.checkProhibited(ctx, c); err != nil {
		return err
	}
	calculated := map[string]bool{}
	for _, calcField := range a.calcs {
		calculated[calcField.field] = true
	}
	modified := false
	for field := range c.given {
		if !calculated[field] && !a.global[field] && a.types[field] != dbal.FieldTypeSummary {
			modified = true
		}
	}
	if modified {
		now := calc.Now()
		for _, f := range a.fields {
			if f.opts.ModificationEnabled && !c.keeps(f.name) {
				c.enter(f.name, a.stamp(f.opts.ModificationType, f.fieldType, now))
			}
		}
	}
	return a.enterFormulasAndLookups(ctx, c, false)
}

// enterFormulasAndLookups computes the calculated values and copies the
// looked-up values of a write, field by field in the order they were
// created, each seeing what the earlier ones entered.
func (a *autoEnter) enterFormulasAndLookups(ctx context.Context, c *change, isNew bool) error {
	for _, f := range a.fields {
		if f.expr != nil {
			value, ok, err := a.evaluate(ctx, f, c, isNew)
			if err != nil {
				return err
			}
			if ok {
				c.enter(f.name, value)
			}
		}
		if f.opts.LookedUpEnabled && f.opts.LookedUpOccurrence != "" && f.opts.LookedUpField != "" {
			if c.keeps(f.name) {
				continue
			}
			triggered := false
			for trigger := range a.lookupTriggers(ctx, f) {
				if c.changed[trigger] {
					triggered = true
					break
				}
			}
			if !triggered {
				continue
			}
			value, ok, err := a.lookup(ctx, f, a.withCalculated(c.merged()))
			if err != nil {
				return err
			}
			if ok {
				c.enter(f.name, value)
			}
		}
	}
	return nil
}

// evaluate computes a field's auto-enter formula when the write calls for
// it. ok is false when it does not.
func (a *autoEnter) evaluate(ctx context.Context, f *autoField, c *change, isNew bool) (interface{}, bool, error) {
	if c.given[f.name] {
		switch {
		case c.mode.overwrite && f.opts.DoNotReplace:
			return nil, false, nil // an import keeps the value it gives
		case !c.mode.overwrite && !(slices.Contains(f.reads, f.name) && !f.opts.DoNotReplace):
			// A value given wins, unless the formula reads the field
			// itself and replaces it: it then reworks what was typed.
			return nil, false, nil
		}
	}
	triggers := a.triggers(ctx, f)
	merged := c.merged()
	if !isNew && !c.given[f.name] {
		triggered := false
		for trigger := range triggers {
			if c.changed[trigger] {
				triggered = true
				break
			}
		}
		if !triggered {
			return nil, false, nil
		}
	}
	if f.opts.DoNotReplace && !c.given[f.name] && !isEmptyValue(merged[f.name]) {
		return nil, false, nil
	}
	if f.opts.SkipIfEmpty && len(triggers) > 0 {
		empty := true
		view := a.withCalculated(merged)
		for trigger := range triggers {
			if !isEmptyValue(view[trigger]) {
				empty = false
				break
			}
		}
		if empty {
			return nil, false, nil
		}
	}
	if f.fieldType == dbal.FieldTypeContainer {
		return nil, false, nil // a formula's container is a reference, not a file to store
	}

	view := a.withCalculated(merged)
	typed := make(calc.Record, len(view))
	for name := range view {
		typed[name] = typedValue(a.table, a.types, view, name, a.s.describeContainer(ctx))
	}
	env := &unstoredContext{RecordContext: calc.RecordContext{Record: typed, Repeats: a.types.repeating()},
		reader: a.relatedReader(), ctx: ctx, table: a.table, occurrence: f.opts.CalculatedContext, row: view}
	answer, err := calc.EvaluateWith(f.expr, env, resultTypeOf(f.fieldType))
	if err != nil {
		return nil, false, fmt.Errorf("%w: the auto-enter calculation of %q could not be computed: %s",
			ErrInvalidValue, f.name, err.Error())
	}
	return answer, true, nil
}

// lookup copies a field's looked-up value from the first related record.
// ok is false when nothing is to be copied.
func (a *autoEnter) lookup(ctx context.Context, f *autoField, row map[string]interface{}) (interface{}, bool, error) {
	if f.fieldType == dbal.FieldTypeContainer {
		return nil, false, nil // a container is not copied
	}
	o := f.opts
	values, err := a.relatedReader().related(ctx, a.table, o.LookedUpContext, row, o.LookedUpOccurrence, o.LookedUpField, reachFirst)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// A lookup that cannot be followed — no path, a field gone, a table
		// the account may not read, a solution not reachable — copies
		// nothing, as one that finds no record.
		return nil, false, nil
	}
	if len(values) > 0 {
		if isEmptyValue(values[0]) && o.LookedUpDontCopyIfEmpty {
			return nil, false, nil
		}
		return storedValue(values[0], f.fieldType), true, nil
	}
	switch o.LookedUpNoMatch {
	case autoenter.NoMatchConstant:
		return storedValue(o.LookedUpNoMatchValue, f.fieldType), true, nil
	case autoenter.NoMatchNextLower, autoenter.NoMatchNextHigher:
		value, found, err := a.nearest(ctx, f, row, o.LookedUpNoMatch == autoenter.NoMatchNextLower)
		if err != nil || !found {
			return nil, false, err
		}
		if isEmptyValue(value) && o.LookedUpDontCopyIfEmpty {
			return nil, false, nil
		}
		return storedValue(value, f.fieldType), true, nil
	}
	return nil, false, nil
}

// nearest reads the looked-up field of the related record whose match field
// holds the closest value below (lower) or above the record's: "Copy next
// lower / higher value". It needs a path of one relationship in this
// solution whose first criterion is =.
func (a *autoEnter) nearest(ctx context.Context, f *autoField, row map[string]interface{}, lower bool) (interface{}, bool, error) {
	o := f.opts
	path, err := a.path(ctx, o.LookedUpContext, o.LookedUpOccurrence)
	if err != nil || len(path) != 1 || path[0].to.External != nil {
		return nil, false, nil
	}
	terms, err := path[0].rel.matchTerms(path[0].from)
	if err != nil || len(terms) == 0 || terms[0].Op != "=" {
		return nil, false, nil
	}
	key := row[terms[0].SourceColumn]
	if emptyMatchValue(key) {
		return nil, false, nil
	}
	target := path[0].to.TableName
	if err := a.relatedReader().checkRead(ctx, target); err != nil {
		return nil, false, nil
	}
	types, err := a.s.tableFields(ctx, target)
	if err != nil {
		return nil, false, nil
	}
	if _, ok := types[o.LookedUpField]; !ok {
		return nil, false, nil
	}
	if _, ok := types[terms[0].TargetColumn]; !ok {
		return nil, false, nil
	}
	dialect := a.s.driver.Dialect()
	op, order := ">", "ASC"
	if lower {
		op, order = "<", "DESC"
	}
	match := dialect.QuoteIdentifier(terms[0].TargetColumn)
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s %s %s ORDER BY %s %s LIMIT 1",
		columnSQL(dialect, o.LookedUpField), dialect.QuoteIdentifier(target), match, op, dialect.Placeholder(1), match, order)
	rows, err := a.s.driver.DB().QueryContext(ctx, q, key)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	found, err := rowsToMaps(rows, types.pick([]string{o.LookedUpField}))
	if err != nil || len(found) == 0 {
		return nil, false, err
	}
	return typedValue(target, types, found[0], o.LookedUpField, nil), true, nil
}

// identity is a formula that reads one value, through which a value is
// written the way a field of a type stores it.
var identity = func() *calc.Expr {
	expr, err := calc.Parse("value")
	if err != nil {
		panic(err)
	}
	return expr
}()

// storedValue is a value as a field of fieldType stores it: a number as a
// number, a date and a timestamp in File4Base's notation, as a calculation
// answer is. A value that does not convert is kept as text in a text field
// and dropped elsewhere, as FileMaker leaves a field it cannot fill.
func storedValue(v interface{}, fieldType dbal.AgnosticFieldType) interface{} {
	if v == nil {
		return nil
	}
	answer, err := calc.EvaluateWith(identity, &calc.RecordContext{Record: calc.Record{"value": v}}, resultTypeOf(fieldType))
	if err != nil {
		if fieldType == dbal.FieldTypeText {
			return fmt.Sprint(v)
		}
		return nil
	}
	return answer
}

// same reports whether two values of a field are the same value, however
// each was written: "1.50" and 1.5, a date as text and as a time.
func (a *autoEnter) same(field string, x, y interface{}) bool {
	t := a.types.typeOf(field)
	if t == dbal.FieldTypeContainer {
		return fmt.Sprint(nilText(x)) == fmt.Sprint(nilText(y))
	}
	return canonical(t, x) == canonical(t, y)
}

func nilText(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func canonical(t dbal.AgnosticFieldType, v interface{}) string {
	if isEmptyValue(v) {
		return ""
	}
	if t == dbal.FieldTypeText || t == "" {
		return fmt.Sprint(nilText(v))
	}
	answer := storedValue(calc.Typed(resultTypeOf(t), v), t)
	if answer == nil {
		return fmt.Sprint(nilText(v))
	}
	return fmt.Sprint(answer)
}

// isEmptyValue reports whether a value is empty, as FileMaker's IsEmpty.
func isEmptyValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []byte:
		return len(t) == 0
	}
	return fmt.Sprint(v) == ""
}

// targetColumns are the fields of the related record the terms give a value.
func targetColumns(terms []matchTerm) []string {
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		out = append(out, t.TargetColumn)
	}
	return out
}

// stamp is the value a creation or modification stamp enters in a field of
// fieldType: the server's clock as Get ( CurrentDate ), Get ( CurrentTime )
// and Get ( CurrentTimestamp ) read it, or the account.
func (a *autoEnter) stamp(kind autoenter.Stamp, fieldType dbal.AgnosticFieldType, now time.Time) interface{} {
	wall := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
	switch kind {
	case autoenter.StampName, autoenter.StampAccountName:
		if a.s.account == "" {
			return nil
		}
		return a.s.account
	case autoenter.StampTime:
		if fieldType == dbal.FieldTypeTimestamp {
			// A time of day in a timestamp is on 0001-01-01, as the
			// calculation engine and FileMaker convert one.
			return time.Date(1, 1, 1, wall.Hour(), wall.Minute(), wall.Second(), 0, time.UTC).Format(time.RFC3339)
		}
		return wall.Format("15:04:05")
	case autoenter.StampTimestamp:
		if fieldType == dbal.FieldTypeTimestamp {
			return wall.Format(time.RFC3339)
		}
		return wall.Format("2006-01-02 15:04:05")
	default:
		if fieldType == dbal.FieldTypeTimestamp {
			return time.Date(wall.Year(), wall.Month(), wall.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		}
		return wall.Format("2006-01-02")
	}
}

// serialColumn is a field's catalog row and options.
type serialColumn struct {
	id, fieldType string
	options       sql.NullString
}

func (s *Service) serialColumn(ctx context.Context, q execer, tableName, field string, lock bool) (serialColumn, error) {
	dialect := s.driver.Dialect()
	query := `SELECT c.id, c.field_type, c.default_value FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE t.name = ` + dialect.Placeholder(1) + ` AND c.name = ` + dialect.Placeholder(2)
	if lock {
		query += lockClause(dialect)
	}
	var col serialColumn
	err := q.QueryRowContext(ctx, query, tableName, field).Scan(&col.id, &col.fieldType, &col.options)
	if errors.Is(err, sql.ErrNoRows) {
		return col, fmt.Errorf("%w: %s.%s", ErrUnknownField, tableName, field)
	}
	return col, err
}

// lockClause locks the catalog row a serial number is taken from. On
// PostgreSQL the lock does not block the foreign keys that point at it.
func lockClause(dialect dbal.Dialect) string {
	if dialect.Engine() == dbal.EnginePostgres {
		return " FOR NO KEY UPDATE"
	}
	return " FOR UPDATE"
}

// takeSerial takes the next serial value of a field and writes the one that
// follows, under a lock of the field's catalog row, so two writers never get
// the same value. Without a transaction to take it in, it is taken in one of
// its own, committed at once: a value taken by a write that then fails is
// not given back, as in FileMaker.
func (s *Service) takeSerial(ctx context.Context, q execer, columnID string) (string, error) {
	if q == nil {
		tx, err := s.driver.DB().BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		next, err := s.takeSerial(ctx, tx, columnID)
		if err != nil {
			return "", err
		}
		return next, tx.Commit()
	}
	dialect := s.driver.Dialect()
	var stored sql.NullString
	query := "SELECT default_value FROM sys_columns WHERE id = " + dialect.Placeholder(1) + lockClause(dialect)
	if err := q.QueryRowContext(ctx, query, columnID).Scan(&stored); err != nil {
		return "", fmt.Errorf("failed reading the next serial value: %w", err)
	}
	opts, err := autoenter.Parse(nullable(stored))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	next := opts.SerialNext
	following, err := autoenter.NextSerial(next, max(opts.SerialIncrement, 1))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	updated, err := autoenter.WithSerialNext(nullable(stored), following)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	query = "UPDATE sys_columns SET default_value = " + dialect.Placeholder(1) + " WHERE id = " + dialect.Placeholder(2)
	if _, err := q.ExecContext(ctx, query, updated, columnID); err != nil {
		return "", fmt.Errorf("failed writing the next serial value: %w", err)
	}
	return next, nil
}

func nullable(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// NextSerialValue is the value the next record gets in a serial number
// field: FileMaker's GetNextSerialValue.
func (s *Service) NextSerialValue(ctx context.Context, tableName, field string) (string, error) {
	col, err := s.serialColumn(ctx, s.driver.DB(), tableName, field, false)
	if err != nil {
		return "", err
	}
	opts, err := autoenter.Parse(nullable(col.options))
	if err != nil || !opts.SerialEnabled {
		return "", fmt.Errorf("%w: %s.%s", ErrNotSerial, tableName, field)
	}
	return opts.SerialNext, nil
}

// SetNextSerialValue sets the value the next record gets in a serial number
// field: FileMaker's Set Next Serial Value. It returns the value set.
func (s *Service) SetNextSerialValue(ctx context.Context, tableName, field, next string) (string, error) {
	next = strings.TrimSpace(next)
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	col, err := s.serialColumn(ctx, tx, tableName, field, true)
	if err != nil {
		return "", err
	}
	opts, err := autoenter.Parse(nullable(col.options))
	if err != nil || !opts.SerialEnabled {
		return "", fmt.Errorf("%w: %s.%s", ErrNotSerial, tableName, field)
	}
	updated, err := autoenter.WithSerialNext(nullable(col.options), next)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	if err := autoenter.Validate(col.fieldType, &updated); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	dialect := s.driver.Dialect()
	query := "UPDATE sys_columns SET default_value = " + dialect.Placeholder(1) + " WHERE id = " + dialect.Placeholder(2)
	if _, err := tx.ExecContext(ctx, query, updated, col.id); err != nil {
		return "", err
	}
	return next, tx.Commit()
}

// NextSerialValue implements calc.Serials: GetNextSerialValue in the
// formulas computed with related records. A field of another table is named
// Table::Field, by its table or one of its occurrences. A field with no
// serial number answers empty.
func (c *unstoredContext) NextSerialValue(field string) (string, error) {
	table, name := c.table, field
	if t, f, ok := strings.Cut(field, "::"); ok {
		table, name = strings.TrimSpace(t), strings.TrimSpace(f)
		if graph, err := c.reader.occurrenceGraph(c.ctx); err == nil {
			if o, ok := graph.byName[strings.ToLower(table)]; ok && o.external == nil {
				table = o.table
			}
		}
	}
	next, err := c.reader.s.NextSerialValue(c.ctx, table, name)
	if errors.Is(err, ErrNotSerial) {
		return "", nil
	}
	return next, err
}

// Get answers Get ( AccountName ) and Get ( UserName ) with the account the
// service reads as; the engine answers the clock and the other selectors.
func (c *unstoredContext) Get(selector string) (any, error) {
	switch selector {
	case "ACCOUNTNAME", "USERNAME":
		return c.reader.s.account, nil
	}
	return c.RecordContext.Get(selector)
}

// DuplicateRow creates a copy of a record, as FileMaker's Duplicate Record:
// every stored field is copied, and the auto-enter options enter new values,
// except that a looked-up value and a calculated value that does not replace
// are copied. Prohibit-modification does not stop it.
func (s *Service) DuplicateRow(ctx context.Context, tableName, id string) (map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	row, err := s.GetRow(ctx, tableName, id)
	if err != nil {
		return nil, err
	}
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return nil, err
	}
	ae, err := s.loadAutoEnter(ctx, tableName, fields)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{"id": true}
	for _, c := range calculations {
		skip[c.field] = true
	}
	for _, f := range ae.fields {
		if f.reentersOnDuplicate() {
			skip[f.name] = true
		}
	}
	record := make(map[string]interface{}, len(row))
	for key, value := range row {
		if skip[ae.baseOf(key)] || fields.typeOf(key) == "" || fields.typeOf(key) == dbal.FieldTypeSummary {
			continue
		}
		record[key] = value
	}
	inserted, err := s.insertRow(ctx, tableName, record, autoMode{})
	if err != nil {
		return nil, err
	}
	// A copied container keeps its file's name and type.
	db, dialect := s.driver.DB(), s.driver.Dialect()
	meta, err := containermeta.Load(ctx, db, dialect, tableName, []string{id})
	if err != nil {
		return nil, err
	}
	copied := map[string]containermeta.Entry{}
	for field, entry := range meta[id] {
		if _, ok := record[field]; ok {
			copied[field] = entry
		}
	}
	if err := containermeta.Put(ctx, db, dialect, tableName, fmt.Sprint(inserted["id"]), copied); err != nil {
		return nil, err
	}
	if len(copied) > 0 {
		return s.GetRow(ctx, tableName, fmt.Sprint(inserted["id"]))
	}
	return inserted, nil
}

// Relookup copies again the looked-up values of records whose lookups start
// from matchField, as FileMaker's Relookup Field Contents does for a found
// set. The records it changes are modified. It returns how many it changed.
func (s *Service) Relookup(ctx context.Context, tableName, matchField string, ids []string) (int, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return 0, err
	}
	if err := checkField(fields, tableName, matchField); err != nil {
		return 0, err
	}
	ae, err := s.loadAutoEnter(ctx, tableName, fields)
	if err != nil {
		return 0, err
	}
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return 0, err
	}
	ae.withCalculations(calculations)
	var lookups []*autoField
	for _, f := range ae.fields {
		if f.opts.LookedUpEnabled && f.opts.LookedUpOccurrence != "" && f.opts.LookedUpField != "" &&
			ae.lookupTriggers(ctx, f)[matchField] {
			lookups = append(lookups, f)
		}
	}
	if len(lookups) == 0 {
		return 0, fmt.Errorf("%w: no looked-up value of %s starts from the match field %s", ErrInvalidValue, tableName, matchField)
	}
	changed := 0
	for _, id := range ids {
		row, err := s.GetRow(ctx, tableName, id)
		if err != nil {
			return changed, err
		}
		view := ae.withCalculated(row)
		updates := map[string]interface{}{}
		for _, f := range lookups {
			value, ok, err := ae.lookup(ctx, f, view)
			if err != nil {
				return changed, err
			}
			if ok && !ae.same(f.name, value, row[f.name]) {
				updates[f.name] = value
			}
		}
		if len(updates) == 0 {
			continue
		}
		if _, err := s.updateRow(ctx, tableName, id, updates, autoMode{}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// applyAutoEnter applies a table's auto-enter options to a write: to a new
// record when id is empty, else to the changes of the record id.
func (s *Service) applyAutoEnter(
	ctx context.Context, tableName string, fields fieldSet, mode autoMode, record map[string]interface{}, id string,
) error {
	if mode.off {
		return nil
	}
	ae, err := s.loadAutoEnter(ctx, tableName, fields)
	if err != nil || !ae.active() {
		return err
	}
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return err
	}
	ae.withCalculations(calculations)
	if id == "" {
		return ae.enterNew(ctx, mode, record)
	}
	current, err := s.GetRow(ctx, tableName, id)
	if err != nil {
		return err
	}
	return ae.enterModified(ctx, mode, record, current)
}
