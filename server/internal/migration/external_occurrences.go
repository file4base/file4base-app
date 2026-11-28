package migration

import (
	"sort"
	"strings"
	"unicode"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// External table occurrences: occurrences of a table of another FileMaker
// file, reached through one of the file's External Data Sources (#107).
//
// The document says which data source an external occurrence goes through,
// and where FileMaker looks for that file, but not which table of it the
// occurrence shows: Save a Copy as XML names no base table for it, and the
// fields used through it are named only by the references that use them. So
// an external occurrence is migrated unbound, with its data source as a file
// reference carrying the FileMaker paths, and its fields named the way the
// other file's own migration names them. Which installed solution and which
// table it shows is decided when the reference is bound, never guessed here.

// ExternalOccurrenceReport describes one external occurrence of a converted
// file, for the operator who has to install and bind the file it reads.
type ExternalOccurrenceReport struct {
	Occurrence string   `json:"occurrence"`
	DataSource string   `json:"data_source"`
	Paths      []string `json:"paths"`
	// Table is the other file's table, when the document names it (a DDR
	// does); empty when it is resolved at bind time.
	Table string `json:"table,omitempty"`
	// Fields are the fields used through the occurrence, as the other
	// file's migration names them.
	Fields []string `json:"fields"`
	// SuggestedDatabase is the one installed solution whose name matches a
	// path of the data source, when exactly one does. Filled in by the API.
	SuggestedDatabase string `json:"suggested_database,omitempty"`
	// Cycle is true when the suggested solution itself has an external
	// data source that leads back to this file. Filled in by the API.
	Cycle bool `json:"cycle"`
}

// externalDataSource is one External Data Source of the converted file.
type externalDataSource struct {
	name      string
	paths     []string
	fileMaker bool
}

// readExternalDataSources reads the file's External Data Sources, by
// lower-cased name: Save a Copy as XML writes
// ExternalDataSourceCatalog/ExternalDataSource with a UniversalPathList, a DDR
// writes ExternalDataSourcesCatalog/FileReference with a pathList attribute.
func readExternalDataSources(root *xmlNode) map[string]externalDataSource {
	out := map[string]externalDataSource{}
	for _, catalog := range designChildren(root, "ExternalDataSourceCatalog", "ExternalDataSourcesCatalog") {
		for _, ds := range findNodes(catalog, "ExternalDataSource", "FileReference", "OdbcDataSource") {
			name := strings.TrimSpace(firstAttr(ds, "name", "Name"))
			if name == "" {
				continue
			}
			kind := firstAttr(ds, "type", "Type")
			src := externalDataSource{
				name:      name,
				fileMaker: ds.Name != "OdbcDataSource" && (kind == "" || strings.EqualFold(kind, "FileMaker")),
			}
			for _, list := range findNodes(ds, "UniversalPathList", "PathList") {
				src.paths = append(src.paths, pathLines(list.Text)...)
			}
			if attr := firstAttr(ds, "pathList", "PathList"); attr != "" {
				src.paths = append(src.paths, splitDDRPathList(attr)...)
			}
			out[strings.ToLower(name)] = src
		}
	}
	return out
}

func pathLines(text string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// splitDDRPathList splits a DDR pathList, which writes its paths on one
// line: a new path starts at a word that begins with a FileMaker path
// prefix (file:, fmnet:, filemac:, filewin:...), so a path with a space in
// it is kept whole.
func splitDDRPathList(attr string) []string {
	var out []string
	for _, line := range pathLines(attr) {
		var current []string
		for _, word := range strings.Fields(line) {
			if len(current) > 0 && isFileMakerPathStart(word) {
				out = append(out, strings.Join(current, " "))
				current = nil
			}
			current = append(current, word)
		}
		if len(current) > 0 {
			out = append(out, strings.Join(current, " "))
		}
	}
	return out
}

func isFileMakerPathStart(word string) bool {
	i := strings.IndexByte(word, ':')
	if i <= 0 {
		return false
	}
	switch strings.ToLower(word[:i]) {
	case "file", "fmnet", "filemac", "filewin", "filelinux", "fmp", "fmp12":
		return true
	}
	return false
}

// externalFieldName is the name the other file's migration gives one of its
// fields: the same sanitization, and a field named id, which File4Base
// keeps for its own primary key, moves to id_2 there as it does here.
// A field the document names by its FileMaker id only (namelessField) is
// given its marker "#<id>", which the bind resolves by that id; a field
// actually named "#4" is sanitized like any other name.
func externalFieldName(raw string) string {
	if id, ok := namelessFieldID(raw); ok {
		return schema.ExternalFieldMarker(id)
	}
	name := sanitizeIdentifier(raw)
	if name == "id" {
		return "id_2"
	}
	return name
}

// externalFieldRefs collects, for each external occurrence (by lower-cased
// name), the source names of the fields the document uses through it: in a
// relationship, a layout, a portal, a value list, a calculation or a script.
// Every such use names its occurrence, nested in Save a Copy as XML or in a
// table attribute in a DDR.
func externalFieldRefs(root *xmlNode, external map[string]string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	var walk func(*xmlNode)
	walk = func(n *xmlNode) {
		for _, c := range n.Children {
			if nodeNamed(c, "FieldReference", "Field") {
				occ := firstAttr(c, "table")
				if ref := findFirstChild(c, "TableOccurrenceReference"); ref != nil && occ == "" {
					occ = firstAttr(ref, "name", "Name")
				}
				field := referencedFieldName(c)
				key := strings.ToLower(occ)
				if _, ok := external[key]; ok && field != "" && !seen[key+"\x00"+field] {
					seen[key+"\x00"+field] = true
					out[key] = append(out[key], field)
				}
			}
			walk(c)
		}
	}
	walk(root)
	return out
}

// namelessFieldPrefix marks, inside the migration, a field reference that
// gave an id and an empty name, as FileMaker writes a field of a file it
// could not open when it saved the document. XML cannot carry U+0001, so no
// name read from a document starts with it: a field really named "#4" is
// never taken for one. It leaves the migration as the marker "#<id>"
// (externalFieldName), and nowhere else.
const namelessFieldPrefix = "\x01"

func namelessField(id string) string { return namelessFieldPrefix + id }

// namelessFieldID is the id of a reference namelessField made.
func namelessFieldID(ref string) (string, bool) {
	id, ok := strings.CutPrefix(ref, namelessFieldPrefix)
	return id, ok && id != ""
}

// readableFieldRefs writes the references namelessField made in text as
// their marker, for a report.
func readableFieldRefs(text string) string {
	return strings.ReplaceAll(text, namelessFieldPrefix, schema.ExternalFieldMarker(""))
}

// referencedFieldName is the name a field reference gives or, when it gives
// only an id, namelessField of that id.
func referencedFieldName(n *xmlNode) string {
	if name := firstAttr(n, "name", "Name"); name != "" {
		return name
	}
	if id := strings.TrimSpace(firstAttr(n, "id", "ID")); id != "" && schema.ValidSourceFieldID(id) {
		return namelessField(id)
	}
	return ""
}

// externalLayoutField is the external occurrence and the field, as the
// other file's migration names it (or its marker), of a layout field object
// (n, or the <FieldReference> it holds) that shows a field of an occurrence
// of another file's table; empty for any other field object. Such a field is
// never bound to a column of this file, whatever its name.
func externalLayoutField(n *xmlNode, occurrences []schema.BundleOccurrence) (occurrence, field string) {
	ref := n
	if inner := findFirstChild(n, "FieldReference"); inner != nil && firstAttr(n, "name", "Name", "field") == "" {
		ref = inner
	}
	occ := firstAttr(ref, "table", "Table")
	if to := findFirstChild(ref, "TableOccurrenceReference"); to != nil && occ == "" {
		occ = firstAttr(to, "name", "Name")
	}
	if occ == "" {
		return "", ""
	}
	name := firstAttr(ref, "field")
	if name == "" {
		name = referencedFieldName(ref)
	}
	for _, o := range occurrences {
		if o.External != nil && strings.EqualFold(o.Name, occ) && name != "" {
			return o.Name, externalFieldName(name)
		}
	}
	return "", ""
}

// markNamelessFieldReferences writes, in the text of a calculation, each
// field its <ChunkList> names by id only, as namelessField: FileMaker's text
// then shows the occurrence without the field ("Occ::" or a placeholder such
// as "Occ::<Field Missing>"), in the order the chunks give. Binding the
// formula then writes it ${Occ}::${#<id>}. A reference the text spells otherwise is left as
// it is, and read as the text has it.
func markNamelessFieldReferences(calcNode *xmlNode, text string) string {
	cursor := 0
	for _, chunk := range findNodes(calcNode, "Chunk") {
		if !strings.EqualFold(firstAttr(chunk, "type"), "FieldRef") {
			continue
		}
		ref := findFirstChild(chunk, "FieldReference")
		if ref == nil || firstAttr(ref, "name", "Name") != "" {
			continue
		}
		nameless := referencedFieldName(ref)
		to := findFirstChild(ref, "TableOccurrenceReference")
		if nameless == "" || to == nil || firstAttr(to, "name", "Name") == "" {
			continue
		}
		occurrence := firstAttr(to, "name", "Name")
		prefix := occurrence + "::"
		// The next reference to the occurrence that names no field: one
		// that names it is a field the chunks name. Only a replacement
		// moves the cursor, so a reference this chunk does not find is
		// still there for the next.
		begin, end := -1, 0
		for from := cursor; begin < 0; {
			at := strings.Index(text[from:], prefix)
			if at < 0 {
				break
			}
			start := from + at + len(prefix)
			if strings.HasPrefix(text[start:], "<") {
				begin, end = start-len(prefix), start
				if close := strings.IndexByte(text[start:], '>'); close > 0 {
					end = start + close + 1
				}
			} else if r := []rune(text[start:]); len(r) == 0 || !(unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) || r[0] == '_' || r[0] == '$') {
				begin, end = start-len(prefix), start
			}
			from = start
		}
		if begin < 0 {
			continue
		}
		replacement := calc.QuoteName(occurrence) + "::" + calc.QuoteName(nameless)
		text = text[:begin] + replacement + text[end:]
		cursor = begin + len(replacement)
	}
	return text
}

// addExternalFileReference makes sure the bundle carries the file reference
// an external occurrence goes through, with the data source's paths.
func addExternalFileReference(bundle *schema.SolutionBundle, name string, paths []string) {
	for i := range bundle.FileReferences {
		if strings.EqualFold(bundle.FileReferences[i].Name, name) {
			if len(bundle.FileReferences[i].Paths) == 0 {
				bundle.FileReferences[i].Paths = paths
			}
			return
		}
	}
	bundle.FileReferences = append(bundle.FileReferences, schema.BundleFileReference{Name: name, Paths: paths})
}

// externalReportFields lists, for the report, the fields used through each
// external occurrence of the converted bundle, as they will be bound.
func externalReportFields(bundle *schema.SolutionBundle, docFields map[string][]string) map[string][]string {
	names := map[string]string{} // occurrence id -> lower name
	for _, o := range bundle.TableOccurrences {
		if o.External != nil {
			names[o.ID] = strings.ToLower(o.Name)
		}
	}
	sets := map[string]map[string]bool{}
	add := func(occ, field string) {
		if occ == "" || field == "" {
			return
		}
		if sets[occ] == nil {
			sets[occ] = map[string]bool{}
		}
		sets[occ][field] = true
	}
	for occ, fields := range docFields {
		for _, f := range fields {
			add(occ, externalFieldName(f))
		}
	}
	for _, r := range bundle.Relationships {
		for _, p := range r.PredicateList() {
			if p.Operator == "×" {
				continue
			}
			if p.LeftColumnID == "" {
				add(names[r.LeftOccurrenceID], p.LeftColumnName)
			}
			if p.RightColumnID == "" {
				add(names[r.RightOccurrenceID], p.RightColumnName)
			}
		}
	}
	for _, vl := range bundle.ValueLists {
		if vl.SourceOccurrenceID != nil {
			add(names[*vl.SourceOccurrenceID], vl.SourceColumnName)
		}
	}
	out := map[string][]string{}
	for occ, set := range sets {
		list := make([]string, 0, len(set))
		for f := range set {
			list = append(list, f)
		}
		sort.Strings(list)
		out[occ] = list
	}
	return out
}
