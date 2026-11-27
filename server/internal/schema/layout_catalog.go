package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The layout and script menus are kept in FileMaker's order, with their
// folders and separators (see migrations/012_layout_catalog.sql): the Data
// API lists them as FileMaker Server does.
//
// A layout or a script is placed by its folder_id and position; a folder or
// a separator is a row of sys_catalog_items, which a layout menu separator
// also gives an occurrence, as FileMaker does. Positions are the order of
// the whole menu, folders' contents included, so a folder's items are in
// position order. A layout or a script with no position (one created here)
// comes after those that have one, by name.

// Menu catalogs.
const (
	CatalogLayouts = "layout"
	CatalogScripts = "script"
)

// Kinds of menu items.
const (
	MenuLayout    = "layout"
	MenuScript    = "script"
	MenuFolder    = "folder"
	MenuSeparator = "separator"
)

// layoutCatalogColumns are added to an older catalog (see
// ensureExternalOccurrenceColumns).
var layoutCatalogColumns = []struct{ table, column, typ string }{
	{"sys_columns", "source_type", "VARCHAR(32)"},
	{"sys_layouts", "folder_id", "VARCHAR(36)"},
	{"sys_layouts", "position", "INT"},
	{"sys_scripts", "position", "INT"},
}

// catalogItemsSQL creates the menus' folders and separators.
func catalogItemsSQL(ts string) string {
	return `CREATE TABLE IF NOT EXISTS sys_catalog_items (
			id VARCHAR(36) PRIMARY KEY,
			catalog VARCHAR(16) NOT NULL,
			kind VARCHAR(16) NOT NULL,
			name VARCHAR(128) NOT NULL,
			folder_id VARCHAR(36),
			position INT,
			table_occurrence_id VARCHAR(36),
			created_at ` + ts + `
		);`
}

// CatalogItem is an item of the layout or the script menu.
type CatalogItem struct {
	// Kind is MenuLayout or MenuScript for a layout or a script, whose ID it
	// is; MenuFolder or MenuSeparator otherwise.
	Kind     string
	ID       string
	Name     string
	FolderID string
	// TableOccurrenceID is a layout's, or a layout menu separator's.
	TableOccurrenceID string
	position          sql.NullInt64
	catalog           string
}

// CatalogNode is a menu item with, for a folder, the items in it.
type CatalogNode struct {
	CatalogItem
	Items []*CatalogNode
}

// Menu reads the layout or the script menu: its items in order, each in its
// folder. An item whose folder is not one of the menu's is at the top.
func (s *Service) Menu(ctx context.Context, catalog string) ([]*CatalogNode, error) {
	items, err := s.menuItems(ctx, catalog)
	if err != nil {
		return nil, err
	}
	return menuTree(items), nil
}

// menuItems reads a menu's items in order.
func (s *Service) menuItems(ctx context.Context, catalog string) ([]CatalogItem, error) {
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}
	var q, kind string
	switch catalog {
	case CatalogLayouts:
		q, kind = `SELECT id, name, COALESCE(folder_id, ''), table_occurrence_id, position, '' FROM sys_layouts`, MenuLayout
	case CatalogScripts:
		q, kind = `SELECT id, name, COALESCE(folder_id, ''), '', position, '' FROM sys_scripts`, MenuScript
	default:
		return nil, fmt.Errorf("unknown menu %q", catalog)
	}
	var items []CatalogItem
	read := func(q string, arg ...interface{}) error {
		rows, err := s.driver.DB().QueryContext(ctx, q, arg...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			it := CatalogItem{catalog: catalog}
			var k string
			if err := rows.Scan(&it.ID, &it.Name, &it.FolderID, &it.TableOccurrenceID, &it.position, &k); err != nil {
				return err
			}
			if it.Kind = kind; k != "" {
				it.Kind = k
			}
			items = append(items, it)
		}
		return rows.Err()
	}
	if err := read(q); err != nil {
		return nil, err
	}
	if err := read(`SELECT id, name, COALESCE(folder_id, ''), COALESCE(table_occurrence_id, ''), position, kind
		FROM sys_catalog_items WHERE catalog = `+s.driver.Dialect().Placeholder(1), catalog); err != nil {
		return nil, err
	}
	sortMenu(items)
	return items, nil
}

// sortMenu puts a menu's items in order: by position, then those without
// one by name.
func sortMenu(items []CatalogItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.position.Valid != b.position.Valid {
			return a.position.Valid
		}
		if a.position.Valid && a.position.Int64 != b.position.Int64 {
			return a.position.Int64 < b.position.Int64
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
}

// menuTree nests a menu's items in their folders, keeping their order. A
// folder that would end up inside itself is taken out to the top.
func menuTree(items []CatalogItem) []*CatalogNode {
	nodes := make([]*CatalogNode, len(items))
	folders := map[string]*CatalogNode{}
	parent := map[string]string{}
	for i, it := range items {
		nodes[i] = &CatalogNode{CatalogItem: it}
		if it.Kind == MenuFolder {
			folders[it.ID] = nodes[i]
			parent[it.ID] = it.FolderID
		}
	}
	// cyclic reports whether folder id is, through its parents, inside
	// itself.
	cyclic := func(id string) bool {
		seen := map[string]bool{}
		for f := parent[id]; f != ""; f = parent[f] {
			if f == id || seen[f] {
				return true
			}
			seen[f] = true
		}
		return false
	}
	var top []*CatalogNode
	for _, n := range nodes {
		f := folders[n.FolderID]
		if f == nil || f == n || (n.Kind == MenuFolder && cyclic(n.ID)) {
			top = append(top, n)
			continue
		}
		f.Items = append(f.Items, n)
	}
	return top
}

// menuEntries is the menus as a solution file carries them. A separator
// whose occurrence was deleted since is written without it, and an item in
// no folder of its menu at the top.
func (s *Service) menuEntries(ctx context.Context) ([]BundleCatalogEntry, error) {
	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(occurrences))
	for _, o := range occurrences {
		known[o.ID] = true
	}
	var out []BundleCatalogEntry
	for _, catalog := range []string{CatalogLayouts, CatalogScripts} {
		items, err := s.menuItems(ctx, catalog)
		if err != nil {
			return nil, err
		}
		folders := map[string]bool{}
		for _, it := range items {
			if it.Kind == MenuFolder {
				folders[it.ID] = true
			}
		}
		for _, it := range items {
			if !folders[it.FolderID] {
				it.FolderID = ""
			}
			if !it.position.Valid && it.FolderID == "" && it.Kind != MenuFolder && it.Kind != MenuSeparator {
				continue // listed by name, after the others, here as there
			}
			e := BundleCatalogEntry{
				Catalog: catalog, Kind: it.Kind, ID: it.ID, Name: menuEntryName(it),
				FolderID: it.FolderID, TableOccurrenceID: menuEntryOccurrence(it),
			}
			if !known[e.TableOccurrenceID] {
				e.TableOccurrenceID = ""
			}
			out = append(out, e)
		}
	}
	return out, nil
}

// A layout's or a script's name and occurrence are its own, not the
// placement's.
func menuEntryName(it CatalogItem) string {
	if it.Kind == MenuFolder || it.Kind == MenuSeparator {
		return it.Name
	}
	return ""
}

func menuEntryOccurrence(it CatalogItem) string {
	if it.Kind == MenuSeparator {
		return it.TableOccurrenceID
	}
	return ""
}

// BundleCatalogEntry is an item of the layout or the script menu of a
// solution file: a layout or a script, by its id in the file, placed there;
// or a folder or a separator. The entries of a menu are in its order, and an
// entry's folder is a folder entry of the same menu.
type BundleCatalogEntry struct {
	Catalog  string `msgpack:"catalog"`
	Kind     string `msgpack:"kind"`
	ID       string `msgpack:"id"`
	Name     string `msgpack:"name,omitempty"`
	FolderID string `msgpack:"folder_id,omitempty"`
	// TableOccurrenceID is a layout menu separator's occurrence.
	TableOccurrenceID string `msgpack:"table_occurrence_id,omitempty"`
}

// checkCatalogEntries validates a solution file's menus against its layouts
// and scripts.
func checkCatalogEntries(entries []BundleCatalogEntry, layouts, scripts, occurrences map[string]bool) error {
	folders := map[string]bool{} // catalog/id: an id names a folder of its own menu
	seen := map[string]bool{}
	for _, e := range entries {
		key := e.Catalog + "/" + e.ID
		if e.ID == "" || seen[key] {
			return fmt.Errorf("menu entry %q is missing or repeated", e.ID)
		}
		seen[key] = true
		switch {
		case e.Catalog == CatalogLayouts && e.Kind == MenuLayout && layouts[e.ID]:
		case e.Catalog == CatalogScripts && e.Kind == MenuScript && scripts[e.ID]:
		case (e.Catalog == CatalogLayouts || e.Catalog == CatalogScripts) && (e.Kind == MenuFolder || e.Kind == MenuSeparator):
			if strings.TrimSpace(e.Name) == "" || utf8.RuneCountInString(e.Name) > 128 {
				return fmt.Errorf("menu %s %q has no usable name", e.Kind, e.ID)
			}
			if e.TableOccurrenceID != "" && (e.Kind != MenuSeparator || e.Catalog != CatalogLayouts || !occurrences[e.TableOccurrenceID]) {
				return fmt.Errorf("menu %s %q names an unknown occurrence", e.Kind, e.ID)
			}
			if e.Kind == MenuFolder {
				folders[key] = true
			}
		default:
			return fmt.Errorf("menu entry %q (%s %s) is not one of the file's", e.ID, e.Catalog, e.Kind)
		}
	}
	for _, e := range entries {
		if e.FolderID != "" && !folders[e.Catalog+"/"+e.FolderID] {
			return fmt.Errorf("menu entry %q is in an unknown folder", e.ID)
		}
	}
	return nil
}

// menuState is what a solution import changes of the menus, so that it can
// be put back.
type menuState struct {
	catalogs   []string      // the menus it replaces
	items      []CatalogItem // folders and separators
	placements []CatalogItem // layouts and scripts
}

func (s *Service) readMenuState(ctx context.Context) (menuState, error) {
	st := menuState{catalogs: []string{CatalogLayouts, CatalogScripts}}
	for _, catalog := range st.catalogs {
		items, err := s.menuItems(ctx, catalog)
		if err != nil {
			return st, err
		}
		for _, it := range items {
			if it.Kind == MenuFolder || it.Kind == MenuSeparator {
				st.items = append(st.items, it)
			} else {
				st.placements = append(st.placements, it)
			}
		}
	}
	return st, nil
}

// writeMenuState replaces the folders and separators of the menus it names
// and places the layouts and scripts it names, in one transaction; the
// other layouts and scripts of those menus are listed after them, by name.
func (s *Service) writeMenuState(ctx context.Context, st menuState) error {
	d := s.driver.Dialect()
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, catalog := range st.catalogs {
		table := "sys_layouts"
		if catalog == CatalogScripts {
			table = "sys_scripts"
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sys_catalog_items WHERE catalog = `+d.Placeholder(1), catalog); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET folder_id = NULL, position = NULL`); err != nil {
			return err
		}
	}
	insert := fmt.Sprintf(`INSERT INTO sys_catalog_items (id, catalog, kind, name, folder_id, position, table_occurrence_id, created_at)
		VALUES (%s, %s, %s, %s, %s, %s, %s, %s)`, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
		d.Placeholder(5), d.Placeholder(6), d.Placeholder(7), d.Placeholder(8))
	now := time.Now().UTC()
	for _, it := range st.items {
		if _, err := tx.ExecContext(ctx, insert, it.ID, it.catalog, it.Kind, it.Name, nullable(it.FolderID),
			it.position, nullable(it.TableOccurrenceID), now); err != nil {
			return fmt.Errorf("failed writing the menu: %w", err)
		}
	}
	for _, it := range st.placements {
		table := "sys_layouts"
		if it.Kind == MenuScript {
			table = "sys_scripts"
		}
		q := fmt.Sprintf(`UPDATE %s SET folder_id = %s, position = %s WHERE id = %s`, table,
			d.Placeholder(1), d.Placeholder(2), d.Placeholder(3))
		if _, err := tx.ExecContext(ctx, q, nullable(it.FolderID), it.position, it.ID); err != nil {
			return fmt.Errorf("failed placing %s %q in the menu: %w", it.Kind, it.Name, err)
		}
	}
	return tx.Commit()
}

// importMenus places a solution file's layouts and scripts in their menus,
// with the file's folders and separators, which replace this database's.
// A menu the file does not carry is left as it is: a file written before
// the menus were kept leaves both. It returns the state to put back if the
// import is rolled back.
func (s *Service) importMenus(ctx context.Context, entries []BundleCatalogEntry, layoutIDs, scriptIDs, occIDs map[string]string) (*menuState, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	previous, err := s.readMenuState(ctx)
	if err != nil {
		return nil, err
	}
	var st menuState
	folderIDs := map[string]string{}
	for _, e := range entries {
		if !slices.Contains(st.catalogs, e.Catalog) {
			st.catalogs = append(st.catalogs, e.Catalog)
		}
		if e.Kind == MenuFolder {
			folderIDs[e.Catalog+"/"+e.ID] = uuid.NewString()
		}
	}
	for i, e := range entries {
		it := CatalogItem{Kind: e.Kind, Name: e.Name, position: sql.NullInt64{Int64: int64(i + 1), Valid: true}, catalog: e.Catalog}
		if e.FolderID != "" {
			it.FolderID = folderIDs[e.Catalog+"/"+e.FolderID]
		}
		switch e.Kind {
		case MenuLayout:
			if it.ID = layoutIDs[e.ID]; it.ID == "" {
				continue
			}
			st.placements = append(st.placements, it)
		case MenuScript:
			if it.ID = scriptIDs[e.ID]; it.ID == "" {
				continue
			}
			st.placements = append(st.placements, it)
		case MenuFolder:
			it.ID = folderIDs[e.Catalog+"/"+e.ID]
			st.items = append(st.items, it)
		case MenuSeparator:
			it.ID = uuid.NewString()
			it.TableOccurrenceID = occIDs[e.TableOccurrenceID]
			st.items = append(st.items, it)
		}
	}
	if err := s.writeMenuState(ctx, st); err != nil {
		return nil, err
	}
	return &previous, nil
}

// ErrLayoutNotFound is returned for a layout name no layout has.
var ErrLayoutNotFound = errors.New("layout not found")

// LayoutByName finds the layout the Data API names: the first in the
// layout menu's order whose name matches, in any case, as FileMaker Server
// finds it. A folder is not a layout.
func (s *Service) LayoutByName(ctx context.Context, name string) (CatalogItem, error) {
	items, err := s.menuItems(ctx, CatalogLayouts)
	if err != nil {
		return CatalogItem{}, err
	}
	for _, it := range items {
		if it.Kind == MenuLayout && strings.EqualFold(it.Name, name) {
			return it, nil
		}
	}
	return CatalogItem{}, ErrLayoutNotFound
}
