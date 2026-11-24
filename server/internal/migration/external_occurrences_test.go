package migration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// Synthetic pair of Save a Copy as XML documents: Orders reads the table of
// Customers through an External Data Source, and Customers reads Orders back,
// a cycle.

const externalOrdersXML = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Customers" type="FileMaker"><File><UniversalPathList>fmnet:/host.example/Customers
file:Customers.fmp12</UniversalPathList></File><UUID>00000000-0000-0000-0000-000000000001</UUID></ExternalDataSource></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Number" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Customer Code" datatype="Text" fieldtype="Normal"/>
  <Field id="3" name="Region" datatype="Text" fieldtype="Normal"/>
  <Field id="4" name="Customer Name" datatype="Text" fieldtype="Calculated"><Calculation><Calculation><Text>Order Customers::Full Name</Text><ChunkList><Chunk type="FieldRef"><FieldReference id="3" name="Full Name"><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Chunk></ChunkList></Calculation></Calculation></Field>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Orders"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Order Customers" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Customers" UUID="00000000-0000-0000-0000-000000000001"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Orders"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Order Customers"/></RightTable>
  <JoinPredicateList>
   <JoinPredicate type="Equal">
    <LeftField><FieldReference id="2" name="Customer Code"><TableOccurrenceReference id="1" name="Orders"/></FieldReference></LeftField>
    <RightField><FieldReference id="1" name="Code"><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></RightField>
   </JoinPredicate>
   <JoinPredicate type="Equal">
    <LeftField><FieldReference id="3" name="Region"><TableOccurrenceReference id="1" name="Orders"/></FieldReference></LeftField>
    <RightField><FieldReference id="2" name="Region"><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></RightField>
   </JoinPredicate>
  </JoinPredicateList>
 </Relationship></RelationshipCatalog>
 <ValueListCatalog>
  <ValueList id="1" name="Customer codes"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="1" name="Code"><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></PrimaryField></Field></ValueList>
 </ValueListCatalog>
 <LayoutCatalog>
  <Layout id="1" name="Order"><TableOccurrenceReference id="1" name="Orders"/></Layout>
  <Layout id="2" name="Customer card"><TableOccurrenceReference id="2" name="Order Customers"/></Layout>
 </LayoutCatalog>
 </AddAction></Structure></FMSaveAsXML>`

const externalCustomersXML = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Orders" type="FileMaker"><File><UniversalPathList>file:Orders.fmp12</UniversalPathList></File></ExternalDataSource>
 <ExternalDataSource id="2" name="Warehouse" type="ODBC"/></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Customers"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Customers"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Region" datatype="Text" fieldtype="Normal"/>
  <Field id="3" name="Full Name" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Customers"><BaseTableSourceReference><BaseTableReference id="129" name="Customers"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Customer Orders" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="3" name="Stock" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="2" name="Warehouse"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Customers"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Customer Orders"/></RightTable>
  <JoinPredicateList><JoinPredicate type="Equal">
   <LeftField><FieldReference id="1" name="Code"><TableOccurrenceReference id="1" name="Customers"/></FieldReference></LeftField>
   <RightField><FieldReference id="2" name="Customer Code"><TableOccurrenceReference id="2" name="Customer Orders"/></FieldReference></RightField>
  </JoinPredicate></JoinPredicateList>
 </Relationship></RelationshipCatalog>
 </AddAction></Structure></FMSaveAsXML>`

func externalOccurrence(t *testing.T, b *schema.SolutionBundle, name string) schema.BundleOccurrence {
	t.Helper()
	for _, o := range b.TableOccurrences {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("occurrence %q was not migrated", name)
	return schema.BundleOccurrence{}
}

func TestExternalOccurrencesAreMigratedUnbound(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: externalOrdersXML})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	for _, item := range res.Report.Rejected {
		t.Errorf("rejected: %+v", item)
	}

	occ := externalOccurrence(t, res.Bundle, "Order Customers")
	if occ.BaseTableID != "" || occ.External == nil || occ.External.FileReference != "Customers" || occ.External.Table != "" {
		t.Fatalf("external occurrence: %+v %+v", occ, occ.External)
	}
	if len(res.Bundle.FileReferences) != 1 {
		t.Fatalf("file references = %+v", res.Bundle.FileReferences)
	}
	ref := res.Bundle.FileReferences[0]
	if ref.Name != "Customers" || ref.Database != "" ||
		strings.Join(ref.Paths, "|") != "fmnet:/host.example/Customers|file:Customers.fmp12" {
		t.Fatalf("file reference: %+v", ref)
	}

	// Both criteria, the external side named as the other file's
	// migration names its fields.
	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("relationships = %d", len(res.Bundle.Relationships))
	}
	var got []string
	names := columnNames(res.Bundle)
	for _, p := range res.Bundle.Relationships[0].PredicateList() {
		if p.RightColumnID != "" {
			t.Fatalf("external side has a local column: %+v", p)
		}
		got = append(got, names[p.LeftColumnID]+" "+p.Operator+" "+p.RightColumnName)
	}
	if want := "orders.customer_code = code; orders.region = region"; strings.Join(got, "; ") != want {
		t.Fatalf("criteria = %v, want %s", got, want)
	}

	var list *schema.BundleValueList
	for i := range res.Bundle.ValueLists {
		if res.Bundle.ValueLists[i].Name == "Customer codes" {
			list = &res.Bundle.ValueLists[i]
		}
	}
	if list == nil || list.SourceOccurrenceID == nil || *list.SourceOccurrenceID != occ.ID ||
		list.SourceColumnName != "code" || list.SourceTableID != nil || list.SourceColumnID != nil {
		t.Fatalf("value list through the external occurrence: %+v", list)
	}

	var layout *schema.BundleLayout
	for i := range res.Bundle.Layouts {
		if res.Bundle.Layouts[i].Name == "Customer card" {
			layout = &res.Bundle.Layouts[i]
		}
	}
	if layout == nil || layout.TableOccurrenceID != occ.ID {
		t.Fatalf("layout on the external occurrence: %+v", layout)
	}

	// The related calculation keeps the occurrence's name and is rewritten
	// to the field's migrated name.
	for _, c := range res.Bundle.Tables[0].Columns {
		if c.Name == "customer_name" {
			if c.FieldType != "CALCULATION" || c.CalculationFormula == nil ||
				!strings.Contains(*c.CalculationFormula, `${Order Customers}::full_name`) {
				t.Fatalf("calculation through the external occurrence: %s %s", c.FieldType, *c.CalculationFormula)
			}
		}
	}

	if len(res.Report.ExternalOccurrences) != 1 {
		t.Fatalf("report.external_occurrences = %+v", res.Report.ExternalOccurrences)
	}
	e := res.Report.ExternalOccurrences[0]
	if e.Occurrence != "Order Customers" || e.DataSource != "Customers" || len(e.Paths) != 2 ||
		strings.Join(e.Fields, ",") != "code,full_name,region" {
		t.Fatalf("external occurrence report: %+v", e)
	}
	manual := false
	for _, item := range res.Report.ManualActionRequired {
		if item.Domain == "occurrence" && item.SourceName == "Order Customers" {
			manual = true
		}
	}
	if !manual {
		t.Fatal("the external occurrence must be a manual action, not silently bound")
	}
}

func TestExternalOccurrenceCycleAndNonFileMakerSource(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: externalCustomersXML})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	occ := externalOccurrence(t, res.Bundle, "Customer Orders")
	if occ.External == nil || occ.External.FileReference != "Orders" {
		t.Fatalf("external occurrence back to Orders: %+v", occ)
	}
	if len(res.Bundle.Relationships) != 1 || res.Bundle.Relationships[0].RightColumnName != "customer_code" {
		t.Fatalf("relationship back to Orders: %+v", res.Bundle.Relationships)
	}
	// An ODBC data source is not a FileMaker file: still refused.
	rejected := false
	for _, item := range res.Report.Rejected {
		if item.Domain == "occurrence" && item.SourceName == "Stock" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("an occurrence of an ODBC data source must stay rejected")
	}
	for _, o := range res.Bundle.TableOccurrences {
		if o.Name == "Stock" {
			t.Fatal("the ODBC occurrence was migrated")
		}
	}
}

func TestDDRPathListKeepsPathsWithSpaces(t *testing.T) {
	got := splitDDRPathList("file:Sales Leads.fmp12 fmnet:/host/Sales Leads.fmp12")
	if strings.Join(got, "|") != "file:Sales Leads.fmp12|fmnet:/host/Sales Leads.fmp12" {
		t.Fatalf("paths = %q", got)
	}
	if externalFieldName("ID") != "id_2" || externalFieldName("Full Name") != "full_name" {
		t.Fatal("external field names must follow the other file's migration")
	}
}

// When the other file is not reachable as FileMaker saves the design, it
// writes that file's fields with an empty name and their id only. Such a
// field is migrated as the marker "#<id>", in a relationship, a value list,
// a calculation and a layout alike, and reported as a manual action rather
// than refused (#107).
const externalNamelessXML = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Customers" type="FileMaker"><File><UniversalPathList>file:Customers.fmp12</UniversalPathList></File></ExternalDataSource></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Number" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Customer Code" datatype="Text" fieldtype="Normal"/>
  <Field id="3" name="Customer Name" datatype="Text" fieldtype="Calculated"><Calculation><Calculation><Text>Order Customers::&lt;Field Missing&gt; &amp; " (" &amp; Order Customers:: &amp; ")"</Text><ChunkList><Chunk type="FieldRef"><FieldReference id="3" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Chunk><Chunk type="NoRef"> &amp; " (" &amp; </Chunk><Chunk type="FieldRef"><FieldReference id="7" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Chunk><Chunk type="NoRef"> &amp; ")"</Chunk></ChunkList></Calculation></Calculation></Field>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Orders"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Order Customers" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Customers"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Order Customers"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Orders"/></RightTable>
  <JoinPredicateList><JoinPredicate type="Equal">
   <LeftField><FieldReference id="1" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></LeftField>
   <RightField><FieldReference id="2" name="Customer Code"><TableOccurrenceReference id="1" name="Orders"/></FieldReference></RightField>
  </JoinPredicate></JoinPredicateList>
 </Relationship></RelationshipCatalog>
 <ValueListCatalog>
  <ValueList id="1" name="Customer names"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="3" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></PrimaryField></Field></ValueList>
 </ValueListCatalog>
 <LayoutCatalog>
  <Layout id="1" name="Order"><TableOccurrenceReference id="1" name="Orders"/><LayoutObjectList><LayoutObject type="Field"><Field><FieldReference id="3" name=""><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></Field></LayoutObject></LayoutObjectList></Layout>
 </LayoutCatalog>
 </AddAction></Structure></FMSaveAsXML>`

func TestExternalFieldsKnownOnlyByIDAreKeptByID(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: externalNamelessXML})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	for _, item := range res.Report.Rejected {
		t.Errorf("rejected: %+v", item)
	}
	occ := externalOccurrence(t, res.Bundle, "Order Customers")

	// Every migrated column keeps its FileMaker id.
	ids := map[string]string{}
	for _, c := range res.Bundle.Tables[0].Columns {
		ids[c.Name] = c.SourceFieldID
	}
	if ids["number"] != "1" || ids["customer_code"] != "2" || ids["customer_name"] != "3" || ids["id"] != "" {
		t.Fatalf("source field ids = %v", ids)
	}

	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("relationships = %d", len(res.Bundle.Relationships))
	}
	names := columnNames(res.Bundle)
	preds := res.Bundle.Relationships[0].PredicateList()
	if len(preds) != 1 || preds[0].LeftColumnID != "" || preds[0].LeftColumnName != "#1" || names[preds[0].RightColumnID] != "orders.customer_code" {
		t.Fatalf("criterion = %+v", preds)
	}

	if len(res.Bundle.ValueLists) != 1 {
		t.Fatalf("value lists = %+v", res.Bundle.ValueLists)
	}
	if vl := res.Bundle.ValueLists[0]; vl.SourceOccurrenceID == nil || *vl.SourceOccurrenceID != occ.ID || vl.SourceColumnName != "#3" {
		t.Fatalf("value list = %+v", vl)
	}

	for _, c := range res.Bundle.Tables[0].Columns {
		if c.Name == "customer_name" {
			if c.FieldType != "CALCULATION" || c.CalculationFormula == nil ||
				!strings.Contains(*c.CalculationFormula, `${Order Customers}::${#3} \u0026 \" (\" \u0026 ${Order Customers}::${#7} \u0026`) {
				t.Fatalf("calculation = %s %v", c.FieldType, *c.CalculationFormula)
			}
		}
	}

	if len(res.Bundle.Layouts) != 1 {
		t.Fatalf("layouts = %d", len(res.Bundle.Layouts))
	}
	encoded, _ := json.Marshal(res.Bundle.Layouts[0].Definition)
	def := string(encoded)
	if !strings.Contains(def, `"field_name":"#3"`) || !strings.Contains(def, `"table_occurrence":"Order Customers"`) {
		t.Fatalf("layout = %s", def)
	}

	if len(res.Report.ExternalOccurrences) != 1 {
		t.Fatalf("report.external_occurrences = %+v", res.Report.ExternalOccurrences)
	}
	if got := strings.Join(res.Report.ExternalOccurrences[0].Fields, ","); got != "#1,#3,#7" {
		t.Fatalf("external occurrence fields = %s", got)
	}
	manual := false
	for _, item := range res.Report.ManualActionRequired {
		if item.Domain == "occurrence" && item.SourceName == "Order Customers" && strings.Contains(item.Detail, "(1, 3, 7)") {
			manual = true
		}
	}
	if !manual {
		t.Fatalf("the fields known by id only must be a manual action: %+v", res.Report.ManualActionRequired)
	}
}

// A field really named "#4" is a name like any other, here and in the other
// file: only a reference with an empty name is kept by id (#107).
func TestAFieldNamedLikeAnIDMarkerIsANameNotAnID(t *testing.T) {
	const doc = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Customers" type="FileMaker"><File><UniversalPathList>file:Customers.fmp12</UniversalPathList></File></ExternalDataSource></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
  <Field id="4" name="Other" datatype="Text" fieldtype="Normal"/>
  <Field id="5" name="#4" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Order List"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Order Customers" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Customers"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Order List"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Order Customers"/></RightTable>
  <JoinPredicateList><JoinPredicate type="Equal">
   <LeftField><FieldReference id="1" name="Code"><TableOccurrenceReference id="1" name="Order List"/></FieldReference></LeftField>
   <RightField><FieldReference id="4" name="#4"><TableOccurrenceReference id="2" name="Order Customers"/></FieldReference></RightField>
  </JoinPredicate></JoinPredicateList>
 </Relationship></RelationshipCatalog>
 <ValueListCatalog>
  <ValueList id="1" name="Local marks"><Source value="FromField"></Source><Field><PrimaryField show="True" sort="True"><FieldReference id="5" name="#4"><TableOccurrenceReference id="1" name="Order List"/></FieldReference></PrimaryField></Field></ValueList>
 </ValueListCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	for _, item := range res.Report.Rejected {
		t.Errorf("rejected: %+v", item)
	}
	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("relationships = %d", len(res.Bundle.Relationships))
	}
	if p := res.Bundle.Relationships[0].PredicateList()[0]; p.RightColumnName != "f_4" {
		t.Fatalf("the other file's field named #4 = %q, want its name sanitized", p.RightColumnName)
	}
	if fields := res.Report.ExternalOccurrences[0].Fields; strings.Join(fields, ",") != "f_4" {
		t.Fatalf("external fields = %v", fields)
	}
	for _, item := range res.Report.ManualActionRequired {
		if strings.Contains(item.Detail, "only by their FileMaker id") {
			t.Fatalf("a named field is not reported as known by id: %+v", item)
		}
	}
	names := columnNames(res.Bundle)
	if len(res.Bundle.ValueLists) != 1 || res.Bundle.ValueLists[0].SourceColumnID == nil {
		t.Fatalf("value lists = %+v", res.Bundle.ValueLists)
	}
	if got := names[*res.Bundle.ValueLists[0].SourceColumnID]; got != "orders.f_4" {
		t.Fatalf("the local value list on #4 reads %s, want orders.f_4 (the field named #4, not the one with id 4)", got)
	}
}

// A layout field object keeps the occurrence it shows its field from: one of
// another file's table is never bound to the local field of the same name,
// which it would otherwise edit (#107).
func TestALayoutFieldOfAnExternalOccurrenceKeepsItsOccurrence(t *testing.T) {
	const doc = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <ExternalDataSourceCatalog><ExternalDataSource id="1" name="Customers" type="FileMaker"><File><UniversalPathList>file:Customers.fmp12</UniversalPathList></File></ExternalDataSource></ExternalDataSourceCatalog>
 <BaseTableCatalog><BaseTable id="129" name="Orders"/></BaseTableCatalog>
 <FieldsForTables><FieldCatalog><BaseTableReference id="129" name="Orders"/><ObjectList>
  <Field id="1" name="Status" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog></FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Order List"><BaseTableSourceReference><BaseTableReference id="129" name="Orders"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Remote Occ" type="External"><BaseTableSourceReference type="ExternalDataSourceReference"><DataSourceReference id="1" name="Customers"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <LayoutCatalog>
  <Layout id="1" name="Order"><TableOccurrenceReference id="1" name="Order List"/>
   <Field table="Remote Occ" name="Status"/>
   <Field table="Order List" name="Status"/>
  </Layout>
 </LayoutCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert failed: %v", err)
	}
	if len(res.Bundle.Layouts) != 1 {
		t.Fatalf("layouts = %d", len(res.Bundle.Layouts))
	}
	objects, _ := res.Bundle.Layouts[0].Definition["objects"].([]map[string]interface{})
	if len(objects) != 2 {
		t.Fatalf("objects = %+v", res.Bundle.Layouts[0].Definition["objects"])
	}
	got := []string{}
	for _, o := range objects {
		b := o["field_binding"].(map[string]interface{})
		got = append(got, fmt.Sprintf("%v::%v", b["table_occurrence"], b["field_name"]))
	}
	if want := "Remote Occ::status,Order List::status"; strings.Join(got, ",") != want {
		t.Fatalf("field objects = %v, want %s", got, want)
	}
	if fields := res.Report.ExternalOccurrences[0].Fields; strings.Join(fields, ",") != "status" {
		t.Fatalf("external fields = %v", fields)
	}
}
