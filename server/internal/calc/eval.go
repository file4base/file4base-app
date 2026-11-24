package calc

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ResultType is what a formula's answer is stored as.
type ResultType string

const (
	ResultText      ResultType = "TEXT"
	ResultNumber    ResultType = "NUMBER"
	ResultDate      ResultType = "DATE"
	ResultTime      ResultType = "TIME"
	ResultTimestamp ResultType = "TIMESTAMP"
	ResultBoolean   ResultType = "BOOLEAN"
)

// ParseResultType reads the result type a field was saved with. The client
// writes it in its own spelling ("Text", "Number", "Date", "Time",
// "Timestamp"); anything unrecognised is text. A time is stored as a
// timestamp column, so "Time" keeps meaning a timestamp for storage; use
// ResultTime explicitly for a time of day.
func ParseResultType(s string) ResultType {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "NUMBER", "NUMERIC", "DECIMAL":
		return ResultNumber
	case "DATE":
		return ResultDate
	case "TIME", "TIMESTAMP", "DATETIME":
		return ResultTimestamp
	case "BOOLEAN", "BOOL":
		return ResultBoolean
	default:
		return ResultText
	}
}

// Record is the record a formula is evaluated against: field name to stored
// value. A name that is not in the record reads as empty.
type Record map[string]any

// Now is the clock formulas read; tests replace it.
var Now = time.Now

// ErrNotAvailable is returned by a Context that cannot answer a question, such
// as a related record on a server that only has the current one. Evaluation
// then fails with a message naming what was missing, rather than guessing.
var ErrNotAvailable = errors.New("not available here")

// Context is everything outside the formula that it may read. A caller
// provides what it has; RecordContext provides a single record.
type Context interface {
	// Field reads a field of the current record. repetition is 1-based; 0
	// means the field's current repetition.
	Field(name string, repetition int) (any, error)
	// Related reads a field through a table occurrence: every related record,
	// in the relationship's sort order. A formula reading Table::Field takes
	// the first; Sum, Count, List and the other aggregates take them all.
	Related(table, field string) ([]any, error)
	// Variable reads a $local or $$global variable, empty when it is not set.
	Variable(name string, repetition int) (any, error)
	// SetVariable sets one, for Let ( [ $x = … ] ; … ).
	SetVariable(name string, repetition int, value any) error
	// Get answers Get ( Selector ), with the selector in upper case. It returns
	// ErrNotAvailable for one it does not know; the clock and UUID selectors
	// are answered by the engine then.
	Get(selector string) (any, error)
	// Locale is how the solution writes numbers and dates as text.
	Locale() *Locale
}

// RecordContext evaluates against one record, with no related records, no
// variables and no session. It is what calculation fields have on the server.
type RecordContext struct {
	Record    Record
	Variables map[string]any
	Loc       *Locale
}

func (c *RecordContext) Field(name string, _ int) (any, error) {
	if c.Record == nil {
		return nil, nil
	}
	return c.Record[name], nil
}

func (c *RecordContext) Related(string, string) ([]any, error) { return nil, ErrNotAvailable }

func (c *RecordContext) Variable(name string, repetition int) (any, error) {
	return c.Variables[variableKey(name, repetition)], nil
}

func (c *RecordContext) SetVariable(name string, repetition int, v any) error {
	if c.Variables == nil {
		c.Variables = map[string]any{}
	}
	c.Variables[variableKey(name, repetition)] = v
	return nil
}

func (c *RecordContext) Get(string) (any, error) { return nil, ErrNotAvailable }
func (c *RecordContext) Locale() *Locale         { return c.Loc }

func variableKey(name string, repetition int) string {
	key := strings.ToLower(name)
	if repetition > 1 {
		return key + "[" + decimal.NewFromInt(int64(repetition)).String() + "]"
	}
	return key
}

// maxDepth bounds custom-function recursion; FileMaker stops at 10,000 calls
// deep and returns "?".
const maxDepth = 10000

// maxSteps bounds the work one evaluation may do, so a While or a recursive
// function that never ends cannot hold a request forever.
const maxSteps = 5_000_000

type evaluator struct {
	ctx       Context
	loc       *Locale
	lib       *Library
	known     *names // the names the formula was parsed with, for Evaluate
	precision int32  // decimal places of computed numbers; SetPrecision raises it
	locals    []map[string]value
	depth     int
	steps     int
	now       time.Time
}

// Evaluate computes the formula against a record and returns the answer in the
// shape the result type asks for, ready to be stored. An empty answer is nil.
func Evaluate(expr *Expr, record Record, result ResultType) (any, error) {
	return EvaluateWith(expr, &RecordContext{Record: record}, result)
}

// EvaluateWith computes the formula in a context and stores the answer as the
// result type asks.
func EvaluateWith(expr *Expr, ctx Context, result ResultType) (any, error) {
	if expr == nil {
		return nil, nil
	}
	v, err := EvaluateValue(expr, ctx)
	if err != nil {
		return nil, err
	}
	return v.store(expr.root.pos(), result, ctx.Locale())
}

// Value is the typed answer of a formula: text, a number, a date, a time or a
// timestamp, or nothing.
type Value = value

// EvaluateValue computes the formula and returns its typed answer, for a
// caller that keeps the type, such as a script variable.
func EvaluateValue(expr *Expr, ctx Context) (Value, error) {
	if expr == nil {
		return empty, nil
	}
	if ctx == nil {
		ctx = &RecordContext{}
	}
	ev := &evaluator{ctx: ctx, loc: ctx.Locale().orISO(), lib: expr.lib, known: expr.known, precision: precision, now: Now()}
	return ev.eval(expr.root)
}

// Text is the value as FileMaker writes it as text under a locale.
func (v Value) Text(loc *Locale) string { return v.asText(loc) }

// IsEmpty reports whether the value is empty.
func (v Value) IsEmpty() bool { return v.isEmpty() }

// Bool is the value as the test of an If.
func (v Value) Bool(loc *Locale) bool { return v.asBool(loc) }

// Kind names the value's type: "empty", "text", "number", "date", "time" or
// "timestamp".
func (v Value) Kind() string {
	return [...]string{"empty", "text", "number", "date", "time", "timestamp"}[kindIndex(v.kind)]
}

func kindIndex(k valueKind) int {
	switch k {
	case kindText:
		return 1
	case kindNumber:
		return 2
	case kindDate:
		return 3
	case kindTime:
		return 4
	case kindTimestamp:
		return 5
	}
	return 0
}

// store converts the answer to what the column holds.
func (v value) store(at int, result ResultType, loc *Locale) (any, error) {
	if v.isEmpty() {
		return nil, nil
	}
	loc = loc.orISO()
	switch result {
	case ResultNumber:
		n, ok := v.numberOrEmpty(loc)
		if !ok {
			return nil, nil
		}
		// Exact: a float64 would turn 9007199254740993 into ...992. The
		// drivers write the text into the NUMERIC column as it is, and JSON
		// writes it as a number.
		return json.Number(n.String()), nil
	case ResultBoolean:
		return v.asBool(loc), nil
	case ResultDate:
		day, ok := v.asDay(loc)
		if !ok {
			return nil, errorAt(at, "the result of the formula is not a date: %q", v.asText(loc))
		}
		y, m, d := civil(day)
		return time.Date(int(y), time.Month(m), int(d), 0, 0, 0, 0, time.UTC).Format("2006-01-02"), nil
	case ResultTime:
		secs, ok := v.asSeconds(loc)
		if !ok {
			return nil, errorAt(at, "the result of the formula is not a time: %q", v.asText(loc))
		}
		return ISOLocale.formatTime(secs), nil
	case ResultTimestamp:
		secs, ok := v.asTimestampSeconds(loc)
		if !ok {
			return nil, errorAt(at, "the result of the formula is not a timestamp: %q", v.asText(loc))
		}
		return toTime(secs).Format(time.RFC3339), nil
	default:
		return v.asText(loc), nil
	}
}

func (ev *evaluator) tick(at int) error {
	ev.steps++
	if ev.steps > maxSteps {
		return errorAt(at, "the formula did too much work and was stopped")
	}
	return nil
}

func (ev *evaluator) eval(n node) (value, error) {
	if err := ev.tick(n.pos()); err != nil {
		return empty, err
	}
	switch t := n.(type) {
	case numberNode:
		return numberValue(t.value), nil
	case textNode:
		return textValue(t.value), nil

	case fieldNode:
		return ev.readField(t)

	case variableNode:
		rep, err := ev.repetition(t.repetition)
		if err != nil {
			return empty, err
		}
		raw, err := ev.ctx.Variable(t.name, rep)
		if err != nil {
			return empty, errorAt(t.at, "%s cannot be read: %v", t.name, err)
		}
		return fromStored(raw), nil

	case localNode:
		for k := len(ev.locals) - 1; k >= 0; k-- {
			if v, ok := ev.locals[k][t.name]; ok {
				return v, nil
			}
		}
		return empty, nil

	case unaryNode:
		operand, err := ev.eval(t.operand)
		if err != nil {
			return empty, err
		}
		if t.op == "NOT" {
			return boolValue(!operand.asBool(ev.loc)), nil
		}
		return negate(operand, ev.loc), nil

	case binaryNode:
		return ev.evalBinary(t)

	case letNode:
		scope := map[string]value{}
		ev.locals = append(ev.locals, scope)
		defer func() { ev.locals = ev.locals[:len(ev.locals)-1] }()
		if err := ev.bind(scope, t.bindings, t.at); err != nil {
			return empty, err
		}
		return ev.eval(t.body)

	case getNode:
		return ev.get(t)

	case whileNode:
		return ev.evalWhile(t)

	case pairNode:
		return empty, errorAt(t.at, "[ … ; … ] is only meaningful inside Substitute")

	case callNode:
		return ev.call(t)
	}
	return empty, errorAt(n.pos(), "this part of the formula cannot be evaluated")
}

// maxIterations is FileMaker's own limit on a While loop.
const maxIterations = 50000

func (ev *evaluator) bind(scope map[string]value, bindings []letBinding, at int) error {
	for _, b := range bindings {
		v, err := ev.eval(b.value)
		if err != nil {
			return err
		}
		if strings.HasPrefix(b.name, "$") {
			if err := ev.ctx.SetVariable(b.name, 1, v); err != nil {
				return errorAt(at, "%s cannot be set: %v", b.name, err)
			}
			continue
		}
		scope[b.name] = v
	}
	return nil
}

func (ev *evaluator) evalWhile(t whileNode) (value, error) {
	scope := map[string]value{}
	ev.locals = append(ev.locals, scope)
	defer func() { ev.locals = ev.locals[:len(ev.locals)-1] }()
	if err := ev.bind(scope, t.initial, t.at); err != nil {
		return empty, err
	}
	for i := 0; ; i++ {
		test, err := ev.eval(t.condition)
		if err != nil {
			return empty, err
		}
		if !test.asBool(ev.loc) {
			break
		}
		if i >= maxIterations {
			return empty, errorAt(t.at, "While stopped after %d iterations", maxIterations)
		}
		if err := ev.bind(scope, t.logic, t.at); err != nil {
			return empty, err
		}
	}
	return ev.eval(t.result)
}

func negate(v value, loc *Locale) value {
	switch v.kind {
	case kindEmpty:
		return empty
	case kindText:
		if strings.TrimSpace(v.text) == "" {
			return empty
		}
		return numberValue(v.asNumber(loc).Neg())
	}
	return numberValue(v.num.Neg()) // FileMaker: -Date ( 11 ; 24 ; 2026 ) is -739944
}

func (ev *evaluator) repetition(n node) (int, error) {
	if n == nil {
		return 0, nil
	}
	v, err := ev.eval(n)
	if err != nil {
		return 0, err
	}
	r := v.asNumber(ev.loc).IntPart()
	if r < 1 {
		r = 1
	}
	return int(r), nil
}

func (ev *evaluator) readField(t fieldNode) (value, error) {
	if t.table != "" {
		values, err := ev.ctx.Related(t.table, t.name)
		if err != nil {
			return empty, ev.contextError(t.at, t.table+"::"+t.name, err)
		}
		if len(values) == 0 {
			return empty, nil
		}
		return fromStored(values[0]), nil
	}
	rep, err := ev.repetition(t.repetition)
	if err != nil {
		return empty, err
	}
	raw, err := ev.ctx.Field(t.name, rep)
	if err != nil {
		return empty, ev.contextError(t.at, t.name, err)
	}
	return fromStored(raw), nil
}

// relatedValues reads every value of a reference, for an aggregate.
func (ev *evaluator) relatedValues(t fieldNode) ([]value, error) {
	values, err := ev.ctx.Related(t.table, t.name)
	if err != nil {
		return nil, ev.contextError(t.at, t.table+"::"+t.name, err)
	}
	out := make([]value, len(values))
	for i, raw := range values {
		out[i] = fromStored(raw)
	}
	return out, nil
}

func (ev *evaluator) contextError(at int, what string, err error) error {
	if errors.Is(err, ErrNotAvailable) {
		return errorAt(at, "%s cannot be read here", what)
	}
	return errorAt(at, "%s cannot be read: %v", what, err)
}

func (ev *evaluator) evalBinary(t binaryNode) (value, error) {
	// AND and OR stop as soon as the answer is known, so the other side may
	// be something that would fail to evaluate.
	switch t.op {
	case "AND", "OR":
		left, err := ev.eval(t.left)
		if err != nil {
			return empty, err
		}
		if t.op == "AND" && !left.asBool(ev.loc) {
			return boolValue(false), nil
		}
		if t.op == "OR" && left.asBool(ev.loc) {
			return boolValue(true), nil
		}
		right, err := ev.eval(t.right)
		if err != nil {
			return empty, err
		}
		return boolValue(right.asBool(ev.loc)), nil
	}

	left, err := ev.eval(t.left)
	if err != nil {
		return empty, err
	}
	right, err := ev.eval(t.right)
	if err != nil {
		return empty, err
	}

	switch t.op {
	case "XOR":
		return boolValue(left.asBool(ev.loc) != right.asBool(ev.loc)), nil
	case "&":
		return concat(left, right, ev.loc), nil
	case "+", "-", "*", "/", "^":
		return ev.arithmetic(t, left, right)
	}
	cmp := compare(left, right, ev.loc)
	switch t.op {
	case "=":
		return boolValue(cmp == 0), nil
	case "<>":
		return boolValue(cmp != 0), nil
	case "<":
		return boolValue(cmp < 0), nil
	case "<=":
		return boolValue(cmp <= 0), nil
	case ">":
		return boolValue(cmp > 0), nil
	case ">=":
		return boolValue(cmp >= 0), nil
	}
	return empty, errorAt(t.at, "unknown operator %q", t.op)
}

// concat joins two values as text. Joining nothing to nothing stays empty.
func concat(a, b value, loc *Locale) value {
	if a.isEmpty() && b.isEmpty() {
		return empty
	}
	return textValue(a.asText(loc) + b.asText(loc))
}

// precision is how many decimal places FileMaker shows of a computed number,
// unless SetPrecision asks for more. A division keeps two more, so 1/3*3 is 1.
const precision = 16

// maxPrecision is the most decimal places SetPrecision gives, as in FileMaker.
const maxPrecision = 400

// dateSeconds is a day number as seconds since 1 January 0001.
func dateSeconds(day decimal.Decimal) decimal.Decimal {
	return day.Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(secondsPerDay))
}

// arithmetic follows FileMaker: text counts by the number it holds, a date
// plus a number is a date, a date minus a date is a number of days, and a
// timestamp or time shifts by seconds. Division by zero has no answer.
func (ev *evaluator) arithmetic(t binaryNode, left, right value) (value, error) {
	if left.isEmpty() && right.isEmpty() {
		return empty, nil
	}
	a, b := left.asNumber(ev.loc), right.asNumber(ev.loc)
	// A date meeting a time or a timestamp counts in seconds.
	if left.kind == kindDate && (right.kind == kindTime || right.kind == kindTimestamp) {
		a = dateSeconds(a)
	}
	if right.kind == kindDate && (left.kind == kindTime || left.kind == kindTimestamp) {
		b = dateSeconds(b)
	}
	var n decimal.Decimal
	switch t.op {
	case "+":
		n = a.Add(b)
	case "-":
		n = a.Sub(b)
	case "*":
		n = a.Mul(b)
	case "/":
		if b.IsZero() {
			return empty, errorAt(t.at, "division by zero")
		}
		n = a.DivRound(b, ev.precision+2)
	case "^":
		p, err := power(a, b, ev.precision)
		if err != nil {
			return empty, errorAt(t.at, "%v", err)
		}
		return numberValue(p), nil
	}
	return value{kind: arithmeticKind(t.op, left.kind, right.kind), num: n}, nil
}

// arithmeticKind is the type of a + or - result, as FileMaker gives it: a
// date, time or timestamp shifted by a number keeps its type, a date plus a
// time is a timestamp, two times add or subtract to a time, and two moments
// subtract to a duration — days for two dates, a time when a timestamp is
// involved. Anything else, and every *, / and ^, is a number.
func arithmeticKind(op string, a, b valueKind) valueKind {
	temporal := func(k valueKind) bool { return k == kindDate || k == kindTime || k == kindTimestamp }
	switch op {
	case "+":
		switch {
		case temporal(a) && !temporal(b):
			return a
		case temporal(b) && !temporal(a):
			return b
		case a == kindDate && b == kindTime, a == kindTime && b == kindDate:
			return kindTimestamp
		case a == kindTime && b == kindTime:
			return kindTime
		case a == kindTimestamp && b == kindTime:
			return kindTimestamp
		}
	case "-":
		switch {
		case temporal(a) && !temporal(b):
			return a
		case a == kindTimestamp && b == kindTime:
			return kindTimestamp
		case a == kindTime && b == kindTime:
			return kindTime
		case a == kindTimestamp && (b == kindTimestamp || b == kindDate), a == kindDate && b == kindTimestamp:
			return kindTime
		}
	}
	return kindNumber
}

// compare orders two values as =, < and > do. When either side is text, or
// nothing, both compare as text in the file's collation, so "10" < 9 is true
// and "abc" is never 0; otherwise they compare as numbers, which is how dates
// and times order.
func compare(a, b value, loc *Locale) int {
	if a.kind == kindText || b.kind == kindText || a.kind == kindEmpty || b.kind == kindEmpty {
		return collate(a.asText(loc), b.asText(loc))
	}
	return a.num.Cmp(b.num)
}
