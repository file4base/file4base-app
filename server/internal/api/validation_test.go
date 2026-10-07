package api_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// Field validation rules saved in the Fields dialog are enforced on record
// writes (#15).
func TestAPI_FieldValidationRulesAreEnforced(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_valid")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	var tbl idOnly
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Orders", "custom_name": "orders"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	addField := func(body map[string]interface{}) string {
		var col idOnly
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token, body, &col)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		return col.ID
	}
	setRules := func(colID, rules string) int {
		return h.do(http.MethodPut, "/api/v1/schemas/tables/"+tbl.ID+"/columns/"+colID, token, map[string]interface{}{"display_name": "x", "validation_rules": rules}, nil).Code
	}
	post := func(body map[string]interface{}) (int, string) {
		rec := h.do(http.MethodPost, "/api/v1/data/orders", token, body, nil)
		return rec.Code, rec.Body.String()
	}

	ref := addField(map[string]interface{}{"name": "reference", "display_name": "Reference", "field_type": "TEXT", "is_nullable": true})
	require.Equal(t, http.StatusOK, setRules(ref, `{"not_empty":true,"not_empty_timing":"always","unique":true,"max_length_enabled":true,"max_length":3}`))

	// The issue's reproduction: every invalid write is rejected
	for _, body := range []map[string]interface{}{
		{"id": "missing"},
		{"id": "empty", "reference": ""},
		{"id": "blank", "reference": "   "},
		{"id": "too-long", "reference": "ABCDE"},
	} {
		code, out := post(body)
		assert.Equal(t, http.StatusUnprocessableEntity, code, "%v: %s", body, out)
	}
	code, out := post(map[string]interface{}{"id": "duplicate1", "reference": "ABC"})
	require.Equal(t, http.StatusCreated, code, out)
	code, out = post(map[string]interface{}{"id": "duplicate2", "reference": "ABC"})
	assert.Equal(t, http.StatusUnprocessableEntity, code, out)
	assert.Contains(t, out, "reference")
	assert.Contains(t, out, "unique")
	rec = h.do(http.MethodPut, "/api/v1/data/orders/duplicate1", token, map[string]string{"reference": ""}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "an update that empties the field is rejected")
	rec = h.do(http.MethodPut, "/api/v1/data/orders/duplicate1", token, map[string]string{"reference": "ABC"}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, "keeping its own value is not a duplicate: %s", rec.Body.String())

	var rows []map[string]interface{}
	h.do(http.MethodGet, "/api/v1/data/orders", token, nil, &rows)
	require.Len(t, rows, 1, "no invalid record was committed")
	assert.Equal(t, "ABC", rows[0]["reference"])

	// Concurrent duplicates: the unique index lets at most one through
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = post(map[string]interface{}{"reference": "XYZ"})
		}(i)
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		} else {
			assert.Equal(t, http.StatusUnprocessableEntity, c)
		}
	}
	assert.Equal(t, 1, created, "exactly one of the concurrent writes succeeds")

	// A unique rule cannot be enabled while records share a value
	note := addField(map[string]interface{}{"name": "note", "display_name": "Note", "field_type": "TEXT", "is_nullable": true})
	rec = h.do(http.MethodPut, "/api/v1/data/orders/duplicate1", token, map[string]string{"note": "same"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	code, out = post(map[string]interface{}{"reference": "QQ", "note": "same"})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, http.StatusUnprocessableEntity, setRules(note, `{"unique":true}`))

	// Malformed rules are rejected when saved
	assert.Equal(t, http.StatusUnprocessableEntity, setRules(note, `not json`))
	assert.Equal(t, http.StatusUnprocessableEntity, setRules(note, `{"strict_type_enabled":true,"strict_type":"Roman Numerals"}`))

	// Strict type, range, existing value and a custom message
	qty := addField(map[string]interface{}{"name": "qty", "display_name": "Qty", "field_type": "TEXT", "is_nullable": true})
	require.Equal(t, http.StatusOK, setRules(qty, `{"strict_type_enabled":true,"strict_type":"Numeric Only","range_enabled":true,"range_min":"1","range_max":"10","custom_message_enabled":true,"custom_message":"Qty must be 1 to 10"}`))
	for _, v := range []interface{}{"abc", "0", "11", 11.5} {
		code, out = post(map[string]interface{}{"reference": "Q1", "qty": v})
		assert.Equal(t, http.StatusUnprocessableEntity, code, "%v", v)
		assert.Contains(t, out, "Qty must be 1 to 10")
	}
	code, out = post(map[string]interface{}{"reference": "Q1", "qty": 7})
	assert.Equal(t, http.StatusCreated, code, out)

	day := addField(map[string]interface{}{"name": "day", "display_name": "Day", "field_type": "TEXT", "is_nullable": true})
	require.Equal(t, http.StatusOK, setRules(day, `{"strict_type_enabled":true,"strict_type":"Date"}`))
	code, _ = post(map[string]interface{}{"reference": "D1", "day": "2026-02-30"})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	code, out = post(map[string]interface{}{"reference": "D1", "day": "2026-02-28"})
	assert.Equal(t, http.StatusCreated, code, out)

	parent := addField(map[string]interface{}{"name": "parent_ref", "display_name": "Parent", "field_type": "TEXT", "is_nullable": true})
	require.Equal(t, http.StatusOK, setRules(parent, `{"existing_value":true}`))
	code, _ = post(map[string]interface{}{"reference": "E1", "parent_ref": "nope"})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	code, out = post(map[string]interface{}{"reference": "E2", "parent_ref": "Q1", "note": "x"})
	assert.Equal(t, http.StatusUnprocessableEntity, code, "no other record has parent_ref Q1 yet: %s", out)
}

// Rules apply to data import when their timing is "always", and a broken
// rule rolls back the whole import; "only during data entry" rules do not
// apply to imports (#15).
func TestAPI_ValidationTimingOnImport(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_valid_imp")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	var tbl idOnly
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Items", "custom_name": "items"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for _, col := range []map[string]interface{}{
		{"name": "always_req", "display_name": "A", "field_type": "TEXT", "is_nullable": true, "validation_rules": `{"not_empty":true,"not_empty_timing":"always"}`},
		{"name": "entry_req", "display_name": "E", "field_type": "TEXT", "is_nullable": true, "validation_rules": `{"not_empty":true,"not_empty_timing":"entry"}`},
	} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token, col, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	importRows := func(rows ...map[string]interface{}) int {
		data, err := msgpack.Marshal(schema.DatabaseDataBundle{Format: schema.DataFormat, Version: schema.DataVersion,
			TablesData: map[string][]map[string]interface{}{"items": rows}})
		require.NoError(t, err)
		return h.postBytes("/api/v1/solutions/import-data", token, data, nil).Code
	}
	assert.Equal(t, http.StatusOK, importRows(map[string]interface{}{"id": "a", "always_req": "x"}), "entry-only rules do not apply to imports")
	assert.Equal(t, http.StatusBadRequest, importRows(map[string]interface{}{"id": "b", "always_req": "x", "entry_req": "y"}, map[string]interface{}{"id": "c"}))
	var rows []interface{}
	h.do(http.MethodGet, "/api/v1/data/items", token, nil, &rows)
	assert.Len(t, rows, 1, "the failed import was rolled back entirely")

	// Data entry applies both
	rec = h.do(http.MethodPost, "/api/v1/data/items", token, map[string]string{"always_req": "x"}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	_ = context.Background()
}

// The data API could only order by one field (#35). `sort` may be repeated or
// comma-separated and a leading "-" sorts that field descending.
func TestParseSortOrderReadsEveryLevel(t *testing.T) {
	order := api.ParseSortOrderForTest([]string{"company,-fee_paid", " last_name "})
	if len(order) != 3 {
		t.Fatalf("got %d levels, want 3: %+v", len(order), order)
	}
	if order[0].Field != "company" || order[0].Descending {
		t.Errorf("level 1 = %+v, want company ascending", order[0])
	}
	if order[1].Field != "fee_paid" || !order[1].Descending {
		t.Errorf("level 2 = %+v, want fee_paid descending", order[1])
	}
	if order[2].Field != "last_name" || order[2].Descending {
		t.Errorf("level 3 = %+v, want last_name ascending", order[2])
	}
}

func TestParseSortOrderIgnoresEmptyEntries(t *testing.T) {
	if got := api.ParseSortOrderForTest([]string{"", " , ", "-"}); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
	if got := api.ParseSortOrderForTest(nil); len(got) != 0 {
		t.Fatalf("got %+v, want nothing", got)
	}
}
