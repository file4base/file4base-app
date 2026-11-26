package schema_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestSplitCustomValues(t *testing.T) {
	values := schema.SplitCustomValues("New\nContinuing\n\n  Lapsed  \nNew\r\nFormer")
	require.Equal(t, []string{"New", "Continuing", "Lapsed", "Former"}, values,
		"blank lines are dropped, values are trimmed and each appears once")

	require.Empty(t, schema.SplitCustomValues(""))
	require.Empty(t, schema.SplitCustomValues("   \n\n  "))
}

// Value lists (#31): a named set a field can be filled from, either typed in
// or taken from what a field already holds.
func TestValueLists(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: testdb.Engine(),
		DSN:        testdb.DevDSN(),
	})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	name := fmt.Sprintf("value_list_customers_%d", stamp)
	tbl, err := svc.CreateTable(ctx, "Value List Customers", name)
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(ctx, tbl.ID) }()

	typeCol, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "customer_type", DisplayName: "Customer Type",
		FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	var created *schema.ValueList

	t.Run("a custom list keeps the values it was given", func(t *testing.T) {
		created, err = svc.CreateValueList(ctx, schema.ValueListInput{
			Name:         fmt.Sprintf("Customer Types %d", stamp),
			Kind:         schema.ValueListCustom,
			CustomValues: "New\nContinuing",
		})
		require.NoError(t, err)
		require.Equal(t, schema.ValueListCustom, created.Kind)

		values, err := svc.ValueListValues(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"New", "Continuing"}, values)
	})

	t.Run("a custom list with no values is refused", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: fmt.Sprintf("Empty %d", stamp), Kind: schema.ValueListCustom, CustomValues: "  \n ",
		})
		assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("a list with no name is refused", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{Name: "  ", CustomValues: "a"})
		assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("two lists cannot share a name", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: created.Name, Kind: schema.ValueListCustom, CustomValues: "x",
		})
		assert.ErrorIs(t, err, schema.ErrValueListExists)
	})

	t.Run("a list taken from a field needs that field", func(t *testing.T) {
		_, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: fmt.Sprintf("Loose %d", stamp), Kind: schema.ValueListFromField,
		})
		assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	t.Run("a list taken from a field offers the values it holds", func(t *testing.T) {
		list, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name:           fmt.Sprintf("Types In Use %d", stamp),
			Kind:           schema.ValueListFromField,
			SourceTableID:  &tbl.ID,
			SourceColumnID: &typeCol.ID,
		})
		require.NoError(t, err)

		// Nothing stored yet, so nothing is offered.
		values, err := svc.ValueListValues(ctx, list.ID)
		require.NoError(t, err)
		require.Empty(t, values)

		db := driver.DB()
		dialect := driver.Dialect()
		for i, value := range []string{"Continuing", "New", "Continuing", ""} {
			q := fmt.Sprintf("INSERT INTO %s (%s, %s) VALUES (%s, %s)",
				dialect.QuoteIdentifier(name), dialect.QuoteIdentifier("id"),
				dialect.QuoteIdentifier("customer_type"), dialect.Placeholder(1), dialect.Placeholder(2))
			_, err := db.ExecContext(ctx, q, fmt.Sprintf("row-%d-%d", stamp, i), value)
			require.NoError(t, err)
		}

		values, err = svc.ValueListValues(ctx, list.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"Continuing", "New"}, values,
			"sorted, without blanks, and each value once")
	})

	t.Run("a list can be renamed and its values replaced", func(t *testing.T) {
		updated, err := svc.UpdateValueList(ctx, created.ID, schema.ValueListInput{
			Name:         fmt.Sprintf("Customer Status %d", stamp),
			Kind:         schema.ValueListCustom,
			CustomValues: "New\nContinuing\nLapsed",
		})
		require.NoError(t, err)
		require.Contains(t, updated.Name, "Customer Status")

		values, err := svc.ValueListValues(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"New", "Continuing", "Lapsed"}, values)
	})

	t.Run("the lists come back by name", func(t *testing.T) {
		lists, err := svc.ListValueLists(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(lists), 2)
		for i := 1; i < len(lists); i++ {
			require.LessOrEqual(t, lists[i-1].Name, lists[i].Name)
		}
	})

	t.Run("deleting a list that is not there is reported", func(t *testing.T) {
		assert.ErrorIs(t, svc.DeleteValueList(ctx, "no-such-list"), schema.ErrValueListNotFound)
	})

	t.Run("a deleted list is gone", func(t *testing.T) {
		require.NoError(t, svc.DeleteValueList(ctx, created.ID))
		_, err := svc.GetValueList(ctx, created.ID)
		assert.ErrorIs(t, err, schema.ErrValueListNotFound)
	})
}

// A value list must survive Save and Open: it goes into the solution file and
// comes back, with a field list's table and column remapped to the
// destination's ids (#31).
func TestValueListsRoundTripThroughASolutionFile(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	name := fmt.Sprintf("vl_roundtrip_%d", stamp)
	tbl, err := svc.CreateTable(ctx, "VL Roundtrip", name)
	require.NoError(t, err)
	defer func() { _ = svc.DeleteTable(ctx, tbl.ID) }()

	col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "city", DisplayName: "City", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)

	custom, err := svc.CreateValueList(ctx, schema.ValueListInput{
		Name: fmt.Sprintf("Types %d", stamp), Kind: schema.ValueListCustom, CustomValues: "New\nContinuing",
	})
	require.NoError(t, err)
	defer func() { _ = svc.DeleteValueList(ctx, custom.ID) }()

	fromField, err := svc.CreateValueList(ctx, schema.ValueListInput{
		Name: fmt.Sprintf("Cities %d", stamp), Kind: schema.ValueListFromField,
		SourceTableID: &tbl.ID, SourceColumnID: &col.ID,
	})
	require.NoError(t, err)
	defer func() { _ = svc.DeleteValueList(ctx, fromField.ID) }()

	file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "VL Roundtrip"})
	require.NoError(t, err)

	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)

	var sawCustom, sawField bool
	for _, vl := range bundle.ValueLists {
		switch vl.ID {
		case custom.ID:
			sawCustom = true
			assert.Equal(t, "New\nContinuing", vl.CustomValues)
		case fromField.ID:
			sawField = true
			require.NotNil(t, vl.SourceColumnID)
			assert.Equal(t, col.ID, *vl.SourceColumnID)
		}
	}
	assert.True(t, sawCustom, "the custom list is in the file")
	assert.True(t, sawField, "the field list is in the file, with the field it reads")

	// Importing the same file again matches the lists by name instead of
	// making a second copy of each.
	before, err := svc.ListValueLists(ctx)
	require.NoError(t, err)
	report, err := svc.ImportSolution(ctx, file)
	require.NoError(t, err)
	after, err := svc.ListValueLists(ctx)
	require.NoError(t, err)

	assert.Equal(t, len(before), len(after), "re-importing must not duplicate the lists")
	assert.Equal(t, 0, report.ValueListsCreated)
	assert.GreaterOrEqual(t, report.ValueListsUpdated, 2)

	// And the field list still resolves against the destination's own field.
	values, err := svc.ValueListValues(ctx, fromField.ID)
	require.NoError(t, err)
	assert.NotNil(t, values)
}

// A list taken from a field can show a second field beside the first or in
// its place, be sorted by either, and include only the values of the records
// related to the record it is shown on (#31).
func TestValueListFieldOptions(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	// Applying the catalog again must not add anything twice.
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	names := map[string]string{
		"clients":  fmt.Sprintf("vlo_clients_%d", stamp),
		"projects": fmt.Sprintf("vlo_projects_%d", stamp),
		"tasks":    fmt.Sprintf("vlo_tasks_%d", stamp),
	}
	tables := map[string]*schema.TableMetadata{}
	for key, name := range names {
		tbl, err := svc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), tbl.ID) })
		tables[key] = tbl
	}
	column := func(table, name string) *schema.ColumnMetadata {
		col, err := svc.AddColumn(ctx, tables[table].ID, schema.ColumnMetadata{
			Name: name, DisplayName: name, FieldType: dbal.FieldTypeText, IsNullable: true,
		})
		require.NoError(t, err)
		return col
	}
	clientCode := column("clients", "code")
	projectClient, projectRef := column("projects", "client_code"), column("projects", "ref")
	taskCode, taskTitle, taskProject := column("tasks", "task_code"), column("tasks", "title"), column("tasks", "project_ref")

	occurrence := map[string]string{}
	occurrences, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		for key, tbl := range tables {
			if o.BaseTableID == tbl.ID {
				occurrence[key] = o.ID
			}
		}
	}
	relate := func(left, right string, leftCol, rightCol *schema.ColumnMetadata) {
		rel, err := svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
			Name:             fmt.Sprintf("vlo_%s_%s_%d", left, right, stamp),
			LeftOccurrenceID: occurrence[left], LeftColumnID: leftCol.ID,
			RightOccurrenceID: occurrence[right], RightColumnID: rightCol.ID, Operator: "=",
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteRelationship(context.Background(), rel.ID) })
	}
	relate("clients", "projects", clientCode, projectClient)
	relate("projects", "tasks", projectRef, taskProject)

	inserted := 0
	insert := func(table string, values map[string]string) string {
		inserted++
		id := fmt.Sprintf("%s-%d-%d", table, stamp, inserted)
		dialect := driver.Dialect()
		cols, marks, args := []string{dialect.QuoteIdentifier("id")}, []string{dialect.Placeholder(1)}, []interface{}{id}
		for col, value := range values {
			cols = append(cols, dialect.QuoteIdentifier(col))
			marks = append(marks, dialect.Placeholder(len(marks)+1))
			args = append(args, value)
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", dialect.QuoteIdentifier(names[table]),
			strings.Join(cols, ", "), strings.Join(marks, ", "))
		_, err := driver.DB().ExecContext(ctx, q, args...)
		require.NoError(t, err)
		return id
	}
	client := insert("clients", map[string]string{"code": "C1"})
	insert("clients", map[string]string{"code": "C2"})
	projectA := insert("projects", map[string]string{"client_code": "C1", "ref": "A"})
	projectB := insert("projects", map[string]string{"client_code": "C2", "ref": "B"})
	for _, task := range [][3]string{{"T3", "Write", "A"}, {"T1", "Review", "A"}, {"T2", "Plan", "B"}, {"T1", "Review", "A"}, {"", "Untitled", "A"}} {
		insert("tasks", map[string]string{"task_code": task[0], "title": task[1], "project_ref": task[2]})
	}

	tasksTable := tables["tasks"].ID
	create := func(in schema.ValueListInput) *schema.ValueList {
		t.Helper()
		in.Name = fmt.Sprintf("%s %d", in.Name, stamp)
		in.Kind = schema.ValueListFromField
		in.SourceTableID, in.SourceColumnID = &tasksTable, &taskCode.ID
		list, err := svc.CreateValueList(ctx, in)
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteValueList(context.Background(), list.ID) })
		return list
	}
	type entry struct{ value, second string }
	resolve := func(id string, record *schema.ValueListRecord) []entry {
		t.Helper()
		items, _, err := svc.ValueListItems(ctx, id, record, nil)
		require.NoError(t, err)
		out := []entry{}
		for _, item := range items {
			e := entry{value: item.Value}
			if item.Second != nil {
				e.second = *item.Second
			}
			out = append(out, e)
		}
		return out
	}

	t.Run("a list of one field keeps FileMaker's defaults", func(t *testing.T) {
		list := create(schema.ValueListInput{Name: "Task codes"})
		assert.Equal(t, schema.ValueListSortFirst, list.SortBy)
		assert.Nil(t, list.SecondColumnID)
		items, truncated, err := svc.ValueListItems(ctx, list.ID, nil, nil)
		require.NoError(t, err)
		require.Len(t, items, 3)
		assert.False(t, truncated)
		assert.Nil(t, items[0].Second, "a list without a second field gives no second value")
		values, err := svc.ValueListValues(ctx, list.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"T1", "T2", "T3"}, values)
	})

	t.Run("a second field is offered beside the first, sorted by either", func(t *testing.T) {
		byFirst := create(schema.ValueListInput{Name: "Tasks by code", SecondColumnID: &taskTitle.ID})
		assert.Equal(t, []entry{{"T1", "Review"}, {"T2", "Plan"}, {"T3", "Write"}}, resolve(byFirst.ID, nil),
			"each value once, and no entry without a first value to store")

		bySecond := create(schema.ValueListInput{
			Name: "Tasks by title", SecondColumnID: &taskTitle.ID, ShowSecondOnly: true, SortBy: "second",
		})
		got, err := svc.GetValueList(ctx, bySecond.ID)
		require.NoError(t, err)
		assert.True(t, got.ShowSecondOnly)
		assert.Equal(t, schema.ValueListSortSecond, got.SortBy)
		assert.Equal(t, []entry{{"T2", "Plan"}, {"T1", "Review"}, {"T3", "Write"}}, resolve(bySecond.ID, nil))
	})

	t.Run("only related values follow the relationships from the record in hand", func(t *testing.T) {
		fromProject := create(schema.ValueListInput{
			Name: "Tasks of the project", SecondColumnID: &taskTitle.ID, SortBy: "second",
			RelatedOccurrenceID: ptr(occurrence["projects"]), SourceOccurrenceID: ptr(occurrence["tasks"]),
		})
		assert.Equal(t, []entry{{"T1", "Review"}, {"T3", "Write"}},
			resolve(fromProject.ID, &schema.ValueListRecord{Table: names["projects"], ID: projectA}))
		assert.Equal(t, []entry{{"T2", "Plan"}},
			resolve(fromProject.ID, &schema.ValueListRecord{Table: names["projects"], ID: projectB}))

		assert.Empty(t, resolve(fromProject.ID, nil), "without a record there is nothing related")
		assert.Empty(t, resolve(fromProject.ID, &schema.ValueListRecord{Table: names["clients"], ID: client}),
			"a record of another table is not one the relationship starts from")
		assert.Empty(t, resolve(fromProject.ID, &schema.ValueListRecord{Table: names["projects"], ID: "no-such-record"}))

		// Two relationships away: a client's projects' tasks.
		fromClient := create(schema.ValueListInput{
			Name: "Tasks of the client", RelatedOccurrenceID: ptr(occurrence["clients"]), SourceOccurrenceID: ptr(occurrence["tasks"]),
		})
		assert.Equal(t, []entry{{value: "T1"}, {value: "T3"}},
			resolve(fromClient.ID, &schema.ValueListRecord{Table: names["clients"], ID: client}))
	})

	t.Run("options that do not fit together are refused", func(t *testing.T) {
		otherTable := tables["projects"].ID
		for name, in := range map[string]schema.ValueListInput{
			"second field of another table":  {SecondColumnID: &projectRef.ID},
			"sorted by a missing second":     {SortBy: "second"},
			"second only without a second":   {ShowSecondOnly: true},
			"an unknown sort":                {SecondColumnID: &taskTitle.ID, SortBy: "third"},
			"related without its occurrence": {RelatedOccurrenceID: ptr(occurrence["projects"])},
			"related from nowhere":           {RelatedOccurrenceID: ptr("no-such-occurrence"), SourceOccurrenceID: ptr(occurrence["tasks"])},
			"read through another table":     {RelatedOccurrenceID: ptr(occurrence["projects"]), SourceOccurrenceID: ptr(occurrence["clients"])},
			"first field of another table":   {SourceTableID: &otherTable},
		} {
			in.Name, in.Kind = fmt.Sprintf("Refused %s %d", name, stamp), schema.ValueListFromField
			if in.SourceTableID == nil {
				in.SourceTableID = &tasksTable
			}
			in.SourceColumnID = &taskCode.ID
			_, err := svc.CreateValueList(ctx, in)
			assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions, name)
		}
	})

	t.Run("a custom list carries none of the field options", func(t *testing.T) {
		list, err := svc.CreateValueList(ctx, schema.ValueListInput{
			Name: fmt.Sprintf("Plain %d", stamp), Kind: schema.ValueListCustom, CustomValues: "a",
			SecondColumnID: &taskTitle.ID, ShowSecondOnly: true, RelatedOccurrenceID: ptr(occurrence["projects"]),
		})
		require.NoError(t, err)
		defer func() { _ = svc.DeleteValueList(ctx, list.ID) }()
		assert.Nil(t, list.SecondColumnID)
		assert.False(t, list.ShowSecondOnly)
		assert.Nil(t, list.RelatedOccurrenceID)
		assert.Empty(t, list.SortBy)
	})

	t.Run("the options survive Save and Open", func(t *testing.T) {
		list := create(schema.ValueListInput{
			Name: "Exported", SecondColumnID: &taskTitle.ID, ShowSecondOnly: true, SortBy: "second",
			RelatedOccurrenceID: ptr(occurrence["projects"]), SourceOccurrenceID: ptr(occurrence["tasks"]),
		})
		file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "VL Options"})
		require.NoError(t, err)
		bundle, err := schema.DecodeSolutionBundle(file)
		require.NoError(t, err)
		var exported *schema.BundleValueList
		for i := range bundle.ValueLists {
			if bundle.ValueLists[i].ID == list.ID {
				exported = &bundle.ValueLists[i]
			}
		}
		require.NotNil(t, exported)
		assert.Equal(t, taskTitle.ID, *exported.SecondColumnID)
		assert.True(t, exported.ShowSecondOnly)
		assert.Equal(t, schema.ValueListSortSecond, exported.SortBy)
		assert.Equal(t, occurrence["projects"], *exported.RelatedOccurrenceID)

		_, err = svc.ImportSolution(ctx, file)
		require.NoError(t, err)
		reimported, err := svc.GetValueList(ctx, list.ID)
		require.NoError(t, err)
		assert.Equal(t, list.SecondColumnID, reimported.SecondColumnID)
		assert.Equal(t, list.RelatedOccurrenceID, reimported.RelatedOccurrenceID)
		assert.Equal(t, list.SourceOccurrenceID, reimported.SourceOccurrenceID)
		assert.True(t, reimported.ShowSecondOnly)
	})

	t.Run("deleting the second field leaves the list with its first", func(t *testing.T) {
		extra := column("tasks", "label")
		list := create(schema.ValueListInput{Name: "Labelled", SecondColumnID: &extra.ID, ShowSecondOnly: true, SortBy: "second"})
		require.NoError(t, svc.DeleteColumn(ctx, tasksTable, extra.ID))
		got, err := svc.GetValueList(ctx, list.ID)
		require.NoError(t, err)
		assert.Nil(t, got.SecondColumnID)
		assert.False(t, got.ShowSecondOnly)
		assert.Equal(t, schema.ValueListSortFirst, got.SortBy)
		assert.Equal(t, []entry{{value: "T1"}, {value: "T2"}, {value: "T3"}}, resolve(list.ID, nil))
	})

	t.Run("the catalog has each foreign key once", func(t *testing.T) {
		if driver.Dialect().Engine() == dbal.EngineSQLite {
			// SQLite keeps the keys in the table's definition.
			var keys int
			require.NoError(t, driver.DB().QueryRowContext(ctx,
				`SELECT COUNT(DISTINCT id) FROM pragma_foreign_key_list('sys_value_lists')`).Scan(&keys))
			assert.Equal(t, 5, keys, "source table and field, second field, and the two occurrences")
			var definition string
			require.NoError(t, driver.DB().QueryRowContext(ctx,
				`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'sys_value_lists'`).Scan(&definition))
			for _, name := range []string{"fk_value_list_second_column_id", "fk_value_list_source_occurrence_id", "fk_value_list_related_occurrence_id"} {
				assert.Equal(t, 1, strings.Count(definition, name), name)
			}
			return
		}
		schemaOf := "current_schema()"
		if driver.Dialect().Engine() == dbal.EngineMariaDB {
			schemaOf = "DATABASE()"
		}
		var keys int
		require.NoError(t, driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.table_constraints
			WHERE table_schema = `+schemaOf+` AND table_name = 'sys_value_lists' AND constraint_type = 'FOREIGN KEY'`).Scan(&keys))
		assert.Equal(t, 5, keys, "source table and field, second field, and the two occurrences")

		// The keys carry the names server/migrations/004 gives them, so
		// applying that file before the server starts adds none twice.
		var named int
		require.NoError(t, driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.table_constraints
			WHERE table_schema = `+schemaOf+` AND table_name = 'sys_value_lists' AND constraint_type = 'FOREIGN KEY'
			AND constraint_name IN ('fk_value_list_second_column_id', 'fk_value_list_source_occurrence_id',
			                        'fk_value_list_related_occurrence_id')`).Scan(&named))
		assert.Equal(t, 3, named)
	})
}

// TestValueListEntries pins what a list taken from a field offers: the
// caller's access, one entry per text it shows, values written as their field
// shows them, and a limit that is applied after grouping and is reported.
func TestValueListEntries(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database unavailable:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	name := fmt.Sprintf("vle_items_%d", stamp)
	tbl, err := svc.CreateTable(ctx, name, name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), tbl.ID) })
	column := func(name string, fieldType dbal.AgnosticFieldType) *schema.ColumnMetadata {
		col, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: name, DisplayName: name, FieldType: fieldType, IsNullable: true,
		})
		require.NoError(t, err)
		return col
	}
	code, label := column("code", dbal.FieldTypeText), column("label", dbal.FieldTypeText)
	day, amount := column("day", dbal.FieldTypeDate), column("amount", dbal.FieldTypeNumber)

	// 600 codes share the label "Same"; a few more records have their own.
	dialect := driver.Dialect()
	var tuples []string
	var args []interface{}
	add := func(values ...interface{}) {
		marks := make([]string, len(values))
		for i := range values {
			marks[i] = dialect.Placeholder(len(args) + i + 1)
		}
		tuples = append(tuples, "("+strings.Join(marks, ", ")+")")
		args = append(args, values...)
	}
	for i := 0; i < 600; i++ {
		add(fmt.Sprintf("r%d", i), fmt.Sprintf("C%03d", i), "Same", nil, nil)
	}
	add("r-a", "B1", "Alpha", "2026-06-15", "12.50")
	add("r-b", "B2", "Alpha", "2026-06-15", "3")
	add("r-c", "B1", "Beta", nil, nil)
	_, err = driver.DB().ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s, %s, %s, %s, %s) VALUES %s",
		dialect.QuoteIdentifier(name), dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier("code"),
		dialect.QuoteIdentifier("label"), dialect.QuoteIdentifier("day"), dialect.QuoteIdentifier("amount"),
		strings.Join(tuples, ", ")), args...)
	require.NoError(t, err)

	lists := 0
	create := func(first *schema.ColumnMetadata, in schema.ValueListInput) string {
		t.Helper()
		lists++
		in.Name = fmt.Sprintf("Entries %d %d", stamp, lists)
		in.Kind, in.SourceTableID, in.SourceColumnID = schema.ValueListFromField, &tbl.ID, &first.ID
		list, err := svc.CreateValueList(ctx, in)
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteValueList(context.Background(), list.ID) })
		return list.ID
	}
	type entry struct{ value, second string }
	resolve := func(id string, reader *data.Service) ([]entry, bool) {
		t.Helper()
		items, truncated, err := svc.ValueListItems(ctx, id, nil, reader)
		require.NoError(t, err)
		out := []entry{}
		for _, item := range items {
			e := entry{value: item.Value}
			if item.Second != nil {
				e.second = *item.Second
			}
			out = append(out, e)
		}
		return out, truncated
	}

	t.Run("showing the second field only offers each text once", func(t *testing.T) {
		id := create(code, schema.ValueListInput{SecondColumnID: &label.ID, ShowSecondOnly: true, SortBy: "second"})
		got, truncated := resolve(id, nil)
		assert.Equal(t, []entry{{"B1", "Alpha"}, {"B1", "Beta"}, {"C000", "Same"}}, got,
			"600 records showing Same are one entry, storing the least code, and hide nothing")
		assert.False(t, truncated)
	})

	t.Run("showing both fields offers each pair once", func(t *testing.T) {
		id := create(code, schema.ValueListInput{SecondColumnID: &label.ID, SortBy: "second"})
		got, truncated := resolve(id, nil)
		require.Len(t, got, 500)
		assert.Equal(t, []entry{{"B1", "Alpha"}, {"B2", "Alpha"}, {"B1", "Beta"}, {"C000", "Same"}}, got[:4],
			"B1 shows two texts, so it is offered with each")
		assert.True(t, truncated, "603 pairs, 500 offered")
	})

	t.Run("a long list is cut after grouping, and says so", func(t *testing.T) {
		got, truncated := resolve(create(code, schema.ValueListInput{}), nil)
		require.Len(t, got, 500)
		assert.Equal(t, entry{value: "B1"}, got[0])
		assert.Equal(t, entry{value: "C497"}, got[499], "the first 500 codes in order, each once")
		assert.True(t, truncated)

		got, truncated = resolve(create(label, schema.ValueListInput{}), nil)
		assert.Equal(t, []entry{{value: "Alpha"}, {value: "Beta"}, {value: "Same"}}, got)
		assert.False(t, truncated)
	})

	t.Run("dates and numbers read as their field shows them", func(t *testing.T) {
		got, _ := resolve(create(day, schema.ValueListInput{}), nil)
		assert.Equal(t, []entry{{value: "2026-06-15"}}, got)
		got, _ = resolve(create(amount, schema.ValueListInput{}), nil)
		assert.Equal(t, []entry{{value: "12.5"}, {value: "3"}}, got)
	})

	t.Run("a table the caller may not read offers nothing", func(t *testing.T) {
		id := create(label, schema.ValueListInput{SecondColumnID: &code.ID})
		denied := data.NewService(driver).WithReadAccess(func(context.Context, string) (bool, error) { return false, nil })
		got, truncated := resolve(id, denied)
		assert.Empty(t, got)
		assert.False(t, truncated)

		allowed := data.NewService(driver).WithReadAccess(func(context.Context, string) (bool, error) { return true, nil })
		got, _ = resolve(id, allowed)
		assert.NotEmpty(t, got)
	})
}

// A list showing only its second field stores the least value of the
// records that show each text, which PostgreSQL has no MIN for when the first
// field is a Boolean or a container. A container is offered as the name of
// its file, the text FileMaker and a calculation give it, on both engines and
// whether the list reads every record or only the related ones.
func TestValueListEntriesOfBooleansAndContainers(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database unavailable:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	tables := map[string]*schema.TableMetadata{}
	for _, key := range []string{"parcels", "boxes"} {
		name := fmt.Sprintf("vlb_%s_%d", key, stamp)
		tbl, err := svc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), tbl.ID) })
		tables[key] = tbl
	}
	column := func(table, name string, fieldType dbal.AgnosticFieldType) *schema.ColumnMetadata {
		col, err := svc.AddColumn(ctx, tables[table].ID, schema.ColumnMetadata{
			Name: name, DisplayName: name, FieldType: fieldType, IsNullable: true,
		})
		require.NoError(t, err)
		return col
	}
	parcelCode := column("parcels", "code", dbal.FieldTypeText)
	boxParcel, label := column("boxes", "parcel_code", dbal.FieldTypeText), column("boxes", "label", dbal.FieldTypeText)
	sealed, scan := column("boxes", "sealed", dbal.FieldTypeBoolean), column("boxes", "scan", dbal.FieldTypeContainer)

	occurrence := map[string]string{}
	occurrences, err := svc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		for key, tbl := range tables {
			if o.BaseTableID == tbl.ID {
				occurrence[key] = o.ID
			}
		}
	}
	rel, err := svc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:             fmt.Sprintf("vlb_parcel_boxes_%d", stamp),
		LeftOccurrenceID: occurrence["parcels"], LeftColumnID: parcelCode.ID,
		RightOccurrenceID: occurrence["boxes"], RightColumnID: boxParcel.ID, Operator: "=",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteRelationship(context.Background(), rel.ID) })

	dialect := driver.Dialect()
	insert := func(table, id string, columns map[string]interface{}) {
		t.Helper()
		cols, marks, args := []string{dialect.QuoteIdentifier("id")}, []string{dialect.Placeholder(1)}, []interface{}{id}
		for col, value := range columns {
			cols = append(cols, dialect.QuoteIdentifier(col))
			marks = append(marks, dialect.Placeholder(len(args)+1))
			args = append(args, value)
		}
		_, err := driver.DB().ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			dialect.QuoteIdentifier(tables[table].Name), strings.Join(cols, ", "), strings.Join(marks, ", ")), args...)
		require.NoError(t, err)
	}
	insert("parcels", fmt.Sprintf("p1-%d", stamp), map[string]interface{}{"code": "P1"})
	parcel := fmt.Sprintf("p1-%d", stamp)
	file := func(id, name string, content []byte) {
		t.Helper()
		require.NoError(t, containermeta.Put(ctx, driver.DB(), dialect, tables["boxes"].Name, id,
			map[string]containermeta.Entry{"scan": {Filename: name, MimeType: "application/octet-stream", SizeBytes: int64(len(content))}}))
	}
	// Two open boxes with a named file each, one closed box whose file has
	// no known name, and one closed box with neither a seal nor a file.
	boxes := []struct {
		id, label string
		sealed    interface{}
		scan      []byte
		filename  string
	}{
		{"b1", "Open", false, []byte("one"), "plan.pdf"},
		{"b2", "Open", true, []byte("two"), "photo.png"},
		{"b3", "Closed", true, []byte("three"), ""},
		{"b4", "Closed", nil, nil, ""},
	}
	for _, b := range boxes {
		id := fmt.Sprintf("%s-%d", b.id, stamp)
		values := map[string]interface{}{"parcel_code": "P1", "label": b.label, "sealed": b.sealed}
		if b.scan != nil {
			values["scan"] = b.scan
		}
		insert("boxes", id, values)
		if b.filename != "" {
			file(id, b.filename, b.scan)
		}
	}

	lists := 0
	create := func(first *schema.ColumnMetadata, in schema.ValueListInput) string {
		t.Helper()
		lists++
		in.Name = fmt.Sprintf("Boxes %d %d", stamp, lists)
		in.Kind, in.SourceTableID, in.SourceColumnID = schema.ValueListFromField, &tables["boxes"].ID, &first.ID
		list, err := svc.CreateValueList(ctx, in)
		require.NoError(t, err)
		t.Cleanup(func() { _ = svc.DeleteValueList(context.Background(), list.ID) })
		return list.ID
	}
	type entry struct{ value, second string }
	resolve := func(id string, record *schema.ValueListRecord) []entry {
		t.Helper()
		items, _, err := svc.ValueListItems(ctx, id, record, nil)
		require.NoError(t, err)
		out := []entry{}
		for _, item := range items {
			e := entry{value: item.Value}
			if item.Second != nil {
				e.second = *item.Second
			}
			out = append(out, e)
		}
		return out
	}

	t.Run("a Boolean first field shown by its second field only", func(t *testing.T) {
		got := resolve(create(sealed, schema.ValueListInput{SecondColumnID: &label.ID, ShowSecondOnly: true, SortBy: "second"}), nil)
		assert.Equal(t, []entry{{"true", "Closed"}, {"false", "Open"}}, got,
			"each label once, storing the least seal of its boxes; a box with no seal offers nothing")
		got = resolve(create(sealed, schema.ValueListInput{SecondColumnID: &label.ID}), nil)
		assert.Equal(t, []entry{{"false", "Open"}, {"true", "Closed"}, {"true", "Open"}}, got)
	})

	t.Run("a container first field is offered as its file name", func(t *testing.T) {
		got := resolve(create(scan, schema.ValueListInput{SecondColumnID: &label.ID, ShowSecondOnly: true, SortBy: "second"}), nil)
		assert.Equal(t, []entry{{"photo.png", "Open"}}, got,
			"a file without a known name offers nothing, so Closed has no entry")
		got = resolve(create(scan, schema.ValueListInput{}), nil)
		assert.Equal(t, []entry{{value: "photo.png"}, {value: "plan.pdf"}}, got)
	})

	t.Run("a container second field is shown as its file name", func(t *testing.T) {
		got := resolve(create(label, schema.ValueListInput{SecondColumnID: &scan.ID}), nil)
		assert.Equal(t, []entry{{"Closed", ""}, {"Open", "photo.png"}, {"Open", "plan.pdf"}}, got)
	})

	t.Run("related containers are offered as their file names too", func(t *testing.T) {
		id := create(scan, schema.ValueListInput{
			SecondColumnID: &label.ID, ShowSecondOnly: true,
			RelatedOccurrenceID: ptr(occurrence["parcels"]), SourceOccurrenceID: ptr(occurrence["boxes"]),
		})
		got := resolve(id, &schema.ValueListRecord{Table: tables["parcels"].Name, ID: parcel})
		assert.Equal(t, []entry{{"photo.png", "Open"}}, got)
	})
}

// FileMaker cannot index an unstored calculation or a summary field, and
// refuses either for a value list: "This field cannot be indexed". File4Base
// refuses them too, rather than keep a list that would always be empty, and
// will not make unstored a calculation a list already reads.
func TestValueListRefusesAFieldThatCannotBeIndexed(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database unavailable:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	name := fmt.Sprintf("vli_lines_%d", stamp)
	tbl, err := svc.CreateTable(ctx, name, name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), tbl.ID) })
	add := func(col schema.ColumnMetadata) *schema.ColumnMetadata {
		t.Helper()
		col.DisplayName, col.IsNullable = col.Name, true
		created, err := svc.AddColumn(ctx, tbl.ID, col)
		require.NoError(t, err)
		return created
	}
	calculation := func(formula string, stored bool) *string {
		raw := fmt.Sprintf(`{"formula":%q,"result_type":"Text","stored":%t}`, formula, stored)
		return &raw
	}
	label := add(schema.ColumnMetadata{Name: "label", FieldType: dbal.FieldTypeText})
	shout := add(schema.ColumnMetadata{Name: "shout", FieldType: dbal.FieldTypeCalculation, CalculationFormula: calculation("Upper ( label )", false)})
	whisper := add(schema.ColumnMetadata{Name: "whisper", FieldType: dbal.FieldTypeCalculation, CalculationFormula: calculation("Lower ( label )", true)})
	total := add(schema.ColumnMetadata{Name: "total", FieldType: dbal.FieldTypeSummary, CalculationFormula: ptr(`{"summary_type":"count","field":"label"}`)})

	list := func(first, second *schema.ColumnMetadata) schema.ValueListInput {
		in := schema.ValueListInput{Name: fmt.Sprintf("Lines %d %s", stamp, first.Name), Kind: schema.ValueListFromField,
			SourceTableID: &tbl.ID, SourceColumnID: &first.ID}
		if second != nil {
			in.Name += " " + second.Name
			in.SecondColumnID = &second.ID
		}
		return in
	}

	for _, c := range []struct {
		first, second *schema.ColumnMetadata
		why           string
	}{
		{shout, nil, "unstored calculation"},
		{label, shout, "unstored calculation"},
		{total, nil, "summary field"},
	} {
		_, err := svc.CreateValueList(ctx, list(c.first, c.second))
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		assert.Contains(t, err.Error(), "cannot be indexed")
		assert.Contains(t, err.Error(), c.why)
	}

	stored, err := svc.CreateValueList(ctx, list(whisper, label))
	require.NoError(t, err, "a stored calculation is indexed like any field")
	t.Cleanup(func() { _ = svc.DeleteValueList(context.Background(), stored.ID) })

	_, err = svc.UpdateValueList(ctx, stored.ID, list(shout, label))
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions, "an update is checked as a creation is")

	_, err = svc.UpdateColumn(ctx, tbl.ID, whisper.ID, schema.UpdateColumnOptions{
		DisplayName: whisper.DisplayName, CalculationFormula: calculation("Lower ( label )", false), UpdateCalculation: true,
	})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	assert.Contains(t, err.Error(), stored.Name, "the refusal names the list that reads the field")
}

// A solution file is a backup: a list it holds was valid when it was saved,
// so restoring it never fails on a field that cannot be indexed. The list is
// kept as it was and the import report says it offers nothing.
func TestARestoredValueListOfAnUnindexableFieldIsKeptWithAWarning(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database unavailable:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))

	stamp := uniqueTestStamp()
	name := fmt.Sprintf("vlr_lines_%d", stamp)
	tbl, err := svc.CreateTable(ctx, name, name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTable(context.Background(), tbl.ID) })
	_, err = svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "label", DisplayName: "label", FieldType: dbal.FieldTypeText, IsNullable: true,
	})
	require.NoError(t, err)
	shout, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "shout", DisplayName: "shout", FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: ptr(`{"formula":"Upper ( label )","result_type":"Text","stored":false}`),
	})
	require.NoError(t, err)

	// A file saved before such lists were refused.
	file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "VL Restore"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	listName := fmt.Sprintf("Shouts %d", stamp)
	bundle.ValueLists = append(bundle.ValueLists, schema.BundleValueList{
		ID: fmt.Sprintf("vlr-%d", stamp), Name: listName, Kind: schema.ValueListFromField,
		SourceTableID: &tbl.ID, SourceColumnID: &shout.ID,
	})
	file, err = msgpack.Marshal(bundle)
	require.NoError(t, err)

	// Restored once (created), then again (updated): kept both times.
	for _, restore := range []string{"created", "updated"} {
		report, err := svc.ImportSolution(ctx, file)
		require.NoError(t, err, "restoring must not fail on a list that was valid when saved (%s)", restore)
		var warned bool
		for _, w := range report.Warnings {
			if strings.Contains(w, listName) && strings.Contains(w, "cannot be indexed") {
				warned = true
			}
		}
		assert.True(t, warned, "the import report says the list offers nothing (%s): %v", restore, report.Warnings)
	}

	lists, err := svc.ListValueLists(ctx)
	require.NoError(t, err)
	var kept *schema.ValueList
	for i := range lists {
		if lists[i].Name == listName {
			kept = &lists[i]
		}
	}
	require.NotNil(t, kept, "the list is restored as it was saved")
	t.Cleanup(func() { _ = svc.DeleteValueList(context.Background(), kept.ID) })
	require.NotNil(t, kept.SourceColumnID)
	assert.Equal(t, shout.ID, *kept.SourceColumnID)

	// Defining one is still refused.
	_, err = svc.UpdateValueList(ctx, kept.ID, schema.ValueListInput{
		Name: listName, Kind: schema.ValueListFromField, SourceTableID: &tbl.ID, SourceColumnID: &shout.ID,
	})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
}

func ptr(s string) *string { return &s }
