package migration

import (
	"encoding/binary"
	"testing"
)

func nativeEncoded(bytes []byte) []byte {
	out := append([]byte(nil), bytes...)
	for i := range out {
		out[i] ^= 0x5a
	}
	return out
}

type testHBAM7FieldSpec struct {
	id         byte
	name       string
	fieldClass byte
	dataType   byte
}

func appendHBAM7TableCatalog(payload []byte, tableID int, tableName string, fields []testHBAM7FieldSpec) []byte {
	tHi := byte(0x80 | ((tableID - 128) >> 8))
	tLo := byte((tableID - 128) & 0xff)
	// [3].[16].[5].[tableID] key 16 = tableName
	payload = append(payload, 0x20, 3, 0x20, 16, 0x20, 5, 0x28, tHi, tLo)
	payload = append(payload, 0x06, 16, byte(len(tableName)))
	payload = append(payload, nativeEncoded([]byte(tableName))...)
	payload = append(payload, 0x40, 0x40, 0x40, 0x40)
	// [tableID].[3].[5].[fid] key 2 = [class, dataType], key 16 = fieldName
	if len(fields) > 0 {
		payload = append(payload, 0x28, tHi, tLo, 0x20, 3, 0x20, 5)
		for _, f := range fields {
			payload = append(payload, 0x20, f.id)
			if f.fieldClass != 0 || f.dataType != 0 {
				payload = append(payload, 0x02, 2, f.fieldClass, f.dataType)
			}
			payload = append(payload, 0x06, 16, byte(len(f.name)))
			payload = append(payload, nativeEncoded([]byte(f.name))...)
			payload = append(payload, 0x40)
		}
		payload = append(payload, 0x40, 0x40, 0x40)
	}
	return payload
}

func TestNativeUnicodePreservesSourceText(t *testing.T) {
	for _, tc := range []struct {
		bytes []byte
		want  string
	}{
		{[]byte("  alpha\t\r\n"), "  alpha\t\r\n"},
		{[]byte{'M', 'o', 'n', 't', 'r', 0xe9, 'a', 'l'}, "Montréal"},
		{[]byte{6, 0x2c}, "€"},
		{[]byte{0x0f, 0xd8, 0x3d, 0xde, 0x9a}, "🚚"},
		{[]byte{0x0b, 0x01, 0xed, 0x9a}, "🚚"},
		{[]byte{0x0f, 0x1a, 0x1a, 0x1a, 0x7a, 0x5f, 0xda}, "\u1a1a\u1a7a\u5fda"},
	} {
		got, err := decodeHBAM7Text(nativeEncoded(tc.bytes))
		if err != nil || got != tc.want {
			t.Fatalf("Unicode value changed: got %q, want %q, error %v", got, tc.want, err)
		}
	}
	for _, b := range [][]byte{{0x0f, 0xd8, 0x3d}, {0x18}, {0x0e, 0x12}} {
		if _, err := decodeHBAM7Text(nativeEncoded(b)); err == nil {
			t.Fatal("malformed text was silently accepted")
		}
	}
}

func TestNativeRecordsUsePathsAcrossPages(t *testing.T) {
	file := make([]byte, 4*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	binary.BigEndian.PutUint32(file[2*4096+8:], 3)
	binary.BigEndian.PutUint32(file[3*4096+4:], 2)
	// The source value deliberately contains the byte pattern the old reader
	// guessed was a record boundary. It is data inside a length-delimited cell.
	value := nativeEncoded([]byte{0x0f, 0x1a, 0x1a, 0x1a, 0x7a, 0x5f, 0xda})
	page := appendHBAM7TableCatalog(nil, 129, "Visits", []testHBAM7FieldSpec{
		{id: 1, name: "Text", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Long", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	page = append(page, 0x28, 0x80, 1, 0x20, 5, 0x28, 0x81, 0x10, 0x06, 1, byte(len(value)))
	page = append(page, value...)
	copy(file[2*4096+20:], page)
	// Record 400, field 2, split across two chunks on its continuation page.
	page = []byte{0x28, 0x80, 1, 0x20, 5, 0x28, 0x81, 0x10, 0x20, 2, 0x07, 1, 0, 2}
	page = append(page, nativeEncoded([]byte("ab"))...)
	page = append(page, 0x07, 1, 0, 2)
	page = append(page, nativeEncoded([]byte("cd"))...)
	page = append(page, 0x40, 0x40, 0x28, 0x81, 0x11) // blank record 401
	copy(file[3*4096+20:], page)
	fields := map[int]map[int]htmlFieldMeta{129: {
		1: {ColName: "text", RawName: "Text", FieldType: "TEXT"},
		2: {ColName: "long", RawName: "Long", FieldType: "TEXT"},
	}}
	got, _, err := readHBAM7Records(file, map[int]string{129: "visits"}, fields, map[string]string{"visits.text": "TEXT", "visits.long": "TEXT"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got["visits"]) != 2 || got["visits"][0]["text"] != "\u1a1a\u1a7a\u5fda" || got["visits"][0]["long"] != "abcd" || len(got["visits"][1]) != 0 {
		t.Fatalf("record identity or cells changed: %+v", got)
	}
	file[3*4096+20] = 0x21
	if _, _, err := readHBAM7Records(file, map[int]string{129: "visits"}, fields, nil, ""); err == nil {
		t.Fatal("malformed input returned a partial import")
	}
}

func TestNativeRecordTokensRemainDistinct(t *testing.T) {
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := appendHBAM7TableCatalog(nil, 129, "Visits", []testHBAM7FieldSpec{
		{id: 1, name: "Code", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x30, 0xc0, 1, 0x10, 0x06, 1, 1)
	payload = append(payload, nativeEncoded([]byte("A"))...)
	payload = append(payload, 0x40, 0x30, 0xc1, 1, 0x10, 0x06, 1, 1)
	payload = append(payload, nativeEncoded([]byte("B"))...)
	copy(file[2*4096+20:], payload)
	got, _, err := readHBAM7Records(file, map[int]string{129: "visits"}, map[int]map[int]htmlFieldMeta{129: {1: {ColName: "code", RawName: "Code", FieldType: "TEXT"}}}, map[string]string{"visits.code": "TEXT"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got["visits"]) != 2 || got["visits"][0]["code"] != "A" || got["visits"][1]["code"] != "B" {
		t.Fatalf("opaque record tokens were merged: %+v", got)
	}
}

func TestNativeDateOrderIsExplicit(t *testing.T) {
	value, err := coerceNativeValue("13/02/2026 14:15:16", "TIMESTAMP", "dmy")
	if err != nil || value != "2026-02-13T14:15:16Z" {
		t.Fatalf("declared native order was not applied: %v %v", value, err)
	}
	if _, err := coerceNativeValue("02/03/2026", "DATE", ""); err == nil {
		t.Fatal("ambiguous native date order was guessed")
	}
	if _, err := coerceNativeValue("02/03/2026 14:15:16", "TIMESTAMP", ""); err == nil {
		t.Fatal("ambiguous native timestamp order was guessed")
	}
	if _, err := AnalyzeAndConvert(MigrationRequest{DesignXML: `<FMPReport/>`, NativeDateOrder: "guess"}); err == nil {
		t.Fatal("unknown native date order was accepted")
	}
}

func TestNativeUnrepresentableValuesReturnNoPartialRows(t *testing.T) {
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := appendHBAM7TableCatalog(nil, 129, "Visits", []testHBAM7FieldSpec{
		{id: 1, name: "Number", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeNumber},
		{id: 2, name: "Text", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = append(payload, 0x28, 0x80, 1, 0x20, 5, 0x20, 1, 0x06, 1, 4)
	payload = append(payload, nativeEncoded([]byte("12-A"))...)
	payload = append(payload, 0x06, 2, 3)
	payload = append(payload, nativeEncoded([]byte{'A', 0, 'B'})...)
	copy(file[2*4096+20:], payload)
	fields := map[int]map[int]htmlFieldMeta{129: {
		1: {ColName: "number", RawName: "Number", FieldType: "NUMBER"},
		2: {ColName: "text", RawName: "Text", FieldType: "TEXT"},
	}}
	types := map[string]string{"visits.number": "NUMBER", "visits.text": "TEXT"}
	rows, _, err := readHBAM7Records(file, map[int]string{129: "visits"}, fields, types, "")
	issue, ok := err.(*nativeValueError)
	if rows != nil || !ok || issue.Records != 1 || issue.Counts["NUMBER"] != 1 || issue.Counts["NUL_TEXT"] != 1 {
		t.Fatal("unrepresentable source values were silently dropped or partially returned")
	}
	types["visits.number"] = "TEXT"
	rows, _, err = readHBAM7RecordsMode(file, map[int]string{129: "visits"}, fields, types, "", true)
	if err != nil || rows["visits"][0]["number"] != "12-A" || rows["visits"][0]["text"] != "A\x00B" {
		t.Fatal("raw qualification lost source text")
	}
}

func TestNativeNumbersDoNotPassThroughFloatingPoint(t *testing.T) {
	value, err := coerceNativeValue("9007199254740993", "NUMBER", "")
	if err != nil || value != "9007199254740993" {
		t.Fatal("native exact decimal was rounded")
	}
	for _, text := range []string{"1/3", "NaN", "Infinity"} {
		if _, err := coerceNativeValue(text, "NUMBER", ""); err == nil {
			t.Fatal("non-representable number was rounded or accepted")
		}
	}
}
