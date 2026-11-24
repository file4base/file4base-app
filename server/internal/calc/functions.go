package calc

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// function is one of the names a formula may call. minArgs/maxArgs bound the
// argument count; maxArgs of -1 means any number. A function with lazy set
// receives its arguments unevaluated: If, Case and Choose evaluate only the
// branch taken, and the aggregates read a related field's every record.
type function struct {
	minArgs int
	maxArgs int
	call    func(ev *evaluator, c callNode, a []value) (value, error)
	lazy    func(ev *evaluator, c callNode) (value, error)
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

// FunctionNames lists every built-in function a formula may call, for
// documentation and for the formula editor.
func FunctionNames() []string {
	names := make([]string, 0, len(functions)+2)
	for name := range functions {
		names = append(names, name)
	}
	names = append(names, "LET", "GET", "WHILE")
	sort.Strings(names)
	return names
}

func (ev *evaluator) call(c callNode) (value, error) {
	if ev.lib != nil {
		if cf, ok := ev.lib.funcs[c.name]; ok {
			return ev.callCustom(c, cf)
		}
	}
	fn, ok := functions[c.name]
	if !ok {
		return empty, errorAt(c.at, "there is no function called %q", c.name)
	}
	if fn.lazy != nil {
		return fn.lazy(ev, c)
	}
	args := make([]value, len(c.args))
	for i, a := range c.args {
		v, err := ev.eval(a)
		if err != nil {
			return empty, err
		}
		args[i] = v
	}
	return fn.call(ev, c, args)
}

func (ev *evaluator) callCustom(c callNode, cf *customFunc) (value, error) {
	if cf.body == nil {
		return empty, errorAt(c.at, "the custom function %s cannot be evaluated", cf.name)
	}
	if ev.depth >= maxDepth {
		return empty, errorAt(c.at, "%s calls itself too deeply", cf.name)
	}
	scope := make(map[string]value, len(cf.params))
	for i, p := range cf.params {
		v, err := ev.eval(c.args[i])
		if err != nil {
			return empty, err
		}
		scope[p] = v
	}
	// A custom function sees only its parameters, not the caller's Let names.
	saved := ev.locals
	ev.locals = []map[string]value{scope}
	ev.depth++
	defer func() { ev.locals = saved; ev.depth-- }()
	return ev.eval(cf.body)
}

var functions map[string]function

func init() {
	functions = map[string]function{}
	for _, group := range []map[string]function{
		logicalFunctions(), textFunctions(), listFunctions(), numberFunctions(),
		aggregateFunctions(), dateFunctions(), conversionFunctions(), formattingFunctions(),
	} {
		for name, fn := range group {
			functions[name] = fn
		}
	}
}

func fixed(n int, call func(ev *evaluator, c callNode, a []value) (value, error)) function {
	return function{minArgs: n, maxArgs: n, call: call}
}

func between(lo, hi int, call func(ev *evaluator, c callNode, a []value) (value, error)) function {
	return function{minArgs: lo, maxArgs: hi, call: call}
}

// ---------------------------------------------------------------- logical

func logicalFunctions() map[string]function {
	return map[string]function{
		"IF": {minArgs: 2, maxArgs: 3, lazy: func(ev *evaluator, c callNode) (value, error) {
			return ev.cases(c.args)
		}},
		"CASE": {minArgs: 2, maxArgs: -1, lazy: func(ev *evaluator, c callNode) (value, error) {
			return ev.cases(c.args)
		}},
		"CHOOSE": {minArgs: 2, maxArgs: -1, lazy: func(ev *evaluator, c callNode) (value, error) {
			test, err := ev.eval(c.args[0])
			if err != nil {
				return empty, err
			}
			i := test.asNumber(ev.loc).IntPart()
			if i < 0 || int(i)+1 >= len(c.args) {
				return empty, nil
			}
			return ev.eval(c.args[i+1])
		}},
		"ISEMPTY": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return boolValue(a[0].isEmpty()), nil
		}),
		"ISVALID": {minArgs: 1, maxArgs: 1, lazy: func(ev *evaluator, c callNode) (value, error) {
			_, err := ev.eval(c.args[0])
			return boolValue(err == nil), nil
		}},
		"GETASBOOLEAN": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return boolValue(a[0].asBool(ev.loc)), nil
		}),
		"COALESCE": {minArgs: 1, maxArgs: -1, call: func(ev *evaluator, _ callNode, a []value) (value, error) {
			for _, v := range a {
				if !v.isEmpty() {
					return v, nil
				}
			}
			return empty, nil
		}},
		"EVALUATE": {minArgs: 1, maxArgs: 2, lazy: (*evaluator).evaluateText},
		"ISVALIDEXPRESSION": {minArgs: 1, maxArgs: 1, lazy: func(ev *evaluator, c callNode) (value, error) {
			v, err := ev.eval(c.args[0])
			if err != nil {
				return empty, err
			}
			src := v.asText(ev.loc)
			if strings.TrimSpace(src) == "" {
				return boolValue(true), nil
			}
			_, perr := parseFormula(src, ev.known, ev.lib, nil)
			return boolValue(perr == nil), nil
		}},
		"GETFIELD": fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			name := a[0].asText(ev.loc)
			if name == "" {
				return empty, nil
			}
			table, field, related := strings.Cut(name, "::")
			if related {
				return ev.readField(fieldNode{table: table, name: field, at: c.at})
			}
			return ev.readField(fieldNode{name: name, at: c.at})
		}),
	}
}

func (ev *evaluator) cases(args []node) (value, error) {
	for i := 0; i+1 < len(args); i += 2 {
		test, err := ev.eval(args[i])
		if err != nil {
			return empty, err
		}
		if test.asBool(ev.loc) {
			return ev.eval(args[i+1])
		}
	}
	if len(args)%2 == 1 {
		return ev.eval(args[len(args)-1])
	}
	return empty, nil
}

func (ev *evaluator) evaluateText(c callNode) (value, error) {
	v, err := ev.eval(c.args[0])
	if err != nil {
		return empty, err
	}
	if len(c.args) == 2 && !readsFields(c.args[1]) {
		return empty, errorAt(c.args[1].pos(), "Evaluate's second argument lists the fields that trigger it")
	}
	src := v.asText(ev.loc)
	if strings.TrimSpace(src) == "" {
		return empty, nil
	}
	root, err := parseFormula(src, ev.known, ev.lib, nil)
	if err != nil {
		return empty, errorAt(c.at, "Evaluate cannot read its formula: %v", err)
	}
	saved := ev.locals
	ev.locals = nil
	defer func() { ev.locals = saved }()
	return ev.eval(root)
}

// readsFields reports whether n is a field or a [ … ] list of fields.
func readsFields(n node) bool {
	switch t := n.(type) {
	case fieldNode, localNode:
		return true
	case pairNode:
		return readsFields(t.search) && readsFields(t.replace)
	}
	return false
}

// ---------------------------------------------------------------- text

func runesOf(v value, loc *Locale) []rune { return []rune(v.asText(loc)) }

func (ev *evaluator) count(v value) int {
	n := v.asNumber(ev.loc).IntPart()
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(n)
}

func textFunctions() map[string]function {
	return map[string]function{
		"UPPER": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(upperText(a[0].asText(ev.loc))), nil
		}),
		"LOWER": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(lowerText(a[0].asText(ev.loc))), nil
		}),
		"PROPER": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(proper(a[0].asText(ev.loc))), nil
		}),
		"TRIM": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(strings.Trim(a[0].asText(ev.loc), " ")), nil
		}),
		"TRIMALL": between(1, 3, func(ev *evaluator, _ callNode, a []value) (value, error) {
			// TrimAll ( text ; trimSpaces ; trimType ): type 3 removes every
			// space; the others leave one space between words.
			parts := strings.FieldsFunc(a[0].asText(ev.loc), func(r rune) bool { return r == ' ' })
			if len(a) == 3 && a[2].asNumber(ev.loc).IntPart() == 3 {
				return textValue(strings.Join(parts, "")), nil
			}
			return textValue(strings.Join(parts, " ")), nil
		}),
		"LENGTH": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return intValue(0), nil
			}
			return intValue(int64(utf8.RuneCountInString(a[0].asText(ev.loc)))), nil
		}),
		"LEFT": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			r := runesOf(a[0], ev.loc)
			n := min(ev.count(a[1]), len(r))
			return textValue(string(r[:n])), nil
		}),
		"RIGHT": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			r := runesOf(a[0], ev.loc)
			n := min(ev.count(a[1]), len(r))
			return textValue(string(r[len(r)-n:])), nil
		}),
		"MIDDLE": fixed(3, func(ev *evaluator, _ callNode, a []value) (value, error) {
			r := runesOf(a[0], ev.loc)
			start := int(a[1].asNumber(ev.loc).IntPart())
			if start < 1 {
				start = 1
			}
			if start > len(r) {
				return textValue(""), nil
			}
			end := min(start-1+ev.count(a[2]), len(r))
			return textValue(string(r[start-1 : end])), nil
		}),
		"POSITION": fixed(4, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return intValue(int64(position(a[0].asText(ev.loc), a[1].asText(ev.loc),
				int(a[2].asNumber(ev.loc).IntPart()), int(a[3].asNumber(ev.loc).IntPart())))), nil
		}),
		"PATTERNCOUNT": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return intValue(int64(len(findText(a[0].asText(ev.loc), a[1].asText(ev.loc))))), nil
		}),
		"SUBSTITUTE": {minArgs: 2, maxArgs: -1, lazy: (*evaluator).substitute},
		"REPLACE": fixed(4, func(ev *evaluator, _ callNode, a []value) (value, error) {
			r := runesOf(a[0], ev.loc)
			start := int(a[1].asNumber(ev.loc).IntPart())
			if start < 1 {
				start = 1
			}
			if start > len(r)+1 {
				start = len(r) + 1
			}
			end := min(start-1+ev.count(a[2]), len(r))
			return textValue(string(r[:start-1]) + a[3].asText(ev.loc) + string(r[end:])), nil
		}),
		"FILTER": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			keep := a[1].asText(ev.loc)
			var b strings.Builder
			for _, r := range a[0].asText(ev.loc) {
				if strings.ContainsRune(keep, r) {
					b.WriteRune(r)
				}
			}
			return textValue(b.String()), nil
		}),
		"EXACT": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return boolValue(a[0].asText(ev.loc) == a[1].asText(ev.loc)), nil
		}),
		"WORDCOUNT": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return intValue(int64(len(words(a[0].asText(ev.loc), ev.loc)))), nil
		}),
		"LEFTWORDS": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(wordRange(a[0].asText(ev.loc), ev.loc, 1, ev.count(a[1]))), nil
		}),
		"RIGHTWORDS": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			n := len(words(a[0].asText(ev.loc), ev.loc))
			k := min(ev.count(a[1]), n)
			return textValue(wordRange(a[0].asText(ev.loc), ev.loc, n-k+1, k)), nil
		}),
		"MIDDLEWORDS": fixed(3, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(wordRange(a[0].asText(ev.loc), ev.loc, max(1, int(a[1].asNumber(ev.loc).IntPart())), ev.count(a[2]))), nil
		}),
		"CHAR": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(charText(a[0].asNumber(ev.loc).IntPart())), nil
		}),
		"CODE": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			units := codeUnits(a[0].asText(ev.loc))
			if len(units) == 0 {
				return empty, nil
			}
			n := decimal.Zero
			hundredK := decimal.NewFromInt(100000)
			for i := len(units) - 1; i >= 0; i-- {
				n = n.Mul(hundredK).Add(decimal.NewFromInt(int64(units[i])))
			}
			return numberValue(n), nil
		}),
		"QUOTE": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(quote(a[0].asText(ev.loc))), nil
		}),
		"SERIALINCREMENT": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(serialIncrement(a[0].asText(ev.loc), a[1].asNumber(ev.loc).IntPart())), nil
		}),
		"CONCAT": {minArgs: 1, maxArgs: -1, call: func(ev *evaluator, _ callNode, a []value) (value, error) {
			var b strings.Builder
			for _, v := range a {
				b.WriteString(v.asText(ev.loc))
			}
			return textValue(b.String()), nil
		}},
	}
}

// substitute is Substitute ( text ; search ; replace ) or Substitute ( text ;
// [ search1 ; replace1 ] ; [ search2 ; replace2 ] … ), applied in order.
func (ev *evaluator) substitute(c callNode) (value, error) {
	text, err := ev.eval(c.args[0])
	if err != nil {
		return empty, err
	}
	s := text.asText(ev.loc)
	pairs := c.args[1:]
	if len(pairs) == 2 {
		if _, isPair := pairs[0].(pairNode); !isPair {
			pairs = []node{pairNode{search: pairs[0], replace: pairs[1]}}
		}
	}
	for _, n := range pairs {
		p, ok := n.(pairNode)
		if !ok {
			return empty, errorAt(n.pos(), "Substitute expects [ search ; replacement ] pairs")
		}
		search, err := ev.eval(p.search)
		if err != nil {
			return empty, err
		}
		repl, err := ev.eval(p.replace)
		if err != nil {
			return empty, err
		}
		if needle := search.asText(ev.loc); needle != "" {
			s = strings.ReplaceAll(s, needle, repl.asText(ev.loc))
		}
	}
	return textValue(s), nil
}

// position finds the occurrence-th search in text from start (1-based),
// ignoring case, backwards when occurrence is negative; 0 when there is none.
// Matches do not overlap.
func position(text, search string, start, occurrence int) int {
	if search == "" || occurrence == 0 {
		return 0
	}
	n := len([]rune(text))
	start = min(max(start, 1), max(n, 1))
	if occurrence > 0 {
		var from []int
		for _, i := range findText(string([]rune(text)[start-1:]), search) {
			from = append(from, i+start-1)
		}
		if occurrence > len(from) {
			return 0
		}
		return from[occurrence-1] + 1
	}
	// Backwards: the matches that begin at or before start, last first.
	var before []int
	all := findText(text, search)
	for k := len(all) - 1; k >= 0; k-- {
		if all[k] <= start-1 {
			before = append(before, all[k])
		}
	}
	if -occurrence > len(before) {
		return 0
	}
	return before[-occurrence-1] + 1
}

func wordRange(s string, loc *Locale, first, n int) string {
	ws := words(s, loc)
	if first < 1 {
		n += first - 1
		first = 1
	}
	if n <= 0 || first > len(ws) {
		return ""
	}
	last := min(first-1+n, len(ws))
	r := []rune(s)
	return string(r[ws[first-1][0]:ws[last-1][1]])
}

// serialIncrement adds to the last number in a text, keeping its width:
// SerialIncrement ( "INV0099" ; 1 ) is "INV0100".
func serialIncrement(s string, by int64) string {
	r := []rune(s)
	end := len(r)
	for end > 0 && !unicode.IsDigit(r[end-1]) {
		end--
	}
	if end == 0 {
		return s + decimal.NewFromInt(by).String()
	}
	start := end
	for start > 0 && unicode.IsDigit(r[start-1]) {
		start--
	}
	n, _ := new(big.Int).SetString(string(r[start:end]), 10)
	n.Add(n, big.NewInt(by))
	digits := n.String()
	if len(digits) < end-start {
		digits = strings.Repeat("0", end-start-len(digits)) + digits
	}
	return string(r[:start]) + digits + string(r[end:])
}

// ---------------------------------------------------------------- lists

// splitValues splits a list at returns, as FileMaker's value functions do: a
// carriage return, a line feed, both, or a Unicode line or paragraph
// separator ends a value. A list's final return does not start another value.
func splitValues(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	r := []rune(s)
	start := 0
	for i := 0; i < len(r); i++ {
		switch r[i] {
		case '\r', '\n', ' ', ' ':
			out = append(out, string(r[start:i]))
			if r[i] == '\r' && i+1 < len(r) && r[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(r) {
		out = append(out, string(r[start:]))
	}
	return out
}

func joinValues(values []string, trailing bool) value {
	if len(values) == 0 {
		return empty
	}
	s := strings.Join(values, pilcrow)
	if trailing {
		s += pilcrow
	}
	return textValue(s)
}

func listFunctions() map[string]function {
	return map[string]function{
		"VALUECOUNT": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return intValue(int64(len(splitValues(a[0].asText(ev.loc))))), nil
		}),
		"GETVALUE": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			vals := splitValues(a[0].asText(ev.loc))
			i := int(a[1].asNumber(ev.loc).IntPart())
			if i < 1 || i > len(vals) {
				return empty, nil
			}
			return textValue(vals[i-1]), nil
		}),
		"LEFTVALUES": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			vals := splitValues(a[0].asText(ev.loc))
			return joinValues(vals[:min(ev.count(a[1]), len(vals))], true), nil
		}),
		"RIGHTVALUES": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			vals := splitValues(a[0].asText(ev.loc))
			n := min(ev.count(a[1]), len(vals))
			return joinValues(vals[len(vals)-n:], true), nil
		}),
		"MIDDLEVALUES": fixed(3, func(ev *evaluator, _ callNode, a []value) (value, error) {
			vals := splitValues(a[0].asText(ev.loc))
			start := int(a[1].asNumber(ev.loc).IntPart())
			if start < 1 {
				start = 1
			}
			if start > len(vals) {
				return empty, nil
			}
			end := min(start-1+ev.count(a[2]), len(vals))
			return joinValues(vals[start-1:end], true), nil
		}),
		"FILTERVALUES": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			keep := map[string]bool{}
			for _, v := range splitValues(a[1].asText(ev.loc)) {
				keep[strings.ToLower(v)] = true
			}
			var out []string
			for _, v := range splitValues(a[0].asText(ev.loc)) {
				if keep[strings.ToLower(v)] {
					out = append(out, v)
				}
			}
			return joinValues(out, true), nil
		}),
		"UNIQUEVALUES": between(1, 3, func(ev *evaluator, c callNode, a []value) (value, error) {
			kind, _, err := valueType(ev, c, a)
			if err != nil {
				return empty, err
			}
			// Values that compare equal are one value; the first is kept.
			seen := map[string]bool{}
			var out []string
			for _, v := range splitValues(a[0].asText(ev.loc)) {
				k := primaryKey(v)
				if kind != 1 {
					k = typedKey(v, kind, ev.loc).String()
				}
				if !seen[k] {
					seen[k] = true
					out = append(out, v)
				}
			}
			return joinValues(out, true), nil
		}),
		"SORTVALUES": between(1, 3, func(ev *evaluator, c callNode, a []value) (value, error) {
			kind, descending, err := valueType(ev, c, a)
			if err != nil {
				return empty, err
			}
			vals := splitValues(a[0].asText(ev.loc))
			less := sortLess
			if kind != 1 {
				less = func(x, y string) bool { return typedKey(x, kind, ev.loc).LessThan(typedKey(y, kind, ev.loc)) }
			}
			sort.SliceStable(vals, func(i, j int) bool {
				if descending {
					return less(vals[j], vals[i])
				}
				return less(vals[i], vals[j])
			})
			return joinValues(vals, true), nil
		}),
	}
}

// valueType reads the type argument of SortValues and UniqueValues: 1 text,
// 2 number, 3 date, 4 time, 5 timestamp; negative sorts in descending order.
func valueType(ev *evaluator, c callNode, a []value) (int64, bool, error) {
	if len(a) < 2 || a[1].isEmpty() {
		return 1, false, nil
	}
	t := a[1].asNumber(ev.loc).IntPart()
	kind := t
	if kind < 0 {
		kind = -kind
	}
	if kind < 1 || kind > 5 {
		return 0, false, errorAt(c.at, "%d is not a value type", t)
	}
	return kind, t < 0, nil
}

// typedKey is a value read as a number, date, time or timestamp, for sorting.
// A value that is none sorts first.
func typedKey(s string, kind int64, loc *Locale) decimal.Decimal {
	v := textValue(s)
	var n decimal.Decimal
	var ok bool
	switch kind {
	case 2:
		n, ok = v.numberOrEmpty(loc)
	case 3:
		var d int64
		d, ok = v.asDay(loc)
		n = decimal.NewFromInt(d)
	case 4:
		n, ok = v.asSeconds(loc)
	default:
		n, ok = v.asTimestampSeconds(loc)
	}
	if !ok {
		return decimal.New(-1, 4000)
	}
	return n
}

// ---------------------------------------------------------------- numbers

func numberFunctions() map[string]function {
	// one is a function of one number. Like FileMaker, it gives nothing for
	// nothing, and 0's answer for text that holds no digit.
	one := func(f func(decimal.Decimal) decimal.Decimal) function {
		return fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			return numberValue(f(a[0].asNumber(ev.loc))), nil
		})
	}
	// exact is a function FileMaker computes to the current precision.
	exact := func(name string, f func(x decimal.Decimal, digits int32) (decimal.Decimal, error)) function {
		return fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			r, err := f(a[0].asNumber(ev.loc), ev.precision+4)
			if err != nil {
				return empty, errorAt(c.at, "%s has no answer for %s", name, ev.loc.formatNumber(a[0].asNumber(ev.loc), 0))
			}
			return numberValue(r), nil
		})
	}
	// float is a trigonometric function, which FileMaker computes in binary
	// floating point: Sin ( Pi / 6 ) is ,4999999999999999.
	float := func(name string, f func(float64) float64) function {
		return fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			r := f(toFloat(a[0].asNumber(ev.loc)))
			if math.IsNaN(r) || math.IsInf(r, 0) {
				return empty, errorAt(c.at, "%s has no answer for %s", name, ev.loc.formatNumber(a[0].asNumber(ev.loc), 0))
			}
			return numberValue(floatDecimal(r)), nil
		})
	}
	places := func(ev *evaluator, a []value, i int) int32 {
		if i >= len(a) {
			return 0
		}
		return int32(a[i].asNumber(ev.loc).IntPart())
	}
	return map[string]function{
		"ABS": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			switch {
			case a[0].isEmpty():
				return empty, nil
			case a[0].kind == kindTime || a[0].kind == kindDate || a[0].kind == kindTimestamp: // Abs of a time is a time
				return value{kind: a[0].kind, num: a[0].num.Abs()}, nil
			}
			return numberValue(a[0].asNumber(ev.loc).Abs()), nil
		}),
		"INT":     one(func(d decimal.Decimal) decimal.Decimal { return d.Truncate(0) }),
		"CEILING": one(decimal.Decimal.Ceil),
		"FLOOR":   one(decimal.Decimal.Floor),
		"SIGN": one(func(d decimal.Decimal) decimal.Decimal {
			return decimal.NewFromInt(int64(d.Sign()))
		}),
		"ROUND": between(1, 2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			return numberValue(a[0].asNumber(ev.loc).Round(places(ev, a, 1))), nil
		}),
		"TRUNCATE": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			x, p := a[0].asNumber(ev.loc), places(ev, a, 1)
			if p >= 0 {
				return numberValue(x.Truncate(p)), nil
			}
			// Truncate ( 1234,5 ; -2 ) is 1200.
			unit := decimal.New(1, -p)
			return numberValue(x.Div(unit).Truncate(0).Mul(unit)), nil
		}),
		"MOD": fixed(2, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			x, y := a[0].asNumber(ev.loc), a[1].asNumber(ev.loc)
			if y.IsZero() {
				return numberValue(x), nil // FileMaker: Mod ( 7 ; 0 ) is 7
			}
			// FileMaker's Mod takes the sign of the divisor.
			r := x.Mod(y)
			if !r.IsZero() && r.Sign() != y.Sign() {
				r = r.Add(y)
			}
			return numberValue(r), nil
		}),
		"DIV": fixed(2, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			x, y := a[0].asNumber(ev.loc), a[1].asNumber(ev.loc)
			if y.IsZero() {
				return empty, errorAt(c.at, "division by zero")
			}
			return numberValue(x.DivRound(y, ev.precision+2).Floor()), nil
		}),
		"SQRT":    exact("Sqrt", decimalSqrt),
		"EXP":     exact("Exp", func(x decimal.Decimal, d int32) (decimal.Decimal, error) { return x.ExpTaylor(d) }),
		"LN":      exact("Ln", decimalLn),
		"LG":      exact("Lg", func(x decimal.Decimal, d int32) (decimal.Decimal, error) { return decimalLog(x, 2, d) }),
		"LOG":     exact("Log", func(x decimal.Decimal, d int32) (decimal.Decimal, error) { return decimalLog(x, 10, d) }),
		"SIN":     float("Sin", math.Sin),
		"COS":     float("Cos", math.Cos),
		"TAN":     float("Tan", math.Tan),
		"ASIN":    float("Asin", math.Asin),
		"ACOS":    float("Acos", math.Acos),
		"ATAN":    float("Atan", math.Atan),
		"DEGREES": float("Degrees", func(x float64) float64 { return x * 180 / math.Pi }),
		"RADIANS": float("Radians", func(x float64) float64 { return x * math.Pi / 180 }),
		"PI": fixed(0, func(ev *evaluator, _ callNode, _ []value) (value, error) {
			return numberValue(piDigits.Round(ev.precision + 2)), nil
		}),
		"RANDOM": fixed(0, func(_ *evaluator, _ callNode, _ []value) (value, error) {
			n, err := rand.Int(rand.Reader, big.NewInt(1e16))
			if err != nil {
				return empty, err
			}
			return numberValue(decimal.NewFromBigInt(n, -16)), nil
		}),
		"SETPRECISION": {minArgs: 2, maxArgs: 2, lazy: func(ev *evaluator, c callNode) (value, error) {
			// SetPrecision ( expression ; places ) computes the expression to
			// between 16 and 400 decimal places.
			pv, err := ev.eval(c.args[1])
			if err != nil {
				return empty, err
			}
			p := int32(min(max(pv.asNumber(ev.loc).IntPart(), precision), 400))
			saved := ev.precision
			ev.precision = p
			defer func() { ev.precision = saved }()
			v, err := ev.eval(c.args[0])
			if err != nil || v.kind != kindNumber {
				return v, err
			}
			v.places = p
			return v, nil
		}},
	}
}

// piDigits is π to more places than SetPrecision's 400.
var piDigits = decimal.RequireFromString("3.14159265358979323846264338327950288419716939937510582097494459230781640628620899862803482534211706798214808651328230664709384460955058223172535940812848111745028410270193852110555964462294895493038196442881097566593344612847564823378678316527120190914564856692346034861045432664821339360726024914127372458700660631558817488152092096282925409171536436789259036001133053054882046652138414695194151160943305727036575959195309218611738193261179310511854807446237996274956735188575272489122793818301194912983367336244065664308602139494639522473719070217986094370277053921717629317675238467481846766940513200056812714526356082778577134275778960917363717872146844090122495343014654958537105079227968925892354201995611212902196086403441815981362977477130996051870721134999999837297804995105973173281609631859502445945534690830264252230825334468503526193118817101000313783875288658753320838142061717766914730359825349042875546873115956286388235378759375195778185778053217122680661300192787661119590921642019")

// toFloat is the binary floating-point number nearest x toward zero, as
// FileMaker converts it for trigonometry: Sin ( Pi / 6 ) is ,4999999999999999.
func toFloat(x decimal.Decimal) float64 {
	f, _ := x.Float64()
	if d := decimal.NewFromFloat(f); d.Abs().GreaterThan(x.Abs()) {
		f = math.Nextafter(f, 0)
	}
	return f
}

// floatDecimal is a binary floating-point number written out exactly enough
// to show its error, as FileMaker shows it.
func floatDecimal(f float64) decimal.Decimal {
	return decimal.RequireFromString(strconv.FormatFloat(f, 'g', 20, 64))
}

func decimalSqrt(x decimal.Decimal, digits int32) (decimal.Decimal, error) {
	if x.IsNegative() {
		return decimal.Zero, fmt.Errorf("negative")
	}
	if x.IsZero() {
		return decimal.Zero, nil
	}
	f, _ := x.Float64()
	r := floatDecimal(math.Sqrt(f))
	two := decimal.NewFromInt(2)
	for i := 0; i < 8; i++ { // Newton's method doubles the correct digits each time
		r = r.Add(x.DivRound(r, digits+4)).DivRound(two, digits+4)
	}
	return r.Round(digits), nil
}

func decimalLn(x decimal.Decimal, digits int32) (decimal.Decimal, error) {
	if !x.IsPositive() {
		return decimal.Zero, fmt.Errorf("not positive")
	}
	return x.Ln(digits)
}

func decimalLog(x decimal.Decimal, base int64, digits int32) (decimal.Decimal, error) {
	if !x.IsPositive() {
		return decimal.Zero, fmt.Errorf("not positive")
	}
	// An exact power of the base has an exact logarithm: Log ( 1000 ) is 3.
	b := decimal.NewFromInt(base)
	p, k := decimal.NewFromInt(1), int64(0)
	for p.LessThan(x) && k < 4000 {
		p, k = p.Mul(b), k+1
	}
	if p.Equal(x) {
		return decimal.NewFromInt(k), nil
	}
	lx, err := x.Ln(digits + 4)
	if err != nil {
		return decimal.Zero, err
	}
	lb, err := b.Ln(digits + 4)
	if err != nil {
		return decimal.Zero, err
	}
	return lx.DivRound(lb, digits), nil
}

// power is a ^ b. A whole exponent is exact; any other goes through
// logarithms to the current precision.
func power(a, b decimal.Decimal, digits int32) (decimal.Decimal, error) {
	if b.IsInteger() && b.Abs().LessThanOrEqual(decimal.NewFromInt(10000)) {
		if b.IsNegative() {
			if a.IsZero() {
				return decimal.Zero, fmt.Errorf("zero cannot be raised to a negative power")
			}
			return decimal.NewFromInt(1).DivRound(a.Pow(b.Neg()), digits+2), nil
		}
		return a.Pow(b), nil
	}
	if a.IsZero() {
		if b.IsNegative() {
			return decimal.Zero, fmt.Errorf("zero cannot be raised to a negative power")
		}
		return decimal.Zero, nil
	}
	if a.IsNegative() {
		return decimal.Zero, fmt.Errorf("%s ^ %s has no answer", a.String(), b.String())
	}
	ln, err := a.Ln(digits + 6)
	if err != nil {
		return decimal.Zero, err
	}
	r, err := ln.Mul(b).ExpTaylor(digits + 4)
	if err != nil {
		return decimal.Zero, err
	}
	return r, nil
}

// ---------------------------------------------------------------- aggregates

// aggregateArgs gathers the values an aggregate works on. With one argument
// that reads a related field, that is every related record's value; with
// several, each argument is one value and a related field gives its first
// record's, as in FileMaker.
func (ev *evaluator) aggregateArgs(c callNode) ([]value, error) {
	if len(c.args) == 1 {
		f, ok := c.args[0].(fieldNode)
		if !ok {
			// FileMaker (error 1214): one argument must be a field, whose
			// related records or repetitions are summarised.
			return nil, errorAt(c.at, "%s needs a field, or two values or more", c.name)
		}
		if f.table != "" && f.repetition == nil {
			return ev.relatedValues(f)
		}
	}
	out := make([]value, 0, len(c.args))
	for _, a := range c.args {
		v, err := ev.eval(a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func aggregate(f func(ev *evaluator, c callNode, vals []value) (value, error)) function {
	return function{minArgs: 1, maxArgs: -1, lazy: func(ev *evaluator, c callNode) (value, error) {
		vals, err := ev.aggregateArgs(c)
		if err != nil {
			return empty, err
		}
		return f(ev, c, vals)
	}}
}

func nonEmpty(vals []value) []value {
	out := vals[:0:0]
	for _, v := range vals {
		if !v.isEmpty() {
			out = append(out, v)
		}
	}
	return out
}

func aggregateFunctions() map[string]function {
	return map[string]function{
		"SUM": aggregate(func(ev *evaluator, _ callNode, vals []value) (value, error) {
			total := decimal.Zero
			for _, v := range nonEmpty(vals) {
				total = total.Add(v.asNumber(ev.loc))
			}
			return numberValue(total), nil
		}),
		"COUNT": aggregate(func(_ *evaluator, _ callNode, vals []value) (value, error) {
			return intValue(int64(len(nonEmpty(vals)))), nil
		}),
		"AVERAGE": aggregate(func(ev *evaluator, _ callNode, vals []value) (value, error) {
			vals = nonEmpty(vals)
			if len(vals) == 0 {
				return empty, nil
			}
			total := decimal.Zero
			for _, v := range vals {
				total = total.Add(v.asNumber(ev.loc))
			}
			return numberValue(total.DivRound(decimal.NewFromInt(int64(len(vals))), ev.precision+2)), nil
		}),
		"MIN": aggregate(func(ev *evaluator, _ callNode, vals []value) (value, error) {
			return extreme(nonEmpty(vals), ev.loc, -1), nil
		}),
		"MAX": aggregate(func(ev *evaluator, _ callNode, vals []value) (value, error) {
			return extreme(nonEmpty(vals), ev.loc, 1), nil
		}),
		"LIST": aggregate(func(ev *evaluator, _ callNode, vals []value) (value, error) {
			var out []string
			for _, v := range nonEmpty(vals) {
				out = append(out, v.asText(ev.loc))
			}
			return joinValues(out, false), nil
		}),
		"LAST": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return a[0], nil
		}),
		"GETNTHRECORD": {minArgs: 2, maxArgs: 2, lazy: func(ev *evaluator, c callNode) (value, error) {
			f, ok := c.args[0].(fieldNode)
			if !ok || f.table == "" {
				return empty, errorAt(c.at, "GetNthRecord needs a related field here, such as Lines::Amount")
			}
			nv, err := ev.eval(c.args[1])
			if err != nil {
				return empty, err
			}
			vals, err := ev.relatedValues(f)
			if err != nil {
				return empty, err
			}
			i := int(nv.asNumber(ev.loc).IntPart())
			if i < 1 || i > len(vals) {
				return empty, nil
			}
			return vals[i-1], nil
		}},
	}
}

func extreme(vals []value, loc *Locale, sign int) value {
	if len(vals) == 0 {
		return empty
	}
	// FileMaker compares two values as text only when both are text:
	// Max ( "10" ; "9" ) is "9" but Max ( "b" ; 9 ) is 9.
	best := vals[0]
	for _, v := range vals[1:] {
		var c int
		if v.kind == kindText && best.kind == kindText {
			c = collate(v.text, best.text)
		} else {
			c = v.asNumber(loc).Cmp(best.asNumber(loc))
		}
		if c*sign > 0 {
			best = v
		}
	}
	return best
}
