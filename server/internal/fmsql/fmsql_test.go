package fmsql

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/stretchr/testify/require"
)

// catalog is a solution of two tables, each with one occurrence, and a
// second occurrence of the first.
type catalog map[string]*Table

func (c catalog) Occurrence(name string) (*Table, error) {
	if strings.EqualFold(name, "Secret") {
		return nil, errors.New("<No Access>")
	}
	for n, t := range c {
		if strings.EqualFold(n, name) {
			return t, nil
		}
	}
	return nil, nil
}

var testCatalog = func() catalog {
	employees := &Table{Name: "employees", Fields: []Field{
		{Name: "id", DisplayName: "ID", Type: dbal.FieldTypeText, Key: true},
		{Name: "emp_id", DisplayName: "EmpID", Type: dbal.FieldTypeNumber},
		{Name: "last_name", DisplayName: "Last Name", Type: dbal.FieldTypeText},
		{Name: "department", DisplayName: "Department", Type: dbal.FieldTypeText},
		{Name: "salary", DisplayName: "Salary", Type: dbal.FieldTypeNumber},
		{Name: "hired", DisplayName: "Hired", Type: dbal.FieldTypeDate},
		{Name: "starts", DisplayName: "Starts", Type: dbal.FieldTypeTimestamp},
		{Name: "active", DisplayName: "Active", Type: dbal.FieldTypeBoolean},
		{Name: "total", DisplayName: "Total", Type: dbal.FieldTypeNumber, Unreadable: "it is an unstored calculation"},
	}}
	departments := &Table{Name: "departments", Fields: []Field{
		{Name: "id", DisplayName: "ID", Type: dbal.FieldTypeText, Key: true},
		{Name: "name", DisplayName: "Name", Type: dbal.FieldTypeText},
		{Name: "floor", DisplayName: "Floor", Type: dbal.FieldTypeNumber},
	}}
	return catalog{"Employees": employees, "Staff": employees, "Departments": departments}
}()

func translate(t *testing.T, d dbal.Dialect, query string, args ...Value) (*Query, error) {
	t.Helper()
	stmt, err := Parse(query)
	if err != nil {
		return nil, err
	}
	return Translate(stmt, testCatalog, d, args, Options{MaxRows: 100})
}

// Every dialect a solution may run on.
var dialects = map[string]dbal.Dialect{"postgres": postgres.New(), "mariadb": mariadb.New()}

// A query is translated to the solution's own tables and columns, with
// every value bound as an argument.
func TestTranslate(t *testing.T) {
	cases := []struct {
		query    string
		args     []Value
		postgres string
		mariadb  string
		bound    []any
		columns  []Kind
		limit    int64
	}{{
		query: `SELECT "Last Name", Salary FROM Employees WHERE Department = ? ORDER BY "Last Name" DESC`,
		args:  []Value{{Kind: KindText, Text: "R&D"}},
		postgres: `SELECT (NULLIF("t1"."last_name", '')) COLLATE "C", "t1"."salary" FROM "employees" "t1" ` +
			`WHERE (NULLIF("t1"."department", '') = CAST($1 AS TEXT)) ORDER BY (NULLIF("t1"."last_name", '')) COLLATE "C" DESC NULLS LAST LIMIT 101`,
		mariadb: "SELECT NULLIF(`t1`.`last_name` COLLATE utf8mb4_nopad_bin, ''), `t1`.`salary` FROM `employees` `t1` " +
			"WHERE (NULLIF(`t1`.`department` COLLATE utf8mb4_nopad_bin, '') = CAST(? AS CHAR CHARACTER SET utf8mb4) COLLATE utf8mb4_nopad_bin) " +
			"ORDER BY NULLIF(`t1`.`last_name` COLLATE utf8mb4_nopad_bin, '') DESC LIMIT 101",
		bound:   []any{"R&D"},
		columns: []Kind{KindText, KindNumber},
		limit:   101,
	}, {
		// Names are found whatever their case, by the name FileMaker gave
		// them or the column's own; a constant compared with a date is one.
		query: `select E.EMP_ID, d.name from staff e left join DEPARTMENTS d on d.Name = e.department where e.hired >= '2020-01-01'`,
		postgres: `SELECT "t1"."emp_id", (NULLIF("t2"."name", '')) COLLATE "C" FROM "employees" "t1" ` +
			`LEFT JOIN "departments" "t2" ON (NULLIF("t2"."name", '') = NULLIF("t1"."department", '')) ` +
			`WHERE ("t1"."hired" >= CAST($1 AS DATE)) LIMIT 101`,
		mariadb: "SELECT `t1`.`emp_id`, NULLIF(`t2`.`name` COLLATE utf8mb4_nopad_bin, '') FROM `employees` `t1` " +
			"LEFT JOIN `departments` `t2` ON (NULLIF(`t2`.`name` COLLATE utf8mb4_nopad_bin, '') = NULLIF(`t1`.`department` COLLATE utf8mb4_nopad_bin, '')) " +
			"WHERE (`t1`.`hired` >= CAST(? AS DATE)) LIMIT 101",
		bound:   []any{"2020-01-01"},
		columns: []Kind{KindNumber, KindText},
		limit:   101,
	}, {
		query: `SELECT Department, SUM(Salary) FROM Employees GROUP BY Department HAVING COUNT(*) > 1 ORDER BY 2 DESC FETCH FIRST 3 ROWS ONLY`,
		postgres: `SELECT (NULLIF("t1"."department", '')) COLLATE "C", SUM("t1"."salary") FROM "employees" "t1" ` +
			`GROUP BY NULLIF("t1"."department", '') HAVING (COUNT(*) > CAST($1 AS NUMERIC)) ORDER BY 2 DESC NULLS LAST LIMIT 3`,
		mariadb: "SELECT NULLIF(`t1`.`department` COLLATE utf8mb4_nopad_bin, ''), SUM(`t1`.`salary`) FROM `employees` `t1` " +
			"GROUP BY NULLIF(`t1`.`department` COLLATE utf8mb4_nopad_bin, '') HAVING (COUNT(*) > CAST(? AS DECIMAL(65, 30))) ORDER BY 2 DESC LIMIT 3",
		bound:   []any{"1"},
		columns: []Kind{KindText, KindNumber},
		limit:   3,
	}, {
		// * reads the fields a query can read, without the record's key.
		query:    `SELECT * FROM Departments OFFSET 2 ROWS`,
		postgres: `SELECT (NULLIF("t1"."name", '')) COLLATE "C", "t1"."floor" FROM "departments" "t1" LIMIT 101 OFFSET 2`,
		mariadb:  "SELECT NULLIF(`t1`.`name` COLLATE utf8mb4_nopad_bin, ''), `t1`.`floor` FROM `departments` `t1` LIMIT 101 OFFSET 2",
		columns:  []Kind{KindText, KindNumber},
		limit:    101,
	}, {
		query: `SELECT Active FROM Employees WHERE Starts < TIMESTAMP '2026-01-01 08:00:00' AND NOT Salary BETWEEN 1 AND 2`,
		postgres: `SELECT (CASE WHEN "t1"."active" THEN 1 WHEN NOT "t1"."active" THEN 0 END) FROM "employees" "t1" ` +
			`WHERE (("t1"."starts" < CAST($1 AS TIMESTAMPTZ)) AND (NOT ("t1"."salary" BETWEEN CAST($2 AS NUMERIC) AND CAST($3 AS NUMERIC)))) LIMIT 101`,
		mariadb: "SELECT `t1`.`active` FROM `employees` `t1` " +
			"WHERE ((`t1`.`starts` < CAST(? AS DATETIME(6))) AND (NOT (`t1`.`salary` BETWEEN CAST(? AS DECIMAL(65, 30)) AND CAST(? AS DECIMAL(65, 30))))) LIMIT 101",
		columns: []Kind{KindNumber},
		limit:   101,
	}}
	for _, c := range cases {
		for name, d := range dialects {
			q, err := translate(t, d, c.query, c.args...)
			require.NoError(t, err, "%s: %s", name, c.query)
			want := c.postgres
			if name == "mariadb" {
				want = c.mariadb
			}
			require.Equal(t, want, q.SQL, "%s: %s", name, c.query)
			require.Equal(t, c.columns, q.Columns, c.query)
			require.Equal(t, c.limit, q.Limit, c.query)
			if c.bound != nil {
				require.Equal(t, c.bound, q.Args, "%s: %s", name, c.query)
			}
		}
	}
}

// What a query cannot do is refused with FileMaker's error, where it is.
func TestTranslateRefuses(t *testing.T) {
	cases := map[string]string{
		`SELECT Nope FROM Employees`:                                       `FQL0007/(1:7): The column named "Nope" does not exist`,
		"SELECT *\nFROM Employees\nWHERE Nope = 1":                         `FQL0007/(3:6):`,
		`SELECT x FROM`:                                                    `FQL0001/(1:13):`,
		`SELECT * FROM Nowhere`:                                            `FQL0002/(1:14): The table named "Nowhere" does not exist`,
		`SELECT * FROM Secret`:                                             `FQL0002/(1:14): The table named "Secret" cannot be read: <No Access>`,
		`SELECT * FROM Employees, Employees`:                               `FQL0003/`,
		`SELECT ID FROM Employees e, Departments d`:                        `FQL0006/`,
		`SELECT Total FROM Employees`:                                      `FQL0007/(1:7): The column named "Total" cannot be read by ExecuteSQL`,
		`SELECT d.Name FROM Employees e`:                                   `FQL0008/`,
		`SELECT e.Nope FROM Employees e`:                                   `FQL0009/`,
		`SELECT * FROM Employees WHERE Hired = DATE '2026-02-30'`:          `FQL0010/`,
		`SELECT * FROM Employees WHERE Hired = 'soon'`:                     `FQL0010/`,
		`SELECT * FROM Employees WHERE Salary`:                             `FQL0011/`,
		`SELECT * FROM Employees ORDER BY 9`:                               `FQL0012/`,
		`SELECT * FROM Employees WHERE Salary = 'abc'`:                     `FQL0013/`,
		`SELECT * FROM Employees WHERE Salary = Department`:                `FQL0017/`,
		`SELECT * FROM Employees WHERE Department LIKE Department`:         `FQL0018/`,
		`SELECT * FROM Employees WHERE Salary = ?`:                         `FQL0020/`,
		`SELECT Department, Salary FROM Employees GROUP BY Department`:     `FQL0025/`,
		`SELECT SUM(MAX(Salary)) FROM Employees`:                           `FQL0005/`,
		`SELECT * FROM Employees WHERE SUM(Salary) > 1`:                    `FQL0049/`,
		`SELECT SUBSTRING(Department, 1, 2) FROM Employees`:                `FQL0030/`,
		`SELECT * FROM Employees OFFSET -1 ROWS`:                           `FQL0050/`,
		`SELECT DISTINCT Department FROM Employees ORDER BY Salary`:        `FQL0001/`,
		`SELECT * FROM Employees UNION SELECT * FROM Departments`:          `UNION`,
		`SELECT * FROM Employees e RIGHT JOIN Departments d ON 1 = 1`:      `RIGHT OUTER JOIN`,
		`SELECT * FROM Employees e FULL JOIN Departments d ON 1 = 1`:       `FQL0001/`,
		`SELECT * FROM Employees e CROSS JOIN Departments d`:               `FQL0001/`,
		`SELECT * FROM Employees WHERE ID IN (SELECT ID FROM Departments)`: `subqueries`,
		`SELECT CASE WHEN 1 = 1 THEN 1 END FROM Employees`:                 `CASE`,
		`SELECT * FROM Employees LIMIT 1`:                                  `LIMIT`,
		`SELECT {d '2026-01-01'} FROM Employees`:                           `FQL0001/`,
		`DELETE FROM Employees`:                                            `SELECT statements only`,
		`UPDATE Employees SET Salary = 0`:                                  `SELECT statements only`,
		`SELECT "" FROM Employees`:                                         `a name is empty`,
		`SELECT 'open FROM Employees`:                                      `is not closed`,
	}
	for query, want := range cases {
		for name, d := range dialects {
			_, err := translate(t, d, query)
			require.Error(t, err, "%s: %s", name, query)
			var fql *Error
			require.True(t, errors.As(err, &fql), "%s: %s: %v", name, query, err)
			require.Contains(t, err.Error(), want, "%s: %s", name, query)
		}
	}
}

// A query's own text never reaches the database: what it says is either
// a name of the solution, replaced by the column's, or a value bound as an
// argument, and anything else is refused.
func TestTranslateInjection(t *testing.T) {
	refused := []string{
		`SELECT * FROM Employees; DROP TABLE employees`,
		`SELECT * FROM Employees WHERE Department = 'x'; DELETE FROM employees --'`,
		`SELECT * FROM "Employees; DROP TABLE employees"`,
		`SELECT "Last Name"" FROM employees; --" FROM Employees`,
		`SELECT * FROM Employees /* */ UNION ALL SELECT name, floor FROM pg_catalog.pg_tables`,
		`SELECT * FROM sys_users`,
		`SELECT pg_sleep(10) FROM Employees`,
		`SELECT SLEEP(10) FROM Employees`,
		`SELECT * FROM Employees e WHERE e.department = 'a' OR 1 = 1 INTO OUTFILE '/tmp/x'`,
		"SELECT * FROM Employees WHERE Department = `x`",
	}
	for _, query := range refused {
		for name, d := range dialects {
			_, err := translate(t, d, query)
			require.Error(t, err, "%s: %s", name, query)
		}
	}

	hostile := `x' OR '1'='1'; DROP TABLE employees; --`
	queries := []string{
		`SELECT "Last Name" FROM Employees WHERE Department = 'x'' OR ''1''=''1''; DROP TABLE employees; --'`,
		`SELECT "Last Name" FROM Employees WHERE Department = ?`,
		`SELECT "Last Name" FROM Employees WHERE Department LIKE ?`,
		`SELECT "Last Name" FROM Employees WHERE Department IN (?, 'a')`,
	}
	for _, query := range queries {
		for name, d := range dialects {
			q, err := translate(t, d, query, Value{Kind: KindText, Text: hostile})
			require.NoError(t, err, "%s: %s", name, query)
			require.NotContains(t, q.SQL, "DROP", "%s: %s", name, query)
			require.NotContains(t, q.SQL, "'1'", "%s: %s", name, query)
			require.Contains(t, q.Args, hostile, "%s: %s", name, query)
		}
	}
}

// LIKE matches % and _ as FileMaker does; its pattern escapes nothing else.
func TestTranslateLike(t *testing.T) {
	for name, d := range dialects {
		q, err := translate(t, d, `SELECT ID FROM Departments WHERE Name LIKE ?`, Value{Kind: KindText, Text: `50!\%`})
		require.NoError(t, err, name)
		require.Contains(t, q.SQL, "ESCAPE '!'", name)
		require.Equal(t, []any{`50!!\%`}, q.Args, name)
	}
}

// A ? is a value of the kind it was given as; a constant compared with a
// field is read as that field's type.
func TestTranslateArguments(t *testing.T) {
	q, err := translate(t, postgres.New(), `SELECT ID FROM Employees WHERE Hired = ? AND Salary > ? AND Starts > ?`,
		Value{Kind: KindDate, Text: "2026-01-05"}, Value{Kind: KindNumber, Text: "12.5"},
		Value{Kind: KindText, Text: "2026-01-05 13:04:05"})
	require.NoError(t, err)
	require.Len(t, q.Args, 3)
	require.Equal(t, "2026-01-05", q.Args[0])
	require.Equal(t, "12.5", q.Args[1])
	require.Contains(t, q.SQL, "CAST($3 AS TIMESTAMPTZ)")

	_, err = translate(t, postgres.New(), `SELECT ID FROM Employees WHERE Salary > ?`, Value{Kind: KindText, Text: "many"})
	require.ErrorContains(t, err, "FQL0013/")
	// An argument no ? takes is not read.
	q, err = translate(t, postgres.New(), `SELECT ID FROM Employees`, Value{Kind: KindText, Text: "spare"})
	require.NoError(t, err)
	require.Empty(t, q.Args)
}

// The answer is bounded: a query asks for one row more than the most it
// may answer, so that one answering more is seen.
func TestTranslateBoundsRows(t *testing.T) {
	stmt, err := Parse(`SELECT ID FROM Departments FETCH FIRST 1000000 ROWS ONLY`)
	require.NoError(t, err)
	q, err := Translate(stmt, testCatalog, postgres.New(), nil, Options{MaxRows: 10})
	require.NoError(t, err)
	require.Equal(t, int64(11), q.Limit)
	require.True(t, strings.HasSuffix(q.SQL, " LIMIT 11"), q.SQL)
}

func TestParseLimits(t *testing.T) {
	_, err := Parse(strings.Repeat("(", 500) + "1")
	require.Error(t, err)
	_, err = Parse("SELECT " + strings.Repeat("NOT ", 500) + "1 = 1 FROM Employees")
	require.Error(t, err)
	_, err = Parse("SELECT '" + strings.Repeat("x", maxQueryLength) + "' FROM Employees")
	require.Error(t, err)
	_, err = Parse(`SELECT ID FROM Employees -- a comment` + "\n" + `/* and another */ WHERE ID = 'a'`)
	require.NoError(t, err)
}

// Values are written as SQL writes them, whatever the locale.
func TestFormat(t *testing.T) {
	at := time.Date(2026, 1, 5, 13, 4, 5, 500_000_000, time.UTC)
	cases := []struct {
		kind Kind
		raw  any
		want string
	}{
		{KindNumber, int64(42), "42"},
		{KindNumber, []byte("12.500"), "12.5"},
		{KindNumber, "1e3", "1000"},
		{KindNumber, true, "1"},
		{KindNumber, 0.25, "0.25"},
		{KindNumber, nil, ""},
		{KindDate, at, "2026-01-05"},
		{KindDate, "2026-01-05T00:00:00Z", "2026-01-05"},
		{KindTimestamp, at, "2026-01-05 13:04:05.5"},
		{KindTimestamp, []byte("2026-01-05 13:04:05"), "2026-01-05 13:04:05"},
		{KindTime, time.Date(1, 1, 1, 8, 30, 0, 0, time.UTC), "08:30:00"},
		{KindText, []byte("Zoë"), "Zoë"},
	}
	for _, c := range cases {
		got, err := Format(c.kind, c.raw)
		require.NoError(t, err, "%v %v", c.kind, c.raw)
		require.Equal(t, c.want, got, "%v %v", c.kind, c.raw)
	}
	_, err := Format(KindNumber, "twelve")
	require.Error(t, err)
}

func TestArgument(t *testing.T) {
	for _, c := range []struct {
		kind Kind
		text string
		ok   bool
	}{
		{KindNumber, "12.5", true}, {KindNumber, "x", false},
		{KindDate, "2026-02-28", true}, {KindDate, "2026-02-30", false},
		{KindTime, "23:59:59", true}, {KindTime, "25:00:00", false},
		{KindTimestamp, "2026-01-05 13:04:05.25", true}, {KindTimestamp, "2026-01-05", false},
		{KindText, "anything", true}, {KindNull, "", true},
	} {
		_, ok := Argument(c.kind, c.text)
		require.Equal(t, c.ok, ok, "%v %q", c.kind, c.text)
	}
}

// MariaDB reads its ? in the order the statement has them, whatever order
// its clauses were translated in.
func TestTranslateBindsInStatementOrder(t *testing.T) {
	q, err := translate(t, mariadb.New(), `SELECT ?, d.Name FROM Employees e JOIN Departments d ON d.Name = ? WHERE e.Salary > ?`,
		Value{Kind: KindText, Text: "first"}, Value{Kind: KindText, Text: "second"}, Value{Kind: KindNumber, Text: "3"})
	require.NoError(t, err)
	require.Equal(t, []any{"first", "second", "3"}, q.Args)
	require.NotContains(t, q.SQL, "\x00")

	// PostgreSQL binds a value once, so that an expression written twice is
	// the same expression.
	q, err = translate(t, postgres.New(), `SELECT DISTINCT COALESCE(Department, '-') FROM Employees ORDER BY COALESCE(Department, '-')`)
	require.NoError(t, err)
	require.Equal(t, []any{"-"}, q.Args)
	require.Contains(t, q.SQL, " ORDER BY 1 ASC")
	q, err = translate(t, postgres.New(), `SELECT COALESCE(Department, '-'), COUNT(*) FROM Employees GROUP BY COALESCE(Department, '-')`)
	require.NoError(t, err)
	require.Equal(t, []any{"-"}, q.Args)
}

// What FileMaker computes, the translation does on both engines.
func TestTranslateSemantics(t *testing.T) {
	// A count divided by a count is not a whole division.
	q, err := translate(t, postgres.New(), `SELECT COUNT(Department) / COUNT(*) FROM Employees`)
	require.NoError(t, err)
	require.Contains(t, q.SQL, "(CAST(COUNT(NULLIF(\"t1\".\"department\", '')) AS NUMERIC) / NULLIF(COUNT(*), 0))")
	// An empty field is nothing, also where it is not equal to a value.
	for name, d := range dialects {
		q, err := translate(t, d, `SELECT ID FROM Departments WHERE Name NOT IN ('A') AND NOT Name = 'B'`)
		require.NoError(t, err, name)
		require.NotContains(t, strings.ReplaceAll(q.SQL, "NULLIF(", ""), `"t1"."name" NOT`, name)
		require.Equal(t, 3, strings.Count(q.SQL, "NULLIF("), "%s: %s", name, q.SQL)
	}
}

// Bounds of what a query may ask for.
func TestTranslateRefusesUnbounded(t *testing.T) {
	for query, want := range map[string]string{
		`SELECT * FROM Employees FETCH FIRST 0 ROWS ONLY`:                          "FQL0052/",
		`SELECT * FROM Employees WHERE Salary > 1e999999999`:                       "FQL0001/",
		`SELECT * FROM Employees WHERE Salary > 1e-999999999`:                      "FQL0001/",
		"SELECT *\r\nFROM Employees\r\nWHERE Nope = 1":                             "FQL0007/(3:6):",
		"SELECT ID FROM Employees WHERE " + strings.Repeat("1 + ", 6000) + "1 = 1": "FQL0004/",
	} {
		_, err := translate(t, postgres.New(), query)
		require.ErrorContains(t, err, want, query[:min(len(query), 60)])
	}
	_, err := translate(t, postgres.New(), `SELECT ID FROM Employees WHERE Salary > ?`, Value{Kind: KindText, Text: "1e999999999"})
	require.ErrorContains(t, err, "FQL0013/")
	_, ok := Argument(KindNumber, "1e999999999")
	require.False(t, ok)
}

// What translating costs stays small, whatever the query: a chain of
// operators as long as a query may be is refused, and the deepest one
// allowed, grouped, selected and sorted by, is translated at once.
func TestTranslateCostIsBounded(t *testing.T) {
	chain := func(n int) string { return strings.Repeat("Salary + ", n) + "Salary" }
	long := "SELECT COUNT(*) FROM Employees GROUP BY Salary HAVING " + chain(7000) + " > 0"
	require.Greater(t, len(long), 60<<10)
	start := time.Now()
	_, err := translate(t, postgres.New(), long)
	require.ErrorContains(t, err, "FQL0004/")
	_, err = translate(t, postgres.New(), "SELECT COUNT(*) FROM Employees GROUP BY Salary HAVING "+chain(4900)+" > 0")
	require.ErrorContains(t, err, "more than 400 operations deep")
	deep := chain(maxTreeDepth - 10)
	for _, query := range []string{
		"SELECT COUNT(*) FROM Employees GROUP BY " + deep + " HAVING " + deep + " > 0",
		"SELECT DISTINCT " + strings.Repeat(deep+", ", 5) + deep + " FROM Employees ORDER BY " + strings.Repeat(deep+", ", 5) + deep,
	} {
		for name, d := range dialects {
			_, err := translate(t, d, query)
			require.NoError(t, err, name)
		}
	}
	require.Less(t, time.Since(start), time.Second)
}

// + joins text only with text: beside a text field, a number field or a
// condition is refused, as || refuses them; a constant is read as text.
func TestTranslatePlusTypes(t *testing.T) {
	for name, d := range dialects {
		for query, want := range map[string]string{
			`SELECT "Last Name" + Salary FROM Employees`:       "FQL0018/(1:21)",
			`SELECT Salary + "Last Name" FROM Employees`:       "FQL0018/(1:7)",
			`SELECT "Last Name" + (Salary = 1) FROM Employees`: "FQL0018/",
			`SELECT "Last Name" + Hired FROM Employees`:        "FQL0018/",
			`SELECT Salary + 'many' FROM Employees`:            "FQL0013/",
			`SELECT "Last Name" || Salary FROM Employees`:      "FQL0018/",
		} {
			_, err := translate(t, d, query)
			require.ErrorContains(t, err, want, "%s: %s", name, query)
		}
		q, err := translate(t, d, `SELECT "Last Name" + 5, "Last Name" + Department, Salary + 5 FROM Employees`)
		require.NoError(t, err, name)
		require.Equal(t, []Kind{KindText, KindText, KindNumber}, q.Columns, name)
		require.Contains(t, q.Args, "5", name)
	}
}
