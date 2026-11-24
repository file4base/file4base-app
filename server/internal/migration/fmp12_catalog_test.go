package migration

import (
	"encoding/binary"
	"strings"
	"testing"
)

func buildSyntheticHBAM7(payload []byte) []byte {
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	copy(file[2*4096+20:], payload)
	return file
}

func appendHBAM7SingleRow(payload []byte, tableID int, rowID byte, values map[byte]string) []byte {
	tHi := byte(0x80 | ((tableID - 128) >> 8))
	tLo := byte((tableID - 128) & 0xff)
	payload = append(payload, 0x28, tHi, tLo, 0x20, 5, 0x20, rowID)
	for fid := byte(1); fid < 32; fid++ {
		val, ok := values[fid]
		if !ok {
			continue
		}
		payload = append(payload, 0x06, fid, byte(len(val)))
		payload = append(payload, nativeEncoded([]byte(val))...)
	}
	payload = append(payload, 0x40, 0x40, 0x40)
	return payload
}

const designSolutionA = `<FMPReport><File name="SolutionA.fmp12"><BaseTableCatalog>` +
	`<BaseTable id="129" name="Contacts"><FieldCatalog>` +
	`<Field id="1" name="FullName" dataType="Text"/>` +
	`<Field id="2" name="Age" dataType="Number"/>` +
	`</FieldCatalog></BaseTable>` +
	`</BaseTableCatalog></File></FMPReport>`

// #130: pairing design A with an unrelated native .fmp12 file B that reuses
// table ID 129 and field IDs 1..2 must be refused before any record is read.
func TestNativeCatalogRejectsMismatchedTableAndFieldNames(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Invoices", []testHBAM7FieldSpec{
		{id: 1, name: "InvoiceNo", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Total", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeNumber},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "INV-001", 2: "42"})
	nativeB := buildSyntheticHBAM7(payload)

	converted, err := AnalyzeAndConvertNative(MigrationRequest{
		DesignXML:      designSolutionA,
		NativeFileName: "SolutionB.fmp12",
	}, nativeB)
	if err != nil {
		t.Fatalf("expected report rejection rather than hard parse error, got %v", err)
	}
	if converted.Report.Counts.RecordsReady != 0 || len(converted.TableRows) != 0 {
		t.Fatalf("mismatched .fmp12 produced ready records: ready=%d rows=%+v", converted.Report.Counts.RecordsReady, converted.TableRows)
	}
	if converted.Report.Counts.RejectedCount == 0 {
		t.Fatalf("expected catalog mismatch rejection, got report: %+v", converted.Report)
	}
	detail := converted.Report.Rejected[0].Detail
	for _, want := range []string{
		`attached file "SolutionB.fmp12"`,
		`design declares "SolutionA.fmp12"`,
		`table 129: design has "Contacts", native has "Invoices"`,
		`field 1: design has "FullName", native has "InvoiceNo"`,
		`field 2: design has "Age", native has "Total"`,
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("rejection detail %q missing %q", detail, want)
		}
	}
}

// #130: same table and field names, but field 2 has a different data type in
// the native file (TEXT instead of NUMBER).
func TestNativeCatalogRejectsFieldTypeMismatch(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Contacts", []testHBAM7FieldSpec{
		{id: 1, name: "FullName", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Age", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Ada", 2: "36"})
	native := buildSyntheticHBAM7(payload)

	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: designSolutionA}, native)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Report.Counts.RecordsReady != 0 || converted.Report.Counts.RejectedCount == 0 {
		t.Fatalf("expected field type mismatch rejection, got counts: %+v", converted.Report.Counts)
	}
	detail := converted.Report.Rejected[0].Detail
	if !strings.Contains(detail, `field 2 ("Age"): design has NUMBER, native has TEXT`) {
		t.Fatalf("unexpected field type mismatch detail: %q", detail)
	}
}

// #130: partial matches (extra tables or extra/missing fields in the native
// file) must also be refused and name every differing table and field.
func TestNativeCatalogRejectsPartialMatch(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Contacts", []testHBAM7FieldSpec{
		{id: 1, name: "FullName", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		// field 2 ("Age") missing; field 3 ("Email") extra
		{id: 3, name: "Email", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = appendHBAM7TableCatalog(payload, 130, "Orders", []testHBAM7FieldSpec{
		{id: 1, name: "OrderNo", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Ada"})
	native := buildSyntheticHBAM7(payload)

	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: designSolutionA}, native)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Report.Counts.RecordsReady != 0 || converted.Report.Counts.RejectedCount == 0 {
		t.Fatalf("expected partial match rejection, got counts: %+v", converted.Report.Counts)
	}
	detail := converted.Report.Rejected[0].Detail
	for _, want := range []string{
		`field 2 ("Age"): missing from native .fmp12 catalog`,
		`field 3 ("Email"): present in native .fmp12 but absent from design`,
		`table 130 ("Orders"): present in native .fmp12 but absent from design`,
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("partial match detail %q missing %q", detail, want)
		}
	}
}

// #130: an HBAM7 binary with no internal catalog cannot be verified against the
// design and must be refused rather than trusting numeric IDs blindly.
func TestNativeCatalogRejectsCataloglessFile(t *testing.T) {
	payload := appendHBAM7SingleRow(nil, 129, 1, map[byte]string{1: "Ada", 2: "36"})
	native := buildSyntheticHBAM7(payload)

	converted, err := AnalyzeAndConvertNative(MigrationRequest{DesignXML: designSolutionA}, native)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Report.Counts.RecordsReady != 0 || converted.Report.Counts.RejectedCount == 0 {
		t.Fatalf("catalog-less native file was accepted: %+v", converted.Report.Counts)
	}
	if !strings.Contains(converted.Report.Rejected[0].Detail, "contains no table catalog") {
		t.Fatalf("unexpected rejection detail: %q", converted.Report.Rejected[0].Detail)
	}
}

// #130: a matching catalog succeeds even when the copy was renamed on disk.
func TestNativeCatalogAcceptsMatchingCatalogOnRenamedCopy(t *testing.T) {
	payload := appendHBAM7TableCatalog(nil, 129, "Contacts", []testHBAM7FieldSpec{
		{id: 1, name: "FullName", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeText},
		{id: 2, name: "Age", fieldClass: hbamFieldClassNormal, dataType: hbamDataTypeNumber},
	})
	payload = appendHBAM7SingleRow(payload, 129, 1, map[byte]string{1: "Ada", 2: "36"})
	native := buildSyntheticHBAM7(payload)

	converted, err := AnalyzeAndConvertNative(MigrationRequest{
		DesignXML:      designSolutionA,
		NativeFileName: "SolutionA_backup_2026.fmp12",
	}, native)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Report.Counts.RejectedCount != 0 || converted.Report.Counts.RecordsReady != 1 {
		t.Fatalf("matching catalog on renamed copy failed: %+v", converted.Report)
	}
	if got := converted.TableRows["contacts"][0]["fullname"]; got != "Ada" {
		t.Fatalf("unexpected extracted row: %+v", converted.TableRows["contacts"])
	}
}
