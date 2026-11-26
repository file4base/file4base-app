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

func TestTransferNameFitsTheLimit(t *testing.T) {
	name := TransferName(strings.Repeat("s", 200), 4, "db", "src", "dst")
	if len([]rune(name)) > 128 {
		t.Fatalf("name is %d characters", len([]rune(name)))
	}
}
