package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two synthetic FileMaker files that import from each other by position, in
// the Save a Copy as XML grammar. Shop imports Ledger's entries into its
// orders, matching on the code and performing auto-enter on one field, and
// replaces its own orders' notes from its customers; Ledger adds Shop's
// orders to its entries. No step names its source table or fields.
const shopDesignXML = `<FMSaveAsXML version="2.2.3.0" File="Shop.fmp12" Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/><BaseTable id="130" name="Customers"/></BaseTableCatalog>
 <FieldsForTables>
  <FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
   <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="Total" datatype="Number" fieldtype="Normal"/>
   <Field id="3" name="Note" datatype="Text" fieldtype="Normal"/>
   <Field id="4" name="Stamp" datatype="Text" fieldtype="Normal"><AutoEnter type="ModificationAccountName"/></Field>
  </ObjectList></FieldCatalog>
  <FieldCatalog><BaseTableReference id="130" name="Customers"/><ObjectList>
   <Field id="1" name="Name" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="City" datatype="Text" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Orders"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Customers"><BaseTableSourceReference><BaseTableReference id="130" name="Customers"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <ScriptCatalog><Script id="1" name="Pull ledger"/><Script id="2" name="Notes from customers"/></ScriptCatalog>
 <StepsForScripts>
 <Script><ScriptReference id="1" name="Pull ledger"/><ObjectList>
  <Step id="35" name="Import Records" enable="True"><ParameterValues>
   <Parameter type="Boolean"><Boolean type="With dialog" value="False"/></Parameter>
   <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Ledger.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
   <Parameter type="ImportField"><ImportField addasnew="True">
    <Options doAutoEntry="True" matchFieldNames="False" splitRepetitions="False"/>
    <Target><TableOccurrenceReference id="1" name="Orders"/></Target>
    <Field membercount="4">
     <Map name="Source field 1 match with " index="1" kind="2" Options="0"><FieldReference id="1" name="Code"/></Map>
     <Map name="Source field 2 import to " index="2" kind="0" Options="0"><FieldReference id="2" name="Total"/></Map>
     <Map name="Source field 3 not import to " index="3" kind="1" Options="0"><FieldReference id="3" name="Note"/></Map>
     <Map name="Source field 4 not import to " index="4" kind="1" Options="2"><FieldReference id="4" name="Stamp"/></Map>
    </Field>
    <action name="Update" value="2"/>
   </ImportField></Parameter>
  </ParameterValues></Step>
 </ObjectList></Script>
 <Script><ScriptReference id="2" name="Notes from customers"/><ObjectList>
  <Step id="35" name="Import Records" enable="True"><ParameterValues>
   <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Shop.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
   <Parameter type="ImportField"><ImportField addasnew="False">
    <Options doAutoEntry="False"/>
    <Target><TableOccurrenceReference id="1" name="Orders"/></Target>
    <Field membercount="4">
     <Map name="Source field 1 not import to " index="1" kind="1" Options="0"><FieldReference id="1" name="Code"/></Map>
     <Map name="Source field 2 not import to " index="2" kind="1" Options="0"><FieldReference id="2" name="Total"/></Map>
     <Map name="Source field 1 import to " index="1" kind="0" Options="0"><FieldReference id="3" name="Note"/></Map>
     <Map name="Source field 4 not import to " index="4" kind="1" Options="0"><FieldReference id="4" name="Stamp"/></Map>
    </Field>
    <action name="Replace" value="1"/>
   </ImportField></Parameter>
  </ParameterValues></Step>
 </ObjectList></Script>
 </StepsForScripts>
</AddAction></Structure></FMSaveAsXML>`

const ledgerDesignXML = `<FMSaveAsXML version="2.2.3.0" File="Ledger.fmp12" Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Entries"/><BaseTable id="130" name="Days"/></BaseTableCatalog>
 <FieldsForTables>
  <FieldCatalog><BaseTableReference id="129" name="Entries"/><ObjectList>
   <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="Amount" datatype="Number" fieldtype="Normal"/>
   <Field id="3" name="Remark" datatype="Text" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
  <FieldCatalog><BaseTableReference id="130" name="Days"/><ObjectList>
   <Field id="1" name="Label" datatype="Text" fieldtype="Normal"/>
   <Field id="2" name="Day" datatype="Date" fieldtype="Normal"/>
   <Field id="3" name="Comment" datatype="Text" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Entries"><BaseTableSourceReference><BaseTableReference id="129" name="Entries"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <ScriptCatalog><Script id="1" name="Pull shop"/></ScriptCatalog>
 <StepsForScripts>
 <Script><ScriptReference id="1" name="Pull shop"/><ObjectList>
  <Step id="35" name="Import Records" enable="True"><ParameterValues>
   <Parameter type="DataSourceReference"><DataSourceReference fileType="FMPR" value="1"><UniversalPathList fileType="FMPR"><ObjectList><Location>file:Shop.fmp12</Location></ObjectList></UniversalPathList></DataSourceReference></Parameter>
   <Parameter type="ImportField"><ImportField addasnew="False">
    <Options doAutoEntry="False"/>
    <Target><TableOccurrenceReference id="1" name="Entries"/></Target>
    <Field membercount="3">
     <Map name="Source field 1 not import to " index="1" kind="1"><FieldReference id="1" name="Code"/></Map>
     <Map name="Source field 2 import to " index="2" kind="0"><FieldReference id="2" name="Amount"/></Map>
     <Map name="Source field 3 import to " index="3" kind="0"><FieldReference id="3" name="Remark"/></Map>
    </Field>
    <action name="Add" value="0"/>
   </ImportField></Parameter>
  </ParameterValues></Step>
 </ObjectList></Script>
 </StepsForScripts>
</AddAction></Structure></FMSaveAsXML>`

type importStepsList struct {
	Steps []api.ImportStepView `json:"steps"`
}

type importStepsOutcome struct {
	Steps []api.ImportStepOutcome `json:"steps"`
}

// #191: migrated Import Records steps that import from another FileMaker
// file wait, disabled, until the migration review creates their transfer;
// the step then runs it with the session's own authority.
func TestAPI_MigratedImportRecordsStepsGetTheirTransfer(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	shopDB, ledgerDB := uniqueDB("f4b_imp_shop"), uniqueDB("f4b_imp_ledger")
	h.createDatabase(shopDB, "alice", "alice-secret")
	h.createDatabase(ledgerDB, "bob", "bob-secret")
	shop := h.login(shopDB, "alice", "alice-secret")
	ledger := h.login(ledgerDB, "bob", "bob-secret")

	for _, m := range []struct{ token, xml string }{{shop, shopDesignXML}, {ledger, ledgerDesignXML}} {
		rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", m.token,
			map[string]interface{}{"design_xml": m.xml}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	// Each step waits, disabled, and says why.
	steps := map[string]schema.ScriptStepMetadata{}
	for _, sc := range h.observeScripts(shop) {
		var full schema.ScriptMetadata
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/scripts/"+sc.ID, shop, nil, &full).Code)
		require.Len(t, full.Steps, 1)
		steps[sc.Name] = full.Steps[0]
	}
	require.Len(t, steps, 2)
	pull := steps["Pull ledger"]
	assert.False(t, pull.IsEnabled, "nothing runs before its transfer exists")
	var params map[string]interface{}
	require.NoError(t, json.Unmarshal(pull.Params, &params))
	assert.Equal(t, true, params["awaiting_transfer"])
	assert.Contains(t, params["migration_note"], "migration review")

	// The review lists them.
	var listed importStepsList
	rec := h.do(http.MethodGet, "/api/v1/transfers/import-steps", shop, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, listed.Steps, 2)
	byScript := map[string]api.ImportStepView{}
	for _, s := range listed.Steps {
		byScript[s.ScriptName] = s
	}
	assert.Equal(t, "Ledger", byScript["Pull ledger"].SourceFile)
	assert.Equal(t, "update", byScript["Pull ledger"].Method)
	assert.Equal(t, []string{"stamp"}, byScript["Pull ledger"].AutoEnter)
	assert.Empty(t, byScript["Pull ledger"].SuggestedDatabase, "nothing says which solution Ledger became")
	assert.Equal(t, shopDB, byScript["Notes from customers"].SuggestedDatabase)
	assert.Equal(t, "this_file", byScript["Notes from customers"].SuggestedBy)

	// Another solution is read only with a session of its own.
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/infer", shop, map[string]interface{}{
		"source_database": ledgerDB, "step_ids": []string{pull.ID},
	}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	type inferred struct {
		Tables []string `json:"tables"`
		Steps  []struct {
			StepID string `json:"step_id"`
			transfer.Inference
		} `json:"steps"`
	}
	var fromLedger inferred
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/infer", shop, map[string]interface{}{
		"source_database": ledgerDB, "peer_token": ledger, "step_ids": []string{pull.ID},
	}, &fromLedger)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, fromLedger.Steps, 1)
	assert.Equal(t, "entries", fromLedger.Steps[0].Table, "Days has a date where the step imports a number")
	assert.Equal(t, transfer.ConfidenceHigh, fromLedger.Steps[0].Confidence, "the code field agrees by name")
	assert.ElementsMatch(t, []string{"entries", "days"}, fromLedger.Tables)

	notes := steps["Notes from customers"]
	var fromShop inferred
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/infer", shop, map[string]interface{}{
		"step_ids": []string{notes.ID},
	}, &fromShop)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "customers", fromShop.Steps[0].Table, "the target table is not its own source")
	assert.Equal(t, transfer.ConfidenceMedium, fromShop.Steps[0].Confidence)

	// Confirming creates each transfer and names it in its step.
	var confirmed importStepsOutcome
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/confirm", shop, map[string]interface{}{
		"items": []map[string]string{
			{"step_id": pull.ID, "source_database": ledgerDB, "source_table": "entries"},
			{"step_id": notes.ID, "source_database": "", "source_table": "customers"},
		},
		"peer_tokens": map[string]string{ledgerDB: ledger},
	}, &confirmed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, confirmed.Steps, 2)
	for _, o := range confirmed.Steps {
		require.Empty(t, o.Error)
		require.NotEmpty(t, o.Transfer)
	}
	rec = h.do(http.MethodGet, "/api/v1/transfers/import-steps", shop, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, listed.Steps, "a step with its transfer no longer waits")

	var after schema.ScriptMetadata
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/scripts/"+pull.ScriptID, shop, nil, &after).Code)
	require.Len(t, after.Steps, 1)
	assert.True(t, after.Steps[0].IsEnabled, "FileMaker had it enabled")
	params = nil
	require.NoError(t, json.Unmarshal(after.Steps[0].Params, &params))
	assert.Equal(t, confirmed.Steps[0].Transfer, params["transfer"])
	assert.NotContains(t, params, "migration_note")
	assert.NotContains(t, params, "awaiting_transfer")

	var pullDef schema.TransferDefinition
	rec = h.do(http.MethodGet, "/api/v1/transfers/"+urlPath(confirmed.Steps[0].Transfer), shop, nil, &pullDef)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, schema.TransferModeUpdateMatching, pullDef.Mode)
	assert.Equal(t, []string{"code"}, pullDef.MatchFields, "only the field FileMaker matched on")
	assert.Equal(t, schema.TransferUnmatchedAdd, pullDef.OnUnmatched)
	assert.Equal(t, []schema.TransferMapping{
		{SourceField: "code", DestinationField: "code"}, {SourceField: "amount", DestinationField: "total"},
	}, pullDef.Mappings)
	assert.True(t, pullDef.PerformAutoEnter)
	assert.Equal(t, []string{"stamp"}, pullDef.AutoEnterFields)

	// Running it: one order updated, one added, auto-enter on the one field.
	for _, row := range []map[string]interface{}{{"code": "A1", "amount": 10}, {"code": "A2", "amount": 20}} {
		require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/entries", ledger, row, nil).Code)
	}
	var first map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/orders", shop, map[string]interface{}{"code": "A1", "total": 1}, &first)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var report transfer.Report
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+pullDef.ID+"/run", shop,
		map[string]interface{}{"peer_token": ledger, "return_record_ids": true}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, report.Updated)
	assert.Equal(t, 1, report.Added)
	assert.Len(t, report.RecordIDs, 2)
	orders := map[string]map[string]interface{}{}
	for _, r := range h.observeRows(shop, "orders") {
		orders[fmt.Sprint(r["code"])] = r
	}
	require.Len(t, orders, 2)
	assert.Equal(t, "10", fmt.Sprint(orders["A1"]["total"]))
	assert.Equal(t, "alice", fmt.Sprint(orders["A2"]["stamp"]), "the field chosen for auto-enter is stamped")

	// Replacing the found set in order, from this solution's own table.
	for _, name := range []string{"N1", "N2"} {
		require.Equal(t, http.StatusCreated,
			h.do(http.MethodPost, "/api/v1/data/customers", shop, map[string]interface{}{"name": name}, nil).Code)
	}
	a2 := fmt.Sprint(orders["A2"]["id"])
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+urlPath(confirmed.Steps[1].Transfer)+"/run", shop,
		map[string]interface{}{"destination_found_set": []string{a2}, "return_record_ids": true}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, schema.TransferModeReplaceFoundSet, report.Mode)
	assert.Equal(t, 1, report.Updated)
	assert.Equal(t, 1, report.Skipped, "no record is left for the second row, and remaining rows are not added")
	assert.Equal(t, []string{a2}, report.RecordIDs)
	orders = map[string]map[string]interface{}{}
	for _, r := range h.observeRows(shop, "orders") {
		orders[fmt.Sprint(r["code"])] = r
	}
	assert.Equal(t, "N1", fmt.Sprint(orders["A2"]["note"]))
	assert.Nil(t, orders["A1"]["note"], "a record outside the found set is left alone")

	// And the other way: Ledger's step reads Shop by position.
	rec = h.do(http.MethodGet, "/api/v1/transfers/import-steps", ledger, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, listed.Steps, 1)
	var fromOrders inferred
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/infer", ledger, map[string]interface{}{
		"source_database": shopDB, "peer_token": shop, "step_ids": []string{listed.Steps[0].StepID},
	}, &fromOrders)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "orders", fromOrders.Steps[0].Table, "customers has no third field")
	assert.Equal(t, transfer.ConfidenceMedium, fromOrders.Steps[0].Confidence)
	rec = h.do(http.MethodPost, "/api/v1/transfers/import-steps/confirm", ledger, map[string]interface{}{
		"items":       []map[string]string{{"step_id": listed.Steps[0].StepID, "source_database": shopDB, "source_table": "orders"}},
		"peer_tokens": map[string]string{shopDB: shop},
	}, &confirmed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, confirmed.Steps, 1)
	assert.Empty(t, confirmed.Steps[0].Error)
}

func urlPath(name string) string { return url.PathEscape(name) }
