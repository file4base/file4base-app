package schema_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitCustomValues(t *testing.T) {
	values := schema.SplitCustomValues("New\nContinuing\n\n  Lapsed  \nNew\r\nFormer")
	require.Equal(t, []string{"New", "Continuing", "Lapsed", "Former"}, values,
		"blank lines are dropped, values are trimmed and each appears once")

	require.Empty(t, schema.SplitCustomValues(""))
	require.Empty(t, schema.SplitCustomValues("   \n\n  "))
}

// Value lists (#31): a named set a field can be filled from, either typed in
// or taken from what a field already holds.
func TestValueLists(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
		DSN:        testdb.DevDSN(),
	})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := time.Now().UnixNano() % 1000000
	name := fmt.Sprintf("value_list_customers_%d", stamp)
	tbl, err := svc.CreateTable(ctx, "Value List Customers", name)
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(ctx, tbl.ID) }()

	typeCol, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "customer_type", DisplayName: "Customer Type",
		FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	var created *schema.ValueList

	t.Run("a custom list keeps the values it was given", func(t *testing.T) {
		created, err = svc.CreateValueList(ctx, schema.ValueListInput{
			Name:         fmt.Sprintf("Customer Types %d", stamp),
			Kind:         schema.ValueListCustom,
			CustomValues: "New\nContinuing",
		})
		require.NoError(t, err)
		require.Equal(t, schema.ValueListCustom, created.Kind)

		values, err := svc.ValueListValues(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"New", "Continuing"}, values)
	})

	t.Run("a custom list with no values is refused", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: fmt.Sprintf("Empty %d", stamp), Kind: schema.ValueListCustom, CustomValues: "  \n ",
		})
		assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("a list with no name is refused", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{Name: "  ", CustomValues: "a"})
		assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("two lists cannot share a name", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: created.Name, Kind: schema.ValueListCustom, CustomValues: "x",
		})
		assert.ErrorIs(t, err, schema.ErrValueListExists)
	})

	t.Run("a list taken from a field needs that field", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: fmt.Sprintf("Loose %d", stamp), Kind: schema.ValueListFromField,
		})
		assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("a list taken from a field offers the values it holds", func(t *testing.T) {
		list, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name:           fmt.Sprintf("Types In Use %d", stamp),
			Kind:           schema.ValueListFromField,
			SourceTableID:  &tbl.ID,
			SourceColumnID: &typeCol.ID,
		})
		require.NoError(t, err)

		// Nothing stored yet, so nothing is offered.
		values, err := svc.ValueListValues(ctx, list.ID)
		require.NoError(t, err)
		require.Empty(t, values)

		db := driver.DB()
		dialect := driver.Dialect()
		for i, value := range []string{"Continuing", "New", "Continuing", ""} {
			q := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES (%s, %s)",
				dialect.QuoteIdentifier(name), dialect.QuoteIdentifier("id"),
				dialect.QuoteIdentifier("customer_type"), dialect.Placeholder(1), dialect.Placeholder(2))
			_, err := db.ExecContext(ctx, q, fmt.Sprintf("row-%d-%d", stamp, i), value)
			require.NoError(t, err)
		}

		values, err = svc.ValueListValues(ctx, list.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"Continuing", "New"}, values,
			"sorted, without blanks, and each value once")
	})

	t.Run("a list can be renamed and its values replaced", func(t *testing.T) {
		updated, err := svc.UpdateValueList(ctx, created.ID, schema.ValueListInput{
			Name:         fmt.Sprintf("Customer Status %d", stamp),
			Kind:         schema.ValueListCustom,
			CustomValues: "New\nContinuing\nLapsed",
		})
		require.NoError(t, err)
		require.Contains(t, updated.Name, "Customer Status")

		values, err := svc.ValueListValues(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"New", "Continuing", "Lapsed"}, values)
	})

	t.Run("the lists come back by name", func(t *testing.T) {
		lists, err := svc.ListValueLists(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(lists), 2)
		for i := 1; i < len(lists); i++ {
			require.LessOrEqual(t, lists[i-1].Name, lists[i].Name)
		}
	})

	t.Run("deleting a list that is not there is reported", func(t *testing.T) {
		assert.ErrorIs(t, svc.DeleteValueList(ctx, "no-such-list"), schema.ErrValueListNotFound)
	})

	t.Run("a deleted list is gone", func(t *testing.T) {
		require.NoError(t, svc.DeleteValueList(ctx, created.ID))
		_, err := svc.GetValueList(ctx, created.ID)
		assert.ErrorIs(t, err, schema.ErrValueListNotFound)
	})
}

// A value list must survive Save and Open: it goes into the solution file and
// comes back, with a field list's table and column remapped to the
// destination's ids (#31).
func TestValueListsRoundTripThroughASolutionFile(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := time.Now().UnixNano() % 1000000
	name := fmt.Sprintf("vl_roundtrip_%d", stamp)
	tbl, err := svc.CreateTable(ctx, "VL Roundtrip", name)
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(ctx, tbl.ID) }()

	col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "city", DisplayName: "City", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	custom, err := svc.CreateValueList(ctx, schema.ValueListInput{
		Name: fmt.Sprintf("Types %d", stamp), Kind: schema.ValueListCustom, CustomValues: "New\nContinuing",
	})
	require.NoError(t, err)
	defer func() { _ = svc.DeleteValueList(ctx, custom.ID) }()

	fromField, err := svc.CreateValueList(ctx, schema.ValueListInput{
		Name: fmt.Sprintf("Cities %d", stamp), Kind: schema.ValueListFromField,
		SourceTableID: &tbl.ID, SourceColumnID: &col.ID,
	})
	require.NoError(t, err)
	defer func() { _ = svc.DeleteValueList(ctx, fromField.ID) }()

	file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "VL Roundtrip"})
	require.NoError(t, err)

	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)

	var sawCustom, sawField bool
	for _, vl := range bundle.ValueLists {
		switch vl.ID {
		case custom.ID:
			sawCustom = true
			assert.Equal(t, "New\nContinuing", vl.CustomValues)
		case fromField.ID:
			sawField = true
			require.NotNil(t, vl.SourceColumnID)
			assert.Equal(t, col.ID, *vl.SourceColumnID)
		}
	}
	assert.True(t, sawCustom, "the custom list is in the file")
	assert.True(t, sawField, "the field list is in the file, with the field it reads")

	// Importing the same file again matches the lists by name instead of
	// making a second copy of each.
	before, err := svc.ListValueLists(ctx)
	require.NoError(t, err)
	report, err := svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	after, err := svc.ListValueLists(ctx)
	require.NoError(t, err)

	assert.Equal(t, len(before), len(after), "re-importing must not duplicate the lists")
	assert.Equal(t, 0, report.ValueListsCreated)
	assert.GreaterOrEqual(t, report.ValueListsUpdated, 2)

	// And the field list still resolves against the destination's own field.
	values, err := svc.ValueListValues(ctx, fromField.ID)
	require.NoError(t, err)
	assert.NotNil(t, values)
}
