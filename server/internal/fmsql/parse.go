package fmsql

import (
	"strconv"
	"strings"
)

// Select is a parsed SELECT statement.
type Select struct {
	src      string
	Distinct bool
	Items    []SelectItem
	From     []FromItem
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  []OrderItem
	Offset   *int64
	Fetch    *int64
	// Params is how many ? the statement has.
	Params int
}

// SelectItem is one column of the result: an expression, or * (Star) for
// every field of the tables, or of one (StarTable).
type SelectItem struct {
	Star      bool
	StarTable string
	Expr      Expr
	Alias     string
	pos       int
}

// FromItem is a table of the FROM clause and the tables joined to it.
type FromItem struct {
	Table TableRef
	Joins []Join
}

// TableRef is a table occurrence, by name, and the alias the query gives it.
type TableRef struct {
	Name  string
	Alias string
	pos   int
}

// Join is [INNER] JOIN or LEFT [OUTER] JOIN.
type Join struct {
	Left  bool
	Table TableRef
	On    Expr
}

// OrderItem is one key of ORDER BY.
type OrderItem struct {
	Expr Expr
	Desc bool
}

// Expr is an expression of the query.
type Expr interface{ position() int }

// Column is a field, qualified by a table occurrence or alias or not.
type Column struct {
	Table, Name string
	pos         int
	// star is the field a * stands for, resolved already.
	star *starField
}

type starField struct {
	table *bound
	field Field
}

// Literal is a constant: text, a number, a date, a time, a timestamp, NULL.
type Literal struct {
	Value Value
	pos   int
}

// Param is a ?, Index from 0 in the order they are written.
type Param struct {
	Index int
	pos   int
}

// Unary is -x or +x.
type Unary struct {
	Op  string
	X   Expr
	pos int
}

// Binary is an arithmetic (+ - * /), concatenation (||), comparison
// (= <> < > <= >=) or logical (AND, OR) operation.
type Binary struct {
	Op   string
	L, R Expr
	pos  int
}

// Not is NOT x.
type Not struct {
	X   Expr
	pos int
}

// Like is x [NOT] LIKE pattern.
type Like struct {
	X, Pattern Expr
	Negated    bool
	pos        int
}

// Between is x [NOT] BETWEEN lo AND hi.
type Between struct {
	X, Lo, Hi Expr
	Negated   bool
	pos       int
}

// In is x [NOT] IN ( list ).
type In struct {
	X       Expr
	List    []Expr
	Negated bool
	pos     int
}

// IsNull is x IS [NOT] NULL.
type IsNull struct {
	X       Expr
	Negated bool
	pos     int
}

// Call is a function: an aggregate (COUNT, SUM, AVG, MIN, MAX), with
// DISTINCT or COUNT(*), or a scalar function.
type Call struct {
	Name     string // upper case
	Distinct bool
	Star     bool
	Args     []Expr
	pos      int
}

func (e *Column) position() int  { return e.pos }
func (e *Literal) position() int { return e.pos }
func (e *Param) position() int   { return e.pos }
func (e *Unary) position() int   { return e.pos }
func (e *Binary) position() int  { return e.pos }
func (e *Not) position() int     { return e.pos }
func (e *Like) position() int    { return e.pos }
func (e *Between) position() int { return e.pos }
func (e *In) position() int      { return e.pos }
func (e *IsNull) position() int  { return e.pos }
func (e *Call) position() int    { return e.pos }

// reserved are the words that cannot be a name unless quoted. They include
// the SQL FileMaker reads that File4Base does not, so such a query is
// refused for what it is rather than taken for a name.
var reserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`ALL AND ANY AS ASC BETWEEN BY CASE CAST CROSS CURRENT_DATE CURRENT_TIME
		CURRENT_TIMESTAMP DATE DELETE DESC DISTINCT ELSE END ESCAPE EXCEPT EXISTS FETCH FIRST FOR FROM FULL
		GROUP HAVING IN INNER INSERT INTERSECT INTO IS JOIN LEFT LIKE LIMIT NATURAL NEXT NOT NULL OFFSET
		ON ONLY OR ORDER OUTER PERCENT RIGHT ROW ROWS SELECT SET THEN TIES TIME TIMESTAMP UNION UPDATE
		USING VALUES WHEN WHERE WITH`) {
		reserved[w] = true
	}
}

// maxDepth bounds how deeply expressions nest, so a query cannot exhaust the
// parser's stack.
const maxDepth = 100

// maxTreeDepth bounds how deep an expression is once read, a chain of
// operators included (a + b + c… is as deep as it is long), so that what
// translating it costs stays small.
const maxTreeDepth = 400

// maxTokens bounds the words, values and signs of a query, so that a long
// chain of operators, which does not nest, is bounded too.
const maxTokens = 10000

type parser struct {
	src    string
	toks   []token
	i      int
	params int
	depth  int
}

// Parse reads a query. Only one SELECT statement is accepted.
func Parse(src string) (*Select, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	if len(toks) > maxTokens {
		return nil, errorAt(src, 0, codeTooComplex, "The query is too complex: it has more than %d words, values and signs.", maxTokens)
	}
	p := &parser{src: src, toks: toks}
	stmt, err := p.selectStatement()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, p.unexpected(t)
	}
	stmt.src = src
	stmt.Params = p.params
	if e := stmt.tooDeep(); e != nil {
		return nil, errorAt(src, e.position(), codeTooComplex,
			"The query is too complex: an expression is more than %d operations deep.", maxTreeDepth)
	}
	return stmt, nil
}

// tooDeep is an expression of the statement deeper than maxTreeDepth.
func (s *Select) tooDeep() Expr {
	exprs := []Expr{s.Where, s.Having}
	for _, item := range s.Items {
		exprs = append(exprs, item.Expr)
	}
	for _, f := range s.From {
		for _, j := range f.Joins {
			exprs = append(exprs, j.On)
		}
	}
	exprs = append(exprs, s.GroupBy...)
	for _, o := range s.OrderBy {
		exprs = append(exprs, o.Expr)
	}
	for _, e := range exprs {
		if e != nil && depthOf(e, 1) > maxTreeDepth {
			return e
		}
	}
	return nil
}

// depthOf is how deep e is, from depth, stopping once past maxTreeDepth.
func depthOf(e Expr, depth int) int {
	if depth > maxTreeDepth {
		return depth
	}
	deepest := depth
	for _, c := range children(e) {
		if d := depthOf(c, depth+1); d > deepest {
			deepest = d
		}
	}
	return deepest
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) peekAt(n int) token {
	if p.i+n < len(p.toks) {
		return p.toks[p.i+n]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

// accept consumes the keyword or symbol s if it comes next.
func (p *parser) accept(s string) bool {
	if p.peek().is(s) {
		p.i++
		return true
	}
	return false
}

func (p *parser) expect(s string) error {
	if !p.accept(s) {
		return p.unexpected(p.peek())
	}
	return nil
}

func (p *parser) fail(pos, code int, format string, args ...any) error {
	return errorAt(p.src, pos, code, format, args...)
}

// unexpected is a syntax error at t, naming the SQL File4Base does not run
// when that is what t is.
func (p *parser) unexpected(t token) error {
	if t.kind == tokWord {
		switch strings.ToUpper(t.text) {
		case "UNION", "INTERSECT", "EXCEPT":
			return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run %s.", strings.ToUpper(t.text))
		case "RIGHT", "FULL":
			return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: %s OUTER JOIN is not supported.", strings.ToUpper(t.text))
		case "CROSS", "NATURAL", "USING":
			return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run %s joins.", strings.ToUpper(t.text))
		case "CASE", "CAST", "EXISTS", "ANY", "ESCAPE", "CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP", "LIMIT", "FOR":
			return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run %s.", strings.ToUpper(t.text))
		case "INSERT", "UPDATE", "DELETE":
			return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: ExecuteSQL runs SELECT statements only.")
		}
	}
	if t.kind == tokEOF {
		return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: it ends too early.")
	}
	return p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query.")
}

func (p *parser) selectStatement() (*Select, error) {
	if !p.peek().is("SELECT") {
		t := p.peek()
		if t.kind == tokWord && (t.is("INSERT") || t.is("UPDATE") || t.is("DELETE") || t.is("CREATE") ||
			t.is("DROP") || t.is("ALTER")) {
			return nil, p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: ExecuteSQL runs SELECT statements only.")
		}
		return nil, p.unexpected(t)
	}
	p.next()
	s := &Select{}
	if p.accept("DISTINCT") {
		s.Distinct = true
	} else {
		p.accept("ALL")
	}
	for {
		item, err := p.selectItem()
		if err != nil {
			return nil, err
		}
		s.Items = append(s.Items, item)
		if !p.accept(",") {
			break
		}
	}
	if err := p.expect("FROM"); err != nil {
		return nil, err
	}
	for {
		from, err := p.fromItem()
		if err != nil {
			return nil, err
		}
		s.From = append(s.From, from)
		if !p.accept(",") {
			break
		}
	}
	var err error
	if p.accept("WHERE") {
		if s.Where, err = p.expr(); err != nil {
			return nil, err
		}
	}
	if p.accept("GROUP") {
		if err := p.expect("BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			s.GroupBy = append(s.GroupBy, e)
			if !p.accept(",") {
				break
			}
		}
	}
	if p.accept("HAVING") {
		if s.Having, err = p.expr(); err != nil {
			return nil, err
		}
	}
	if p.accept("ORDER") {
		if err := p.expect("BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			item := OrderItem{Expr: e}
			if p.accept("DESC") {
				item.Desc = true
			} else {
				p.accept("ASC")
			}
			s.OrderBy = append(s.OrderBy, item)
			if !p.accept(",") {
				break
			}
		}
	}
	if p.peek().is("OFFSET") {
		t := p.next()
		n, err := p.count(t, codeBadOffset, "offset count in OFFSET")
		if err != nil {
			return nil, err
		}
		if !p.accept("ROWS") && !p.accept("ROW") {
			return nil, p.unexpected(p.peek())
		}
		s.Offset = &n
	}
	if p.peek().is("FETCH") {
		t := p.next()
		if !p.accept("FIRST") && !p.accept("NEXT") {
			return nil, p.unexpected(p.peek())
		}
		n := int64(1)
		if p.peek().kind == tokNumber {
			var err error
			if n, err = p.count(t, codeBadFetch, "fetch count in FETCH"); err != nil {
				return nil, err
			}
			if n < 1 {
				return nil, p.fail(t.pos, codeBadFetch, "The fetch count in FETCH clause is not valid.")
			}
		}
		if p.peek().is("PERCENT") {
			return nil, p.fail(p.peek().pos, codeBadFetch, "The fetch count in FETCH clause is not valid: File4Base does not run FETCH ... PERCENT.")
		}
		if !p.accept("ROWS") && !p.accept("ROW") {
			return nil, p.unexpected(p.peek())
		}
		if p.peek().is("WITH") {
			return nil, p.fail(p.peek().pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run FETCH ... WITH TIES.")
		}
		if err := p.expect("ONLY"); err != nil {
			return nil, err
		}
		s.Fetch = &n
	}
	return s, nil
}

// count reads the whole number of an OFFSET or a FETCH.
func (p *parser) count(at token, code int, what string) (int64, error) {
	t := p.next()
	n, err := strconv.ParseInt(t.text, 10, 64)
	if t.kind != tokNumber || err != nil || n < 0 {
		return 0, p.fail(at.pos, code, "The %s clause is not valid.", what)
	}
	return n, nil
}

func (p *parser) selectItem() (SelectItem, error) {
	t := p.peek()
	if t.is("*") {
		p.next()
		return SelectItem{Star: true, pos: t.pos}, nil
	}
	if (t.kind == tokWord || t.kind == tokQuoted) && p.peekAt(1).is(".") && p.peekAt(2).is("*") {
		if t.kind == tokWord && reserved[strings.ToUpper(t.text)] {
			return SelectItem{}, p.unexpected(t)
		}
		p.i += 3
		return SelectItem{Star: true, StarTable: t.text, pos: t.pos}, nil
	}
	e, err := p.expr()
	if err != nil {
		return SelectItem{}, err
	}
	item := SelectItem{Expr: e, pos: t.pos}
	alias, err := p.alias()
	if err != nil {
		return SelectItem{}, err
	}
	item.Alias = alias
	return item, nil
}

// alias reads [AS] name after a column or a table.
func (p *parser) alias() (string, error) {
	explicit := p.accept("AS")
	t := p.peek()
	switch {
	case t.kind == tokQuoted, t.kind == tokWord && !reserved[strings.ToUpper(t.text)]:
		p.next()
		return t.text, nil
	case explicit:
		return "", p.unexpected(t)
	}
	return "", nil
}

func (p *parser) tableRef() (TableRef, error) {
	t := p.next()
	if t.kind != tokQuoted && (t.kind != tokWord || reserved[strings.ToUpper(t.text)]) {
		if t.is("(") {
			return TableRef{}, p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run subqueries.")
		}
		return TableRef{}, p.unexpected(t)
	}
	ref := TableRef{Name: t.text, pos: t.pos}
	alias, err := p.alias()
	if err != nil {
		return TableRef{}, err
	}
	ref.Alias = alias
	return ref, nil
}

func (p *parser) fromItem() (FromItem, error) {
	table, err := p.tableRef()
	if err != nil {
		return FromItem{}, err
	}
	item := FromItem{Table: table}
	for {
		var join Join
		switch {
		case p.accept("JOIN"):
		case p.peek().is("INNER") && p.peekAt(1).is("JOIN"):
			p.i += 2
		case p.peek().is("LEFT"):
			p.next()
			p.accept("OUTER")
			if err := p.expect("JOIN"); err != nil {
				return FromItem{}, err
			}
			join.Left = true
		default:
			return item, nil
		}
		if join.Table, err = p.tableRef(); err != nil {
			return FromItem{}, err
		}
		if err := p.expect("ON"); err != nil {
			return FromItem{}, err
		}
		if join.On, err = p.expr(); err != nil {
			return FromItem{}, err
		}
		item.Joins = append(item.Joins, join)
	}
}

func (p *parser) enter(pos int) error {
	p.depth++
	if p.depth > maxDepth {
		return p.fail(pos, codeTooComplex, "The query is too complex: expressions nest more than %d deep.", maxDepth)
	}
	return nil
}

func (p *parser) expr() (Expr, error) {
	if err := p.enter(p.peek().pos); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	return p.or()
}

func (p *parser) or() (Expr, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.peek().is("OR") {
		t := p.next()
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "OR", L: left, R: right, pos: t.pos}
	}
	return left, nil
}

func (p *parser) and() (Expr, error) {
	left, err := p.not()
	if err != nil {
		return nil, err
	}
	for p.peek().is("AND") {
		t := p.next()
		right, err := p.not()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "AND", L: left, R: right, pos: t.pos}
	}
	return left, nil
}

func (p *parser) not() (Expr, error) {
	if p.peek().is("NOT") {
		t := p.next()
		if err := p.enter(t.pos); err != nil {
			return nil, err
		}
		defer func() { p.depth-- }()
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return &Not{X: x, pos: t.pos}, nil
	}
	return p.predicate()
}

func (p *parser) predicate() (Expr, error) {
	left, err := p.additive()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	for _, op := range []string{"=", "<>", "<=", ">=", "<", ">"} {
		if t.is(op) {
			p.next()
			if p.peek().is("ANY") || p.peek().is("ALL") {
				return nil, p.fail(p.peek().pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run subqueries.")
			}
			right, err := p.additive()
			if err != nil {
				return nil, err
			}
			return &Binary{Op: op, L: left, R: right, pos: t.pos}, nil
		}
	}
	negated := false
	if t.is("NOT") && (p.peekAt(1).is("LIKE") || p.peekAt(1).is("BETWEEN") || p.peekAt(1).is("IN")) {
		p.next()
		negated = true
		t = p.peek()
	}
	switch {
	case t.is("LIKE"):
		p.next()
		pattern, err := p.additive()
		if err != nil {
			return nil, err
		}
		if p.peek().is("ESCAPE") {
			return nil, p.unexpected(p.peek())
		}
		return &Like{X: left, Pattern: pattern, Negated: negated, pos: t.pos}, nil
	case t.is("BETWEEN"):
		p.next()
		lo, err := p.additive()
		if err != nil {
			return nil, err
		}
		if err := p.expect("AND"); err != nil {
			return nil, err
		}
		hi, err := p.additive()
		if err != nil {
			return nil, err
		}
		return &Between{X: left, Lo: lo, Hi: hi, Negated: negated, pos: t.pos}, nil
	case t.is("IN"):
		p.next()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		if p.peek().is("SELECT") {
			return nil, p.fail(p.peek().pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run subqueries.")
		}
		in := &In{X: left, Negated: negated, pos: t.pos}
		for {
			e, err := p.additive()
			if err != nil {
				return nil, err
			}
			in.List = append(in.List, e)
			if !p.accept(",") {
				break
			}
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return in, nil
	case t.is("IS") && !negated:
		p.next()
		is := &IsNull{X: left, Negated: p.accept("NOT"), pos: t.pos}
		if err := p.expect("NULL"); err != nil {
			return nil, err
		}
		return is, nil
	case negated:
		return nil, p.unexpected(t)
	}
	return left, nil
}

func (p *parser) additive() (Expr, error) {
	left, err := p.multiplicative()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if !t.is("+") && !t.is("-") && !t.is("||") {
			return left, nil
		}
		p.next()
		right, err := p.multiplicative()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: t.text, L: left, R: right, pos: t.pos}
	}
}

func (p *parser) multiplicative() (Expr, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if !t.is("*") && !t.is("/") {
			return left, nil
		}
		p.next()
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: t.text, L: left, R: right, pos: t.pos}
	}
}

func (p *parser) unary() (Expr, error) {
	t := p.peek()
	if t.is("-") || t.is("+") {
		p.next()
		if err := p.enter(t.pos); err != nil {
			return nil, err
		}
		defer func() { p.depth-- }()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: t.text, X: x, pos: t.pos}, nil
	}
	return p.primary()
}

// aggregates are the functions that total a column across the rows.
var aggregates = map[string]bool{"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true}

func (p *parser) primary() (Expr, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		n, ok := parseNumber(t.text)
		if !ok {
			return nil, p.fail(t.pos, codeSyntax, "There is an error in the syntax of the query: %s is not a number SQL can hold.", t.text)
		}
		return &Literal{Value: Value{Kind: KindNumber, Text: n.String()}, pos: t.pos}, nil
	case tokString:
		return &Literal{Value: Value{Kind: KindText, Text: t.text}, pos: t.pos}, nil
	case tokParam:
		p.params++
		return &Param{Index: p.params - 1, pos: t.pos}, nil
	case tokQuoted:
		return p.column(t)
	case tokSymbol:
		if t.text == "(" {
			if p.peek().is("SELECT") {
				return nil, p.fail(p.peek().pos, codeSyntax, "There is an error in the syntax of the query: File4Base does not run subqueries.")
			}
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return e, nil
		}
	case tokWord:
		upper := strings.ToUpper(t.text)
		switch upper {
		case "NULL":
			return &Literal{Value: Value{Kind: KindNull}, pos: t.pos}, nil
		case "DATE", "TIME", "TIMESTAMP":
			s := p.peek()
			if s.kind != tokString {
				return nil, p.unexpected(s)
			}
			p.next()
			v, ok := temporalLiteral(upper, s.text)
			if !ok {
				return nil, p.fail(s.pos, codeBadTemporal, "The literal value %q is not a valid DATE, TIME or TIMESTAMP.", s.text)
			}
			return &Literal{Value: v, pos: t.pos}, nil
		}
		if p.peek().is("(") {
			return p.call(t, upper)
		}
		if reserved[upper] {
			return nil, p.unexpected(t)
		}
		return p.column(t)
	}
	return nil, p.unexpected(t)
}

// column reads name or table.name, t being the first name.
func (p *parser) column(t token) (Expr, error) {
	if !p.peek().is(".") {
		return &Column{Name: t.text, pos: t.pos}, nil
	}
	p.next()
	n := p.next()
	if n.kind != tokQuoted && (n.kind != tokWord || reserved[strings.ToUpper(n.text)]) {
		return nil, p.unexpected(n)
	}
	if p.peek().is(".") {
		return nil, p.unexpected(p.peek())
	}
	return &Column{Table: t.text, Name: n.text, pos: t.pos}, nil
}

func (p *parser) call(t token, name string) (Expr, error) {
	if reserved[name] {
		return nil, p.unexpected(t)
	}
	p.next() // (
	c := &Call{Name: name, pos: t.pos}
	if aggregates[name] {
		if name == "COUNT" && p.peek().is("*") {
			p.next()
			c.Star = true
			return c, p.expect(")")
		}
		if p.accept("DISTINCT") {
			c.Distinct = true
		} else {
			p.accept("ALL")
		}
	}
	if p.accept(")") {
		return c, nil
	}
	for {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		c.Args = append(c.Args, e)
		if !p.accept(",") {
			break
		}
	}
	return c, p.expect(")")
}
