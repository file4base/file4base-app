package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPI_SavedFinds covers naming a set of find requests and running it
// again (#34): what is stored, who may change it, and what is refused.
func TestAPI_SavedFinds(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_finds")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
		map[string]string{"display_name": "Customers", "custom_name": "customers"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for _, field := range []string{"city", "status"} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner,
			map[string]interface{}{"display_name": field, "name": field, "field_type": "TEXT"}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	for _, row := range []map[string]string{
		{"city": "New York", "status": "Continuing"},
		{"city": "London", "status": "New"},
		{"city": "Paris", "status": "New"},
	} {
		require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/customers", owner, row, nil).Code)
	}

	// "In New York or in London, omitting the new ones" — the find of the
	// tutorial, saved under a name.
	body := map[string]interface{}{
		"name":       "New York or London",
		"table_name": "customers",
		"requests": []map[string]interface{}{
			{"values": map[string]string{"city": "New York"}},
			{"values": map[string]string{"city": "London"}},
			{"values": map[string]string{"status": "New"}, "omit": true},
		},
	}
	var saved schema.SavedFind
	rec = h.do(http.MethodPost, "/api/v1/saved-finds", owner, body, &saved)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "customers", saved.TableName)
	require.Len(t, saved.Requests, 3)
	// The criteria are kept as typed, so the find can be opened and changed.
	assert.Equal(t, map[string]string{"city": "New York"}, saved.Requests[0].Values)
	assert.True(t, saved.Requests[2].Omit)

	// It is listed for its table, and running it is the find endpoint with
	// the stored requests: New York stays, London's new customer is omitted.
	var listed []schema.SavedFind
	rec = h.do(http.MethodGet, "/api/v1/saved-finds?table=customers", owner, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, listed, 1)
	assert.Equal(t, "New York or London", listed[0].Name)

	var found []map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/customers/find", owner, map[string]interface{}{
		"requests": []map[string]interface{}{
			{"criteria": []map[string]interface{}{{"field_name": "city", "operator": "=", "value": "New York"}}},
			{"criteria": []map[string]interface{}{{"field_name": "city", "operator": "=", "value": "London"}}},
			{"criteria": []map[string]interface{}{{"field_name": "status", "operator": "=", "value": "New"}}, "omit": true},
		},
	}, &found)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, found, 1)
	assert.Equal(t, "New York", found[0]["city"])

	// A second find with the same name on the same table is refused; the
	// same name on another table is not.
	rec = h.do(http.MethodPost, "/api/v1/saved-finds", owner, body, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// What cannot be stored is refused rather than kept to fail later.
	for name, bad := range map[string]map[string]interface{}{
		"no name": {"table_name": "customers", "requests": []map[string]interface{}{
			{"values": map[string]string{"city": "Paris"}}}},
		"unknown table": {"name": "x", "table_name": "suppliers", "requests": []map[string]interface{}{
			{"values": map[string]string{"city": "Paris"}}}},
		"unknown field": {"name": "y", "table_name": "customers", "requests": []map[string]interface{}{
			{"values": map[string]string{"postcode": "75008"}}}},
		"nothing typed": {"name": "z", "table_name": "customers", "requests": []map[string]interface{}{
			{"values": map[string]string{"city": "   "}}}},
		"no requests": {"name": "w", "table_name": "customers", "requests": []map[string]interface{}{}},
	} {
		rec = h.do(http.MethodPost, "/api/v1/saved-finds", owner, bad, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%s: %s", name, rec.Body.String())
	}

	// Renaming keeps the table and the requests.
	var renamed schema.SavedFind
	rec = h.do(http.MethodPut, "/api/v1/saved-finds/"+saved.ID, owner, map[string]interface{}{
		"name":       "East coast and London",
		"table_name": "ignored",
		"requests":   body["requests"],
	}, &renamed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "East coast and London", renamed.Name)
	assert.Equal(t, "customers", renamed.TableName)
	assert.Len(t, renamed.Requests, 3)

	// A user who cannot reach the table neither sees the find nor saves one.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var layout struct {
		ID string `json:"id"`
	}
	var occurrences []struct {
		ID          string `json:"id"`
		BaseTableID string `json:"base_table_id"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", owner, nil, &occurrences).Code)
	var occurrenceID string
	for _, o := range occurrences {
		if o.BaseTableID == tbl.ID {
			occurrenceID = o.ID
		}
	}
	require.NotEmpty(t, occurrenceID)
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", owner,
		map[string]interface{}{"name": "Customers Form", "to_id": occurrenceID}, &layout)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var carol struct {
		ID string `json:"id"`
	}
	var users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/security/users", owner, nil, &users).Code)
	for _, u := range users {
		if u.Username == "carol" {
			carol.ID = u.ID
		}
	}
	require.NotEmpty(t, carol.ID)
	rec = h.do(http.MethodPut, fmt.Sprintf("/api/v1/security/users/%s/permissions", carol.ID), owner,
		map[string]interface{}{"permissions": []map[string]string{
			{"layout_id": layout.ID, "access_level": "none"},
		}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	carolToken := h.login(db, "carol", "carol-secret-pw")
	var carolList []schema.SavedFind
	rec = h.do(http.MethodGet, "/api/v1/saved-finds", carolToken, nil, &carolList)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, carolList, "a find on an unreachable table is not listed")
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodGet, "/api/v1/saved-finds?table=customers", carolToken, nil, nil).Code)
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodDelete, "/api/v1/saved-finds/"+saved.ID, carolToken, nil, nil).Code)

	// The account that saved it can delete it, and then it is gone.
	assert.Equal(t, http.StatusNoContent,
		h.do(http.MethodDelete, "/api/v1/saved-finds/"+saved.ID, owner, nil, nil).Code)
	assert.Equal(t, http.StatusNotFound,
		h.do(http.MethodGet, "/api/v1/saved-finds/"+saved.ID, owner, nil, nil).Code)
}

// TestAPI_SavedFindsTravelInTheBundle: a saved find is part of the solution,
// so it is exported with it and restored into another database (#34).
func TestAPI_SavedFindsTravelInTheBundle(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	srcDB, dstDB := uniqueDB("f4b_find_src"), uniqueDB("f4b_find_dst")
	h.createDatabase(srcDB, "alice", "alice-secret")
	h.createDatabase(dstDB, "olivia", "olivia-secret")
	src := h.login(srcDB, "alice", "alice-secret")
	dst := h.login(dstDB, "olivia", "olivia-secret")

	var tbl struct {
		ID string `json:"id"`
	}
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/schemas/tables", src,
		map[string]string{"display_name": "Customers", "custom_name": "customers"}, &tbl).Code)
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", src,
		map[string]interface{}{"display_name": "city", "name": "city", "field_type": "TEXT"}, nil).Code)
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/saved-finds", src, map[string]interface{}{
		"name": "Paris", "table_name": "customers",
		"requests": []map[string]interface{}{{"values": map[string]string{"city": "Paris"}}},
	}, nil).Code)

	file := exportSolution(t, h, src, map[string]interface{}{"solution_name": "Customers"})
	report := map[string]interface{}{}
	rec := h.postBytes("/api/v1/solutions/import", dst, file, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 1, report["saved_finds_created"])

	var restored []schema.SavedFind
	require.Equal(t, http.StatusOK,
		h.do(http.MethodGet, "/api/v1/saved-finds?table=customers", dst, nil, &restored).Code)
	require.Len(t, restored, 1)
	assert.Equal(t, "Paris", restored[0].Name)
	assert.Equal(t, map[string]string{"city": "Paris"}, restored[0].Requests[0].Values)
	assert.Empty(t, restored[0].CreatedBy, "an imported find has no owner account")

	// Importing the same file again does not make a second copy.
	rec = h.postBytes("/api/v1/solutions/import", dst, file, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 0, report["saved_finds_created"])
	require.Equal(t, http.StatusOK,
		h.do(http.MethodGet, "/api/v1/saved-finds?table=customers", dst, nil, &restored).Code)
	assert.Len(t, restored, 1)
}

// A saved find may hold a criterion on a related field (#46), which is
// checked against the related table rather than the one being searched.
func TestAPI_SavedFindOnARelatedField(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_relfind")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	ids := map[string]string{}
	for _, table := range []struct{ display, name string }{
		{"Companies", "companies"},
		{"Customers", "customers"},
	} {
		var tbl struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
			map[string]string{"display_name": table.display, "custom_name": table.name}, &tbl)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		ids[table.name] = tbl.ID
	}
	columns := map[string]string{}
	for _, col := range []struct{ table, field string }{
		{"companies", "company"},
		{"companies", "company_address"},
		{"customers", "company"},
		{"customers", "last_name"},
	} {
		var created struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables/"+ids[col.table]+"/columns", owner,
			map[string]interface{}{"display_name": col.field, "name": col.field, "field_type": "TEXT"}, &created)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		columns[col.table+"."+col.field] = created.ID
	}

	var occurrences []struct {
		ID          string `json:"id"`
		BaseTableID string `json:"base_table_id"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", owner, nil, &occurrences).Code)
	occurrence := map[string]string{}
	for _, o := range occurrences {
		for name, id := range ids {
			if o.BaseTableID == id {
				occurrence[name] = o.ID
			}
		}
	}

	var rel struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/relationships", owner, map[string]interface{}{
		"name":                "companies_customers",
		"left_occurrence_id":  occurrence["companies"],
		"left_column_id":      columns["companies.company"],
		"right_occurrence_id": occurrence["customers"],
		"right_column_id":     columns["customers.company"],
		"operator":            "=",
	}, &rel)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	key := "rel:" + rel.ID + ":" + occurrence["companies"] + ":company_address"
	var saved schema.SavedFind
	rec = h.do(http.MethodPost, "/api/v1/saved-finds", owner, map[string]interface{}{
		"name": "Customers in Paris", "table_name": "customers",
		"requests": []map[string]interface{}{{"values": map[string]string{key: "*Paris*"}}},
	}, &saved)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "*Paris*", saved.Requests[0].Values[key])

	// A field the related table does not have is refused, as an unknown own
	// field is.
	rec = h.do(http.MethodPost, "/api/v1/saved-finds", owner, map[string]interface{}{
		"name": "Nope", "table_name": "customers",
		"requests": []map[string]interface{}{{"values": map[string]string{
			"rel:" + rel.ID + ":" + occurrence["companies"] + ":postcode": "75008",
		}}},
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// So is a relationship that does not reach the table being searched.
	rec = h.do(http.MethodPost, "/api/v1/saved-finds", owner, map[string]interface{}{
		"name": "Nope either", "table_name": "customers",
		"requests": []map[string]interface{}{{"values": map[string]string{
			"rel:00000000-0000-0000-0000-000000000000::company_address": "Paris",
		}}},
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// And the find itself runs: a criterion on the related field finds the
	// customers whose company is in Paris.
	for _, row := range []map[string]string{
		{"company": "XYZ Inc.", "company_address": "14 Avenue Foch, Paris"},
		{"company": "ABC Company", "company_address": "51 Market Street, San Francisco"},
	} {
		require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/companies", owner, row, nil).Code)
	}
	for _, row := range []map[string]string{
		{"last_name": "Durand", "company": "XYZ Inc."},
		{"last_name": "Smith", "company": "ABC Company"},
	} {
		require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/customers", owner, row, nil).Code)
	}

	relatedFind := map[string]interface{}{
		"requests": []map[string]interface{}{{"criteria": []map[string]interface{}{{
			"field_name":      "company_address",
			"operator":        "LIKE",
			"value":           "%Paris%",
			"relationship_id": rel.ID,
			"occurrence":      occurrence["companies"],
		}}}},
	}
	var found []map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/customers/find", owner, relatedFind, &found)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, found, 1)
	assert.Equal(t, "Durand", found[0]["last_name"])

	// A user who cannot see the companies table cannot search customers
	// through it either: the criterion reads the related table.
	var layout struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", owner,
		map[string]interface{}{"name": "Companies Form", "to_id": occurrence["companies"]}, &layout)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/security/users", owner, nil, &users).Code)
	carol := ""
	for _, u := range users {
		if u.Username == "carol" {
			carol = u.ID
		}
	}
	require.NotEmpty(t, carol)
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+carol+"/permissions", owner,
		map[string]interface{}{"permissions": []map[string]string{
			{"layout_id": layout.ID, "access_level": "none"},
		}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	carolToken := h.login(db, "carol", "carol-secret-pw")
	assert.Equal(t, http.StatusOK,
		h.do(http.MethodPost, "/api/v1/data/customers/find", carolToken, map[string]interface{}{
			"requests": []map[string]interface{}{{"criteria": []map[string]interface{}{
				{"field_name": "last_name", "operator": "=", "value": "Durand"},
			}}},
		}, nil).Code, "their own table is still searchable")
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodPost, "/api/v1/data/customers/find", carolToken, relatedFind, nil).Code)
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodPost, "/api/v1/data/customers/summary", carolToken, relatedFind, nil).Code)
}
