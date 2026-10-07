package calc

import (
	"math"
	"strings"
	"time"
)

// function is one of the names a formula may call. minArgs/maxArgs bound the
// argument count; maxArgs of -1 means any number.
type function struct {
	minArgs int
	maxArgs int
	call    func(t callNode, args []value) (value, error)
}

func (f function) checkArity(pos int, name string, got int) error {
	if got < f.minArgs {
		return errorAt(pos, "%s needs at least %d argument(s), got %d", name, f.minArgs, got)
	}
	if f.maxArgs >= 0 && got > f.maxArgs {
		return errorAt(pos, "%s takes at most %d argument(s), got %d", name, f.maxArgs, got)
	}
	return nil
}

// FunctionNames lists every function a formula may call, for documentation and
// for the formula editor.
func FunctionNames() []string {
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	return names
}

// evalCall evaluates a function. IF is handled here rather than in the table
// because only the branch that is taken may be evaluated.
func evalCall(t callNode, record Record) (value, error) {
	if t.name == "IF" {
		test, err := eval(t.args[0], record)
		if err != nil {
			return empty, err
		}
		if test.asBool() {
			return eval(t.args[1], record)
		}
		if len(t.args) == 3 {
			return eval(t.args[2], record)
		}
		return empty, nil
	}

	args := make([]value, 0, len(t.args))
	for _, argNode := range t.args {
		v, err := eval(argNode, record)
		if err != nil {
			return empty, err
		}
		args = append(args, v)
	}
	return functions[t.name].call(t, args)
}

var functions map[string]function

func init() {
	functions = map[string]function{
		// IF's branches are evaluated lazily in evalCall; the entry is here so
		// the parser knows the name and its arity.
		"IF": {minArgs: 2, maxArgs: 3, call: func(t callNode, args []value) (value, error) {
			return empty, errorAt(t.at, "IF is evaluated directly")
		}},

		"UPPER": text1(strings.ToUpper),
		"LOWER": text1(strings.ToLower),
		"TRIM":  text1(strings.TrimSpace),
		"LENGTH": {minArgs: 1, maxArgs: 1, call: func(t callNode, a []value) (value, error) {
			return numberValue(float64(len([]rune(a[0].asText())))), nil
		}},

		"CONCAT": {minArgs: 1, maxArgs: -1, call: func(t callNode, a []value) (value, error) {
			var b strings.Builder
			for _, v := range a {
				b.WriteString(v.asText())
			}
			return textValue(b.String()), nil
		}},

		"LEFT": {minArgs: 2, maxArgs: 2, call: func(t callNode, a []value) (value, error) {
			runes, n, err := textAndCount(t, a)
			if err != nil {
				return empty, err
			}
			if n > len(runes) {
				n = len(runes)
			}
			return textValue(string(runes[:n])), nil
		}},

		"RIGHT": {minArgs: 2, maxArgs: 2, call: func(t callNode, a []value) (value, error) {
			runes, n, err := textAndCount(t, a)
			if err != nil {
				return empty, err
			}
			if n > len(runes) {
				n = len(runes)
			}
			return textValue(string(runes[len(runes)-n:])), nil
		}},

		// MIDDLE(text, start, count): start counts from 1, like FileMaker.
		"MIDDLE": {minArgs: 3, maxArgs: 3, call: func(t callNode, a []value) (value, error) {
			runes := []rune(a[0].asText())
			start, err := a[1].asNumber(t.at, "the start position of MIDDLE")
			if err != nil {
				return empty, err
			}
			count, err := a[2].asNumber(t.at, "the length given to MIDDLE")
			if err != nil {
				return empty, err
			}
			from := int(start) - 1
			if from < 0 {
				from = 0
			}
			if from >= len(runes) || count <= 0 {
				return empty, nil
			}
			to := from + int(count)
			if to > len(runes) {
				to = len(runes)
			}
			return textValue(string(runes[from:to])), nil
		}},

		"ROUND": {minArgs: 1, maxArgs: 2, call: func(t callNode, a []value) (value, error) {
			n, err := a[0].asNumber(t.at, "the number given to ROUND")
			if err != nil {
				return empty, err
			}
			places := 0.0
			if len(a) == 2 {
				if places, err = a[1].asNumber(t.at, "the decimal places given to ROUND"); err != nil {
					return empty, err
				}
			}
			factor := math.Pow(10, places)
			return numberValue(math.Round(n*factor) / factor), nil
		}},

		"ABS": number1(math.Abs),

		"SUM": numberFold("SUM", func(acc, n float64) float64 { return acc + n }, 0),
		"MIN": numberReduce("MIN", math.Min),
		"MAX": numberReduce("MAX", math.Max),

		"AVERAGE": {minArgs: 1, maxArgs: -1, call: func(t callNode, a []value) (value, error) {
			total := 0.0
			for _, v := range a {
				n, err := v.asNumber(t.at, "an argument of AVERAGE")
				if err != nil {
					return empty, err
				}
				total += n
			}
			return numberValue(total / float64(len(a))), nil
		}},

		"ISEMPTY": {minArgs: 1, maxArgs: 1, call: func(t callNode, a []value) (value, error) {
			return boolValue(a[0].isEmpty()), nil
		}},

		// COALESCE returns the first argument that has something in it.
		"COALESCE": {minArgs: 1, maxArgs: -1, call: func(t callNode, a []value) (value, error) {
			for _, v := range a {
				if !v.isEmpty() {
					return v, nil
				}
			}
			return empty, nil
		}},

		"TODAY": {minArgs: 0, maxArgs: 0, call: func(t callNode, a []value) (value, error) {
			now := Now().UTC()
			return timeValue(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)), nil
		}},
		"NOW": {minArgs: 0, maxArgs: 0, call: func(t callNode, a []value) (value, error) {
			return timeValue(Now().UTC()), nil
		}},

		"YEAR":  datePart(func(t time.Time) float64 { return float64(t.Year()) }),
		"MONTH": datePart(func(t time.Time) float64 { return float64(t.Month()) }),
		"DAY":   datePart(func(t time.Time) float64 { return float64(t.Day()) }),
	}
}

// Now is the clock the time functions read. Tests replace it so TODAY() and
// NOW() are predictable.
var Now = time.Now

func text1(f func(string) string) function {
	return function{minArgs: 1, maxArgs: 1, call: func(t callNode, a []value) (value, error) {
		if a[0].isEmpty() {
			return empty, nil
		}
		return textValue(f(a[0].asText())), nil
	}}
}

func number1(f func(float64) float64) function {
	return function{minArgs: 1, maxArgs: 1, call: func(t callNode, a []value) (value, error) {
		n, err := a[0].asNumber(t.at, "the argument")
		if err != nil {
			return empty, err
		}
		return numberValue(f(n)), nil
	}}
}

func numberFold(name string, f func(acc, n float64) float64, start float64) function {
	return function{minArgs: 1, maxArgs: -1, call: func(t callNode, a []value) (value, error) {
		acc := start
		for _, v := range a {
			n, err := v.asNumber(t.at, "an argument of "+name)
			if err != nil {
				return empty, err
			}
			acc = f(acc, n)
		}
		return numberValue(acc), nil
	}}
}

func numberReduce(name string, f func(a, b float64) float64) function {
	return function{minArgs: 1, maxArgs: -1, call: func(t callNode, a []value) (value, error) {
		acc, err := a[0].asNumber(t.at, "an argument of "+name)
		if err != nil {
			return empty, err
		}
		for _, v := range a[1:] {
			n, err := v.asNumber(t.at, "an argument of "+name)
			if err != nil {
				return empty, err
			}
			acc = f(acc, n)
		}
		return numberValue(acc), nil
	}}
}

func datePart(f func(time.Time) float64) function {
	return function{minArgs: 1, maxArgs: 1, call: func(t callNode, a []value) (value, error) {
		if a[0].isEmpty() {
			return empty, nil
		}
		when, err := a[0].asTime(t.at, "the argument")
		if err != nil {
			return empty, err
		}
		return numberValue(f(when)), nil
	}}
}

func textAndCount(t callNode, a []value) ([]rune, int, error) {
	n, err := a[1].asNumber(t.at, "the number of characters")
	if err != nil {
		return nil, 0, err
	}
	if n < 0 {
		return nil, 0, errorAt(t.at, "the number of characters cannot be negative")
	}
	return []rune(a[0].asText()), int(n), nil
}
