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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The left side's options of a relationship whose left side is another
// solution's table (#107): Peer's accounts on the left, this solution's
// invoices on the right, so creating and deleting through the left side's
// options writes in Peer.
func TestExternalLeftSideOptions_CreateAndDeleteInTheOtherSolution(t *testing.T) {
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		testdb.Unavailable(t, "database not available:", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
	}
	open := func(prefix string) (string, dbal.DatabaseDriver) {
		name := fmt.Sprintf("f4b_%s_%d", prefix, uniqueTestStamp())
		require.NoError(t, mgr.CreateDatabase(ctx, name))
		t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
		d, err := mgr.DriverFor(ctx, name)
		require.NoError(t, err)
		require.NoError(t, schema.NewService(d).EnsureSystemTables(ctx))
		return name, d
	}
	homeDB, home := open("xside_home")
	peerDB, peer := open("xside_peer")
	homeSchema, peerSchema := schema.NewService(home), schema.NewService(peer)

	accounts, err := peerSchema.CreateTable(ctx, "accounts", "accounts")
	require.NoError(t, err)
	_, err = peerSchema.AddColumn(ctx, accounts.ID, schema.ColumnMetadata{Name: "code", DisplayName: "code", FieldType: dbal.FieldTypeText, IsNullable: true})
	require.NoError(t, err)
	invoices, err := homeSchema.CreateTable(ctx, "invoices", "invoices")
	require.NoError(t, err)
	accountCode, err := homeSchema.AddColumn(ctx, invoices.ID, schema.ColumnMetadata{Name: "account_code", DisplayName: "account_code", FieldType: dbal.FieldTypeText, IsNullable: true})
	require.NoError(t, err)
	occurrences, err := homeSchema.ListTableOccurrences(ctx)
	require.NoError(t, err)
	require.Len(t, occurrences, 1)

	ref, err := homeSchema.CreateFileReference(ctx, schema.FileReferenceInput{Name: "Peer"})
	require.NoError(t, err)
	ext, err := homeSchema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "invoice_accounts", External: &schema.ExternalOccurrenceInput{FileReference: "Peer"},
	})
	require.NoError(t, err)
	rel, err := homeSchema.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "accounts_invoices", LeftOccurrenceID: ext.ID, RightOccurrenceID: occurrences[0].ID,
		Predicates: []schema.RelationshipPredicate{
			{LeftColumnName: "code", Operator: "=", RightColumnID: accountCode.ID},
		},
		LeftAllowCreation: true, LeftCascadeDelete: true,
	})
	require.NoError(t, err)
	_, err = homeSchema.BindFileReference(ctx, ref.ID, peerDB, peerSchema, map[string]string{"invoice_accounts": "accounts"})
	require.NoError(t, err)

	peerData := data.NewService(peer).WithDatabase(peerDB)
	homeData := data.NewService(home).WithDatabase(homeDB).WithPeers(staticPeers{peerDB: peerData})
	codes := func() []string {
		rows, err := peerData.ListRows(ctx, "accounts", data.QueryOptions{})
		require.NoError(t, err)
		out := []string{}
		for _, r := range rows {
			out = append(out, fmt.Sprint(r["code"]))
		}
		return out
	}

	invoice, err := homeData.InsertRow(ctx, "invoices", map[string]interface{}{"account_code": "A-1"})
	require.NoError(t, err)
	created, err := homeData.CreateRelatedRow(ctx, "invoices", fmt.Sprint(invoice["id"]), rel.ID, ext.ID, nil)
	require.NoError(t, err, "the left side allows creation of its records, in Peer")
	assert.Equal(t, "A-1", created["code"])
	_, err = peerData.InsertRow(ctx, "accounts", map[string]interface{}{"code": "B-2"})
	require.NoError(t, err)

	require.NoError(t, homeData.DeleteRow(ctx, "invoices", fmt.Sprint(invoice["id"])))
	assert.Equal(t, []string{"B-2"}, codes(), "the invoice's account went with it, in Peer")
}
