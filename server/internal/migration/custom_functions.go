package migration

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// Custom functions: what FileMaker defines in Manage Custom Functions, a
// name, its parameters and a formula over them, which any formula of the file
// may call. A calculation calling one could not be read without it, so it
// was converted to a stored value and its formula lost. They are read from
// the design before any calculation is, translated like a calculation, and
// carried in the bundle, so the calculations that call them are imported as
// calculations and computed by the same functions.

// sourceCustomFunction is a custom function as the design document writes it.
type sourceCustomFunction struct {
	name       string
	parameters []string
	formula    string
	access     string // schema.CustomFunctionAccessAll or …FullAccess
}

// functionMissing is what FileMaker writes in place of a function it does not
// have, such as a plug-in's when the plug-in is not installed.
const functionMissing = "<Function Missing>"

// readXMLCustomFunctions reads the custom functions of an XML design: a
// Database Design Report's <CustomFunctionCatalog>, whose functions name
// their parameters in a parameters attribute, or a Save a Copy as XML
// <CustomFunctionsCatalog>, which lists them in an <ObjectList>.
func readXMLCustomFunctions(root *xmlNode) []sourceCustomFunction {
	var nodes []*xmlNode
	var visit func(*xmlNode)
	visit = func(container *xmlNode) {
		for _, child := range container.Children {
			switch {
			case nodeNamed(child, "CustomFunction", "customFunction"):
				nodes = append(nodes, child)
			case nodeNamed(child, "ObjectList", "Group", "group", "Folder", "folder"):
				visit(child)
			}
		}
	}
	for _, catalog := range designChildren(root, "CustomFunctionCatalog", "CustomFunctionsCatalog") {
		visit(catalog)
	}
	out := make([]sourceCustomFunction, 0, len(nodes))
	for _, n := range nodes {
		fn := sourceCustomFunction{
			name:    firstAttr(n, "name", "Name"),
			formula: calculationText(n),
			access:  customFunctionAccess(firstAttr(n, "access", "visible")),
		}
		if list := firstAttr(n, "parameters"); list != "" {
			fn.parameters = splitParameters(list)
		} else {
			for _, holder := range []*xmlNode{n, findFirstChild(n, "ObjectList", "ParameterList", "Parameters")} {
				if holder == nil {
					continue
				}
				for _, p := range holder.Children {
					if nodeNamed(p, "Parameter", "parameter") {
						if name := firstAttr(p, "name", "Name"); name != "" {
							fn.parameters = append(fn.parameters, name)
						}
					}
				}
			}
		}
		out = append(out, fn)
	}
	return out
}

// customFunctionAccess reads FileMaker's "Availability" of a function: "All"
// (visible="True" in a Database Design Report, "All accounts" or "Tous les
// comptes" in an HTML one) or only accounts with full access.
func customFunctionAccess(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all", "true", "all accounts", "tous les comptes":
		return schema.CustomFunctionAccessAll
	}
	return schema.CustomFunctionAccessFullAccess
}

func splitParameters(list string) []string {
	var out []string
	for _, p := range strings.Split(list, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// reHTMLCustomFunctionRow is one function of an HTML report's custom
// functions section, and reHTMLCell one of its cells: the name, the
// parameters, the availability and the formula.
var (
	reHTMLCustomFunctionRow = regexp.MustCompile(`(?is)<tr class="regularcharttext"[^>]*>(.*?)</tr>`)
	reHTMLCell              = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	reHTMLLineBreak         = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// readHTMLCustomFunctions reads the custom functions section of an HTML
// Database Design Report.
func readHTMLCustomFunctions(rawHTML string) []sourceCustomFunction {
	section := sectionBetween(rawHTML, `name="valCustomFunctionsSectionAnchor_"`, `class="partheading"`)
	if section == "" {
		return nil
	}
	if i := strings.Index(section, "</table>"); i >= 0 {
		section = section[:i]
	}
	var out []sourceCustomFunction
	for _, row := range reHTMLCustomFunctionRow.FindAllStringSubmatch(section, -1) {
		cells := reHTMLCell.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 4 {
			continue
		}
		out = append(out, sourceCustomFunction{
			name:       cleanHTMLText(cells[0][1]),
			parameters: splitParameters(cleanHTMLText(cells[1][1])),
			access:     customFunctionAccess(cleanHTMLText(cells[2][1])),
			formula:    htmlFormulaText(cells[3][1]),
		})
	}
	return out
}

// htmlFormulaText is a formula an HTML report writes, its lines kept: a
// "//" comment ends with its line.
func htmlFormulaText(cell string) string {
	text := reHTMLLineBreak.ReplaceAllString(cell, "\n")
	text = reHTMLTags.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, " ", " ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(text)
}

// bindCustomFunctions reads a design's custom functions with the names and
// the locale its formulas are written in, binds the fields they read to the
// imported ones, and puts them in the bundle. It sets the binding's
// libraries, so every calculation read afterwards may call them.
//
// Every name is known before any formula is read, so a function may call
// itself, or one defined after it, or one that calls it back. A function
// whose formula cannot be translated is reported and left out, without
// stopping the others; so is one that calls it, which could not be computed.
func bindCustomFunctions(defs []sourceCustomFunction, b *formulaBinding, bundle *schema.SolutionBundle) []ReportItem {
	var items []ReportItem
	var candidates []sourceCustomFunction
	seen := map[string]bool{}
	for _, fn := range defs {
		fn.name = strings.TrimSpace(fn.name)
		key := strings.ToUpper(fn.name)
		switch {
		case fn.name == "":
			items = append(items, ReportItem{Category: CategoryRejected, Domain: "custom_function", SourceName: "(unnamed)",
				Detail: "A custom function without a name was not migrated"})
			continue
		case seen[key]:
			items = append(items, ReportItem{Category: CategoryRejected, Domain: "custom_function", SourceName: fn.name,
				Detail: fmt.Sprintf("A second custom function named %s was not migrated; FileMaker names each once", fn.name)})
			continue
		}
		seen[key] = true
		candidates = append(candidates, fn)
	}
	if len(candidates) == 0 {
		return items
	}

	failed := map[string]error{} // by upper-case name
	source := calc.Options{Tables: b.occurrences, Locale: b.locale}
	var functions, stored *calc.Library
	var bound []schema.BundleCustomFunction
	var changed []bool
	// Each round reads the functions not yet failed; a function failing
	// makes the ones that call it fail on the next, until none does.
	for round := 0; round <= len(candidates); round++ {
		var defs []calc.CustomFunction
		for _, fn := range candidates {
			if failed[strings.ToUpper(fn.name)] == nil {
				defs = append(defs, calc.CustomFunction{Name: fn.name, Parameters: fn.parameters, Formula: fn.formula})
			}
		}
		lib, err := calc.NewLibrary(defs, source)
		var broken *calc.LibraryError
		if errors.As(err, &broken) {
			for name, problem := range broken.Problems {
				failed[strings.ToUpper(name)] = problem
			}
			continue
		}
		functions = lib
		opts := source
		opts.Library = lib
		bound, changed = bound[:0], changed[:0]
		var storedDefs []calc.CustomFunction
		progress := false
		for _, def := range defs {
			formula, unresolved, err := calc.RewriteFunction(def, opts, b.renamer(nil))
			if err == nil && len(unresolved) > 0 {
				err = unresolvedReferences(unresolved)
			}
			if err != nil {
				failed[strings.ToUpper(def.Name)] = err
				progress = true
				continue
			}
			fn := candidateNamed(candidates, def.Name)
			bound = append(bound, schema.BundleCustomFunction{Name: def.Name, Parameters: def.Parameters, Formula: formula, Access: fn.access})
			changed = append(changed, formula != def.Formula)
			storedDefs = append(storedDefs, calc.CustomFunction{Name: def.Name, Parameters: def.Parameters, Formula: formula})
		}
		if progress {
			continue
		}
		stored, err = calc.NewLibrary(storedDefs, calc.Options{})
		if errors.As(err, &broken) {
			// Not expected: the stored formula is the same formula with
			// File4Base's names. Said rather than imported broken.
			for name, problem := range broken.Problems {
				failed[strings.ToUpper(name)] = fmt.Errorf("its translation does not read back (%v)", problem)
			}
			continue
		}
		break
	}

	b.functions, b.stored, b.dropped = functions, stored, map[string]string{}
	for _, fn := range candidates {
		if failed[strings.ToUpper(fn.name)] != nil {
			b.dropped[strings.ToUpper(fn.name)] = fn.name
		}
	}
	for i, fn := range bound {
		item := ReportItem{Category: CategoryPreserved, Domain: "custom_function", SourceName: fn.Name, TargetName: fn.Name,
			Detail: fmt.Sprintf("Custom function %s(%s), available to %s: %s", fn.Name, strings.Join(fn.Parameters, "; "),
				accessLabel(fn.Access), candidateNamed(candidates, fn.Name).formula)}
		if changed[i] {
			item.Category = CategoryTranslated
			item.Detail = fmt.Sprintf("Custom function %s(%s), available to %s, bound to the imported fields: %q now reads %q",
				fn.Name, strings.Join(fn.Parameters, "; "), accessLabel(fn.Access), readableFieldRefs(candidateNamed(candidates, fn.Name).formula), readableFieldRefs(fn.Formula))
		}
		items = append(items, item)
	}
	for _, fn := range candidates {
		problem := failed[strings.ToUpper(fn.name)]
		if problem == nil {
			continue
		}
		items = append(items,
			ReportItem{Category: CategoryUnsupported, Domain: "custom_function", SourceName: fn.name,
				Detail: fmt.Sprintf("Custom function %s %s; it was not migrated, and the calculations that call it are converted to stored values",
					fn.name, b.unreadable(fn.formula, problem))},
			ReportItem{Category: CategoryManualActionRequired, Domain: "custom_function", SourceName: fn.name,
				Detail: manualCustomFunctionAction(fn.name)})
	}
	bundle.CustomFunctions = bound
	return items
}

// manualCustomFunctionAction says what to do about a custom function that
// was not migrated.
func manualCustomFunctionAction(name string) string {
	if calc.IsBuiltinFunction(name) {
		return fmt.Sprintf("Rename the custom function %s, which is named like a built-in function, and the calls to it, in FileMaker and migrate again, or rewrite them in File4Base", name)
	}
	return fmt.Sprintf("Rewrite the custom function %s and the calculations that call it in File4Base, or correct it in FileMaker and migrate again", name)
}

func candidateNamed(candidates []sourceCustomFunction, name string) sourceCustomFunction {
	for _, fn := range candidates {
		if strings.EqualFold(fn.name, name) {
			return fn
		}
	}
	return sourceCustomFunction{}
}

func accessLabel(access string) string {
	if access == schema.CustomFunctionAccessFullAccess {
		return "full-access accounts only"
	}
	return "all accounts"
}

// unresolvedReferences says which fields a custom function reads that the
// import does not have. A custom function has no table of its own, so a
// field it reads is named through an occurrence.
func unresolvedReferences(refs []calc.Reference) error {
	var names []string
	seen := map[string]bool{}
	for _, ref := range refs {
		name := ref.Field
		if ref.Table != "" {
			name = ref.Table + "::" + ref.Field
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return fmt.Errorf("reads %s, which is neither one of its parameters nor a field of the imported tables", quotedList(names))
}

// unreadable says why a formula could not be read: the FileMaker function or
// the custom function it calls that File4Base does not have, or what the
// parser stopped at.
func (b formulaBinding) unreadable(formula string, err error) string {
	if strings.Contains(formula, functionMissing) {
		return fmt.Sprintf("calls a function FileMaker shows as %s: a plug-in function whose plug-in was not installed when the file was saved, which File4Base does not have either", functionMissing)
	}
	var problem *calc.Error
	if errors.As(err, &problem) && problem.Function != "" {
		if name, ok := b.dropped[strings.ToUpper(problem.Function)]; ok {
			return fmt.Sprintf("calls the custom function %s, which could not be migrated", name)
		}
	}
	if errors.As(err, &problem) {
		return fmt.Sprintf("could not be read (%v)", err)
	}
	return err.Error()
}
