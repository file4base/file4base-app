package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
// FileMaker writes one: parts with their size, a theme with CSS, a library
// picture and the usual nesting of groups and tab panels. Its objects,
// nested ones too, have their bounds in layout coordinates, as some tools
// write them; nestedRelativeBody is written as FileMaker does. The names
// are invented.
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
		testdb.Unavailable(t, "database not available:", err)
	}
	t.Cleanup(func() { _ = driver.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
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

// A button's step writes the field it names, a script of another file is
// never bound to one of this file's, and a protected PDF is not written
// unprotected.
func TestNativeLayoutButtonStepsKeepTheirTargets(t *testing.T) {
	button := func(uid, action string) string {
		return `<LayoutObject type="Button"><UUID>00000000-0000-0000-0000-0000000000` + uid + `</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
                <Button><Label><Text><StyledText><Data><![CDATA[B` + uid + `]]></Data></StyledText></Text></Label>
                <action>` + action + `</action></Button></LayoutObject>`
	}
	buttons := button("a1", `<Step enable="True" id="91" name="Replace Field Contents"><ParameterValues>
                  <Parameter type="FieldReference"><FieldReference id="7" name="Unit_Price"><TableOccurrenceReference id="1" name="Recipes"/></FieldReference></Parameter>
                </ParameterValues></Step>`) +
		button("a2", `<Step enable="True" id="91" name="Replace Field Contents"><ParameterValues>
                  <Parameter type="FieldReference"><FieldReference id="9" name="Nowhere"><TableOccurrenceReference id="1" name="Recipes"/></FieldReference></Parameter>
                </ParameterValues></Step>`) +
		button("a3", `<ScriptReference id="5" name="Report" UUID="r"><FileReference id="1" name="Reporting"/></ScriptReference><Options>4</Options>`) +
		button("a4", `<Step enable="True" id="144" name="Save Records as PDF"><ParameterValues>
                  <Parameter type="Options"><Options type="Records being browsed" value="1"><Security><Open value="True"/><Control value="False"/></Security></Options></Parameter>
                </ParameterValues></Step>`) +
		button("a5", `<Step enable="True" id="1" name="Perform Script"><ParameterValues>
                  <Parameter type="List"><List><ScriptReference id="5" name="Report"><FileReference id="1" name="Reporting"/></ScriptReference></List></Parameter>
                </ParameterValues></Step>`)
	design := strings.Replace(layoutObjectsFixture, `<Field id="5" name="Archived" datatype="number" fieldtype="normal"/>`,
		`<Field id="5" name="Archived" datatype="number" fieldtype="normal"/>
        <Field id="6" name="Unit Price" datatype="number" fieldtype="normal"/>
        <Field id="7" name="Unit_Price" datatype="number" fieldtype="normal"/>`, 1)
	design = strings.Replace(design, "<LayoutCatalog>", `<ScriptCatalog><Script id="5" name="Delete Everything"/></ScriptCatalog>
    <LayoutCatalog>`, 1)
	design = strings.Replace(design, `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>`, buttons, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]bool{}
	for _, tbl := range got.Bundle.Tables {
		for _, c := range tbl.Columns {
			if c.DisplayName == "Unit Price" || c.DisplayName == "Unit_Price" {
				columns[c.DisplayName+"="+c.Name] = true
			}
		}
	}
	if !columns["Unit Price=unit_price"] || !columns["Unit_Price=unit_price_2"] {
		t.Fatalf("the two fields must be imported as two columns: %v", columns)
	}
	actions := map[string]map[string]interface{}{}
	forEachLayoutObject(got.Bundle.Layouts[0].Definition, func(o map[string]interface{}) {
		if a, _ := o["action"].(map[string]interface{}); a != nil {
			actions[o["text"].(string)] = a
		}
	})
	params := func(a map[string]interface{}) map[string]interface{} { return a["params"].(map[string]interface{}) }

	if a := actions["Ba1"]; a == nil || params(a)["field"] != "unit_price_2" || a["enabled"] == false {
		t.Fatalf("the button must replace the second field's column: %v", a)
	}
	if a := actions["Ba2"]; a == nil || a["enabled"] != false || a["field_problem"] == nil {
		t.Fatalf("a step naming a field that was not imported is switched off: %v", a)
	}
	if a := actions["Ba3"]; a == nil || a["script_id"] != nil || a["enabled"] != false || a["file_reference"] != "Reporting" {
		t.Fatalf("a script of another file must not run the local script with its id: %v", a)
	}
	if a := actions["Ba4"]; a == nil || params(a)["pdf_security"] != true {
		t.Fatalf("a protected PDF must carry pdf_security: %v", a)
	}
	if a := actions["Ba5"]; a == nil || params(a)["file_reference"] != "Reporting" {
		t.Fatalf("a Perform Script step naming another file's script must keep that file: %v", a)
	}
	external := false
	for _, it := range got.Report.ManualActionRequired {
		if strings.Contains(it.Detail, "1 run a script of another FileMaker file") {
			external = true
		}
	}
	if !external {
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
                  <SortSpecification value="True" maintain="True" blanksLast="False"><SortList membercount="3">
                    <Sort type="Descending"><PrimaryField><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference></PrimaryField></Sort>
                    <Sort type="Ascending"><PrimaryField><FieldReference name="Title" id="1"><TableOccurrenceReference name="Recipes" id="1"/></FieldReference></PrimaryField></Sort>
                    <Sort type="Custom"><PrimaryField><FieldReference name="RecipeTitle" id="1"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference></PrimaryField><ValueListReference id="9" name="Not In This File"/></Sort>
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
	if len(byType["portal"]) != 3 {
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
	// FileMaker's whole sort order is kept, a field of another occurrence
	// and a value list's order included, though the portal sorts by its
	// own fields only.
	sortFields, _ := json.Marshal(first["sort_fields"])
	if string(sortFields) != `[{"field_name":"quantity","order":"descending","table_occurrence":"Ingredients"},`+
		`{"field_name":"title","order":"ascending","table_occurrence":"Recipes"},`+
		`{"field_name":"recipetitle","order":"custom","table_occurrence":"Ingredients"}]` {
		t.Fatalf("portal sort fields: %s", sortFields)
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
	// Each says which portal it is in; an object outside a portal says none.
	objects := def["objects"].([]interface{})
	if row, portal := objects[fieldAt].(map[string]interface{}), objects[portalAt].(map[string]interface{}); row["portal"] == nil || row["portal"] != portal["id"] {
		t.Fatalf("row field in portal %v, want %v", row["portal"], portal["id"])
	}
	for _, raw := range objects[:portalAt] {
		if o := raw.(map[string]interface{}); o["portal"] != nil {
			t.Fatalf("an object before the portal says it is in %v", o["portal"])
		}
	}
	// A portal several relationships away lists the records along its
	// path, and creates none.
	far := byType["portal"][2]["portal_config"].(map[string]interface{})
	if path, _ := far["occurrence_path"].([]interface{}); len(path) != 2 || path[0] != "Ingredients" ||
		path[1] != "Ingredient Recipes" || far["relationship_id"] != nil || far["allow_creation"] != false ||
		far["occurrence"] != "Ingredient Recipes" {
		t.Fatalf("portal several relationships away: %v", far)
	}
	kept := false
	for _, l := range byType["label"] {
		kept = kept || l["text"] == "Far"
	}
	if !kept {
		t.Fatal("the row objects of a portal several relationships away are kept")
	}
	if c := got.Report.Counts.LayoutObjects["Portal"]; c.Source != 3 || c.Migrated != 3 {
		t.Fatalf("portal counts: %+v", c)
	}
	var review string
	for _, it := range got.Report.ManualActionRequired {
		if it.Domain == "layout" {
			review += it.Detail
		}
	}
	for _, want := range []string{"1 portal showing the records of an occurrence several relationships away, listed along the path", "1 portal whose filter calculation could not be bound", "2 portal sort fields of another occurrence or in a value list's order, left out of the portal's sort"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review %q lacks %q", review, want)
		}
	}
}

// fmCondition writes a <Condition> as FileMaker's Save a Copy as XML does:
// its type (0 "Formula is", 1 to 20 a "Value is" condition), the
// calculation FileMaker keeps for it, the values its dialog shows and the
// format it gives, with a red text.
func fmCondition(kind int, formula, start, end string) string {
	rng := ""
	if start != "-" {
		rng = "<Range><Start>" + start + "</Start><End>" + end + "</End></Range>"
	}
	return fmt.Sprintf(`<Condition type="%d" id="0"><Options>7</Options><Calculation><Text><![CDATA[%s]]></Text></Calculation>%s
	  <LocalCSS type="ObjectCustomStyle" name="" displayName=""><![CDATA[self:normal .self
{
	color: rgba(100%%,0%%,0%%,1);
	background-color: rgba(0%%,0%%,100%%,1);
	font-weight: bold;
}
]]></LocalCSS></Condition>`, kind, formula, rng)
}

// The calculations FileMaker 26 was seen to write for its "Value is"
// conditions, with the values its dialog shows (a date as the file's
// locale writes it, "-" for none); not between, not containing, beginning
// with, ending with, after today, more than x days passed, more than and
// less than x days ahead were not seen, and are written alike.
var fmValueConditions = map[string][]struct {
	kind                int
	formula, start, end string
}{
	"price": {
		{1, `Self≥1 and Self≤5`, "1", "5"},
		{2, `not (Self≥1 and Self≤5)`, "1", "5"},
		{3, `Self=0`, "0", ""},
		{4, `Self≠2,5`, "2,5", ""},
		{5, `Self>100`, "100", ""},
		{6, `Self<0`, "0", ""},
		{7, `Self≥10`, "10", ""},
		{8, `Self≤10`, "10", ""},
		{13, `IsEmpty(Self)`, "-", ""},
		{0, `Self < 0 and Price ≠ 7`, "-", ""},
		{5, `Self>40`, "50", ""}, // a calculation that disagrees with its dialog
	},
	"served on": {
		{5, `Self>Date(3;15;1990)`, "15/03/1990", ""},
		{14, `GetAsDate(Self) < Get(CurrentDate)`, "-", ""},
		{15, `GetAsDate(Self) = Get(CurrentDate)`, "-", ""},
		{16, `GetAsDate(Self) > Get(CurrentDate)`, "-", ""},
		{17, `(Get(CurrentDate) - GetAsDate(Self)) > 3`, "3", ""},
		{18, `(GetAsDate(Self) - Get(CurrentDate)) > 3`, "3", ""},
		{19, `(Get(CurrentDate) - GetAsDate(Self)) < 4`, "4", ""},
		{20, `(GetAsDate(Self) - Get(CurrentDate)) < 4`, "4", ""},
	},
	"unit": {
		{3, `Self="k\"g"`, `k"g`, ""},
		{4, `Self≠"kg"`, "kg", ""},
		{9, `PatternCount(Self; "g") > 0`, "g", ""},
		{10, `PatternCount(Self; "g") = 0`, "g", ""},
		{11, `Left(Self; Length("k")) = "k"`, "k", ""},
		{12, `Right(Self; Length("g")) = "g"`, "g", ""},
	},
}

func TestNativeLayoutConditionalFormats(t *testing.T) {
	conditions := func(field string) string {
		var b strings.Builder
		for _, c := range fmValueConditions[field] {
			b.WriteString(fmCondition(c.kind, c.formula, c.start, c.end))
		}
		return b.String()
	}
	replace := func(design, uuid, formatting string) string {
		at := "<UUID>00000000-0000-0000-0000-" + uuid + "</UUID>"
		if !strings.Contains(design, at) {
			t.Fatalf("no object %s", uuid)
		}
		return strings.Replace(design, at, at+"<Conditions><Formatting>"+formatting+"</Formatting></Conditions>", 1)
	}
	design := strings.Replace(layoutObjectsFixture,
		`<Conditions><Hide findMode="True"><Calculation><Text><![CDATA[Archived = 1]]></Text></Calculation></Hide></Conditions>`,
		`<Conditions><Hide findMode="True"><Calculation><Text><![CDATA[Archived = 1]]></Text></Calculation></Hide>
		<Formatting>`+fmCondition(0, "Price > 100", "-", "")+conditions("price")+
			fmCondition(0, "Nowhere::Thing", "-", "")+fmCondition(21, "Self > 0", "-", "")+`</Formatting></Conditions>`, 1)
	design = replace(design, "000000000004", conditions("served on"))
	design = replace(design, "000000000005", conditions("unit"))
	// A related field's Self reads it from the layout's occurrence.
	design = replace(design, "000000000031", fmCondition(5, "Self>5", "5", ""))
	// A box showing repetitions 2 and 3 of a field of three has a Self for
	// each.
	design = strings.Replace(design, `<Field id="2" name="Quantity" datatype="number" fieldtype="normal"/>`,
		`<Field id="2" name="Quantity" datatype="number" fieldtype="normal" maxRepetition="3"/>`, 1)
	design = replace(design, "000000000006", fmCondition(13, "IsEmpty(Self)", "-", "")+fmCondition(0, "Recipes::Price > 1", "-", ""))
	// A condition switched off in FileMaker is never applied. Self reads a
	// field of an occurrence no relationship joins to the layout's, which
	// cannot be read.
	design = replace(design, "000000000007", strings.Replace(fmCondition(13, "IsEmpty(Self)", "-", ""), "<Options>7</Options>", "<Options>6</Options>", 1)+
		fmCondition(14, "GetAsDate(Self) = Get(CurrentDate)", "-", ""))
	// A text's Self is the text it shows, which one merging fields
	// changes on each record.
	design = replace(design, "000000000001", fmCondition(0, "Recipes::Price > 1", "-", "")+fmCondition(0, `Self = "Recipe"`, "-", ""))
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, SourceLocale: "fr-FR"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	rulesOf := map[string][]interface{}{}
	for _, raw := range def["objects"].([]interface{}) {
		o := raw.(map[string]interface{})
		if rules, _ := o["style"].(map[string]interface{})["conditional_rules"].([]interface{}); rules != nil {
			if b, ok := o["field_binding"].(map[string]interface{}); ok {
				rulesOf[fmt.Sprint(b["table_occurrence"], "::", b["field_name"])] = rules
			} else {
				rulesOf[fmt.Sprint(o["type"])] = rules
			}
		}
	}
	formulas := func(key string) []string {
		var out []string
		for _, raw := range rulesOf[key] {
			r := raw.(map[string]interface{})
			if r["operator"] != "formula" || r["text_color"] != "#FF0000" || r["fill_color"] != "#0000FF" || r["bold"] != true {
				t.Fatalf("%s: rule %v", key, r)
			}
			out = append(out, r["value"].(string))
		}
		return out
	}
	// Each condition keeps FileMaker's calculation, Self included, with
	// its fields bound and its decimals written with a point.
	want := map[string][]string{
		"Recipes::price": {"price > 100", "Self≥1 and Self≤5", "not (Self≥1 and Self≤5)", "Self=0", "Self≠2.5",
			"Self>100", "Self<0", "Self≥10", "Self≤10", "IsEmpty(Self)", "Self < 0 and price ≠ 7", "Self>40"},
		"Recipes::served_on": {"Self>Date(3;15;1990)", "GetAsDate(Self) < Get(CurrentDate)",
			"GetAsDate(Self) = Get(CurrentDate)", "GetAsDate(Self) > Get(CurrentDate)",
			"(Get(CurrentDate) - GetAsDate(Self)) > 3", "(GetAsDate(Self) - Get(CurrentDate)) > 3",
			"(Get(CurrentDate) - GetAsDate(Self)) < 4", "(GetAsDate(Self) - Get(CurrentDate)) < 4"},
		"Recipes::unit": {`Self="k\"g"`, `Self≠"kg"`, `PatternCount(Self; "g") > 0`, `PatternCount(Self; "g") = 0`,
			`Left(Self; Length("k")) = "k"`, `Right(Self; Length("g")) = "g"`},
		"Ingredient Recipes::price": {"Self>5"},
		// Browse mode answers Self in each repetition the box shows.
		"Ingredients::quantity": {"IsEmpty(Self)", "Recipes::price > 1"},
		"label":                 {"Recipes::price > 1"},
	}
	if len(rulesOf) != len(want) {
		t.Fatalf("objects with rules: %v", rulesOf)
	}
	for key, w := range want {
		if g := formulas(key); !slices.Equal(g, w) {
			t.Errorf("%s:\n got %q\nwant %q", key, g, w)
		}
	}
	var review string
	for _, it := range got.Report.ManualActionRequired {
		if it.Domain == "layout" {
			review += it.Detail
		}
	}
	// Not kept: a formula that cannot be bound, an unknown condition, a
	// Self that cannot be read and the merging text's Self.
	for _, w := range []string{"4 conditional formats not kept", "1 conditional format switched off in FileMaker, left out",
		`1 conditional format whose "Value is" condition shows other values than the calculation FileMaker keeps for it`} {
		if !strings.Contains(review, w) {
			t.Errorf("review %q lacks %q", review, w)
		}
	}
}

// The objects of a portal's rows keep their conditional formats, Self
// included, which each row answers as its own record's value.
func TestNativeLayoutConditionalFormatInAPortalRow(t *testing.T) {
	portal := `<LayoutObject type="Portal">
                <UUID>00000000-0000-0000-0000-000000000050</UUID>
                <Bounds top="300" left="20" bottom="400" right="300"/>
                <Portal>
                  <TableOccurrenceReference id="2" name="Ingredients"/>
                  <ObjectList>
                    <LayoutObject type="Edit Box"><UUID>00000000-0000-0000-0000-000000000051</UUID><Bounds top="300" left="20" bottom="320" right="120"/>
                      <Conditions><Formatting>` + fmCondition(6, "Self<1", "1", "") +
		fmCondition(0, "Ingredients::Quantity > Price", "", "") + `</Formatting></Conditions>
                      <Field><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference></Field>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>`
	design := strings.Replace(layoutObjectsFixture, `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>`, portal, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	if !strings.Contains(string(encoded), `"value":"Self\u003c1"`) {
		t.Fatalf("definition: %s", encoded)
	}
	// Each row evaluates it from the portal's occurrence: a field of the
	// layout's, which FileMaker reads from the layout, is written qualified.
	if !strings.Contains(string(encoded), `"value":"Ingredients::quantity \u003e Recipes::price"`) {
		t.Fatalf("the layout's field read from the portal's occurrence: %s", encoded)
	}
	for _, it := range got.Report.ManualActionRequired {
		if strings.Contains(it.Detail, "conditional format") {
			t.Errorf("reported: %s", it.Detail)
		}
	}
}

// Texts and buttons keep their conditional formats, with Self as the text
// they show: a button bar's segments and a popover button too. A grouped
// button's, a calculated label's Self and a merging text's Self are not
// kept. In a portal row, a hide condition is the row's, as its formats
// are.
func TestNativeLayoutConditionalFormatsOfTextsAndButtons(t *testing.T) {
	formats := func(formulas ...string) string {
		var b strings.Builder
		for _, f := range formulas {
			b.WriteString(fmCondition(0, f, "-", ""))
		}
		return "<Conditions><Formatting>" + b.String() + "</Formatting></Conditions>"
	}
	// formats with a hide condition.
	hidden := func(hide string, formulas ...string) string {
		return strings.Replace(formats(formulas...), "<Conditions>",
			"<Conditions><Hide><Calculation><Text><![CDATA["+hide+"]]></Text></Calculation></Hide>", 1)
	}
	objects := `<LayoutObject type="Text"><UUID>00000000-0000-0000-0000-000000000070</UUID><Bounds top="240" left="20" bottom="255" right="100"/>
                ` + hidden(`Self = "Shut"`, `Self = "Open"`, "Price > 1") + `
                <Text><StyledText><Data><![CDATA[Open]]></Data></StyledText></Text>
              </LayoutObject>
              <LayoutObject type="Text"><UUID>00000000-0000-0000-0000-000000000071</UUID><Bounds top="240" left="100" bottom="255" right="200"/>
                ` + hidden(`Self = "x"`, `Self = "x"`) + `
                <Text><StyledText><Data><![CDATA[Price <<Price>>]]></Data></StyledText></Text>
              </LayoutObject>
              <LayoutObject type="Button"><UUID>00000000-0000-0000-0000-000000000072</UUID><Bounds top="260" left="20" bottom="275" right="100"/>
                ` + formats(`Self = "Go"`) + `
                <Button><Label><Text><StyledText><Data><![CDATA[Go]]></Data></StyledText></Text></Label></Button>
              </LayoutObject>
              <LayoutObject type="Button"><UUID>00000000-0000-0000-0000-000000000073</UUID><Bounds top="260" left="100" bottom="275" right="200"/>
                ` + hidden("IsEmpty(Self)", `Self = "x"`, "Price = 2") + `
                <Button><Label><Calculation><Text><![CDATA[Title & "!"]]></Text></Calculation></Label></Button>
              </LayoutObject>
              <LayoutObject type="Button Bar"><UUID>00000000-0000-0000-0000-000000000074</UUID><Bounds top="280" left="20" bottom="295" right="220"/>
                <ButtonBar><ObjectList membercount="1">
                  <LayoutObject type="Button"><UUID>00000000-0000-0000-0000-000000000075</UUID><Bounds top="280" left="20" bottom="295" right="120"/>
                    ` + formats(`Self = "Seg"`) + `
                    <Button><Label><Text><StyledText><Data><![CDATA[Seg]]></Data></StyledText></Text></Label></Button>
                  </LayoutObject>
                  <LayoutObject type="Popover Button"><UUID>00000000-0000-0000-0000-00000000007a</UUID><Bounds top="280" left="120" bottom="295" right="220"/>
                    ` + formats(`Self = "Pop"`) + `
                    <PopoverButton><Label><Text><StyledText><Data><![CDATA[Pop]]></Data></StyledText></Text></Label></PopoverButton>
                  </LayoutObject>
                </ObjectList></ButtonBar>
              </LayoutObject>
              <LayoutObject type="Popover Button"><UUID>00000000-0000-0000-0000-000000000076</UUID><Bounds top="300" left="20" bottom="315" right="100"/>
                ` + formats(`Self = "More"`) + `
                <PopoverButton><Label><Text><StyledText><Data><![CDATA[More]]></Data></StyledText></Text></Label></PopoverButton>
              </LayoutObject>
              <LayoutObject type="Grouped Button"><UUID>00000000-0000-0000-0000-000000000077</UUID><Bounds top="320" left="20" bottom="335" right="200"/>
                ` + formats("Price > 3") + `
                <GroupedButton><ObjectList membercount="0"></ObjectList></GroupedButton>
              </LayoutObject>
              <LayoutObject type="Portal"><UUID>00000000-0000-0000-0000-000000000078</UUID><Bounds top="340" left="20" bottom="400" right="300"/>
                <Portal><TableOccurrenceReference id="2" name="Ingredients"/><ObjectList>
                  <LayoutObject type="Text"><UUID>00000000-0000-0000-0000-000000000079</UUID><Bounds top="340" left="20" bottom="355" right="120"/>
                    <Conditions><Hide><Calculation><Text><![CDATA[Ingredients::Quantity > Price]]></Text></Calculation></Hide>
                    <Formatting>` + fmCondition(0, `Self = "Row"`, "-", "") + `</Formatting></Conditions>
                    <Text><StyledText><Data><![CDATA[Row]]></Data></StyledText></Text>
                  </LayoutObject>
                </ObjectList></Portal>
              </LayoutObject>`
	design := strings.Replace(layoutObjectsFixture, `<LayoutObject type="Button">
                <UUID>00000000-0000-0000-0000-00000000000e</UUID>
                <Bounds top="240" left="20" bottom="255" right="100"/>
              </LayoutObject>`, objects, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	formulasOf := map[string][]string{}
	hideOf := map[string]interface{}{}
	for _, raw := range def["objects"].([]interface{}) {
		o := raw.(map[string]interface{})
		key := fmt.Sprint(o["type"], ":", o["text"])
		hideOf[key] = o["hide_when"]
		rules, _ := o["style"].(map[string]interface{})["conditional_rules"].([]interface{})
		for _, r := range rules {
			rule := r.(map[string]interface{})
			if rule["text_color"] != "#FF0000" || rule["fill_color"] != "#0000FF" || rule["bold"] != true {
				t.Fatalf("%s: rule %v", key, rule)
			}
			formulasOf[key] = append(formulasOf[key], rule["value"].(string))
		}
	}
	want := map[string][]string{
		"label:Open":          {`Self = "Open"`, "price > 1"},
		"button:Go":           {`Self = "Go"`},
		"button:":             {"price = 2"}, // the calculated label's
		"button:Seg":          {`Self = "Seg"`},
		"popover_button:More": {`Self = "More"`},
		"popover_button:Pop":  {`Self = "Pop"`}, // a button bar's segment
		"label:Row":           {`Self = "Row"`},
	}
	if len(formulasOf) != len(want) {
		t.Fatalf("objects with formats: %v", formulasOf)
	}
	for key, w := range want {
		if g := formulasOf[key]; !slices.Equal(g, w) {
			t.Errorf("%s:\n got %q\nwant %q", key, g, w)
		}
	}
	// The row evaluates its hide condition from the portal's occurrence:
	// the layout's field is written qualified.
	if h := hideOf["label:Row"]; h != "Ingredients::quantity > Recipes::price" {
		t.Errorf("hide condition in a portal row: %v", h)
	}
	// A hide condition reads the text's Self as its formats do: not kept
	// where Browse mode does not know it.
	if h := hideOf["label:Open"]; h != `Self = "Shut"` {
		t.Errorf("hide condition of a text: %v", h)
	}
	for _, key := range []string{"button:", "label:Price {{price}}"} {
		if h, seen := hideOf[key]; !seen || h != nil {
			t.Errorf("hide condition reading an unknown Self (%s): %v, %v", key, seen, h)
		}
	}
	var review string
	for _, it := range got.Report.ManualActionRequired {
		if it.Domain == "layout" {
			review += it.Detail
		}
	}
	for _, w := range []string{"3 conditional formats not kept (a condition that could not be bound, or on an object other than a field, a text or a button)",
		`2 objects with a "Hide object when" calculation that could not be bound`} {
		if !strings.Contains(review, w) {
			t.Errorf("review %q lacks %q", review, w)
		}
	}
}

func TestRangeInFormula(t *testing.T) {
	rng := func(start, end string) *xmlNode {
		return &xmlNode{Name: "Range", Children: []*xmlNode{{Name: "Start", Text: start}, {Name: "End", Text: end}}}
	}
	for _, c := range []struct {
		formula, start, end string
		operands            int
		want                bool
	}{
		{"Self>0", "0", "", 1, true},
		{"Self≥1 and Self≤5", "1", "5", 2, true},
		{"Self≥1 and Self≤5", "1", "6", 2, false},
		{"Self>40", "50", "", 1, false},
		{"Self>Date(3;15;1990)", "15/03/1990", "", 1, true},
		{"Self>Date(3;15;1990)", "16/03/1990", "", 1, false},
		{`Self="a \"c\" \\ d"`, `a "c" \ d`, "", 1, true},
		{`Self="Kg"`, "kg", "", 1, true},
		{`Self="kg"`, "&quot;kg&quot;", "", 1, true},
		{`Self="kg"`, "g", "", 1, false},
		{`Self="12"`, "12", "", 1, true},
		{"Self>-5", "5", "", 1, false},
		{"Self>-5", "-5", "", 1, true},
		{"Self>2.5", "2,50", "", 1, true},
		{"Self>2.5", "25", "", 1, false},
		{"IsEmpty(Self)", "", "", 0, true},
		{"Self=1", "", "", 1, true},
	} {
		if got := rangeInFormula(rng(c.start, c.end), c.operands, c.formula); got != c.want {
			t.Errorf("rangeInFormula(%q, %q, %q) = %v, want %v", c.formula, c.start, c.end, got, c.want)
		}
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

func TestNativeLayoutButtonStepFieldOfAnotherFile(t *testing.T) {
	l := &nativeLayout{occName: "Orders", ctx: &layoutObjectContext{binding: formulaBinding{
		occByName: map[string]boundOccurrence{"stock": {name: "Stock", external: true}},
	}}}
	// The decoder keeps the source name's sanitised form, which for "ID"
	// is the other file's primary key: its migration names that field id_2.
	entry := map[string]interface{}{"field": "id", "field_label": "ID", "field_table": "Stock"}
	if !l.bindStepFields(map[string]interface{}{"target": entry}) {
		t.Fatal("a field of another file cannot be checked here and must not switch the button off")
	}
	if entry["field"] != "id_2" {
		t.Fatalf("field = %v, want the other file's column id_2", entry["field"])
	}
}

// An edit box keeps its value list, as FileMaker keeps it for every control
// (the one it auto-completes from); Display Style 5 is taken as a concealed
// edit box.
func TestNativeLayoutEditBoxKeepsItsValueListAndConcealment(t *testing.T) {
	old := `<FieldReference name="Price" id="2"><TableOccurrenceReference name="Ingredient Recipes" id="4"/></FieldReference>
                  <Display Style="0" show="1"/>`
	if !strings.Contains(layoutObjectsFixture, old) {
		t.Fatal("the fixture changed")
	}
	design := strings.Replace(layoutObjectsFixture, old, `<FieldReference name="Price" id="2"><TableOccurrenceReference name="Ingredient Recipes" id="4"/></FieldReference>
                  <Display Style="5" show="1"><ValueListReference name="Units" id="1"/></Display>`, 1)
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(encoded, &def); err != nil {
		t.Fatal(err)
	}
	var unitList string
	for _, vl := range got.Bundle.ValueLists {
		if vl.Name == "Units" {
			unitList = vl.ID
		}
	}
	found := false
	for _, f := range objectsByType(def)["field"] {
		b := f["field_binding"].(map[string]interface{})
		switch {
		case f["y"] == 200.0 && f["x"] == 220.0:
			found = true
			if b["control_style"] != "edit_box" || b["value_list_id"] != unitList || unitList == "" || b["concealed"] != true {
				t.Fatalf("concealed edit box with a value list: %v", b)
			}
		case b["concealed"] != nil:
			t.Fatalf("a field that is not concealed says it is: %v", b)
		}
	}
	if !found {
		t.Fatal("no edit box at 220,200")
	}
}

// nestedRelativeBody is the body of a layout as FileMaker's Save as XML
// writes it: an object inside a portal, a tab panel, a group, a grouped
// button, a button bar or a popover's panel has its bounds from the
// container's top-left corner, a popover's panel its own on the layout.
// The objects are invented.
const nestedRelativeBody = `<ObjectList>
              <LayoutObject type="Portal" name="rows"><UUID>00000000-0000-0000-0000-0000000000a1</UUID><Bounds top="70" left="20" bottom="170" right="320"/>
                <Portal><TableOccurrenceReference id="2" name="Ingredients"/><Options index="1" show="4"/>
                  <ObjectList>
                    <LayoutObject type="Edit Box" name="qty"><UUID>00000000-0000-0000-0000-0000000000a2</UUID><Bounds top="2" left="4" bottom="22" right="104"/>
                      <Field><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference><Display Style="0" show="1"/></Field>
                    </LayoutObject>
                    <LayoutObject type="Text" name="edge"><UUID>00000000-0000-0000-0000-0000000000a3</UUID><Bounds top="-0.5" left="110" bottom="20" right="200"/>
                      <Text><StyledText><Data><![CDATA[Edge]]></Data></StyledText></Text>
                    </LayoutObject>
                    <LayoutObject type="Grouped Button" name="rowbutton"><UUID>00000000-0000-0000-0000-0000000000a4</UUID><Bounds top="2" left="210" bottom="22" right="290"/>
                      <GroupedButton><ObjectList>
                        <LayoutObject type="Text" name="rowlabel"><UUID>00000000-0000-0000-0000-0000000000a5</UUID><Bounds top="2" left="2" bottom="18" right="78"/>
                          <Text><StyledText><Data><![CDATA[Open]]></Data></StyledText></Text>
                        </LayoutObject>
                      </ObjectList>
                      <action><Step enable="True" id="16" name="Go to Record/Request/Page"><Options>0</Options></Step></action></GroupedButton>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>
              <LayoutObject type="Tab Control" name="tabs"><UUID>00000000-0000-0000-0000-0000000000b1</UUID><Bounds top="130" left="340" bottom="250" right="590"/>
                <TabControl default="0"><ObjectList>
                  <LayoutObject type="Panel"><Bounds top="0" left="0" bottom="120" right="250"/>
                    <TabPanel><Calculation><Text><![CDATA["One"]]></Text></Calculation><ObjectList>
                      <LayoutObject type="Group" name="group"><UUID>00000000-0000-0000-0000-0000000000b2</UUID><Bounds top="40" left="10" bottom="80" right="200"/>
                        <GroupedButton><ObjectList>
                          <LayoutObject type="Edit Box" name="title"><UUID>00000000-0000-0000-0000-0000000000b3</UUID><Bounds top="5" left="5" bottom="25" right="105"/>
                            <Field><FieldReference name="Title" id="1"><TableOccurrenceReference name="Recipes" id="1"/></FieldReference><Display Style="0" show="1"/></Field>
                          </LayoutObject>
                        </ObjectList></GroupedButton>
                      </LayoutObject>
                    </ObjectList></TabPanel>
                  </LayoutObject>
                </ObjectList></TabControl>
              </LayoutObject>
              <LayoutObject type="Popover Button" name="pop"><UUID>00000000-0000-0000-0000-0000000000c1</UUID><Bounds top="180" left="20" bottom="195" right="100"/>
                <PopoverButton>
                  <Label><Text><StyledText><Data><![CDATA[More]]></Data></StyledText></Text></Label>
                  <LayoutObject type="PopoverPanel" name="poppanel"><UUID>00000000-0000-0000-0000-0000000000c2</UUID><Bounds top="200" left="110" bottom="250" right="300"/>
                    <ObjectList>
                      <LayoutObject type="Text" name="inside"><UUID>00000000-0000-0000-0000-0000000000c3</UUID><Bounds top="10" left="10" bottom="30" right="100"/>
                        <Text><StyledText><Data><![CDATA[Inside]]></Data></StyledText></Text>
                      </LayoutObject>
                    </ObjectList>
                  </LayoutObject>
                </PopoverButton>
              </LayoutObject>
              <LayoutObject type="Button Bar" name="bar"><UUID>00000000-0000-0000-0000-0000000000d1</UUID><Bounds top="230" left="20" bottom="245" right="220"/>
                <ButtonBar><ObjectList membercount="1">
                  <LayoutObject type="Button" name="segment"><UUID>00000000-0000-0000-0000-0000000000d2</UUID><Bounds top="0" left="100" bottom="15" right="200"/>
                    <Button><Label><Text><StyledText><Data><![CDATA[Next]]></Data></StyledText></Text></Label>
                    <action><Step enable="True" id="16" name="Go to Record/Request/Page"><Options>0</Options></Step></action></Button>
                  </LayoutObject>
                </ObjectList></ButtonBar>
              </LayoutObject>
            </ObjectList>`

// withBody is layoutObjectsFixture with body as its body's objects.
func withBody(t *testing.T, body string) string {
	t.Helper()
	at := strings.Index(layoutObjectsFixture, `<Definition type="Body"`)
	start := at + strings.Index(layoutObjectsFixture[at:], "<ObjectList>")
	end := at + strings.Index(layoutObjectsFixture[at:], "</ObjectList>\n          </Part>") + len("</ObjectList>")
	if at < 0 || start < at || end < start {
		t.Fatal("the fixture's body moved")
	}
	return layoutObjectsFixture[:start] + body + layoutObjectsFixture[end:]
}

// convertedByName converts design and answers its layout's objects by name.
func convertedByName(t *testing.T, design string) (map[string]map[string]interface{}, []map[string]interface{}) {
	t.Helper()
	got, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got.Bundle.Layouts[0].Definition)
	var def map[string]interface{}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	named := map[string]map[string]interface{}{}
	var all []map[string]interface{}
	for _, o := range def["objects"].([]interface{}) {
		m := o.(map[string]interface{})
		all = append(all, m)
		if n, _ := m["name"].(string); n != "" {
			named[n] = m
		}
	}
	return named, all
}

func box(o map[string]interface{}) [4]float64 {
	return [4]float64{o["x"].(float64), o["y"].(float64), o["width"].(float64), o["height"].(float64)}
}

func TestNativeLayoutNestedBoundsFromTheirContainer(t *testing.T) {
	named, _ := convertedByName(t, withBody(t, nestedRelativeBody))
	want := map[string][4]float64{
		"rows":      {20, 70, 300, 100},
		"qty":       {24, 72, 100, 20},
		"edge":      {130, 70, 90, 20}, // jutting half a point above the portal: drawn from its edge
		"rowbutton": {230, 72, 80, 20},
		"rowlabel":  {232, 74, 76, 16},
		"tabs":      {340, 130, 250, 120},
		"title":     {355, 175, 100, 20}, // tab control + panel + group + its own
		"pop":       {20, 180, 80, 15},
		"poppanel":  {110, 200, 190, 50}, // a popover's panel is placed on the layout
		"inside":    {120, 210, 90, 20},
		"segment":   {120, 230, 100, 15},
	}
	for name, b := range want {
		o := named[name]
		if o == nil {
			t.Fatalf("%s not migrated: %v", name, named)
		}
		if box(o) != b {
			t.Errorf("%s: %v, want %v", name, box(o), b)
		}
	}
	portal := named["rows"]
	for _, name := range []string{"qty", "edge", "rowbutton", "rowlabel"} {
		if named[name]["portal"] != portal["id"] {
			t.Errorf("%s is not a row object of the portal: %v", name, named[name])
		}
	}
	if p, _ := named["title"]["panel"].(map[string]interface{}); p == nil || p["control"] != named["tabs"]["id"] {
		t.Errorf("title is not on the tab panel: %v", named["title"])
	}
}

// A file whose nested objects lie within their containers only in layout
// coordinates, as some tools write them, is read in layout coordinates.
func TestNativeLayoutNestedBoundsInLayoutCoordinates(t *testing.T) {
	named, _ := convertedByName(t, withBody(t, `<ObjectList>
              <LayoutObject type="Portal" name="rows"><UUID>00000000-0000-0000-0000-0000000000a1</UUID><Bounds top="130" left="20" bottom="230" right="320"/>
                <Portal><TableOccurrenceReference id="2" name="Ingredients"/><Options index="1" show="4"/>
                  <ObjectList>
                    <LayoutObject type="Edit Box" name="qty"><UUID>00000000-0000-0000-0000-0000000000a2</UUID><Bounds top="132" left="24" bottom="152" right="124"/>
                      <Field><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference><Display Style="0" show="1"/></Field>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>
            </ObjectList>`))
	if b := box(named["qty"]); b != [4]float64{24, 132, 100, 20} || named["qty"]["portal"] != named["rows"]["id"] {
		t.Fatalf("qty: %v", named["qty"])
	}
	// One that fits its portal read either way, but only its first row in
	// layout coordinates.
	named, _ = convertedByName(t, withBody(t, `<ObjectList>
              <LayoutObject type="Portal" name="rows"><UUID>00000000-0000-0000-0000-0000000000a1</UUID><Bounds top="70" left="20" bottom="170" right="320"/>
                <Portal><TableOccurrenceReference id="2" name="Ingredients"/><Options index="1" show="4"/>
                  <ObjectList>
                    <LayoutObject type="Edit Box" name="qty"><UUID>00000000-0000-0000-0000-0000000000a2</UUID><Bounds top="72" left="24" bottom="88" right="124"/>
                      <Field><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference><Display Style="0" show="1"/></Field>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>
            </ObjectList>`))
	if b := box(named["qty"]); b != [4]float64{24, 72, 100, 16} {
		t.Fatalf("qty in the first row: %v", named["qty"])
	}
	// The handwritten fixture, written so throughout, keeps its places.
	named, all := convertedByName(t, layoutObjectsFixture)
	for _, o := range all {
		if o["type"] == "oval" && (o["x"] != 430.0 || o["y"] != 160.0) {
			t.Fatalf("oval on the tab panel: %v", o)
		}
	}
	if b := box(named["price"]); b[0] != 20 || b[1] != 70 {
		t.Fatalf("grouped field: %v", named["price"])
	}
}

func TestNativeLayoutPortalRowsAtTheirPortalsEdges(t *testing.T) {
	named, _ := convertedByName(t, withBody(t, `<ObjectList>
              <LayoutObject type="Portal" name="rows"><UUID>00000000-0000-0000-0000-0000000000a1</UUID><Bounds top="70" left="20" bottom="170" right="320"/>
                <Portal><TableOccurrenceReference id="2" name="Ingredients"/><Options index="1" show="5"/>
                  <ObjectList>
                    <LayoutObject type="Popover Button" name="pop"><UUID>00000000-0000-0000-0000-0000000000c1</UUID><Bounds top="2" left="250" bottom="18" right="290"/>
                      <PopoverButton>
                        <Label><Text><StyledText><Data><![CDATA[More]]></Data></StyledText></Text></Label>
                        <LayoutObject type="PopoverPanel" name="poppanel"><UUID>00000000-0000-0000-0000-0000000000c2</UUID><Bounds top="100" left="280" bottom="160" right="380"/>
                          <ObjectList>
                            <LayoutObject type="Text" name="inside"><UUID>00000000-0000-0000-0000-0000000000c3</UUID><Bounds top="5" left="5" bottom="25" right="80"/>
                              <Text><StyledText><Data><![CDATA[Inside]]></Data></StyledText></Text>
                            </LayoutObject>
                          </ObjectList>
                        </LayoutObject>
                      </PopoverButton>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>
              <LayoutObject type="Portal" name="narrow"><UUID>00000000-0000-0000-0000-0000000000a5</UUID><Bounds top="180" left="0" bottom="230" right="50.1"/>
                <Portal><TableOccurrenceReference id="2" name="Ingredients"/><Options index="1" show="2"/>
                  <ObjectList>
                    <LayoutObject type="Edit Box" name="qty"><UUID>00000000-0000-0000-0000-0000000000a6</UUID><Bounds top="2" left="8.2" bottom="22" right="50.6"/>
                      <Field><FieldReference name="Quantity" id="2"><TableOccurrenceReference name="Ingredients" id="2"/></FieldReference><Display Style="0" show="1"/></Field>
                    </LayoutObject>
                  </ObjectList>
                </Portal>
              </LayoutObject>
            </ObjectList>`))
	// Rows as short as FileMaker draws them.
	if c := named["rows"]["portal_config"].(map[string]interface{}); c["row_count"] != 5.0 || c["row_height"] != 20.0 {
		t.Fatalf("portal rows: %v", c)
	}
	// A popover opened from a row is drawn whole over the layout.
	if b := box(named["pop"]); b != [4]float64{270, 72, 40, 16} || named["pop"]["portal"] != named["rows"]["id"] {
		t.Fatalf("popover button: %v", named["pop"])
	}
	if b := box(named["poppanel"]); b != [4]float64{280, 100, 100, 60} {
		t.Fatalf("popover panel: %v", named["poppanel"])
	}
	if b := box(named["inside"]); b != [4]float64{285, 105, 75, 20} {
		t.Fatalf("popover object: %v", named["inside"])
	}
	// Trimmed to the portal's edge, as a client adds it up.
	qty, narrow := named["qty"], named["narrow"]
	if qty["x"] != 8.2 || qty["x"].(float64)+qty["width"].(float64) > narrow["x"].(float64)+narrow["width"].(float64) || qty["width"].(float64) < 41.8 {
		t.Fatalf("trimmed row object: %v", qty)
	}
}
