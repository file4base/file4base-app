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
	for _, d := range defs {
		key := strings.ToUpper(strings.TrimSpace(d.Name))
		if lib.funcs[key].body == nil {
			delete(lib.funcs, key)
		}
	}
	if len(problems) > 0 {
		return lib, &LibraryError{Problems: problems}
	}
	return lib, nil
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
