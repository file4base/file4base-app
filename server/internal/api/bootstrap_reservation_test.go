package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
)

type reservedProblem struct {
	Detail            string `json:"detail"`
	ReservedDatabase  string `json:"reserved_database"`
	SetupCodeRequired bool   `json:"setup_code_required"`
}

// The databases whose owners become server administrators cannot be taken
// by whoever creates them first (#236): only a super-owner, or the operator
// with the setup code, may create, clone or migrate into them.
func TestBootstrap_ReservedDatabasesNeedSuperOwnerOrSetupCode(t *testing.T) {
	opsDB, spareDB, adminDB, otherDB := uniqueDB("f4b_ops"), uniqueDB("f4b_spare"), uniqueDB("f4b_adm"), uniqueDB("f4b_other")
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		AdminDatabase:               adminDB,
		SuperOwnerSeed:              []string{opsDB + ":root", spareDB + ":deputy"},
	})
	h.createDatabase(otherDB, "mallory", "mallory-secret-pw")
	mallory := h.login(otherDB, "mallory", "mallory-secret-pw")
	ctx := context.Background()
	exists := func(db string) bool {
		_, ok, err := h.mgr.LookupDatabase(ctx, db)
		require.NoError(t, err)
		return ok
	}
	create := func(token string, body map[string]string) (int, reservedProblem) {
		var p reservedProblem
		rec := h.do(http.MethodPost, "/api/v1/databases", token, body, &p)
		if rec.Code == http.StatusCreated {
			t.Cleanup(func() { _ = h.mgr.DropDatabase(ctx, body["database"]) })
		}
		return rec.Code, p
	}

	for _, db := range []string{opsDB, adminDB} {
		// Anonymous, then an owner of another database: refused, nothing created.
		for _, token := range []string{"", mallory} {
			code, p := create(token, map[string]string{"database": db, "user": "root", "password": "squatter-secret-pw"})
			assert.Equal(t, http.StatusForbidden, code, db)
			assert.Equal(t, db, p.ReservedDatabase)
			assert.True(t, p.SetupCodeRequired)
			assert.Contains(t, p.Detail, "setup code")
		}
		code, p := create("", map[string]string{"database": db, "user": "root", "password": "squatter-secret-pw", "setup_code": "WRONG-CODE"})
		assert.Equal(t, http.StatusForbidden, code)
		assert.Contains(t, p.Detail, "not valid")

		// Clone and migration into it, by an owner of another database or anonymously.
		var cp reservedProblem
		rec := h.do(http.MethodPost, "/api/v1/solutions/clone", mallory,
			map[string]string{"target_database": db, "owner_user": "root", "owner_password": "squatter-secret-pw"}, &cp)
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.False(t, cp.SetupCodeRequired, "clone takes no setup code")
		for _, token := range []string{"", mallory} {
			rec = h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", token,
				map[string]string{"design_xml": "<FMSaveAsXML/>", "target_database": db, "owner_user": "root", "owner_password": "squatter-secret-pw"}, nil)
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		}
		assert.False(t, exists(db), "%s was not created", db)
	}

	// The operator creates the seeded database with the setup code, typed in
	// any case and grouping; its seeded owner signs in as a super-owner.
	code, _ := create("", map[string]string{"database": opsDB, "user": "root", "password": "root-secret-pw", "setup_code": " test-setup-code0236 "})
	require.Equal(t, http.StatusCreated, code)
	root := h.login(opsDB, "root", "root-secret-pw")
	var access accessView
	h.do(http.MethodGet, "/api/v1/server/access", root, nil, &access)
	assert.True(t, access.SuperOwner)

	// A super-owner needs no code, for creation as for clone.
	code, _ = create(root, map[string]string{"database": adminDB, "user": "admin", "password": "admin-secret-pw"})
	assert.Equal(t, http.StatusCreated, code)
	rec := h.do(http.MethodPost, "/api/v1/solutions/clone", root,
		map[string]string{"target_database": spareDB, "owner_user": "deputy", "owner_password": "deputy-secret-pw"}, nil)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	if rec.Code == http.StatusCreated {
		t.Cleanup(func() { _ = h.mgr.DropDatabase(ctx, spareDB) })
	}

	// Other names are not affected.
	code, _ = create("", map[string]string{"database": uniqueDB("f4b_free"), "user": "eve", "password": "eve-secret-pw"})
	assert.Equal(t, http.StatusCreated, code)
}
