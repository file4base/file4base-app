package data_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// relatedFixture is the tutorial's own shape: companies, and customers that
// name the company they belong to, joined on the company name.
type relatedFixture struct {
	schemaSvc *schema.Service
	dataSvc   *data.Service

	companies, customers string // physical table names
	relationship         string
	customersOccurrence  string
	companiesOccurrence  string
}

func newRelatedFixture(t *testing.T, ctx context.Context, driver dbal.DatabaseDriver,
	allowCreation, cascadeDelete bool, sortRelated string) *relatedFixture {
	t.Helper()

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))

	stamp := time.Now().UnixNano() % 1000000
	f := &relatedFixture{
		schemaSvc: schemaSvc,
		dataSvc:   data.NewService(driver),
		companies: fmt.Sprintf("rel_companies_%d", stamp),
		customers: fmt.Sprintf("rel_customers_%d", stamp),
	}

	companies, err := schemaSvc.CreateTable(ctx, "Rel Companies", f.companies)
	require.NoError(t, err)
	t.Cleanup(func() { _ = schemaSvc.DeleteTable(context.Background(), companies.ID) })

	customers, err := schemaSvc.CreateTable(ctx, "Rel Customers", f.customers)
	require.NoError(t, err)
	t.Cleanup(func() { _ = schemaSvc.DeleteTable(context.Background(), customers.ID) })

	companyNameOnCompanies, err := schemaSvc.AddColumn(ctx, companies.ID, schema.ColumnMetadata{
		Name: "company", DisplayName: "Company", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)
	_, err = schemaSvc.AddColumn(ctx, companies.ID, schema.ColumnMetadata{
		Name: "company_address", DisplayName: "Company Address", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	companyNameOnCustomers, err := schemaSvc.AddColumn(ctx, customers.ID, schema.ColumnMetadata{
		Name: "company", DisplayName: "Company", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)
	_, err = schemaSvc.AddColumn(ctx, customers.ID, schema.ColumnMetadata{
		Name: "last_name", DisplayName: "Last Name", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	occurrences, err := schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		switch o.BaseTableID {
		case companies.ID:
			f.companiesOccurrence = o.ID
		case customers.ID:
			f.customersOccurrence = o.ID
		}
	}
	require.NotEmpty(t, f.companiesOccurrence, "creating a table registers its occurrence")
	require.NotEmpty(t, f.customersOccurrence)

	in := schema.CreateRelationshipInput{
		Name:              fmt.Sprintf("companies_customers_%d", stamp),
		LeftOccurrenceID:  f.companiesOccurrence,
		LeftColumnID:      companyNameOnCompanies.ID,
		RightOccurrenceID: f.customersOccurrence,
		RightColumnID:     companyNameOnCustomers.ID,
		Operator:          "=",
		AllowCreation:     allowCreation,
		CascadeDelete:     cascadeDelete,
	}
	if sortRelated != "" {
		in.SortRelated = &sortRelated
	}
	rel, err := schemaSvc.CreateRelationship(ctx, in)
	require.NoError(t, err)
	t.Cleanup(func() { _ = schemaSvc.DeleteRelationship(context.Background(), rel.ID) })
	f.relationship = rel.ID

	return f
}

func relatedTestDriver(t *testing.T) (dbal.DatabaseDriver, context.Context) {
	t.Helper()
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return nil, nil
	}
	t.Cleanup(func() { _ = driver.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return nil, nil
	}
	return driver, ctx
}

func lastNames(rows []map[string]interface{}) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprint(r["last_name"]))
	}
	return out
}

// Reading the other side of a relationship (#36): what a portal shows and what
// a related field on a layout reads.
func TestRelatedRows(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "")

	bakery, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{
		"company": "Favorite Bakery", "company_address": "12 Mill Lane",
	})
	require.NoError(t, err)
	_, err = f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{
		"company": "Other Bakery", "company_address": "99 Elsewhere",
	})
	require.NoError(t, err)
	nameless, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{
		"company_address": "Nowhere",
	})
	require.NoError(t, err)

	for _, c := range []struct{ company, lastName string }{
		{"Favorite Bakery", "Soto"},
		{"Favorite Bakery", "Alvarez"},
		{"Favorite Bakery", "Mendez"},
		{"Other Bakery", "Kane"},
		{"", "Unattached"},
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{
			"company": c.company, "last_name": c.lastName,
		})
		require.NoError(t, err)
	}

	bakeryID := bakery["id"].(string)

	t.Run("a portal shows the records that match", func(t *testing.T) {
		rows, err := f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			f.relationship, f.customersOccurrence, data.QueryOptions{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"Soto", "Alvarez", "Mendez"}, lastNames(rows),
			"only the customers of this company, and not the one with no company")
	})

	t.Run("the side to read can be left out when the tables differ", func(t *testing.T) {
		rows, err := f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			f.relationship, "", data.QueryOptions{})
		require.NoError(t, err)
		assert.Len(t, rows, 3)
	})

	t.Run("the relationship is read the other way round as well", func(t *testing.T) {
		customers, err := f.dataSvc.ListRows(ctx, f.customers, data.QueryOptions{})
		require.NoError(t, err)
		var sotoID string
		for _, c := range customers {
			if c["last_name"] == "Soto" {
				sotoID = c["id"].(string)
			}
		}
		require.NotEmpty(t, sotoID)

		rows, err := f.dataSvc.RelatedRows(ctx, f.customers, sotoID,
			f.relationship, f.companiesOccurrence, data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, rows, 1, "a customer has one company")
		assert.Equal(t, "12 Mill Lane", rows[0]["company_address"],
			"this is the related field a layout shows")
	})

	t.Run("an empty match field relates to nothing", func(t *testing.T) {
		rows, err := f.dataSvc.RelatedRows(ctx, f.companies, nameless["id"].(string),
			f.relationship, f.customersOccurrence, data.QueryOptions{})
		require.NoError(t, err)
		assert.Empty(t, rows,
			"an empty key must not sweep up every record whose own key is empty")
	})

	t.Run("the caller's sort order is honoured", func(t *testing.T) {
		rows, err := f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			f.relationship, f.customersOccurrence,
			data.QueryOptions{Sort: []data.SortField{{Field: "last_name"}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"Alvarez", "Mendez", "Soto"}, lastNames(rows))

		rows, err = f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			f.relationship, f.customersOccurrence,
			data.QueryOptions{Sort: []data.SortField{{Field: "last_name", Descending: true}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"Soto", "Mendez", "Alvarez"}, lastNames(rows))
	})

	t.Run("a field the related table does not have is refused", func(t *testing.T) {
		_, err := f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			f.relationship, f.customersOccurrence,
			data.QueryOptions{Sort: []data.SortField{{Field: "no_such_field"}}})
		assert.ErrorIs(t, err, data.ErrUnknownField)
	})

	t.Run("a relationship that is not there is reported", func(t *testing.T) {
		_, err := f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			"no-such-relationship", "", data.QueryOptions{})
		assert.ErrorIs(t, err, data.ErrRelationshipNotFound)
	})

	t.Run("a relationship that does not reach the table is reported", func(t *testing.T) {
		_, err := f.dataSvc.RelatedRows(ctx, f.companies, bakeryID,
			f.relationship, "no-such-occurrence", data.QueryOptions{})
		assert.ErrorIs(t, err, data.ErrRelationshipNotFound)
	})

	t.Run("creation is refused unless the relationship allows it", func(t *testing.T) {
		_, err := f.dataSvc.CreateRelatedRow(ctx, f.companies, bakeryID,
			f.relationship, f.customersOccurrence, map[string]interface{}{"last_name": "Nope"})
		assert.ErrorIs(t, err, data.ErrCreationNotAllowed)
	})
}

// "Sort related records" on the relationship is the order a portal falls back
// to when it asks for none of its own (#36).
func TestRelatedRowsUseTheRelationshipsOwnSort(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "-last_name")

	company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Sorted"})
	require.NoError(t, err)
	for _, name := range []string{"Soto", "Alvarez", "Mendez"} {
		_, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{
			"company": "Sorted", "last_name": name,
		})
		require.NoError(t, err)
	}

	rows, err := f.dataSvc.RelatedRows(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Soto", "Mendez", "Alvarez"}, lastNames(rows))

	// Reading the relationship the other way round, a customer's company, must
	// not try to sort companies by a field only customers have.
	customers, err := f.dataSvc.ListRows(ctx, f.customers, QueryOptionsEmpty())
	require.NoError(t, err)
	require.NotEmpty(t, customers)
	back, err := f.dataSvc.RelatedRows(ctx, f.customers, customers[0]["id"].(string),
		f.relationship, f.companiesOccurrence, data.QueryOptions{})
	require.NoError(t, err, "the relationship's sort belongs to the side it points at")
	require.Len(t, back, 1)

	// A sort naming a field the side being read does not have is dropped
	// rather than leaving the portal blank...
	_, err = f.schemaSvc.UpdateRelationship(ctx, f.relationship, schema.UpdateRelationshipInput{
		SortRelated: strPtr("no_such_field"),
	})
	require.NoError(t, err)
	rows, err = f.dataSvc.RelatedRows(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence, data.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, rows, 3)

	// ...but what the caller asks for is still checked, so a sort typed into
	// Portal Setup is reported rather than ignored.
	_, err = f.dataSvc.RelatedRows(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence,
		data.QueryOptions{Sort: []data.SortField{{Field: "no_such_field"}}})
	assert.ErrorIs(t, err, data.ErrUnknownField)

	// What the caller asks for still wins.
	rows, err = f.dataSvc.RelatedRows(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence,
		data.QueryOptions{Sort: []data.SortField{{Field: "last_name"}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"Alvarez", "Mendez", "Soto"}, lastNames(rows))
}

// Typing into the last row of a portal creates a related record, and the match
// field is filled by the relationship rather than by the caller (#36).
func TestCreateRelatedRow(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, true, false, "")

	company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Creating Co"})
	require.NoError(t, err)
	nameless, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company_address": "Nowhere"})
	require.NoError(t, err)

	created, err := f.dataSvc.CreateRelatedRow(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence, map[string]interface{}{"last_name": "Nuevo"})
	require.NoError(t, err)
	assert.Equal(t, "Creating Co", created["company"], "the match field is filled from the parent")
	assert.Equal(t, "Nuevo", created["last_name"])

	rows, err := f.dataSvc.RelatedRows(ctx, f.companies, company["id"].(string),
		f.relationship, f.customersOccurrence, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the new record is in the portal straight away")

	t.Run("the caller cannot aim the new record at another parent", func(t *testing.T) {
		other, err := f.dataSvc.CreateRelatedRow(ctx, f.companies, company["id"].(string),
			f.relationship, f.customersOccurrence,
			map[string]interface{}{"last_name": "Hijacked", "company": "Somewhere Else"})
		require.NoError(t, err)
		assert.Equal(t, "Creating Co", other["company"],
			"the relationship owns the match field, not the request body")
	})

	t.Run("a parent with an empty match field has nothing to create against", func(t *testing.T) {
		_, err := f.dataSvc.CreateRelatedRow(ctx, f.companies, nameless["id"].(string),
			f.relationship, f.customersOccurrence, map[string]interface{}{"last_name": "Orphan"})
		assert.ErrorIs(t, err, data.ErrNoMatchValue)
	})
}

// "Delete related records in this table when a record is deleted in the other
// table" (#36): deleting a company takes its customers with it, and only its
// own.
func TestCascadeDelete(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, true, "")

	doomed, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Closing Down"})
	require.NoError(t, err)
	_, err = f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Staying Open"})
	require.NoError(t, err)

	for _, c := range []struct{ company, lastName string }{
		{"Closing Down", "Soto"},
		{"Closing Down", "Alvarez"},
		{"Staying Open", "Kane"},
		{"", "Unattached"},
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{
			"company": c.company, "last_name": c.lastName,
		})
		require.NoError(t, err)
	}

	require.NoError(t, f.dataSvc.DeleteRow(ctx, f.companies, doomed["id"].(string)))

	remaining, err := f.dataSvc.ListRows(ctx, f.customers, data.QueryOptions{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Kane", "Unattached"}, lastNames(remaining),
		"the deleted company's customers went with it, and nobody else's")
}

// A relationship without the option deletes nothing but the record asked for.
func TestDeleteWithoutCascadeLeavesRelatedRecords(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "")

	company, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Keeps Them"})
	require.NoError(t, err)
	_, err = f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{
		"company": "Keeps Them", "last_name": "Soto",
	})
	require.NoError(t, err)

	require.NoError(t, f.dataSvc.DeleteRow(ctx, f.companies, company["id"].(string)))

	remaining, err := f.dataSvc.ListRows(ctx, f.customers, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Soto"}, lastNames(remaining))
}

func strPtr(s string) *string { return &s }

// QueryOptionsEmpty is the default page: no sort, the default limit.
func QueryOptionsEmpty() data.QueryOptions { return data.QueryOptions{} }
