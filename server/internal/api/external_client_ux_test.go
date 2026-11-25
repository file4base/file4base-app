package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A solution a file reference is mapped to can be signed in to with
// /auth/link before any occurrence goes through the reference: the client's
// Specify Table lists its tables to make the first one, and Perform Script
// signs in to it. A solution nothing of the session's names still cannot
// (#107).
func TestAPI_LinkReachesTheSolutionOfAMappedFileReference(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	survey, directory, other := uniqueDB("f4b_ux_survey"), uniqueDB("f4b_ux_dir"), uniqueDB("f4b_ux_other")
	h.createDatabase(survey, "alice", "alice-secret")
	h.createDatabase(directory, "dana", "dana-secret")
	h.createDatabase(other, "erin", "erin-secret")
	alice := h.login(survey, "alice", "alice-secret")
	dana := h.login(directory, "dana", "dana-secret")
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", dana, map[string]string{"display_name": "staff", "custom_name": "staff"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	link := func(database, user, password string) (int, api.LinkedSolution) {
		var out api.LinkedSolution
		rec := h.do(http.MethodPost, "/api/v1/auth/link", alice,
			map[string]string{"database": database, "username": user, "password": password}, nil)
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		}
		return rec.Code, out
	}

	// Not named yet: refused.
	code, _ := link(directory, "dana", "dana-secret")
	require.Equal(t, http.StatusNotFound, code)

	// An unmapped reference names no solution either.
	rec = h.do(http.MethodPost, "/api/v1/file-references", alice, map[string]interface{}{"name": "Directory"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	code, _ = link(directory, "dana", "dana-secret")
	require.Equal(t, http.StatusNotFound, code)

	// Mapped, with no occurrence through it yet.
	rec = h.do(http.MethodPost, "/api/v1/file-references", alice, map[string]interface{}{"name": "Staff Directory", "database": directory}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	code, linked := link(directory, "dana", "dana-secret")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, directory, linked.Database)
	assert.Equal(t, []string{"Staff Directory"}, linked.References)
	require.NotEmpty(t, linked.Token)

	// The linked session reads that solution's tables as its account.
	var tables []struct {
		Name string `json:"name"`
	}
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", linked.Token, nil, &tables)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	names := []string{}
	for _, tbl := range tables {
		names = append(names, tbl.Name)
	}
	assert.Contains(t, names, "staff")

	code, _ = link(other, "erin", "erin-secret")
	assert.Equal(t, http.StatusNotFound, code, "a solution no reference names is not linked")

	// A transfer names its far solution with no file reference: Import
	// Records signs in to it the same way.
	results := uniqueDB("f4b_ux_results")
	h.transferSolution(t, results, "bob", "bob-secret", "tallies", map[string]string{"code": "TEXT"})
	var answers struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", alice, map[string]string{"display_name": "answers", "custom_name": "answers"}, &answers)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+answers.ID+"/columns", alice,
		map[string]interface{}{"display_name": "answer_code", "name": "answer_code", "field_type": "TEXT"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	code, _ = link(results, "bob", "bob-secret")
	require.Equal(t, http.StatusNotFound, code, "no transfer reaches it yet")
	rec = h.do(http.MethodPost, "/api/v1/transfers", alice, map[string]interface{}{
		"name":        "Send tallies",
		"source":      map[string]string{"table": "answers"},
		"destination": map[string]string{"database": results, "table": "tallies"},
		"mappings":    []map[string]string{{"source_field": "answer_code", "destination_field": "code"}},
		"mode":        schema.TransferModeAdd,
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	code, linked = link(results, "bob", "bob-secret")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, results, linked.Database)
	assert.Empty(t, linked.References, "a transfer is no file reference")
	assert.NotEmpty(t, linked.Token)
}

// A value list taken from a field of another solution's table has a second
// field, named as its first one is, shown beside it or in its place and
// sorted by it, and can include only the values related to the record a
// layout of this solution shows (#107).
func TestAPI_ExternalValueListSecondFieldAndRelatedValues(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token

	type item struct {
		Value  string  `json:"value"`
		Second *string `json:"second"`
	}
	type values struct {
		Values []string `json:"values"`
		Items  []item   `json:"items"`
	}
	type saved struct {
		ID               string `json:"id"`
		SecondColumnName string `json:"second_column_name"`
		SortBy           string `json:"sort_by"`
	}
	var last saved
	create := func(body map[string]interface{}) (int, string) {
		last = saved{}
		body["kind"], body["source_occurrence_id"] = "field", p.ext
		rec := h.do(http.MethodPost, "/api/v1/value-lists", p.owner, body, &last)
		return rec.Code, last.ID
	}
	read := func(id, query string) values {
		var out values
		rec := h.do(http.MethodGet, "/api/v1/value-lists/"+id+"/values"+query, token, nil, &out)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return out
	}

	t.Run("second field", func(t *testing.T) {
		code, id := create(map[string]interface{}{
			"name": "Codes and names", "source_column_name": "code", "second_column_name": "full_name", "sort_by": "second",
		})
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, "full_name", last.SecondColumnName)
		assert.Equal(t, "second", last.SortBy)
		var lists []saved
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/value-lists", p.owner, nil, &lists).Code)
		found := false
		for _, l := range lists {
			if l.ID == id {
				found = true
				assert.Equal(t, "full_name", l.SecondColumnName, "read back from the catalog")
				assert.Equal(t, "second", l.SortBy)
			}
		}
		assert.True(t, found)

		got := read(id, "")
		require.Len(t, got.Items, 3)
		seconds := []string{}
		for _, it := range got.Items {
			require.NotNil(t, it.Second)
			seconds = append(seconds, it.Value+"/"+*it.Second)
		}
		assert.Equal(t, []string{"C-1/Ada", "C-1/Ada South", "C-2/Bob"}, seconds, "sorted by the second field")
	})

	t.Run("related values", func(t *testing.T) {
		code, id := create(map[string]interface{}{
			"name": "Names of the order's customer", "source_column_name": "full_name", "related_occurrence_id": p.ordersOcc,
		})
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, []string{"Ada South"}, read(id, "?table=orders&record_id="+p.orderIDs["south"]).Values)
		assert.Empty(t, read(id, "?table=orders&record_id="+p.orderIDs["empty"]).Values)

		// No record in hand: nothing related, and the other solution is not
		// opened for it, so a session not signed in there gets an empty
		// list, not a 424.
		assert.Empty(t, read(id, "").Values)
		var none values
		rec := h.do(http.MethodGet, "/api/v1/value-lists/"+id+"/values", p.owner, nil, &none)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Empty(t, none.Values)
		rec = h.do(http.MethodGet, "/api/v1/value-lists/"+id+"/values?table=orders&record_id="+p.orderIDs["south"], p.owner, nil, nil)
		assert.Equal(t, http.StatusFailedDependency, rec.Code, "with a record, the other solution is read: not signed in there")
	})

	t.Run("refused", func(t *testing.T) {
		code, _ := create(map[string]interface{}{"name": "Starts outside", "source_column_name": "full_name", "related_occurrence_id": p.ext})
		assert.Equal(t, http.StatusUnprocessableEntity, code, "related values start from an occurrence of this solution")
		code, _ = create(map[string]interface{}{"name": "Second by id", "source_column_name": "full_name", "second_column_id": "c-1"})
		assert.Equal(t, http.StatusUnprocessableEntity, code)
		code, _ = create(map[string]interface{}{"name": "Sorted by nothing", "source_column_name": "full_name", "sort_by": "second"})
		assert.Equal(t, http.StatusUnprocessableEntity, code)
	})
}
