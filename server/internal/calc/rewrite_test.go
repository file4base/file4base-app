package calc

import (
	"reflect"
	"strings"
	"testing"
)

func TestRewriteRenamesReferencesAndNormalisesNumbers(t *testing.T) {
	fr, _ := LocaleFor("fr-FR")
	opts := Options{
		Fields: []string{"Prix HT €", "Quantité", "Note"},
		Tables: map[string][]string{"Lignes client": {"Montant", "Date de début"}},
		Locale: &fr,
	}
	cols := map[string]string{"Prix HT €": "prix_ht", "Quantité": "quantite", "Lignes client::Montant": "lignes_client::montant"}
	src := "Let ( [ x = Prix HT € * 1,2 ; y = \"Prix HT € 1,5\" ] ; // remise 0,5\n x * Quantité - Sum ( Lignes client::Montant ) + Note + 0,05 )"
	got, unresolved, err := Rewrite(src, opts, func(table, field string) (string, bool) {
		key := field
		if table != "" {
			key = table + "::" + field
		}
		c, ok := cols[key]
		return c, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Let ( [ x = prix_ht * 1.2 ; y = \"Prix HT € 1,5\" ] ; // remise 0,5\n x * quantite - Sum ( lignes_client::montant ) + Note + 0.05 )"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if !reflect.DeepEqual(unresolved, []Reference{{Field: "Note"}}) {
		t.Fatalf("unresolved = %v", unresolved)
	}
	if _, err := ParseWith(got, Options{}); err != nil && !strings.Contains(err.Error(), "Note") {
		t.Fatalf("the rewritten formula does not parse: %v", err)
	}
}

func TestQuoteName(t *testing.T) {
	for in, want := range map[string]string{"prix_ht": "prix_ht", "Prix HT": "${Prix HT}", "A + B": "${A + B}", "and": "${and}", "2x": "${2x}"} {
		if got := QuoteName(in); got != want {
			t.Errorf("QuoteName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReplaceSelfWritesTheObjectsField(t *testing.T) {
	opts := Options{Tables: map[string][]string{"Visit lines": {"Due date", "Amount"}}}
	with := "${Visit lines}::${Due date}"
	for src, want := range map[string]string{
		"Self≥1 and Self≤5":                          "${Visit lines}::${Due date}≥1 and ${Visit lines}::${Due date}≤5",
		"PatternCount(Self; \"Self\") > 0 // Self":   "PatternCount(${Visit lines}::${Due date}; \"Self\") > 0 // Self",
		"(Get(CurrentDate) - GetAsDate(SELF)) < 4":   "(Get(CurrentDate) - GetAsDate(${Visit lines}::${Due date})) < 4",
		"Let ( x = self ; x > Visit lines::Amount )": "Let ( x = ${Visit lines}::${Due date} ; x > Visit lines::Amount )",
		"Visit lines::Amount > 3":                    "Visit lines::Amount > 3",
	} {
		got, err := ReplaceSelf(src, opts, with)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got != want {
			t.Errorf("ReplaceSelf(%q) = %q, want %q", src, got, want)
		}
		if _, err := ParseWith(got, opts); err != nil {
			t.Errorf("%q does not parse: %v", got, err)
		}
	}
	if _, err := ReplaceSelf("Self >", opts, with); err == nil {
		t.Fatal("a formula that cannot be read is an error")
	}
}

func TestQuoteQualifiedNameReadsBack(t *testing.T) {
	for _, c := range [][3]string{
		{"Visits", "score", "Visits::score"},
		{"Visits", "pi", "${Visits}::${pi}"},
		{"Order Lines", "amount", "${Order Lines}::amount"},
		{"Visits", "Due date", "${Visits}::${Due date}"},
	} {
		got := QuoteQualifiedName(c[0], c[1])
		if got != c[2] {
			t.Errorf("QuoteQualifiedName(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
		expr, err := ParseWith(got+" > 1", Options{})
		if err != nil {
			t.Fatalf("%q does not parse: %v", got, err)
		}
		if refs := expr.RelatedFields(); len(refs) != 1 || refs[0].Table != c[0] || refs[0].Field != c[1] {
			t.Errorf("%q reads %v", got, refs)
		}
	}
}
