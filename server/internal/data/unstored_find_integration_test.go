package data_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unstoredFindFixture is companies whose unstored calculations read their
// customers, each beside a stored field holding what the calculation
// computes, so that a find, a sort or a summary on the calculation can be
// compared with the same one on a stored field.
type unstoredFindFixture struct {
	*relatedFixture
	names map[string]string // id -> company
}

// pairs are each unstored calculation and the stored field holding its value.
var unstoredPairs = [][2]string{
	{"total_fees", "stored_total"}, {"headline", "stored_headline"}, {"since", "stored_since"},
}

func newUnstoredFindFixture(t *testing.T) *unstoredFindFixture {
	t.Helper()
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return nil
	}
	t.Cleanup(func() { data.SetUnstoredFindLimits(data.DefaultUnstoredFindLimits) })
	f := &unstoredFindFixture{relatedFixture: newRelatedFixture(t, ctx, driver, false, false, ""), names: map[string]string{}}
	tables, err := f.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	tableID := map[string]string{}
	for _, tbl := range tables {
		tableID[tbl.Name] = tbl.ID
	}
	occurrences, err := f.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	customers := ""
	for _, o := range occurrences {
		if o.ID == f.customersOccurrence {
			customers = o.Name
		}
	}
	require.NotEmpty(t, customers)
	add := func(table, field string, kind dbal.AgnosticFieldType, formula *string) {
		t.Helper()
		_, err := f.schemaSvc.AddColumn(ctx, tableID[table], schema.ColumnMetadata{
			Name: field, DisplayName: field, FieldType: kind, IsNullable: true, CalculationFormula: formula,
		})
		require.NoError(t, err)
	}
	add(f.customers, "fee", dbal.FieldTypeNumber, nil)
	add(f.companies, "total_fees", dbal.FieldTypeCalculation, unstoredCalcJSON(fmt.Sprintf("Sum ( %s::fee )", customers), "Number"))
	add(f.companies, "headline", dbal.FieldTypeCalculation,
		unstoredCalcJSON(fmt.Sprintf(`company & ": " & %s::last_name`, customers), "Text"))
	add(f.companies, "since", dbal.FieldTypeCalculation,
		unstoredCalcJSON(fmt.Sprintf("If ( Count ( %s::fee ) > 0 ; Date ( 1 ; Count ( %s::fee ) ; 2026 ) ; \"\" )", customers, customers), "Date"))
	add(f.companies, "record_number", dbal.FieldTypeCalculation, unstoredCalcJSON("Get ( RecordID )", "Number"))
	// It names the field it reads only when it runs.
	add(f.companies, "by_name", dbal.FieldTypeCalculation, unstoredCalcJSON(`GetField ( "company" )`, "Text"))
	add(f.companies, "stored_total", dbal.FieldTypeNumber, nil)
	add(f.companies, "stored_headline", dbal.FieldTypeText, nil)
	add(f.companies, "stored_since", dbal.FieldTypeDate, nil)
	add(f.companies, "fees_total", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryTotal, "total_fees"))
	add(f.companies, "fees_average", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryAverage, "total_fees"))
	add(f.companies, "fees_highest", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryMaximum, "total_fees"))
	add(f.companies, "fees_count", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryCount, "total_fees"))
	add(f.companies, "stored_fees_total", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryTotal, "stored_total"))
	add(f.companies, "stored_fees_average", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryAverage, "stored_total"))
	add(f.companies, "stored_fees_highest", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryMaximum, "stored_total"))
	add(f.companies, "stored_fees_count", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryCount, "stored_total"))

	for _, c := range []struct {
		company string
		fees    []interface{}
	}{
		{"Alpha Bakery", []interface{}{100, 25.5}},
		{"beta Books", []interface{}{300}},
		{"Gamma Garage", nil},
		{"Delta Deli", []interface{}{50, 50}},
		{"Épsilon Bakery", []interface{}{nil}},
		{"Zeta Bakery", []interface{}{-12.25, 1000, 0}},
	} {
		row, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": c.company})
		require.NoError(t, err)
		f.names[fmt.Sprint(row["id"])] = c.company
		for i, fee := range c.fees {
			_, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{
				"company": c.company, "last_name": fmt.Sprintf("%c%d", 'Q'-i, i), "fee": fee,
			})
			require.NoError(t, err)
		}
	}
	// The stored copies hold what the calculations compute.
	all, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, all, 6)
	for _, row := range all {
		copied := map[string]interface{}{}
		for _, pair := range unstoredPairs {
			v := row[pair[0]]
			if at, ok := v.(time.Time); ok {
				v = at.Format("2006-01-02")
			}
			copied[pair[1]] = v
		}
		_, err := f.dataSvc.UpdateRow(ctx, f.companies, fmt.Sprint(row["id"]), copied)
		require.NoError(t, err)
	}
	return f
}

func (f *unstoredFindFixture) companiesOf(rows []map[string]interface{}) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, f.names[fmt.Sprint(r["id"])])
	}
	return out
}

// stored rewrites requests or a sort onto the stored copies.
func storedCriteria(requests []data.FindRequest) []data.FindRequest {
	out := make([]data.FindRequest, len(requests))
	for i, req := range requests {
		out[i] = data.FindRequest{Omit: req.Omit}
		for _, crit := range req.Criteria {
			crit.FieldName = storedName(crit.FieldName)
			out[i].Criteria = append(out[i].Criteria, crit)
		}
	}
	return out
}

func storedName(field string) string {
	for _, pair := range unstoredPairs {
		if pair[0] == field {
			return pair[1]
		}
	}
	return field
}

func crit(field string, value interface{}) data.FindCriterion {
	return data.FindCriterion{FieldName: field, Value: value}
}

// A find, a sort and a summary on an unstored calculation give what they
// give on a stored field holding the same values: every operator, omitting
// and several requests, each sort direction and empty values, on both
// engines.
func TestUnstoredFind_AsOnAStoredField(t *testing.T) {
	f := newUnstoredFindFixture(t)
	if f == nil {
		return
	}
	ctx := t.Context()

	finds := map[string][]data.FindRequest{
		"greater":           {{Criteria: []data.FindCriterion{crit("total_fees", ">100")}}},
		"at least":          {{Criteria: []data.FindCriterion{crit("total_fees", ">=100")}}},
		"less or equal":     {{Criteria: []data.FindCriterion{crit("total_fees", "<=125.5")}}},
		"less":              {{Criteria: []data.FindCriterion{crit("total_fees", "<0")}}},
		"exact":             {{Criteria: []data.FindCriterion{crit("total_fees", "==100")}}},
		"equal":             {{Criteria: []data.FindCriterion{crit("total_fees", "=125.5")}}},
		"range":             {{Criteria: []data.FindCriterion{crit("total_fees", "100...300")}}},
		"any value":         {{Criteria: []data.FindCriterion{crit("total_fees", "*")}}},
		"empty":             {{Criteria: []data.FindCriterion{crit("total_fees", "=")}}},
		"not":               {{Criteria: []data.FindCriterion{crit("total_fees", "!100")}}},
		"text contains":     {{Criteria: []data.FindCriterion{crit("headline", "bakery")}}},
		"text accent":       {{Criteria: []data.FindCriterion{crit("headline", "==Épsilon Bakery: Q0")}}},
		"text wildcard":     {{Criteria: []data.FindCriterion{crit("headline", "*Q1")}}},
		"text after":        {{Criteria: []data.FindCriterion{crit("headline", ">delta")}}},
		"date":              {{Criteria: []data.FindCriterion{crit("since", "2026-01-02")}}},
		"date month":        {{Criteria: []data.FindCriterion{crit("since", "2026-01")}}},
		"date before":       {{Criteria: []data.FindCriterion{crit("since", "<2026-01-03")}}},
		"date range":        {{Criteria: []data.FindCriterion{crit("since", "2026-01-02...2026-01-03")}}},
		"and a stored one":  {{Criteria: []data.FindCriterion{crit("company", "bakery"), crit("total_fees", ">0")}}},
		"two calculations":  {{Criteria: []data.FindCriterion{crit("headline", "bakery"), crit("since", "*")}}},
		"several requests":  {{Criteria: []data.FindCriterion{crit("total_fees", ">200")}}, {Criteria: []data.FindCriterion{crit("company", "Delta")}}},
		"omit":              {{Omit: true, Criteria: []data.FindCriterion{crit("total_fees", ">100")}}},
		"include then omit": {{Criteria: []data.FindCriterion{crit("company", "bakery")}}, {Omit: true, Criteria: []data.FindCriterion{crit("total_fees", "<200")}}},
		"omit a stored one": {{Criteria: []data.FindCriterion{crit("total_fees", "*")}}, {Omit: true, Criteria: []data.FindCriterion{crit("company", "Delta")}}},
		// Constrain Found Set adds criteria to every request; Extend adds
		// requests: both are requests the find is given.
		"constrained": {{Criteria: []data.FindCriterion{crit("company", "a"), crit("total_fees", ">=100"), crit("headline", "q")}}},
		"extended":    {{Criteria: []data.FindCriterion{crit("total_fees", "<50")}}, {Criteria: []data.FindCriterion{crit("since", "2026-01-01")}}, {Criteria: []data.FindCriterion{crit("headline", "books")}}},
	}
	for name, requests := range finds {
		got, err := f.dataSvc.ExecuteFind(ctx, f.companies, requests, data.QueryOptions{})
		require.NoError(t, err, name)
		want, err := f.dataSvc.ExecuteFind(ctx, f.companies, storedCriteria(requests), data.QueryOptions{})
		require.NoError(t, err, name)
		assert.Equal(t, f.companiesOf(want), f.companiesOf(got), name)
	}

	// Some answers checked on their own, so that a mistake both sides share
	// is seen too.
	got, err := f.dataSvc.ExecuteFind(ctx, f.companies, finds["greater"], data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Alpha Bakery", "beta Books", "Zeta Bakery"}, f.companiesOf(got))
	assert.Equal(t, "125.5", fmt.Sprint(got[0]["total_fees"]), "the records found have their calculations computed")
	got, err = f.dataSvc.ExecuteFind(ctx, f.companies, finds["omit"], data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Gamma Garage", "Delta Deli", "Épsilon Bakery"}, f.companiesOf(got))

	sorts := map[string][]data.SortField{
		"number":             {{Field: "total_fees"}},
		"number descending":  {{Field: "total_fees", Descending: true}},
		"text":               {{Field: "headline"}},
		"text descending":    {{Field: "headline", Descending: true}},
		"date":               {{Field: "since"}},
		"then a stored one":  {{Field: "since", Descending: true}, {Field: "company"}},
		"after a stored one": {{Field: "company", Descending: true}, {Field: "total_fees"}},
	}
	for name, order := range sorts {
		stored := make([]data.SortField, len(order))
		for i, level := range order {
			stored[i] = data.SortField{Field: storedName(level.Field), Descending: level.Descending}
		}
		for _, page := range []data.QueryOptions{{}, {Limit: 2, Offset: 1}} {
			page.Sort = order
			got, err := f.dataSvc.ListRows(ctx, f.companies, page)
			require.NoError(t, err, name)
			page.Sort = stored
			want, err := f.dataSvc.ListRows(ctx, f.companies, page)
			require.NoError(t, err, name)
			assert.Equal(t, f.companiesOf(want), f.companiesOf(got), "%s, page %d+%d", name, page.Offset, page.Limit)
		}
		// A found set sorted.
		got, err := f.dataSvc.ExecuteFind(ctx, f.companies, finds["text contains"], data.QueryOptions{Sort: order})
		require.NoError(t, err, name)
		want, err := f.dataSvc.ExecuteFind(ctx, f.companies, storedCriteria(finds["text contains"]), data.QueryOptions{Sort: stored})
		require.NoError(t, err, name)
		assert.Equal(t, f.companiesOf(want), f.companiesOf(got), "found and sorted by %s", name)
	}
	got, err = f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{Sort: sorts["number descending"]})
	require.NoError(t, err)
	var valued []string
	for _, row := range got {
		if row["total_fees"] != nil {
			valued = append(valued, f.names[fmt.Sprint(row["id"])])
		}
	}
	assert.Equal(t, []string{"Zeta Bakery", "beta Books", "Alpha Bakery", "Delta Deli", "Gamma Garage"}, valued,
		"numbers sort as numbers, an empty one where the engine puts it")

	t.Run("a summary", func(t *testing.T) {
		for _, requests := range [][]data.FindRequest{nil, finds["text contains"], finds["omit"]} {
			for _, groupBy := range [][]string{nil, {"since"}, {"headline"}} {
				got, err := f.dataSvc.Summarize(ctx, f.companies, data.SummaryRequest{Requests: requests, GroupBy: groupBy,
					Fields: []string{"fees_total", "fees_average", "fees_highest", "fees_count"}})
				require.NoError(t, err)
				stored := make([]string, len(groupBy))
				for i, g := range groupBy {
					stored[i] = storedName(g)
				}
				want, err := f.dataSvc.Summarize(ctx, f.companies, data.SummaryRequest{Requests: storedCriteria(requests), GroupBy: stored,
					Fields: []string{"stored_fees_total", "stored_fees_average", "stored_fees_highest", "stored_fees_count"}})
				require.NoError(t, err)
				assert.Equal(t, want.Count, got.Count)
				for _, name := range []string{"total", "average", "highest", "count"} {
					assert.Equal(t, want.Grand["stored_fees_"+name], got.Grand["fees_"+name], name)
				}
				require.Len(t, got.Groups, len(want.Groups))
				for i := range want.Groups {
					assert.Equal(t, want.Groups[i].Count, got.Groups[i].Count)
					assert.Equal(t, fmt.Sprint(want.Groups[i].Values[storedName(strings.Join(groupBy, ""))]),
						fmt.Sprint(got.Groups[i].Values[strings.Join(groupBy, "")]))
					assert.Equal(t, want.Groups[i].Summaries["stored_fees_total"], got.Groups[i].Summaries["fees_total"])
				}
			}
		}
		got, err := f.dataSvc.Summarize(ctx, f.companies, data.SummaryRequest{Fields: []string{"fees_total"}})
		require.NoError(t, err)
		assert.Equal(t, "1513.25", got.Grand["fees_total"])
	})

	t.Run("Get functions", func(t *testing.T) {
		all, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{})
		require.NoError(t, err)
		third := fmt.Sprint(all[2]["record_number"])
		got, err := f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{crit("record_number", "=="+third)}},
		}, data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"Gamma Garage"}, f.companiesOf(got))
		got, err = f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{Sort: []data.SortField{{Field: "record_number", Descending: true}}})
		require.NoError(t, err)
		names := f.companiesOf(all)
		slices.Reverse(names)
		assert.Equal(t, names, f.companiesOf(got), "by record ID, the order they were created in, reversed")

		// GetField reads a field no formula names.
		got, err = f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{crit("by_name", "==Gamma Garage")}},
		}, data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"Gamma Garage"}, f.companiesOf(got))
	})

	t.Run("bounded", func(t *testing.T) {
		data.SetUnstoredFindLimits(data.UnstoredFindLimits{Records: 3})
		_, err := f.dataSvc.ExecuteFind(ctx, f.companies, finds["greater"], data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrTooManyToCompute)
		assert.Contains(t, err.Error(), "this find needs \"total_fees\", an unstored calculation, computed for 6 records")
		assert.Contains(t, err.Error(), "narrow it with a criterion on a stored field first")
		_, err = f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{SortBy: "headline", SortAsc: true})
		require.ErrorIs(t, err, data.ErrTooManyToCompute)
		assert.Contains(t, err.Error(), "this sort needs")
		_, err = f.dataSvc.Summarize(ctx, f.companies, data.SummaryRequest{Fields: []string{"fees_total"}})
		require.ErrorIs(t, err, data.ErrTooManyToCompute)
		assert.Contains(t, err.Error(), "this summary needs")
		// A misspelt field is answered as such, before anything is counted.
		_, err = f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{crit("total_fees", ">1")}},
			{Criteria: []data.FindCriterion{crit("no_such_field", "x")}},
		}, data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrUnknownField)

		// A criterion on a stored field narrows what is computed.
		got, err := f.dataSvc.ExecuteFind(ctx, f.companies, finds["and a stored one"], data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"Alpha Bakery", "Zeta Bakery"}, f.companiesOf(got))

		data.SetUnstoredFindLimits(data.UnstoredFindLimits{Duration: time.Nanosecond})
		_, err = f.dataSvc.ExecuteFind(ctx, f.companies, finds["greater"], data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrTooManyToCompute)
		assert.Contains(t, err.Error(), "the longest it takes")

		data.SetUnstoredFindLimits(data.UnstoredFindLimits{Bytes: 10})
		_, err = f.dataSvc.ExecuteFind(ctx, f.companies, finds["greater"], data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrTooManyToCompute)
		assert.Contains(t, err.Error(), "bytes of values")
		data.SetUnstoredFindLimits(data.DefaultUnstoredFindLimits)
	})

	t.Run("a related record's unstored calculation is still refused", func(t *testing.T) {
		_, err := f.dataSvc.ExecuteFind(ctx, f.customers, []data.FindRequest{{Criteria: []data.FindCriterion{{
			FieldName: "total_fees", Value: ">1", RelationshipID: f.relationship, Occurrence: f.companiesOccurrence,
		}}}}, data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrUnstoredField)
	})
}

// TestUnstoredFindTiming times a find, a sort and a summary on an unstored
// Sum ( Customers::fee ) over 10,000 and 50,000 companies of three customers
// each, every company a candidate. It is a measurement, not a check, so it
// runs only when F4B_UNSTORED_TIMING is set:
//
//	F4B_UNSTORED_TIMING=1 go test -run TestUnstoredFindTiming -v ./internal/data/
func TestUnstoredFindTiming(t *testing.T) {
	if os.Getenv("F4B_UNSTORED_TIMING") == "" {
		t.Skip("a timing, run with F4B_UNSTORED_TIMING=1")
	}
	driver, _ := relatedTestDriver(t)
	if driver == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	data.SetUnstoredFindLimits(data.UnstoredFindLimits{Records: 1_000_000, Duration: time.Hour, Bytes: 1 << 30})
	defer data.SetUnstoredFindLimits(data.DefaultUnstoredFindLimits)

	for _, n := range []int{10000, 50000} {
		f := newRelatedFixture(t, ctx, driver, false, false, "")
		occurrences, err := f.schemaSvc.ListTableOccurrences(ctx)
		require.NoError(t, err)
		customers := ""
		for _, o := range occurrences {
			if o.ID == f.customersOccurrence {
				customers = o.Name
			}
		}
		tables, err := f.schemaSvc.ListTables(ctx)
		require.NoError(t, err)
		tableID := map[string]string{}
		for _, tbl := range tables {
			tableID[tbl.Name] = tbl.ID
		}
		for _, c := range []struct {
			table, name string
			kind        dbal.AgnosticFieldType
			formula     *string
		}{
			{f.customers, "fee", dbal.FieldTypeNumber, nil},
			{f.companies, "total_fees", dbal.FieldTypeCalculation, unstoredCalcJSON(fmt.Sprintf("Sum ( %s::fee )", customers), "Number")},
			{f.companies, "fees_total", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryTotal, "total_fees")},
		} {
			_, err := f.schemaSvc.AddColumn(ctx, tableID[c.table], schema.ColumnMetadata{
				Name: c.name, DisplayName: c.name, FieldType: c.kind, IsNullable: true, CalculationFormula: c.formula,
			})
			require.NoError(t, err)
		}
		timingInsert(t, ctx, driver, f.companies, []string{"company"}, n, func(i int) []interface{} {
			return []interface{}{fmt.Sprintf("Company %06d", i)}
		})
		timingInsert(t, ctx, driver, f.customers, []string{"company", "last_name", "fee"}, 3*n, func(i int) []interface{} {
			return []interface{}{fmt.Sprintf("Company %06d", i/3), fmt.Sprintf("Customer %d", i), i % 997}
		})

		engine := driver.Dialect().Engine()
		find := func(over string, extra ...data.FindCriterion) func() error {
			return func() error {
				rows, err := f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
					{Criteria: append(extra, data.FindCriterion{FieldName: "total_fees", Value: ">" + over})},
				}, data.QueryOptions{Limit: 500})
				if err == nil && len(rows) == 0 {
					err = fmt.Errorf("found nothing")
				}
				return err
			}
		}
		measure := func(index, what string, run func() error) {
			started := time.Now()
			err := run()
			t.Logf("%s, %d companies, %s (%s): %s, %v", engine, n, what, index, time.Since(started).Round(time.Millisecond), err)
		}
		all := func(index string) {
			measure(index, "find total_fees > 2000, first 500", find("2000"))
			measure(index, "sort by total_fees descending, first 500", func() error {
				_, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{Sort: []data.SortField{{Field: "total_fees", Descending: true}}, Limit: 500})
				return err
			})
			measure(index, "summary Total of total_fees", func() error {
				_, err := f.dataSvc.Summarize(ctx, f.companies, data.SummaryRequest{Fields: []string{"fees_total"}})
				return err
			})
			measure(index, "find total_fees > 500 narrowed to 1% by a stored field", find("500", data.FindCriterion{FieldName: "company", Value: "Company 0000*"}))
		}
		// Without an index on the match field, every page of 500 companies
		// reads Customers once per company: quadratic, so measured at 10,000
		// only; at 50,000 the default limits refuse it after 30 seconds.
		if n == 10000 {
			all("match field not indexed")
		} else {
			data.SetUnstoredFindLimits(data.DefaultUnstoredFindLimits)
			measure("match field not indexed, default limits", "find total_fees > 2000", find("2000"))
			data.SetUnstoredFindLimits(data.UnstoredFindLimits{Records: 1_000_000, Duration: time.Hour, Bytes: 1 << 30})
		}
		// The index multikey.go documents for PostgreSQL; on MariaDB a
		// field of one line is compared whole, which a prefix index serves.
		index := fmt.Sprintf("CREATE INDEX f4b_timing_company_%d ON %s ((array_remove(string_to_array(replace(%s, E'\\r', E'\\n'), E'\\n'), ''))) ",
			n, driver.Dialect().QuoteIdentifier(f.customers), driver.Dialect().QuoteIdentifier("company"))
		if engine == dbal.EnginePostgres {
			index = strings.Replace(index, "((", "USING gin ((", 1)
		} else {
			index = fmt.Sprintf("CREATE INDEX f4b_timing_company_%d ON %s (%s(191))", n, driver.Dialect().QuoteIdentifier(f.customers), driver.Dialect().QuoteIdentifier("company"))
		}
		_, err = driver.DB().ExecContext(ctx, index)
		require.NoError(t, err)
		_, _ = driver.DB().ExecContext(ctx, "ANALYZE "+driver.Dialect().QuoteIdentifier(f.customers))
		all("match field indexed")
		data.SetUnstoredFindLimits(data.DefaultUnstoredFindLimits)
		measure("match field indexed, default limits", "find total_fees > 2000", find("2000"))
		data.SetUnstoredFindLimits(data.UnstoredFindLimits{Records: 1_000_000, Duration: time.Hour, Bytes: 1 << 30})
	}
}

// timingInsert inserts n rows into table, in statements of 1,000 rows.
func timingInsert(t *testing.T, ctx context.Context, driver dbal.DatabaseDriver, table string, columns []string, n int, row func(i int) []interface{}) {
	t.Helper()
	dialect := driver.Dialect()
	quoted := []string{dialect.QuoteIdentifier("id")}
	for _, c := range columns {
		quoted = append(quoted, dialect.QuoteIdentifier(c))
	}
	for start := 0; start < n; start += 1000 {
		var marks []string
		var args []interface{}
		for i := start; i < start+1000 && i < n; i++ {
			values := append([]interface{}{uuid.NewString()}, row(i)...)
			places := make([]string, len(values))
			for j := range values {
				places[j] = dialect.Placeholder(len(args) + j + 1)
			}
			marks = append(marks, "("+strings.Join(places, ", ")+")")
			args = append(args, values...)
		}
		_, err := driver.DB().ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
			dialect.QuoteIdentifier(table), strings.Join(quoted, ", "), strings.Join(marks, ", ")), args...)
		require.NoError(t, err)
	}
}
