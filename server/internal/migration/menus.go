package migration

import (
	"strings"

	"github.com/google/uuid"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// menuBuilder follows a FileMaker layout or script catalog, which is flat: a
// folder is an item with isFolder="True" that the next item with
// isFolder="Marker" ends (a catalog FileMaker wrote without end markers
// leaves it open to the end), and a separator is an item with
// isSeparatorItem="True". It gives the menu as a solution file carries it,
// with the migrated layouts and scripts in FileMaker's order.
type menuBuilder struct {
	catalog string
	kind    string
	open    []string
	entries []schema.BundleCatalogEntry
}

func newMenuBuilder(catalog, kind string) *menuBuilder {
	return &menuBuilder{catalog: catalog, kind: kind}
}

func (m *menuBuilder) folder() string {
	if len(m.open) == 0 {
		return ""
	}
	return m.open[len(m.open)-1]
}

// add places node, the next item of the catalog: id is the layout or the
// script it was migrated to, empty if it was not; occurrenceID is the
// occurrence of a layout menu separator.
func (m *menuBuilder) add(node *xmlNode, id, occurrenceID string) {
	switch {
	case nativeFolderEnd(node):
		if len(m.open) > 0 {
			m.open = m.open[:len(m.open)-1]
		}
	case nativeFolderMarker(node):
		name := strings.TrimSpace(firstAttr(node, "name", "Name"))
		if name == "" {
			name = "-"
		}
		folderID := uuid.NewString()
		m.entries = append(m.entries, schema.BundleCatalogEntry{
			Catalog: m.catalog, Kind: schema.MenuFolder, ID: folderID, Name: name, FolderID: m.folder(),
		})
		m.open = append(m.open, folderID)
	case nativeSeparatorMarker(node):
		e := schema.BundleCatalogEntry{
			Catalog: m.catalog, Kind: schema.MenuSeparator, ID: uuid.NewString(), Name: "-", FolderID: m.folder(),
		}
		if m.catalog == schema.CatalogLayouts {
			e.TableOccurrenceID = occurrenceID
		}
		m.entries = append(m.entries, e)
	case id != "":
		m.entries = append(m.entries, schema.BundleCatalogEntry{
			Catalog: m.catalog, Kind: m.kind, ID: id, FolderID: m.folder(),
		})
	}
}

// nativeFolderEnd reports the item that ends a folder of a layout or a
// script catalog.
func nativeFolderEnd(node *xmlNode) bool {
	return strings.EqualFold(firstAttr(node, "isFolder"), "Marker")
}
