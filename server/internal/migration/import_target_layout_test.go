package migration

import (
	"strings"
	"testing"
)

// Synthetic design: Import Records steps saved without their target
// occurrence, and one pairing its fields by matching names (#191). No source
// export is reproduced.
func importTargetDesign(steps string) string {
	importStep := func(fields string, matchNames string) string {
		return `<Step id="35" name="Import Records" enable="True"><ParameterValues>
  <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Ledger.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
  <Parameter type="ImportField"><ImportField addasnew="False"><Options matchFieldNames="` + matchNames + `" doAutoEntry="False"/><Target/>
   <Field>` + fields + `</Field><action name="Add" value="0"/></ImportField></Parameter>
 </ParameterValues></Step>`
	}
	live := `<Map name="Source field 1 import to " index="1" kind="0"><FieldReference id="1" name="Code"/></Map>` +
		`<Map name="Source field 2 import to " index="2" kind="0"><FieldReference id="2" name="Total"/></Map>`
	deleted := `<Map name="Source field 1 import to " index="1" kind="0"/>` +
		`<Map name="Source field 2 import to " index="2" kind="0"/>` +
		`<Map name="Source field 3 not import to " index="3" kind="1"/>`
	r := strings.NewReplacer("{import}", importStep(live, "False"), "{deleted}", importStep(deleted, "False"),
		"{byname}", importStep(live, "True"))
	return `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Total" datatype="Number" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog><TableOccurrence id="7" name="Open Orders"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence></TableOccurrenceCatalog>
 <LayoutCatalog><Layout id="40" name="Order List"><TableOccurrenceReference id="7" name="Open Orders"/></Layout></LayoutCatalog>
 <ScriptCatalog><Script id="81" name="Steps"/></ScriptCatalog>
 <StepsForScripts><Script><ScriptReference id="81" name="Steps"/><ObjectList>` + r.Replace(steps) + `
 </ObjectList></Script></StepsForScripts>
 </AddAction></Structure></FMSaveAsXML>`
}

const (
	goToOrderList = `<Step id="6" name="Go to Layout" enable="True"><ParameterValues><Parameter type="LayoutReferenceContainer"><LayoutReferenceContainer value="5"><LayoutReference id="40" name="Order List"/></LayoutReferenceContainer></Parameter></ParameterValues></Step>`
	goToDeleted   = `<Step id="6" name="Go to Layout" enable="True"><ParameterValues><Parameter type="LayoutReferenceContainer"><LayoutReferenceContainer value="5"/></Parameter></ParameterValues></Step>`
	stepIf        = `<Step id="68" name="If" enable="True"><ParameterValues><Parameter type="Calculation"><Calculation><Calculation><Text>$go</Text></Calculation></Calculation></Parameter></ParameterValues></Step>`
	stepEndIf     = `<Step id="70" name="End If" enable="True"/>`
	stepComment   = `<Step id="89" name="# (comment)" enable="True"/>`
)

func TestImportTargetFromTheLayoutBeforeIt(t *testing.T) {
	steps := func(t *testing.T, design string) []map[string]interface{} {
		t.Helper()
		result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: importTargetDesign(design)})
		if err != nil {
			t.Fatal(err)
		}
		var out []map[string]interface{}
		for _, st := range result.Bundle.Scripts[0].Steps {
			if st.StepType == "Import Records" {
				if _, left := st.Params["current_layout_table"]; left {
					t.Errorf("the layout the script was on was left in the step: %#v", st.Params)
				}
				out = append(out, st.Params)
			}
		}
		return out
	}

	t.Run("a Go to Layout naming its layout fixes the target", func(t *testing.T) {
		p := steps(t, goToOrderList+stepComment+"{import}")[0]
		if p["target_table"] != "orders" || p["target_table_from_layout"] != true {
			t.Fatalf("target = %v (from layout %v)", p["target_table"], p["target_table_from_layout"])
		}
		if note, _ := p["migration_note"].(string); !strings.Contains(note, "into orders") {
			t.Errorf("note = %s", note)
		}
	})

	t.Run("a branch between them leaves it unknown", func(t *testing.T) {
		for name, design := range map[string]string{
			"inside an If":      goToOrderList + stepIf + "{import}" + stepEndIf,
			"after an End If":   stepIf + goToOrderList + stepEndIf + "{import}",
			"no Go to Layout":   "{import}",
			"a deleted layout":  goToOrderList + goToDeleted + "{import}",
			"a disabled layout": strings.Replace(goToOrderList, `enable="True"`, `enable="False"`, 1) + "{import}",
		} {
			if p := steps(t, design)[0]; p["target_table"] != nil {
				t.Errorf("%s: target = %v", name, p["target_table"])
			}
		}
	})

	t.Run("fields deleted with their table are said to be", func(t *testing.T) {
		p := steps(t, goToDeleted+"{deleted}")[0]
		if p["deleted_target_fields"] != 2 {
			t.Fatalf("deleted_target_fields = %#v", p["deleted_target_fields"])
		}
		note, _ := p["migration_note"].(string)
		if !strings.Contains(note, "deleted from the target table in FileMaker") ||
			!strings.Contains(note, "imports nothing either") {
			t.Errorf("note = %s", note)
		}
	})

	t.Run("matching names is kept and said", func(t *testing.T) {
		p := steps(t, goToOrderList+"{byname}")[0]
		if p["match_field_names"] != true {
			t.Fatalf("match_field_names = %#v", p["match_field_names"])
		}
		if note, _ := p["migration_note"].(string); !strings.Contains(note, "by name at each run") {
			t.Errorf("note = %s", note)
		}
	})
}
