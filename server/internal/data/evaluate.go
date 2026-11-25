package data

import (
	"context"
	"errors"
	"fmt"
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
}

// EvaluateExpression computes one expression against one record of a table,
// with the engine the calculation fields use, so the viewer and the field
// agree (#50).
//
// It only reads: nothing it is given can write, because evaluating a formula
// is all the engine can do.
func (s *Service) EvaluateExpression(
	ctx context.Context, tableName, recordID, expression, resultType string,
) (*Evaluation, error) {
	return s.EvaluateExpressionFrom(ctx, tableName, "", recordID, expression, resultType)
}

// EvaluateExpressionFrom is EvaluateExpression seen from the occurrence
// occurrence of the table, by name or id: what a related field the formula
// reads is related to, as a formula on a layout reads from the layout's
// occurrence. Empty is the table's own occurrence.
func (s *Service) EvaluateExpressionFrom(
	ctx context.Context, tableName, occurrence, recordID, expression, resultType string,
) (*Evaluation, error) {
	evaluations, err := s.EvaluateExpressions(ctx, tableName, occurrence, recordID, []string{expression}, resultType)
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
	lib, err := LoadLibrary(ctx, s.driver)
	if err != nil {
		return nil, err
	}
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}

	record := calc.Record{}
	row := map[string]interface{}{}
	if recordID != "" {
		if row, err = s.GetRow(ctx, tableName, recordID); err != nil {
			return nil, err
		}
		for name := range row {
			record[name] = typedValue(tableName, fields, row, name, s.describeContainer(ctx))
		}
	}

	result := calc.ParseResultType(resultType)
	// Related fields are read from the occurrence given, or the table's own
	// occurrence, as a formula typed in the Data Viewer is in FileMaker. One
	// reader serves every expression, so what one reads the others reuse.
	env := &unstoredContext{RecordContext: calc.RecordContext{Record: record, Repeats: fields.repeating()},
		reader: s.newRelatedReader(), ctx: ctx, table: tableName, occurrence: occurrence, row: row}
	out := make([]ExpressionResult, len(expressions))
	for i, expression := range expressions {
		if strings.TrimSpace(expression) == "" {
			out[i].Err = fmt.Errorf("%w: there is nothing to evaluate", ErrInvalidExpression)
			continue
		}
		expr, err := calc.ParseWith(expression, calc.Options{Library: lib})
		if err != nil {
			out[i].Err = fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
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
		value, err := calc.EvaluateWith(expr, env, result)
		if err != nil {
			out[i].Err = fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
			continue
		}
		out[i].Evaluation = &Evaluation{
			Expression:    expression,
			Value:         value,
			Text:          evaluationText(value, result),
			ResultType:    string(result),
			Fields:        read,
			UnknownFields: unknown,
			RecordID:      recordID,
		}
	}
	return out, nil
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
