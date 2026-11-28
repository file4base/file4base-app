package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/fmdapi"
)

// The reference file migrates with the record IDs FileMaker Server answers
// for its records, and none of them modified (#208): every recordId and
// modId a read fixture recorded is the migrated record's.
func TestDataAPIReference_KeepsRecordIdentities(t *testing.T) {
	store, err := control.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, store.Close()) })
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		ControlStore:                store,
		DataAPI:                     &fmdapi.Config{Databases: []fmdapi.DatabaseAlias{{Name: dataAPIDatabase, Database: dataAPIDatabase}}},
	})
	_, exists, err := h.mgr.LookupDatabase(t.Context(), dataAPIDatabase)
	require.NoError(t, err)
	require.False(t, exists, "a database named %s already exists on the test server; this test creates and drops it, so drop it first", dataAPIDatabase)
	created := false
	t.Cleanup(func() {
		if created {
			assert.NoError(t, h.mgr.DropDatabase(context.Background(), dataAPIDatabase))
		}
	})
	require.NoError(t, migrateDataAPIReference(t, h, &created))

	// Each layout's table, and the serial number field that numbers its
	// records as FileMaker does.
	tables := map[string][2]string{
		"API_Customers": {"customers", "customerid"}, "API_Customers_Nested": {"customers", "customerid"},
		"API_Customers_Portals": {"customers", "customerid"}, "API_Customers_Restricted": {"customers", "customerid"},
		"Customers": {"customers", "customerid"}, "API_Products": {"products", "productid"},
		"API_Orders": {"orders", "orderid"}, "API_OrderLines": {"orderlines", "lineid"},
		"API_Contacts": {"contacts", "contactid"},
	}
	// What the read fixtures recorded: by table, record ID, the
	// modification count.
	recorded := map[string]map[string]string{}
	files, err := filepath.Glob(filepath.Join(dataAPIDir, "fixtures", "records", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		var fixture struct {
			Steps []struct {
				Request struct {
					Path string `json:"path"`
				} `json:"request"`
				Response struct {
					Body json.RawMessage `json:"body"`
				} `json:"response"`
			} `json:"steps"`
		}
		require.NoError(t, json.Unmarshal(raw, &fixture))
		for _, step := range fixture.Steps {
			_, rest, found := strings.Cut(step.Request.Path, "/layouts/")
			layout, _, _ := strings.Cut(rest, "/")
			table, ok := tables[layout]
			if !found || !ok {
				continue
			}
			var body struct {
				Response struct {
					Data []struct {
						RecordID string `json:"recordId"`
						ModID    string `json:"modId"`
					} `json:"data"`
				} `json:"response"`
			}
			if json.Unmarshal(step.Response.Body, &body) != nil {
				continue
			}
			for _, record := range body.Response.Data {
				if record.RecordID == "" {
					continue
				}
				if recorded[table[0]] == nil {
					recorded[table[0]] = map[string]string{}
				}
				recorded[table[0]][record.RecordID] = record.ModID
			}
		}
	}
	require.Len(t, recorded, 5, "every table's records were recorded")

	name, _, err := h.mgr.LookupDatabase(t.Context(), dataAPIDatabase)
	require.NoError(t, err)
	driver, err := h.mgr.DriverFor(t.Context(), name)
	require.NoError(t, err)
	svc := data.NewService(driver)
	serials := map[string]string{}
	for _, table := range tables {
		serials[table[0]] = table[1]
	}
	for table, serial := range serials {
		rows, err := svc.ListRows(t.Context(), table, data.QueryOptions{Limit: data.MaxPageSize})
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, fmt.Sprint(row["id"]))
		}
		identities, err := svc.RecordIdentities(t.Context(), table, ids)
		require.NoError(t, err)
		migrated := map[string]string{}
		for _, row := range rows {
			identity := identities[fmt.Sprint(row["id"])]
			recordID := fmt.Sprint(identity.RecordID)
			assert.Equal(t, fmt.Sprint(row[serial]), recordID, "%s: FileMaker numbers this file's records as its serial numbers", table)
			migrated[recordID] = fmt.Sprint(identity.ModID)
		}
		for recordID, modID := range recorded[table] {
			assert.Equal(t, modID, migrated[recordID], "%s record %s", table, recordID)
		}
	}
}
