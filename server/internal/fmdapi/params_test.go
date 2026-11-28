package fmdapi

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func str(s string) Value { return Value{Kind: KindString, Text: s} }

func num(src string) Value {
	v, msg := parseJSON(units(src))
	if msg != nil || v.Kind != KindNumber {
		panic("not a number: " + src)
	}
	return v
}

func units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+r>>10), uint16(0xDC00+r&0x3FF))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

func TestPositiveInteger(t *testing.T) {
	for _, c := range []struct {
		v    Value
		want int64 // 0: refused
	}{
		{str("1"), 1}, {num("4"), 4}, {str("4"), 4}, {str("1.5"), 1}, {num("1.5"), 1},
		{str(" 7"), 7}, {str("12abc"), 12},
		{num("1000000000000000000"), 1e18},
		{str("99999999999999999999"), math.MaxInt64},
		{num("1e400"), 0}, // Infinity: parseInt("Infinity") is NaN
		{str("0"), 0}, {num("0"), 0}, {str("-1"), 0}, {str("-5"), 0}, {str("abc"), 0}, {str(""), 0},
		{Value{Kind: KindNull}, 0}, {Value{Kind: KindBool, Bool: true}, 0}, {Value{Kind: KindArray}, 0},
	} {
		got, err := positiveInteger("_limit", c.v)
		if c.want == 0 {
			if err == nil || err.Message != "The _limit must be an integer with value greater than 0." || err.Status != 400 || err.Code != "960" || !err.Front {
				t.Errorf("positiveInteger(%+v) = %d, %v; want the recorded refusal", c.v, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("positiveInteger(%+v) = %d, %v; want %d", c.v, got, err, c.want)
		}
	}
}

func TestDateFormats(t *testing.T) {
	for _, c := range []struct {
		v    Value
		want int // -1: refused
	}{
		{num("0"), 0}, {num("1"), 1}, {num("2"), 2}, {str("2"), 2}, {str("3"), -1}, {str("iso"), -1},
		{num("-1"), -1}, {Value{Kind: KindNull}, -1},
	} {
		got, err := dateFormats(c.v)
		if c.want < 0 {
			if err == nil || err.Message != "The dateformats must be an integer with value 0, 1 or 2." {
				t.Errorf("dateFormats(%+v) = %d, %v; want the recorded refusal", c.v, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("dateFormats(%+v) = %d, %v; want %d", c.v, got, err, c.want)
		}
	}
}

func TestUnknownParametersKeepObjectKeysOrder(t *testing.T) {
	body, msg := parseJSON(units(`{"query":[],"zeta":1,"10":1,"alpha":1,"2":1,"zeta":2}`))
	if msg != nil {
		t.Fatal(string(rune(msg[0])))
	}
	err := unknownParameters(body.Keys(), func(k string) bool { return k == "query" })
	if err == nil || err.Message != "Unknown parameter(s): 2,10,zeta,alpha" {
		t.Fatalf("got %v", err)
	}
	if unknownParameters([]string{"query"}, func(string) bool { return true }) != nil {
		t.Fatal("known keys refused")
	}
}

func TestParseDatabaseAliases(t *testing.T) {
	got, err := ParseDatabaseAliases(" sales , Sales Archive 2024 = archive ,, ")
	want := []DatabaseAlias{{"sales", "sales"}, {"Sales Archive 2024", "archive"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, bad := range []string{"a,A", "=db", "name=", "x=a,X=b"} {
		if _, err := ParseDatabaseAliases(bad); err == nil {
			t.Errorf("ParseDatabaseAliases(%q) accepted", bad)
		}
	}
	cfg := Config{Databases: want}
	if a, ok := cfg.alias("SALES ARCHIVE 2024"); !ok || a.Database != "archive" {
		t.Errorf("alias in another case: %+v %v", a, ok)
	}
	if _, ok := cfg.alias("sales.fmp12"); ok {
		t.Error("alias with an extension matched")
	}
}

func TestParseQuery(t *testing.T) {
	q := parseQuery("b=1&a=x+y&b=2&%zz=%E9&c=%C3%A9&&d&5=n&script.param=a%2Bb")
	var got []string
	for _, m := range q.Members {
		got = append(got, m.Key+"="+m.Value.Text)
	}
	want := "5=n b=1 a=x y %zz=%E9 c=é d= script.param=a+b"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %q, want %q", strings.Join(got, " "), want)
	}
}
