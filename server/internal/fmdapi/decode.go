package fmdapi

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// The checks of the read parameters, in the two places FileMaker Server
// makes them: the request validator types the JSON body against the
// published schema, then the front end refuses unknown parameters and
// reads the numeric ones. Both run before the engine sees the token.

// propType is a body property's type in the published schema.
type propType uint8

const (
	propString      propType = iota + 1
	propObjectArray          // array of objects
	propStringArray          // array of strings
	propIntOrString          // oneOf integer, string
)

// findProps are the properties of a _find body.
var findProps = map[string]propType{
	"query":                   propObjectArray,
	"sort":                    propObjectArray,
	"script":                  propString,
	"script.param":            propString,
	"script.prerequest":       propString,
	"script.prerequest.param": propString,
	"script.presort":          propString,
	"script.presort.param":    propString,
	"offset":                  propIntOrString,
	"limit":                   propIntOrString,
	"layout.response":         propString,
	"portal":                  propStringArray,
	"dateformats":             propIntOrString,
}

// findPortalPaging are the prefixes of a portal's paging in a _find body:
// offset.<portal> and limit.<portal>, without the query's underscore.
var findPortalPaging = []string{"offset.", "limit."}

// readQuery are the query parameters of a record read.
var readQuery = map[string]bool{
	"script": true, "script.param": true,
	"script.prerequest": true, "script.prerequest.param": true,
	"script.presort": true, "script.presort.param": true,
	"layout.response": true, "portal": true, "dateformats": true,
}

// rangeQuery adds the paging and sort of a record range.
var rangeQuery = map[string]bool{"_offset": true, "_limit": true, "_sort": true}

// queryPortalPaging are the prefixes of a portal's paging in a query.
// FileMaker Server also lets limit.<portal> through, unlike offset.<portal>:
// recorded, and kept.
var queryPortalPaging = []string{"_offset.", "_limit.", "limit."}

// checkFindBody is the request validator's and the front end's check of a
// _find body. A request without a body has an empty one.
func checkFindBody(body *Value) *Error {
	b := Value{Kind: KindObject}
	if body != nil {
		b = *body
	}
	if err := checkSchema(b, findProps, "query"); err != nil {
		return err
	}
	if err := unknownParameters(b.Keys(), func(k string) bool {
		_, ok := findProps[k]
		return ok || hasAnyPrefix(k, findPortalPaging)
	}); err != nil {
		return err
	}
	for _, name := range []string{"offset", "limit"} {
		if v, ok := b.Get(name); ok {
			if _, err := positiveInteger(name, v); err != nil {
				return err
			}
		}
	}
	return checkPagingAndFormats(b, findPortalPaging)
}

// checkSchema types an object body against props, as the request
// validator does: the root, the required property, then each property in
// the body's order.
func checkSchema(b Value, props map[string]propType, required string) *Error {
	if b.Kind != KindObject {
		return parameterError("Expected type object but found type " + b.SchemaType())
	}
	if _, ok := b.Get(required); !ok {
		return frontError(http.StatusBadRequest, "10", "Missing required property: "+required)
	}
	for _, m := range b.Members {
		if err := checkProp(m.Key, props[m.Key], m.Value); err != nil {
			return err
		}
	}
	return nil
}

func checkProp(name string, t propType, v Value) *Error {
	switch t {
	case propString:
		if v.Kind != KindString {
			return schemaTypeError(name, "string", v)
		}
	case propObjectArray, propStringArray:
		if v.Kind != KindArray {
			return schemaTypeError(name, "array", v)
		}
		item, kind := "object", KindObject
		if t == propStringArray {
			item, kind = "string", KindString
		}
		for _, e := range v.Elems {
			if e.Kind != kind {
				return schemaTypeError(name, item, e)
			}
		}
	case propIntOrString:
		if v.Kind != KindString && v.SchemaType() != "integer" {
			return parameterError("'" + name + "': Data does not match any schemas from 'oneOf'")
		}
	}
	return nil
}

// checkRangeQuery is the front end's check of the query of a record range,
// GET .../layouts/{layout}/records.
func checkRangeQuery(req *request) *Error {
	q := parseQuery(req.URL.RawQuery)
	if err := unknownParameters(q.Keys(), func(k string) bool {
		return readQuery[k] || rangeQuery[k] || hasAnyPrefix(k, queryPortalPaging)
	}); err != nil {
		return err
	}
	for _, name := range []string{"_offset", "_limit"} {
		if v, ok := q.Get(name); ok {
			if _, err := positiveInteger(name, v); err != nil {
				return err
			}
		}
	}
	return checkPagingAndFormats(q, queryPortalPaging)
}

// checkRecordQuery is the front end's check of a record read,
// GET .../layouts/{layout}/records/{recordId}.
func checkRecordQuery(req *request) *Error {
	id, ok := readInteger(Value{Kind: KindString, Text: req.params["recordId"]})
	if !ok || !(id > 0) {
		return parameterError("recordId must be an integer with value greater than 0.")
	}
	if id >= 1<<63 {
		// FileMaker Server's front end fails on a record ID beyond a 64-bit
		// integer (recorded for 99999999999999999999; the exact bound is
		// assumed).
		return frontError(http.StatusInternalServerError, "3", "x[Symbol.for(...)] is not a function")
	}
	q := parseQuery(req.URL.RawQuery)
	if err := unknownParameters(q.Keys(), func(k string) bool {
		return readQuery[k] || hasAnyPrefix(k, queryPortalPaging)
	}); err != nil {
		return err
	}
	return checkPagingAndFormats(q, queryPortalPaging)
}

// checkPagingAndFormats reads the portal paging parameters, in the order
// they come, then dateformats.
func checkPagingAndFormats(params Value, paging []string) *Error {
	for _, m := range params.Members {
		if hasAnyPrefix(m.Key, paging) {
			if _, err := positiveInteger(m.Key, m.Value); err != nil {
				return err
			}
		}
	}
	if v, ok := params.Get("dateformats"); ok {
		if _, err := dateFormats(v); err != nil {
			return err
		}
	}
	return nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// parseQuery reads a query string as Express does, into an object of
// strings in Object.keys order: '+' is a space, an escape that does not
// decode stays as sent, and a repeated parameter keeps its first value.
func parseQuery(raw string) Value {
	var b objectBuilder
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		k = decodeQueryComponent(k)
		if seen[k] {
			continue
		}
		seen[k] = true
		b.set(k, Value{Kind: KindString, Text: decodeQueryComponent(v)})
	}
	return Value{Kind: KindObject, Members: b.build()}
}

func decodeQueryComponent(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	if d, err := url.PathUnescape(s); err == nil && utf8.ValidString(d) {
		return d
	}
	return s
}
