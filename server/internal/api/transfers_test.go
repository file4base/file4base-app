package api_test

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transferSolution creates a database with one table and the fields given,
// and returns the owner's session token.
func (h *apiHarness) transferSolution(
	t *testing.T, db, user, password, table string, fields map[string]string,
) string {
	t.Helper()
	h.createDatabase(db, user, password)
	token := h.login(db, user, password)

	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token,
		map[string]string{"display_name": table, "custom_name": table}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for name, fieldType := range fields {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token,
			map[string]interface{}{"display_name": name, "name": name, "field_type": fieldType}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	return token
}

// TestAPI_TransferRunsWithASessionForEachEnd is the endpoint half of #96: a
// transfer between two solutions needs this session and a peer_token for the
// far one, and the definition itself never carries a credential.
func TestAPI_TransferRunsWithASessionForEachEnd(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	dispatchDB := uniqueDB("f4b_tx_dispatch")
	resultsDB := uniqueDB("f4b_tx_results")
	dispatch := h.transferSolution(t, dispatchDB, "alice", "alice-secret", "surveys",
		map[string]string{"survey_code": "TEXT", "title": "TEXT"})
	results := h.transferSolution(t, resultsDB, "bob", "bob-secret", "project_surveys",
		map[string]string{"code": "TEXT", "label": "TEXT"})

	for _, row := range []map[string]string{
		{"survey_code": "SV-001", "title": "Enquête de satisfaction"},
		{"survey_code": "SV-002", "title": "Enquête de suivi"},
	} {
		require.Equal(t, http.StatusCreated,
			h.do(http.MethodPost, "/api/v1/data/surveys", dispatch, row, nil).Code)
	}

	definition := map[string]interface{}{
		"name":        "Send surveys",
		"source":      map[string]string{"table": "surveys"},
		"destination": map[string]string{"database": resultsDB, "table": "project_surveys"},
		"mappings": []map[string]string{
			{"source_field": "survey_code", "destination_field": "code"},
			{"source_field": "title", "destination_field": "label"},
		},
		"match_fields": []string{"code"},
		"mode":         schema.TransferModeUpdateMatching,
	}

	var saved schema.TransferDefinition
	rec := h.do(http.MethodPost, "/api/v1/transfers", dispatch, definition, &saved)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, resultsDB, saved.Destination.Database)
	assert.Equal(t, schema.TransferRejectedAbort, saved.OnRejected, "the safe default is stored")

	// Without a peer token there is no authority over the far solution, and
	// the server says so rather than reaching into it.
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", dispatch, nil, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "peer_token")

	// A session of a third solution is not authority over the destination
	// either, whatever role it holds there.
	strangerDB := uniqueDB("f4b_tx_stranger")
	stranger := h.transferSolution(t, strangerDB, "carol", "carol-secret", "whatever",
		map[string]string{"note": "TEXT"})
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", dispatch,
		map[string]string{"peer_token": stranger}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", dispatch,
		map[string]string{"peer_token": "not-a-session"}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// With a session for each end it runs, and the report says what it did.
	var report transfer.Report
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", dispatch,
		map[string]string{"peer_token": results}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, report.Committed)
	assert.Equal(t, 2, report.Rows)
	assert.Equal(t, 2, report.Added)
	assert.Equal(t, 0, report.Updated)
	assert.Empty(t, report.Rejected)
	assert.Equal(t, dispatchDB, report.SourceDatabase)
	assert.Equal(t, resultsDB, report.DestinationDatabase)

	// The records are in the far solution, read with that solution's own
	// session.
	var arrived []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/project_surveys", results, nil, &arrived)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, arrived, 2)

	// Replaying it updates the same two records: no second copy appears.
	rec = h.do(http.MethodPost, "/api/v1/transfers/Send%20surveys/run", dispatch,
		map[string]string{"peer_token": results}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 0, report.Added, "a replay adds nothing")
	assert.Equal(t, 2, report.Updated)

	arrived = nil
	rec = h.do(http.MethodGet, "/api/v1/data/project_surveys", results, nil, &arrived)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, arrived, 2, "still two records after the replay")
}

// TestAPI_TransferRefusesTheWholeRunOnARejectedRow holds the documented
// transaction contract at the endpoint: 422, nothing written, and the
// rejected rows named.
func TestAPI_TransferRefusesTheWholeRunOnARejectedRow(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	dispatchDB := uniqueDB("f4b_tx_src")
	resultsDB := uniqueDB("f4b_tx_dst")
	dispatch := h.transferSolution(t, dispatchDB, "alice", "alice-secret", "answers",
		map[string]string{"answer_code": "TEXT", "score_text": "TEXT"})
	results := h.transferSolution(t, resultsDB, "bob", "bob-secret", "project_answers",
		map[string]string{"code": "TEXT", "score": "NUMBER"})

	for _, row := range []map[string]string{
		{"answer_code": "AN-001", "score_text": "12.5"},
		{"answer_code": "AN-002", "score_text": "non renseigné"},
	} {
		require.Equal(t, http.StatusCreated,
			h.do(http.MethodPost, "/api/v1/data/answers", dispatch, row, nil).Code)
	}

	definition := map[string]interface{}{
		"name":        "Send answers",
		"source":      map[string]string{"table": "answers"},
		"destination": map[string]string{"database": resultsDB, "table": "project_answers"},
		"mappings": []map[string]string{
			{"source_field": "answer_code", "destination_field": "code"},
			{"source_field": "score_text", "destination_field": "score"},
		},
		"match_fields": []string{"code"},
		"mode":         schema.TransferModeUpdateMatching,
		"on_rejected":  schema.TransferRejectedAbort,
	}
	var saved schema.TransferDefinition
	rec := h.do(http.MethodPost, "/api/v1/transfers", dispatch, definition, &saved)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var refused struct {
		transfer.Report
		Error string `json:"error"`
	}
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", dispatch,
		map[string]string{"peer_token": results}, &refused)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.False(t, refused.Committed)
	assert.Equal(t, 2, refused.Rows)
	assert.Equal(t, 0, refused.Added)
	require.Len(t, refused.Rejected, 1)
	assert.Equal(t, "score_text", refused.Rejected[0].SourceField)
	assert.Equal(t, "score", refused.Rejected[0].DestinationField)
	assert.NotEmpty(t, refused.Error)

	// Nothing was written, so the run can be replayed once the source is
	// corrected.
	var arrived []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/project_answers", results, nil, &arrived)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, arrived)

	// With the rejected row quarantined instead, the good row commits and
	// the rejected one is still reported.
	definition["on_rejected"] = schema.TransferRejectedSkip
	rec = h.do(http.MethodPut, "/api/v1/transfers/"+saved.ID, dispatch, definition, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var report transfer.Report
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", dispatch,
		map[string]string{"peer_token": results}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, report.Committed)
	assert.Equal(t, 1, report.Added)
	require.Len(t, report.Rejected, 1)

	arrived = nil
	rec = h.do(http.MethodGet, "/api/v1/data/project_answers", results, nil, &arrived)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, arrived, 1)
	assert.Equal(t, "AN-001", arrived[0]["code"])
}

// TestAPI_TransferWithinOneSolutionNeedsNoPeerToken covers the simple case:
// two tables of the same solution.
func TestAPI_TransferWithinOneSolutionNeedsNoPeerToken(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	db := uniqueDB("f4b_tx_local")
	owner := h.transferSolution(t, db, "alice", "alice-secret", "inbox",
		map[string]string{"ref": "TEXT", "note": "TEXT"})

	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
		map[string]string{"display_name": "archive", "custom_name": "archive"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for _, field := range []string{"ref", "note"} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner,
			map[string]interface{}{"display_name": field, "name": field, "field_type": "TEXT"}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/inbox", owner,
		map[string]string{"ref": "R-1", "note": "à archiver"}, nil).Code)

	var saved schema.TransferDefinition
	rec = h.do(http.MethodPost, "/api/v1/transfers", owner, map[string]interface{}{
		"name":        "Archive inbox",
		"source":      map[string]string{"table": "inbox"},
		"destination": map[string]string{"table": "archive"},
		"mappings": []map[string]string{
			{"source_field": "ref", "destination_field": "ref"},
			{"source_field": "note", "destination_field": "note"},
		},
		"match_fields": []string{"ref"},
	}, &saved)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var report transfer.Report
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", owner, nil, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, report.Committed)
	assert.Equal(t, 1, report.Added)

	// Offering a peer token for a transfer that has no far end is a mistake
	// worth reporting, not something to accept quietly.
	rec = h.do(http.MethodPost, "/api/v1/transfers/"+saved.ID+"/run", owner,
		map[string]string{"peer_token": owner}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// TestAPI_TransferDefinitionsAreADesignObject covers the CRUD side: who may
// change a definition, and what is refused before it can fail at run time.
func TestAPI_TransferDefinitionsAreADesignObject(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	db := uniqueDB("f4b_tx_crud")
	owner := h.transferSolution(t, db, "alice", "alice-secret", "surveys",
		map[string]string{"survey_code": "TEXT"})

	definition := map[string]interface{}{
		"name":        "Send surveys",
		"source":      map[string]string{"table": "surveys"},
		"destination": map[string]string{"database": "somewhere_else", "table": "project_surveys"},
		"mappings": []map[string]string{
			{"source_field": "survey_code", "destination_field": "code"},
		},
		"match_fields": []string{"code"},
	}

	var saved schema.TransferDefinition
	rec := h.do(http.MethodPost, "/api/v1/transfers", owner, definition, &saved)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// It is listed, and readable by id and by name.
	var listed []schema.TransferDefinition
	rec = h.do(http.MethodGet, "/api/v1/transfers", owner, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, listed, 1)

	rec = h.do(http.MethodGet, "/api/v1/transfers/Send%20surveys", owner, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/transfers/no-such-transfer", owner, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// The same name twice is refused.
	rec = h.do(http.MethodPost, "/api/v1/transfers", owner, definition, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// A definition that could not run is refused when it is saved.
	for why, spoil := range map[string]func(map[string]interface{}){
		"a match field nothing is mapped to": func(d map[string]interface{}) {
			d["match_fields"] = []string{"external_ref"}
		},
		"an unknown rejected policy": func(d map[string]interface{}) { d["on_rejected"] = "ignore" },
		"an internal destination table": func(d map[string]interface{}) {
			d["destination"] = map[string]string{"table": "sys_users"}
		},
	} {
		t.Run(why, func(t *testing.T) {
			bad := map[string]interface{}{}
			for k, v := range definition {
				bad[k] = v
			}
			bad["name"] = why
			spoil(bad)
			rec := h.do(http.MethodPost, "/api/v1/transfers", owner, bad, nil)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		})
	}

	// A regular user may read and run a transfer but not design one.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]string{"username": "dave", "password": "dave-secret", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(db, "dave", "dave-secret")

	rec = h.do(http.MethodGet, "/api/v1/transfers", user, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/transfers", user, map[string]interface{}{
		"name":        "User's own transfer",
		"source":      map[string]string{"table": "surveys"},
		"destination": map[string]string{"database": "somewhere_else", "table": "x"},
		"mappings": []map[string]string{
			{"source_field": "survey_code", "destination_field": "code"},
		},
		"match_fields": []string{"code"},
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = h.do(http.MethodDelete, "/api/v1/transfers/"+saved.ID, user, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = h.do(http.MethodDelete, "/api/v1/transfers/"+saved.ID, owner, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = h.do(http.MethodGet, "/api/v1/transfers/"+saved.ID, owner, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
