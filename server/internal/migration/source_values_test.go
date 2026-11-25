package migration

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
)

// GetAsNumber, read as FileMaker Pro 26.0.2 with French settings read it
// (testdata/claris26-fr-scalars), is what a permissive Number value migrates to.
func TestFileMakerNumberMatchesTheFrenchSourceOracle(t *testing.T) {
	raw, err := os.ReadFile("testdata/claris26-fr-scalars/source-results.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	inputs := map[string]string{ // the literal each observation was evaluated on (expression.fmcalc)
		"number_suffix":         "12-A",
		"number_fraction_text":  "1/2",
		"number_embedded_minus": "12-34",
		"number_leading_minus":  "-3a7",
		"number_parentheses":    "(4x2)",
		"number_empty":          "",
		"number_nondigit":       "word",
		"number_comma":          "7,125",
		"number_dot":            "7.125",
		"number_precise_comma":  "9007199254740993,125",
		"number_precise_dot":    "9007199254740993.125",
	}
	for key, input := range inputs {
		want, ok := oracle.Values[key]
		if !ok {
			t.Fatalf("oracle has no %s", key)
		}
		got, digits := fileMakerNumber(input, ',')
		if !digits {
			got = ""
		}
		// The oracle is FileMaker's French text of the number.
		if french := strings.Replace(got, ".", ",", 1); french != want {
			t.Errorf("GetAsNumber(%q) = %q, FileMaker read %q", input, french, want)
		}
	}
}

func TestParseSourceLocale(t *testing.T) {
	for raw, tag := range map[string]string{"fr-FR": "fr-FR", "fr_fr": "fr-FR", " ES-es ": "es-ES", "en-US": "en-US"} {
		loc, err := parseSourceLocale(raw)
		if err != nil || loc == nil || loc.Tag != tag {
			t.Fatalf("parseSourceLocale(%q) = %v, %v", raw, loc, err)
		}
	}
	if loc, err := parseSourceLocale(""); err != nil || loc != nil {
		t.Fatalf("an empty locale is no locale: %v %v", loc, err)
	}
	if _, err := parseSourceLocale("xx-YY"); !errors.Is(err, ErrInvalidMigrationXML) {
		t.Fatalf("an unknown locale must be refused, got %v", err)
	}
}

func TestInterpretSourceValue(t *testing.T) {
	fr, _ := parseSourceLocale("fr-FR")
	us, _ := parseSourceLocale("en-US")
	cases := []struct {
		name, text, kind string
		loc              *sourceLocale
		order            data.DateOrder
		value            interface{}
		original         string
	}{
		{"fr decimal", "7,125", "NUMBER", fr, data.DateDMY, "7.125", ""},
		{"fr dot is not a decimal", "7.125", "NUMBER", fr, data.DateDMY, "7125", "7.125"},
		{"suffix", "12-A", "NUMBER", fr, data.DateDMY, "12", "12-A"},
		{"parentheses", "(4x2)", "NUMBER", fr, data.DateDMY, "-42", "(4x2)"},
		{"no digit", "N/A", "NUMBER", fr, data.DateDMY, nil, "N/A"},
		{"us decimal", "7.125", "NUMBER", us, data.DateMDY, "7.125", ""},
		{"us comma is not a decimal", "7,125", "NUMBER", us, data.DateMDY, "7125", "7,125"},
		{"blank", "   ", "NUMBER", fr, data.DateDMY, nil, ""},
		{"fr date", "13/11/2026", "DATE", fr, data.DateDMY, nil, ""},
		{"no date", "31/02/2026", "DATE", fr, data.DateDMY, nil, "31/02/2026"},
		{"time of day", "15:04:05", "TIMESTAMP", fr, data.DateDMY, "0001-01-01T15:04:05Z", ""},
		{"time with a fraction", "15:04:05,25", "TIMESTAMP", fr, data.DateDMY, "0001-01-01T15:04:05.25Z", ""},
		{"us time with a fraction", "15:04:05.25", "TIMESTAMP", us, data.DateMDY, "0001-01-01T15:04:05.25Z", ""},
		{"duration", "49:30:00", "TIMESTAMP", fr, data.DateDMY, nil, "49:30:00"},
		{"negative duration", "-01:02:03,125", "TIMESTAMP", fr, data.DateDMY, nil, "-01:02:03,125"},
	}
	for _, tc := range cases {
		value, original, err := interpretSourceValue(tc.text, tc.kind, tc.loc, tc.order)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.name == "fr date" {
			if value == nil || original != "" {
				t.Fatalf("%s: a valid date is migrated as a date, got %v %q", tc.name, value, original)
			}
			continue
		}
		if value != tc.value || original != tc.original {
			t.Errorf("%s: got (%v, %q), want (%v, %q)", tc.name, value, original, tc.value, tc.original)
		}
	}
	if _, _, err := interpretSourceValue("1\x1d2", "NUMBER", fr, data.DateDMY); !errors.Is(err, errSourceRepetitions) {
		t.Fatalf("repetitions must stay refused, got %v", err)
	}
}

const permissiveDesign = `<FMPReport><File name="Synthetic"><BaseTableCatalog><BaseTable name="Visits"><FieldCatalog>` +
	`<Field name="Amount" dataType="Number"/><Field name="Visited" dataType="Date"/></FieldCatalog></BaseTable>` +
	`</BaseTableCatalog></File></FMPReport>`

const permissiveRecords = `<FMPXMLRESULT><METADATA><FIELD NAME="Amount" TYPE="NUMBER"/><FIELD NAME="Visited" TYPE="DATE"/></METADATA>` +
	`<RESULTSET FOUND="3"><ROW><COL><DATA>12-A</DATA></COL><COL><DATA>11/13/2026</DATA></COL></ROW>` +
	`<ROW><COL><DATA>7,125</DATA></COL><COL><DATA>soon</DATA></COL></ROW>` +
	`<ROW><COL><DATA>N/A</DATA></COL><COL><DATA></DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`

// With the source locale named, a dataset of permissive values is ready in
// full: every record, typed values and the original texts beside them.
func TestRecordExportWithSourceLocaleMigratesPermissiveValues(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: permissiveDesign, SourceLocale: "fr-FR",
		DataFiles: map[string]string{"Visits": permissiveRecords}})
	if err != nil {
		t.Fatal(err)
	}
	counts := res.Report.Counts
	if counts.RecordsDetected != 3 || counts.RecordsReady != 3 || counts.SourceTextValues != 3 {
		t.Fatalf("detected %d, ready %d, original texts %d; rejected: %+v", counts.RecordsDetected, counts.RecordsReady,
			counts.SourceTextValues, res.Report.Rejected)
	}
	rows := res.TableRows["visits"]
	texts := func(i int) map[string]string { m, _ := rows[i][sourceTextKey].(map[string]string); return m }
	if rows[0]["amount"] != "12" || texts(0)["amount"] != "12-A" || texts(0)["visited"] != "" {
		t.Fatalf("row 1: %+v", rows[0])
	}
	if rows[1]["amount"] != "7.125" || texts(1)["amount"] != "" || rows[1]["visited"] != nil || texts(1)["visited"] != "soon" {
		t.Fatalf("row 2: %+v", rows[1])
	}
	if rows[2]["amount"] != nil || texts(2)["amount"] != "N/A" {
		t.Fatalf("row 3: %+v", rows[2])
	}
	if len(res.Report.Translated) == 0 || !strings.Contains(res.Report.Translated[len(res.Report.Translated)-1].Detail, "fr-FR") {
		t.Fatalf("the report must name the values kept with their text: %+v", res.Report.Translated)
	}
}

// Without a locale the migration stays strict, and says what would migrate
// the values.
func TestRecordExportWithoutSourceLocaleStaysStrict(t *testing.T) {
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: permissiveDesign,
		DataFiles: map[string]string{"Visits": permissiveRecords}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Counts.RecordsReady != 0 || len(res.TableRows["visits"]) != 0 {
		t.Fatal("permissive values must not be migrated without a source locale")
	}
	hinted := false
	for _, item := range res.Report.Rejected {
		hinted = hinted || strings.Contains(item.Detail, "source_locale")
	}
	if !hinted {
		t.Fatalf("the refusal must point at source_locale: %+v", res.Report.Rejected)
	}
	if _, err := AnalyzeAndConvert(MigrationRequest{DesignXML: permissiveDesign, SourceLocale: "tlh-KX"}); !errors.Is(err, ErrInvalidMigrationXML) {
		t.Fatalf("an unknown source locale must be refused, got %v", err)
	}
}

// nativeVisit builds an original HBAM7 file whose single leaf block holds one
// record of table 129 with the given field values (field id → text).
func nativeVisit(values map[byte]string) []byte {
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := appendHBAM7TableCatalog(nil, 129, "Visits", []testHBAM7FieldSpec{
		{id: 1, name: "Amount", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeNumber},
		{id: 2, name: "Visited", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeDate},
		{id: 3, name: "Lasted", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeTime},
	})
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1)
	for id := byte(1); id < 16; id++ {
		value, ok := values[id]
		if !ok {
			continue
		}
		payload = append(payload, 0x06, id, byte(len(value)))
		for _, b := range []byte(value) {
			payload = append(payload, b^0x5a)
		}
	}
	copy(file[2*4096+20:], payload)
	return file
}

func TestNativeRecordsWithSourceLocaleKeepOriginalText(t *testing.T) {
	fields := map[int]map[int]htmlFieldMeta{129: {
		1: {ColName: "amount", RawName: "Amount", FieldType: "NUMBER"},
		2: {ColName: "visited", RawName: "Visited", FieldType: "DATE"},
		3: {ColName: "lasted", RawName: "Lasted", FieldType: "TIMESTAMP"},
	}}
	types := map[string]string{"visits.amount": "NUMBER", "visits.visited": "DATE", "visits.lasted": "TIMESTAMP"}
	file := nativeVisit(map[byte]string{1: "(4x2)", 2: "13/11/2026", 3: "49:30:00"})

	if _, _, err := readHBAM7Records(file, map[int]string{129: "visits"}, fields, types, "dmy"); err == nil {
		t.Fatal("without a locale the native values must stay refused")
	}
	fr, _ := parseSourceLocale("fr-FR")
	rows, _, err := readHBAM7RecordsWith(file, map[int]string{129: "visits"}, fields, types,
		nativeReadOptions{dateOrder: "dmy", locale: fr})
	if err != nil {
		t.Fatal(err)
	}
	row := rows["visits"][0]
	texts, _ := row[sourceTextKey].(map[string]string)
	if row["amount"] != "-42" || texts["amount"] != "(4x2)" {
		t.Fatalf("amount: %+v", row)
	}
	if row["visited"] == nil || texts["visited"] != "" {
		t.Fatalf("a valid date needs no original text: %+v", row)
	}
	if row["lasted"] != nil || texts["lasted"] != "49:30:00" {
		t.Fatalf("a duration keeps its text: %+v", row)
	}
}

// The client's migration dialog offers the same list
// (ApiClient.migrationSourceLocales); change both together.
func TestSupportedSourceLocalesMatchTheClient(t *testing.T) {
	want := []string{"ca-ES", "de-AT", "de-DE", "en-AU", "en-GB", "en-IE", "en-NZ", "en-US",
		"es-ES", "fr-BE", "fr-FR", "it-IT", "nl-BE", "nl-NL", "pt-BR", "pt-PT"}
	got := SupportedSourceLocales()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("supported locales %v, the client offers %v", got, want)
	}
}

func TestNativeNULTextIsRepairedOnlyWithSourceLocale(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Visits", []testHBAM7FieldSpec{
		{id: 1, name: "Note", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Only Nul", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "ab\x00cd", 2: "\x00"})
	file := buildSyntheticHBAM7(payload)
	fields := map[int]map[int]htmlFieldMeta{129: {
		1: {ColName: "note", RawName: "Note", FieldType: "TEXT"},
		2: {ColName: "only_nul", RawName: "Only Nul", FieldType: "TEXT"},
	}}
	types := map[string]string{"visits.note": "TEXT", "visits.only_nul": "TEXT"}

	stripped := 0
	if _, _, err := readHBAM7RecordsWith(file, map[int]string{129: "visits"}, fields, types,
		nativeReadOptions{nulStripped: &stripped}); err == nil {
		t.Fatal("without a locale a NUL in Text must stay refused")
	}
	fr, _ := parseSourceLocale("fr-FR")
	rows, _, err := readHBAM7RecordsWith(file, map[int]string{129: "visits"}, fields, types,
		nativeReadOptions{locale: fr, nulStripped: &stripped})
	if err != nil {
		t.Fatal(err)
	}
	row := rows["visits"][0]
	if row["note"] != "abcd" || stripped != 2 {
		t.Fatalf("NUL repair: row %+v, stripped %d", row, stripped)
	}
	if _, ok := row["only_nul"]; ok {
		t.Fatalf("a value made only of NUL is empty: %+v", row)
	}
}
