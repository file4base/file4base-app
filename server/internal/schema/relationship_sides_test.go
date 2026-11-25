package schema_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// Each side of a relationship has its own options, as in FileMaker. The
// columns of the left side's were added to an existing catalog, and travel
// in a solution file.

func sideOptionsSolution(t *testing.T, s *externalSolution) (*schema.RelationshipMetadata, func(*testing.T) schema.RelationshipMetadata) {
	t.Helper()
	ctx := t.Context()
	projects, p := s.table(t, ctx, "projects", "code", "title")
	tasks, k := s.table(t, ctx, "tasks", "project_code", "due")
	sort, leftSort := "due", "-title"
	rel, err := s.svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:             "projects_tasks",
		LeftOccurrenceID: s.occurrenceOf(t, ctx, projects.ID), LeftColumnID: p["code"],
		RightOccurrenceID: s.occurrenceOf(t, ctx, tasks.ID), RightColumnID: k["project_code"],
		Operator:      "=",
		CascadeDelete: true, SortRelated: &sort,
		LeftAllowCreation: true, LeftSortRelated: &leftSort,
	})
	require.NoError(t, err)
	reload := func(t *testing.T) schema.RelationshipMetadata {
		t.Helper()
		rels, err := s.svc.ListRelationships(ctx)
		require.NoError(t, err)
		require.Len(t, rels, 1)
		return rels[0]
	}
	return rel, reload
}

func TestRelationshipSideOptions_StoredAndUpdatedPerSide(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	s := newExternalSolution(t, ctx, mgr, "rel_sides")
	rel, reload := sideOptionsSolution(t, s)

	got := reload(t)
	assert.False(t, got.AllowCreation)
	assert.True(t, got.CascadeDelete)
	assert.Equal(t, "due", got.SortRelated)
	assert.True(t, got.LeftAllowCreation)
	assert.False(t, got.LeftCascadeDelete)
	assert.Equal(t, "-title", got.LeftSortRelated)

	on, off, clear := true, false, ""
	_, err := s.svc.UpdateRelationship(ctx, rel.ID, schema.UpdateRelationshipInput{
		LeftAllowCreation: &off, LeftCascadeDelete: &on, LeftSortRelated: &clear,
	})
	require.NoError(t, err)
	got = reload(t)
	assert.False(t, got.LeftAllowCreation)
	assert.True(t, got.LeftCascadeDelete)
	assert.Empty(t, got.LeftSortRelated, "an empty sort clears it")
	assert.True(t, got.CascadeDelete, "the right side's options are left as they were")
	assert.Equal(t, "due", got.SortRelated)
}

func TestRelationshipSideOptions_RoundTripThroughASolutionFile(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	src := newExternalSolution(t, ctx, mgr, "rel_sides_src")
	sideOptionsSolution(t, src)

	file, err := src.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Side Options"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	require.Len(t, bundle.Relationships, 1)
	assert.True(t, bundle.Relationships[0].LeftAllowCreation)
	assert.Equal(t, "-title", bundle.Relationships[0].LeftSortRelated)

	dst := newExternalSolution(t, ctx, mgr, "rel_sides_dst")
	_, err = dst.svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	rels, err := dst.svc.ListRelationships(ctx)
	require.NoError(t, err)
	require.Len(t, rels, 1)
	got := rels[0]
	assert.False(t, got.AllowCreation)
	assert.True(t, got.CascadeDelete)
	assert.Equal(t, "due", got.SortRelated)
	assert.True(t, got.LeftAllowCreation)
	assert.False(t, got.LeftCascadeDelete)
	assert.Equal(t, "-title", got.LeftSortRelated)

	t.Run("a file written before the left side had options opens with none", func(t *testing.T) {
		var raw map[string]interface{}
		require.NoError(t, msgpack.Unmarshal(file, &raw))
		for _, r := range raw["relationships"].([]interface{}) {
			m := r.(map[string]interface{})
			require.Contains(t, m, "left_allow_creation", "the file names the left side's options")
			delete(m, "left_allow_creation")
			delete(m, "left_cascade_delete")
			delete(m, "left_sort_related")
		}
		older, err := msgpack.Marshal(raw)
		require.NoError(t, err)

		old := newExternalSolution(t, ctx, mgr, "rel_sides_old")
		_, err = old.svc.ImportSolution(ctx, older)
		require.NoError(t, err)
		rels, err := old.svc.ListRelationships(ctx)
		require.NoError(t, err)
		require.Len(t, rels, 1)
		assert.True(t, rels[0].CascadeDelete)
		assert.Equal(t, "due", rels[0].SortRelated)
		assert.False(t, rels[0].LeftAllowCreation)
		assert.False(t, rels[0].LeftCascadeDelete)
		assert.Empty(t, rels[0].LeftSortRelated)
	})
}

// A catalog created before the left side had options gets their columns the
// first time a relationship is read, and its relationships keep acting as
// they did: no option on their left side.
func TestRelationshipSideOptions_UpgradeAnOlderCatalog(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	old := newExternalSolution(t, ctx, mgr, "rel_sides_upgrade")
	rel, _ := sideOptionsSolution(t, old)
	for _, column := range []string{"left_allow_creation", "left_cascade_delete", "left_sort_related"} {
		_, err := old.driver.DB().ExecContext(ctx, "ALTER TABLE sys_relationships DROP COLUMN "+column)
		require.NoError(t, err)
	}

	// A connection pool of its own: nothing in this process has upgraded the
	// catalog through it.
	other, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	fresh, err := other.DriverFor(ctx, old.name)
	require.NoError(t, err)
	svc := schema.NewService(fresh)

	rels, err := svc.ListRelationships(ctx)
	require.NoError(t, err, "relationships are read from a catalog nobody signed in to since the upgrade")
	require.Len(t, rels, 1)
	assert.Equal(t, rel.ID, rels[0].ID)
	assert.True(t, rels[0].CascadeDelete, "the right side's options are kept")
	assert.Equal(t, "due", rels[0].SortRelated)
	assert.False(t, rels[0].LeftAllowCreation, "an existing relationship has no option on its left side")
	assert.False(t, rels[0].LeftCascadeDelete)
	assert.Empty(t, rels[0].LeftSortRelated)

	require.NoError(t, svc.EnsureSystemTables(ctx))
	require.NoError(t, svc.EnsureSystemTables(ctx), "the upgrade is idempotent")

	on := true
	_, err = svc.UpdateRelationship(ctx, rel.ID, schema.UpdateRelationshipInput{LeftCascadeDelete: &on})
	require.NoError(t, err)
	rels, err = svc.ListRelationships(ctx)
	require.NoError(t, err)
	assert.True(t, rels[0].LeftCascadeDelete)
}
