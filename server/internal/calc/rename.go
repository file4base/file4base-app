package calc

import (
	"fmt"
	"sort"
	"strings"
)

// CallsFunction reports whether the formula itself, as written, calls the
// custom function name (whatever its case), with arguments or, for one
// without parameters, by its name alone. The custom functions it calls are
// not looked into: a formula that reaches name only through another
// function does not call it.
func (e *Expr) CallsFunction(name string) bool {
	return len(e.calls(strings.ToUpper(strings.TrimSpace(name)))) > 0
}

// CallCount is how many times the formula itself, as written, calls the
// custom function name.
func (e *Expr) CallCount(name string) int {
	return len(e.calls(strings.ToUpper(strings.TrimSpace(name))))
}

// calls lists where the formula itself calls the function upper.
func (e *Expr) calls(upper string) []int {
	var at []int
	seen := map[int]bool{}
	e.walkOwn(func(n node) {
		if c, ok := n.(callNode); ok && c.name == upper && !seen[c.at] {
			seen[c.at] = true
			at = append(at, c.at)
		}
	})
	sort.Ints(at)
	return at
}

// ParseFunction reads the formula of a custom function, in which its
// parameters are names of their own; opts.Library holds the functions it may
// call, itself included.
func ParseFunction(fn CustomFunction, opts Options) (*Expr, error) {
	known := opts.names()
	root, err := parseFormula(fn.Formula, known, opts.Library, fn.Parameters)
	if err != nil {
		return nil, err
	}
	return &Expr{root: root, src: fn.Formula, lib: opts.Library, known: known}, nil
}

// RenameFunction returns the formula src with every call to the custom
// function from written to instead, as FileMaker updates the formulas that
// call a custom function it renames. Text, comments and everything else stay
// as written; changed is false when the formula does not call it.
//
// opts.Library is the library before the rename, which src is read with;
// renamed is the library after it. params are the parameters of the custom
// function whose formula src is, nil for any other formula.
//
// The rewritten formula is read back with renamed, and must call the
// function exactly where src called it: a formula that reads a field of the
// new name as a plain word, which the rename would turn into a call, is an
// error rather than a formula that silently changes its meaning.
func RenameFunction(src string, params []string, opts Options, renamed *Library, from, to string) (out string, changed bool, err error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	before, err := ParseFunction(CustomFunction{Formula: src, Parameters: params}, opts)
	if err != nil {
		return src, false, err
	}
	at := before.calls(strings.ToUpper(from))
	// Where the new name is called already, before the rename: a case-only
	// rename keeps these.
	already := 0
	if !strings.EqualFold(from, to) {
		already = len(before.calls(strings.ToUpper(to)))
	}

	out = src
	if len(at) > 0 {
		tokens, err := lex(src, before.known)
		if err != nil {
			return src, false, err
		}
		ends := make(map[int]int, len(tokens))
		for _, t := range tokens {
			ends[t.pos] = t.end
		}
		runes := []rune(src)
		var b strings.Builder
		last := 0
		for _, pos := range at {
			end, ok := ends[pos]
			if !ok || end <= pos {
				return src, false, fmt.Errorf("the call to %s at %d cannot be rewritten", from, pos)
			}
			b.WriteString(string(runes[last:pos]))
			b.WriteString(to)
			last = end
		}
		b.WriteString(string(runes[last:]))
		out = b.String()
	}

	after := opts
	after.Library = renamed
	check, err := ParseFunction(CustomFunction{Formula: out, Parameters: params}, after)
	if err != nil {
		return src, false, fmt.Errorf("once %s is renamed %s, the formula no longer reads: %w", from, to, err)
	}
	if got := len(check.calls(strings.ToUpper(to))); got != len(at)+already {
		return src, false, fmt.Errorf("once %s is renamed %s, the formula would read %s as a call to it: quote the field as ${%s}", from, to, to, to)
	}
	return out, len(at) > 0 && out != src, nil
}

// RecursesEndlessly reports whether the custom function name, whatever its
// case, calls itself, directly or through other custom functions, before any
// condition could stop it: on every evaluation, so it never returns.
// FileMaker accepts recursive custom functions, and so does File4Base; only
// one that cannot end is refused. A call reached only through a branch of
// If, Case or Choose, or the right side of and/or, may not be made, and so
// does not count.
func (l *Library) RecursesEndlessly(name string) bool {
	if l == nil {
		return false
	}
	target := strings.ToUpper(strings.TrimSpace(name))
	start, ok := l.funcs[target]
	if !ok {
		return false
	}
	entered := map[string]bool{target: true}
	var rec func(n node) bool
	rec = func(n node) bool {
		switch t := n.(type) {
		case nil:
			return false
		case binaryNode:
			if t.op == "AND" || t.op == "OR" {
				return rec(t.left)
			}
		case callNode:
			if t.name == target {
				return true
			}
			if cf, ok := l.funcs[t.name]; ok {
				for _, a := range t.args {
					if rec(a) {
						return true
					}
				}
				if entered[t.name] {
					return false
				}
				entered[t.name] = true
				return rec(cf.body)
			}
			if fn, ok := functions[t.name]; ok && fn.lazy != nil {
				switch t.name {
				case "IF", "CASE", "CHOOSE":
					return len(t.args) > 0 && rec(t.args[0])
				}
				return false
			}
		case whileNode:
			for _, b := range t.initial {
				if rec(b.value) {
					return true
				}
			}
			return rec(t.condition)
		}
		for _, child := range children(n) {
			if rec(child) {
				return true
			}
		}
		return false
	}
	return rec(start.body)
}
