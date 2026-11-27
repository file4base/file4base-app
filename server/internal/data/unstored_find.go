package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Finding, sorting and summarizing by unstored calculations (#149).
//
// An unstored calculation holds nothing in its column: it is computed when a
// record is read. FileMaker still finds, sorts and summarizes by one, by
// computing it for every record it has to look at, and so does File4Base:
//
//  1. The criteria the engine can judge run first. A request is kept without
//     its criteria on unstored calculations, an omitting request that has one
//     is left out, and a request left with no criterion takes in every
//     record, so the candidates are every record the find may return.
//  2. The unstored calculations the find, the sort or the summary names, and
//     those they read, are computed for the candidates, a page at a time, in
//     creation order, by the evaluator every read uses (relatedReader.fill),
//     the related records of a page read together.
//  3. The values computed are bound to the statement as one parameter, a
//     table of the record's creation order and the values (unstoredBinding),
//     and the find, the sort and the summary run as they do on stored fields,
//     reading the calculation there instead of in its empty column. Every
//     operator, omitting request, sort direction, empty value and aggregate
//     works as it does on a stored field of that result type, on each engine.
//
// It is bounded by UnstoredFindLimits: past the most records, the time or
// the size of the values, the request is refused with ErrTooManyToCompute,
// asking for a criterion on a stored field that narrows the candidates.
//
// The calculations are computed as the caller: a related table the caller
// may not read is <No Access> to the formula, as on any read, so a find
// cannot tell more than the records' values show.

// UnstoredFindLimits bound what one find, sort or summary computes of
// unstored calculations.
type UnstoredFindLimits struct {
	// Records is the most candidate records whose calculations are computed.
	Records int
	// Duration is the longest computing them may take.
	Duration time.Duration
	// Bytes is the most bytes of computed values bound to one statement. On
	// MariaDB, it stays below the server's max_allowed_packet.
	Bytes int
}

// DefaultUnstoredFindLimits are the limits unless SetUnstoredFindLimits
// changes them.
var DefaultUnstoredFindLimits = UnstoredFindLimits{Records: 50000, Duration: 30 * time.Second, Bytes: 16 << 20}

var unstoredFindLimits atomic.Pointer[UnstoredFindLimits]

// SetUnstoredFindLimits changes the limits of every service; a limit not
// above zero keeps its default.
func SetUnstoredFindLimits(l UnstoredFindLimits) {
	if l.Records <= 0 {
		l.Records = DefaultUnstoredFindLimits.Records
	}
	if l.Duration <= 0 {
		l.Duration = DefaultUnstoredFindLimits.Duration
	}
	if l.Bytes <= 0 {
		l.Bytes = DefaultUnstoredFindLimits.Bytes
	}
	unstoredFindLimits.Store(&l)
}

func currentUnstoredFindLimits() UnstoredFindLimits {
	if l := unstoredFindLimits.Load(); l != nil {
		return *l
	}
	return DefaultUnstoredFindLimits
}

// ErrTooManyToCompute is returned for a find, sort or summary by an unstored
// calculation that would compute it for more records, for longer or into
// more data than UnstoredFindLimits allow.
var ErrTooManyToCompute = errors.New("too many records to compute")

// unstoredPage is how many candidates are computed at a time.
const unstoredPage = 500

// boundTable is the name of the table of computed values in a statement.
const boundTable = "f4b_u"

// unstoredBinding is the values of the unstored calculations of a table
// computed for one statement: cte defines the table of values from the
// statement's first parameter, value; join joins it to the table by the
// records' creation order; columns reads each calculation there.
type unstoredBinding struct {
	cte, join string
	value     interface{}
	columns   map[string]string
	quote     func(string) string
}

// column reads a field: an unstored calculation computed for the statement,
// any other field by its column.
func (b *unstoredBinding) column(field string) string {
	if c, ok := b.columns[field]; ok {
		return c
	}
	return b.quote(field)
}

// findClauses resolves what a statement on tableName needs to read the
// records the requests find: the relationships their criteria reach through,
// and the values of the unstored calculations they, or extra (the fields the
// statement sorts or summarizes by), name, computed first. what names the
// statement in a refusal: "sort" or "summary".
func (s *Service) findClauses(
	ctx context.Context, tableName string, fields fieldSet, requests []FindRequest, extra []string, what string,
) (findClauses, error) {
	related, err := s.resolveFindRelations(ctx, tableName, requests)
	if err != nil {
		return findClauses{}, err
	}
	if err := s.refuseUnstoredCriteria(ctx, requests, related); err != nil {
		return findClauses{}, err
	}
	bound, err := s.bindUnstored(ctx, tableName, fields, requests, related, extra, what)
	if err != nil {
		return findClauses{}, err
	}
	return buildBoundFindClauses(s.driver.Dialect(), fields, tableName, requests, related, bound)
}

// bindUnstored computes the unstored calculations of tableName the criteria
// or extra name, for every record the requests may find, and binds them. It
// is nil when they name none.
func (s *Service) bindUnstored(
	ctx context.Context, tableName string, fields fieldSet, requests []FindRequest,
	related map[string]relatedFindTarget, extra []string, what string,
) (*unstoredBinding, error) {
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return nil, err
	}
	unstored := map[string]bool{}
	for _, c := range calculations {
		if c.unstored {
			unstored[c.field] = true
		}
	}
	if len(unstored) == 0 {
		return nil, nil
	}
	var want []string
	add := func(field string) {
		if unstored[field] && !slices.Contains(want, field) {
			want = append(want, field)
		}
	}
	for _, req := range requests {
		for _, crit := range req.Criteria {
			if crit.FieldName != "" && !crit.isRelated() {
				if unstored[crit.FieldName] {
					what = "find"
				}
				add(crit.FieldName)
			}
		}
	}
	for _, field := range extra {
		add(field)
	}
	if len(want) == 0 {
		return nil, nil
	}

	c := &unstoredCompute{s: s, table: tableName, fields: fields, want: want, what: what,
		limits: currentUnstoredFindLimits(), started: time.Now()}
	if c.find, err = buildFindClauses(s.driver.Dialect(), fields, tableName, candidateRequests(requests, unstored), related); err != nil {
		return nil, err
	}
	if c.seq, err = s.creationOrder(ctx, tableName); err != nil {
		return nil, err
	}
	if err := c.count(ctx); err != nil {
		return nil, err
	}
	if err := c.compute(ctx, calculations); err != nil {
		return nil, err
	}
	return c.binding(), nil
}

// candidateRequests are the requests the engine can judge, for the records
// that may meet requests once their criteria on unstored calculations are
// judged too: each request without them, an omitting request that has one
// left out, and no including request at all when one is left with no
// criterion, since it may take in any record.
func candidateRequests(requests []FindRequest, unstored map[string]bool) []FindRequest {
	var out []FindRequest
	everyRecord := false
	for _, req := range requests {
		kept := make([]FindCriterion, 0, len(req.Criteria))
		judged := false
		for _, crit := range req.Criteria {
			if crit.FieldName == "" {
				continue
			}
			if !crit.isRelated() && unstored[crit.FieldName] {
				judged = true
				continue
			}
			kept = append(kept, crit)
		}
		switch {
		case !judged:
			out = append(out, req)
		case req.Omit:
			// Which records it omits is known once it is computed.
		case len(kept) == 0:
			everyRecord = true
		default:
			out = append(out, FindRequest{Criteria: kept})
		}
	}
	if everyRecord {
		out = slices.DeleteFunc(out, func(req FindRequest) bool { return !req.Omit })
	}
	return out
}

// unstoredCompute computes the unstored calculations want names for the
// candidates find selects.
type unstoredCompute struct {
	s       *Service
	table   string
	fields  fieldSet
	want    []string
	what    string
	limits  UnstoredFindLimits
	started time.Time
	find    findClauses
	// seq is the column the candidates are read in the order of, and the
	// values are joined to the table by: the creation order, else the id.
	seq string
	// total is how many candidates there are; done, how many are computed.
	total, done int
	// rows is the JSON array of [seq, value…] of every candidate.
	rows bytes.Buffer
}

// count counts the candidates, and refuses more than the limit allows.
func (c *unstoredCompute) count(ctx context.Context) error {
	dialect := c.s.driver.Dialect()
	q := fmt.Sprintf("%sSELECT COUNT(*) FROM %s %s", c.find.with, dialect.QuoteIdentifier(c.table), c.find.where)
	if err := c.s.driver.DB().QueryRowContext(ctx, q, c.find.values...).Scan(&c.total); err != nil {
		return fmt.Errorf("failed counting the records of %s to compute %s for: %w", c.table, c.names(), err)
	}
	if c.total > c.limits.Records {
		return c.refuse(fmt.Sprintf("for %d records of %s, more than the %d it computes at most", c.total, c.table, c.limits.Records))
	}
	return nil
}

// refuse is the error of a computation past a limit.
func (c *unstoredCompute) refuse(why string) error {
	return fmt.Errorf("%w: this %s needs %s computed %s; narrow it with a criterion on a stored field first",
		ErrTooManyToCompute, c.what, c.names(), why)
}

// names are the calculations computed, as a refusal names them.
func (c *unstoredCompute) names() string {
	quoted := make([]string, len(c.want))
	for i, f := range c.want {
		quoted[i] = strconv.Quote(f)
	}
	if len(quoted) == 1 {
		return quoted[0] + ", an unstored calculation,"
	}
	return strings.Join(quoted, ", ") + ", unstored calculations,"
}

// compute reads the candidates a page at a time, in the order of seq, and
// computes the calculations for each page, its related records read
// together. What a page computed is kept as its values only.
func (c *unstoredCompute) compute(ctx context.Context, calculations []calculation) error {
	maxBytes, err := c.maxBytes(ctx)
	if err != nil {
		return err
	}
	dialect := c.s.driver.Dialect()
	quoted := dialect.QuoteIdentifier(c.table)
	seqColumn := quoted + "." + dialect.QuoteIdentifier(c.seq)
	read := pageFields(c.fields, neededCalculations(calculations, c.want))
	list := selectList(dialect, read)
	const seqKey = "f4b$seq"
	if c.seq != "id" {
		list += ", " + seqColumn + " AS " + dialect.QuoteIdentifier(seqKey)
	}
	reader := c.s.newRelatedReader()
	encoder := json.NewEncoder(&c.rows)
	encoder.SetEscapeHTML(false)
	c.rows.WriteByte('[')
	var last interface{}
	for {
		where, args := c.find.where, slices.Clone(c.find.values)
		if last != nil {
			after := fmt.Sprintf("%s > %s", seqColumn, dialect.Placeholder(len(args)+1))
			if where == "" {
				where = "WHERE " + after
			} else {
				where += " AND " + after
			}
			args = append(args, last)
		}
		q := fmt.Sprintf("%sSELECT %s FROM %s %s ORDER BY %s LIMIT %d", c.find.with, list, quoted, where, seqColumn, unstoredPage)
		rows, err := c.s.driver.DB().QueryContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("failed reading the records of %s to compute %s for: %w", c.table, c.names(), err)
		}
		records, err := rowsToMaps(rows, read)
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if len(records) == 0 {
			break
		}
		seqs := make([]interface{}, len(records))
		for i, r := range records {
			if c.seq == "id" {
				seqs[i] = r["id"]
				continue
			}
			seqs[i] = sequenceNumber(r[seqKey])
			delete(r, seqKey)
		}
		if err := reader.fill(ctx, c.table, records, c.want); err != nil {
			return err
		}
		reader.forgetRecords()
		for i, r := range records {
			row := make([]interface{}, 0, len(c.want)+1)
			row = append(row, seqs[i])
			for _, field := range c.want {
				row = append(row, boundValue(r[field], c.fields[field], dialect.Engine()))
			}
			if c.done > 0 {
				c.rows.WriteByte(',')
			}
			if err := encoder.Encode(row); err != nil {
				return err
			}
			c.done++
		}
		last = seqs[len(seqs)-1]
		switch {
		case c.done > c.limits.Records:
			// Records added since they were counted.
			return c.refuse(fmt.Sprintf("for more than %d records of %s, the most it computes", c.limits.Records, c.table))
		case c.rows.Len() > maxBytes:
			return c.refuse(fmt.Sprintf("for %d records of %s, into more than %d bytes of values, the most it binds", c.total, c.table, maxBytes))
		case time.Since(c.started) > c.limits.Duration:
			return c.refuse(fmt.Sprintf("for %d records of %s, and had computed it for %d after %s, the longest it takes",
				c.total, c.table, c.done, c.limits.Duration))
		}
		if len(records) < unstoredPage {
			break
		}
	}
	c.rows.WriteByte(']')
	return nil
}

// maxBytes is the most bytes of values one statement binds: the limit, and
// on MariaDB, below what one packet to the server may hold.
func (c *unstoredCompute) maxBytes(ctx context.Context) (int, error) {
	limit := c.limits.Bytes
	if c.s.driver.Dialect().Engine() != dbal.EngineMariaDB {
		return limit, nil
	}
	var packet int64
	if err := c.s.driver.DB().QueryRowContext(ctx, "SELECT @@max_allowed_packet").Scan(&packet); err != nil {
		return 0, fmt.Errorf("failed reading max_allowed_packet: %w", err)
	}
	// The statement and the other values take the rest of the packet.
	return int(min(int64(limit), packet-packet/8)), nil
}

// pageFields are the fields a page of candidates is read with: every field,
// but a container that no calculation computed reads, whose data would only
// take room.
func pageFields(fields fieldSet, needed map[string]bool) fieldSet {
	out := make(fieldSet, len(fields))
	for key, kind := range fields {
		base := strings.TrimSuffix(key, "[]")
		if fields[base] == dbal.FieldTypeContainer && !needed[base] {
			continue
		}
		out[key] = kind
	}
	return out
}

// sequenceNumber reads a creation order number, which a driver may return as
// text.
func sequenceNumber(v interface{}) interface{} {
	switch t := v.(type) {
	case []byte:
		v = string(t)
	}
	if text, ok := v.(string); ok {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n
		}
	}
	return v
}

var numericText = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// boundValue is how a computed value of a field of type kind is bound: as
// the text its column type reads, nil for an empty value or one the type
// cannot hold (FileMaker's "?"), which finds as an empty field does.
func boundValue(v interface{}, kind dbal.AgnosticFieldType, engine dbal.EngineType) interface{} {
	v = textOf(v)
	if v == nil {
		return nil
	}
	switch kind {
	case dbal.FieldTypeNumber:
		text := strings.TrimSpace(fmt.Sprint(v))
		if !numericText.MatchString(text) {
			return nil
		}
		return text
	case dbal.FieldTypeDate:
		switch t := v.(type) {
		case time.Time:
			return t.Format("2006-01-02")
		case string:
			if at, err := time.Parse("2006-01-02", strings.TrimSpace(t)); err == nil {
				return at.Format("2006-01-02")
			}
		}
		return nil
	case dbal.FieldTypeTimestamp:
		at, ok := v.(time.Time)
		if !ok {
			text, isText := v.(string)
			if !isText {
				return nil
			}
			parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
			if err != nil {
				return nil
			}
			at = parsed
		}
		if engine == dbal.EngineMariaDB {
			return dbal.TemporalValue(engine, kind, at)
		}
		return at.UTC().Format(time.RFC3339Nano)
	case dbal.FieldTypeBoolean:
		b := dbal.NormalizeBool(v)
		on, ok := b.(bool)
		switch {
		case !ok:
			return nil
		case engine == dbal.EngineMariaDB && on:
			return 1
		case engine == dbal.EngineMariaDB:
			return 0
		}
		return on
	}
	return fmt.Sprint(v)
}

// binding is the table of the values computed, for the statement to read.
func (c *unstoredCompute) binding() *unstoredBinding {
	dialect := c.s.driver.Dialect()
	quote := dialect.QuoteIdentifier
	b := &unstoredBinding{value: c.rows.String(), columns: map[string]string{}, quote: quote}
	seqName := quote("f4b$seq")
	names := []string{seqName}
	for i, field := range c.want {
		name := quote(fmt.Sprintf("f4b$u%d", i))
		names = append(names, name)
		b.columns[field] = boundTable + "." + name
	}
	if dialect.Engine() == dbal.EngineMariaDB {
		columns := make([]string, len(names))
		for i, name := range names {
			columns[i] = fmt.Sprintf("%s %s PATH '$[%d]'", name, c.mariaType(i), i)
		}
		b.cte = fmt.Sprintf("%s AS (SELECT * FROM JSON_TABLE(%s, '$[*]' COLUMNS (%s)) AS f4b_e)",
			boundTable, dialect.Placeholder(1), strings.Join(columns, ", "))
	} else {
		reads := make([]string, len(names))
		for i := range names {
			reads[i] = c.postgresRead(i)
		}
		b.cte = fmt.Sprintf("%s (%s) AS (SELECT %s FROM json_array_elements(CAST(%s AS json)) AS f4b_e)",
			boundTable, strings.Join(names, ", "), strings.Join(reads, ", "), dialect.Placeholder(1))
	}
	b.join = fmt.Sprintf(" JOIN %s ON %s.%s = %s.%s", boundTable, boundTable, seqName, quote(c.table), quote(c.seq))
	return b
}

// kindOf is the type of the i-th value of a row: the sequence, then each
// calculation's.
func (c *unstoredCompute) kindOf(i int) dbal.AgnosticFieldType {
	if i == 0 {
		if c.seq == "id" {
			return dbal.FieldTypeText
		}
		return "SEQUENCE"
	}
	return c.fields[c.want[i-1]]
}

// postgresRead reads the i-th value of a row on PostgreSQL, as its column
// type would hold it.
func (c *unstoredCompute) postgresRead(i int) string {
	text := fmt.Sprintf("f4b_e->>%d", i)
	switch kind := c.kindOf(i); kind {
	case "SEQUENCE":
		return "CAST(" + text + " AS BIGINT)"
	case dbal.FieldTypeNumber, dbal.FieldTypeDate, dbal.FieldTypeTimestamp, dbal.FieldTypeBoolean:
		return fmt.Sprintf("CAST(%s AS %s)", text, c.s.driver.Dialect().MapType(kind))
	}
	return text
}

// mariaType is the JSON_TABLE type of the i-th value of a row on MariaDB.
func (c *unstoredCompute) mariaType(i int) string {
	switch kind := c.kindOf(i); kind {
	case "SEQUENCE":
		return "BIGINT"
	case dbal.FieldTypeNumber, dbal.FieldTypeDate, dbal.FieldTypeTimestamp, dbal.FieldTypeBoolean:
		return c.s.driver.Dialect().MapType(kind)
	}
	return "LONGTEXT"
}

// forgetRecords drops what was computed and read for the records of one
// page, keeping what was learnt of the schema, so the next page is computed
// in the memory one page takes.
func (st *readState) forgetRecords() {
	clear(st.values)
	clear(st.computing)
	clear(st.prefetched)
	for _, r := range st.readers {
		clear(r.hops)
		clear(r.overflow)
		clear(r.sums)
	}
}
