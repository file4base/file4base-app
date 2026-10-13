package api_test

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_StructureClipboard(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	srcDB := uniqueDB("f4b_clip_src")
	dstDB := uniqueDB("f4b_clip_dst")
	h.createDatabase(srcDB, "alice", "alice-secret")
	h.createDatabase(dstDB, "bob", "bob-secret-pw")
	srcOwner := h.login(srcDB, "alice", "alice-secret")
	dstOwner := h.login(dstDB, "bob", "bob-secret-pw")

	// 1. Build a table, columns, a script, and a layout in srcDB.
	var srcTable schema.TableMetadata
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", srcOwner, map[string]interface{}{
		"display_name": "Customers",
	}, &srcTable)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var emailCol schema.ColumnMetadata
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+srcTable.ID+"/columns", srcOwner, map[string]interface{}{
		"name":         "email",
		"display_name": "Email",
		"field_type":   "text",
	}, &emailCol)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var phoneCol schema.ColumnMetadata
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+srcTable.ID+"/columns", srcOwner, map[string]interface{}{
		"name":         "phone",
		"display_name": "Phone",
		"field_type":   "text",
	}, &phoneCol)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var srcScript schema.ScriptMetadata
	rec = h.do(http.MethodPost, "/api/v1/schemas/scripts", srcOwner, map[string]interface{}{
		"name":          "Notify Customer",
		"context_table": "customers",
		"is_active":     true,
		"steps": []map[string]interface{}{
			{
				"sequence_idx": 1,
				"step_type":    "go_to_layout",
				"params":       map[string]interface{}{"layout_name": "Customer Card"},
				"is_enabled":   true,
			},
		},
	}, &srcScript)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var srcLayout schema.LayoutMetadata
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", srcOwner, map[string]interface{}{
		"name":                "Customer Card",
		"table_occurrence_id": srcTable.ID,
		"definition": map[string]interface{}{
			"table_occurrence": "customers",
			"objects": []map[string]interface{}{
				{"type": "field", "field_name": "email"},
				{"type": "field", "field_name": "missing_in_dst"},
				{
					"type": "button",
					"action": map[string]interface{}{
						"type":        "perform_script",
						"script_id":   srcScript.ID,
						"script_name": "Notify Customer",
					},
				},
			},
		},
	}, &srcLayout)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// 2. Copy the layout from srcDB and preview it in dstDB BEFORE the table or script exists.
	var layoutEnv schema.ClipboardEnvelope
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/copy", srcOwner, map[string]interface{}{
		"kind":      "layout",
		"layout_id": srcLayout.ID,
	}, &layoutEnv)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, schema.ClipboardFormat, layoutEnv.Format)
	assert.Equal(t, "layout", layoutEnv.Kind)

	var layoutPreview schema.ClipboardPreview
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/preview", dstOwner, map[string]interface{}{
		"envelope": layoutEnv,
	}, &layoutPreview)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, layoutPreview.HasCollision)
	// Should report both missing table_occurrence ("customers") and missing script ("Notify Customer").
	require.Len(t, layoutPreview.MissingDependencies, 2)

	// 3. Copy and paste the table from srcDB into dstDB, then handle collision with rename.
	var tableEnv schema.ClipboardEnvelope
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/copy", srcOwner, map[string]interface{}{
		"kind":     "table",
		"table_id": srcTable.ID,
	}, &tableEnv)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var pasteTableReport schema.ClipboardPasteReport
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/paste", dstOwner, map[string]interface{}{
		"envelope": tableEnv,
	}, &pasteTableReport)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Customers", pasteTableReport.CreatedName)

	// Pasting the same table again with on_collision=rename creates "Customers Copy".
	var pasteTableCopy schema.ClipboardPasteReport
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/paste", dstOwner, map[string]interface{}{
		"envelope":     tableEnv,
		"on_collision": "rename",
	}, &pasteTableCopy)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Customers Copy", pasteTableCopy.CreatedName)
	assert.Equal(t, "rename", pasteTableCopy.CollisionResolved)

	// 4. Copy a single column ("Phone") and paste it into "Customers Copy" (collision -> rename).
	var colEnv schema.ClipboardEnvelope
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/copy", srcOwner, map[string]interface{}{
		"kind":       "columns",
		"table_id":   srcTable.ID,
		"column_ids": []string{phoneCol.ID},
	}, &colEnv)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, colEnv.Columns, 1)

	var pasteColReport schema.ClipboardPasteReport
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/paste", dstOwner, map[string]interface{}{
		"envelope":        colEnv,
		"target_table_id": pasteTableCopy.CreatedID,
		"on_collision":    "rename",
	}, &pasteColReport)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Phone Copy", pasteColReport.CreatedName)

	// 5. Copy and paste the script into dstDB, then paste the layout and verify script_id is remapped.
	var scriptEnv schema.ClipboardEnvelope
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/copy", srcOwner, map[string]interface{}{
		"kind":      "script",
		"script_id": srcScript.ID,
	}, &scriptEnv)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var pasteScriptReport schema.ClipboardPasteReport
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/paste", dstOwner, map[string]interface{}{
		"envelope": scriptEnv,
	}, &pasteScriptReport)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Notify Customer", pasteScriptReport.CreatedName)

	// Now paste the layout into dstDB: table_occurrence and script exist, only "missing_in_dst" field is missing.
	var pasteLayoutReport schema.ClipboardPasteReport
	rec = h.do(http.MethodPost, "/api/v1/solutions/clipboard/paste", dstOwner, map[string]interface{}{
		"envelope": layoutEnv,
	}, &pasteLayoutReport)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Customer Card", pasteLayoutReport.CreatedName)
	require.Len(t, pasteLayoutReport.MissingDependencies, 1)
	assert.Equal(t, "field", pasteLayoutReport.MissingDependencies[0].Kind)
	assert.Equal(t, "missing_in_dst", pasteLayoutReport.MissingDependencies[0].Name)

	var dstLayout schema.LayoutMetadata
	rec = h.do(http.MethodGet, "/api/v1/schemas/layouts/"+pasteLayoutReport.CreatedID, dstOwner, nil, &dstLayout)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, string(dstLayout.Definition), pasteScriptReport.CreatedID)
}
