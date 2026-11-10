package schema_test

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

// validTransfer is a definition that passes every structural check, so each
// test can spoil exactly the one thing it is about.
func validTransfer(name string) schema.TransferInput {
	return schema.TransferInput{
		Name:        name,
		Source:      schema.TransferEnd{Table: "surveys"},
		Destination: schema.TransferEnd{Database: "results_db", Table: "surveys"},
		Mappings: []schema.TransferMapping{
			{SourceField: "survey_code", DestinationField: "survey_code"},
			{SourceField: "title", DestinationField: "label"},
		},
		MatchFields: []string{"survey_code"},
		Mode:        schema.TransferModeUpdateMatching,
	}
}

func transferTestService(t *testing.T) (*schema.Service, context.Context) {
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

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	return svc, ctx
}

// uniqueTransferName keeps concurrent runs of the suite out of each other's
// way: the catalog makes the name unique across the whole solution.
func uniqueTransferName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s_%d", t.Name(), time.Now().UnixNano()%1000000)
}

func TestCreateTransfer_StoresAndReadsBackTheWholeDefinition(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	in := validTransfer(uniqueTransferName(t))
	in.FoundSet = []data.FindRequest{{
		Criteria: []data.FindCriterion{{FieldName: "status", Operator: "=", Value: "ready"}},
	}}
	in.Sort = []data.SortField{{Field: "survey_code"}}
	in.OnUnmatched = schema.TransferUnmatchedSkip
	in.OnRejected = schema.TransferRejectedSkip
	in.DateOrder = string(data.DateDMY)

	created, err := svc.CreateTransfer(ctx, in, "account-1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), created.ID) })

	read, err := svc.GetTransfer(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, in.Name, read.Name)
	assert.Equal(t, "", read.Source.Database, "a local end stays local")
	assert.True(t, read.Source.IsLocal())
	assert.Equal(t, "surveys", read.Source.Table)
	assert.Equal(t, "results_db", read.Destination.Database)
	assert.Equal(t, in.Mappings, read.Mappings)
	assert.Equal(t, []string{"survey_code"}, read.MatchFields)
	assert.Equal(t, schema.TransferModeUpdateMatching, read.Mode)
	assert.Equal(t, schema.TransferUnmatchedSkip, read.OnUnmatched)
	assert.Equal(t, schema.TransferRejectedSkip, read.OnRejected)
	assert.Equal(t, string(data.DateDMY), read.DateOrder)
	assert.Equal(t, "account-1", read.CreatedBy)
	require.Len(t, read.FoundSet, 1)
	require.Len(t, read.FoundSet[0].Criteria, 1)
	assert.Equal(t, "status", read.FoundSet[0].Criteria[0].FieldName)
	require.Len(t, read.Sort, 1)
	assert.Equal(t, "survey_code", read.Sort[0].Field)

	// A script step names its transfer, so a name has to resolve one, and
	// case is not part of the name.
	byName, err := svc.GetTransferByName(ctx, in.Name)
	require.NoError(t, err)
	assert.Equal(t, created.ID, byName.ID)
}

func TestCreateTransfer_DefaultsAreTheSafeOnes(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	in := validTransfer(uniqueTransferName(t))
	in.Mode = ""
	in.OnUnmatched = ""
	in.OnRejected = ""
	in.DateOrder = ""

	created, err := svc.CreateTransfer(ctx, in, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), created.ID) })

	// Updating matching records is the replayable mode, and aborting on a
	// rejected row is the all-or-nothing contract an import already follows.
	assert.Equal(t, schema.TransferModeUpdateMatching, created.Mode)
	assert.Equal(t, schema.TransferUnmatchedAdd, created.OnUnmatched)
	assert.Equal(t, schema.TransferRejectedAbort, created.OnRejected)
	assert.Equal(t, string(data.DateISO), created.DateOrder)
}

func TestCreateTransfer_RefusesDefinitionsThatCouldNotRun(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	cases := []struct {
		why   string
		spoil func(*schema.TransferInput)
	}{
		{"no name", func(in *schema.TransferInput) { in.Name = "  " }},
		{"no source table", func(in *schema.TransferInput) { in.Source.Table = "" }},
		{"no destination table", func(in *schema.TransferInput) { in.Destination.Table = "" }},
		{"no mappings", func(in *schema.TransferInput) { in.Mappings = nil }},
		{"a mapping with no destination", func(in *schema.TransferInput) {
			in.Mappings = []schema.TransferMapping{{SourceField: "title"}}
		}},
		{"two sources sent to one destination", func(in *schema.TransferInput) {
			in.Mappings = append(in.Mappings, schema.TransferMapping{
				SourceField: "subtitle", DestinationField: "label",
			})
		}},
		{"a match field nothing is mapped to", func(in *schema.TransferInput) {
			in.MatchFields = []string{"external_ref"}
		}},
		{"matching on nothing", func(in *schema.TransferInput) { in.MatchFields = nil }},
		{"adding while matching on a key", func(in *schema.TransferInput) {
			in.Mode = schema.TransferModeAdd
		}},
		{"an unknown mode", func(in *schema.TransferInput) { in.Mode = "merge" }},
		{"an unknown unmatched policy", func(in *schema.TransferInput) { in.OnUnmatched = "quarantine" }},
		{"an unknown rejected policy", func(in *schema.TransferInput) { in.OnRejected = "ignore" }},
		{"an unknown date order", func(in *schema.TransferInput) { in.DateOrder = "ymd" }},
		{"an internal table as the destination", func(in *schema.TransferInput) {
			in.Destination.Table = "sys_users"
		}},
		{"an engine-internal database", func(in *schema.TransferInput) {
			in.Destination.Database = "template1"
		}},
		{"a database name that is not one", func(in *schema.TransferInput) {
			in.Destination.Database = "results db; DROP TABLE surveys"
		}},
		{"the same table at both ends", func(in *schema.TransferInput) {
			in.Destination = schema.TransferEnd{Table: "surveys"}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			in := validTransfer(uniqueTransferName(t))
			tc.spoil(&in)
			created, err := svc.CreateTransfer(ctx, in, "")
			if created != nil {
				t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), created.ID) })
			}
			require.ErrorIs(t, err, schema.ErrInvalidTransfer)
		})
	}
}

func TestCreateTransfer_RefusesASecondDefinitionWithTheSameName(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	name := uniqueTransferName(t)
	first, err := svc.CreateTransfer(ctx, validTransfer(name), "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), first.ID) })

	// A script step resolves a transfer by name, so two transfers may never
	// share one — not even in a different case.
	second := validTransfer(name)
	_, err = svc.CreateTransfer(ctx, second, "")
	require.ErrorIs(t, err, schema.ErrTransferExists)
}

func TestUpdateTransfer_ReplacesTheDefinitionAndKeepsItsOwner(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	created, err := svc.CreateTransfer(ctx, validTransfer(uniqueTransferName(t)), "account-1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), created.ID) })

	in := validTransfer(created.Name)
	in.Mode = schema.TransferModeAdd
	in.MatchFields = nil
	updated, err := svc.UpdateTransfer(ctx, created.ID, in)
	require.NoError(t, err)
	assert.Equal(t, schema.TransferModeAdd, updated.Mode)
	assert.Empty(t, updated.MatchFields)
	assert.Equal(t, "account-1", updated.CreatedBy)
	// The creation time is the one it was created with. It is compared with a
	// tolerance because the value an update reads back came from the database,
	// which keeps a timestamp to the microsecond, while the one Create
	// returned is the time.Now() it was built from, to the nanosecond.
	assert.WithinDuration(t, created.CreatedAt.UTC(), updated.CreatedAt.UTC(), time.Millisecond)
	assert.True(t, updated.UpdatedAt.After(created.CreatedAt.Add(-time.Millisecond)),
		"the update time moved forward")
}

func TestTransfers_NotFoundAndDelete(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	_, err := svc.GetTransfer(ctx, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, schema.ErrTransferNotFound)
	_, err = svc.GetTransferByName(ctx, "no transfer has this name")
	require.ErrorIs(t, err, schema.ErrTransferNotFound)
	require.ErrorIs(t, svc.DeleteTransfer(ctx, "00000000-0000-0000-0000-000000000000"),
		schema.ErrTransferNotFound)

	created, err := svc.CreateTransfer(ctx, validTransfer(uniqueTransferName(t)), "")
	require.NoError(t, err)
	require.NoError(t, svc.DeleteTransfer(ctx, created.ID))
	_, err = svc.GetTransfer(ctx, created.ID)
	require.ErrorIs(t, err, schema.ErrTransferNotFound)
}

func TestListTransfers_IncludesTheDefinitionByName(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	created, err := svc.CreateTransfer(ctx, validTransfer(uniqueTransferName(t)), "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), created.ID) })

	all, err := svc.ListTransfers(ctx)
	require.NoError(t, err)
	found := false
	for _, def := range all {
		if def.ID == created.ID {
			found = true
			assert.Equal(t, created.Name, def.Name)
			assert.Equal(t, created.Mappings, def.Mappings)
		}
	}
	assert.True(t, found, "the new transfer is in the list")
}

func TestTransferDefinition_FieldOrderFollowsTheMappings(t *testing.T) {
	def := &schema.TransferDefinition{Mappings: []schema.TransferMapping{
		{SourceField: "survey_code", DestinationField: "code"},
		{SourceField: "title", DestinationField: "label"},
	}}
	// The export reads the source fields in mapping order and the import
	// writes the destination fields in the same order, so the two lists are
	// the two ends of one column order.
	assert.Equal(t, []string{"survey_code", "title"}, def.SourceFields())
	assert.Equal(t, []string{"code", "label"}, def.DestinationFields())
}
