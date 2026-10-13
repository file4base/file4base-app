package migration

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dataio"
)

var reUUIDHex = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// populateDataRows populates tableRows from optional FMPXMLRESULT data files
// and/or an attached binary .fmp12 file (base64-encoded in req.FMP12Base64).
func populateDataRows(
	req MigrationRequest,
	canonicalTableName map[string]string,
	tableIDByName map[string]string,
	colTypeByTableCol map[string]string,
	fmpTableMeta map[int]string,
	fmpFieldMeta map[int]map[int]htmlFieldMeta,
	tableRows map[string][]map[string]interface{},
	report *ConversionReport,
) error {
	// 1. Parse optional FMPXMLRESULT / XML data files per table
	if len(req.DataFiles) > 0 {
		tableKeys := make([]string, 0, len(req.DataFiles))
		for k := range req.DataFiles {
			tableKeys = append(tableKeys, k)
		}
		sort.Strings(tableKeys)
		for _, rawTableKey := range tableKeys {
			xmlContent := strings.TrimSpace(req.DataFiles[rawTableKey])
			if xmlContent == "" {
				continue
			}
			if IsFMP12Binary(rawTableKey, []byte(xmlContent)) {
				return ErrBinaryFMP12
			}
			sqlTable := canonicalTableName[strings.ToLower(strings.TrimSpace(rawTableKey))]
			if sqlTable == "" {
				sqlTable = sanitizeIdentifier(rawTableKey)
			}
			if tableIDByName[sqlTable] == "" {
				addReportItem(report, ReportItem{
					Category:   CategoryRejected,
					Domain:     "data",
					SourceName: rawTableKey,
					Detail:     fmt.Sprintf("Data file targeting unknown table %q was rejected", rawTableKey),
				})
				continue
			}
			parsed, err := dataio.Parse([]byte(xmlContent), dataio.Options{Format: dataio.FormatXML})
			if err != nil {
				addReportItem(report, ReportItem{
					Category:   CategoryRejected,
					Domain:     "data",
					SourceName: rawTableKey,
					TargetName: sqlTable,
					Detail:     fmt.Sprintf("Failed parsing FMPXMLRESULT / XML records: %v", err),
				})
				continue
			}
			rows := make([]map[string]interface{}, 0, len(parsed.Rows))
			for _, rawRow := range parsed.Rows {
				rec := make(map[string]interface{})
				for colIdx, colHeader := range parsed.Columns {
					if colIdx >= len(rawRow) {
						continue
					}
					valStr := strings.TrimSpace(rawRow[colIdx])
					if valStr == "" {
						continue
					}
					colName := sanitizeIdentifier(colHeader)
					fType := colTypeByTableCol[sqlTable+"."+colName]
					if fType == "CALCULATION" || fType == "SUMMARY" {
						continue
					}
					rec[colName] = coerceDataValue(valStr, fType)
				}
				if len(rec) > 0 {
					rows = append(rows, rec)
				}
			}
			tableRows[sqlTable] = rows
			report.Counts.RecordsReady += len(rows)
			addReportItem(report, ReportItem{
				Category:   CategoryPreserved,
				Domain:     "data",
				SourceName: rawTableKey,
				TargetName: sqlTable,
				Detail:     fmt.Sprintf("Parsed %d records from FMPXMLRESULT dataset", len(rows)),
			})
		}
	}

	// 2. Extract records & embedded container blobs from optional binary .fmp12 (HBAM2)
	if strings.TrimSpace(req.FMP12Base64) != "" {
		rawBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.FMP12Base64))
		if err != nil {
			return fmt.Errorf("invalid base64 in fmp12_base64: %w", err)
		}
		extracted, containersCount, err := extractFMP12Records(rawBytes, fmpTableMeta, fmpFieldMeta, colTypeByTableCol)
		if err != nil {
			addReportItem(report, ReportItem{
				Category:   CategoryRejected,
				Domain:     "data",
				SourceName: "fmp12_binary",
				Detail:     fmt.Sprintf("Failed extracting records from .fmp12 binary: %v", err),
			})
		} else {
			tableNames := make([]string, 0, len(extracted))
			for k := range extracted {
				tableNames = append(tableNames, k)
			}
			sort.Strings(tableNames)
			for _, tbl := range tableNames {
				rows := extracted[tbl]
				if len(rows) == 0 {
					continue
				}
				tableRows[tbl] = append(tableRows[tbl], rows...)
				report.Counts.RecordsReady += len(rows)
				detail := fmt.Sprintf("Extracted %d records from .fmp12 HBAM2 binary page tree", len(rows))
				if containersCount[tbl] > 0 {
					detail = fmt.Sprintf("Extracted %d records (including %d embedded container images) from .fmp12 HBAM2 binary", len(rows), containersCount[tbl])
				}
				addReportItem(report, ReportItem{
					Category:   CategoryPreserved,
					Domain:     "data",
					SourceName: tbl + " (.fmp12)",
					TargetName: tbl,
					Detail:     detail,
				})
			}
		}
	}

	// 3. Evaluate same-table calculation fields & cross-table List() fields on extracted rows
	precomputeExtractedCalculations(tableRows, fmpTableMeta, fmpFieldMeta, colTypeByTableCol)

	return nil
}

// extractFMP12Records walks the HBAM2 leaf B-tree pages of a FileMaker 12+
// (.fmp12) binary file and extracts all table rows and embedded JPEG/PNG
// container streams.
func extractFMP12Records(
	data []byte,
	fmpTableMeta map[int]string,
	fmpFieldMeta map[int]map[int]htmlFieldMeta,
	colTypeByTableCol map[string]string,
) (map[string][]map[string]interface{}, map[string]int, error) {
	const pageSize = 4096
	if len(data) < pageSize*3 || !bytes.Contains(data[:minInt(len(data), pageSize)], []byte("HBAM2")) {
		return nil, nil, fmt.Errorf("not a valid FileMaker HBAM2 (.fmp12) file")
	}

	numPages := len(data) / pageSize
	// Follow the doubly-linked leaf page chain starting at page 2; also collect
	// any unvisited leaf pages (flags == 0, level == 0) in page order.
	visited := make(map[int]bool)
	var leafPayloads [][]byte

	curr := 2
	for curr > 0 && curr < numPages && !visited[curr] {
		visited[curr] = true
		blk := data[curr*pageSize : (curr+1)*pageSize]
		flags := binary.BigEndian.Uint16(blk[0:2])
		level := binary.BigEndian.Uint16(blk[2:4])
		nextPg := int(binary.BigEndian.Uint32(blk[8:12]))
		freeBytes := int(binary.BigEndian.Uint16(blk[14:16]))
		if flags == 0 && level == 0 {
			endOff := pageSize - freeBytes
			if endOff > pageSize || endOff < 20 {
				endOff = pageSize
			}
			leafPayloads = append(leafPayloads, blk[20:endOff])
		}
		curr = nextPg
	}

	// Step A: Extract embedded JPEG container blobs keyed by container index (1, 2, 3...)
	containers := extractFMP12Containers(leafPayloads)

	// Step B: Reassemble continuous record streams per table index (fmpTableID = 128 + tblIdx)
	tableStreams := make(map[int][]byte)
	var tableOrder []int
	ensureTableStream := func(fmpTblID int) {
		if _, exists := tableStreams[fmpTblID]; !exists {
			tableStreams[fmpTblID] = nil
			tableOrder = append(tableOrder, fmpTblID)
		}
	}

	for _, p := range leafPayloads {
		// 1. Continuation page for a table's record tree (20 05):
		// Starts with: 28 80 <tbl_idx> 20 05 20 <rec_id> ...
		if len(p) >= 7 && p[0] == 0x28 && p[1] == 0x80 && p[3] == 0x20 && p[4] == 0x05 && p[5] == 0x20 {
			fmpTblID := 128 + int(p[2])
			if fmpTableMeta[fmpTblID] != "" {
				ensureTableStream(fmpTblID)
				endIdx := len(p)
				for k := 7; k+2 < len(p); k++ {
					if p[k] == 0x28 && p[k+1] == 0x80 && p[k+2] >= 1 && p[k+2] <= 32 {
						endIdx = k
						break
					}
				}
				tableStreams[fmpTblID] = append(tableStreams[fmpTblID], p[7:endIdx]...)
			}
		}

		// 2. Primary record section start: 40 40 40 20 05 80 preceded earlier on the page by 28 80 <tbl_idx>
		idx40 := bytes.Index(p, []byte{0x40, 0x40, 0x40, 0x20, 0x05, 0x80})
		if idx40 >= 0 {
			fmpTblID := 0
			for back := idx40 - 1; back >= 0; back-- {
				if back+2 < len(p) && p[back] == 0x28 && p[back+1] == 0x80 {
					cand := 128 + int(p[back+2])
					if fmpTableMeta[cand] != "" {
						fmpTblID = cand
						break
					}
				}
			}
			if fmpTblID != 0 {
				ensureTableStream(fmpTblID)
				endIdx := len(p)
				for k := idx40 + 6; k+2 < len(p); k++ {
					if p[k] == 0x28 && p[k+1] == 0x80 && p[k+2] >= 1 && p[k+2] <= 32 {
						endIdx = k
						break
					}
				}
				tableStreams[fmpTblID] = append(tableStreams[fmpTblID], p[idx40+6:endIdx]...)
			}
		}
	}

	outRows := make(map[string][]map[string]interface{})
	containerCounts := make(map[string]int)

	for _, fmpTblID := range tableOrder {
		sqlTable := fmpTableMeta[fmpTblID]
		fieldsMeta := fmpFieldMeta[fmpTblID]
		if sqlTable == "" || len(fieldsMeta) == 0 {
			continue
		}
		recOrder, recMap := splitTableRecordStream(tableStreams[fmpTblID])
		for _, recID := range recOrder {
			rawStream := recMap[recID]
			row, hasContainer := decodeFMP12RecordFields(rawStream, sqlTable, fieldsMeta, colTypeByTableCol, containers)
			if len(row) > 0 {
				outRows[sqlTable] = append(outRows[sqlTable], row)
				if hasContainer {
					containerCounts[sqlTable]++
				}
			}
		}
	}

	return outRows, containerCounts, nil
}

// splitTableRecordStream splits a table's reassembled 20 05 record tree stream
// into individual record byte slices by tracking nested subtrees (20 <id> 80 ... 40).
func splitTableRecordStream(s []byte) ([]int, map[int][]byte) {
	var recOrder []int
	recs := make(map[int][]byte)
	curRec := -1
	depth := 0
	i := 0

	startRec := func(id int) {
		curRec = id
		if _, exists := recs[id]; !exists {
			recOrder = append(recOrder, id)
		}
	}

	for i < len(s) {
		b := s[i]
		if curRec < 0 && b == 0x20 && i+3 <= len(s) && s[i+2] == 0x80 {
			startRec(int(s[i+1]))
			i += 3
			continue
		}
		if b == 0x40 {
			if depth == 0 && i+2 <= len(s) && s[i+1] == 0x40 {
				// 40 40 at depth 0 closes the 20 05 record tree
				break
			}
			if depth == 0 && i+4 <= len(s) && s[i+1] == 0x20 && s[i+3] == 0x80 {
				startRec(int(s[i+2]))
				i += 4
				continue
			}
			if depth > 0 {
				depth--
			}
			if curRec >= 0 {
				recs[curRec] = append(recs[curRec], b)
			}
			i++
			continue
		}
		if b == 0x20 && i+3 <= len(s) && s[i+2] == 0x80 {
			depth++
			if curRec >= 0 {
				recs[curRec] = append(recs[curRec], s[i:i+3]...)
			}
			i += 3
			continue
		}
		switch b {
		case 0x01, 0x02, 0x03, 0x04, 0x05:
			sz := int(b)*2 + 1
			if b == 0x01 {
				sz = 3
			} else if b == 0x02 {
				sz = 4
			} else if b == 0x03 {
				sz = 6
			} else if b == 0x04 {
				sz = 8
			} else if b == 0x05 {
				sz = 10
			}
			end := minInt(i+sz, len(s))
			if curRec >= 0 {
				recs[curRec] = append(recs[curRec], s[i:end]...)
			}
			i = end
		case 0x06:
			if i+3 <= len(s) {
				ln := int(s[i+2])
				end := minInt(i+3+ln, len(s))
				if curRec >= 0 {
					recs[curRec] = append(recs[curRec], s[i:end]...)
				}
				i = end
			} else {
				i++
			}
		case 0x07:
			if i+4 <= len(s) {
				ln := int(binary.BigEndian.Uint16(s[i+2 : i+4]))
				end := minInt(i+4+ln, len(s))
				if curRec >= 0 {
					recs[curRec] = append(recs[curRec], s[i:end]...)
				}
				i = end
			} else {
				i++
			}
		case 0xfc:
			if i+6 <= len(s) {
				i += 6
			} else {
				i++
			}
		case 0xfb:
			if i+3 <= len(s) {
				i += 3
			} else {
				i++
			}
		default:
			if curRec >= 0 {
				recs[curRec] = append(recs[curRec], b)
			}
			i++
		}
	}
	return recOrder, recs
}

func decodeFMP12RecordFields(
	b []byte,
	sqlTable string,
	fieldsMeta map[int]htmlFieldMeta,
	colTypeByTableCol map[string]string,
	containers map[int][]byte,
) (map[string]interface{}, bool) {
	row := make(map[string]interface{})
	hasContainer := false
	i := 0

	setFieldVal := func(fid int, rawVal []byte) {
		meta, ok := fieldsMeta[fid]
		if !ok {
			return
		}
		fType := colTypeByTableCol[sqlTable+"."+meta.ColName]
		if fType == "CALCULATION" || fType == "SUMMARY" {
			return
		}
		if fType == "CONTAINER" {
			return
		}
		strVal := strings.TrimSpace(decodeFMP12Text(rawVal))
		if strVal == "" {
			return
		}
		row[meta.ColName] = coerceDataValue(strVal, fType)
	}

	for i < len(b) {
		tag := b[i]
		switch {
		case tag == 0xfc && i+6 <= len(b):
			i += 6
		case tag == 0xfb && i+3 <= len(b):
			i += 3
		case tag == 0x01 && i+3 <= len(b):
			setFieldVal(int(b[i+1]), b[i+2:i+3])
			i += 3
		case tag == 0x02 && i+4 <= len(b):
			setFieldVal(int(b[i+1]), b[i+2:i+4])
			i += 4
		case tag == 0x03 && i+6 <= len(b):
			setFieldVal(int(b[i+1]), b[i+2:i+6])
			i += 6
		case tag == 0x04 && i+8 <= len(b):
			setFieldVal(int(b[i+1]), b[i+2:i+8])
			i += 8
		case tag == 0x05 && i+10 <= len(b):
			setFieldVal(int(b[i+1]), b[i+2:i+10])
			i += 10
		case tag == 0x06 && i+3 <= len(b):
			fid := int(b[i+1])
			ln := int(b[i+2])
			end := minInt(i+3+ln, len(b))
			setFieldVal(fid, b[i+3:end])
			i = end
		case tag == 0x07 && i+4 <= len(b):
			fid := int(b[i+1])
			ln := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
			end := minInt(i+4+ln, len(b))
			setFieldVal(fid, b[i+4:end])
			i = end
		case tag == 0x20 && i+3 <= len(b) && b[i+2] == 0x80:
			fid := int(b[i+1])
			meta, ok := fieldsMeta[fid]
			i += 3
			for i < len(b) {
				if b[i] == 0x40 {
					i++
					break
				}
				switch {
				case b[i] == 0x06 && i+3 <= len(b):
					subID := int(b[i+1])
					ln := int(b[i+2])
					end := minInt(i+3+ln, len(b))
					val := b[i+3 : end]
					if ok && subID == 1 {
						if meta.FieldType == "CONTAINER" || colTypeByTableCol[sqlTable+"."+meta.ColName] == "CONTAINER" {
							// First 2 XOR'd bytes encode the ASCII container index ("01", "02", "03")
							if len(val) >= 2 {
								cIdxStr := string([]byte{val[0] ^ 0x5A, val[1] ^ 0x5A})
								if cID, err := strconv.Atoi(cIdxStr); err == nil {
									if imgBytes, exists := containers[cID]; exists {
										row[meta.ColName] = imgBytes
										hasContainer = true
									}
								}
							}
						} else {
							setFieldVal(fid, val)
						}
					}
					i = end
				case b[i] == 0x07 && i+4 <= len(b):
					subID := int(b[i+1])
					ln := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
					end := minInt(i+4+ln, len(b))
					if ok && subID == 1 {
						setFieldVal(fid, b[i+4:end])
					}
					i = end
				case b[i] == 0x05 && i+10 <= len(b):
					i += 10
				case b[i] == 0x04 && i+8 <= len(b):
					i += 8
				case b[i] == 0x03 && i+6 <= len(b):
					i += 6
				case b[i] == 0x02 && i+4 <= len(b):
					i += 4
				case b[i] == 0x01 && i+3 <= len(b):
					i += 3
				default:
					i++
				}
			}
		default:
			i++
		}
	}
	return row, hasContainer
}

func decodeFMP12Text(raw []byte) string {
	xored := make([]byte, len(raw))
	for i, c := range raw {
		xored[i] = c ^ 0x5A
	}
	var out strings.Builder
	inFBLigature := false
	for i := 0; i < len(xored); i++ {
		b := xored[i]
		// FileMaker SCSU window switch 0x1B 0x9E for Latin ligatures (fi, fl)
		if b == 0x1B && i+1 < len(xored) {
			next := xored[i+1]
			if next == 0x9E {
				inFBLigature = true
				i++
				continue
			}
			if next == 0x5A {
				inFBLigature = false
				i++
				continue
			}
		}
		if inFBLigature {
			switch b {
			case 0x81:
				out.WriteString("fi")
			case 0x82:
				out.WriteString("fl")
			}
			inFBLigature = false
			continue
		}
		switch {
		case b == '\r' || b == '\n' || b == 0x0B:
			out.WriteByte('\n')
		case b == '\t':
			out.WriteByte('\t')
		case b >= 32 && b < 127:
			out.WriteByte(b)
		case b == 0xB0:
			out.WriteString("°")
		case b == 0xE9:
			out.WriteString("é")
		}
	}
	return out.String()
}

// extractFMP12Containers reconstructs embedded JPEG streams from HBAM2 leaf payloads.
func extractFMP12Containers(leafPayloads [][]byte) map[int][]byte {
	containers := make(map[int][]byte)
	curID := 0
	nextID := 0
	expectedLen := 0
	var curBuf []byte

	flush := func() {
		if curID > 0 && len(curBuf) > 0 {
			if expectedLen > 0 && len(curBuf) > expectedLen {
				curBuf = curBuf[:expectedLen]
			}
			containers[curID] = curBuf
		}
		curID = 0
		expectedLen = 0
		curBuf = nil
	}

	jpegMarker := []byte{0x38, 0x04, 'J', 'P', 'E', 'G', 0x80, 0x03, 0x00}
	for _, p := range leafPayloads {
		// 1. Continuation page for active container: starts with 28 80 01 20 1f 20 05 20 <id> 38 04 JPEG
		if curID > 0 && len(p) >= 16 && p[0] == 0x28 && p[1] == 0x80 && bytes.Equal(p[9:15], []byte{0x38, 0x04, 'J', 'P', 'E', 'G'}) && p[15] != 0x80 {
			cid := int(p[8])
			if cid == curID {
				pos := 15
				for pos < len(p) {
					if p[pos] == 0x07 && pos+4 <= len(p) {
						ln := int(binary.BigEndian.Uint16(p[pos+2 : pos+4]))
						end := minInt(pos+4+ln, len(p))
						curBuf = append(curBuf, p[pos+4:end]...)
						pos = end
					} else if p[pos] == 0x0f && pos+5 <= len(p) && p[pos+1] == 0x80 {
						ln := int(binary.BigEndian.Uint16(p[pos+3 : pos+5]))
						end := minInt(pos+5+ln, len(p))
						curBuf = append(curBuf, p[pos+5:end]...)
						pos = end
					} else if p[pos] == 0x06 && pos+3 <= len(p) {
						ln := int(p[pos+2])
						end := minInt(pos+3+ln, len(p))
						curBuf = append(curBuf, p[pos+3:end]...)
						pos = end
					} else {
						break
					}
				}
				if expectedLen > 0 && len(curBuf) >= expectedLen {
					flush()
				}
			}
		}

		// 2. Look for JPEG header chunk: ... 38 04 4a 50 45 47 80 03 00 <4_byte_len>
		if idx := bytes.Index(p, jpegMarker); idx >= 0 && idx+len(jpegMarker)+4 <= len(p) {
			flush()
			nextID++
			curID = nextID
			lenOff := idx + len(jpegMarker)
			expectedLen = int(binary.BigEndian.Uint32(p[lenOff : lenOff+4]))
			pos := lenOff + 4
			for pos < len(p) {
				if p[pos] == 0x07 && pos+4 <= len(p) {
					ln := int(binary.BigEndian.Uint16(p[pos+2 : pos+4]))
					end := minInt(pos+4+ln, len(p))
					curBuf = append(curBuf, p[pos+4:end]...)
					pos = end
				} else if p[pos] == 0x0f && pos+5 <= len(p) && p[pos+1] == 0x80 {
					ln := int(binary.BigEndian.Uint16(p[pos+3 : pos+5]))
					end := minInt(pos+5+ln, len(p))
					curBuf = append(curBuf, p[pos+5:end]...)
					pos = end
				} else {
					break
				}
			}
		}
	}
	flush()
	return containers
}

// precomputeExtractedCalculations evaluates same-table calculations (such as
// yield_calc and ingredient_formatted) and cross-table List() aggregations
// (such as recipes.tags and recipes.ingredients_formatted) so imported rows
// immediately have their calculated values populated.
func precomputeExtractedCalculations(
	tableRows map[string][]map[string]interface{},
	fmpTableMeta map[int]string,
	fmpFieldMeta map[int]map[int]htmlFieldMeta,
	colTypeByTableCol map[string]string,
) {
	// 1. Evaluate same-table calculation formulas on recipe_ingredients and recipes
	for fmpTblID, sqlTable := range fmpTableMeta {
		rows := tableRows[sqlTable]
		if len(rows) == 0 {
			continue
		}
		fields := fmpFieldMeta[fmpTblID]
		knownCols := map[string]string{"id": "id"}
		for _, fm := range fields {
			knownCols[strings.ToLower(fm.RawName)] = fm.ColName
			knownCols[fm.ColName] = fm.ColName
		}
		for _, fm := range fields {
			if colTypeByTableCol[sqlTable+"."+fm.ColName] != "CALCULATION" {
				continue
			}
			formula := extractHTMLFormula(fm.RawOptions, sqlTable, sqlTable, knownCols)
			exprStr, _, ok := translateAndValidateFormula(formula)
			if !ok {
				continue
			}
			expr, err := calc.Parse(exprStr)
			if err != nil {
				continue
			}
			for _, r := range rows {
				if val, err := calc.Evaluate(expr, calc.Record(r), calc.ResultText); err == nil && val != nil {
					r[fm.ColName] = val
				}
			}
		}
	}

	// 2. Populate cross-table List() fields on recipes (tags, ingredients_formatted)
	recipesRows := tableRows["recipes"]
	if len(recipesRows) == 0 {
		return
	}
	ingRows := tableRows["recipe_ingredients"]
	tagRows := tableRows["recipe_tags"]

	for _, rec := range recipesRows {
		recID := strings.ToLower(fmt.Sprint(rec["id"]))
		if recID == "" {
			continue
		}
		if len(tagRows) > 0 {
			var tags []string
			for _, tr := range tagRows {
				if strings.EqualFold(fmt.Sprint(tr["recipe_id"]), recID) {
					if tVal := strings.TrimSpace(fmt.Sprint(tr["tag"])); tVal != "" && tr["tag"] != nil {
						tags = append(tags, tVal)
					}
				}
			}
			if len(tags) > 0 {
				rec["tags"] = strings.Join(tags, ", ")
			}
		}
		if len(ingRows) > 0 {
			var ings []string
			for _, ir := range ingRows {
				if strings.EqualFold(fmt.Sprint(ir["recipe_id"]), recID) {
					if fmtVal := strings.TrimSpace(fmt.Sprint(ir["ingredient_formatted"])); fmtVal != "" && ir["ingredient_formatted"] != nil {
						ings = append(ings, fmtVal)
					}
				}
			}
			if len(ings) > 0 {
				rec["ingredients_formatted"] = strings.Join(ings, "\n")
			}
		}
	}
}

// parseFractionOrFloat parses numbers including vulgar/mixed fractions like
// "1/3", "3/4", "1 1/4" into float64.
func parseFractionOrFloat(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	// Mixed fraction: "1 1/4"
	parts := strings.Fields(s)
	if len(parts) == 2 {
		whole, err1 := strconv.ParseFloat(parts[0], 64)
		frac, ok2 := parseSimpleFraction(parts[1])
		if err1 == nil && ok2 {
			val := whole + frac
			return math.Round(val*10000) / 10000, true
		}
	}
	if len(parts) == 1 {
		if frac, ok := parseSimpleFraction(parts[0]); ok {
			return math.Round(frac*10000) / 10000, true
		}
	}
	return 0, false
}

func parseSimpleFraction(s string) (float64, bool) {
	slash := strings.IndexByte(s, '/')
	if slash <= 0 || slash >= len(s)-1 {
		return 0, false
	}
	num, err1 := strconv.ParseFloat(strings.TrimSpace(s[:slash]), 64)
	den, err2 := strconv.ParseFloat(strings.TrimSpace(s[slash+1:]), 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0, false
	}
	return num / den, true
}
