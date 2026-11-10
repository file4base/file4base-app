package schema_test

import (
	"context"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileReferences_StoreMapAndRefuse(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	name := uniqueTransferName(t)
	// A reference can start unmapped: that is what the migration creates.
	ref, err := svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: name})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteFileReference(context.Background(), ref.ID) })
	assert.False(t, ref.IsMapped())

	// A script step names it, in any case.
	byName, err := svc.GetFileReferenceByName(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, ref.ID, byName.ID)

	mapped, err := svc.UpdateFileReference(ctx, ref.ID, schema.FileReferenceInput{Name: name, Database: "results_db"})
	require.NoError(t, err)
	assert.True(t, mapped.IsMapped())
	read, err := svc.GetFileReference(ctx, ref.ID)
	require.NoError(t, err)
	assert.Equal(t, "results_db", read.Database)

	_, err = svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: name})
	require.ErrorIs(t, err, schema.ErrFileReferenceExists, "names are unique: a step resolves by name")

	for why, in := range map[string]schema.FileReferenceInput{
		"no name":                     {Name: " "},
		"an engine-internal database": {Name: name + "_x", Database: "postgres"},
		"a name that is no database":  {Name: name + "_y", Database: "results db; drop"},
	} {
		_, err := svc.CreateFileReference(ctx, in)
		assert.ErrorIs(t, err, schema.ErrInvalidFileReference, why)
	}

	_, err = svc.GetFileReferenceByName(ctx, "no reference has this name")
	require.ErrorIs(t, err, schema.ErrFileReferenceNotFound)
}

// A file reference travels in the solution bundle, and restoring a bundle
// never repoints a reference the destination has already mapped (#107).
func TestFileReferences_RoundTripThroughASolutionFile(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}

	mappedName := uniqueTransferName(t) + "_mapped"
	unmappedName := uniqueTransferName(t) + "_unmapped"
	mapped, err := svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: mappedName, Database: "results_db"})
	require.NoError(t, err)
	unmapped, err := svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: unmappedName})
	require.NoError(t, err)

	bundle, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Dispatch"})
	require.NoError(t, err)
	decoded, err := schema.DecodeSolutionBundle(bundle)
	require.NoError(t, err)
	found := map[string]string{}
	for _, f := range decoded.FileReferences {
		found[f.Name] = f.Database
	}
	assert.Equal(t, "results_db", found[mappedName])
	assert.Contains(t, found, unmappedName)
	assert.Equal(t, "", found[unmappedName])

	// The destination already maps one of them elsewhere, and lost the other.
	_, err = svc.UpdateFileReference(ctx, mapped.ID, schema.FileReferenceInput{Name: mappedName, Database: "elsewhere_db"})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteFileReference(ctx, unmapped.ID))

	report, err := svc.ImportSolution(ctx, bundle)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, report.FileReferencesCreated, 1)

	kept, err := svc.GetFileReferenceByName(ctx, mappedName)
	require.NoError(t, err)
	assert.Equal(t, "elsewhere_db", kept.Database, "an existing mapping is never repointed by a restore")
	restored, err := svc.GetFileReferenceByName(ctx, unmappedName)
	require.NoError(t, err)
	assert.False(t, restored.IsMapped())

	t.Cleanup(func() {
		_ = svc.DeleteFileReference(context.Background(), kept.ID)
		_ = svc.DeleteFileReference(context.Background(), restored.ID)
	})
}
