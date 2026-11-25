package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// layoutObjectsFixture is a handwritten <FMSaveAsXML> layout in the shape
// FileMaker writes one: parts with their size, objects with their bounds in
// layout coordinates, a theme with CSS, a library picture and the usual
// nesting of groups and tab panels. The names are invented.
const layoutObjectsFixture = `<?xml version="1.0" encoding="UTF-8"?>
<FMSaveAsXML Has_DDR_INFO="True">
  <File name="Recipes.fmp12">
    <BaseTableCatalog>
      <BaseTable id="1" name="Recipes"/>
      <BaseTable id="2" name="Ingredients"/>
    </BaseTableCatalog>
    <FieldsForTables>
      <FieldCatalog>
        <BaseTableSourceReference id="1" name="Recipes"/>
        <Field id="1" name="Title" datatype="text" fieldtype="normal"/>
        <Field id="2" name="Price" datatype="number" fieldtype="normal"/>
        <Field id="3" name="Served On" datatype="date" fieldtype="normal"/>
        <Field id="4" name="Unit" datatype="text" fieldtype="normal"/>
        <Field id="5" name="Archived" datatype="number" fieldtype="normal"/>
      </FieldCatalog>
      <FieldCatalog>
        <BaseTableSourceReference id="2" name="Ingredients"/>
        <Field id="1" name="RecipeTitle" datatype="text" fieldtype="normal"/>
        <Field id="2" name="Quantity" datatype="number" fieldtype="normal"/>
      </FieldCatalog>
    </FieldsForTables>
    <RelationshipGraph>
      <TableList>
        <Table id="1" name="Recipes" baseTable="Recipes" baseTableId="1"/>
        <Table id="2" name="Ingredients" baseTable="Ingredients" baseTableId="2"/>
        <Table id="3" name="Loose Ingredients" baseTable="Ingredients" baseTableId="2"/>
        <Table id="4" name="Ingredient Recipes" baseTable="Recipes" baseTableId="1"/>
      </TableList>
      <RelationshipList>
        <Relationship id="1" name="Recipes_Ingredients">
          <LeftTable name="Recipes"/>
          <RightTable name="Ingredients"/>
          <JoinPredicateList>
            <JoinPredicate type="Equal">
              <LeftField><Field name="Title" table="Recipes"/></LeftField>
              <RightField><Field name="RecipeTitle" table="Ingredients"/></RightField>
            </JoinPredicate>
          </JoinPredicateList>
        </Relationship>
        <Relationship id="2" name="Ingredients_Recipes">
          <LeftTable name="Ingredients"/>
          <RightTable name="Ingredient Recipes"/>
          <JoinPredicateList>
            <JoinPredicate type="Equal">
              <LeftField><Field name="RecipeTitle" table="Ingredients"/></LeftField>
              <RightField><Field name="Title" table="Ingredient Recipes"/></RightField>
            </JoinPredicate>
          </JoinPredicateList>
        </Relationship>
      </RelationshipList>
    </RelationshipGraph>
    <ValueListCatalog>
      <ValueList name="Units"><CustomValues><Text>g</Text><Text>kg</Text></CustomValues></ValueList>
    </ValueListCatalog>
    <ThemeCatalog>
      <Theme name="Plain">
        <CSS><![CDATA[
/* synthetic theme */
body:normal .self { background-color: rgba(100%,100%,100%,1); }
header:normal .self { background-color: rgba(0%,0%,50%,1); }
text_box:normal .self { font-family: -fm-font-family(Inter,Inter-Regular); font-size: 12pt; color: rgba(10%,10%,10%,1); }
text_box.title:normal .self { font-size: 20pt; font-weight: bold; text-align: center; }
edit_box:normal .self { border-top-style: solid; border-top-width: 1pt; border-top-color: rgba(50%,50%,50%,1); border-top-left-radius: 3pt 3pt; }
edit_box:hover .self { background-color: rgba(100%,0%,0%,1); }
rectangle:normal .self { background-color: rgba(0%,0%,0%,0.5); }
]]></CSS>
      </Theme>
    </ThemeCatalog>
    <LibraryCatalog>
      <BinaryData>
        <LibraryReference id="7" key="1"/>
        <StreamList>
          <Stream name="FNAM" type="Hex">00</Stream>
          <Stream name="PNGf" type="Base64" size="68">iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==</Stream>
        </StreamList>
      </BinaryData>
    </LibraryCatalog>
    <LayoutCatalog>
      <Layout id="1" name="Recipe Card" width="600">
        <TableOccurrenceReference id="1" name="Recipes"/>
        <LayoutThemeReference name="Plain"/>
        <PartsList>
          <Part type="Header">
            <Definition type="Header" kind="1" size="60" absolute="0"/>
            <ObjectList>
              <LayoutObject type="Text" name="heading">
                <UUID>00000000-0000-0000-0000-000000000001</UUID>
                <Bounds top="10" left="20" bottom="40" right="300"/>
                <LocalCSS name="title"><![CDATA[self:normal .self { color: rgba(100%,0%,0%,1); }]]></LocalCSS>
                <Text><StyledText><Data><![CDATA[Recipe <<Title>> of <<Ingredients::Quantity>>]]></Data></StyledText></Text>
              </LayoutObject>
            </ObjectList>
          </Part>
          <Part type="Body">
            <Definition type="Body" kind="4" size="200" absolute="60"/>
            <ObjectList>
              <LayoutObject type="Group">
                <UUID>00000000-0000-0000-0000-000000000002</UUID>
                <Bounds top="70" left="20" bottom="130" right="320"/>
                <GroupedButton>
                  <ObjectList>
                    <LayoutObject type="Edit Box" name="price">
                      <UUID>00000000-0000-0000-0000-000000000003</UUID>
                      <Bounds top="70" left="20" bottom="90" right="200"/>
                      <Conditions><Hide findMode="True"><Calculation><Text><![CDATA[Archived = 1]]></Text></Calculation></Hide></Conditions>
                      <Tooltip><Calculation><Text><![CDATA["Price in euros"]]></Text></Calculation></Tooltip>
                      <Field>
                        <FieldReference name="Price" id="2"><TableOccurrenceReference name="Recipes" id="1"/></FieldReference>
                        <Display Style="0" show="1"><Placeholder findMode="False"><Calculation><Text><![CDATA["0.00"]]></Text></Calculation></Placeholder></Display>
                      </Field>
                      <ExtendedAttributes><Formatting><Numeric><Options>524</Options><DecimalDigits>2</DecimalDigits><Symbol>€</Symbol></Numeric></Formatting></ExtendedAttributes>
                    </LayoutObject>
                    <LayoutObject type="Drop-down Calendar">
                      <UUID>00000000-0000-0000-0000-000000000004</UUID>
                      <Bounds top="100" left="20" bottom="120" right="200"/>
                      <Field>
                        <FieldReference name="Served On" id="3"><TableOccurrenceReference name="Recipes" id="1"/></FieldReference>
                        <Display Style="6" show="1"/>
                        <Usage type="4" inputMode="0"/>
                      </Field>
                      <ExtendedAttributes><Formatting><Date><Separator>/</Separator></Date></Formatting></ExtendedAttributes>
                    </LayoutObject>
                  </ObjectList>
                </GroupedButton>
              </LayoutObject>
              <LayoutObject type="Pop-up Menu">
                <UUID>00000000-0000-0000-0000-000000000005</UUID>
                <Bounds top="140" left="20" bottom="160" right="200"/>
                <Field>
                  <FieldReference name="Unit" id="4"><TableOccurrenceReference name="Recipes" id="1"/></FieldReference>
                  <Display Style="2" show="1"><ValueListReference name="Units" id="1"/></Display>
                  <Usage type="1" inputMode="0"/>
                </Field>
              </LayoutObject>
              <LayoutObject type="Edit Box">
                <UUID>00000000-0000-0000-0000-000000000006</UUID>
                <Bounds top="170" left="20" bottom="190" right="200"/>
                <Field>
                  <FieldReference name="Quantity" id="2" repetition="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference>
                  <Display Style="0" show="-3"/>
                </Field>
              </LayoutObject>
              <LayoutObject type="Edit Box">
                <UUID>00000000-0000-0000-0000-000000000007</UUID>
                <Bounds top="200" left="20" bottom="220" right="200"/>
                <Field>
                  <FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Loose Ingredients" id="3"/></FieldReference>
                  <Display Style="0" show="1"/>
                </Field>
              </LayoutObject>
              <LayoutObject type="Edit Box">
                <UUID>00000000-0000-0000-0000-000000000031</UUID>
                <Bounds top="200" left="220" bottom="220" right="320"/>
                <Field>
                  <FieldReference name="Price" id="2"><TableOccurrenceReference name="Ingredient Recipes" id="4"/></FieldReference>
                  <Display Style="0" show="1"/>
                </Field>
              </LayoutObject>
              <LayoutObject type="Rectangle">
                <UUID>00000000-0000-0000-0000-000000000008</UUID>
                <Bounds top="65" left="340" bottom="125" right="500"/>
              </LayoutObject>
              <LayoutObject type="Line">
                <UUID>00000000-0000-0000-0000-000000000009</UUID>
                <Bounds top="230" left="20" bottom="240" right="500"/>
                <LocalCSS><![CDATA[self:normal .self { border-top-style: solid; border-top-width: 2pt; border-top-color: rgba(0%,0%,0%,1); }]]></LocalCSS>
              </LayoutObject>
              <LayoutObject type="Graphic">
                <UUID>00000000-0000-0000-0000-00000000000a</UUID>
                <Bounds top="130" left="340" bottom="180" right="400"/>
                <Graphic><BinaryData><LibraryReference id="7" key="1"/></BinaryData><Formatting><Options>3</Options></Formatting></Graphic>
              </LayoutObject>
              <LayoutObject type="Tab Control">
                <UUID>00000000-0000-0000-0000-00000000000b</UUID>
                <Bounds top="130" left="420" bottom="250" right="590"/>
                <TabControl default="1">
                  <ObjectList>
                    <LayoutObject type="Panel"><Bounds top="150" left="420" bottom="250" right="590"/>
                      <TabPanel><Calculation><Text><![CDATA["One"]]></Text></Calculation><ObjectList>
                        <LayoutObject type="Oval"><UUID>00000000-0000-0000-0000-00000000000c</UUID><Bounds top="160" left="430" bottom="180" right="450"/></LayoutObject>
                      </ObjectList></TabPanel>
                    </LayoutObject>
                    <LayoutObject type="Panel"><Bounds top="150" left="420" bottom="250" right="590"/>
                      <TabPanel><Calculation><Text><![CDATA["Two"]]></Text></Calculation><ObjectList>
                        <LayoutObject type="Rounded Rectangle"><UUID>00000000-0000-0000-0000-00000000000d</UUID><Bounds top="160" left="430" bottom="200" right="500"/></LayoutObject>
                      </ObjectList></TabPanel>
                    </LayoutObject>
                  </ObjectList>
                </TabControl>
              </LayoutObject>
              <LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>
            </ObjectList>
          </Part>
          <Part type="Footer">
            <Definition type="Footer" kind="7" size="30" absolute="260"/>
          </Part>
        </PartsList>
        <UUID>00000000-0000-0000-0000-0000000000ff</UUID>
        <Options>512</Options>
      </Layout>
    </LayoutCatalog>
  </File>
</FMSaveAsXML>`

func convertLayoutFixture(t *testing.T) (*ConvertedSolution, map[string]interface{}) {
	t.Helper()
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: layoutObjectsFixture, NativeDateOrder: "dmy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bundle.Layouts) != 1 {
		t.Fatalf("layouts: %d (%+v)", len(got.Bundle.Layouts), got.Report.Rejected)
	}
	// What the import stores is JSON: read the definition back as it would.
	raw, err := json.Marshal(got.Bundle.Layouts[0].Definition)
	if err != nil {
		t.Fatal(err)
	}
	var def map[string]interface{}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	return got, def
}

func objectsByType(def map[string]interface{}) map[string][]map[string]interface{} {
	out := map[string][]map[string]interface{}{}
	for _, o := range def["objects"].([]interface{}) {
		m := o.(map[string]interface{})
		out[m["type"].(string)] = append(out[m["type"].(string)], m)
	}
	return out
}

func TestNativeLayoutPartsAndGeometry(t *testing.T) {
	_, def := convertLayoutFixture(t)
	if def["width"] != 600.0 || def["default_view"] != "list" {
		t.Fatalf("layout width/view: %v %v", def["width"], def["default_view"])
	}
	parts := def["parts"].([]interface{})
	want := []struct {
		kind   string
		height float64
	}{{"header", 60}, {"body", 200}, {"footer", 30}}
	if len(parts) != len(want) {
		t.Fatalf("parts: %v", parts)
	}
	ids := map[string]bool{}
	for i, p := range parts {
		m := p.(map[string]interface{})
		if m["type"] != want[i].kind || m["height"] != want[i].height {
			t.Fatalf("part %d: %v", i, m)
		}
		id, _ := m["id"].(string)
		if id == "" || ids[id] {
			t.Fatalf("part %d needs its own id: %v", i, m)
		}
		ids[id] = true
	}
	if parts[0].(map[string]interface{})["fill_color"] != "#000080" {
		t.Fatalf("header fill from the theme: %v", parts[0])
	}

	byType := objectsByType(def)
	label := byType["label"][0]
	if label["x"] != 20.0 || label["y"] != 10.0 || label["width"] != 280.0 || label["height"] != 30.0 {
		t.Fatalf("label bounds: %v", label)
	}
	// A related field is not one of the layout's columns: left as written.
	if label["text"] != "Recipe {{title}} of <<Ingredients::Quantity>>" {
		t.Fatalf("merge text: %q", label["text"])
	}
	style := label["style"].(map[string]interface{})
	if style["font_size"] != 20.0 || style["font_weight"] != "bold" || style["text_align"] != "center" ||
		style["text_color"] != "#FF0000" || style["font_family"] != "Inter" {
		t.Fatalf("label style cascade: %v", style)
	}

	// Grouped objects are kept one by one, at their own place; a hover state
	// does not leak into the normal style.
	fields := byType["field"]
	if len(fields) != 6 {
		t.Fatalf("fields: %d", len(fields))
	}
	price := fields[0]
	if price["y"] != 70.0 || price["name"] != "price" || price["tooltip"] != "Price in euros" ||
		price["hide_when"] != "archived = 1" || price["hide_in_find"] != true {
		t.Fatalf("price field: %v", price)
	}
	ps := price["style"].(map[string]interface{})
	if ps["number_format"] != "currency" || ps["currency_symbol"] != "€" || ps["decimal_places"] != 2.0 ||
		ps["use_thousands_separator"] != true || ps["border_width"] != 1.0 || ps["corner_radius"] != 3.0 || ps["fill_color"] != nil {
		t.Fatalf("price style: %v", ps)
	}
	pb := price["field_binding"].(map[string]interface{})
	if pb["field_name"] != "price" || pb["placeholder"] != "0.00" || pb["control_style"] != "edit_box" {
		t.Fatalf("price binding: %v", pb)
	}
	cal := fields[1]
	if cal["field_binding"].(map[string]interface{})["control_style"] != "drop_down_calendar" ||
		cal["style"].(map[string]interface{})["date_format"] != "dd/mm/yyyy" {
		t.Fatalf("calendar: %v", cal)
	}
	unit := fields[2]["field_binding"].(map[string]interface{})
	if unit["control_style"] != "pop_up_menu" || unit["value_list_id"] == nil || unit["value_list_id"] == "" {
		t.Fatalf("pop-up menu: %v", unit)
	}
	related := fields[3]["field_binding"].(map[string]interface{})
	if related["relationship_id"] == nil || related["table_occurrence"] != "Ingredients" ||
		related["repetitions_shown"] != 3.0 || related["repetition_orientation"] != "horizontal" {
		t.Fatalf("related field: %v", related)
	}
	loose := fields[4]["field_binding"].(map[string]interface{})
	if loose["unresolved_relationship"] != true || loose["relationship_id"] != nil {
		t.Fatalf("a field no relationship reaches must say so: %v", loose)
	}
	far := fields[5]["field_binding"].(map[string]interface{})
	if path, _ := far["occurrence_path"].([]interface{}); len(path) != 2 || path[0] != "Ingredients" || path[1] != "Ingredient Recipes" ||
		far["relationship_id"] != nil || far["unresolved_relationship"] != nil || far["field_name"] != "price" {
		t.Fatalf("a field two relationships away is read along its path: %v", far)
	}

	rect := byType["rect"][0]
	if rect["style"].(map[string]interface{})["fill_color"] != "#80000000" {
		t.Fatalf("rectangle: %v", rect)
	}
	line := byType["line"][0]
	if line["height"] != 2.0 || line["y"] != 234.0 || line["width"] != 480.0 {
		t.Fatalf("line box: %v", line)
	}
	media := byType["media"][0]["media"].(map[string]interface{})
	if media["mime_type"] != "image/png" || media["fit"] != "fill" || !strings.HasPrefix(media["data"].(string), "iVBOR") {
		t.Fatalf("picture: %v", media)
	}
	// The tab control is kept beneath its panels, with its labels and the
	// tab in front (default="1"); every panel's objects say which panel
	// they sit on.
	tabs := byType["tab_control"]
	if len(tabs) != 1 || tabs[0]["default_panel"] != 1.0 || fmt.Sprint(tabs[0]["tabs"]) != "[One Two]" ||
		tabs[0]["x"] != 420.0 || tabs[0]["height"] != 120.0 {
		t.Fatalf("tab control: %v", tabs)
	}
	panelOf := func(o map[string]interface{}) string {
		p, _ := o["panel"].(map[string]interface{})
		return fmt.Sprint(p["control"] == tabs[0]["id"], " ", p["index"])
	}
	if len(byType["oval"]) != 1 || panelOf(byType["oval"][0]) != "true 0" ||
		len(byType["rounded_rect"]) != 1 || panelOf(byType["rounded_rect"][0]) != "true 1" {
		t.Fatalf("panels: %v / %v", byType["oval"], byType["rounded_rect"])
	}
	order := map[string]int{}
	for i, o := range def["objects"].([]interface{}) {
		order[o.(map[string]interface{})["type"].(string)] = i
	}
	if order["tab_control"] > order["oval"] {
		t.Fatal("a tab control is drawn beneath its panels")
	}
	// A button with no label, icon or action is kept as an empty button.
	if len(byType["button"]) != 1 || byType["button"][0]["action"] != nil {
		t.Fatalf("buttons: %v", byType["button"])
	}
}

func TestNativeLayoutObjectIDsAreStable(t *testing.T) {
	first, def1 := convertLayoutFixture(t)
	_, def2 := convertLayoutFixture(t)
	ids := map[string]bool{}
	for i, o := range def1["objects"].([]interface{}) {
		id := o.(map[string]interface{})["id"].(string)
		if id == "" || ids[id] {
			t.Fatalf("object %d id %q not unique", i, id)
		}
		ids[id] = true
		if def2["objects"].([]interface{})[i].(map[string]interface{})["id"] != id {
			t.Fatalf("object %d id changed between two migrations of the same file", i)
		}
	}
	_ = first
}

func TestNativeLayoutReportAndCounts(t *testing.T) {
	got, _ := convertLayoutFixture(t)
	counts := got.Report.Counts.LayoutObjects
	check := func(kind string, source, migrated int) {
		t.Helper()
		if c := counts[kind]; c.Source != source || c.Migrated != migrated {
			t.Fatalf("%s: %+v, want %d/%d", kind, c, source, migrated)
		}
	}
	check("Edit Box", 4, 4)
	check("Text", 1, 1)
	check("Oval", 1, 1)
	check("Rounded Rectangle", 1, 1)
	check("Tab Control", 1, 1)
	check("Panel", 2, 2)
	check("Button", 1, 1)
	check("Group", 1, 1)
	if got.Report.Counts.RejectedCount != 0 {
		t.Fatalf("layout objects must never reject a layout: %+v", got.Report.Rejected)
	}
	var unsupported, review int
	for _, it := range append(append([]ReportItem{}, got.Report.Unsupported...), got.Report.ManualActionRequired...) {
		if it.Domain != "layout" {
			continue
		}
		switch it.Category {
		case CategoryUnsupported:
			// Every object of the fixture is migrated now.
			unsupported++
			t.Fatalf("unsupported detail: %s", it.Detail)
		case CategoryManualActionRequired:
			review++
			if !strings.Contains(it.Detail, "1 field of an occurrence no relationship joins") ||
				!strings.Contains(it.Detail, "1 merge field") {
				t.Fatalf("review detail: %s", it.Detail)
			}
		}
	}
	if unsupported != 0 || review != 1 {
		t.Fatalf("one review item per layout: %d unsupported, %d review", unsupported, review)
	}
}

func TestCalcLiteral(t *testing.T) {
	for in, want := range map[string]string{`"a"`: "a", `"a¶b"`: "a\nb", `"say \"hi\""`: `say "hi"`} {
		if got, ok := calcLiteral(in); !ok || got != want {
			t.Fatalf("%s: %q %v", in, got, ok)
		}
	}
	for _, in := range []string{`"a" & "b"`, `Title`, `""a`} {
		if _, ok := calcLiteral(in); ok {
			t.Fatalf("%s is not a literal", in)
		}
	}
}

func TestFMCSSColor(t *testing.T) {
	for in, want := range map[string]string{
		"rgba(100%,0%,0%,1)":   "#FF0000",
		"rgba(0%,0%,0%,0.5)":   "#80000000",
		"transparent":          "#00000000",
		"rgb(255, 255, 255)":   "#FFFFFF",
		"rgba(50%,50%,50%,1)":  "#808080",
		"rgba(20%,40%,60%, 1)": "#336699",
	} {
		if got, ok := cssColor(in); !ok || got != want {
			t.Fatalf("%s: %s", in, got)
		}
	}
	if _, ok := cssColor("-fm-gradient(x)"); ok {
		t.Fatal("a gradient is not a colour")
	}
}

// A migrated field object names the value list it offers by the id the
// migration gave the list; the import must point it at the list it creates.
func TestNativeLayoutValueListSurvivesImport(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
	}

	stamp := time.Now().UnixNano()
	suffix := fmt.Sprintf(" %d", stamp)
	design := strings.NewReplacer(
		`name="Recipes"`, `name="Recipes`+suffix+`"`, `baseTable="Recipes"`, `baseTable="Recipes`+suffix+`"`,
		`table="Recipes"`, `table="Recipes`+suffix+`"`,
		`name="Ingredients"`, `name="Ingredients`+suffix+`"`, `baseTable="Ingredients"`, `baseTable="Ingredients`+suffix+`"`,
		`table="Ingredients"`, `table="Ingredients`+suffix+`"`,
		`name="Loose Ingredients"`, `name="Loose Ingredients`+suffix+`"`,
		`name="Ingredient Recipes"`, `name="Ingredient Recipes`+suffix+`"`, `table="Ingredient Recipes"`, `table="Ingredient Recipes`+suffix+`"`,
		`name="Units"`, `name="Units`+suffix+`"`, `name="Recipe Card"`, `name="Recipe Card`+suffix+`"`,
	).Replace(layoutObjectsFixture)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	require.NoError(t, err)
	require.Len(t, res.Bundle.Layouts, 1)
	for i := range res.Bundle.TableOccurrences {
		res.Bundle.TableOccurrences[i].XPos = float64(100_000 + stamp%1_000_000)
		res.Bundle.TableOccurrences[i].YPos = float64(100_000 + i*200)
	}
	file, err := msgpack.Marshal(res.Bundle)
	require.NoError(t, err)

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	_, err = svc.ImportSolution(ctx, file)
	require.NoError(t, err)

	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	for _, tbl := range res.Bundle.Tables {
		for _, created := range tables {
			if created.Name == tbl.Name {
				id := created.ID
				t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), id) })
			}
		}
	}
	lists, err := svc.ListValueLists(ctx)
	require.NoError(t, err)
	listID := ""
	for _, vl := range lists {
		if vl.Name == "Units"+suffix {
			listID = vl.ID
			id := vl.ID
			t.Cleanup(func() { _ = svc.DeleteValueList(context.Background(), id) })
		}
	}
	require.NotEmpty(t, listID)
	layouts, err := svc.ListLayouts(ctx)
	require.NoError(t, err)
	var stored map[string]interface{}
	for _, l := range layouts {
		if l.Name == "Recipe Card"+suffix {
			id := l.ID
			t.Cleanup(func() { _ = svc.DeleteLayout(context.Background(), id) })
			require.NoError(t, json.Unmarshal(l.Definition, &stored))
		}
	}
	require.NotNil(t, stored)
	found := false
	for _, o := range stored["objects"].([]interface{}) {
		if fb, ok := o.(map[string]interface{})["field_binding"].(map[string]interface{}); ok && fb["value_list_id"] != nil {
			require.Equal(t, listID, fb["value_list_id"], "the field offers the list the import created")
			found = true
		}
	}
	require.True(t, found)
}

// A layout of the classic look names no theme and takes the file's.
func TestNativeLayoutWithoutThemeTakesTheFilesTheme(t *testing.T) {
	design := strings.Replace(layoutObjectsFixture, `<LayoutThemeReference name="Plain"/>`, `<LayoutThemeReference></LayoutThemeReference>`, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	parts := got.Bundle.Layouts[0].Definition["parts"].([]map[string]interface{})
	if parts[0]["fill_color"] != "#000080" {
		t.Fatalf("header fill: %v", parts[0])
	}
}

// A trigger set on a migrated object is attached to that object by its id,
// whether the object shows a field or not (#26).
func TestNativeLayoutTriggersFindTheirObject(t *testing.T) {
	trigger := `<ScriptTriggers membercount="1"><ScriptTrigger id="1" action="OnObjectEnter" browseMode="True"><ScriptReference id="1" name="Synthetic"/></ScriptTrigger></ScriptTriggers>`
	design := strings.Replace(layoutObjectsFixture,
		`<Tooltip><Calculation><Text><![CDATA["Price in euros"]]></Text></Calculation></Tooltip>`,
		`<Tooltip><Calculation><Text><![CDATA["Price in euros"]]></Text></Calculation></Tooltip>`+trigger, 1)
	design = strings.Replace(design,
		`<Bounds top="65" left="340" bottom="125" right="500"/>`,
		`<Bounds top="65" left="340" bottom="125" right="500"/>`+trigger, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	def := got.Bundle.Layouts[0].Definition
	ids := map[string]string{}
	for _, o := range def["objects"].([]map[string]interface{}) {
		if o["type"] == "rect" || o["name"] == "price" {
			ids[o["type"].(string)] = o["id"].(string)
		}
	}
	list, _ := def[schema.LayoutScriptTriggersKey].([]interface{})
	if len(list) != 2 {
		t.Fatalf("triggers: %v", list)
	}
	attached := map[string]bool{}
	for _, raw := range list {
		attached[raw.(map[string]interface{})["object_id"].(string)] = true
	}
	if !attached[ids["field"]] || !attached[ids["rect"]] {
		t.Fatalf("triggers %v not attached to objects %v", list, ids)
	}
}

// A field object keeps the repetition it starts at and whether it may be
// entered in each mode; a date is read in the file's order, and in no
// guessed order when the file's is unknown.
func TestNativeLayoutEntryRepetitionAndDates(t *testing.T) {
	_, def := convertLayoutFixture(t)
	if def["date_order"] != "dmy" {
		t.Fatalf("date order: %v", def["date_order"])
	}
	fields := objectsByType(def)["field"]
	binding := func(i int) map[string]interface{} { return fields[i]["field_binding"].(map[string]interface{}) }
	if b := binding(0); b["allow_browse_entry"] != nil || b["allow_find_entry"] != nil {
		t.Fatalf("a field FileMaker lets be entered says nothing: %v", b)
	}
	if b := binding(1); b["allow_find_entry"] != false || b["allow_browse_entry"] != nil {
		t.Fatalf("no entry in Find mode: %v", b)
	}
	if b := binding(2); b["allow_browse_entry"] != false || b["allow_find_entry"] != nil {
		t.Fatalf("no entry in Browse mode: %v", b)
	}
	if b := binding(3); b["first_repetition"] != 2.0 || b["repetitions_shown"] != 3.0 {
		t.Fatalf("repetitions 2 to 4: %v", b)
	}
	if _, ok := binding(0)["first_repetition"]; ok {
		t.Fatalf("a box from the first repetition says nothing: %v", binding(0))
	}

	for order, want := range map[string]interface{}{"mdy": "mm/dd/yyyy", "dmy": "dd/mm/yyyy", "": nil} {
		got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: layoutObjectsFixture, NativeDateOrder: order})
		if err != nil {
			t.Fatal(err)
		}
		d := got.Bundle.Layouts[0].Definition
		calendars := 0
		for _, o := range d["objects"].([]map[string]interface{}) {
			if b, _ := o["field_binding"].(map[string]interface{}); b != nil && b["control_style"] == "drop_down_calendar" {
				calendars++
				style, _ := o["style"].(map[string]interface{})
				if got := style["date_format"]; got != want {
					t.Fatalf("order %q: date format %v, want %v", order, got, want)
				}
			}
		}
		if calendars != 1 {
			t.Fatalf("calendars: %d", calendars)
		}
		if order == "" && d["date_order"] != nil {
			t.Fatalf("no date order is made up: %v", d["date_order"])
		}
	}
}

// Buttons, button bars and grouped buttons run what FileMaker's run: a
// script, bound to the imported scripts by id then by a name only one has,
// or a single step. One whose script cannot be told is kept switched off.
func TestNativeLayoutButtonsRunTheirScripts(t *testing.T) {
	scripts := `<ScriptCatalog>
      <Script id="5" name="Open Recipe"/>
      <Script id="6" name="Twice"/>
      <Script id="7" name="Twice"/>
    </ScriptCatalog>
    <LayoutCatalog>`
	buttons := `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
                <Button>
                  <Label><Text><StyledText><Data><![CDATA[Open]]></Data></StyledText></Text></Label>
                  <action><ScriptReference id="5" name="Open Recipe" UUID="x"/><Calculation><Text><![CDATA[Title]]></Text></Calculation><Options>4</Options></action>
                </Button>
              </LayoutObject>
              <LayoutObject type="Button Bar">
                <UUID>00000000-0000-0000-0000-000000000040</UUID>
                <Bounds top="260" left="20" bottom="280" right="220"/>
                <ButtonBar><ObjectList membercount="2">
                  <LayoutObject type="Button"><UUID>00000000-0000-0000-0000-000000000041</UUID><Bounds top="260" left="20" bottom="280" right="120"/>
                    <Button><Label><Text><StyledText><Data><![CDATA[Twice]]></Data></StyledText></Text></Label>
                    <action><ScriptReference id="99" name="Twice" UUID="y"/><Options>4</Options></action></Button>
                  </LayoutObject>
                  <LayoutObject type="Button"><UUID>00000000-0000-0000-0000-000000000042</UUID><Bounds top="260" left="120" bottom="280" right="220"/>
                    <Button><Label><Text><StyledText><Data><![CDATA[Next]]></Data></StyledText></Text></Label>
                    <action><Step enable="True" id="16" name="Go to Record/Request/Page"><Options>0</Options></Step></action></Button>
                  </LayoutObject>
                </ObjectList><Select><id>1</id></Select></ButtonBar>
              </LayoutObject>
              <LayoutObject type="Grouped Button">
                <UUID>00000000-0000-0000-0000-000000000043</UUID>
                <Bounds top="285" left="20" bottom="300" right="200"/>
                <GroupedButton><ObjectList membercount="1">
                  <LayoutObject type="Text"><UUID>00000000-0000-0000-0000-000000000044</UUID><Bounds top="285" left="20" bottom="300" right="200"/>
                    <Text><StyledText><Data><![CDATA[Gone]]></Data></StyledText></Text>
                  </LayoutObject>
                </ObjectList>
                <action><ScriptReference id="42" name="Deleted Script" UUID="z"/><Options>4</Options></action></GroupedButton>
              </LayoutObject>`
	design := strings.Replace(layoutObjectsFixture, "<LayoutCatalog>", scripts, 1)
	design = strings.Replace(design, `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>`, buttons, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	scriptID := map[string]string{}
	for _, sc := range got.Bundle.Scripts {
		scriptID[sc.Name] = sc.ID
	}
	if scriptID["Open Recipe"] == "" {
		t.Fatalf("scripts: %v", scriptID)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	var buttonsSeen []map[string]interface{}
	gonePos, groupPos := -1, -1
	for i, raw := range def["objects"].([]interface{}) {
		o := raw.(map[string]interface{})
		if o["type"] == "button" {
			buttonsSeen = append(buttonsSeen, o)
			if o["text"] == "" {
				groupPos = i
			}
		}
		if o["type"] == "label" && o["text"] == "Gone" {
			gonePos = i
		}
		if strings.Contains(string(encoded), sourceScriptIDKey) {
			t.Fatalf("the FileMaker script id must not be stored: %s", encoded)
		}
	}
	if len(buttonsSeen) != 4 {
		t.Fatalf("buttons: %v", buttonsSeen)
	}
	action := func(i int) map[string]interface{} { return buttonsSeen[i]["action"].(map[string]interface{}) }

	open := action(0)
	if buttonsSeen[0]["text"] != "Open" || open["type"] != "perform_script" || open["script_id"] != scriptID["Open Recipe"] ||
		open["enabled"] == false || open["parameter"] == nil || open["parameter_problem"] != nil {
		t.Fatalf("script button: %v", buttonsSeen[0])
	}
	// Two scripts are named "Twice" and the id is not one of them.
	if twice := action(1); twice["script_id"] != nil || twice["enabled"] != false {
		t.Fatalf("ambiguous script button: %v", twice)
	}
	if next := action(2); next["type"] != "single_step" || next["step_type"] != "Go to Record/Request/Page" || next["enabled"] == false {
		t.Fatalf("single step button: %v", next)
	}
	// The grouped button is drawn by its members, its click over them.
	if gone := action(3); gone["enabled"] != false || groupPos < gonePos || gonePos < 0 {
		t.Fatalf("grouped button: %v (label at %d, button at %d)", buttonsSeen[3], gonePos, groupPos)
	}
	counts := got.Report.Counts.LayoutObjects
	for _, kind := range []string{"Button", "Button Bar", "Grouped Button"} {
		if c := counts[kind]; c.Migrated == 0 || c.Migrated != c.Source {
			t.Fatalf("%s: %+v", kind, c)
		}
	}
	if counts["Button"].Migrated != 3 {
		t.Fatalf("counts: %+v", counts)
	}
	reported := false
	for _, it := range got.Report.ManualActionRequired {
		if strings.Contains(it.Detail, "Migrated buttons kept switched off") &&
			strings.Contains(it.Detail, "1 run a script that is not among") && strings.Contains(it.Detail, "1 run a script whose name several") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("report: %+v", got.Report.ManualActionRequired)
	}
}

func TestNativeLayoutPortalsShowTheirRelatedRecords(t *testing.T) {
	portals := `<LayoutObject type="Portal">
                <UUID>00000000-0000-0000-0000-000000000050</UUID>
                <Bounds top="300" left="20" bottom="400" right="300"/>
                <Portal>
                  <TableOccurrenceReference id="2" name="Ingredients"/>
                  <Options index="2" show="4"/>
                  <SortSpecification value="True" maintain="True" blanksLast="False"><SortList membercount="2">
                    <Sort type="Descending"><PrimaryField><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference></PrimaryField></Sort>
                    <Sort type="Ascending"><PrimaryField><FieldReference name="Title" id="1"><TableOccurrenceReference name="Recipes" id="1"/></FieldReference></PrimaryField></Sort>
                  </SortList></SortSpecification>
                  <Calculation><Text><![CDATA[Quantity > 1]]></Text></Calculation>
                  <ObjectList>
                    <LayoutObject type="Edit Box"><UUID>00000000-0000-0000-0000-000000000051</UUID><Bounds top="300" left="20" bottom="320" right="120"/>
                      <Field><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference></Field>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>
              <LayoutObject type="Portal">
                <UUID>00000000-0000-0000-0000-000000000052</UUID>
                <Bounds top="410" left="20" bottom="500" right="300"/>
                <Portal>
                  <TableOccurrenceReference id="2" name="Ingredients"/>
                  <Options index="1" show="3"/>
                  <Calculation><Text><![CDATA[Nowhere::Thing = 1]]></Text></Calculation>
                  <ObjectList/>
                </Portal>
              </LayoutObject>
              <LayoutObject type="Portal">
                <UUID>00000000-0000-0000-0000-000000000053</UUID>
                <Bounds top="510" left="20" bottom="600" right="300"/>
                <Portal>
                  <TableOccurrenceReference id="4" name="Ingredient Recipes"/>
                  <Options index="1" show="3"/>
                  <ObjectList>
                    <LayoutObject type="Text"><UUID>00000000-0000-0000-0000-000000000054</UUID><Bounds top="510" left="20" bottom="530" right="120"/>
                      <Text><StyledText><Data><![CDATA[Far]]></Data></StyledText></Text>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>`
	design := strings.Replace(layoutObjectsFixture, `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>`, portals, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	byType := objectsByType(def)
	if len(byType["portal"]) != 2 {
		t.Fatalf("portals: %v", byType["portal"])
	}
	var relID string
	for _, r := range got.Bundle.Relationships {
		if r.Name == "Recipes_Ingredients" {
			relID = r.ID
		}
	}
	first := byType["portal"][0]["portal_config"].(map[string]interface{})
	if first["relationship_id"] != relID || first["occurrence"] != "Ingredients" || first["initial_row"] != 2.0 ||
		first["row_count"] != 4.0 || first["sort"] != "-quantity" || first["filter"] != "quantity > 1" ||
		first["filter_problem"] != nil || first["allow_creation"] != true {
		t.Fatalf("portal: %v", first)
	}
	// A filter that cannot be bound shows no record rather than all of them.
	second := byType["portal"][1]["portal_config"].(map[string]interface{})
	if second["filter"] != "0" || second["filter_problem"] != true {
		t.Fatalf("portal with an unbound filter: %v", second)
	}
	// The portal's row objects follow it, so they are drawn over it.
	var portalAt, fieldAt = -1, -1
	for i, raw := range def["objects"].([]interface{}) {
		o := raw.(map[string]interface{})
		if o["type"] == "portal" && portalAt < 0 {
			portalAt = i
		}
		if b, _ := o["field_binding"].(map[string]interface{}); b != nil && b["table_occurrence"] == "Ingredients" && o["y"] == 300.0 {
			fieldAt = i
		}
	}
	if portalAt < 0 || fieldAt < portalAt {
		t.Fatalf("row field at %d, portal at %d", fieldAt, portalAt)
	}
	for _, l := range byType["label"] {
		if l["text"] == "Far" {
			t.Fatal("a portal several relationships away is left out with its objects")
		}
	}
	if c := got.Report.Counts.LayoutObjects["Portal"]; c.Source != 3 || c.Migrated != 2 {
		t.Fatalf("portal counts: %+v", c)
	}
	var review string
	for _, it := range got.Report.ManualActionRequired {
		if it.Domain == "layout" {
			review += it.Detail
		}
	}
	for _, want := range []string{"1 portal showing the records of an occurrence several relationships away", "1 portal whose filter calculation could not be bound", "1 portal sort field of another occurrence"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review %q lacks %q", review, want)
		}
	}
}

func TestNativeLayoutConditionalFormatsByCalculation(t *testing.T) {
	design := strings.Replace(layoutObjectsFixture,
		`<Conditions><Hide findMode="True"><Calculation><Text><![CDATA[Archived = 1]]></Text></Calculation></Hide></Conditions>`,
		`<Conditions><Hide findMode="True"><Calculation><Text><![CDATA[Archived = 1]]></Text></Calculation></Hide>
		<Formatting>
		  <Condition id="1" type="0"><Options>7</Options><Calculation><Text><![CDATA[Price > 100]]></Text></Calculation>
		    <LocalCSS><![CDATA[self:normal .self { color: rgba(100%,0%,0%,1); background-color: rgba(0%,0%,100%,1); font-weight: bold; }]]></LocalCSS></Condition>
		  <Condition id="2" type="3"><Options>5</Options><Calculation><Text><![CDATA[10]]></Text></Calculation>
		    <LocalCSS><![CDATA[self:normal .self { color: rgba(0%,100%,0%,1); }]]></LocalCSS></Condition>
		  <Condition id="3" type="0"><Options>7</Options><Calculation><Text><![CDATA[Nowhere::Thing]]></Text></Calculation>
		    <LocalCSS><![CDATA[self:normal .self { color: rgba(0%,100%,0%,1); }]]></LocalCSS></Condition>
		</Formatting></Conditions>`, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	var rules []interface{}
	for _, o := range objectsByType(def)["field"] {
		if o["name"] == "price" {
			rules, _ = o["style"].(map[string]interface{})["conditional_rules"].([]interface{})
		}
	}
	if len(rules) != 1 {
		t.Fatalf("rules: %v", rules)
	}
	rule := rules[0].(map[string]interface{})
	if rule["operator"] != "formula" || rule["value"] != "price > 100" || rule["text_color"] != "#FF0000" ||
		rule["fill_color"] != "#0000FF" || rule["bold"] != true {
		t.Fatalf("rule: %v", rule)
	}
	var review string
	for _, it := range got.Report.ManualActionRequired {
		if it.Domain == "layout" {
			review += it.Detail
		}
	}
	if !strings.Contains(review, "2 conditional formats not kept") {
		t.Fatalf("review: %s", review)
	}
}

func TestNativeLayoutPopoversOpenOverTheLayout(t *testing.T) {
	popover := `<LayoutObject type="Popover Button">
                <UUID>00000000-0000-0000-0000-000000000060</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
                <PopoverButton>
                  <Label><Text><StyledText><Data><![CDATA[More]]></Data></StyledText></Text></Label>
                  <LayoutObject type="PopoverPanel">
                    <UUID>00000000-0000-0000-0000-000000000061</UUID>
                    <Bounds top="100" left="300" bottom="200" right="500"/>
                    <ObjectList>
                      <LayoutObject type="Text"><UUID>00000000-0000-0000-0000-000000000062</UUID><Bounds top="110" left="310" bottom="130" right="400"/>
                        <Text><StyledText><Data><![CDATA[Inside]]></Data></StyledText></Text>
                      </LayoutObject>
                    </ObjectList>
                  </LayoutObject>
                </PopoverButton>
              </LayoutObject>`
	design := strings.Replace(layoutObjectsFixture, `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>`, popover, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	objects := def["objects"].([]interface{})
	var button map[string]interface{}
	for _, raw := range objects {
		if o := raw.(map[string]interface{}); o["type"] == "popover_button" {
			button = o
		}
	}
	if button == nil || button["text"] != "More" || button["default_panel"] != -1.0 || button["action"] != nil {
		t.Fatalf("popover button: %v", button)
	}
	// The panel's background and objects come last, on the popover's panel.
	n := len(objects)
	back, inside := objects[n-2].(map[string]interface{}), objects[n-1].(map[string]interface{})
	for _, o := range []map[string]interface{}{back, inside} {
		p, _ := o["panel"].(map[string]interface{})
		if p == nil || p["control"] != button["id"] || p["index"] != 0.0 {
			t.Fatalf("popover object: %v", o)
		}
	}
	if back["type"] != "rect" || back["x"] != 300.0 || inside["text"] != "Inside" {
		t.Fatalf("popover panel: %v / %v", back, inside)
	}
	if c := got.Report.Counts.LayoutObjects["Popover Button"]; c.Source != 1 || c.Migrated != 1 {
		t.Fatalf("popover counts: %+v", c)
	}
}
