package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// External table occurrences.
//
// A FileMaker relationship graph can hold occurrences of a table of another
// file: the occurrence names an External Data Source, and the records are that
// file's. In File4Base the other file is another solution, the data source is
// a file reference (#107), and the occurrence keeps the reference and the name
// of the table in that solution instead of a local base table:
//
//   - base_table_id is NULL;
//   - file_reference_id names the file reference;
//   - external_table names the table in the other solution, by name, because
//     ids differ between databases and change when a bundle is restored. It is
//     empty while the table is not known: FileMaker's Save a Copy as XML does
//     not name it, so it is resolved when the reference is bound.
//
// A relationship side, a value list or a calculation that goes through an
// external occurrence names the field, since it has no local id.
//
// Binding maps the file reference to an installed solution and resolves each
// occurrence's table there; verifying checks the table and every field used
// through the occurrence exist there with compatible types. Neither reads any
// record. Reading records through an external occurrence is refused with
// data.ErrExternalUnavailable until cross-solution reads exist.

// externalOccurrenceColumns are the columns a catalog created before
// external occurrences existed lacks, by table. server/migrations/005 and 008
// add the same for PostgreSQL.
var externalOccurrenceColumns = []struct{ table, column, typ string }{
	{"sys_table_occurrences", "file_reference_id", "VARCHAR(36)"},
	{"sys_table_occurrences", "external_table", "VARCHAR(64)"},
	{"sys_file_references", "paths", "TEXT"},
	{"sys_value_lists", "source_column_name", "VARCHAR(64)"},
	{"sys_value_lists", "second_column_name", "VARCHAR(64)"},
	{"sys_columns", "source_field_id", "VARCHAR(32)"},
}

// externalNullableColumns were NOT NULL before an occurrence or a
// relationship side could be external.
var externalNullableColumns = map[string]bool{
	"sys_table_occurrences.base_table_id":            true,
	"sys_relationships.left_column_id":               true,
	"sys_relationships.right_column_id":              true,
	RelationshipPredicatesTable + ".left_column_id":  true,
	RelationshipPredicatesTable + ".right_column_id": true,
}

// upgradedCatalogs are the databases (by connection pool) this process has
// brought to the external occurrence shape, so the check runs once each.
var upgradedCatalogs sync.Map

// ensureExternalCatalog brings the catalog to the external occurrence shape
// once per database and process. Sign-in upgrades the catalog with the rest
// of the system tables; this covers a database a request reaches before
// anyone signed in to it since the server was upgraded, such as the far end
// of a transfer or a server tool.
func (s *Service) ensureExternalCatalog(ctx context.Context) error {
	db := s.driver.DB()
	if _, done := upgradedCatalogs.Load(db); done {
		return nil
	}
	if err := s.ensureExternalOccurrenceColumns(ctx); err != nil {
		return err
	}
	upgradedCatalogs.Store(db, true)
	return nil
}

// ensureExternalOccurrenceColumns brings an existing catalog to the external
// occurrence shape, and gives its relationships the options of their left
// side (relationshipSideColumns). It is idempotent and reads before it writes: a column is
// added only when missing, and a NOT NULL constraint is dropped only while
// it is there, so a catalog already upgraded is only read. A system table
// that does not exist yet is left alone: it is created in the new shape.
func (s *Service) ensureExternalOccurrenceColumns(ctx context.Context) error {
	db := s.driver.DB()
	mariadb := s.driver.Dialect().Engine() == dbal.EngineMariaDB
	q := `SELECT table_name, column_name, is_nullable FROM information_schema.columns
		WHERE table_name IN ('sys_table_occurrences', 'sys_file_references', 'sys_value_lists', 'sys_columns', 'sys_relationships', 'sys_layouts', 'sys_scripts', 'sys_catalog_items', '` +
		RelationshipPredicatesTable + `') AND table_schema = `
	if mariadb {
		q += `DATABASE()`
	} else {
		q += `current_schema()`
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed reading the catalog columns: %w", err)
	}
	tables := map[string]bool{}
	columns := map[string]bool{}
	var required []string // table.column still NOT NULL
	for rows.Next() {
		var table, column, nullable string
		if err := rows.Scan(&table, &column, &nullable); err != nil {
			rows.Close()
			return err
		}
		key := strings.ToLower(table) + "." + strings.ToLower(column)
		tables[strings.ToLower(table)], columns[key] = true, true
		if strings.EqualFold(nullable, "NO") && externalNullableColumns[key] {
			required = append(required, key)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, c := range slices.Concat(externalOccurrenceColumns, relationshipSideColumns, layoutCatalogColumns) {
		if !tables[c.table] || columns[c.table+"."+c.column] {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s`, c.table, c.column, c.typ)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed adding %s.%s: %w", c.table, c.column, err)
		}
	}
	if tables["sys_layouts"] && !tables["sys_catalog_items"] {
		if _, err := db.ExecContext(ctx, catalogItemsSQL(s.driver.Dialect().TimestampColumn())); err != nil {
			return fmt.Errorf("failed creating sys_catalog_items: %w", err)
		}
	}
	for _, key := range required {
		table, column, _ := strings.Cut(key, ".")
		stmt := fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL`, table, column)
		if mariadb {
			stmt = fmt.Sprintf(`ALTER TABLE %s MODIFY %s VARCHAR(36) NULL`, table, column)
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed making %s optional: %w", key, err)
		}
	}
	return nil
}

// ValidSourceFieldID reports whether id can be a FileMaker field id: empty,
// or up to 32 digits.
func ValidSourceFieldID(id string) bool {
	if len(id) > 32 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// A field of another solution's table that the source document names only
// by its FileMaker id — FileMaker writes an empty name when the other file
// could not be opened as it saved the design — is kept as the marker
// "#<id>" wherever a name of such a field goes: a relationship side, a
// predicate, a value list, a formula or a layout. A marker is never a field
// name (a name cannot start with "#"), so it cannot be taken for one; binding
// resolves it by the source_field_id of the other solution's fields (#107).
const externalFieldMarkerPrefix = "#"

// ExternalFieldMarker is the marker of a field known only by its FileMaker id.
func ExternalFieldMarker(sourceFieldID string) string {
	return externalFieldMarkerPrefix + sourceFieldID
}

// ExternalFieldSourceID returns the FileMaker id a marker carries.
func ExternalFieldSourceID(name string) (string, bool) {
	id, ok := strings.CutPrefix(name, externalFieldMarkerPrefix)
	if !ok || id == "" || !ValidSourceFieldID(id) {
		return "", false
	}
	return id, true
}

// normalizeExternalField checks the name of a field of another solution's
// table: a marker is kept as it is, a name is normalized.
func normalizeExternalField(name string) (string, error) {
	name = strings.TrimSpace(name)
	if _, ok := ExternalFieldSourceID(name); ok {
		return name, nil
	}
	name = dbal.NormalizeIdentifier(name)
	if err := dbal.CheckIdentifier("column", name); err != nil {
		return "", err
	}
	return name, nil
}

// externalFieldKey is the stored form of a field of another solution's
// table: its marker, or its normalized name.
func externalFieldKey(name string) string {
	if n, err := normalizeExternalField(name); err == nil {
		return n
	}
	return dbal.NormalizeIdentifier(name)
}

// ExternalOccurrence is what an occurrence of another solution's table holds
// in place of a base table.
type ExternalOccurrence struct {
	FileReferenceID string `json:"file_reference_id"`
	// FileReference is the reference's name: FileMaker's data source name.
	FileReference string `json:"file_reference"`
	// Database is the solution the reference is mapped to, empty while it
	// is not.
	Database string `json:"database"`
	// Table is the table's name in that solution, empty while it is not
	// known.
	Table string `json:"table"`
}

// ExternalOccurrenceInput creates an external occurrence: the file reference
// (by id or name) and, when known, the table's name in the other solution.
type ExternalOccurrenceInput struct {
	FileReference string `json:"file_reference"`
	Table         string `json:"table,omitempty"`
}

// ErrFileReferenceInUse is returned when a file reference that table
// occurrences go through is deleted.
var ErrFileReferenceInUse = errors.New("file reference is used by table occurrences")

// ErrInvalidOccurrence is returned for a table occurrence that cannot be
// stored as given.
var ErrInvalidOccurrence = errors.New("invalid table occurrence")

// normalizeExternalTable checks the name of a table of another solution.
func normalizeExternalTable(name string) (string, error) {
	name = dbal.NormalizeIdentifier(name)
	if name == "" {
		return "", nil
	}
	if err := dbal.CheckUserTableName(name); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidOccurrence, err)
	}
	return name, nil
}

// fileReferenceByIDOrName reads a reference by id, else by name.
func (s *Service) fileReferenceByIDOrName(ctx context.Context, idOrName string) (*FileReference, error) {
	idOrName = strings.TrimSpace(idOrName)
	ref, err := s.GetFileReference(ctx, idOrName)
	if errors.Is(err, ErrFileReferenceNotFound) {
		return s.GetFileReferenceByName(ctx, idOrName)
	}
	return ref, err
}

// The status of an external occurrence, as verifying its binding reports it.
const (
	// ExternalUnbound: the file reference is not mapped, or the table in the
	// other solution is not known.
	ExternalUnbound = "unbound"
	// ExternalBound: the table and every field used through the occurrence
	// are in the other solution, with compatible types.
	ExternalBound = "bound"
	// ExternalMismatch: the table or a field is not there, or a match field
	// has a type its local counterpart cannot be matched with.
	ExternalMismatch = "mismatch"
)

// ExternalOccurrenceStatus is the verification of one external occurrence.
type ExternalOccurrenceStatus struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Table  string `json:"table"`
	Status string `json:"status"`
	// Fields are the fields used through the occurrence, by name.
	Fields []string `json:"fields"`
	// Resolved names, in the other solution's table, the fields used
	// through the occurrence that this solution knows only by their
	// FileMaker id ("#4": "code").
	Resolved map[string]string `json:"resolved,omitempty"`
	// Candidates are the tables of the other solution that hold every one
	// of those fields, when the table is not known.
	Candidates []string `json:"candidates,omitempty"`
	// Problems say what does not match.
	Problems []string `json:"problems,omitempty"`
}

// ExternalBindingReport is the verification of every occurrence that goes
// through one file reference.
type ExternalBindingReport struct {
	FileReferenceID string                     `json:"file_reference_id"`
	FileReference   string                     `json:"file_reference"`
	Database        string                     `json:"database"`
	Occurrences     []ExternalOccurrenceStatus `json:"occurrences"`
}

// externalFieldUse is one use of a field through an external occurrence.
type externalFieldUse struct {
	name  string
	where string
	// local is the type of the local field a relationship matches it
	// with, empty when there is none.
	local dbal.AgnosticFieldType
}

// externalOccurrencesOf lists the occurrences that go through a reference.
func (s *Service) externalOccurrencesOf(ctx context.Context, referenceID string) ([]TableOccurrence, error) {
	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	var out []TableOccurrence
	for _, o := range occurrences {
		if o.External != nil && o.External.FileReferenceID == referenceID {
			out = append(out, o)
		}
	}
	return out, nil
}

// externalFieldUses collects, for each external occurrence, the fields used
// through it: by the relationships that match on it, the value lists that
// read it, the layouts that show it and the calculations that read it.
func (s *Service) externalFieldUses(ctx context.Context, occurrences []TableOccurrence) (map[string][]externalFieldUse, error) {
	uses := map[string][]externalFieldUse{}
	if len(occurrences) == 0 {
		return uses, nil
	}
	byName := map[string]string{} // lower-case occurrence name -> id
	external := map[string]bool{}
	for _, o := range occurrences {
		byName[strings.ToLower(o.Name)] = o.ID
		external[o.ID] = true
	}
	add := func(occurrenceID, name, where string, local dbal.AgnosticFieldType) {
		if name = externalFieldKey(name); name != "" {
			uses[occurrenceID] = append(uses[occurrenceID], externalFieldUse{name: name, where: where, local: local})
		}
	}

	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	columnType := map[string]dbal.AgnosticFieldType{}
	for _, t := range tables {
		for _, c := range t.Columns {
			columnType[c.ID] = StorageType(c.FieldType, c.CalculationFormula)
		}
	}

	relationships, err := s.ListRelationships(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range relationships {
		where := fmt.Sprintf("relationship %q", r.Name)
		for _, p := range r.Predicates {
			if p.Operator == "×" { // a Cartesian criterion reads no field
				continue
			}
			if external[r.LeftOccurrenceID] {
				add(r.LeftOccurrenceID, p.LeftColumnName, where, columnType[p.RightColumnID])
			}
			if external[r.RightOccurrenceID] {
				add(r.RightOccurrenceID, p.RightColumnName, where, columnType[p.LeftColumnID])
			}
		}
	}

	lists, err := s.ListValueLists(ctx)
	if err != nil {
		return nil, err
	}
	for _, vl := range lists {
		if vl.SourceOccurrenceID != nil && external[*vl.SourceOccurrenceID] {
			add(*vl.SourceOccurrenceID, vl.SourceColumnName, fmt.Sprintf("value list %q", vl.Name), "")
			if vl.SecondColumnName != "" {
				add(*vl.SourceOccurrenceID, vl.SecondColumnName, fmt.Sprintf("value list %q", vl.Name), "")
			}
		}
	}

	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range layouts {
		var def interface{}
		if json.Unmarshal(l.Definition, &def) != nil {
			continue
		}
		own := ""
		if external[l.TableOccurrenceID] {
			own = l.TableOccurrenceID
		}
		where := fmt.Sprintf("layout %q", l.Name)
		walkLayoutFields(def, func(occurrence, field string) {
			switch {
			case occurrence == "" && own != "":
				add(own, field, where, "")
			case occurrence != "":
				if id := byName[strings.ToLower(occurrence)]; id != "" {
					add(id, field, where, "")
				}
			}
		})
	}

	lib, err := s.library(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		for _, c := range t.Columns {
			if c.FieldType != dbal.FieldTypeCalculation || c.CalculationFormula == nil {
				continue
			}
			expr, err := calc.ParseWith(data.ReadStoredCalculation(*c.CalculationFormula).Formula, calc.Options{Library: lib})
			if err != nil {
				continue
			}
			for _, ref := range expr.RelatedFields() {
				if id := byName[strings.ToLower(ref.Table)]; id != "" {
					add(id, ref.Field, fmt.Sprintf("calculation %s.%s", t.Name, c.Name), "")
				}
			}
		}
	}
	return uses, nil
}

// walkLayoutFields calls fn for every field object of a layout definition
// with the occurrence it names (empty for the layout's own) and its field.
func walkLayoutFields(node interface{}, fn func(occurrence, field string)) {
	switch v := node.(type) {
	case map[string]interface{}:
		if binding, ok := v["field_binding"].(map[string]interface{}); ok {
			field, _ := binding["field_name"].(string)
			if field == "" {
				field, _ = v["field_name"].(string)
			}
			occurrence, _ := binding["table_occurrence"].(string)
			if field != "" && !strings.Contains(field, "::") {
				fn(strings.TrimSpace(occurrence), field)
			}
		}
		for _, child := range v {
			walkLayoutFields(child, fn)
		}
	case []interface{}:
		for _, child := range v {
			walkLayoutFields(child, fn)
		}
	}
}

// VerifyFileReference reports, for every occurrence that goes through the
// reference, whether its table and fields are in the solution the reference
// is mapped to. peer is that solution's catalog; nil when the reference is
// not mapped, which leaves every occurrence unbound. Nothing is written.
func (s *Service) VerifyFileReference(ctx context.Context, referenceID string, peer *Service) (*ExternalBindingReport, error) {
	return s.bindFileReference(ctx, referenceID, "", peer, nil, false)
}

// BindFileReference resolves the table of every occurrence that goes through
// the reference in peer, the catalog of the solution the reference is mapped
// to, saves each table it can resolve and reports the verification.
//
// An occurrence keeps the table it already names. One that names none is
// given the table tables names for it (by occurrence name), else the one
// table of peer that holds every field used through it, preferring a table
// named like the occurrence. When none or several do, it stays unbound with
// the candidates listed: a table is never guessed (#107).
//
// database, when not empty, maps the reference to that solution (peer is its
// catalog). Everything is checked before anything is written: a table that
// tables names for an occurrence that does not go through the reference, or
// that peer does not have, fails the bind with nothing saved. The mapping and
// the tables are then saved together, in one transaction.
func (s *Service) BindFileReference(ctx context.Context, referenceID, database string, peer *Service, tables map[string]string) (*ExternalBindingReport, error) {
	if peer == nil {
		return nil, fmt.Errorf("%w: a file reference is bound to a solution", ErrInvalidFileReference)
	}
	return s.bindFileReference(ctx, referenceID, database, peer, tables, true)
}

func (s *Service) bindFileReference(ctx context.Context, referenceID, database string, peer *Service, tables map[string]string, save bool) (*ExternalBindingReport, error) {
	ref, err := s.GetFileReference(ctx, referenceID)
	if err != nil {
		return nil, err
	}
	mapped := ref.Database
	if database = strings.TrimSpace(database); save && database != "" {
		// Not saved yet: only once every check below has passed.
		ref.Database = database
	}
	occurrences, err := s.externalOccurrencesOf(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	uses, err := s.externalFieldUses(ctx, occurrences)
	if err != nil {
		return nil, err
	}
	report := &ExternalBindingReport{
		FileReferenceID: ref.ID, FileReference: ref.Name, Database: ref.Database,
		Occurrences: []ExternalOccurrenceStatus{},
	}

	// The other solution's tables, by name, with their fields' types. A
	// field migrated from FileMaker is also found by its marker, so a field
	// this solution knows only by its FileMaker id is matched by that id.
	var peerTables map[string]map[string]dbal.AgnosticFieldType
	peerBySource := map[string]map[string][]string{} // table -> marker -> field names
	var peerNames []string
	if peer != nil && ref.IsMapped() {
		list, err := peer.ListTables(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed reading the tables of %s: %w", ref.Database, err)
		}
		peerTables = map[string]map[string]dbal.AgnosticFieldType{}
		for _, t := range list {
			fields := map[string]dbal.AgnosticFieldType{}
			bySource := map[string][]string{}
			for _, c := range t.Columns {
				fields[c.Name] = StorageType(c.FieldType, c.CalculationFormula)
				if c.SourceFieldID != "" {
					// Two fields with one id are both kept: the id is then
					// ambiguous there, and never resolved to either.
					marker := ExternalFieldMarker(c.SourceFieldID)
					fields[marker] = fields[c.Name]
					bySource[marker] = append(bySource[marker], c.Name)
				}
			}
			peerTables[t.Name], peerBySource[t.Name] = fields, bySource
			peerNames = append(peerNames, t.Name)
		}
		sort.Strings(peerNames)
	}
	chosen := map[string]string{}
	if save {
		through := map[string]bool{}
		for _, o := range occurrences {
			through[strings.ToLower(o.Name)] = true
		}
		for occurrence, pick := range tables {
			key := strings.ToLower(strings.TrimSpace(occurrence))
			if !through[key] {
				return nil, fmt.Errorf("%w: no table occurrence %q goes through the file reference %q", ErrInvalidFileReference, occurrence, ref.Name)
			}
			table, err := normalizeExternalTable(pick)
			if err != nil {
				return nil, err
			}
			if _, ok := peerTables[table]; table != "" && !ok {
				return nil, fmt.Errorf("%w: %s has no table %q for %q", ErrInvalidFileReference, ref.Database, table, occurrence)
			}
			chosen[key] = table
		}
	}
	pending := map[string]string{} // occurrence id -> table to save

	for _, o := range occurrences {
		st := ExternalOccurrenceStatus{ID: o.ID, Name: o.Name, Table: o.External.Table, Fields: []string{}}
		seen := map[string]bool{}
		for _, u := range uses[o.ID] {
			if !seen[u.name] {
				seen[u.name] = true
				st.Fields = append(st.Fields, u.name)
			}
		}
		sort.Strings(st.Fields)

		if peerTables == nil {
			st.Status = ExternalUnbound
			st.Problems = []string{fmt.Sprintf("the file reference %q is not mapped to a solution", ref.Name)}
			report.Occurrences = append(report.Occurrences, st)
			continue
		}

		if save {
			table := st.Table
			if pick, ok := chosen[strings.ToLower(o.Name)]; ok {
				table = pick
			}
			if table == "" {
				table = resolveExternalTable(o.Name, st.Fields, peerTables, peerNames)
			}
			if table != o.External.Table {
				pending[o.ID] = table
				st.Table = table
			}
		}

		if st.Table == "" {
			st.Status = ExternalUnbound
			st.Candidates = externalCandidates(st.Fields, peerTables, peerNames)
			switch len(st.Candidates) {
			case 0:
				st.Problems = []string{fmt.Sprintf("no table of %s holds every field used through %q", ref.Database, o.Name)}
			default:
				st.Problems = []string{fmt.Sprintf("the table of %q in %s is not known: choose one of the candidates", o.Name, ref.Database)}
			}
			report.Occurrences = append(report.Occurrences, st)
			continue
		}

		fields, ok := peerTables[st.Table]
		if !ok {
			st.Status = ExternalMismatch
			st.Problems = []string{fmt.Sprintf("%s has no table %q", ref.Database, st.Table)}
			report.Occurrences = append(report.Occurrences, st)
			continue
		}
		reported := map[string]bool{}
		for _, u := range uses[o.ID] {
			typ, ok := fields[u.name]
			sourceID, byID := ExternalFieldSourceID(u.name)
			matches := peerBySource[st.Table][u.name]
			if ok && byID && len(matches) == 1 {
				if st.Resolved == nil {
					st.Resolved = map[string]string{}
				}
				st.Resolved[u.name] = matches[0]
			}
			var problem string
			switch {
			case ok && byID && len(matches) > 1:
				problem = fmt.Sprintf("the field of %s with FileMaker id %s, used by %s, is ambiguous in %s.%s: %s all have that id",
					o.Name, sourceID, u.where, ref.Database, st.Table, strings.Join(matches, ", "))
			case !ok && byID:
				problem = fmt.Sprintf("the field of %s with FileMaker id %s, used by %s, is not in %s.%s: none of its fields was migrated with that id",
					o.Name, sourceID, u.where, ref.Database, st.Table)
			case !ok:
				problem = fmt.Sprintf("%s::%s, used by %s, is not a field of %s.%s", o.Name, u.name, u.where, ref.Database, st.Table)
			case u.local != "" && !matchableTypes(u.local, typ):
				problem = fmt.Sprintf("%s::%s is %s in %s, but %s matches it with a %s field", o.Name, u.name, typ, ref.Database, u.where, u.local)
			}
			if problem != "" && !reported[problem] {
				reported[problem] = true
				st.Problems = append(st.Problems, problem)
			}
		}
		st.Status = ExternalBound
		if len(st.Problems) > 0 {
			st.Status = ExternalMismatch
		}
		report.Occurrences = append(report.Occurrences, st)
	}
	if save && (len(pending) > 0 || !strings.EqualFold(mapped, ref.Database)) {
		if err := s.saveBinding(ctx, ref.ID, ref.Database, !strings.EqualFold(mapped, ref.Database), pending); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// saveBinding saves what a bind resolved, all of it or nothing: the solution
// the reference is mapped to, when it changed, and each occurrence's table.
func (s *Service) saveBinding(ctx context.Context, referenceID, database string, remap bool, tables map[string]string) error {
	d := s.driver.Dialect()
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if remap {
		q := `UPDATE sys_file_references SET database_name = ` + d.Placeholder(1) + `, updated_at = ` + d.Placeholder(2) + ` WHERE id = ` + d.Placeholder(3)
		if _, err := tx.ExecContext(ctx, q, database, time.Now().UTC(), referenceID); err != nil {
			return fmt.Errorf("failed mapping the file reference: %w", err)
		}
	}
	for occurrenceID, table := range tables {
		q := `UPDATE sys_table_occurrences SET external_table = ` + d.Placeholder(1) + ` WHERE id = ` + d.Placeholder(2)
		if _, err := tx.ExecContext(ctx, q, nullable(table), occurrenceID); err != nil {
			return fmt.Errorf("failed saving the table of the occurrence: %w", err)
		}
	}
	return tx.Commit()
}

// externalCandidates are the tables that hold every one of fields, by name,
// or for a marker, by FileMaker id.
func externalCandidates(fields []string, tables map[string]map[string]dbal.AgnosticFieldType, names []string) []string {
	var out []string
	for _, name := range names {
		all := true
		for _, f := range fields {
			if _, ok := tables[name][f]; !ok {
				all = false
				break
			}
		}
		if all {
			out = append(out, name)
		}
	}
	return out
}

// resolveExternalTable is the one candidate table, or the candidate named
// like the occurrence; empty when that does not single one out.
func resolveExternalTable(occurrence string, fields []string, tables map[string]map[string]dbal.AgnosticFieldType, names []string) string {
	candidates := externalCandidates(fields, tables, names)
	own := dbal.NormalizeIdentifier(occurrence)
	for _, c := range candidates {
		if c == own {
			return c
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

// matchableTypes reports whether a relationship can match a field of one type
// with a field of the other: the same storage type, as a local relationship
// compares them in SQL. A container is never matched.
func matchableTypes(a, b dbal.AgnosticFieldType) bool {
	return a == b && a != dbal.FieldTypeContainer
}

func (s *Service) setExternalTable(ctx context.Context, occurrenceID, table string) error {
	d := s.driver.Dialect()
	q := `UPDATE sys_table_occurrences SET external_table = ` + d.Placeholder(1) + ` WHERE id = ` + d.Placeholder(2)
	if _, err := s.driver.DB().ExecContext(ctx, q, nullable(table), occurrenceID); err != nil {
		return fmt.Errorf("failed saving the table of the occurrence: %w", err)
	}
	return nil
}

// nullable is NULL for an empty string.
func nullable(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

// ExternalDependency is a solution this one reads through its external
// occurrences (#107), with the file references mapped to it.
type ExternalDependency struct {
	Database   string   `json:"database"`
	References []string `json:"references"`
}

// ExternalDependencies lists the solutions the external occurrences of this
// one are mapped to, by database name. An occurrence whose reference is not
// mapped depends on nothing yet.
func (s *Service) ExternalDependencies(ctx context.Context) ([]ExternalDependency, error) {
	occurrences, err := s.ListTableOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	var deps []ExternalDependency
	index := map[string]int{}
	for _, o := range occurrences {
		if o.External == nil || strings.TrimSpace(o.External.Database) == "" {
			continue
		}
		key := o.External.Database
		i, seen := index[key]
		if !seen {
			i = len(deps)
			index[key] = i
			deps = append(deps, ExternalDependency{Database: o.External.Database})
		}
		if !slices.Contains(deps[i].References, o.External.FileReference) {
			deps[i].References = append(deps[i].References, o.External.FileReference)
		}
	}
	slices.SortFunc(deps, func(a, b ExternalDependency) int { return strings.Compare(a.Database, b.Database) })
	return deps, nil
}
