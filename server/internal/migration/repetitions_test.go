package migration

import (
	"fmt"
	"strings"
	"testing"
)

// A repeating field keeps its maximum and every one of its repetitions from an
// FMPXMLRESULT export, each typed on its own.
func TestAnalyzeAndConvert_MigratesRepeatingFields(t *testing.T) {
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Samples"><FieldCatalog>` +
		`<Field name="Reading" dataType="Number" fieldType="Normal"><Storage global="False" maxRepetition="3"/></Field>` +
		`<Field name="Marker" dataType="Text" fieldType="Normal" maxRepetition="2"/>` +
		`<Field name="Single" dataType="Number" fieldType="Normal"/>` +
		`</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`
	records := `<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult"><METADATA>` +
		`<FIELD NAME="Reading" TYPE="NUMBER" MAXREPEAT="3"/><FIELD NAME="Marker" TYPE="TEXT" MAXREPEAT="2"/><FIELD NAME="Single" TYPE="NUMBER"/>` +
		`</METADATA><RESULTSET FOUND="1">` +
		`<ROW RECORDID="1"><COL><DATA>4</DATA><DATA></DATA><DATA>7.5</DATA></COL>` +
		`<COL><DATA>up</DATA><DATA>down</DATA><DATA>dropped</DATA></COL><COL><DATA>2</DATA></COL></ROW>` +
		`</RESULTSET></FMPXMLRESULT>`

	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Samples": records}})
	if err != nil {
		t.Fatal(err)
	}
	reps := map[string]int{}
	for _, tbl := range res.Bundle.Tables {
		for _, c := range tbl.Columns {
			reps[c.Name] = c.MaxRepetitions
		}
	}
	if reps["reading"] != 3 || reps["marker"] != 2 || reps["single"] > 1 {
		t.Fatalf("maximum repetitions not carried into the schema: %v", reps)
	}
	for _, item := range append(res.Report.Unsupported, res.Report.ManualActionRequired...) {
		if strings.Contains(strings.ToLower(item.Detail), "repetition") {
			t.Fatalf("a repeating field is migrated, not reported as lost: %+v", item)
		}
	}
	rows := res.TableRows["samples"]
	if len(rows) != 1 {
		t.Fatalf("rows: %v (rejected: %+v)", rows, res.Report.Rejected)
	}
	row := rows[0]
	got := fmt.Sprint(row["reading"], "|", row["reading[2]"], "|", row["reading[3]"], "|", row["marker"], "|", row["marker[2]"], "|", row["single"])
	if got != "4|<nil>|7.5|up|down|2" {
		t.Fatalf("repetitions: %s (row %v)", got, row)
	}
	if _, ok := row["marker[3]"]; ok {
		t.Fatalf("a repetition past the maximum has nowhere to go: %v", row)
	}
}

// A field that does not repeat still refuses several typed values.
func TestAnalyzeAndConvert_NonRepeatingFieldRefusesRepetitions(t *testing.T) {
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Samples"><FieldCatalog><Field name="Single" dataType="Number"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`
	records := `<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult"><METADATA><FIELD NAME="Single" TYPE="NUMBER"/></METADATA>` +
		`<RESULTSET FOUND="1"><ROW RECORDID="1"><COL><DATA>1</DATA><DATA>2</DATA></COL></ROW></RESULTSET></FMPXMLRESULT>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Samples": records}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Counts.RecordsReady != 0 || len(res.Report.Rejected) == 0 {
		t.Fatalf("two values in a single Number field must be rejected: %+v", res.Report.Counts)
	}
}

// appendHBAM7Repetition writes repetition rep of field fid of the current
// record: a two-byte key, 0x80|fid and rep, as FileMaker keys repetitions.
func appendHBAM7Repetition(payload []byte, fid, rep byte, value string) []byte {
	payload = append(payload, 0x0e, 0x80|fid, rep, byte(len(value)))
	return append(payload, nativeEncoded([]byte(value))...)
}

// A native file's repetitions are each migrated under their own key, typed;
// a field the design does not say repeats keeps them joined, as before.
func TestNativeRepetitionsAreMigratedSeparately(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Samples", []testHBAM7FieldSpec{
		{id: 1, name: "Reading", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeNumber},
		{id: 2, name: "Marker", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 3, name: "Joined", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1)
	payload = appendHBAM7Repetition(payload, 1, 1, "12")
	payload = appendHBAM7Repetition(payload, 1, 3, "0.25")
	payload = appendHBAM7Repetition(payload, 1, 4, "9") // past the maximum
	payload = appendHBAM7Repetition(payload, 2, 2, "east")
	payload = appendHBAM7Repetition(payload, 3, 1, "a")
	payload = appendHBAM7Repetition(payload, 3, 2, "b")
	payload = append(payload, 0x40, 0x40, 0x40)

	fields := map[int]map[int]htmlFieldMeta{129: {
		1: {ColName: "reading", RawName: "Reading", FieldType: "NUMBER", Repetitions: 3},
		2: {ColName: "marker", RawName: "Marker", FieldType: "TEXT", Repetitions: 2},
		3: {ColName: "joined", RawName: "Joined", FieldType: "TEXT", Repetitions: 1},
	}}
	types := map[string]string{"samples.reading": "NUMBER", "samples.marker": "TEXT", "samples.joined": "TEXT"}
	rows, _, err := readHBAM7RecordsWith(buildSyntheticHBAM7(payload), map[int]string{129: "samples"}, fields, types,
		nativeReadOptions{dateOrder: "dmy"})
	if err != nil {
		t.Fatal(err)
	}
	row := rows["samples"][0]
	got := fmt.Sprint(row["reading"], "|", row["reading[2]"], "|", row["reading[3]"], "|", row["marker"], "|", row["marker[2]"])
	if got != "12|<nil>|0.25|<nil>|east" {
		t.Fatalf("repetitions: %s (row %v)", got, row)
	}
	if _, ok := row["reading[4]"]; ok {
		t.Fatalf("a repetition past the maximum has nowhere to go: %v", row)
	}
	if row["joined"] != "a\x1db" {
		t.Fatalf("a field not known to repeat keeps its repetitions joined: %q", row["joined"])
	}
}
