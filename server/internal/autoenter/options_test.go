package autoenter_test

import (
	"encoding/json"
	"testing"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(s string) *string { return &s }

func TestParse_DefaultsAndOldValues(t *testing.T) {
	for _, stored := range []*string{nil, ptr(""), ptr("plain text from an older client")} {
		o, err := autoenter.Parse(stored)
		require.NoError(t, err)
		assert.False(t, o.Any())
	}

	o, err := autoenter.Parse(ptr(`{"serial_enabled":true}`))
	require.NoError(t, err)
	assert.Equal(t, "1", o.SerialNext)
	assert.EqualValues(t, 1, o.SerialIncrement)
	assert.Equal(t, autoenter.GenerateOnCreation, o.SerialGenerate)
	assert.True(t, o.SkipIfEmpty, "absent is FileMaker's default")
	assert.Equal(t, autoenter.NoMatchDoNotCopy, o.LookedUpNoMatch)

	o, err = autoenter.Parse(ptr(`{"serial_enabled":true,"serial_next":25,"serial_increment":"","calculated_skip_if_empty":false}`))
	require.NoError(t, err)
	assert.Equal(t, "25", o.SerialNext)
	assert.EqualValues(t, 1, o.SerialIncrement, "an empty increment keeps the default")
	assert.False(t, o.SkipIfEmpty)

	o, err = autoenter.Parse(ptr(`{"looked_up_enabled":true,"looked_up_target":"Rates::amount"}`))
	require.NoError(t, err)
	assert.Equal(t, "Rates", o.LookedUpOccurrence)
	assert.Equal(t, "amount", o.LookedUpField)

	_, err = autoenter.Parse(ptr(`{"serial_increment":1.5}`))
	require.ErrorIs(t, err, autoenter.ErrInvalid)
	_, err = autoenter.Parse(ptr(`{"creation_enabled":"yes"}`))
	require.ErrorIs(t, err, autoenter.ErrInvalid)
}

func TestValidate(t *testing.T) {
	ok := []struct{ fieldType, options string }{
		{"TEXT", `{"creation_enabled":true,"creation_type":"Account Name"}`},
		{"DATE", `{"modification_enabled":true,"modification_type":"Date"}`},
		{"TIMESTAMP", `{"creation_enabled":true,"creation_type":"Time"}`},
		{"NUMBER", `{"serial_enabled":true,"serial_next":"100","serial_increment":10}`},
		{"TEXT", `{"serial_enabled":true,"serial_next":"INV0001","serial_generate":"commit"}`},
		{"TEXT", `{"calculated_enabled":true,"calculated_formula":"Upper ( name )"}`},
		{"NUMBER", `{"looked_up_enabled":true,"looked_up_occurrence":"Rates","looked_up_field":"amount","looked_up_no_match":"copy_next_lower"}`},
		{"TEXT", `{"data_enabled":true,"data_value":"Open","prohibit_modification":true}`},
		{"", `{"storage_global":true,"indexing":"minimal"}`},
	}
	for _, c := range ok {
		assert.NoError(t, autoenter.Validate(c.fieldType, ptr(c.options)), c.options)
	}

	refused := []struct{ fieldType, options string }{
		{"TEXT", `{"creation_enabled":true,"creation_type":"Weekday"}`},
		{"DATE", `{"creation_enabled":true,"creation_type":"Name"}`},
		{"NUMBER", `{"modification_enabled":true,"modification_type":"Date"}`},
		{"DATE", `{"serial_enabled":true}`},
		{"NUMBER", `{"serial_enabled":true,"serial_next":"A1"}`},
		{"TEXT", `{"serial_enabled":true,"serial_next":"none"}`},
		{"TEXT", `{"serial_enabled":true,"serial_increment":0}`},
		{"TEXT", `{"serial_enabled":true,"serial_generate":"later"}`},
		{"TEXT", `{"calculated_enabled":true,"calculated_formula":""}`},
		{"TEXT", `{"calculated_enabled":true,"calculated_formula":"Upper ( "}`},
		{"TEXT", `{"looked_up_enabled":true,"looked_up_occurrence":"Rates"}`},
		{"TEXT", `{"looked_up_enabled":true,"looked_up_occurrence":"Rates","looked_up_field":"x","looked_up_no_match":"guess"}`},
	}
	for _, c := range refused {
		assert.ErrorIs(t, autoenter.Validate(c.fieldType, ptr(c.options)), autoenter.ErrInvalid, c.options)
	}
}

func TestNextSerial(t *testing.T) {
	cases := []struct {
		next      string
		increment int64
		want      string
	}{
		{"1", 1, "2"},
		{"95", 5, "100"},
		{"INV0009", 1, "INV0010"},
		{"A99", 1, "A100"},
		{"2026-0007-B", 1, "2026-0008-B"},
		{"99999999999999999999", 1, "100000000000000000000"},
	}
	for _, c := range cases {
		got, err := autoenter.NextSerial(c.next, c.increment)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, c.next)
	}
	_, err := autoenter.NextSerial("ABC", 1)
	require.ErrorIs(t, err, autoenter.ErrInvalid)
}

func TestWithSerialNextKeepsOtherKeys(t *testing.T) {
	out, err := autoenter.WithSerialNext(ptr(`{"serial_enabled":true,"serial_next":"7","indexing":"all","custom":{"a":1}}`), "8")
	require.NoError(t, err)
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &obj))
	assert.Equal(t, "8", obj["serial_next"])
	assert.Equal(t, "all", obj["indexing"])
	assert.Equal(t, map[string]any{"a": float64(1)}, obj["custom"])

	assert.True(t, autoenter.HasKey(ptr(out), "serial_next"))
	assert.False(t, autoenter.HasKey(ptr(`{"serial_enabled":true}`), "serial_next"))
	assert.False(t, autoenter.HasKey(nil, "serial_next"))
}

func TestEncodeRoundTrip(t *testing.T) {
	value := "Open"
	in := autoenter.Options{
		CreationEnabled: true, CreationType: autoenter.StampTimestamp,
		SerialEnabled: true, SerialNext: "INV0001", SerialIncrement: 2, SerialGenerate: autoenter.GenerateOnCommit,
		DataEnabled: true, DataValue: &value,
		CalculatedEnabled: true, CalculatedFormula: `"x"`, DoNotReplace: true, SkipIfEmpty: false,
		LookedUpEnabled: true, LookedUpOccurrence: "Rates", LookedUpField: "amount",
		LookedUpNoMatch: autoenter.NoMatchConstant, LookedUpNoMatchValue: "0", LookedUpDontCopyIfEmpty: true,
		ProhibitModification: true,
	}
	encoded := autoenter.Encode(in)
	require.NoError(t, autoenter.Validate("TEXT", &encoded))
	out, err := autoenter.Parse(&encoded)
	require.NoError(t, err)
	assert.Equal(t, in.CreationType, out.CreationType)
	assert.Equal(t, "INV0001", out.SerialNext)
	assert.EqualValues(t, 2, out.SerialIncrement)
	assert.Equal(t, autoenter.GenerateOnCommit, out.SerialGenerate)
	require.NotNil(t, out.DataValue)
	assert.Equal(t, "Open", *out.DataValue)
	assert.True(t, out.DoNotReplace)
	assert.False(t, out.SkipIfEmpty)
	assert.Equal(t, autoenter.NoMatchConstant, out.LookedUpNoMatch)
	assert.Equal(t, "0", out.LookedUpNoMatchValue)
	assert.True(t, out.LookedUpDontCopyIfEmpty)
	assert.True(t, out.ProhibitModification)
}

func TestStampFits(t *testing.T) {
	assert.True(t, autoenter.StampFits(autoenter.StampName, "TEXT"))
	assert.True(t, autoenter.StampFits(autoenter.StampTime, "TIMESTAMP"))
	assert.False(t, autoenter.StampFits(autoenter.StampTime, "DATE"))
	assert.False(t, autoenter.StampFits(autoenter.StampDate, "NUMBER"))
	assert.False(t, autoenter.StampFits(autoenter.StampAccountName, "TIMESTAMP"))
}
