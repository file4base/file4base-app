package schema_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// menuPaths writes a menu as "folder/item" paths, in order; a separator is
// "-" with its occurrence's id, if any.
func menuPaths(nodes []*schema.CatalogNode, prefix string) []string {
	var out []string
	for _, n := range nodes {
		name := prefix + n.Name
		switch n.Kind {
		case schema.MenuFolder:
			out = append(out, name+"/")
			out = append(out, menuPaths(n.Items, name+"/")...)
		case schema.MenuSeparator:
			out = append(out, prefix+"-")
		default:
			out = append(out, name)
		}
	}
	return out
}

// The layout and script menus, with their folders and separators, travel
// in a solution file and come back in their order; a file written before
// the menus were kept leaves them as they are; a layout created afterwards
// is listed after the others.
func TestMenusRoundTripThroughASolutionFile(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	src := newExternalSolution(t, ctx, mgr, "menus_src")
	tbl, _ := src.table(t, ctx, "items", "code")
	occ := src.occurrenceOf(t, ctx, tbl.ID)
	layoutIDs := map[string]string{}
	for _, name := range []string{"Zeta", "Alpha", "Inner"} {
		l, err := src.svc.CreateLayout(ctx, name, occ, []byte(`{"objects":[]}`))
		require.NoError(t, err)
		layoutIDs[name] = l.ID
	}
	scriptIDs := map[string]string{}
	for _, name := range []string{"Run", "Setup"} {
		sc, err := src.svc.CreateScript(ctx, name, "items", nil, true, nil)
		require.NoError(t, err)
		scriptIDs[name] = sc.ID
	}

	// Without menus, the layouts and scripts are listed by name.
	menu, err := src.svc.Menu(ctx, schema.CatalogLayouts)
	require.NoError(t, err)
	assert.Equal(t, []string{"Alpha", "Inner", "Zeta"}, menuPaths(menu, ""))

	file, err := src.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Menus"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	assert.Empty(t, bundle.Catalog, "menus listed by name are not written")
	bundle.Catalog = []schema.BundleCatalogEntry{
		{Catalog: schema.CatalogLayouts, Kind: schema.MenuLayout, ID: layoutIDs["Zeta"]},
		{Catalog: schema.CatalogLayouts, Kind: schema.MenuSeparator, ID: "sep", Name: "-", TableOccurrenceID: occ},
		{Catalog: schema.CatalogLayouts, Kind: schema.MenuFolder, ID: "f1", Name: "Work"},
		{Catalog: schema.CatalogLayouts, Kind: schema.MenuFolder, ID: "f2", Name: "Deeper", FolderID: "f1"},
		{Catalog: schema.CatalogLayouts, Kind: schema.MenuLayout, ID: layoutIDs["Inner"], FolderID: "f2"},
		{Catalog: schema.CatalogLayouts, Kind: schema.MenuLayout, ID: layoutIDs["Alpha"], FolderID: "f1"},
		{Catalog: schema.CatalogScripts, Kind: schema.MenuFolder, ID: "f1", Name: "Tools"},
		{Catalog: schema.CatalogScripts, Kind: schema.MenuScript, ID: scriptIDs["Setup"], FolderID: "f1"},
		{Catalog: schema.CatalogScripts, Kind: schema.MenuSeparator, ID: "s-sep", Name: "-"},
		{Catalog: schema.CatalogScripts, Kind: schema.MenuScript, ID: scriptIDs["Run"]},
	}
	withMenus, err := msgpack.Marshal(bundle)
	require.NoError(t, err)

	dst := newExternalSolution(t, ctx, mgr, "menus_dst")
	_, err = dst.svc.ImportSolution(ctx, withMenus)
	require.NoError(t, err)
	wantLayouts := []string{"Zeta", "-", "Work/", "Work/Deeper/", "Work/Deeper/Inner", "Work/Alpha"}
	wantScripts := []string{"Tools/", "Tools/Setup", "-", "Run"}
	check := func(t *testing.T, svc *schema.Service, layouts, scripts []string) {
		t.Helper()
		menu, err := svc.Menu(ctx, schema.CatalogLayouts)
		require.NoError(t, err)
		assert.Equal(t, layouts, menuPaths(menu, ""))
		menu, err = svc.Menu(ctx, schema.CatalogScripts)
		require.NoError(t, err)
		assert.Equal(t, scripts, menuPaths(menu, ""))
	}
	check(t, dst.svc, wantLayouts, wantScripts)
	menu, err = dst.svc.Menu(ctx, schema.CatalogLayouts)
	require.NoError(t, err)
	dstOcc := dst.occurrenceOf(t, ctx, mustTable(t, dst, "items"))
	assert.Equal(t, dstOcc, menu[1].TableOccurrenceID, "the separator keeps its occurrence")

	// The first layout of the menu's order is the one a name finds, in any
	// case; a folder is not a layout.
	found, err := dst.svc.LayoutByName(ctx, "inner")
	require.NoError(t, err)
	assert.Equal(t, "Inner", found.Name)
	_, err = dst.svc.LayoutByName(ctx, "Work")
	assert.ErrorIs(t, err, schema.ErrLayoutNotFound)

	t.Run("an export carries the menus again", func(t *testing.T) {
		again, err := dst.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Menus"})
		require.NoError(t, err)
		other := newExternalSolution(t, ctx, mgr, "menus_again")
		_, err = other.svc.ImportSolution(ctx, again)
		require.NoError(t, err)
		check(t, other.svc, wantLayouts, wantScripts)
	})

	t.Run("a file without menus leaves them as they are", func(t *testing.T) {
		_, err := dst.svc.ImportSolution(ctx, file)
		require.NoError(t, err)
		check(t, dst.svc, wantLayouts, wantScripts)
	})

	t.Run("a layout created here comes last", func(t *testing.T) {
		_, err = dst.svc.CreateLayout(ctx, "Aardvark", dstOcc, []byte(`{"objects":[]}`))
		require.NoError(t, err)
		check(t, dst.svc, append(append([]string{}, wantLayouts...), "Aardvark"), wantScripts)
	})

	t.Run("a design put back has its menus as they were", func(t *testing.T) {
		plain := newExternalSolution(t, ctx, mgr, "menus_restore")
		_, err := plain.svc.ImportSolution(ctx, file)
		require.NoError(t, err)
		snap, err := plain.svc.SnapshotDesign(ctx, "Menus")
		require.NoError(t, err)
		_, err = plain.svc.ImportSolution(ctx, withMenus)
		require.NoError(t, err)
		check(t, plain.svc, wantLayouts, wantScripts)
		require.NoError(t, plain.svc.RestoreDesign(ctx, snap))
		check(t, plain.svc, []string{"Alpha", "Inner", "Zeta"}, []string{"Run", "Setup"})
	})

	t.Run("a menu naming what the file does not have is refused", func(t *testing.T) {
		bad := *bundle
		bad.Catalog = append([]schema.BundleCatalogEntry{}, bundle.Catalog...)
		bad.Catalog[4].FolderID = "nowhere"
		raw, err := msgpack.Marshal(bad)
		require.NoError(t, err)
		_, err = newExternalSolution(t, ctx, mgr, "menus_bad").svc.ImportSolution(ctx, raw)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown folder")
	})
}

func mustTable(t *testing.T, s *externalSolution, name string) string {
	t.Helper()
	tables, err := s.svc.ListTables(t.Context())
	require.NoError(t, err)
	for _, tb := range tables {
		if strings.EqualFold(tb.Name, name) {
			return tb.ID
		}
	}
	t.Fatalf("no table %s", name)
	return ""
}

// A field's source type is kept, travels in a solution file and says what
// File4Base's own type cannot; one that does not agree with the field's type
// is not taken.
func TestSourceTypeOfAField(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	src := newExternalSolution(t, ctx, mgr, "srctype_src")
	tbl, err := src.svc.CreateTable(ctx, "slots", "slots")
	require.NoError(t, err)
	_, err = src.svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "starts", DisplayName: "Starts", FieldType: dbal.FieldTypeTimestamp, IsNullable: true, SourceType: schema.SourceTypeTime,
	})
	require.NoError(t, err)
	_, err = src.svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "total", DisplayName: "Total", FieldType: dbal.FieldTypeNumber, IsNullable: true, SourceType: "summary/number",
	})
	require.NoError(t, err)
	_, err = src.svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "odd", DisplayName: "Odd", FieldType: dbal.FieldTypeText, IsNullable: true, SourceType: "normal/whatever",
	})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)

	file, err := src.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Slots"})
	require.NoError(t, err)
	dst := newExternalSolution(t, ctx, mgr, "srctype_dst")
	_, err = dst.svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	tables, err := dst.svc.ListTables(ctx)
	require.NoError(t, err)
	kinds := map[string]string{}
	for _, tb := range tables {
		for _, c := range tb.Columns {
			k, r := c.FieldKind()
			kinds[c.Name] = c.SourceType + " " + k + "/" + r
		}
	}
	assert.Equal(t, schema.SourceTypeTime+" normal/time", kinds["starts"])
	assert.Equal(t, "summary/number summary/number", kinds["total"])
}

// FieldKind takes a source type only where it agrees with File4Base's type.
func TestFieldKindTakesAnAgreeingSourceTypeOnly(t *testing.T) {
	calc := func(result string) *string { s := `{"formula":"1","result_type":"` + result + `"}`; return &s }
	for _, tc := range []struct {
		col        schema.ColumnMetadata
		kind, want string
	}{
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeTimestamp}, "normal", "timeStamp"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeTimestamp, SourceType: schema.SourceTypeTime}, "normal", "time"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeDate, SourceType: schema.SourceTypeTime}, "normal", "date"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeCalculation, CalculationFormula: calc("Timestamp"), SourceType: schema.SourceTypeCalculationTime}, "calculation", "time"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeCalculation, CalculationFormula: calc("Container")}, "calculation", "container"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeTimestamp, SourceType: "summary/time"}, "summary", "time"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeText, SourceType: "summary/number"}, "normal", "text"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeBoolean}, "normal", "number"},
		{schema.ColumnMetadata{FieldType: dbal.FieldTypeContainer}, "normal", "container"},
	} {
		k, r := tc.col.FieldKind()
		assert.Equal(t, tc.kind+"/"+tc.want, k+"/"+r, "%+v", tc.col)
	}
}

// A layout's contract places a field in the portal its object names; a
// layout saved before objects named their portal places it by its bounds.
func TestCompileLayoutContract(t *testing.T) {
	def := `{"objects":[
		{"id":"p1","type":"portal","name":"Lines","x":0,"y":100,"width":300,"height":100,
		 "portal_config":{"occurrence":"Lines","sort":"-qty,code"}},
		{"id":"f1","type":"field","x":10,"y":110,"width":50,"height":20,"portal":"p1",
		 "field_binding":{"field_name":"qty","table_occurrence":"Lines","control_style":"edit_box"}},
		{"id":"f2","type":"field","x":10,"y":10,"width":50,"height":20,
		 "field_binding":{"field_name":"name","control_style":"pop_up_menu","value_list_id":"vl","first_repetition":2,"repetitions_shown":3}},
		{"id":"f3","type":"field","x":10,"y":150,"width":50,"height":20,
		 "field_binding":{"field_name":"gone","missing":true}},
		{"id":"f4","type":"field","x":20,"y":150,"width":50,"height":20,"portal":"",
		 "field_binding":{"field_name":"note","table_occurrence":"Lines"}},
		{"id":"f5","type":"field","x":80,"y":150,"width":50,"height":20,
		 "field_binding":{"field_name":"price","table_occurrence":"Lines"}},
		{"id":"f6","type":"field","x":150,"y":150,"width":50,"height":20,"panel":{"control":"tabs","index":1},
		 "field_binding":{"field_name":"memo"}},
		{"id":"f7","type":"field","x":250,"y":190,"width":80,"height":20,
		 "field_binding":{"field_name":"total"}}
	]}`
	c, err := schema.CompileLayoutContract([]byte(def))
	require.NoError(t, err)
	require.Len(t, c.Portals, 1)
	p := c.Portals[0]
	assert.Equal(t, "Lines", p.Name)
	assert.Equal(t, []schema.ContractSort{
		{Occurrence: "Lines", Field: "qty", Order: "descending"},
		{Occurrence: "Lines", Field: "code", Order: "ascending"},
	}, p.Sort)
	fields := func(list []schema.ContractField) []string {
		out := []string{}
		for _, f := range list {
			out = append(out, f.Field)
		}
		return out
	}
	// f1 names the portal; f5 says nothing and lies wholly inside it on
	// its panel; f4 says it is in none; f6 is on a panel of another
	// control; f7 overlaps the portal's edge; f3's field is missing.
	assert.Equal(t, []string{"qty", "price"}, fields(p.Fields))
	assert.Equal(t, []string{"name", "note", "memo", "total"}, fields(c.Fields))
	assert.Equal(t, schema.ContractField{
		Field: "name", ControlStyle: "pop_up_menu", ValueListID: "vl", RepetitionStart: 2, RepetitionsShown: 3,
	}, c.Fields[0])
}

// Layouts sharing a name, as FileMaker allows, stay two when a file is
// imported again, each updated from its own.
func TestLayoutsSharingANameStayApartOnImport(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	src := newExternalSolution(t, ctx, mgr, "dup_src")
	tbl, _ := src.table(t, ctx, "items", "code")
	occ := src.occurrenceOf(t, ctx, tbl.ID)
	for _, width := range []string{"101", "202"} {
		_, err := src.svc.CreateLayout(ctx, "Twin", occ, []byte(`{"objects":[],"width":`+width+`}`))
		require.NoError(t, err)
	}
	file, err := src.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Twins"})
	require.NoError(t, err)
	dst := newExternalSolution(t, ctx, mgr, "dup_dst")
	for i := 0; i < 2; i++ {
		_, err = dst.svc.ImportSolution(ctx, file)
		require.NoError(t, err)
	}
	layouts, err := dst.svc.ListLayouts(ctx)
	require.NoError(t, err)
	widths := []string{}
	for _, l := range layouts {
		var d struct {
			Width json.Number `json:"width"`
		}
		require.NoError(t, json.Unmarshal(l.Definition, &d))
		widths = append(widths, d.Width.String())
	}
	sort.Strings(widths)
	assert.Equal(t, []string{"101", "202"}, widths)
}

// A solution file never widens what an existing account may do: on each
// layout and table it names, the account keeps the stricter of its own
// access and the file's. An import that fails gives every account back
// exactly the permissions it had, "no rows" included.
func TestImportNeverWidensAnExistingAccount(t *testing.T) {
	mgr, ctx := externalTestManager(t)
	if mgr == nil {
		return
	}
	src := newExternalSolution(t, ctx, mgr, "acc_src")
	tbl, _ := src.table(t, ctx, "items", "code")
	occ := src.occurrenceOf(t, ctx, tbl.ID)
	fileIDs := map[string]string{}
	for _, name := range []string{"Main", "Other"} {
		l, err := src.svc.CreateLayout(ctx, name, occ, []byte(`{"objects":[]}`))
		require.NoError(t, err)
		fileIDs[name] = l.ID
	}
	plain, err := src.svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Accounts"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(plain)
	require.NoError(t, err)
	bundle.Users = []schema.BundleAccount{
		{Username: "dana", Role: "user",
			Permissions: []schema.BundlePermission{
				{LayoutID: fileIDs["Main"], AccessLevel: schema.AccessReadWrite},
				{LayoutID: fileIDs["Other"], AccessLevel: schema.AccessReadOnly},
			},
			TablePermissions: []schema.BundleTablePermission{
				{TableName: "items", AccessLevel: schema.AccessReadWrite, CanCreate: true, CanDelete: true},
			}},
		{Username: "eve", Role: "user",
			Permissions: []schema.BundlePermission{{LayoutID: fileIDs["Main"], AccessLevel: schema.AccessReadOnly}}},
	}
	withAccounts, err := msgpack.Marshal(bundle)
	require.NoError(t, err)

	dst := newExternalSolution(t, ctx, mgr, "acc_dst")
	_, err = dst.svc.ImportSolution(ctx, plain)
	require.NoError(t, err)
	dstIDs := map[string]string{}
	layouts, err := dst.svc.ListLayouts(ctx)
	require.NoError(t, err)
	for _, l := range layouts {
		dstIDs[l.ID] = l.Name
	}
	users := map[string]string{}
	for _, name := range []string{"dana", "eve"} {
		u, err := dst.svc.CreateUser(ctx, name, "Secret-pass-1", "user")
		require.NoError(t, err)
		users[name] = u.ID
	}
	reset := func() {
		t.Helper()
		var mainID string
		for id, name := range dstIDs {
			if name == "Main" {
				mainID = id
			}
		}
		require.NoError(t, dst.svc.SetUserPermissions(ctx, users["dana"], []schema.UserLayoutPermission{{LayoutID: mainID, AccessLevel: schema.AccessNone}}))
		require.NoError(t, dst.svc.SetUserTablePermissions(ctx, users["dana"], []schema.UserTablePermission{
			{TableName: "items", AccessLevel: schema.AccessReadWrite, CanCreate: true, CanEdit: true, CanDelete: true},
		}))
		require.NoError(t, dst.svc.SetUserPermissions(ctx, users["eve"], nil))
	}
	layoutPerms := func(user string) map[string]string {
		t.Helper()
		perms, err := dst.svc.GetUserPermissions(ctx, users[user])
		require.NoError(t, err)
		out := map[string]string{}
		for _, p := range perms {
			out[dstIDs[p.LayoutID]] = p.AccessLevel
		}
		return out
	}
	tablePerms := func(user string) []schema.UserTablePermission {
		t.Helper()
		perms, err := dst.svc.GetUserTablePermissions(ctx, users[user])
		require.NoError(t, err)
		for i := range perms {
			perms[i].ID, perms[i].UserID = "", ""
		}
		return perms
	}

	reset()
	_, err = dst.svc.ImportSolution(ctx, withAccounts)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Main": schema.AccessNone, "Other": schema.AccessReadOnly}, layoutPerms("dana"),
		"a layout the account may not see stays hidden; one the file restricts is restricted")
	assert.Equal(t, []schema.UserTablePermission{
		{TableName: "items", AccessLevel: schema.AccessReadWrite, CanCreate: true, CanDelete: true},
	}, tablePerms("dana"), "a record privilege the file withholds is withdrawn")
	assert.Equal(t, map[string]string{"Main": schema.AccessReadOnly}, layoutPerms("eve"))
	assert.Empty(t, tablePerms("eve"))

	// The import fails after the accounts (the saved finds can no longer
	// be read): the accounts are back as they were.
	reset()
	_, err = dst.driver.DB().ExecContext(ctx, `ALTER TABLE sys_saved_finds RENAME TO sys_saved_finds_off`)
	require.NoError(t, err)
	bundle.SavedFinds = []schema.BundleSavedFind{{Name: "Recent", TableName: "items"}}
	failing, err := msgpack.Marshal(bundle)
	require.NoError(t, err)
	_, err = dst.svc.ImportSolution(ctx, failing)
	require.Error(t, err)
	assert.Equal(t, map[string]string{"Main": schema.AccessNone}, layoutPerms("dana"))
	assert.Equal(t, []schema.UserTablePermission{
		{TableName: "items", AccessLevel: schema.AccessReadWrite, CanCreate: true, CanEdit: true, CanDelete: true},
	}, tablePerms("dana"))
	assert.Empty(t, layoutPerms("eve"), "an account that had no rows has none again")

	// Undoing a migration puts the accounts' permissions back as the
	// snapshot found them, although importing its file only restricts.
	// A failed import may have created the table again (MariaDB).
	_, err = dst.driver.DB().ExecContext(ctx, `DROP TABLE IF EXISTS sys_saved_finds`)
	require.NoError(t, err)
	_, err = dst.driver.DB().ExecContext(ctx, `ALTER TABLE sys_saved_finds_off RENAME TO sys_saved_finds`)
	require.NoError(t, err)
	reset()
	snap, err := dst.svc.SnapshotDesign(ctx, "Accounts")
	require.NoError(t, err)
	_, err = dst.svc.ImportSolution(ctx, withAccounts)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"Main": schema.AccessReadOnly}, layoutPerms("eve"))
	require.NoError(t, dst.svc.RestoreDesign(ctx, snap))
	assert.Equal(t, map[string]string{"Main": schema.AccessNone}, layoutPerms("dana"))
	assert.Equal(t, []schema.UserTablePermission{
		{TableName: "items", AccessLevel: schema.AccessReadWrite, CanCreate: true, CanEdit: true, CanDelete: true},
	}, tablePerms("dana"))
	assert.Empty(t, layoutPerms("eve"))
}
