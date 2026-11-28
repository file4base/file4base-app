package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrInvalidExpression is returned for an expression that does not parse or
// cannot be computed. Its message carries the position the parser stopped at,
// which is what the Data Viewer shows (#50).
var ErrInvalidExpression = errors.New("invalid expression")

// Evaluation is the answer to one watched expression.
type Evaluation struct {
	// Expression as it was given.
	Expression string `json:"expression"`

	// Value is the result in the shape the result type asks for, and Text is
	// the same thing written out, so a viewer can show something without
	// deciding how to format every type itself.
	Value interface{} `json:"value"`
	Text  string      `json:"text"`

	ResultType string `json:"result_type"`

	// Fields are the fields the expression reads, and UnknownFields are the
	// ones the table does not have. A missing field reads as empty rather
	// than failing — which is the engine's rule — so the viewer says which
	// ones they are instead of showing a quietly wrong answer.
	Fields        []string `json:"fields"`
	UnknownFields []string `json:"unknown_fields,omitempty"`

	// RecordID is the record it was evaluated against, empty when there was
	// none.
	RecordID string `json:"record_id,omitempty"`

	// Variables are the $ and $$ variables the expression set, with Let
	// ( [ $x = … ] ; … ), as text, by lower-case name: a script keeps them.
	Variables map[string]string `json:"variables,omitempty"`
}

// EvaluationEnv is what a script gives the expressions it evaluates: its $
// and $$ variables, and the answers to Get functions only the client knows
// (Get ( ScriptParameter ), Get ( LayoutName ), …), by selector.
type EvaluationEnv struct {
	Variables map[string]any
	Get       map[string]any
	// Values are the fields of the record as it is being edited and not
	// committed yet (a new record especially), by name, read in place of
	// what is stored.
	Values map[string]any
	// Self is, for the expression at the same index, the field Self reads
	// (see SelfField); a nil entry, or none, leaves Self unknown.
	Self []*SelfField
	// Context is the record of the layout a portal shows the records
	// evaluated on (see ContextRecord); nil evaluates them on their own.
	Context *ContextRecord
	// External says the records are of an occurrence of another solution's
	// table, as the occurrence's route knows it. The occurrence evaluated
	// in a Context must be such an occurrence when it is set, and one of
	// this solution's tables when it is not (ErrOccurrenceElsewhere).
	External bool
	// PortalRow is, for a layout's record, the active portal row (see
	// PortalRow); nil when no portal row is active. Not with a Context.
	PortalRow *PortalRow
}

// PortalRow is the active row of a portal on a layout, by the portal's
// occurrence (name or id) and the row's record id, that a script's
// formulas are evaluated with, as FileMaker evaluates them while a portal
// row is active: the layout's record stays the current record, and the
// row is the current record of the portal's occurrence. A reference to a
// field of that occurrence reads the row, one through it the records
// related to the row; an aggregate of the occurrence (Count, Sum, List…)
// reads every record related to the layout's record, and any other
// occurrence is related to the layout's record.
type PortalRow struct {
	Occurrence string `json:"occurrence"`
	RecordID   string `json:"record_id"`
}

// ContextRecord is the record of a layout, seen from its occurrence (by
// name or id), that a record of a portal on it is evaluated in, as
// FileMaker evaluates a portal row's conditions and the portal's filter:
// from the layout, with the row as the current record of the portal's
// occurrence. A reference to a field of that occurrence then reads the
// row, and so does one through it; a reference to the layout's own
// occurrence reads the layout's record, and any other is related to the
// layout's record, never read back from the row through a relationship.
// An aggregate of the portal's occurrence (Count, Sum, List, Last…) reads
// every record related to the layout's record. A field named without an
// occurrence reads the row, as the formulas bound for a portal write the
// row's fields.
type ContextRecord struct {
	Occurrence string `json:"occurrence"`
	RecordID   string `json:"record_id"`
}

// envContext answers the Get functions the environment gives, and the
// others as the record's context does.
type envContext struct {
	*unstoredContext
	get  map[string]any
	self *SelfField
}

func (c *envContext) Get(selector string) (any, error) {
	if selector == "SELF" && c.self != nil {
		return c.self.read(c.unstoredContext)
	}
	if v, ok := c.get[selector]; ok {
		return v, nil
	}
	return c.unstoredContext.Get(selector)
}

// SelfField is what Self reads in a layout object's conditions: the field
// the object shows, of the record evaluated (no occurrence) or of an
// occurrence, by name, seen from the one the expressions are evaluated
// from, with the repetition the object shows (0 and 1 are the first); or,
// for a text or a button, the text it shows, which FileMaker's Self
// answers there.
type SelfField struct {
	Occurrence string  `json:"occurrence,omitempty"`
	Field      string  `json:"field,omitempty"`
	Repetition int     `json:"repetition,omitempty"`
	Text       *string `json:"text,omitempty"`
}

// bound reports whether f names a field or gives a text.
func (f *SelfField) bound() bool {
	return f != nil && (f.Text != nil || strings.TrimSpace(f.Field) != "")
}

// read answers Self: the text given, or the field's value in the record
// evaluated, or in the first record related through the occurrence, in the
// repetition given, as a reference to that field reads it. Self is answered
// as a value, not written into the formula, so a Let variable of the same
// name does not capture it.
func (f *SelfField) read(c *unstoredContext) (any, error) {
	if f.Text != nil {
		return *f.Text, nil
	}
	rep := max(f.Repetition, 1)
	if f.Occurrence == "" {
		return c.Field(f.Field, rep)
	}
	name := f.Field
	if rep > 1 {
		name += "[" + strconv.Itoa(rep) + "]"
	}
	values, err := c.RelatedFirst(f.Occurrence, name)
	if err != nil || len(values) == 0 {
		return nil, err
	}
	return values[0], nil
}

// variableInput is a variable as a script sends it: a number written as a
// plain decimal number is a number, as Set Variable [ $n ; 10 ] keeps one,
// and any other text stays text (a leading zero, as in "007", too).
func variableInput(v any) any {
	switch t := v.(type) {
	case nil, bool, float64, json.Number:
		return t
	case string:
		if plainNumber.MatchString(t) {
			return json.Number(t)
		}
		return t
	}
	return fmt.Sprint(v) // a list or an object is not a value a variable holds
}

var plainNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// EvaluateExpression computes one expression against one record of a table,
// with the engine the calculation fields use, so the viewer and the field
// agree (#50).
//
// It only reads: nothing it is given can write, because evaluating a formula
// is all the engine can do.
func (s *Service) EvaluateExpression(
	ctx context.Context, tableName, recordID, expression, resultType string,
) (*Evaluation, error) {
	return s.EvaluateExpressionIn(ctx, tableName, recordID, expression, resultType, EvaluationEnv{})
}

// EvaluateExpressionIn is EvaluateExpression with a script's variables and
// Get answers: how a script step evaluates a formula the client cannot,
// with the solution's custom functions, in one request.
func (s *Service) EvaluateExpressionIn(
	ctx context.Context, tableName, recordID, expression, resultType string, in EvaluationEnv,
) (*Evaluation, error) {
	return s.evaluateOne(ctx, tableName, "", recordID, expression, resultType, in)
}

// EvaluateExpressionFrom is EvaluateExpression seen from the occurrence
// occurrence of the table, by name or id: what a related field the formula
// reads is related to, as a formula on a layout reads from the layout's
// occurrence. Empty is the table's own occurrence.
func (s *Service) EvaluateExpressionFrom(
	ctx context.Context, tableName, occurrence, recordID, expression, resultType string,
) (*Evaluation, error) {
	return s.evaluateOne(ctx, tableName, occurrence, recordID, expression, resultType, EvaluationEnv{})
}

func (s *Service) evaluateOne(
	ctx context.Context, tableName, occurrence, recordID, expression, resultType string, in EvaluationEnv,
) (*Evaluation, error) {
	evaluations, err := s.EvaluateExpressionsIn(ctx, tableName, occurrence, recordID, []string{expression}, resultType, in)
	if err != nil {
		return nil, err
	}
	return evaluations[0].Evaluation, evaluations[0].Err
}

// ExpressionResult is one expression's answer, or why it has none.
type ExpressionResult struct {
	*Evaluation
	Err error
}

// EvaluateExpressions evaluates several expressions against one record,
// read once, seen from occurrence as EvaluateExpressionFrom is. An
// expression that does not parse or evaluate has its own error (wrapping
// ErrInvalidExpression); the error returned is about the record or the
// table, and then nothing is evaluated.
func (s *Service) EvaluateExpressions(
	ctx context.Context, tableName, occurrence, recordID string, expressions []string, resultType string,
) ([]ExpressionResult, error) {
	return s.EvaluateExpressionsIn(ctx, tableName, occurrence, recordID, expressions, resultType, EvaluationEnv{})
}

// EvaluateExpressionsIn is EvaluateExpressions with a script's variables,
// Get answers and the values of a record being edited (in). Every
// expression starts from the variables given; the ones it sets are its own.
// The solution's custom functions are known to every expression.
func (s *Service) EvaluateExpressionsIn(
	ctx context.Context, tableName, occurrence, recordID string, expressions []string, resultType string,
	in EvaluationEnv,
) ([]ExpressionResult, error) {
	e, err := s.newEvaluator(ctx, tableName, occurrence, expressions, resultType, in)
	if err != nil {
		return nil, err
	}
	return e.evaluate(ctx, recordID)
}

// EvaluateExpressionsForRecords is EvaluateExpressionsIn for several
// records of the table, all evaluated in the same context: the layout's
// record, the occurrence and the solution the records are read in are
// resolved once, and one reader serves every record, so what is read for
// one (the layout's aggregates, a related record) the others reuse. The
// answer is in the order of recordIDs, nil for a record not found (deleted
// since it was listed); any other error is the request's, and nothing is
// answered.
func (s *Service) EvaluateExpressionsForRecords(
	ctx context.Context, tableName, occurrence string, recordIDs []string, expressions []string, resultType string,
	in EvaluationEnv,
) ([][]ExpressionResult, error) {
	e, err := s.newEvaluator(ctx, tableName, occurrence, expressions, resultType, in)
	if err != nil {
		return nil, err
	}
	out := make([][]ExpressionResult, len(recordIDs))
	for i, id := range recordIDs {
		results, err := e.evaluate(ctx, id)
		if errors.Is(err, ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i] = results
	}
	return out, nil
}

// evaluator evaluates expressions, parsed once, against records evaluated
// in one scope (see evaluationScope).
type evaluator struct {
	s           *Service
	reader      *relatedReader
	scope       *evaluationScope
	expressions []string
	parsed      []*calc.Expr
	invalid     []error
	reads       []string
	result      calc.ResultType
	variables   map[string]any
	get         map[string]any
	in          EvaluationEnv
}

func (s *Service) newEvaluator(
	ctx context.Context, tableName, occurrence string, expressions []string, resultType string, in EvaluationEnv,
) (*evaluator, error) {
	lib, err := LoadLibrary(ctx, s.driver)
	if err != nil {
		return nil, err
	}
	// One reader serves every expression and every record, so what one
	// reads the others reuse.
	reader := s.newRelatedReader()
	scope, err := reader.evaluationScope(ctx, tableName, occurrence, in.Context, in.External, in.PortalRow)
	if err != nil {
		return nil, err
	}
	if len(in.Values) > 0 && scope.database != "" {
		return nil, fmt.Errorf("%w: the values of a record being edited are not evaluated in another solution", ErrInvalidValue)
	}
	e := &evaluator{s: s, reader: reader, scope: scope, expressions: expressions, in: in,
		parsed: make([]*calc.Expr, len(expressions)), invalid: make([]error, len(expressions)),
		result: calc.ParseResultType(resultType), variables: make(map[string]any, len(in.Variables)),
		get: make(map[string]any, len(in.Get))}
	for i, expression := range expressions {
		if strings.TrimSpace(expression) == "" {
			e.invalid[i] = fmt.Errorf("%w: there is nothing to evaluate", ErrInvalidExpression)
			continue
		}
		expr, err := calc.ParseWith(expression, calc.Options{Library: lib})
		if err != nil {
			e.invalid[i] = fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
			continue
		}
		e.parsed[i] = expr
		e.reads = append(e.reads, expr.Fields()...)
	}
	for name, v := range in.Variables {
		e.variables[strings.ToLower(strings.TrimSpace(name))] = variableInput(v)
	}
	for selector, v := range in.Get {
		e.get[strings.ToUpper(strings.TrimSpace(selector))] = v
	}
	return e, nil
}

// evaluate evaluates the expressions against the record recordID (none when
// empty).
func (e *evaluator) evaluate(ctx context.Context, recordID string) ([]ExpressionResult, error) {
	at, err := e.scope.at(ctx, recordID)
	if err != nil {
		return nil, err
	}
	fields, row := at.fields, at.row
	if len(e.in.Values) > 0 {
		if err := e.s.overlayEdit(ctx, e.reader, at.table, fields, row, e.in.Values, e.reads); err != nil {
			return nil, err
		}
	}
	record := calc.Record{}
	for name := range row {
		record[name] = inSolution(typedValue(at.table, fields, row, name, at.describe), at.database)
	}

	out := make([]ExpressionResult, len(e.expressions))
	for i, expr := range e.parsed {
		if expr == nil {
			out[i].Err = e.invalid[i]
			continue
		}
		read := expr.Fields()
		unknown := []string{}
		for _, name := range read {
			if _, ok := fields[name]; !ok {
				unknown = append(unknown, name)
			}
		}
		sort.Strings(unknown)
		scope := make(map[string]any, len(e.variables))
		for name, v := range e.variables {
			scope[name] = v
		}
		// Related fields are read from the occurrence given, or the table's
		// own occurrence, as a formula typed in the Data Viewer is in
		// FileMaker.
		env := &envContext{unstoredContext: &unstoredContext{
			RecordContext: calc.RecordContext{Record: record, Repeats: fields.repeating(), Variables: scope},
			reader:        e.reader, ctx: ctx, table: at.from.table, occurrence: at.from.occurrence, row: at.from.row,
			pin: at.pin}, get: e.get}
		if i < len(e.in.Self) && e.in.Self[i].bound() {
			env.self = e.in.Self[i]
		}
		value, err := calc.EvaluateWith(expr, env, e.result)
		if err != nil {
			out[i].Err = fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
			continue
		}
		var set map[string]string
		for name, v := range env.Variables {
			if given, ok := e.variables[name]; ok && given == v {
				continue
			}
			if set == nil {
				set = map[string]string{}
			}
			set[name] = variableText(v)
		}
		out[i].Evaluation = &Evaluation{
			Expression:    e.expressions[i],
			Value:         value,
			Text:          evaluationText(value, e.result),
			ResultType:    string(e.result),
			Fields:        read,
			UnknownFields: unknown,
			RecordID:      recordID,
			Variables:     set,
		}
	}
	return out, nil
}

// variableText is a variable's value as a script keeps it, as text.
func variableText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case calc.Value:
		return t.Text(nil)
	case string:
		return t
	}
	return fmt.Sprint(v)
}

// evaluationText writes the answer the way the viewer shows it, in the same
// spelling an export uses, so a date reads as a date. An empty answer is
// empty text, not "null": a formula that comes to nothing is not an error,
// and the viewer marks it as empty itself.
func evaluationText(value interface{}, result calc.ResultType) string {
	if value == nil {
		return ""
	}
	if box, ok := value.(calc.Container); ok {
		return box.Filename // a container reads as its name, as GetAsText does
	}
	return exportText(storageTypeOf(result), value)
}

// storageTypeOf is the column type a result of this kind would be stored in.
func storageTypeOf(result calc.ResultType) dbal.AgnosticFieldType {
	switch result {
	case calc.ResultNumber:
		return dbal.FieldTypeNumber
	case calc.ResultDate:
		return dbal.FieldTypeDate
	case calc.ResultTimestamp:
		return dbal.FieldTypeTimestamp
	case calc.ResultBoolean:
		return dbal.FieldTypeBoolean
	default:
		return dbal.FieldTypeText
	}
}

// overlayEdit lays the values of a record being edited over row, the record
// as stored (empty for a new one), and recomputes what they change: the
// stored calculations, and the unstored ones among reads and those they
// read. The values given for calculation fields, and for fields the table
// does not have, are ignored: the formula owns the former.
func (s *Service) overlayEdit(ctx context.Context, reader *relatedReader, tableName string, fields fieldSet,
	row, values map[string]interface{}, reads []string) error {
	calculations, err := reader.tableCalculations(ctx, tableName)
	if err != nil {
		return err
	}
	isCalculation := make(map[string]bool, len(calculations))
	for _, c := range calculations {
		isCalculation[c.field] = true
	}
	for name, v := range values {
		if _, known := fields[name]; known && !isCalculation[name] && name != "id" {
			row[name] = v
		}
	}
	for _, c := range calculations {
		if c.unstored {
			continue
		}
		if answers, err := c.evaluate(row); err == nil {
			for key, answer := range answers {
				row[key] = answer
			}
		}
	}
	return reader.fill(ctx, tableName, []map[string]interface{}{row}, reads)
}
