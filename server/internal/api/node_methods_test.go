package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// No method is registered with chi: chi keeps its methods in the bits of an
// int, so the methods Node knows would overflow it on a 32-bit build and
// panic at start-up. chi therefore still refuses them on its own, which this
// binary's package init would have changed.
func TestNoMethodIsRegisteredWithChi(t *testing.T) {
	r := chi.NewRouter()
	r.Handle("/x", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(299) }))
	for m := range nodeOnlyMethods {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(m, "/x", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s reached a chi route (%d): a method was registered with chi", m, w.Code)
		}
	}

	// And none is registered anywhere in the server's code.
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(src), "RegisterMethod(") {
			t.Errorf("%s calls chi.RegisterMethod", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A method only Node knows reaches the Data API on a /fmi/data path, and
// still gets chi's 405 everywhere else.
func TestDataAPIMethodsMiddleware(t *testing.T) {
	r := chi.NewRouter()
	r.Use(dataAPIMethods(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(298) })))
	r.Handle("/fmi/data/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(299) }))
	r.Get("/api/v1/x", func(http.ResponseWriter, *http.Request) {})
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"PROPFIND", "/fmi/data/v1/productInfo", 298},
		{"M-SEARCH", "/fmi/data", 298},
		{"PROPFIND", "/api/v1/x", http.StatusMethodNotAllowed},
		{"PROPFIND", "/fmi/database", http.StatusMethodNotAllowed},
		{"propfind", "/fmi/data/v1/productInfo", http.StatusMethodNotAllowed},
		{"GET", "/fmi/data/v1/productInfo", 299},
		{"QUERY", "/fmi/data/v1/productInfo", 299},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, w.Code, c.want)
		}
	}
}
