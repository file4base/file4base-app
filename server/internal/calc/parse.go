package calc

import (
	"math"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// Expr is a parsed formula.
type Expr struct {
	root  node
	src   string
	lib   *Library
	known *names
}

// String returns the formula as it was written.
func (e *Expr) String() string { return e.src }

// Fields lists the fields of the formula's own table that it reads, sorted and
// without duplicates. It is what the schema service uses to find the order to
// compute calculations in, and to reject a formula that depends on itself.
func (e *Expr) Fields() []string {
	seen := map[string]struct{}{}
	e.walk(func(n node) {
		if f, ok := n.(fieldNode); ok && f.table == "" {
			seen[f.name] = struct{}{}
		}
	})
	return sortedKeys(seen)
}

// Reference is a field read through a table occurrence: Table::Field.
type Reference struct {
	Table string
	Field string
	// Missing is set for FileMaker's placeholder for a deleted table or
	// field, such as <Table Missing>::<Field Missing>. Reading it is an error.
	Missing bool
}

// RelatedFields lists the Table::Field references of the formula.
func (e *Expr) RelatedFields() []Reference {
	seen := map[Reference]struct{}{}
	e.walk(func(n node) {
		if f, ok := n.(fieldNode); ok && f.table != "" {
			seen[Reference{Table: f.table, Field: f.name, Missing: f.missing}] = struct{}{}
		}
	})
	out := make([]Reference, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// RelatedUse is how a formula reads a related field.
type RelatedUse int

const (
	// UseFirst reads the first related record: Occurrence::Field.
	UseFirst RelatedUse = iota
	// UseTotals reads an aggregate of every related record that its totals
	// answer: Sum, Count, Average, Min or Max of one related field.
	UseTotals
	// UseAll reads every related record: List.
	UseAll
	// UseLast reads the last related record whose field holds a value: Last.
	UseLast
	// UseNth reads the Nth related record: GetNthRecord ( Field ; N ), N
	// being 0 when it is not a number written in the formula.
	UseNth
)

// RelatedRead is one way a formula reads a Table::Field reference.
type RelatedRead struct {
	Reference
	Use RelatedUse
	// Function is the aggregate a UseTotals read is for, in upper case.
	Function string
	// N is the record a UseNth read is for, 0 when known only once computed.
	N int
}

// RelatedReads lists how the formula reads each Table::Field reference, so
// what it reads can be fetched ahead, no further than it needs.
func (e *Expr) RelatedReads() []RelatedRead {
	seen := map[RelatedRead]struct{}{}
	add := func(f fieldNode, use RelatedUse, function string, n int) {
		seen[RelatedRead{Reference: Reference{Table: f.table, Field: f.name, Missing: f.missing},
			Use: use, Function: function, N: n}] = struct{}{}
	}
	entered := map[string]bool{}
	var rec func(n node)
	rec = func(n node) {
		if n == nil {
			return
		}
		switch t := n.(type) {
		case fieldNode:
			if t.table != "" {
				add(t, UseFirst, "", 0)
			}
		case callNode:
			if use, ok := wholeUse(t); ok {
				f := t.args[0].(fieldNode)
				function, n := "", 0
				switch use {
				case UseTotals:
					function = t.name
				case UseNth:
					if num, ok := t.args[1].(numberNode); ok && num.value.IsInteger() && num.value.IsPositive() &&
						num.value.LessThanOrEqual(decimal.NewFromInt(math.MaxInt32)) {
						n = int(num.value.IntPart())
					}
				}
				add(f, use, function, n)
				for _, a := range t.args[1:] {
					rec(a)
				}
				return
			}
		}
		if body := e.customBody(n, entered); body != nil {
			rec(body)
		}
		for _, child := range children(n) {
			rec(child)
		}
	}
	rec(e.root)
	out := make([]RelatedRead, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Use != b.Use {
			return a.Use < b.Use
		}
		if a.Function != b.Function {
			return a.Function < b.Function
		}
		return a.N < b.N
	})
	return out
}

// wholeUse is how a call reads its first argument when that is a related
// field it reads across the related records, as aggregateArgs, Last and
// GetNthRecord do.
func wholeUse(c callNode) (RelatedUse, bool) {
	if len(c.args) == 0 {
		return 0, false
	}
	f, ok := c.args[0].(fieldNode)
	if !ok || f.table == "" || f.repetition != nil {
		return 0, false
	}
	switch {
	case len(c.args) == 1 && (c.name == "SUM" || c.name == "COUNT" || c.name == "AVERAGE" || c.name == "MIN" || c.name == "MAX"):
		return UseTotals, true
	case len(c.args) == 1 && c.name == "LIST":
		return UseAll, true
	case len(c.args) == 1 && c.name == "LAST":
		return UseLast, true
	case len(c.args) == 2 && c.name == "GETNTHRECORD":
		return UseNth, true
	}
	return 0, false
}

// Functions lists the built-in and custom functions the formula calls, and
// GetSelectors the Get(…) functions it asks for, both in upper case.
func (e *Expr) Functions() []string {
	seen := map[string]struct{}{}
	e.walk(func(n node) {
		if c, ok := n.(callNode); ok {
			seen[c.name] = struct{}{}
		}
	})
	return sortedKeys(seen)
}

// GetSelectors lists the Get(…) functions the formula asks for, such as
// "ACCOUNTNAME" or "CURRENTDATE".
func (e *Expr) GetSelectors() []string {
	seen := map[string]struct{}{}
	e.walk(func(n node) {
		if g, ok := n.(getNode); ok {
			seen[g.selector] = struct{}{}
		}
	})
	return sortedKeys(seen)
}

// Variables lists the $local and $$global variables the formula reads or sets.
func (e *Expr) Variables() []string {
	seen := map[string]struct{}{}
	e.walk(func(n node) {
		switch t := n.(type) {
		case variableNode:
			seen[t.name] = struct{}{}
		case letNode:
			for _, b := range t.bindings {
				if strings.HasPrefix(b.name, "$") {
					seen[b.name] = struct{}{}
				}
			}
		case whileNode:
			for _, b := range append(append([]letBinding(nil), t.initial...), t.logic...) {
				if strings.HasPrefix(b.name, "$") {
					seen[b.name] = struct{}{}
				}
			}
		}
	})
	return sortedKeys(seen)
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// walk visits every node of the formula and of the custom functions it
// calls, each function once: what a formula reads, calls or sets includes
// what the custom functions it calls do.
func (e *Expr) walk(visit func(node)) {
	entered := map[string]bool{}
	var rec func(node)
	rec = func(n node) {
		if n == nil {
			return
		}
		visit(n)
		if body := e.customBody(n, entered); body != nil {
			rec(body)
		}
		for _, child := range children(n) {
			rec(child)
		}
	}
	rec(e.root)
}

// walkOwn visits the nodes of the formula itself, as it is written.
func (e *Expr) walkOwn(visit func(node)) {
	var rec func(node)
	rec = func(n node) {
		if n == nil {
			return
		}
		visit(n)
		for _, child := range children(n) {
			rec(child)
		}
	}
	rec(e.root)
}

// customBody is the formula of the custom function n calls, the first time
// it is met (entered records it), and nil otherwise.
func (e *Expr) customBody(n node, entered map[string]bool) node {
	c, ok := n.(callNode)
	if !ok || e.lib == nil || entered[c.name] {
		return nil
	}
	cf, ok := e.lib.funcs[c.name]
	if !ok {
		return nil
	}
	entered[c.name] = true
	return cf.body
}

// children are the nodes a node is made of.
func children(n node) []node {
	switch t := n.(type) {
	case unaryNode:
		return []node{t.operand}
	case binaryNode:
		return []node{t.left, t.right}
	case callNode:
		return t.args
	case letNode:
		out := make([]node, 0, len(t.bindings)+1)
		for _, b := range t.bindings {
			out = append(out, b.value)
		}
		return append(out, t.body)
	case fieldNode:
		return []node{t.repetition}
	case variableNode:
		return []node{t.repetition}
	case pairNode:
		return []node{t.search, t.replace}
	case whileNode:
		out := make([]node, 0, len(t.initial)+len(t.logic)+2)
		for _, b := range t.initial {
			out = append(out, b.value)
		}
		out = append(out, t.condition)
		for _, b := range t.logic {
			out = append(out, b.value)
		}
		return append(out, t.result)
	}
	return nil
}

type node interface{ pos() int }

type numberNode struct {
	value decimal.Decimal
	at    int
}
type textNode struct {
	value string
	at    int
}

// fieldNode reads a field: of the formula's own table when table is empty,
// otherwise of the first related record through that table occurrence.
type fieldNode struct {
	table, name string
	repetition  node // nil reads the first (or current) repetition
	at          int
	missing     bool // the table or field was deleted in FileMaker
}

// variableNode reads a $local or $$global script variable.
type variableNode struct {
	name       string
	repetition node
	at         int
}

// localNode reads a name bound by Let or a custom function's parameter.
type localNode struct {
	name string // lower case
	at   int
}
type unaryNode struct {
	op      string
	operand node
	at      int
}
type binaryNode struct {
	op          string
	left, right node
	at          int
}
type callNode struct {
	name string // upper case
	args []node
	at   int
}
type getNode struct {
	selector string // upper case
	at       int
}
type letBinding struct {
	name  string // lower case, or $var / $$var as written
	value node
}
type letNode struct {
	bindings []letBinding
	body     node
	at       int
}

// pairNode is [ search ; replacement ] in Substitute.
type pairNode struct {
	search, replace node
	at              int
}

// whileNode is While ( [ initial ] ; condition ; [ logic ] ; result ).
type whileNode struct {
	initial   []letBinding
	condition node
	logic     []letBinding
	result    node
	at        int
}

func (n pairNode) pos() int     { return n.at }
func (n whileNode) pos() int    { return n.at }
func (n numberNode) pos() int   { return n.at }
func (n textNode) pos() int     { return n.at }
func (n fieldNode) pos() int    { return n.at }
func (n variableNode) pos() int { return n.at }
func (n localNode) pos() int    { return n.at }
func (n unaryNode) pos() int    { return n.at }
func (n binaryNode) pos() int   { return n.at }
func (n callNode) pos() int     { return n.at }
func (n getNode) pos() int      { return n.at }
func (n letNode) pos() int      { return n.at }

// Options is what the caller knows about the formula's surroundings. With
// them, field names holding spaces or symbols and Table::Field references
// resolve as FileMaker resolves them; without them, a field must be written
// as a plain word.
type Options struct {
	// Fields are the fields of the formula's own table.
	Fields []string
	// Tables maps each table occurrence the formula may read through to its
	// fields.
	Tables map[string][]string
	// Library holds the solution's custom functions.
	Library *Library
	// Locale is the file's. With a comma decimal separator, as in a French
	// file, numbers in the formula are written 1,5 and only ";" separates
	// arguments.
	Locale *Locale
}

// Parse reads a formula written with plain field names. An empty formula is
// an error: a calculation field with nothing to compute is a mistake, not an
// empty value.
func Parse(src string) (*Expr, error) { return ParseWith(src, Options{}) }

// ParseWith reads a formula against the names of its table, its table
// occurrences and its custom functions.
func ParseWith(src string, opts Options) (*Expr, error) {
	known := opts.names()
	root, err := parseFormula(src, known, opts.Library, nil)
	if err != nil {
		return nil, err
	}
	return &Expr{root: root, src: src, lib: opts.Library, known: known}, nil
}

// isCustom reports whether the solution defines a custom function of that
// (upper-case) name, which then takes the place of a built-in one.
func (p *parser) isCustom(upper string) bool {
	if p.lib == nil {
		return false
	}
	_, ok := p.lib.funcs[upper]
	return ok
}

// isConstant reports whether name is a custom function without parameters.
func (p *parser) isConstant(name string) bool {
	if p.lib == nil {
		return false
	}
	cf, ok := p.lib.funcs[strings.ToUpper(name)]
	return ok && len(cf.params) == 0
}

func parseFormula(src string, known *names, lib *Library, params []string) (node, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errorAt(0, "the formula is empty")
	}
	tokens, err := lex(src, known)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 1 {
		// Only comments, as FileMaker allows for a condition switched off by
		// commenting it out: the answer is empty.
		return textNode{at: 0}, nil
	}
	p := &parser{tokens: tokens, lib: lib}
	if len(params) > 0 {
		scope := map[string]bool{}
		for _, name := range params {
			scope[strings.ToLower(name)] = true
		}
		p.scopes = append(p.scopes, scope)
	}
	root, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, errorAt(p.peek().pos, "unexpected %q after the end of the formula", p.peek().text)
	}
	return root, nil
}

func (o Options) names() *names {
	n := &names{local: longestFirst(o.Fields), fields: map[string][]string{}, decimal: o.Locale.decimal()}
	for table, fields := range o.Tables {
		n.tables = append(n.tables, table)
		n.fields[strings.ToLower(table)] = longestFirst(fields)
	}
	n.tables = longestFirst(n.tables)
	if o.Library != nil {
		for _, f := range o.Library.funcs {
			n.funcs = append(n.funcs, f.name)
		}
		n.funcs = longestFirst(n.funcs)
	}
	return n
}

func longestFirst(in []string) []string {
	out := append([]string(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return len([]rune(out[i])) > len([]rune(out[j])) })
	return out
}

type parser struct {
	tokens []token
	i      int
	lib    *Library
	scopes []map[string]bool // names bound by Let and custom-function parameters
}

func (p *parser) peek() token { return p.tokens[p.i] }
func (p *parser) next() token { t := p.tokens[p.i]; p.i++; return t }

func (p *parser) bound(name string) bool {
	lower := strings.ToLower(name)
	for k := len(p.scopes) - 1; k >= 0; k-- {
		if p.scopes[k][lower] {
			return true
		}
	}
	return false
}

// FileMaker's precedence, loosest first: OR and XOR, AND, NOT, comparison,
// &, + and -, * and /, ^, then a sign.
func (p *parser) parseExpression() (node, error) { return p.parseOr() }

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().isKeyword("OR") || p.peek().isKeyword("XOR") {
		t := p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: strings.ToUpper(t.text), left: left, right: right, at: t.pos}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().isKeyword("AND") {
		at := p.next().pos
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "AND", left: left, right: right, at: at}
	}
	return left, nil
}

func (p *parser) parseNot() (node, error) {
	if p.peek().isKeyword("NOT") {
		at := p.next().pos
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: "NOT", operand: operand, at: at}, nil
	}
	return p.parseComparison()
}

var comparisonOperators = map[string]string{
	"=": "=", "==": "=", "<>": "<>", "!=": "<>", "<": "<", "<=": "<=", ">": ">", ">=": ">=",
}

func (p *parser) parseComparison() (node, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		op, ok := comparisonOperators[t.text]
		if t.kind != tokOperator || !ok {
			return left, nil
		}
		p.next()
		right, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: op, left: left, right: right, at: t.pos}
	}
}

func (p *parser) parseConcat() (node, error) {
	return p.parseLeftAssoc(p.parseAdditive, "&")
}

func (p *parser) parseAdditive() (node, error) {
	return p.parseLeftAssoc(p.parseMultiplicative, "+", "-")
}

func (p *parser) parseMultiplicative() (node, error) {
	return p.parseLeftAssoc(p.parsePower, "*", "/")
}

func (p *parser) parsePower() (node, error) {
	return p.parseLeftAssoc(p.parseUnary, "^")
}

func (p *parser) parseLeftAssoc(operand func() (node, error), ops ...string) (node, error) {
	left, err := operand()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		matched := false
		for _, op := range ops {
			if t.isOperator(op) {
				matched = true
				break
			}
		}
		if !matched {
			return left, nil
		}
		p.next()
		right, err := operand()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: t.text, left: left, right: right, at: t.pos}
	}
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().isOperator("-") || p.peek().isOperator("+") {
		t := p.next()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if t.text == "+" {
			return operand, nil
		}
		return unaryNode{op: "-", operand: operand, at: t.pos}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		value, err := decimal.NewFromString(t.text)
		if err != nil {
			return nil, errorAt(t.pos, "%q is not a number", t.text)
		}
		return numberNode{value: value, at: t.pos}, nil

	case tokString:
		return textNode{value: t.text, at: t.pos}, nil

	case tokLParen:
		inner, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, errorAt(p.peek().pos, "expected \")\"")
		}
		p.next()
		return inner, nil

	case tokLBracket:
		search, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokComma {
			return nil, errorAt(p.peek().pos, "expected \";\" between the search and its replacement")
		}
		p.next()
		replace, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRBracket {
			return nil, errorAt(p.peek().pos, "expected \"]\"")
		}
		p.next()
		return pairNode{search: search, replace: replace, at: t.pos}, nil

	case tokVariable:
		rep, err := p.parseRepetition()
		if err != nil {
			return nil, err
		}
		return variableNode{name: t.text, repetition: rep, at: t.pos}, nil

	case tokField:
		if t.table == "" && p.bound(t.text) {
			return localNode{name: strings.ToLower(t.text), at: t.pos}, nil
		}
		rep, err := p.parseRepetition()
		if err != nil {
			return nil, err
		}
		return fieldNode{table: t.table, name: t.text, repetition: rep, at: t.pos, missing: t.missing}, nil

	case tokIdent:
		if t.table == "" {
			switch {
			case p.peek().kind == tokLParen:
				return p.parseCall(t)
			case p.bound(t.text):
				return localNode{name: strings.ToLower(t.text), at: t.pos}, nil
			case p.isConstant(t.text):
				// A custom function without parameters is called by its
				// name alone, as FileMaker writes it: kVAT * price.
				return callNode{name: strings.ToUpper(t.text), at: t.pos}, nil
			case strings.EqualFold(t.text, "true"):
				return numberNode{value: decimal.NewFromInt(1), at: t.pos}, nil
			case strings.EqualFold(t.text, "false"):
				return numberNode{value: decimal.Zero, at: t.pos}, nil
			case strings.EqualFold(t.text, "pi"):
				return callNode{name: "PI", at: t.pos}, nil
			case strings.EqualFold(t.text, "random") && !p.isCustom("RANDOM"):
				return callNode{name: "RANDOM", at: t.pos}, nil
			case strings.EqualFold(t.text, "self"):
				// The object the formula belongs to (a layout object's
				// condition, a field's validation): the context answers it.
				return getNode{selector: "SELF", at: t.pos}, nil
			}
		}
		rep, err := p.parseRepetition()
		if err != nil {
			return nil, err
		}
		return fieldNode{table: t.table, name: t.text, repetition: rep, at: t.pos}, nil

	case tokEOF:
		return nil, errorAt(t.pos, "the formula ends too early")
	}
	return nil, errorAt(t.pos, "unexpected %q", t.text)
}

// parseRepetition reads an optional [n] after a field or variable.
func (p *parser) parseRepetition() (node, error) {
	if p.peek().kind != tokLBracket {
		return nil, nil
	}
	p.next()
	rep, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokRBracket {
		return nil, errorAt(p.peek().pos, "expected \"]\" to close the repetition")
	}
	p.next()
	return rep, nil
}

func (p *parser) parseCall(name token) (node, error) {
	upper := strings.ToUpper(name.text)
	switch upper {
	case "LET":
		return p.parseLet(name)
	case "GET":
		return p.parseGet(name)
	case "WHILE":
		return p.parseWhile(name)
	}
	p.next() // "("
	args := []node{}
	if p.peek().kind != tokRParen {
		for {
			arg, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.peek().kind == tokComma {
				p.next()
				continue
			}
			break
		}
	}
	if p.peek().kind != tokRParen {
		return nil, errorAt(p.peek().pos, "expected \")\" to close %s(", upper)
	}
	p.next()

	if p.lib != nil {
		if cf, ok := p.lib.funcs[upper]; ok {
			if len(args) != len(cf.params) {
				return nil, errorAt(name.pos, "%s takes %d argument(s), got %d", cf.name, len(cf.params), len(args))
			}
			return callNode{name: upper, args: args, at: name.pos}, nil
		}
	}
	if upper == "TEXTSTYLEADD" || upper == "TEXTSTYLEREMOVE" {
		// Bold, Italic + Underline… are style names here, not fields.
		for i := 1; i < len(args); i++ {
			args[i] = styleNames(args[i])
		}
	}
	fn, known := functions[upper]
	if !known {
		err := errorAt(name.pos, "there is no function called %q", name.text)
		err.Function = name.text
		return nil, err
	}
	if err := fn.checkArity(name.pos, upper, len(args)); err != nil {
		return nil, err
	}
	return callNode{name: upper, args: args, at: name.pos}, nil
}

// parseBindings reads name = value definitions separated by ";", inside
// brackets when bracketed. Each plain name is added to scope as soon as it is
// defined, so the values after it can use it.
func (p *parser) parseBindings(bracketed bool, scope map[string]bool) ([]letBinding, error) {
	var bindings []letBinding
	for {
		if bracketed && p.peek().kind == tokRBracket {
			break // an empty or ";"-terminated list
		}
		nt := p.next()
		var bname string
		switch {
		case nt.kind == tokVariable:
			bname = nt.text
		case (nt.kind == tokIdent || nt.kind == tokField) && nt.table == "":
			bname = strings.ToLower(nt.text)
		default:
			return nil, errorAt(nt.pos, "expected a name to define")
		}
		if !p.peek().isOperator("=") {
			return nil, errorAt(p.peek().pos, "expected \"=\" after %q", nt.text)
		}
		p.next()
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, letBinding{name: bname, value: value})
		if !strings.HasPrefix(bname, "$") {
			scope[bname] = true
		}
		if !bracketed || p.peek().kind != tokComma {
			break
		}
		p.next()
	}
	if bracketed {
		if p.peek().kind != tokRBracket {
			return nil, errorAt(p.peek().pos, "expected \"]\" to close the definitions")
		}
		p.next()
	}
	return bindings, nil
}

// parseLet reads Let ( name = value ; body ) and Let ( [ a = 1 ; b = a + 1 ] ;
// body ). Each name is visible to the values after it and to the body. A name
// written $x or $$x sets that script variable instead.
func (p *parser) parseLet(name token) (node, error) {
	p.next() // "("
	scope := map[string]bool{}
	p.scopes = append(p.scopes, scope)
	defer func() { p.scopes = p.scopes[:len(p.scopes)-1] }()

	bracketed := p.peek().kind == tokLBracket
	if bracketed {
		p.next()
	}
	bindings, err := p.parseBindings(bracketed, scope)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokComma {
		return nil, errorAt(p.peek().pos, "Let expects \";\" before its result")
	}
	p.next()
	body, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokRParen {
		return nil, errorAt(p.peek().pos, "expected \")\" to close LET(")
	}
	p.next()
	return letNode{bindings: bindings, body: body, at: name.pos}, nil
}

// parseWhile reads While ( [ initial ] ; condition ; [ logic ] ; result ). The
// names the initial definitions make are visible everywhere after them.
func (p *parser) parseWhile(name token) (node, error) {
	p.next() // "("
	scope := map[string]bool{}
	p.scopes = append(p.scopes, scope)
	defer func() { p.scopes = p.scopes[:len(p.scopes)-1] }()

	section := func(what string) ([]letBinding, error) {
		bracketed := p.peek().kind == tokLBracket
		if bracketed {
			p.next()
		}
		b, err := p.parseBindings(bracketed, scope)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokComma {
			return nil, errorAt(p.peek().pos, "While expects \";\" after its %s", what)
		}
		p.next()
		return b, nil
	}
	initial, err := section("initial definitions")
	if err != nil {
		return nil, err
	}
	condition, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokComma {
		return nil, errorAt(p.peek().pos, "While expects \";\" after its condition")
	}
	p.next()
	logic, err := section("logic")
	if err != nil {
		return nil, err
	}
	result, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokRParen {
		return nil, errorAt(p.peek().pos, "expected \")\" to close WHILE(")
	}
	p.next()
	return whileNode{initial: initial, condition: condition, logic: logic, result: result, at: name.pos}, nil
}

// parseGet reads Get ( Selector ).
func (p *parser) parseGet(name token) (node, error) {
	p.next() // "("
	sel := p.next()
	if (sel.kind != tokIdent && sel.kind != tokField) || sel.table != "" {
		return nil, errorAt(sel.pos, "Get expects the name of what to get, such as Get ( CurrentDate )")
	}
	if p.peek().kind != tokRParen {
		return nil, errorAt(p.peek().pos, "expected \")\" to close GET(")
	}
	p.next()
	return getNode{selector: strings.ToUpper(sel.text), at: name.pos}, nil
}

func styleNames(n node) node {
	switch t := n.(type) {
	case fieldNode:
		if t.table == "" {
			return textNode{value: t.name, at: t.at}
		}
	case binaryNode:
		return binaryNode{op: t.op, left: styleNames(t.left), right: styleNames(t.right), at: t.at}
	}
	return n
}
