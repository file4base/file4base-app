package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// While no fixture passes, these keep the conformance comparator honest: what
// it tolerates is exactly the closed list of placeholders, and nothing else.
func TestDataAPIConformance_ComparatorIsExact(t *testing.T) {
	same := func(want, got string) bool { return compareJSONTokens([]byte(want), []byte(got)) == "" }

	assert.True(t, same(`{"a":1,"b":[true,null,"x"]}`, `{ "a" : 1, "b" : [ true, null, "x" ] }`), "whitespace is not significant")
	assert.True(t, same(`{"s":"é"}`, `{"s":"é"}`), "strings are compared unescaped")
	assert.False(t, same(`{"a":1,"b":2}`, `{"b":2,"a":1}`), "key order counts")
	assert.False(t, same(`{"n":-3.0}`, `{"n":-3}`), "number spelling counts")
	assert.False(t, same(`{"n":"1"}`, `{"n":1}`), "value types count")
	assert.False(t, same(`{"a":1}`, `{"a":1,"a":1}`), "duplicate keys count")
	assert.False(t, same(`{"a":[]}`, `{"a":{}}`), "container kinds count")
	assert.False(t, same(`{"a":1}`, `{"a":1}{}`), "trailing values count")

	assert.True(t, same(`{"token":"<token>"}`, `{"token":"3f2a9c"}`))
	assert.False(t, same(`{"token":"<token>"}`, `{"token":""}`), "a token is never empty")
	now := time.Now()
	assert.True(t, same(`{"t":"<now>"}`, `{"t":"`+now.Format("01/02/2006 15:04:05")+`"}`))
	assert.True(t, same(`{"t":"<now>"}`, `{"t":"`+now.Format("2006-01-02T15:04:05.000")+`"}`))
	assert.False(t, same(`{"t":"<now>"}`, `{"t":"`+now.AddDate(0, 0, -3).Format("01/02/2006 15:04:05")+`"}`), "a stale timestamp is not now")
	assert.False(t, same(`{"t":"<now>"}`, `{"t":"yesterday"}`))
	assert.True(t, daMatch("sid=<cookie>; Expires=<date>; Path=/", "sid=ab12; Expires=Thu, 26 Nov 2026 13:04:11 GMT; Path=/"))
	assert.False(t, daMatch("sid=<cookie>; Expires=<date>; Path=/", "sid=ab12; Expires=; Path=/"), "an expiry is an HTTP date")
	assert.False(t, daMatch("sid=<cookie>; Path=/", "sid=; Path=/"), "a cookie value is never empty")
	url := `"https://<host>/Streaming_SSL/MainDB/<container-token>.png?RCType=EmbeddedRCFileProcessor"`
	assert.True(t, same(`{"u":`+url+`}`, `{"u":"https://example.test:8443/Streaming_SSL/MainDB/AB12CD.png?RCType=EmbeddedRCFileProcessor"}`))
	assert.False(t, same(`{"u":`+url+`}`, `{"u":"https://example.test/Streaming_SSL/MainDB/AB12CD.jpg?RCType=EmbeddedRCFileProcessor"}`),
		"only the host and the path token may differ")
	assert.False(t, same(`{"m":"OK"}`, `{"m":"<token>"}`), "placeholders are only honoured on the recorded side")
}

// A fixture replayed against a handler that answers exactly what FileMaker
// answered passes, with its captures and header subset applied.
func TestDataAPIConformance_ReplayPassesOnIdenticalAnswers(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/fmi/data/vLatest/databases/F4B_DataAPI_Reference/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != dataAPIBasic("api_rw") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-FM-Data-Access-Token", "tok123")
		_, _ = w.Write([]byte(`{"response":{"token":"tok123"},"messages":[{"code":"0","message":"OK"}]}`))
	})
	r.Get("/fmi/data/vLatest/validateSession", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Header.Get("Authorization") != "Bearer tok123" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"messages":[{"code":"952","message":"Invalid FileMaker Data API token (*)"}],"response":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"response":{},"messages":[{"code":"0","message":"OK"}]}`))
	})

	var f daFixture
	require.NoError(t, json.Unmarshal([]byte(`{"fixture":"self/replay@vLatest","steps":[
	 {"id":"login","request":{"method":"POST","path":"/fmi/data/vLatest/databases/F4B_DataAPI_Reference/sessions",
	   "headers":[["Content-Type","application/json"],["Authorization","{{basic:api_rw}}"]],"body":"{}"},
	  "capture":[{"var":"rw","from":"token"}],
	  "response":{"status":200,"headers":[["Content-Type","application/json; charset=utf-8"],["X-FM-Data-Access-Token","<token>"]],
	   "body":{"response":{"token":"<token>"},"messages":[{"code":"0","message":"OK"}]}}},
	 {"id":"validate","request":{"method":"GET","path":"/fmi/data/vLatest/validateSession","headers":[["Authorization","Bearer {{token:rw}}"]]},
	  "response":{"status":200,"headers":[["Content-Type","application/json; charset=utf-8"]],
	   "body":{"response":{},"messages":[{"code":"0","message":"OK"}]}}}]}`), &f))

	h := &apiHarness{t: t, router: r}
	diffs, sent := replayDataAPIFixture(h, f)
	assert.Empty(t, diffs)
	assert.Equal(t, 2, sent)

	// A login whose header and body tokens differ does not pass.
	r.Post("/fmi/data/vLatest/databases/Other/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-FM-Data-Access-Token", "tok123")
		_, _ = w.Write([]byte(`{"response":{"token":"tok456"},"messages":[{"code":"0","message":"OK"}]}`))
	})
	mixed := f
	mixed.Steps = []daStep{f.Steps[0]}
	req := *f.Steps[0].Request
	req.Path = "/fmi/data/vLatest/databases/Other/sessions"
	mixed.Steps[0].Request = &req
	resp := *f.Steps[0].Response
	mixed.Steps[0].Response = &resp
	diffs, _ = replayDataAPIFixture(h, mixed)
	assert.Equal(t, []string{"self/replay@vLatest step login: the token header and the token in the body differ"}, diffs)

	// The same fixture fails as soon as one recorded detail differs.
	f.Steps[1].Response.Headers = append(f.Steps[1].Response.Headers, [2]string{"Cache-Control", "no-cache"})
	diffs, _ = replayDataAPIFixture(h, f)
	assert.Len(t, diffs, 1)
}
