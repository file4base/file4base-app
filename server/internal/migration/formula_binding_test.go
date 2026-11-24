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

	t.Run("a related field is quarantined", func(t *testing.T) {
		res, col := convert(t, `Sum ( Order Lines::Line Total )`, "")
		reportSays(t, res, col, `"Order Lines::Line Total"`, "related")
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
