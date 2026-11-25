package migration

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A field "Validated by calculation" keeps its formula, bound to the
// imported fields, with its message, its timing and whether the person may
// override it. A formula switched off in FileMaker is not enforced there,
// and is not migrated; one that cannot be bound is reported.
func TestValidationByCalculationIsMigrated(t *testing.T) {
	field := func(name, validation string) string {
		return `<Field name="` + name + `" dataType="Text" fieldType="Normal">` + validation + `</Field>`
	}
	doc := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable name="Parcels"><FieldCatalog>` +
		`<Field name="Weight Kg" dataType="Number" fieldType="Normal"/>` +
		field("Code", `<Validation alwaysValidate="False" type="Always" allowOverride="False" notEmpty="False" unique="False" existing="False">`+
			`<Calculated enable="True"><Calculation><TableOccurrenceReference name="Parcels"/><Text><![CDATA[Length ( Self ) = 5 and Weight Kg < 30]]></Text></Calculation></Calculated>`+
			`<Message>Five characters, under 30 kg</Message></Validation>`) +
		field("Label", `<Validation alwaysValidate="False" type="OnlyDuringDataEntry" allowOverride="True" notEmpty="True" unique="False" existing="False">`+
			`<Calculated enable="False"><Calculation><Text><![CDATA[Self <> "x"]]></Text></Calculation></Calculated></Validation>`) +
		field("Ref", `<Validation alwaysValidate="False" type="OnlyDuringDataEntry" allowOverride="True" notEmpty="False" unique="False" existing="False">`+
			`<Calculated enable="True"><Calculation><Text><![CDATA[Ghosts::Name = Self]]></Text></Calculation></Calculated></Validation>`) +
		`</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: doc})
	require.NoError(t, err)
	columns := map[string]schema.BundleColumn{}
	for _, col := range res.Bundle.Tables[0].Columns {
		columns[col.Name] = col
	}

	code, err := validation.Parse(columns["code"].ValidationRules)
	require.NoError(t, err)
	require.NotNil(t, code)
	assert.True(t, code.CalculationEnabled)
	assert.Equal(t, "Length ( Self ) = 5 and weight_kg < 30", code.Calculation)
	assert.Equal(t, "parcels", code.CalculationContext, "the imported occurrence")
	assert.Equal(t, "always", code.Timing)
	assert.False(t, code.AllowOverride)
	assert.Equal(t, "Five characters, under 30 kg", code.CustomMessage)

	label, err := validation.Parse(columns["label"].ValidationRules)
	require.NoError(t, err)
	require.NotNil(t, label)
	assert.True(t, label.NotEmpty)
	assert.False(t, label.CalculationEnabled, "a formula switched off is not migrated")
	assert.True(t, label.AllowOverride)
	assert.Equal(t, "entry", label.Timing)
	assert.True(t, columns["label"].IsNullable, "a not-empty rule the person may override is not a NOT NULL column")

	assert.Nil(t, columns["ref"].ValidationRules, "its only rule could not be bound")
	var preserved, dropped bool
	for _, it := range res.Report.Preserved {
		if it.TargetName == "parcels.code" && strings.Contains(it.Detail, "Validation by calculation") {
			preserved = true
		}
	}
	for _, it := range res.Report.Unsupported {
		if it.TargetName == "parcels.ref" && strings.Contains(it.Detail, "Validation by calculation") {
			dropped = true
		}
	}
	assert.True(t, preserved, "the migrated formula is reported")
	assert.True(t, dropped, "the formula that could not be bound is reported")
}
