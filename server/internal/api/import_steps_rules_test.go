package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two more synthetic FileMaker files (#191). Depot imports Stock's lines into
// its items twice: by position, matching on a stored calculation and putting
// a text quantity into a number, and by matching names, whose case differs
// between the files. A third step's target table was deleted in FileMaker.
const depotDesignXML = `<FMSaveAsXML version="2.2.3.0" File="Depot.fmp12" Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Items"/></BaseTableCatalog>
 <FieldsForTables>
  <FieldCatalog><BaseTableReference id="129" name="Items"/><ObjectList>
   <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="Qty" datatype="Number" fieldtype="Normal"/>
   <Field id="3" name="Note" datatype="Text" fieldtype="Normal"/>
   <Field id="4" name="Key" datatype="Text" fieldtype="Calculated"><Calculation><Calculation><Text>Upper ( Code )</Text><ChunkList><Chunk type="FunctionRef">Upper</Chunk><Chunk type="NoRef"> ( </Chunk><Chunk type="FieldRef"><FieldReference id="1" name="Code"><TableOccurrenceReference id="1" name="Items"/></FieldReference></Chunk><Chunk type="NoRef"> )</Chunk></ChunkList></Calculation></Calculation></Field>
  </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Items"><BaseTableSourceReference><BaseTableReference id="129" name="Items"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <ScriptCatalog><Script id="1" name="Pull stock"/><Script id="2" name="Notes by name"/><Script id="3" name="Gone"/></ScriptCatalog>
 <StepsForScripts>
 <Script><ScriptReference id="1" name="Pull stock"/><ObjectList>
  <Step id="35" name="Import Records" enable="True"><ParameterValues>
   <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Stock.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
   <Parameter type="ImportField"><ImportField addasnew="False">
    <Options doAutoEntry="False" matchFieldNames="False"/>
    <Target><TableOccurrenceReference id="1" name="Items"/></Target>
    <Field>
     <Map name="Source field 1 match with " index="1" kind="2"><FieldReference id="4" name="Key"/></Map>
     <Map name="Source field 2 import to " index="2" kind="0"><FieldReference id="2" name="Qty"/></Map>
    </Field>
    <action name="Update" value="2"/>
   </ImportField></Parameter>
  </ParameterValues></Step>
 </ObjectList></Script>
 <Script><ScriptReference id="2" name="Notes by name"/><ObjectList>
  <Step id="35" name="Import Records" enable="True"><ParameterValues>
   <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Stock.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
   <Parameter type="ImportField"><ImportField addasnew="False">
    <Options doAutoEntry="False" matchFieldNames="True"/>
    <Target><TableOccurrenceReference id="1" name="Items"/></Target>
    <Field>
     <Map name="Source field 1 import to " index="1" kind="0"><FieldReference id="1" name="Code"/></Map>
     <Map name="Source field 2 import to " index="2" kind="0"><FieldReference id="3" name="Note"/></Map>
    </Field>
    <action name="Add" value="0"/>
   </ImportField></Parameter>
  </ParameterValues></Step>
 </ObjectList></Script>
 <Script><ScriptReference id="3" name="Gone"/><ObjectList>
  <Step id="35" name="Import Records" enable="True"><ParameterValues>
   <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Stock.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
   <Parameter type="ImportField"><ImportField addasnew="False">
    <Options doAutoEntry="False"/><Target/>
    <Field><Map name="Source field 1 import to " index="1" kind="0"/><Map name="Source field 2 import to " index="2" kind="0"/></Field>
    <action name="Add" value="0"/>
   </ImportField></Parameter>
  </ParameterValues></Step>
 </ObjectList></Script>
 </StepsForScripts>
</AddAction></Structure></FMSaveAsXML>`

const stockDesignXML = `<FMSaveAsXML version="2.2.3.0" File="Stock.fmp12" Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Lines"/><BaseTable id="130" name="Photos"/></BaseTableCatalog>
 <FieldsForTables>
  <FieldCatalog><BaseTableReference id="129" name="Lines"/><ObjectList>
   <Field id="1" name="CODE" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="Count" datatype="Text" fieldtype="Normal"/>
   <Field id="3" name="NOTE" datatype="Text" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
  <FieldCatalog><BaseTableReference id="130" name="Photos"/><ObjectList>
   <Field id="1" name="Ref" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="Scan" datatype="Binary" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Lines"><BaseTableSourceReference><BaseTableReference id="129" name="Lines"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
</AddAction></Structure></FMSaveAsXML>`

func TestAPI_ImportStepsUnderFileMakersRules(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	depotDB, stockDB := uniqueDB("f4b_imp_depot"), uniqueDB("f4b_imp_stock")
	h.createDatabase(depotDB, "alice", "alice-secret")
	h.createDatabase(stockDB, "bob", "bob-secret")
	depot := h.login(depotDB, "alice", "alice-secret")
	stock := h.login(stockDB, "bob", "bob-secret")
	for _, m := range []struct{ token, xml string }{{depot, depotDesignXML}, {stock, stockDesignXML}} {
		rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", m.token,
			map[string]interface{}{"design_xml": m.xml}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	var listed importStepsList
	rec := h.do(http.MethodGet, "/api/v1/transfers/import-steps", depot, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	byScript := map[string]api.ImportStepView{}
	for _, s := range listed.Steps {
		byScript[s.ScriptName] = s
	}
	require.Len(t, byScript, 3)
	assert.Contains(t, byScript["Gone"].Problem, "deleted from the target table in FileMaker")
	assert.True(t, byScript["Notes by name"].MatchByName)
	assert.Empty(t, byScript["Pull stock"].Problem)

	var out inferredSteps
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/infer", depot, map[string]interface{}{
		"source_database": stockDB, "peer_token": stock,
		"step_ids": []string{byScript["Pull stock"].StepID, byScript["Notes by name"].StepID},
	}, &out)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, out.Steps, 2)

	// By position: the only table FileMaker could import from, its text
	// count going into a number, which a person has to accept.
	pull := out.Steps[0]
	assert.Equal(t, "lines", pull.Table)
	assert.Equal(t, transfer.ConfidenceLow, pull.Confidence)
	assert.Contains(t, pull.Reason, "2 count (TEXT) → qty (NUMBER)")
	require.NotEmpty(t, pull.Candidates)
	assert.True(t, pull.Candidates[0].Possible)
	assert.False(t, pull.Candidates[0].Fits)
	// By name: the case differs, the names are the same.
	byName := out.Steps[1]
	assert.True(t, byName.ByName)
	assert.Equal(t, "lines", byName.Table)
	assert.Equal(t, transfer.ConfidenceHigh, byName.Confidence)

	// In bulk, the server infers each step again: the low-confidence one is
	// refused, the high-confidence one created.
	var confirmed importStepsOutcome
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/confirm", depot, map[string]interface{}{
		"items": []map[string]string{
			{"step_id": byScript["Pull stock"].StepID, "source_database": stockDB, "source_table": "lines"},
			{"step_id": byScript["Notes by name"].StepID, "source_database": stockDB, "source_table": "lines"},
		},
		"peer_tokens": map[string]string{stockDB: stock},
	}, &confirmed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, confirmed.Steps, 2)
	assert.Contains(t, confirmed.Steps[0].Error, "low confidence")
	assert.Contains(t, confirmed.Steps[0].Error, "confirm the step on its own")
	assert.Empty(t, confirmed.Steps[0].Transfer)
	require.Empty(t, confirmed.Steps[1].Error)

	// On its own, with the table picked, it is created.
	var alone importStepsOutcome
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/confirm", depot, map[string]interface{}{
		"items":       []map[string]string{{"step_id": byScript["Pull stock"].StepID, "source_database": stockDB, "source_table": "lines"}},
		"peer_tokens": map[string]string{stockDB: stock},
	}, &alone)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, alone.Steps, 1)
	require.Empty(t, alone.Steps[0].Error)
	confirmed.Steps[0] = alone.Steps[0]

	for _, row := range []map[string]interface{}{
		{"code": "a1", "count": "5", "note": "first"},
		{"code": "b2", "count": "several", "note": "second"},
	} {
		rec := h.do(http.MethodPost, "/api/v1/data/lines", stock, row, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	for _, item := range []map[string]interface{}{{"code": "A1", "qty": 1}, {"code": "B2", "qty": 7}} {
		rec = h.do(http.MethodPost, "/api/v1/data/items", depot, item, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	// The stored calculation finds the records and is never written; the
	// count that is not a number is left out of its record, which is still
	// updated, and reported (#231).
	var report transfer.Report
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+urlPath(confirmed.Steps[0].Transfer)+"/run", depot,
		map[string]interface{}{"peer_token": stock}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, report.Updated)
	assert.Equal(t, 1, report.FieldsSkipped)
	require.Equal(t, 1, report.RejectedCount())
	assert.Equal(t, 2, report.Rejected[0].Row)
	assert.False(t, report.Rejected[0].RecordSkipped)
	qty := map[string]string{}
	for _, r := range h.observeRows(depot, "items") {
		qty[fmt.Sprint(r["key"])] = fmt.Sprint(r["qty"])
	}
	assert.Equal(t, map[string]string{"A1": "5", "B2": "7"}, qty)

	// Matching names pairs CODE with code and NOTE with note at each run.
	var byNameDef schema.TransferDefinition
	rec = h.do(http.MethodGet, "/api/v1/transfers/"+urlPath(confirmed.Steps[1].Transfer), depot, nil, &byNameDef)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, byNameDef.MatchByName)
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+byNameDef.ID+"/run", depot,
		map[string]interface{}{"peer_token": stock}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, report.Added)
	assert.Empty(t, report.Unpaired)
	notes := map[string]bool{}
	for _, r := range h.observeRows(depot, "items") {
		notes[fmt.Sprint(r["note"])] = true
	}
	assert.True(t, notes["first"] && notes["second"], "notes = %v", notes)
}
