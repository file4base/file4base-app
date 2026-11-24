package migration

import (
	"fmt"
	"sort"
	"strings"
)

// nativeCatalog is the table and field catalog a native file carries about
// itself: table names by table id, field names by table and field id.
type nativeCatalog struct {
	tables map[int]string
	fields map[int]map[int]string
}

// readHBAM7Catalog reads the names a FileMaker 12+ file stores for its own
// tables (path 3/16/5/<table>) and fields (<table>/3/5/<field>). A name is
// held inline under key 16 of the object's node or, when long, as segments
// 1, 2, ... of its sub-path 16. The layout follows Evan Miller's MIT-licensed
// fmptools (list_tables.c, list_columns.c, fmp.c name_chunk).
func readHBAM7Catalog(source []byte) (*nativeCatalog, error) {
	type part struct {
		order int
		text  []byte
	}
	parts := map[[2]int][]part{} // {table, field}; field 0 is the table name
	add := func(table, field, order int, raw []byte) {
		k := [2]int{table, field}
		parts[k] = append(parts[k], part{order: order, text: append([]byte(nil), raw...)})
	}
	err := walkHBAM7(source, func(c hbamChunk) error {
		if c.kind != 'v' && c.kind != 'g' {
			return nil
		}
		n := len(c.path)
		if n < 4 {
			return nil
		}
		p := func(i int) int { return hbamPathInt(c.path[i]) }
		var table, field int
		switch {
		case p(0) == 3 && p(1) == 16 && p(2) == 5 && p(3) >= 128:
			table = p(3)
		case p(0) >= 128 && p(1) == 3 && p(2) == 5 && p(3) >= 1:
			table, field = p(0), p(3)
		default:
			return nil
		}
		switch {
		case n == 4 && c.kind == 'v' && c.key == 16:
			add(table, field, 0, c.data)
		case n == 5 && p(4) == 16 && c.key >= 1:
			add(table, field, c.key, c.data)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	cat := &nativeCatalog{tables: map[int]string{}, fields: map[int]map[int]string{}}
	for k, ps := range parts {
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].order < ps[j].order })
		var b strings.Builder
		for _, x := range ps {
			text, err := decodeHBAM7Text(x.text)
			if err != nil {
				return nil, fmt.Errorf("invalid native catalog name encoding")
			}
			b.WriteString(text)
		}
		name := b.String()
		if k[1] == 0 {
			cat.tables[k[0]] = name
			continue
		}
		if cat.fields[k[0]] == nil {
			cat.fields[k[0]] = map[int]string{}
		}
		cat.fields[k[0]][k[1]] = name
	}
	return cat, nil
}

// nativeDesignMismatches compares the design's tables and fields with the
// native file's own catalog. Records are addressed by numeric ids that every
// FileMaker file reuses (base tables from 129, fields from 1), so without this
// check a design from one solution silently maps another file's records onto
// its fields (#130). It returns a description per disagreement, sorted.
func nativeDesignMismatches(cat *nativeCatalog, tables map[int]string, fields map[int]map[int]htmlFieldMeta) []string {
	var out []string
	for tid, sqlName := range tables {
		native, ok := cat.tables[tid]
		if !ok {
			out = append(out, fmt.Sprintf("table %q (id %d) is not in the native file", sqlName, tid))
			continue
		}
		if s := sanitizeIdentifier(native); s != sqlName && !strings.HasPrefix(sqlName, s) {
			out = append(out, fmt.Sprintf("table id %d is %q in the design but %q in the native file", tid, sqlName, native))
		}
		for fid, meta := range fields[tid] {
			nativeField, ok := cat.fields[tid][fid]
			if !ok {
				continue // a field added after the copy holds no data in it
			}
			if !strings.EqualFold(strings.TrimSpace(nativeField), strings.TrimSpace(meta.RawName)) {
				out = append(out, fmt.Sprintf("field id %d of table %q is %q in the design but %q in the native file",
					fid, sqlName, meta.RawName, nativeField))
			}
		}
	}
	sort.Strings(out)
	return out
}
