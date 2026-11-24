package migration

import "testing"

func TestSourceUUIDShapedTextStaysVerbatim(t *testing.T) {
	const text = "  A0000000-B000-C000-D000-E00000000000  "
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Reference" dataType="Text"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	records := `<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult"><METADATA><FIELD NAME="Reference" TYPE="TEXT"/></METADATA><RESULTSET FOUND="1"><ROW><COL><DATA>` + text + `</DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`
	result, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Visits": records}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.TableRows["visits"][0]["reference"]; got != text {
		t.Fatal("text matching a UUID was trimmed or case-folded")
	}
}
