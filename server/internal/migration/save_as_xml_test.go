package migration

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

// saveAsXMLFixture is a <FMSaveAsXML> document in the shape an authentic
// FileMaker Pro 20 / Claris 26 "Save a Copy as XML" export was reported to
// have (#87): the fields of a base table live in FieldsForTables and point
// back at the table, the steps of a script live in StepsForScripts, and the
// attribute names are lower-cased.
//
// It is handwritten, like every other fixture in this package, and the
// compatibility scope says so: it cannot stand in for an authentic export.
const saveAsXMLFixture = `<?xml version="1.0" encoding="UTF-8"?>
<FMSaveAsXML Has_DDR_INFO="True">
  <File name="Recipes.fmp12">
    <BaseTableCatalog>
      <BaseTable id="1" name="Recipes"/>
      <BaseTable id="2" name="Ingredients"/>
    </BaseTableCatalog>
    <FieldsForTables>
      <FieldCatalog>
        <BaseTableSourceReference id="1" name="Recipes"/>
        <Field id="1" name="Title" datatype="text" fieldtype="normal"/>
        <Field id="2" name="Servings" datatype="number" fieldtype="normal"/>
      </FieldCatalog>
      <FieldCatalog>
        <BaseTableSourceReference id="2" name="Ingredients"/>
        <Field id="1" name="RecipeTitle" datatype="text" fieldtype="normal"/>
        <Field id="2" name="Quantity" datatype="number" fieldtype="normal"/>
      </FieldCatalog>
    </FieldsForTables>
    <RelationshipGraph>
      <TableList>
        <Table id="1" name="Recipes" baseTable="Recipes" baseTableId="1"/>
        <Table id="2" name="Ingredients" baseTable="Ingredients" baseTableId="2"/>
      </TableList>
      <RelationshipList>
        <Relationship id="1" name="Recipes_Ingredients">
          <LeftTable name="Recipes"/>
          <RightTable name="Ingredients" cascadeCreate="True"/>
          <JoinPredicateList>
            <JoinPredicate type="Equal">
              <LeftField><Field name="Title" table="Recipes"/></LeftField>
              <RightField><Field name="RecipeTitle" table="Ingredients"/></RightField>
            </JoinPredicate>
          </JoinPredicateList>
        </Relationship>
      </RelationshipList>
    </RelationshipGraph>
    <ScriptCatalog>
      <Script id="1" name="Add Recipe" contextTable="Recipes"/>
    </ScriptCatalog>
    <StepsForScripts>
      <StepList>
        <ScriptSourceReference id="1" name="Add Recipe"/>
        <Step id="76" name="Set Field" enable="True">
          <Field name="Servings" table="Recipes"/>
          <Calculation><Text>4</Text></Calculation>
        </Step>
        <Step id="103" name="Exit Script" enable="True">
          <Calculation><Text>"added"</Text></Calculation>
        </Step>
      </StepList>
    </StepsForScripts>
  </File>
</FMSaveAsXML>`

// A Save a Copy as XML document converts with its fields, its joins and its
// script steps, which is the whole of #87: the format was refused outright,
// and renaming its root produced tables with no fields at all.
func TestAnalyzeAndConvert_ReadsSaveAsXMLNativeGrammar(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: saveAsXMLFixture})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if res.Report.SourceFormat != "filemaker_save_as_xml_native" {
		t.Fatalf("expected the native Save a Copy as XML format, got %q", res.Report.SourceFormat)
	}
	if res.Report.SolutionName != "Recipes" {
		t.Fatalf("expected the solution to be named after the file, got %q", res.Report.SolutionName)
	}

	if len(res.Bundle.Tables) != 2 {
		t.Fatalf("expected the two declared base tables, got %d", len(res.Bundle.Tables))
	}
	columns := map[string]string{}
	for _, table := range res.Bundle.Tables {
		for _, col := range table.Columns {
			columns[table.Name+"."+col.Name] = col.FieldType
		}
	}
	// The fields are in FieldsForTables, not inside <BaseTable>: reading the
	// document without relocating them gave tables holding nothing but the
	// synthetic id.
	for path, want := range map[string]string{
		"recipes.title":           "TEXT",
		"recipes.servings":        "NUMBER",
		"ingredients.recipetitle": "TEXT",
		"ingredients.quantity":    "NUMBER",
	} {
		got, ok := columns[path]
		if !ok {
			t.Fatalf("field %s was not migrated; got %v", path, sortedKeys(columns))
		}
		// A lower-cased datatype has to be read as the type it names.
		if got != want {
			t.Fatalf("field %s: expected %s, got %s", path, want, got)
		}
	}

	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("expected the one declared relationship, got %d (rejected: %+v)",
			len(res.Bundle.Relationships), res.Report.Rejected)
	}

	if len(res.Bundle.Scripts) != 1 {
		t.Fatalf("expected the one declared script, got %d", len(res.Bundle.Scripts))
	}
	steps := res.Bundle.Scripts[0].Steps
	if len(steps) != 2 {
		t.Fatalf("expected the script's two steps from StepsForScripts, got %d", len(steps))
	}
	if steps[0].StepType != "Set Field" || steps[1].StepType != "Exit Script" {
		t.Fatalf("the steps must keep their order and identity, got %q then %q",
			steps[0].StepType, steps[1].StepType)
	}
	if field, _ := steps[0].Params["field"].(string); field != "servings" {
		t.Fatalf("the step's operand must point at the imported column, got %q", field)
	}
}

// An export taken without "Include details for analysis tools" says so in the
// report, because the fix is one option away rather than a different product.
func TestAnalyzeAndConvert_SaveAsXMLWithoutAnalysisDetailSaysSo(t *testing.T) {
	doc := strings.Replace(saveAsXMLFixture, `Has_DDR_INFO="True"`, `Has_DDR_INFO="False"`, 1)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("a document that still carries its schema must convert: %v", err)
	}
	var said bool
	for _, item := range res.Report.ManualActionRequired {
		if strings.Contains(item.Detail, "Include details for analysis tools") {
			said = true
		}
	}
	if !said {
		t.Fatalf("expected the report to name the export option, got %+v", res.Report.ManualActionRequired)
	}
}

// A field catalog naming a table the document does not declare is reported,
// not dropped in silence.
func TestAnalyzeAndConvert_SaveAsXMLUnlinkedFieldCatalogIsReported(t *testing.T) {
	doc := strings.Replace(saveAsXMLFixture,
		`<BaseTableSourceReference id="2" name="Ingredients"/>`,
		`<BaseTableSourceReference id="99" name="Gone"/>`, 1)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	var reported bool
	for _, item := range res.Report.Rejected {
		if strings.Contains(item.Detail, "the document does not declare") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("expected the unlinked field catalog to be rejected by name, got %+v", res.Report.Rejected)
	}
}

// The check that makes every grammar mismatch fail loudly: a document that
// declares tables and produces no field of them is refused instead of being
// reported as a faithful conversion of nothing (#87).
func TestAnalyzeAndConvert_RefusesAConversionThatMigratedNoField(t *testing.T) {
	doc := `<FMSaveAsXML Has_DDR_INFO="True"><File name="Recipes.fmp12"><BaseTableCatalog>` +
		`<BaseTable id="1" name="Recipes"/><BaseTable id="2" name="Ingredients"/>` +
		`</BaseTableCatalog><FieldsForSomethingElse>` +
		`<FieldCatalog><Field name="Title" datatype="text"/></FieldCatalog>` +
		`</FieldsForSomethingElse></File></FMSaveAsXML>`

	_, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if !errors.Is(err, ErrInvalidMigrationXML) {
		t.Fatalf("expected the conversion to be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "not one field") {
		t.Fatalf("the refusal must say what is wrong, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "Include details for analysis tools") {
		t.Fatalf("the refusal must say what to do about it, got %q", err.Error())
	}
}

// A design that declares no tables at all is a different matter: a value-list
// or script only document still converts.
func TestAnalyzeAndConvert_EmptyGuardLeavesATablelessDesignAlone(t *testing.T) {
	doc := `<FMPReport><File name="Lists.fmp12"><ValueListCatalog>` +
		`<ValueList name="Answers"><CustomValues><Text>Yes</Text><Text>No</Text></CustomValues></ValueList>` +
		`</ValueListCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("a design with no tables must still convert: %v", err)
	}
	if len(res.Bundle.ValueLists) != 1 {
		t.Fatalf("expected the value list to survive, got %d", len(res.Bundle.ValueLists))
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
