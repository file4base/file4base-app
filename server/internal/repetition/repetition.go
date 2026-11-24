// Package repetition stores the repetitions of FileMaker-style repeating
// fields.
//
// A field with a maximum of N repetitions keeps repetition 1 in its own
// column, exactly as a field that does not repeat: it is typed, indexed,
// sorted and searched like any other column, and a client that knows nothing
// of repetitions reads and writes it unchanged. Repetitions 2 to N are kept
// together in a companion column (Column) holding a JSON array, element 0
// being repetition 2. The array is text on every engine, so PostgreSQL and
// MariaDB store it the same way, and it is only ever read and written whole.
//
// A record exchanges repetition n of a field under the key "field[n]" (Key),
// which is also how a formula names it. Repetition 1 is the field's own key.
package repetition

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/shopspring/decimal"
)

// Max is the largest number of repetitions a field may have, as in FileMaker.
const Max = 32000

// ErrInvalid is returned for a repetition value its field cannot hold.
var ErrInvalid = errors.New("invalid repetition value")

// columnSuffix ends the name of a companion column. "$" cannot appear in a
// field name (see dbal.CheckIdentifier), so no field can ever take the name.
const columnSuffix = "$reps"

// Column is the physical column that holds repetitions 2 to N of field. A
// name too long for the engines' 63-byte limit is shortened and made unique
// with a digest of the full name.
func Column(field string) string {
	if len(field)+len(columnSuffix) <= dbal.MaxIdentifierLength {
		return field + columnSuffix
	}
	sum := sha256.Sum256([]byte(field))
	digest := "$" + hex.EncodeToString(sum[:4])
	keep := dbal.MaxIdentifierLength - len(columnSuffix) - len(digest)
	return field[:keep] + digest + columnSuffix
}

// Key is the record key of repetition n of field: the field's own name for
// the first, "field[n]" for the others.
func Key(field string, n int) string {
	if n <= 1 {
		return field
	}
	return field + "[" + strconv.Itoa(n) + "]"
}

// Split reads a record key "field[n]". ok is false for a key that names no
// repetition, such as a plain field name.
func Split(key string) (field string, n int, ok bool) {
	if !strings.HasSuffix(key, "]") {
		return key, 1, false
	}
	open := strings.LastIndexByte(key, '[')
	if open <= 0 {
		return key, 1, false
	}
	n, err := strconv.Atoi(key[open+1 : len(key)-1])
	if err != nil || n < 1 || strconv.Itoa(n) != key[open+1:len(key)-1] {
		return key, 1, false
	}
	return key[:open], n, true
}

// Check validates a maximum number of repetitions.
func Check(max int) error {
	if max < 1 || max > Max {
		return fmt.Errorf("the maximum number of repetitions must be between 1 and %d, not %d", Max, max)
	}
	return nil
}

// Encode is the companion column's content for repetitions 2 to N, element
// 0 being repetition 2. Trailing empty repetitions are not kept, and nothing
// to keep is NULL.
func Encode(values []any) (any, error) {
	end := len(values)
	for end > 0 && values[end-1] == nil {
		end--
	}
	if end == 0 {
		return nil, nil
	}
	b, err := json.Marshal(values[:end])
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Decode reads a companion column: the stored element of each repetition
// from 2 on, nil where it is empty.
func Decode(raw any) ([]any, error) {
	var text string
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return nil, fmt.Errorf("%w: repetitions must be stored as a JSON array, not %T", ErrInvalid, raw)
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var out []any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return out, nil
}

// Normalize turns a value written to a repetition of a field of type t into
// the element stored for it: text as a string, a number as its decimal text,
// a date as YYYY-MM-DD, a timestamp in RFC 3339 (UTC), a boolean as a
// boolean, and a container as standard base64. Nothing, or empty text in a
// field that is not text, is nil.
func Normalize(t dbal.AgnosticFieldType, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok && s == "" {
		return nil, nil
	}
	switch t {
	case dbal.FieldTypeNumber:
		n, err := toDecimal(v)
		if err != nil {
			return nil, err
		}
		return n.String(), nil
	case dbal.FieldTypeDate:
		at, err := toTime(v, true)
		if err != nil {
			return nil, err
		}
		return at.Format("2006-01-02"), nil
	case dbal.FieldTypeTimestamp:
		at, err := toTime(v, false)
		if err != nil {
			return nil, err
		}
		return at.UTC().Format(time.RFC3339Nano), nil
	case dbal.FieldTypeBoolean:
		b, ok := dbal.NormalizeBool(v).(bool)
		if !ok {
			switch s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v))); s {
			case "true", "1", "yes":
				return true, nil
			case "false", "0", "no":
				return false, nil
			}
			return nil, fmt.Errorf("%w: %v is not a boolean", ErrInvalid, v)
		}
		return b, nil
	case dbal.FieldTypeContainer:
		switch c := v.(type) {
		case []byte:
			if len(c) == 0 {
				return nil, nil
			}
			return base64.StdEncoding.EncodeToString(c), nil
		case string:
			if _, err := base64.StdEncoding.DecodeString(c); err != nil {
				return nil, fmt.Errorf("%w: a container value must be base64-encoded binary data", ErrInvalid)
			}
			return c, nil
		}
		return nil, fmt.Errorf("%w: a container value must be base64-encoded binary data", ErrInvalid)
	default:
		switch s := v.(type) {
		case string:
			return s, nil
		case []byte:
			if len(s) == 0 {
				return nil, nil
			}
			return string(s), nil
		case time.Time:
			return s.UTC().Format(time.RFC3339Nano), nil
		}
		return fmt.Sprint(v), nil
	}
}

// Read turns a stored element back into what a column of type t reads as
// through the data service: text and numbers as strings, dates and
// timestamps as times, booleans as booleans and containers as base64 text.
func Read(t dbal.AgnosticFieldType, element any) any {
	if element == nil {
		return nil
	}
	text := ""
	switch e := element.(type) {
	case bool:
		if t == dbal.FieldTypeBoolean {
			return e
		}
		text = strconv.FormatBool(e)
	case json.Number:
		text = e.String()
	case string:
		text = e
	default:
		text = fmt.Sprint(e)
	}
	switch t {
	case dbal.FieldTypeNumber:
		return dbal.TrimNumericText(text)
	case dbal.FieldTypeDate:
		if at, err := time.Parse("2006-01-02", text); err == nil {
			return at
		}
	case dbal.FieldTypeTimestamp:
		if at, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return at
		}
	case dbal.FieldTypeBoolean:
		return dbal.NormalizeBool(text)
	}
	return text
}

func toDecimal(v any) (decimal.Decimal, error) {
	switch n := v.(type) {
	case json.Number:
		return parseDecimal(n.String())
	case string:
		return parseDecimal(n)
	case []byte:
		return parseDecimal(string(n))
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			break
		}
		return decimal.NewFromFloat(n), nil
	case float32:
		return decimal.NewFromFloat32(n), nil
	case int:
		return decimal.NewFromInt(int64(n)), nil
	case int32:
		return decimal.NewFromInt(int64(n)), nil
	case int64:
		return decimal.NewFromInt(n), nil
	case decimal.Decimal:
		return n, nil
	case bool:
		if n {
			return decimal.NewFromInt(1), nil
		}
		return decimal.Zero, nil
	}
	return decimal.Zero, fmt.Errorf("%w: %v is not a number", ErrInvalid, v)
}

func parseDecimal(s string) (decimal.Decimal, error) {
	n, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: %q is not a number", ErrInvalid, s)
	}
	return n, nil
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}

func toTime(v any, dateOnly bool) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		if dateOnly {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
		return t, nil
	case string:
		s := strings.TrimSpace(t)
		for _, layout := range timeLayouts {
			if at, err := time.Parse(layout, s); err == nil {
				if dateOnly {
					return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), nil
				}
				return at, nil
			}
		}
	}
	what := "timestamp"
	if dateOnly {
		what = "date"
	}
	return time.Time{}, fmt.Errorf("%w: %v is not a %s", ErrInvalid, v, what)
}

// Pack is the companion column content of a field of type t with max
// repetitions, given the values of its repetitions from 2 (by number).
// Repetitions not given are empty.
func Pack(t dbal.AgnosticFieldType, max int, values map[int]any) (any, error) {
	if max < 2 {
		return nil, nil
	}
	elements := make([]any, max-1)
	for n, v := range values {
		if n < 2 || n > max {
			return nil, fmt.Errorf("%w: repetition %d is outside 2 to %d", ErrInvalid, n, max)
		}
		element, err := Normalize(t, v)
		if err != nil {
			return nil, fmt.Errorf("repetition %d: %w", n, err)
		}
		elements[n-2] = element
	}
	return Encode(elements)
}

// Merge is the companion column content once the given repetitions (from 2,
// up to max) of a field of type t are written over the stored content raw.
// The other repetitions keep their stored elements, including those past
// max, kept from a time the maximum was higher: as in FileMaker, they come
// back if the maximum is raised again.
func Merge(t dbal.AgnosticFieldType, max int, raw any, values map[int]any) (any, error) {
	elements, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	for n, v := range values {
		if n < 2 || n > max {
			return nil, fmt.Errorf("%w: repetition %d is outside 2 to %d", ErrInvalid, n, max)
		}
		element, err := Normalize(t, v)
		if err != nil {
			return nil, fmt.Errorf("repetition %d: %w", n, err)
		}
		for len(elements) < n-1 {
			elements = append(elements, nil)
		}
		elements[n-2] = element
	}
	return Encode(elements)
}

// Unpack reads a companion column back: the repetitions from 2 that hold a
// value, by number, as Read gives them. Repetitions past max are not read.
func Unpack(t dbal.AgnosticFieldType, max int, raw any) (map[int]any, error) {
	elements, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	out := map[int]any{}
	for i, element := range elements {
		if i+2 > max {
			break
		}
		if element != nil {
			out[i+2] = Read(t, element)
		}
	}
	return out, nil
}
