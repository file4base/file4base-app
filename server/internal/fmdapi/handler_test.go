package fmdapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// fakeDatabases knows databases by name and has no driver: the tests stop
// before any check that needs one.
type fakeDatabases map[string]bool

func (f fakeDatabases) LookupDatabase(_ context.Context, name string) (string, bool, error) {
	return name, f[name], nil
}

func (fakeDatabases) DriverFor(context.Context, string) (dbal.DatabaseDriver, error) {
	return nil, errors.New("no driver in this test")
}

func testHandler(cfg Config) (*Handler, *auth.Store) {
	if cfg.Databases == nil {
		cfg.Databases = []DatabaseAlias{{"Sales", "sales"}, {"Archive", "archive"}, {"Gone", "gone"}}
	}
	sessions := auth.NewStore(time.Hour)
	return New(cfg, Deps{
		Databases: fakeDatabases{"sales": true, "archive": true},
		Sessions:  sessions,
	}), sessions
}

type call struct {
	method, target string
	headers        map[string]string
	body           string
}

func serve(h http.Handler, c call) *httptest.ResponseRecorder {
	var body io.Reader
	if c.body != "" {
		body = strings.NewReader(c.body)
	}
	r := httptest.NewRequest(c.method, c.target, body)
	for k, v := range c.headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const (
	okTail   = `"messages":[{"code":"0","message":"OK"}]}`
	jsonType = "application/json"
)

func TestPipeline(t *testing.T) {
	h, _ := testHandler(Config{})
	bearer := map[string]string{"Authorization": "Bearer nosuchtoken"}
	find := "/fmi/data/v1/databases/Sales/layouts/L/_find"
	for _, c := range []struct {
		name string
		call
		status int
		body   string
	}{
		{"productInfo", call{"GET", "/fmi/data/vLatest/productInfo", nil, ""}, 200,
			`{"response":{"productInfo":{"name":"File4Base Data API","dateFormat":"MM/dd/yyyy","timeFormat":"HH:mm:ss","timeStampFormat":"MM/dd/yyyy HH:mm:ss"}},` + okTail},
		{"merged slashes", call{"GET", "/fmi/data//v2//productInfo/", nil, ""}, 200, ""},
		{"literal in any case", call{"GET", "/fmi/data/v1/PRODUCTINFO", nil, ""}, 200, ""},
		{"version", call{"GET", "/fmi/data/v3/productInfo", nil, ""}, 400, `{"response":{}}`},
		{"version case", call{"GET", "/fmi/data/vlatest/productInfo", nil, ""}, 400, `{"response":{}}`},
		{"no route", call{"GET", "/fmi/data/v1/nosuchroute?a=<b>", nil, ""}, 404,
			"<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>Error</title>\n</head>\n<body>\n<pre>Cannot GET /fmi/data/v1/nosuchroute?a=%3Cb%3E</pre>\n</body>\n</html>\n"},
		{"no version", call{"GET", "/fmi/data/", nil, ""}, 404, ""},
		{"method", call{"POST", "/fmi/data/v1/productInfo", nil, ""}, 405,
			`{"messages":[{"message":"Resource doesn't support the specified HTTP verb","code":"1704"}],"response":{}}`},
		{"databases", call{"GET", "/fmi/data/v1/databases", map[string]string{"Authorization": "Basic eDp5"}, ""}, 200,
			`{"response":{"databases":[{"name":"Sales"},{"name":"Archive"}]},` + okTail},
		{"databases bearer", call{"GET", "/fmi/data/v1/databases", bearer, ""}, 401,
			`{"messages":[{"message":"HTTP Authorization header invalid","code":"960"}],"response":{}}`},
		{"no authorization", call{"GET", "/fmi/data/v1/validateSession", nil, ""}, 400,
			`{"messages":[{"message":"Request validation failed: Parameter (Authorization) is required","code":"10"}],"response":{}}`},
		{"basic", call{"GET", "/fmi/data/v1/validateSession", map[string]string{"Authorization": "Basic eDp5"}, ""}, 401,
			`{"messages":[{"message":"Authorization header 'Basic' invalid.","code":"212"}],"response":{}}`},
		{"unknown token", call{"GET", "/fmi/data/v1/validateSession", bearer, ""}, 401,
			`{"messages":[{"code":"952","message":"Invalid FileMaker Data API token (*)"}],"response":{}}`},
		{"not served", call{"GET", "/fmi/data/v1/databases/Sales/layouts", bearer, ""}, 401, ""},
		{"content type", call{"POST", find, map[string]string{"Content-Type": "text/plain"}, `{}`}, 400,
			`{"messages":[{"message":"Invalid content type (text/plain).  These are valid: application/json","code":"1708"}],"response":{}}`},
		{"no content type", call{"POST", find, nil, ``}, 400,
			`{"messages":[{"message":"Invalid content type (application/octet-stream).  These are valid: application/json","code":"1708"}],"response":{}}`},
		{"malformed body", call{"POST", find, map[string]string{"Content-Type": jsonType}, `{`}, 200,
			`{"messages":[{"message":"Expected property name or '}' in JSON at position 1 (line 1 column 2)","code":"1630"}],"response":{}}`},
		{"body before route", call{"GET", "/fmi/data/v1/nosuchroute", map[string]string{"Content-Type": jsonType}, `x`}, 200, ""},
		{"authorization before body", call{"POST", find, map[string]string{"Content-Type": jsonType}, `{"lmit":1}`}, 400, ""},
		{"body before token", call{"POST", find, map[string]string{"Content-Type": jsonType, "Authorization": "Bearer x"}, `{"query":[],"lmit":1}`}, 400,
			`{"messages":[{"message":"Unknown parameter(s): lmit","code":"960"}],"response":{}}`},
		{"query before token", call{"GET", "/fmi/data/v1/databases/Sales/layouts/L/records?limit=1", bearer, ""}, 400, ""},
		{"undecodable param", call{"GET", "/fmi/data/v1/databases/%E9/layouts", bearer, ""}, 200,
			`{"messages":[{"message":"Failed to decode param '%E9'","code":"1630"}],"response":{}}`},
		{"utf-16 body", call{"POST", find, map[string]string{"Content-Type": "application/json; charset=utf-16le", "Authorization": "Bearer x"}, "{\x00\"\x00l\x00\"\x00:\x001\x00}\x00"}, 400,
			`{"messages":[{"message":"Missing required property: query","code":"10"}],"response":{}}`},
		{"unknown utf charset", call{"POST", find, map[string]string{"Content-Type": "application/json; charset=utf-7"}, `{}`}, 200,
			`{"messages":[{"message":"specified encoding unsupported","code":"1630"}],"response":{}}`},
		{"other charset", call{"POST", find, map[string]string{"Content-Type": "application/json; charset=latin1"}, `{}`}, 200,
			`{"messages":[{"message":"unsupported charset \"LATIN1\"","code":"1630"}],"response":{}}`},
		{"too many values", call{"POST", find, map[string]string{"Content-Type": jsonType}, "[" + strings.Repeat("0,", maxJSONValues) + "0]"}, 200,
			`{"messages":[{"message":"request entity too large","code":"1630"}],"response":{}}`},
		{"too deep", call{"POST", find, map[string]string{"Content-Type": jsonType}, strings.Repeat("[", maxJSONValues+1)}, 200,
			`{"messages":[{"message":"request entity too large","code":"1630"}],"response":{}}`},
		{"logout unknown", call{"DELETE", "/fmi/data/v1/databases/Sales/sessions/nosuchtoken", nil, ""}, 401, ""},
	} {
		w := serve(h, c.call)
		if w.Code != c.status || (c.body != "" && w.Body.String() != c.body) {
			t.Errorf("%s: %d %s, want %d %s", c.name, w.Code, w.Body.String(), c.status, c.body)
		}
		if got := w.Header().Get("Content-Length"); got != itoa(w.Body.Len()) {
			t.Errorf("%s: Content-Length %s for %d bytes", c.name, got, w.Body.Len())
		}
	}
}

func TestProductInfoConfigured(t *testing.T) {
	h, _ := testHandler(Config{ProductName: "P", ProductVersion: "1.2", ProductBuildDate: "01/02/2026"})
	w := serve(h, call{"GET", "/fmi/data/v1/productInfo", nil, ""})
	want := `{"response":{"productInfo":{"name":"P","buildDate":"01/02/2026","version":"1.2","dateFormat":"MM/dd/yyyy","timeFormat":"HH:mm:ss","timeStampFormat":"MM/dd/yyyy HH:mm:ss"}},` + okTail
	if w.Body.String() != want || w.Header().Get("Cache-Control") != "" {
		t.Fatalf("%s (Cache-Control %q)", w.Body.String(), w.Header().Get("Cache-Control"))
	}
}

func TestHeadAnswersWithoutBody(t *testing.T) {
	h, _ := testHandler(Config{})
	get := serve(h, call{"GET", "/fmi/data/v1/productInfo", nil, ""})
	head := serve(h, call{"HEAD", "/fmi/data/v1/productInfo", nil, ""})
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != itoa(get.Body.Len()) {
		t.Fatalf("HEAD: %d, %d bytes, Content-Length %s", head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
	}
}

func TestLogout(t *testing.T) {
	h, sessions := testHandler(Config{})
	token, _, err := sessions.CreateDataAPI("u1", "user", "user", "sales", "sAles", "stamp")
	if err != nil {
		t.Fatal(err)
	}
	// The database must be named as at sign-in, case included.
	if w := serve(h, call{"DELETE", "/fmi/data/v1/databases/Sales/sessions/" + token, nil, ""}); w.Code != 401 {
		t.Fatalf("logout under another case: %d %s", w.Code, w.Body.String())
	}
	w := serve(h, call{"DELETE", "/fmi/data/v1/databases/sAles/sessions/" + token, nil, ""})
	if w.Code != 200 || w.Body.String() != `{"response":{},`+okTail || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	if _, ok := sessions.DataAPI(token, false); ok {
		t.Fatal("the session outlived its logout")
	}
	if w := serve(h, call{"DELETE", "/fmi/data/v1/databases/sAles/sessions/" + token, nil, ""}); w.Code != 401 {
		t.Fatalf("second logout: %d", w.Code)
	}
}

func TestLoginRefusals(t *testing.T) {
	h, _ := testHandler(Config{})
	login := "/fmi/data/v1/databases/Sales/sessions"
	for _, c := range []struct {
		name string
		call
		status int
		body   string
	}{
		{"no authorization", call{"POST", login, map[string]string{"Content-Type": jsonType}, `{}`}, 401,
			`{"messages":[{"message":"HTTP Authorization header or OAuth headers are missing.","code":"10"}],"response":{}}`},
		{"basic without colon", call{"POST", login, map[string]string{"Content-Type": jsonType, "Authorization": "Basic eHl6"}, `{}`}, 401,
			`{"messages":[{"message":"Authorization header 'Basic' invalid.","code":"212"}],"response":{}}`},
		{"unknown body key", call{"POST", login, map[string]string{"Content-Type": jsonType, "Authorization": "Basic eDp5"}, `{"foo":"bar"}`}, 400,
			`{"messages":[{"message":"Unknown parameter(s): foo","code":"960"}],"response":{}}`},
		{"fmDataSource not array", call{"POST", login, map[string]string{"Content-Type": jsonType}, `{"fmDataSource":{"database":"x"}}`}, 400,
			`{"messages":[{"message":"'fmDataSource': Expected type array but found type object","code":"960"}],"response":{}}`},
		{"unknown database", call{"POST", "/fmi/data/v1/databases/Nope/sessions", map[string]string{"Content-Type": jsonType, "Authorization": "Basic eDp5"}, `{}`}, 500,
			`{"messages":[{"code":"802","message":"Unable to open file"}],"response":{}}`},
		{"listed but missing", call{"POST", "/fmi/data/v1/databases/Gone/sessions", map[string]string{"Content-Type": jsonType, "Authorization": "Basic eDp5"}, `{}`}, 500, ""},
	} {
		w := serve(h, c.call)
		if w.Code != c.status || (c.body != "" && w.Body.String() != c.body) {
			t.Errorf("%s: %d %s, want %d %s", c.name, w.Code, w.Body.String(), c.status, c.body)
		}
		if w.Header().Get("X-FM-Data-Access-Token") != "" {
			t.Errorf("%s: token header on a refusal", c.name)
		}
	}
}
