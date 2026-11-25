package data

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Sorting related records by an unstored calculation.
//
// An unstored calculation holds no value in its column: it is computed when a
// record is read, so the engine cannot sort by it. A portal, or a read of a
// relationship's records, whose sort uses one is sorted here instead, as
// FileMaker sorts it: every related record is read in creation order, up to
// maxRelatedRecords (more is refused with ErrTooManyRelated, as for a
// calculation reading them), its unstored calculations are computed, and the
// records are sorted (stably, so records that sort alike stay in creation
// order) before the page is taken. A read of that kind costs the
// calculations of every related record, not of one page.
//
// A found set, which may hold any number of records, is still never sorted by
// an unstored calculation (ErrUnstoredField), nor are the related records a
// calculation reads through a relationship sorted by one.

// unstoredSort is the order of a read, when any of its fields is an unstored
// calculation of tableName: the read is then sorted here rather than by the
// engine.
func (s *Service) unstoredSort(ctx context.Context, tableName string, order []SortField) ([]SortField, error) {
	if len(order) == 0 {
		return nil, nil
	}
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return nil, err
	}
	fields := sortFields(order)
	if slices.ContainsFunc(calculations, func(c calculation) bool { return c.unstored && slices.Contains(fields, c.field) }) {
		return order, nil
	}
	return nil, nil
}

// read reads the related records of rows, with their unstored calculations
// computed. A read sorted here has at most maxRelatedRecords of them.
func (q *relatedQuery) read(ctx context.Context, rows *sql.Rows) ([]map[string]interface{}, error) {
	if q.sorted == nil {
		return q.dst.readRows(ctx, q.target.TableName, rows, q.fields)
	}
	records, err := rowsToMaps(rows, q.fields)
	if err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(records) > maxRelatedRecords {
		return nil, wrapSide(q.target, fmt.Errorf("%w through %s to sort them by %s, which is computed only when a record is shown; "+
			"narrow the relationship, or store the calculation's result in Field Options",
			ErrTooManyRelated, q.rel.Name, strings.Join(sortFields(q.sorted), ", ")))
	}
	if err := q.dst.fillUnstored(ctx, q.target.TableName, records); err != nil {
		return nil, err
	}
	return records, nil
}

// sortPage sorts the related records of one record in hand and keeps the
// page asked for.
func (q *relatedQuery) sortPage(records []map[string]interface{}) []map[string]interface{} {
	sortRecords(q.dst.driver.Dialect().Engine(), q.fields, q.sorted, records)
	if q.offset >= len(records) {
		return []map[string]interface{}{}
	}
	return records[q.offset:min(len(records), q.offset+q.limit)]
}

// sortRecords sorts records by order, stably, the way the engine sorts the
// same values: numbers as numbers, dates and timestamps in time, text as a
// FileMaker sort orders it (calc.CompareText), and an empty value where the
// engine puts a NULL (last in ascending order on PostgreSQL, first on
// MariaDB).
func sortRecords(engine dbal.EngineType, fields fieldSet, order []SortField, records []map[string]interface{}) {
	emptyFirst := engine == dbal.EngineMariaDB
	slices.SortStableFunc(records, func(a, b map[string]interface{}) int {
		for _, level := range order {
			c := compareSortValues(a[level.Field], b[level.Field], fields[level.Field], emptyFirst)
			if level.Descending {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		return 0
	})
}

// compareSortValues orders two values of a field of type kind.
func compareSortValues(a, b interface{}, kind dbal.AgnosticFieldType, emptyFirst bool) int {
	ea, eb := emptySortValue(a), emptySortValue(b)
	switch {
	case ea && eb:
		return 0
	case ea != eb:
		if ea == emptyFirst {
			return -1
		}
		return 1
	}
	switch kind {
	case dbal.FieldTypeNumber:
		x, okA := new(big.Rat).SetString(fmt.Sprint(textOf(a)))
		y, okB := new(big.Rat).SetString(fmt.Sprint(textOf(b)))
		if okA && okB {
			return x.Cmp(y)
		}
	case dbal.FieldTypeDate, dbal.FieldTypeTimestamp:
		x, okA := a.(time.Time)
		y, okB := b.(time.Time)
		if okA && okB {
			return x.Compare(y)
		}
	case dbal.FieldTypeBoolean:
		x, okA := a.(bool)
		y, okB := b.(bool)
		if okA && okB {
			switch {
			case x == y:
				return 0
			case !x:
				return -1
			}
			return 1
		}
	}
	return calc.CompareText(fmt.Sprint(textOf(a)), fmt.Sprint(textOf(b)))
}

func emptySortValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []byte:
		return len(t) == 0
	}
	return false
}
