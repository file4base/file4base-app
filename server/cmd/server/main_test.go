package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// corsRequest sends one request through the middleware configured the way
// main configures it, from a CORS_ALLOWED_ORIGINS value.
func corsRequest(t *testing.T, setting, method, origin string) *httptest.ResponseRecorder {
	t.Helper()
	allowed, _ := parseCORSOrigins(setting)
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
		rec := corsRequest(t, "", method, "https://attacker.example")
		for name := range rec.Header() {
			if strings.HasPrefix(name, "Access-Control-") {
				t.Errorf("%s: unexpected %s header with the default configuration", method, name)
			}
		}
	}
	// A same-origin request (the bundled Web Client through its Nginx) is
	// served normally; browsers need no CORS header for it.
	if rec := corsRequest(t, "", http.MethodGet, ""); rec.Code != http.StatusOK {
		t.Fatalf("request without Origin: got %d", rec.Code)
	}
}

func TestCORS_ListedOriginsOnly(t *testing.T) {
	allowed := "https://files.example.lan:3443, http://localhost:3000/"

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

	for _, origin := range []string{"https://attacker.example", "http://files.example.lan:3443", "https://files.example.lan", ""} {
		rec := corsRequest(t, allowed, http.MethodOptions, origin)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("unlisted origin %q: Access-Control-Allow-Origin = %q", origin, got)
		}
		// The answer depends on Origin, so a cache must key on it even
		// when no CORS header was sent.
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("unlisted origin %q: Vary = %q", origin, got)
		}
	}
}

// "*" stays available as an explicit choice, with a startup warning.
func TestCORS_WildcardIsExplicitAndWarned(t *testing.T) {
	if got := corsRequest(t, "*", http.MethodGet, "https://any.example").Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("wildcard: Access-Control-Allow-Origin = %q", got)
	}
	if _, notices := parseCORSOrigins("*"); !strings.Contains(strings.Join(notices, "\n"), "Warning") {
		t.Fatal("wildcard: no startup warning")
	}
	// Only an exact "*" is the wildcard: "*/" or "https://*" are dropped
	// with a warning, and never widen access.
	for _, setting := range []string{"*/", "https://*", " */ ,"} {
		rec := corsRequest(t, setting, http.MethodGet, "https://any.example")
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("setting %q: Access-Control-Allow-Origin = %q", setting, got)
		}
	}
}

func TestParseCORSOrigins(t *testing.T) {
	allowed, notices := parseCORSOrigins("")
	if got := strings.Join(notices, "\n"); allowed != nil || strings.Contains(got, "Warning") || !strings.Contains(got, "no cross-origin") {
		t.Fatalf("default: allowed = %q, notices = %q", allowed, got)
	}
	allowed, notices = parseCORSOrigins("files.example.lan:3000,https://ok.example/,http://x.example/app")
	if len(allowed) != 1 || allowed[0] != "https://ok.example" {
		t.Fatalf("allowed = %q, want only https://ok.example", allowed)
	}
	got := strings.Join(notices, "\n")
	for _, bad := range []string{`"files.example.lan:3000"`, `"http://x.example/app"`} {
		if !strings.Contains(got, bad) {
			t.Errorf("no warning for %s in %q", bad, got)
		}
	}
	if strings.Contains(got, `"https://ok.example`+"/"+`"`) {
		t.Errorf("valid origin flagged: %q", got)
	}
	// Only invalid entries: no access, and the notices say so.
	if allowed, notices = parseCORSOrigins("nope"); allowed != nil || !strings.Contains(strings.Join(notices, "\n"), "no cross-origin") {
		t.Fatalf("invalid only: allowed = %q, notices = %q", allowed, notices)
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

// A setting is matched the way a browser writes Origin: lower case, without
// the scheme's default port; anything with a path, query, fragment, wildcard
// or invalid port is not an origin.
func TestWebOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://Files.Example:443":   "https://files.example",
		"http://files.example:80/":    "http://files.example",
		"https://files.example:3443":  "https://files.example:3443",
		"http://[::1]:3000":           "http://[::1]:3000",
		"https://files.example//":     "",
		"https://files.example/app":   "",
		"https://files.example?":      "",
		"https://files.example#x":     "",
		"https://:443":                "",
		"https://files.example:0":     "",
		"https://files.example:70000": "",
		"https://user@files.example":  "",
		"ftp://files.example":         "",
		"files.example:3000":          "",
		"https://*.example":           "",
	} {
		if got := webOrigin(in); got != want {
			t.Errorf("webOrigin(%q) = %q, want %q", in, got, want)
		}
	}
	// A default port in the setting still matches the browser's Origin.
	rec := corsRequest(t, "https://files.example:443", http.MethodGet, "https://files.example")
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://files.example" {
		t.Fatalf("default port: Access-Control-Allow-Origin = %q", got)
	}
}
