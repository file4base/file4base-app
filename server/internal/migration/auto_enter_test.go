package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// A synthetic Save as XML solution: Invoices looks its rate up from Rates
// through Invoice Rates (Invoices::Rate Code = Invoice Rates::Code).
const autoEnterSaveAsXML = `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>
 <BaseTableCatalog><BaseTable id="129" name="Invoices"/><BaseTable id="130" name="Rates"/></BaseTableCatalog>
 <FieldsForTables>
 <FieldCatalog><BaseTableReference id="129" name="Invoices"/><ObjectList>
  <Field id="1" name="Invoice Number" datatype="Text" fieldtype="Normal"><AutoEnter type="SerialNumber" prohibitModification="True"><SerialNumber enable="True" nextValue="INV0042" increment="2" generate="OnCommit"/><Calculated enable="False"/></AutoEnter></Field>
  <Field id="2" name="Created" datatype="Timestamp" fieldtype="Normal"><AutoEnter type="CreationTimestamp"/></Field>
  <Field id="3" name="Edited By" datatype="Text" fieldtype="Normal"><AutoEnter type="ModificationAccountName"/></Field>
  <Field id="4" name="Status" datatype="Text" fieldtype="Normal"><AutoEnter type="ConstantData"><ConstantData enable="True">Draft</ConstantData></AutoEnter></Field>
  <Field id="5" name="Customer" datatype="Text" fieldtype="Normal"/>
  <Field id="6" name="Label" datatype="Text" fieldtype="Normal"><AutoEnter type="Calculated"><Calculated enable="True" overwriteExisting="True" alwaysEvaluate="True"><Calculation><Calculation><Text>Upper ( Customer )</Text></Calculation></Calculation></Calculated></AutoEnter></Field>
  <Field id="7" name="Rate Code" datatype="Text" fieldtype="Normal"/>
  <Field id="8" name="Rate" datatype="Number" fieldtype="Normal"><AutoEnter type="Looked_up"><Looked_up enable="True" dontCopyIfEmpty="True" noMatchCopyOption="CopyNextLower"><FieldReference id="2" name="Amount"><TableOccurrenceReference id="2" name="Invoice Rates"/></FieldReference></Looked_up></AutoEnter></Field>
  <Field id="9" name="Fallback" datatype="Number" fieldtype="Normal"><AutoEnter type="Looked_up"><Looked_up enable="True" noMatchCopyOption="UseConstant"><ConstantData>0</ConstantData><FieldReference id="2" name="Amount"><TableOccurrenceReference id="3" name="Orphan Rates"/></FieldReference></Looked_up></AutoEnter></Field>
  <Field id="10" name="Count" datatype="Number" fieldtype="Normal"><AutoEnter type="CreationName"/></Field>
  <Field id="11" name="Previous" datatype="Text" fieldtype="Normal"><AutoEnter type="LastVisited"/></Field>
  <Field id="12" name="Rate Label" datatype="Text" fieldtype="Normal"><AutoEnter type="Calculated"><Calculated enable="True"><Calculation><Calculation><Text>Invoice Rates::Name &amp; " " &amp; Customer</Text></Calculation></Calculation></Calculated></AutoEnter></Field>
 </ObjectList></FieldCatalog>
 <FieldCatalog><BaseTableReference id="130" name="Rates"/><ObjectList>
  <Field id="1" name="Code" datatype="Text" fieldtype="Normal"/>
  <Field id="2" name="Amount" datatype="Number" fieldtype="Normal"/>
  <Field id="3" name="Name" datatype="Text" fieldtype="Normal"/>
 </ObjectList></FieldCatalog>
 </FieldsForTables>
 <TableOccurrenceCatalog>
  <TableOccurrence id="1" name="Invoices"><BaseTableSourceReference><BaseTableReference id="129" name="Invoices"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="2" name="Invoice Rates"><BaseTableSourceReference><BaseTableReference id="130" name="Rates"/></BaseTableSourceReference></TableOccurrence>
  <TableOccurrence id="3" name="Orphan Rates"><BaseTableSourceReference><BaseTableReference id="130" name="Rates"/></BaseTableSourceReference></TableOccurrence>
 </TableOccurrenceCatalog>
 <RelationshipCatalog><Relationship id="1">
  <LeftTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="1" name="Invoices"/></LeftTable>
  <RightTable cascadeCreate="False" cascadeDelete="False"><TableOccurrenceReference id="2" name="Invoice Rates"/></RightTable>
  <JoinPredicateList><JoinPredicate type="Equal">
   <LeftField><FieldReference id="7" name="Rate Code"><TableOccurrenceReference id="1" name="Invoices"/></FieldReference></LeftField>
   <RightField><FieldReference id="1" name="Code"><TableOccurrenceReference id="2" name="Invoice Rates"/></FieldReference></RightField>
  </JoinPredicate></JoinPredicateList>
 </Relationship></RelationshipCatalog>
 </AddAction></Structure></FMSaveAsXML>`

func autoEnterOf(t *testing.T, res *ConvertedSolution, table, display string) (autoenter.Options, *schema.BundleColumn) {
	t.Helper()
	for i := range res.Bundle.Tables {
		if res.Bundle.Tables[i].Name != table {
			continue
		}
		for j := range res.Bundle.Tables[i].Columns {
			col := &res.Bundle.Tables[i].Columns[j]
			if col.DisplayName == display {
				o, err := autoenter.Parse(col.DefaultValue)
				if err != nil {
					t.Fatalf("%s: %v", display, err)
				}
				if err := autoenter.Validate(col.FieldType, col.DefaultValue); err != nil {
					t.Fatalf("%s: the migrated options are refused: %v", display, err)
				}
				return o, col
			}
		}
	}
	t.Fatalf("no column %s.%s", table, display)
	return autoenter.Options{}, nil
}

func reportHas(res *ConvertedSolution, category ItemCategory, source string, words ...string) bool {
	var items []ReportItem
	switch category {
	case CategoryPreserved:
		items = res.Report.Preserved
	case CategoryUnsupported:
		items = res.Report.Unsupported
	case CategoryManualActionRequired:
		items = res.Report.ManualActionRequired
	}
	for _, item := range items {
		if !strings.EqualFold(item.SourceName, source) {
			continue
		}
		all := true
		for _, w := range words {
			if !strings.Contains(item.Detail, w) {
				all = false
			}
		}
		if all {
			return true
		}
	}
	return false
}

func dumpReport(res *ConvertedSolution) string {
	var b strings.Builder
	for _, group := range [][]ReportItem{res.Report.Preserved, res.Report.Unsupported, res.Report.ManualActionRequired} {
		for _, item := range group {
			b.WriteString(string(item.Category) + " " + item.SourceName + ": " + item.Detail + "\n")
		}
	}
	return b.String()
}

func TestAutoEnterMigration_SaveAsXML(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: autoEnterSaveAsXML})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	report := dumpReport(res)

	o, _ := autoEnterOf(t, res, "invoices", "Invoice Number")
	if !o.SerialEnabled || o.SerialNext != "INV0042" || o.SerialIncrement != 2 || o.SerialGenerate != autoenter.GenerateOnCommit || !o.ProhibitModification {
		t.Fatalf("serial not kept: %+v", o)
	}
	if o.CalculatedEnabled {
		t.Fatal("a disabled calculation must stay off")
	}
	if o, _ = autoEnterOf(t, res, "invoices", "Created"); !o.CreationEnabled || o.CreationType != autoenter.StampTimestamp {
		t.Fatalf("creation stamp not kept: %+v", o)
	}
	if o, _ = autoEnterOf(t, res, "invoices", "Edited By"); !o.ModificationEnabled || o.ModificationType != autoenter.StampAccountName {
		t.Fatalf("modification stamp not kept: %+v", o)
	}
	if o, _ = autoEnterOf(t, res, "invoices", "Status"); o.DataValue == nil || *o.DataValue != "Draft" {
		t.Fatalf("constant not kept: %+v", o)
	}

	o, _ = autoEnterOf(t, res, "invoices", "Label")
	if !o.CalculatedEnabled || o.DoNotReplace || o.SkipIfEmpty || !strings.Contains(o.CalculatedFormula, "customer") {
		t.Fatalf("calculated value not bound to the imported names: %+v", o)
	}

	o, _ = autoEnterOf(t, res, "invoices", "Rate")
	if !o.LookedUpEnabled || o.LookedUpOccurrence != "Invoice Rates" || o.LookedUpField != "amount" ||
		!o.LookedUpDontCopyIfEmpty || o.LookedUpNoMatch != autoenter.NoMatchNextLower {
		t.Fatalf("lookup not kept: %+v\n%s", o, report)
	}

	o, _ = autoEnterOf(t, res, "invoices", "Rate Label")
	if !o.CalculatedEnabled || !strings.Contains(o.CalculatedFormula, "${Invoice Rates}::name") || !o.DoNotReplace {
		t.Fatalf("a related reference must be bound: %+v\n%s", o, report)
	}

	if o, _ = autoEnterOf(t, res, "invoices", "Fallback"); o.LookedUpEnabled {
		t.Fatalf("a lookup no relationship reaches must be dropped: %+v", o)
	}
	if !reportHas(res, CategoryUnsupported, "invoices.Fallback", "looked-up value", "no relationship") {
		t.Fatalf("the dropped lookup must be reported:\n%s", report)
	}
	if o, _ = autoEnterOf(t, res, "invoices", "Count"); o.CreationEnabled {
		t.Fatal("a name stamp in a number field must be dropped")
	}
	if !reportHas(res, CategoryUnsupported, "invoices.Count") {
		t.Fatalf("the dropped stamp must be reported:\n%s", report)
	}
	if o, _ = autoEnterOf(t, res, "invoices", "Previous"); !o.ValueLastVisited {
		t.Fatal("value from last visited record is kept for the dialog")
	}
	if !reportHas(res, CategoryUnsupported, "invoices.Previous") {
		t.Fatalf("value from last visited record must be reported as not entered:\n%s", report)
	}
	if !reportHas(res, CategoryPreserved, "invoices.Invoice Number", "Auto-enter options:", "serial") {
		t.Fatalf("the kept options must be reported:\n%s", report)
	}
	if len(res.Report.ManualActionRequired) == 0 {
		t.Fatal("dropped options ask for an action")
	}
}

// The Database Design Report and the clipboard write the options as flags.
func TestAutoEnterMigration_DDRFlags(t *testing.T) {
	doc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable name="Tickets" id="1"><FieldCatalog>` +
		`<Field name="Ticket" dataType="Number" fieldType="Normal"><AutoEnter allowEditing="False" constant="False" calculation="False" lookup="False" value="SerialNumber"><Serial generate="OnCreation" nextValue="500" increment="10"/></AutoEnter></Field>` +
		`<Field name="Opened" dataType="Date" fieldType="Normal"><AutoEnter allowEditing="True" constant="False" calculation="False" lookup="False" value="CreationDate"/></Field>` +
		`<Field name="Subject" dataType="Text" fieldType="Normal"/>` +
		`<Field name="Slug" dataType="Text" fieldType="Normal"><AutoEnter allowEditing="True" constant="False" calculation="True" lookup="False" overwriteExisting="False"><Calculation><![CDATA[Lower ( Subject )]]></Calculation></AutoEnter></Field>` +
		`<Field name="Queue" dataType="Text" fieldType="Normal"><AutoEnter allowEditing="True" constant="True" calculation="False" lookup="False"><ConstantData>Support</ConstantData></AutoEnter></Field>` +
		`<Field name="Ignored" dataType="Text" fieldType="Normal"><AutoEnter allowEditing="True" constant="False" calculation="False" lookup="False"><ConstantData>never</ConstantData></AutoEnter></Field>` +
		`</FieldCatalog></BaseTable></BaseTableCatalog>` +
		`<TableOccurrenceCatalog><TableOccurrence name="Tickets" baseTable="Tickets"/></TableOccurrenceCatalog></File></FMPReport>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	report := dumpReport(res)
	o, _ := autoEnterOf(t, res, "tickets", "Ticket")
	if !o.SerialEnabled || o.SerialNext != "500" || o.SerialIncrement != 10 || !o.ProhibitModification {
		t.Fatalf("serial: %+v\n%s", o, report)
	}
	if o, _ = autoEnterOf(t, res, "tickets", "Opened"); !o.CreationEnabled || o.CreationType != autoenter.StampDate || o.ProhibitModification {
		t.Fatalf("stamp: %+v", o)
	}
	o, _ = autoEnterOf(t, res, "tickets", "Slug")
	if !o.CalculatedEnabled || !o.DoNotReplace || !strings.Contains(o.CalculatedFormula, "subject") {
		t.Fatalf("calc: %+v\n%s", o, report)
	}
	if o, _ = autoEnterOf(t, res, "tickets", "Queue"); o.DataValue == nil || *o.DataValue != "Support" {
		t.Fatalf("constant: %+v", o)
	}
	if o, _ = autoEnterOf(t, res, "tickets", "Ignored"); o.Any() {
		t.Fatalf("a constant flagged off must not be entered: %+v", o)
	}
}

// The HTML report lists the options as sentences.
func TestAutoEnterMigration_HTMLItems(t *testing.T) {
	cell := `<strong>Auto-Enter:</strong><ul><li>Serial: Generate: On commit, Next value: R-0100, Increment: 5</li>` +
		`<li>Creation Timestamp</li><li>Do not replace existing value for field (if any)</li>` +
		`<li>Context table: Things Calculation: Upper ( Name )</li></ul>`
	items := htmlAutoEnterItems(cell)
	if len(items) != 4 {
		t.Fatalf("items: %q", items)
	}
	col := schema.BundleColumn{Name: "code", FieldType: "TEXT"}
	report := convertHTMLAutoEnter("things", "Things", "Code", &col, items, map[string]string{"name": "name"}, formulaBinding{})
	o, err := autoenter.Parse(col.DefaultValue)
	if err != nil {
		t.Fatal(err)
	}
	if !o.SerialEnabled || o.SerialNext != "R-0100" || o.SerialIncrement != 5 || o.SerialGenerate != autoenter.GenerateOnCommit {
		t.Fatalf("serial: %+v", o)
	}
	if !o.CreationEnabled || o.CreationType != autoenter.StampTimestamp {
		// A creation timestamp in a text field is kept as text.
		t.Fatalf("stamp: %+v", o)
	}
	if !o.ProhibitModification {
		t.Fatal("without Allow editing the field is protected")
	}
	var preserved bool
	for _, item := range report {
		if item.Category == CategoryPreserved && strings.Contains(item.Detail, "Auto-enter options:") {
			preserved = true
		}
	}
	if !preserved {
		t.Fatalf("report: %+v", report)
	}

	lookup := schema.BundleColumn{Name: "rate", FieldType: "NUMBER"}
	report = convertHTMLAutoEnter("things", "Things", "Rate", &lookup, []string{"Allow editing", "Looked-up value: Rates::Amount"}, nil, formulaBinding{})
	var manual bool
	for _, item := range report {
		if item.Category == CategoryManualActionRequired {
			manual = true
		}
	}
	if !manual {
		t.Fatalf("an HTML lookup asks for an action: %+v", report)
	}

	// The "=" of a comparison is part of the formula, not a prefix.
	flag := schema.BundleColumn{Name: "single", FieldType: "NUMBER"}
	convertHTMLAutoEnter("things", "Things", "Single", &flag,
		[]string{"Allow editing", "Context table: Things Calculation: If ( Things::Qty = 1 ; 1 ; 0 )"},
		map[string]string{"qty": "qty"}, formulaBinding{})
	o, err = autoenter.Parse(flag.DefaultValue)
	if err != nil {
		t.Fatal(err)
	}
	if !o.CalculatedEnabled || !strings.Contains(o.CalculatedFormula, "If") || !strings.Contains(o.CalculatedFormula, "= 1") {
		t.Fatalf("comparison formula: %+v", o)
	}
	short := schema.BundleColumn{Name: "one", FieldType: "NUMBER"}
	convertHTMLAutoEnter("things", "Things", "One", &short,
		[]string{"Context table: Things Calculation: qty = 1"}, map[string]string{"qty": "qty"}, formulaBinding{})
	o, err = autoenter.Parse(short.DefaultValue)
	if err != nil {
		t.Fatal(err)
	}
	if !o.CalculatedEnabled || strings.ReplaceAll(o.CalculatedFormula, " ", "") != "qty=1" {
		t.Fatalf("qty = 1 stays a comparison: %q", o.CalculatedFormula)
	}
}

// The public recipe report: every field but its Get ( UUID ) keys, which
// become File4Base's own record id, lists "Allow editing" and nothing else
// but one auto-enter calculation that reads a related occurrence, reported
// for a manual set-up.
func TestAutoEnterMigration_RecipeHTMLReport(t *testing.T) {
	raw, err := os.ReadFile("testdata/recipes_html_ddr.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	res, err := AnalyzeAndConvert(MigrationRequest{SolutionName: "Recipes", DesignXML: string(raw)})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	for _, table := range res.Bundle.Tables {
		for _, col := range table.Columns {
			o, err := autoenter.Parse(col.DefaultValue)
			if err != nil {
				t.Fatalf("%s.%s: %v", table.Name, col.Name, err)
			}
			if o.ProhibitModification || o.CalculatedEnabled {
				t.Fatalf("%s.%s: %+v", table.Name, col.Name, o)
			}
		}
	}
	related := false
	for _, item := range res.Report.Unsupported {
		if strings.Contains(item.Detail, "Auto-enter calculation") {
			related = true
		}
	}
	if !related {
		t.Fatalf("the auto-enter calculation reading a related occurrence must be reported\n%s", dumpReport(res))
	}
}
