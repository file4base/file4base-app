package migration

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// HBAM7 field class and data type codes observed in [tableID].[3].[5].[fieldID] key 2.
const (
	hbamFieldClassNormal      byte = 0x01
	hbamFieldClassCalculation byte = 0x02
	hbamFieldClassSummary     byte = 0x03

	hbamDataTypeText      byte = 0x01
	hbamDataTypeNumber    byte = 0x02
	hbamDataTypeDate      byte = 0x03
	hbamDataTypeTime      byte = 0x04
	hbamDataTypeTimestamp byte = 0x05
	hbamDataTypeContainer byte = 0x06
)

type hbam7FieldCatalogEntry struct {
	ID         int
	Name       string
	FieldClass byte
	DataType   byte
	HasType    bool
}

type hbam7Catalog struct {
	Tables       map[int]string
	Fields       map[int]map[int]*hbam7FieldCatalogEntry
	RecordTables map[int]bool
}

type hbam7CatalogCollector struct {
	tablePresent map[int]bool
	tableNames   map[int][]byte
	fieldEntries map[int]map[int]*hbam7FieldCatalogEntry
	fieldNames   map[int]map[int][]byte
	recordTables map[int]bool
}

func newHBAM7CatalogCollector() *hbam7CatalogCollector {
	return &hbam7CatalogCollector{
		tablePresent: map[int]bool{},
		tableNames:   map[int][]byte{},
		fieldEntries: map[int]map[int]*hbam7FieldCatalogEntry{},
		fieldNames:   map[int]map[int][]byte{},
		recordTables: map[int]bool{},
	}
}

func (col *hbam7CatalogCollector) observe(c hbamChunk) {
	if len(c.path) < 3 {
		return
	}
	p0 := hbamPathInt(c.path[0])
	p1 := hbamPathInt(c.path[1])
	p2 := hbamPathInt(c.path[2])

	// Base table catalog: [3].[16].[5].[tableID] (key 16 = name, or [3].[16].[5].[tableID].[16] segments).
	if p0 == 3 && p1 == 16 && p2 == 5 && len(c.path) >= 4 {
		tid := hbamPathInt(c.path[3])
		if tid < 129 {
			return
		}
		col.tablePresent[tid] = true
		if len(c.path) == 4 && (c.kind == 'v' || c.kind == 'g') && c.key == 16 {
			col.tableNames[tid] = append([]byte(nil), c.data...)
		} else if len(c.path) == 5 && hbamPathInt(c.path[4]) == 16 && (c.kind == 'v' || c.kind == 'g') && c.key != 0 {
			col.tableNames[tid] = append(col.tableNames[tid], c.data...)
		}
		return
	}

	// Field catalog: [tableID].[3].[5].[fieldID] (key 16 = name, key 2 = field class/type header).
	if p0 >= 129 && p1 == 3 && p2 == 5 && len(c.path) >= 4 {
		tid := p0
		fid := hbamPathInt(c.path[3])
		if fid < 1 {
			return
		}
		if col.fieldEntries[tid] == nil {
			col.fieldEntries[tid] = map[int]*hbam7FieldCatalogEntry{}
		}
		entry := col.fieldEntries[tid][fid]
		if entry == nil {
			entry = &hbam7FieldCatalogEntry{ID: fid}
			col.fieldEntries[tid][fid] = entry
		}
		if len(c.path) == 4 && (c.kind == 'v' || c.kind == 'g') {
			switch c.key {
			case 16:
				if col.fieldNames[tid] == nil {
					col.fieldNames[tid] = map[int][]byte{}
				}
				col.fieldNames[tid][fid] = append([]byte(nil), c.data...)
			case 2:
				if len(c.data) >= 2 {
					entry.FieldClass = c.data[0]
					entry.DataType = c.data[1]
					entry.HasType = true
				}
			}
		} else if len(c.path) == 5 && hbamPathInt(c.path[4]) == 16 && (c.kind == 'v' || c.kind == 'g') && c.key != 0 {
			if col.fieldNames[tid] == nil {
				col.fieldNames[tid] = map[int][]byte{}
			}
			col.fieldNames[tid][fid] = append(col.fieldNames[tid][fid], c.data...)
		}
		return
	}

	// Record sections: [tableID].[5].[rowToken]...
	if p0 >= 129 && p1 == 5 && len(c.path[2]) > 0 {
		col.recordTables[p0] = true
	}
}

func (col *hbam7CatalogCollector) build() (*hbam7Catalog, error) {
	cat := &hbam7Catalog{
		Tables:       make(map[int]string, len(col.tablePresent)),
		Fields:       make(map[int]map[int]*hbam7FieldCatalogEntry, len(col.fieldEntries)),
		RecordTables: col.recordTables,
	}
	for tid := range col.tablePresent {
		name := ""
		if raw := col.tableNames[tid]; len(raw) > 0 {
			decoded, err := decodeHBAM7Text(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid native table %d catalog name: %w", tid, err)
			}
			name = decoded
		}
		cat.Tables[tid] = name
	}
	for tid, fMap := range col.fieldEntries {
		cat.Fields[tid] = make(map[int]*hbam7FieldCatalogEntry, len(fMap))
		for fid, entry := range fMap {
			copied := *entry
			if raw := col.fieldNames[tid][fid]; len(raw) > 0 {
				decoded, err := decodeHBAM7Text(raw)
				if err != nil {
					return nil, fmt.Errorf("invalid native table %d field %d catalog name: %w", tid, fid, err)
				}
				copied.Name = decoded
			}
			cat.Fields[tid][fid] = &copied
		}
	}
	return cat, nil
}

// nativeCatalogMismatchError reports that an attached .fmp12 file's internal
// table/field catalog does not match the attached design document (#130).
type nativeCatalogMismatchError struct {
	DesignFile string
	NativeFile string
	Diffs      []string
}

func (e *nativeCatalogMismatchError) Error() string {
	var header strings.Builder
	header.WriteString("native .fmp12 catalog does not match the design")
	if e.DesignFile != "" && e.NativeFile != "" && !sameFileStem(e.DesignFile, e.NativeFile) {
		fmt.Fprintf(&header, " (attached file %q, design declares %q)", e.NativeFile, e.DesignFile)
	}
	if len(e.Diffs) == 0 {
		return header.String()
	}
	const maxShown = 25
	shown := e.Diffs
	extra := 0
	if len(shown) > maxShown {
		extra = len(shown) - maxShown
		shown = shown[:maxShown]
	}
	header.WriteString(": ")
	header.WriteString(strings.Join(shown, "; "))
	if extra > 0 {
		fmt.Fprintf(&header, "; and %d more difference(s)", extra)
	}
	return header.String()
}

func sameFileStem(a, b string) bool {
	return strings.EqualFold(stripFMPExt(filepath.Base(strings.TrimSpace(a))), stripFMPExt(filepath.Base(strings.TrimSpace(b))))
}

func stripFMPExt(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range []string{".fmp12", ".fp7", ".fmp7"} {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

func verifyHBAM7Catalog(
	catalog *hbam7Catalog,
	tables map[int]string,
	tableRawNames map[int]string,
	fields map[int]map[int]htmlFieldMeta,
	designFile string,
	nativeFile string,
) error {
	if catalog == nil || len(catalog.Tables) == 0 {
		return &nativeCatalogMismatchError{
			DesignFile: designFile,
			NativeFile: nativeFile,
			Diffs:      []string{"native .fmp12 file contains no table catalog to verify against the design"},
		}
	}

	var diffs []string

	// Collect sorted union of all table IDs across design, native catalog, and native record sections.
	tableIDSet := map[int]bool{}
	for tid := range tables {
		tableIDSet[tid] = true
	}
	for tid := range catalog.Tables {
		tableIDSet[tid] = true
	}
	for tid := range catalog.RecordTables {
		tableIDSet[tid] = true
	}
	tableIDs := make([]int, 0, len(tableIDSet))
	for tid := range tableIDSet {
		tableIDs = append(tableIDs, tid)
	}
	sort.Ints(tableIDs)

	for _, tid := range tableIDs {
		designSQL, inDesign := tables[tid]
		nativeName, inNativeCatalog := catalog.Tables[tid]
		designRaw := tableRawNames[tid]
		designDisplay := designRaw
		if designDisplay == "" {
			designDisplay = designSQL
		}

		if inDesign && !inNativeCatalog {
			diffs = append(diffs, fmt.Sprintf("table %d (%q): missing from native .fmp12 catalog", tid, designDisplay))
			continue
		}
		if !inDesign {
			label := nativeName
			if label == "" {
				label = "<unnamed>"
			}
			diffs = append(diffs, fmt.Sprintf("table %d (%q): present in native .fmp12 but absent from design", tid, label))
			continue
		}

		// Table ID exists in both design and native catalog; verify name.
		if designRaw != "" {
			if nativeName != designRaw {
				diffs = append(diffs, fmt.Sprintf("table %d: design has %q, native has %q", tid, designRaw, nativeName))
			}
		} else if nativeName != "" && !strings.EqualFold(sanitizeIdentifier(nativeName), designSQL) {
			diffs = append(diffs, fmt.Sprintf("table %d: design has %q, native has %q", tid, designSQL, nativeName))
		}

		// Verify fields for this table.
		designFields := fields[tid]
		nativeFields := catalog.Fields[tid]
		fieldIDSet := map[int]bool{}
		for fid := range designFields {
			fieldIDSet[fid] = true
		}
		for fid := range nativeFields {
			fieldIDSet[fid] = true
		}
		fieldIDs := make([]int, 0, len(fieldIDSet))
		for fid := range fieldIDSet {
			fieldIDs = append(fieldIDs, fid)
		}
		sort.Ints(fieldIDs)

		for _, fid := range fieldIDs {
			dField, inD := designFields[fid]
			nField, inN := nativeFields[fid]
			if inD && !inN {
				dName := dField.RawName
				if dName == "" {
					dName = dField.ColName
				}
				diffs = append(diffs, fmt.Sprintf("table %d (%s) field %d (%q): missing from native .fmp12 catalog", tid, designDisplay, fid, dName))
				continue
			}
			if !inD {
				nName := nField.Name
				if nName == "" {
					nName = "<unnamed>"
				}
				diffs = append(diffs, fmt.Sprintf("table %d (%s) field %d (%q): present in native .fmp12 but absent from design", tid, designDisplay, fid, nName))
				continue
			}

			// Verify field name.
			dName := dField.RawName
			if dName != "" {
				if nField.Name != dName {
					diffs = append(diffs, fmt.Sprintf("table %d (%s) field %d: design has %q, native has %q", tid, designDisplay, fid, dName, nField.Name))
				}
			} else if nField.Name != "" && !strings.EqualFold(sanitizeIdentifier(nField.Name), dField.ColName) {
				diffs = append(diffs, fmt.Sprintf("table %d (%s) field %d: design has %q, native has %q", tid, designDisplay, fid, dField.ColName, nField.Name))
			}

			// Verify field kind & data type when recorded in the native catalog.
			if nField.HasType {
				if ok, dLabel, nLabel := hbam7FieldTypeMatches(nField, dField); !ok {
					fLabel := dName
					if fLabel == "" {
						fLabel = dField.ColName
					}
					diffs = append(diffs, fmt.Sprintf("table %d (%s) field %d (%q): design has %s, native has %s", tid, designDisplay, fid, fLabel, dLabel, nLabel))
				}
			}
		}
	}

	if len(diffs) > 0 {
		return &nativeCatalogMismatchError{
			DesignFile: designFile,
			NativeFile: nativeFile,
			Diffs:      diffs,
		}
	}
	return nil
}

func hbam7DataTypeName(dt byte) string {
	switch dt {
	case hbamDataTypeText:
		return "TEXT"
	case hbamDataTypeNumber:
		return "NUMBER"
	case hbamDataTypeDate:
		return "DATE"
	case hbamDataTypeTime:
		return "TIME"
	case hbamDataTypeTimestamp:
		return "TIMESTAMP"
	case hbamDataTypeContainer:
		return "CONTAINER"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02x)", dt)
	}
}

func hbam7FieldTypeLabel(entry *hbam7FieldCatalogEntry) string {
	dt := hbam7DataTypeName(entry.DataType)
	switch entry.FieldClass {
	case hbamFieldClassNormal:
		return dt
	case hbamFieldClassCalculation:
		return "CALCULATION(" + dt + ")"
	case hbamFieldClassSummary:
		return "SUMMARY(" + dt + ")"
	default:
		return fmt.Sprintf("CLASS(0x%02x)/%s", entry.FieldClass, dt)
	}
}

func hbam7FieldTypeMatches(native *hbam7FieldCatalogEntry, design htmlFieldMeta) (bool, string, string) {
	nLabel := hbam7FieldTypeLabel(native)
	dKind := strings.ToUpper(strings.TrimSpace(design.SourceKind))
	dType := strings.ToUpper(strings.TrimSpace(design.SourceDataType))
	if dType == "" {
		dType = strings.ToUpper(strings.TrimSpace(design.FieldType))
	}
	if dType == "" {
		// No type declared in design metadata; cannot contradict native type.
		return true, "", nLabel
	}
	// Normalize common FileMaker/HTML DDR aliases.
	switch dType {
	case "BINARY", "BLOB":
		dType = "CONTAINER"
	case "NUMERIC", "DECIMAL", "INTEGER", "FLOAT", "BOOLEAN", "BOOL":
		dType = "NUMBER"
	case "DATETIME":
		dType = "TIMESTAMP"
	case "CALCULATED":
		dType = "CALCULATION"
	}

	dLabel := dType
	if dKind != "" && dKind != "NORMAL" && dKind != dType {
		dLabel = dKind + "(" + dType + ")"
	}

	switch native.FieldClass {
	case hbamFieldClassCalculation:
		if dKind != "CALCULATION" && dType != "CALCULATION" {
			return false, dLabel, nLabel
		}
		if dType == "CALCULATION" {
			return true, dLabel, nLabel
		}
		return hbam7DataTypeMatches(native.DataType, dType), dLabel, nLabel
	case hbamFieldClassSummary:
		if dKind == "SUMMARY" || dType == "SUMMARY" {
			return true, dLabel, nLabel
		}
		return false, dLabel, nLabel
	case hbamFieldClassNormal:
		if dKind == "CALCULATION" || dKind == "SUMMARY" || dType == "CALCULATION" || dType == "SUMMARY" {
			return false, dLabel, nLabel
		}
		return hbam7DataTypeMatches(native.DataType, dType), dLabel, nLabel
	default:
		return true, dLabel, nLabel
	}
}

func hbam7DataTypeMatches(nativeDT byte, designType string) bool {
	switch nativeDT {
	case hbamDataTypeText:
		return designType == "TEXT"
	case hbamDataTypeNumber:
		return designType == "NUMBER"
	case hbamDataTypeDate:
		return designType == "DATE"
	case hbamDataTypeTime:
		// File4Base maps FileMaker Time fields to TIMESTAMP.
		return designType == "TIME" || designType == "TIMESTAMP"
	case hbamDataTypeTimestamp:
		return designType == "TIMESTAMP"
	case hbamDataTypeContainer:
		return designType == "CONTAINER"
	default:
		return true
	}
}
