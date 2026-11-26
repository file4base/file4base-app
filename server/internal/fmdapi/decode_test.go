package fmdapi

import (
	"net/http/httptest"
	"testing"
)

// The replies below are those recorded in testdata/dataapi; those marked
// "assumed" are not.

func TestCheckFindBody(t *testing.T) {
	for _, c := range []struct {
		body, status, code, message string
	}{
		{`{"query":[{"Code":"C*"}],"offset":"3","limit":4,"layout.response":"L","dateformats":2,"portal":["P"],"offset.P":2,"limit.P":"1"}`, "", "", ""},
		{`{"query":[{"Code":"C*"}],"limit":1000000000000000000,"offset":1000000000000000001}`, "", "", ""},
		{`{"limit":1}`, "400", "10", "Missing required property: query"},
		{``, "400", "10", "Missing required property: query"},
		{`{"query":{"Name":"Alpha"}}`, "400", "960", "'query': Expected type array but found type object"},
		{`{"query":[{"Rating":"1"}],"script":"S","script.param":null}`, "400", "960", "'script.param': Expected type string but found type null"},
		{`{"query":[{"ProductCode":"P001"}],"lmit":1}`, "400", "960", "Unknown parameter(s): lmit"},
		{`{"query":[{"CustomerID":"9"}],"portal":["OrdersPortal"],"_offset.OrdersPortal":2,"_limit.OrdersPortal":1}`, "400", "960", "Unknown parameter(s): _offset.OrdersPortal,_limit.OrdersPortal"},
		{`{"query":[{"Code":"C*"}],"offset":0}`, "400", "960", "The offset must be an integer with value greater than 0."},
		{`{"query":[{"Code":"C*"}],"limit":0}`, "400", "960", "The limit must be an integer with value greater than 0."},
		{`{"query":[{"ProductCode":"P00*"}],"limit":"two"}`, "400", "960", "The limit must be an integer with value greater than 0."},
		// assumed
		{`[]`, "400", "960", "Expected type object but found type array"},
		{`{"query":["x"]}`, "400", "960", "'query': Expected type object but found type string"},
		{`{"query":[],"limit":1.5}`, "400", "960", "'limit': Data does not match any schemas from 'oneOf'"},
		{`{"query":[],"offset.P":0}`, "400", "960", "The offset.P must be an integer with value greater than 0."},
		{`{"query":[],"dateformats":"3"}`, "400", "960", "The dateformats must be an integer with value 0, 1 or 2."},
	} {
		var body *Value
		if c.body != "" {
			v, msg := parseBody(units(c.body))
			if msg != nil {
				t.Fatalf("%s: %s", c.body, show(msg))
			}
			body = &v
		}
		checkError(t, c.body, checkFindBody(body), c.status, c.code, c.message)
	}
}

func TestCheckRangeQuery(t *testing.T) {
	for _, c := range []struct {
		query, status, code, message string
	}{
		{"_limit=1&_offset=1.5&_sort=x&portal=%5B%22P%22%5D&_offset.P=2&_limit.P=1&layout.response=L&dateformats=2&script=S&script.param=p", "", "", ""},
		{"_limit=99999999999999999999", "", "", ""},
		{"_limit=1&_offset=9&portal=%5B%22OrdersPortal%22%5D&offset.OrdersPortal=2&limit.OrdersPortal=1", "400", "960", "Unknown parameter(s): offset.OrdersPortal"},
		{"_limit=1&_script=API_Success", "400", "960", "Unknown parameter(s): _script"},
		{"_limit=1&foo=bar", "400", "960", "Unknown parameter(s): foo"},
		{"limit=1", "400", "960", "Unknown parameter(s): limit"},
		{"_offset=0&_limit=1", "400", "960", "The _offset must be an integer with value greater than 0."},
		{"_offset=-1&_limit=1", "400", "960", "The _offset must be an integer with value greater than 0."},
		{"_limit=", "400", "960", "The _limit must be an integer with value greater than 0."},
		{"_limit=abc", "400", "960", "The _limit must be an integer with value greater than 0."},
		{"_limit=1&portal=%5B%22OrdersPortal%22%5D&_offset.OrdersPortal=0", "400", "960", "The _offset.OrdersPortal must be an integer with value greater than 0."},
		{"_limit=1&portal=%5B%22OrdersPortal%22%5D&_limit.OrdersPortal=0", "400", "960", "The _limit.OrdersPortal must be an integer with value greater than 0."},
		{"_limit=1&dateformats=3", "400", "960", "The dateformats must be an integer with value 0, 1 or 2."},
		{"_limit=1&dateformats=iso", "400", "960", "The dateformats must be an integer with value 0, 1 or 2."},
	} {
		req := &request{Request: httptest.NewRequest("GET", "/fmi/data/v1/databases/D/layouts/L/records?"+c.query, nil)}
		checkError(t, c.query, checkRangeQuery(req), c.status, c.code, c.message)
	}
}

func TestCheckRecordQuery(t *testing.T) {
	for _, c := range []struct {
		id, query, status, code, message string
	}{
		{"1", "portal=%5B%22P%22%5D&_limit.P=2&layout.response=L&dateformats=1", "", "", ""},
		{"0", "", "400", "960", "recordId must be an integer with value greater than 0."},
		{"-1", "", "400", "960", "recordId must be an integer with value greater than 0."},
		{"abc", "", "400", "960", "recordId must be an integer with value greater than 0."},
		{"99999999999999999999", "", "500", "3", "x[Symbol.for(...)] is not a function"},
		{"9000000000000000000", "", "", "", ""},
		// assumed
		{"1", "_limit=1", "400", "960", "Unknown parameter(s): _limit"},
	} {
		req := &request{
			Request: httptest.NewRequest("GET", "/fmi/data/v1/databases/D/layouts/L/records/"+c.id+"?"+c.query, nil),
			params:  map[string]string{"recordId": c.id},
		}
		checkError(t, c.id+"?"+c.query, checkRecordQuery(req), c.status, c.code, c.message)
	}
}

func checkError(t *testing.T, input string, err *Error, status, code, message string) {
	t.Helper()
	if status == "" {
		if err != nil {
			t.Errorf("%s: refused with %v", input, err)
		}
		return
	}
	if err == nil {
		t.Errorf("%s: accepted, want %s %s %q", input, status, code, message)
		return
	}
	if got := itoa(err.Status); got != status || err.Code != code || err.Message != message || !err.Front {
		t.Errorf("%s: got %d %s %q (front %v), want %s %s %q", input, err.Status, err.Code, err.Message, err.Front, status, code, message)
	}
}

func itoa(n int) string { return formatJSNumber(float64(n)) }
