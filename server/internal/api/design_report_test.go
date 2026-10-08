package api_test

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getRaw performs a GET and returns the recorder, for the endpoints that
// answer a document rather than JSON.
func getRaw(t *testing.T, h *apiHarness, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// The design report (#49): what the solution is made of, as a page to read
// and as text for version control.
func TestAPI_DesignReport(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_ddr")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")
	src := buildSourceSolution(t, h, owner)
	_ = src

	// ─── HTML: the report to read ─────────────────────────────────────────
	rec := getRaw(t, h, "/api/v1/solutions/design-report?format=html&solution=Invoices", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "Invoices_design.html")

	page := rec.Body.String()
	for _, expected := range []string{
		"<!doctype html>", "Invoices", "Tables", "Relationships", "Layouts", "Scripts",
		"customers", "invoices", "Accounts and privileges", "alice",
		"What this report does not describe",
	} {
		assert.Contains(t, page, expected)
	}
	// It is one file: no script to run and nothing to fetch.
	assert.NotContains(t, page, "<script")
	assert.NotContains(t, page, "http://")

	// ─── JSON: the design as text ─────────────────────────────────────────
	rec = getRaw(t, h, "/api/v1/solutions/design-report?format=json&solution=Invoices", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	first := rec.Body.String()

	var report struct {
		Format   string `json:"format"`
		Version  string `json:"version"`
		Solution string `json:"solution"`
		Counts   struct {
			Tables, Fields, Layouts, Scripts, Relationships int
		} `json:"counts"`
		Tables []struct {
			Name   string `json:"name"`
			Fields []struct {
				Name       string          `json:"name"`
				Type       string          `json:"type"`
				Validation json.RawMessage `json:"validation"`
			} `json:"fields"`
		} `json:"tables"`
		Relationships []struct {
			Name       string `json:"name"`
			Left       string `json:"left"`
			LeftField  string `json:"left_field"`
			Right      string `json:"right"`
			RightField string `json:"right_field"`
		} `json:"relationships"`
		Layouts []struct {
			Name       string          `json:"name"`
			Definition json.RawMessage `json:"definition"`
		} `json:"layouts"`
		Accounts []struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"accounts"`
		GeneratedAt string   `json:"generated_at"`
		Notes       []string `json:"notes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	assert.Equal(t, "file4base_design", report.Format)
	assert.Equal(t, "Invoices", report.Solution)
	assert.Equal(t, 2, report.Counts.Tables)
	assert.Positive(t, report.Counts.Fields)
	assert.Positive(t, report.Counts.Relationships)
	require.NotEmpty(t, report.Layouts)
	assert.NotEmpty(t, report.Layouts[0].Definition, "the layout travels verbatim, for a diff to work on")
	assert.NotEmpty(t, report.Notes)

	// No timestamp in the text form, so the same design written twice is the
	// same bytes — which is what makes it worth committing.
	assert.Empty(t, report.GeneratedAt)
	rec = getRaw(t, h, "/api/v1/solutions/design-report?format=json&solution=Invoices", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, first, rec.Body.String(), "two reports of one design are identical")

	// ─── Nothing secret, in any format ────────────────────────────────────
	for _, format := range []string{"html", "xml", "json"} {
		rec = getRaw(t, h, "/api/v1/solutions/design-report?format="+format+"&solution=Invoices", owner)
		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.NotContains(t, body, "alice-secret", format)
		assert.NotContains(t, strings.ToLower(body), "password_hash", format)
		assert.NotContains(t, body, "$2a$", format)
	}

	// ─── XML: the same design, parseable ──────────────────────────────────
	rec = getRaw(t, h, "/api/v1/solutions/design-report?format=xml&solution=Invoices", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/xml")
	var parsed struct {
		XMLName  xml.Name `xml:"design"`
		Solution string   `xml:"solution,attr"`
		Tables   []struct {
			Name   string `xml:"name,attr"`
			Fields []struct {
				Name string `xml:"name,attr"`
				Type string `xml:"type,attr"`
			} `xml:"fields>field"`
		} `xml:"tables>table"`
		Scripts []struct {
			Name  string `xml:"name,attr"`
			Steps []struct {
				Type       string `xml:"type,attr"`
				Parameters string `xml:"parameters"`
			} `xml:"steps>step"`
		} `xml:"scripts>script"`
	}
	require.NoError(t, xml.Unmarshal(rec.Body.Bytes(), &parsed), rec.Body.String())
	assert.Equal(t, "Invoices", parsed.Solution)
	require.Len(t, parsed.Tables, 2)
	assert.NotEmpty(t, parsed.Tables[0].Fields)

	// ─── Who may read it ──────────────────────────────────────────────────
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(db, "carol", "carol-secret-pw")
	assert.Equal(t, http.StatusForbidden,
		getRaw(t, h, "/api/v1/solutions/design-report?format=html", user).Code)
	assert.Equal(t, http.StatusUnauthorized,
		getRaw(t, h, "/api/v1/solutions/design-report?format=html", "").Code)

	// A format File4Base does not write is refused rather than guessed.
	assert.Equal(t, http.StatusUnprocessableEntity,
		getRaw(t, h, "/api/v1/solutions/design-report?format=pdf", owner).Code)
}
