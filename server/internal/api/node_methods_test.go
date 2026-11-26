package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// A method only Node knows reaches a route registered for every method,
// as the Data API's are, and still gets chi's 405 on a route that names its
// methods.
func TestNodeOnlyMethodsReachDataAPIRoutes(t *testing.T) {
	r := chi.NewRouter()
	r.Handle("/fmi/data/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(299) }))
	r.Get("/api/v1/x", func(http.ResponseWriter, *http.Request) {})
	for path, want := range map[string]int{"/fmi/data/v1/productInfo": 299, "/api/v1/x": http.StatusMethodNotAllowed} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PROPFIND", path, nil))
		if w.Code != want {
			t.Errorf("PROPFIND %s: %d, want %d", path, w.Code, want)
		}
	}
}
