package migration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// A calculation is read with the names and the locale it was written with,
// and bound to the columns the import created.
func TestCalculationsBindToTheImportedFields(t *testing.T) {
	design := func(fields string) string {
		return `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
			`<BaseTable name="Orders" id="1"><FieldCatalog>` +
			`<Field name="Prix HT" dataType="Number"/>` +
			`<Field name="Amount" dataType="Number"/>` + fields +
			`</FieldCatalog></BaseTable>` +
			`<BaseTable name="Order Lines" id="2"><FieldCatalog>` +
			`<Field name="Line Total" dataType="Number"/>` +
			`</FieldCatalog></BaseTable></BaseTableCatalog>` +
			`<TableOccurrenceCatalog>` +
			`<TableOccurrence name="Orders" baseTable="Orders"/>` +
			`<TableOccurrence name="Order Lines" baseTable="Order Lines"/>` +
			`</TableOccurrenceCatalog></File></FMPReport>`
	}
	calcField := func(formula, resultType string) string {
		return `<Field name="Result" fieldType="Calculation" dataType="` + resultType + `"><Calculation><![CDATA[` +
			formula + `]]></Calculation></Field>`
	}
	convert := func(t *testing.T, formula, locale string) (*ConvertedSolution, *schema.BundleColumn) {
		t.Helper()
		res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design(calcField(formula, "Number")), SourceLocale: locale})
		if err != nil {
			t.Fatalf("conversion failed: %v", err)
		}
		for i := range res.Bundle.Tables {
			for j := range res.Bundle.Tables[i].Columns {
				if col := &res.Bundle.Tables[i].Columns[j]; col.DisplayName == "Result" {
					return res, col
				}
			}
		}
		t.Fatal("no Result column")
		return nil, nil
	}
	evaluate := func(t *testing.T, col *schema.BundleColumn, record calc.Record) string {
		t.Helper()
		if col.FieldType != "CALCULATION" || col.CalculationFormula == nil {
			t.Fatalf("the calculation must be kept; got a %s column", col.FieldType)
		}
		var payload struct{ Formula string }
		if err := json.Unmarshal([]byte(*col.CalculationFormula), &payload); err != nil {
			t.Fatal(err)
		}
		expr, err := calc.Parse(payload.Formula)
		if err != nil {
			t.Fatalf("the stored formula %q must read as File4Base formulas do: %v", payload.Formula, err)
		}
		v, err := calc.EvaluateValue(expr, &calc.RecordContext{Record: record})
		if err != nil {
			t.Fatalf("%q: %v", payload.Formula, err)
		}
		return v.Text(nil)
	}
	reportSays := func(t *testing.T, res *ConvertedSolution, col *schema.BundleColumn, words ...string) {
		t.Helper()
		if col.FieldType != "NUMBER" || col.CalculationFormula != nil {
			t.Fatalf("the calculation must be quarantined as a stored NUMBER column; got %s", col.FieldType)
		}
		var details []string
		for _, item := range res.Report.Unsupported {
			details = append(details, item.Detail)
		}
		all := strings.Join(details, "\n")
		for _, w := range words {
			if !strings.Contains(all, w) {
				t.Fatalf("the report must say %q; it says:\n%s", w, all)
			}
		}
		if len(res.Report.ManualActionRequired) == 0 {
			t.Fatal("a quarantined calculation must ask for an action")
		}
	}

	t.Run("a field name with a space", func(t *testing.T) {
		_, col := convert(t, `Prix HT * 2`, "")
		if got := evaluate(t, col, calc.Record{"prix_ht": 10}); got != "20" {
			t.Fatalf("got %q, want 20", got)
		}
	})

	t.Run("a decimal comma under the source locale", func(t *testing.T) {
		_, col := convert(t, `Amount * 1,5`, "fr-FR")
		if got := evaluate(t, col, calc.Record{"amount": 10}); got != "15" {
			t.Fatalf("got %q, want 15", got)
		}
	})

	t.Run("a decimal comma without a locale asks for one", func(t *testing.T) {
		res, col := convert(t, `Amount * 1,5`, "")
		reportSays(t, res, col, "source_locale")
	})

	t.Run("Get functions and FileMaker operators are kept as written", func(t *testing.T) {
		res, col := convert(t, `If ( Amount ≥ 1 and Amount ≠ 2 ; Year ( Get ( CurrentDate ) ) ; 0 )`, "")
		if got := evaluate(t, col, calc.Record{"amount": 5}); len(got) != 4 {
			t.Fatalf("got %q, want the current year", got)
		}
		if res.Report.Counts.TranslatedCount != 1 {
			// "Amount" becomes "amount": the only change.
			t.Fatalf("only the field names may change; translated %d", res.Report.Counts.TranslatedCount)
		}
	})

	t.Run("a related field no relationship reaches is quarantined", func(t *testing.T) {
		res, col := convert(t, `Sum ( Order Lines::Line Total )`, "")
		reportSays(t, res, col, `"Order Lines::Line Total"`, "no relationship leads there")
	})

	t.Run("a deleted field is quarantined", func(t *testing.T) {
		res, col := convert(t, `<Table Missing>::<Field Missing> + Amount`, "")
		reportSays(t, res, col, "deleted in FileMaker")
	})

	t.Run("a layout object read is quarantined", func(t *testing.T) {
		res, col := convert(t, `GetLayoutObjectAttribute ( "total" ; "content" )`, "")
		reportSays(t, res, col, "layout object")
	})

	t.Run("a formula that does not read is quarantined", func(t *testing.T) {
		res, col := convert(t, `Amount * (`, "")
		reportSays(t, res, col, "could not be read")
	})
}

// A calculation that reads related records is computed each time a record is
// read, as FileMaker requires, and its related fields are bound to the
// imported occurrences and columns. A calculation reading it becomes unstored
// too; one reading only its own record stays stored.
func TestRelatedCalculationsAreUnstored(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: relatedCalculationsDesign("")})
	if err != nil {
		t.Fatal(err)
	}
	specs := map[string]map[string]interface{}{}
	for _, table := range res.Bundle.Tables {
		for _, col := range table.Columns {
			if col.CalculationFormula == nil {
				continue
			}
			var spec map[string]interface{}
			if err := json.Unmarshal([]byte(*col.CalculationFormula), &spec); err != nil {
				t.Fatal(err)
			}
			specs[col.DisplayName] = spec
		}
	}
	for _, name := range []string{"Items Total", "First Label", "Net"} {
		if specs[name]["stored"] != false {
			t.Fatalf("%s must be unstored: %v", name, specs[name])
		}
	}
	if _, set := specs["Discount Twice"]["stored"]; set {
		t.Fatalf("a calculation of its own record stays stored: %v", specs["Discount Twice"])
	}
	if _, set := specs["Product Key"]["stored"]; set || specs["Product Key"] == nil {
		t.Fatalf("a calculation a relationship matches on stays stored: %v", specs["Product Key"])
	}
	if _, kept := specs["Featured Key"]; kept {
		t.Fatal("a match field that reads related records cannot be computed on read: it is quarantined")
	}
	for _, table := range res.Bundle.Tables {
		for _, col := range table.Columns {
			if spec := specs[col.DisplayName]; spec != nil && spec["stored"] == false && !col.IsNullable {
				t.Fatalf("%s is unstored, so its column must be nullable", col.DisplayName)
			}
		}
	}
	if specs["Items Total"]["context"] != "Invoices" {
		t.Fatalf("the context occurrence must be kept: %v", specs["Items Total"])
	}

	reads := map[string][]calc.Reference{}
	for _, name := range []string{"Items Total", "First Label"} {
		expr, err := calc.Parse(specs[name]["formula"].(string))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		reads[name] = expr.RelatedFields()
	}
	if want := (calc.Reference{Table: "Invoice Items", Field: "line_amount"}); len(reads["Items Total"]) != 1 || reads["Items Total"][0] != want {
		t.Fatalf("Items Total reads %v, want %v", reads["Items Total"], want)
	}
	if want := (calc.Reference{Table: "Products", Field: "label"}); len(reads["First Label"]) != 1 || reads["First Label"][0] != want {
		t.Fatalf("First Label reads %v, want %v (two relationships away)", reads["First Label"], want)
	}

	var report []string
	for _, item := range append(res.Report.Translated, res.Report.Preserved...) {
		report = append(report, item.Detail)
	}
	if all := strings.Join(report, "\n"); !strings.Contains(all, "computed each time a record is shown (unstored)") {
		t.Fatalf("the report must say the calculation is unstored; it says:\n%s", all)
	}
	if len(res.Report.Unsupported) != 1 || !strings.Contains(res.Report.Unsupported[0].Detail, "a relationship matches records on it") {
		t.Fatalf("only Featured Key is unsupported, as a match field: %+v", res.Report.Unsupported)
	}
}

// relatedCalculationsDesign is a synthetic invoicing design: invoices, their
// items, and the product each item names. suffix keeps the tables of one
// test run apart from another's.
func relatedCalculationsDesign(suffix string) string {
	r := strings.NewReplacer("Invoices", "Invoices"+suffix, "Invoice Items", "Invoice Items"+suffix, "Products", "Products"+suffix)
	calcField := func(name, formula, storage string) string {
		dataType := "Number"
		if strings.HasSuffix(name, "Label") || strings.HasSuffix(name, "Key") {
			dataType = "Text"
		}
		return `<Field name="` + name + `" fieldType="Calculation" dataType="` + dataType + `">` + storage +
			`<Calculation table="Invoices"><![CDATA[` + formula + `]]></Calculation></Field>`
	}
	return r.Replace(`<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Invoices"><FieldCatalog>` +
		`<Field name="Invoice ID" dataType="Text"/><Field name="Discount" dataType="Number"/>` +
		// Net is declared before the field it reads.
		calcField("Net", `Items Total - Discount`, `<Storage storeCalculationResults="True"/>`) +
		calcField("Items Total", `Sum ( Invoice Items::Line Amount )`, `<Storage storeCalculationResults="False"/>`) +
		calcField("First Label", `Products::Label`, ``) +
		calcField("Discount Twice", `Discount * 2`, ``) +
		calcField("Featured Key", `Invoice Items::Product ID`, ``) +
		`</FieldCatalog></BaseTable>` +
		`<BaseTable name="Invoice Items"><FieldCatalog>` +
		`<Field name="Invoice ID" dataType="Text"/><Field name="Product ID" dataType="Text"/><Field name="Line Amount" dataType="Number"/>` +
		`<Field name="Product Key" fieldType="Calculation" dataType="Text"><Storage storeCalculationResults="False"/>` +
		`<Calculation table="Invoice Items"><![CDATA[Product ID]]></Calculation></Field>` +
		`</FieldCatalog></BaseTable>` +
		`<BaseTable name="Products"><FieldCatalog>` +
		`<Field name="Product ID" dataType="Text"/><Field name="Label" dataType="Text"/>` +
		`</FieldCatalog></BaseTable></BaseTableCatalog>` +
		`<TableOccurrenceCatalog>` +
		`<TableOccurrence name="Invoices" baseTable="Invoices"/>` +
		`<TableOccurrence name="Invoice Items" baseTable="Invoice Items"/>` +
		`<TableOccurrence name="Products" baseTable="Products"/>` +
		`<TableOccurrence name="Featured Products" baseTable="Products"/>` +
		`</TableOccurrenceCatalog><RelationshipCatalog>` +
		`<Relationship name="Items" left="Invoices" left_field="Invoice ID" right="Invoice Items" right_field="Invoice ID" operator="="/>` +
		`<Relationship name="Product" left="Invoice Items" left_field="Product Key" right="Products" right_field="Product ID" operator="="/>` +
		`<Relationship name="Featured" left="Invoices" left_field="Featured Key" right="Featured Products" right_field="Product ID" operator="="/>` +
		`</RelationshipCatalog></File></FMPReport>`)
}
