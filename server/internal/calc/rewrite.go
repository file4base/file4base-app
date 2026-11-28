package calc

import (
	"sort"
	"strings"
)

// Rewrite returns the formula with every field reference replaced by what
// rename answers for it, and every number written with "." as its decimal
// separator, whatever the locale it was written in. Text, comments, spacing
// and everything else stay exactly as written.
//
// It is how an imported formula is made to read the fields it was written
// against once they have new names: the formula is read with the source's
// names and locale (opts), so a field called "Prix HT €" or a French 1,5 is
// understood, and is written back in the form File4Base stores.
//
// rename gets the occurrence ("" for the formula's own table) and the field
// as the formula wrote them, and returns the text to put in their place, such
// as "prix_ht" or "lines::amount". A reference it cannot place (ok false) is
// left as written and listed in unresolved, in the order of the formula.
func Rewrite(src string, opts Options, rename func(table, field string) (string, bool)) (out string, unresolved []Reference, err error) {
	expr, err := ParseWith(src, opts)
	if err != nil {
		return "", nil, err
	}
	return rewrite(expr, rename)
}

// RewriteFunction is Rewrite for the formula of a custom function, in which
// its parameters are names of their own rather than fields. opts.Library
// holds the functions it may call, itself included.
func RewriteFunction(fn CustomFunction, opts Options, rename func(table, field string) (string, bool)) (out string, unresolved []Reference, err error) {
	known := opts.names()
	root, err := parseFormula(fn.Formula, known, opts.Library, fn.Parameters)
	if err != nil {
		return "", nil, err
	}
	return rewrite(&Expr{root: root, src: fn.Formula, lib: opts.Library, known: known}, rename)
}

func rewrite(expr *Expr, rename func(table, field string) (string, bool)) (out string, unresolved []Reference, err error) {
	src := expr.src
	tokens, err := lex(src, expr.known)
	if err != nil {
		return "", nil, err
	}
	ends := make(map[int]int, len(tokens))
	for _, t := range tokens {
		ends[t.pos] = t.end
	}

	var edits []edit
	seen := map[int]bool{}
	expr.walkOwn(func(n node) {
		f, ok := n.(fieldNode)
		if !ok || seen[f.at] {
			return
		}
		seen[f.at] = true
		if f.missing {
			unresolved = append(unresolved, Reference{Table: f.table, Field: f.name, Missing: true})
			return
		}
		if text, ok := rename(f.table, f.name); ok {
			edits = append(edits, edit{f.at, ends[f.at], text})
			return
		}
		unresolved = append(unresolved, Reference{Table: f.table, Field: f.name})
	})
	if d := expr.known.decimalSeparator(); d != '.' {
		for _, t := range tokens {
			if t.kind == tokNumber {
				edits = append(edits, edit{t.pos, t.end, t.text})
			}
		}
	}
	return applyEdits(src, edits), unresolved, nil
}

// edit replaces the runes of a formula from from to to with text.
type edit struct {
	from, to int
	text     string
}

func applyEdits(src string, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].from < edits[j].from })
	runes := []rune(src)
	var b strings.Builder
	at := 0
	for _, e := range edits {
		b.WriteString(string(runes[at:e.from]))
		b.WriteString(e.text)
		at = e.to
	}
	b.WriteString(string(runes[at:]))
	return b.String()
}

// ReplaceSelf returns the formula with each Self, the value of the field or
// object the formula belongs to, written as with, such as
// "${Invoices}::Amount". Everything else stays exactly as written; a Self
// inside quotes or a comment is text, and stays.
//
// It is how a condition FileMaker evaluates on a layout object, whose Self is
// the object's field, is made to read that field where nothing answers Self.
func ReplaceSelf(src string, opts Options, with string) (string, error) {
	expr, err := ParseWith(src, opts)
	if err != nil {
		return "", err
	}
	tokens, err := lex(src, expr.known)
	if err != nil {
		return "", err
	}
	self := make(map[int]int, len(tokens))
	for _, t := range tokens {
		if t.isKeyword("self") {
			self[t.pos] = t.end
		}
	}
	var edits []edit
	expr.walkOwn(func(n node) {
		if g, ok := n.(getNode); ok && g.selector == "SELF" {
			if end, ok := self[g.at]; ok {
				edits = append(edits, edit{g.at, end, with})
				delete(self, g.at)
			}
		}
	})
	return applyEdits(src, edits), nil
}

// QuoteName writes a field or occurrence name so that a formula reads it back
// as that name: as itself when it is a plain word, otherwise as ${name}.
func QuoteName(name string) string {
	plain := name != ""
	for i, r := range name {
		if i == 0 && !isNameStart(r) || i > 0 && !isNameRune(r) {
			plain = false
			break
		}
	}
	if plain && !isReservedWord(name) {
		return name
	}
	return "${" + name + "}"
}

// QuoteQualifiedName writes Occurrence::Field so that a formula reads it back
// as that field of that occurrence. A quoted field needs its occurrence
// quoted too: only ${Occurrence}:: is read as one before a ${Field}.
func QuoteQualifiedName(occurrence, field string) string {
	f := QuoteName(field)
	if o := QuoteName(occurrence); o == occurrence && f == field {
		return o + "::" + f
	}
	return "${" + occurrence + "}::" + f
}

func isReservedWord(name string) bool {
	switch strings.ToUpper(name) {
	case "AND", "OR", "XOR", "NOT", "TRUE", "FALSE", "PI", "RANDOM", "SELF":
		return true
	}
	return false
}

// QuoteFieldName is QuoteName for a field a formula calling the custom
// functions of lib reads: a field named like one of them is quoted too, so
// it is not read as a call to it.
func QuoteFieldName(name string, lib *Library) string {
	if lib.Has(name) {
		return "${" + name + "}"
	}
	return QuoteName(name)
}
