package migration

import (
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/schema"
)

// A record's token in the page tree is its record ID, as a path integer.
func TestNativeRecordID(t *testing.T) {
	for _, tc := range []struct {
		token string
		want  int64
		ok    bool
	}{
		{"\x01", 1, true},
		{"\x7f", 127, true},
		{"\x80\x00", 128, true},
		{"\x80\x05", 133, true},
		{"\x81\x02", 386, true},
		{"\xff\xff", 32895, true},
		{"", 0, false},
		{"\x00", 0, false},
		{"\x05\x01", 0, false},
		{"\xc0\x01\x02", 0, false},
	} {
		got, ok := nativeRecordID(tc.token)
		if got != tc.want || ok != tc.ok {
			t.Errorf("nativeRecordID(%q) = %d, %v; want %d, %v", tc.token, got, ok, tc.want, tc.ok)
		}
	}
}

func identityOf(row map[string]interface{}) (map[string]interface{}, bool) {
	identity, ok := row[schema.RecordIdentityKey].(map[string]interface{})
	return identity, ok
}

// The records of a native file keep the record IDs FileMaker gave them,
// in one byte or two, and count no modification, as FileMaker's migrated
// records do.
func TestNativeRecordsKeepTheirRecordIDs(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Visits", []testHBAM7FieldSpec{
		{id: 1, name: "Code", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = appendHBAM7SingleRow(payload, 129, 7, map[byte]string{1: "V-7"})
	payload = appendHBAM7SingleRow(payload, 129, 2, map[byte]string{1: "V-2"})
	// A record numbered past 127: its token is two bytes, 0x80 0x05.
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x28, 0x80, 0x05, 0x06, 1, 5)
	payload = append(payload, nativeEncoded([]byte("V-133"))...)
	payload = append(payload, 0x40, 0x40, 0x40)
	fields := map[int]map[int]htmlFieldMeta{129: {1: {ColName: "code", RawName: "Code", FieldType: "TEXT"}}}
	unnumbered := map[string]int{}
	rows, _, err := readHBAM7RecordsWith(buildSyntheticHBAM7(payload), map[int]string{129: "visits"}, fields,
		map[string]string{"visits.code": "TEXT"}, nativeReadOptions{recordIDs: unnumbered})
	if err != nil {
		t.Fatal(err)
	}
	got := rows["visits"]
	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}
	for i, want := range []struct {
		code string
		id   int64
	}{{"V-2", 2}, {"V-7", 7}, {"V-133", 133}} {
		identity, ok := identityOf(got[i])
		if got[i]["code"] != want.code || !ok || identity["record_id"] != want.id || identity["mod_id"] != int64(0) {
			t.Errorf("record %d: %v, want %s with record ID %d and no modification", i, got[i], want.code, want.id)
		}
	}
	if len(unnumbered) != 0 {
		t.Errorf("every record ID was read, yet %v were not", unnumbered)
	}

	plain, _, err := readHBAM7Records(buildSyntheticHBAM7(payload), map[int]string{129: "visits"}, fields,
		map[string]string{"visits.code": "TEXT"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := identityOf(plain["visits"][0]); ok {
		t.Error("only a migration asks for the record IDs")
	}
}

// An FMPXMLRESULT export keeps each record's RECORDID and MODID; a record
// the file does not number, or numbers as another, is given a new ID and
// keeps its MODID, and the report says so.
func TestAnalyzeAndConvert_KeepsXMLRecordIdentities(t *testing.T) {
	design := `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog>` +
		`<BaseTable name="Visits"><FieldCatalog><Field name="Code" dataType="Text"/></FieldCatalog></BaseTable>` +
		`</BaseTableCatalog></File></FMPReport>`
	records := `<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult"><METADATA>` +
		`<FIELD NAME="Code" TYPE="TEXT"/></METADATA><RESULTSET FOUND="4">` +
		`<ROW MODID="4" RECORDID="12"><COL><DATA>V-12</DATA></COL></ROW>` +
		`<ROW MODID="0" RECORDID="30"><COL><DATA>V-30</DATA></COL></ROW>` +
		`<ROW MODID="2"><COL><DATA>V-none</DATA></COL></ROW>` +
		`<ROW MODID="1" RECORDID="12"><COL><DATA>V-again</DATA></COL></ROW>` +
		`</RESULTSET></FMPXMLRESULT>`
	res, err := AnalyzeAndConvert(MigrationRequest{DesignXML: design, DataFiles: map[string]string{"Visits": records}})
	if err != nil {
		t.Fatal(err)
	}
	rows := res.TableRows["visits"]
	if len(rows) != 4 {
		t.Fatalf("expected 4 records, got %d", len(rows))
	}
	for i, want := range [][2]int64{{12, 4}, {30, 0}, {0, 2}, {0, 1}} {
		identity, ok := identityOf(rows[i])
		recordID, numbered := identity["record_id"]
		if !ok || identity["mod_id"] != want[1] || numbered != (want[0] > 0) || (numbered && recordID != want[0]) {
			t.Errorf("record %d carries %v, want record ID %d (0: a new one) and modification count %d", i, rows[i], want[0], want[1])
		}
	}
	found := false
	for _, item := range res.Report.Translated {
		if item.Domain == "data" && item.TargetName == "visits" && strings.HasPrefix(item.Detail, "2 record(s) did not say") {
			found = true
		}
	}
	if !found {
		t.Error("the record given a new ID is reported")
	}
}
