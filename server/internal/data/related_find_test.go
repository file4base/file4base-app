package data_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Find on a related field (#46): a criterion on a field of the related table
// finds the records whose related records match it — the parents, not the
// related records.
func TestExecuteFind_RelatedField(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "")

	for _, company := range []map[string]interface{}{
		{"company": "ABC Company", "company_address": "51 Market Street, San Francisco"},
		{"company": "DEF Ltd.", "company_address": "77 Nathan Road, Hong Kong"},
		{"company": "XYZ Inc.", "company_address": "14 Avenue Foch, Paris"},
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.companies, company)
		require.NoError(t, err)
	}
	for _, customer := range []map[string]interface{}{
		{"last_name": "Smith", "company": "ABC Company"},
		{"last_name": "Tang", "company": "DEF Ltd."},
		{"last_name": "Durand", "company": "XYZ Inc."},
		{"last_name": "LeFranc", "company": "XYZ Inc."},
		{"last_name": "Nobody", "company": ""},
		{"last_name": "Unknown", "company": "Not A Company"},
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.customers, customer)
		require.NoError(t, err)
	}

	related := func(field, operator string, value interface{}) data.FindCriterion {
		return data.FindCriterion{
			FieldName:      field,
			Operator:       operator,
			Value:          value,
			RelationshipID: f.relationship,
			Occurrence:     f.companiesOccurrence,
		}
	}
	find := func(requests ...data.FindRequest) []string {
		rows, err := f.dataSvc.ExecuteFind(ctx, f.customers, requests, data.QueryOptions{
			Sort: []data.SortField{{Field: "last_name"}},
		})
		require.NoError(t, err)
		return lastNames(rows)
	}

	// "Every customer whose company is in Paris" — the question a relational
	// find is for. It answers customers, not companies.
	assert.Equal(t, []string{"Durand", "LeFranc"},
		find(data.FindRequest{Criteria: []data.FindCriterion{related("company_address", "LIKE", "%Paris%")}}))

	// The operators are the same ones a criterion on an own field takes.
	assert.Equal(t, []string{"Tang"},
		find(data.FindRequest{Criteria: []data.FindCriterion{related("company", "=", "DEF Ltd.")}}))
	assert.Equal(t, []string{"Smith"},
		find(data.FindRequest{Criteria: []data.FindCriterion{related("company_address", "LIKE", "%San Francisco%")}}))

	// A criterion on an own field and one on a related field in the same
	// request are combined with AND, as two own fields are.
	assert.Equal(t, []string{"LeFranc"},
		find(data.FindRequest{Criteria: []data.FindCriterion{
			related("company_address", "LIKE", "%Paris%"),
			{FieldName: "last_name", Operator: "=", Value: "LeFranc"},
		}}))

	// It composes with the OR and omit semantics of #34.
	assert.Equal(t, []string{"Durand", "LeFranc", "Tang"}, find(
		data.FindRequest{Criteria: []data.FindCriterion{related("company_address", "LIKE", "%Paris%")}},
		data.FindRequest{Criteria: []data.FindCriterion{related("company_address", "LIKE", "%Hong Kong%")}},
	))
	assert.Equal(t, []string{"Durand"}, find(
		data.FindRequest{Criteria: []data.FindCriterion{related("company_address", "LIKE", "%Paris%")}},
		data.FindRequest{Criteria: []data.FindCriterion{{FieldName: "last_name", Operator: "=", Value: "LeFranc"}}, Omit: true},
	))

	// A record with an empty match field relates to nothing, so it never
	// matches a related criterion — not even "the field is empty", which is
	// about the related record's field, not about having no related record.
	for _, criterion := range []data.FindCriterion{
		related("company_address", "IS_EMPTY", nil),
		related("company_address", "LIKE", "%%"),
	} {
		found := find(data.FindRequest{Criteria: []data.FindCriterion{criterion}})
		assert.NotContains(t, found, "Nobody", "an empty match field relates to nothing")
		assert.NotContains(t, found, "Unknown", "a match value with no related record finds nothing")
	}

	// Reading the relationship the other way round works as well: the
	// companies that have a customer called Tang.
	rows, err := f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{{
		Criteria: []data.FindCriterion{{
			FieldName:      "last_name",
			Operator:       "=",
			Value:          "Tang",
			RelationshipID: f.relationship,
			Occurrence:     f.customersOccurrence,
		}},
	}}, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "DEF Ltd.", rows[0]["company"])

	// What cannot be searched is reported rather than silently ignored.
	_, err = f.dataSvc.ExecuteFind(ctx, f.customers, []data.FindRequest{{
		Criteria: []data.FindCriterion{related("postcode", "=", "75008")},
	}}, data.QueryOptions{})
	require.ErrorIs(t, err, data.ErrUnknownField)

	_, err = f.dataSvc.ExecuteFind(ctx, f.customers, []data.FindRequest{{
		Criteria: []data.FindCriterion{{
			FieldName: "company_address", Operator: "=", Value: "x",
			RelationshipID: "00000000-0000-0000-0000-000000000000",
		}},
	}}, data.QueryOptions{})
	require.ErrorIs(t, err, data.ErrRelationshipNotFound)
}
