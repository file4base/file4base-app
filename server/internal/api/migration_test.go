package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/migration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_FileMakerMigration(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	dbName := uniqueDB("f4b_mig")
	h.createDatabase(dbName, "alice", "alice-secret")
	ownerTok := h.login(dbName, "alice", "alice-secret")

	// 1. Direct upload of a binary .fmp12 file to /api/v1/solutions/import is rejected with 422 and migration guidance.
	hbamBytes := append([]byte{0x00, 0x01, 0x00, 0x00, 0x00}, []byte("HBAM2 FileMaker Pro 12.0")...)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/import?filename=Inventory.fmp12", bytes.NewReader(hbamBytes))
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Unsupported Binary .fmp12 Format")

	ddrXML := `<?xml version="1.0" encoding="UTF-8"?>
<FMPReport type="Report" version="19.0">
  <File name="FieldSpecimens.fmp12">
    <BaseTableCatalog>
      <BaseTable name="Specimens">
        <FieldCatalog>
          <Field name="Code" dataType="Text" fieldType="Normal"/>
          <Field name="Weight" dataType="Number" fieldType="Normal"/>
          <Field name="CollectedDate" dataType="Date" fieldType="Calculation">
            <Calculation><Text>Get ( CurrentDate )</Text></Calculation>
          </Field>
          <Field name="UnsupportedSQL" dataType="Text" fieldType="Calculation">
            <Calculation><Text>ExecuteSQL("SELECT Code FROM Specimens"; ""; "")</Text></Calculation>
          </Field>
        </FieldCatalog>
      </BaseTable>
    </BaseTableCatalog>
    <TableOccurrenceCatalog>
      <TableOccurrence name="Specimens" baseTable="Specimens"/>
    </TableOccurrenceCatalog>
    <LayoutCatalog>
      <Layout name="Specimen Form" table="Specimens">
        <Part type="body" height="300"/>
        <Field name="Code"/>
        <Field name="Weight"/>
        <ScriptTrigger event="OnRecordLoad" script="AuditScript"/>
      </Layout>
    </LayoutCatalog>
    <ScriptCatalog>
      <Script name="AuditScript">
        <StepList>
          <Step name="Go to Layout" layout="Specimen Form" enable="True"/>
          <Step name="Perform AppleScript" script="beep" enable="True"/>
        </StepList>
      </Script>
    </ScriptCatalog>
    <AccountCatalog>
      <Account name="botanist" privilegeSet="[Data Entry Only]"/>
    </AccountCatalog>
  </File>
</FMPReport>`

	fmpDataXML := `<?xml version="1.0" encoding="UTF-8"?>
<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult">
  <METADATA>
    <FIELD NAME="Code" TYPE="TEXT"/>
    <FIELD NAME="Weight" TYPE="NUMBER"/>
  </METADATA>
  <RESULTSET FOUND="2">
    <ROW RECORDID="1" MODID="1">
      <COL><DATA>SP-101</DATA></COL>
      <COL><DATA>12.5</DATA></COL>
    </ROW>
    <ROW RECORDID="2" MODID="1">
      <COL><DATA>SP-102</DATA></COL>
      <COL><DATA>19.0</DATA></COL>
    </ROW>
  </RESULTSET>
</FMPXMLRESULT>`

	// 2. Dry-run preflight returns the conversion report without creating tables.
	var preflightRes struct {
		Status string                      `json:"status"`
		Report *migration.ConversionReport `json:"report"`
	}
	rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/preflight", ownerTok, map[string]interface{}{
		"design_xml": ddrXML,
		"data_files": map[string]string{
			"Specimens": fmpDataXML,
		},
	}, &preflightRes)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "preflight_ok", preflightRes.Status)
	require.NotNil(t, preflightRes.Report)
	assert.Equal(t, "FieldSpecimens", preflightRes.Report.SolutionName)
	assert.True(t, preflightRes.Report.TriggersQuarantined)
	assert.Equal(t, 1, preflightRes.Report.Counts.Tables)
	assert.Equal(t, 2, preflightRes.Report.Counts.RecordsReady)
	assert.NotEmpty(t, preflightRes.Report.Unsupported)
	assert.NotEmpty(t, preflightRes.Report.ManualActionRequired)

	// 3. Apply imports the converted solution and FMPXMLRESULT records.
	var applyRes struct {
		Status          string                      `json:"status"`
		Database        string                      `json:"database"`
		RecordsInserted int                         `json:"records_inserted"`
		Report          *migration.ConversionReport `json:"report"`
	}
	rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", ownerTok, map[string]interface{}{
		"design_xml": ddrXML,
		"data_files": map[string]string{
			"Specimens": fmpDataXML,
		},
	}, &applyRes)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "migrated", applyRes.Status)
	assert.Equal(t, 2, applyRes.RecordsInserted)
}
