package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// autoFixture is a synthetic solution: an "orders" table whose fields carry
// auto-enter options, and a "products" table they look values up from,
// related by orders::code = products::code.
type autoFixture struct {
	schemaSvc   *schema.Service
	dataSvc     *data.Service
	driver      dbal.DatabaseDriver
	orders      string
	products    string
	ordersID    string
	productsOcc string // the products occurrence name
	columns     map[string]string
	// productsID, relationship and codes (each table's code column, by
	// table name) let a test add a criterion to the relationship.
	productsID   string
	relationship string
	codes        map[string]string
}

type autoColumn struct {
	name      string
	fieldType dbal.AgnosticFieldType
	options   map[string]any
	formula   string // a calculation field's formula
}

func options(o map[string]any) *string {
	if o == nil {
		return nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		panic(err)
	}
	s := string(b)
	return &s
}

func newAutoFixture(t *testing.T, ctx context.Context, driver dbal.DatabaseDriver, columns ...autoColumn) *autoFixture {
	t.Helper()
	f := &autoFixture{schemaSvc: schema.NewService(driver), driver: driver, columns: map[string]string{}}
	f.dataSvc = data.NewService(driver).WithAccount("synthetic.clerk")
	require.NoError(t, f.schemaSvc.EnsureSystemTables(ctx))
	stamp := uniqueTestStamp()
	f.orders, f.products = fmt.Sprintf("ae_orders_%d", stamp), fmt.Sprintf("ae_products_%d", stamp)

	ids := map[string]string{}
	keys := map[string]string{}
	for _, name := range []string{f.products, f.orders} {
		tbl, err := f.schemaSvc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		id := tbl.ID
		t.Cleanup(func() { _ = f.schemaSvc.DeleteTable(context.Background(), id) })
		ids[name] = id
		col, err := f.schemaSvc.AddColumn(ctx, id, schema.ColumnMetadata{Name: "code", DisplayName: "code", FieldType: dbal.FieldTypeText, IsNullable: true})
		require.NoError(t, err)
		keys[name] = col.ID
	}
	f.ordersID, f.productsID, f.codes = ids[f.orders], ids[f.products], keys
	for _, c := range []autoColumn{
		{name: "price", fieldType: dbal.FieldTypeNumber},
		{name: "label", fieldType: dbal.FieldTypeText},
	} {
		_, err := f.schemaSvc.AddColumn(ctx, ids[f.products], schema.ColumnMetadata{Name: c.name, DisplayName: c.name, FieldType: c.fieldType, IsNullable: true})
		require.NoError(t, err)
	}

	occurrences, err := f.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	occ := map[string]string{}
	for _, o := range occurrences {
		switch o.BaseTableID {
		case ids[f.orders]:
			occ[f.orders] = o.ID
		case ids[f.products]:
			occ[f.products], f.productsOcc = o.ID, o.Name
		}
	}
	rel, err := f.schemaSvc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:             fmt.Sprintf("ae_rel_%d", stamp),
		LeftOccurrenceID: occ[f.orders], LeftColumnID: keys[f.orders],
		RightOccurrenceID: occ[f.products], RightColumnID: keys[f.products],
		Operator: "=",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.schemaSvc.DeleteRelationship(context.Background(), rel.ID) })
	f.relationship = rel.ID

	for _, c := range columns {
		meta := schema.ColumnMetadata{Name: c.name, DisplayName: c.name, FieldType: c.fieldType, IsNullable: true, DefaultValue: options(c.options)}
		if c.formula != "" {
			formula := fmt.Sprintf(`{"formula":%q,"result_type":"Text"}`, c.formula)
			meta.CalculationFormula = &formula
		}
		col, err := f.schemaSvc.AddColumn(ctx, f.ordersID, meta)
		require.NoError(t, err, c.name)
		f.columns[c.name] = col.ID
	}
	return f
}

func (f *autoFixture) setOptions(t *testing.T, ctx context.Context, field string, o map[string]any) {
	t.Helper()
	_, err := f.schemaSvc.UpdateColumn(ctx, f.ordersID, f.columns[field], schema.UpdateColumnOptions{
		DisplayName: field, DefaultValue: options(o), UpdateDefaultValue: true,
	})
	require.NoError(t, err, field)
}

func (f *autoFixture) product(t *testing.T, ctx context.Context, code string, price any, label any) {
	t.Helper()
	_, err := f.dataSvc.InsertRow(ctx, f.products, map[string]interface{}{"code": code, "price": price, "label": label})
	require.NoError(t, err)
}

func (f *autoFixture) insert(t *testing.T, ctx context.Context, record map[string]interface{}) map[string]interface{} {
	t.Helper()
	row, err := f.dataSvc.InsertRow(ctx, f.orders, record)
	require.NoError(t, err)
	return row
}

func (f *autoFixture) update(t *testing.T, ctx context.Context, id any, record map[string]interface{}) map[string]interface{} {
	t.Helper()
	row, err := f.dataSvc.UpdateRow(ctx, f.orders, fmt.Sprint(id), record)
	require.NoError(t, err)
	return row
}

func (f *autoFixture) get(t *testing.T, ctx context.Context, id any) map[string]interface{} {
	t.Helper()
	row, err := f.dataSvc.GetRow(ctx, f.orders, fmt.Sprint(id))
	require.NoError(t, err)
	return row
}

func text(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// moment is a timestamp as UTC text: a timestamp column may read back in
// the session's zone.
func moment(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format(time.RFC3339)
	}
	return text(v)
}

func number(v any) string {
	s := text(v)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func fixClock(t *testing.T, at time.Time) {
	t.Helper()
	calc.Now = func() time.Time { return at }
	t.Cleanup(func() { calc.Now = time.Now })
}

// Concurrent writers never get the same serial number, and the next value
// moves past every one they took.
func TestAutoEnter_SerialsAreAtomic(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "num", fieldType: dbal.FieldTypeNumber, options: map[string]any{"serial_enabled": true, "serial_next": 10, "serial_increment": 5}},
		autoColumn{name: "ref", fieldType: dbal.FieldTypeText, options: map[string]any{"serial_enabled": true, "serial_next": "INV0098"}},
	)

	const writers, each = 8, 5
	var mu sync.Mutex
	var nums, refs []string
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				row, err := f.dataSvc.InsertRow(ctx, f.orders, map[string]interface{}{"code": "X"})
				if err != nil {
					errs <- err
					return
				}
				mu.Lock()
				nums, refs = append(nums, number(row["num"])), append(refs, text(row["ref"]))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Len(t, nums, writers*each)
	seen := map[string]bool{}
	for _, n := range nums {
		require.False(t, seen[n], "serial %s given twice", n)
		seen[n] = true
	}
	for i := 0; i < writers*each; i++ {
		require.True(t, seen[fmt.Sprint(10+5*i)], "serial %d missing", 10+5*i)
	}
	sort.Strings(refs)
	require.Equal(t, "INV0098", refs[0])
	require.Equal(t, "INV0099", refs[1])
	require.Equal(t, "INV0100", refs[2])
	require.Len(t, map[string]bool{refs[0]: true, refs[len(refs)-1]: true}, 2)

	next, err := f.dataSvc.NextSerialValue(ctx, f.orders, "num")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(10+5*writers*each), next)

	t.Run("a value given wins and does not take a serial", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"num": 7})
		require.Equal(t, "7", number(row["num"]))
		after, err := f.dataSvc.NextSerialValue(ctx, f.orders, "num")
		require.NoError(t, err)
		require.Equal(t, next, after)
	})

	t.Run("Set Next Serial Value and GetNextSerialValue", func(t *testing.T) {
		set, err := f.dataSvc.SetNextSerialValue(ctx, f.orders, "ref", "Z-500")
		require.NoError(t, err)
		require.Equal(t, "Z-500", set)
		row := f.insert(t, ctx, map[string]interface{}{})
		require.Equal(t, "Z-500", text(row["ref"]))
		got, err := f.dataSvc.NextSerialValue(ctx, f.orders, "ref")
		require.NoError(t, err)
		require.Equal(t, "Z-501", got)

		_, err = f.dataSvc.SetNextSerialValue(ctx, f.orders, "num", "abc")
		require.Error(t, err)
		_, err = f.dataSvc.NextSerialValue(ctx, f.orders, "code")
		require.ErrorIs(t, err, data.ErrNotSerial)
	})

	t.Run("saving the options without serial_next keeps the live value", func(t *testing.T) {
		before, err := f.dataSvc.NextSerialValue(ctx, f.orders, "ref")
		require.NoError(t, err)
		f.setOptions(t, ctx, "ref", map[string]any{"serial_enabled": true, "serial_increment": 1})
		after, err := f.dataSvc.NextSerialValue(ctx, f.orders, "ref")
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
}

// Creation and modification stamps, and what counts as a modification.
func TestAutoEnter_Stamps(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	stamp := func(kind string, creation bool) map[string]any {
		if creation {
			return map[string]any{"creation_enabled": true, "creation_type": kind}
		}
		return map[string]any{"modification_enabled": true, "modification_type": kind}
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "note", fieldType: dbal.FieldTypeText},
		autoColumn{name: "shared", fieldType: dbal.FieldTypeText, options: map[string]any{"storage_global": true}},
		autoColumn{name: "created_on", fieldType: dbal.FieldTypeDate, options: stamp("Date", true)},
		autoColumn{name: "created_at", fieldType: dbal.FieldTypeTimestamp, options: stamp("Timestamp", true)},
		autoColumn{name: "created_time", fieldType: dbal.FieldTypeText, options: stamp("Time", true)},
		autoColumn{name: "created_clock", fieldType: dbal.FieldTypeTimestamp, options: stamp("Time", true)},
		autoColumn{name: "created_by", fieldType: dbal.FieldTypeText, options: stamp("Name", true)},
		autoColumn{name: "modified_at", fieldType: dbal.FieldTypeTimestamp, options: stamp("Timestamp", false)},
		autoColumn{name: "modified_by", fieldType: dbal.FieldTypeText, options: stamp("Account Name", false)},
		autoColumn{name: "shout", fieldType: dbal.FieldTypeCalculation, formula: "Upper ( note )"},
	)

	fixClock(t, time.Date(2026, 3, 4, 9, 15, 30, 0, time.UTC))
	row := f.insert(t, ctx, map[string]interface{}{"note": "first"})
	assert.Contains(t, text(row["created_on"]), "2026-03-04")
	assert.Contains(t, moment(row["created_at"]), "2026-03-04")
	assert.Contains(t, moment(row["created_at"]), "09:15:30")
	assert.Equal(t, "09:15:30", text(row["created_time"]))
	assert.Contains(t, moment(f.get(t, ctx, row["id"])["created_clock"]), "09:15:30", "a time in a timestamp field, as migrated Time fields are")
	assert.Equal(t, "synthetic.clerk", text(row["created_by"]))
	assert.Contains(t, moment(row["modified_at"]), "09:15:30")
	assert.Equal(t, "synthetic.clerk", text(row["modified_by"]))

	fixClock(t, time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC))
	t.Run("a global field change is not a modification", func(t *testing.T) {
		f.update(t, ctx, row["id"], map[string]interface{}{"shared": "everyone"})
		got := f.get(t, ctx, row["id"])
		assert.Contains(t, moment(got["modified_at"]), "2026-03-04")
	})
	t.Run("sending back unchanged values is not a modification", func(t *testing.T) {
		f.update(t, ctx, row["id"], map[string]interface{}{"note": "first"})
		got := f.get(t, ctx, row["id"])
		assert.Contains(t, moment(got["modified_at"]), "2026-03-04")
	})
	t.Run("a stored field change stamps, creation stamps stay", func(t *testing.T) {
		other := data.NewService(driver).WithAccount("synthetic.editor")
		_, err := other.UpdateRow(ctx, f.orders, fmt.Sprint(row["id"]), map[string]interface{}{"note": "second"})
		require.NoError(t, err)
		got := f.get(t, ctx, row["id"])
		assert.Contains(t, moment(got["modified_at"]), "2026-03-05")
		assert.Contains(t, moment(got["modified_at"]), "10:00:00")
		assert.Equal(t, "synthetic.editor", text(got["modified_by"]))
		assert.Contains(t, moment(got["created_at"]), "2026-03-04")
		assert.Equal(t, "synthetic.clerk", text(got["created_by"]))
	})
	t.Run("a stamp given on creation wins", func(t *testing.T) {
		got := f.insert(t, ctx, map[string]interface{}{"created_by": "given"})
		assert.Equal(t, "given", text(got["created_by"]))
	})
}

// Calculated values: replace or not, skip when empty, own-field formulas,
// related references and Get ( AccountName ).
func TestAutoEnter_CalculatedValue(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	calcOpt := func(formula string, extra map[string]any) map[string]any {
		o := map[string]any{"calculated_enabled": true, "calculated_formula": formula}
		for k, v := range extra {
			o[k] = v
		}
		return o
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "first", fieldType: dbal.FieldTypeText},
		autoColumn{name: "last", fieldType: dbal.FieldTypeText},
		autoColumn{name: "full", fieldType: dbal.FieldTypeText, options: calcOpt(`first & " " & last`, nil)},
		autoColumn{name: "initial_full", fieldType: dbal.FieldTypeText, options: calcOpt(`first & " " & last`, map[string]any{"do_not_replace": true})},
		autoColumn{name: "tag", fieldType: dbal.FieldTypeText, options: calcOpt(`"T-" & first`, map[string]any{"calculated_skip_if_empty": true})},
		autoColumn{name: "tag_always", fieldType: dbal.FieldTypeText, options: calcOpt(`"T-" & first`, map[string]any{"calculated_skip_if_empty": false})},
		autoColumn{name: "upper_self", fieldType: dbal.FieldTypeText, options: calcOpt(`Upper ( upper_self )`, nil)},
		autoColumn{name: "who", fieldType: dbal.FieldTypeText, options: calcOpt(`Get ( AccountName )`, nil)},
		autoColumn{name: "product_label", fieldType: dbal.FieldTypeText, options: map[string]any{}},
		autoColumn{name: "next_num", fieldType: dbal.FieldTypeText},
	)
	// Options that read the products occurrence need its name, known now.
	f.setOptions(t, ctx, "product_label", calcOpt(f.productsOcc+"::label", nil))
	f.product(t, ctx, "P1", 12, "Widget")
	f.product(t, ctx, "P2", 30, "Gadget")

	row := f.insert(t, ctx, map[string]interface{}{"first": "Ada", "last": "Lovelace", "upper_self": "mixed", "code": "P1"})
	assert.Equal(t, "Ada Lovelace", text(row["full"]))
	assert.Equal(t, "Ada Lovelace", text(row["initial_full"]))
	assert.Equal(t, "T-Ada", text(row["tag"]))
	assert.Equal(t, "MIXED", text(row["upper_self"]), "a formula reading its own field reworks the value typed")
	assert.Equal(t, "synthetic.clerk", text(row["who"]))
	assert.Equal(t, "Widget", text(row["product_label"]))

	t.Run("re-evaluated when a field it reads changes; do-not-replace keeps", func(t *testing.T) {
		got := f.update(t, ctx, row["id"], map[string]interface{}{"last": "Byron"})
		assert.Equal(t, "Ada Byron", text(got["full"]))
		assert.Equal(t, "Ada Lovelace", text(got["initial_full"]))
	})
	t.Run("a related reference re-evaluates when its match field changes", func(t *testing.T) {
		got := f.update(t, ctx, row["id"], map[string]interface{}{"code": "P2"})
		assert.Equal(t, "Gadget", text(got["product_label"]))
	})
	t.Run("a value given wins", func(t *testing.T) {
		got := f.update(t, ctx, row["id"], map[string]interface{}{"full": "Typed"})
		assert.Equal(t, "Typed", text(got["full"]))
	})
	t.Run("all referenced fields empty: skipped unless asked to evaluate", func(t *testing.T) {
		got := f.insert(t, ctx, map[string]interface{}{"last": "Only"})
		assert.Equal(t, "", text(got["tag"]))
		assert.Equal(t, "T-", text(got["tag_always"]))
	})
	t.Run("GetNextSerialValue in an auto-enter formula", func(t *testing.T) {
		g := newAutoFixture(t, ctx, driver,
			autoColumn{name: "num", fieldType: dbal.FieldTypeNumber, options: map[string]any{"serial_enabled": true, "serial_next": 40}},
			autoColumn{name: "peek", fieldType: dbal.FieldTypeText, options: map[string]any{"calculated_enabled": true, "calculated_formula": `GetNextSerialValue ( "x" ; "num" )`}},
		)
		got := g.insert(t, ctx, map[string]interface{}{})
		assert.Equal(t, "40", number(got["num"]))
		assert.Equal(t, "41", text(got["peek"]), "the serial is taken before the formula runs")
	})
}

// Looked-up values with each no-match option and don't-copy-if-empty.
func TestAutoEnter_LookedUpValue(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "plain", fieldType: dbal.FieldTypeNumber},
		autoColumn{name: "lower", fieldType: dbal.FieldTypeNumber},
		autoColumn{name: "higher", fieldType: dbal.FieldTypeNumber},
		autoColumn{name: "constant", fieldType: dbal.FieldTypeNumber},
		autoColumn{name: "label", fieldType: dbal.FieldTypeText},
		autoColumn{name: "label_keep", fieldType: dbal.FieldTypeText},
	)
	lookup := func(field, from string, extra map[string]any) {
		o := map[string]any{"looked_up_enabled": true, "looked_up_occurrence": f.productsOcc, "looked_up_field": from}
		for k, v := range extra {
			o[k] = v
		}
		f.setOptions(t, ctx, field, o)
	}
	lookup("plain", "price", nil)
	lookup("lower", "price", map[string]any{"looked_up_no_match": "copy_next_lower"})
	lookup("higher", "price", map[string]any{"looked_up_no_match": "copy_next_higher"})
	lookup("constant", "price", map[string]any{"looked_up_no_match": "use_constant", "looked_up_no_match_value": "-1"})
	lookup("label", "label", nil)
	lookup("label_keep", "label", map[string]any{"looked_up_dont_copy_if_empty": true})

	f.product(t, ctx, "B", 20, "Bravo")
	f.product(t, ctx, "D", 40, nil)
	f.product(t, ctx, "F", 60, "Foxtrot")

	t.Run("a match copies", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"code": "B"})
		for _, field := range []string{"plain", "lower", "higher", "constant"} {
			assert.Equal(t, "20", number(row[field]), field)
		}
		assert.Equal(t, "Bravo", text(row["label"]))
	})
	t.Run("no match: do not copy, next lower, next higher, constant", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"code": "E"})
		assert.Equal(t, "", text(row["plain"]))
		assert.Equal(t, "40", number(row["lower"]))
		assert.Equal(t, "60", number(row["higher"]))
		assert.Equal(t, "-1", number(row["constant"]))
	})
	t.Run("no lower and no higher record copies nothing", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"code": "A"})
		assert.Equal(t, "", text(row["lower"]))
		assert.Equal(t, "20", number(row["higher"]))
		row = f.insert(t, ctx, map[string]interface{}{"code": "Z"})
		assert.Equal(t, "60", number(row["lower"]))
		assert.Equal(t, "", text(row["higher"]))
	})
	t.Run("don't copy contents if empty", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"code": "B"})
		assert.Equal(t, "Bravo", text(row["label_keep"]))
		got := f.update(t, ctx, row["id"], map[string]interface{}{"code": "D"})
		assert.Equal(t, "", text(got["label"]), "an empty value is copied")
		assert.Equal(t, "Bravo", text(got["label_keep"]), "an empty value is not copied")
		assert.Equal(t, "40", number(got["plain"]))
	})
	t.Run("a value given wins; an unrelated change does not look up again", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"code": "B", "plain": 99})
		assert.Equal(t, "99", number(row["plain"]))
		f.update(t, ctx, row["id"], map[string]interface{}{"label": "edited"})
		got := f.get(t, ctx, row["id"])
		assert.Equal(t, "99", number(got["plain"]))
		assert.Equal(t, "edited", text(got["label"]))
	})
	t.Run("Relookup Field Contents", func(t *testing.T) {
		row := f.insert(t, ctx, map[string]interface{}{"code": "F"})
		_, err := f.dataSvc.UpdateRow(ctx, f.products, fmt.Sprint(productID(t, ctx, f, "F")), map[string]interface{}{"price": 65})
		require.NoError(t, err)
		assert.Equal(t, "60", number(f.get(t, ctx, row["id"])["plain"]))
		changed, err := f.dataSvc.Relookup(ctx, f.orders, "code", []string{fmt.Sprint(row["id"])})
		require.NoError(t, err)
		require.Len(t, changed, 1)
		assert.Equal(t, "65", number(changed[0]["plain"]), "the record as it is now")
		assert.Equal(t, "65", number(f.get(t, ctx, row["id"])["plain"]))
		_, err = f.dataSvc.Relookup(ctx, f.orders, "label", []string{fmt.Sprint(row["id"])})
		require.ErrorIs(t, err, data.ErrInvalidValue)
	})
}

// Copy next lower / higher value takes the nearest record among those
// meeting every other criterion of the relationship, not the nearest of the
// whole table.
func TestAutoEnter_NearestMeetsEveryCriterion(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "region", fieldType: dbal.FieldTypeText},
		autoColumn{name: "lower", fieldType: dbal.FieldTypeNumber},
		autoColumn{name: "higher", fieldType: dbal.FieldTypeNumber},
	)
	region, err := f.schemaSvc.AddColumn(ctx, f.productsID, schema.ColumnMetadata{Name: "region", DisplayName: "region", FieldType: dbal.FieldTypeText, IsNullable: true})
	require.NoError(t, err)
	_, err = f.schemaSvc.UpdateRelationship(ctx, f.relationship, schema.UpdateRelationshipInput{Predicates: []schema.RelationshipPredicate{
		{LeftColumnID: f.codes[f.orders], Operator: "=", RightColumnID: f.codes[f.products]},
		{LeftColumnID: f.columns["region"], Operator: "=", RightColumnID: region.ID},
	}})
	require.NoError(t, err)
	for field, option := range map[string]string{"lower": "copy_next_lower", "higher": "copy_next_higher"} {
		f.setOptions(t, ctx, field, map[string]any{"looked_up_enabled": true, "looked_up_occurrence": f.productsOcc,
			"looked_up_field": "price", "looked_up_no_match": option})
	}
	for _, p := range []struct {
		code, region string
		price        int
	}{{"B", "north", 20}, {"C", "north", 30}, {"D", "south", 40}, {"F", "north", 60}} {
		_, err := f.dataSvc.InsertRow(ctx, f.products, map[string]interface{}{"code": p.code, "region": p.region, "price": p.price})
		require.NoError(t, err)
	}

	row := f.insert(t, ctx, map[string]interface{}{"code": "E", "region": "north"})
	assert.Equal(t, "30", number(row["lower"]), "D is lower but in another region")
	assert.Equal(t, "60", number(row["higher"]))
	row = f.insert(t, ctx, map[string]interface{}{"code": "E", "region": "south"})
	assert.Equal(t, "40", number(row["lower"]))
	assert.Equal(t, "", text(row["higher"]), "no south record is higher")
	row = f.insert(t, ctx, map[string]interface{}{"code": "E"})
	assert.Equal(t, "", text(row["lower"]), "an empty match field relates nothing")
}

// A lookup copies from the first record a path of two relationships leads
// to: orders -> products (code) -> regions (label = code).
func TestAutoEnter_MultiHopLookup(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver, autoColumn{name: "region_name", fieldType: dbal.FieldTypeText})
	regions := fmt.Sprintf("ae_regions_%d", uniqueTestStamp())
	tbl, err := f.schemaSvc.CreateTable(ctx, regions, regions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.schemaSvc.DeleteTable(context.Background(), tbl.ID) })
	code, err := f.schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{Name: "code", DisplayName: "code", FieldType: dbal.FieldTypeText, IsNullable: true})
	require.NoError(t, err)
	_, err = f.schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{Name: "name", DisplayName: "name", FieldType: dbal.FieldTypeText, IsNullable: true})
	require.NoError(t, err)
	tables, err := f.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	label := ""
	for _, table := range tables {
		for _, c := range table.Columns {
			if table.ID == f.productsID && c.Name == "label" {
				label = c.ID
			}
		}
	}
	require.NotEmpty(t, label)
	occurrences, err := f.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	occ := map[string]string{}
	regionsOcc := ""
	for _, o := range occurrences {
		occ[o.BaseTableID] = o.ID
		if o.BaseTableID == tbl.ID {
			regionsOcc = o.Name
		}
	}
	rel, err := f.schemaSvc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:             regions + "_rel",
		LeftOccurrenceID: occ[f.productsID], LeftColumnID: label,
		RightOccurrenceID: occ[tbl.ID], RightColumnID: code.ID, Operator: "=",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.schemaSvc.DeleteRelationship(context.Background(), rel.ID) })
	f.setOptions(t, ctx, "region_name", map[string]any{"looked_up_enabled": true, "looked_up_occurrence": regionsOcc, "looked_up_field": "name"})

	f.product(t, ctx, "P1", 10, "N")
	_, err = f.dataSvc.InsertRow(ctx, regions, map[string]interface{}{"code": "N", "name": "North"})
	require.NoError(t, err)
	row := f.insert(t, ctx, map[string]interface{}{"code": "P1"})
	assert.Equal(t, "North", text(row["region_name"]))
	got := f.update(t, ctx, row["id"], map[string]interface{}{"code": "P9"})
	assert.Equal(t, "North", text(got["region_name"]), "no related record: the value is kept, as FileMaker does")
}

// Self is the field's own value in its auto-enter calculation, empty on a
// new record nobody typed into; and the calculation may call a custom
// function of the solution.
func TestAutoEnter_SelfAndCustomFunctions(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	calcOpt := func(formula string) map[string]any {
		return map[string]any{"calculated_enabled": true, "calculated_formula": formula}
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "first", fieldType: dbal.FieldTypeText},
		autoColumn{name: "self_upper", fieldType: dbal.FieldTypeText, options: calcOpt(`Upper ( Self )`)},
		autoColumn{name: "seen", fieldType: dbal.FieldTypeText, options: calcOpt(`If ( IsEmpty ( Self ) ; "new" ; Self & "+" )`)},
		autoColumn{name: "shout", fieldType: dbal.FieldTypeText},
	)

	row := f.insert(t, ctx, map[string]interface{}{"self_upper": "mixed"})
	assert.Equal(t, "MIXED", text(row["self_upper"]))
	assert.Equal(t, "new", text(row["seen"]), "Self is empty on a new record")
	got := f.update(t, ctx, row["id"], map[string]interface{}{"self_upper": "again", "seen": "x"})
	assert.Equal(t, "AGAIN", text(got["self_upper"]))
	assert.Equal(t, "x+", text(got["seen"]), "Self is what is typed")
	got = f.update(t, ctx, row["id"], map[string]interface{}{"first": "unrelated"})
	assert.Equal(t, "x+", text(got["seen"]), "a change to another field does not compute it again")

	stamp := uniqueTestStamp()
	shout := fmt.Sprintf("Shout_%d", stamp)
	_, err := f.schemaSvc.UpdateColumn(ctx, f.ordersID, f.columns["shout"], schema.UpdateColumnOptions{
		DisplayName: "shout", DefaultValue: options(calcOpt(shout + ` ( first )`)), UpdateDefaultValue: true,
	})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions, "a function the solution does not have")
	d := driver.Dialect()
	_, err = driver.DB().ExecContext(ctx, `INSERT INTO `+data.CustomFunctionsTable+` (id, name, parameters, formula, access, created_at, updated_at) VALUES (`+
		d.Placeholder(1)+`, `+d.Placeholder(2)+`, `+d.Placeholder(3)+`, `+d.Placeholder(4)+`, 'all', `+d.Placeholder(5)+`, `+d.Placeholder(6)+`)`,
		fmt.Sprintf("ae-cf-%d", stamp), shout, `["t"]`, `Upper ( t ) & "!"`, time.Now().UTC(), time.Now().UTC())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = driver.DB().ExecContext(context.Background(), `DELETE FROM `+data.CustomFunctionsTable+` WHERE name = `+d.Placeholder(1), shout)
	})
	f.setOptions(t, ctx, "shout", calcOpt(shout+` ( first )`))
	row = f.insert(t, ctx, map[string]interface{}{"first": "hey"})
	assert.Equal(t, "HEY!", text(row["shout"]))
}

// GetNextSerialValue reads a serial only in a table the account may read.
func TestAutoEnter_NextSerialValueNeedsReadAccess(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "num", fieldType: dbal.FieldTypeNumber, options: map[string]any{"serial_enabled": true, "serial_next": 5}},
	)
	_, err := f.schemaSvc.AddColumn(ctx, f.productsID, schema.ColumnMetadata{Name: "num", DisplayName: "num", FieldType: dbal.FieldTypeNumber,
		IsNullable: true, DefaultValue: options(map[string]any{"serial_enabled": true, "serial_next": 70})})
	require.NoError(t, err)
	restricted := data.NewService(driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		return table != f.products, nil
	})
	next, err := restricted.NextSerialValue(ctx, f.orders, "num")
	require.NoError(t, err)
	assert.Equal(t, "5", next)
	_, err = restricted.NextSerialValue(ctx, f.products, "num")
	require.ErrorIs(t, err, data.ErrNoAccess)

	f.columns["code"] = f.codes[f.orders]
	f.setOptions(t, ctx, "code", map[string]any{"calculated_enabled": true,
		"calculated_formula": fmt.Sprintf(`GetNextSerialValue ( "x" ; "%s::num" )`, f.productsOcc)})
	_, err = restricted.InsertRow(ctx, f.orders, map[string]interface{}{})
	require.Error(t, err, "the formula does not read a table the account may not")
	assert.Contains(t, err.Error(), data.ErrNoAccess.Error())
	row, err := f.dataSvc.InsertRow(ctx, f.orders, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "70", text(row["code"]))
}

func productID(t *testing.T, ctx context.Context, f *autoFixture, code string) any {
	t.Helper()
	rows, err := f.dataSvc.ExecuteFind(ctx, f.products, []data.FindRequest{
		{Criteria: []data.FindCriterion{{FieldName: "code", Value: "==" + code}}},
	}, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]["id"]
}

// Prohibit modification refuses data entry, not scripts.
func TestAutoEnter_ProhibitModification(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "status", fieldType: dbal.FieldTypeText, options: map[string]any{"data_enabled": true, "data_value": "Open", "prohibit_modification": true}},
		autoColumn{name: "note", fieldType: dbal.FieldTypeText},
	)

	row := f.insert(t, ctx, map[string]interface{}{"note": "a"})
	assert.Equal(t, "Open", text(row["status"]), "a constant is entered")

	_, err := f.dataSvc.InsertRow(ctx, f.orders, map[string]interface{}{"status": "Closed"})
	require.ErrorIs(t, err, data.ErrProhibitedModification)
	_, err = f.dataSvc.UpdateRow(ctx, f.orders, fmt.Sprint(row["id"]), map[string]interface{}{"status": "Closed"})
	require.ErrorIs(t, err, data.ErrProhibitedModification)

	f.update(t, ctx, row["id"], map[string]interface{}{"status": "Open", "note": "b"})

	script := data.AsScript(ctx)
	got, err := f.dataSvc.UpdateRow(script, f.orders, fmt.Sprint(row["id"]), map[string]interface{}{"status": "Closed"})
	require.NoError(t, err)
	assert.Equal(t, "Closed", text(got["status"]))
	got, err = f.dataSvc.InsertRow(script, f.orders, map[string]interface{}{"status": "Draft"})
	require.NoError(t, err)
	assert.Equal(t, "Draft", text(got["status"]))
}

// Duplicate Record copies stored fields and enters new auto-enter values,
// except looked-up values and calculated values that do not replace.
func TestAutoEnter_DuplicateRow(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "num", fieldType: dbal.FieldTypeNumber, options: map[string]any{"serial_enabled": true, "serial_next": 1}},
		autoColumn{name: "note", fieldType: dbal.FieldTypeText},
		autoColumn{name: "created_at", fieldType: dbal.FieldTypeTimestamp, options: map[string]any{"creation_enabled": true, "creation_type": "Timestamp"}},
		autoColumn{name: "kept", fieldType: dbal.FieldTypeText, options: map[string]any{"calculated_enabled": true, "calculated_formula": `note & "!"`, "do_not_replace": true}},
		autoColumn{name: "fresh", fieldType: dbal.FieldTypeText, options: map[string]any{"calculated_enabled": true, "calculated_formula": `note & "?"`}},
		autoColumn{name: "price", fieldType: dbal.FieldTypeNumber},
		autoColumn{name: "locked", fieldType: dbal.FieldTypeText, options: map[string]any{"prohibit_modification": true}},
		autoColumn{name: "photo", fieldType: dbal.FieldTypeContainer},
	)
	f.setOptions(t, ctx, "price", map[string]any{"looked_up_enabled": true, "looked_up_occurrence": f.productsOcc, "looked_up_field": "price"})
	f.product(t, ctx, "P", 10, "x")

	fixClock(t, time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC))
	row := f.insert(t, ctx, map[string]interface{}{"note": "hello", "code": "P", "photo": "aGVsbG8="})
	require.NoError(t, containermeta.Put(ctx, driver.DB(), driver.Dialect(), f.orders, fmt.Sprint(row["id"]), map[string]containermeta.Entry{
		"photo": {Filename: "hello.txt", MimeType: "text/plain", SizeBytes: 5},
	}))
	_, err := f.dataSvc.UpdateRow(data.AsScript(ctx), f.orders, fmt.Sprint(row["id"]), map[string]interface{}{"locked": "secret", "kept": "manual", "fresh": "manual"})
	require.NoError(t, err)
	_, err = f.dataSvc.UpdateRow(ctx, f.products, fmt.Sprint(productID(t, ctx, f, "P")), map[string]interface{}{"price": 11})
	require.NoError(t, err)

	fixClock(t, time.Date(2026, 1, 3, 8, 0, 0, 0, time.UTC))
	dup, err := f.dataSvc.DuplicateRow(ctx, f.orders, fmt.Sprint(row["id"]))
	require.NoError(t, err)
	assert.NotEqual(t, text(row["id"]), text(dup["id"]))
	assert.Equal(t, "2", number(dup["num"]))
	assert.Equal(t, "hello", text(dup["note"]))
	assert.Equal(t, "P", text(dup["code"]))
	assert.Contains(t, moment(dup["created_at"]), "2026-01-03")
	assert.Equal(t, "manual", text(dup["kept"]), "do-not-replace is copied")
	assert.Equal(t, "hello?", text(dup["fresh"]), "a replacing calculation is evaluated anew")
	assert.Equal(t, "10", number(dup["price"]), "a looked-up value is copied, not looked up again")
	assert.Equal(t, "secret", text(dup["locked"]), "prohibit modification does not stop a duplicate")
	meta, err := containermeta.Load(ctx, driver.DB(), driver.Dialect(), f.orders, []string{fmt.Sprint(dup["id"])})
	require.NoError(t, err)
	assert.Equal(t, "hello.txt", meta[fmt.Sprint(dup["id"])]["photo"].Filename, "a copied container keeps its file name")
}

// Imports perform the options only when asked.
func TestAutoEnter_Import(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newAutoFixture(t, ctx, driver,
		autoColumn{name: "num", fieldType: dbal.FieldTypeNumber, options: map[string]any{"serial_enabled": true, "serial_next": 100}},
		autoColumn{name: "note", fieldType: dbal.FieldTypeText},
		autoColumn{name: "status", fieldType: dbal.FieldTypeText, options: map[string]any{"data_enabled": true, "data_value": "New", "prohibit_modification": true}},
		autoColumn{name: "kept", fieldType: dbal.FieldTypeText, options: map[string]any{"calculated_enabled": true, "calculated_formula": `"calc"`, "do_not_replace": true}},
		autoColumn{name: "modified_by", fieldType: dbal.FieldTypeText, options: map[string]any{"modification_enabled": true, "modification_type": "Account Name"}},
	)
	source := parseCSV(t, "Num,Note,Status,Kept\n5,alpha,Imported,given\n6,beta,Imported,\n")
	mappings := []data.ImportMapping{{Column: 0, Field: "num"}, {Column: 1, Field: "note"}, {Column: 2, Field: "status"}, {Column: 3, Field: "kept"}}

	byNote := func(f *autoFixture) map[string]map[string]interface{} {
		rows, err := f.dataSvc.ListRows(ctx, f.orders, data.QueryOptions{})
		require.NoError(t, err)
		out := map[string]map[string]interface{}{}
		for _, r := range rows {
			out[text(r["note"])] = r
		}
		return out
	}

	t.Run("off: the imported values as they are", func(t *testing.T) {
		report, err := f.dataSvc.ImportRecords(ctx, f.orders, source, data.ImportOptions{Action: data.ImportAdd, Mappings: mappings})
		require.NoError(t, err)
		require.Equal(t, 2, report.Added)
		rows := byNote(f)
		assert.Equal(t, "5", number(rows["alpha"]["num"]))
		assert.Equal(t, "Imported", text(rows["alpha"]["status"]))
		assert.Equal(t, "", text(rows["beta"]["kept"]))
		assert.Equal(t, "", text(rows["alpha"]["modified_by"]))
		next, err := f.dataSvc.NextSerialValue(ctx, f.orders, "num")
		require.NoError(t, err)
		assert.Equal(t, "100", next)
	})

	t.Run("on: entered over the imported values, do-not-replace keeps them", func(t *testing.T) {
		g := newAutoFixture(t, ctx, driver,
			autoColumn{name: "num", fieldType: dbal.FieldTypeNumber, options: map[string]any{"serial_enabled": true, "serial_next": 100}},
			autoColumn{name: "note", fieldType: dbal.FieldTypeText},
			autoColumn{name: "status", fieldType: dbal.FieldTypeText, options: map[string]any{"data_enabled": true, "data_value": "New", "prohibit_modification": true}},
			autoColumn{name: "kept", fieldType: dbal.FieldTypeText, options: map[string]any{"calculated_enabled": true, "calculated_formula": `"calc"`, "do_not_replace": true}},
			autoColumn{name: "modified_by", fieldType: dbal.FieldTypeText, options: map[string]any{"modification_enabled": true, "modification_type": "Account Name"}},
		)
		report, err := g.dataSvc.ImportRecords(ctx, g.orders, source, data.ImportOptions{Action: data.ImportAdd, Mappings: mappings, PerformAutoEnter: true})
		require.NoError(t, err)
		require.Equal(t, 2, report.Added)
		rows := byNote(g)
		require.Len(t, rows, 2)
		assert.ElementsMatch(t, []string{"100", "101"}, []string{number(rows["alpha"]["num"]), number(rows["beta"]["num"])})
		for _, r := range rows {
			assert.Equal(t, "New", text(r["status"]))
			assert.Equal(t, "synthetic.clerk", text(r["modified_by"]))
		}
		assert.Equal(t, "given", text(rows["alpha"]["kept"]))
		assert.Equal(t, "calc", text(rows["beta"]["kept"]))
		serial := number(rows["alpha"]["num"])

		update := parseCSV(t, "Note,Status\nalpha,Changed\n")
		other := data.NewService(driver).WithAccount("synthetic.importer")
		report, err = other.ImportRecords(ctx, g.orders, update, data.ImportOptions{
			Action: data.ImportUpdateMatching, MatchFields: []string{"note"},
			Mappings:         []data.ImportMapping{{Column: 0, Field: "note"}, {Column: 1, Field: "status"}},
			PerformAutoEnter: true,
		})
		require.NoError(t, err)
		require.Equal(t, 1, report.Updated)
		got := byNote(g)["alpha"]
		assert.Equal(t, "Changed", text(got["status"]), "prohibit modification does not stop an import")
		assert.Equal(t, "synthetic.importer", text(got["modified_by"]))
		assert.Equal(t, serial, number(got["num"]), "an updated record keeps its serial")
		assert.Equal(t, "synthetic.clerk", text(byNote(g)["beta"]["modified_by"]))
	})
}
