package transfer

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// Synthetic tables only: a FileMaker id per field gives its position.
func table(name string, fields ...string) schema.TableMetadata {
	t := schema.TableMetadata{Name: name, Columns: []schema.ColumnMetadata{
		{Name: "id", FieldType: dbal.FieldTypeText, IsPrimaryKey: true},
	}}
	for i := 0; i+2 < len(fields); i += 3 {
		t.Columns = append(t.Columns, schema.ColumnMetadata{
			Name: fields[i], FieldType: dbal.AgnosticFieldType(fields[i+1]), SourceFieldID: fields[i+2],
		})
	}
	return t
}

func importStepParams() map[string]interface{} {
	return map[string]interface{}{
		"import_source": map[string]interface{}{"kind": "filemaker", "file": "Ledger"},
		"target_table":  "orders",
		"import_method": "update",
		"add_remaining": true,
		"auto_enter":    true,
		"field_mappings": []interface{}{
			map[string]interface{}{"action": "match", "target_field": "code", "target_field_id": "1", "source_position": float64(2)},
			map[string]interface{}{"action": "import", "target_field": "total", "target_field_id": "2", "source_position": float64(3)},
		},
		"auto_enter_fields":    []interface{}{"stamp"},
		"auto_enter_field_ids": []interface{}{"9"},
	}
}

func TestPositionTableOrdersByFileMakerID(t *testing.T) {
	pt := PositionTable(table("ledger", "b", "TEXT", "12", "a", "NUMBER", "3", "local", "TEXT", ""))
	if len(pt.Fields) != 2 || pt.Fields[0].Name != "a" || pt.Fields[1].Name != "b" {
		t.Fatalf("fields = %#v", pt.Fields)
	}
}

func TestInferSource(t *testing.T) {
	step, ok := ParseImportStep(importStepParams())
	if !ok || len(step.Maps) != 2 || step.MaxPosition() != 3 || len(step.AutoEnterFields) != 1 {
		t.Fatalf("step = %#v", step)
	}
	target := NewTargetColumns(table("orders",
		"code", "TEXT", "1", "total", "NUMBER", "2", "stamp", "TEXT", "9"))

	entries := PositionTable(table("entries", "x", "NUMBER", "1", "code", "TEXT", "2", "total", "NUMBER", "3"))
	people := PositionTable(table("people", "name", "TEXT", "1", "city", "TEXT", "2", "age", "NUMBER", "3"))
	short := PositionTable(table("short", "x", "NUMBER", "1", "y", "TEXT", "2"))

	t.Run("names break a tie between tables that fit by type", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{people, entries, short}, "")
		if inf.Table != "entries" || inf.Confidence != ConfidenceHigh {
			t.Fatalf("inference = %#v", inf)
		}
		if len(inf.Candidates) != 3 || inf.Candidates[0].Table != "entries" || inf.Candidates[2].Fits {
			t.Fatalf("candidates = %#v", inf.Candidates)
		}
		// Another table fits by type alone, and the reason says so.
		if !strings.Contains(inf.Reason, "1 other table(s) fit by type alone") {
			t.Fatalf("reason = %q", inf.Reason)
		}
		// The pairing the person confirms: each position and its fields.
		want := []PositionPair{
			{Position: 2, SourceField: "code", TargetField: "code", Match: true},
			{Position: 3, SourceField: "total", TargetField: "total"},
		}
		if got := inf.Candidates[0].Pairs; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("pairs = %#v", got)
		}
		if got := inf.Candidates[2].Pairs; len(got) != 2 || !got[1].Missing {
			t.Fatalf("a position past the last field is not marked missing: %#v", got)
		}
	})
	t.Run("a field deleted since the migration keeps its position", func(t *testing.T) {
		// Field 2 was deleted in File4Base: without its place, position 3
		// would shift onto nothing and position 2 onto total.
		cut := table("entries", "x", "NUMBER", "1", "total", "NUMBER", "3")
		shifted := PositionTable(cut)
		if len(shifted.Fields) != 2 || shifted.Fields[1].Name != "total" {
			t.Fatalf("fields = %#v", shifted.Fields)
		}
		kept := PositionTable(cut, "2", "3", "junk")
		if len(kept.Fields) != 3 || !kept.Fields[1].Deleted || kept.Fields[2].Name != "total" {
			t.Fatalf("fields = %#v", kept.Fields)
		}
		inf := InferSource(step, target, []PositionedTable{kept}, "")
		c := inf.Candidates[0]
		if inf.Table != "" || c.Fits || c.DeletedPositions != 1 || !c.Pairs[0].Deleted ||
			c.Pairs[1].SourceField != "total" || !strings.Contains(inf.Reason, "deleted from it") {
			t.Fatalf("inference = %#v", inf)
		}
		if _, err := PlanTransfer(step, "n", "", kept, target); err == nil {
			t.Fatal("a step reading a deleted field was planned")
		}
		// A table not migrated from FileMaker gets no positions from tombstones.
		if pt := PositionTable(table("local", "a", "TEXT", ""), "1"); len(pt.Fields) != 0 {
			t.Fatalf("fields = %#v", pt.Fields)
		}
	})
	t.Run("the only table that fits by type, unconfirmed by names", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{people, short}, "")
		if inf.Table != "people" || inf.Confidence != ConfidenceMedium {
			t.Fatalf("inference = %#v", inf)
		}
	})
	t.Run("several tables fit and names say nothing: none is picked", func(t *testing.T) {
		other := people
		other.Name = "people_copy"
		inf := InferSource(step, target, []PositionedTable{people, other}, "")
		if inf.Table != "" || !strings.Contains(inf.Reason, "2 tables") {
			t.Fatalf("inference = %#v", inf)
		}
	})
	t.Run("the target table is not its own source", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{entries}, "entries")
		if inf.Table != "" || len(inf.Candidates) != 0 {
			t.Fatalf("inference = %#v", inf)
		}
	})

	t.Run("the planned transfer", func(t *testing.T) {
		in, err := PlanTransfer(step, "n", "ledger_db", entries, target)
		if err != nil {
			t.Fatal(err)
		}
		if in.Mode != schema.TransferModeUpdateMatching || in.OnUnmatched != schema.TransferUnmatchedAdd ||
			len(in.MatchFields) != 1 || in.MatchFields[0] != "code" || in.Source.Database != "ledger_db" ||
			in.Mappings[0].SourceField != "code" || in.Mappings[1].SourceField != "total" ||
			!in.PerformAutoEnter || len(in.AutoEnterFields) != 1 || in.AutoEnterFields[0] != "stamp" ||
			in.OnRejected != schema.TransferRejectedSkip {
			t.Fatalf("input = %#v", in)
		}
		if _, err := PlanTransfer(step, "n", "", short, target); err == nil {
			t.Fatal("a table without the positions the step reads was accepted")
		}
	})
	t.Run("replace matches on nothing, add has no match either", func(t *testing.T) {
		p := importStepParams()
		p["import_method"] = "replace"
		s, _ := ParseImportStep(p)
		in, err := PlanTransfer(s, "n", "", entries, target)
		if err != nil || in.Mode != schema.TransferModeReplaceFoundSet || len(in.MatchFields) != 0 {
			t.Fatalf("input = %#v, %v", in, err)
		}
	})
	t.Run("a renamed target field is found by its FileMaker id", func(t *testing.T) {
		renamed := NewTargetColumns(table("orders",
			"code_2", "TEXT", "1", "total", "NUMBER", "2", "stamp", "TEXT", "9"))
		in, err := PlanTransfer(step, "n", "", entries, renamed)
		if err != nil || in.Mappings[0].DestinationField != "code_2" || in.MatchFields[0] != "code_2" {
			t.Fatalf("input = %#v, %v", in, err)
		}
	})
}

func TestImportStepProblems(t *testing.T) {
	target := NewTargetColumns(table("orders", "code", "TEXT", "1", "total", "NUMBER", "2", "stamp", "TEXT", "9"))
	entries := PositionTable(table("entries", "x", "NUMBER", "1", "code", "TEXT", "2", "total", "NUMBER", "3"))

	t.Run("a match field saved without its position is not dropped from the key", func(t *testing.T) {
		p := importStepParams()
		maps := p["field_mappings"].([]interface{})
		p["field_mappings"] = append(maps, map[string]interface{}{"action": "match", "target_field": "stamp"})
		s, _ := ParseImportStep(p)
		if s.Unresolved != 1 || !strings.Contains(s.Problem(), "without their source position") {
			t.Fatalf("step = %#v", s)
		}
		if _, err := PlanTransfer(s, "n", "", entries, target); err == nil {
			t.Fatal("a step matching on fewer keys than FileMaker was planned")
		}
	})
	t.Run("splitting repetitions into records is not a transfer", func(t *testing.T) {
		p := importStepParams()
		p["split_repetitions"] = true
		s, _ := ParseImportStep(p)
		if _, err := PlanTransfer(s, "n", "", entries, target); err == nil {
			t.Fatal("a step splitting repetitions was planned")
		}
	})
	t.Run("a computed path is read", func(t *testing.T) {
		p := importStepParams()
		p["import_source"].(map[string]interface{})["path_is_calculation"] = true
		if s, _ := ParseImportStep(p); !s.PathIsCalculation || s.Problem() != "" {
			t.Fatalf("step = %#v", s)
		}
	})
}

func TestTransferNameFitsTheLimit(t *testing.T) {
	name := TransferName(strings.Repeat("s", 200), 4, "db", "src", "dst")
	if len([]rune(name)) > 128 {
		t.Fatalf("name is %d characters", len([]rune(name)))
	}
}

// FileMaker imports a value into a field of another type without converting
// it; only container data and other fields do not mix (#191). A table that
// fits only so is inferred with low confidence, the differing pairs named,
// and never when another one does.
func TestInferSourceUnderFileMakersTypeRule(t *testing.T) {
	step, _ := ParseImportStep(importStepParams())
	target := NewTargetColumns(table("orders",
		"code", "TEXT", "1", "total", "NUMBER", "2", "stamp", "TEXT", "9"))
	entries := PositionTable(table("entries", "x", "NUMBER", "1", "code", "TEXT", "2", "total", "NUMBER", "3"))
	loose := PositionTable(table("loose", "x", "TEXT", "1", "ref", "NUMBER", "2", "total", "NUMBER", "3"))
	pictures := PositionTable(table("pictures", "x", "TEXT", "1", "code", "TEXT", "2", "scan", "CONTAINER", "3"))
	short := PositionTable(table("short", "x", "NUMBER", "1", "y", "TEXT", "2"))

	t.Run("the only table FileMaker could import from is low, its differing pairs named", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{short, pictures, loose}, "")
		if inf.Table != "loose" || inf.Confidence != ConfidenceLow {
			t.Fatalf("inference = %#v", inf)
		}
		if !strings.Contains(inf.Reason, "2 ref (NUMBER) → code (TEXT)") || !strings.Contains(inf.Reason, "1 value(s)") {
			t.Fatalf("reason = %q", inf.Reason)
		}
		c := inf.Candidates[0]
		if c.Table != "loose" || c.Fits || !c.Possible || c.Pairs[0].SourceType != dbal.FieldTypeNumber ||
			c.Pairs[0].TargetType != dbal.FieldTypeText || c.Pairs[1].SourceType != "" {
			t.Fatalf("candidate = %#v", c)
		}
		for _, other := range inf.Candidates[1:] {
			if other.Possible {
				t.Fatalf("%s is possible: %#v", other.Table, other)
			}
		}
		if p := inf.Candidates[1].Pairs; inf.Candidates[1].Table == "pictures" && !p[1].Incompatible {
			t.Fatalf("container data into a number is not marked: %#v", p)
		}
	})
	t.Run("a table that fits by type wins over one FileMaker could import from", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{loose, entries}, "")
		if inf.Table != "entries" || inf.Confidence != ConfidenceHigh {
			t.Fatalf("inference = %#v", inf)
		}
	})
	t.Run("two such tables are left to a person", func(t *testing.T) {
		twin := loose
		twin.Name = "loose_copy"
		inf := InferSource(step, target, []PositionedTable{loose, twin}, "")
		if inf.Table != "" || inf.Confidence != "" || !strings.Contains(inf.Reason, "2 tables FileMaker could import from") {
			t.Fatalf("inference = %#v", inf)
		}
	})
	t.Run("container data goes into no other field", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{pictures}, "")
		if inf.Table != "" || inf.Candidates[0].Possible {
			t.Fatalf("inference = %#v", inf)
		}
	})
	t.Run("the pair as a person reads it", func(t *testing.T) {
		p := PositionPair{Position: 2, SourceField: "amount", TargetField: "total",
			SourceType: dbal.FieldTypeText, TargetType: dbal.FieldTypeNumber}
		if got := p.Describe(); got != "2 amount (TEXT) → total (NUMBER)" {
			t.Fatalf("describe = %q", got)
		}
	})
}

// A step matching names pairs its fields by name, whatever their case, at
// each run (#191).
func TestInferSourceByMatchingNames(t *testing.T) {
	p := importStepParams()
	p["match_field_names"] = true
	maps := p["field_mappings"].([]interface{})
	delete(maps[1].(map[string]interface{}), "source_position") // positions say nothing here
	step, _ := ParseImportStep(p)
	if !step.MatchByName || step.Problem() != "" || len(step.Maps) != 2 {
		t.Fatalf("step = %#v (%s)", step, step.Problem())
	}
	target := NewTargetColumns(table("orders",
		"code", "TEXT", "1", "total", "NUMBER", "2", "stamp", "TEXT", "9"))
	named := PositionTable(table("named", "Total", "NUMBER", "1", "x", "TEXT", "2", "CODE", "TEXT", "3"))
	texts := PositionTable(table("texts", "code", "TEXT", "1", "total", "TEXT", "2"))
	partial := PositionTable(table("partial", "code", "TEXT", "1", "x", "NUMBER", "2", "y", "NUMBER", "3"))

	t.Run("the only table naming every target field", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{partial, named}, "")
		if inf.Table != "named" || inf.Confidence != ConfidenceHigh || !inf.ByName {
			t.Fatalf("inference = %#v", inf)
		}
		pairs := inf.Candidates[0].Pairs
		if pairs[0].SourceField != "CODE" || pairs[0].Position != 0 || pairs[1].SourceField != "Total" {
			t.Fatalf("pairs = %#v", pairs)
		}
		in, err := PlanTransfer(step, "n", "", named, target)
		if err != nil || !in.MatchByName || in.Mappings[0].SourceField != "CODE" || in.Mappings[1].SourceField != "Total" ||
			in.MatchFields[0] != "code" {
			t.Fatalf("input = %#v, %v", in, err)
		}
	})
	t.Run("a pair of another type is low", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{texts, partial}, "")
		if inf.Table != "texts" || inf.Confidence != ConfidenceLow ||
			!strings.Contains(inf.Reason, "total (TEXT) → total (NUMBER)") {
			t.Fatalf("inference = %#v", inf)
		}
	})
	t.Run("a table naming only some is left to a person, and pairs again at each run", func(t *testing.T) {
		inf := InferSource(step, target, []PositionedTable{partial}, "")
		if inf.Table != "" || !strings.Contains(inf.Reason, "partial names 1 of them") {
			t.Fatalf("inference = %#v", inf)
		}
		in, err := PlanTransfer(step, "n", "", partial, target)
		if err != nil || !in.MatchByName || in.Mappings[1].SourceField != "total" {
			t.Fatalf("input = %#v, %v", in, err)
		}
	})
	t.Run("two tables naming every field are left to a person", func(t *testing.T) {
		twin := named
		twin.Name = "named_copy"
		if inf := InferSource(step, target, []PositionedTable{named, twin}, ""); inf.Table != "" {
			t.Fatalf("inference = %#v", inf)
		}
	})
}

// A stored calculation is matched on and never written, as FileMaker
// matches on one "for matching only" (#191).
func TestPlanTransferMatchesOnAStoredCalculation(t *testing.T) {
	step, _ := ParseImportStep(importStepParams())
	entries := PositionTable(table("entries", "x", "NUMBER", "1", "code", "TEXT", "2", "total", "NUMBER", "3"))
	orders := func(formula string) TargetColumns {
		t := table("orders", "total", "NUMBER", "2", "stamp", "TEXT", "9")
		t.Columns = append(t.Columns, schema.ColumnMetadata{
			Name: "code", FieldType: dbal.FieldTypeCalculation, SourceFieldID: "1", CalculationFormula: &formula,
		})
		return NewTargetColumns(t)
	}
	stored := orders(`{"formula":"Upper ( x )","result_type":"Text"}`)
	if inf := InferSource(step, stored, []PositionedTable{entries}, ""); inf.Table != "entries" {
		t.Fatalf("the calculation's result type was not weighed: %#v", inf)
	}
	if in, err := PlanTransfer(step, "n", "", entries, stored); err != nil || in.MatchFields[0] != "code" {
		t.Fatalf("input = %#v, %v", in, err)
	}
	unstored := orders(`{"formula":"Upper ( x )","result_type":"Text","stored":false}`)
	if _, err := PlanTransfer(step, "n", "", entries, unstored); err == nil ||
		!strings.Contains(err.Error(), "unstored calculation") {
		t.Fatalf("an unstored match field was planned: %v", err)
	}
	p := importStepParams()
	p["import_method"] = "add"
	add, _ := ParseImportStep(p)
	if _, err := PlanTransfer(add, "n", "", entries, stored); err == nil ||
		!strings.Contains(err.Error(), "calculation field") {
		t.Fatalf("an import into a calculation was planned: %v", err)
	}
}

func TestImportStepWhoseTargetWasDeleted(t *testing.T) {
	p := importStepParams()
	p["field_mappings"] = []interface{}{}
	p["deleted_target_fields"] = float64(3)
	s, _ := ParseImportStep(p)
	if !strings.Contains(s.Problem(), "the 3 field(s) the step imports into or matches on were deleted from the "+
		"target table in FileMaker") {
		t.Fatalf("problem = %q", s.Problem())
	}
}

// Names pair whatever their case, FileMaker field names being unique within a
// table regardless of it; two source names a case apart are not guessed at.
func TestPairByName(t *testing.T) {
	def := &schema.TransferDefinition{Name: "n", Mappings: []schema.TransferMapping{
		{SourceField: "old", DestinationField: "total"},
		{SourceField: "x", DestinationField: "note"},
	}}
	paired, unpaired, err := pairByName(def, map[string]dbal.AgnosticFieldType{
		"Total": dbal.FieldTypeNumber, "code": dbal.FieldTypeText, "Total[]": "REPETITIONS:2",
	})
	if err != nil || len(paired.Mappings) != 1 || paired.Mappings[0].SourceField != "Total" ||
		len(unpaired) != 1 || unpaired[0] != "note" || def.Mappings[0].SourceField != "old" {
		t.Fatalf("paired = %#v, unpaired = %v, %v", paired, unpaired, err)
	}
	if _, _, err := pairByName(def, map[string]dbal.AgnosticFieldType{
		"Total": dbal.FieldTypeNumber, "TOTAL": dbal.FieldTypeNumber,
	}); err == nil {
		t.Fatal("two source fields a case apart were paired")
	}
	if _, _, err := pairByName(def, map[string]dbal.AgnosticFieldType{"code": dbal.FieldTypeText}); err == nil {
		t.Fatal("a run writing nothing was not refused")
	}
}
