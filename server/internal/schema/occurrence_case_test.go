package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// A migrated table "Customers" is created as customers, and brings a default
// occurrence of that name. The file's own occurrence "Customers" is the same
// one in FileMaker's case-insensitive naming: it takes over the default
// instead of being created beside it, which MariaDB refused on its unique
// name and PostgreSQL accepted, leaving an invented second box (#146).
func TestImportSolution_OccurrenceDifferingOnlyInCaseTakesOverTheDefault(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	sol := newExternalSolution(t, ctx, mgr, "occ_case")

	file, err := sol.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Occurrence Case"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	bundle.Tables = append(bundle.Tables, schema.BundleTable{ID: "t-customers", Name: "customers", DisplayName: "Customers"})
	bundle.TableOccurrences = append(bundle.TableOccurrences,
		schema.BundleOccurrence{ID: "o-customers", BaseTableID: "t-customers", Name: "Customers", XPos: 40, YPos: 60},
		schema.BundleOccurrence{ID: "o-billing", BaseTableID: "t-customers", Name: "Billing Customers", XPos: 300, YPos: 60},
	)
	file, err = msgpack.Marshal(bundle)
	require.NoError(t, err)

	_, err = sol.svc.ImportSolution(ctx, file)
	require.NoError(t, err)

	occs, err := sol.svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	names := map[string]float64{}
	for _, o := range occs {
		names[o.Name] = o.XPos
	}
	assert.Equal(t, map[string]float64{"Customers": 40, "Billing Customers": 300}, names,
		"exactly the file's occurrences, spelled as the file spells them")
}
