package data_test

import (
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// How a cell of text becomes a value. The rule throughout: an empty cell is
// nothing, and anything that will not convert is an error rather than a
// silently wrong number (#38).
func TestCoerce(t *testing.T) {
	coerce := data.CoerceForTest

	t.Run("an empty cell is nothing, never a zero", func(t *testing.T) {
		for _, fieldType := range []dbal.AgnosticFieldType{
			dbal.FieldTypeText, dbal.FieldTypeNumber, dbal.FieldTypeDate,
			dbal.FieldTypeTimestamp, dbal.FieldTypeBoolean, dbal.FieldTypeContainer,
		} {
			value, err := coerce(fieldType, "", data.DateISO)
			require.NoError(t, err, fieldType)
			assert.Nil(t, value, "%s: an empty cell must not become a value", fieldType)
		}
	})

	t.Run("text keeps what it was given, spaces and all", func(t *testing.T) {
		value, err := coerce(dbal.FieldTypeText, "  Durand  ", data.DateISO)
		require.NoError(t, err)
		assert.Equal(t, "  Durand  ", value, "trimming someone's data is not the import's business")
	})

	t.Run("numbers", func(t *testing.T) {
		for text, want := range map[string]string{
			"100":      "100",
			" 100 ":    "100",
			"-42.5":    "-42.5",
			"1,234.56": "1234.56", // comma groups, dot decides
			"1.234,56": "1234.56", // the European way round
			"1 234,56": "1234.56", // spaces group
			"1234":     "1234",
			"0,5":      "0.5",  // a lone comma with one digit after it
			"1,234":    "1234", // a lone comma with three digits groups
		} {
			value, err := coerce(dbal.FieldTypeNumber, text, data.DateISO)
			require.NoError(t, err, text)
			assert.Equal(t, want, value, "%q", text)
		}
	})

	t.Run("a number that is not one is reported, not zeroed", func(t *testing.T) {
		for _, text := range []string{"abc", "12abc", "1.2.3.4", "€100"} {
			_, err := coerce(dbal.FieldTypeNumber, text, data.DateISO)
			assert.Error(t, err, "%q should be refused", text)
		}
	})

	t.Run("booleans read the words people write", func(t *testing.T) {
		for _, text := range []string{"true", "TRUE", "yes", "Y", "1", "t"} {
			value, err := coerce(dbal.FieldTypeBoolean, text, data.DateISO)
			require.NoError(t, err, text)
			assert.Equal(t, true, value, text)
		}
		for _, text := range []string{"false", "No", "n", "0", "F"} {
			value, err := coerce(dbal.FieldTypeBoolean, text, data.DateISO)
			require.NoError(t, err, text)
			assert.Equal(t, false, value, text)
		}
		_, err := coerce(dbal.FieldTypeBoolean, "maybe", data.DateISO)
		assert.Error(t, err)
	})

	t.Run("a date is read in the order the caller said", func(t *testing.T) {
		// 03/04/2011 cannot be read without being told; the import is told.
		dmy, err := coerce(dbal.FieldTypeDate, "03/04/2011", data.DateDMY)
		require.NoError(t, err)
		assert.Equal(t, "2011-04-03", dmy, "3 April")

		mdy, err := coerce(dbal.FieldTypeDate, "03/04/2011", data.DateMDY)
		require.NoError(t, err)
		assert.Equal(t, "2011-03-04", mdy, "4 March")
	})

	t.Run("an ISO date reads in any order", func(t *testing.T) {
		for _, order := range []data.DateOrder{data.DateISO, data.DateDMY, data.DateMDY} {
			value, err := coerce(dbal.FieldTypeDate, "2011-04-03", order)
			require.NoError(t, err, order)
			assert.Equal(t, "2011-04-03", value)
		}
	})

	t.Run("a date in the wrong order for the setting is reported", func(t *testing.T) {
		// 25 is no month, so this cannot be month-first.
		_, err := coerce(dbal.FieldTypeDate, "25/04/2011", data.DateMDY)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mdy", "the error says which order was tried")
	})

	t.Run("timestamps", func(t *testing.T) {
		for _, text := range []string{
			"2011-04-03T09:30:00Z", "2011-04-03T09:30:00", "2011-04-03 09:30:00", "2011-04-03 09:30",
		} {
			_, err := coerce(dbal.FieldTypeTimestamp, text, data.DateISO)
			assert.NoError(t, err, text)
		}
		_, err := coerce(dbal.FieldTypeTimestamp, "yesterday", data.DateISO)
		assert.Error(t, err)
	})

	t.Run("a container value is base64 or nothing", func(t *testing.T) {
		value, err := coerce(dbal.FieldTypeContainer, "aGVsbG8=", data.DateISO)
		require.NoError(t, err)
		assert.Equal(t, "aGVsbG8=", value)

		_, err = coerce(dbal.FieldTypeContainer, "not base64!", data.DateISO)
		assert.Error(t, err)
	})
}
