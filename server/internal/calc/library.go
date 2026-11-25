package calc

import (
	"fmt"
	"sort"
	"strings"
)

// CustomFunction is a solution's own function, defined in FileMaker's Manage
// Custom Functions: a name, its parameters, and a formula over them. It may
// call itself, as FileMaker's recursive custom functions do.
type CustomFunction struct {
	Name       string
	Parameters []string
	Formula    string
}

// Library is a solution's custom functions, parsed once and shared by every
// formula of the solution.
type Library struct {
	funcs map[string]*customFunc // by upper-case name
}

type customFunc struct {
	name   string
	params []string // lower case
	body   node
}

// LibraryError names the custom functions whose formula could not be read.
type LibraryError struct {
	Problems map[string]error
}

func (e *LibraryError) Error() string {
	names := make([]string, 0, len(e.Problems))
	for n := range e.Problems {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s: %v", n, e.Problems[n]))
	}
	return "custom functions that cannot be read: " + strings.Join(parts, "; ")
}

// NewLibrary parses a solution's custom functions against the same names its
// formulas use. A function whose formula cannot be read is left out and
// reported in a *LibraryError, so the formulas that do not call it still work;
// the library is usable either way.
func NewLibrary(defs []CustomFunction, opts Options) (*Library, error) {
	lib := &Library{funcs: map[string]*customFunc{}}
	// Every name is declared first so the functions may call each other,
	// and themselves, whatever order they are defined in.
	for _, d := range defs {
		params := make([]string, len(d.Parameters))
		for i, p := range d.Parameters {
			params[i] = strings.ToLower(strings.TrimSpace(p))
		}
		lib.funcs[strings.ToUpper(strings.TrimSpace(d.Name))] = &customFunc{name: strings.TrimSpace(d.Name), params: params}
	}
	opts.Library = lib
	known := opts.names()
	problems := map[string]error{}
	for _, d := range defs {
		key := strings.ToUpper(strings.TrimSpace(d.Name))
		body, err := parseFormula(d.Formula, known, lib, d.Parameters)
		if err != nil {
			problems[d.Name] = err
			continue
		}
		lib.funcs[key].body = body
	}
	// A function that calls one that cannot be read cannot be computed
	// either, whatever its own formula says: it is left out too, and said
	// why, rather than failing each time a formula reaches it.
	for changed := true; changed; {
		changed = false
		for _, d := range defs {
			key := strings.ToUpper(strings.TrimSpace(d.Name))
			cf := lib.funcs[key]
			if cf == nil || cf.body == nil {
				continue
			}
			for _, callee := range lib.calls(cf.body) {
				if other := lib.funcs[callee]; other.body == nil {
					cf.body = nil
					problems[d.Name] = fmt.Errorf("calls the custom function %s, which cannot be read", other.name)
					changed = true
					break
				}
			}
		}
	}
	for _, d := range defs {
		key := strings.ToUpper(strings.TrimSpace(d.Name))
		if cf := lib.funcs[key]; cf != nil && cf.body == nil {
			delete(lib.funcs, key)
		}
	}
	if len(problems) > 0 {
		return lib, &LibraryError{Problems: problems}
	}
	return lib, nil
}

// calls lists the custom functions of the library a formula calls itself,
// in upper case.
func (l *Library) calls(root node) []string {
	seen := map[string]struct{}{}
	var rec func(node)
	rec = func(n node) {
		if n == nil {
			return
		}
		if c, ok := n.(callNode); ok {
			if _, custom := l.funcs[c.name]; custom {
				seen[c.name] = struct{}{}
			}
		}
		for _, child := range children(n) {
			rec(child)
		}
	}
	rec(root)
	return sortedKeys(seen)
}

// Names lists the custom functions of the library.
func (l *Library) Names() []string {
	if l == nil {
		return nil
	}
	out := make([]string, 0, len(l.funcs))
	for _, f := range l.funcs {
		out = append(out, f.name)
	}
	sort.Strings(out)
	return out
}
