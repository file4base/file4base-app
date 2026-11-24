package migration

// The HBAM7 chunk grammar and path model below follow Evan Miller's MIT-licensed
// fmptools (src/block.c and HACKING), revision fd32a6c4f425a377f27efd558cf9bc7cc09985a6.
// Copyright (c) 2020 Evan Miller. The license is retained in
// server/tools/notices/fmptools/LICENSE. This implementation reads whole chunk paths,
// rather than guessing record boundaries from byte sequences inside values.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

var reSourceNumericLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

type hbamChunk struct {
	path    [][]byte
	longKey []byte
	key     int
	keyLen  int
	data    []byte
	kind    byte // 'v': complete keyed value; 'g': segment; 'l': long key; 's': unkeyed data; 'p': path changed
}

func hbamPathInt(b []byte) int {
	switch len(b) {
	case 1:
		return int(b[0])
	case 2:
		return 128 + (int(b[0]&127) << 8) + int(b[1])
	case 3:
		return 128 + int(binary.BigEndian.Uint16(b[1:]))
	}
	return 0
}

func walkHBAM7(data []byte, consume func(hbamChunk) error) error {
	const size = 4096
	if len(data) < size*3 || len(data)%size != 0 || string(data[15:20]) != "HBAM7" {
		return fmt.Errorf("unsupported native file header")
	}
	pages := len(data) / size
	first := 0
	for n := 2; n < pages; n++ {
		b := data[n*size : (n+1)*size]
		if b[0] == 0 && binary.BigEndian.Uint16(b[12:14]) == 0 && binary.BigEndian.Uint32(b[4:8]) == 0 {
			first = n
			break
		}
	}
	if first == 0 {
		return fmt.Errorf("native file has no live leaf chain")
	}
	seen := make(map[int]bool)
	previous := 0
	for page := first; page != 0; {
		if page < 2 || page >= pages || seen[page] {
			return fmt.Errorf("invalid native leaf chain")
		}
		seen[page] = true
		block := data[page*size : (page+1)*size]
		if int(binary.BigEndian.Uint32(block[4:8])) != previous {
			return fmt.Errorf("native leaf chain has inconsistent links")
		}
		if block[0] != 0 || binary.BigEndian.Uint16(block[12:14]) != 0 {
			return fmt.Errorf("native leaf chain enters a non-leaf block")
		}
		if err := walkHBAM7Payload(block[20:], consume); err != nil {
			return fmt.Errorf("native block %d: %w", page, err)
		}
		previous = page
		page = int(binary.BigEndian.Uint32(block[8:12]))
	}
	return nil
}

func walkHBAM7Payload(payload []byte, consume func(hbamChunk) error) error {
	var path [][]byte
	for pos := 0; pos < len(payload); {
		start := pos
		code := payload[pos]
		pos++
		pop := code&0xc0 == 0xc0
		if pop {
			code &= 0x3f
		}
		take := func(n int) ([]byte, error) {
			if n < 0 || n > len(payload)-pos {
				return nil, fmt.Errorf("truncated chunk at byte %d", start)
			}
			b := payload[pos : pos+n]
			pos += n
			return b, nil
		}
		read := func(n int) (int, error) {
			b, e := take(n)
			if e != nil {
				return 0, e
			}
			if n == 1 {
				return int(b[0]), nil
			}
			return int(binary.BigEndian.Uint16(b)), nil
		}
		var chunk hbamChunk
		chunk.path = path
		chunk.kind = 's'
		keyBytes, dataLength, keyLength := 0, 0, 0
		var err error
		switch {
		case code == 0:
			if pos == len(payload) || payload[pos] == 0 {
				return nil
			}
			dataLength = 1
		case code >= 1 && code <= 5:
			keyBytes = 1
			dataLength = 2 * int(code-1)
			if code == 1 {
				dataLength = 1
			}
		case code == 6 || code == 7:
			keyBytes = 1
			keyLength = int(code - 5)
		case code == 8:
			dataLength = 2
		case code == 0x0e && pos < len(payload) && payload[pos] == 0xff:
			dataLength = 6
		case code >= 9 && code <= 13:
			keyBytes = 2
			dataLength = 2 * int(code-9)
			if code == 9 {
				dataLength = 1
			}
		case code == 14 || code == 15:
			keyBytes = 2
			keyLength = int(code - 13)
		case code == 16:
			dataLength = 3
		case code >= 17 && code <= 21:
			dataLength = 3 + 2*int(code-17)
			if code == 17 {
				dataLength++
			}
		case code == 22 || code == 23:
			chunk.kind = 'l'
			chunk.longKey, err = take(3)
			if err == nil {
				dataLength, err = read(int(code - 21))
			}
		case code == 0x23:
			dataLength, err = read(1)
		case code == 24:
			dataLength, err = read(1)
		case code == 0x1b && pos < len(payload) && payload[pos] == 0:
			_, err = take(1)
			keyBytes = 1
			dataLength = 4
		case code >= 25 && code <= 29:
			dataLength, err = read(1)
			if err == nil {
				chunk.data, err = take(dataLength)
				if err == nil {
					_, err = take(2*int(code-25) + boolInt(code == 25))
				}
				dataLength = 0
			}
		case code == 30 || code == 31:
			var n int
			n, err = read(1)
			if err == nil {
				chunk.kind = 'l'
				chunk.longKey, err = take(n)
			}
			if err == nil {
				dataLength, err = read(int(code - 29))
			}
		case code == 0x20 || code == 0x28 || code == 0x30 || code == 0x38:
			n := 1
			if code == 0x28 {
				n = 2
			}
			if code == 0x30 {
				n = 3
			}
			if code == 0x38 {
				n, err = read(1)
			} else if code == 0x20 && pos < len(payload) && payload[pos] == 0xfe {
				pos++
				n = 8
			}
			var component []byte
			if err == nil {
				component, err = take(n)
			}
			if err == nil {
				path = append(path, component)
				chunk.path = path
				chunk.kind = 'p'
			}
		case code == 0x3d || code == 0x40:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
			chunk.path = path
			chunk.kind = 'p'
		case code == 0x80 || code == 0x88:
			continue
		default:
			return fmt.Errorf("unsupported chunk code 0x%02x at byte %d", code, start)
		}
		if err != nil {
			return err
		}
		if keyBytes > 0 {
			var key []byte
			key, err = take(keyBytes)
			if err != nil {
				return err
			}
			chunk.key = hbamPathInt(key)
			chunk.keyLen = keyBytes
			chunk.kind = 'v'
			if code == 7 || code == 15 {
				chunk.kind = 'g'
			}
			if keyLength > 0 {
				dataLength, err = read(keyLength)
				if err != nil {
					return err
				}
			}
		}
		if dataLength > 0 {
			chunk.data, err = take(dataLength)
			if err != nil {
				return err
			}
		}
		if err := consume(chunk); err != nil {
			return err
		}
		if pop && len(path) > 0 {
			path = path[:len(path)-1]
		}
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// readHBAM7Records collects fields by their source table, record and field IDs.
// Keyed chunks and long-value segments may span pages; byte patterns inside a
// value have no structural meaning. Source text is not trimmed or rounded.
func readHBAM7Records(source []byte, tables map[int]string, fields map[int]map[int]htmlFieldMeta, types map[string]string, dateOrder string) (map[string][]map[string]interface{}, map[string]int, error) {
	return readHBAM7RecordsWith(source, tables, fields, types, nativeReadOptions{dateOrder: dateOrder})
}

// Raw mode is used only by local source qualification. It keeps source text,
// including values the target cannot store, and does not claim a typed import.
func readHBAM7RecordsMode(source []byte, tables map[int]string, fields map[int]map[int]htmlFieldMeta, types map[string]string, dateOrder string, rawValues bool) (map[string][]map[string]interface{}, map[string]int, error) {
	return readHBAM7RecordsWith(source, tables, fields, types, nativeReadOptions{dateOrder: dateOrder, rawValues: rawValues})
}

// nativeReadOptions says how native record values are read.
type nativeReadOptions struct {
	// dateOrder is the order of the file's dates: iso, dmy or mdy.
	dateOrder string
	// rawValues keeps every value as its source text (local qualification).
	rawValues bool
	// locale, when named, migrates permissive values with their original
	// text instead of refusing them (#124).
	locale *sourceLocale
	// nulStripped, when set, counts Text values whose NUL characters were
	// removed: with a named locale the migration repairs values the target
	// cannot store instead of refusing the dataset. PostgreSQL text cannot
	// hold NUL, and FileMaker shows nothing for it.
	nulStripped *int
}

func readHBAM7RecordsWith(source []byte, tables map[int]string, fields map[int]map[int]htmlFieldMeta, types map[string]string, opts nativeReadOptions) (map[string][]map[string]interface{}, map[string]int, error) {
	dateOrder, rawValues := opts.dateOrder, opts.rawValues
	type cellKey struct {
		table, field int
		row          string
	}
	cells := map[cellKey][][]byte{}
	rows := map[int]map[string]bool{}
	issues := map[string]int{}
	err := walkHBAM7(source, func(c hbamChunk) error {
		if len(c.path) < 3 {
			return nil
		}
		table, section := hbamPathInt(c.path[0]), hbamPathInt(c.path[1])
		// Record tokens are opaque: three-byte tokens can share their final
		// two bytes. Reducing them to path integers silently merges records.
		row := string(c.path[2])
		if tables[table] == "" || section != 5 || len(row) == 0 {
			return nil
		}
		if rows[table] == nil {
			rows[table] = map[string]bool{}
		}
		rows[table][row] = true
		if c.kind == 'l' && !rawValues {
			issues["LONG_FIELD_KEY"]++
			return nil
		}
		if c.kind != 'v' && c.kind != 'g' {
			return nil
		}
		fid, keyLen := c.key, c.keyLen
		if len(c.path) == 4 {
			fid = hbamPathInt(c.path[3])
			keyLen = len(c.path[3])
			// Key zero formats text but carries required binary metadata in
			// a container. Do not discard it as text formatting.
			if c.key == 0 && types[tables[table]+"."+fields[table][fid].ColName] != "CONTAINER" {
				return nil
			}
		} else if len(c.path) != 3 {
			return nil
		}
		repetition := 1
		if _, ok := fields[table][fid]; !ok {
			if keyLen != 2 || fid < 128 {
				return nil
			}
			candidate, rep := (fid-128)>>8, (fid-128)&255
			if _, ok := fields[table][candidate]; !ok || rep < 1 {
				return nil
			}
			fid, repetition = candidate, rep
		}
		if len(c.path) == 3 && c.kind == 'v' && c.key == 252 {
			return nil
		}
		key := cellKey{table: table, row: row, field: fid}
		slots := cells[key]
		for len(slots) < repetition {
			slots = append(slots, nil)
		}
		if len(c.path) == 3 {
			slots[repetition-1] = append([]byte(nil), c.data...)
		} else {
			slots[repetition-1] = append(slots[repetition-1], c.data...)
		}
		cells[key] = slots
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	out := map[string][]map[string]interface{}{}
	tableIDs := make([]int, 0, len(rows))
	for table := range rows {
		tableIDs = append(tableIDs, table)
	}
	sort.Ints(tableIDs)
	for _, table := range tableIDs {
		records := rows[table]
		fieldIDs := make([]int, 0, len(fields[table]))
		for fid := range fields[table] {
			fieldIDs = append(fieldIDs, fid)
		}
		sort.Ints(fieldIDs)
		ids := make([]string, 0, len(records))
		for row := range records {
			ids = append(ids, row)
		}
		sort.Strings(ids)
		for _, rowID := range ids {
			row := map[string]interface{}{}
			for _, fid := range fieldIDs {
				meta := fields[table][fid]
				repetitions := cells[cellKey{table: table, row: rowID, field: fid}]
				if len(repetitions) == 0 {
					continue
				}
				kind := types[tables[table]+"."+meta.ColName]
				if kind == "CONTAINER" {
					if !rawValues {
						for _, part := range repetitions {
							if len(part) > 0 {
								issues["CONTAINER"]++
								break
							}
						}
					}
					continue
				}
				if kind == "CALCULATION" || kind == "SUMMARY" {
					continue
				}

				// Each repetition is an independent SCSU stream. Missing slots
				// stay empty, rather than shifting later values to the left.
				values := make([]string, len(repetitions))
				for rep, raw := range repetitions {
					value, e := decodeHBAM7Text(raw)
					if e != nil {
						return nil, nil, fmt.Errorf("invalid native text encoding")
					}
					values[rep] = value
				}
				text := strings.Join(values, "\x1d")
				var err error
				if text == "" {
					continue
				}
				if !rawValues && strings.ContainsRune(text, 0) {
					if opts.locale == nil || opts.nulStripped == nil {
						issues["NUL_TEXT"]++
						continue
					}
					text = strings.ReplaceAll(text, "\x00", "")
					*opts.nulStripped++
					if text == "" {
						continue
					}
				}
				var value interface{}
				switch {
				case kind == "TEXT" || kind == "":
					value = text
				case opts.locale != nil && !rawValues:
					var original string
					order := data.DateOrder(strings.ToLower(dateOrder))
					if order == "" {
						order = opts.locale.DateOrder
					}
					value, original, err = interpretSourceValue(text, kind, opts.locale, order)
					if errors.Is(err, errSourceRepetitions) {
						issues["REPETITIONS"]++
						continue
					}
					if err == nil && original != "" {
						addSourceText(row, meta.ColName, original)
					}
				default:
					value, err = coerceNativeValue(text, kind, dateOrder)
				}
				if err != nil {
					issues[kind]++
					continue
				}
				row[meta.ColName] = value
			}
			out[tables[table]] = append(out[tables[table]], row)
		}
	}
	if len(issues) > 0 {
		records := 0
		for _, rr := range rows {
			records += len(rr)
		}
		return nil, nil, &nativeValueError{Records: records, Counts: issues}
	}
	return out, map[string]int{}, nil
}

type nativeValueError struct {
	Records int
	Counts  map[string]int
}

func (e *nativeValueError) Error() string {
	keys := make([]string, 0, len(e.Counts))
	for kind := range e.Counts {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, kind := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, e.Counts[kind]))
	}
	return fmt.Sprintf("native source has %d records, but values cannot be represented (%s); no rows returned", e.Records, strings.Join(parts, ", "))
}

func coerceNativeValue(text, kind, dateOrder string) (interface{}, error) {
	order := data.DateOrder(strings.ToLower(dateOrder))
	if order == "" {
		order = data.DateISO
	}
	if kind == "DATE" {
		return data.CoerceImportValue(dbal.FieldTypeDate, text, order)
	}
	if kind == "NUMBER" {
		return coerceSourceValue(text, kind)
	}
	if kind != "TIMESTAMP" {
		return coerceSourceValue(text, kind)
	}
	if value, err := data.CoerceImportValue(dbal.FieldTypeTimestamp, text, data.DateISO); err == nil {
		return value, nil
	}
	// Native dates follow the file's locale, unlike FMPXMLRESULT's MDY export.
	// Keep the existing timestamp coercer's UTC convention; this does not
	// establish source timezone or time/duration field compatibility.
	layouts := []string{}
	if order == data.DateDMY {
		layouts = []string{"02/01/2006 15:04:05", "02/01/2006 15:04", "2/1/2006 15:04:05", "2/1/2006 15:04"}
	}
	if order == data.DateMDY {
		layouts = []string{"01/02/2006 15:04:05", "01/02/2006 15:04", "1/2/2006 15:04:05", "1/2/2006 15:04"}
	}
	for _, layout := range layouts {
		if at, err := time.Parse(layout, strings.TrimSpace(text)); err == nil {
			return at.Format(time.RFC3339), nil
		}
	}
	return nil, fmt.Errorf("native timestamp is not representable in the declared date order")
}
