package data

import (
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/stretchr/testify/assert"
)

// Records sorted once read are sorted the way the engine sorts the same
// values: numbers as numbers, dates in time, text ignoring case and accents
// first, an empty value where the engine puts a NULL, and records that sort
// alike in the order they were read.
func TestSortRecords(t *testing.T) {
	fields := fieldSet{"n": dbal.FieldTypeNumber, "day": dbal.FieldTypeDate, "label": dbal.FieldTypeText}
	ids := func(records []map[string]interface{}) []string {
		var out []string
		for _, r := range records {
			out = append(out, fmt.Sprint(r["id"]))
		}
		return out
	}
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	records := func() []map[string]interface{} {
		return []map[string]interface{}{
			{"id": "a", "n": "10", "day": day(3), "label": "éclair"},
			{"id": "b", "n": "9", "day": nil, "label": "Banana"},
			{"id": "c", "n": nil, "day": day(1), "label": "apple"},
			{"id": "d", "n": "9.5", "day": day(2), "label": "banana"},
			{"id": "e", "n": "9", "day": day(1), "label": ""},
		}
	}

	r := records()
	sortRecords(dbal.EnginePostgres, fields, []SortField{{Field: "n"}}, r)
	assert.Equal(t, []string{"b", "e", "d", "a", "c"}, ids(r), "numbers as numbers, ties in order, empty last on PostgreSQL")
	r = records()
	sortRecords(dbal.EngineMariaDB, fields, []SortField{{Field: "n"}}, r)
	assert.Equal(t, []string{"c", "b", "e", "d", "a"}, ids(r), "empty first on MariaDB")
	r = records()
	sortRecords(dbal.EnginePostgres, fields, []SortField{{Field: "n", Descending: true}}, r)
	assert.Equal(t, []string{"c", "a", "d", "b", "e"}, ids(r), "descending, ties still in order")
	r = records()
	sortRecords(dbal.EnginePostgres, fields, []SortField{{Field: "day"}, {Field: "label", Descending: true}}, r)
	assert.Equal(t, []string{"e", "c", "d", "a", "b"}, ids(r), "dates in time, then the next level, an empty value first descending")
	r = records()
	sortRecords(dbal.EnginePostgres, fields, []SortField{{Field: "label"}}, r)
	assert.Equal(t, []string{"c", "d", "b", "a", "e"}, ids(r), "text ignoring case and accents first")
}
