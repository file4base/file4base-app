package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
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
	assert.True(t, same(`{"a":{},"b":[{"c":"<token>"},{}],"d":{"e":[]}}`, `{"a":{},"b":[{"c":"x1"},{}],"d":{"e":[]}}`), "nesting is followed")

	assert.True(t, same(`{"token":"<token>"}`, `{"token":"3f2a9c"}`))
	assert.False(t, same(`{"token":"<token>"}`, `{"token":""}`), "a token is never empty")
	assert.False(t, same(`{"token":"<token>"}`, `{"token":"<token>"}`), "a placeholder does not match itself")
	assert.False(t, same(`{"token":"<token>"}`, `{"token":"a b"}`), "a token is a b64token")
	assert.False(t, same(`{"<token>":1}`, `{"OtherField":1}`), "placeholders never apply to keys")
	assert.False(t, same(`{"a":{"<token>":1}}`, `{"a":{"x":1}}`), "nor to nested keys")
	assert.True(t, same(`{"a":[{"k":"<token>"}],"<token>":"v"}`, `{"a":[{"k":"x"}],"<token>":"v"}`), "a key is compared literally")

	now := time.Now()
	assert.True(t, same(`{"t":"<now>"}`, `{"t":"`+now.Format("01/02/2006 15:04:05")+`"}`))
	assert.True(t, same(`{"t":"<now>"}`, `{"t":"`+now.Format("2006-01-02T15:04:05.000")+`"}`))
	assert.True(t, daMatch("<now> to <now>", now.Format("01/02/2006 15:04:05")+" to "+now.Format("2006-01-02 15:04:05")), "several <now> are each checked")
	assert.False(t, daMatch("<now> to <now>", now.Format("01/02/2006 15:04:05")+" to "+now.AddDate(0, 0, -3).Format("2006-01-02 15:04:05")))
	assert.False(t, same(`{"t":"<now>"}`, `{"t":"`+now.AddDate(0, 0, -3).Format("01/02/2006 15:04:05")+`"}`), "a stale timestamp is not now")
	assert.False(t, same(`{"t":"<now>"}`, `{"t":"yesterday"}`))

	assert.True(t, daMatch("sid=<cookie>; Expires=<date>; Path=/", "sid=ab12; Expires=Thu, 26 Nov 2026 13:04:11 GMT; Path=/"))
	assert.False(t, daMatch("sid=<cookie>; Expires=<date>; Path=/", "sid=ab12; Expires=; Path=/"), "an expiry is an HTTP date")
	assert.False(t, daMatch("sid=<cookie>; Expires=<date>; Path=/", "sid=ab12; Expires=Fri, 26 Nov 2026 13:04:11 GMT; Path=/"), "the weekday must be right")
	assert.False(t, daMatch("sid=<cookie>; Expires=<date>; Path=/", "sid=ab12; Expires=Thu, 99 Nov 2026 88:77:66 GMT; Path=/"), "the date must exist")
	assert.False(t, daMatch("sid=<cookie>; Path=/", "sid=; Path=/"), "a cookie value is never empty")
	assert.False(t, daMatch("sid=<cookie>; Path=/", "sid=a,b; Path=/"), "a cookie value has no comma")
	assert.False(t, daMatch("sid=<cookie>; Path=/", "sid=<cookie>; Path=/"))

	url := `"https://<host>/Streaming_SSL/MainDB/<container-token>.png?RCType=EmbeddedRCFileProcessor"`
	assert.True(t, same(`{"u":`+url+`}`, `{"u":"https://`+dataAPIReplayHost+`/Streaming_SSL/MainDB/AB12CD.png?RCType=EmbeddedRCFileProcessor"}`))
	assert.False(t, same(`{"u":`+url+`}`, `{"u":"https://other.example/Streaming_SSL/MainDB/AB12CD.png?RCType=EmbeddedRCFileProcessor"}`),
		"<host> is the replay host")
	assert.False(t, same(`{"u":`+url+`}`, `{"u":"https://user@`+dataAPIReplayHost+`/Streaming_SSL/MainDB/AB12CD.png?RCType=EmbeddedRCFileProcessor"}`))
	assert.False(t, same(`{"u":`+url+`}`, `{"u":"https://`+dataAPIReplayHost+`/Streaming_SSL/MainDB/AB12CD.jpg?RCType=EmbeddedRCFileProcessor"}`),
		"only the host and the path token may differ")
	assert.False(t, same(`{"m":"OK"}`, `{"m":"<token>"}`), "placeholders are only honoured on the recorded side")
}

const selfLoginFixture = `{"fixture":"self/replay@vLatest","steps":[
 {"id":"login","request":{"method":"POST","path":"/fmi/data/vLatest/databases/F4B_DataAPI_Reference/sessions",
   "headers":[["Content-Type","application/json"],["Authorization","{{basic:api_rw}}"]],"body":"{}"},
  "capture":[{"var":"rw","from":"token"}],
  "response":{"status":200,"headers":[["Content-Type","application/json; charset=utf-8"],["X-FM-Data-Access-Token","<token>"]],
   "body":{"response":{"token":"<token>"},"messages":[{"code":"0","message":"OK"}]}}},
 {"id":"validate","request":{"method":"GET","path":"/fmi/data/vLatest/validateSession","headers":[["Authorization","Bearer {{token:rw}}"]]},
  "response":{"status":200,"headers":[["Content-Type","application/json; charset=utf-8"]],
   "body":{"response":{},"messages":[{"code":"0","message":"OK"}]}}}]}`

func selfFixture(t *testing.T, raw string) daFixture {
	t.Helper()
	var f daFixture
	require.NoError(t, json.Unmarshal([]byte(raw), &f))
	return f
}

func diffKinds(diffs []daDiff) []string {
	var out []string
	for _, d := range diffs {
		out = append(out, d.stepID+" "+d.kind)
	}
	return out
}

// A fixture replayed against a handler that answers exactly what FileMaker
// answered passes, with its captures and header subset applied.
func TestDataAPIConformance_ReplayPassesOnIdenticalAnswers(t *testing.T) {
	bodyToken := "tok123"
	setHeader := func(w http.ResponseWriter) { w.Header().Set("X-FM-Data-Access-Token", "tok123") }
	contentLength := ""
	r := chi.NewRouter()
	r.Post("/fmi/data/vLatest/databases/F4B_DataAPI_Reference/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != dataAPIBasic("api_rw") || r.Host != dataAPIReplayHost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		setHeader(w)
		if contentLength != "" {
			w.Header().Set("Content-Length", contentLength)
		}
		_, _ = w.Write([]byte(`{"response":{"token":"` + bodyToken + `"},"messages":[{"code":"0","message":"OK"}]}`))
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
	h := &apiHarness{t: t, router: r}

	f := selfFixture(t, selfLoginFixture)
	diffs, sent := replayDataAPIFixture(h, f, nil)
	assert.Empty(t, diffs)
	assert.Equal(t, 2, sent)

	// A login whose header and body tokens differ does not pass.
	bodyToken = "tok456"
	diffs, _ = replayDataAPIFixture(h, f, nil)
	assert.Equal(t, []string{"login token"}, diffKinds(diffs))

	// Nor does one whose header name is set directly in lower case, so that
	// a lookup by its canonical name would miss it.
	setHeader = func(w http.ResponseWriter) { w.Header()["x-fm-data-access-token"] = []string{"tok123"} }
	diffs, _ = replayDataAPIFixture(h, f, nil)
	assert.Equal(t, []string{"login token"}, diffKinds(diffs))
	bodyToken = "tok123"
	setHeader = func(w http.ResponseWriter) { w.Header().Set("X-FM-Data-Access-Token", "tok123") }

	// A declared length that is not the body's is a difference.
	contentLength = "0"
	diffs, _ = replayDataAPIFixture(h, f, nil)
	assert.Equal(t, []string{"login content-length"}, diffKinds(diffs))
	contentLength = ""

	// An account the migration lost fails every step that uses it.
	diffs, _ = replayDataAPIFixture(h, f, []string{"api_rw"})
	assert.Equal(t, []string{"login stand-in"}, diffKinds(diffs))

	// The same fixture fails as soon as one recorded detail differs.
	f.Steps[1].Response.Headers = append(f.Steps[1].Response.Headers, [2]string{"Cache-Control", "no-cache"})
	diffs, _ = replayDataAPIFixture(h, f, nil)
	assert.Equal(t, []string{"validate header:cache-control"}, diffKinds(diffs))
}

// A URL the server returned is only followed on the replay host.
func TestDataAPIConformance_FollowsOnlyReplayHostURLs(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/Streaming/MainDB/x.png", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("png")) })
	h := &apiHarness{t: t, router: r}
	for raw, ok := range map[string]bool{
		"https://" + dataAPIReplayHost + "/Streaming/MainDB/x.png":      true,
		"https://user@" + dataAPIReplayHost + "/Streaming/MainDB/x.png": false,
		"https://wrong.example/Streaming/MainDB/x.png":                  false,
		"http://" + dataAPIReplayHost + "/Streaming/MainDB/x.png":       false,
		"/Streaming/MainDB/x.png":                                       false,
	} {
		st := &daState{tokens: map[string]string{}, vars: map[string]string{"u": raw}}
		res := sendDataAPIRequest(h, st, daStep{ID: "get", Request: &daRequest{Method: "GET", URL: "{{var:u}}"}})
		assert.Equal(t, ok, res.refused == "", raw)
	}
}

func TestDataAPIConformance_ExpectedFailuresRatchet(t *testing.T) {
	f := selfFixture(t, selfLoginFixture)
	entry := func(line string) *daEntry {
		x, problems := parseDataAPIExpected([]byte(line))
		require.Empty(t, problems)
		return x.lookup("self/replay@vLatest", "postgres")
	}
	diff := func(step, kind string) daDiff { return daDiff{stepID: step, kind: kind, msg: step + " " + kind} }
	joined := func(p []string, _ bool) string { return strings.Join(p, "\n") }

	_, ok := judgeDataAPIFixture(f, entry("self/replay@vLatest validate body"), "postgres", []daDiff{diff("validate", "body")})
	assert.True(t, ok, "failing exactly as listed")
	assert.Contains(t, joined(judgeDataAPIFixture(f, entry("self/replay@vLatest validate body"), "postgres",
		[]daDiff{diff("login", "status"), diff("validate", "body")})), "regressed", "failing earlier")
	assert.Contains(t, joined(judgeDataAPIFixture(f, entry("self/replay@vLatest validate body"), "postgres",
		[]daDiff{diff("validate", "status")})), "regressed", "failing differently, at an earlier check")
	assert.Contains(t, joined(judgeDataAPIFixture(f, entry("self/replay@vLatest login status"), "postgres",
		[]daDiff{diff("validate", "body")})), "replace `self/replay@vLatest login status` with `self/replay@vLatest validate body`", "progress must be recorded")
	assert.Contains(t, joined(judgeDataAPIFixture(f, entry("self/replay@vLatest login status"), "postgres", nil)), "now passes")
	assert.Contains(t, joined(judgeDataAPIFixture(f, nil, "postgres", []daDiff{diff("login", "status")})),
		"would be listed as `self/replay@vLatest login status`")
	problems, ok := judgeDataAPIFixture(f, entry("self/replay@vLatest login status"), "postgres",
		[]daDiff{diff("login", "status"), {stepID: "validate", kind: "harness", harness: true, msg: "broken"}})
	assert.False(t, ok, "a harness error is never expected")
	assert.Contains(t, problems, "harness: broken")

	// An entry for one engine wins over one for every engine.
	x, problems := parseDataAPIExpected([]byte("self/replay@vLatest login status\nself/replay@vLatest validate body mariadb\nmigration@mariadb #146\n"))
	require.Empty(t, problems)
	assert.Equal(t, "status", x.lookup("self/replay@vLatest", "postgres").kind)
	assert.Equal(t, "body", x.lookup("self/replay@vLatest", "mariadb").kind)
	_, problems = parseDataAPIExpected([]byte("migration@mariadb #999\nmigration@sqlite #146\nx y nonsense\nx y\n"))
	assert.Len(t, problems, 4)
}

func TestDataAPIConformance_ListOnlyShrinks(t *testing.T) {
	retitled := strings.Replace(selfLoginFixture, `"fixture":"self/replay@vLatest",`, `"fixture":"self/replay@vLatest","title":"new title","rows":["V9"],`, 1)
	rerecorded := strings.Replace(selfLoginFixture, `"message":"OK"}]}}}]}`, `"message":"OK!"}]}}}]}`, 1)
	require.NotEqual(t, selfLoginFixture, rerecorded)
	tree := func(list string, fixture ...string) daTree {
		tr := daTree{expected: []byte(list), fixtures: map[string][]byte{}}
		for _, f := range fixture {
			tr.fixtures["self/replay@vLatest"] = []byte(f)
		}
		return tr
	}
	none := tree("")
	none.expected = nil
	violates := func(base, head daTree) bool { return len(dataAPIRatchetViolations(base, head)) > 0 }

	assert.False(t, violates(none, tree("self/replay@vLatest login status", selfLoginFixture)), "the suite is introduced")
	assert.False(t, violates(tree("self/replay@vLatest login status", selfLoginFixture), tree("", selfLoginFixture)), "an entry is removed")
	assert.False(t, violates(tree("self/replay@vLatest login status", selfLoginFixture), tree("self/replay@vLatest validate body", selfLoginFixture)),
		"it now fails later")
	assert.True(t, violates(tree("self/replay@vLatest validate body", selfLoginFixture), tree("self/replay@vLatest login status", selfLoginFixture)),
		"it now fails earlier")
	assert.True(t, violates(tree("", selfLoginFixture), tree("self/replay@vLatest login status", selfLoginFixture)), "a passing fixture is listed")
	assert.True(t, violates(tree("", selfLoginFixture), tree("self/replay@vLatest login status", retitled)),
		"a new title or audit rows are not a new recording")
	assert.False(t, violates(tree("", selfLoginFixture), tree("self/replay@vLatest login status", rerecorded)), "a re-recorded fixture may be listed")
	assert.False(t, violates(tree(""), tree("self/replay@vLatest login status", selfLoginFixture)), "a new fixture may be listed")
	assert.True(t, violates(tree("", selfLoginFixture), tree("migration@postgres #146", selfLoginFixture)), "a migration entry may not be added")
	assert.True(t, violates(tree("self/replay@vLatest login status", selfLoginFixture), tree("")), "a fixture may not be deleted")
}
