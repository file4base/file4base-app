package schema_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestSolutionService_MessagePackSerialization(t *testing.T) {
	bundle := schema.SolutionBundle{
		Format:       "file4base_solution",
		Version:      "1.0",
		SolutionName: "Invoice Manager",
		DatabaseConnection: schema.DatabaseConnectionConfig{
			Engine:   "postgres",
			Host:     "localhost",
			Port:     5432,
			Database: "invoices_db",
			User:     "file4base",
			Password: "dev_password",
		},
		Tables: []schema.TableMetadata{
			{
				ID:          "tbl-1",
				Name:        "invoices",
				DisplayName: "Invoices",
				Columns: []schema.ColumnMetadata{
					{
						ID:           "col-1",
						TableID:      "tbl-1",
						Name:         "id",
						DisplayName:  "ID",
						FieldType:    "TEXT",
						IsPrimaryKey: true,
					},
					{
						ID:          "col-2",
						TableID:     "tbl-1",
						Name:        "amount",
						DisplayName: "Amount",
						FieldType:   "NUMBER",
					},
				},
			},
		},
		Users: []schema.UserAccount{
			{
				ID:       "usr-1",
				Username: "admin",
				Role:     "Full Access",
			},
		},
	}

	// Pack to MessagePack
	bytes, err := msgpack.Marshal(bundle)
	require.NoError(t, err)
	assert.NotEmpty(t, bytes)

	// Unpack from MessagePack
	var decoded schema.SolutionBundle
	err = msgpack.Unmarshal(bytes, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "file4base_solution", decoded.Format)
	assert.Equal(t, "Invoice Manager", decoded.SolutionName)
	assert.Equal(t, "invoices_db", decoded.DatabaseConnection.Database)
	assert.Equal(t, "file4base", decoded.DatabaseConnection.User)
	assert.Len(t, decoded.Tables, 1)
	assert.Equal(t, "invoices", decoded.Tables[0].Name)
	assert.Len(t, decoded.Tables[0].Columns, 2)
}

func TestDatabaseDataBundle_MessagePackSerialization(t *testing.T) {
	dataBundle := schema.DatabaseDataBundle{
		Format:       "file4base_data",
		Version:      "1.0",
		DatabaseName: "invoices_db",
		TablesData: map[string][]map[string]interface{}{
			"invoices": {
				{"id": "inv-1", "amount": 100.5},
				{"id": "inv-2", "amount": 250.0},
			},
		},
	}

	bytes, err := msgpack.Marshal(dataBundle)
	require.NoError(t, err)
	assert.NotEmpty(t, bytes)

	var decoded schema.DatabaseDataBundle
	err = msgpack.Unmarshal(bytes, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "file4base_data", decoded.Format)
	assert.Equal(t, "invoices_db", decoded.DatabaseName)
	assert.Len(t, decoded.TablesData["invoices"], 2)
}
