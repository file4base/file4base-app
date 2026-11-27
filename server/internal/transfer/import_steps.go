package transfer

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// Import Records steps migrated from FileMaker that import from another
// FileMaker file (#191).
//
// FileMaker saves such a step with the source fields by position only —
// "Source field 3 import to Total" — and without the source table. A source
// position is the field's place in the source table's field list in creation
// order, every field counted: calculations, summaries and globals too. A
// table migrated from FileMaker keeps each field's FileMaker id, which
// FileMaker hands out in creation order, so the field at a position is found
// again by sorting the table's fields by that id. Which table the step read
// is inferred, when one table alone fits, and always confirmed by a person
// before a transfer is created.

// ErrImportStep is returned when a step cannot be turned into a transfer.
var ErrImportStep = errors.New("import step")

// Confidence of an inferred source table.
const (
	// ConfidenceHigh: the only table whose fields fit every imported
	// position by type, and whose field names agree with the target's on at
	// least half of them.
	ConfidenceHigh = "high"
	// ConfidenceMedium: the only table whose fields fit every imported
	// position by type; the names do not confirm it.
	ConfidenceMedium = "medium"
)

// ImportStep is what a migrated Import Records step says about the import
// it ran.
type ImportStep struct {
	// SourceFile is the FileMaker file it reads, "" when the path is
	// computed as the script runs.
	SourceFile  string
	TargetTable string
	// Method is FileMaker's: "add", "update" (matching records) or
	// "replace" (the found set, in order).
	Method       string
	AddRemaining bool
	Maps         []ImportStepMap
	// AutoEnter is the step-wide option; AutoEnterFields, when not nil, is
	// the per-field choice FileMaker 2023 and later make, and wins.
	AutoEnter       bool
	AutoEnterFields []ImportStepField

	// Unresolved counts the fields the step imports or matches on that
	// were saved without a source position or a target field: a transfer
	// without them would import less, or match on fewer keys, than
	// FileMaker does.
	Unresolved int
	// SplitRepetitions is FileMaker's "Split repeating field values into
	// separate records", which a transfer does not do.
	SplitRepetitions bool
	// PathIsCalculation: the file is chosen as the script runs, which a
	// transfer, bound to one solution, cannot follow.
	PathIsCalculation bool
}

// Problem says why the step cannot be given a transfer as it was saved,
// or "" when it can.
func (s ImportStep) Problem() string {
	switch {
	case s.Unresolved > 0:
		return fmt.Sprintf("%d field(s) the step imports or matches on were saved without their source position",
			s.Unresolved)
	case len(s.Maps) == 0:
		return "the step imports no field"
	case strings.TrimSpace(s.TargetTable) == "":
		return "the step names no target table"
	case s.Method == "update" && !s.hasMatch():
		return "the step updates matching records and matches on no field"
	case s.SplitRepetitions:
		return "the step splits repeating field values into separate records, which a transfer does not do"
	}
	return ""
}

func (s ImportStep) hasMatch() bool {
	for _, m := range s.Maps {
		if m.Match {
			return true
		}
	}
	return false
}

// ImportStepMap is one target field the step writes or matches on.
type ImportStepMap struct {
	// Position is the source field's 1-based place in the source table.
	Position int
	Target   ImportStepField
	Match    bool
	// SourceName is the source field's name, when FileMaker saved one.
	SourceName string
}

// ImportStepField names a field of the target table: its name as the
// migration wrote it, and its FileMaker id, which finds it where the
// migration had to rename it.
type ImportStepField struct {
	Name string
	ID   string
}

// ParseImportStep reads the parameters a migrated Import Records step was
// saved with. ok is false for a step that does not import from a FileMaker
// file.
func ParseImportStep(params map[string]interface{}) (step ImportStep, ok bool) {
	source, _ := params["import_source"].(map[string]interface{})
	if kind, _ := source["kind"].(string); kind != "filemaker" {
		return step, false
	}
	step.SourceFile, _ = source["file"].(string)
	step.TargetTable, _ = params["target_table"].(string)
	step.Method, _ = params["import_method"].(string)
	step.AddRemaining, _ = params["add_remaining"].(bool)
	step.AutoEnter, _ = params["auto_enter"].(bool)
	step.SplitRepetitions, _ = params["split_repetitions"].(bool)
	step.PathIsCalculation, _ = source["path_is_calculation"].(bool)
	for _, raw := range listOf(params["field_mappings"]) {
		m, _ := raw.(map[string]interface{})
		if m == nil {
			continue
		}
		action, _ := m["action"].(string)
		if action != "import" && action != "match" {
			continue
		}
		pos := intOf(m["source_position"])
		name, _ := m["target_field"].(string)
		if pos <= 0 || strings.TrimSpace(name) == "" {
			step.Unresolved++
			continue
		}
		id, _ := m["target_field_id"].(string)
		src, _ := m["source_field"].(string)
		step.Maps = append(step.Maps, ImportStepMap{
			Position: pos, Target: ImportStepField{Name: name, ID: id},
			Match: action == "match", SourceName: src,
		})
	}
	if names, given := params["auto_enter_fields"]; given {
		ids := listOf(params["auto_enter_field_ids"])
		step.AutoEnterFields = []ImportStepField{}
		for i, raw := range listOf(names) {
			name, _ := raw.(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			f := ImportStepField{Name: name}
			if i < len(ids) {
				f.ID, _ = ids[i].(string)
			}
			step.AutoEnterFields = append(step.AutoEnterFields, f)
		}
	}
	return step, true
}

func listOf(v interface{}) []interface{} {
	switch list := v.(type) {
	case []interface{}:
		return list
	case []map[string]interface{}:
		out := make([]interface{}, len(list))
		for i, m := range list {
			out[i] = m
		}
		return out
	case []string:
		out := make([]interface{}, len(list))
		for i, s := range list {
			out[i] = s
		}
		return out
	}
	return nil
}

func intOf(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		if n == float64(int(n)) {
			return int(n)
		}
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

// MaxPosition is the highest source position the step reads.
func (s ImportStep) MaxPosition() int {
	highest := 0
	for _, m := range s.Maps {
		if m.Position > highest {
			highest = m.Position
		}
	}
	return highest
}

// PositionedField is a field of a table migrated from FileMaker, at its place
// in FileMaker's field order.
type PositionedField struct {
	Name string                 `json:"name"`
	Type dbal.AgnosticFieldType `json:"type"`
	// Deleted is a field deleted since the migration, which keeps its
	// place so the positions after it do not shift; it has no name or type.
	Deleted bool `json:"deleted,omitempty"`
}

// PositionedTable is a table's fields in FileMaker's creation order. Fields
// that did not come from FileMaker have no place in it and are left out.
type PositionedTable struct {
	Name   string            `json:"name"`
	Fields []PositionedField `json:"fields"`
}

// PositionTable orders a table's fields by the FileMaker id the migration
// kept (#107). A table with no such field was not migrated from FileMaker
// and has no positions: it comes back with no fields. deleted are the
// FileMaker ids of its fields deleted since, which keep their places.
func PositionTable(t schema.TableMetadata, deleted ...string) PositionedTable {
	type idField struct {
		id    int
		field PositionedField
	}
	var withID []idField
	for _, c := range t.Columns {
		id, err := strconv.Atoi(strings.TrimSpace(c.SourceFieldID))
		if err != nil || id <= 0 || c.IsPrimaryKey {
			continue
		}
		withID = append(withID, idField{id, PositionedField{
			Name: c.Name, Type: schema.StorageType(c.FieldType, c.CalculationFormula),
		}})
	}
	live := make(map[int]bool, len(withID))
	for _, f := range withID {
		live[f.id] = true
	}
	for _, raw := range deleted {
		id, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || id <= 0 || live[id] || len(withID) == 0 {
			continue
		}
		live[id] = true
		withID = append(withID, idField{id, PositionedField{Deleted: true}})
	}
	sort.SliceStable(withID, func(i, j int) bool { return withID[i].id < withID[j].id })
	out := PositionedTable{Name: t.Name, Fields: make([]PositionedField, 0, len(withID))}
	for _, f := range withID {
		out.Fields = append(out.Fields, f.field)
	}
	return out
}

// TargetColumns resolves the target fields of a step against the target
// table: by FileMaker id first, which survives a renamed field, then by name.
type TargetColumns struct {
	byID   map[string][]string
	byName map[string]string
	types  map[string]dbal.AgnosticFieldType
}

// NewTargetColumns reads the target table's fields.
func NewTargetColumns(t schema.TableMetadata) TargetColumns {
	tc := TargetColumns{byID: map[string][]string{}, byName: map[string]string{},
		types: map[string]dbal.AgnosticFieldType{}}
	for _, c := range t.Columns {
		if c.IsPrimaryKey {
			continue
		}
		if id := strings.TrimSpace(c.SourceFieldID); id != "" {
			tc.byID[id] = append(tc.byID[id], c.Name)
		}
		tc.byName[strings.ToLower(c.Name)] = c.Name
		tc.types[c.Name] = schema.StorageType(c.FieldType, c.CalculationFormula)
	}
	return tc
}

// Resolve answers the column a step's field is, or "" when the table has none.
func (tc TargetColumns) Resolve(f ImportStepField) string {
	if names := tc.byID[strings.TrimSpace(f.ID)]; len(names) == 1 {
		return names[0]
	}
	return tc.byName[strings.ToLower(strings.TrimSpace(f.Name))]
}

// Type is the type of a resolved column.
func (tc TargetColumns) Type(column string) dbal.AgnosticFieldType { return tc.types[column] }

// SourceCandidate is a source table weighed for a step.
type SourceCandidate struct {
	Table string `json:"table"`
	// Fields is how many positioned fields the table has; it has to reach
	// the step's highest position to be a candidate at all.
	Fields int `json:"fields"`
	// TypesMatched is how many imported positions hold a field of the
	// target field's type, out of Positions.
	TypesMatched int `json:"types_matched"`
	// NamesMatched is how many imported positions hold a field named as the
	// target field (or as the source field FileMaker named).
	NamesMatched int  `json:"names_matched"`
	Positions    int  `json:"positions"`
	Fits         bool `json:"fits"`
	// DeletedPositions is how many positions the step reads hold a field
	// deleted since the migration.
	DeletedPositions int `json:"deleted_positions,omitempty"`
	// Pairs is what each position the step reads would be with this table:
	// the source field, and the target field it goes into.
	Pairs []PositionPair `json:"pairs"`
}

// PositionPair is one source position resolved against a source table.
type PositionPair struct {
	Position    int    `json:"position"`
	SourceField string `json:"source_field,omitempty"`
	TargetField string `json:"target_field"`
	Match       bool   `json:"match,omitempty"`
	// Deleted: the field at this position was deleted since the migration.
	Deleted bool `json:"deleted,omitempty"`
	// Missing: the table has no field at this position.
	Missing bool `json:"missing,omitempty"`
}

// Inference is what the source tables of a solution say about a step.
type Inference struct {
	// Table is the inferred source table, "" when none or several fit.
	Table      string `json:"table,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Reason     string `json:"reason"`
	// Candidates are every table of the solution weighed, best first, for
	// a person to pick from.
	Candidates []SourceCandidate `json:"candidates"`
	// TablesHidden is how many tables of the source solution the account
	// reading it may not export from, and so were not weighed: a table
	// "the only one that fits" may not be.
	TablesHidden int `json:"tables_hidden,omitempty"`
}

// InferSource weighs the tables of a source solution for a step and infers
// the one it read, when exactly one fits: its fields reach every position the
// step reads and the field at each of them has the type of the target field
// it is imported into or matched on. A tie between several fitting tables is
// broken only by field names agreeing on at least half the positions, and
// only when one table alone does. exclude is a table that cannot be the
// source, the target itself when the source is the same solution.
func InferSource(step ImportStep, target TargetColumns, tables []PositionedTable, exclude string) Inference {
	inf := Inference{Candidates: []SourceCandidate{}}
	if len(step.Maps) == 0 {
		inf.Reason = "the step imports no field"
		return inf
	}
	maxPos := step.MaxPosition()
	for _, t := range tables {
		if exclude != "" && t.Name == exclude {
			continue
		}
		c := SourceCandidate{Table: t.Name, Fields: len(t.Fields), Positions: len(step.Maps), Pairs: []PositionPair{}}
		for _, m := range step.Maps {
			column := target.Resolve(m.Target)
			pair := PositionPair{Position: m.Position, TargetField: column, Match: m.Match}
			if pair.TargetField == "" {
				pair.TargetField = m.Target.Name
			}
			if m.Position > len(t.Fields) {
				pair.Missing = true
				c.Pairs = append(c.Pairs, pair)
				continue
			}
			src := t.Fields[m.Position-1]
			pair.SourceField, pair.Deleted = src.Name, src.Deleted
			c.Pairs = append(c.Pairs, pair)
			if src.Deleted {
				c.DeletedPositions++
				continue
			}
			if column != "" && sameKind(src.Type, target.Type(column)) {
				c.TypesMatched++
			}
			if sameName(src.Name, column) || sameName(src.Name, m.Target.Name) ||
				(m.SourceName != "" && sameName(src.Name, m.SourceName)) {
				c.NamesMatched++
			}
		}
		c.Fits = len(t.Fields) >= maxPos && c.TypesMatched == c.Positions
		inf.Candidates = append(inf.Candidates, c)
	}
	sort.SliceStable(inf.Candidates, func(i, j int) bool {
		a, b := inf.Candidates[i], inf.Candidates[j]
		if a.Fits != b.Fits {
			return a.Fits
		}
		if a.TypesMatched != b.TypesMatched {
			return a.TypesMatched > b.TypesMatched
		}
		if a.NamesMatched != b.NamesMatched {
			return a.NamesMatched > b.NamesMatched
		}
		return a.Table < b.Table
	})

	var fitting, named []SourceCandidate
	for _, c := range inf.Candidates {
		if !c.Fits {
			continue
		}
		fitting = append(fitting, c)
		if 2*c.NamesMatched >= c.Positions {
			named = append(named, c)
		}
	}
	switch {
	case len(fitting) == 0:
		inf.Reason = fmt.Sprintf("no table of the solution has a field of the right type at each of the %d "+
			"position(s) the step reads (up to position %d); pick the source table", len(step.Maps), maxPos)
		for _, c := range inf.Candidates {
			if c.DeletedPositions > 0 && c.TypesMatched+c.DeletedPositions == c.Positions {
				inf.Reason += fmt.Sprintf("; %s would fit, but %d of the fields the step reads were deleted from it "+
					"since the migration", c.Table, c.DeletedPositions)
				break
			}
		}
	case len(named) == 1:
		inf.Table, inf.Confidence = named[0].Table, ConfidenceHigh
		inf.Reason = fmt.Sprintf("the only table whose fields fit every position by type and agree by name on %d of %d",
			named[0].NamesMatched, named[0].Positions)
		if others := len(fitting) - 1; others > 0 {
			inf.Reason += fmt.Sprintf("; %d other table(s) fit by type alone", others)
		}
	case len(fitting) == 1:
		inf.Table, inf.Confidence = fitting[0].Table, ConfidenceMedium
		inf.Reason = "the only table whose fields fit every position by type; the field names do not confirm it"
	default:
		inf.Reason = fmt.Sprintf("%d tables fit every position by type; pick the source table", len(fitting))
	}
	return inf
}

// sameKind compares a source field's type with a target field's, both as
// they are stored: a FileMaker time is stored as a timestamp, so a time and
// a timestamp compare equal, as the field types FileMaker imports between.
func sameKind(a, b dbal.AgnosticFieldType) bool {
	return a != "" && a == b
}

func sameName(a, b string) bool {
	na, nb := squash(a), squash(b)
	return na != "" && na == nb
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PlanTransfer builds the transfer that runs a step: from table of the source
// solution sourceDatabase ("" for the solution holding the step) into the
// step's target table, mapping each position to the field at that place,
// matching only on the fields FileMaker matched on, with FileMaker's method
// and auto-enter choice.
//
// A source record a destination field cannot hold is passed over, the way
// FileMaker passes over a record it cannot import, and the run reports it.
func PlanTransfer(
	step ImportStep, name, sourceDatabase string, source PositionedTable, target TargetColumns,
) (schema.TransferInput, error) {
	in := schema.TransferInput{
		Name:        name,
		Source:      schema.TransferEnd{Database: sourceDatabase, Table: source.Name},
		Destination: schema.TransferEnd{Table: step.TargetTable},
		OnRejected:  schema.TransferRejectedSkip,
		DateOrder:   "iso",
	}
	if problem := step.Problem(); problem != "" {
		return in, fmt.Errorf("%w: %s", ErrImportStep, problem)
	}
	for _, m := range step.Maps {
		if m.Position > len(source.Fields) {
			return in, fmt.Errorf("%w: the step reads source field %d and %s has %d field(s) from FileMaker",
				ErrImportStep, m.Position, source.Name, len(source.Fields))
		}
		if source.Fields[m.Position-1].Deleted {
			return in, fmt.Errorf("%w: the step reads source field %d, which was deleted from %s since the migration",
				ErrImportStep, m.Position, source.Name)
		}
		column := target.Resolve(m.Target)
		if column == "" {
			return in, fmt.Errorf("%w: %s has no field %q", ErrImportStep, step.TargetTable, m.Target.Name)
		}
		in.Mappings = append(in.Mappings, schema.TransferMapping{
			SourceField: source.Fields[m.Position-1].Name, DestinationField: column,
		})
		if m.Match {
			in.MatchFields = append(in.MatchFields, column)
		}
	}
	switch step.Method {
	case "update":
		in.Mode = schema.TransferModeUpdateMatching
		if len(in.MatchFields) == 0 {
			return in, fmt.Errorf("%w: the step updates matching records and matches on no field", ErrImportStep)
		}
	case "replace":
		in.Mode = schema.TransferModeReplaceFoundSet
		in.MatchFields = nil
	default:
		in.Mode = schema.TransferModeAdd
		in.MatchFields = nil
	}
	in.OnUnmatched = schema.TransferUnmatchedSkip
	if step.AddRemaining {
		in.OnUnmatched = schema.TransferUnmatchedAdd
	}
	if step.AutoEnterFields != nil {
		for _, f := range step.AutoEnterFields {
			if column := target.Resolve(f); column != "" {
				in.AutoEnterFields = append(in.AutoEnterFields, column)
			}
		}
		in.PerformAutoEnter = len(in.AutoEnterFields) > 0
	} else {
		in.PerformAutoEnter = step.AutoEnter
	}
	return in, nil
}

// TransferName is the name a step's transfer is created with: the step's
// place and the two tables, within the limit a name has.
func TransferName(scriptName string, stepIndex int, sourceDatabase, sourceTable, targetTable string) string {
	from := sourceTable
	if sourceDatabase != "" {
		from = sourceDatabase + "." + sourceTable
	}
	name := fmt.Sprintf("Import %s from %s (%s, step %d)", targetTable, from, scriptName, stepIndex)
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	return strings.TrimSpace(name)
}
