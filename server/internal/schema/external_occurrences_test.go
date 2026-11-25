package schema_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/migration"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// externalSolution is a solution database of its own: external occurrences
// cross solutions, and a solution is a database.
type externalSolution struct {
	name   string
	driver dbal.DatabaseDriver
	svc    *schema.Service
}

func externalTestManager(t *testing.T) (*dbal.MultiDatabaseManager, context.Context) {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		t.Skip("database not available:", err)
		return nil, nil
	}
	t.Cleanup(func() { _ = mgr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return nil, nil
	}
	return mgr, ctx
}

func newExternalSolution(t *testing.T, ctx context.Context, mgr *dbal.MultiDatabaseManager, prefix string) *externalSolution {
	t.Helper()
	name := fmt.Sprintf("f4b_%s_%d", prefix, uniqueTestStamp())
	require.NoError(t, mgr.CreateDatabase(ctx, name))
	t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
	driver, err := mgr.DriverFor(ctx, name)
	require.NoError(t, err)
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	return &externalSolution{name: name, driver: driver, svc: svc}
}

func (s *externalSolution) table(t *testing.T, ctx context.Context, name string, fields ...string) (*schema.TableMetadata, map[string]string) {
	t.Helper()
	tbl, err := s.svc.CreateTable(ctx, name, name)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, f := range fields {
		col, err := s.svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: f, DisplayName: f, FieldType: dbal.FieldTypeText, IsNullable: true,
		})
		require.NoError(t, err)
		ids[f] = col.ID
	}
	return tbl, ids
}

func (s *externalSolution) occurrenceOf(t *testing.T, ctx context.Context, tableID string) string {
	t.Helper()
	occurrences, err := s.svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		if o.BaseTableID == tableID {
			return o.ID
		}
	}
	t.Fatalf("no occurrence of table %s", tableID)
	return ""
}

// An occurrence of another solution's table is stored unbound, bound to a
// table only when the operator or its fields single one out, verified
// against the other solution, and never read in place of a local table
// (#107).
func TestExternalOccurrences_BindVerifyAndRefuseReads(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	orders := newExternalSolution(t, ctx, mgr, "ext_orders")
	peer := newExternalSolution(t, ctx, mgr, "ext_peer")

	// The other solution: two tables hold both fields used through the
	// occurrence, a third holds only one.
	peer.table(t, ctx, "customers", "code", "full_name")
	peer.table(t, ctx, "customers_archive", "code", "full_name")
	peer.table(t, ctx, "contacts", "code")

	ordersTable, orderFields := orders.table(t, ctx, "orders", "customer_code")
	ordersOcc := orders.occurrenceOf(t, ctx, ordersTable.ID)

	ref, err := orders.svc.CreateFileReference(ctx, schema.FileReferenceInput{
		Name: "Peer", Paths: []string{"file:Peer.fmp12", "fmnet:/192.0.2.10/Peer.fmp12"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"file:Peer.fmp12", "fmnet:/192.0.2.10/Peer.fmp12"}, ref.Paths)

	ext, err := orders.svc.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "Order Customers", External: &schema.ExternalOccurrenceInput{FileReference: "Peer"},
	})
	require.NoError(t, err)
	require.NotNil(t, ext.External)
	assert.Empty(t, ext.BaseTableID)
	assert.Equal(t, "Peer", ext.External.FileReference)
	assert.Empty(t, ext.External.Table, "the table is not known until the reference is bound")

	_, err = orders.svc.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "Nowhere", External: &schema.ExternalOccurrenceInput{FileReference: "no such reference"},
	})
	assert.Error(t, err, "an external occurrence goes through a reference of this solution")

	rel, err := orders.svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_customers", LeftOccurrenceID: ordersOcc, RightOccurrenceID: ext.ID,
		Predicates: []schema.RelationshipPredicate{{
			LeftColumnID: orderFields["customer_code"], Operator: "=", RightColumnName: "Code",
		}},
	})
	require.NoError(t, err)
	preds := rel.Predicates
	require.Len(t, preds, 1)
	assert.Empty(t, preds[0].RightColumnID)
	assert.Equal(t, "code", preds[0].RightColumnName, "a field of the other solution is named, normalized")

	list, err := orders.svc.CreateValueList(ctx, schema.ValueListInput{
		Name: "Customer names", Kind: schema.ValueListFromField,
		SourceOccurrenceID: &ext.ID, SourceColumnName: "full_name",
	})
	require.NoError(t, err)
	assert.Nil(t, list.SourceTableID)
	assert.Equal(t, "full_name", list.SourceColumnName)

	_, err = orders.svc.CreateValueList(ctx, schema.ValueListInput{
		Name: "Local named", Kind: schema.ValueListFromField,
		SourceOccurrenceID: &ordersOcc, SourceColumnName: "customer_code",
	})
	assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions, "a field of this solution is given by id")

	// Unmapped: nothing to compare with.
	report, err := orders.svc.VerifyFileReference(ctx, ref.ID, nil)
	require.NoError(t, err)
	require.Len(t, report.Occurrences, 1)
	assert.Equal(t, schema.ExternalUnbound, report.Occurrences[0].Status)
	assert.Equal(t, []string{"code", "full_name"}, report.Occurrences[0].Fields)

	_, err = orders.svc.UpdateFileReference(ctx, ref.ID, schema.FileReferenceInput{Name: "Peer", Database: peer.name, Paths: ref.Paths})
	require.NoError(t, err)

	// Two tables hold every field: the table is not guessed.
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, nil)
	require.NoError(t, err)
	st := report.Occurrences[0]
	assert.Equal(t, schema.ExternalUnbound, st.Status)
	assert.Equal(t, []string{"customers", "customers_archive"}, st.Candidates)
	occ, err := orders.svc.GetTableOccurrence(ctx, ext.ID)
	require.NoError(t, err)
	assert.Empty(t, occ.External.Table, "an ambiguous bind saves nothing")

	// A table without one of the fields is a mismatch, said precisely.
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, map[string]string{"order customers": "contacts"})
	require.NoError(t, err)
	st = report.Occurrences[0]
	assert.Equal(t, schema.ExternalMismatch, st.Status)
	require.Len(t, st.Problems, 1)
	assert.Contains(t, st.Problems[0], "full_name")

	// The operator's choice binds it.
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, map[string]string{"Order Customers": "customers"})
	require.NoError(t, err)
	st = report.Occurrences[0]
	assert.Equal(t, schema.ExternalBound, st.Status, st.Problems)
	assert.Equal(t, "customers", st.Table)
	report, err = orders.svc.VerifyFileReference(ctx, ref.ID, peer.svc)
	require.NoError(t, err)
	assert.Equal(t, schema.ExternalBound, report.Occurrences[0].Status)

	// The relationship names the table in the other solution.
	reader := data.NewService(orders.driver)
	resolved, err := reader.Relationship(ctx, rel.ID)
	require.NoError(t, err)
	target := resolved.External()
	require.NotNil(t, target)
	assert.Equal(t, data.ExternalTarget{Occurrence: "Order Customers", Reference: "Peer", Database: peer.name, Table: "customers"}, *target)

	// Without a session group, no record is read through it, and nothing
	// local is read instead.
	_, _, err = orders.svc.ValueListItems(ctx, list.ID, nil, reader)
	require.ErrorIs(t, err, data.ErrExternalUnavailable)
	var unavailable *data.ExternalUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, "Order Customers", unavailable.Occurrence)
	assert.Equal(t, "Peer", unavailable.Reference)
	assert.Equal(t, peer.name, unavailable.Database)
	assert.Equal(t, data.ExternalMissing, unavailable.Reason)

	// Signed in to this solution only, the list says so.
	reader.WithPeers(staticPeers{})
	_, _, err = orders.svc.ValueListItems(ctx, list.ID, nil, reader)
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, data.ExternalNotSignedIn, unavailable.Reason)

	// With a session there, the list offers the values of the other solution's field.
	_, err = data.NewService(peer.driver).InsertRow(ctx, "customers", map[string]interface{}{"code": "C-1", "full_name": "Ada"})
	require.NoError(t, err)
	reader.WithPeers(staticPeers{peer.name: data.NewService(peer.driver)})
	items, _, err := orders.svc.ValueListItems(ctx, list.ID, nil, reader)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Ada", items[0].Value)

	// The reference cannot go while occurrences go through it.
	require.ErrorIs(t, orders.svc.DeleteFileReference(ctx, ref.ID), schema.ErrFileReferenceInUse)

	// The whole design travels in a solution file and is restored as it was.
	bundleBytes, err := orders.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Orders"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(bundleBytes)
	require.NoError(t, err)
	var exported *schema.BundleOccurrence
	for i := range bundle.TableOccurrences {
		if bundle.TableOccurrences[i].ID == ext.ID {
			exported = &bundle.TableOccurrences[i]
		}
	}
	require.NotNil(t, exported)
	require.NotNil(t, exported.External)
	assert.Equal(t, schema.BundleExternalOccurrence{FileReference: "Peer", Table: "customers"}, *exported.External)

	restored := newExternalSolution(t, ctx, mgr, "ext_restored")
	_, err = restored.svc.ImportSolution(ctx, bundleBytes)
	require.NoError(t, err)
	occurrences, err := restored.svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	var back *schema.TableOccurrence
	for i := range occurrences {
		if occurrences[i].Name == "Order Customers" {
			back = &occurrences[i]
		}
	}
	require.NotNil(t, back)
	require.NotNil(t, back.External)
	assert.Equal(t, "Peer", back.External.FileReference)
	assert.Equal(t, "customers", back.External.Table)
	lists, err := restored.svc.ListValueLists(ctx)
	require.NoError(t, err)
	require.Len(t, lists, 1)
	assert.Equal(t, "full_name", lists[0].SourceColumnName)
	require.NotNil(t, lists[0].SourceOccurrenceID)
	assert.Equal(t, back.ID, *lists[0].SourceOccurrenceID)

	// Importing it again changes nothing.
	_, err = restored.svc.ImportSolution(ctx, bundleBytes)
	require.NoError(t, err)
	again, err := restored.svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	assert.Len(t, again, len(occurrences))

	// A file whose occurrence of the same name shows another table of the
	// other solution is a conflict, not a silent retarget.
	for i := range bundle.TableOccurrences {
		if bundle.TableOccurrences[i].External != nil {
			bundle.TableOccurrences[i].External.Table = "customers_archive"
		}
	}
	retargeted, err := msgpack.Marshal(bundle)
	require.NoError(t, err)
	_, err = restored.svc.ImportSolution(ctx, retargeted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Order Customers")
	kept, err := restored.svc.GetTableOccurrence(ctx, back.ID)
	require.NoError(t, err)
	assert.Equal(t, "customers", kept.External.Table)
}

// A catalog created before external occurrences existed is brought to the
// new shape the first time it is used, and again by EnsureSystemTables,
// which then only reads it.
func TestExternalOccurrences_UpgradeAnOlderCatalog(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	old := newExternalSolution(t, ctx, mgr, "ext_upgrade")
	tbl, _ := old.table(t, ctx, "items", "code")

	mariadb := testdb.Engine() == dbal.EngineMariaDB
	stmts := []string{
		`ALTER TABLE sys_table_occurrences DROP COLUMN file_reference_id`,
		`ALTER TABLE sys_table_occurrences DROP COLUMN external_table`,
		`ALTER TABLE sys_file_references DROP COLUMN paths`,
		`ALTER TABLE sys_value_lists DROP COLUMN source_column_name`,
		`ALTER TABLE sys_columns DROP COLUMN source_field_id`,
	}
	for _, c := range []string{"sys_table_occurrences.base_table_id", "sys_relationships.left_column_id",
		"sys_relationships.right_column_id", "sys_relationship_predicates.left_column_id", "sys_relationship_predicates.right_column_id"} {
		table, column, _ := strings.Cut(c, ".")
		if mariadb {
			stmts = append(stmts, fmt.Sprintf(`ALTER TABLE %s MODIFY %s VARCHAR(36) NOT NULL`, table, column))
		} else {
			stmts = append(stmts, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s SET NOT NULL`, table, column))
		}
	}
	for _, q := range stmts {
		_, err := old.driver.DB().ExecContext(ctx, q)
		require.NoError(t, err, q)
	}

	// A connection pool of its own: nothing in this process has upgraded the
	// catalog through it.
	other, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	fresh, err := other.DriverFor(ctx, old.name)
	require.NoError(t, err)
	require.NotSame(t, old.driver.DB(), fresh.DB())
	svc := schema.NewService(fresh)
	occurrences, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err, "an occurrence is read from a catalog nobody signed in to since the upgrade")
	require.NotEmpty(t, occurrences)
	assert.Equal(t, tbl.ID, occurrences[0].BaseTableID)

	tables, err := svc.ListTables(ctx)
	require.NoError(t, err, "columns are read from a catalog nobody signed in to since the upgrade")
	require.Len(t, tables, 1)

	require.NoError(t, svc.EnsureSystemTables(ctx))
	require.NoError(t, svc.EnsureSystemTables(ctx), "the upgrade is idempotent")

	_, err = svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: "Other", Paths: []string{"file:Other.fmp12"}})
	require.NoError(t, err)
	ext, err := svc.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "Other Items", External: &schema.ExternalOccurrenceInput{FileReference: "Other", Table: "items"},
	})
	require.NoError(t, err, "base_table_id is optional after the upgrade")
	assert.Equal(t, "items", ext.External.Table)
}

// A field the source document names only by its FileMaker id — FileMaker
// writes an empty name when the other file was not reachable — is kept as
// the marker "#<id>", found by that id among the fields of the other
// solution migrated from FileMaker, and used to tell candidate tables apart;
// several candidates are still listed, never guessed (#107).
func TestExternalOccurrences_FieldsKnownOnlyByIDBindBySourceFieldID(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	orders := newExternalSolution(t, ctx, mgr, "ext_byid")
	peer := newExternalSolution(t, ctx, mgr, "ext_byid_peer")

	// The other solution, as its migration wrote it: each field with the
	// id FileMaker gave it. Two tables have fields 1 and 3, the third has
	// no field 3.
	migrated := func(table string, fields map[string]string) {
		tbl, err := peer.svc.CreateTable(ctx, table, table)
		require.NoError(t, err)
		for name, id := range fields {
			_, err := peer.svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
				Name: name, DisplayName: name, FieldType: dbal.FieldTypeText, IsNullable: true, SourceFieldID: id,
			})
			require.NoError(t, err)
		}
	}
	migrated("customers", map[string]string{"code": "1", "full_name": "3"})
	migrated("customers_archive", map[string]string{"code": "1", "label": "3"})
	migrated("suppliers", map[string]string{"code": "1", "name": "2"})

	tables, err := peer.svc.ListTables(ctx)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, tbl := range tables {
		for _, c := range tbl.Columns {
			ids[tbl.Name+"."+c.Name] = c.SourceFieldID
		}
	}
	assert.Equal(t, "3", ids["customers.full_name"])
	assert.Equal(t, "", ids["customers.id"], "a column not migrated from FileMaker has no source id")
	tbl, err := peer.svc.CreateTable(ctx, "notes", "notes")
	require.NoError(t, err)
	_, err = peer.svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "body", DisplayName: "body", FieldType: dbal.FieldTypeText, IsNullable: true, SourceFieldID: "x1",
	})
	assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions, "a source field id is digits only")

	ordersTable, orderFields := orders.table(t, ctx, "orders", "customer_code")
	ordersOcc := orders.occurrenceOf(t, ctx, ordersTable.ID)
	ref, err := orders.svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: "Peer", Paths: []string{"file:Peer.fmp12"}})
	require.NoError(t, err)
	ext, err := orders.svc.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "Order Customers", External: &schema.ExternalOccurrenceInput{FileReference: "Peer"},
	})
	require.NoError(t, err)
	rel, err := orders.svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_customers", LeftOccurrenceID: ordersOcc, RightOccurrenceID: ext.ID,
		Predicates: []schema.RelationshipPredicate{{
			LeftColumnID: orderFields["customer_code"], Operator: "=", RightColumnName: "#1",
		}},
	})
	require.NoError(t, err)
	require.Len(t, rel.Predicates, 1)
	assert.Equal(t, "#1", rel.Predicates[0].RightColumnName, "a marker is kept as it is")
	_, err = orders.svc.CreateValueList(ctx, schema.ValueListInput{
		Name: "Customer names", Kind: schema.ValueListFromField,
		SourceOccurrenceID: &ext.ID, SourceColumnName: "#3",
	})
	require.NoError(t, err)

	_, err = orders.svc.UpdateFileReference(ctx, ref.ID, schema.FileReferenceInput{Name: "Peer", Database: peer.name, Paths: ref.Paths})
	require.NoError(t, err)

	// Fields 1 and 3 rule suppliers out; two tables still have both.
	report, err := orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, nil)
	require.NoError(t, err)
	st := report.Occurrences[0]
	assert.Equal(t, schema.ExternalUnbound, st.Status)
	assert.Equal(t, []string{"customers", "customers_archive"}, st.Candidates)
	assert.Equal(t, []string{"#1", "#3"}, st.Fields)

	// A table without one of the ids is a mismatch, naming the id.
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, map[string]string{"Order Customers": "suppliers"})
	require.NoError(t, err)
	st = report.Occurrences[0]
	assert.Equal(t, schema.ExternalMismatch, st.Status)
	require.Len(t, st.Problems, 1)
	assert.Contains(t, st.Problems[0], "FileMaker id 3")

	// The operator's choice binds it, each id resolved to its field there.
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, map[string]string{"Order Customers": "customers"})
	require.NoError(t, err)
	st = report.Occurrences[0]
	assert.Equal(t, schema.ExternalBound, st.Status, st.Problems)
	assert.Equal(t, map[string]string{"#1": "code", "#3": "full_name"}, st.Resolved)

	// Two fields with the same id: neither is picked.
	migrated("customers_dup", map[string]string{"code": "1", "full_name": "3", "alias": "3"})
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", peer.svc, map[string]string{"Order Customers": "customers_dup"})
	require.NoError(t, err)
	st = report.Occurrences[0]
	assert.Equal(t, schema.ExternalMismatch, st.Status)
	require.Len(t, st.Problems, 1)
	assert.Contains(t, st.Problems[0], "ambiguous")
	assert.Contains(t, st.Problems[0], "alias")
	assert.Contains(t, st.Problems[0], "full_name")
	assert.Equal(t, map[string]string{"#1": "code"}, st.Resolved)

	// The ids travel in the other solution's file, and come back.
	bundleBytes, err := peer.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Peer"})
	require.NoError(t, err)
	restored := newExternalSolution(t, ctx, mgr, "ext_byid_restored")
	_, err = restored.svc.ImportSolution(ctx, bundleBytes)
	require.NoError(t, err)
	tables, err = restored.svc.ListTables(ctx)
	require.NoError(t, err)
	back := map[string]string{}
	for _, tbl := range tables {
		for _, c := range tbl.Columns {
			back[tbl.Name+"."+c.Name] = c.SourceFieldID
		}
	}
	assert.Equal(t, "3", back["customers.full_name"])
	assert.Equal(t, "3", back["customers_archive.label"])
}

// Two FileMaker files migrated end to end: Orders was saved while Customers
// was not reachable, so its relationship, value list, calculation and layout
// name Customers' fields by id only. Customers' own migration keeps each
// field's id, and binding Orders' file reference finds every one of them.
func TestExternalOccurrences_MigratedFieldsKnownOnlyByIDAreBoundByID(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	const customersXML = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Customers"/><BaseTable id="130" name="Contacts"/></BaseTableCatalog>
 <FieldsForTables>
  <FieldCatalog><BaseTableReference id="129" name="Customers"/><ObjectList>
   <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
   <Field id="3" name="Full Name" datatype="Text" fieldtype="Normal"/>
   <Field id="7" name="Nickname" datatype="Text" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
  <FieldCatalog><BaseTableReference id="130" name="Contacts"/><ObjectList>
   <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
   <Field id="3" name="Phone" datatype="Text" fieldtype="Normal"/>
  </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Customer List"><BaseTableSourceReference><BaseTableReference id="129" name="Customers"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Contact List"><BaseTableSourceReference><BaseTableReference id="130" name="Contacts"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	const ordersXML = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Customers" type="FileMaker"><File><UniversalPathList>file:Customers.fmp12</UniversalPathList></File></ExternalDataSource></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Customer Code" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Customer Label" datatype="Text" fieldtype="Calculated"><Calculation><Calculation><Text>Order Customers::&lt;Field Missing&gt; &amp; " / " &amp; Order Customers::&lt;Field Missing&gt;</Text><ChunkList><Chunk type="FieldRef"><FieldReference id="3" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Chunk><Chunk type="NoRef"> &amp; " / " &amp; </Chunk><Chunk type="FieldRef"><FieldReference id="7" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Chunk></ChunkList></Calculation></Calculation></Field>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Order List"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Order Customers" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Customers"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Order Customers"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Order List"/></RightTable>
  <JoinPredicateList><JoinPredicate type="Equal">
   <LeftField><FieldReference id="1" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></LeftField>
   <RightField><FieldReference id="1" name="Customer Code"><TableOccurrenceReference id="1" name="Order List"/></FieldReference></RightField>
  </JoinPredicate></JoinPredicateList>
 </Relationship></RelationshipCatalog>
 <ValueListCatalog>
  <ValueList id="1" name="Customer names"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="3" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></PrimaryField></Field></ValueList>
 </ValueListCatalog>
 <LayoutCatalog>
  <Layout id="1" name="Order"><TableOccurrenceReference id="1" name="Order List"/><LayoutObjectList><LayoutObject type="Field"><Field><FieldReference id="7" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Field></LayoutObject></LayoutObjectList></Layout>
 </LayoutCatalog>
 </AddAction></Structure></FMSaveAsXML>`

	// Occurrences are not named like their tables, which MariaDB's import
	// does not tell apart yet (#146).
	install := func(prefix, xml string) *externalSolution {
		res, err := migration.AnalyzeAndConvert(migration.MigrationRequest{DesignXML: xml})
		require.NoError(t, err)
		require.Empty(t, res.Report.Rejected)
		encoded, err := msgpack.Marshal(res.Bundle)
		require.NoError(t, err)
		sol := newExternalSolution(t, ctx, mgr, prefix)
		_, err = sol.svc.ImportSolution(ctx, encoded)
		require.NoError(t, err)
		return sol
	}
	customers := install("ext_mig_customers", customersXML)
	orders := install("ext_mig_orders", ordersXML)

	refs, err := orders.svc.ListFileReferences(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	ref := refs[0]
	report, err := orders.svc.VerifyFileReference(ctx, ref.ID, nil)
	require.NoError(t, err)
	require.Len(t, report.Occurrences, 1)
	assert.Equal(t, []string{"#1", "#3", "#7"}, report.Occurrences[0].Fields,
		"the relationship, the value list, the calculation and the layout each name a field by id")

	_, err = orders.svc.UpdateFileReference(ctx, ref.ID, schema.FileReferenceInput{Name: ref.Name, Database: customers.name, Paths: ref.Paths})
	require.NoError(t, err)
	// Contacts has fields 1 and 3 but no 7: Customers is the one table.
	report, err = orders.svc.BindFileReference(ctx, ref.ID, "", customers.svc, nil)
	require.NoError(t, err)
	st := report.Occurrences[0]
	assert.Equal(t, schema.ExternalBound, st.Status, st.Problems)
	assert.Equal(t, "customers", st.Table)
	assert.Equal(t, map[string]string{"#1": "code", "#3": "full_name", "#7": "nickname"}, st.Resolved)
}

// A delete whose cascade reaches another solution's table is refused before
// anything is deleted: not the record, and not the local records an earlier
// relationship of the cascade would have removed (#107).
func TestExternalOccurrences_CascadeThroughAnExternalOccurrenceDeletesNothing(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	sol := newExternalSolution(t, ctx, mgr, "ext_cascade")
	orders, orderFields := sol.table(t, ctx, "orders", "code")
	lines, lineFields := sol.table(t, ctx, "lines", "order_code")
	ordersOcc, linesOcc := sol.occurrenceOf(t, ctx, orders.ID), sol.occurrenceOf(t, ctx, lines.ID)
	_, err := sol.svc.CreateFileReference(ctx, schema.FileReferenceInput{Name: "Peer", Paths: []string{"file:Peer.fmp12"}})
	require.NoError(t, err)
	ext, err := sol.svc.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{
		Name: "Order Notes", External: &schema.ExternalOccurrenceInput{FileReference: "Peer"},
	})
	require.NoError(t, err)

	// The local cascade first, the external one after it.
	_, err = sol.svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_lines", LeftOccurrenceID: ordersOcc, RightOccurrenceID: linesOcc, CascadeDelete: true,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: orderFields["code"], Operator: "=", RightColumnID: lineFields["order_code"]}},
	})
	require.NoError(t, err)
	_, err = sol.svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name: "orders_notes", LeftOccurrenceID: ordersOcc, RightOccurrenceID: ext.ID, CascadeDelete: true,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: orderFields["code"], Operator: "=", RightColumnName: "order_code"}},
	})
	require.NoError(t, err)

	reader := data.NewService(sol.driver)
	order, err := reader.InsertRow(ctx, "orders", map[string]interface{}{"code": "A1"})
	require.NoError(t, err)
	line, err := reader.InsertRow(ctx, "lines", map[string]interface{}{"order_code": "A1"})
	require.NoError(t, err)

	err = reader.DeleteRow(ctx, "orders", order["id"].(string))
	require.ErrorIs(t, err, data.ErrExternalUnavailable)
	_, err = reader.GetRow(ctx, "orders", order["id"].(string))
	assert.NoError(t, err, "the record is kept")
	_, err = reader.GetRow(ctx, "lines", line["id"].(string))
	assert.NoError(t, err, "the local related record is kept")
}

// A solution without external occurrences is written exactly as before: none
// of the keys external occurrences add appear in its file.
func TestExternalOccurrences_AbsentFromALocalSolutionFile(t *testing.T) {
	b := goldenSolution()
	b.Relationships = []schema.BundleRelationship{{
		ID: "r-1", Name: "self", LeftOccurrenceID: "o-inv", LeftColumnID: "c-id",
		RightOccurrenceID: "o-inv", RightColumnID: "c-status", Operator: "=",
	}}
	b.FileReferences = []schema.BundleFileReference{{Name: "Ledger", Database: "ledger_db"}}
	b.ValueLists = []schema.BundleValueList{{ID: "vl-1", Name: "Statuses", Kind: schema.ValueListFromField,
		SourceTableID: ptr("t-inv"), SourceColumnID: ptr("c-status")}}
	encoded, err := msgpack.Marshal(b)
	require.NoError(t, err)
	var generic map[string]interface{}
	require.NoError(t, msgpack.Unmarshal(encoded, &generic))

	keysOf := func(v interface{}) map[string]interface{} {
		list, ok := v.([]interface{})
		require.True(t, ok)
		require.NotEmpty(t, list)
		m, ok := list[0].(map[string]interface{})
		require.True(t, ok)
		return m
	}
	assert.NotContains(t, keysOf(generic["table_occurrences"]), "external")
	rel := keysOf(generic["relationships"])
	assert.NotContains(t, rel, "left_column_name")
	assert.NotContains(t, rel, "right_column_name")
	assert.NotContains(t, keysOf(generic["file_references"]), "paths")
	assert.NotContains(t, keysOf(generic["value_lists"]), "source_column_name")
	var columns map[string]interface{}
	for _, tbl := range generic["tables"].([]interface{}) {
		if cols, ok := tbl.(map[string]interface{})["columns"].([]interface{}); ok && len(cols) > 0 {
			columns = cols[0].(map[string]interface{})
		}
	}
	require.NotNil(t, columns)
	assert.NotContains(t, columns, "source_field_id")
}

// staticPeers opens the solutions it holds, as the services given.
type staticPeers map[string]*data.Service

func (p staticPeers) Data(_ context.Context, database string) (*data.Service, error) {
	if svc, ok := p[database]; ok {
		return svc, nil
	}
	return nil, &data.ExternalUnavailableError{Database: database, Reason: data.ExternalNotSignedIn}
}
