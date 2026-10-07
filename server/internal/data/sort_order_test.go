package data

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
)

// Sorting used to take a single field, so "by company, then by last name within
// each company" could not be expressed (#35).

func TestSortOrderPrefersTheListOverTheSingleField(t *testing.T) {
	opts := QueryOptions{
		SortBy:  "id",
		SortAsc: true,
		Sort: []SortField{
			{Field: "company"},
			{Field: "last_name", Descending: true},
		},
	}

	order := opts.SortOrder()
	if len(order) != 2 {
		t.Fatalf("got %d levels, want 2: %+v", len(order), order)
	}
	if order[0].Field != "company" || order[0].Descending {
		t.Errorf("first level = %+v, want company ascending", order[0])
	}
	if order[1].Field != "last_name" || !order[1].Descending {
		t.Errorf("second level = %+v, want last_name descending", order[1])
	}
}

func TestSortOrderFallsBackToTheSingleField(t *testing.T) {
	order := QueryOptions{SortBy: "last_name", SortAsc: false}.SortOrder()
	if len(order) != 1 || order[0].Field != "last_name" || !order[0].Descending {
		t.Fatalf("got %+v, want one descending last_name level", order)
	}

	if got := (QueryOptions{}).SortOrder(); len(got) != 0 {
		t.Fatalf("no sort options should give no order, got %+v", got)
	}
}

func TestSortOrderDropsBlankFields(t *testing.T) {
	order := QueryOptions{Sort: []SortField{{Field: " "}, {Field: "city"}}}.SortOrder()
	if len(order) != 1 || order[0].Field != "city" {
		t.Fatalf("got %+v, want only city", order)
	}
}

func testFields() fieldSet {
	return fieldSet{
		"id":        dbal.FieldTypeText,
		"company":   dbal.FieldTypeText,
		"last_name": dbal.FieldTypeText,
		"fee_paid":  dbal.FieldTypeNumber,
	}
}

func TestOrderByClauseOrdersByEveryLevelInTurn(t *testing.T) {
	clause, err := orderByClause(&postgres.PostgresDialect{}, testFields(), "customers", []SortField{
		{Field: "company"},
		{Field: "fee_paid", Descending: true},
		{Field: "last_name"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `ORDER BY "company" ASC, "fee_paid" DESC, "last_name" ASC`
	if clause != want {
		t.Fatalf("clause = %q, want %q", clause, want)
	}
}

func TestOrderByClauseIsEmptyWithoutAnOrder(t *testing.T) {
	clause, err := orderByClause(&postgres.PostgresDialect{}, testFields(), "customers", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clause != "" {
		t.Fatalf("clause = %q, want empty", clause)
	}
}

func TestOrderByClauseRejectsAFieldTheTableDoesNotHave(t *testing.T) {
	_, err := orderByClause(&postgres.PostgresDialect{}, testFields(), "customers", []SortField{
		{Field: "company"},
		{Field: "salary; DROP TABLE customers"},
	})
	if err == nil {
		t.Fatal("a field that is not registered must be rejected")
	}
}

func TestOrderByClauseIgnoresARepeatedField(t *testing.T) {
	clause, err := orderByClause(&postgres.PostgresDialect{}, testFields(), "customers", []SortField{
		{Field: "company"},
		{Field: "company", Descending: true},
		{Field: "last_name"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The second mention can never change the order, so it is dropped rather
	// than emitted as a contradictory level.
	want := `ORDER BY "company" ASC, "last_name" ASC`
	if clause != want {
		t.Fatalf("clause = %q, want %q", clause, want)
	}
}
