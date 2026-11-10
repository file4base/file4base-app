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

// TestAPI_TransferSchedulesAreAuthorizedByBothSolutions covers the endpoint
// half of #108: a schedule names an account in each solution to run as and
// stores no secret, so creating one has to prove now what every run will
// prove again — and only an administrator of each solution can name an
// account of it.
func TestAPI_TransferSchedulesAreAuthorizedByBothSolutions(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	dispatchDB := uniqueDB("f4b_sched_dispatch")
	resultsDB := uniqueDB("f4b_sched_results")
	dispatch := h.transferSolution(t, dispatchDB, "alice", "alice-secret", "surveys",
		map[string]string{"survey_code": "TEXT", "title": "TEXT"})
	results := h.transferSolution(t, resultsDB, "bob", "bob-secret", "project_surveys",
		map[string]string{"code": "TEXT", "label": "TEXT"})

	for _, row := range []map[string]string{
		{"survey_code": "SV-001", "title": "Enquête de satisfaction"},
		{"survey_code": "SV-002", "title": "Enquête de suivi"},
	} {
		require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/surveys", dispatch, row, nil).Code)
	}
	var def schema.TransferDefinition
	rec := h.do(http.MethodPost, "/api/v1/transfers", dispatch, map[string]interface{}{
		"name":        "Send surveys",
		"source":      map[string]string{"table": "surveys"},
		"destination": map[string]string{"database": resultsDB, "table": "project_surveys"},
		"mappings": []map[string]string{
			{"source_field": "survey_code", "destination_field": "code"},
			{"source_field": "title", "destination_field": "label"},
		},
		"match_fields": []string{"code"},
	}, &def)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// The accounts the schedule will run as, and a plain user of Results.
	for _, account := range []struct{ token, name, role string }{
		{dispatch, "sender", "admin"},
		{results, "receiver", "admin"},
		{results, "carol", "user"},
	} {
		rec := h.do(http.MethodPost, "/api/v1/security/users", account.token,
			map[string]string{"username": account.name, "password": account.name + "-secret", "role": account.role}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	carol := h.login(resultsDB, "carol", "carol-secret")

	body := func(peer, destinationAccount string) map[string]interface{} {
		b := map[string]interface{}{
			"name":                "Nightly surveys",
			"transfer":            "Send surveys",
			"cron":                "0 2 * * *",
			"source_account":      "sender",
			"destination_account": destinationAccount,
		}
		if peer != "" {
			b["peer_token"] = peer
		}
		return b
	}

	// Without a session of Results nothing vouches for an account there.
	rec = h.do(http.MethodPost, "/api/v1/transfer-schedules", dispatch, body("", "receiver"), nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// A plain user of Results cannot name an account of Results to run as.
	rec = h.do(http.MethodPost, "/api/v1/transfer-schedules", dispatch, body(carol, "receiver"), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "only an owner or administrator")

	// An account that does not exist, or could not do the work, is refused
	// now rather than failing every night.
	rec = h.do(http.MethodPost, "/api/v1/transfer-schedules", dispatch, body(results, "nobody"), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "has no account nobody")

	// Bob administers Results, so the schedule can run as receiver there.
	var sched schema.TransferSchedule
	rec = h.do(http.MethodPost, "/api/v1/transfer-schedules", dispatch, body(results, "receiver"), &sched)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, resultsDB, sched.DestinationDatabase)
	assert.NotContains(t, rec.Body.String(), results, "the peer token is never stored or echoed")

	// Running it now is a run like any other: as its accounts, recorded.
	var run struct {
		Status string `json:"status"`
		Report struct {
			Added     int  `json:"added"`
			Committed bool `json:"committed"`
		} `json:"report"`
	}
	rec = h.do(http.MethodPost, "/api/v1/transfer-schedules/"+sched.ID+"/run", dispatch, nil, &run)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, schema.ScheduleRunCommitted, run.Status)
	assert.Equal(t, 2, run.Report.Added)

	var arrived []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/project_surveys", results, nil, &arrived)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, arrived, 2)

	var history []json.RawMessage
	rec = h.do(http.MethodGet, "/api/v1/transfer-schedules/"+sched.ID+"/runs", dispatch, nil, &history)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, history, 1)

	// When the account loses its authority, the next run is refused — a 422
	// with the recorded run — and nothing more is written.
	var users []schema.UserMetadata
	rec = h.do(http.MethodGet, "/api/v1/security/users", results, nil, &users)
	require.Equal(t, http.StatusOK, rec.Code)
	for _, u := range users {
		if u.Username == "receiver" {
			rec = h.do(http.MethodPut, "/api/v1/security/users/"+u.ID, results,
				map[string]interface{}{"is_active": false}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		}
	}
	rec = h.do(http.MethodPost, "/api/v1/transfer-schedules/"+sched.ID+"/run", dispatch, nil, &run)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Equal(t, schema.ScheduleRunRefused, run.Status)
	assert.Contains(t, rec.Body.String(), "is disabled")

	// Schedules are an administrator's: a plain user cannot even list them.
	rec = h.do(http.MethodPost, "/api/v1/security/users", dispatch,
		map[string]string{"username": "dave", "password": "dave-secret", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	dave := h.login(dispatchDB, "dave", "dave-secret")
	rec = h.do(http.MethodGet, "/api/v1/transfer-schedules", dave, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Pausing and deleting.
	var paused schema.TransferSchedule
	rec = h.do(http.MethodPut, "/api/v1/transfer-schedules/"+sched.ID, dispatch,
		map[string]interface{}{"enabled": false}, &paused)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, paused.Enabled)
	assert.Nil(t, paused.NextRunAt, "a paused schedule has no next run")
	rec = h.do(http.MethodDelete, "/api/v1/transfer-schedules/"+sched.ID, dispatch, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// The Server Tools schedules are not executed by this server. They used to
// report "success" for a run that never happened; they now say so (#108).
func TestAPI_ServerToolsSchedulesNeverClaimARunThatDidNotHappen(t *testing.T) {
	adminDB := uniqueDB("f4b_sched_admin")
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, AdminDatabase: adminDB})
	h.createDatabase(adminDB, "root", "root-pass-1")
	root := h.login(adminDB, "root", "root-pass-1")

	var listing struct {
		Schedules []map[string]interface{} `json:"schedules"`
	}
	rec := h.do(http.MethodGet, "/api/v1/server/schedules", root, nil, &listing)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(t, listing.Schedules)
	for _, job := range listing.Schedules {
		assert.Equal(t, "not executed", job["last_status"], "%v", job["name"])
		assert.Equal(t, false, job["executed"])
		assert.NotContains(t, job, "last_run_at", "no job claims a run it never had")
	}

	id := listing.Schedules[0]["id"].(string)
	rec = h.do(http.MethodPost, "/api/v1/server/schedules/"+id+"/run", root, nil, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "was not run")
	assert.Contains(t, rec.Body.String(), "/api/v1/transfer-schedules")

	rec = h.do(http.MethodGet, "/api/v1/server/schedules", root, nil, &listing)
	require.Equal(t, http.StatusOK, rec.Code)
	for _, job := range listing.Schedules {
		assert.Equal(t, "not executed", job["last_status"], "asking to run it changed nothing")
	}
}
