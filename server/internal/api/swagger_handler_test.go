package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwaggerHandler(t *testing.T) {
	handler, err := api.NewSwaggerHandler()
	require.NoError(t, err)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	t.Run("OpenAPI JSON Spec direct", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/swagger/openapi.json", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

		var spec map[string]interface{}
		err := json.Unmarshal(rec.Body.Bytes(), &spec)
		require.NoError(t, err)
		assert.Equal(t, "3.0.3", spec["openapi"])

		info := spec["info"].(map[string]interface{})
		assert.Equal(t, "File4Base API Engine", info["title"])
	})

	t.Run("Swagger UI Index HTML", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/swagger/", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "<title>File4Base API - Swagger UI</title>")
		assert.Contains(t, rec.Body.String(), "SwaggerUIBundle")
	})

	t.Run("Swagger Redirects", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/swagger", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMovedPermanently, rec.Code)
		assert.Equal(t, "/swagger/", rec.Header().Get("Location"))

		reqDocs := httptest.NewRequest("GET", "/docs", nil)
		recDocs := httptest.NewRecorder()
		r.ServeHTTP(recDocs, reqDocs)

		assert.Equal(t, http.StatusMovedPermanently, recDocs.Code)
		assert.Equal(t, "/swagger/", recDocs.Header().Get("Location"))
	})
}

// The vendored Swagger UI ships with its legal files (#7): every license file
// named in a script banner is served next to the script, and so are the
// Apache-2.0 LICENSE and NOTICE.
func TestSwaggerHandler_ServesThirdPartyLicenses(t *testing.T) {
	handler, err := api.NewSwaggerHandler()
	require.NoError(t, err)
	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}

	banner := regexp.MustCompile(`see ([\w.-]+\.LICENSE\.txt)`)
	for _, script := range []string{"swagger-ui-bundle.js", "swagger-ui-standalone-preset.js"} {
		rec := get("/swagger/" + script)
		require.Equal(t, http.StatusOK, rec.Code, script)
		head := rec.Body.String()
		if len(head) > 200 {
			head = head[:200]
		}
		m := banner.FindStringSubmatch(head)
		require.NotNil(t, m, "%s has no license banner", script)
		notice := get("/swagger/" + m[1])
		assert.Equal(t, http.StatusOK, notice.Code, m[1])
		assert.Contains(t, notice.Body.String(), "@license", m[1])
	}

	license := get("/swagger/LICENSE")
	assert.Equal(t, http.StatusOK, license.Code)
	assert.Contains(t, license.Body.String(), "Apache License")
	notice := get("/swagger/NOTICE")
	assert.Equal(t, http.StatusOK, notice.Code)
	assert.Contains(t, notice.Body.String(), "swagger-ui")
}
