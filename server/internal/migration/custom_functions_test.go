package migration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// customFunctionsDesign is a synthetic French Database Design Report whose
// calculations call its custom functions: one calls a function defined after
// it, one calls itself, two call each other, one is a constant written with a
// decimal comma, one reads a field, one cannot be read, which another
// calls, and one is named like a built-in function, which another calls. A
// field whose imported name is a custom function's is read beside it.
// tableTag is appended to the table's name and tag to every function's,
// so the design can be applied to a shared database.
func customFunctionsDesign(tableTag, tag string) string {
	r := strings.NewReplacer("Articles", "Articles"+tableTag, "PrixTTC", "PrixTTC"+tag, "TauxTVA", "TauxTVA"+tag,
		"Factorielle", "Factorielle"+tag, "EstPair", "EstPair"+tag, "EstImpair", "EstImpair"+tag,
		"PréfixeCatégorie", "PréfixeCatégorie"+tag, "AppelleCassé", "AppelleCassé"+tag, "Cassé", "Cassé"+tag,
		"PrixCatalogue", "PrixCatalogue"+tag, "Remise", "Remise"+tag, "Nettoie", "Nettoie"+tag)
	function := func(name, params, visible, formula string) string {
		return `<CustomFunction id="1" functionArity="1" visible="` + visible + `" parameters="` + params + `" name="` + name + `">` +
			`<Calculation><![CDATA[` + formula + `]]></Calculation></CustomFunction>`
	}
	calcField := func(name, dataType, formula string) string {
		return `<Field name="` + name + `" fieldType="Calculation" dataType="` + dataType + `"><Storage storeCalculationResults="True"/>` +
			`<Calculation table="Articles"><![CDATA[` + formula + `]]></Calculation></Field>`
	}
	return r.Replace(`<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Articles"><FieldCatalog>` +
		`<Field name="Prix HT" dataType="Number"/><Field name="Quantité" dataType="Number"/><Field name="Catégorie" dataType="Text"/>` +
		`<Field name="_Remise" dataType="Number"/>` +
		calcField("Prix TTC", "Number", `PrixTTC ( Prix HT )`) +
		calcField("Fact", "Number", `Factorielle ( Quantité )`) +
		calcField("Parité", "Number", `EstPair ( Quantité )`) +
		calcField("Code", "Text", `PréfixeCatégorie ( Catégorie )`) +
		calcField("Double", "Number", `AppelleCassé ( Quantité )`) +
		calcField("Requête", "Text", `ExecuteSQL ( "SELECT 1" ; "" ; "" )`) +
		calcField("Net", "Number", `Prix HT * ( 1 - _Remise ) + Remise`) +
		calcField("Nettoyé", "Text", `Trim ( Catégorie )`) +
		`</FieldCatalog></BaseTable></BaseTableCatalog>` +
		`<TableOccurrenceCatalog><TableOccurrence name="Articles" baseTable="Articles"/></TableOccurrenceCatalog>` +
		`<CustomFunctionCatalog>` +
		function("PrixTTC", "montant", "True", `Round ( montant * ( 1 + TauxTVA ) ; 2 )`) +
		function("Factorielle", "n", "True", `If ( n ≤ 1 ; 1 ; n * Factorielle ( n - 1 ) )`) +
		function("EstPair", "n", "True", `If ( n = 0 ; 1 ; EstImpair ( n - 1 ) )`) +
		function("EstImpair", "n", "True", `If ( n = 0 ; 0 ; EstPair ( n - 1 ) )`) +
		function("PréfixeCatégorie", "libellé", "True", `"CAT-" & Upper ( Substitute ( libellé ; ¶ ; " " ) )`) +
		function("TauxTVA", "", "False", `0,2`) +
		function("AppelleCassé", "x", "True", `Cassé ( x ) * 2`) +
		function("Cassé", "x", "True", `x + + )`) +
		function("PrixCatalogue", "", "True", `Articles::Prix HT * 1`) +
		function("Remise", "", "True", `0,1`) +
		function("Trim", "texte", "True", `texte`) +
		function("Nettoie", "texte", "True", `Trim ( texte )`) +
		`</CustomFunctionCatalog></File></FMPReport>`)
}

func reportItems(r *ConversionReport) []ReportItem {
	var all []ReportItem
	for _, list := range [][]ReportItem{r.Preserved, r.Translated, r.Unsupported, r.Rejected, r.ManualActionRequired} {
		all = append(all, list...)
	}
	return all
}

func itemsFor(r *ConversionReport, domain, source string) []ReportItem {
	var out []ReportItem
	for _, it := range reportItems(r) {
		if it.Domain == domain && strings.EqualFold(it.SourceName, source) {
			out = append(out, it)
		}
	}
	return out
}

func columnByDisplayName(t *testing.T, table schema.BundleTable, name string) schema.BundleColumn {
	t.Helper()
	for _, c := range table.Columns {
		if c.DisplayName == name {
			return c
		}
	}
	t.Fatalf("no column %q in %s", name, table.Name)
	return schema.BundleColumn{}
}

// Custom functions are imported, translated like calculations, before the
// calculations that call them, which therefore import as calculations; one
// that cannot be read is reported with the one that calls it, and the
// calculations calling either say so; one running ExecuteSQL is unstored.
func TestCustomFunctionsMigrate(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: customFunctionsDesign("", ""), SourceLocale: "fr-FR"})
	require.NoError(t, err)
	report := res.Report

	byName := map[string]schema.BundleCustomFunction{}
	for _, fn := range res.Bundle.CustomFunctions {
		byName[fn.Name] = fn
	}
	require.Len(t, byName, 8, "every function but the broken one, the one named like a built-in, and their callers")
	require.Equal(t, 8, report.Counts.CustomFunctions)
	require.Equal(t, []string{"montant"}, byName["PrixTTC"].Parameters)
	require.Equal(t, "0.2", byName["TauxTVA"].Formula, "a decimal comma is written as File4Base reads it")
	require.Equal(t, schema.CustomFunctionAccessFullAccess, byName["TauxTVA"].Access)
	require.Equal(t, schema.CustomFunctionAccessAll, byName["PrixTTC"].Access)
	require.Equal(t, "Articles::prix_ht * 1", byName["PrixCatalogue"].Formula, "a field it reads is bound to the imported column")
	require.Equal(t, `"CAT-" & Upper ( Substitute ( libellé ; ¶ ; " " ) )`, byName["PréfixeCatégorie"].Formula)

	for _, name := range []string{"PrixTTC", "Factorielle", "EstPair", "EstImpair", "PréfixeCatégorie"} {
		items := itemsFor(report, "custom_function", name)
		require.Len(t, items, 1, name)
		require.Equal(t, CategoryPreserved, items[0].Category, name)
	}
	for _, name := range []string{"TauxTVA", "PrixCatalogue", "Remise"} {
		items := itemsFor(report, "custom_function", name)
		require.Len(t, items, 1, name)
		require.Equal(t, CategoryTranslated, items[0].Category, name)
	}
	broken := itemsFor(report, "custom_function", "Cassé")
	require.Len(t, broken, 2)
	require.Equal(t, CategoryUnsupported, broken[0].Category)
	require.Contains(t, broken[0].Detail, "could not be read")
	require.Equal(t, CategoryManualActionRequired, broken[1].Category)
	caller := itemsFor(report, "custom_function", "AppelleCassé")
	require.Len(t, caller, 2)
	require.Contains(t, caller[0].Detail, "calls the custom function Cassé, which cannot be read")
	builtin := itemsFor(report, "custom_function", "Trim")
	require.Len(t, builtin, 2)
	require.Equal(t, CategoryUnsupported, builtin[0].Category)
	require.Contains(t, builtin[0].Detail, "is named like the built-in function TRIM, which a custom function cannot replace")
	require.Equal(t, CategoryManualActionRequired, builtin[1].Category)
	require.Contains(t, builtin[1].Detail, "Rename the custom function Trim")
	require.Contains(t, itemsFor(report, "custom_function", "Nettoie")[0].Detail, "calls the custom function Trim, which cannot be read")

	table := res.Bundle.Tables[0]
	for _, name := range []string{"Prix TTC", "Fact", "Parité", "Code"} {
		require.Equal(t, "CALCULATION", columnByDisplayName(t, table, name).FieldType, name)
	}
	require.Contains(t, *columnByDisplayName(t, table, "Prix TTC").CalculationFormula, `PrixTTC ( prix_ht )`)
	// _Remise is imported as remise, which is quoted so as not to read as
	// the custom function Remise.
	require.Equal(t, "CALCULATION", columnByDisplayName(t, table, "Net").FieldType)
	require.Contains(t, *columnByDisplayName(t, table, "Net").CalculationFormula, `prix_ht * ( 1 - ${remise} ) + Remise`)
	for name, why := range map[string]string{
		"Double":  "calls the custom function AppelleCassé, which could not be migrated",
		"Nettoyé": "calls the custom function Trim, which could not be migrated",
	} {
		col := columnByDisplayName(t, table, name)
		require.NotEqual(t, "CALCULATION", col.FieldType, name)
		found := false
		for _, it := range itemsFor(report, "field", table.Name+"."+name) {
			found = found || it.Category == CategoryUnsupported && strings.Contains(it.Detail, why)
		}
		require.True(t, found, "%s: %q", name, why)
	}
	// ExecuteSQL is computed when shown, as the answer of a query changes
	// without the record changing.
	query := columnByDisplayName(t, table, "Requête")
	require.Equal(t, "CALCULATION", query.FieldType)
	require.Contains(t, *query.CalculationFormula, `"stored":false`)
	found := false
	for _, it := range itemsFor(report, "field", table.Name+".Requête") {
		found = found || strings.Contains(it.Detail, "runs a query with ExecuteSQL")
	}
	require.True(t, found, "Requête is reported as unstored")
	for _, it := range reportItems(report) {
		require.NotContains(t, it.Detail, "there is no function called", it.SourceName)
	}
}

// Save a Copy as XML lists a function's parameters as elements of its
// ObjectList and writes its formula in a <Text>.
func TestCustomFunctionsMigrateFromSaveAsXML(t *testing.T) {
	doc := `<FMSaveAsXML Has_DDR_INFO="True"><Structure><AddAction>` +
		`<BaseTableCatalog><BaseTable id="1" name="Orders"/></BaseTableCatalog>` +
		`<FieldsForTables><FieldCatalog><BaseTableSourceReference id="1" name="Orders"/>` +
		`<Field id="1" name="Amount" fieldtype="Normal" datatype="Number"/>` +
		`<Field id="2" name="Doubled" fieldtype="Calculated" datatype="Number"><Calculation><Text><![CDATA[Twice ( Amount )]]></Text></Calculation></Field>` +
		`</FieldCatalog></FieldsForTables>` +
		`<CustomFunctionsCatalog membercount="2"><ObjectList membercount="2">` +
		`<CustomFunction id="1" name="Twice" access="All"><Calculation><Text><![CDATA[Times ( value ; 2 )]]></Text></Calculation>` +
		`<ObjectList membercount="1"><Parameter name="value"></Parameter></ObjectList></CustomFunction>` +
		`<CustomFunction id="2" name="Times" access="FullAccessOnly"><Calculation><Text><![CDATA[a * b]]></Text></Calculation>` +
		`<ObjectList membercount="2"><Parameter name="a"></Parameter><Parameter name="b"></Parameter></ObjectList></CustomFunction>` +
		`</ObjectList></CustomFunctionsCatalog>` +
		`</AddAction></Structure></FMSaveAsXML>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	require.NoError(t, err)
	require.Equal(t, []schema.BundleCustomFunction{
		{Name: "Twice", Parameters: []string{"value"}, Formula: "Times ( value ; 2 )", Access: schema.CustomFunctionAccessAll},
		{Name: "Times", Parameters: []string{"a", "b"}, Formula: "a * b", Access: schema.CustomFunctionAccessFullAccess},
	}, res.Bundle.CustomFunctions)
	require.Equal(t, "CALCULATION", columnByDisplayName(t, res.Bundle.Tables[0], "Doubled").FieldType)
}

// An HTML Database Design Report lists the custom functions in a section of
// their own, with their parameters, availability and formula.
func TestCustomFunctionsMigrateFromHTMLDDR(t *testing.T) {
	raw, err := os.ReadFile("testdata/recipes_html_ddr.html")
	require.NoError(t, err)
	section := `<p><span class="partheading"><a name="valCustomFunctionsSectionAnchor_"/>Custom Functions</span></p>` +
		`<table><tbody><tr class="chartheaders"><td>Function Name</td><td>Parameters</td><td>Availability</td><td>Definition</td><td>In fields</td></tr>` +
		`<tr class="regularcharttext" vAlign="top"><td><a name="1CustomFunctionAnchor_"/>Halve</td><td>x</td><td>All accounts</td>` +
		"<td>// half of x\n<strong>Round</strong> ( <a href=\"#2CustomFunctionAnchor_\">Ratio</a> ( x ; 2 ) ; 2 )</td><td><ul></ul></td></tr>" +
		`<tr class="regularcharttext" vAlign="top"><td><a name="2CustomFunctionAnchor_"/>Ratio</td><td>a;b</td><td>Only accounts assigned full access privileges</td>` +
		`<td>a / b &amp; &quot;&quot;</td><td><ul></ul></td></tr>` +
		`<tr class="regularcharttext" vAlign="top"><td><a name="3CustomFunctionAnchor_"/>Taux</td><td></td><td>All accounts</td>` +
		`<td>0,5</td><td><ul></ul></td></tr>` +
		`<tr class="regularcharttext" vAlign="top"><td><a name="4CustomFunctionAnchor_"/>NomRecette</td><td></td><td>All accounts</td>` +
		`<td><strong>Upper</strong> ( recipes::Name )</td><td><ul></ul></td></tr></tbody></table>`
	doc := strings.Replace(string(raw), `<span class="partheading"><a name="valBaseDirectoriesSectionAnchor_"/>`,
		section+`<span class="partheading"><a name="valBaseDirectoriesSectionAnchor_"/>`, 1)
	require.NotEqual(t, string(raw), doc)

	// Read with the source's locale, and against the table occurrences,
	// which the report lists after the functions.
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc, SourceLocale: "fr-FR"})
	require.NoError(t, err)
	require.Equal(t, []schema.BundleCustomFunction{
		{Name: "Halve", Parameters: []string{"x"}, Formula: "// half of x\nRound ( Ratio ( x ; 2 ) ; 2 )", Access: schema.CustomFunctionAccessAll},
		{Name: "Ratio", Parameters: []string{"a", "b"}, Formula: `a / b & ""`, Access: schema.CustomFunctionAccessFullAccess},
		{Name: "Taux", Formula: "0.5", Access: schema.CustomFunctionAccessAll},
		{Name: "NomRecette", Formula: "Upper ( recipes::name )", Access: schema.CustomFunctionAccessAll},
	}, res.Bundle.CustomFunctions)
	require.Len(t, itemsFor(res.Report, "custom_function", "Halve"), 1)
}

// A migrated design applies with its custom functions, its calculations
// compute through them, and an exported solution file carries them.
func TestCustomFunctionsApplyAndCompute(t *testing.T) {
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
	tag := fmt.Sprintf("_%d", stamp)
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: customFunctionsDesign(fmt.Sprintf(" %d", stamp), tag), SourceLocale: "fr-FR"})
	require.NoError(t, err)
	for i := range res.Bundle.TableOccurrences {
		res.Bundle.TableOccurrences[i].XPos = float64(200_000 + stamp%1_000_000)
	}
	file, err := msgpack.Marshal(res.Bundle)
	require.NoError(t, err)

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	t.Cleanup(func() {
		d := driver.Dialect()
		_, _ = driver.DB().ExecContext(context.Background(),
			`DELETE FROM `+data.CustomFunctionsTable+` WHERE name LIKE `+d.Placeholder(1), "%"+tag)
		data.InvalidateLibrary(driver)
	})
	imported, err := schemaSvc.ImportSolution(ctx, file)
	require.NoError(t, err)
	require.Equal(t, 8, imported.CustomFunctionsCreated)
	table := res.Bundle.Tables[0]
	tables, err := schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	for _, created := range tables {
		if created.Name == table.Name {
			id := created.ID
			t.Cleanup(func() { _ = schemaSvc.DeleteTable(context.Background(), id) })
		}
	}

	col := func(name string) string { return columnByDisplayName(t, table, name).Name }
	row, err := data.NewService(driver).InsertRow(ctx, table.Name, map[string]interface{}{
		col("Prix HT"): 100, col("Quantité"): 5, col("Catégorie"): "outils\rmain", col("_Remise" + tag): 0.5,
	})
	require.NoError(t, err)
	require.Equal(t, "120", fmt.Sprint(row[col("Prix TTC")]), "through a function defined after the one called")
	require.Equal(t, "120", fmt.Sprint(row[col("Fact")]), "a recursive function")
	require.Equal(t, "0", fmt.Sprint(row[col("Parité")]), "two functions calling each other")
	require.Equal(t, "CAT-OUTILS MAIN", fmt.Sprint(row[col("Code")]))
	require.Contains(t, *columnByDisplayName(t, table, "Net").CalculationFormula, "( 1 - ${", "the field is quoted")
	require.Equal(t, "50.1", fmt.Sprint(row[col("Net")]), "the field _Remise, imported as remise, beside the function Remise")

	// The solution file carries the functions as they were imported.
	exported, err := schemaSvc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "custom functions"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(exported)
	require.NoError(t, err)
	carried := map[string]schema.BundleCustomFunction{}
	for _, fn := range bundle.CustomFunctions {
		carried[fn.Name] = fn
	}
	for _, fn := range res.Bundle.CustomFunctions {
		require.Equal(t, fn, carried[fn.Name])
	}
}
