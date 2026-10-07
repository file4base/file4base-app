package data_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func companiesToCustomers() *data.Relationship {
	return &data.Relationship{
		ID:   "rel-1",
		Name: "companies_customers",
		Left: data.RelationSide{
			OccurrenceID: "occ-companies", OccurrenceName: "Companies",
			TableName: "companies", Column: "company",
		},
		Right: data.RelationSide{
			OccurrenceID: "occ-customers", OccurrenceName: "Customers",
			TableName: "customers", Column: "company",
		},
		Operator: "=",
	}
}

func TestRelationshipSides(t *testing.T) {
	rel := companiesToCustomers()

	t.Run("named by occurrence id", func(t *testing.T) {
		source, target, err := rel.Sides("companies", "occ-customers")
		require.NoError(t, err)
		assert.Equal(t, "companies", source.TableName)
		assert.Equal(t, "customers", target.TableName)
	})

	t.Run("named by occurrence name", func(t *testing.T) {
		_, target, err := rel.Sides("customers", "Companies")
		require.NoError(t, err)
		assert.Equal(t, "companies", target.TableName)
	})

	t.Run("the other table is taken when no side is named", func(t *testing.T) {
		_, target, err := rel.Sides("companies", "")
		require.NoError(t, err)
		assert.Equal(t, "customers", target.TableName)

		_, target, err = rel.Sides("customers", "")
		require.NoError(t, err)
		assert.Equal(t, "companies", target.TableName)
	})

	t.Run("a table the relationship does not reach is refused", func(t *testing.T) {
		_, _, err := rel.Sides("invoices", "")
		assert.ErrorIs(t, err, data.ErrRelationshipNotFound)
	})

	t.Run("a side that is not one of the two is refused", func(t *testing.T) {
		_, _, err := rel.Sides("companies", "occ-invoices")
		assert.ErrorIs(t, err, data.ErrRelationshipNotFound)
	})

	t.Run("naming a side the record is not on is refused", func(t *testing.T) {
		// Asking for the customers of a customer record: the near side of the
		// relationship would be Companies, which this record is not in.
		_, _, err := rel.Sides("customers", "occ-customers")
		assert.ErrorIs(t, err, data.ErrRelationshipNotFound)
	})
}

// A relationship joining a table to itself has no "other" table, so the side
// to read has to be named rather than guessed.
func TestRelationshipSidesOfASelfJoin(t *testing.T) {
	rel := &data.Relationship{
		Name: "employees_managers",
		Left: data.RelationSide{
			OccurrenceID: "occ-employees", OccurrenceName: "Employees",
			TableName: "employees", Column: "manager_id",
		},
		Right: data.RelationSide{
			OccurrenceID: "occ-managers", OccurrenceName: "Managers",
			TableName: "employees", Column: "id",
		},
		Operator: "=",
	}

	_, _, err := rel.Sides("employees", "")
	assert.ErrorIs(t, err, data.ErrRelationshipNotFound)

	source, target, err := rel.Sides("employees", "occ-managers")
	require.NoError(t, err)
	assert.Equal(t, "manager_id", source.Column)
	assert.Equal(t, "id", target.Column)
}

func TestParseSortSpec(t *testing.T) {
	assert.Equal(t,
		[]data.SortField{{Field: "company"}, {Field: "fee_paid", Descending: true}},
		data.ParseSortSpec("company, -fee_paid"))

	assert.Equal(t, []data.SortField{{Field: "company"}}, data.ParseSortSpec("+company"))
	assert.Nil(t, data.ParseSortSpec(""))
	assert.Nil(t, data.ParseSortSpec(" , , - "))
}
