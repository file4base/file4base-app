// Package autoenter reads and checks a field's auto-enter options, the
// Auto-Enter tab of FileMaker's Field Options dialog.
//
// The options are kept, with the field's storage options, as one JSON object
// in sys_columns.default_value (the column name predates the options). Every
// key is optional, and an absent key is the option being off, so a column
// saved by an older client, or holding no options at all, keeps working:
//
//	creation_enabled, creation_type          Creation: Date, Time, Timestamp,
//	modification_enabled, modification_type  Name or Account Name
//	serial_enabled, serial_generate,         Serial number: "creation" or
//	serial_next, serial_increment            "commit", the next value (a
//	                                         number, or text ending in one,
//	                                         such as "INV0001") and the
//	                                         increment (a whole number ≥ 1)
//	value_last_visited                       Value from last visited record
//	data_enabled, data_value                 Data: a constant
//	calculated_enabled, calculated_formula,  Calculated value, evaluated from
//	calculated_context, do_not_replace,      calculated_context (a table
//	calculated_skip_if_empty                 occurrence of the field's table)
//	looked_up_enabled, looked_up_context,    Looked-up value: the field
//	looked_up_occurrence, looked_up_field,   looked_up_field of the occurrence
//	looked_up_no_match,                      looked_up_occurrence, reached from
//	looked_up_no_match_value,                looked_up_context
//	looked_up_dont_copy_if_empty
//	prohibit_modification                    Prohibit modification of value
//	                                         during data entry
//
// The other keys of the object (storage_global, indexing, …) are the
// Storage tab's and are kept as they are. See docs/specs/auto_enter_options.md.
package autoenter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
)

// ErrInvalid is returned for options that cannot be applied.
var ErrInvalid = errors.New("invalid auto-enter options")

// Stamp is what a creation or modification option enters.
type Stamp string

const (
	StampDate        Stamp = "Date"
	StampTime        Stamp = "Time"
	StampTimestamp   Stamp = "Timestamp"
	StampName        Stamp = "Name"
	StampAccountName Stamp = "Account Name"
)

// Stamps lists every stamp, in the order the dialog offers them.
var Stamps = []Stamp{StampDate, StampTime, StampTimestamp, StampName, StampAccountName}

// Serial generation moments.
const (
	GenerateOnCreation = "creation"
	GenerateOnCommit   = "commit"
)

// NoMatch is what a lookup copies when no related record matches.
type NoMatch string

const (
	NoMatchDoNotCopy  NoMatch = "do_not_copy"
	NoMatchNextLower  NoMatch = "copy_next_lower"
	NoMatchNextHigher NoMatch = "copy_next_higher"
	NoMatchConstant   NoMatch = "use_constant"
)

// Options are the auto-enter options of one field.
type Options struct {
	CreationEnabled     bool  `json:"creation_enabled,omitempty"`
	CreationType        Stamp `json:"creation_type,omitempty"`
	ModificationEnabled bool  `json:"modification_enabled,omitempty"`
	ModificationType    Stamp `json:"modification_type,omitempty"`

	SerialEnabled  bool   `json:"serial_enabled,omitempty"`
	SerialGenerate string `json:"serial_generate,omitempty"`
	// SerialNext is the value the next record gets. It is read from a JSON
	// number or string, and the server keeps it as a string once it has
	// allocated one.
	SerialNext      string `json:"-"`
	SerialIncrement int64  `json:"-"`

	ValueLastVisited bool `json:"value_last_visited,omitempty"`

	DataEnabled bool    `json:"data_enabled,omitempty"`
	DataValue   *string `json:"data_value,omitempty"`

	CalculatedEnabled bool   `json:"calculated_enabled,omitempty"`
	CalculatedFormula string `json:"calculated_formula,omitempty"`
	CalculatedContext string `json:"calculated_context,omitempty"`
	// DoNotReplace is "Do not replace existing value of field (if any)":
	// the formula is evaluated only while the field is empty.
	DoNotReplace bool `json:"do_not_replace,omitempty"`
	// SkipIfEmpty is "Do not evaluate if all referenced fields are empty",
	// on unless the options say otherwise, as in FileMaker.
	SkipIfEmpty bool `json:"-"`

	LookedUpEnabled         bool    `json:"looked_up_enabled,omitempty"`
	LookedUpContext         string  `json:"looked_up_context,omitempty"`
	LookedUpOccurrence      string  `json:"looked_up_occurrence,omitempty"`
	LookedUpField           string  `json:"looked_up_field,omitempty"`
	LookedUpNoMatch         NoMatch `json:"looked_up_no_match,omitempty"`
	LookedUpNoMatchValue    string  `json:"looked_up_no_match_value,omitempty"`
	LookedUpDontCopyIfEmpty bool    `json:"looked_up_dont_copy_if_empty,omitempty"`

	ProhibitModification bool `json:"prohibit_modification,omitempty"`

	// StorageGlobal is the Storage tab's "Use global storage": changing such
	// a field does not modify the record.
	StorageGlobal bool `json:"storage_global,omitempty"`
}

// Any reports whether an option enters a value or protects the field.
func (o Options) Any() bool {
	return o.CreationEnabled || o.ModificationEnabled || o.SerialEnabled || o.DataEnabled ||
		o.CalculatedEnabled || o.LookedUpEnabled || o.ProhibitModification
}

// HasConstant reports whether Data enters a value.
func (o Options) HasConstant() bool {
	return o.DataEnabled && o.DataValue != nil && *o.DataValue != ""
}

// raw is the shape the options are decoded from: the keys whose JSON type
// is not fixed are read as raw values.
type raw struct {
	Options
	SerialNext      json.RawMessage `json:"serial_next"`
	SerialIncrement json.RawMessage `json:"serial_increment"`
	SkipIfEmpty     *bool           `json:"calculated_skip_if_empty"`
	LookedUpTarget  string          `json:"looked_up_target"`
}

// Parse reads the options a column holds. Nothing, and text that is not a
// JSON object (what columns held before the options were an object), are no
// options; a value of the wrong type is an error.
func Parse(stored *string) (Options, error) {
	if stored == nil || strings.TrimSpace(*stored) == "" {
		return Options{}, nil
	}
	text := strings.TrimSpace(*stored)
	if !strings.HasPrefix(text, "{") {
		return Options{}, nil
	}
	var r raw
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return Options{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	o := r.Options
	o.SkipIfEmpty = r.SkipIfEmpty == nil || *r.SkipIfEmpty
	o.SerialIncrement = 1
	if len(r.SerialNext) > 0 && string(r.SerialNext) != "null" {
		next, err := jsonScalarText(r.SerialNext)
		if err != nil {
			return Options{}, fmt.Errorf("%w: serial_next: %v", ErrInvalid, err)
		}
		o.SerialNext = next
	}
	if o.SerialNext == "" {
		o.SerialNext = "1"
	}
	if len(r.SerialIncrement) > 0 && string(r.SerialIncrement) != "null" {
		text, err := jsonScalarText(r.SerialIncrement)
		if err == nil && text != "" {
			n, ok := new(big.Int).SetString(text, 10)
			if !ok || !n.IsInt64() {
				return Options{}, fmt.Errorf("%w: serial_increment must be a whole number", ErrInvalid)
			}
			o.SerialIncrement = n.Int64()
		} else if err != nil {
			return Options{}, fmt.Errorf("%w: serial_increment must be a whole number", ErrInvalid)
		}
	}
	if o.SerialGenerate == "" {
		o.SerialGenerate = GenerateOnCreation
	}
	if o.LookedUpNoMatch == "" {
		o.LookedUpNoMatch = NoMatchDoNotCopy
	}
	// The dialog once kept a lookup as one text, "Occurrence::Field".
	if o.LookedUpOccurrence == "" && o.LookedUpField == "" && r.LookedUpTarget != "" {
		if occ, field, ok := strings.Cut(r.LookedUpTarget, "::"); ok {
			o.LookedUpOccurrence, o.LookedUpField = strings.TrimSpace(occ), strings.TrimSpace(field)
		}
	}
	if o.CreationType == "" {
		o.CreationType = StampDate
	}
	if o.ModificationType == "" {
		o.ModificationType = StampDate
	}
	return o, nil
}

func jsonScalarText(raw json.RawMessage) (string, error) {
	var v interface{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t), nil
	case json.Number:
		return t.String(), nil
	}
	return "", fmt.Errorf("must be a number or a text")
}

// Validate checks the options of a column of fieldType ("" when it is not
// known): a stamp, serial or formula that could never be entered is refused
// when it is saved rather than failing every new record.
func Validate(fieldType string, stored *string) error {
	o, err := Parse(stored)
	if err != nil {
		return err
	}
	fieldType = strings.ToUpper(fieldType)
	for _, s := range []struct {
		on   bool
		kind string
		t    Stamp
	}{{o.CreationEnabled, "creation", o.CreationType}, {o.ModificationEnabled, "modification", o.ModificationType}} {
		if !s.on {
			continue
		}
		if !validStamp(s.t) {
			return fmt.Errorf("%w: %s_type must be one of Date, Time, Timestamp, Name or Account Name", ErrInvalid, s.kind)
		}
		if fieldType != "" && !StampFits(s.t, fieldType) {
			return fmt.Errorf("%w: a %s field cannot hold the %s %s", ErrInvalid, strings.ToLower(fieldType), s.kind, strings.ToLower(string(s.t)))
		}
	}
	if o.SerialEnabled {
		if o.SerialGenerate != GenerateOnCreation && o.SerialGenerate != GenerateOnCommit {
			return fmt.Errorf("%w: serial_generate must be %q or %q", ErrInvalid, GenerateOnCreation, GenerateOnCommit)
		}
		if o.SerialIncrement < 1 {
			return fmt.Errorf("%w: serial_increment must be a whole number of at least 1", ErrInvalid)
		}
		if _, err := NextSerial(o.SerialNext, 0); err != nil {
			return err
		}
		switch fieldType {
		case "", "TEXT":
		case "NUMBER":
			if !isInteger(o.SerialNext) {
				return fmt.Errorf("%w: the next serial value of a number field must be a whole number, not %q", ErrInvalid, o.SerialNext)
			}
		default:
			return fmt.Errorf("%w: a %s field cannot hold a serial number", ErrInvalid, strings.ToLower(fieldType))
		}
	}
	if o.CalculatedEnabled {
		if strings.TrimSpace(o.CalculatedFormula) == "" {
			return fmt.Errorf("%w: a calculated value needs a formula", ErrInvalid)
		}
		if _, err := calc.Parse(o.CalculatedFormula); err != nil {
			return fmt.Errorf("%w: the auto-enter calculation does not read: %v", ErrInvalid, err)
		}
	}
	if o.LookedUpEnabled {
		if o.LookedUpOccurrence == "" || o.LookedUpField == "" {
			return fmt.Errorf("%w: a looked-up value needs the table occurrence and the field to copy from", ErrInvalid)
		}
		switch o.LookedUpNoMatch {
		case NoMatchDoNotCopy, NoMatchNextLower, NoMatchNextHigher, NoMatchConstant:
		default:
			return fmt.Errorf("%w: looked_up_no_match must be do_not_copy, copy_next_lower, copy_next_higher or use_constant", ErrInvalid)
		}
	}
	return nil
}

func validStamp(s Stamp) bool {
	for _, known := range Stamps {
		if s == known {
			return true
		}
	}
	return false
}

// StampFits reports whether a field of fieldType can hold a stamp: a date in
// a date, timestamp or text field, a time or timestamp in a timestamp or text
// field (File4Base keeps FileMaker's Time fields as timestamps), a name in a
// text field.
func StampFits(s Stamp, fieldType string) bool {
	switch strings.ToUpper(fieldType) {
	case "TEXT":
		return true
	case "DATE":
		return s == StampDate
	case "TIMESTAMP":
		return s == StampDate || s == StampTime || s == StampTimestamp
	}
	return false
}

func isInteger(text string) bool {
	_, ok := new(big.Int).SetString(strings.TrimSpace(text), 10)
	return ok
}

// NextSerial is the serial value that follows next when increment is added
// to it. A whole number is added to; in a text, the last run of digits is,
// keeping its leading zeros and widening only when it overflows, as
// FileMaker does: "INV0009" is followed by "INV0010", "A99" by "A100".
// A value with no digit cannot be incremented.
func NextSerial(next string, increment int64) (string, error) {
	next = strings.TrimSpace(next)
	if n, ok := new(big.Int).SetString(next, 10); ok {
		return n.Add(n, big.NewInt(increment)).String(), nil
	}
	end := strings.LastIndexFunc(next, isDigit)
	if end < 0 {
		return "", fmt.Errorf("%w: the next serial value %q has no number to increment", ErrInvalid, next)
	}
	start := end
	for start > 0 && isDigit(rune(next[start-1])) {
		start--
	}
	digits := next[start : end+1]
	n, _ := new(big.Int).SetString(digits, 10)
	n.Add(n, big.NewInt(increment))
	text := n.String()
	if len(text) < len(digits) {
		text = strings.Repeat("0", len(digits)-len(text)) + text
	}
	return next[:start] + text + next[end+1:], nil
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// WithSerialNext is the stored options with serial_next set to next, every
// other key kept as it is.
func WithSerialNext(stored *string, next string) (string, error) {
	obj := map[string]json.RawMessage{}
	if stored != nil && strings.HasPrefix(strings.TrimSpace(*stored), "{") {
		if err := json.Unmarshal([]byte(*stored), &obj); err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	encoded, _ := json.Marshal(next)
	obj["serial_next"] = encoded
	out, err := json.Marshal(obj)
	return string(out), err
}

// HasKey reports whether the stored options name key at all.
func HasKey(stored *string, key string) bool {
	if stored == nil {
		return false
	}
	obj := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(*stored), &obj) != nil {
		return false
	}
	_, ok := obj[key]
	return ok
}

// Encode writes options as a stored JSON object, the keys the server reads
// only: what a migration and the tests produce.
func Encode(o Options) string {
	obj := map[string]interface{}{}
	b, _ := json.Marshal(o)
	_ = json.Unmarshal(b, &obj)
	if o.SerialEnabled {
		next, increment := o.SerialNext, o.SerialIncrement
		if next == "" {
			next = "1"
		}
		if increment < 1 {
			increment = 1
		}
		obj["serial_next"] = next
		obj["serial_increment"] = increment
	}
	if o.CalculatedEnabled {
		obj["calculated_skip_if_empty"] = o.SkipIfEmpty
	}
	out, _ := json.Marshal(obj)
	return string(out)
}
