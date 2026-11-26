package fmsql

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Field is a field of a table occurrence, as the query may name it: by what
// the solution calls it (DisplayName, a FileMaker field's name) or by its
// column (Name), whatever their case.
type Field struct {
	Name        string
	DisplayName string
	// Type is what the column holds: TEXT, NUMBER, DATE, TIMESTAMP or
	// BOOLEAN; a calculation is the type of its result.
	Type dbal.AgnosticFieldType
	// Unreadable says why a query cannot read the field (an unstored
	// calculation, a summary field, a container), empty when it can.
	Unreadable string
	// Key is the record's own id, read by its column name only, and left
	// out of *.
	Key bool
}

// Table is the base table of a table occurrence: its table in the database
// and its fields, in the order they were created.
type Table struct {
	Name   string
	Fields []Field
}

// Catalog resolves the table occurrences a query names. Occurrence answers
// nil and no error for a name the solution has no occurrence of, and an
// error, such as the account not being allowed to read the table, that the
// query is refused for.
type Catalog interface {
	Occurrence(name string) (*Table, error)
}

// Options bound what a query may return.
type Options struct {
	// MaxRows is the most rows a query may answer; the statement fetches
	// one more, so a caller can tell a longer answer and refuse it.
	MaxRows int
}

// Query is the statement to run, its parameters, and the kind of each
// column it answers, to Format them.
type Query struct {
	SQL     string
	Args    []any
	Columns []Kind
	// Limit is how many rows the statement fetches at most: MaxRows + 1
	// unless the query fetches fewer.
	Limit int64
}

// maxTables bounds the tables one query may join.
const maxTables = 16

type bound struct {
	name  string // the alias, or the occurrence's name, as written
	table *Table
	sql   string // the alias the statement gives it: t1, t2…
}

type translator struct {
	src    string
	d      dbal.Dialect
	pg     bool
	args   []Value
	params []param
	// kinds keeps the kind of each expression once found: finding the kind
	// of a + asks for its sides' more than once.
	kinds map[Expr]kindResult
	all   []*bound // every table of the query
	scope []*bound // the tables a name may be resolved against
	// clause is where an expression is: what refuses an aggregate there.
	clause      string
	inAggregate bool
}

// Translate builds the statement of a parsed query: names resolved against
// catalog, args bound to its ? in order, and nothing of the query's text in
// the statement.
func Translate(stmt *Select, catalog Catalog, d dbal.Dialect, args []Value, opts Options) (*Query, error) {
	t := &translator{src: stmt.src, d: d, args: args, kinds: map[Expr]kindResult{}}
	switch d.Engine() {
	case dbal.EnginePostgres:
		t.pg = true
	case dbal.EngineMariaDB:
	default:
		return nil, t.fail(0, codeSyntax, "ExecuteSQL does not run on the %s engine.", d.Engine())
	}
	if stmt.Params > len(args) {
		return nil, t.fail(0, codeArgumentCount, "The query has %d ? but %d argument(s) were given.", stmt.Params, len(args))
	}
	if opts.MaxRows < 1 {
		opts.MaxRows = 1
	}

	from, err := t.from(stmt.From, catalog)
	if err != nil {
		return nil, err
	}
	t.scope = t.all

	// The columns of the answer, * expanded.
	type column struct {
		expr  Expr
		alias string
	}
	var columns []column
	for _, item := range stmt.Items {
		if !item.Star {
			columns = append(columns, column{expr: item.Expr, alias: item.Alias})
			continue
		}
		tables := t.all
		if item.StarTable != "" {
			b := t.lookup(item.StarTable)
			if b == nil {
				return nil, t.fail(item.pos, codeNoTableInScope, "The table named %q does not exist in the column reference's scope.", item.StarTable)
			}
			tables = []*bound{b}
		}
		for _, b := range tables {
			for _, f := range b.table.Fields {
				if f.Key || f.Unreadable != "" {
					continue
				}
				columns = append(columns, column{expr: &Column{Table: b.name, Name: f.Name, pos: item.pos,
					star: &starField{table: b, field: f}}})
			}
		}
	}
	if len(columns) == 0 {
		return nil, t.fail(0, codeNoColumn, "The query selects no column that can be read.")
	}

	// A query is grouped when it says GROUP BY or HAVING, or totals rows.
	grouped := len(stmt.GroupBy) > 0 || stmt.Having != nil
	for _, c := range columns {
		grouped = grouped || hasAggregate(c.expr)
	}
	for _, o := range stmt.OrderBy {
		grouped = grouped || hasAggregate(o.Expr)
	}
	groupKeys := map[string]bool{}
	for _, g := range stmt.GroupBy {
		if isConstant(g) {
			return nil, t.fail(g.position(), codeSyntax, "There is an error in the syntax of the query: GROUP BY names columns, not constants.")
		}
		groupKeys[t.key(g)] = true
	}
	covered := func(e Expr) error {
		if !grouped {
			return nil
		}
		return t.checkGrouped(e, groupKeys)
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	if stmt.Distinct {
		sb.WriteString("DISTINCT ")
	}
	q := &Query{}
	t.clause = "SELECT"
	for i, c := range columns {
		kind, err := t.kindOf(c.expr)
		if err != nil {
			return nil, err
		}
		if err := covered(c.expr); err != nil {
			return nil, err
		}
		if kind == kindCondition {
			return nil, t.fail(c.expr.position(), codeIncompatibleTypes, "An expression contains incompatible data types: a column of the result cannot be a condition.")
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		s, err := t.emit(c.expr, kind, ordered)
		if err != nil {
			return nil, err
		}
		sb.WriteString(s)
		if kind == KindNull {
			kind = KindText
		}
		q.Columns = append(q.Columns, kind)
	}

	sb.WriteString(" FROM ")
	sb.WriteString(from)

	if stmt.Where != nil {
		t.clause = "WHERE"
		s, err := t.condition(stmt.Where)
		if err != nil {
			return nil, err
		}
		sb.WriteString(" WHERE ")
		sb.WriteString(s)
	}
	if len(stmt.GroupBy) > 0 {
		t.clause = "GROUP BY"
		sb.WriteString(" GROUP BY ")
		for i, g := range stmt.GroupBy {
			kind, err := t.kindOf(g)
			if err != nil {
				return nil, err
			}
			if kind == kindCondition {
				return nil, t.fail(g.position(), codeIncompatibleTypes, "An expression contains incompatible data types: GROUP BY cannot group by a condition.")
			}
			s, err := t.emit(g, kind, 0)
			if err != nil {
				return nil, err
			}
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(s)
		}
	}
	if stmt.Having != nil {
		t.clause = "HAVING"
		s, err := t.condition(stmt.Having)
		if err != nil {
			return nil, err
		}
		if err := covered(stmt.Having); err != nil {
			return nil, err
		}
		sb.WriteString(" HAVING ")
		sb.WriteString(s)
	}
	if len(stmt.OrderBy) > 0 {
		t.clause = "ORDER BY"
		sb.WriteString(" ORDER BY ")
		for i, o := range stmt.OrderBy {
			if i > 0 {
				sb.WriteString(", ")
			}
			ordinal := 0
			switch e := o.Expr.(type) {
			case *Literal:
				n, err := strconv.Atoi(e.Value.Text)
				if e.Value.Kind != KindNumber || err != nil || n < 1 || n > len(columns) {
					return nil, t.fail(e.pos, codeBadOrdinal, "The ordinal reference %q in the ORDER BY clause is not valid.", e.Value.Text)
				}
				ordinal = n
			case *Column:
				if e.Table == "" {
					for j, c := range columns {
						if c.alias != "" && strings.EqualFold(c.alias, e.Name) {
							ordinal = j + 1
							break
						}
					}
				}
			}
			var s string
			if ordinal > 0 {
				s = strconv.Itoa(ordinal)
			} else {
				if isConstant(o.Expr) {
					return nil, t.fail(o.Expr.position(), codeSyntax, "There is an error in the syntax of the query: ORDER BY sorts by columns, not constants.")
				}
				if _, err := t.kindOf(o.Expr); err != nil {
					return nil, err
				}
				if err := covered(o.Expr); err != nil {
					return nil, err
				}
				if stmt.Distinct {
					// It sorts by the column selected, written as its
					// number so that it is the same expression.
					key := t.key(o.Expr)
					for j, c := range columns {
						if t.key(c.expr) == key {
							ordinal = j + 1
							break
						}
					}
					if ordinal == 0 {
						return nil, t.fail(o.Expr.position(), codeSyntax, "There is an error in the syntax of the query: "+
							"with SELECT DISTINCT, ORDER BY sorts by the columns selected.")
					}
				}
				kind, err := t.kindOf(o.Expr)
				if err != nil {
					return nil, err
				}
				if kind == kindCondition {
					return nil, t.fail(o.Expr.position(), codeIncompatibleTypes, "An expression contains incompatible data types: ORDER BY cannot sort by a condition.")
				}
				if ordinal > 0 {
					s = strconv.Itoa(ordinal)
				} else if s, err = t.emit(o.Expr, kind, ordered); err != nil {
					return nil, err
				}
			}
			sb.WriteString(s)
			// Nothing sorts before every value, on both engines.
			switch {
			case o.Desc && t.pg:
				sb.WriteString(" DESC NULLS LAST")
			case o.Desc:
				sb.WriteString(" DESC")
			case t.pg:
				sb.WriteString(" ASC NULLS FIRST")
			default:
				sb.WriteString(" ASC")
			}
		}
	}
	q.Limit = int64(opts.MaxRows) + 1
	if stmt.Fetch != nil && *stmt.Fetch < q.Limit {
		q.Limit = *stmt.Fetch
	}
	sb.WriteString(" LIMIT " + strconv.FormatInt(q.Limit, 10))
	if stmt.Offset != nil {
		sb.WriteString(" OFFSET " + strconv.FormatInt(*stmt.Offset, 10))
	}
	q.SQL, q.Args = t.placeholders(sb.String())
	return q, nil
}

func (t *translator) fail(pos, code int, format string, args ...any) *Error {
	return errorAt(t.src, pos, code, format, args...)
}

// from resolves the FROM clause and writes it.
func (t *translator) from(items []FromItem, catalog Catalog) (string, error) {
	resolve := func(ref TableRef) (*bound, error) {
		if len(t.all) >= maxTables {
			return nil, t.fail(ref.pos, codeTooComplex, "The query is too complex. The maximum number of tables (%d) has been exceeded.", maxTables)
		}
		table, err := catalog.Occurrence(ref.Name)
		if err != nil {
			var fqlErr *Error
			if errors.As(err, &fqlErr) {
				return nil, err
			}
			return nil, t.fail(ref.pos, codeNoTable, "The table named %q cannot be read: %v.", ref.Name, err)
		}
		if table == nil {
			return nil, t.fail(ref.pos, codeNoTable, "The table named %q does not exist.", ref.Name)
		}
		name := ref.Name
		if ref.Alias != "" {
			name = ref.Alias
		}
		for _, b := range t.all {
			if strings.EqualFold(b.name, name) {
				return nil, t.fail(ref.pos, codeDuplicateTable, "The table named %q already exists in this query.", name)
			}
		}
		b := &bound{name: name, table: table, sql: "t" + strconv.Itoa(len(t.all)+1)}
		t.all = append(t.all, b)
		return b, nil
	}
	table := func(b *bound) string {
		return t.d.QuoteIdentifier(b.table.Name) + " " + t.d.QuoteIdentifier(b.sql)
	}
	var parts []string
	for _, item := range items {
		first, err := resolve(item.Table)
		if err != nil {
			return "", err
		}
		chain := []*bound{first}
		s := table(first)
		for _, j := range item.Joins {
			b, err := resolve(j.Table)
			if err != nil {
				return "", err
			}
			chain = append(chain, b)
			// ON sees the tables of its own chain, as the database does.
			t.scope, t.clause = chain, "ON"
			on, err := t.condition(j.On)
			if err != nil {
				return "", err
			}
			kind := " INNER JOIN "
			if j.Left {
				kind = " LEFT JOIN "
			}
			s += kind + table(b) + " ON " + on
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", "), nil
}

// lookup is the table a qualifier names: its alias, or the occurrence's
// name when it has none.
func (t *translator) lookup(name string) *bound {
	for _, b := range t.scope {
		if strings.EqualFold(b.name, name) {
			return b
		}
	}
	return nil
}

// field finds a field of a table by what the solution calls it, then by
// its column; the record id only by its column.
func (tb *Table) field(name string) ([]Field, bool) {
	var byDisplay, byName, key []Field
	for _, f := range tb.Fields {
		switch {
		case f.Key:
			if strings.EqualFold(f.Name, name) {
				key = append(key, f)
			}
		case strings.EqualFold(f.DisplayName, name):
			byDisplay = append(byDisplay, f)
		case strings.EqualFold(f.Name, name):
			byName = append(byName, f)
		}
	}
	for _, found := range [][]Field{byDisplay, byName, key} {
		if len(found) > 0 {
			return found, true
		}
	}
	return nil, false
}

// resolve is the table and field a column names.
func (t *translator) resolve(c *Column) (*bound, Field, error) {
	if c.star != nil {
		return c.star.table, c.star.field, nil
	}
	var candidates []*bound
	if c.Table != "" {
		b := t.lookup(c.Table)
		if b == nil {
			return nil, Field{}, t.fail(c.pos, codeNoTableInScope, "The table named %q does not exist in the column reference's scope.", c.Table)
		}
		candidates = []*bound{b}
	} else {
		candidates = t.scope
	}
	var found *bound
	var field Field
	for _, b := range candidates {
		fields, ok := b.table.field(c.Name)
		if !ok {
			continue
		}
		if found != nil || len(fields) > 1 {
			return nil, Field{}, t.fail(c.pos, codeAmbiguousColumn, "The column named %q appears in more than one table in the column reference's scope.", c.Name)
		}
		found, field = b, fields[0]
	}
	if found == nil {
		if c.Table != "" {
			return nil, Field{}, t.fail(c.pos, codeNoColumnInTable, "The column named %q does not exist in table %q.", c.Name, c.Table)
		}
		return nil, Field{}, t.fail(c.pos, codeNoColumn, "The column named %q does not exist in any table in the column reference's scope.", c.Name)
	}
	if field.Unreadable != "" {
		return nil, Field{}, t.fail(c.pos, codeNoColumn, "The column named %q cannot be read by ExecuteSQL: %s.", c.Name, field.Unreadable)
	}
	return found, field, nil
}

func fieldKind(f Field) (Kind, bool) {
	switch f.Type {
	case dbal.FieldTypeText:
		return KindText, true
	case dbal.FieldTypeNumber, dbal.FieldTypeBoolean:
		return KindNumber, true
	case dbal.FieldTypeDate:
		return KindDate, true
	case dbal.FieldTypeTimestamp:
		return KindTimestamp, true
	}
	return KindNull, false
}

func isConstant(e Expr) bool {
	switch e.(type) {
	case *Literal, *Param:
		return true
	}
	return false
}

// constant is the value of a literal or a ? argument. Empty text is
// nothing, as an empty field is in FileMaker.
func (t *translator) constant(e Expr) Value {
	var v Value
	switch c := e.(type) {
	case *Literal:
		v = c.Value
	case *Param:
		v = t.args[c.Index]
	}
	if v.Kind == KindText && v.Text == "" {
		return Value{Kind: KindNull}
	}
	return v
}

func hasAggregate(e Expr) bool {
	found := false
	walk(e, func(e Expr) bool {
		if c, ok := e.(*Call); ok && aggregates[c.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}

// walk visits e and, while visit answers true, what it is made of.
func walk(e Expr, visit func(Expr) bool) {
	if e == nil || !visit(e) {
		return
	}
	for _, child := range children(e) {
		walk(child, visit)
	}
}

func children(e Expr) []Expr {
	switch x := e.(type) {
	case *Unary:
		return []Expr{x.X}
	case *Binary:
		return []Expr{x.L, x.R}
	case *Not:
		return []Expr{x.X}
	case *Like:
		return []Expr{x.X, x.Pattern}
	case *Between:
		return []Expr{x.X, x.Lo, x.Hi}
	case *In:
		return append([]Expr{x.X}, x.List...)
	case *IsNull:
		return []Expr{x.X}
	case *Call:
		return x.Args
	}
	return nil
}

// key identifies an expression once its names are resolved, to tell that
// a column is one GROUP BY names.
func (t *translator) key(e Expr) string {
	switch x := e.(type) {
	case *Column:
		b, f, err := t.resolve(x)
		if err != nil {
			return "?" + x.Table + "." + x.Name
		}
		return "c:" + b.sql + "." + f.Name
	case *Literal:
		return "l:" + x.Value.Kind.String() + ":" + x.Value.Text
	case *Param:
		return "p:" + strconv.Itoa(x.Index)
	}
	var b strings.Builder
	switch x := e.(type) {
	case *Unary:
		b.WriteString("u" + x.Op)
	case *Binary:
		b.WriteString("b" + x.Op)
	case *Not:
		b.WriteString("not")
	case *Like:
		b.WriteString(fmt.Sprintf("like%v", x.Negated))
	case *Between:
		b.WriteString(fmt.Sprintf("between%v", x.Negated))
	case *In:
		b.WriteString(fmt.Sprintf("in%v", x.Negated))
	case *IsNull:
		b.WriteString(fmt.Sprintf("null%v", x.Negated))
	case *Call:
		b.WriteString(fmt.Sprintf("f:%s:%v:%v", x.Name, x.Distinct, x.Star))
	}
	b.WriteString("(")
	for i, c := range children(e) {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(t.key(c))
	}
	b.WriteString(")")
	return b.String()
}

// checkGrouped refuses a column of a grouped query that is neither grouped
// by nor totalled.
func (t *translator) checkGrouped(e Expr, groupKeys map[string]bool) error {
	if groupKeys[t.key(e)] {
		return nil
	}
	switch x := e.(type) {
	case *Call:
		if aggregates[x.Name] {
			return nil
		}
	case *Column:
		return t.fail(x.pos, codeNotGrouped, "All non-aggregated column references in the SELECT list and HAVING clause must be in the GROUP BY clause.")
	}
	for _, c := range children(e) {
		if err := t.checkGrouped(c, groupKeys); err != nil {
			return err
		}
	}
	return nil
}

type kindResult struct {
	kind Kind
	err  error
}

// kindOf is the kind of an expression, checking its names and its types.
func (t *translator) kindOf(e Expr) (Kind, error) {
	if r, ok := t.kinds[e]; ok {
		return r.kind, r.err
	}
	k, err := t.findKind(e)
	t.kinds[e] = kindResult{k, err}
	return k, err
}

func (t *translator) findKind(e Expr) (Kind, error) {
	switch x := e.(type) {
	case *Column:
		_, f, err := t.resolve(x)
		if err != nil {
			return 0, err
		}
		k, ok := fieldKind(f)
		if !ok {
			return 0, t.fail(x.pos, codeNoColumn, "The column named %q cannot be read by ExecuteSQL.", x.Name)
		}
		return k, nil
	case *Literal, *Param:
		return base(t.constant(e).Kind), nil
	case *Unary:
		if _, err := t.unify(x.pos, []Expr{x.X}, KindNumber, false); err != nil {
			return 0, err
		}
		return KindNumber, nil
	case *Binary:
		switch x.Op {
		case "AND", "OR":
			for _, side := range []Expr{x.L, x.R} {
				if err := t.isCondition(side); err != nil {
					return 0, err
				}
			}
			return kindCondition, nil
		case "=", "<>", "<", ">", "<=", ">=":
			if _, err := t.unify(x.pos, []Expr{x.L, x.R}, KindNull, x.Op != "=" && x.Op != "<>"); err != nil {
				return 0, err
			}
			return kindCondition, nil
		case "||":
			if _, err := t.unify(x.pos, []Expr{x.L, x.R}, KindText, false); err != nil {
				return 0, err
			}
			return KindText, nil
		case "+":
			if t.concatenates(x) {
				return KindText, nil
			}
			fallthrough
		default:
			if _, err := t.unify(x.pos, []Expr{x.L, x.R}, KindNumber, false); err != nil {
				return 0, err
			}
			return KindNumber, nil
		}
	case *Not:
		if err := t.isCondition(x.X); err != nil {
			return 0, err
		}
		return kindCondition, nil
	case *Like:
		if _, err := t.unify(x.pos, []Expr{x.X}, KindText, false); err != nil {
			return 0, err
		}
		if !isConstant(x.Pattern) {
			return 0, t.fail(x.Pattern.position(), codeIncompatibleTypes, "An expression contains incompatible data types: the pattern of LIKE must be a text constant or a ?.")
		}
		if _, ok := convert(t.constant(x.Pattern), KindText); !ok {
			return 0, t.fail(x.Pattern.position(), codeIncompatibleTypes, "An expression contains incompatible data types.")
		}
		return kindCondition, nil
	case *Between:
		if _, err := t.unify(x.pos, []Expr{x.X, x.Lo, x.Hi}, KindNull, true); err != nil {
			return 0, err
		}
		return kindCondition, nil
	case *In:
		if _, err := t.unify(x.pos, append([]Expr{x.X}, x.List...), KindNull, false); err != nil {
			return 0, err
		}
		return kindCondition, nil
	case *IsNull:
		k, err := t.kindOf(x.X)
		if err != nil {
			return 0, err
		}
		if k == kindCondition {
			return 0, t.fail(x.pos, codeIncompatibleTypes, "An expression contains incompatible data types.")
		}
		return kindCondition, nil
	case *Call:
		return t.callKind(x)
	}
	return 0, t.fail(e.position(), codeSyntax, "There is an error in the syntax of the query.")
}

func (t *translator) isCondition(e Expr) error {
	k, err := t.kindOf(e)
	if err != nil {
		return err
	}
	if k != kindCondition {
		return t.fail(e.position(), codeNotPredicate, "Predicate must contain a logical operation (=, <, OR, AND, IS NULL, ...).")
	}
	return nil
}

// concatenates reports whether a + joins text, as FileMaker's SQL lets it:
// when one side is a text that is not a constant, or both are text.
func (t *translator) concatenates(x *Binary) bool {
	texts, fields := 0, 0
	for _, side := range []Expr{x.L, x.R} {
		if k, err := t.kindOf(side); err == nil && k == KindText {
			texts++
			if !isConstant(side) {
				fields++
			}
		}
	}
	return texts == 2 || fields == 1
}

// unify finds the kind several expressions compare as, or the kind want
// they must be when it is set. A constant takes the kind of what it is
// compared with; anything else must already be of that kind. ordered asks
// for a kind that sorts.
func (t *translator) unify(pos int, exprs []Expr, want Kind, ordered bool) (Kind, error) {
	target := want
	kinds := make([]Kind, len(exprs))
	for i, e := range exprs {
		k, err := t.kindOf(e)
		if err != nil {
			return 0, err
		}
		if k == kindCondition {
			return 0, t.fail(e.position(), codeIncompatibleTypes, "An expression contains incompatible data types: a condition is not a value.")
		}
		kinds[i] = k
		if target == KindNull && !isConstant(e) && k != KindNull {
			target = k
		}
	}
	if target == KindNull {
		for i, e := range exprs {
			if isConstant(e) && kinds[i] != KindNull {
				target = kinds[i]
				break
			}
		}
	}
	for i, e := range exprs {
		if isConstant(e) {
			if _, ok := convert(t.constant(e), target); !ok {
				code := codeIncompatible
				if target == KindDate || target == KindTimestamp {
					code = codeBadTemporal
					return 0, t.fail(e.position(), code, "The literal value %q is not a valid DATE, TIME or TIMESTAMP.", t.constant(e).Text)
				}
				return 0, t.fail(e.position(), code, "Incompatible types in assignment: %q is not a %s.", t.constant(e).Text, target)
			}
			continue
		}
		if kinds[i] != target && kinds[i] != KindNull {
			code := codeNotComparable
			if want != KindNull {
				code = codeIncompatibleTypes
				return 0, t.fail(e.position(), code, "An expression contains incompatible data types: a %s where a %s is needed.", kinds[i], target)
			}
			return 0, t.fail(pos, code, "An expression contains data types that cannot be compared: %s and %s.", target, kinds[i])
		}
	}
	return target, nil
}

// scalars are the functions of one row the subset has, by name: the kind of
// their arguments (KindNull: any, the same for all) and of their answer.
var scalars = map[string]struct {
	min, max int
	arg, out Kind
	sql      string
}{
	"UPPER":       {1, 1, KindText, KindText, "UPPER"},
	"LOWER":       {1, 1, KindText, KindText, "LOWER"},
	"TRIM":        {1, 1, KindText, KindText, "TRIM"},
	"LTRIM":       {1, 1, KindText, KindText, "LTRIM"},
	"RTRIM":       {1, 1, KindText, KindText, "RTRIM"},
	"LENGTH":      {1, 1, KindText, KindNumber, "CHAR_LENGTH"},
	"CHAR_LENGTH": {1, 1, KindText, KindNumber, "CHAR_LENGTH"},
	"ABS":         {1, 1, KindNumber, KindNumber, "ABS"},
	"COALESCE":    {2, -1, KindNull, KindNull, "COALESCE"},
}

func (t *translator) callKind(c *Call) (Kind, error) {
	if aggregates[c.Name] {
		if t.inAggregate {
			return 0, t.fail(c.pos, codeAggregate, "Expressions involving aggregations are not supported: %s inside an aggregate.", c.Name)
		}
		switch t.clause {
		case "WHERE", "ON", "GROUP BY":
			return 0, t.fail(c.pos, codeAggregateInWhere, "Aggregation expressions are not allowed in the %s clause.", t.clause)
		}
		if c.Star {
			return KindNumber, nil
		}
		if len(c.Args) != 1 {
			return 0, t.fail(c.pos, codeArgumentCount, "An invalid number of parameters was supplied to the function %q", c.Name)
		}
		t.inAggregate = true
		defer func() { t.inAggregate = false }()
		want := KindNull
		if c.Name == "SUM" || c.Name == "AVG" {
			want = KindNumber
		}
		k, err := t.unify(c.pos, c.Args, want, c.Name == "MIN" || c.Name == "MAX")
		if err != nil {
			return 0, err
		}
		if c.Name == "COUNT" {
			return KindNumber, nil
		}
		return k, nil
	}
	fn, ok := scalars[c.Name]
	if !ok {
		return 0, t.fail(c.pos, codeInvalidFunction, "%q is an invalid function: File4Base does not run it in ExecuteSQL.", c.Name)
	}
	if len(c.Args) < fn.min || fn.max >= 0 && len(c.Args) > fn.max {
		return 0, t.fail(c.pos, codeArgumentCount, "An invalid number of parameters was supplied to the function %q", c.Name)
	}
	k, err := t.unify(c.pos, c.Args, fn.arg, false)
	if err != nil {
		return 0, err
	}
	if fn.out != KindNull {
		return fn.out, nil
	}
	return k, nil
}

// How an expression is written.
const (
	// ordered sorts text by its characters' code points, as FileMaker's
	// SQL does, whatever the database's collation.
	ordered = 1 << iota
)

// condition writes a test.
func (t *translator) condition(e Expr) (string, error) {
	if err := t.isCondition(e); err != nil {
		return "", err
	}
	return t.emit(e, kindCondition, 0)
}

// emit writes an expression whose kind is known, a constant as a parameter
// of the kind want.
func (t *translator) emit(e Expr, want Kind, how int) (string, error) {
	switch x := e.(type) {
	case *Column:
		b, f, err := t.resolve(x)
		if err != nil {
			return "", err
		}
		s := t.d.QuoteIdentifier(b.sql) + "." + t.d.QuoteIdentifier(f.Name)
		switch f.Type {
		case dbal.FieldTypeText:
			if !t.pg {
				s += " COLLATE " + mariaCollation
			}
			// An empty text is nothing, as an empty field is in FileMaker.
			return t.collate("NULLIF("+s+", '')", how), nil
		case dbal.FieldTypeBoolean:
			if t.pg {
				return "(CASE WHEN " + s + " THEN 1 WHEN NOT " + s + " THEN 0 END)", nil
			}
		}
		return s, nil
	case *Literal, *Param:
		v, ok := convert(t.constant(e), want)
		if !ok {
			return "", t.fail(e.position(), codeIncompatible, "Incompatible types in assignment.")
		}
		return t.collateKind(t.bind(v), base(v.Kind), how), nil
	case *Unary:
		s, err := t.emit(x.X, KindNumber, 0)
		if err != nil {
			return "", err
		}
		if x.Op == "-" {
			return "(-" + s + ")", nil
		}
		return s, nil
	case *Binary:
		return t.binary(x, how)
	case *Not:
		s, err := t.emit(x.X, kindCondition, 0)
		return "(NOT " + s + ")", err
	case *Like:
		s, err := t.emit(x.X, KindText, 0)
		if err != nil {
			return "", err
		}
		pattern, _ := convert(t.constant(x.Pattern), KindText)
		if pattern.Kind != KindNull {
			// Only % and _ are wildcards: the escape character is doubled.
			pattern.Text = strings.ReplaceAll(pattern.Text, "!", "!!")
		}
		op := " LIKE "
		if x.Negated {
			op = " NOT LIKE "
		}
		return "(" + s + op + t.bind(pattern) + " ESCAPE '!')", nil
	case *Between:
		k, err := t.unify(x.pos, []Expr{x.X, x.Lo, x.Hi}, KindNull, true)
		if err != nil {
			return "", err
		}
		parts := make([]string, 3)
		for i, side := range []Expr{x.X, x.Lo, x.Hi} {
			if parts[i], err = t.emit(side, k, ordered); err != nil {
				return "", err
			}
		}
		op := " BETWEEN "
		if x.Negated {
			op = " NOT BETWEEN "
		}
		return "(" + parts[0] + op + parts[1] + " AND " + parts[2] + ")", nil
	case *In:
		all := append([]Expr{x.X}, x.List...)
		k, err := t.unify(x.pos, all, KindNull, false)
		if err != nil {
			return "", err
		}
		left, err := t.emit(x.X, k, 0)
		if err != nil {
			return "", err
		}
		items := make([]string, len(x.List))
		for i, item := range x.List {
			if items[i], err = t.emit(item, k, 0); err != nil {
				return "", err
			}
		}
		op := " IN ("
		if x.Negated {
			op = " NOT IN ("
		}
		return "(" + left + op + strings.Join(items, ", ") + "))", nil
	case *IsNull:
		k, err := t.kindOf(x.X)
		if err != nil {
			return "", err
		}
		s, err := t.emit(x.X, k, 0)
		if err != nil {
			return "", err
		}
		if x.Negated {
			return "(" + s + " IS NOT NULL)", nil
		}
		return "(" + s + " IS NULL)", nil
	case *Call:
		return t.call(x, how)
	}
	return "", t.fail(e.position(), codeSyntax, "There is an error in the syntax of the query.")
}

func (t *translator) binary(x *Binary, how int) (string, error) {
	switch x.Op {
	case "AND", "OR":
		l, err := t.emit(x.L, kindCondition, 0)
		if err != nil {
			return "", err
		}
		r, err := t.emit(x.R, kindCondition, 0)
		if err != nil {
			return "", err
		}
		return "(" + l + " " + x.Op + " " + r + ")", nil
	case "=", "<>", "<", ">", "<=", ">=":
		sorted := x.Op != "=" && x.Op != "<>"
		k, err := t.unify(x.pos, []Expr{x.L, x.R}, KindNull, sorted)
		if err != nil {
			return "", err
		}
		lhow, rhow := 0, 0
		if sorted {
			lhow, rhow = ordered, ordered
		}
		l, err := t.emit(x.L, k, lhow)
		if err != nil {
			return "", err
		}
		r, err := t.emit(x.R, k, rhow)
		if err != nil {
			return "", err
		}
		return "(" + l + " " + x.Op + " " + r + ")", nil
	}
	kind, err := t.kindOf(x)
	if err != nil {
		return "", err
	}
	l, err := t.emit(x.L, kind, 0)
	if err != nil {
		return "", err
	}
	r, err := t.emit(x.R, kind, 0)
	if err != nil {
		return "", err
	}
	if kind == KindText {
		// An empty operand adds nothing, as FileMaker joins text; joining
		// two empty ones is empty, so nothing.
		l, r = "COALESCE("+l+", '')", "COALESCE("+r+", '')"
		if t.pg {
			return t.collate("NULLIF("+l+" || "+r+", '')", how), nil
		}
		return "NULLIF(CONCAT(" + l + ", " + r + "), '')", nil
	}
	if x.Op == "/" {
		// A division by zero is nothing, on both engines; PostgreSQL would
		// divide two whole numbers, as two counts, to a whole number.
		if t.pg {
			l = "CAST(" + l + " AS NUMERIC)"
		}
		return "(" + l + " / NULLIF(" + r + ", 0))", nil
	}
	return "(" + l + " " + x.Op + " " + r + ")", nil
}

func (t *translator) call(c *Call, how int) (string, error) {
	kind, err := t.callKind(c)
	if err != nil {
		return "", err
	}
	if c.Star {
		return "COUNT(*)", nil
	}
	argKind := kind
	if aggregates[c.Name] {
		t.inAggregate = true
		defer func() { t.inAggregate = false }()
		if c.Name == "COUNT" {
			if argKind, err = t.kindOf(c.Args[0]); err != nil {
				return "", err
			}
		}
		argHow := 0
		if c.Name == "MIN" || c.Name == "MAX" {
			argHow = ordered
		}
		arg, err := t.emit(c.Args[0], argKind, argHow)
		if err != nil {
			return "", err
		}
		distinct := ""
		if c.Distinct {
			distinct = "DISTINCT "
		}
		return t.collateKind(c.Name+"("+distinct+arg+")", kind, how), nil
	}
	fn := scalars[c.Name]
	if fn.arg != KindNull {
		argKind = fn.arg
	}
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		if args[i], err = t.emit(a, argKind, 0); err != nil {
			return "", err
		}
	}
	return t.collateKind(fn.sql+"("+strings.Join(args, ", ")+")", kind, how), nil
}

// collateKind is collate for an expression of kind k.
func (t *translator) collateKind(s string, k Kind, how int) string {
	if k != KindText {
		return s
	}
	return t.collate(s, how)
}

// collate makes a text expression sort by code point on PostgreSQL, where
// ordered asks for it; MariaDB already compares every text as binary.
func (t *translator) collate(s string, how int) string {
	if !t.pg || how&ordered == 0 {
		return s
	}
	return "(" + s + `) COLLATE "C"`
}

// bind writes a constant as a parameter of its kind.
func (t *translator) bind(v Value) string {
	if v.Kind == KindNull {
		return "NULL"
	}
	var arg any = v.Text
	var typ string
	switch base(v.Kind) {
	case KindNumber:
		typ = "NUMERIC"
		if !t.pg {
			typ = "DECIMAL(65, 30)"
		}
	case KindDate:
		typ = "DATE"
	case KindTimestamp:
		at, _ := time.Parse(timestampLayout, v.Text)
		at = at.UTC()
		arg = dbal.TemporalValue(t.d.Engine(), dbal.FieldTypeTimestamp, at)
		typ = "TIMESTAMPTZ"
		if !t.pg {
			typ = "DATETIME(6)"
		}
	default:
		typ = "TEXT"
		if !t.pg {
			typ = "CHAR CHARACTER SET utf8mb4"
		}
	}
	t.params = append(t.params, param{arg: arg, key: typ + "\x00" + v.Text})
	s := "CAST(" + paramMark + strconv.Itoa(len(t.params)-1) + paramMark + " AS " + typ + ")"
	if base(v.Kind) == KindText && !t.pg {
		s += " COLLATE " + mariaCollation
	}
	return s
}

// mariaCollation compares text by code point, trailing spaces included, as
// PostgreSQL's "C" does: utf8mb4_bin would ignore them.
const mariaCollation = "utf8mb4_nopad_bin"

// A param is a value bound to the statement: key is its type and value.
type param struct {
	arg any
	key string
}

// paramMark encloses, in the statement being written, the number of a
// value bound; no text of the query is written in a statement, so nothing
// else holds it.
const paramMark = "\x00"

// placeholders writes the dialect's placeholders where values are bound,
// with the values in the order the statement reads them, however its
// clauses were written. PostgreSQL binds the same value of the same type
// once, so that an expression written twice, in SELECT DISTINCT and ORDER
// BY, or GROUP BY and SELECT, is the same expression.
func (t *translator) placeholders(statement string) (string, []any) {
	parts := strings.Split(statement, paramMark)
	var sb strings.Builder
	var args []any
	numbers := map[string]int{}
	for i, part := range parts {
		if i%2 == 0 {
			sb.WriteString(part)
			continue
		}
		n, _ := strconv.Atoi(part)
		p := t.params[n]
		if t.pg {
			if at, ok := numbers[p.key]; ok {
				sb.WriteString(t.d.Placeholder(at))
				continue
			}
		}
		args = append(args, p.arg)
		numbers[p.key] = len(args)
		sb.WriteString(t.d.Placeholder(len(args)))
	}
	return sb.String(), args
}
