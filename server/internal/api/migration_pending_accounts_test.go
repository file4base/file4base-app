package api_test

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #198 — the accounts a FileMaker migration creates have no usable password.
// The apply response names them, the account list keeps saying so after the
// migration window is closed, and setting a password is what lets them sign
// in and clears the mark.
func TestAPI_FileMakerMigrationAccountsPendingPassword(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	dbName := uniqueDB("f4b_mig_pending")
	h.createDatabase(dbName, "alice", "alice-secret")
	ownerTok := h.login(dbName, "alice", "alice-secret")

	ddrXML := `<?xml version="1.0" encoding="UTF-8"?>
<FMPReport type="Report" version="19.0">
  <File name="Greenhouse.fmp12">
    <BaseTableCatalog>
      <BaseTable name="Plants">
        <FieldCatalog>
          <Field name="Code" dataType="Text" fieldType="Normal"/>
        </FieldCatalog>
      </BaseTable>
    </BaseTableCatalog>
    <TableOccurrenceCatalog>
      <TableOccurrence name="Plants" baseTable="Plants"/>
    </TableOccurrenceCatalog>
    <LayoutCatalog>
      <Layout name="Plant Form" table="Plants">
        <Part type="body" height="300"/>
        <Field name="Code"/>
      </Layout>
    </LayoutCatalog>
    <AccountCatalog>
      <Account name="botanist" privilegeSet="[Data Entry Only]"/>
      <Account name="gardener" privilegeSet="[Data Entry Only]"/>
      <Account name="keeper" privilegeSet="[Full Access]"/>
    </AccountCatalog>
  </File>
</FMPReport>`

	var applyRes struct {
		Status       string `json:"status"`
		ImportReport struct {
			AccountsCreated         int      `json:"accounts_created"`
			AccountsPendingPassword []string `json:"accounts_pending_password"`
		} `json:"import_report"`
	}
	rec := h.do(http.MethodPost, "/api/v1/solutions/migrate/apply", ownerTok, map[string]interface{}{
		"design_xml": ddrXML,
	}, &applyRes)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, 3, applyRes.ImportReport.AccountsCreated)
	assert.ElementsMatch(t, []string{"botanist", "gardener", "keeper"}, applyRes.ImportReport.AccountsPendingPassword)

	byName := func() map[string]schema.UserMetadata {
		out := map[string]schema.UserMetadata{}
		for _, u := range h.observeUsers(ownerTok) {
			out[u.Username] = u
		}
		return out
	}
	users := byName()
	require.Contains(t, users, "botanist")
	require.Contains(t, users, "gardener")
	assert.True(t, users["botanist"].PasswordPending)
	assert.False(t, users["botanist"].IsActive, "a migrated account cannot sign in")
	assert.False(t, users["alice"].PasswordPending, "the owner who migrated has a password")

	// Switching the account on without a password leaves it waiting for one.
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+users["gardener"].ID, ownerTok, map[string]interface{}{
		"role": users["gardener"].Role, "is_active": true,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, byName()["gardener"].PasswordPending)

	// A blank password would clear the mark of an account that still cannot
	// sign in: it is refused.
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+users["gardener"].ID, ownerTok, map[string]interface{}{
		"password": "   ",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.True(t, byName()["gardener"].PasswordPending)

	// An owner switched on without a password cannot sign in, so it does not
	// count as the active owner the database must keep.
	require.Equal(t, "owner", users["keeper"].Role)
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+users["keeper"].ID, ownerTok, map[string]interface{}{
		"is_active": true,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+users["alice"].ID, ownerTok, map[string]interface{}{
		"role": "admin",
	}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "alice is the only owner who can sign in: "+rec.Body.String())
	assert.Equal(t, "owner", byName()["alice"].Role)

	// Setting a password and enabling the account lets it sign in.
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+users["botanist"].ID, ownerTok, map[string]interface{}{
		"role": users["botanist"].Role, "password": "botanist-secret-1", "is_active": true,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	after := byName()
	assert.False(t, after["botanist"].PasswordPending)
	assert.True(t, after["botanist"].IsActive)
	assert.True(t, after["gardener"].PasswordPending, "only the account given a password changes")
	assert.NotEmpty(t, h.login(dbName, "botanist", "botanist-secret-1"))

	// An account given a password cannot clear another's mark by itself.
	botanistTok := h.login(dbName, "botanist", "botanist-secret-1")
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+users["gardener"].ID, botanistTok, map[string]interface{}{
		"password": "hijack-attempt-1",
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.True(t, byName()["gardener"].PasswordPending)
}

// The accounts a solution import creates are marked the same way (#198).
func TestAPI_SolutionImportMarksAccountsPendingPassword(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	srcDB, dstDB := uniqueDB("f4b_pend_src"), uniqueDB("f4b_pend_dst")
	h.createDatabase(srcDB, "alice", "alice-secret")
	h.createDatabase(dstDB, "olivia", "olivia-secret")
	srcToken := h.login(srcDB, "alice", "alice-secret")
	dstToken := h.login(dstDB, "olivia", "olivia-secret")
	_ = buildSourceSolution(t, h, srcToken)
	file := exportSolution(t, h, srcToken, map[string]interface{}{"solution_name": "Pending Accounts"})

	rec := h.postBytes("/api/v1/solutions/import", dstToken, file, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	pending := map[string]bool{}
	for _, u := range h.observeUsers(dstToken) {
		pending[u.Username] = u.PasswordPending
	}
	assert.Equal(t, map[string]bool{"alice": true, "bob": true, "olivia": false}, pending)
}
