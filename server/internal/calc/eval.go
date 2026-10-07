package calc

import (
	"strings"
	"time"
)

// ResultType is what a formula's answer is stored as.
type ResultType string

const (
	ResultText      ResultType = "TEXT"
	ResultNumber    ResultType = "NUMBER"
	ResultDate      ResultType = "DATE"
	ResultTimestamp ResultType = "TIMESTAMP"
	ResultBoolean   ResultType = "BOOLEAN"
)

// ParseResultType reads the result type a field was saved with. The client
// writes it in its own spelling ("Text", "Number", "Date", "Time",
// "Timestamp"); anything unrecognised is text.
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

// Evaluate computes the formula against a record and returns the answer in the
// shape the result type asks for, ready to be stored. An empty answer is nil.
func Evaluate(expr *Expr, record Record, result ResultType) (any, error) {
	if expr == nil {
		return nil, nil
	}
	v, err := eval(expr.root, record)
	if err != nil {
		return nil, err
	}
	return v.store(expr.root.pos(), result)
}

// store converts the answer to what the column holds.
func (v value) store(at int, result ResultType) (any, error) {
	if v.isEmpty() {
		return nil, nil
	}
	switch result {
	case ResultNumber:
		n, err := v.asNumber(at, "the result of the formula")
		if err != nil {
			return nil, err
		}
		return n, nil
	case ResultBoolean:
		return v.asBool(), nil
	case ResultDate:
		t, err := v.asTime(at, "the result of the formula")
		if err != nil {
			return nil, err
		}
		return t.Format("2006-01-02"), nil
	case ResultTimestamp:
		t, err := v.asTime(at, "the result of the formula")
		if err != nil {
			return nil, err
		}
		return t.Format(time.RFC3339), nil
	default:
		return v.asText(), nil
	}
}

func eval(n node, record Record) (value, error) {
	switch t := n.(type) {
	case numberNode:
		return numberValue(t.value), nil
	case textNode:
		return textValue(t.value), nil
	case boolNode:
		return boolValue(t.value), nil

	case fieldNode:
		raw, present := record[t.name]
		if !present {
			// A formula that names a field the table does not have is caught
			// when it is saved; reaching here means the record simply has no
			// value for it.
			return empty, nil
		}
		return fromStored(raw), nil

	case unaryNode:
		operand, err := eval(t.operand, record)
		if err != nil {
			return empty, err
		}
		switch t.op {
		case "-":
			n, err := operand.asNumber(t.at, "the value being negated")
			if err != nil {
				return empty, err
			}
			return numberValue(-n), nil
		case "NOT":
			return boolValue(!operand.asBool()), nil
		}
		return empty, errorAt(t.at, "unknown operator %q", t.op)

	case binaryNode:
		return evalBinary(t, record)

	case callNode:
		return evalCall(t, record)
	}
	return empty, errorAt(n.pos(), "this part of the formula cannot be evaluated")
}

func evalBinary(t binaryNode, record Record) (value, error) {
	// AND and OR stop as soon as the answer is known, so the other side may
	// be something that would fail to evaluate.
	switch t.op {
	case "AND":
		left, err := eval(t.left, record)
		if err != nil {
			return empty, err
		}
		if !left.asBool() {
			return boolValue(false), nil
		}
		right, err := eval(t.right, record)
		if err != nil {
			return empty, err
		}
		return boolValue(right.asBool()), nil
	case "OR":
		left, err := eval(t.left, record)
		if err != nil {
			return empty, err
		}
		if left.asBool() {
			return boolValue(true), nil
		}
		right, err := eval(t.right, record)
		if err != nil {
			return empty, err
		}
		return boolValue(right.asBool()), nil
	}

	left, err := eval(t.left, record)
	if err != nil {
		return empty, err
	}
	right, err := eval(t.right, record)
	if err != nil {
		return empty, err
	}

	switch t.op {
	case "&":
		return textValue(left.asText() + right.asText()), nil

	case "+", "-", "*", "/":
		a, err := left.asNumber(t.at, "the left side of "+t.op)
		if err != nil {
			return empty, err
		}
		b, err := right.asNumber(t.at, "the right side of "+t.op)
		if err != nil {
			return empty, err
		}
		switch t.op {
		case "+":
			return numberValue(a + b), nil
		case "-":
			return numberValue(a - b), nil
		case "*":
			return numberValue(a * b), nil
		case "/":
			if b == 0 {
				return empty, errorAt(t.at, "division by zero")
			}
			return numberValue(a / b), nil
		}
	}

	cmp, err := compare(left, right, t.at)
	if err != nil {
		return empty, err
	}
	switch t.op {
	case "=", "==":
		return boolValue(cmp == 0), nil
	case "<>", "!=":
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

// compare orders two values: as numbers when both are numeric, as moments when
// both are moments, otherwise as text ignoring case — which is how Find mode
// matches text, so a formula agrees with a find.
func compare(a, b value, at int) (int, error) {
	if a.isEmpty() && b.isEmpty() {
		return 0, nil
	}
	if a.kind == kindTime && b.kind == kindTime {
		switch {
		case a.when.Before(b.when):
			return -1, nil
		case a.when.After(b.when):
			return 1, nil
		}
		return 0, nil
	}
	if a.isNumeric() && b.isNumeric() {
		x, err := a.asNumber(at, "the left side of the comparison")
		if err != nil {
			return 0, err
		}
		y, err := b.asNumber(at, "the right side of the comparison")
		if err != nil {
			return 0, err
		}
		switch {
		case x < y:
			return -1, nil
		case x > y:
			return 1, nil
		}
		return 0, nil
	}
	return strings.Compare(strings.ToLower(a.asText()), strings.ToLower(b.asText())), nil
}
