package migration

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// convertDataAPIReference converts the synthetic reference file of the Data
// API conformance suite (testdata/dataapi), whose menus, portals,
// privileges and fields the Data API fixtures recorded.
func convertDataAPIReference(t *testing.T) *ConvertedSolution {
	t.Helper()
	f, err := os.Open("../../testdata/dataapi/reference/F4B_DataAPI_Reference.xml.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	design, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: string(design), SourceLocale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// menuOutline writes a converted menu as "folder/item" paths, in order.
func menuOutline(b *schema.SolutionBundle, catalog string) []string {
	names := map[string]string{}
	for _, l := range b.Layouts {
		names[l.ID] = l.Name
	}
	for _, s := range b.Scripts {
		names[s.ID] = s.Name
	}
	folders := map[string]string{}
	var out []string
	for _, e := range b.Catalog {
		if e.Catalog != catalog {
			continue
		}
		name := e.Name
		if name == "" {
			name = names[e.ID]
		}
		path := name
		if e.FolderID != "" {
			path = folders[e.FolderID] + "/" + name
		}
		if e.Kind == schema.MenuFolder {
			folders[e.ID] = path
			path += "/"
		}
		out = append(out, path)
	}
	return out
}

// The layout and script menus keep FileMaker's order, folders and
// separators. The reference file's layout folders have no end marker, so
// each lasts to the end of the menu; its script folders have them.
func TestReferenceMenus(t *testing.T) {
	got := convertDataAPIReference(t)
	layouts := strings.Join(menuOutline(got.Bundle, schema.CatalogLayouts), " | ")
	want := "Customers | Orders | OrderLines | Contacts | Products | Setup_Report | - | API/ | " +
		"API/API_Customers | API/API_Customers_Portals | API/API_Customers_Restricted | API/API_Orders | " +
		"API/API_OrderLines | API/API_Contacts | API/API_Products | API/API_NoAccess | API/API_Empty | " +
		"API/API_Duplicate | API/API_Duplicate | API/- | API/Nested/ | API/Nested/API_Customers_Nested"
	if layouts != want {
		t.Errorf("layout menu:\n got %s\nwant %s", layouts, want)
	}
	scripts := strings.Join(menuOutline(got.Bundle, schema.CatalogScripts), " | ")
	want = "API/ | API/API_Success | API/API_EchoParam | API/API_SecondParamValue | API/API_Error_NoRecords | " +
		"API/API_Error_ExitResult | API/API_Unsupported | API/API_FindActive | API/API_OmitFirst | " +
		"API/API_SortByNameDesc | API/API_SetGlobal | API/API_SetVar | API/API_ReadVar | API/API_UnicodeResult | " +
		"API/- | API/Diagnostics/ | API/Diagnostics/API_ReturnContext | Setup/ | Setup/Build Reference Data | Setup/Self Check"
	if scripts != want {
		t.Errorf("script menu:\n got %s\nwant %s", scripts, want)
	}
	occNames := map[string]string{}
	for _, o := range got.Bundle.TableOccurrences {
		occNames[o.ID] = o.Name
	}
	for _, e := range got.Bundle.Catalog {
		if e.Kind == schema.MenuSeparator && e.Catalog == schema.CatalogLayouts && occNames[e.TableOccurrenceID] != "Customers" {
			t.Errorf("a layout menu separator has the occurrence %q, want Customers", occNames[e.TableOccurrenceID])
		}
	}
}

// A privilege set's custom layout privileges become its accounts' layout
// access: none where it has no access, read-only where it may only view the
// records. Folders carry none.
func TestReferenceLayoutAccess(t *testing.T) {
	got := convertDataAPIReference(t)
	names := map[string]string{}
	for _, l := range got.Bundle.Layouts {
		names[l.ID] = l.Name
	}
	access := map[string]map[string]string{}
	for _, u := range got.Bundle.Users {
		access[u.Username] = map[string]string{}
		for _, p := range u.Permissions {
			access[u.Username][names[p.LayoutID]] = p.AccessLevel
		}
	}
	if rw := access["api_rw"]; len(rw) != 1 || rw["API_NoAccess"] != schema.AccessNone {
		t.Errorf("api_rw layout access = %v, want API_NoAccess none only", rw)
	}
	ro := access["api_ro"]
	if ro["API_NoAccess"] != schema.AccessNone || ro["API_Customers"] != schema.AccessReadOnly || ro["Customers"] != schema.AccessReadOnly {
		t.Errorf("api_ro layout access = %v, want API_NoAccess none and the others read-only", ro)
	}
	if len(access["admin"]) != 0 {
		t.Errorf("admin layout access = %v, want none set: an administrator reaches every layout", access["admin"])
	}
}

// The fields keep what the Data API describes of them: FileMaker's type
// where File4Base keeps another, the Storage tab and the Save as XML
// validation elements.
func TestReferenceFieldFacts(t *testing.T) {
	got := convertDataAPIReference(t)
	cols := map[string]schema.BundleColumn{}
	for _, tb := range got.Bundle.Tables {
		if tb.Name != "customers" && !strings.EqualFold(tb.Name, "Customers") {
			continue
		}
		for _, c := range tb.Columns {
			cols[c.DisplayName] = c
		}
	}
	if len(cols) == 0 {
		t.Fatal("no Customers table")
	}
	for name, want := range map[string]string{
		"PreferredTime": schema.SourceTypeTime, "gTime": schema.SourceTypeTime,
		"cDurationPlusHour": schema.SourceTypeCalculationTime, "sCount": "summary/number",
		"Name": "", "LastContact": "",
	} {
		if c := cols[name]; c.SourceType != want {
			t.Errorf("%s: source type %q, want %q", name, c.SourceType, want)
		}
	}
	options := func(name string) map[string]interface{} {
		var o map[string]interface{}
		if c := cols[name]; c.DefaultValue != nil {
			if err := json.Unmarshal([]byte(*c.DefaultValue), &o); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		return o
	}
	if o := options("gText"); o["storage_global"] != true {
		t.Errorf("gText options %v, want storage_global", o)
	}
	if o := options("Code"); o["indexing"] != "minimal" || o["auto_index"] != true || o["index_language"] != "English" || o["storage_global"] != nil {
		t.Errorf("Code options %v, want minimal automatic English indexing, not global", o)
	}
	// An auto-entered serial keeps its options beside the Storage tab's.
	if o := options("CustomerID"); o["serial_enabled"] != true || o["index_language"] == nil {
		t.Errorf("CustomerID options %v, want its serial number and its index language", o)
	}
	rules := func(name string) map[string]interface{} {
		var r map[string]interface{}
		if c := cols[name]; c.ValidationRules != nil {
			if err := json.Unmarshal([]byte(*c.ValidationRules), &r); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		return r
	}
	if r := rules("Code"); r["max_length_enabled"] != true || r["max_length"] != float64(8) {
		t.Errorf("Code rules %v, want at most 8 characters", r)
	}
	if r := rules("Rating"); r["strict_type_enabled"] != true || r["strict_type"] != "Numeric Only" {
		t.Errorf("Rating rules %v, want strictly numeric", r)
	}
	if r := rules("SignupDate"); r["strict_type"] != "4-Digit Year" {
		t.Errorf("SignupDate rules %v, want a 4-digit year", r)
	}
}

// Every row object of a portal says which portal it is in, and the
// layout's other objects say none.
func TestReferencePortalMembership(t *testing.T) {
	got := convertDataAPIReference(t)
	for _, l := range got.Bundle.Layouts {
		if l.Name != "API_Customers_Portals" {
			continue
		}
		raw, _ := json.Marshal(l.Definition)
		c, err := schema.CompileLayoutContract(raw)
		if err != nil {
			t.Fatal(err)
		}
		rows := map[string]int{}
		for _, p := range c.Portals {
			name := p.Name
			if name == "" {
				name = p.Occurrence
			}
			rows[name] = len(p.Fields)
		}
		want := map[string]int{"Customer_Contacts": 4, "OrderLinesPortal": 5, "OrdersPortal": 6, "PaidOrdersPortal": 3, "SameCountryPortal": 3}
		if len(rows) != len(want) {
			t.Fatalf("portals %v, want %v", rows, want)
		}
		for k, n := range want {
			if rows[k] != n {
				t.Errorf("portal %s has %d row fields, want %d", k, rows[k], n)
			}
		}
		if len(c.Fields) != 6 {
			t.Errorf("%d fields outside the portals, want 6", len(c.Fields))
		}
		return
	}
	t.Fatal("no API_Customers_Portals layout")
}

// A layout whose records a set may not see is hidden, not read-only, and a
// set with no access to any layout has none on each of them.
func TestPrivilegeLayoutAccessKeepsDenials(t *testing.T) {
	root, err := parseXMLTree([]byte(`<FMSaveAsXML><Structure><AddAction><PrivilegeSetsCatalog>
		<PrivilegeSet id="1" name="Partial"><access><Layouts Custom="True"><Custom><ObjectList>
			<Layout type="existing" access="ReadOnly" records="NoAccess"><LayoutReference id="1" name="A"></LayoutReference></Layout>
			<Layout type="existing" access="ReadOnly" records="ReadOnly"><LayoutReference id="2" name="B"></LayoutReference></Layout>
			<Layout type="existing" access="ReadOnly" records="ReadWrite"><LayoutReference id="3" name="C"></LayoutReference></Layout>
		</ObjectList></Custom></Layouts></access></PrivilegeSet>
		<PrivilegeSet id="2" name="Nothing"><access><Layouts View="NoAccess"></Layouts></access></PrivilegeSet>
		<PrivilegeSet id="3" name="Everything"><access><Layouts Create="True" Edit="True" Delete="True"></Layouts></access></PrivilegeSet>
	</PrivilegeSetsCatalog></AddAction></Structure></FMSaveAsXML>`))
	if err != nil {
		t.Fatal(err)
	}
	got := privilegeLayoutAccess(root, map[string]string{"1": "la", "2": "lb", "3": "lc"})
	want := map[string][]schema.BundlePermission{
		"partial": {{LayoutID: "la", AccessLevel: schema.AccessNone}, {LayoutID: "lb", AccessLevel: schema.AccessReadOnly}},
		"nothing": {{LayoutID: "la", AccessLevel: schema.AccessNone}, {LayoutID: "lb", AccessLevel: schema.AccessNone}, {LayoutID: "lc", AccessLevel: schema.AccessNone}},
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("layout access\n got %s\nwant %s", g, w)
	}
}

// A privilege set is an owner's or an administrator's by its name only when
// it grants full access; a restricted one named so is a user's, with its
// layout privileges, and the report says so.
func TestPrivilegeSetNamedAdminIsNotFullAccessByName(t *testing.T) {
	doc := `<FMSaveAsXML><Structure><AddAction>
 <LayoutCatalog><Layout id="1" name="Secret"></Layout></LayoutCatalog>
 <AccountsCatalog><ObjectList>
  <Account id="1"><Authentication><AccountName>boss</AccountName></Authentication><PrivilegeSetReference id="7" name="Admin"/></Account>
  <Account id="2"><Authentication><AccountName>chief</AccountName></Authentication><PrivilegeSetReference id="8" name="Administrator"/></Account>
  <Account id="3"><Authentication><AccountName>nobody</AccountName></Authentication><PrivilegeSetReference id="9" name="Owner"/></Account>
 </ObjectList></AccountsCatalog>
 <PrivilegeSetsCatalog><ObjectList>
  <PrivilegeSet id="7" name="Admin"><access><Records Create="True" Edit="True" Delete="True"></Records>
   <Layouts Custom="True"><Custom><ObjectList><Layout type="existing" access="NoAccess" records="NoAccess"><LayoutReference id="1" name="Secret"></LayoutReference></Layout></ObjectList></Custom></Layouts>
   <ValueLists Create="True" Edit="True" Delete="True"></ValueLists><Scripts Create="True" Edit="True" Delete="True"></Scripts>
   <Other manageDatabase="False" manageAccounts="False" manageExtPrivs="False"></Other></access></PrivilegeSet>
  ` + fullAccessSet(8, "Administrator") + `
 </ObjectList></PrivilegeSetsCatalog>
 </AddAction></Structure></FMSaveAsXML>`
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	perms := map[string]int{}
	for _, u := range got.Bundle.Users {
		roles[u.Username], perms[u.Username] = u.Role, len(u.Permissions)
	}
	if roles["boss"] != "user" || perms["boss"] != 1 {
		t.Errorf("boss: role %q with %d layout permission(s), want user with the set's one", roles["boss"], perms["boss"])
	}
	if roles["chief"] != "admin" {
		t.Errorf("chief: role %q, want admin: its set grants full access", roles["chief"])
	}
	if roles["nobody"] != "user" {
		t.Errorf("nobody: role %q, want user: the file does not define its set", roles["nobody"])
	}
	flagged := map[string]bool{}
	for _, it := range got.Report.ManualActionRequired {
		if it.Domain == "account" && strings.Contains(it.Detail, "named like a full-access set") {
			flagged[it.SourceName] = true
		}
	}
	if !flagged["boss"] || !flagged["nobody"] || flagged["chief"] {
		t.Errorf("accounts reported = %v, want boss and nobody", flagged)
	}
}

// An object lying within a portal's bounds without being one of its row
// objects says it is in no portal, so a client taking membership from the
// bounds does not draw it in the rows; an object on another panel, a row
// object and one only overlapping the portal are left as they are.
func TestObjectsWithinAPortalButNotItsRowsSaySo(t *testing.T) {
	objects := []map[string]interface{}{
		{"id": "p", "type": "portal", "x": 0.0, "y": 100.0, "width": 300.0, "height": 100.0},
		{"id": "row", "type": "field", "x": 10.0, "y": 110.0, "width": 50.0, "height": 20.0, "portal": "p"},
		{"id": "over", "type": "text", "x": 100.0, "y": 110.0, "width": 50.0, "height": 20.0},
		{"id": "tab", "type": "field", "x": 100.0, "y": 150.0, "width": 50.0, "height": 20.0, "panel": map[string]interface{}{"control": "t", "index": 1}},
		{"id": "edge", "type": "field", "x": 280.0, "y": 150.0, "width": 50.0, "height": 20.0},
	}
	markOutsidePortals(objects)
	got := map[string]interface{}{}
	for _, o := range objects {
		if v, ok := o["portal"]; ok {
			got[o["id"].(string)] = v
		}
	}
	want := map[string]interface{}{"row": "p", "over": ""}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("portal keys %v, want %v", got, want)
	}
}
