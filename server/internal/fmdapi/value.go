package fmdapi

import (
	"math"
	"sort"
	"strconv"
)

// Kind is the JavaScript type of a value JSON.parse produced.
type Kind uint8

// The kinds of Value.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// Value is a request body value as FileMaker Server's front end holds it
// after JSON.parse: a JavaScript value, objects in Object.keys order.
type Value struct {
	Kind Kind
	Bool bool
	// Num is the number JSON.parse produced, and Text its source spelling.
	Num float64
	// Text is a string's content, or a number's source spelling.
	Text    string
	Elems   []Value
	Members []Member
}

// Member is one property of an object Value.
type Member struct {
	Key   string
	Value Value
}

// Get returns an object's property.
func (v Value) Get(key string) (Value, bool) {
	for _, m := range v.Members {
		if m.Key == key {
			return m.Value, true
		}
	}
	return Value{}, false
}

// Keys returns an object's keys in Object.keys order.
func (v Value) Keys() []string {
	keys := make([]string, len(v.Members))
	for i, m := range v.Members {
		keys[i] = m.Key
	}
	return keys
}

// SchemaType names the type of v as FileMaker Server's request validator
// does in "Expected type X but found type Y": a whole number is an integer.
func (v Value) SchemaType() string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindBool:
		return "boolean"
	case KindNumber:
		if v.Num == math.Trunc(v.Num) && !math.IsInf(v.Num, 0) {
			return "integer"
		}
		return "number"
	case KindString:
		return "string"
	case KindArray:
		return "array"
	default:
		return "object"
	}
}

// jsString is the value as JavaScript's String() spells it, for the kinds
// a numeric parameter may arrive as.
func (v Value) jsString() (string, bool) {
	switch v.Kind {
	case KindString:
		return v.Text, true
	case KindNumber:
		return formatJSNumber(v.Num), true
	}
	return "", false
}

// objectBuilder collects the properties JSON.parse defines on an object: a
// repeated key keeps its first position and takes the last value.
type objectBuilder struct {
	members []Member
	index   map[string]int
}

func (b *objectBuilder) set(key string, v Value) {
	if i, ok := b.index[key]; ok {
		b.members[i].Value = v
		return
	}
	if b.index == nil {
		b.index = map[string]int{}
	}
	b.index[key] = len(b.members)
	b.members = append(b.members, Member{Key: key, Value: v})
}

// build orders the properties as Object.keys lists them: array indices
// first, ascending, then the other keys in creation order.
func (b *objectBuilder) build() []Member {
	out := b.members
	if out == nil {
		out = []Member{}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aok := arrayIndex(out[i].Key)
		bi, bok := arrayIndex(out[j].Key)
		switch {
		case aok && bok:
			return ai < bi
		default:
			return aok && !bok
		}
	})
	return out
}

// arrayIndex reports whether key is a canonical array index, 0 to 2^32-2.
func arrayIndex(key string) (uint32, bool) {
	if key == "" || len(key) > 10 || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n > math.MaxUint32-1 {
		return 0, false
	}
	return uint32(n), true
}
