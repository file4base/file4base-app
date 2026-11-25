package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/migration"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An occurrence of another solution's table is created, bound and verified
// with a session of that solution. A session signed in before the bind is
// not signed in to that solution: every read through it is refused with 424
// external_unavailable, never answered from a table of this solution
// (#107).
func TestAPI_ExternalOccurrencesBindVerifyAndRefuseReads(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	orders, peer := uniqueDB("f4b_ext_orders"), uniqueDB("f4b_ext_peer")
	h.createDatabase(orders, "alice", "alice-secret")
	h.createDatabase(peer, "bob", "bob-secret")
	owner := h.login(orders, "alice", "alice-secret")
	peerOwner := h.login(peer, "bob", "bob-secret")

	type table struct {
		ID string `json:"id"`
	}
	createTable := func(token, name string, fields ...string) (string, map[string]string) {
		var tbl table
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": name, "custom_name": name}, &tbl)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		ids := map[string]string{}
		for _, f := range fields {
			var col table
			rec := h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token, map[string]interface{}{
				"name": f, "display_name": f, "field_type": "TEXT", "is_nullable": true,
			}, &col)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			ids[f] = col.ID
		}
		return tbl.ID, ids
	}
	createTable(peerOwner, "customers", "code", "full_name")
	ordersTable, orderFields := createTable(owner, "orders", "customer_code")

	var occurrences []schema.TableOccurrence
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", owner, nil, &occurrences).Code)
	ordersOcc := ""
	for _, o := range occurrences {
		if o.BaseTableID == ordersTable {
			ordersOcc = o.ID
		}
	}
	require.NotEmpty(t, ordersOcc)

	var ref schema.FileReference
	rec := h.do(http.MethodPost, "/api/v1/file-references", owner,
		map[string]interface{}{"name": "Peer", "paths": []string{"file:Peer.fmp12"}}, &ref)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", owner, map[string]interface{}{"name": "Nothing"}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "an occurrence names a table or a file reference")

	var ext schema.TableOccurrence
	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", owner, map[string]interface{}{
		"name": "Order Customers", "external": map[string]string{"file_reference": "Peer"},
	}, &ext)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NotNil(t, ext.External)
	assert.Equal(t, "Peer", ext.External.FileReference)

	var rel struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/relationships", owner, map[string]interface{}{
		"name": "orders_customers", "left_occurrence_id": ordersOcc, "right_occurrence_id": ext.ID,
		"predicates": []map[string]string{{"left_column_id": orderFields["customer_code"], "operator": "=", "right_column_name": "code"}},
	}, &rel)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var list struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/value-lists", owner, map[string]interface{}{
		"name": "Customer names", "kind": "field", "source_occurrence_id": ext.ID, "source_column_name": "full_name",
	}, &list)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Unmapped, verifying needs no session of the other solution.
	var report schema.ExternalBindingReport
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/verify", owner, map[string]string{}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, report.Occurrences, 1)
	assert.Equal(t, schema.ExternalUnbound, report.Occurrences[0].Status)

	// Binding reads the other solution's catalog with a session of its own.
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", owner, map[string]interface{}{"database": peer}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a peer_token is required")
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", owner,
		map[string]interface{}{"database": peer, "peer_token": owner}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a session of this solution does not read another")

	// A bind that fails saves nothing: not even the solution the reference
	// is mapped to, which the scripts it runs would follow.
	for _, tables := range []map[string]string{{"Order Customers": "no_such_table"}, {"No Such Occurrence": "customers"}} {
		rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", owner,
			map[string]interface{}{"database": peer, "peer_token": peerOwner, "tables": tables}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		var still schema.FileReference
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/file-references/"+ref.ID, owner, nil, &still).Code)
		assert.Empty(t, still.Database, "a failed bind leaves the reference unmapped")
	}

	// Binding and verifying read the design of both solutions: not for a
	// regular user.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]string{"username": "dave", "password": "dave-secret", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(orders, "dave", "dave-secret")
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/verify", user, map[string]string{}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", user,
		map[string]interface{}{"database": peer, "peer_token": peerOwner}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", owner,
		map[string]interface{}{"database": peer, "peer_token": peerOwner}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, report.Occurrences, 1)
	assert.Equal(t, schema.ExternalBound, report.Occurrences[0].Status, report.Occurrences[0].Problems)
	assert.Equal(t, "customers", report.Occurrences[0].Table, "the one table holding every field used")
	assert.Equal(t, peer, report.Database)

	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/verify", owner,
		map[string]string{"peer_token": peerOwner}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, schema.ExternalBound, report.Occurrences[0].Status)

	// A read through it is refused, and says which reference to sign in to.
	var order map[string]interface{}
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/orders", owner, map[string]interface{}{"customer_code": "C-1"}, &order).Code)
	rec = h.do(http.MethodGet, fmt.Sprintf("/api/v1/data/orders/%v/related?relationship=%s", order["id"], rel.ID), owner, nil, nil)
	assertExternalUnavailable(t, rec, peer, "not_signed_in")

	rec = h.do(http.MethodGet, "/api/v1/value-lists/"+list.ID+"/values", owner, nil, nil)
	assertExternalUnavailable(t, rec, peer, "not_signed_in")

	rec = h.do(http.MethodDelete, "/api/v1/file-references/"+ref.ID, owner, nil, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "occurrences still go through the reference")
}

func assertExternalUnavailable(t *testing.T, rec interface {
	Result() *http.Response
}, database, reason string) {
	t.Helper()
	res := rec.Result()
	defer res.Body.Close()
	require.Equal(t, http.StatusFailedDependency, res.StatusCode)
	assert.Contains(t, res.Header.Get("Content-Type"), "application/problem+json")
	var problem struct {
		Type      string `json:"type"`
		Status    int    `json:"status"`
		Code      string `json:"code"`
		Reference string `json:"reference"`
		Database  string `json:"database"`
		Reason    string `json:"reason"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&problem))
	assert.Equal(t, "https://file4base.org/errors/external-unavailable", problem.Type)
	assert.Equal(t, http.StatusFailedDependency, problem.Status)
	assert.Equal(t, "external_unavailable", problem.Code)
	assert.Equal(t, "Peer", problem.Reference)
	assert.Equal(t, database, problem.Database)
	assert.Equal(t, reason, problem.Reason)
}

// The preflight of a file with an external occurrence names the installed
// solution its data source most likely is and, for a signed-in caller,
// whether that solution's own external occurrences lead back: a cycle, to be
// bound from both ends. Nothing is bound by the preflight.
func TestAPI_MigratePreflightSuggestsTheSolutionOfAnExternalOccurrence(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	home, customers := uniqueDB("f4b_ext_home"), uniqueDB("f4b_ext_customers")
	h.createDatabase(home, "alice", "alice-secret")
	owner := h.login(home, "alice", "alice-secret")

	// The other file, installed first: it has an occurrence going back to
	// Orders, as its own migration would leave it.
	h.createDatabase(customers, "carol", "carol-secret-2026")
	peer := h.login(customers, "carol", "carol-secret-2026")
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", peer, map[string]string{"display_name": "Customers", "custom_name": "customers"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/file-references", peer,
		map[string]interface{}{"name": "Orders", "paths": []string{"file:Orders.fmp12"}}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", peer, map[string]interface{}{
		"name": "Customer Orders", "external": map[string]string{"file_reference": "Orders"},
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	ordersXML := fmt.Sprintf(`<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Customers" type="FileMaker"><File><UniversalPathList>fmnet:/host.example/%s
file:%s.fmp12</UniversalPathList></File></ExternalDataSource></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Customer Code" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Orders"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Order Customers" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Customers"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 </AddAction></Structure></FMSaveAsXML>`, customers, customers)

	type preflight struct {
		Report *migration.ConversionReport `json:"report"`
	}
	for _, signedIn := range []bool{true, false} {
		token := ""
		if signedIn {
			token = owner
		}
		var res preflight
		rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/preflight", token, map[string]interface{}{
			"design_xml": ordersXML, "solution_name": "Orders",
		}, &res)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotNil(t, res.Report)
		require.Len(t, res.Report.ExternalOccurrences, 1)
		e := res.Report.ExternalOccurrences[0]
		assert.Equal(t, "Order Customers", e.Occurrence)
		assert.Equal(t, customers, e.SuggestedDatabase)
		assert.Equal(t, signedIn, e.Cycle, "only a signed-in caller learns about the other solution's design")

		manual := false
		for _, item := range res.Report.ManualActionRequired {
			manual = manual || item.SourceName == "Order Customers"
		}
		assert.True(t, manual, "an external occurrence is a manual action, not a rejection")
		for _, item := range res.Report.Rejected {
			assert.NotEqual(t, "Order Customers", item.SourceName)
		}
	}
}
