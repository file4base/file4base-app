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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
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
// testdata/dataapi/expected_failures.txt records, for each fixture that does
// not pass yet, the step and the check at which it first fails. Failing
// earlier or differently fails the test, and so does passing; see the
// README for how the list may change.

const (
	dataAPIDir      = "../../testdata/dataapi"
	dataAPIDatabase = "F4B_DataAPI_Reference"
	dataAPIOwner    = "conformance_owner"
	// Every replayed request is sent to this host, and a <host> placeholder
	// stands for exactly this host.
	dataAPIReplayHost = "fms.conformance.test"
)

// The accounts of the reference file, by the names the fixtures use. Their
// passwords are set by this test; the recorded fixtures never contain any.
var dataAPIAccounts = []string{"admin", "api_rw", "api_ro", "noapi"}

func dataAPIPassword(account string) string { return "conformance-" + account + "-9" }

// The migration failures expected_failures.txt may excuse, by the issue that
// tracks each. Any other migration failure fails the test.
var dataAPIKnownMigrationFailures = map[string]*regexp.Regexp{
	// MariaDB: a table's default occurrence collides with the occurrence whose
	// name differs from it only in case.
	"#146": regexp.MustCompile(`\Amigrate/apply answered 400: .*Duplicate entry 'Customers' for key 'name'`),
}

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

func parseDataAPIFixture(id string, raw []byte) (daFixture, error) {
	var f daFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, fmt.Errorf("fixture %s: %w", id, err)
	}
	if f.Fixture != id {
		return f, fmt.Errorf("fixture %s declares itself as %q", id, f.Fixture)
	}
	if len(f.Steps) == 0 {
		return f, fmt.Errorf("fixture %s has no step", id)
	}
	seen := map[string]bool{}
	for _, s := range f.Steps {
		if s.ID == "" || seen[s.ID] {
			return f, fmt.Errorf("fixture %s: step ids must be present and unique (%q)", id, s.ID)
		}
		seen[s.ID] = true
	}
	return f, nil
}

func loadDataAPIFixtures(t *testing.T) []daFixture {
	t.Helper()
	tree, err := daWorkTree()
	require.NoError(t, err)
	require.NotEmpty(t, tree.fixtures, "the Data API fixtures must be readable")
	var out []daFixture
	for id, raw := range tree.fixtures {
		f, err := parseDataAPIFixture(id, raw)
		require.NoError(t, err)
		out = append(out, f)
	}
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

// ---------------------------------------------------------------- expected failures

// daEntry is one line of expected_failures.txt: either
//
//	migration@ENGINE ISSUE
//
// for a migration that fails on an engine for a known cause, or
//
//	FIXTURE STEP KIND [ENGINE]
//
// for a fixture that first fails at that step, with that kind of difference.
type daEntry struct {
	line    int
	fixture string
	step    string
	kind    string
	engine  string // "" for every engine
	cause   string // migration entries only
}

func (e daEntry) String() string {
	if e.cause != "" {
		return "migration@" + e.engine + " " + e.cause
	}
	s := e.fixture + " " + e.step + " " + e.kind
	if e.engine != "" {
		s += " " + e.engine
	}
	return s
}

type daExpectations struct {
	migration map[string]daEntry // by engine
	fixtures  map[string]daEntry // by fixture + "|" + engine
}

// lookup returns the entry for a fixture on an engine, if any; an entry for
// that engine wins over one for every engine.
func (x daExpectations) lookup(fixture, engine string) *daEntry {
	for _, k := range []string{fixture + "|" + engine, fixture + "|"} {
		if e, ok := x.fixtures[k]; ok {
			return &e
		}
	}
	return nil
}

var dataAPIEngines = map[string]bool{string(dbal.EnginePostgres): true, string(dbal.EngineMariaDB): true}

func parseDataAPIExpected(raw []byte) (daExpectations, []string) {
	x := daExpectations{migration: map[string]daEntry{}, fixtures: map[string]daEntry{}}
	var problems []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		bad := func(why string) {
			problems = append(problems, fmt.Sprintf("expected_failures.txt:%d: %s: %q", n, why, line))
		}
		f := strings.Fields(line)
		if engine, ok := strings.CutPrefix(f[0], "migration@"); ok {
			switch {
			case len(f) != 2 || !dataAPIEngines[engine]:
				bad("a migration entry is migration@ENGINE ISSUE")
			case dataAPIKnownMigrationFailures[f[1]] == nil:
				bad("no known migration failure is tracked by " + f[1])
			case x.migration[engine].line != 0:
				bad("a second migration entry for " + engine)
			default:
				x.migration[engine] = daEntry{line: n, engine: engine, cause: f[1]}
			}
			continue
		}
		e := daEntry{line: n}
		switch len(f) {
		case 4:
			e.engine = f[3]
			fallthrough
		case 3:
			e.fixture, e.step, e.kind = f[0], f[1], f[2]
		default:
			bad("an entry is FIXTURE STEP KIND [ENGINE]")
			continue
		}
		key := e.fixture + "|" + e.engine
		switch {
		case e.engine != "" && !dataAPIEngines[e.engine]:
			bad("unknown engine")
		case !daValidKind(e.kind):
			bad("unknown kind of difference " + e.kind)
		case x.fixtures[key].line != 0:
			bad("a second entry for this fixture")
		default:
			x.fixtures[key] = e
		}
	}
	if err := sc.Err(); err != nil {
		problems = append(problems, err.Error())
	}
	return x, problems
}

func loadExpectedFailures(t *testing.T, fixtures []daFixture) daExpectations {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataAPIDir, "expected_failures.txt"))
	require.NoError(t, err)
	x, problems := parseDataAPIExpected(raw)
	byID := map[string]daFixture{}
	for _, f := range fixtures {
		byID[f.Fixture] = f
	}
	for _, e := range x.fixtures {
		f, ok := byID[e.fixture]
		if !ok {
			problems = append(problems, fmt.Sprintf("expected_failures.txt:%d lists %s, which is not a fixture", e.line, e.fixture))
		} else if _, ok := daPosition(f, e.step, e.kind); !ok {
			problems = append(problems, fmt.Sprintf("expected_failures.txt:%d: %s has no step %s", e.line, e.fixture, e.step))
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
	if len(problems) > 0 {
		t.FailNow()
	}
	return x
}

// The kinds of difference, in the order a step's checks run. A concurrent
// step checks its background request ("background.KIND"), then whether a
// probe ran at all ("no-probe"), then the probe ("probe.KIND").
var dataAPIKindRank = map[string]int{
	"unreplayable": 0, "url": 2, "timeout": 3, "status": 4,
	"content-length": 5, "header": 6, "token": 7, "body": 8,
}

func daKindOrder(kind string) (int, string, bool) {
	base := 0
	switch {
	case kind == "no-probe":
		return 100, "", true
	case strings.HasPrefix(kind, "background."):
		kind = strings.TrimPrefix(kind, "background.")
	case strings.HasPrefix(kind, "probe."):
		base, kind = 200, strings.TrimPrefix(kind, "probe.")
	}
	name, sub, _ := strings.Cut(kind, ":")
	r, ok := dataAPIKindRank[name]
	if !ok || (name == "header") != (sub != "") {
		return 0, "", false
	}
	return base + r, sub, true
}

func daValidKind(kind string) bool { _, _, ok := daKindOrder(kind); return ok }

// daPos is where a fixture first fails: the step, then the check within it.
type daPos struct {
	step int
	rank int
	sub  string
}

func (p daPos) before(q daPos) bool {
	if p.step != q.step {
		return p.step < q.step
	}
	if p.rank != q.rank {
		return p.rank < q.rank
	}
	return p.sub < q.sub
}

func daPosition(f daFixture, step, kind string) (daPos, bool) {
	rank, sub, ok := daKindOrder(kind)
	if !ok {
		return daPos{}, false
	}
	for i, s := range f.Steps {
		if s.ID == step {
			return daPos{i, rank, sub}, true
		}
	}
	return daPos{}, false
}

// judgeDataAPIFixture compares a fixture's differences with its entry in
// expected_failures.txt (nil when it must pass). It returns the problems to
// report, and whether the fixture failed exactly as listed.
func judgeDataAPIFixture(f daFixture, entry *daEntry, engine string, diffs []daDiff) ([]string, bool) {
	var problems []string
	var first *daDiff
	var firstPos daPos
	for i := range diffs {
		d := &diffs[i]
		if d.harness {
			// The fixture or the replay is broken: never an expected failure.
			problems = append(problems, "harness: "+d.msg)
			continue
		}
		p, ok := daPosition(f, d.stepID, d.kind)
		if !ok {
			problems = append(problems, fmt.Sprintf("harness: %s has no order (%s)", d.kind, d.msg))
			continue
		}
		if first == nil || p.before(firstPos) {
			first, firstPos = d, p
		}
	}
	line := func() string {
		e := daEntry{fixture: f.Fixture, step: first.stepID, kind: first.kind}
		if entry != nil {
			e.engine = entry.engine
		}
		return e.String()
	}
	switch {
	case first == nil && entry == nil:
	case first == nil:
		problems = append(problems, fmt.Sprintf("%s now passes on %s: remove `%s` from testdata/dataapi/expected_failures.txt", f.Fixture, engine, entry))
	case entry == nil:
		for _, d := range diffs {
			if !d.harness {
				problems = append(problems, d.msg)
			}
		}
		problems = append(problems, fmt.Sprintf("%s fails on %s; its first difference would be listed as `%s`", f.Fixture, engine, line()))
	default:
		want, _ := daPosition(f, entry.step, entry.kind)
		switch {
		case first.stepID == entry.step && first.kind == entry.kind:
			return problems, len(problems) == 0
		case want.before(firstPos):
			problems = append(problems, fmt.Sprintf("%s now first fails later, at %s: replace `%s` with `%s` in testdata/dataapi/expected_failures.txt (%s)",
				f.Fixture, first.stepID, entry, line(), first.msg))
		default:
			for _, d := range diffs {
				if p, ok := daPosition(f, d.stepID, d.kind); ok && !d.harness && !want.before(p) {
					problems = append(problems, d.msg)
				}
			}
			problems = append(problems, fmt.Sprintf("%s regressed: it first fails as `%s`, listed as `%s`", f.Fixture, line(), entry))
		}
	}
	return problems, false
}

// ---------------------------------------------------------------- the test

func TestDataAPIConformance(t *testing.T) {
	fixtures := loadDataAPIFixtures(t)
	expected := loadExpectedFailures(t, fixtures)
	engine := string(testdb.Engine())

	store, err := control.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, store.Close()) })
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, ControlStore: store})
	_, exists, err := h.mgr.LookupDatabase(t.Context(), dataAPIDatabase)
	require.NoError(t, err)
	if exists {
		t.Fatalf("a database named %s already exists on the test server; this test creates and drops it, so drop it first", dataAPIDatabase)
	}
	// Only a database this run created is ever dropped.
	created := false
	t.Cleanup(func() {
		if created {
			if err := h.mgr.DropDatabase(context.Background(), dataAPIDatabase); err != nil {
				t.Errorf("dropping %s: %v", dataAPIDatabase, err)
			}
		}
	})

	migrationEntry, migrationListed := expected.migration[engine]
	shared, blocked, broken := false, false, false
	passed, failedAsExpected, skipped, requests := 0, 0, 0, 0
	for _, f := range fixtures {
		f := f
		t.Run(f.Fixture, func(t *testing.T) {
			switch {
			case broken:
				skipped++
				t.Skip("not replayed: the reference file failed to migrate (see above)")
			case blocked:
				skipped++
				t.Skipf("not replayed: the reference file does not migrate on %s (%s)", engine, migrationEntry.cause)
			}
			if f.Fresh || !shared {
				err := migrateDataAPIReference(t, h, &created)
				if err != nil {
					if !migrationListed || !dataAPIKnownMigrationFailures[migrationEntry.cause].MatchString(err.Error()) {
						broken = true
						t.Fatalf("the reference file does not migrate on %s, and not for a listed cause: %v", engine, err)
					}
					// Fixture expectations are only evaluated where the
					// reference file migrates.
					blocked = true
					skipped++
					t.Skipf("not replayed: the reference file does not migrate on %s (%s): %v", engine, migrationEntry.cause, err)
				}
				shared = !f.Fresh
			}
			diffs, sent := replayDataAPIFixture(h, f)
			requests += sent
			problems, asListed := judgeDataAPIFixture(f, expected.lookup(f.Fixture, engine), engine, diffs)
			for _, p := range problems {
				t.Error(p)
			}
			switch {
			case asListed:
				failedAsExpected++
				t.Logf("fails as listed, %d difference(s); first: %s", len(diffs), daFirstMessage(diffs))
			case len(problems) == 0:
				passed++
			}
		})
	}
	if migrationListed && !blocked && !broken {
		t.Errorf("the reference file now migrates on %s: remove `%s` from testdata/dataapi/expected_failures.txt", engine, migrationEntry)
	}
	t.Logf("%s: %d fixtures, %d requests replayed; %d pass, %d fail as listed, %d not replayed",
		engine, len(fixtures), requests, passed, failedAsExpected, skipped)
}

func daFirstMessage(diffs []daDiff) string {
	if len(diffs) == 0 {
		return ""
	}
	return diffs[0].msg
}

// migrateDataAPIReference replaces the reference database with a fresh
// migration of the reference file, and gives its accounts test passwords.
// created tracks whether the database exists because this run created it.
func migrateDataAPIReference(t *testing.T, h *apiHarness, created *bool) error {
	t.Helper()
	ctx := t.Context()
	_, exists, err := h.mgr.LookupDatabase(ctx, dataAPIDatabase)
	require.NoError(t, err)
	if exists {
		require.True(t, *created, "%s exists, but this run did not create it; is another run using it?", dataAPIDatabase)
		require.NoError(t, h.mgr.DropDatabase(ctx, dataAPIDatabase))
		*created = false
	}
	rec := daJSON(h, http.MethodPost, "/api/v1/databases", "", map[string]string{
		"database": dataAPIDatabase, "user": dataAPIOwner, "password": dataAPIPassword(dataAPIOwner),
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	*created = true

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
		return fmt.Errorf("migrate/apply answered %d: %s", rec.Code, rec.Body.String())
	}

	var users []schema.UserMetadata
	rec = daJSON(h, http.MethodGet, "/api/v1/security/users", login.Token, nil, &users)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	byName := map[string]schema.UserMetadata{}
	for _, u := range users {
		byName[u.Username] = u
	}
	for _, account := range dataAPIAccounts {
		u, ok := byName[account]
		if !ok {
			return fmt.Errorf("the migration did not create the account %s", account)
		}
		rec = daJSON(h, http.MethodPut, "/api/v1/security/users/"+u.ID, login.Token, map[string]interface{}{
			"role": u.Role, "password": dataAPIPassword(account), "is_active": true,
		}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	return nil
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

// ---------------------------------------------------------------- the list only shrinks

// daTree is the conformance data at one revision.
type daTree struct {
	expected []byte            // nil when there is no expected_failures.txt
	fixtures map[string][]byte // by fixture id
}

func daWorkTree() (daTree, error) {
	tree := daTree{fixtures: map[string][]byte{}}
	raw, err := os.ReadFile(filepath.Join(dataAPIDir, "expected_failures.txt"))
	if err != nil {
		return tree, err
	}
	tree.expected = raw
	root := filepath.Join(dataAPIDir, "fixtures")
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tree.fixtures[strings.TrimSuffix(filepath.ToSlash(rel), ".json")] = raw
		return nil
	})
	return tree, err
}

func daGit(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, ee.Stderr)
	}
	return out, err
}

func daGitTree(t *testing.T, rev string) daTree {
	t.Helper()
	_, err := daGit(t, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	require.NoError(t, err, "the base revision %s is not available; fetch it first", rev)
	top, err := daGit(t, "rev-parse", "--show-toplevel")
	require.NoError(t, err)
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	require.NoError(t, err)
	dir, err := filepath.Abs(dataAPIDir)
	require.NoError(t, err)
	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	rel, err := filepath.Rel(root, dir)
	require.NoError(t, err)
	rel = filepath.ToSlash(rel)

	tree := daTree{fixtures: map[string][]byte{}}
	if _, err := daGit(t, "cat-file", "-e", rev+":"+rel+"/expected_failures.txt"); err == nil {
		tree.expected, err = daGit(t, "show", rev+":"+rel+"/expected_failures.txt")
		require.NoError(t, err)
	}
	list, err := daGit(t, "ls-tree", "-r", "--full-tree", "--name-only", rev, "--", rel+"/fixtures/")
	require.NoError(t, err)
	for _, path := range strings.Split(strings.TrimSpace(string(list)), "\n") {
		if !strings.HasSuffix(path, ".json") {
			continue
		}
		raw, err := daGit(t, "show", rev+":"+path)
		require.NoError(t, err)
		tree.fixtures[strings.TrimSuffix(strings.TrimPrefix(path, rel+"/fixtures/"), ".json")] = raw
	}
	if tree.expected != nil {
		require.NotEmpty(t, tree.fixtures, "no fixture found at %s in %s", rel, rev)
	}
	return tree
}

// daExchanges is what a fixture recorded, without its descriptions: two
// fixtures with the same exchanges differ only in titles, notes or audit
// rows.
func daExchanges(raw []byte) (string, error) {
	var f struct {
		Fixture string                       `json:"fixture"`
		Fresh   bool                         `json:"fresh"`
		Steps   []map[string]json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", err
	}
	strip := func(m map[string]json.RawMessage) { delete(m, "note"); delete(m, "rows") }
	for _, s := range f.Steps {
		strip(s)
		c, ok := s["concurrent"]
		if !ok {
			continue
		}
		var cm map[string]json.RawMessage
		if err := json.Unmarshal(c, &cm); err != nil {
			return "", err
		}
		for _, k := range []string{"background", "probe"} {
			var sm map[string]json.RawMessage
			if err := json.Unmarshal(cm[k], &sm); err != nil {
				return "", err
			}
			strip(sm)
			cm[k], _ = json.Marshal(sm)
		}
		s["concurrent"], _ = json.Marshal(cm)
	}
	out, err := json.Marshal(f)
	return string(out), err
}

// dataAPIRatchetViolations lists the ways head lets expected_failures.txt
// grow compared with base. A fixture may be newly listed, or listed as
// failing elsewhere, only if it is new or its recorded exchanges changed. An
// entry may move to a later step or check, which is progress. A migration
// entry may never be added or changed, and no fixture may be deleted.
func dataAPIRatchetViolations(base, head daTree) []string {
	if base.expected == nil {
		return nil // the suite is introduced by head
	}
	var out []string
	for id := range base.fixtures {
		if _, ok := head.fixtures[id]; !ok {
			out = append(out, fmt.Sprintf("fixture %s was deleted; fixtures are only ever re-recorded", id))
		}
	}
	old, _ := parseDataAPIExpected(base.expected)
	cur, problems := parseDataAPIExpected(head.expected)
	out = append(out, problems...)
	for engine, e := range cur.migration {
		if was, ok := old.migration[engine]; !ok || was.cause != e.cause {
			out = append(out, fmt.Sprintf("`%s` was added: a migration that works must keep working", e))
		}
	}
	keys := make([]string, 0, len(cur.fixtures))
	for k := range cur.fixtures {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := cur.fixtures[k]
		was := old.lookup(e.fixture, e.engine)
		if was != nil && was.step == e.step && was.kind == e.kind {
			continue
		}
		oldRaw, existed := base.fixtures[e.fixture]
		if !existed {
			continue // a new fixture
		}
		a, errA := daExchanges(oldRaw)
		b, errB := daExchanges(head.fixtures[e.fixture])
		if errA != nil || errB != nil {
			out = append(out, fmt.Sprintf("%s cannot be read: %v %v", e.fixture, errA, errB))
			continue
		}
		if a != b {
			continue // re-recorded
		}
		f, err := parseDataAPIFixture(e.fixture, head.fixtures[e.fixture])
		if err != nil {
			out = append(out, err.Error())
			continue
		}
		if was != nil {
			from, ok1 := daPosition(f, was.step, was.kind)
			to, ok2 := daPosition(f, e.step, e.kind)
			if ok1 && ok2 && from.before(to) {
				continue // it now fails later
			}
		}
		out = append(out, fmt.Sprintf("`%s` was added to the expected failures, but the exchanges recorded in %s did not change", e, e.fixture))
	}
	return out
}

// CI runs this with F4B_DATAAPI_BASE_REF set to the pull request's base, or
// to the previous commit of main.
func TestDataAPIExpectedFailuresOnlyShrink(t *testing.T) {
	rev := os.Getenv("F4B_DATAAPI_BASE_REF")
	if rev == "" {
		t.Skip("set F4B_DATAAPI_BASE_REF to the revision to compare with; CI does")
	}
	head, err := daWorkTree()
	require.NoError(t, err)
	for _, v := range dataAPIRatchetViolations(daGitTree(t, rev), head) {
		t.Error(v)
	}
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
	headers http.Header // canonical names
	body    []byte
	err     error  // the request could not be built from the fixture
	refused string // the request was not sent: what the server gave cannot be followed
}

// daDiff is one difference, located in its fixture.
type daDiff struct {
	step    int
	stepID  string
	kind    string // see dataAPIKindRank
	msg     string
	harness bool // the fixture or the replay is at fault; never an expected failure
}

func replayDataAPIFixture(h *apiHarness, f daFixture) ([]daDiff, int) {
	st := &daState{tokens: map[string]string{}, vars: map[string]string{}}
	var diffs []daDiff
	sent := 0
	for i, step := range f.Steps {
		where := f.Fixture + " step " + step.ID
		var d []daDiff
		switch {
		case step.IdleSeconds != nil:
			// Waiting out FileMaker's idle timeout in real time is not an
			// option in CI; this needs a session clock the test can move.
			d = append(d, daDiff{kind: "unreplayable", msg: fmt.Sprintf("%s: an idle wait of %vs cannot be replayed without a session clock seam", where, *step.IdleSeconds)})
		case step.Concurrent != nil:
			cd, n := replayConcurrent(h, st, where, step.Concurrent)
			d = append(d, cd...)
			sent += n
		case step.Request != nil && step.Response != nil:
			res := sendDataAPIRequest(h, st, step)
			sent++
			d = append(d, compareDataAPIResponse(where, step.Response, res)...)
		default:
			d = append(d, daDiff{kind: "harness", harness: true, msg: where + ": the fixture step has no request and response"})
		}
		for j := range d {
			d[j].step, d[j].stepID = i, step.ID
		}
		diffs = append(diffs, d...)
	}
	return diffs, sent
}

// dataAPIBackgroundLimit bounds a concurrent step's background request.
const dataAPIBackgroundLimit = 2 * time.Minute

func daPrefixed(prefix string, diffs []daDiff) []daDiff {
	for i := range diffs {
		if !diffs[i].harness {
			diffs[i].kind = prefix + diffs[i].kind
		}
	}
	return diffs
}

// replayConcurrent sends the background request, then repeats the probe
// while the background request is still running, until the probe answers
// until_code. A probe that only gets there after the background request has
// finished does not count.
func replayConcurrent(h *apiHarness, st *daState, where string, c *daConcurrent) ([]daDiff, int) {
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
			return []daDiff{{kind: "timeout", msg: fmt.Sprintf("%s: the background request %s did not finish within %s", where, c.Background.ID, dataAPIBackgroundLimit)}}, sent
		}
	}
	diffs := daPrefixed("background.", compareDataAPIResponse(where+" (background "+c.Background.ID+")", c.Background.Response, *background))
	if probe == nil {
		return append(diffs, daDiff{kind: "no-probe", msg: fmt.Sprintf("%s: the background request %s finished before any probe was sent", where, c.Background.ID)}), sent
	}
	return append(diffs, daPrefixed("probe.", compareDataAPIResponse(where+" (probe "+c.Probe.ID+")", c.Probe.Response, *probe))...), sent
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

// daCanonicalHeaders merges header names that differ only in case, as a
// client reading them off the wire would, in a deterministic order.
func daCanonicalHeaders(h http.Header) http.Header {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := http.Header{}
	for _, k := range keys {
		ck := http.CanonicalHeaderKey(k)
		out[ck] = append(out[ck], h[k]...)
	}
	return out
}

func sendDataAPIRequest(h *apiHarness, st *daState, step daStep) daResult {
	rq := step.Request
	target := st.expand(rq.Path)
	if rq.URL != "" {
		// A URL the server returned (a container URL) is only followed when
		// it points at the server the replay talks to.
		raw := st.expand(rq.URL)
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Host != dataAPIReplayHost || u.Opaque != "" {
			return daResult{refused: fmt.Sprintf("the URL to follow, %q, is not an https URL on %s", raw, dataAPIReplayHost)}
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
	req.Host = dataAPIReplayHost
	// FileMaker Server answers over HTTPS only; so does the replay.
	req.TLS = &tls.ConnectionState{ServerName: dataAPIReplayHost}
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
	res := daResult{status: resp.StatusCode, headers: daCanonicalHeaders(resp.Header), body: rec.Body.Bytes()}
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
// on this list is compared exactly. None of the grammars admits "<" or ">",
// so that a placeholder never matches itself.
var dataAPIAllowed = map[string]string{
	// RFC 6750 b64token.
	"<token>": `[A-Za-z0-9\-._~+/]+=*`,
	// RFC 6265 cookie-octets.
	"<cookie>":          `[\x21\x23-\x2B\x2D-\x3A\x3D\x3F-\x5B\x5D-\x7E]+`,
	"<host>":            regexp.QuoteMeta(dataAPIReplayHost),
	"<container-token>": `[A-Za-z0-9._~%-]+`,
	// FileMaker's own timestamp formats; the value must also be close to now.
	"<now>": `(?:\d\d/\d\d/\d{4} \d\d:\d\d:\d\d|\d{4}-\d\d-\d\d[T ]\d\d:\d\d:\d\d)(?:\.\d+)?`,
	// An RFC 9110 IMF-fixdate, as in a cookie's Expires attribute; it must
	// also be a real date.
	"<date>": `(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d\d (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} \d\d:\d\d:\d\d GMT`,
}

var dataAPIAllowedRE = regexp.MustCompile(`<token>|<cookie>|<host>|<container-token>|<now>|<date>`)

// dataAPINowWindow is how far a <now> value may be from the replay's clock.
// The server writes its local wall time, whose zone the test does not know.
const dataAPINowWindow = 36 * time.Hour

type daPattern struct {
	re     *regexp.Regexp
	checks []string // per capture group: the placeholder it holds
}

var dataAPIPatterns sync.Map

// daMatch compares one expected string, which may hold placeholders, with
// the actual one.
func daMatch(expected, actual string) bool {
	if !dataAPIAllowedRE.MatchString(expected) {
		return expected == actual
	}
	cached, ok := dataAPIPatterns.Load(expected)
	if !ok {
		var b strings.Builder
		var checks []string
		b.WriteString(`\A`)
		last := 0
		for _, loc := range dataAPIAllowedRE.FindAllStringIndex(expected, -1) {
			b.WriteString(regexp.QuoteMeta(expected[last:loc[0]]))
			name := expected[loc[0]:loc[1]]
			pattern := dataAPIAllowed[name]
			if name == "<now>" || name == "<date>" {
				b.WriteString(`(` + pattern + `)`)
				checks = append(checks, name)
			} else {
				b.WriteString(`(?:` + pattern + `)`)
			}
			last = loc[1]
		}
		b.WriteString(regexp.QuoteMeta(expected[last:]))
		b.WriteString(`\z`)
		cached, _ = dataAPIPatterns.LoadOrStore(expected, daPattern{re: regexp.MustCompile(b.String()), checks: checks})
	}
	p := cached.(daPattern)
	m := p.re.FindStringSubmatchIndex(actual)
	if m == nil {
		return false
	}
	// Group i+1 holds the value of the i-th <now> or <date>.
	for i, name := range p.checks {
		s := actual[m[2*i+2]:m[2*i+3]]
		switch name {
		case "<now>":
			if !daIsNow(s) {
				return false
			}
		case "<date>":
			if tm, err := time.Parse(http.TimeFormat, s); err != nil || tm.Format(http.TimeFormat) != s {
				return false
			}
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

// daBodyless reports the statuses whose response a client receives with no
// body, whatever the handler wrote.
func daBodyless(status int) bool {
	return (status >= 100 && status < 200) || status == http.StatusNoContent || status == http.StatusNotModified
}

func compareDataAPIResponse(where string, want *daResponse, got daResult) []daDiff {
	if want == nil {
		return []daDiff{{kind: "harness", harness: true, msg: where + ": the fixture has no response"}}
	}
	if got.err != nil {
		return []daDiff{{kind: "harness", harness: true, msg: fmt.Sprintf("%s: %v", where, got.err)}}
	}
	if got.refused != "" {
		return []daDiff{{kind: "url", msg: where + ": " + got.refused}}
	}
	var diffs []daDiff
	add := func(kind, format string, args ...interface{}) {
		diffs = append(diffs, daDiff{kind: kind, msg: where + ": " + fmt.Sprintf(format, args...)})
	}
	if want.Status != got.status {
		add("status", "status %d, want %d (body %s)", got.status, want.Status, daShort(got.body))
	}
	// What a client receives: the declared length must be the body's.
	if cl := got.headers.Values("Content-Length"); len(cl) > 0 {
		n, err := strconv.Atoi(cl[0])
		if len(cl) > 1 || err != nil || n != len(got.body) {
			add("content-length", "Content-Length %q, but the body has %d bytes", cl, len(got.body))
		}
	}
	if daBodyless(got.status) && len(got.body) > 0 {
		add("content-length", "a %d response carries no body, but %d bytes were written", got.status, len(got.body))
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
			add("token", "the token header and the token in the body differ")
		}
	}
	switch {
	case len(want.Body) > 0:
		if d := compareJSONTokens(want.Body, got.body); d != "" {
			add("body", "body %s; got %s", d, daShort(got.body))
		}
	case want.BodyText != nil:
		if !daMatch(*want.BodyText, string(got.body)) {
			add("body", "body %s, want %s", daShort(got.body), daShort([]byte(*want.BodyText)))
		}
	case want.BodyBase64 != nil:
		raw, err := base64.StdEncoding.DecodeString(*want.BodyBase64)
		if err != nil {
			return append(diffs, daDiff{kind: "harness", harness: true, msg: where + ": the fixture's body_base64 is not base64"})
		}
		sum := sha256.Sum256(got.body)
		if !bytes.Equal(raw, got.body) || (want.BodySHA256 != "" && hex.EncodeToString(sum[:]) != want.BodySHA256) ||
			(want.BodyLength != nil && *want.BodyLength != len(got.body)) {
			add("body", "body bytes differ (%d bytes, want %d)", len(got.body), len(raw))
		}
	default:
		diffs = append(diffs, daDiff{kind: "harness", harness: true, msg: where + ": the fixture records no body"})
	}
	return diffs
}

// compareDataAPIHeaders compares the recorded header subset: the same names,
// the same number of values per name, in the same order. got must have
// canonical names.
func compareDataAPIHeaders(where string, want [][2]string, got http.Header) []daDiff {
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
	var diffs []daDiff
	for _, k := range sorted {
		w, g := wantBy[k], gotBy[k]
		ok := len(w) == len(g)
		for i := 0; ok && i < len(w); i++ {
			ok = daMatch(w[i], g[i])
		}
		if !ok {
			diffs = append(diffs, daDiff{kind: "header:" + k, msg: fmt.Sprintf("%s: header %s is %q, want %q", where, k, g, w)})
		}
	}
	return diffs
}

// compareJSONTokens walks both documents token by token, so that key order,
// duplicate keys and the spelling of numbers all count. Strings are compared
// after JSON unescaping; placeholders apply to values only, never to keys.
func compareJSONTokens(want, got []byte) string {
	wd := json.NewDecoder(bytes.NewReader(want))
	wd.UseNumber()
	gd := json.NewDecoder(bytes.NewReader(got))
	gd.UseNumber()
	// For each open container: whether it is an object whose next token is a key.
	type frame struct{ object, key bool }
	var stack []frame
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
				return fmt.Sprintf("ends early at token %d (after %q), want %v", i, lastKey, daTok(wt))
			}
			return "is not valid JSON: " + gerr.Error()
		}
		_, isString := wt.(string)
		isKey := isString && len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].key
		same := false
		if isKey {
			ws, _ := wt.(string)
			gs, ok := gt.(string)
			same = ok && ws == gs
		} else {
			same = daSameToken(wt, gt)
		}
		if !same {
			return fmt.Sprintf("differs at token %d (after %q): %s, want %s", i, lastKey, daTok(gt), daTok(wt))
		}
		if s, ok := wt.(string); ok && isKey {
			lastKey = s
		}
		// Where the next token stands.
		switch d, _ := wt.(json.Delim); {
		case d == '{' || d == '[':
			if len(stack) > 0 && stack[len(stack)-1].object {
				stack[len(stack)-1].key = true
			}
			stack = append(stack, frame{object: d == '{', key: d == '{'})
		case d == '}' || d == ']':
			stack = stack[:len(stack)-1]
		case len(stack) > 0 && stack[len(stack)-1].object:
			stack[len(stack)-1].key = !isKey
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
