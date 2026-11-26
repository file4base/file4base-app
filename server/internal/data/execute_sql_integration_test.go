package data_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/require"
)

// sqlSolution is two tables, staff and departments, with occurrences of the
// names FileMaker would give them, and synthetic records.
type sqlSolution struct {
	schemaSvc          *schema.Service
	dataSvc            *data.Service
	driver             dbal.DatabaseDriver
	staff, departments string // physical table names
	staffID            string
	staffOcc, deptOcc  string // occurrence names
	ada                string // a record id of staff
}

func newSQLSolution(t *testing.T) (*sqlSolution, context.Context) {
	t.Helper()
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return nil, nil
	}
	s := &sqlSolution{schemaSvc: schema.NewService(driver), dataSvc: data.NewService(driver), driver: driver}
	require.NoError(t, s.schemaSvc.EnsureSystemTables(ctx))
	stamp := uniqueTestStamp()
	s.staff, s.departments = fmt.Sprintf("sql_staff_%d", stamp), fmt.Sprintf("sql_departments_%d", stamp)
	fields := map[string][]schema.ColumnMetadata{
		s.staff: {
			{Name: "last_name", DisplayName: "Last Name", FieldType: dbal.FieldTypeText, IsNullable: true},
			{Name: "dept", DisplayName: "Dept", FieldType: dbal.FieldTypeText, IsNullable: true},
			{Name: "salary", DisplayName: "Salary", FieldType: dbal.FieldTypeNumber, IsNullable: true},
			{Name: "hired", DisplayName: "Hired", FieldType: dbal.FieldTypeDate, IsNullable: true},
			{Name: "clock_in", DisplayName: "Clock In", FieldType: dbal.FieldTypeTimestamp, IsNullable: true},
			{Name: "seen", DisplayName: "Seen", FieldType: dbal.FieldTypeTimestamp, IsNullable: true},
		},
		s.departments: {
			{Name: "code", DisplayName: "Code", FieldType: dbal.FieldTypeText, IsNullable: true},
			{Name: "label", DisplayName: "Label", FieldType: dbal.FieldTypeText, IsNullable: true},
		},
	}
	ids := map[string]string{}
	for _, name := range []string{s.staff, s.departments} {
		tbl, err := s.schemaSvc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		id := tbl.ID
		ids[name] = id
		t.Cleanup(func() { _ = s.schemaSvc.DeleteTable(context.Background(), id) })
		for _, f := range fields[name] {
			_, err := s.schemaSvc.AddColumn(ctx, id, f)
			require.NoError(t, err)
		}
	}
	s.staffID = ids[s.staff]
	occurrences, err := s.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	for _, o := range occurrences {
		switch o.BaseTableID {
		case ids[s.staff]:
			s.staffOcc = o.Name
		case ids[s.departments]:
			s.deptOcc = o.Name
		}
	}
	require.NotEmpty(t, s.staffOcc)
	require.NotEmpty(t, s.deptOcc)

	for _, r := range []map[string]interface{}{
		{"last_name": "Lovelace", "dept": "R&D", "salary": 5200.5, "hired": "2024-03-01",
			"clock_in": "0001-01-01T08:30:00Z", "seen": "2026-01-05T13:04:05Z"},
		{"last_name": "Hopper", "dept": "R&D", "salary": 6100, "hired": "2021-09-15"},
		{"last_name": "Noether", "dept": "Ops", "salary": 4800, "hired": "2023-01-10"},
		{"last_name": "anonymous", "dept": "r&d", "salary": 1},
		{"last_name": "Blank"},
	} {
		row, err := s.dataSvc.InsertRow(ctx, s.staff, r)
		require.NoError(t, err)
		if r["last_name"] == "Lovelace" {
			s.ada = fmt.Sprint(row["id"])
		}
	}
	for _, r := range []map[string]interface{}{
		{"code": "R&D", "label": "Research"},
		{"code": "Ops", "label": "Operations"},
	} {
		_, err := s.dataSvc.InsertRow(ctx, s.departments, r)
		require.NoError(t, err)
	}
	return s, ctx
}

// q quotes an occurrence name for a query written in a formula's string.
func q(name string) string { return `\"` + name + `\"` }

// evaluate is a formula as a script's Set Variable step has the server
// compute it: with the script's variables, through EvaluateExpressionsIn,
// as the /evaluate endpoint does.
func (s *sqlSolution) evaluate(t *testing.T, ctx context.Context, svc *data.Service, formula string, vars map[string]any) *data.Evaluation {
	t.Helper()
	results, err := svc.EvaluateExpressionsIn(ctx, s.staff, "", s.ada, []string{formula}, "Text",
		data.EvaluationEnv{Variables: vars})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err, formula)
	return results[0].Evaluation
}

// ExecuteSQL in an unstored calculation field answers from the records as
// they are when the record is read; a stored one is refused.
func TestExecuteSQLCalculationField(t *testing.T) {
	s, ctx := newSQLSolution(t)
	if s == nil {
		return
	}
	formula := fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*), SUM(Salary) FROM %s WHERE Dept = ?" ; "/" ; "" ; dept )`, q(s.staffOcc))
	_, err := s.schemaSvc.AddColumn(ctx, s.staffID, schema.ColumnMetadata{Name: "colleagues", DisplayName: "Colleagues",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: unstoredCalcJSON(formula, "Text")})
	require.NoError(t, err)

	row, err := s.dataSvc.GetRow(ctx, s.staff, s.ada)
	require.NoError(t, err)
	require.Equal(t, "2/11300.5", row["colleagues"])

	_, err = s.dataSvc.InsertRow(ctx, s.staff, map[string]interface{}{"last_name": "Turing", "dept": "R&D", "salary": 100})
	require.NoError(t, err)
	row, err = s.dataSvc.GetRow(ctx, s.staff, s.ada)
	require.NoError(t, err)
	require.Equal(t, "3/11400.5", row["colleagues"], "read again when the record is read")

	_, err = s.schemaSvc.AddColumn(ctx, s.staffID, schema.ColumnMetadata{Name: "stored_sql", DisplayName: "Stored SQL",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: calcJSON(formula, "Text")})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	require.ErrorContains(t, err, "ExecuteSQL")
}

// ExecuteSQL in a script's Set Variable step: joins, grouping, ordering,
// the types written as SQL writes them, and separators.
func TestExecuteSQLSetVariable(t *testing.T) {
	s, ctx := newSQLSolution(t)
	if s == nil {
		return
	}
	cases := []struct{ formula, want string }{{
		fmt.Sprintf(`ExecuteSQL ( "SELECT \"Last Name\", Salary, Hired, \"Clock In\", Seen FROM %s WHERE Dept = ? ORDER BY \"Last Name\"" ; " | " ; "¶" ; $dept )`, q(s.staffOcc)),
		"Hopper | 6100 | 2021-09-15 |  | \rLovelace | 5200.5 | 2024-03-01 | 08:30:00 | 2026-01-05 13:04:05",
	}, {
		// Default separators; text compares case-sensitively, as FileMaker's.
		fmt.Sprintf(`ExecuteSQL ( "select s.\"last name\" from %s s where s.dept = 'r&d'" ; "" ; "" )`, q(s.staffOcc)),
		"anonymous",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT d.Label, COUNT(*) FROM %s s JOIN %s d ON d.Code = s.Dept GROUP BY d.Label ORDER BY 2 DESC, 1" ; ", " ; "; " )`,
			q(s.staffOcc), q(s.deptOcc)),
		"Research, 2; Operations, 1",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT s.\"Last Name\", d.Label FROM %s s LEFT JOIN %s d ON d.Code = s.Dept WHERE s.Salary < 5000 OR s.Salary IS NULL ORDER BY 1 FETCH FIRST 2 ROWS ONLY" ; "=" ; "&" )`,
			q(s.staffOcc), q(s.deptOcc)),
		"Blank=&Noether=Operations",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT \"Last Name\" FROM %s WHERE Hired BETWEEN ? AND DATE '2024-12-31' AND \"Clock In\" = ? AND Seen > ?" ; "" ; "" ; Date ( 1 ; 1 ; 2024 ) ; Time ( 8 ; 30 ; 0 ) ; "2026-01-01 00:00:00" )`,
			q(s.staffOcc)),
		"Lovelace",
	}, {
		// Nothing found is empty; an empty field is NULL.
		fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*) FROM %s WHERE Dept IS NULL" ; "" ; "" )`, q(s.staffOcc)),
		"1",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT \"Last Name\" FROM %s WHERE \"Last Name\" LIKE ? ORDER BY 1 DESC" ; "" ; "" ; "%%o%%" )`, q(s.staffOcc)),
		"anonymous\rNoether\rLovelace\rHopper",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT \"Last Name\" FROM %s WHERE Dept = 'Nowhere'" ; "" ; "" )`, q(s.staffOcc)),
		"",
	}, {
		fmt.Sprintf(`ExecuteSQLe ( "SELECT Nope FROM %s" ; "" ; "" )`, q(s.staffOcc)),
		`? ERROR: FQL0007/(1:7): The column named "Nope" does not exist in any table in the column reference's scope.`,
	}, {
		`ExecuteSQL ( "SELECT * FROM NoSuchTable" ; "" ; "" )`,
		"?",
	}, {
		// Trailing spaces count, as in FileMaker; a count over a count is
		// not a whole division; empty fields are in neither IN nor NOT IN.
		fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*) FROM %s WHERE Dept = 'Ops '" ; "" ; "" )`, q(s.staffOcc)),
		"0",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(Dept) / COUNT(*), SUM(Salary) / 2 FROM %s" ; "" ; "" )`, q(s.staffOcc)),
		"0.8,8050.75",
	}, {
		fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*) FROM %s WHERE Dept NOT IN ('Ops')" ; "" ; "" )`, q(s.staffOcc)),
		"3",
	}, {
		// Arguments are bound in the order of their ?, whatever the clause.
		fmt.Sprintf(`ExecuteSQL ( "SELECT ?, d.Label FROM %s s JOIN %s d ON d.Code = ? WHERE s.\"Last Name\" = ?" ; "" ; "" ; "x" ; "Ops" ; "Hopper" )`,
			q(s.staffOcc), q(s.deptOcc)),
		"x,Operations",
	}}
	for _, c := range cases {
		ev := s.evaluate(t, ctx, s.dataSvc, `Let ( $list = `+c.formula+` ; $list )`, map[string]any{"$dept": "R&D"})
		require.Equal(t, c.want, ev.Text, c.formula)
		if c.want != "" {
			require.Equal(t, c.want, ev.Variables["$list"], c.formula)
		}
	}
}

// A table the account may not read answers "?", never its records, as a
// related field reads <No Access>.
func TestExecuteSQLRespectsAccess(t *testing.T) {
	s, ctx := newSQLSolution(t)
	if s == nil {
		return
	}
	formula := fmt.Sprintf(`ExecuteSQL ( "SELECT Label FROM %s ORDER BY 1" ; "" ; "," )`, q(s.deptOcc))
	_, err := s.schemaSvc.AddColumn(ctx, s.staffID, schema.ColumnMetadata{Name: "labels", DisplayName: "Labels",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: unstoredCalcJSON(formula, "Text")})
	require.NoError(t, err)

	row, err := s.dataSvc.GetRow(ctx, s.staff, s.ada)
	require.NoError(t, err)
	require.Equal(t, "Operations,Research", row["labels"])

	restricted := data.NewService(s.driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		return table != s.departments, nil
	})
	row, err = restricted.GetRow(ctx, s.staff, s.ada)
	require.NoError(t, err)
	require.Equal(t, "?", row["labels"])

	require.Equal(t, "?", s.evaluate(t, ctx, restricted, formula, nil).Text)
	joined := fmt.Sprintf(`ExecuteSQLe ( "SELECT s.Dept FROM %s s JOIN %s d ON d.Code = s.Dept" ; "" ; "" )`, q(s.staffOcc), q(s.deptOcc))
	answer := s.evaluate(t, ctx, restricted, joined, nil).Text
	require.Contains(t, answer, "? ERROR: FQL0002/")
	require.Contains(t, answer, "<No Access>")
	require.NotContains(t, answer, "Research")
	// The tables it may read still answer.
	own := fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*) FROM %s" ; "" ; "" )`, q(s.staffOcc))
	require.Equal(t, "5", s.evaluate(t, ctx, restricted, own, nil).Text)
}

// Neither a query nor its arguments can reach beyond the solution's
// tables, nor change them.
func TestExecuteSQLInjection(t *testing.T) {
	s, ctx := newSQLSolution(t)
	if s == nil {
		return
	}
	count := fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*) FROM %s" ; "" ; "" )`, q(s.staffOcc))
	for _, formula := range []string{
		fmt.Sprintf(`ExecuteSQL ( "SELECT * FROM %s; DROP TABLE %s" ; "" ; "" )`, q(s.staffOcc), s.departments),
		fmt.Sprintf(`ExecuteSQL ( "DELETE FROM %s" ; "" ; "" )`, q(s.staffOcc)),
		fmt.Sprintf(`ExecuteSQL ( "SELECT * FROM %s" ; "" ; "" )`, s.departments+`\"; DROP TABLE `+s.departments+` --`),
		`ExecuteSQL ( "SELECT name FROM sys_tables" ; "" ; "" )`,
		`ExecuteSQL ( "SELECT username FROM sys_users" ; "" ; "" )`,
		fmt.Sprintf(`ExecuteSQL ( "SELECT \"Last Name\" FROM %s UNION SELECT Label FROM %s" ; "" ; "" )`, q(s.staffOcc), q(s.deptOcc)),
		fmt.Sprintf(`ExecuteSQL ( "SELECT pg_sleep(10), SLEEP(10) FROM %s" ; "" ; "" )`, q(s.staffOcc)),
	} {
		require.Equal(t, "?", s.evaluate(t, ctx, s.dataSvc, formula, nil).Text, formula)
	}
	for _, hostile := range []string{`x' OR '1'='1`, `R&D'; DROP TABLE ` + s.departments + `; --`, `%' OR 1=1 --`} {
		formula := fmt.Sprintf(`ExecuteSQL ( "SELECT \"Last Name\" FROM %s WHERE Dept = ? OR \"Last Name\" LIKE ?" ; "" ; "" ; $x ; $x )`, q(s.staffOcc))
		require.Equal(t, "", s.evaluate(t, ctx, s.dataSvc, formula, map[string]any{"$x": hostile}).Text, hostile)
	}
	require.Equal(t, "5", s.evaluate(t, ctx, s.dataSvc, count, nil).Text)
	labels := fmt.Sprintf(`ExecuteSQL ( "SELECT COUNT(*) FROM %s" ; "" ; "" )`, q(s.deptOcc))
	require.Equal(t, "2", s.evaluate(t, ctx, s.dataSvc, labels, nil).Text, "the departments are still there")
}
