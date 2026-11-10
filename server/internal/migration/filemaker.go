// Package migration converts FileMaker Database Design Report (DDR) /
// Save-a-Copy-as-XML design documents and FMPXMLRESULT record exports into
// File4Base solution bundles, producing a structured ConversionReport (#26).
//
// It also detects proprietary binary .fmp12 uploads so the API and client can
// reject them with actionable migration instructions instead of an opaque
// MessagePack decoding error.
package migration

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/google/uuid"
)

// ErrBinaryFMP12 indicates that the caller supplied a binary .fmp12 container
// rather than a File4Base bundle or a FileMaker XML export.
var ErrBinaryFMP12 = errors.New("binary .fmp12 files cannot be opened directly; export a FileMaker DDR / Save a Copy as XML and use the Assisted FileMaker Migration workflow")

// ErrInvalidMigrationXML indicates that the supplied design XML could not be
// parsed as a FileMaker DDR, FMPSaveAsXML, or File4Base design XML document.
var ErrInvalidMigrationXML = errors.New("invalid FileMaker design XML")

// ItemCategory classifies how each source artifact was handled during conversion.
type ItemCategory string

const (
	CategoryPreserved            ItemCategory = "preserved"
	CategoryTranslated           ItemCategory = "translated"
	CategoryUnsupported          ItemCategory = "unsupported"
	CategoryRejected             ItemCategory = "rejected"
	CategoryManualActionRequired ItemCategory = "manual_action_required"
)

// ReportItem describes a single schema object, formula, script step, account,
// or dataset processed by the converter.
type ReportItem struct {
	Category   ItemCategory `json:"category"`
	Domain     string       `json:"domain"` // table, field, occurrence, relationship, value_list, layout, script, trigger, account, data
	SourceName string       `json:"source_name"`
	TargetName string       `json:"target_name,omitempty"`
	Detail     string       `json:"detail"`
}

// SummaryCounts summarizes the conversion outcome at a glance.
type SummaryCounts struct {
	Tables               int `json:"tables"`
	Fields               int `json:"fields"`
	Occurrences          int `json:"table_occurrences"`
	Relationships        int `json:"relationships"`
	ValueLists           int `json:"value_lists"`
	Layouts              int `json:"layouts"`
	Scripts              int `json:"scripts"`
	Accounts             int `json:"accounts"`
	RecordsReady         int `json:"records_ready"`
	PreservedCount       int `json:"preserved_count"`
	TranslatedCount      int `json:"translated_count"`
	UnsupportedCount     int `json:"unsupported_count"`
	RejectedCount        int `json:"rejected_count"`
	ManualActionCount    int `json:"manual_action_count"`
}

// ConversionReport is the deterministic preflight and post-import audit report
// returned by POST /api/v1/solutions/migrate/preflight and /apply.
type ConversionReport struct {
	SourceFormat         string        `json:"source_format"`
	SolutionName         string        `json:"solution_name"`
	TriggersQuarantined  bool          `json:"triggers_quarantined"`
	AccountsDisabled     bool          `json:"accounts_disabled"`
	Counts               SummaryCounts `json:"counts"`
	Preserved            []ReportItem  `json:"preserved"`
	Translated           []ReportItem  `json:"translated"`
	Unsupported          []ReportItem  `json:"unsupported"`
	Rejected             []ReportItem  `json:"rejected"`
	ManualActionRequired []ReportItem  `json:"manual_action_required"`
}

// MigrationRequest is the payload accepted by the preflight and apply endpoints.
type MigrationRequest struct {
	SolutionName   string            `json:"solution_name,omitempty"`
	DesignXML      string            `json:"design_xml"`
	DataFiles      map[string]string `json:"data_files,omitempty"` // TableName -> FMPXMLRESULT XML content
	FMP12Base64    string            `json:"fmp12_base64,omitempty"`
	TargetDatabase string            `json:"target_database,omitempty"`
	OwnerUser      string            `json:"owner_user,omitempty"`
	OwnerPassword  string            `json:"owner_password,omitempty"`
}

// ConvertedSolution holds the ready-to-import File4Base SolutionBundle and
// any parsed table rows extracted from FMPXMLRESULT files.
type ConvertedSolution struct {
	Bundle     *schema.SolutionBundle
	TableRows  map[string][]map[string]interface{}
	Report     *ConversionReport
}

// IsFMP12Binary returns true when filename has a .fmp12/.fp7 extension or data
// starts with the FileMaker Pro binary header signature (HBAM2 / \x00\x01\x00\x00).
//
// An XML payload is never a binary database, and the check below says so before
// looking at the content: every FMPXMLRESULT export carries a <PRODUCT> element
// naming the product it came from, and FileMaker versions up to 12 spell that
// name "FileMaker Pro" (13 and later split it as NAME="FileMaker"
// VERSION="Pro 13.0v1"). Without the guard, the signature scan matched that
// element and a valid record export was rejected as a binary file.
func IsFMP12Binary(filename string, data []byte) bool {
	lower := strings.ToLower(strings.TrimSpace(filename))
	if strings.HasSuffix(lower, ".fmp12") || strings.HasSuffix(lower, ".fp7") || strings.HasSuffix(lower, ".fp5") {
		return true
	}
	if looksLikeXML(data) {
		return false
	}
	if len(data) >= 20 {
		if bytes.Contains(data[:minInt(len(data), 4096)], []byte("HBAM2")) ||
			bytes.Contains(data[:minInt(len(data), 4096)], []byte("FileMaker Pro")) {
			return true
		}
		// Classic FileMaker binary magic header (\x00\x01\x00\x00\x00)
		if data[0] == 0x00 && data[1] == 0x01 && data[2] == 0x00 && data[3] == 0x00 && data[4] == 0x00 {
			return true
		}
	}
	return false
}

// looksLikeXML reports whether data opens an XML document: an element or a
// declaration, after any byte order mark and leading whitespace.
func looksLikeXML(data []byte) bool {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("<"))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// supportedScriptSteps maps canonical File4Base script step names (lowercased)
// to their exact display spelling in File4Base.
var supportedScriptSteps = map[string]string{
	"go to layout":              "Go to Layout",
	"go to record/request/page": "Go to Record/Request/Page",
	"go to record":              "Go to Record/Request/Page",
	"enter browse mode":         "Enter Browse Mode",
	"enter find mode":           "Enter Find Mode",
	"enter preview mode":        "Enter Preview Mode",
	"new record/request":        "New Record/Request",
	"duplicate record/request":  "Duplicate Record/Request",
	"delete record/request":     "Delete Record/Request",
	"commit records/requests":   "Commit Records/Requests",
	"revert record/request":     "Revert Record/Request",
	"set field":                 "Set Field",
	"perform find":              "Perform Find",
	"show all records":          "Show All Records",
	"show omitted only":         "Show Omitted Only",
	"omit record":               "Omit Record",
	"sort records":              "Sort Records",
	"perform script":            "Perform Script",
	"exit script":               "Exit Script",
	"halt script":               "Halt Script",
	"show custom dialog":        "Show Custom Dialog",
	"open url":                  "Open URL",
	"comment":                   "Comment",
	"#":                         "Comment",
}

// catalogOnlyScriptSteps are control-flow or variable steps that File4Base
// stores in the script workspace catalog but halts on in the client-side runner.
var catalogOnlyScriptSteps = map[string]string{
	"if":              "If",
	"else if":         "Else If",
	"else":            "Else",
	"end if":          "End If",
	"loop":            "Loop",
	"exit loop if":    "Exit Loop If",
	"end loop":        "End Loop",
	"set variable":    "Set Variable",
	"import records":  "Import Records",
	"export records":  "Export Records",
	"print":           "Print",
	"insert from url": "Insert from URL",
}

// AnalyzeAndConvert parses a FileMaker DDR / Save-as-XML document (XML or HTML)
// and optional FMPXMLRESULT / .fmp12 table exports, returning a ConvertedSolution
// with its full ConversionReport. It never touches a database.
func AnalyzeAndConvert(req MigrationRequest) (*ConvertedSolution, error) {
	rawXML := strings.TrimSpace(req.DesignXML)
	if rawXML == "" {
		return nil, fmt.Errorf("%w: design_xml is required", ErrInvalidMigrationXML)
	}
	if IsFMP12Binary("", []byte(rawXML)) {
		return nil, ErrBinaryFMP12
	}
	if isHTMLDDR(rawXML) {
		return analyzeAndConvertHTMLDDR(req)
	}

	root, err := parseXMLTree([]byte(rawXML))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMigrationXML, err)
	}

	report := &ConversionReport{
		SourceFormat:         detectSourceFormat(root),
		SolutionName:         strings.TrimSpace(req.SolutionName),
		TriggersQuarantined:  true,
		AccountsDisabled:     true,
		Preserved:            make([]ReportItem, 0),
		Translated:           make([]ReportItem, 0),
		Unsupported:          make([]ReportItem, 0),
		Rejected:             make([]ReportItem, 0),
		ManualActionRequired: make([]ReportItem, 0),
	}
	if report.SourceFormat == "filemaker_save_as_xml_native" {
		// FMDeveloperTool 20+ / Claris 26 write <FMSaveAsXML>, whose grammar
		// keeps fields in FieldsForTables/FieldCatalog and script steps in
		// StepsForScripts. The converter does not read that grammar, and
		// renaming the root to <FMPSaveAsXML> produces tables with no fields,
		// so the document is refused with the formats that do work (#87).
		return nil, fmt.Errorf("%w: <FMSaveAsXML> (FileMaker Pro 20 / Claris 26 \"Save a Copy as XML\") is not supported yet: %s",
			ErrInvalidMigrationXML, supportedDesignFormats)
	}
	if report.SourceFormat == "unknown" {
		return nil, fmt.Errorf("%w: root element <%s> is not a recognized FileMaker design document: %s",
			ErrInvalidMigrationXML, root.Name, supportedDesignFormats)
	}

	if report.SolutionName == "" {
		report.SolutionName = extractSolutionName(root)
	}
	if report.SolutionName == "" {
		report.SolutionName = "Migrated_Solution"
	}

	bundle := &schema.SolutionBundle{
		Format:       schema.SolutionFormat,
		Version:      schema.SolutionVersion,
		SolutionName: report.SolutionName,
		ExportedAt:   time.Now().UTC().Format(time.RFC3339),
		Tables:       make([]schema.BundleTable, 0),
	}

	// 1. Extract Base Tables & Fields
	tableIDByName := make(map[string]string)       // lower(tableName) -> tableID
	canonicalTableName := make(map[string]string)  // lower(tableName) -> sql table name
	colIDByTableCol := make(map[string]string)     // lower(tableName) + "." + lower(colName) -> colID
	colTypeByTableCol := make(map[string]string)   // lower(tableName) + "." + lower(colName) -> FieldType

	// The .fmp12 record reader addresses rows by the numeric table and field
	// ids FileMaker assigns, which the DDR carries as id attributes (#91).
	fmpTableMeta := make(map[int]string)                // fmpTableID -> sqlTableName
	fmpFieldMeta := make(map[int]map[int]htmlFieldMeta) // fmpTableID -> fmpFieldID -> meta

	baseTables := findNodes(root, "BaseTable", "Table", "table")
	seenTables := make(map[string]bool)
	for _, bt := range baseTables {
		// Avoid treating <Table> inside <TableOccurrence> or <Layout> as a base table
		if len(findNodes(bt, "Field", "field", "FieldCatalog")) == 0 && bt.Name != "BaseTable" {
			continue
		}
		rawName := firstAttr(bt, "name", "display_name", "Name")
		if rawName == "" {
			continue
		}
		sqlName, tookFrom := uniqueSQLName(sanitizeIdentifier(rawName), seenTables)
		if tookFrom != "" {
			addReportItem(report, ReportItem{
				Category:   CategoryTranslated,
				Domain:     "table",
				SourceName: rawName,
				TargetName: sqlName,
				Detail:     fmt.Sprintf("Table name normalizes to %q, which an earlier table already took; imported as %q instead of being discarded", tookFrom, sqlName),
			})
		}

		tID := uuid.NewString()
		tableIDByName[strings.ToLower(rawName)] = tID
		tableIDByName[sqlName] = tID
		canonicalTableName[strings.ToLower(rawName)] = sqlName
		canonicalTableName[sqlName] = sqlName

		bTable := schema.BundleTable{
			ID:          tID,
			Name:        sqlName,
			DisplayName: rawName,
			Columns: []schema.BundleColumn{
				{
					ID:           uuid.NewString(),
					Name:         "id",
					DisplayName:  "ID",
					FieldType:    "TEXT",
					IsNullable:   false,
					IsPrimaryKey: true,
				},
			},
		}
		colIDByTableCol[sqlName+".id"] = bTable.Columns[0].ID
		colTypeByTableCol[sqlName+".id"] = "TEXT"

		fmpTableID := 0
		if raw := firstAttr(bt, "id", "ID"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				fmpTableID = parsed
				fmpTableMeta[parsed] = sqlName
				fmpFieldMeta[parsed] = make(map[int]htmlFieldMeta)
			}
		}

		fieldNodes := findDirectOrCatalogChildren(bt, "FieldCatalog", "fields", "Field", "field")
		seenCols := map[string]bool{"id": true}
		for _, fn := range fieldNodes {
			fRawName := firstAttr(fn, "name", "display_name", "Name")
			if fRawName == "" {
				continue
			}
			colName, tookFrom := uniqueSQLName(sanitizeIdentifier(fRawName), seenCols)
			switch tookFrom {
			case "":
			case "id":
				// File4Base manages its own TEXT primary key, so a source
				// field named id keeps its definition under another name
				// rather than disappearing behind the generated one (#89).
				addReportItem(report, ReportItem{
					Category:   CategoryTranslated,
					Domain:     "field",
					SourceName: sqlName + "." + fRawName,
					TargetName: sqlName + "." + colName,
					Detail:     fmt.Sprintf("File4Base manages its own TEXT primary key %q, so this source field was imported as %q", "id", colName),
				})
				addReportItem(report, ReportItem{
					Category:   CategoryManualActionRequired,
					Domain:     "field",
					SourceName: sqlName + "." + fRawName,
					TargetName: sqlName + "." + colName,
					Detail:     fmt.Sprintf("Repoint anything that referenced the source field %q at %q, or at the generated primary key", fRawName, colName),
				})
			default:
				addReportItem(report, ReportItem{
					Category:   CategoryTranslated,
					Domain:     "field",
					SourceName: sqlName + "." + fRawName,
					TargetName: sqlName + "." + colName,
					Detail:     fmt.Sprintf("Field name normalizes to %q, which an earlier field already took; imported as %q instead of being discarded", tookFrom, colName),
				})
			}

			col, items := convertFieldNode(sqlName, fRawName, colName, fn)
			for _, it := range items {
				addReportItem(report, it)
			}
			bTable.Columns = append(bTable.Columns, col)
			colIDByTableCol[sqlName+"."+colName] = col.ID
			colIDByTableCol[strings.ToLower(rawName)+"."+strings.ToLower(fRawName)] = col.ID
			colTypeByTableCol[sqlName+"."+colName] = col.FieldType
			if fmpTableID > 0 {
				if raw := firstAttr(fn, "id", "ID"); raw != "" {
					if fieldID, err := strconv.Atoi(raw); err == nil && fieldID > 0 {
						fmpFieldMeta[fmpTableID][fieldID] = htmlFieldMeta{
							TableID:   fmpTableID,
							FieldID:   fieldID,
							TableSQL:  sqlName,
							RawName:   fRawName,
							ColName:   colName,
							FieldType: col.FieldType,
						}
					}
				}
			}
			report.Counts.Fields++
		}

		bundle.Tables = append(bundle.Tables, bTable)
		addReportItem(report, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "table",
			SourceName: rawName,
			TargetName: sqlName,
			Detail:     fmt.Sprintf("Base table with %d migrated fields", len(bTable.Columns)-1),
		})
	}
	report.Counts.Tables = len(bundle.Tables)
	if report.Counts.Tables > 0 && report.Counts.Fields == 0 {
		// A design whose every table arrives without a single field was not
		// understood, whatever its root element says; reporting it as a
		// successful conversion hid an unread grammar behind empty tables
		// (#87).
		return nil, fmt.Errorf("%w: %d base table(s) were found but not one field, so this document's field catalog was not understood: %s",
			ErrInvalidMigrationXML, report.Counts.Tables, supportedDesignFormats)
	}

	// 2. Extract Table Occurrences
	occIDByName := make(map[string]string) // lower(occName) -> occID
	occTableByName := make(map[string]string)
	occNodes := findNodes(root, "TableOccurrence", "occurrence")
	for idx, on := range occNodes {
		occName := firstAttr(on, "name", "Name")
		if occName == "" {
			continue
		}
		baseName := firstAttr(on, "baseTable", "base_table", "BaseTable")
		if baseName == "" {
			if btRef := findFirstChild(on, "BaseTableReference"); btRef != nil {
				baseName = firstAttr(btRef, "name", "Name")
			}
		}
		tID := tableIDByName[strings.ToLower(baseName)]
		tSQL := canonicalTableName[strings.ToLower(baseName)]
		if tID == "" && len(bundle.Tables) > 0 {
			// Try matching occurrence name to base table name
			tID = tableIDByName[strings.ToLower(occName)]
			tSQL = canonicalTableName[strings.ToLower(occName)]
		}
		if tID == "" {
			addReportItem(report, ReportItem{
				Category:   CategoryRejected,
				Domain:     "occurrence",
				SourceName: occName,
				Detail:     fmt.Sprintf("Base table %q was not found in BaseTableCatalog", baseName),
			})
			continue
		}
		occID := uuid.NewString()
		occIDByName[strings.ToLower(occName)] = occID
		occTableByName[strings.ToLower(occName)] = tSQL
		bundle.TableOccurrences = append(bundle.TableOccurrences, schema.BundleOccurrence{
			ID:          occID,
			BaseTableID: tID,
			Name:        occName,
			XPos:        float64(80 + (idx%4)*260),
			YPos:        float64(80 + (idx/4)*180),
		})
		addReportItem(report, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "occurrence",
			SourceName: occName,
			TargetName: occName,
			Detail:     fmt.Sprintf("Bound to base table %q", tSQL),
		})
	}
	// Ensure every base table has at least one default Table Occurrence
	for idx, tbl := range bundle.Tables {
		if _, ok := occIDByName[strings.ToLower(tbl.Name)]; !ok && occIDByName[strings.ToLower(tbl.DisplayName)] == "" {
			occID := uuid.NewString()
			occIDByName[strings.ToLower(tbl.Name)] = occID
			occIDByName[strings.ToLower(tbl.DisplayName)] = occID
			occTableByName[strings.ToLower(tbl.Name)] = tbl.Name
			occTableByName[strings.ToLower(tbl.DisplayName)] = tbl.Name
			bundle.TableOccurrences = append(bundle.TableOccurrences, schema.BundleOccurrence{
				ID:          occID,
				BaseTableID: tbl.ID,
				Name:        tbl.Name,
				XPos:        float64(80 + (idx%4)*260),
				YPos:        float64(80 + (idx/4)*180),
			})
		}
	}
	report.Counts.Occurrences = len(bundle.TableOccurrences)

	// 3. Extract Relationships
	relNodes := findNodes(root, "Relationship", "relationship")
	for _, rn := range relNodes {
		rel, item := convertRelationshipNode(rn, occIDByName, occTableByName, colIDByTableCol)
		addReportItem(report, item)
		if rel != nil {
			bundle.Relationships = append(bundle.Relationships, *rel)
		}
	}
	report.Counts.Relationships = len(bundle.Relationships)

	// 4. Extract Value Lists
	vlNodes := findNodes(root, "ValueList", "value_list")
	for _, vn := range vlNodes {
		vl, item := convertValueListNode(vn, tableIDByName, canonicalTableName, colIDByTableCol)
		addReportItem(report, item)
		if vl != nil {
			bundle.ValueLists = append(bundle.ValueLists, *vl)
		}
	}
	report.Counts.ValueLists = len(bundle.ValueLists)

	// 5. Extract Layouts (and quarantine any layout triggers)
	layoutNodes := findCatalogNodes(root, "Layout", "layout")
	for _, ln := range layoutNodes {
		lay, items := convertLayoutNode(ln, bundle.TableOccurrences, occIDByName, occTableByName)
		for _, it := range items {
			addReportItem(report, it)
		}
		if lay != nil {
			bundle.Layouts = append(bundle.Layouts, *lay)
		}
	}
	report.Counts.Layouts = len(bundle.Layouts)

	// 6. Extract Scripts & Steps
	scriptNodes := findCatalogNodes(root, "Script", "script")
	defaultContextTable := ""
	if len(bundle.Tables) > 0 {
		defaultContextTable = bundle.Tables[0].Name
	}
	for _, sn := range scriptNodes {
		// Skip <script> references inside <layout>
		if firstAttr(sn, "where") != "" && len(sn.Children) == 0 {
			continue
		}
		sc, items := convertScriptNode(sn, defaultContextTable)
		for _, it := range items {
			addReportItem(report, it)
		}
		if sc != nil {
			bundle.Scripts = append(bundle.Scripts, *sc)
		}
	}
	report.Counts.Scripts = len(bundle.Scripts)

	// 7. Extract Accounts & Privilege Sets (always disabled until password reset)
	accNodes := findNodes(root, "Account", "account")
	for _, an := range accNodes {
		acc, items := convertAccountNode(an)
		for _, it := range items {
			addReportItem(report, it)
		}
		if acc != nil {
			bundle.Users = append(bundle.Users, *acc)
		}
	}
	report.Counts.Accounts = len(bundle.Users)

	// 8. Parse optional FMPXMLRESULT / XML data files and any attached .fmp12
	// binary. Both routes go through the same reader, so an attachment sent
	// with an XML design is read rather than ignored (#91).
	tableRows := make(map[string][]map[string]interface{})
	if err := populateDataRows(req, canonicalTableName, tableIDByName, colTypeByTableCol,
		fmpTableMeta, fmpFieldMeta, tableRows, report); err != nil {
		return nil, err
	}

	return &ConvertedSolution{
		Bundle:    bundle,
		TableRows: tableRows,
		Report:    report,
	}, nil
}

func addReportItem(r *ConversionReport, item ReportItem) {
	switch item.Category {
	case CategoryPreserved:
		r.Preserved = append(r.Preserved, item)
		r.Counts.PreservedCount++
	case CategoryTranslated:
		r.Translated = append(r.Translated, item)
		r.Counts.TranslatedCount++
	case CategoryUnsupported:
		r.Unsupported = append(r.Unsupported, item)
		r.Counts.UnsupportedCount++
	case CategoryRejected:
		r.Rejected = append(r.Rejected, item)
		r.Counts.RejectedCount++
	case CategoryManualActionRequired:
		r.ManualActionRequired = append(r.ManualActionRequired, item)
		r.Counts.ManualActionCount++
	}
}

func convertFieldNode(tableSQL, rawFieldName, colName string, fn *xmlNode) (schema.BundleColumn, []ReportItem) {
	var items []ReportItem
	rawType := strings.ToUpper(strings.TrimSpace(firstAttr(fn, "dataType", "fieldType", "type", "Type")))
	fieldKind := strings.ToUpper(strings.TrimSpace(firstAttr(fn, "fieldType", "kind")))

	col := schema.BundleColumn{
		ID:          uuid.NewString(),
		Name:        colName,
		DisplayName: rawFieldName,
		FieldType:   "TEXT",
		IsNullable:  true,
	}

	// Check repetitions & global storage
	if reps := firstAttr(fn, "maxRepetition", "repetitions"); reps != "" && reps != "1" {
		items = append(items,
			ReportItem{
				Category:   CategoryUnsupported,
				Domain:     "field",
				SourceName: tableSQL + "." + rawFieldName,
				TargetName: tableSQL + "." + colName,
				Detail:     fmt.Sprintf("Repeating field (repetitions=%s) collapsed to single column; normalize repeating values into a related table", reps),
			},
			ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "field",
				SourceName: tableSQL + "." + rawFieldName,
				TargetName: tableSQL + "." + colName,
				Detail:     "Review repeating field data and move extra repetitions to a child table if needed",
			},
		)
	}
	if globalAttr := strings.ToLower(firstAttr(fn, "global", "globalStorage")); globalAttr == "true" || globalAttr == "yes" || globalAttr == "1" {
		items = append(items, ReportItem{
			Category:   CategoryTranslated,
			Domain:     "field",
			SourceName: tableSQL + "." + rawFieldName,
			TargetName: tableSQL + "." + colName,
			Detail:     "FileMaker global field converted to standard table column",
		})
	}

	// Determine calculation or summary formula if present
	calcText := ""
	if calcNode := findFirstChild(fn, "Calculation", "calculation"); calcNode != nil {
		if textNode := findFirstChild(calcNode, "Text"); textNode != nil && strings.TrimSpace(textNode.Text) != "" {
			calcText = strings.TrimSpace(textNode.Text)
		} else {
			calcText = strings.TrimSpace(calcNode.Text)
		}
	}

	if fieldKind == "CALCULATION" || rawType == "CALCULATION" || (calcText != "" && rawType != "SUMMARY" && fieldKind != "SUMMARY") {
		resultType := normalizeCalcResultType(rawType)
		expr, translatedNote, ok := translateAndValidateFormula(calcText)
		if ok {
			payload, _ := json.Marshal(map[string]interface{}{
				"formula":     expr,
				"result_type": resultType,
			})
			s := string(payload)
			col.FieldType = "CALCULATION"
			col.CalculationFormula = &s
			if translatedNote != "" {
				items = append(items, ReportItem{
					Category:   CategoryTranslated,
					Domain:     "field",
					SourceName: tableSQL + "." + rawFieldName,
					TargetName: tableSQL + "." + colName,
					Detail:     translatedNote,
				})
			} else {
				items = append(items, ReportItem{
					Category:   CategoryPreserved,
					Domain:     "field",
					SourceName: tableSQL + "." + rawFieldName,
					TargetName: tableSQL + "." + colName,
					Detail:     fmt.Sprintf("Calculation field (%s): %s", resultType, expr),
				})
			}
			return col, items
		}
		// Formula uses unsupported FileMaker functions: preserve column as stored value and flag for manual review
		col.FieldType = mapBasicFieldType(resultType)
		items = append(items,
			ReportItem{
				Category:   CategoryUnsupported,
				Domain:     "field",
				SourceName: tableSQL + "." + rawFieldName,
				TargetName: tableSQL + "." + colName,
				Detail:     fmt.Sprintf("Unsupported calculation formula %q converted to stored %s column", calcText, col.FieldType),
			},
			ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "field",
				SourceName: tableSQL + "." + rawFieldName,
				TargetName: tableSQL + "." + colName,
				Detail:     fmt.Sprintf("Rewrite calculation formula %q using supported File4Base functions", calcText),
			},
		)
		return col, items
	}

	if fieldKind == "SUMMARY" || rawType == "SUMMARY" {
		col.FieldType = mapBasicFieldType(rawType)
		items = append(items, ReportItem{
			Category:   CategoryTranslated,
			Domain:     "field",
			SourceName: tableSQL + "." + rawFieldName,
			TargetName: tableSQL + "." + colName,
			Detail:     "Summary field imported; verify summary target field in Manage Database",
		})
		return col, items
	}

	switch rawType {
	case "NUMBER", "NUMERIC", "DECIMAL", "INTEGER", "FLOAT":
		col.FieldType = "NUMBER"
	case "DATE":
		col.FieldType = "DATE"
	case "TIME":
		col.FieldType = "TIMESTAMP"
		items = append(items, ReportItem{
			Category:   CategoryTranslated,
			Domain:     "field",
			SourceName: tableSQL + "." + rawFieldName,
			TargetName: tableSQL + "." + colName,
			Detail:     "FileMaker Time field mapped to File4Base TIMESTAMP",
		})
		return col, items
	case "TIMESTAMP", "DATETIME":
		col.FieldType = "TIMESTAMP"
	case "BOOLEAN", "BOOL":
		col.FieldType = "BOOLEAN"
	case "CONTAINER", "BINARY", "BLOB":
		col.FieldType = "CONTAINER"
	default:
		col.FieldType = "TEXT"
	}

	items = append(items, ReportItem{
		Category:   CategoryPreserved,
		Domain:     "field",
		SourceName: tableSQL + "." + rawFieldName,
		TargetName: tableSQL + "." + colName,
		Detail:     fmt.Sprintf("Field mapped as %s", col.FieldType),
	})
	return col, items
}

var (
	reGetCurrentDate      = regexp.MustCompile(`(?i)Get\s*\(\s*CurrentDate\s*\)`)
	reGetCurrentTimestamp = regexp.MustCompile(`(?i)Get\s*\(\s*(CurrentTimestamp|CurrentHostTimestamp)\s*\)`)
)

func translateAndValidateFormula(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	// If raw is already a File4Base JSON calculation specification, unwrap it
	if strings.HasPrefix(trimmed, "{") {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			if expr, ok := parsed["formula"].(string); ok && strings.TrimSpace(expr) != "" {
				trimmed = strings.TrimSpace(expr)
			} else if expr, ok := parsed["expression"].(string); ok && strings.TrimSpace(expr) != "" {
				trimmed = strings.TrimSpace(expr)
			}
		}
	}

	translated := trimmed
	var notes []string
	if reGetCurrentDate.MatchString(translated) {
		translated = reGetCurrentDate.ReplaceAllString(translated, "TODAY()")
		notes = append(notes, "Get(CurrentDate) -> TODAY()")
	}
	if reGetCurrentTimestamp.MatchString(translated) {
		translated = reGetCurrentTimestamp.ReplaceAllString(translated, "NOW()")
		notes = append(notes, "Get(CurrentTimestamp) -> NOW()")
	}
	if strings.Contains(translated, "≠") {
		translated = strings.ReplaceAll(translated, "≠", "<>")
		notes = append(notes, "≠ -> <>")
	}
	if strings.Contains(translated, "≤") {
		translated = strings.ReplaceAll(translated, "≤", "<=")
		notes = append(notes, "≤ -> <=")
	}
	if strings.Contains(translated, "≥") {
		translated = strings.ReplaceAll(translated, "≥", ">=")
		notes = append(notes, "≥ -> >=")
	}

	if _, err := calc.Parse(translated); err != nil {
		return translated, "", false
	}
	if len(notes) > 0 {
		return translated, fmt.Sprintf("Translated formula (%s): %s", strings.Join(notes, ", "), translated), true
	}
	return translated, "", true
}

func normalizeCalcResultType(rawType string) string {
	switch strings.ToUpper(strings.TrimSpace(rawType)) {
	case "NUMBER", "NUMERIC", "DECIMAL", "INTEGER":
		return "Number"
	case "DATE":
		return "Date"
	case "TIME", "TIMESTAMP", "DATETIME":
		return "Timestamp"
	case "BOOLEAN", "BOOL":
		return "Boolean"
	default:
		return "Text"
	}
}

func mapBasicFieldType(resultType string) string {
	switch strings.ToUpper(strings.TrimSpace(resultType)) {
	case "NUMBER":
		return "NUMBER"
	case "DATE":
		return "DATE"
	case "TIME", "TIMESTAMP":
		return "TIMESTAMP"
	case "BOOLEAN":
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}

func convertRelationshipNode(
	rn *xmlNode,
	occIDByName map[string]string,
	occTableByName map[string]string,
	colIDByTableCol map[string]string,
) (*schema.BundleRelationship, ReportItem) {
	relName := firstAttr(rn, "name", "Name", "id")
	leftOcc := firstAttr(rn, "left", "leftTable", "LeftTable")
	leftField := firstAttr(rn, "left_field", "leftField", "LeftField")
	rightOcc := firstAttr(rn, "right", "rightTable", "RightTable")
	rightField := firstAttr(rn, "right_field", "rightField", "RightField")
	op := firstAttr(rn, "operator", "Operator")

	// Support FileMaker DDR JoinPredicateList structure
	if leftOcc == "" || rightOcc == "" {
		if jp := findFirstChild(rn, "JoinPredicate", "Predicate"); jp != nil {
			op = firstAttr(jp, "type", "operator")
			if lNode := findFirstChild(jp, "LeftField"); lNode != nil {
				if fNode := findFirstChild(lNode, "Field"); fNode != nil {
					leftOcc = firstAttr(fNode, "table", "occurrence")
					leftField = firstAttr(fNode, "name")
				}
			}
			if rNode := findFirstChild(jp, "RightField"); rNode != nil {
				if fNode := findFirstChild(rNode, "Field"); fNode != nil {
					rightOcc = firstAttr(fNode, "table", "occurrence")
					rightField = firstAttr(fNode, "name")
				}
			}
		}
	}

	if relName == "" {
		relName = fmt.Sprintf("%s_to_%s", leftOcc, rightOcc)
	}

	leftOccID := occIDByName[strings.ToLower(leftOcc)]
	rightOccID := occIDByName[strings.ToLower(rightOcc)]
	leftTbl := occTableByName[strings.ToLower(leftOcc)]
	rightTbl := occTableByName[strings.ToLower(rightOcc)]

	leftColID := colIDByTableCol[leftTbl+"."+sanitizeIdentifier(leftField)]
	if leftColID == "" {
		leftColID = colIDByTableCol[strings.ToLower(leftTbl)+"."+strings.ToLower(leftField)]
	}
	rightColID := colIDByTableCol[rightTbl+"."+sanitizeIdentifier(rightField)]
	if rightColID == "" {
		rightColID = colIDByTableCol[strings.ToLower(rightTbl)+"."+strings.ToLower(rightField)]
	}

	if leftOccID == "" || rightOccID == "" || leftColID == "" || rightColID == "" {
		return nil, ReportItem{
			Category:   CategoryRejected,
			Domain:     "relationship",
			SourceName: relName,
			Detail:     fmt.Sprintf("Unresolvable join endpoints (%s.%s -> %s.%s)", leftOcc, leftField, rightOcc, rightField),
		}
	}

	normOp := normalizeRelOperator(op)
	if normOp == "x" {
		return nil, ReportItem{
			Category:   CategoryUnsupported,
			Domain:     "relationship",
			SourceName: relName,
			Detail:     "Cartesian product (x) relationship is not supported in File4Base",
		}
	}

	allowCreate := parseBoolAttr(rn, "allow_creation", "allowCreate", "rightAllowCreate")
	cascadeDelete := parseBoolAttr(rn, "cascade_delete", "cascadeDelete", "rightCascadeDelete")

	return &schema.BundleRelationship{
			ID:                uuid.NewString(),
			Name:              relName,
			LeftOccurrenceID:  leftOccID,
			LeftColumnID:      leftColID,
			RightOccurrenceID: rightOccID,
			RightColumnID:     rightColID,
			Operator:          normOp,
			AllowCreation:     allowCreate,
			CascadeDelete:     cascadeDelete,
			SortRelated:       firstAttr(rn, "sort_related", "sortRelated"),
		}, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "relationship",
			SourceName: relName,
			TargetName: relName,
			Detail:     fmt.Sprintf("%s.%s %s %s.%s", leftOcc, leftField, normOp, rightOcc, rightField),
		}
}

func normalizeRelOperator(op string) string {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "", "=", "==", "equal":
		return "="
	case "≠", "!=", "<>", "notequal", "not equal":
		return "≠"
	case "<", "lessthan", "less":
		return "<"
	case "≤", "<=", "lessthanorequal":
		return "≤"
	case ">", "greaterthan", "greater":
		return ">"
	case "≥", ">=", "greaterthanorequal":
		return "≥"
	case "x", "cartesian", "cartesianproduct":
		return "x"
	default:
		return "="
	}
}

func convertValueListNode(
	vn *xmlNode,
	tableIDByName map[string]string,
	canonicalTableName map[string]string,
	colIDByTableCol map[string]string,
) (*schema.BundleValueList, ReportItem) {
	name := firstAttr(vn, "name", "Name")
	if name == "" {
		return nil, ReportItem{
			Category:   CategoryRejected,
			Domain:     "value_list",
			SourceName: "(unnamed)",
			Detail:     "Value list without a name was skipped",
		}
	}

	kind := strings.ToLower(firstAttr(vn, "kind", "source", "type"))
	if kind == "field" || kind == "fromfield" || findFirstChild(vn, "Field") != nil {
		tblName := firstAttr(vn, "table", "base_table")
		fldName := firstAttr(vn, "field")
		if fChild := findFirstChild(vn, "Field"); fChild != nil {
			if tblName == "" {
				tblName = firstAttr(fChild, "table")
			}
			if fldName == "" {
				fldName = firstAttr(fChild, "name")
			}
		}
		tID := tableIDByName[strings.ToLower(tblName)]
		tSQL := canonicalTableName[strings.ToLower(tblName)]
		cID := colIDByTableCol[tSQL+"."+sanitizeIdentifier(fldName)]
		if tID != "" && cID != "" {
			return &schema.BundleValueList{
					ID:             uuid.NewString(),
					Name:           name,
					Kind:           "field",
					SourceTableID:  &tID,
					SourceColumnID: &cID,
				}, ReportItem{
					Category:   CategoryPreserved,
					Domain:     "value_list",
					SourceName: name,
					TargetName: name,
					Detail:     fmt.Sprintf("Dynamic field value list from %s.%s", tblName, fldName),
				}
		}
	}

	var vals []string
	for _, vNode := range findNodes(vn, "Value", "value") {
		if strings.TrimSpace(vNode.Text) != "" {
			vals = append(vals, strings.TrimSpace(vNode.Text))
		}
	}
	if len(vals) == 0 {
		if cv := findFirstChild(vn, "CustomValues"); cv != nil {
			// A FileMaker DDR carries one <Text> child per entry; a design
			// document written by hand may instead put the whole list in the
			// element's own character data, one value per line.
			for _, tNode := range findNodes(cv, "Text", "text") {
				if trimmed := strings.TrimSpace(tNode.Text); trimmed != "" {
					vals = append(vals, trimmed)
				}
			}
			if len(vals) == 0 {
				for _, line := range strings.Split(cv.Text, "\n") {
					if trimmed := strings.TrimSpace(line); trimmed != "" {
						vals = append(vals, trimmed)
					}
				}
			}
		}
	}

	// File4Base rejects a custom value list with no entries, so an empty one is
	// reported here instead of reaching the import and rolling the whole
	// solution back.
	if len(vals) == 0 {
		return nil, ReportItem{
			Category:   CategoryManualActionRequired,
			Domain:     "value_list",
			SourceName: name,
			Detail:     fmt.Sprintf("Custom value list %q carried no values and was skipped; recreate it in Manage Database > Value Lists", name),
		}
	}

	return &schema.BundleValueList{
			ID:           uuid.NewString(),
			Name:         name,
			Kind:         "custom",
			CustomValues: strings.Join(vals, "\n"),
		}, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "value_list",
			SourceName: name,
			TargetName: name,
			Detail:     fmt.Sprintf("Custom value list with %d entries", len(vals)),
		}
}

func convertLayoutNode(
	ln *xmlNode,
	occurrences []schema.BundleOccurrence,
	occIDByName map[string]string,
	occTableByName map[string]string,
) (*schema.BundleLayout, []ReportItem) {
	var items []ReportItem
	name := firstAttr(ln, "name", "Name")
	if name == "" {
		return nil, nil
	}
	occName := firstAttr(ln, "table_occurrence", "table", "tableOccurrence")
	if occName == "" {
		if tRef := findFirstChild(ln, "Table", "TableOccurrenceReference"); tRef != nil {
			occName = firstAttr(tRef, "name", "Name")
		}
	}
	occID := occIDByName[strings.ToLower(occName)]
	if occID == "" && len(occurrences) > 0 {
		occID = occurrences[0].ID
		occName = occurrences[0].Name
	}

	// Check if an explicit File4Base JSON layout definition exists
	def := map[string]interface{}{
		"name":         name,
		"width":        1024,
		"default_view": "form",
		"parts": []map[string]interface{}{
			{"type": "header", "height": 48},
			{"type": "body", "height": 360},
			{"type": "footer", "height": 40},
		},
		"objects": []map[string]interface{}{},
	}

	if defNode := findFirstChild(ln, "definition", "Definition"); defNode != nil && strings.TrimSpace(defNode.Text) != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(defNode.Text), &parsed); err == nil {
			def = parsed
		}
	} else {
		// Convert parts and objects from FileMaker DDR <PartsList> / <Part> / <Object>
		partNodes := findNodes(ln, "Part", "part")
		if len(partNodes) > 0 {
			parts := make([]map[string]interface{}, 0, len(partNodes))
			for _, pn := range partNodes {
				pType := strings.ToLower(firstAttr(pn, "type", "Type"))
				if pType == "" {
					pType = "body"
				}
				height := 120.0
				if hStr := firstAttr(pn, "height", "size"); hStr != "" {
					if h, err := strconv.ParseFloat(hStr, 64); err == nil && h > 0 {
						height = h
					}
				}
				partMap := map[string]interface{}{
					"type":   pType,
					"height": height,
				}
				if bf := firstAttr(pn, "break_field", "subSummaryField"); bf != "" {
					partMap["break_field"] = sanitizeIdentifier(bf)
				}
				parts = append(parts, partMap)
			}
			def["parts"] = parts
		}

		// Convert field objects on layout
		fieldNodes := findNodes(ln, "Field", "field", "FieldObj")
		objs := make([]map[string]interface{}, 0)
		for idx, fn := range fieldNodes {
			fName := firstAttr(fn, "field", "name", "Name")
			if fName == "" {
				continue
			}
			colName := sanitizeIdentifier(fName)
			objs = append(objs, map[string]interface{}{
				"id":         uuid.NewString(),
				"type":       "field",
				"field_name": colName,
				"label":      fName,
				"x":          24,
				"y":          64 + idx*44,
				"width":      240,
				"height":     32,
				"field_binding": map[string]interface{}{
					"field_name":    colName,
					"control_style": "edit_box",
				},
			})
		}
		if len(objs) > 0 {
			def["objects"] = objs
		}
	}

	// Quarantine any layout-level script triggers so imported scripts never auto-execute
	if _, hasTriggers := def["script_triggers"]; hasTriggers {
		delete(def, "script_triggers")
		items = append(items, ReportItem{
			Category:   CategoryManualActionRequired,
			Domain:     "trigger",
			SourceName: name,
			TargetName: name,
			Detail:     "Layout script triggers were quarantined (disabled on import); review and re-enable in Layout Setup",
		})
	}
	for _, trigNode := range findNodes(ln, "ScriptTrigger", "Trigger") {
		trigName := firstAttr(trigNode, "event", "name", "type")
		scriptRef := firstAttr(trigNode, "script", "scriptName")
		items = append(items, ReportItem{
			Category:   CategoryManualActionRequired,
			Domain:     "trigger",
			SourceName: fmt.Sprintf("%s (%s -> %s)", name, trigName, scriptRef),
			TargetName: name,
			Detail:     "Layout script trigger quarantined on import; review target script before enabling in Layout Setup",
		})
	}

	items = append(items, ReportItem{
		Category:   CategoryPreserved,
		Domain:     "layout",
		SourceName: name,
		TargetName: name,
		Detail:     fmt.Sprintf("Layout bound to table occurrence %q", occName),
	})

	return &schema.BundleLayout{
		ID:                uuid.NewString(),
		Name:              name,
		TableOccurrenceID: occID,
		Definition:        def,
	}, items
}

func convertScriptNode(sn *xmlNode, defaultContextTable string) (*schema.BundleScript, []ReportItem) {
	var items []ReportItem
	name := firstAttr(sn, "name", "Name")
	if name == "" {
		return nil, nil
	}
	ctxTable := firstAttr(sn, "context_table", "contextTable")
	if ctxTable == "" {
		ctxTable = defaultContextTable
	}

	stepNodes := findDirectOrCatalogChildren(sn, "StepList", "steps", "Step", "step")
	steps := make([]schema.BundleStep, 0, len(stepNodes))

	for idx, stNode := range stepNodes {
		rawStepName := strings.TrimSpace(firstAttr(stNode, "name", "type", "Name"))
		if rawStepName == "" {
			continue
		}
		enabled := true
		if enAttr := strings.ToLower(firstAttr(stNode, "enable", "enabled", "active")); enAttr == "false" || enAttr == "no" || enAttr == "0" {
			enabled = false
		}

		lowerStep := strings.ToLower(rawStepName)
		canonical, isSupported := supportedScriptSteps[lowerStep]
		if !isSupported {
			canonical = catalogOnlyScriptSteps[lowerStep]
		}

		params, structured, unmapped := decodeStepParams(stNode, canonical)
		if len(unmapped) > 0 {
			items = append(items, ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail: fmt.Sprintf("Step %q carries FileMaker options File4Base does not read (%s); they were kept under filemaker_parameters for review",
					rawStepName, strings.Join(unmapped, ", ")),
			})
		}

		if isSupported {
			steps = append(steps, schema.BundleStep{
				ID:          uuid.NewString(),
				SequenceIdx: idx + 1,
				StepType:    canonical,
				Params:      params,
				IsEnabled:   enabled,
			})
			continue
		}

		if canonical != "" {
			steps = append(steps, schema.BundleStep{
				ID:          uuid.NewString(),
				SequenceIdx: idx + 1,
				StepType:    canonical,
				Params:      params,
				IsEnabled:   enabled,
			})
			items = append(items, ReportItem{
				Category:   CategoryTranslated,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail:     fmt.Sprintf("Step %q imported into Script Workspace catalog (halts client runner until server script runner is enabled)", canonical),
			})
			continue
		}

		// Unsupported FileMaker script step: preserve as a disabled step so the
		// script can still be inspected, carrying its original operands so
		// nothing the source declared is lost (#88).
		commentParams := map[string]interface{}{
			"text":                    fmt.Sprintf("[UNSUPPORTED FM STEP: %s]", rawStepName),
			"original_filemaker_step": rawStepName,
		}
		if len(structured) > 0 {
			commentParams["filemaker_parameters"] = structured
		}
		steps = append(steps, schema.BundleStep{
			ID:          uuid.NewString(),
			SequenceIdx: idx + 1,
			StepType:    "Comment",
			Params:      commentParams,
			IsEnabled:   false,
		})
		items = append(items,
			ReportItem{
				Category:   CategoryUnsupported,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail:     fmt.Sprintf("Unsupported FileMaker script step %q converted to disabled comment step", rawStepName),
			},
			ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail:     fmt.Sprintf("Replace unsupported script step %q in script %q", rawStepName, name),
			},
		)
	}

	items = append(items, ReportItem{
		Category:   CategoryPreserved,
		Domain:     "script",
		SourceName: name,
		TargetName: name,
		Detail:     fmt.Sprintf("Script migrated with %d steps", len(steps)),
	})

	return &schema.BundleScript{
		ID:           uuid.NewString(),
		Name:         name,
		ContextTable: ctxTable,
		IsActive:     true,
		Steps:        steps,
	}, items
}

// stepParamAttrs are the <Step> attributes that describe the step itself
// rather than one of its operands.
var stepParamAttrs = map[string]bool{
	"name": true, "type": true, "Name": true, "enable": true,
	"enabled": true, "active": true, "id": true, "index": true,
}

// decodeStepParams reads one <Step> element into File4Base step parameters.
//
// FileMaker writes a step's operands as child elements, not attributes: the
// target of a Set Field is a <Field>, its value a <Calculation><Text>, the
// destination of a Go to Layout a <Layout>, the called script of a Perform
// Script a <Script> beside the <Calculation> holding the script parameter.
// Copying attributes alone left every one of those empty while the report said
// the script was preserved (#88).
//
// It returns the step parameters, the whole child structure for the audit
// trail, and the names of the child elements it could not map, so a step that
// carries semantics File4Base does not run is flagged rather than quietly
// reduced.
func decodeStepParams(stNode *xmlNode, canonicalStep string) (map[string]interface{}, map[string]interface{}, []string) {
	params := make(map[string]interface{})
	for k, v := range stNode.Attrs {
		if !stepParamAttrs[k] {
			params[k] = v
		}
	}

	structured := decodeNodeChildren(stNode)
	var unmapped []string
	seenUnmapped := make(map[string]bool)
	flagUnmapped := func(elementName string) {
		if seenUnmapped[elementName] {
			return
		}
		seenUnmapped[elementName] = true
		unmapped = append(unmapped, elementName)
	}

	calcText := ""
	for _, child := range stNode.Children {
		switch child.Name {
		case "Calculation", "calculation":
			if text := calculationText(child); text != "" && calcText == "" {
				calcText = text
			}
		case "Field", "field":
			if fieldName := firstAttr(child, "name", "Name"); fieldName != "" {
				params["field"] = sanitizeIdentifier(fieldName)
				params["field_label"] = fieldName
				if tbl := firstAttr(child, "table", "Table"); tbl != "" {
					params["field_table"] = sanitizeIdentifier(tbl)
				}
			}
		case "Layout", "layout":
			if layoutName := firstAttr(child, "name", "Name"); layoutName != "" {
				params["layout_name"] = layoutName
			}
		case "Script", "script":
			if scriptName := firstAttr(child, "name", "Name"); scriptName != "" {
				params["script_name"] = scriptName
			}
		case "Name", "VariableName":
			if text := strings.TrimSpace(child.Text); text != "" {
				params["name"] = text
			}
		case "Value", "value":
			if text := calculationText(child); text != "" {
				params["value"] = text
			}
		case "params", "ParameterValues":
			decoded := false
			if raw := strings.TrimSpace(child.Text); strings.HasPrefix(raw, "{") {
				nested := make(map[string]interface{})
				if json.Unmarshal([]byte(raw), &nested) == nil {
					for k, v := range nested {
						params[k] = v
					}
					decoded = true
				}
			}
			if !decoded {
				for k, v := range decodeNodeChildren(child) {
					params[k] = v
				}
			}
		case "FileReference", "ExternalDataSource":
			if refName := firstAttr(child, "name", "Name"); refName != "" {
				params["file_reference"] = refName
			}
			flagUnmapped(child.Name)
		default:
			flagUnmapped(child.Name)
		}
	}

	if calcText != "" {
		switch canonicalStep {
		case "Set Field":
			params["value"] = calcText
		case "If", "Else If", "Exit Loop If":
			params["condition"] = calcText
		case "Set Variable":
			if _, ok := params["value"]; !ok {
				params["value"] = calcText
			}
		case "Open URL":
			params["url"] = calcText
		case "Perform Script":
			params["script_parameter"] = calcText
		case "Show Custom Dialog":
			params["message"] = calcText
		}
		if _, ok := params["expression"]; !ok {
			params["expression"] = calcText
		}
	}
	if len(structured) > 0 {
		params["filemaker_parameters"] = structured
	}

	return params, structured, unmapped
}

// calculationText reads the formula a <Calculation> element holds, which
// FileMaker writes either as a <Text> child or as the element's own text.
func calculationText(n *xmlNode) string {
	if textNode := findFirstChild(n, "Text", "text"); textNode != nil {
		if text := strings.TrimSpace(textNode.Text); text != "" {
			return text
		}
	}
	if calcNode := findFirstChild(n, "Calculation", "calculation"); calcNode != nil {
		if text := calculationText(calcNode); text != "" {
			return text
		}
	}
	return strings.TrimSpace(n.Text)
}

// decodeNodeChildren turns an element's children into a map, keeping their
// attributes, their text and their own children, so an operand File4Base does
// not read is still carried into the imported step for review (#88).
func decodeNodeChildren(n *xmlNode) map[string]interface{} {
	out := make(map[string]interface{})
	for _, child := range n.Children {
		value := decodeNodeValue(child)
		if value == nil {
			continue
		}
		key := child.Name
		switch existing := out[key].(type) {
		case nil:
			out[key] = value
		case []interface{}:
			out[key] = append(existing, value)
		default:
			out[key] = []interface{}{existing, value}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeNodeValue(n *xmlNode) interface{} {
	text := strings.TrimSpace(n.Text)
	children := decodeNodeChildren(n)
	if len(n.Attrs) == 0 && children == nil {
		if text == "" {
			return nil
		}
		return text
	}
	value := make(map[string]interface{}, len(n.Attrs)+2)
	for k, v := range n.Attrs {
		value[k] = v
	}
	if text != "" {
		value["#text"] = text
	}
	for k, v := range children {
		value[k] = v
	}
	return value
}

func convertAccountNode(an *xmlNode) (*schema.BundleAccount, []ReportItem) {
	username := strings.TrimSpace(firstAttr(an, "name", "username", "AccountName", "Name"))
	if username == "" || strings.EqualFold(username, "[Guest]") || strings.EqualFold(username, "Guest") {
		return nil, []ReportItem{
			{
				Category:   CategoryRejected,
				Domain:     "account",
				SourceName: username,
				Detail:     "Guest / anonymous account is rejected by File4Base's zero-default-account security policy",
			},
		}
	}

	privSet := strings.TrimSpace(firstAttr(an, "privilegeSet", "role", "PrivilegeSet"))
	role := "user"
	switch strings.ToLower(privSet) {
	case "[full access]", "full access", "owner":
		role = "owner"
	case "admin", "administrator", "[data entry only]":
		if strings.Contains(strings.ToLower(privSet), "admin") {
			role = "admin"
		}
	}

	items := []ReportItem{
		{
			Category:   CategoryTranslated,
			Domain:     "account",
			SourceName: username,
			TargetName: username,
			Detail:     fmt.Sprintf("Account mapped from privilege set %q to role %q (imported disabled without password)", privSet, role),
		},
		{
			Category:   CategoryManualActionRequired,
			Domain:     "account",
			SourceName: username,
			TargetName: username,
			Detail:     fmt.Sprintf("Assign a password and enable migrated account %q in Manage Security before sign-in", username),
		},
	}

	return &schema.BundleAccount{
		ID:       uuid.NewString(),
		Username: username,
		Role:     role,
		IsActive: false,
	}, items
}

// ─── Minimal XML Tree Helpers ──────────────────────────────────────────────

type xmlNode struct {
	Name     string
	Attrs    map[string]string
	Text     string
	Children []*xmlNode
}

func parseXMLTree(data []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	var root *xmlNode
	var stack []*xmlNode

	for {
		tok, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{
				Name:  t.Name.Local,
				Attrs: make(map[string]string, len(t.Attr)),
			}
			for _, a := range t.Attr {
				n.Attrs[a.Name.Local] = a.Value
			}
			if root == nil {
				root = n
			} else if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("document contains no XML root element")
	}
	return root, nil
}

// supportedDesignFormats names the design documents the converter reads, which
// is what an unsupported document is refused with (#87).
const supportedDesignFormats = "File4Base reads a Database Design Report exported from FileMaker Pro 12 to 19 " +
	"(<FMPReport> XML or the HTML report), a <FMPSaveAsXML> design document, and its own design XML"

func detectSourceFormat(root *xmlNode) string {
	switch root.Name {
	case "FMSaveAsXML":
		return "filemaker_save_as_xml_native"
	case "FMPReport":
		return "filemaker_ddr_xml"
	case "FMPSaveAsXML":
		return "filemaker_save_as_xml"
	case "design":
		if strings.EqualFold(root.Attrs["format"], "file4base_design") {
			return "file4base_design_xml"
		}
		return "filemaker_ddr_xml"
	case "File", "Database":
		return "filemaker_ddr_xml"
	default:
		return "unknown"
	}
}

func extractSolutionName(root *xmlNode) string {
	if s := firstAttr(root, "solution", "name", "file", "File"); s != "" {
		return strings.TrimSuffix(s, ".fmp12")
	}
	if f := findFirstChild(root, "File", "Database"); f != nil {
		if s := firstAttr(f, "name", "Name"); s != "" {
			return strings.TrimSuffix(s, ".fmp12")
		}
	}
	return ""
}

func firstAttr(n *xmlNode, keys ...string) string {
	for _, k := range keys {
		if v, ok := n.Attrs[k]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseBoolAttr(n *xmlNode, keys ...string) bool {
	val := strings.ToLower(firstAttr(n, keys...))
	return val == "true" || val == "yes" || val == "1"
}

func findFirstChild(n *xmlNode, names ...string) *xmlNode {
	nameSet := make(map[string]bool, len(names))
	for _, nm := range names {
		nameSet[nm] = true
	}
	for _, c := range n.Children {
		if nameSet[c.Name] {
			return c
		}
	}
	return nil
}

func findNodes(root *xmlNode, names ...string) []*xmlNode {
	nameSet := make(map[string]bool, len(names))
	for _, nm := range names {
		nameSet[nm] = true
	}
	var out []*xmlNode
	var walk func(*xmlNode)
	walk = func(n *xmlNode) {
		for _, c := range n.Children {
			if nameSet[c.Name] {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(root)
	return out
}

// findCatalogNodes collects the nodes named names that define an object, which
// is every one of them except those a script step points at: FileMaker writes
// the called script of a Perform Script step as a nested <Script> element and
// the destination of a Go to Layout step as a nested <Layout> element. Reading
// those as definitions invented an empty script and a layout the source never
// declared (#88).
func findCatalogNodes(root *xmlNode, names ...string) []*xmlNode {
	nameSet := make(map[string]bool, len(names))
	for _, nm := range names {
		nameSet[nm] = true
	}
	referenceParents := map[string]bool{
		"Step": true, "step": true, "ScriptTrigger": true, "Trigger": true,
		"ButtonAction": true, "ButtonBar": true,
	}
	var out []*xmlNode
	var walk func(n *xmlNode, insideStep bool)
	walk = func(n *xmlNode, insideStep bool) {
		for _, c := range n.Children {
			if nameSet[c.Name] && !insideStep {
				out = append(out, c)
			}
			walk(c, insideStep || referenceParents[c.Name])
		}
	}
	walk(root, false)
	return out
}

// uniqueSQLName reserves base in taken, or the first free base_2, base_3 … when
// base is already reserved. The second return value is the name that was
// already taken, so the caller can report the collision rather than drop the
// definition that lost it (#89).
func uniqueSQLName(base string, taken map[string]bool) (string, string) {
	if !taken[base] {
		taken[base] = true
		return base, ""
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s_%d", base, n)
		if !taken[candidate] {
			taken[candidate] = true
			return candidate, base
		}
	}
}

func findDirectOrCatalogChildren(parent *xmlNode, catalogName1, catalogName2, itemName1, itemName2 string) []*xmlNode {
	var out []*xmlNode
	for _, c := range parent.Children {
		if c.Name == itemName1 || c.Name == itemName2 {
			out = append(out, c)
		} else if c.Name == catalogName1 || c.Name == catalogName2 {
			for _, gc := range c.Children {
				if gc.Name == itemName1 || gc.Name == itemName2 {
					out = append(out, gc)
				}
			}
		}
	}
	return out
}

func sanitizeIdentifier(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	res := strings.Trim(b.String(), "_")
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	if res == "" {
		return "item"
	}
	if res[0] >= '0' && res[0] <= '9' {
		res = "f_" + res
	}
	return res
}

// coerceSourceValue turns one cell of a record export into the value its field
// holds, through the conversion every File4Base import runs. A number keeps
// every digit the export wrote, because it is stored as the exact decimal it
// was written as rather than passing through a float64, which rounded
// 9007199254740993 to an even neighbour (#90).
//
// A fraction, which the .fmp12 binary reader meets but a FMPXMLRESULT export
// does not write, is still read as a number.
func coerceSourceValue(valStr, fieldType string) (interface{}, error) {
	if fieldType == "NUMBER" {
		if value, err := data.CoerceImportValue(dbal.FieldTypeNumber, valStr, data.DateISO); err == nil {
			return value, nil
		}
		if f, ok := parseFractionOrFloat(valStr); ok {
			return f, nil
		}
		return nil, errors.New("is not a number")
	}
	if fieldType == "DATE" {
		// FMPXMLRESULT writes a date as MM/DD/YYYY; the import's MDY order
		// reads that and an ISO date both.
		return data.CoerceImportValue(dbal.FieldTypeDate, valStr, data.DateMDY)
	}
	if fieldType == "TIMESTAMP" {
		if value, err := data.CoerceImportValue(dbal.FieldTypeTimestamp, valStr, data.DateISO); err == nil {
			return value, nil
		}
		// A FileMaker timestamp is a MM/DD/YYYY date and a time, and a Time
		// field holds a time of day on its own, which File4Base keeps in a
		// TIMESTAMP column.
		trimmed := strings.TrimSpace(valStr)
		for _, layout := range []string{
			"01/02/2006 15:04:05", "01/02/2006 15:04", "1/2/2006 15:04:05", "1/2/2006 15:04",
			"01/02/2006", "1/2/2006",
		} {
			if at, err := time.Parse(layout, trimmed); err == nil {
				return at.Format(time.RFC3339), nil
			}
		}
		for _, layout := range []string{"15:04:05", "15:04"} {
			if at, err := time.Parse(layout, trimmed); err == nil {
				return at.Format(time.RFC3339), nil
			}
		}
		return nil, errors.New("is not a date and time")
	}
	if fieldType == "TEXT" || fieldType == "" {
		// A UUID is lower-cased so that a migrated key matches the ids
		// File4Base generates, and nothing else about the text is touched.
		if reUUIDHex.MatchString(strings.TrimSpace(valStr)) {
			return strings.ToLower(strings.TrimSpace(valStr)), nil
		}
	}
	return data.CoerceImportValue(agnosticFieldType(fieldType), valStr, data.DateISO)
}

// agnosticFieldType reads a bundle column's field type as the engine's own.
func agnosticFieldType(fieldType string) dbal.AgnosticFieldType {
	switch strings.ToUpper(strings.TrimSpace(fieldType)) {
	case "NUMBER":
		return dbal.FieldTypeNumber
	case "DATE":
		return dbal.FieldTypeDate
	case "TIMESTAMP":
		return dbal.FieldTypeTimestamp
	case "BOOLEAN":
		return dbal.FieldTypeBoolean
	case "CONTAINER":
		return dbal.FieldTypeContainer
	default:
		return dbal.FieldTypeText
	}
}

func coerceDataValue(valStr, fieldType string) interface{} {
	switch fieldType {
	case "NUMBER":
		if f, ok := parseFractionOrFloat(valStr); ok {
			return f
		}
		return nil
	case "BOOLEAN":
		low := strings.ToLower(valStr)
		return low == "true" || low == "1" || low == "yes"
	default:
		if reUUIDHex.MatchString(valStr) {
			return strings.ToLower(valStr)
		}
		return valStr
	}
}
