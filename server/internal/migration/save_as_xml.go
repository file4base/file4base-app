package migration

import (
	"fmt"
	"strings"
)

// Reading a <FMSaveAsXML> design document, which is what FileMaker Pro 20 /
// Claris 26 "Save a Copy as XML" writes (#87).
//
// The document carries the same objects a Database Design Report does, but it
// does not nest them the same way: a base table's fields are not inside the
// <BaseTable> element, they are in a FieldsForTables section that points back
// at the table, and a script's steps are in a StepsForScripts section that
// points back at the script. Attribute names are written in lower case
// (datatype, fieldtype) where the DDR writes them camel-cased.
//
// Renaming the root to <FMPSaveAsXML> is not a conversion path and never was:
// it made the document parse and then produced tables with no fields at all,
// occurrences the source never had and no relationships — a migration
// reported as successful that had migrated nothing. So the document is
// adapted into the shape the converter reads, explicitly, and what could not
// be linked is reported rather than left empty. A document that still ends up
// with tables and no fields at all is refused by the empty-field check in
// AnalyzeAndConvert rather than reported as a faithful conversion of nothing.
//
// IMPORTANT, and stated in the user-facing documentation too: Claris
// publishes no grammar for this format. Its own help calls the XML a preview
// that "may change significantly in future versions" and is "not intended for
// use in production systems", and from FileMaker Pro 2026 the root carries
// Has_DDR_INFO, which is True only when the file was exported with "Include
// details for analysis tools" — the option that puts the analysis detail in
// the document at all. This reader is therefore written against the structure
// an authentic export was reported to have, and the conversion is refused
// rather than reported as faithful when that structure is not found
// (see guardAgainstEmptyConversion).

// adaptSaveAsXML rewrites a parsed <FMSaveAsXML> tree into the shape the DDR
// converter reads: every field catalog is moved under the base table it
// belongs to, and every step list under the script it belongs to. It returns
// what it did, and what it could not place.
func adaptSaveAsXML(root *xmlNode) []ReportItem {
	var items []ReportItem

	if !parseBoolAttr(root, "Has_DDR_INFO", "has_ddr_info") && hasAttr(root, "Has_DDR_INFO", "has_ddr_info") {
		// The attribute is there and says False: the export was taken without
		// "Include details for analysis tools", so the analysis detail this
		// conversion needs was never written to the file. Saying so here is
		// the difference between "your file is unsupported" and "re-export it
		// with one more option ticked".
		items = append(items, ReportItem{
			Category:   CategoryManualActionRequired,
			Domain:     "file",
			SourceName: firstAttr(root, "name", "File"),
			Detail: "This Save a Copy as XML export carries Has_DDR_INFO=\"False\": it was written without " +
				"\"Include details for analysis tools\", which is the option that puts the schema detail in the " +
				"document. Export the file again with that option selected, or produce a Database Design Report instead",
		})
	}

	items = append(items, relocateFieldCatalogs(root)...)
	items = append(items, relocateScriptSteps(root)...)
	return items
}

// detach removes child from wherever it hangs under parent, so a relocated
// section is moved rather than aliased into two places in the tree.
func detach(parent, child *xmlNode) {
	var walk func(n *xmlNode) bool
	walk = func(n *xmlNode) bool {
		for i, c := range n.Children {
			if c == child {
				n.Children = append(n.Children[:i:i], n.Children[i+1:]...)
				return true
			}
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(parent)
}

func hasAttr(n *xmlNode, keys ...string) bool {
	for _, k := range keys {
		if _, ok := n.Attrs[k]; ok {
			return true
		}
	}
	return false
}

// relocateFieldCatalogs moves each <FieldCatalog> of a FieldsForTables section
// under the <BaseTable> it names, so the converter finds a table's fields
// where it looks for them.
func relocateFieldCatalogs(root *xmlNode) []ReportItem {
	holders := findNodes(root, "FieldsForTables", "FieldsForTable")
	if len(holders) == 0 {
		return nil
	}

	byName, byID := indexBaseTables(root)
	declared := findNodes(root, "BaseTable", "baseTable")
	var items []ReportItem

	for _, holder := range holders {
		for _, catalog := range findNodes(holder, "FieldCatalog", "fieldCatalog") {
			name, id := catalogOwner(catalog)
			owner := resolveOwner(byName, byID, name, id)
			if owner == nil && name == "" && id == "" && len(declared) == 1 {
				// The catalog names no table and the document declares
				// exactly one, so there is only one table its fields can
				// belong to. Where more than one is declared the link is
				// genuinely unknown and is reported instead of guessed: a
				// field placed on the wrong table is worse than a field
				// reported as unplaced.
				owner = declared[0]
				items = append(items, ReportItem{
					Category:   CategoryTranslated,
					Domain:     "table",
					SourceName: firstAttr(owner, "name", "Name"),
					TargetName: firstAttr(owner, "name", "Name"),
					Detail: "A field catalog of this Save a Copy as XML document names no base table; it was read " +
						"as belonging to the only table the document declares",
				})
			}
			if owner == nil {
				items = append(items, ReportItem{
					Category:   CategoryRejected,
					Domain:     "table",
					SourceName: ownerLabel(name, id),
					Detail: fmt.Sprintf("A field catalog of this Save a Copy as XML document names a base table (%s) "+
						"the document does not declare, so its %d field(s) could not be migrated",
						ownerLabel(name, id), len(findNodes(catalog, "Field", "field"))),
				})
				continue
			}
			// Moved, not copied: leaving it under FieldsForTables as well
			// would make the same element a child of two parents, and every
			// walk of the tree would then meet its fields twice.
			detach(holder, catalog)
			owner.Children = append(owner.Children, catalog)
		}
	}
	return items
}

// relocateScriptSteps moves each step list of a StepsForScripts section under
// the <Script> it names.
func relocateScriptSteps(root *xmlNode) []ReportItem {
	holders := findNodes(root, "StepsForScripts", "StepsForScript")
	if len(holders) == 0 {
		return nil
	}

	byName := map[string]*xmlNode{}
	byID := map[string]*xmlNode{}
	for _, script := range findCatalogNodes(root, "Script", "script") {
		if name := firstAttr(script, "name", "Name"); name != "" {
			byName[strings.ToLower(name)] = script
		}
		if id := firstAttr(script, "id", "ID"); id != "" {
			byID[id] = script
		}
	}

	var items []ReportItem
	for _, holder := range holders {
		for _, list := range findNodes(holder, "StepList", "Steps", "steps") {
			name, id := catalogOwner(list)
			owner := resolveOwner(byName, byID, name, id)
			if owner == nil {
				items = append(items, ReportItem{
					Category:   CategoryRejected,
					Domain:     "script",
					SourceName: ownerLabel(name, id),
					Detail: fmt.Sprintf("A step list of this Save a Copy as XML document names a script (%s) the "+
						"document does not declare, so its %d step(s) could not be migrated",
						ownerLabel(name, id), len(findNodes(list, "Step", "step"))),
				})
				continue
			}
			detach(holder, list)
			owner.Children = append(owner.Children, list)
		}
	}
	return items
}

// indexBaseTables keys every declared base table by its name and by its id.
func indexBaseTables(root *xmlNode) (byName, byID map[string]*xmlNode) {
	byName, byID = map[string]*xmlNode{}, map[string]*xmlNode{}
	for _, bt := range findNodes(root, "BaseTable", "baseTable") {
		if name := firstAttr(bt, "name", "Name"); name != "" {
			byName[strings.ToLower(name)] = bt
		}
		if id := firstAttr(bt, "id", "ID"); id != "" {
			byID[id] = bt
		}
	}
	return byName, byID
}

// catalogOwner reads the object a relocated section belongs to. The document
// names it either on the section itself or in a nested reference element,
// such as <BaseTableSourceReference>.
func catalogOwner(section *xmlNode) (name, id string) {
	name = firstAttr(section, "baseTable", "table", "script", "name", "Name")
	id = firstAttr(section, "baseTableId", "tableId", "scriptId", "id", "ID")

	for _, ref := range section.Children {
		if !strings.Contains(strings.ToLower(ref.Name), "reference") {
			continue
		}
		if name == "" {
			name = firstAttr(ref, "name", "Name", "baseTable", "table", "script")
		}
		if id == "" {
			id = firstAttr(ref, "id", "ID", "baseTableId", "tableId", "scriptId")
		}
	}
	return name, id
}

// resolveOwner finds the object a section belongs to, by name first because a
// name is what the rest of the document joins on, then by id.
func resolveOwner(byName, byID map[string]*xmlNode, name, id string) *xmlNode {
	if name != "" {
		if owner, ok := byName[strings.ToLower(name)]; ok {
			return owner
		}
	}
	if id != "" {
		if owner, ok := byID[id]; ok {
			return owner
		}
	}
	return nil
}

func ownerLabel(name, id string) string {
	switch {
	case name != "" && id != "":
		return fmt.Sprintf("%s, id %s", name, id)
	case name != "":
		return name
	case id != "":
		return "id " + id
	default:
		return "unnamed"
	}
}
