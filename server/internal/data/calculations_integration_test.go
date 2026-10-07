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
	"github.com/stretchr/testify/require"
)

func calcJSON(formula, resultType string) *string {
	s := fmt.Sprintf(`{"formula":%q,"result_type":%q}`, formula, resultType)
	return &s
}

// Calculation fields used to be created as plain text columns that nothing
// ever filled in (#30). The tutorial's Annual Fee, end to end against a real
// engine.
func TestCalculationFields(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
		DSN:        testdb.DevDSN(),
	})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	dataSvc := data.NewService(driver)

	name := fmt.Sprintf("calc_customers_%d", time.Now().UnixNano()%1000000)
	tbl, err := schemaSvc.CreateTable(ctx, "Calc Customers", name)
	require.NoError(t, err)
	defer func() { _ = schemaSvc.DeleteTable(ctx, tbl.ID) }()

	add := func(field, label string, kind dbal.AgnosticFieldType, formula *string) *schema.ColumnMetadata {
		col, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name:               field,
			DisplayName:        label,
			FieldType:          kind,
			IsNullable:         true,
			CalculationFormula: formula,
		})
		require.NoError(t, err)
		return col
	}

	add("first_name", "First Name", dbal.FieldTypeText, nil)
	add("last_name", "Last Name", dbal.FieldTypeText, nil)
	add("customer_type", "Customer Type", dbal.FieldTypeText, nil)
	annualFee := add("annual_fee", "Annual Fee", dbal.FieldTypeCalculation,
		calcJSON(`IF(customer_type = "Continuing"; 100; 200)`, "Number"))
	add("full_name", "Full Name", dbal.FieldTypeCalculation,
		calcJSON(`UPPER(last_name) & ", " & first_name`, "Text"))

	t.Run("the formula fills the field when a record is created", func(t *testing.T) {
		row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"first_name":    "Mary",
			"last_name":     "Smith",
			"customer_type": "Continuing",
		})
		require.NoError(t, err)
		require.Equal(t, "100", fmt.Sprintf("%v", row["annual_fee"]))
		require.Equal(t, "SMITH, Mary", row["full_name"])
	})

	t.Run("changing a field the formula reads updates the result", func(t *testing.T) {
		row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"first_name":    "William",
			"last_name":     "Johnson",
			"customer_type": "Continuing",
		})
		require.NoError(t, err)
		id := row["id"].(string)
		require.Equal(t, "100", fmt.Sprintf("%v", row["annual_fee"]))

		updated, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"customer_type": "New"})
		require.NoError(t, err)
		require.Equal(t, "200", fmt.Sprintf("%v", updated["annual_fee"]))

		// And a change to a field the other formula reads.
		updated, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"last_name": "Johnston"})
		require.NoError(t, err)
		require.Equal(t, "JOHNSTON, William", updated["full_name"])
	})

	t.Run("a value sent for a calculation field is ignored", func(t *testing.T) {
		row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"first_name":    "Sophie",
			"last_name":     "Tang",
			"customer_type": "New",
			"annual_fee":    "1",
		})
		require.NoError(t, err)
		require.Equal(t, "200", fmt.Sprintf("%v", row["annual_fee"]),
			"the formula owns the value, not the caller")
	})

	t.Run("a numeric result is stored as a number, so it sorts as one", func(t *testing.T) {
		found, err := dataSvc.ListRows(ctx, name, data.QueryOptions{
			Limit: 100,
			Sort:  []data.SortField{{Field: "annual_fee", Descending: true}, {Field: "last_name"}},
		})
		require.NoError(t, err)
		require.NotEmpty(t, found)
		// 200 before 100: as text, "100" would come first.
		require.Equal(t, "200", fmt.Sprintf("%v", found[0]["annual_fee"]))
	})

	t.Run("a calculation field can be found on", func(t *testing.T) {
		found, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "annual_fee", Operator: ">=", Value: 200}}},
		}, data.QueryOptions{Sort: []data.SortField{{Field: "last_name"}}})
		require.NoError(t, err)
		require.NotEmpty(t, found)
		for _, row := range found {
			require.Equal(t, "200", fmt.Sprintf("%v", row["annual_fee"]))
		}
	})

	t.Run("changing the formula recomputes the records already stored", func(t *testing.T) {
		_, err := schemaSvc.UpdateColumn(ctx, tbl.ID, annualFee.ID, schema.UpdateColumnOptions{
			DisplayName:        "Annual Fee",
			UpdateCalculation:  true,
			CalculationFormula: calcJSON(`IF(customer_type = "Continuing"; 150; 250)`, "Number"),
		})
		require.NoError(t, err)

		written, err := dataSvc.RecalculateTable(ctx, name)
		require.NoError(t, err)
		require.Greater(t, written, 0)

		rows, err := dataSvc.ListRows(ctx, name, data.QueryOptions{Limit: 100, SortBy: "last_name"})
		require.NoError(t, err)
		for _, row := range rows {
			want := "250"
			if fmt.Sprintf("%v", row["customer_type"]) == "Continuing" {
				want = "150"
			}
			require.Equal(t, want, fmt.Sprintf("%v", row["annual_fee"]),
				"record %v was not recomputed", row["last_name"])
		}
	})

	t.Run("a formula that does not parse is refused", func(t *testing.T) {
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name:               "broken",
			DisplayName:        "Broken",
			FieldType:          dbal.FieldTypeCalculation,
			IsNullable:         true,
			CalculationFormula: calcJSON(`1 +`, "Number"),
		})
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("a formula reading a field the table does not have is refused", func(t *testing.T) {
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name:               "unknown_reader",
			DisplayName:        "Unknown Reader",
			FieldType:          dbal.FieldTypeCalculation,
			IsNullable:         true,
			CalculationFormula: calcJSON(`salary * 2`, "Number"),
		})
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		require.Contains(t, err.Error(), "salary")
	})

	t.Run("a formula that reads itself is refused", func(t *testing.T) {
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name:               "self_reader",
			DisplayName:        "Self Reader",
			FieldType:          dbal.FieldTypeCalculation,
			IsNullable:         true,
			CalculationFormula: calcJSON(`self_reader + 1`, "Number"),
		})
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		require.Contains(t, err.Error(), "depend on each other")
	})

	t.Run("a calculation may read another calculation", func(t *testing.T) {
		add("fee_label", "Fee Label", dbal.FieldTypeCalculation,
			calcJSON(`"Fee " & annual_fee & " for " & full_name`, "Text"))

		row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"first_name":    "Le",
			"last_name":     "Nguyen",
			"customer_type": "Continuing",
		})
		require.NoError(t, err)
		require.Equal(t, "Fee 150 for NGUYEN, Le", row["fee_label"],
			"the dependent calculation must see the value just computed")
	})

	t.Run("a ring of calculations is refused", func(t *testing.T) {
		ring := add("ring_a", "Ring A", dbal.FieldTypeCalculation, calcJSON(`first_name`, "Text"))
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name:               "ring_b",
			DisplayName:        "Ring B",
			FieldType:          dbal.FieldTypeCalculation,
			IsNullable:         true,
			CalculationFormula: calcJSON(`ring_a`, "Text"),
		})
		require.NoError(t, err)

		// Closing the ring: ring_a would now read ring_b, which reads ring_a.
		_, err = schemaSvc.UpdateColumn(ctx, tbl.ID, ring.ID, schema.UpdateColumnOptions{
			DisplayName:        "Ring A",
			UpdateCalculation:  true,
			CalculationFormula: calcJSON(`ring_b`, "Text"),
		})
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		require.Contains(t, err.Error(), "depend on each other")
	})
}
