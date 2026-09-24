package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
