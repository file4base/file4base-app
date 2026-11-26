package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func corsRequest(t *testing.T, allowed []string, method, origin string) *httptest.ResponseRecorder {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(method, "/api/v1/databases", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	}
	rec := httptest.NewRecorder()
	corsMiddleware(allowed)(next).ServeHTTP(rec, req)
	return rec
}

// The default (CORS_ALLOWED_ORIGINS unset) grants no cross-origin access:
// no response carries an Access-Control-* header, so a browser lets only
// same-origin pages read it (#202).
func TestCORS_DefaultGrantsNoCrossOriginAccess(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodOptions} {
		rec := corsRequest(t, splitList(""), method, "https://attacker.example")
		for name := range rec.Header() {
			if strings.HasPrefix(name, "Access-Control-") {
				t.Errorf("%s: unexpected %s header with the default configuration", method, name)
			}
		}
	}
	// A same-origin request (the bundled Web Client through its Nginx) is
	// served normally; browsers need no CORS header for it.
	if rec := corsRequest(t, splitList(""), http.MethodGet, ""); rec.Code != http.StatusOK {
		t.Fatalf("request without Origin: got %d", rec.Code)
	}
}

func TestCORS_ListedOriginsOnly(t *testing.T) {
	allowed := splitList("https://files.example.lan:3443, http://localhost:3000/")

	rec := corsRequest(t, allowed, http.MethodOptions, "https://files.example.lan:3443")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://files.example.lan:3443" {
		t.Fatalf("listed origin: Access-Control-Allow-Origin = %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("listed origin: preflight lacks Access-Control-Allow-Headers")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("listed origin: Vary = %q", got)
	}
	// A trailing slash in the setting does not matter.
	if got := corsRequest(t, allowed, http.MethodGet, "http://localhost:3000").Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("listed origin with trailing slash: Access-Control-Allow-Origin = %q", got)
	}

	for _, origin := range []string{"https://attacker.example", "http://files.example.lan:3443", "https://files.example.lan"} {
		rec := corsRequest(t, allowed, http.MethodOptions, origin)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("unlisted origin %s: Access-Control-Allow-Origin = %q", origin, got)
		}
	}
}

// "*" stays available as an explicit choice, with a startup warning.
func TestCORS_WildcardIsExplicitAndWarned(t *testing.T) {
	if got := corsRequest(t, splitList("*"), http.MethodGet, "https://any.example").Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("wildcard: Access-Control-Allow-Origin = %q", got)
	}
	if !strings.Contains(strings.Join(corsNotices(splitList("*")), "\n"), "Warning") {
		t.Fatal("wildcard: no startup warning")
	}
}

func TestCORSNotices(t *testing.T) {
	if got := strings.Join(corsNotices(nil), "\n"); strings.Contains(got, "Warning") || !strings.Contains(got, "no cross-origin") {
		t.Fatalf("default notice = %q", got)
	}
	got := strings.Join(corsNotices(splitList("files.example.lan:3000,https://ok.example,http://x.example/app")), "\n")
	for _, bad := range []string{`"files.example.lan:3000"`, `"http://x.example/app"`} {
		if !strings.Contains(got, bad) {
			t.Errorf("no warning for %s in %q", bad, got)
		}
	}
	if strings.Contains(got, `"https://ok.example" is not`) {
		t.Errorf("valid origin flagged: %q", got)
	}
}

func TestPublicDatabaseCreationIsOffByDefault(t *testing.T) {
	t.Setenv("ALLOW_PUBLIC_DATABASE_CREATION", "")
	if publicDatabaseCreation() {
		t.Fatal("unset ALLOW_PUBLIC_DATABASE_CREATION allows public creation")
	}
	t.Setenv("ALLOW_PUBLIC_DATABASE_CREATION", "not-a-bool")
	if publicDatabaseCreation() {
		t.Fatal("an invalid value allows public creation")
	}
	t.Setenv("ALLOW_PUBLIC_DATABASE_CREATION", "true")
	if !publicDatabaseCreation() {
		t.Fatal("ALLOW_PUBLIC_DATABASE_CREATION=true is ignored")
	}
	if !strings.HasPrefix(publicCreationNotice(true), "Warning") {
		t.Fatal("public creation on: no startup warning")
	}
	if !strings.Contains(publicCreationNotice(false), "ALLOW_PUBLIC_DATABASE_CREATION=true") {
		t.Fatal("public creation off: the notice does not say how to create the first solution")
	}
}
