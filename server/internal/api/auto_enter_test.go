package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The auto-enter routes and the data-entry header, through the mounted API.
func TestAPI_AutoEnterOptions(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_autoenter")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	var table schema.TableMetadata
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Tickets", "custom_name": "tickets"}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	column := func(name, fieldType, options string) *httptest.ResponseRecorder {
		return h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", token, map[string]interface{}{
			"name": name, "display_name": name, "field_type": fieldType, "is_nullable": true, "default_value": options,
		}, nil)
	}
	rec = column("num", "NUMBER", `{"serial_enabled":true,"serial_next":7}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = column("status", "TEXT", `{"data_enabled":true,"data_value":"Open","prohibit_modification":true}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = column("subject", "TEXT", "")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = column("bad", "DATE", `{"creation_enabled":true,"creation_type":"Name"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a stamp the field cannot hold is refused: %s", rec.Body.String())

	send := func(method, target string, body interface{}, script bool) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, target, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if script {
			req.Header.Set(api.EntryHeader, "script")
		}
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}

	var row map[string]interface{}
	rec = send(http.MethodPost, "/api/v1/data/tickets", map[string]string{"subject": "First"}, false)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))
	assert.Equal(t, json.Number("7"), json.Number(fmt.Sprint(row["num"])))
	assert.IsType(t, float64(0), row["num"], "a number field answers a number")
	assert.Equal(t, "Open", row["status"])
	id := row["id"].(string)

	rec = send(http.MethodPut, "/api/v1/data/tickets/"+id, map[string]string{"status": "Closed"}, false)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Prohibited Modification")
	rec = send(http.MethodPut, "/api/v1/data/tickets/"+id, map[string]string{"status": "Closed"}, true)
	assert.Equal(t, http.StatusOK, rec.Code, "a script may set it: %s", rec.Body.String())

	var serial api.NextSerialValue
	rec = h.do(http.MethodGet, "/api/v1/data/tickets/serials/num", token, nil, &serial)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "8", serial.NextValue)
	rec = h.do(http.MethodPut, "/api/v1/data/tickets/serials/num", token, map[string]interface{}{"next_value": 100}, &serial)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "100", serial.NextValue)
	rec = h.do(http.MethodGet, "/api/v1/data/tickets/serials/subject", token, nil, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	var dup map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/tickets/"+id+"/duplicate", token, nil, &dup)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotEqual(t, id, dup["id"])
	assert.Equal(t, "100", fmt.Sprint(dup["num"]))
	assert.Equal(t, "First", dup["subject"])
	assert.Equal(t, "Open", dup["status"], "the constant is entered anew")

	rec = h.do(http.MethodPost, "/api/v1/data/tickets/relookup", token, map[string]interface{}{"field": "subject", "record_ids": []string{id}}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "no lookup starts from subject: %s", rec.Body.String())
}

// A record of another solution's table is duplicated, and its serial number
// read and set, in that solution as the linked account: the serial is that
// solution's, never copied.
func TestAPI_AutoEnterOnExternalOccurrence(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token
	p.setPeerPrivileges(t, true, true, false)

	var tables []schema.TableMetadata
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", p.peerOwner, nil, &tables)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	customers := ""
	for _, tbl := range tables {
		if tbl.Name == "customers" {
			customers = tbl.ID
		}
	}
	require.NotEmpty(t, customers)
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+customers+"/columns", p.peerOwner, map[string]interface{}{
		"name": "ref", "display_name": "ref", "field_type": "TEXT", "is_nullable": true,
		"default_value": `{"serial_enabled":true,"serial_next":"R-0040"}`,
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	records := "/api/v1/occurrences/" + p.ext + "/records"
	ada := fmt.Sprint(p.peerCustomers(t)["Ada"]["id"])
	var copied map[string]interface{}
	rec = h.do(http.MethodPost, records+"/"+ada+"/duplicate", token, nil, &copied)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Ada", copied["full_name"])
	assert.Equal(t, "R-0040", copied["ref"], "the other solution enters its own serial")
	assert.NotEqual(t, ada, fmt.Sprint(copied["id"]))

	var serial api.NextSerialValue
	rec = h.do(http.MethodGet, records+"/serials/ref", token, nil, &serial)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "R-0041", serial.NextValue)
	rec = h.do(http.MethodPut, records+"/serials/ref", token, map[string]interface{}{"next_value": "R-0100"}, &serial)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/data/customers/serials/ref", p.peerOwner, nil, &serial)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "R-0100", serial.NextValue, "set in the other solution")

	p.setPeerPrivileges(t, false, true, false)
	rec = h.do(http.MethodPost, records+"/"+ada+"/duplicate", token, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "duplicating needs the other solution's create privilege: %s", rec.Body.String())
}
