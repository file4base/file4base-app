package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// editors starts clients that edit random records through the data
// service, as a user saving a record does, until ctx ends. A client waits
// think (on average, 0.5 to 1.5 times it) between two saves; with no think
// time the clients save as fast as they can, the worst case. wait returns
// when they have all stopped.
func (b *bench) editors(ctx context.Context, clients int, think time.Duration, lat *latencies) (wait func()) {
	var wg sync.WaitGroup
	for c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, uint64(c)))
			for n := 0; ctx.Err() == nil; n++ {
				id := b.ids[r.IntN(len(b.ids))]
				start := time.Now()
				_, err := b.data.UpdateRow(ctx, "members", id, map[string]any{
					"counter": float64(n), "notes": fmt.Sprintf("edited by client %d, edit %d", c, n),
				})
				lat.add(time.Since(start), err)
				if think > 0 {
					select {
					case <-time.After(think/2 + time.Duration(r.Int64N(int64(think)))):
					case <-ctx.Done():
					}
				}
			}
		}()
	}
	return wg.Wait
}

func (b *bench) editorsScenario(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	for _, run := range []struct {
		clients int
		think   time.Duration
	}{{1, 0}, {10, 0}, {50, 0}, {50, time.Second}} {
		lat := &latencies{}
		loadCtx, cancel := context.WithTimeout(ctx, b.duration)
		start := time.Now()
		b.editors(loadCtx, run.clients, run.think, lat)()
		cancel()
		name := fmt.Sprintf("clients_%d", run.clients)
		if run.think > 0 {
			name += "_think_" + run.think.String()
		}
		out[name] = lat.summary(time.Since(start))
	}
	return out, nil
}

// replaceChunked is Replace Field Contents over the first n records,
// committed every chunk records. File4Base has no server-side Replace yet;
// this is the statement sequence one would run.
func (b *bench) replaceChunked(ctx context.Context, field, value string, n, chunk int) map[string]any {
	d := b.driver.Dialect()
	q := fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s > %s AND %s <= %s",
		d.QuoteIdentifier("members"), d.QuoteIdentifier(field), d.Placeholder(1),
		d.QuoteIdentifier(dbal.CreationOrderColumn), d.Placeholder(2),
		d.QuoteIdentifier(dbal.CreationOrderColumn), d.Placeholder(3))
	lat := &latencies{}
	start := time.Now()
	updated := int64(0)
	for from := 0; from < n && ctx.Err() == nil; from += chunk {
		t := time.Now()
		res, err := b.exec(ctx, q, value, from, from+chunk)
		lat.add(time.Since(t), err)
		if err == nil {
			k, _ := res.RowsAffected()
			updated += k
		}
	}
	out := lat.summary(0)
	out["records"] = updated
	out["chunk"] = chunk
	out["seconds"] = round(time.Since(start).Seconds(), 2)
	return out
}

// importRows is n records as an import file would give them.
func (b *bench) importRows(n, offset int) *dataio.Table {
	t := &dataio.Table{Columns: []string{"Name", "City", "Category", "Fee", "Joined", "Notes"}}
	for i := range n {
		m := b.members[(offset+i)%len(b.members)]
		t.Rows = append(t.Rows, []string{m.name, m.city, m.category, m.fee, m.joined, "imported " + notes(m.index, b.noteBytes)})
	}
	return t
}

var importMappings = []data.ImportMapping{
	{Column: 0, Field: "name"}, {Column: 1, Field: "city"}, {Column: 2, Field: "category"},
	{Column: 3, Field: "fee"}, {Column: 4, Field: "joined"}, {Column: 5, Field: "notes"},
}

// importBatches imports n records into table in calls of batch records
// (batch = n: one transaction, as an import is today).
func (b *bench) importBatches(ctx context.Context, table string, n, batch int) map[string]any {
	lat := &latencies{}
	start := time.Now()
	added := 0
	for from := 0; from < n && ctx.Err() == nil; from += batch {
		t := time.Now()
		rep, err := b.data.ImportRecords(ctx, table, b.importRows(min(batch, n-from), from),
			data.ImportOptions{Action: data.ImportAdd, Mappings: importMappings})
		lat.add(time.Since(t), err)
		if err == nil {
			added += rep.Added
		}
	}
	elapsed := time.Since(start)
	out := lat.summary(0)
	out["records"] = added
	out["batch"] = batch
	out["seconds"] = round(elapsed.Seconds(), 2)
	out["records_per_s"] = int(float64(added) / elapsed.Seconds())
	return out
}

// combined is gate 1: 50 editors while a Replace over 100,000 records and an
// import of 20,000 run: with the import in one transaction (as it is today)
// or in 2,000-record commits, and editors saving as fast as they can or
// about once a second.
func (b *bench) combined(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	for _, phase := range []struct {
		name  string
		batch int
		think time.Duration
	}{
		{"atomic_import", 20000, 0}, {"chunked_import", 2000, 0},
		{"atomic_import_think_1s", 20000, time.Second}, {"atomic_import_100k_think_1s", 100000, time.Second},
	} {
		loadCtx, cancel := context.WithCancel(ctx)
		lat := &latencies{}
		start := time.Now()
		wait := b.editors(loadCtx, 50, phase.think, lat)
		var replace, imp map[string]any
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); replace = b.replaceChunked(ctx, "category", "Remplacé", 100000, 1000) }()
		go func() { defer wg.Done(); imp = b.importBatches(ctx, "members", max(20000, phase.batch), phase.batch) }()
		wg.Wait()
		if rest := b.duration - time.Since(start); rest > 0 {
			select {
			case <-time.After(rest):
			case <-ctx.Done():
			}
		}
		cancel()
		wait()
		out[phase.name] = map[string]any{"editors_50": lat.summary(time.Since(start)), "replace_100k": replace, "import": imp}
	}
	return out, nil
}

// finds times finds, sorts and reports on the members table (median of 3).
func (b *bench) finds(ctx context.Context) (map[string]any, error) {
	like := func(field, v string) []data.FindRequest {
		return []data.FindRequest{{Criteria: []data.FindCriterion{{FieldName: field, Operator: "LIKE", Value: v}}}}
	}
	ops := []struct {
		name string
		f    func() error
	}{
		{"find_contains_first_page", func() error {
			_, err := b.data.ExecuteFind(ctx, "members", like("name", "%strasse%"), data.QueryOptions{Limit: 100})
			return err
		}},
		{"find_equals_sorted_first_page", func() error {
			_, err := b.data.ExecuteFind(ctx, "members", []data.FindRequest{{Criteria: []data.FindCriterion{
				{FieldName: "city", Operator: "=", Value: "köln"}}}},
				data.QueryOptions{Limit: 100, Sort: []data.SortField{{Field: "name"}}})
			return err
		}},
		{"find_number_range_first_page", func() error {
			_, err := b.data.ExecuteFind(ctx, "members", []data.FindRequest{{Criteria: []data.FindCriterion{
				{FieldName: "fee", Operator: "RANGE", Value: "100", ValueTo: "200"}}}}, data.QueryOptions{Limit: 100})
			return err
		}},
		{"sort_all_by_name_first_page", func() error {
			_, err := b.data.ListRows(ctx, "members", data.QueryOptions{Limit: 100, Sort: []data.SortField{{Field: "name"}, {Field: "city"}}})
			return err
		}},
		{"report_grand_totals", func() error {
			_, err := b.data.Summarize(ctx, "members", data.SummaryRequest{})
			return err
		}},
		{"report_by_city", func() error {
			_, err := b.data.Summarize(ctx, "members", data.SummaryRequest{GroupBy: []string{"city"}})
			return err
		}},
		{"report_find_by_category", func() error {
			_, err := b.data.Summarize(ctx, "members", data.SummaryRequest{Requests: like("name", "%müller%"), GroupBy: []string{"category"}})
			return err
		}},
		{"get_100_records", func() error {
			for i := range 100 {
				if _, err := b.data.GetRow(ctx, "members", b.ids[(i*7919)%len(b.ids)]); err != nil {
					return err
				}
			}
			return nil
		}},
	}
	out := map[string]any{}
	for _, op := range ops {
		v, err := timed(3, op.f)
		if err != nil {
			return out, fmt.Errorf("%s: %w", op.name, err)
		}
		out[op.name+"_ms"] = v
	}
	return out, nil
}

// importThroughput imports 50,000 records into an empty table in one call.
func (b *bench) importThroughput(ctx context.Context) (map[string]any, error) {
	if _, err := b.createMembers(ctx, "Imports", "imports"); err != nil {
		return nil, err
	}
	return b.importBatches(ctx, "imports", 50000, 50000), nil
}

// typeChange is gate 3: the code field (text holding numbers) becomes a
// number field on every record, while 5 editors keep saving records.
func (b *bench) typeChange(ctx context.Context) (map[string]any, error) {
	stmt, err := b.driver.Dialect().BuildAlterColumnTypeSQL("members", "code", dbal.FieldTypeNumber)
	if err != nil {
		return nil, err
	}
	loadCtx, cancel := context.WithCancel(ctx)
	lat := &latencies{}
	wait := b.editors(loadCtx, 5, 0, lat)
	time.Sleep(time.Second)
	start := time.Now()
	_, ddlErr := b.exec(ctx, stmt)
	ddl := time.Since(start)
	time.Sleep(time.Second)
	cancel()
	wait()
	out := map[string]any{"ddl_seconds": round(ddl.Seconds(), 2), "editors_5": lat.summary(ddl + 2*time.Second)}
	if ddlErr != nil {
		out["ddl_error"] = ddlErr.Error()
		return out, nil
	}
	var numbers int
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s >= 0", b.driver.Dialect().QuoteIdentifier("members"), b.driver.Dialect().QuoteIdentifier("code"))
	if err := b.driver.DB().QueryRowContext(ctx, q).Scan(&numbers); err != nil {
		return out, err
	}
	out["records_converted"] = numbers
	return out, nil
}

// storage is the size the solution takes.
func (b *bench) storage(ctx context.Context) (map[string]any, error) {
	db := b.driver.DB()
	switch b.engine {
	case dbal.EngineSQLite:
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return nil, err
		}
		st, err := os.Stat(b.sqlitePath())
		if err != nil {
			return nil, err
		}
		return map[string]any{"bytes": st.Size(), "mb": mb(st.Size())}, nil
	case dbal.EnginePostgres:
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&n); err != nil {
			return nil, err
		}
		return map[string]any{"bytes": n, "mb": mb(n)}, nil
	default:
		out, err := b.dockerExec(ctx, "du", "-sb", "/var/lib/mysql/"+b.dbName)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.Fields(string(out))[0], 10, 64)
		if err != nil {
			return nil, err
		}
		return map[string]any{"bytes": n, "mb": mb(n), "note": "du of the database directory (file-per-table)"}, nil
	}
}

func mb(n int64) float64 { return round(float64(n)/(1<<20), 1) }

// backupRestore backs the solution up and restores it into a new database,
// then compares the restored data with the original.
func (b *bench) backupRestore(ctx context.Context) (map[string]any, error) {
	const restored = "bench_restored"
	out := map[string]any{}
	before, err := dataFingerprint(ctx, b.driver.DB(), b.driver.Dialect())
	if err != nil {
		return nil, err
	}
	switch b.engine {
	case dbal.EngineSQLite:
		dir := b.backupDir
		if dir == "" {
			dir = b.adminDSN
		}
		dest := filepath.Join(dir, "bench.backup")
		_ = os.Remove(dest)
		start := time.Now()
		if err := dbal.BackupSQLite(ctx, b.driver.DB(), dest); err != nil {
			return nil, err
		}
		out["backup_seconds"] = round(time.Since(start).Seconds(), 2)
		st, _ := os.Stat(dest)
		out["backup_mb"] = mb(st.Size())
		if b.noRestore {
			_ = os.Remove(dest)
			return out, nil
		}
		target := filepath.Join(b.adminDSN, restored+dbal.SQLiteFileExtension)
		_ = b.mgr.DropDatabase(ctx, restored)
		start = time.Now()
		if err := dbal.RestoreSQLiteDatabase(ctx, dest, target); err != nil {
			return nil, err
		}
		out["restore_seconds"] = round(time.Since(start).Seconds(), 2)
		_ = os.Remove(dest)
	case dbal.EnginePostgres:
		user := pgUser(b.adminDSN)
		start := time.Now()
		if _, err := b.dockerExec(ctx, "pg_dump", "-U", user, "-Fc", "-f", "/tmp/bench.dump", b.dbName); err != nil {
			return nil, err
		}
		out["backup_seconds"] = round(time.Since(start).Seconds(), 2)
		if size, err := b.dockerExec(ctx, "stat", "-c", "%s", "/tmp/bench.dump"); err == nil {
			n, _ := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
			out["backup_mb"] = mb(n)
		}
		if b.noRestore {
			_, _ = b.dockerExec(ctx, "rm", "-f", "/tmp/bench.dump")
			return out, nil
		}
		_ = b.mgr.DropDatabase(ctx, restored)
		start = time.Now()
		if _, err := b.dockerExec(ctx, "createdb", "-U", user, restored); err != nil {
			return nil, err
		}
		if _, err := b.dockerExec(ctx, "pg_restore", "-U", user, "-d", restored, "/tmp/bench.dump"); err != nil {
			return nil, err
		}
		out["restore_seconds"] = round(time.Since(start).Seconds(), 2)
		_, _ = b.dockerExec(ctx, "rm", "-f", "/tmp/bench.dump")
	default:
		user, pass := mariaCredentials(b.adminDSN)
		login := fmt.Sprintf("-u%s -p%s", user, pass)
		start := time.Now()
		if _, err := b.dockerExec(ctx, "sh", "-c", fmt.Sprintf("mariadb-dump %s --single-transaction %s > /tmp/bench.sql", login, b.dbName)); err != nil {
			return nil, err
		}
		out["backup_seconds"] = round(time.Since(start).Seconds(), 2)
		if size, err := b.dockerExec(ctx, "stat", "-c", "%s", "/tmp/bench.sql"); err == nil {
			n, _ := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
			out["backup_mb"] = mb(n)
		}
		_ = b.mgr.DropDatabase(ctx, restored)
		start = time.Now()
		if _, err := b.dockerExec(ctx, "sh", "-c", fmt.Sprintf("mariadb %s -e 'CREATE DATABASE %s' && mariadb %s %s < /tmp/bench.sql", login, restored, login, restored)); err != nil {
			return nil, err
		}
		out["restore_seconds"] = round(time.Since(start).Seconds(), 2)
		_, _ = b.dockerExec(ctx, "rm", "-f", "/tmp/bench.sql")
	}
	drv, err := b.mgr.GetDriver(ctx, restored)
	if err != nil {
		return out, err
	}
	after, err := dataFingerprint(ctx, drv.DB(), drv.Dialect())
	if err != nil {
		return out, err
	}
	out["fingerprint"] = before
	out["restored_fingerprint"] = after
	out["restored_matches"] = before == after
	return out, nil
}

// dataFingerprint is an order-independent digest of every record of the
// members table and every system table row count: the same data gives the
// same digest whatever order the engine returns it in.
func dataFingerprint(ctx context.Context, db *sql.DB, d dbal.Dialect) (string, error) {
	cols := []string{"id", "name", "city", "category", "fee", "joined", "notes", "counter", "code"}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = d.QuoteIdentifier(c)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s", strings.Join(quoted, ", "), d.QuoteIdentifier("members")))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var sum uint64
	n := 0
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		h := sha256.New()
		for _, v := range vals {
			switch t := v.(type) {
			case []byte:
				h.Write(t)
			case time.Time:
				h.Write([]byte(t.UTC().Format(time.RFC3339Nano)))
			default:
				fmt.Fprint(h, t)
			}
			h.Write([]byte{0})
		}
		sum += binary.BigEndian.Uint64(h.Sum(nil))
		n++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%016x", n, sum), nil
}

// fingerprints is gate 2: finds, sorts and a report over non-ASCII text, on
// a 20,000-record table, compared with references worked out in Go: the
// find semantics File4Base defines (Unicode lower case, substring, accents
// significant), the order of calc.CompareText (a sort in a calculation,
// the in-memory sort, FileMaker's order) and plain byte order.
func (b *bench) fingerprints(ctx context.Context) (map[string]any, error) {
	const n = 20000
	if _, err := b.createMembers(ctx, "Texts", "texts"); err != nil {
		return nil, err
	}
	sample := b.members[:min(n, len(b.members))]
	if err := b.insertMembers(ctx, "texts", sample); err != nil {
		return nil, err
	}
	out := map[string]any{}
	lower := func(s string) string { return strings.Map(unicode.ToLower, s) }
	type find struct {
		name     string
		requests []data.FindRequest
		match    func(m member) bool
	}
	crit := func(field, op, v string) data.FindCriterion {
		return data.FindCriterion{FieldName: field, Operator: op, Value: v}
	}
	value := func(m member, field string) string {
		switch field {
		case "name":
			return m.name
		case "city":
			return m.city
		}
		return m.category
	}
	contains := func(field, v string) (data.FindRequest, func(member) bool) {
		return data.FindRequest{Criteria: []data.FindCriterion{crit(field, "LIKE", "%"+v+"%")}},
			func(m member) bool { return strings.Contains(lower(value(m, field)), lower(v)) }
	}
	equals := func(field, v string) (data.FindRequest, func(member) bool) {
		return data.FindRequest{Criteria: []data.FindCriterion{crit(field, "=", v)}},
			func(m member) bool { return lower(value(m, field)) == lower(v) }
	}
	one := func(name string, r data.FindRequest, f func(member) bool) find {
		return find{name, []data.FindRequest{r}, f}
	}
	var finds []find
	for _, c := range []struct{ name, field, v string }{
		{"name_contains_élodie", "name", "élodie"}, {"name_contains_STRASSE", "name", "STRASSE"},
		{"name_contains_straße", "name", "straße"}, {"name_contains_yılmaz", "name", "yılmaz"},
		{"name_contains_YILMAZ", "name", "YILMAZ"}, {"name_contains_dotless_ı", "name", "ı"},
		{"name_contains_İNCE", "name", "İNCE"}, {"city_contains_ö", "city", "ö"},
	} {
		r, f := contains(c.field, c.v)
		finds = append(finds, one(c.name, r, f))
	}
	for _, c := range []struct{ name, field, v string }{
		{"city_equals_istanbul", "city", "istanbul"}, {"city_equals_İSTANBUL", "city", "İSTANBUL"},
		{"city_equals_ISTANBUL", "city", "ISTANBUL"}, {"category_equals_éco", "category", "éco"},
	} {
		r, f := equals(c.field, c.v)
		finds = append(finds, one(c.name, r, f))
	}
	{
		r1, f1 := equals("city", "paris")
		r2, f2 := contains("name", "zoë")
		finds = append(finds, find{"two_requests_paris_or_zoë", []data.FindRequest{r1, r2},
			func(m member) bool { return f1(m) || f2(m) }})
		r3, f3 := contains("city", "z")
		r4, f4 := contains("name", "e")
		r4.Omit = true
		finds = append(finds, find{"omit_city_z_without_e", []data.FindRequest{r3, r4},
			func(m member) bool { return f3(m) && !f4(m) }})
	}
	allMatch := true
	for _, f := range finds {
		ids, err := b.findIDs(ctx, f.requests)
		if err != nil {
			return out, fmt.Errorf("%s: %w", f.name, err)
		}
		var want []string
		for _, m := range sample {
			if f.match(m) {
				want = append(want, m.id)
			}
		}
		slices.Sort(ids)
		slices.Sort(want)
		got, ref := digest(ids), digest(want)
		out["find_"+f.name] = map[string]any{"count": len(ids), "fingerprint": got, "reference_count": len(want), "matches_reference": got == ref}
		allMatch = allMatch && got == ref
	}
	out["finds_match_reference"] = allMatch

	// Sort by name, then id: the name and id sequence.
	seq, err := b.sortedSequence(ctx)
	if err != nil {
		return out, err
	}
	byCalc := slices.Clone(sample)
	slices.SortStableFunc(byCalc, func(x, y member) int {
		if c := calc.CompareText(x.name, y.name); c != 0 {
			return c
		}
		return strings.Compare(x.id, y.id)
	})
	byBytes := slices.Clone(sample)
	slices.SortStableFunc(byBytes, func(x, y member) int {
		if c := strings.Compare(x.name, y.name); c != 0 {
			return c
		}
		return strings.Compare(x.id, y.id)
	})
	seqOf := func(ms []member) []string {
		s := make([]string, len(ms))
		for i, m := range ms {
			s[i] = m.name + "\x00" + m.id
		}
		return s
	}
	got := digest(seq)
	out["sort_by_name"] = map[string]any{
		"fingerprint": got, "matches_calc_compare_text": got == digest(seqOf(byCalc)),
		"matches_byte_order": got == digest(seqOf(byBytes)), "first_names": firstNamesOf(seq, 12),
	}

	// A report: count and total fee by city.
	sum, err := b.data.Summarize(ctx, "texts", data.SummaryRequest{GroupBy: []string{"city"}})
	if err != nil {
		return out, err
	}
	var lines []string
	for _, g := range sum.Groups {
		lines = append(lines, fmt.Sprintf("%v|%d|%s", g.Values["city"], g.Count, money(g.Summaries["total_fee"])))
	}
	slices.Sort(lines)
	refGroups := map[string][2]int64{}
	for _, m := range sample {
		cents, _ := strconv.ParseInt(strings.ReplaceAll(m.fee, ".", ""), 10, 64)
		g := refGroups[m.city]
		refGroups[m.city] = [2]int64{g[0] + 1, g[1] + cents}
	}
	var refLines []string
	for city, g := range refGroups {
		refLines = append(refLines, fmt.Sprintf("%s|%d|%d.%02d", city, g[0], g[1]/100, g[1]%100))
	}
	slices.Sort(refLines)
	out["report_by_city"] = map[string]any{
		"groups": len(lines), "reference_groups": len(refLines), "fingerprint": digest(lines),
		"matches_reference": digest(lines) == digest(refLines), "grand_total": money(sum.Grand["total_fee"]),
	}
	return out, nil
}

// findIDs is every record a find returns, page by page in id order.
func (b *bench) findIDs(ctx context.Context, requests []data.FindRequest) ([]string, error) {
	var ids []string
	for offset := 0; ; offset += data.MaxPageSize {
		rows, err := b.data.ExecuteFind(ctx, "texts", requests, data.QueryOptions{
			Limit: data.MaxPageSize, Offset: offset, Sort: []data.SortField{{Field: "id"}},
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			ids = append(ids, fmt.Sprint(r["id"]))
		}
		if len(rows) < data.MaxPageSize {
			return ids, nil
		}
	}
}

func (b *bench) sortedSequence(ctx context.Context) ([]string, error) {
	var seq []string
	for offset := 0; ; offset += data.MaxPageSize {
		rows, err := b.data.ListRows(ctx, "texts", data.QueryOptions{
			Limit: data.MaxPageSize, Offset: offset, Sort: []data.SortField{{Field: "name"}, {Field: "id"}},
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			seq = append(seq, fmt.Sprint(r["name"])+"\x00"+fmt.Sprint(r["id"]))
		}
		if len(rows) < data.MaxPageSize {
			return seq, nil
		}
	}
}

func firstNamesOf(seq []string, n int) []string {
	var out []string
	for _, s := range seq {
		name, _, _ := strings.Cut(s, "\x00")
		if len(out) == 0 || out[len(out)-1] != name {
			out = append(out, name)
		}
		if len(out) == n {
			break
		}
	}
	return out
}

func money(v any) string {
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	if err != nil {
		return fmt.Sprint(v)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func digest(lines []string) string {
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
