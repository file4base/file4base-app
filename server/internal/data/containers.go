package data

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
)

// Containers in calculations.
//
// A formula sees a container field as a calc.Container: where the file is
// (table, record, field) and what is known of it. A calculation whose result
// is a container does not copy the file: its TEXT column holds a reference to
// the container field the answer came from, as JSON {"table", "record",
// "field"}, and a read resolves the reference to the file's current bytes
// and metadata, so the data API returns a container result exactly as it
// returns a container field.

// FieldTypeContainerResult is the type a fieldSet gives a calculation whose
// result is a container. It is physically a TEXT column holding a reference.
const FieldTypeContainerResult dbal.AgnosticFieldType = "CONTAINER_RESULT"

// containerRef is where a container result's file is: a container field, or
// one of its repetitions ("field[n]"), of a record, of this solution or of
// another one named by Database (#107).
type containerRef struct {
	Table    string `json:"table"`
	Record   string `json:"record"`
	Field    string `json:"field"`
	Database string `json:"database,omitempty"`
}

// encodeContainerRef is what a container result stores.
func encodeContainerRef(c calc.Container) string {
	b, _ := json.Marshal(containerRef{Table: c.Table, Record: c.Record, Field: c.Field, Database: c.Database})
	return string(b)
}

// decodeContainerRef reads what a container result stores; anything else is
// no container.
func decodeContainerRef(v any) (containerRef, bool) {
	var text string
	switch t := v.(type) {
	case string:
		text = t
	case []byte:
		text = string(t)
	default:
		return containerRef{}, false
	}
	var ref containerRef
	if !strings.HasPrefix(strings.TrimSpace(text), "{") || json.Unmarshal([]byte(text), &ref) != nil ||
		ref.Table == "" || ref.Record == "" || ref.Field == "" {
		return containerRef{}, false
	}
	return ref, true
}

// containerSize is the byte length of a container value: bytes on a write,
// base64 text on a read.
func containerSize(v any) (int64, bool) {
	switch t := v.(type) {
	case []byte:
		return int64(len(t)), len(t) > 0
	case string:
		if t == "" {
			return 0, false
		}
		n := base64.StdEncoding.DecodedLen(len(t)) - strings.Count(t[max(len(t)-2, 0):], "=")
		return int64(n), true
	}
	return 0, false
}

// typedValue is the value under a record key of a table as a formula sees it.
// A container is a calc.Container, described by describe only if the formula
// reads its name or type; an empty one is nil.
func typedValue(table string, fields fieldSet, record map[string]interface{}, key string, describe func(*calc.Container)) any {
	switch fields.typeOf(key) {
	case dbal.FieldTypeContainer:
		size, ok := containerSize(record[key])
		if !ok {
			return nil
		}
		c := calc.Container{Table: table, Record: fmt.Sprint(record["id"]), Field: key, Size: size, Describe: describe}
		if meta, ok := record[containermeta.Key].(map[string]containermeta.Entry); ok {
			if e, ok := meta[key]; ok {
				c.Filename, c.MimeType, c.SHA256, c.Describe = e.Filename, e.MimeType, e.SHA256, nil
			}
		}
		if _, fresh := record[key].([]byte); fresh {
			// Written by this very request: whatever metadata was stored is
			// of the file it replaces.
			c.Describe = nil
		}
		return c
	case FieldTypeContainerResult:
		ref, ok := decodeContainerRef(record[key])
		if !ok {
			return nil
		}
		if _, held := record[ref.Field]; held && ref.Database == "" && ref.Table == table &&
			ref.Record == fmt.Sprint(record["id"]) && fields.typeOf(ref.Field) == dbal.FieldTypeContainer {
			// A container of this very record is read from the record, which
			// holds what this request writes, not from the database, which
			// does not have it yet.
			return typedValue(table, fields, record, ref.Field, describe)
		}
		return calc.Container{Table: ref.Table, Record: ref.Record, Field: ref.Field, Database: ref.Database, Describe: describe}
	}
	return calc.Typed(resultTypeOf(fields.typeOf(key)), record[key])
}

// loadedContainer is a container's bytes and stored metadata.
type loadedContainer struct {
	bytes []byte
	meta  *containermeta.Entry
}

// loadContainers reads the files the references name, in one query per table
// for the bytes and one for the metadata. A reference to a record, field or
// table that is gone, or to a field that is no container, is left out.
func (s *Service) loadContainers(ctx context.Context, refs []containerRef) (map[containerRef]loadedContainer, error) {
	byTable := map[string][]containerRef{}
	for _, ref := range refs {
		byTable[ref.Table] = append(byTable[ref.Table], ref)
	}
	out := make(map[containerRef]loadedContainer, len(refs))
	dialect := s.driver.Dialect()
	for table, refs := range byTable {
		fields, err := s.tableFields(ctx, table)
		if err != nil {
			continue // the table is gone
		}
		read := fieldSet{"id": fields["id"]}
		ids := map[string]bool{}
		for _, ref := range refs {
			base := ref.Field
			if b, n, ok := repetition.Split(ref.Field); ok && n > 1 {
				base = b
				if _, ok := fields[repetitionsKey(base)]; ok {
					read[repetitionsKey(base)] = fields[repetitionsKey(base)]
				}
			}
			if fields[base] != dbal.FieldTypeContainer {
				continue
			}
			read[base] = fields[base]
			ids[ref.Record] = true
		}
		if len(ids) == 0 {
			continue
		}
		idList := make([]string, 0, len(ids))
		for id := range ids {
			idList = append(idList, id)
		}
		sort.Strings(idList)
		rows, err := s.selectByIDs(ctx, table, read, idList)
		if err != nil {
			return nil, err
		}
		meta, err := containermeta.Load(ctx, s.driver.DB(), dialect, table, idList)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			row, ok := rows[ref.Record]
			if !ok {
				continue
			}
			text, _ := row[ref.Field].(string)
			b, err := base64.StdEncoding.DecodeString(text)
			if err != nil || len(b) == 0 {
				continue
			}
			loaded := loadedContainer{bytes: b}
			if e, ok := meta[ref.Record][ref.Field]; ok {
				loaded.meta = &e
			}
			out[ref] = loaded
		}
	}
	return out, nil
}

// loadContainerRefs is loadContainers for references of this solution and of
// others. A file of another solution is read there, as the account the
// caller's session group holds in it; one of a table that account may not
// read, or of a solution that cannot be read, is left out.
func (s *Service) loadContainerRefs(ctx context.Context, refs []containerRef) (map[containerRef]loadedContainer, error) {
	byDatabase := map[string][]containerRef{}
	for _, ref := range refs {
		byDatabase[ref.Database] = append(byDatabase[ref.Database], ref)
	}
	out, err := s.loadContainers(ctx, byDatabase[""])
	if err != nil {
		return nil, err
	}
	delete(byDatabase, "")
	for database, refs := range byDatabase {
		byTable := map[string][]containerRef{}
		for _, ref := range refs {
			byTable[ref.Table] = append(byTable[ref.Table], ref)
		}
		for table, refs := range byTable {
			peer, err := s.Open(ctx, ExternalTarget{Database: database, Table: table})
			if err != nil {
				continue
			}
			there := make([]containerRef, len(refs))
			for i, ref := range refs {
				there[i] = ref
				there[i].Database = ""
			}
			loaded, err := peer.loadContainers(ctx, there)
			if err != nil {
				return nil, err
			}
			for i, ref := range refs {
				if l, ok := loaded[there[i]]; ok {
					out[ref] = l
				}
			}
		}
	}
	return out, nil
}

// selectByIDs reads the given fields of the records of a table, by id.
func (s *Service) selectByIDs(ctx context.Context, table string, fields fieldSet, ids []string) (map[string]map[string]interface{}, error) {
	dialect := s.driver.Dialect()
	out := make(map[string]map[string]interface{}, len(ids))
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		marks := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for i, id := range chunk {
			marks[i] = dialect.Placeholder(i + 1)
			args[i] = id
		}
		query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)", selectList(dialect, fields),
			dialect.QuoteIdentifier(table), dialect.QuoteIdentifier("id"), strings.Join(marks, ", "))
		rows, err := s.driver.DB().QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed reading containers of %s: %w", table, err)
		}
		records, err := rowsToMaps(rows, fields)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			out[fmt.Sprint(r["id"])] = r
		}
	}
	return out, nil
}

// describeContainer is the loader a formula describes a container with when
// it reads its name or type: the file's stored metadata, and its size for a
// container reached through a reference, whose bytes the formula has not
// seen. A reference is described only when the caller may read its table,
// in its own solution, as resolveContainerResults resolves it.
func (s *Service) describeContainer(ctx context.Context) func(*calc.Container) {
	return func(c *calc.Container) {
		if c.Database != "" {
			peer, err := s.Open(ctx, ExternalTarget{Database: c.Database, Table: c.Table})
			if err != nil {
				return
			}
			there := *c
			there.Database, there.Describe = "", nil
			peer.describeContainer(ctx)(&there)
			c.Filename, c.MimeType, c.SHA256, c.Size = there.Filename, there.MimeType, there.SHA256, there.Size
			return
		}
		ref := containerRef{Table: c.Table, Record: c.Record, Field: c.Field}
		if c.Size > 0 {
			// A container of a record in hand, which was read with access.
			meta, err := containermeta.Load(ctx, s.driver.DB(), s.driver.Dialect(), c.Table, []string{c.Record})
			if err == nil {
				if e, ok := meta[c.Record][c.Field]; ok {
					c.Filename, c.MimeType, c.SHA256 = e.Filename, e.MimeType, e.SHA256
				}
			}
			return
		}
		if s.newRelatedReader().checkRead(ctx, c.Table) != nil {
			return
		}
		loaded, err := s.loadContainers(ctx, []containerRef{ref})
		if err != nil {
			return
		}
		if l, ok := loaded[ref]; ok {
			c.Size = int64(len(l.bytes))
			if l.meta != nil {
				c.Filename, c.MimeType, c.SHA256 = l.meta.Filename, l.meta.MimeType, l.meta.SHA256
			}
		}
	}
}

// resolveContainerResults replaces the references container results hold in
// rows of a table with the files they name, as base64 with their metadata
// under containermeta.Key, the way a container field reads. A reference that
// no longer resolves, or to a table the caller may not read, is empty.
func (s *Service) resolveContainerResults(ctx context.Context, tableName string, rows []map[string]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return err
	}
	if !hasContainerResults(fields) {
		return nil
	}
	type cell struct {
		row map[string]interface{}
		key string
		ref containerRef
	}
	var cells []cell
	var refs []containerRef
	reader := s.newRelatedReader() // it remembers what the caller may read
	for _, row := range rows {
		for key, v := range row {
			if fields.typeOf(key) != FieldTypeContainerResult || fields.isInternal(key) {
				continue
			}
			ref, ok := decodeContainerRef(v)
			if !ok {
				row[key] = nil
				continue
			}
			if ref.Database == "" && ref.Table != tableName && reader.checkRead(ctx, ref.Table) != nil {
				row[key] = nil
				continue
			}
			cells = append(cells, cell{row, key, ref})
			refs = append(refs, ref)
		}
	}
	if len(cells) == 0 {
		return nil
	}
	loaded, err := s.loadContainerRefs(ctx, refs)
	if err != nil {
		return err
	}
	for _, c := range cells {
		l, ok := loaded[c.ref]
		if !ok {
			c.row[c.key] = nil
			continue
		}
		c.row[c.key] = base64.StdEncoding.EncodeToString(l.bytes)
		if l.meta == nil {
			continue
		}
		meta, _ := c.row[containermeta.Key].(map[string]containermeta.Entry)
		if meta == nil {
			meta = map[string]containermeta.Entry{}
			c.row[containermeta.Key] = meta
		}
		meta[c.key] = *l.meta
	}
	return nil
}

func hasContainerResults(fields fieldSet) bool {
	for _, t := range fields {
		if t == FieldTypeContainerResult {
			return true
		}
	}
	return false
}
