package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// externalPair is two solutions: Orders, whose orders relate to the
// customers of Peer through an external occurrence on two criteria
// (code and region), and Peer. alice owns Orders and has an account of
// her own in Peer.
type externalPair struct {
	h                  *apiHarness
	orders, peer       string
	owner, peerOwner   string // alice in Orders, bob in Peer, signed in before the bind
	aliceInPeer        string // alice's user id in Peer
	ordersOcc, ext     string
	relationship, list string
	orderIDs           map[string]string // label -> record id
}

func newExternalPair(t *testing.T, opts api.Options, peerPassword string) *externalPair {
	t.Helper()
	opts.AllowPublicDatabaseCreation = true
	p := &externalPair{h: newAPIHarness(t, opts), orders: uniqueDB("f4b_xr_orders"), orderIDs: map[string]string{}}
	h := p.h
	p.peer = uniqueDB("f4b_xr_peer")
	h.createDatabase(p.orders, "alice", "alice-secret")
	h.createDatabase(p.peer, "bob", "bob-secret")
	p.owner = h.login(p.orders, "alice", "alice-secret")
	p.peerOwner = h.login(p.peer, "bob", "bob-secret")

	type idOnly struct {
		ID string `json:"id"`
	}
	createTable := func(token, name string, fields ...string) (string, map[string]string) {
		var tbl idOnly
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": name, "custom_name": name}, &tbl)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		ids := map[string]string{}
		for _, f := range fields {
			var col idOnly
			rec := h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", token, map[string]interface{}{
				"name": f, "display_name": f, "field_type": "TEXT", "is_nullable": true,
			}, &col)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			ids[f] = col.ID
		}
		return tbl.ID, ids
	}
	createTable(p.peerOwner, "customers", "code", "region", "full_name")
	for _, c := range [][3]string{{"C-1", "north", "Ada"}, {"C-1", "south", "Ada South"}, {"C-2", "north", "Bob"}} {
		rec := h.do(http.MethodPost, "/api/v1/data/customers", p.peerOwner, map[string]string{"code": c[0], "region": c[1], "full_name": c[2]}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	var alice idOnly
	rec := h.do(http.MethodPost, "/api/v1/security/users", p.peerOwner,
		map[string]string{"username": "alice", "password": peerPassword, "role": "user"}, &alice)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	p.aliceInPeer = alice.ID
	p.setPeerAccess(t, schema.AccessReadOnly)

	ordersTable, orderFields := createTable(p.owner, "orders", "customer_code", "region")
	var occurrences []schema.TableOccurrence
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", p.owner, nil, &occurrences).Code)
	for _, o := range occurrences {
		if o.BaseTableID == ordersTable {
			p.ordersOcc = o.ID
		}
	}
	require.NotEmpty(t, p.ordersOcc)

	var ref idOnly
	rec = h.do(http.MethodPost, "/api/v1/file-references", p.owner, map[string]interface{}{"name": "Peer"}, &ref)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var ext idOnly
	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", p.owner, map[string]interface{}{
		"name": "Order Customers", "external": map[string]string{"file_reference": "Peer"},
	}, &ext)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	p.ext = ext.ID
	var rel idOnly
	rec = h.do(http.MethodPost, "/api/v1/schemas/relationships", p.owner, map[string]interface{}{
		"name": "orders_customers", "left_occurrence_id": p.ordersOcc, "right_occurrence_id": ext.ID,
		"predicates": []map[string]string{
			{"left_column_id": orderFields["customer_code"], "operator": "=", "right_column_name": "code"},
			{"left_column_id": orderFields["region"], "operator": "=", "right_column_name": "region"},
		},
	}, &rel)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	p.relationship = rel.ID
	var list idOnly
	rec = h.do(http.MethodPost, "/api/v1/value-lists", p.owner, map[string]interface{}{
		"name": "Customer names", "kind": "field", "source_occurrence_id": ext.ID, "source_column_name": "full_name",
	}, &list)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	p.list = list.ID
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", p.owner,
		map[string]interface{}{"database": p.peer, "peer_token": p.peerOwner, "tables": map[string]string{"Order Customers": "customers"}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for label, order := range map[string][2]string{
		"north": {"C-1", "north"}, "south": {"C-1", "south"}, "code only": {"C-2", "south"}, "empty": {"", "north"},
	} {
		var row map[string]interface{}
		rec := h.do(http.MethodPost, "/api/v1/data/orders", p.owner, map[string]string{"customer_code": order[0], "region": order[1]}, &row)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		p.orderIDs[label] = fmt.Sprint(row["id"])
	}
	return p
}

// setPeerAccess sets what alice may do with Peer's customers.
func (p *externalPair) setPeerAccess(t *testing.T, level string) {
	t.Helper()
	rec := p.h.do(http.MethodPut, "/api/v1/security/users/"+p.aliceInPeer+"/table-permissions", p.peerOwner, map[string]interface{}{
		"permissions": []map[string]interface{}{{"table_name": "customers", "access_level": level}},
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

type loginResult struct {
	Token  string               `json:"token"`
	Linked []api.LinkedSolution `json:"linked"`
}

func (p *externalPair) signIn(t *testing.T, database, password string) loginResult {
	t.Helper()
	var res loginResult
	rec := p.h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": database, "username": "alice", "password": password}, &res)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return res
}

func (p *externalPair) related(token, order string) (*http.Response, []string) {
	rec := p.h.do(http.MethodGet, fmt.Sprintf("/api/v1/data/orders/%s/related?relationship=%s", p.orderIDs[order], p.relationship), token, nil, nil)
	return rec.Result(), namesOf(rec.Code, rec.Body.Bytes())
}

func namesOf(code int, body []byte) []string {
	if code != http.StatusOK {
		return nil
	}
	var rows []map[string]interface{}
	_ = json.Unmarshal(body, &rows)
	names := []string{}
	for _, r := range rows {
		names = append(names, fmt.Sprint(r["full_name"]))
	}
	return names
}

func assertUnavailable(t *testing.T, res *http.Response, database, reason string) {
	t.Helper()
	defer res.Body.Close()
	require.Equal(t, http.StatusFailedDependency, res.StatusCode)
	var problem struct {
		Code, Reference, Database, Reason string
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&problem))
	assert.Equal(t, "external_unavailable", problem.Code)
	assert.Equal(t, "Peer", problem.Reference)
	assert.Equal(t, database, problem.Database)
	assert.Equal(t, reason, problem.Reason)
}

// Signing in to Orders signs in to Peer with the same credentials, and every
// read of Peer's records through Orders is made as alice's account there,
// with its privileges: read-only reads, no access refuses, and a group not
// signed in to Peer is told so and links it with /auth/link (#107).
func TestAPI_ExternalReadsAsTheLinkedAccount(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h

	login := p.signIn(t, p.orders, "alice-secret")
	require.Len(t, login.Linked, 1)
	linked := login.Linked[0]
	assert.Equal(t, p.peer, linked.Database)
	assert.Equal(t, "linked", linked.Status)
	assert.Equal(t, []string{"Peer"}, linked.References)
	assert.Equal(t, "alice", linked.Username)
	require.NotEmpty(t, linked.Token)
	token := login.Token

	var session struct {
		Linked []api.LinkedSolution `json:"linked"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/auth/session", token, nil, &session).Code)
	require.Len(t, session.Linked, 1)
	assert.Equal(t, p.peer, session.Linked[0].Database)
	assert.Empty(t, session.Linked[0].Token, "a token is given once")

	// Related records: both criteria hold, an empty key relates to nothing.
	t.Run("related records and fields", func(t *testing.T) {
		for order, want := range map[string][]string{"north": {"Ada"}, "south": {"Ada South"}, "code only": {}, "empty": {}} {
			res, names := p.related(token, order)
			require.Equal(t, http.StatusOK, res.StatusCode, order)
			assert.Equal(t, want, names, order)
		}
		ids := []string{p.orderIDs["north"], p.orderIDs["south"], p.orderIDs["code only"], p.orderIDs["empty"]}
		var batch struct {
			Records map[string][]map[string]interface{} `json:"records"`
		}
		rec := h.do(http.MethodPost, "/api/v1/data/orders/related", token, map[string]interface{}{
			"relationship": p.relationship, "record_ids": ids,
		}, &batch)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, batch.Records, 4)
		require.Len(t, batch.Records[p.orderIDs["north"]], 1)
		assert.Equal(t, "Ada", batch.Records[p.orderIDs["north"]][0]["full_name"])
		assert.Equal(t, "Ada South", batch.Records[p.orderIDs["south"]][0]["full_name"])
		assert.Empty(t, batch.Records[p.orderIDs["empty"]])

		tooMany := make([]string, 101)
		rec = h.do(http.MethodPost, "/api/v1/data/orders/related", token, map[string]interface{}{
			"relationship": p.relationship, "record_ids": tooMany,
		}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("value list", func(t *testing.T) {
		var values struct {
			Values []string `json:"values"`
		}
		rec := h.do(http.MethodGet, "/api/v1/value-lists/"+p.list+"/values", token, nil, &values)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, []string{"Ada", "Ada South", "Bob"}, values.Values)
	})

	t.Run("layout on the external occurrence", func(t *testing.T) {
		rec := h.do(http.MethodGet, "/api/v1/occurrences/"+p.ext+"/records?sort=full_name", token, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		names := namesOf(rec.Code, rec.Body.Bytes())
		sort.Strings(names)
		assert.Equal(t, []string{"Ada", "Ada South", "Bob"}, names)

		rec = h.do(http.MethodPost, "/api/v1/occurrences/"+p.ext+"/records/find", token, map[string]interface{}{
			"requests": []map[string]interface{}{{"criteria": []map[string]string{{"field_name": "full_name", "operator": "=", "value": "Bob"}}}},
		}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "Bob")
		assert.NotContains(t, rec.Body.String(), "Ada")

		var table schema.TableMetadata
		rec = h.do(http.MethodGet, "/api/v1/occurrences/"+p.ext+"/table", token, nil, &table)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "customers", table.Name)
		fields := []string{}
		for _, c := range table.Columns {
			fields = append(fields, c.Name)
		}
		assert.Subset(t, fields, []string{"code", "region", "full_name"})

		// The same routes serve a local occurrence as /api/v1/data does.
		rec = h.do(http.MethodGet, "/api/v1/occurrences/"+p.ordersOcc+"/records", token, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var orders []map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &orders))
		assert.Len(t, orders, 4)
		rec = h.do(http.MethodGet, "/api/v1/occurrences/no-such-occurrence/records", token, nil, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)
		// The occurrence catalog routes still answer under the same prefix.
		rec = h.do(http.MethodGet, "/api/v1/occurrences", token, nil, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	// A read-only account in Peer reads there and writes nothing: every write
	// is refused with what to do about it.
	t.Run("read-only refuses writes", func(t *testing.T) {
		var any []map[string]interface{}
		rec := h.do(http.MethodGet, "/api/v1/occurrences/"+p.ext+"/records", token, nil, &any)
		require.Equal(t, http.StatusOK, rec.Code)
		id := fmt.Sprint(any[0]["id"])
		for _, c := range []struct{ method, target string }{
			{http.MethodPost, "/api/v1/occurrences/" + p.ext + "/records"},
			{http.MethodPut, "/api/v1/occurrences/" + p.ext + "/records/" + id},
			{http.MethodPatch, "/api/v1/occurrences/" + p.ext + "/records/" + id},
			{http.MethodDelete, "/api/v1/occurrences/" + p.ext + "/records/" + id},
		} {
			rec := h.do(c.method, c.target, token, map[string]string{"full_name": "Eve"}, nil)
			require.Equal(t, http.StatusForbidden, rec.Code, c.method+" "+rec.Body.String())
			assert.Contains(t, rec.Body.String(), p.peer, c.method)
			assert.Contains(t, rec.Body.String(), `\"Peer\"`, c.method)
			assert.Contains(t, rec.Body.String(), "ask an administrator", c.method)
		}
	})

	t.Run("no access in the other solution", func(t *testing.T) {
		p.setPeerAccess(t, schema.AccessNone)
		defer p.setPeerAccess(t, schema.AccessReadOnly)
		res, _ := p.related(token, "north")
		assertUnavailable(t, res, p.peer, "no_access")
		rec := h.do(http.MethodPost, "/api/v1/data/orders/related", token, map[string]interface{}{
			"relationship": p.relationship, "record_ids": []string{p.orderIDs["north"]},
		}, nil)
		assertUnavailable(t, rec.Result(), p.peer, "no_access")
		rec = h.do(http.MethodGet, "/api/v1/occurrences/"+p.ext+"/records", token, nil, nil)
		assertUnavailable(t, rec.Result(), p.peer, "no_access")
		rec = h.do(http.MethodGet, "/api/v1/value-lists/"+p.list+"/values", token, nil, nil)
		assertUnavailable(t, rec.Result(), p.peer, "no_access")
	})

	t.Run("a field the other solution does not have", func(t *testing.T) {
		var list struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/value-lists", p.owner, map[string]interface{}{
			"name": "Nicknames", "kind": "field", "source_occurrence_id": p.ext, "source_column_name": "nickname",
		}, &list)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		rec = h.do(http.MethodGet, "/api/v1/value-lists/"+list.ID+"/values", token, nil, nil)
		assertUnavailable(t, rec.Result(), p.peer, "schema_mismatch")
	})

	t.Run("not signed in, then linked", func(t *testing.T) {
		rec := h.do(http.MethodDelete, "/api/v1/auth/link/"+p.peer, token, nil, nil)
		require.Equal(t, http.StatusNoContent, rec.Code)
		rec = h.do(http.MethodGet, "/api/v1/auth/session", linked.Token, nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "an unlinked session is revoked")
		res, _ := p.related(token, "north")
		assertUnavailable(t, res, p.peer, "not_signed_in")
		rec = h.do(http.MethodGet, "/api/v1/occurrences/"+p.ext+"/records", token, nil, nil)
		assertUnavailable(t, rec.Result(), p.peer, "not_signed_in")

		link := func(database, user, password string) (int, api.LinkedSolution) {
			var out api.LinkedSolution
			rec := h.do(http.MethodPost, "/api/v1/auth/link", token, map[string]string{"database": database, "username": user, "password": password}, nil)
			if rec.Code == http.StatusOK {
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
			}
			return rec.Code, out
		}
		code, _ := link(p.peer, "alice", "wrong")
		assert.Equal(t, http.StatusUnauthorized, code)
		code, _ = link(p.orders, "alice", "alice-secret")
		assert.Equal(t, http.StatusConflict, code, "the group is signed in to its own solution")
		code, _ = link("f4b_no_such_solution", "alice", "x")
		assert.Equal(t, http.StatusNotFound, code, "only a solution this one reads")

		// Linked as another account of Peer: its privileges apply.
		code, out := link(p.peer, "bob", "bob-secret")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "bob", out.Username)
		assert.Equal(t, []string{"Peer"}, out.References)
		res, names := p.related(token, "north")
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, []string{"Ada"}, names)

		code, _ = link(p.peer, "alice", "alice-secret")
		require.Equal(t, http.StatusOK, code)
		var session struct {
			Linked []api.LinkedSolution `json:"linked"`
		}
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/auth/session", token, nil, &session).Code)
		require.Len(t, session.Linked, 1, "a link replaces the one the group held there")
		assert.Equal(t, "alice", session.Linked[0].Username)
	})

	t.Run("an account changed in the other solution is signed out there", func(t *testing.T) {
		rec := h.do(http.MethodPut, "/api/v1/security/users/"+p.aliceInPeer, p.peerOwner,
			map[string]interface{}{"password": "alice-new-secret", "role": "user"}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		res, _ := p.related(token, "north")
		assertUnavailable(t, res, p.peer, "not_signed_in")
	})

	t.Run("signing out signs out of every linked solution", func(t *testing.T) {
		login := p.signIn(t, p.orders, "alice-secret")
		require.Len(t, login.Linked, 1)
		assert.Equal(t, "refused", login.Linked[0].Status, "Peer's password changed")
		rec := h.do(http.MethodPost, "/api/v1/auth/link", login.Token, map[string]string{"database": p.peer, "username": "alice", "password": "alice-new-secret"}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var member api.LinkedSolution
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &member))
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/auth/session", member.Token, nil, nil).Code)

		require.Equal(t, http.StatusNoContent, h.do(http.MethodPost, "/api/v1/auth/logout", login.Token, nil, nil).Code)
		assert.Equal(t, http.StatusUnauthorized, h.do(http.MethodGet, "/api/v1/auth/session", member.Token, nil, nil).Code)
	})
}

// Two solutions that read each other link each other once: the group's
// own sign-in answers for the solution it started in (#107).
func TestAPI_ExternalCycleLinksEachSolutionOnce(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h

	// Peer reads Orders back.
	var ref struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/file-references", p.peerOwner, map[string]interface{}{"name": "Orders"}, &ref)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/occurrences", p.peerOwner, map[string]interface{}{
		"name": "Customer Orders", "external": map[string]string{"file_reference": "Orders"},
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+ref.ID+"/bind", p.peerOwner,
		map[string]interface{}{"database": p.orders, "peer_token": p.owner, "tables": map[string]string{"Customer Orders": "orders"}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	login := p.signIn(t, p.orders, "alice-secret")
	require.Len(t, login.Linked, 1, "Orders is not linked to itself")
	assert.Equal(t, p.peer, login.Linked[0].Database)
	assert.Equal(t, "linked", login.Linked[0].Status)

	login = p.signIn(t, p.peer, "alice-secret")
	require.Len(t, login.Linked, 1)
	assert.Equal(t, p.orders, login.Linked[0].Database)

	// Signed in to Peer, Orders' records are read as alice in Orders.
	var occurrences []schema.TableOccurrence
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", login.Token, nil, &occurrences).Code)
	back := ""
	for _, o := range occurrences {
		if o.Name == "Customer Orders" {
			back = o.ID
		}
	}
	require.NotEmpty(t, back)
	rec = h.do(http.MethodGet, "/api/v1/occurrences/"+back+"/records", login.Token, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var orders []map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &orders))
	assert.Len(t, orders, 4)
}

// Signing in to a solution never fails because a solution it reads refuses
// the same credentials, and those automatic attempts count against that
// solution's own lockout: signing in to Orders cannot guess Peer's
// passwords (#107).
func TestAPI_ExternalAutoLinkIsThrottled(t *testing.T) {
	th := auth.NewLoginThrottle(2, time.Minute, time.Hour)
	p := newExternalPair(t, api.Options{LoginThrottle: th}, "another-secret")

	statuses := []string{}
	for i := 0; i < 3; i++ {
		login := p.signIn(t, p.orders, "alice-secret")
		require.Len(t, login.Linked, 1)
		assert.Empty(t, login.Linked[0].Token)
		statuses = append(statuses, login.Linked[0].Status)
	}
	assert.Equal(t, []string{"refused", "throttled", "throttled"}, statuses)

	// Peer is locked out for alice, even with her right password there.
	login := p.signIn(t, p.orders, "alice-secret")
	rec := p.h.do(http.MethodPost, "/api/v1/auth/link", login.Token,
		map[string]string{"database": p.peer, "username": "alice", "password": "another-secret"}, nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	// Another account of Peer is not.
	rec = p.h.do(http.MethodPost, "/api/v1/auth/link", login.Token,
		map[string]string{"database": p.peer, "username": "bob", "password": "bob-secret"}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// A field of Peer known in Orders only by its FileMaker id ("#11") is read,
// in a relationship predicate and a value list, as the field of Peer
// migrated with that id: the one binding reports it resolved to. An id no
// field of Peer has is a schema mismatch (#107).
func TestAPI_ExternalMarkersReadTheFieldBoundToTheirID(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	driver, err := h.mgr.DriverFor(ctx, p.peer)
	require.NoError(t, err)
	for field, id := range map[string]string{"code": "11", "full_name": "13"} {
		_, err := driver.DB().ExecContext(ctx, h.rebind(`UPDATE sys_columns SET source_field_id = $1
			WHERE name = $2 AND table_id = (SELECT id FROM sys_tables WHERE name = 'customers')`), id, field)
		require.NoError(t, err)
	}

	var ordersFields map[string]string
	for _, tbl := range h.observeTables(p.owner) {
		if tbl.Name == "orders" {
			ordersFields = map[string]string{}
			for _, c := range tbl.Columns {
				ordersFields[c.Name] = c.ID
			}
		}
	}
	require.NotNil(t, ordersFields)
	relationship := func(name, code string) string {
		var rel struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/schemas/relationships", p.owner, map[string]interface{}{
			"name": name, "left_occurrence_id": p.ordersOcc, "right_occurrence_id": p.ext,
			"predicates": []map[string]string{
				{"left_column_id": ordersFields["customer_code"], "operator": "=", "right_column_name": code},
				{"left_column_id": ordersFields["region"], "operator": "=", "right_column_name": "region"},
			},
		}, &rel)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		return rel.ID
	}
	byID, unknown := relationship("orders_customers_by_id", "#11"), relationship("orders_customers_unknown", "#99")
	var list struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/value-lists", p.owner, map[string]interface{}{
		"name": "Customer names by id", "kind": "field", "source_occurrence_id": p.ext, "source_column_name": "#13",
	}, &list)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var refs []schema.FileReference
	refs = h.observeFileReferences(p.owner)
	require.Len(t, refs, 1)
	var report schema.ExternalBindingReport
	rec = h.do(http.MethodPost, "/api/v1/file-references/"+refs[0].ID+"/verify", p.owner, map[string]string{"peer_token": p.peerOwner}, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, report.Occurrences, 1)
	assert.Equal(t, map[string]string{"#11": "code", "#13": "full_name"}, report.Occurrences[0].Resolved)

	token := p.signIn(t, p.orders, "alice-secret").Token
	related := func(relationship, order string) *httptest.ResponseRecorder {
		return h.do(http.MethodGet, fmt.Sprintf("/api/v1/data/orders/%s/related?relationship=%s", p.orderIDs[order], relationship), token, nil, nil)
	}
	for order, want := range map[string][]string{"north": {"Ada"}, "south": {"Ada South"}, "code only": {}} {
		rec := related(byID, order)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, want, namesOf(rec.Code, rec.Body.Bytes()), order)
	}
	var batch struct {
		Records map[string][]map[string]interface{} `json:"records"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/orders/related", token, map[string]interface{}{
		"relationship": byID, "record_ids": []string{p.orderIDs["north"]},
	}, &batch)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, batch.Records[p.orderIDs["north"]], 1)
	assert.Equal(t, "Ada", batch.Records[p.orderIDs["north"]][0]["full_name"])

	var values struct {
		Values []string `json:"values"`
	}
	rec = h.do(http.MethodGet, "/api/v1/value-lists/"+list.ID+"/values", token, nil, &values)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"Ada", "Ada South", "Bob"}, values.Values)

	rec = related(unknown, "north")
	require.Equal(t, http.StatusFailedDependency, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "schema_mismatch")
}
