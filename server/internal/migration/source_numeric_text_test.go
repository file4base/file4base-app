package migration

import "testing"

func TestSourceNumericTextCannotBecomeRoundedFraction(t *testing.T) {
	// A literal-only Claris Pro 26.0.2 French Data Viewer probe returned "12"
	// for GetAsText(GetAsNumber("1/2")), not a division result. Neither numeric
	// interpretation preserves permissive Number text, so strict migration must
	// report it instead of silently replacing the source value.
	for _, text := range []string{"1/2", "1/3", "1 1/4", "12-A", "7,125", "NaN", "Infinity"} {
		if value, err := coerceSourceValue(text, "NUMBER"); err == nil {
			t.Fatalf("source text %q was silently replaced with %v", text, value)
		}
	}
	const design = `<FMPReport><File name="Synthetic"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Amount" dataType="Number"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	const records = `<FMPXMLRESULT><METADATA><FIELD NAME="Amount" TYPE="NUMBER"/></METADATA><RESULTSET FOUND="2"><ROW><COL><DATA>1/2</DATA></COL></ROW><ROW><COL><DATA>7</DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Visits": records}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Counts.RecordsDetected != 2 || result.Report.Counts.RecordsReady != 0 || len(result.TableRows["visits"]) != 0 || len(result.Report.Rejected) == 0 {
		t.Fatal("a dataset with an unrepresentable cell became partially ready")
	}
}
