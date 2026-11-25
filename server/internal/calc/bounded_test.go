package calc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boundedLines answers Lines::Amount as related records 3, "", 5 and 2, and
// records how each was asked for.
type boundedLines struct {
	RecordContext
	totals bool // answer aggregates from totals
	asked  []string
}

func (b *boundedLines) Related(_, _ string) ([]any, error) {
	b.asked = append(b.asked, "all")
	return []any{"3", "", "5", "2"}, nil
}

func (b *boundedLines) RelatedFirst(_, _ string) ([]any, error) {
	b.asked = append(b.asked, "first")
	return []any{"3"}, nil
}

func (b *boundedLines) RelatedTotals(_, _, function string) (RelatedTotals, bool, error) {
	b.asked = append(b.asked, "totals "+function)
	if !b.totals {
		return RelatedTotals{}, false, nil
	}
	return RelatedTotals{Records: 4, Values: 3, Sum: "10", Min: "2", Max: "5"}, true, nil
}

// A Bounded context is asked for no more than a formula needs, and an
// aggregate answered from totals is what it is from every value.
func TestBoundedContextReadsNoFurtherThanNeeded(t *testing.T) {
	cases := []struct{ formula, want, asked string }{
		{"Lines::Amount", "3", "first"},
		{"Sum ( Lines::Amount )", "10", "totals SUM"},
		{"Count ( Lines::Amount )", "3", "totals COUNT"},
		{"Average ( Lines::Amount )", "3.3333333333333333", "totals AVERAGE"},
		{"Min ( Lines::Amount )", "2", "totals MIN"},
		{"Max ( Lines::Amount )", "5", "totals MAX"},
		{"List ( Lines::Amount )", "3\r5\r2", "all"},
		{"Last ( Lines::Amount )", "2", "all"},
		{"GetNthRecord ( Lines::Amount ; 3 )", "5", "all"},
		{"Sum ( Lines::Amount ; 1 )", "4", "first"},
	}
	for _, c := range cases {
		for _, totals := range []bool{true, false} {
			expr, err := Parse(c.formula)
			require.NoError(t, err, c.formula)
			ctx := &boundedLines{totals: totals}
			got, err := EvaluateWith(expr, ctx, ResultText)
			require.NoError(t, err, c.formula)
			if got == nil {
				got = ""
			}
			assert.Equal(t, c.want, got, "%s, totals %t", c.formula, totals)
			if totals || !strings.HasPrefix(c.asked, "totals") {
				assert.Equal(t, []string{c.asked}, ctx.asked, c.formula)
			} else {
				assert.Equal(t, []string{c.asked, "all"}, ctx.asked, "%s reads every value when there are no totals", c.formula)
			}
		}
	}
}

// Sum of related records whose field is empty in every one is empty; of no
// related record, 0.
func TestTotalsOfEmptyValues(t *testing.T) {
	for _, c := range []struct {
		totals RelatedTotals
		want   any
	}{
		{RelatedTotals{Records: 2}, nil},
		{RelatedTotals{}, "0"},
	} {
		expr, err := Parse("Sum ( Lines::Amount )")
		require.NoError(t, err)
		got, err := EvaluateWith(expr, &fixedTotals{totals: c.totals}, ResultText)
		require.NoError(t, err)
		assert.Equal(t, c.want, got)
	}
}

type fixedTotals struct {
	RecordContext
	totals RelatedTotals
}

func (f *fixedTotals) RelatedFirst(_, _ string) ([]any, error) { return nil, nil }

func (f *fixedTotals) RelatedTotals(_, _, _ string) (RelatedTotals, bool, error) {
	return f.totals, true, nil
}

// RelatedReads tells how a formula reads each related field.
func TestRelatedReads(t *testing.T) {
	expr, err := Parse(`Lines::Amount + Sum ( Lines::Amount ) + Count ( Lines::Code ) & List ( Lines::Code ) & ` +
		`GetNthRecord ( Lines::Note ; Lines::Rank ) & Sum ( Lines::Amount ; Lines::Tax ) & Lines::Amount[2]`)
	require.NoError(t, err)
	type read struct {
		field    string
		use      RelatedUse
		function string
	}
	var got []read
	for _, r := range expr.RelatedReads() {
		assert.Equal(t, "Lines", r.Table)
		got = append(got, read{r.Field, r.Use, r.Function})
	}
	assert.Equal(t, []read{
		{"Amount", UseFirst, ""},
		{"Amount", UseTotals, "SUM"},
		{"Code", UseTotals, "COUNT"},
		{"Code", UseAll, ""},
		{"Note", UseAll, ""},
		{"Rank", UseFirst, ""},
		{"Tax", UseFirst, ""},
	}, got)
}
