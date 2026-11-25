package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
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
}

// envContext answers the Get functions the environment gives, and the
// others as the record's context does.
type envContext struct {
	*unstoredContext
	get map[string]any
}

func (c *envContext) Get(selector string) (any, error) {
	if v, ok := c.get[selector]; ok {
		return v, nil
	}
	return c.unstoredContext.Get(selector)
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
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: there is nothing to evaluate", ErrInvalidExpression)
	}

	lib, err := LoadLibrary(ctx, s.driver)
	if err != nil {
		return nil, err
	}
	expr, err := calc.ParseWith(expression, calc.Options{Library: lib})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
	}

	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}

	reader := s.newRelatedReader()
	record := calc.Record{}
	row := map[string]interface{}{}
	if recordID != "" {
		if row, err = s.GetRow(ctx, tableName, recordID); err != nil {
			return nil, err
		}
	}
	if len(in.Values) > 0 {
		if err := s.overlayEdit(ctx, reader, tableName, fields, row, in.Values, expr.Fields()); err != nil {
			return nil, err
		}
	}
	for name := range row {
		record[name] = typedValue(tableName, fields, row, name, s.describeContainer(ctx))
	}

	read := expr.Fields()
	unknown := []string{}
	for _, name := range read {
		if _, ok := fields[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)

	result := calc.ParseResultType(resultType)
	// Related fields are read from the table's own occurrence, as a formula
	// typed in the Data Viewer is in FileMaker.
	variables := make(map[string]any, len(in.Variables))
	for name, v := range in.Variables {
		variables[strings.ToLower(strings.TrimSpace(name))] = variableInput(v)
	}
	get := make(map[string]any, len(in.Get))
	for selector, v := range in.Get {
		get[strings.ToUpper(strings.TrimSpace(selector))] = v
	}
	scope := make(map[string]any, len(variables))
	for name, v := range variables {
		scope[name] = v
	}
	env := &envContext{unstoredContext: &unstoredContext{
		RecordContext: calc.RecordContext{Record: record, Repeats: fields.repeating(), Variables: scope},
		reader:        reader, ctx: ctx, table: tableName, row: row}, get: get}
	value, err := calc.EvaluateWith(expr, env, result)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
	}
	var set map[string]string
	for name, v := range env.Variables {
		if given, ok := variables[name]; ok && given == v {
			continue
		}
		if set == nil {
			set = map[string]string{}
		}
		set[name] = variableText(v)
	}

	return &Evaluation{
		Expression:    expression,
		Value:         value,
		Text:          evaluationText(value, result),
		ResultType:    string(result),
		Fields:        read,
		UnknownFields: unknown,
		RecordID:      recordID,
		Variables:     set,
	}, nil
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
