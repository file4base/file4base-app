package api_test

import (
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setPeerPrivileges gives alice read-write access to Peer's customers with
// the given record privileges.
func (p *externalPair) setPeerPrivileges(t *testing.T, create, edit, del bool) {
	t.Helper()
	rec := p.h.do(http.MethodPut, "/api/v1/security/users/"+p.aliceInPeer+"/table-permissions", p.peerOwner, map[string]interface{}{
		"permissions": []map[string]interface{}{{
			"table_name": "customers", "access_level": schema.AccessReadWrite,
			"can_create": create, "can_edit": edit, "can_delete": del,
		}},
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// updateRelationship sets what the orders-customers relationship allows.
func (p *externalPair) updateRelationship(t *testing.T, allowCreation, cascadeDelete bool) {
	t.Helper()
	rec := p.h.do(http.MethodPut, "/api/v1/schemas/relationships/"+p.relationship, p.owner, map[string]interface{}{
		"allow_creation": allowCreation, "cascade_delete": cascadeDelete,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// peerCustomers are Peer's customers as Peer's owner reads them, by name.
func (p *externalPair) peerCustomers(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	var rows []map[string]interface{}
	rec := p.h.do(http.MethodGet, "/api/v1/data/customers", p.peerOwner, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	out := map[string]map[string]interface{}{}
	for _, r := range rows {
		out[fmt.Sprint(r["full_name"])] = r
	}
	return out
}

func (p *externalPair) peerCustomerNames(t *testing.T) []string {
	t.Helper()
	names := []string{}
	for n := range p.peerCustomers(t) {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// orderLabels are the labels of the orders still in Orders.
func (p *externalPair) orderLabels(t *testing.T) []string {
	t.Helper()
	var rows []map[string]interface{}
	rec := p.h.do(http.MethodGet, "/api/v1/data/orders", p.owner, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	ids := map[string]bool{}
	for _, r := range rows {
		ids[fmt.Sprint(r["id"])] = true
	}
	labels := []string{}
	for label, id := range p.orderIDs {
		if ids[id] {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	return labels
}

type privilegesBody struct {
	Privileges api.RecordPrivileges `json:"privileges"`
}

func (p *externalPair) occurrencePrivileges(t *testing.T, token, occurrence string) api.RecordPrivileges {
	t.Helper()
	var body privilegesBody
	rec := p.h.do(http.MethodGet, "/api/v1/occurrences/"+occurrence+"/table", token, nil, &body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return body.Privileges
}

// Peer's records are written through Orders as alice's account in Peer, with
// Peer's privileges: what she may do there she does, what she may not is
// refused naming Peer, its database and what to ask for (#107).
func TestAPI_ExternalWritesAsTheLinkedAccount(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token
	ada := fmt.Sprint(p.peerCustomers(t)["Ada"]["id"])
	occRecord := "/api/v1/occurrences/" + p.ext + "/records/"

	t.Run("privileges follow the other solution", func(t *testing.T) {
		assert.Equal(t, api.RecordPrivileges{Read: true}, p.occurrencePrivileges(t, token, p.ext))
		p.setPeerPrivileges(t, false, true, false)
		assert.Equal(t, api.RecordPrivileges{Read: true, Edit: true}, p.occurrencePrivileges(t, token, p.ext))
		p.setPeerAccess(t, schema.AccessNone)
		rec := h.do(http.MethodGet, "/api/v1/occurrences/"+p.ext+"/table", token, nil, nil)
		assertUnavailable(t, rec.Result(), p.peer, "no_access")
		// alice owns Orders: its own tables are hers.
		assert.Equal(t, api.RecordPrivileges{Read: true, Create: true, Edit: true, Delete: true},
			p.occurrencePrivileges(t, token, p.ordersOcc))
	})

	t.Run("no access refuses writes as unavailable", func(t *testing.T) {
		p.setPeerAccess(t, schema.AccessNone)
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			rec := h.do(method, occRecord+ada, token, map[string]string{"full_name": "Eve"}, nil)
			assertUnavailable(t, rec.Result(), p.peer, "no_access")
		}
		assert.Contains(t, p.peerCustomers(t), "Ada")
	})

	t.Run("edit without create or delete", func(t *testing.T) {
		p.setPeerPrivileges(t, false, true, false)
		var row map[string]interface{}
		rec := h.do(http.MethodPut, occRecord+ada, token, map[string]string{"full_name": "Ada Edited"}, &row)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "Ada Edited", row["full_name"])
		rec = h.do(http.MethodPatch, occRecord+ada, token, map[string]string{"full_name": "Ada"}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		got := p.peerCustomers(t)
		require.Contains(t, got, "Ada")
		assert.Equal(t, "C-1", got["Ada"]["code"], "a partial update keeps the other fields")

		rec = h.do(http.MethodDelete, occRecord+ada, token, nil, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "may not delete records of it")
		assert.Contains(t, rec.Body.String(), p.peer)
		rec = h.do(http.MethodPost, "/api/v1/occurrences/"+p.ext+"/records", token, map[string]string{"full_name": "Eve"}, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "may not create records in it")
		assert.Equal(t, []string{"Ada", "Ada South", "Bob"}, p.peerCustomerNames(t))
	})

	t.Run("create and delete on a layout of the occurrence", func(t *testing.T) {
		p.setPeerPrivileges(t, true, true, true)
		var row map[string]interface{}
		rec := h.do(http.MethodPost, "/api/v1/occurrences/"+p.ext+"/records", token,
			map[string]string{"code": "C-9", "region": "west", "full_name": "Dee"}, &row)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		require.Contains(t, p.peerCustomers(t), "Dee")
		rec = h.do(http.MethodDelete, occRecord+fmt.Sprint(row["id"]), token, nil, nil)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.NotContains(t, p.peerCustomers(t), "Dee")
		rec = h.do(http.MethodDelete, occRecord+fmt.Sprint(row["id"]), token, nil, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, "a record already deleted is not found")
	})

	t.Run("create through the relationship sets both keys in Peer", func(t *testing.T) {
		p.setPeerPrivileges(t, true, true, true)
		related := "/api/v1/data/orders/" + p.orderIDs["code only"] + "/related?relationship=" + p.relationship
		rec := h.do(http.MethodPost, related, token, map[string]string{"full_name": "Cy"}, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, "the relationship does not allow creation yet: %s", rec.Body.String())

		p.updateRelationship(t, true, false)
		p.setPeerPrivileges(t, false, true, true)
		rec = h.do(http.MethodPost, related, token, map[string]string{"full_name": "Cy"}, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "may not create records in it")
		assert.Contains(t, rec.Body.String(), p.peer)

		p.setPeerPrivileges(t, true, true, true)
		var row map[string]interface{}
		rec = h.do(http.MethodPost, related, token, map[string]string{"full_name": "Cy"}, &row)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, "C-2", row["code"])
		assert.Equal(t, "south", row["region"])
		res, names := p.related(token, "code only")
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, []string{"Cy"}, names)

		// The same through the occurrence route of the orders' layout.
		rec = h.do(http.MethodPost, "/api/v1/occurrences/"+p.ordersOcc+"/records/"+p.orderIDs["code only"]+
			"/related?relationship="+p.relationship, token, map[string]string{"full_name": "Cy Two"}, &row)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, "C-2", row["code"])

		// Allow-creation is on the customers' side: a customer creates no order.
		labels := p.orderLabels(t)
		rec = h.do(http.MethodPost, occRecord+ada+"/related?relationship="+p.relationship, token,
			map[string]string{"label": "Backwards"}, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "create the record from")
		assert.Equal(t, labels, p.orderLabels(t))

		// An empty key relates to nothing, so there is nothing to create.
		rec = h.do(http.MethodPost, "/api/v1/data/orders/"+p.orderIDs["empty"]+"/related?relationship="+p.relationship,
			token, map[string]string{"full_name": "Nobody"}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		assert.NotContains(t, p.peerCustomers(t), "Nobody")
	})
}

// Deleting an order whose relationship cascades deletes its customers in
// Peer first, as alice in Peer, and refuses the whole delete before any
// change when she may not delete them (#107).
func TestAPI_ExternalCascadeDelete(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token
	p.updateRelationship(t, false, true)

	p.setPeerPrivileges(t, true, true, false)
	rec := h.do(http.MethodDelete, "/api/v1/data/orders/"+p.orderIDs["north"], token, nil, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "may not delete records of it")
	assert.Contains(t, rec.Body.String(), `\"Peer\"`)
	assert.Equal(t, []string{"code only", "empty", "north", "south"}, p.orderLabels(t), "nothing of Orders is deleted")
	assert.Equal(t, []string{"Ada", "Ada South", "Bob"}, p.peerCustomerNames(t))

	p.setPeerAccess(t, schema.AccessNone)
	rec = h.do(http.MethodDelete, "/api/v1/data/orders/"+p.orderIDs["north"], token, nil, nil)
	assertUnavailable(t, rec.Result(), p.peer, "no_access")
	assert.Equal(t, []string{"code only", "empty", "north", "south"}, p.orderLabels(t))

	p.setPeerPrivileges(t, false, false, true)
	rec = h.do(http.MethodDelete, "/api/v1/data/orders/"+p.orderIDs["north"], token, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"code only", "empty", "south"}, p.orderLabels(t))
	assert.Equal(t, []string{"Ada South", "Bob"}, p.peerCustomerNames(t), "only the customer both keys match")

	// A key that matches nothing in Peer deletes the order alone.
	rec = h.do(http.MethodDelete, "/api/v1/occurrences/"+p.ordersOcc+"/records/"+p.orderIDs["code only"], token, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"empty", "south"}, p.orderLabels(t))
	assert.Equal(t, []string{"Ada South", "Bob"}, p.peerCustomerNames(t))
}

// Two solutions that read each other and cascade both ways delete each
// record once: Orders' orders delete Peer's customers, whose orders back in
// Orders are deleted with the first one, in Orders' transaction (#107).
func TestAPI_ExternalCascadeDeleteCycle(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	p.updateRelationship(t, false, true)
	p.setPeerPrivileges(t, false, false, true)

	type idOnly struct {
		ID string `json:"id"`
	}
	var ref idOnly
	rec := h.do(http.MethodPost, "/api/v1/file-references", p.peerOwner, map[string]interface{}{"name": "Orders"}, &ref)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var back idOnly
	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", p.peerOwner, map[string]interface{}{
		"name": "Customer Orders", "external": map[string]string{"file_reference": "Orders"},
	}, &back)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var occurrences []schema.TableOccurrence
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", p.peerOwner, nil, &occurrences).Code)
	customersOcc := ""
	for _, o := range occurrences {
		if o.Name == "customers" {
			customersOcc = o.ID
		}
	}
	require.NotEmpty(t, customersOcc)
	var tables []schema.TableMetadata
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/tables", p.peerOwner, nil, &tables).Code)
	codeColumn := ""
	for _, tbl := range tables {
		if tbl.Name == "customers" {
			for _, c := range tbl.Columns {
				if c.Name == "code" {
					codeColumn = c.ID
				}
			}
		}
	}
	require.NotEmpty(t, codeColumn)
	rec = h.do(http.MethodPost, "/api/v1/schemas/relationships", p.peerOwner, map[string]interface{}{
		"name": "customers_orders", "left_occurrence_id": customersOcc, "right_occurrence_id": back.ID,
		"predicates":     []map[string]string{{"left_column_id": codeColumn, "operator": "=", "right_column_name": "customer_code"}},
		"cascade_delete": true,
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", p.peerOwner,
		map[string]interface{}{"database": p.orders, "peer_token": p.owner, "tables": map[string]string{"Customer Orders": "orders"}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	token := p.signIn(t, p.orders, "alice-secret").Token
	rec = h.do(http.MethodDelete, "/api/v1/data/orders/"+p.orderIDs["north"], token, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	// north (C-1, north) -> Ada -> every C-1 order: south -> Ada South.
	assert.Equal(t, []string{"code only", "empty"}, p.orderLabels(t))
	assert.Equal(t, []string{"Bob"}, p.peerCustomerNames(t))
}
