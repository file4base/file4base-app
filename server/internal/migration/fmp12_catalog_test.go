package migration

import (
	"encoding/binary"
	"strings"
	"testing"
)

// nativeCatalogFile builds a synthetic HBAM7 file whose own catalog names
// table 129 and its fields 1 and 2. Path pushes: 0x20 <1 byte>, 0x28 <2 bytes>
// (0x80 0x01 is id 129); 0x40 pops; 0x06 <key> <len> <data> is a keyed value.
func nativeCatalogFile(table string, fields ...string) []byte {
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	value := func(key byte, text string) []byte {
		enc := nativeEncoded([]byte(text))
		return append([]byte{0x06, key, byte(len(enc))}, enc...)
	}
	p := []byte{0x20, 3, 0x20, 16, 0x20, 5, 0x28, 0x80, 1}
	p = append(p, value(16, table)...)
	p = append(p, 0x40, 0x40, 0x40, 0x40)
	p = append(p, 0x28, 0x80, 1, 0x20, 3, 0x20, 5)
	for i, f := range fields {
		p = append(p, 0x20, byte(i+1))
		if len(f) > 6 {
			// A long name is stored as segments 1, 2 of sub-path 16.
			p = append(p, 0x20, 16)
			p = append(p, value(1, f[:4])...)
			p = append(p, value(2, f[4:])...)
			p = append(p, 0x40)
		} else {
			p = append(p, value(16, f)...)
		}
		p = append(p, 0x40)
	}
	copy(file[2*4096+20:], p)
	return file
}

func TestNativeCatalogNamesTablesAndFields(t *testing.T) {
	cat, err := readHBAM7Catalog(nativeCatalogFile("Visits", "Code", "Visit note"))
	if err != nil {
		t.Fatal(err)
	}
	if cat.tables[129] != "Visits" || cat.fields[129][1] != "Code" || cat.fields[129][2] != "Visit note" {
		t.Fatalf("catalog: %+v", cat)
	}
}

func TestNativeFileOfAnotherSolutionIsRefused(t *testing.T) {
	design := map[int]string{129: "visits"}
	fields := map[int]map[int]htmlFieldMeta{129: {1: {RawName: "Code"}, 2: {RawName: "Visit note"}, 3: {RawName: "Added later"}}}

	same, _ := readHBAM7Catalog(nativeCatalogFile("Visits", "Code", "Visit note"))
	if m := nativeDesignMismatches(same, design, fields); len(m) != 0 {
		t.Fatalf("the design's own file was refused: %v", m)
	}
	other, _ := readHBAM7Catalog(nativeCatalogFile("Invoices", "Number", "Amount due"))
	m := nativeDesignMismatches(other, design, fields)
	if len(m) != 3 || !strings.Contains(strings.Join(m, ";"), `"Invoices"`) {
		t.Fatalf("another solution's file must be refused, got %v", m)
	}
	if m := nativeDesignMismatches(same, map[int]string{130: "orders"}, nil); len(m) != 1 {
		t.Fatalf("a design table missing from the file must be reported, got %v", m)
	}
}
