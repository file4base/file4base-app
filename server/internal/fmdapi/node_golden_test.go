package fmdapi

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"unicode/utf16"
)

// nodeGolden is testdata/node-golden.json: what Node.js does with the
// inputs, recorded by testdata/node-golden.js.
type nodeGolden struct {
	Node  string `json:"node"`
	Parse []struct {
		In  []uint16 `json:"in"`
		OK  *string  `json:"ok"`
		Err []uint16 `json:"err"`
	} `json:"parse"`
	Body []struct {
		In  []uint16 `json:"in"`
		OK  *string  `json:"ok"`
		Err []uint16 `json:"err"`
	} `json:"body"`
	Numbers []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"numbers"`
	Strings []struct {
		In  []uint16 `json:"in"`
		Out string   `json:"out"`
	} `json:"strings"`
	ParseInt []struct {
		In  []uint16 `json:"in"`
		Out string   `json:"out"`
	} `json:"parseInt"`
	Base64 []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"base64"`
}

func loadNodeGolden(t *testing.T) nodeGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/node-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g nodeGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// show prints UTF-16 for a failure message.
func show(u []uint16) string { return strconv.Quote(string(utf16.Decode(u))) }

// stringify is JSON.stringify of a parsed value.
func stringify(v Value) string {
	var buf []byte
	var walk func(Value)
	walk = func(v Value) {
		switch v.Kind {
		case KindNull:
			buf = append(buf, "null"...)
		case KindBool:
			buf = strconv.AppendBool(buf, v.Bool)
		case KindNumber:
			buf = appendJSON(buf, v.Num)
		case KindString:
			buf = appendJSString(buf, v.Text)
		case KindArray:
			buf = append(buf, '[')
			for i, e := range v.Elems {
				if i > 0 {
					buf = append(buf, ',')
				}
				walk(e)
			}
			buf = append(buf, ']')
		case KindObject:
			buf = append(buf, '{')
			for i, m := range v.Members {
				if i > 0 {
					buf = append(buf, ',')
				}
				buf = appendJSString(buf, m.Key)
				buf = append(buf, ':')
				walk(m.Value)
			}
			buf = append(buf, '}')
		}
	}
	walk(v)
	return string(buf)
}

func TestJSONParseMatchesNode(t *testing.T) {
	g := loadNodeGolden(t)
	for _, c := range g.Parse {
		v, msg := parseJSON(c.In)
		checkParsed(t, "JSON.parse", c.In, c.OK, c.Err, v, msg)
	}
}

func TestBodyParserMatchesNode(t *testing.T) {
	g := loadNodeGolden(t)
	for _, c := range g.Body {
		v, msg := parseBody(c.In)
		checkParsed(t, "body-parser", c.In, c.OK, c.Err, v, msg)
	}
}

func checkParsed(t *testing.T, what string, in []uint16, ok *string, wantErr []uint16, v Value, msg []uint16) {
	t.Helper()
	switch {
	case ok != nil && msg != nil:
		t.Errorf("%s(%s): error %s, want %s", what, show(in), show(msg), *ok)
	case ok != nil && stringify(v) != *ok:
		t.Errorf("%s(%s) = %s, want %s", what, show(in), stringify(v), *ok)
	case ok == nil && show(msg) != show(wantErr):
		t.Errorf("%s(%s): error %s, want %s", what, show(in), show(msg), show(wantErr))
	}
}

func TestNumberSpellingMatchesNode(t *testing.T) {
	for _, c := range loadNodeGolden(t).Numbers {
		f, err := strconv.ParseFloat(c.In, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := formatJSNumber(f); got != c.Out {
			t.Errorf("String(%s) = %q, want %q", c.In, got, c.Out)
		}
	}
}

func TestStringEscapingMatchesNode(t *testing.T) {
	for _, c := range loadNodeGolden(t).Strings {
		if got := string(Marshal(wtf8(c.In))); got != c.Out {
			t.Errorf("JSON.stringify(%s) = %s, want %s", show(c.In), got, c.Out)
		}
	}
}

func TestParseIntMatchesNode(t *testing.T) {
	for _, c := range loadNodeGolden(t).ParseInt {
		n, ok := parseIntJS(wtf8(c.In))
		got := "NaN"
		if ok {
			got = formatJSNumber(n)
		}
		if got != c.Out {
			t.Errorf("parseInt(%s) = %s, want %s", show(c.In), got, c.Out)
		}
	}
}

func TestBase64MatchesNode(t *testing.T) {
	for _, c := range loadNodeGolden(t).Base64 {
		if got := hex.EncodeToString(decodeBase64Node(c.In)); got != c.Out {
			t.Errorf("Buffer.from(%q, 'base64') = %s, want %s", c.In, got, c.Out)
		}
	}
}
