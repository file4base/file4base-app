package data_test

import (
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSummarySpec(t *testing.T) {
	spec, err := data.ParseSummarySpec(`{"summary_type":"Total","field":" fee_paid ","running":true}`)
	require.NoError(t, err)
	assert.Equal(t, data.SummaryTotal, spec.Type, "the kind is read without regard to case")
	assert.Equal(t, "fee_paid", spec.Field, "and trimmed")
	assert.True(t, spec.Running)

	for _, broken := range []string{
		``,
		`   `,
		`not json`,
		`{"field":"fee_paid"}`, // no kind
		`{"summary_type":"median","field":"fee_paid"}`, // not a kind we compute
		`{"summary_type":"total"}`,                     // nothing to summarize
	} {
		_, err := data.ParseSummarySpec(broken)
		assert.ErrorIs(t, err, data.ErrInvalidSummary, "should refuse %q", broken)
	}
}

// A summary field defined before they were computed holds the shape the
// Field Options dialog used to save. It is read rather than ignored.
func TestParseSummarySpecReadsTheOlderShape(t *testing.T) {
	spec, err := data.ParseSummarySpec(
		`{"operation":"SUM","target_column":"fee_paid","running_total":true}`)
	require.NoError(t, err)
	assert.Equal(t, data.SummaryTotal, spec.Type)
	assert.Equal(t, "fee_paid", spec.Field)
	assert.True(t, spec.Running)

	for name, want := range map[string]data.SummaryType{
		"AVG": data.SummaryAverage, "COUNT": data.SummaryCount,
		"MIN": data.SummaryMinimum, "MAX": data.SummaryMaximum,
	} {
		spec, err := data.ParseSummarySpec(
			fmt.Sprintf(`{"operation":%q,"target_column":"fee_paid"}`, name))
		require.NoError(t, err, name)
		assert.Equal(t, want, spec.Type, name)
	}

	// An operation that was never offered is still refused rather than
	// quietly becoming a total.
	_, err = data.ParseSummarySpec(`{"operation":"MEDIAN","target_column":"fee_paid"}`)
	assert.ErrorIs(t, err, data.ErrInvalidSummary)
}

func TestSummarySpecRoundTrip(t *testing.T) {
	spec := data.SummarySpec{Type: data.SummaryAverage, Field: "fee_paid", Running: true}
	encoded, err := spec.Encode()
	require.NoError(t, err)

	back, err := data.ParseSummarySpec(encoded)
	require.NoError(t, err)
	assert.Equal(t, spec, *back)
}

func TestCheckSummarySpec(t *testing.T) {
	fields := map[string]dbal.AgnosticFieldType{
		"fee_paid":      dbal.FieldTypeNumber,
		"last_name":     dbal.FieldTypeText,
		"customer_type": dbal.FieldTypeText,
		"fee_total":     dbal.FieldTypeSummary,
	}

	t.Run("a total over a number field is fine", func(t *testing.T) {
		require.NoError(t, data.CheckSummarySpec(
			data.SummarySpec{Type: data.SummaryTotal, Field: "fee_paid"}, fields, "fee_total"))
	})

	t.Run("counting, minimum and maximum work over anything", func(t *testing.T) {
		for _, kind := range []data.SummaryType{data.SummaryCount, data.SummaryMinimum, data.SummaryMaximum} {
			assert.NoError(t, data.CheckSummarySpec(
				data.SummarySpec{Type: kind, Field: "last_name"}, fields, "s"), "%s over text", kind)
		}
	})

	t.Run("totalling text is refused", func(t *testing.T) {
		for _, kind := range []data.SummaryType{
			data.SummaryTotal, data.SummaryAverage, data.SummaryStandardDeviation, data.SummaryFractionOfTotal,
		} {
			err := data.CheckSummarySpec(data.SummarySpec{Type: kind, Field: "last_name"}, fields, "s")
			assert.ErrorIs(t, err, data.ErrInvalidSummary, "%s over text", kind)
		}
	})

	t.Run("a field the table does not have is refused", func(t *testing.T) {
		err := data.CheckSummarySpec(
			data.SummarySpec{Type: data.SummaryTotal, Field: "no_such_field"}, fields, "s")
		assert.ErrorIs(t, err, data.ErrInvalidSummary)
	})

	t.Run("a summary over a summary is refused", func(t *testing.T) {
		err := data.CheckSummarySpec(
			data.SummarySpec{Type: data.SummaryTotal, Field: "fee_total"}, fields, "s")
		assert.ErrorIs(t, err, data.ErrInvalidSummary,
			"a summary has no value in a record to total")
	})

	t.Run("a summary over itself is refused", func(t *testing.T) {
		err := data.CheckSummarySpec(
			data.SummarySpec{Type: data.SummaryTotal, Field: "fee_total"}, fields, "fee_total")
		assert.ErrorIs(t, err, data.ErrInvalidSummary)
	})
}
