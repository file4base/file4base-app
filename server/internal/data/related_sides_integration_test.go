package data_test

import (
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In FileMaker each side of a relationship has its own options, acting on the
// records of that side. The fixture's companies are its left side and its
// customers its right; these set the left side's options, which act on
// companies, from a customer.

func setLeftOptions(t *testing.T, f *relatedFixture, in schema.UpdateRelationshipInput) {
	t.Helper()
	_, err := f.schemaSvc.UpdateRelationship(t.Context(), f.relationship, in)
	require.NoError(t, err)
}

func companyAddresses(rows []map[string]interface{}) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprint(r["company_address"]))
	}
	return out
}

func TestCreateRelatedRowOnTheLeftSide(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "")
	allow := true
	setLeftOptions(t, f, schema.UpdateRelationshipInput{LeftAllowCreation: &allow})

	customer, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{
		"company": "Founded From A Customer", "last_name": "Soto",
	})
	require.NoError(t, err)

	created, err := f.dataSvc.CreateRelatedRow(ctx, f.customers, customer["id"].(string),
		f.relationship, f.companiesOccurrence, map[string]interface{}{"company_address": "1 Main St"})
	require.NoError(t, err)
	assert.Equal(t, "Founded From A Customer", created["company"], "the match field is filled from the customer")

	rows, err := f.dataSvc.RelatedRows(ctx, f.customers, customer["id"].(string),
		f.relationship, f.companiesOccurrence, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"1 Main St"}, companyAddresses(rows))

	t.Run("the right side's own option is still off", func(t *testing.T) {
		company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "No Portal Rows"})
		require.NoError(t, err)
		_, err = f.dataSvc.CreateRelatedRow(ctx, f.companies, company["id"].(string),
			f.relationship, f.customersOccurrence, map[string]interface{}{"last_name": "Refused"})
		require.ErrorIs(t, err, data.ErrCreationNotAllowed)
		assert.ErrorContains(t, err, "create the record from")
	})
}

func TestCreateRelatedRowOnEitherSide(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, true, false, "")
	allow := true
	setLeftOptions(t, f, schema.UpdateRelationshipInput{LeftAllowCreation: &allow})

	company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Both Ways"})
	require.NoError(t, err)
	customer, err := f.dataSvc.CreateRelatedRow(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence, map[string]interface{}{"last_name": "Kane"})
	require.NoError(t, err)
	other, err := f.dataSvc.CreateRelatedRow(ctx, f.customers, fmt.Sprint(customer["id"]),
		f.relationship, f.companiesOccurrence, map[string]interface{}{"company_address": "Branch"})
	require.NoError(t, err)
	assert.Equal(t, "Both Ways", other["company"])
}

// The left side's "Delete related records": deleting a customer deletes its
// company, and deleting a company leaves its customers alone.
func TestCascadeDeleteOnTheLeftSide(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "")
	purge := true
	setLeftOptions(t, f, schema.UpdateRelationshipInput{LeftCascadeDelete: &purge})

	for _, c := range []struct{ company, address string }{
		{"Goes", "gone"},
		{"Stays", "kept"},
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{
			"company": c.company, "company_address": c.address,
		})
		require.NoError(t, err)
	}
	leaving, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Goes", "last_name": "Soto"})
	require.NoError(t, err)
	_, err = f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Stays", "last_name": "Kane"})
	require.NoError(t, err)

	require.NoError(t, f.dataSvc.DeleteRow(ctx, f.customers, leaving["id"].(string)))
	companies, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"kept"}, companyAddresses(companies), "the customer's company went with it")

	t.Run("a left-side option never deletes the other way round", func(t *testing.T) {
		stays := companies[0]
		require.NoError(t, f.dataSvc.DeleteRow(ctx, f.companies, stays["id"].(string)))
		customers, err := f.dataSvc.ListRows(ctx, f.customers, data.QueryOptions{})
		require.NoError(t, err)
		assert.Equal(t, []string{"Kane"}, lastNames(customers), "deleting a company deletes no customer")
	})
}

// With both sides' option on, a delete on either side takes the other side's
// matching records, and the cascade stops at records already deleted.
func TestCascadeDeleteOnBothSides(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, true, "")
	purge := true
	setLeftOptions(t, f, schema.UpdateRelationshipInput{LeftCascadeDelete: &purge})

	company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Linked"})
	require.NoError(t, err)
	_, err = f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Apart"})
	require.NoError(t, err)
	for _, name := range []string{"Soto", "Alvarez"} {
		_, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Linked", "last_name": name})
		require.NoError(t, err)
	}
	_, err = f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Apart", "last_name": "Kane"})
	require.NoError(t, err)

	require.NoError(t, f.dataSvc.DeleteRow(ctx, f.companies, company["id"].(string)))

	customers, err := f.dataSvc.ListRows(ctx, f.customers, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Kane"}, lastNames(customers))
	companies, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, companies, 1)
	assert.Equal(t, "Apart", companies[0]["company"])
}

// Each side's sort orders the records of that side, read from a record of the
// other.
func TestRelatedRowsUseEachSidesSort(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "last_name")
	setLeftOptions(t, f, schema.UpdateRelationshipInput{LeftSortRelated: strPtr("-company_address")})

	for _, address := range []string{"B", "C", "A"} {
		_, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Shared", "company_address": address})
		require.NoError(t, err)
	}
	var customerID string
	for _, name := range []string{"Soto", "Alvarez"} {
		row, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Shared", "last_name": name})
		require.NoError(t, err)
		customerID = row["id"].(string)
	}

	companies, err := f.dataSvc.RelatedRows(ctx, f.customers, customerID,
		f.relationship, f.companiesOccurrence, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"C", "B", "A"}, companyAddresses(companies), "the left side's sort")

	all, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{})
	require.NoError(t, err)
	customers, err := f.dataSvc.RelatedRows(ctx, f.companies, all[0]["id"].(string),
		f.relationship, f.customersOccurrence, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Alvarez", "Soto"}, lastNames(customers), "the right side's sort")
}
