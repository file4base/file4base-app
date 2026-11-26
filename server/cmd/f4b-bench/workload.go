package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// bench is one engine's solution database and the services over it.
type bench struct {
	engine    dbal.EngineType
	mgr       *dbal.MultiDatabaseManager
	adminDSN  string
	dbName    string
	driver    dbal.DatabaseDriver
	schema    *schema.Service
	data      *data.Service
	table     *schema.TableMetadata
	members   []member
	ids       []string
	noteBytes int
	duration  time.Duration
	container string
	backupDir string
	noRestore bool
	results   map[string]any
	maxWAL    atomic.Int64
}

// watchWAL records the largest size the WAL reaches (SQLite), every 100 ms,
// until the returned function is called.
func (b *bench) watchWAL() (stop func()) {
	if b.engine != dbal.EngineSQLite {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		for {
			if st, err := os.Stat(b.sqlitePath() + "-wal"); err == nil {
				for {
					cur := b.maxWAL.Load()
					if st.Size() <= cur || b.maxWAL.CompareAndSwap(cur, st.Size()) {
						break
					}
				}
			}
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	return func() { close(done) }
}

// member is one generated record of the members table.
type member struct {
	id, name, city, category, fee, joined string
	index                                 int
}

// notes is a record's notes, made when they are written rather than kept:
// a 10 GB solution's notes do not fit in memory.
func notes(index, length int) string {
	r := rand.New(rand.NewPCG(seed, uint64(index)))
	var b strings.Builder
	for b.Len() < length {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(noteWords[r.IntN(len(noteWords))])
	}
	return b.String()
}

// The text values, chosen for what they test: case variants, accents,
// German ß, Turkish dotted and dotless i, and letters outside Latin-1.
var (
	firstNames = []string{
		"Élodie", "Elodie", "élodie", "Zoë", "Zoe", "Jürgen", "Jurgen", "Ömer", "İsmail", "Ismail",
		"ılgın", "Işık", "Ayşe", "François", "Francois", "Søren", "Ægir", "Ñuño", "Łukasz", "Anna",
		"anna", "ANNA", "Bob", "bob", "Ève", "Eve", "Ångström", "Ørjan", "Çağla", "José", "Jose",
		"Zeynep", "Šárka", "Đorđe", "Margaux", "Noël", "Noel", "Chloé", "Chloe", "Gülşen",
	}
	lastNames = []string{
		"Müller", "Mueller", "MÜLLER", "Straße", "Strasse", "STRASSE", "Lefèvre", "Lefevre", "Dubois",
		"Çelik", "Yılmaz", "YILMAZ", "Öztürk", "Ozturk", "Gómez", "Gomez", "Østergaard", "Ostergaard",
		"Weiß", "Weiss", "van der Berg", "O'Brien", "Le Clézio", "de La Fontaine", "Zürcher", "Ünal",
		"İnce", "Ince", "ıssız", "Martin", "martin", "Bernard", "Petit", "Dupont", "Groß", "Gross",
	}
	cities = []string{
		"Besançon", "Besancon", "Köln", "Koeln", "Zürich", "Zurich", "İzmir", "Izmir", "Istanbul",
		"İstanbul", "istanbul", "Málaga", "Malaga", "Göteborg", "Oslo", "Saint-Étienne", "Saint-Etienne",
		"Nürnberg", "München", "Muenchen", "Ankara", "Paris", "paris", "PARIS", "Lyon", "Genève",
		"Geneve", "Kraków", "Łódź", "Århus", "Reykjavík", "Straßburg",
	}
	categories = []string{"Éco", "Eco", "eco", "Pro", "pro", "Étudiant", "Etudiant", "Senior", "Jeune", "Famille"}
	noteWords  = []string{
		"adhésion", "renouvelée", "cotisation", "réglée", "Straße", "größer", "çalışma", "ışık",
		"İlk", "rappel", "envoyé", "dossier", "complet", "à", "vérifier", "the", "member", "paid",
	}
)

const seed = 199

func open(ctx context.Context, engine dbal.EngineType, admin string, rows, noteBytes int, reuse bool) (*bench, error) {
	if admin == "" {
		return nil, errors.New("-admin is required")
	}
	if engine == dbal.EngineSQLite {
		abs, err := filepath.Abs(admin)
		if err != nil {
			return nil, err
		}
		admin = abs
	}
	mgr, err := dbal.NewMultiDatabaseManager(engine, admin)
	if err != nil {
		return nil, err
	}
	b := &bench{
		engine: engine, mgr: mgr, adminDSN: admin, dbName: fmt.Sprintf("bench_%d", rows),
		noteBytes: noteBytes, results: map[string]any{},
	}
	b.members = generate(rows)
	b.ids = make([]string, len(b.members))
	for i, m := range b.members {
		b.ids[i] = m.id
	}
	if !reuse {
		_ = mgr.DropDatabase(ctx, b.dbName)
		if err := mgr.CreateDatabase(ctx, b.dbName); err != nil {
			return nil, fmt.Errorf("creating %s: %w", b.dbName, err)
		}
	}
	if b.driver, err = mgr.GetDriver(ctx, b.dbName); err != nil {
		return nil, err
	}
	b.schema = schema.NewService(b.driver)
	b.data = data.NewService(b.driver)
	if err := b.schema.EnsureSystemTables(ctx); err != nil {
		return nil, err
	}
	if reuse {
		tables, err := b.schema.ListTables(ctx)
		if err != nil {
			return nil, err
		}
		for i := range tables {
			if tables[i].Name == "members" {
				b.table = &tables[i]
			}
		}
		if b.table == nil {
			return nil, errors.New("-reuse: no members table")
		}
		return b, nil
	}
	start := time.Now()
	if b.table, err = b.createMembers(ctx, "Members", "members"); err != nil {
		return nil, err
	}
	if err := b.insertMembers(ctx, "members", b.members); err != nil {
		return nil, err
	}
	b.results["seed"] = map[string]any{"rows": rows, "seconds": round(time.Since(start).Seconds(), 1),
		"rows_per_s": int(float64(rows) / time.Since(start).Seconds())}
	return b, nil
}

func (b *bench) close() {
	if b.driver != nil {
		b.driver.Close()
	}
	b.mgr.Close()
}

// generate makes the same records on every engine: the order, the ids and
// the values come from one seeded generator.
func generate(n int) []member {
	src := rand.NewChaCha8([32]byte{seed})
	r := rand.New(src)
	out := make([]member, n)
	base := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range out {
		id, err := uuid.NewRandomFromReader(src)
		if err != nil {
			panic(err)
		}
		out[i] = member{
			id:       id.String(),
			name:     firstNames[r.IntN(len(firstNames))] + " " + lastNames[r.IntN(len(lastNames))],
			city:     cities[r.IntN(len(cities))],
			category: categories[r.IntN(len(categories))],
			fee:      fmt.Sprintf("%d.%02d", r.IntN(1000), r.IntN(100)),
			joined:   base.AddDate(0, 0, r.IntN(4000)).Format("2006-01-02"),
			index:    i,
		}
	}
	return out
}

// createMembers creates the table through the schema service, as Manage
// Database does: text, number and date fields, and two summary fields.
func (b *bench) createMembers(ctx context.Context, display, name string) (*schema.TableMetadata, error) {
	tbl, err := b.schema.CreateTable(ctx, display, name)
	if err != nil {
		return nil, err
	}
	fields := []struct {
		name string
		kind dbal.AgnosticFieldType
	}{
		{"name", dbal.FieldTypeText}, {"city", dbal.FieldTypeText}, {"category", dbal.FieldTypeText},
		{"fee", dbal.FieldTypeNumber}, {"joined", dbal.FieldTypeDate}, {"notes", dbal.FieldTypeText},
		{"counter", dbal.FieldTypeNumber}, {"code", dbal.FieldTypeText},
	}
	for _, f := range fields {
		if _, err := b.schema.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: f.name, DisplayName: f.name, FieldType: f.kind, IsNullable: true,
		}); err != nil {
			return nil, fmt.Errorf("adding %s: %w", f.name, err)
		}
	}
	for _, s := range []struct {
		name string
		spec data.SummarySpec
	}{
		{"total_fee", data.SummarySpec{Type: data.SummaryTotal, Field: "fee"}},
		{"member_count", data.SummarySpec{Type: data.SummaryCount, Field: "name"}},
	} {
		encoded, err := s.spec.Encode()
		if err != nil {
			return nil, err
		}
		if _, err := b.schema.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: s.name, DisplayName: s.name, FieldType: dbal.FieldTypeSummary, IsNullable: true,
			CalculationFormula: &encoded,
		}); err != nil {
			return nil, fmt.Errorf("adding %s: %w", s.name, err)
		}
	}
	return tbl, nil
}

var memberColumns = []string{"id", "name", "city", "category", "fee", "joined", "notes", "code"}

// insertMembers loads records with multi-row INSERTs, 5,000 records a
// transaction: seeding, not a measured File4Base path.
func (b *bench) insertMembers(ctx context.Context, table string, rows []member) error {
	d := b.driver.Dialect()
	quoted := make([]string, len(memberColumns))
	for i, c := range memberColumns {
		quoted[i] = d.QuoteIdentifier(c)
	}
	const perStatement, perTx = 500, 5000
	db := b.driver.DB()
	for start := 0; start < len(rows); start += perTx {
		end := min(start+perTx, len(rows))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for s := start; s < end; s += perStatement {
			e := min(s+perStatement, end)
			var q strings.Builder
			fmt.Fprintf(&q, "INSERT INTO %s (%s) VALUES ", d.QuoteIdentifier(table), strings.Join(quoted, ", "))
			args := make([]any, 0, (e-s)*len(memberColumns))
			for i := s; i < e; i++ {
				if i > s {
					q.WriteString(", ")
				}
				q.WriteByte('(')
				for c := range memberColumns {
					if c > 0 {
						q.WriteString(", ")
					}
					q.WriteString(d.Placeholder(len(args) + 1))
					args = append(args, rowValue(rows[i], c, b.noteBytes))
				}
				q.WriteByte(')')
			}
			if _, err := tx.ExecContext(ctx, q.String(), args...); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("seeding %s: %w", table, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func rowValue(m member, column, noteBytes int) any {
	switch memberColumns[column] {
	case "id":
		return m.id
	case "name":
		return m.name
	case "city":
		return m.city
	case "category":
		return m.category
	case "fee":
		return m.fee
	case "joined":
		return m.joined
	case "notes":
		return notes(m.index, noteBytes)
	case "code":
		return strconv.Itoa(m.index % 100000)
	}
	return nil
}

func (b *bench) serverVersion(ctx context.Context) string {
	q := "SELECT version()"
	if b.engine == dbal.EngineSQLite {
		q = "SELECT 'SQLite ' || sqlite_version()"
	}
	var v string
	if err := b.driver.DB().QueryRowContext(ctx, q).Scan(&v); err != nil {
		return "unknown: " + err.Error()
	}
	return v
}

// lockError reports whether an error is the engine refusing a write because
// another one holds what it needs: SQLite's BUSY, a lock timeout or a
// deadlock elsewhere. These are the errors that would reach a user.
func lockError(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code() & 0xff
		return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "40P01" || pe.Code == "40001" || pe.Code == "55P03" || pe.Code == "57014"
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1205 || me.Number == 1213
	}
	return strings.Contains(err.Error(), "database is locked")
}

// latencies collects durations from several goroutines.
type latencies struct {
	mu     sync.Mutex
	d      []time.Duration
	locks  int
	errs   int
	sample string
}

func (l *latencies) add(d time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if lockError(err) {
			l.locks++
		} else {
			l.errs++
		}
		if l.sample == "" {
			l.sample = err.Error()
		}
		return
	}
	l.d = append(l.d, d)
}

func (l *latencies) summary(elapsed time.Duration) map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := slices.Clone(l.d)
	slices.Sort(d)
	pct := func(p float64) float64 {
		if len(d) == 0 {
			return 0
		}
		i := int(math.Ceil(p*float64(len(d)))) - 1
		return ms(d[max(i, 0)])
	}
	out := map[string]any{
		"ops": len(d), "p50_ms": pct(0.50), "p99_ms": pct(0.99), "max_ms": pct(1),
		"lock_errors": l.locks, "other_errors": l.errs,
	}
	if elapsed > 0 {
		out["ops_per_s"] = int(float64(len(d)) / elapsed.Seconds())
	}
	if l.sample != "" {
		out["first_error"] = l.sample
	}
	return out
}

func ms(d time.Duration) float64 { return round(float64(d)/float64(time.Millisecond), 2) }

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// timed runs f n times and returns the median duration in milliseconds.
func timed(n int, f func() error) (float64, error) {
	var d []time.Duration
	for range n {
		start := time.Now()
		if err := f(); err != nil {
			return 0, err
		}
		d = append(d, time.Since(start))
	}
	slices.Sort(d)
	return ms(d[len(d)/2]), nil
}

// sqlitePath is the file of the bench database (SQLite).
func (b *bench) sqlitePath() string {
	return filepath.Join(b.adminDSN, b.dbName+dbal.SQLiteFileExtension)
}

// dockerExec runs a command in the engine's container.
func (b *bench) dockerExec(ctx context.Context, args ...string) ([]byte, error) {
	if b.container == "" {
		return nil, errors.New("-container is required for this scenario")
	}
	cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", b.container}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// exec runs a statement on the bench database.
func (b *bench) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return b.driver.DB().ExecContext(ctx, q, args...)
}
