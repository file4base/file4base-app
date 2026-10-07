package calc

import (
	"sort"
	"strconv"
	"strings"
)

// Expr is a parsed formula.
type Expr struct {
	root node
	src  string
}

// String returns the formula as it was written.
func (e *Expr) String() string { return e.src }

// Fields lists the field names the formula reads, sorted and without
// duplicates. It is what the schema service uses to find the order to compute
// calculations in, and to reject a formula that depends on itself.
func (e *Expr) Fields() []string {
	seen := map[string]struct{}{}
	collectFields(e.root, seen)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type node interface{ pos() int }

type numberNode struct {
	value float64
	at    int
}
type textNode struct {
	value string
	at    int
}
type boolNode struct {
	value bool
	at    int
}
type fieldNode struct {
	name string
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
	name string
	args []node
	at   int
}

func (n numberNode) pos() int { return n.at }
func (n textNode) pos() int   { return n.at }
func (n boolNode) pos() int   { return n.at }
func (n fieldNode) pos() int  { return n.at }
func (n unaryNode) pos() int  { return n.at }
func (n binaryNode) pos() int { return n.at }
func (n callNode) pos() int   { return n.at }

func collectFields(n node, into map[string]struct{}) {
	switch t := n.(type) {
	case fieldNode:
		into[t.name] = struct{}{}
	case unaryNode:
		collectFields(t.operand, into)
	case binaryNode:
		collectFields(t.left, into)
		collectFields(t.right, into)
	case callNode:
		for _, arg := range t.args {
			collectFields(arg, into)
		}
	}
}

// Parse reads a formula. An empty formula is an error: a calculation field
// with nothing to compute is a mistake, not an empty value.
func Parse(src string) (*Expr, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errorAt(0, "the formula is empty")
	}
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	root, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, errorAt(p.peek().pos, "unexpected %q after the end of the formula", p.peek().text)
	}
	return &Expr{root: root, src: src}, nil
}

type parser struct {
	tokens []token
	i      int
}

func (p *parser) peek() token { return p.tokens[p.i] }
func (p *parser) next() token { t := p.tokens[p.i]; p.i++; return t }
func (p *parser) backUp()     { p.i-- }
func (p *parser) atEnd() bool { return p.peek().kind == tokEOF }

// expression := orExpr
func (p *parser) parseExpression() (node, error) { return p.parseOr() }

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().isKeyword("OR") {
		at := p.next().pos
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "OR", left: left, right: right, at: at}
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

var comparisonOperators = map[string]struct{}{
	"=": {}, "==": {}, "<>": {}, "!=": {}, "<": {}, "<=": {}, ">": {}, ">=": {},
}

func (p *parser) parseComparison() (node, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if t.kind == tokOperator {
		if _, ok := comparisonOperators[t.text]; ok {
			p.next()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			return binaryNode{op: t.text, left: left, right: right, at: t.pos}, nil
		}
	}
	return left, nil
}

func (p *parser) parseConcat() (node, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for p.peek().isOperator("&") {
		at := p.next().pos
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "&", left: left, right: right, at: at}
	}
	return left, nil
}

func (p *parser) parseAdditive() (node, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.peek().isOperator("+") || p.peek().isOperator("-") {
		t := p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: t.text, left: left, right: right, at: t.pos}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().isOperator("*") || p.peek().isOperator("/") {
		t := p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: t.text, left: left, right: right, at: t.pos}
	}
	return left, nil
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
		value, err := strconv.ParseFloat(t.text, 64)
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

	case tokIdent:
		switch {
		case strings.EqualFold(t.text, "true"):
			return boolNode{value: true, at: t.pos}, nil
		case strings.EqualFold(t.text, "false"):
			return boolNode{value: false, at: t.pos}, nil
		}
		if p.peek().kind == tokLParen {
			return p.parseCall(t)
		}
		return fieldNode{name: t.text, at: t.pos}, nil

	case tokEOF:
		return nil, errorAt(t.pos, "the formula ends too early")
	}

	p.backUp()
	return nil, errorAt(t.pos, "unexpected %q", t.text)
}

func (p *parser) parseCall(name token) (node, error) {
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
		return nil, errorAt(p.peek().pos, "expected \")\" to close %s(", strings.ToUpper(name.text))
	}
	p.next()

	fn, known := functions[strings.ToUpper(name.text)]
	if !known {
		return nil, errorAt(name.pos, "there is no function called %q", name.text)
	}
	if err := fn.checkArity(name.pos, strings.ToUpper(name.text), len(args)); err != nil {
		return nil, err
	}
	return callNode{name: strings.ToUpper(name.text), args: args, at: name.pos}, nil
}
