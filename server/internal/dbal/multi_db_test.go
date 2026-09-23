package dbal_test

import (
	"context"
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiDatabaseManager_BuildDSN(t *testing.T) {
	mgr, err := dbal.NewMultiDatabaseManager(dbal.EnginePostgres, "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable")
	require.NoError(t, err)

	assert.Equal(t, "file4base_dev", mgr.ActiveDatabase())

	dsnNew := mgr.BuildDSN("invoices_db")
	assert.Equal(t, "postgres://file4base:dev_password@localhost:5432/invoices_db?sslmode=disable", dsnNew)
}

func TestMultiDatabaseManager_InvalidNames(t *testing.T) {
	mgr, err := dbal.NewMultiDatabaseManager(dbal.EnginePostgres, "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable")
	require.NoError(t, err)

	ctx := context.Background()

	// Invalid characters or injection attempts should be rejected
	err = mgr.CreateDatabase(ctx, "invoices; DROP DATABASE file4base_dev;")
	assert.Error(t, err)

	_, err = mgr.SetActiveDatabase(ctx, "123invalid")
	assert.Error(t, err)
}
