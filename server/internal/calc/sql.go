package calc

import (
	"strings"
	"time"
)

// SQL is a Context that runs the queries of ExecuteSQL, as the account the
// formula is computed for, with that account's access: what an unstored
// calculation, an auto-enter or validation calculation and a script's
// formula have on the server. A Context without it answers "?".
type SQL interface {
	// ExecuteSQL runs query with args bound to its ? in order, and answers
	// each row's values written as text, or why the query cannot be run.
	ExecuteSQL(query string, args []SQLArgument) ([][]string, error)
}

// SQLArgument is a value given to ExecuteSQL for a ?, written as SQL writes
// it: a number as 12.5, a date as 2026-01-05, a time as 13:04:05 and a
// timestamp as 2026-01-05 13:04:05.
type SQLArgument struct {
	// Kind is "empty", "text", "number", "date", "time" or "timestamp".
	Kind string
	Text string
}

func sqlFunctions() map[string]function {
	return map[string]function{
		// ExecuteSQL ( query ; fieldSeparator ; rowSeparator { ; arguments… } )
		// answers "?" for a query that cannot be read or run, as FileMaker
		// does; ExecuteSQLe answers "? ERROR: " and why.
		"EXECUTESQL": {minArgs: 3, maxArgs: -1, call: func(ev *evaluator, _ callNode, a []value) (value, error) {
			return ev.executeSQL(a, false), nil
		}},
		"EXECUTESQLE": {minArgs: 3, maxArgs: -1, call: func(ev *evaluator, _ callNode, a []value) (value, error) {
			return ev.executeSQL(a, true), nil
		}},
	}
}

// maxSQLAnswer is the longest text ExecuteSQL answers, separators included.
const maxSQLAnswer = 32 << 20

func (ev *evaluator) executeSQL(a []value, detailed bool) value {
	refused := func(why string) value {
		if detailed {
			return textValue("? ERROR: " + why)
		}
		return textValue("?")
	}
	runner, ok := ev.ctx.(SQL)
	if !ok {
		return refused("ExecuteSQL cannot run here: a query is run by a calculation computed when it is shown " +
			"(unstored), an auto-enter or validation calculation, or a script")
	}
	fields, rows := a[1].asText(ev.loc), a[2].asText(ev.loc)
	if fields == "" {
		fields = ","
	}
	if rows == "" {
		rows = pilcrow
	}
	args := make([]SQLArgument, 0, len(a)-3)
	for _, v := range a[3:] {
		args = append(args, sqlArgument(v, ev.loc))
	}
	answer, err := runner.ExecuteSQL(a[0].asText(ev.loc), args)
	if err != nil {
		return refused(err.Error())
	}
	if len(answer) == 0 {
		return empty
	}
	size := 0
	for _, row := range answer {
		for _, cell := range row {
			size += len(cell) + len(fields)
		}
		size += len(rows)
	}
	if size > maxSQLAnswer {
		return refused("the answer, with its separators, is longer than 32 MB")
	}
	var b strings.Builder
	b.Grow(size)
	for i, row := range answer {
		if i > 0 {
			b.WriteString(rows)
		}
		for j, cell := range row {
			if j > 0 {
				b.WriteString(fields)
			}
			b.WriteString(cell)
		}
	}
	return textValue(b.String())
}

// sqlArgument is a value as ExecuteSQL binds it: with its type, so a number
// compares as a number and a date as a date.
func sqlArgument(v value, loc *Locale) SQLArgument {
	switch {
	case v.box != nil:
		return SQLArgument{Kind: "text", Text: v.asText(loc)}
	case v.kind == kindText:
		return SQLArgument{Kind: "text", Text: v.text}
	case v.kind == kindNumber:
		return SQLArgument{Kind: "number", Text: v.num.String()}
	case v.kind == kindDate:
		y, m, d := civil(v.num.IntPart())
		return SQLArgument{Kind: "date", Text: time.Date(int(y), time.Month(m), int(d), 0, 0, 0, 0, time.UTC).Format("2006-01-02")}
	case v.kind == kindTime:
		return SQLArgument{Kind: "time", Text: ISOLocale.formatTime(v.num)}
	case v.kind == kindTimestamp:
		return SQLArgument{Kind: "timestamp", Text: toTime(v.num).Format("2006-01-02 15:04:05.999999999")}
	}
	return SQLArgument{Kind: "empty"}
}

// RunsSQL reports whether the formula, or a custom function it calls, runs
// a query with ExecuteSQL: it reads other records, so what it answers
// changes without its own record changing.
func (e *Expr) RunsSQL() bool {
	for _, name := range e.Functions() {
		if name == "EXECUTESQL" || name == "EXECUTESQLE" {
			return true
		}
	}
	return false
}
