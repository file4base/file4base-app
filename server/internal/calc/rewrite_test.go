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
