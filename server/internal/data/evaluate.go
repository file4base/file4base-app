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
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: there is nothing to evaluate", ErrInvalidExpression)
	}

	expr, err := calc.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
	}

	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}

	record := calc.Record{}
	if recordID != "" {
		row, err := s.GetRow(ctx, tableName, recordID)
		if err != nil {
			return nil, err
		}
		for name, value := range row {
			record[name] = value
		}
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
	value, err := calc.Evaluate(expr, record, result)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidExpression, err.Error())
	}

	return &Evaluation{
		Expression:    expression,
		Value:         value,
		Text:          evaluationText(value, result),
		ResultType:    string(result),
		Fields:        read,
		UnknownFields: unknown,
		RecordID:      recordID,
	}, nil
}

// evaluationText writes the answer the way the viewer shows it, in the same
// spelling an export uses, so a date reads as a date. An empty answer is
// empty text, not "null": a formula that comes to nothing is not an error,
// and the viewer marks it as empty itself.
func evaluationText(value interface{}, result calc.ResultType) string {
	if value == nil {
		return ""
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
