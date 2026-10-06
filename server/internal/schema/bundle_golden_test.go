package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// goldenSolution is the reference solution written to
// testdata/server_solution_v2.f4p, which the Flutter tests read
// (client/test/solution_bundle_contract_test.dart).
func goldenSolution() schema.SolutionBundle {
	opts := `{"data_enabled":true,"data_value":"Open"}`
	parent := "s-if"
	return schema.SolutionBundle{
		Format: schema.SolutionFormat, Version: schema.SolutionVersion,
		SolutionName: "Invoices Pro", ExportedAt: "2026-10-06T09:00:00.123456Z",
		DatabaseConnection: schema.BundleConnection{Engine: "postgres", Host: "localhost", Port: 5432, Database: "invoices_db", User: "alice", SSLMode: "disable"},
		Tables: []schema.BundleTable{{
			ID: "t-inv", Name: "invoices", DisplayName: "Invoices",
			Columns: []schema.BundleColumn{
				{ID: "c-id", Name: "id", DisplayName: "ID", FieldType: "TEXT", IsPrimaryKey: true},
				{ID: "c-status", Name: "status", DisplayName: "Status", FieldType: "TEXT", IsNullable: true, DefaultValue: &opts},
			},
		}},
		TableOccurrences: []schema.BundleOccurrence{{ID: "o-inv", BaseTableID: "t-inv", Name: "invoices", XPos: 40, YPos: 60.5}},
		Relationships:    []schema.BundleRelationship{},
		Layouts: []schema.BundleLayout{{
			ID: "l-inv", Name: "Invoices Layout", TableOccurrenceID: "o-inv",
			Definition: map[string]interface{}{
				"width": int64(900), "transition": "fade",
				"objects": []interface{}{map[string]interface{}{"id": "btn", "type": "button", "x": int64(10), "y": 10.5, "tab_order": int64(1)}},
			},
		}},
		Scripts: []schema.BundleScript{{
			ID: "sc-archive", Name: "Archive", IsActive: true,
			Steps: []schema.BundleStep{
				{ID: "s-if", SequenceIdx: 1, StepType: "if", Params: map[string]interface{}{"calc": "1"}, IsEnabled: true},
				{ID: "s-dlg", SequenceIdx: 2, StepType: "show_dialog", Params: map[string]interface{}{"title": "Done"}, IsEnabled: false, ParentStepID: &parent},
			},
		}},
		Users: []schema.BundleAccount{{
			ID: "u-bob", Username: "bob", Role: "user", IsActive: false,
			Permissions: []schema.BundlePermission{{LayoutID: "l-inv", AccessLevel: "read_only"}},
		}},
		FileOptions: map[string]interface{}{"auto_login_enabled": true, "default_username": "alice"},
		PageSetup:   map[string]interface{}{"paper": "A4"},
	}
}

func TestGoldenServerSolutionFile(t *testing.T) {
	path := filepath.Join("testdata", "server_solution_v2.f4p")
	data, err := msgpack.Marshal(goldenSolution())
	require.NoError(t, err)
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		require.NoError(t, os.WriteFile(path, data, 0o644))
	}
	stored, err := os.ReadFile(path)
	require.NoError(t, err, "run with UPDATE_GOLDENS=1 to create the fixture")

	var want, got map[string]interface{}
	require.NoError(t, msgpack.Unmarshal(data, &want))
	require.NoError(t, msgpack.Unmarshal(stored, &got))
	assert.Equal(t, want, got, "the server's encoding of the reference solution changed; update the fixture and the client contract test together")

	decoded, err := schema.DecodeSolutionBundle(stored)
	require.NoError(t, err)
	assert.Equal(t, "Invoices Pro", decoded.SolutionName)
}

// Files written by the Flutter client (client/test/solution_bundle_contract_test.dart
// regenerates them) decode with the server's types.
func TestGoldenClientSolutionFiles(t *testing.T) {
	for _, name := range []string{"client_new_database_v2.f4p", "client_legacy_v1.f4p"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		require.NoError(t, err, name)
		b, err := schema.DecodeSolutionBundle(data)
		require.NoError(t, err, name)
		assert.Equal(t, schema.SolutionFormat, b.Format, name)
		assert.NotEmpty(t, b.DatabaseConnection.Database, name)
		require.NotEmpty(t, b.Users, name)
		assert.Equal(t, "owner", b.Users[0].Role, name)
	}
}
