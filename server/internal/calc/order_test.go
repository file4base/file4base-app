package calc_test

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/calc"
)

func TestOrderPutsADependencyFirst(t *testing.T) {
	// total reads subtotal, which reads price (an ordinary field).
	order, err := calc.Order(map[string][]string{
		"subtotal": {"price", "quantity"},
		"total":    {"subtotal", "tax_rate"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "subtotal" || order[1] != "total" {
		t.Fatalf("order = %v, want [subtotal total]", order)
	}
}

func TestOrderIgnoresOrdinaryFields(t *testing.T) {
	order, err := calc.Order(map[string][]string{"full_name": {"first_name", "last_name"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 1 || order[0] != "full_name" {
		t.Fatalf("order = %v, want [full_name]", order)
	}
}

func TestOrderIsStable(t *testing.T) {
	reads := map[string][]string{"c": {"price"}, "a": {"price"}, "b": {"price"}}
	first, err := calc.Order(reads)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := calc.Order(reads)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("order changed between runs: %v then %v", first, again)
			}
		}
	}
}

func TestOrderRejectsAFieldThatReadsItself(t *testing.T) {
	_, err := calc.Order(map[string][]string{"total": {"total", "price"}})
	var ce *calc.CycleError
	if err == nil {
		t.Fatal("a field reading itself must be rejected")
	}
	if !asCycle(err, &ce) {
		t.Fatalf("got %T, want *calc.CycleError", err)
	}
	if !strings.Contains(err.Error(), "total") {
		t.Errorf("the error should name the field: %v", err)
	}
}

func TestOrderRejectsARingOfFields(t *testing.T) {
	_, err := calc.Order(map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	})
	var ce *calc.CycleError
	if err == nil || !asCycle(err, &ce) {
		t.Fatalf("got %v (%T), want a *calc.CycleError", err, err)
	}
	if len(ce.Fields) < 3 {
		t.Errorf("the cycle should name every field in the ring, got %v", ce.Fields)
	}
}

func TestOrderOfNothingIsNothing(t *testing.T) {
	order, err := calc.Order(nil)
	if err != nil || len(order) != 0 {
		t.Fatalf("Order(nil) = %v, %v", order, err)
	}
}

func asCycle(err error, target **calc.CycleError) bool {
	if ce, ok := err.(*calc.CycleError); ok {
		*target = ce
		return true
	}
	return false
}
