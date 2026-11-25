package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// Synthetic designs only: Projects and their Tasks, joined on the project
// code. In FileMaker each side of a relationship has its own "Allow creation
// of records in this table", "Delete related records in this table when a
// record is deleted in the other table" and "Sort records", acting on that
// side's records; the importers keep each on its own side.

type sideOptions struct {
	create, purge bool
	sort          string
}

func optionsOf(rel schema.BundleRelationship) (left, right sideOptions) {
	return sideOptions{rel.LeftAllowCreation, rel.LeftCascadeDelete, rel.LeftSortRelated},
		sideOptions{rel.AllowCreation, rel.CascadeDelete, rel.SortRelated}
}

func onlyRelationship(t *testing.T, res *ConvertedSolution) schema.BundleRelationship {
	t.Helper()
	if len(res.Bundle.Relationships) != 1 {
		t.Fatalf("expected the one relationship, got %d (rejected: %+v)", len(res.Bundle.Relationships), res.Report.Rejected)
	}
	return res.Bundle.Relationships[0]
}

func relationshipReportDetail(t *testing.T, report *ConversionReport) string {
	t.Helper()
	for _, item := range report.Preserved {
		if item.Domain == "relationship" {
			return item.Detail
		}
	}
	t.Fatalf("no relationship in the report: %+v", report.Preserved)
	return ""
}

// projectTasksDDR is a classic DDR whose relationship sides carry the given
// attributes and children.
func projectTasksDDR(leftAttrs, leftChildren, rightAttrs, rightChildren string) string {
	return `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable id="129" name="Projects"><FieldCatalog>` +
		`<Field id="1" name="Code" dataType="Text" fieldType="Normal"/>` +
		`<Field id="2" name="Title" dataType="Text" fieldType="Normal"/>` +
		`</FieldCatalog></BaseTable>` +
		`<BaseTable id="130" name="Tasks"><FieldCatalog>` +
		`<Field id="1" name="Project Code" dataType="Text" fieldType="Normal"/>` +
		`<Field id="2" name="Due" dataType="Date" fieldType="Normal"/>` +
		`<Field id="3" name="Rank" dataType="Number" fieldType="Normal"/>` +
		`</FieldCatalog></BaseTable>` +
		`</BaseTableCatalog><RelationshipGraph>` +
		`<TableList><Table id="1" name="Projects" baseTable="Projects" baseTableId="129"/>` +
		`<Table id="2" name="Project Tasks" baseTable="Tasks" baseTableId="130"/></TableList>` +
		`<RelationshipList><Relationship id="1">` +
		`<LeftTable name="Projects"` + leftAttrs + `>` + leftChildren + `</LeftTable>` +
		`<RightTable name="Project Tasks"` + rightAttrs + `>` + rightChildren + `</RightTable>` +
		`<JoinPredicateList><JoinPredicate type="Equal">` +
		`<LeftField><Field id="1" name="Code" table="Projects"/></LeftField>` +
		`<RightField><Field id="1" name="Project Code" table="Project Tasks"/></RightField>` +
		`</JoinPredicate></JoinPredicateList></Relationship></RelationshipList></RelationshipGraph></File></FMPReport>`
}

func TestRelationshipSideOptionsFromDDR(t *testing.T) {
	const on = ` cascadeCreate="True" cascadeDelete="True"`
	tasksSort := `<SortList><Sort type="Descending"><Field id="2" name="Due" table="Project Tasks"/></Sort>` +
		`<Sort type="Ascending"><Field id="3" name="Rank" table="Project Tasks"/></Sort></SortList>`
	projectsSort := `<SortList><Sort type="Ascending"><Field id="2" name="Title" table="Projects"/></Sort></SortList>`

	for _, tc := range []struct {
		name                        string
		leftAttrs, leftChildren     string
		rightAttrs, rightChildren   string
		wantLeft, wantRight         sideOptions
		wantDetail, wantNotInDetail string
	}{
		{
			name:       "the right side's options act on tasks",
			rightAttrs: on, rightChildren: tasksSort,
			wantRight:       sideOptions{true, true, "-due,rank"},
			wantDetail:      "Project Tasks: records created from Projects, deleted with their Projects record, sorted by -due,rank",
			wantNotInDetail: "Projects: records",
		},
		{
			name:      "the left side's options act on projects, and delete no task",
			leftAttrs: on, leftChildren: projectsSort,
			wantLeft:        sideOptions{true, true, "title"},
			wantDetail:      "; Projects: records created from Project Tasks, deleted with their Project Tasks record, sorted by title",
			wantNotInDetail: "Project Tasks: records",
		},
		{
			name:      "each side keeps its own",
			leftAttrs: ` cascadeDelete="True"`, leftChildren: projectsSort,
			rightAttrs: ` cascadeCreate="True"`, rightChildren: tasksSort,
			wantLeft:   sideOptions{false, true, "title"},
			wantRight:  sideOptions{true, false, "-due,rank"},
			wantDetail: "Projects: deleted with their Project Tasks record, sorted by title; Project Tasks: records created from Projects, sorted by -due,rank",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := AnalyzeAndConvert(MigrationRequest{
				DesignXML: projectTasksDDR(tc.leftAttrs, tc.leftChildren, tc.rightAttrs, tc.rightChildren),
			})
			if err != nil {
				t.Fatalf("AnalyzeAndConvert: %v", err)
			}
			left, right := optionsOf(onlyRelationship(t, res))
			if left != tc.wantLeft || right != tc.wantRight {
				t.Fatalf("options: left %+v right %+v, want left %+v right %+v", left, right, tc.wantLeft, tc.wantRight)
			}
			detail := relationshipReportDetail(t, res.Report)
			if !strings.Contains(detail, tc.wantDetail) {
				t.Fatalf("the report says %q, want it to say %q", detail, tc.wantDetail)
			}
			if tc.wantNotInDetail != "" && strings.Contains(detail, tc.wantNotInDetail) {
				t.Fatalf("the report says %q, which must not name %q", detail, tc.wantNotInDetail)
			}
		})
	}
}

// A sort FileMaker keeps on a field this importer cannot keep is reported
// rather than dropped silently, and the levels it can keep are kept.
func TestRelationshipSortLevelsThatCannotBeKeptAreReported(t *testing.T) {
	sort := `<SortList>` +
		`<Sort type="Custom"><Field id="3" name="Rank" table="Project Tasks"/></Sort>` +
		`<Sort type="Ascending"><Field id="2" name="Title" table="Projects"/></Sort>` +
		`<Sort type="Ascending"><Field id="9" name="Gone" table="Project Tasks"/></Sort>` +
		`<Sort type="Descending"><Field id="2" name="Due" table="Project Tasks"/></Sort>` +
		`</SortList>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: projectTasksDDR("", "", "", sort)})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	if got := onlyRelationship(t, res).SortRelated; got != "rank,-due" {
		t.Fatalf("sort = %q, want rank,-due", got)
	}
	var notes []string
	for _, item := range res.Report.Translated {
		if item.Domain == "relationship" {
			notes = append(notes, item.Detail)
		}
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"Rank is sorted by a value list", "Projects::Title is a field of another occurrence", "Gone is not a migrated field"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the report must say %q, got %q", want, joined)
		}
	}
}

// A "Save a Copy as XML" export names the side's options the same way, and
// its sort in a SortSpecification whose fields are references.
func TestRelationshipSideOptionsFromSaveAsXML(t *testing.T) {
	doc := strings.Replace(saveAsXMLFixture,
		`<LeftTable name="Recipes"/>`,
		`<LeftTable name="Recipes" type="Local" cascadeCreate="False" cascadeDelete="True">`+
			`<TableOccurrenceReference id="1" name="Recipes"/>`+
			`<SortSpecification value="True" maintain="False"><SortList>`+
			`<Sort type="Descending"><PrimaryField><FieldReference id="2" name="Servings">`+
			`<TableOccurrenceReference id="1" name="Recipes"/></FieldReference></PrimaryField></Sort>`+
			`</SortList></SortSpecification></LeftTable>`, 1)
	doc = strings.Replace(doc,
		`<RightTable name="Ingredients" cascadeCreate="True"/>`,
		`<RightTable name="Ingredients" type="Local" cascadeCreate="True" cascadeDelete="False">`+
			`<TableOccurrenceReference id="2" name="Ingredients"/>`+
			`<SortSpecification value="False"/></RightTable>`, 1)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	left, right := optionsOf(onlyRelationship(t, res))
	if want := (sideOptions{false, true, "-servings"}); left != want {
		t.Fatalf("left side: got %+v, want %+v", left, want)
	}
	if want := (sideOptions{true, false, ""}); right != want {
		t.Fatalf("right side: got %+v, want %+v", right, want)
	}
}

// projectTasksHTML is an HTML Database Design Report whose relationship
// gives each side's option in that side's column.
func projectTasksHTML(create, purge, sort [2]string) string {
	row := func(title string, cells [2]string) string {
		return `<tr vAlign="top"><td class="chartheadersnobg">` + title + `</td><td class="regularcharttext">` + cells[0] +
			`</td><td class="regularcharttext"></td><td class="regularcharttext">` + cells[1] + `</td></tr>`
	}
	return `<!DOCTYPE html><html><body>` +
		`<span class="partheading"><a name="valFileOptionOverviewSectionAnchor_"/>Synthetic.fmp12</span>` +
		`<span class="section"><a name="valTablesSectionAnchor_"/>Tables</span>` +
		`<span class="subheader"><a name="129BaseTableAnchor_"/>projects</span><table>` +
		`<tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>` +
		`<tr><td><a name="1291FieldAnchor_"/>code</td><td>Normal, Text</td><td></td></tr>` +
		`<tr><td><a name="1292FieldAnchor_"/>title</td><td>Normal, Text</td><td></td></tr></table>` +
		`<span class="subheader"><a name="130BaseTableAnchor_"/>tasks</span><table>` +
		`<tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>` +
		`<tr><td><a name="1301FieldAnchor_"/>project_code</td><td>Normal, Text</td><td></td></tr>` +
		`<tr><td><a name="1302FieldAnchor_"/>due</td><td>Normal, Date</td><td></td></tr></table>` +
		`<span class="section"><a name="valRelationshipsSectionAnchor_"/>Relationships</span>` +
		`<span class="subheader">Table Occurrences</span><table>` +
		`<tr><th>Name</th><th>Source</th></tr>` +
		`<tr><td>projects</td><td>projects</td></tr><tr><td>tasks</td><td>tasks</td></tr></table>` +
		`<span class="partheading"><a name="valDetailsSectionAnchor_"/>Relationships: Details</span>` +
		`<p><span class="partheading">Relationship: <a name="tasks=projectsRelationshipAnchor_"/><em>tasks=projects</em></span></p>` +
		`<table width="100%" border="1"><tbody>` +
		`<tr vAlign="top"><td class="chartheadersnobg">Table Occurrence</td><td class="regularcharttext"><em>tasks</em></td>` +
		`<td class="regularcharttext"></td><td class="regularcharttext"><em>projects</em></td></tr>` +
		`<tr vAlign="top"><td class="chartheadersnobg">Field</td><td class="regularcharttext"><a href="#1301FieldAnchor_">project_code</a></td>` +
		`<td class="regularcharttext" align="middle">=</td><td class="regularcharttext"><a href="#1291FieldAnchor_">code</a></td></tr>` +
		row("Allow creation of records via this relationship", create) +
		row("Delete related records in this table when a record is deleted in the other table", purge) +
		row("Sort records", sort) +
		`</tbody></table>` +
		`<span class="section"><a name="valLayoutsSectionAnchor_"/>Layouts</span>` +
		`<span class="section"><a name="valScriptsSectionAnchor_"/>Scripts</span>` +
		`</body></html>`
}

func TestRelationshipSideOptionsFromHTMLDDR(t *testing.T) {
	off := [2]string{"Off", "Off"}
	byDue := `<p>On</p><p>Field: <a href="#1302FieldAnchor_"><em>tasks</em>::due</a></p><ul><li>Descending order</li></ul>`
	byTitle := `<p>On</p><p>Field: <a href="#1292FieldAnchor_">title</a></p><ul><li>Ascending order</li></ul>`

	for _, tc := range []struct {
		name                string
		create, purge, sort [2]string
		wantLeft, wantRight sideOptions
	}{
		{
			// The report's left table is tasks: deleting a project deletes
			// its tasks, and deleting a task deletes no project.
			name:   "the left column's options act on the left table only",
			create: [2]string{"On", "Off"}, purge: [2]string{"On", "Off"}, sort: [2]string{byDue, "<p>Off</p>"},
			wantLeft: sideOptions{true, true, "-due"},
		},
		{
			name:   "the right column's options act on the right table only",
			create: [2]string{"Off", "On"}, purge: [2]string{"Off", "On"}, sort: [2]string{"Off", byTitle},
			wantRight: sideOptions{true, true, "title"},
		},
		{
			name:   "both columns",
			create: [2]string{"On", "On"}, purge: [2]string{"Off", "On"}, sort: [2]string{byDue, byTitle},
			wantLeft:  sideOptions{true, false, "-due"},
			wantRight: sideOptions{true, true, "title"},
		},
		{
			name:   "none",
			create: off, purge: off, sort: off,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: projectTasksHTML(tc.create, tc.purge, tc.sort)})
			if err != nil {
				t.Fatalf("AnalyzeAndConvert: %v", err)
			}
			if res.Report.SourceFormat != "filemaker_ddr_html" {
				t.Fatalf("read as %q", res.Report.SourceFormat)
			}
			left, right := optionsOf(onlyRelationship(t, res))
			if left != tc.wantLeft || right != tc.wantRight {
				t.Fatalf("options: left %+v right %+v, want left %+v right %+v", left, right, tc.wantLeft, tc.wantRight)
			}
		})
	}
}

// A report that says a relationship deletes, without saying on which side,
// is not guessed at: deleting on the wrong side deletes parents with a child.
func TestHTMLDDRDeleteWithoutASideIsLeftToTheOperator(t *testing.T) {
	doc := projectTasksHTMLLegacy
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	rel := onlyRelationship(t, res)
	left, right := optionsOf(rel)
	if left.purge || right.purge {
		t.Fatalf("a delete option of no stated side must not be set: left %+v right %+v", left, right)
	}
	if !right.create {
		t.Fatalf("an unsided Create is the right side's, as before: %+v", right)
	}
	found := false
	for _, item := range res.Report.ManualActionRequired {
		if item.Domain == "relationship" && strings.Contains(item.Detail, "Delete") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the operator must be told to set the delete option: %+v", res.Report.ManualActionRequired)
	}
}

const projectTasksHTMLLegacy = `<!DOCTYPE html><html><body>` +
	`<span class="partheading"><a name="valFileOptionOverviewSectionAnchor_"/>Synthetic.fmp12</span>` +
	`<span class="section"><a name="valTablesSectionAnchor_"/>Tables</span>` +
	`<span class="subheader"><a name="129BaseTableAnchor_"/>projects</span><table>` +
	`<tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>` +
	`<tr><td><a name="1291FieldAnchor_"/>code</td><td>Normal, Text</td><td></td></tr></table>` +
	`<span class="subheader"><a name="130BaseTableAnchor_"/>tasks</span><table>` +
	`<tr><th>Field Name</th><th>Field Type</th><th>Options | Comments</th></tr>` +
	`<tr><td><a name="1301FieldAnchor_"/>project_code</td><td>Normal, Text</td><td></td></tr></table>` +
	`<span class="section"><a name="valRelationshipsSectionAnchor_"/>Relationships</span>` +
	`<span class="subheader">Table Occurrences</span><table>` +
	`<tr><th>Name</th><th>Source</th></tr>` +
	`<tr><td>projects</td><td>projects</td></tr><tr><td>tasks</td><td>tasks</td></tr></table>` +
	`<span class="subheader"><a name="valDetailsSectionAnchor_"/>Details</span>` +
	`<span class="subheader"><a name="1RelationshipAnchor_"/>tasks=projects</span><table>` +
	`<tr><td>Predicate:</td><td></td><td>tasks::project_code = projects::code</td></tr>` +
	`<tr><td></td><td>Create</td><td>Yes</td></tr>` +
	`<tr><td></td><td>Delete</td><td>Yes</td></tr>` +
	`</table>` +
	`<span class="section"><a name="valLayoutsSectionAnchor_"/>Layouts</span>` +
	`<span class="section"><a name="valScriptsSectionAnchor_"/>Scripts</span>` +
	`</body></html>`

// The recipe report sets its options on the left table, the table of the
// child records: deleting a recipe deletes its tags, deleting a tag deletes
// no recipe, and a recipe's ingredients are sorted, not the recipe.
func TestRecipeHTMLDDRKeepsItsOptionsOnTheLeftSide(t *testing.T) {
	raw, err := os.ReadFile("testdata/recipes_html_ddr.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	res, err := AnalyzeAndConvert(MigrationRequest{SolutionName: "Recipes", DesignXML: string(raw)})
	if err != nil {
		t.Fatalf("AnalyzeAndConvert: %v", err)
	}
	options := map[string][2]sideOptions{}
	for _, rel := range res.Bundle.Relationships {
		left, right := optionsOf(rel)
		options[rel.Name] = [2]sideOptions{left, right}
	}
	tags, ok := options["recipe_tags=recipes"]
	if !ok {
		t.Fatalf("no recipe_tags=recipes relationship in %v", options)
	}
	if want := (sideOptions{create: true, purge: true}); tags[0] != want {
		t.Fatalf("recipe_tags side: got %+v, want %+v", tags[0], want)
	}
	if tags[1] != (sideOptions{}) {
		t.Fatalf("recipes side: got %+v, want no option", tags[1])
	}
	ingredients := options["recipe_ingredients=recipes"]
	if want := (sideOptions{create: true, sort: "grouping,order"}); ingredients[0] != want {
		t.Fatalf("recipe_ingredients side: got %+v, want %+v", ingredients[0], want)
	}
}

// An HTML report writes each criterion's operator in the middle cell of its
// "Field" row, in FileMaker's signs; each is kept as it is, and one this
// reader does not know refuses the relationship rather than being read as =.
func TestHTMLDDRRelationshipOperators(t *testing.T) {
	off := [2]string{"Off", "Off"}
	withCriteria := func(rows string) string {
		doc := projectTasksHTML(off, off, off)
		field := `<tr vAlign="top"><td class="chartheadersnobg">Field</td><td class="regularcharttext"><a href="#1301FieldAnchor_">project_code</a></td>` +
			`<td class="regularcharttext" align="middle">=</td><td class="regularcharttext"><a href="#1291FieldAnchor_">code</a></td></tr>`
		if !strings.Contains(doc, field) {
			t.Fatal("the fixture's criterion row changed")
		}
		return strings.Replace(doc, field, rows, 1)
	}
	criterion := func(title, left, op, right string) string {
		return `<tr vAlign="top"><td class="chartheadersnobg">` + title + `</td><td class="regularcharttext">` + left +
			`</td><td class="regularcharttext" align="middle">` + op + `</td><td class="regularcharttext">` + right + `</td></tr>`
	}

	for _, tc := range []struct{ cell, want string }{
		{"=", "="}, {"&ne;", "≠"}, {"≠", "≠"}, {"&lt;", "<"}, {"&le;", "≤"}, {"&gt;", ">"}, {"≥", "≥"}, {"&times;", "×"},
	} {
		res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: withCriteria(criterion("Field", "project_code", tc.cell, "code"))})
		if err != nil {
			t.Fatalf("%s: %v", tc.cell, err)
		}
		if got := describeCriteria(res.Bundle, onlyRelationship(t, res)); len(got) != 1 || got[0] != "tasks.project_code "+tc.want+" projects.code" {
			t.Fatalf("%s: criteria %v", tc.cell, got)
		}
	}

	t.Run("every criterion, each with its operator", func(t *testing.T) {
		res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: withCriteria(
			criterion("Field", "project_code", "=", "code") + criterion("", "due", "&ge;", "title"))})
		if err != nil {
			t.Fatal(err)
		}
		got := describeCriteria(res.Bundle, onlyRelationship(t, res))
		if want := "tasks.project_code = projects.code; tasks.due ≥ projects.title"; strings.Join(got, "; ") != want {
			t.Fatalf("criteria %v, want %s", got, want)
		}
	})

	for _, op := range []string{"~", ""} {
		t.Run("an operator that is not known refuses the relationship: "+op, func(t *testing.T) {
			res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: withCriteria(criterion("Field", "project_code", op, "code"))})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Bundle.Relationships) != 0 {
				t.Fatalf("migrated as %v", describeCriteria(res.Bundle, res.Bundle.Relationships[0]))
			}
			if len(res.Report.Rejected) == 0 || !strings.Contains(res.Report.Rejected[0].Detail, "Unsupported join operator") {
				t.Fatalf("the report must say why: %+v", res.Report.Rejected)
			}
		})
	}

	t.Run("a report of three cells writes the operator between the fields", func(t *testing.T) {
		for _, tc := range []struct{ text, want string }{
			{"tasks::project_code &ge; projects::code", "≥"},
			{"tasks::project_code&lt;projects::code", "<"},
		} {
			doc := strings.Replace(projectTasksHTMLLegacy, "tasks::project_code = projects::code", tc.text, 1)
			res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
			if err != nil {
				t.Fatal(err)
			}
			if got := describeCriteria(res.Bundle, onlyRelationship(t, res)); len(got) != 1 || got[0] != "tasks.project_code "+tc.want+" projects.code" {
				t.Fatalf("%s: criteria %v", tc.text, got)
			}
		}
		doc := strings.Replace(projectTasksHTMLLegacy, "tasks::project_code = projects::code", "tasks::project_code like projects::code", 1)
		res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Bundle.Relationships) != 0 || len(res.Report.Rejected) == 0 {
			t.Fatalf("an operator that is not known must refuse the relationship: %+v", res.Report.Rejected)
		}
	})
}
