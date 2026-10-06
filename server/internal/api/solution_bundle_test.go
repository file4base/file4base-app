package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// postBytes sends a MessagePack body and decodes a JSON answer into out.
func (h *apiHarness) postBytes(path, token string, body []byte, out interface{}) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-msgpack")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		require.NoError(h.t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec
}

type idOnly struct {
	ID string `json:"id"`
}

type solutionSource struct {
	customersTable, invoicesTable string
	customOcc                     string
	scriptID                      string
	customersLayout, invoicesLay  string
}

// buildSourceSolution creates a small but complete solution: two tables with
// field options, a custom occurrence, a relationship, two layouts (one whose
// button and trigger run a script), a script with a nested step and a
// disabled user with layout permissions.
func buildSourceSolution(t *testing.T, h *apiHarness, token string) solutionSource {
	var src solutionSource
	var tbl idOnly
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Customers", "custom_name": "customers"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	src.customersTable = tbl.ID
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Invoices", "custom_name": "invoices"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	src.invoicesTable = tbl.ID

	for _, col := range []map[string]interface{}{
		{"name": "customer_ref", "display_name": "Customer", "field_type": "TEXT", "is_nullable": true},
		{"name": "status", "display_name": "Status", "field_type": "TEXT", "is_nullable": true, "default_value": `{"data_enabled":true,"data_value":"Open"}`, "validation_rules": `{"not_empty":true}`},
		{"name": "scan", "display_name": "Scan", "field_type": "CONTAINER", "is_nullable": true},
	} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+src.invoicesTable+"/columns", token, col, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	var occs []struct {
		ID, Name    string
		BaseTableID string `json:"base_table_id"`
	}
	rec = h.do(http.MethodGet, "/api/v1/schemas/occurrences", token, nil, &occs)
	require.Equal(t, http.StatusOK, rec.Code)
	occByName := map[string]string{}
	for _, o := range occs {
		occByName[o.Name] = o.ID
	}
	var tables []struct {
		ID      string
		Name    string
		Columns []struct{ ID, Name string }
	}
	h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables)
	colID := map[string]string{}
	for _, tb := range tables {
		for _, c := range tb.Columns {
			colID[tb.Name+"."+c.Name] = c.ID
		}
	}

	var occ idOnly
	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", token, map[string]interface{}{"base_table_id": src.invoicesTable, "name": "InvoicesCustom", "x_pos": 40, "y_pos": 60}, &occ)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	src.customOcc = occ.ID
	rec = h.do(http.MethodPost, "/api/v1/schemas/relationships", token, map[string]interface{}{
		"name": "invoice_customer", "left_occurrence_id": occByName["invoices"], "left_column_id": colID["invoices.customer_ref"],
		"right_occurrence_id": occByName["customers"], "right_column_id": colID["customers.id"], "operator": "=",
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var script idOnly
	rec = h.do(http.MethodPost, "/api/v1/schemas/scripts", token, map[string]interface{}{
		"name": "Archive", "is_active": true,
		"steps": []map[string]interface{}{
			{"id": "s-if", "sequence_idx": 1, "step_type": "if", "params": map[string]interface{}{"calc": "1"}, "is_enabled": true},
			{"id": "s-dlg", "sequence_idx": 2, "step_type": "show_dialog", "params": map[string]interface{}{"title": "Done", "count": 3}, "is_enabled": false, "parent_step_id": "s-if"},
			{"id": "s-end", "sequence_idx": 3, "step_type": "end_if", "params": map[string]interface{}{}, "is_enabled": true},
		},
	}, &script)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	src.scriptID = script.ID

	var lay idOnly
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", token, map[string]interface{}{
		"name": "Customers Layout", "table_occurrence_id": occByName["customers"], "definition": map[string]interface{}{"objects": []interface{}{}},
	}, &lay)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	src.customersLayout = lay.ID
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", token, map[string]interface{}{
		"name": "Invoices Layout", "table_occurrence_id": occByName["invoices"],
		"definition": map[string]interface{}{
			"width":           900,
			"on_layout_enter": map[string]interface{}{"type": "perform_script", "script_id": script.ID, "script_name": "Archive"},
			"objects": []interface{}{map[string]interface{}{
				"id": "btn", "type": "button", "x": 10, "y": 10.5, "width": 120, "height": 30, "tab_order": 1,
				"action": map[string]interface{}{"type": "perform_script", "script_id": script.ID, "script_name": "Archive"},
			}},
		},
	}, &lay)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	src.invoicesLay = lay.ID

	var bob idOnly
	rec = h.do(http.MethodPost, "/api/v1/security/users", token, map[string]interface{}{"username": "bob", "password": "bob-secret-1", "role": "user", "is_active": false}, &bob)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+bob.ID+"/permissions", token, map[string]interface{}{"permissions": []map[string]string{
		{"layout_id": src.customersLayout, "access_level": "none"},
		{"layout_id": src.invoicesLay, "access_level": "read_only"},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return src
}

func exportSolution(t *testing.T, h *apiHarness, token string, body interface{}) []byte {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/solutions/export", token, body, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.Bytes()
}

// A solution exported from one database restores into another with its
// relationships, layout bindings, scripts and accounts intact (#3, #11, #17),
// carries no secrets (#10) and can be imported again without duplicates.
func TestAPI_SolutionRoundTripAcrossDatabases(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	srcDB, dstDB := uniqueDB("f4b_sol_src"), uniqueDB("f4b_sol_dst")
	h.createDatabase(srcDB, "alice", "alice-secret")
	h.createDatabase(dstDB, "olivia", "olivia-secret")
	srcToken := h.login(srcDB, "alice", "alice-secret")
	dstToken := h.login(dstDB, "olivia", "olivia-secret")
	src := buildSourceSolution(t, h, srcToken)

	file := exportSolution(t, h, srcToken, map[string]interface{}{
		"solution_name": "Invoices Pro",
		"file_options":  map[string]interface{}{"auto_login_enabled": true, "default_username": "alice", "default_password": "alice-secret"},
		"page_setup":    map[string]interface{}{"paper": "A4"},
	})

	// No secret leaves the server (#10)
	assert.NotContains(t, string(file), "alice-secret")
	assert.NotContains(t, string(file), "bob-secret")
	assert.NotContains(t, strings.ToLower(string(file)), "password")
	var decoded map[string]interface{}
	require.NoError(t, msgpack.Unmarshal(file, &decoded))
	assert.Equal(t, "2.0", decoded["version"])
	assert.IsType(t, "", decoded["exported_at"], "timestamps are RFC 3339 strings")
	assert.Equal(t, "A4", decoded["page_setup"].(map[string]interface{})["paper"])
	assert.Equal(t, "alice", decoded["database_connection"].(map[string]interface{})["user"])

	var report map[string]interface{}
	rec := h.postBytes("/api/v1/solutions/import", dstToken, file, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 2, report["tables_created"])
	assert.EqualValues(t, 1, report["occurrences_created"], "the custom occurrence; defaults come with the tables")
	assert.EqualValues(t, 1, report["relationships_created"])
	assert.EqualValues(t, 2, report["layouts_created"])
	assert.EqualValues(t, 1, report["scripts_created"])
	assert.EqualValues(t, 2, report["accounts_created"], "alice and bob; olivia already exists")
	assert.ElementsMatch(t, []interface{}{"alice", "bob"}, report["accounts_pending_password"])

	verifyRestoredSolution(t, h, dstToken)

	// Importing the same file again changes nothing structural
	rec = h.postBytes("/api/v1/solutions/import", dstToken, file, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, k := range []string{"tables_created", "columns_created", "occurrences_created", "relationships_created", "layouts_created", "scripts_created", "accounts_created"} {
		assert.EqualValues(t, 0, report[k], k)
	}
	assert.EqualValues(t, 2, report["layouts_updated"])
	assert.EqualValues(t, 1, report["scripts_updated"])
	verifyRestoredSolution(t, h, dstToken)
	_ = src
}

func verifyRestoredSolution(t *testing.T, h *apiHarness, token string) {
	t.Helper()
	var tables []struct {
		ID      string
		Name    string
		Columns []struct {
			ID, Name        string
			DefaultValue    *string `json:"default_value"`
			ValidationRules *string `json:"validation_rules"`
		}
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables).Code)
	tableName := map[string]string{}
	colByID := map[string]string{}
	for _, tb := range tables {
		tableName[tb.ID] = tb.Name
		for _, c := range tb.Columns {
			colByID[c.ID] = tb.Name + "." + c.Name
			if tb.Name == "invoices" && c.Name == "status" {
				require.NotNil(t, c.DefaultValue)
				assert.JSONEq(t, `{"data_enabled":true,"data_value":"Open"}`, *c.DefaultValue)
				require.NotNil(t, c.ValidationRules)
			}
		}
	}
	assert.ElementsMatch(t, []string{"customers", "invoices"}, mapValues(tableName))

	var occs []struct {
		ID, Name    string
		BaseTableID string `json:"base_table_id"`
	}
	h.do(http.MethodGet, "/api/v1/schemas/occurrences", token, nil, &occs)
	occTable := map[string]string{}
	occByName := map[string]string{}
	for _, o := range occs {
		occTable[o.ID] = tableName[o.BaseTableID]
		occByName[o.Name] = tableName[o.BaseTableID]
	}
	assert.Equal(t, map[string]string{"customers": "customers", "invoices": "invoices", "InvoicesCustom": "invoices"}, occByName)

	var rels []struct {
		LeftOccurrenceID  string `json:"left_occurrence_id"`
		LeftColumnID      string `json:"left_column_id"`
		RightOccurrenceID string `json:"right_occurrence_id"`
		RightColumnID     string `json:"right_column_id"`
	}
	h.do(http.MethodGet, "/api/v1/schemas/relationships", token, nil, &rels)
	require.Len(t, rels, 1)
	assert.Equal(t, "invoices", occTable[rels[0].LeftOccurrenceID])
	assert.Equal(t, "invoices.customer_ref", colByID[rels[0].LeftColumnID])
	assert.Equal(t, "customers", occTable[rels[0].RightOccurrenceID])
	assert.Equal(t, "customers.id", colByID[rels[0].RightColumnID])

	var scripts []struct {
		ID    string
		Name  string
		Steps []struct {
			ID           string
			SequenceIdx  int             `json:"sequence_idx"`
			StepType     string          `json:"step_type"`
			Params       json.RawMessage `json:"params"`
			IsEnabled    bool            `json:"is_enabled"`
			ParentStepID *string         `json:"parent_step_id"`
		}
	}
	h.do(http.MethodGet, "/api/v1/schemas/scripts", token, nil, &scripts)
	require.Len(t, scripts, 1)
	sc := scripts[0]
	assert.Equal(t, "Archive", sc.Name)
	require.Len(t, sc.Steps, 3)
	assert.Equal(t, []string{"if", "show_dialog", "end_if"}, []string{sc.Steps[0].StepType, sc.Steps[1].StepType, sc.Steps[2].StepType})
	assert.False(t, sc.Steps[1].IsEnabled)
	require.NotNil(t, sc.Steps[1].ParentStepID)
	assert.Equal(t, sc.Steps[0].ID, *sc.Steps[1].ParentStepID, "the parent reference follows the remapped step id")
	assert.JSONEq(t, `{"title":"Done","count":3}`, string(sc.Steps[1].Params))

	var layouts []struct {
		ID                string          `json:"id"`
		Name              string          `json:"name"`
		TableOccurrenceID string          `json:"table_occurrence_id"`
		Definition        json.RawMessage `json:"definition"`
	}
	h.do(http.MethodGet, "/api/v1/schemas/layouts", token, nil, &layouts)
	require.Len(t, layouts, 2)
	layoutID := map[string]string{}
	for _, l := range layouts {
		layoutID[l.Name] = l.ID
		switch l.Name {
		case "Customers Layout":
			assert.Equal(t, "customers", occTable[l.TableOccurrenceID])
		case "Invoices Layout":
			assert.Equal(t, "invoices", occTable[l.TableOccurrenceID], "each layout stays on its own table (#3)")
			var def struct {
				Width         interface{} `json:"width"`
				OnLayoutEnter struct {
					ScriptID string `json:"script_id"`
				} `json:"on_layout_enter"`
				Objects []struct {
					Y        float64 `json:"y"`
					TabOrder int     `json:"tab_order"`
					Action   struct {
						ScriptID string `json:"script_id"`
					} `json:"action"`
				} `json:"objects"`
			}
			require.NoError(t, json.Unmarshal(l.Definition, &def))
			assert.Equal(t, sc.ID, def.OnLayoutEnter.ScriptID, "triggers point to the restored script")
			require.Len(t, def.Objects, 1)
			assert.Equal(t, sc.ID, def.Objects[0].Action.ScriptID, "buttons point to the restored script")
			assert.Equal(t, 10.5, def.Objects[0].Y)
			assert.Equal(t, 1, def.Objects[0].TabOrder)
		}
	}

	var users []struct {
		ID       string
		Username string
		Role     string
		IsActive bool `json:"is_active"`
	}
	h.do(http.MethodGet, "/api/v1/security/users", token, nil, &users)
	byName := map[string]int{}
	for i, u := range users {
		byName[u.Username] = i
	}
	require.Contains(t, byName, "bob")
	bob := users[byName["bob"]]
	assert.Equal(t, "user", bob.Role)
	assert.False(t, bob.IsActive, "a restored account stays disabled (#11)")
	require.Contains(t, byName, "alice")
	assert.False(t, users[byName["alice"]].IsActive, "missing accounts are created disabled, pending a password")
	assert.True(t, users[byName["olivia"]].IsActive, "existing accounts are unchanged")

	var perms []struct {
		LayoutID    string `json:"layout_id"`
		AccessLevel string `json:"access_level"`
	}
	h.do(http.MethodGet, "/api/v1/security/users/"+bob.ID+"/permissions", token, nil, &perms)
	got := map[string]string{}
	for _, p := range perms {
		got[p.LayoutID] = p.AccessLevel
	}
	assert.Equal(t, map[string]string{layoutID["Customers Layout"]: "none", layoutID["Invoices Layout"]: "read_only"}, got)

	// Restored accounts cannot sign in: no password was carried over
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": dbOfToken(t, h, token), "username": "bob", "password": "bob-secret-1"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func dbOfToken(t *testing.T, h *apiHarness, token string) string {
	var sess struct {
		Database string `json:"database"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/auth/session", token, nil, &sess).Code)
	return sess.Database
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// A bundle with a dangling reference is rejected before anything is written;
// a write that fails part-way removes what the import had created (#3).
func TestAPI_SolutionImportValidatesAndRollsBack(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_sol_rb")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")
	ctx := context.Background()

	tableCount := func() int {
		var tables []interface{}
		h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables)
		return len(tables)
	}

	idCol := func(id string) schema.BundleColumn {
		return schema.BundleColumn{ID: id, Name: "id", DisplayName: "ID", FieldType: "TEXT", IsPrimaryKey: true}
	}
	dangling := schema.SolutionBundle{
		Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Broken",
		Tables:           []schema.BundleTable{{ID: "t1", Name: "fresh", DisplayName: "Fresh", Columns: []schema.BundleColumn{idCol("c1")}}},
		TableOccurrences: []schema.BundleOccurrence{{ID: "o1", BaseTableID: "t1", Name: "fresh"}},
		Layouts:          []schema.BundleLayout{{ID: "l1", Name: "Fresh Layout", TableOccurrenceID: "missing-occurrence", Definition: map[string]interface{}{}}},
	}
	data, err := msgpack.Marshal(dangling)
	require.NoError(t, err)
	rec := h.postBytes("/api/v1/solutions/import", token, data, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "table occurrence that is not in the file")
	assert.Equal(t, 0, tableCount(), "validation happens before any write")

	// "things" exists in the catalog, but its physical table already has a
	// column the catalog does not know: adding that field fails after
	// "fresh" was created, so "fresh" must disappear again.
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Things", "custom_name": "things"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	driver, err := h.mgr.DriverFor(ctx, db)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(ctx, `ALTER TABLE things ADD COLUMN extra TEXT`)
	require.NoError(t, err)

	failing := schema.SolutionBundle{
		Format: schema.SolutionFormat, Version: schema.SolutionVersion, SolutionName: "Partial",
		Tables: []schema.BundleTable{
			{ID: "t1", Name: "fresh", DisplayName: "Fresh", Columns: []schema.BundleColumn{idCol("c1"), {ID: "c2", Name: "note", DisplayName: "Note", FieldType: "TEXT", IsNullable: true}}},
			{ID: "t2", Name: "things", DisplayName: "Things", Columns: []schema.BundleColumn{idCol("c3"), {ID: "c4", Name: "extra", DisplayName: "Extra", FieldType: "TEXT", IsNullable: true}}},
		},
		TableOccurrences: []schema.BundleOccurrence{{ID: "o1", BaseTableID: "t1", Name: "fresh"}, {ID: "o2", BaseTableID: "t2", Name: "things"}},
	}
	data, err = msgpack.Marshal(failing)
	require.NoError(t, err)
	rec = h.postBytes("/api/v1/solutions/import", token, data, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "rolled back")
	assert.Equal(t, 1, tableCount(), "the table created by the failed import was removed")
	var exists bool
	require.NoError(t, driver.DB().QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'fresh')`).Scan(&exists))
	assert.False(t, exists, "and so was its physical table")

	// Not a solution file at all
	rec = h.postBytes("/api/v1/solutions/import", token, []byte{0x81, 0xa6, 'f', 'o', 'r', 'm', 'a', 't', 0xa3, 'x', 'y', 'z'}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Solution files exported by servers before 2.0 (Go field names, MessagePack
// timestamps) are still restored.
func TestAPI_SolutionImportReadsLegacyServerExports(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_sol_legacy")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	now := time.Now().UTC()
	legacy := struct {
		Format           string                        `msgpack:"format"`
		Version          string                        `msgpack:"version"`
		SolutionName     string                        `msgpack:"solution_name"`
		Tables           []schema.TableMetadata        `msgpack:"tables"`
		TableOccurrences []schema.TableOccurrence      `msgpack:"table_occurrences"`
		Relationships    []schema.RelationshipMetadata `msgpack:"relationships"`
		Layouts          []schema.LayoutMetadata       `msgpack:"layouts"`
		CreatedAt        time.Time                     `msgpack:"created_at"`
	}{
		Format: schema.SolutionFormat, Version: "1.0", SolutionName: "Old", CreatedAt: now,
		Tables: []schema.TableMetadata{{ID: "t1", Name: "notes", DisplayName: "Notes", CreatedAt: now, Columns: []schema.ColumnMetadata{
			{ID: "c1", TableID: "t1", Name: "id", DisplayName: "ID", FieldType: "TEXT", IsPrimaryKey: true, CreatedAt: now},
			{ID: "c2", TableID: "t1", Name: "body", DisplayName: "Body", FieldType: "TEXT", IsNullable: true, CreatedAt: now},
		}}},
		TableOccurrences: []schema.TableOccurrence{{ID: "o1", BaseTableID: "t1", Name: "notes", CreatedAt: now}},
		Layouts:          []schema.LayoutMetadata{{ID: "l1", Name: "Notes Layout", TableOccurrenceID: "o1", Definition: json.RawMessage(`{"objects":[]}`), CreatedAt: now}},
	}
	data, err := msgpack.Marshal(legacy)
	require.NoError(t, err)
	var report map[string]interface{}
	rec := h.postBytes("/api/v1/solutions/import", token, data, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 1, report["tables_created"])
	assert.EqualValues(t, 1, report["columns_created"])
	assert.EqualValues(t, 1, report["layouts_created"])
}

// CONTAINER values survive the data API and the data bundle byte for byte
// (#14), and a data import either restores every record or none (#4).
func TestAPI_DataBundleKeepsBinaryAndIsAtomic(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	srcDB, dstDB := uniqueDB("f4b_data_src"), uniqueDB("f4b_data_dst")
	h.createDatabase(srcDB, "alice", "alice-secret")
	h.createDatabase(dstDB, "alice", "alice-secret")
	src := h.login(srcDB, "alice", "alice-secret")
	dst := h.login(dstDB, "alice", "alice-secret")

	for _, token := range []string{src, dst} {
		var tbl idOnly
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Attachments", "custom_name": "attachments"}, &tbl)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		for _, col := range []map[string]interface{}{
			{"name": "payload", "display_name": "Payload", "field_type": "CONTAINER", "is_nullable": true},
			{"name": "amount", "display_name": "Amount", "field_type": "NUMBER", "is_nullable": true},
			{"name": "note", "display_name": "Note", "field_type": "TEXT", "is_nullable": true},
		} {
			rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token, col, nil)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		}
	}

	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	binary := []byte{0, 255, 128, 92, 10, 13, 0, 1}
	seed, err := msgpack.Marshal(schema.DatabaseDataBundle{
		Format: schema.DataFormat, Version: schema.DataVersion, DatabaseName: srcDB,
		TablesData: map[string][]map[string]interface{}{"attachments": {
			{"id": "ascii", "payload": []byte("plain text"), "note": "café ✓"},
			{"id": "binary", "payload": binary, "amount": 12.5},
			{"id": "all", "payload": allBytes},
			{"id": "empty", "payload": []byte{}},
		}},
	})
	require.NoError(t, err)
	var report map[string]interface{}
	rec := h.postBytes("/api/v1/solutions/import-data", src, seed, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 4, report["records_inserted"])
	assert.EqualValues(t, 4, report["records_count"])

	// JSON API: CONTAINER values are base64, exactly reversible
	var row map[string]interface{}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/attachments/binary", src, nil, &row).Code)
	assert.Equal(t, "AP+AXAoNAAE=", row["payload"])
	rec = h.do(http.MethodPost, "/api/v1/data/attachments", src, map[string]string{"id": "via-json", "payload": "AP+AXAoNAAE="}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/attachments/via-json", src, nil, &row).Code)
	assert.Equal(t, "AP+AXAoNAAE=", row["payload"])
	rec = h.do(http.MethodPost, "/api/v1/data/attachments", src, map[string]string{"id": "bad", "payload": "not base64!"}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = h.do(http.MethodPut, "/api/v1/data/attachments/ascii", src, map[string]string{"payload": "%%%"}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// The export is standard MessagePack with real binary values
	req := httptest.NewRequest(http.MethodGet, "/api/v1/solutions/export-data", nil)
	req.Header.Set("Authorization", "Bearer "+src)
	exp := httptest.NewRecorder()
	h.router.ServeHTTP(exp, req)
	require.Equal(t, http.StatusOK, exp.Code, exp.Body.String())
	var exported schema.DatabaseDataBundle
	require.NoError(t, msgpack.Unmarshal(exp.Body.Bytes(), &exported))
	byID := map[string]map[string]interface{}{}
	for _, r := range exported.TablesData["attachments"] {
		byID[r["id"].(string)] = r
	}
	assert.Equal(t, binary, byID["binary"]["payload"])
	assert.Equal(t, allBytes, byID["all"]["payload"])
	assert.Equal(t, "café ✓", byID["ascii"]["note"])

	// ...and restores byte for byte elsewhere
	rec = h.postBytes("/api/v1/solutions/import-data", dst, exp.Body.Bytes(), &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 5, report["records_inserted"])
	for id, want := range map[string]string{"binary": "AP+AXAoNAAE=", "empty": ""} {
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/attachments/"+id, dst, nil, &row).Code)
		assert.Equal(t, want, row["payload"], id)
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/attachments/all", dst, nil, &row).Code)
	assert.Equal(t, "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8gISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0+P0BBQkNERUZHSElKS0xNTk9QUVJTVFVWV1hZWltcXV5fYGFiY2RlZmdoaWprbG1ub3BxcnN0dXZ3eHl6e3x9fn+AgYKDhIWGh4iJiouMjY6PkJGSk5SVlpeYmZqbnJ2en6ChoqOkpaanqKmqq6ytrq+wsbKztLW2t7i5uru8vb6/wMHCw8TFxsfIycrLzM3Oz9DR0tPU1dbX2Nna29zd3t/g4eLj5OXm5+jp6uvs7e7v8PHy8/T19vf4+fr7/P3+/w==", row["payload"])

	// Importing the same file again inserts nothing and says so
	rec = h.postBytes("/api/v1/solutions/import-data", dst, exp.Body.Bytes(), &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 0, report["records_inserted"])
	assert.EqualValues(t, 5, report["records_skipped"])

	// One bad record rolls the whole import back (#4)
	countRows := func(token string) int {
		var rows []interface{}
		h.do(http.MethodGet, "/api/v1/data/attachments?limit=1000", token, nil, &rows)
		return len(rows)
	}
	before := countRows(dst)
	bad, err := msgpack.Marshal(schema.DatabaseDataBundle{
		Format: schema.DataFormat, Version: schema.DataVersion,
		TablesData: map[string][]map[string]interface{}{
			"attachments": {{"id": "new-valid", "amount": 1}, {"id": "new-invalid", "amount": "NOT_A_NUMBER"}},
			"unknown":     {{"id": "x"}},
		},
	})
	require.NoError(t, err)
	rec = h.postBytes("/api/v1/solutions/import-data", dst, bad, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing was imported")
	assert.Equal(t, before, countRows(dst), "the valid record before the failing one was rolled back")

	// Tables the destination does not have are reported, not silently dropped
	partial, err := msgpack.Marshal(schema.DatabaseDataBundle{
		Format: schema.DataFormat, Version: schema.DataVersion,
		TablesData: map[string][]map[string]interface{}{"attachments": {{"id": "new-valid"}}, "unknown": {{"id": "x"}}},
	})
	require.NoError(t, err)
	rec = h.postBytes("/api/v1/solutions/import-data", dst, partial, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 1, report["records_inserted"])
	assert.Equal(t, []interface{}{"unknown"}, report["skipped_tables"])
}

// Files written by the Flutter client are restored by the server: the
// initial file of a new database and a 1.0 client file (#5).
func TestAPI_SolutionImportReadsClientFiles(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_sol_client")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	for _, name := range []string{"client_new_database_v2.f4p", "client_legacy_v1.f4p"} {
		data, err := os.ReadFile(filepath.Join("..", "schema", "testdata", name))
		require.NoError(t, err)
		var report map[string]interface{}
		rec := h.postBytes("/api/v1/solutions/import", token, data, &report)
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", name, rec.Body.String())
		assert.EqualValues(t, 0, report["accounts_created"], "%s: the owner already exists and is left unchanged", name)
		if name == "client_legacy_v1.f4p" {
			assert.EqualValues(t, 1, report["tables_created"])
			assert.EqualValues(t, 1, report["columns_created"])
			assert.EqualValues(t, 1, report["layouts_created"])
		}
	}
	// The signed-in owner keeps their password
	h.login(db, "alice", "alice-secret")
}
