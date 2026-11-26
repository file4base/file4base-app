package calc

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
)

// Two sort keys compare as CompareText compares their texts.
func TestSortKeyComparesAsCompareText(t *testing.T) {
	alphabet := []string{"a", "A", "b", "B", "e", "E", "s", "S", "z", "Z", " ", "-", "0", "9", "\x00", "\x01",
		"é", "É", "è", "ß", "ẞ", "ı", "İ", "i", "I", "æ", "Æ", "œ", "ø", "ü", "́", "￿", "日"}
	r := rand.New(rand.NewSource(7))
	random := func(ascii bool) string {
		var b strings.Builder
		for n := r.Intn(6); n > 0; n-- {
			c := alphabet[r.Intn(len(alphabet))]
			if ascii {
				c = string(rune(r.Intn(128)))
			}
			b.WriteString(c)
		}
		return b.String()
	}
	sign := func(n int) int {
		switch {
		case n < 0:
			return -1
		case n > 0:
			return 1
		}
		return 0
	}
	for i := 0; i < 200000; i++ {
		a, b := random(i%2 == 0), random(i%3 == 0)
		switch i % 7 {
		case 0:
			b = strings.ToUpper(a)
		case 1:
			b = a + random(false)
		}
		if got, want := sign(bytes.Compare(SortKey(a), SortKey(b))), sign(CompareText(a, b)); got != want {
			t.Fatalf("%q vs %q: key order %d, CompareText %d", a, b, got, want)
		}
	}
}
