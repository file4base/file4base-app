package api_test

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/require"
)

func TestAPI_MigratedRecordsKeepCollidingSourceFields(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_identity")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog>
	<Field name="Code" dataType="Text"/><Field name="code" dataType="Text"/><Field name="id" dataType="Text"/><Field name="Notes" dataType="Text"/>
	<Field name="Mirror" fieldType="Calculation" dataType="Text"><Calculation>Code &amp; ":" &amp; code &amp; ":" &amp; id</Calculation></Field>
	</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	records := `<FMPXMLRESULT><METADATA><FIELD NAME="Code" TYPE="TEXT"/><FIELD NAME="code" TYPE="TEXT"/><FIELD NAME="id" TYPE="TEXT"/><FIELD NAME="Notes" TYPE="TEXT"/></METADATA><RESULTSET FOUND="1"><ROW RECORDID="1"><COL><DATA>first</DATA></COL><COL><DATA>second</DATA></COL><COL><DATA>business-key</DATA></COL><COL><DATA>   </DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`
	var result struct {
		RecordsInserted int `json:"records_inserted"`
	}
	response := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", token, map[string]interface{}{
		"design_xml": design, "data_files": map[string]string{"Visits": records},
	}, &result)
	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, 1, result.RecordsInserted)
	var rows []map[string]interface{}
	response = h.do(http.MethodGet, "/api/v1/data/visits/", token, nil, &rows)
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, rows, 1)
	require.Equal(t, "first", rows[0]["code"])
	require.Equal(t, "second", rows[0]["code_2"])
	require.Equal(t, "business-key", rows[0]["id_2"])
	require.Equal(t, "   ", rows[0]["notes"])
	require.NotEqual(t, "business-key", rows[0]["id"])
	require.Equal(t, "first:second:business-key", rows[0]["mirror"])
}

func TestAPI_NativeExtractionFailureDoesNotApplySchema(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_native_reject")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := appendSyntheticHBAM7Catalog(nil, 129, "Visits", []syntheticCatalogField{
		{id: 1, name: "Amount", fieldClass: 1, dataType: 2},
	})
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x06, 1, 4)
	for _, b := range []byte("12-A") {
		payload = append(payload, b^0x5a)
	}
	copy(file[2*4096+20:], payload)
	request := map[string]interface{}{"design_xml": `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog><Field id="1" name="Amount" dataType="Number"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`, "fmp12_base64": base64.StdEncoding.EncodeToString(file)}
	var before, after []struct {
		Name string `json:"name"`
	}
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &before)
	require.Equal(t, http.StatusOK, rec.Code)
	var preflight struct {
		Report struct {
			Counts struct {
				Rejected int `json:"rejected_count"`
				Detected int `json:"records_detected"`
			} `json:"counts"`
		} `json:"report"`
	}
	rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/preflight", token, request, &preflight)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Greater(t, preflight.Report.Counts.Rejected, 0)
	require.Equal(t, 1, preflight.Report.Counts.Detected)
	rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", token, request, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Native Data Import Blocked")
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &after)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, before, after)
}

func TestAPI_NativeAutoEnterConstantAppliesAndRuns(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_native_default")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	design := `<FMSaveAsXML><Structure><AddAction><BaseTableCatalog><BaseTable id="129" name="Visits"/></BaseTableCatalog><FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Visits"/><ObjectList><Field id="1" name="Status" datatype="Text" fieldtype="Normal"><AutoEnter><ConstantData>ready</ConstantData></AutoEnter></Field></ObjectList></FieldCatalog></FieldsForTables></AddAction></Structure></FMSaveAsXML>`
	rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", token, map[string]interface{}{"design_xml": design}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/data/visits/", token, map[string]interface{}{}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/visits/", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, rows, 1)
	require.Equal(t, "ready", rows[0]["status"])
}

func TestAPI_XMLDataRejectionDoesNotApplySchema(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_xml_reject")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	var before, after []struct {
		Name string `json:"name"`
	}
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &before)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", token, map[string]interface{}{
		"design_xml": `<FMPReport><File name="Synthetic"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog><Field name="Amount" dataType="Number"/></FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`,
		"data_files": map[string]string{"Visits": `<FMPXMLRESULT><METADATA><FIELD NAME="Amount" TYPE="NUMBER"/></METADATA><RESULTSET FOUND="1"><ROW><COL><DATA>1/2</DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`},
	}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Data Import Blocked")
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &after)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, before, after)
}

// #130: a .fmp12 whose internal catalog does not match the attached design
// must be rejected on preflight and blocked with 422 on apply without touching
// the target database.
func TestAPI_MismatchedNativeCatalogDoesNotApplySchema(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_native_mismatch")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")

	// Native file belongs to "Invoices" (table 129, field 1 "Total" Text),
	// whereas the design declares "Visits" (table 129, field 1 "Amount" Number).
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := appendSyntheticHBAM7Catalog(nil, 129, "Invoices", []syntheticCatalogField{
		{id: 1, name: "Total", fieldClass: 1, dataType: 1},
	})
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x06, 1, 2, '9'^0x5a, '9'^0x5a)
	copy(file[2*4096+20:], payload)

	var before, after []struct {
		Name string `json:"name"`
	}
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &before)
	require.Equal(t, http.StatusOK, rec.Code)

	var preflight struct {
		Report struct {
			Counts struct {
				Rejected int `json:"rejected_count"`
				Ready    int `json:"records_ready"`
			} `json:"counts"`
			Rejected []struct {
				Detail string `json:"detail"`
			} `json:"rejected"`
		} `json:"report"`
	}
	rec = h.doMultipart("/api/v1/solutions/migrate/preflight", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
		{name: "native", filename: "Invoices.fmp12", body: file},
	}, &preflight)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Greater(t, preflight.Report.Counts.Rejected, 0)
	require.Equal(t, 0, preflight.Report.Counts.Ready)
	require.NotEmpty(t, preflight.Report.Rejected)
	require.Contains(t, preflight.Report.Rejected[0].Detail, `design has "Visits", native has "Invoices"`)
	require.Contains(t, preflight.Report.Rejected[0].Detail, `design has "Amount", native has "Total"`)

	rec = h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": nativeVisitsDesign}),
		{name: "native", filename: "Invoices.fmp12", body: file},
	}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Native Data Import Blocked")

	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &after)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, before, after)
}
