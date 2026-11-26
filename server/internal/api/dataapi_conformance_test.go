package api_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
)

// The FileMaker Data API conformance suite of #208 (phase P0).
//
// testdata/dataapi holds a synthetic FileMaker file, its Save-as-XML design
// and the request/response pairs FileMaker Server returned for it, one
// fixture per scenario and API version. This test migrates the same file
// through the documented migration path, gives the migrated accounts test
// passwords, replays every fixture against /fmi/data/... and compares the
// status, the recorded header subset and the body exactly: key order, value
// types and number spelling included.
//
// The only differences tolerated are the closed list of placeholders the
// recorder writes into the fixtures (see testdata/dataapi/README.md):
// session tokens and cookies, the container URL host and path token, and
// timestamps that depend on the current time. Anything else is a mismatch.
//
// testdata/dataapi/expected_failures.txt lists the fixtures that do not pass
// yet. It can only shrink: a listed fixture that passes fails the test, so
// whoever makes it pass has to remove it from the list.

const (
	dataAPIDir      = "../../testdata/dataapi"
	dataAPIDatabase = "F4B_DataAPI_Reference"
	dataAPIOwner    = "conformance_owner"
)

// The accounts of the reference file, by the names the fixtures use. Their
// passwords are set by this test; the recorded fixtures never contain any.
var dataAPIAccounts = []string{"admin", "api_rw", "api_ro", "noapi"}

func dataAPIPassword(account string) string { return "conformance-" + account + "-9" }

// The response headers the recorder keeps, by lower-case name; every x-fm-*
// header is kept as well.
var dataAPIRecordedHeaders = map[string]bool{
	"content-type": true, "cache-control": true, "set-cookie": true,
	"content-disposition": true, "location": true, "www-authenticate": true,
}

type daFixture struct {
	Fixture  string   `json:"fixture"`
	Scenario string   `json:"scenario"`
	Group    string   `json:"group"`
	Version  string   `json:"version"`
	Fresh    bool     `json:"fresh"`
	Title    string   `json:"title"`
	Rows     []string `json:"rows"`
	Steps    []daStep `json:"steps"`

	text string // the fixture file, to see which accounts it uses
}

type daStep struct {
	ID          string        `json:"id"`
	Rows        []string      `json:"rows"`
	Note        string        `json:"note"`
	IdleSeconds *float64      `json:"idle_seconds"`
	Request     *daRequest    `json:"request"`
	Capture     []daCapture   `json:"capture"`
	Response    *daResponse   `json:"response"`
	Concurrent  *daConcurrent `json:"concurrent"`
}

type daRequest struct {
	Method    string      `json:"method"`
	Path      string      `json:"path"`
	URL       string      `json:"url"`
	Headers   [][2]string `json:"headers"`
	Body      *string     `json:"body"`
	Multipart []daPart    `json:"multipart"`
	Cookies   bool        `json:"cookies"`
}

type daPart struct {
	Name        string  `json:"name"`
	Filename    *string `json:"filename"`
	ContentType string  `json:"content_type"`
	Text        string  `json:"text"`
	Base64      *string `json:"base64"`
	Repeat      *struct {
		Text  string `json:"text"`
		Count int    `json:"count"`
	} `json:"repeat"`
}

type daCapture struct {
	Var    string        `json:"var"`
	From   string        `json:"from"`
	Path   []interface{} `json:"path"`
	Header string        `json:"header"`
}

type daResponse struct {
	Status     int             `json:"status"`
	Headers    [][2]string     `json:"headers"`
	Body       json.RawMessage `json:"body"`
	BodyText   *string         `json:"body_text"`
	BodyBase64 *string         `json:"body_base64"`
	BodySHA256 string          `json:"body_sha256"`
	BodyLength *int            `json:"body_length"`
}

type daConcurrent struct {
	Background daStep  `json:"background"`
	Probe      daStep  `json:"probe"`
	UntilCode  string  `json:"until_code"`
	StartDelay float64 `json:"start_delay"`
	Interval   float64 `json:"interval"`
	MaxTries   int     `json:"max_tries"`
}

func loadDataAPIFixtures(t *testing.T) []daFixture {
	t.Helper()
	root := filepath.Join(dataAPIDir, "fixtures")
	var out []daFixture
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var f daFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if len(f.Steps) == 0 {
			return fmt.Errorf("%s has no step", path)
		}
		f.text = string(raw)
		rel, _ := filepath.Rel(root, path)
		if want := strings.TrimSuffix(filepath.ToSlash(rel), ".json"); f.Fixture != want {
			return fmt.Errorf("%s declares fixture %q", path, f.Fixture)
		}
		out = append(out, f)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out, "the Data API fixtures must be readable")
	// Fixtures that share one migration first, then those that need a fresh
	// copy each, so that the shared one is migrated once.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Fresh != out[j].Fresh {
			return !out[i].Fresh
		}
		return out[i].Fixture < out[j].Fixture
	})
	return out
}

func loadExpectedFailures(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(filepath.Join(dataAPIDir, "expected_failures.txt"))
	require.NoError(t, err)
	defer f.Close()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		require.False(t, out[line], "expected_failures.txt lists %s twice", line)
		out[line] = true
	}
	require.NoError(t, sc.Err())
	return out
}

func TestDataAPIConformance(t *testing.T) {
	fixtures := loadDataAPIFixtures(t)
	expected := loadExpectedFailures(t)
	known := map[string]bool{"migration@" + string(dbal.EnginePostgres): true, "migration@" + string(dbal.EngineMariaDB): true}
	for _, f := range fixtures {
		known[f.Fixture] = true
	}
	for name := range expected {
		require.True(t, known[name], "expected_failures.txt lists %s, which is not a fixture", name)
	}

	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	if _, exists, err := h.mgr.LookupDatabase(t.Context(), dataAPIDatabase); err == nil && exists {
		t.Fatalf("a database named %s already exists on the test server; this test creates and drops it, so drop it first", dataAPIDatabase)
	}
	t.Cleanup(func() { _ = h.mgr.DropDatabase(context.Background(), dataAPIDatabase) })

	// "migration@<engine>" records that the reference file does not migrate
	// on that engine yet. Every fixture then fails, and must be listed too.
	migrationEntry := "migration@" + string(testdb.Engine())
	var migrationErr error
	var lost []string
	shared := false
	passed, failedAsExpected, requests := 0, 0, 0
	for _, f := range fixtures {
		f := f
		t.Run(f.Fixture, func(t *testing.T) {
			if migrationErr == nil && (f.Fresh || !shared) {
				lost, migrationErr = migrateDataAPIReference(t, h)
				shared = migrationErr == nil && !f.Fresh
			}
			var diffs []string
			switch {
			case migrationErr != nil && !expected[migrationEntry]:
				t.Fatalf("the reference file does not migrate: %v", migrationErr)
			case migrationErr != nil:
				diffs = []string{"the reference file does not migrate on this engine (" + migrationEntry + "): " + migrationErr.Error()}
			default:
				// An account the migration lost is only a stand-in: a fixture
				// that signs in with it cannot pass, whatever it answers.
				for _, account := range lost {
					if dataAPIUsesAccount(f.text, account) {
						diffs = append(diffs, fmt.Sprintf("%s: account %s was not migrated; it was replayed with a stand-in", f.Fixture, account))
					}
				}
				replayed, sent := replayDataAPIFixture(h, f)
				requests += sent
				diffs = append(diffs, replayed...)
			}
			switch {
			case len(diffs) == 0 && expected[f.Fixture]:
				t.Errorf("%s now passes: remove it from testdata/dataapi/expected_failures.txt", f.Fixture)
			case len(diffs) > 0 && !expected[f.Fixture]:
				for _, d := range diffs {
					t.Error(d)
				}
			case len(diffs) > 0:
				failedAsExpected++
				t.Logf("expected failure, %d difference(s); first: %s", len(diffs), diffs[0])
			default:
				passed++
			}
		})
	}
	if migrationErr == nil && expected[migrationEntry] {
		t.Errorf("the reference file now migrates: remove %s from testdata/dataapi/expected_failures.txt", migrationEntry)
	}
	t.Logf("%s: %d fixtures, %d requests replayed; %d pass, %d fail as expected", testdb.Engine(), len(fixtures), requests, passed, failedAsExpected)
}

// dataAPIUsesAccount reports whether a fixture signs in as, or names, an account.
func dataAPIUsesAccount(text, account string) bool {
	for _, p := range []string{"{{basic:" + account + "}}", "{{basic:" + account + ":", "{{user:" + account + "}}", "{{password:" + account + "}}"} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// migrateDataAPIReference replaces the reference database with a fresh
// migration of the reference file, and gives its accounts test passwords. It
// returns the accounts the migration did not create.
func migrateDataAPIReference(t *testing.T, h *apiHarness) ([]string, error) {
	t.Helper()
	ctx := t.Context()
	if _, exists, err := h.mgr.LookupDatabase(ctx, dataAPIDatabase); err == nil && exists {
		require.NoError(t, h.mgr.DropDatabase(ctx, dataAPIDatabase))
	}
	rec := daJSON(h, http.MethodPost, "/api/v1/databases", "", map[string]string{
		"database": dataAPIDatabase, "user": dataAPIOwner, "password": dataAPIPassword(dataAPIOwner),
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var login struct {
		Token string `json:"token"`
	}
	rec = daJSON(h, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"database": dataAPIDatabase, "username": dataAPIOwner, "password": dataAPIPassword(dataAPIOwner),
	}, &login)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	native, err := os.ReadFile(filepath.Join(dataAPIDir, "reference", dataAPIDatabase+".fmp12"))
	require.NoError(t, err)
	gz, err := os.Open(filepath.Join(dataAPIDir, "reference", dataAPIDatabase+".xml.gz"))
	require.NoError(t, err)
	defer gz.Close()
	zr, err := gzip.NewReader(gz)
	require.NoError(t, err)
	design, err := io.ReadAll(zr)
	require.NoError(t, err)

	body, contentType := multipartBody(t, []uploadPart{
		// The reference file is an en-US file, and deliberately holds values
		// a Number, Date or Time field accepts without validating them.
		{name: "request", body: []byte(`{"source_locale":"en-US"}`)},
		{name: "design", filename: dataAPIDatabase + ".xml", body: design},
		{name: "native", filename: dataAPIDatabase + ".fmp12", body: native},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/migrate/apply", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	rec = httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		return nil, fmt.Errorf("migrate/apply answered %d: %s", rec.Code, rec.Body.String())
	}

	var users []schema.UserMetadata
	rec = daJSON(h, http.MethodGet, "/api/v1/security/users", login.Token, nil, &users)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	byName := map[string]schema.UserMetadata{}
	for _, u := range users {
		byName[u.Username] = u
	}
	var missing []string
	for _, account := range dataAPIAccounts {
		u, ok := byName[account]
		if !ok {
			// The account did not survive the migration. A plain user stands
			// in for it, so that the replay still shows the other
			// differences; the fixtures that use it are failed regardless.
			missing = append(missing, account)
			rec = daJSON(h, http.MethodPost, "/api/v1/security/users", login.Token, map[string]interface{}{
				"username": account, "password": dataAPIPassword(account), "role": "user", "is_active": true,
			}, nil)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			continue
		}
		rec = daJSON(h, http.MethodPut, "/api/v1/security/users/"+u.ID, login.Token, map[string]interface{}{
			"role": u.Role, "password": dataAPIPassword(account), "is_active": true,
		}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	if len(missing) > 0 {
		t.Logf("the migration did not create the accounts %v; plain users stand in for them", missing)
	}
	return missing, nil
}

// daJSON is apiHarness.do without the harness's own *testing.T, so that it
// can be called from subtests.
func daJSON(h *apiHarness, method, target, token string, in, out interface{}) *httptest.ResponseRecorder {
	var body io.Reader = http.NoBody
	if in != nil {
		raw, _ := json.Marshal(in)
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, body)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		_ = json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec
}

// ---------------------------------------------------------------- replay

type daState struct {
	mu      sync.Mutex
	tokens  map[string]string
	vars    map[string]string
	cookies []string
}

type daResult struct {
	status  int
	headers http.Header
	body    []byte
	err     error
}

func replayDataAPIFixture(h *apiHarness, f daFixture) ([]string, int) {
	st := &daState{tokens: map[string]string{}, vars: map[string]string{}}
	var diffs []string
	sent := 0
	for _, step := range f.Steps {
		where := f.Fixture + " step " + step.ID
		switch {
		case step.IdleSeconds != nil:
			// Waiting out FileMaker's idle timeout in real time is not an
			// option in CI; this needs a session clock the test can move.
			diffs = append(diffs, fmt.Sprintf("%s: an idle wait of %vs cannot be replayed without a session clock seam", where, *step.IdleSeconds))
		case step.Concurrent != nil:
			d, n := replayConcurrent(h, st, where, step.Concurrent)
			diffs = append(diffs, d...)
			sent += n
		case step.Request != nil && step.Response != nil:
			res := sendDataAPIRequest(h, st, step)
			sent++
			diffs = append(diffs, compareDataAPIResponse(where, step.Response, res)...)
		default:
			diffs = append(diffs, where+": the fixture step has no request")
		}
	}
	return diffs, sent
}

// dataAPIBackgroundLimit bounds a concurrent step's background request.
const dataAPIBackgroundLimit = 2 * time.Minute

// replayConcurrent sends the background request, then repeats the probe
// while the background request is still running, until the probe answers
// until_code. A probe that only gets there after the background request has
// finished does not count.
func replayConcurrent(h *apiHarness, st *daState, where string, c *daConcurrent) ([]string, int) {
	interval := time.Duration(c.Interval * float64(time.Second))
	maxTries := c.MaxTries
	if maxTries <= 0 {
		maxTries = 300
	}
	done := make(chan daResult, 1)
	go func() { done <- sendDataAPIRequest(h, st, c.Background) }()
	time.Sleep(time.Duration(c.StartDelay * float64(time.Second)))

	var probe *daResult
	var background *daResult
	sent := 1
	for try := 0; try < maxTries; try++ {
		select {
		case r := <-done:
			background = &r
		default:
		}
		if background != nil {
			break
		}
		r := sendDataAPIRequest(h, st, c.Probe)
		probe = &r
		sent++
		if dataAPICode(r.body) == c.UntilCode {
			break
		}
		time.Sleep(interval)
	}
	if background == nil {
		select {
		case r := <-done:
			background = &r
		case <-time.After(dataAPIBackgroundLimit):
			return []string{fmt.Sprintf("%s: the background request %s did not finish within %s", where, c.Background.ID, dataAPIBackgroundLimit)}, sent
		}
	}
	diffs := compareDataAPIResponse(where+" (background "+c.Background.ID+")", c.Background.Response, *background)
	if probe == nil {
		return append(diffs, fmt.Sprintf("%s: the background request %s finished before any probe was sent", where, c.Background.ID)), sent
	}
	return append(diffs, compareDataAPIResponse(where+" (probe "+c.Probe.ID+")", c.Probe.Response, *probe)...), sent
}

func dataAPICode(body []byte) string {
	var env struct {
		Messages []struct {
			Code string `json:"code"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Messages) == 0 {
		return ""
	}
	return env.Messages[0].Code
}

var dataAPIPlaceholder = regexp.MustCompile(`\{\{(token|basic|user|password|var):([^}]*)\}\}`)

func (st *daState) expand(s string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return dataAPIPlaceholder.ReplaceAllStringFunc(s, func(m string) string {
		g := dataAPIPlaceholder.FindStringSubmatch(m)
		kind, arg := g[1], g[2]
		switch kind {
		case "token":
			if v, ok := st.tokens[arg]; ok {
				return v
			}
			return "no-such-token-" + arg
		case "basic":
			return dataAPIBasic(arg)
		case "user":
			return arg
		case "password":
			return dataAPIPassword(arg)
		default:
			if v, ok := st.vars[arg]; ok {
				return v
			}
			return "missing-var-" + arg
		}
	})
}

// dataAPIBasic mirrors the recorder: ACC, ACC:wrong, ACC:empty, ACC:upper or
// raw:USER:PASS.
func dataAPIBasic(spec string) string {
	parts := strings.Split(spec, ":")
	user, pass := parts[0], dataAPIPassword(parts[0])
	switch {
	case parts[0] == "raw" && len(parts) >= 2:
		user, pass = parts[1], strings.Join(parts[2:], ":")
	case len(parts) == 2 && parts[1] == "wrong":
		pass = "definitely-not-the-password"
	case len(parts) == 2 && parts[1] == "upper":
		user = strings.ToUpper(user)
	case len(parts) == 2 && parts[1] == "empty":
		pass = ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// newDataAPIRequest builds the recorded request. A target the standard
// library refuses is reported as a difference, not as a crash of the suite.
func newDataAPIRequest(method, target string, body io.Reader) (req *http.Request, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the recorded request cannot be built: %v", r)
		}
	}()
	return httptest.NewRequest(method, target, body), nil
}

func sendDataAPIRequest(h *apiHarness, st *daState, step daStep) daResult {
	rq := step.Request
	target := st.expand(rq.Path)
	if rq.URL != "" {
		u, err := url.Parse(st.expand(rq.URL))
		if err != nil {
			return daResult{err: err}
		}
		target = u.RequestURI()
	}

	var body io.Reader
	contentType := ""
	switch {
	case len(rq.Multipart) > 0:
		pr, pw := io.Pipe()
		// Whatever happens to the request, the writer must not stay blocked.
		defer pr.CloseWithError(errors.New("request finished"))
		mw := multipart.NewWriter(pw)
		contentType = mw.FormDataContentType()
		go func() { pw.CloseWithError(writeDataAPIParts(mw, rq.Multipart)) }()
		body = pr
	case rq.Body != nil:
		body = strings.NewReader(st.expand(*rq.Body))
	}
	req, err := newDataAPIRequest(rq.Method, target, body)
	if err != nil {
		return daResult{err: err}
	}
	if body == nil {
		req.Body = http.NoBody
	}
	// FileMaker Server answers over HTTPS only; so does the replay.
	req.TLS = &tls.ConnectionState{}
	for _, kv := range rq.Headers {
		req.Header.Add(kv[0], st.expand(kv[1]))
	}
	if contentType != "" {
		req.Header.Add("Content-Type", contentType)
	}
	st.mu.Lock()
	if rq.Cookies && len(st.cookies) > 0 {
		// As the recorder did: every cookie set so far in the scenario.
		req.Header.Add("Cookie", strings.Join(st.cookies, "; "))
	}
	st.mu.Unlock()

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	// Result() holds the headers as they were when the response started,
	// which is what a client receives.
	resp := rec.Result()
	res := daResult{status: resp.StatusCode, headers: resp.Header, body: rec.Body.Bytes()}
	st.remember(step.Capture, res)
	return res
}

func writeDataAPIParts(mw *multipart.Writer, parts []daPart) error {
	for _, p := range parts {
		hdr := textproto.MIMEHeader{}
		disp := fmt.Sprintf(`form-data; name="%s"`, p.Name)
		if p.Filename != nil {
			disp += fmt.Sprintf(`; filename="%s"`, *p.Filename)
		}
		hdr.Set("Content-Disposition", disp)
		if p.ContentType != "" {
			hdr.Set("Content-Type", p.ContentType)
		}
		w, err := mw.CreatePart(hdr)
		if err != nil {
			return err
		}
		switch {
		case p.Base64 != nil:
			raw, err := base64.StdEncoding.DecodeString(*p.Base64)
			if err != nil {
				return err
			}
			_, err = w.Write(raw)
			if err != nil {
				return err
			}
		case p.Repeat != nil:
			chunk := []byte(strings.Repeat(p.Repeat.Text, 4096))
			for n := p.Repeat.Count; n > 0; n -= 4096 {
				if n < 4096 {
					chunk = chunk[:n*len(p.Repeat.Text)]
				}
				if _, err := w.Write(chunk); err != nil {
					return err
				}
			}
		default:
			if _, err := io.WriteString(w, p.Text); err != nil {
				return err
			}
		}
	}
	return mw.Close()
}

// remember applies a step's captures and keeps the cookies the response set.
func (st *daState) remember(captures []daCapture, res daResult) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, c := range res.headers.Values("Set-Cookie") {
		pair := strings.SplitN(c, ";", 2)[0]
		name := strings.SplitN(pair, "=", 2)[0] + "="
		kept := st.cookies[:0]
		for _, old := range st.cookies {
			if !strings.HasPrefix(old, name) {
				kept = append(kept, old)
			}
		}
		st.cookies = append(kept, pair)
	}
	var parsed interface{}
	dec := json.NewDecoder(bytes.NewReader(res.body))
	dec.UseNumber()
	if dec.Decode(&parsed) != nil {
		parsed = nil
	}
	for _, c := range captures {
		switch c.From {
		case "token":
			v := res.headers.Get("X-FM-Data-Access-Token")
			if v == "" {
				if s, ok := daLookup(parsed, []interface{}{"response", "token"}).(string); ok {
					v = s
				}
			}
			if v != "" {
				st.tokens[c.Var] = v
			}
		case "body":
			switch v := daLookup(parsed, c.Path).(type) {
			case nil:
			case string:
				st.vars[c.Var] = v
			case json.Number:
				st.vars[c.Var] = v.String()
			default:
				st.vars[c.Var] = fmt.Sprint(v)
			}
		case "header":
			if v := res.headers.Get(c.Header); v != "" {
				st.vars[c.Var] = v
			}
		}
	}
}

func daLookup(v interface{}, path []interface{}) interface{} {
	for _, p := range path {
		switch key := p.(type) {
		case string:
			m, ok := v.(map[string]interface{})
			if !ok {
				return nil
			}
			v = m[key]
		case float64:
			a, ok := v.([]interface{})
			if !ok || int(key) < 0 || int(key) >= len(a) {
				return nil
			}
			v = a[int(key)]
		default:
			return nil
		}
	}
	return v
}

// ---------------------------------------------------------------- comparison

// The closed list of tolerated differences. Each is a placeholder the
// recorder wrote in place of a value that cannot be reproduced; anything not
// on this list is compared exactly.
var dataAPIAllowed = map[string]string{
	"<token>":           `[^\s;,"]+`,
	"<cookie>":          `[^;\s]+`,
	"<host>":            `[^/\s"?#]+`,
	"<container-token>": `[A-Za-z0-9._~%-]+`,
	// FileMaker's own timestamp formats; the value must also be close to now.
	"<now>": `(\d\d/\d\d/\d{4} \d\d:\d\d:\d\d|\d{4}-\d\d-\d\d[T ]\d\d:\d\d:\d\d)(?:\.\d+)?`,
	// An HTTP date, as in a cookie's Expires attribute.
	"<date>": `[A-Z][a-z]{2}, \d\d [A-Z][a-z]{2} \d{4} \d\d:\d\d:\d\d GMT`,
}

var dataAPIAllowedRE = regexp.MustCompile(`<token>|<cookie>|<host>|<container-token>|<now>|<date>`)

// dataAPINowWindow is how far a <now> value may be from the replay's clock.
// The server writes its local wall time, whose zone the test does not know.
const dataAPINowWindow = 36 * time.Hour

var dataAPIPatterns sync.Map

// daMatch compares one expected string, which may hold placeholders, with
// the actual one.
func daMatch(expected, actual string) bool {
	if !dataAPIAllowedRE.MatchString(expected) {
		return expected == actual
	}
	re, ok := dataAPIPatterns.Load(expected)
	if !ok {
		var b strings.Builder
		b.WriteString(`\A`)
		last := 0
		for _, loc := range dataAPIAllowedRE.FindAllStringIndex(expected, -1) {
			b.WriteString(regexp.QuoteMeta(expected[last:loc[0]]))
			name := expected[loc[0]:loc[1]]
			pattern := dataAPIAllowed[name]
			if name == "<now>" {
				pattern = `(?P<now>` + pattern + `)`
			} else {
				pattern = `(?:` + pattern + `)`
			}
			b.WriteString(pattern)
			last = loc[1]
		}
		b.WriteString(regexp.QuoteMeta(expected[last:]))
		b.WriteString(`\z`)
		re, _ = dataAPIPatterns.LoadOrStore(expected, regexp.MustCompile(b.String()))
	}
	rx := re.(*regexp.Regexp)
	m := rx.FindStringSubmatch(actual)
	if m == nil {
		return false
	}
	for i, name := range rx.SubexpNames() {
		if name == "now" && !daIsNow(m[i]) {
			return false
		}
	}
	return true
}

func daIsNow(s string) bool {
	for _, layout := range []string{"01/02/2006 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if len(s) < len(layout) {
			continue
		}
		t, err := time.Parse(layout, s[:len(layout)])
		if err != nil {
			continue
		}
		now := time.Now()
		wall := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
		d := wall.Sub(t)
		return d <= dataAPINowWindow && d >= -dataAPINowWindow
	}
	return false
}

func compareDataAPIResponse(where string, want *daResponse, got daResult) []string {
	if want == nil {
		return []string{where + ": the fixture has no response"}
	}
	if got.err != nil {
		return []string{fmt.Sprintf("%s: %v", where, got.err)}
	}
	var diffs []string
	if want.Status != got.status {
		diffs = append(diffs, fmt.Sprintf("%s: status %d, want %d (body %s)", where, got.status, want.Status, daShort(got.body)))
	}
	diffs = append(diffs, compareDataAPIHeaders(where, want.Headers, got.headers)...)
	// A login answers its token twice; a placeholder matches each on its
	// own, so the two must also be the same token.
	if header := got.headers.Get("X-FM-Data-Access-Token"); header != "" {
		var env struct {
			Response struct {
				Token string `json:"token"`
			} `json:"response"`
		}
		if json.Unmarshal(got.body, &env) == nil && env.Response.Token != "" && env.Response.Token != header {
			diffs = append(diffs, where+": the token header and the token in the body differ")
		}
	}
	switch {
	case len(want.Body) > 0:
		if d := compareJSONTokens(want.Body, got.body); d != "" {
			diffs = append(diffs, fmt.Sprintf("%s: body %s; got %s", where, d, daShort(got.body)))
		}
	case want.BodyText != nil:
		if !daMatch(*want.BodyText, string(got.body)) {
			diffs = append(diffs, fmt.Sprintf("%s: body %s, want %s", where, daShort(got.body), daShort([]byte(*want.BodyText))))
		}
	case want.BodyBase64 != nil:
		raw, err := base64.StdEncoding.DecodeString(*want.BodyBase64)
		sum := sha256.Sum256(got.body)
		if err != nil || !bytes.Equal(raw, got.body) || (want.BodySHA256 != "" && hex.EncodeToString(sum[:]) != want.BodySHA256) ||
			(want.BodyLength != nil && *want.BodyLength != len(got.body)) {
			diffs = append(diffs, fmt.Sprintf("%s: body bytes differ (%d bytes, want %d)", where, len(got.body), len(raw)))
		}
	default:
		diffs = append(diffs, where+": the fixture records no body")
	}
	return diffs
}

// compareDataAPIHeaders compares the recorded header subset: the same names,
// the same number of values per name, in the same order.
func compareDataAPIHeaders(where string, want [][2]string, got http.Header) []string {
	wantBy := map[string][]string{}
	for _, kv := range want {
		k := strings.ToLower(kv[0])
		wantBy[k] = append(wantBy[k], kv[1])
	}
	gotBy := map[string][]string{}
	for k, vs := range got {
		lk := strings.ToLower(k)
		if dataAPIRecordedHeaders[lk] || strings.HasPrefix(lk, "x-fm-") {
			gotBy[lk] = append(gotBy[lk], vs...)
		}
	}
	names := map[string]bool{}
	for k := range wantBy {
		names[k] = true
	}
	for k := range gotBy {
		names[k] = true
	}
	sorted := make([]string, 0, len(names))
	for k := range names {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var diffs []string
	for _, k := range sorted {
		w, g := wantBy[k], gotBy[k]
		ok := len(w) == len(g)
		for i := 0; ok && i < len(w); i++ {
			ok = daMatch(w[i], g[i])
		}
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: header %s is %q, want %q", where, k, g, w))
		}
	}
	return diffs
}

// compareJSONTokens walks both documents token by token, so that key order,
// duplicate keys and the spelling of numbers all count. Strings are compared
// after JSON unescaping.
func compareJSONTokens(want, got []byte) string {
	wd := json.NewDecoder(bytes.NewReader(want))
	wd.UseNumber()
	gd := json.NewDecoder(bytes.NewReader(got))
	gd.UseNumber()
	lastKey := ""
	for i := 0; ; i++ {
		wt, werr := wd.Token()
		gt, gerr := gd.Token()
		if werr == io.EOF && gerr == io.EOF {
			return ""
		}
		if werr != nil {
			return "the fixture body is not valid JSON: " + werr.Error()
		}
		if gerr != nil {
			if gerr == io.EOF {
				return fmt.Sprintf("ends early at token %d (after %q), want %v", i, lastKey, wt)
			}
			return "is not valid JSON: " + gerr.Error()
		}
		if !daSameToken(wt, gt) {
			return fmt.Sprintf("differs at token %d (after %q): %s, want %s", i, lastKey, daTok(gt), daTok(wt))
		}
		if s, ok := wt.(string); ok {
			lastKey = s
		}
	}
}

func daSameToken(want, got json.Token) bool {
	switch w := want.(type) {
	case json.Delim:
		g, ok := got.(json.Delim)
		return ok && g == w
	case string:
		g, ok := got.(string)
		return ok && daMatch(w, g)
	case json.Number:
		g, ok := got.(json.Number)
		return ok && g == w
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case nil:
		return got == nil
	}
	return false
}

func daTok(t json.Token) string {
	switch v := t.(type) {
	case json.Delim:
		return v.String()
	case json.Number:
		return "number " + v.String()
	case nil:
		return "null"
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

func daShort(b []byte) string {
	const max = 240
	s := string(b)
	if len(s) > max {
		s = s[:max] + "..."
	}
	return fmt.Sprintf("%q", s)
}
