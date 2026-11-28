package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/migration"
	"github.com/file4base/file4base-app/server/internal/schema"
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

// #97 — a migration into a database that already holds a solution is all or
// nothing: when the records cannot be imported, the design it merged goes
// back to what it was, so a retry does not build on a half-migrated schema.
func TestAPI_FileMakerMigrationRollsBackIntoAnExistingDatabase(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	dbName := uniqueDB("f4b_mig_rb")
	h.createDatabase(dbName, "alice", "alice-secret")
	ownerTok := h.login(dbName, "alice", "alice-secret")

	tableNames := func() []string {
		var tables []struct {
			Name string `json:"name"`
		}
		rec := h.do(http.MethodGet, "/api/v1/schemas/tables", ownerTok, nil, &tables)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		names := make([]string, 0, len(tables))
		for _, t := range tables {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		return names
	}

	// A solution the database already holds, migrated in successfully.
	firstDDR := `<FMPReport><File name="Visits.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text" fieldType="Normal"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`
	rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", ownerTok, map[string]interface{}{
		"design_xml": firstDDR,
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	before := tableNames()
	require.Contains(t, before, "visits")
	require.NotContains(t, before, "importedevents")

	// A second migration whose records cannot be imported: the field is
	// unique and validated on every write, and both exported records carry
	// the same value.
	failingDDR := `<FMPReport><File name="Events.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="ImportedEvents"><FieldCatalog>` +
		`<Field name="Code" dataType="Text" fieldType="Normal">` +
		`<Validation alwaysValidate="True"><Unique value="True"/></Validation>` +
		`</Field></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`
	duplicateRecords := `<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult"><METADATA>` +
		`<FIELD NAME="Code" TYPE="TEXT"/></METADATA><RESULTSET FOUND="2">` +
		`<ROW RECORDID="1"><COL><DATA>EV-1</DATA></COL></ROW>` +
		`<ROW RECORDID="2"><COL><DATA>EV-1</DATA></COL></ROW>` +
		`</RESULTSET></FMPXMLRESULT>`

	rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", ownerTok, map[string]interface{}{
		"design_xml": failingDDR,
		"data_files": map[string]string{"ImportedEvents": duplicateRecords},
	}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Migration Data Import Error")
	assert.NotContains(t, rec.Body.String(), "could not be fully restored")

	assert.Equal(t, before, tableNames(),
		"a failed migration leaves the design exactly as it was")
}

// A migration grants fmrest to the role of the privilege sets that carry it
// (#213) only when that role held no account before: otherwise it would open
// the Data API to accounts the file knows nothing of, so it reports instead.
func TestAPI_MigrationGrantsFMRestOnlyToRolesItFills(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	design := `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Notes"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Notes"/><ObjectList><Field id="1" name="Body" datatype="Text" fieldtype="Normal"/></ObjectList></FieldCatalog></FieldsForTables>
 <ExtendedPrivilegesCatalog membercount="1"><ObjectList membercount="1">
  <ExtendedPrivilege id="9" name="fmrest"><ObjectList membercount="1"><PrivilegeSetReference id="4" name="Clerks"/></ObjectList></ExtendedPrivilege>
 </ObjectList></ExtendedPrivilegesCatalog>
 <AccountsCatalog membercount="2"><ObjectList membercount="2">
  <Account id="3" kind="0" type="FileMaker" enable="True"><Authentication><AccountName>clerk</AccountName></Authentication><PrivilegeSetReference id="4" name="Clerks"/></Account>
  <Account id="5" kind="0" type="FileMaker" enable="True"><Authentication><AccountName>viewer</AccountName></Authentication><PrivilegeSetReference id="6" name="Viewers"/></Account>
 </ObjectList></AccountsCatalog>
 </AddAction></Structure></FMSaveAsXML>`

	migrate := func(withUser, granted bool) (bool, *migration.ConversionReport) {
		db := uniqueDB("f4b_migrest")
		h.createDatabase(db, "alice", "alice-secret")
		owner := h.login(db, "alice", "alice-secret")
		if withUser {
			rec := h.do(http.MethodPost, "/api/v1/security/users", owner,
				map[string]interface{}{"username": "dave", "password": "dave-secret-pw", "role": "user"}, nil)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		}
		if granted {
			rec := h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
				{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
			}}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		}
		var res struct {
			Report *migration.ConversionReport `json:"report"`
		}
		rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", owner, map[string]interface{}{"design_xml": design}, &res)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var listed struct {
			Privileges []schema.RolePrivileges `json:"privileges"`
		}
		rec = h.do(http.MethodGet, "/api/v1/security/privileges", owner, nil, &listed)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		for _, p := range listed.Privileges {
			if p.Role == "user" {
				return p.FMRest, res.Report
			}
		}
		t.Fatal("no user role listed")
		return false, nil
	}

	// The role's privilege items, whatever their category.
	userItems := func(report *migration.ConversionReport) (translated, manual []migration.ReportItem) {
		for _, it := range report.Translated {
			if it.Domain == "privilege" && it.TargetName == "user" {
				translated = append(translated, it)
			}
		}
		for _, it := range report.ManualActionRequired {
			if it.Domain == "privilege" && it.TargetName == "user" {
				manual = append(manual, it)
			}
		}
		return translated, manual
	}

	// One item per role: granted, with the set that gains it without
	// carrying it to review.
	granted, report := migrate(false, false)
	assert.True(t, granted, "the role only migrated accounts hold is granted fmrest")
	translated, manual := userItems(report)
	assert.Empty(t, translated)
	require.Len(t, manual, 1, "%+v", manual)
	assert.Contains(t, manual[0].Detail, `is granted to the role "user"`)
	assert.Contains(t, manual[0].Detail, `"Viewers" do not carry fmrest`)

	// A role that already holds it keeps it, and the same item.
	granted, report = migrate(true, true)
	assert.True(t, granted)
	translated, manual = userItems(report)
	assert.Empty(t, translated)
	require.Len(t, manual, 1, "%+v", manual)
	assert.Contains(t, manual[0].Detail, `is granted to the role "user"`)

	granted, report = migrate(true, false)
	assert.False(t, granted, "a role that already held an account is not granted fmrest")
	translated, manual = userItems(report)
	assert.Empty(t, translated, "nor is it reported as granted")
	require.Len(t, manual, 1, "one item says it was not granted, and why: %+v", manual)
	assert.Contains(t, manual[0].Detail, "which was not granted")
	assert.Contains(t, manual[0].Detail, "already had accounts")
	assert.Equal(t, "Clerks", manual[0].SourceName)
	assert.Equal(t, len(report.Translated), report.Counts.TranslatedCount)
}

// A layout migrated before objects inside containers were placed from them
// is repaired by migrating the file again into the same database: the
// migrated layout is replaced, not duplicated.
func TestAPI_MigratingAgainReplacesMisplacedNestedObjects(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	dbName := uniqueDB("f4b_mig_nest")
	h.createDatabase(dbName, "alice", "alice-secret")
	ownerTok := h.login(dbName, "alice", "alice-secret")

	// As FileMaker's Save as XML writes it: the group's member from the
	// group's corner.
	design := `<?xml version="1.0" encoding="UTF-8"?>
<FMSaveAsXML Has_DDR_INFO="True"><File name="Notes.fmp12">
  <BaseTableCatalog><BaseTable id="1" name="Notes"/></BaseTableCatalog>
  <FieldsForTables><FieldCatalog><BaseTableSourceReference id="1" name="Notes"/>
    <Field id="1" name="Body" datatype="text" fieldtype="normal"/></FieldCatalog></FieldsForTables>
  <RelationshipGraph><TableList><Table id="1" name="Notes" baseTable="Notes" baseTableId="1"/></TableList><RelationshipList/></RelationshipGraph>
  <LayoutCatalog><Layout id="1" name="Note" width="400">
    <TableOccurrenceReference id="1" name="Notes"/>
    <PartsList><Part type="Body"><Definition type="Body" kind="4" size="300" absolute="0"/><ObjectList>
      <LayoutObject type="Group"><UUID>00000000-0000-0000-0000-000000000001</UUID><Bounds top="100" left="50" bottom="140" right="250"/>
        <GroupedButton><ObjectList>
          <LayoutObject type="Text" name="caption"><UUID>00000000-0000-0000-0000-000000000002</UUID><Bounds top="5" left="5" bottom="25" right="105"/>
            <Text><StyledText><Data><![CDATA[Caption]]></Data></StyledText></Text>
          </LayoutObject>
        </ObjectList></GroupedButton>
      </LayoutObject>
    </ObjectList></Part></PartsList>
    <UUID>00000000-0000-0000-0000-0000000000ff</UUID>
  </Layout></LayoutCatalog>
</File></FMSaveAsXML>`
	migrate := func() {
		rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", ownerTok, map[string]interface{}{"design_xml": design}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	caption := func() (schema.LayoutMetadata, map[string]interface{}, map[string]interface{}) {
		var found []schema.LayoutMetadata
		for _, l := range h.observeLayouts(ownerTok) {
			if l.Name == "Note" {
				found = append(found, l)
			}
		}
		require.Len(t, found, 1)
		var def map[string]interface{}
		require.NoError(t, json.Unmarshal(found[0].Definition, &def))
		for _, o := range def["objects"].([]interface{}) {
			if m := o.(map[string]interface{}); m["name"] == "caption" {
				return found[0], def, m
			}
		}
		t.Fatalf("no caption: %s", found[0].Definition)
		return schema.LayoutMetadata{}, nil, nil
	}

	migrate()
	_, _, o := caption()
	assert.Equal(t, []interface{}{55.0, 105.0}, []interface{}{o["x"], o["y"]})

	// Where an earlier migration put it, at the corner of the layout.
	layout, def, o := caption()
	o["x"], o["y"] = 5.0, 5.0
	raw, err := json.Marshal(def)
	require.NoError(t, err)
	rec := h.do(http.MethodPut, "/api/v1/layouts/"+layout.ID, ownerTok, map[string]interface{}{"name": layout.Name, "definition": json.RawMessage(raw)}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	_, _, o = caption()
	require.Equal(t, []interface{}{5.0, 5.0}, []interface{}{o["x"], o["y"]})

	migrate()
	again, _, o := caption()
	assert.Equal(t, layout.ID, again.ID)
	assert.Equal(t, []interface{}{55.0, 105.0}, []interface{}{o["x"], o["y"]})
}
