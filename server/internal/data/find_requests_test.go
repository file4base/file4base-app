package data

import "testing"

// #34 — an omitting find request was OR-ed into the result, so "paid in 2011,
// except in March" meant "paid in 2011 OR not in March", which is almost every
// record. An omitting request must subtract from what the others found.

func TestFindWhereUnionsIncludingRequests(t *testing.T) {
	got := buildFindWhere([]string{"(a)", "(b)"}, nil)
	want := "WHERE ((a) OR (b))"
	if got != want {
		t.Fatalf("where = %q, want %q", got, want)
	}
}

func TestFindWhereSubtractsOmittingRequests(t *testing.T) {
	got := buildFindWhere([]string{"(year = 2011)"}, []string{"(month = 3)"})
	want := "WHERE ((year = 2011)) AND NOT ((month = 3))"
	if got != want {
		t.Fatalf("where = %q, want %q", got, want)
	}
}

func TestFindWhereWithOnlyOmittingRequestsStartsFromEveryRecord(t *testing.T) {
	got := buildFindWhere(nil, []string{"(city = 'New York')"})
	want := "WHERE NOT ((city = 'New York'))"
	if got != want {
		t.Fatalf("where = %q, want %q", got, want)
	}
}

func TestFindWhereWithNoRequestsIsEmpty(t *testing.T) {
	if got := buildFindWhere(nil, nil); got != "" {
		t.Fatalf("where = %q, want empty", got)
	}
}

func TestFindWhereSubtractsTheUnionOfEveryOmittingRequest(t *testing.T) {
	got := buildFindWhere([]string{"(a)"}, []string{"(b)", "(c)"})
	// Not "NOT (b) AND NOT (c)" spelled out, but the same thing: a record is
	// dropped when it matches any omitting request.
	want := "WHERE ((a)) AND NOT ((b) OR (c))"
	if got != want {
		t.Fatalf("where = %q, want %q", got, want)
	}
}
