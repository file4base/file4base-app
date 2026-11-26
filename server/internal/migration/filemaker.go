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
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/validation"
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
	Tables            int `json:"tables"`
	Fields            int `json:"fields"`
	Occurrences       int `json:"table_occurrences"`
	Relationships     int `json:"relationships"`
	ValueLists        int `json:"value_lists"`
	Layouts           int `json:"layouts"`
	Scripts           int `json:"scripts"`
	Accounts          int `json:"accounts"`
	RecordsReady      int `json:"records_ready"`
	RecordsDetected   int `json:"records_detected,omitempty"`
	PreservedCount    int `json:"preserved_count"`
	TranslatedCount   int `json:"translated_count"`
	UnsupportedCount  int `json:"unsupported_count"`
	RejectedCount     int `json:"rejected_count"`
	ManualActionCount int `json:"manual_action_count"`

	// CustomFunctions counts the custom functions migrated.
	CustomFunctions int `json:"custom_functions,omitempty"`

	// LayoutObjects counts the layout objects of each FileMaker kind the
	// source has, and how many of them were migrated.
	LayoutObjects map[string]LayoutObjectCount `json:"layout_objects,omitempty"`

	// SourceTextValues counts the values migrated with their original text
	// beside FileMaker's reading of it (#124).
	SourceTextValues int `json:"source_text_values,omitempty"`

	// Container migration counts (#125).
	ContainerCellsDetected      int `json:"container_cells_detected,omitempty"`
	ContainerFilesReconstructed int `json:"container_files_reconstructed,omitempty"`
	ContainerFilesMissing       int `json:"container_files_missing,omitempty"`
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
	// ExternalOccurrences lists the occurrences of another file's tables,
	// which are migrated unbound (#107).
	ExternalOccurrences []ExternalOccurrenceReport `json:"external_occurrences,omitempty"`
}

// MigrationRequest is the payload accepted by the preflight and apply endpoints.
type MigrationRequest struct {
	nativeData      []byte
	NativeDateOrder string            `json:"native_date_order,omitempty"`
	SolutionName    string            `json:"solution_name,omitempty"`
	DesignXML       string            `json:"design_xml"`
	DataFiles       map[string]string `json:"data_files,omitempty"` // TableName -> FMPXMLRESULT XML content
	FMP12Base64     string            `json:"fmp12_base64,omitempty"`
	TargetDatabase  string            `json:"target_database,omitempty"`
	OwnerUser       string            `json:"owner_user,omitempty"`
	OwnerPassword   string            `json:"owner_password,omitempty"`

	// NativeFileName is the filename of the attached .fmp12 copy when known
	// (set by migration-preflight and multipart uploads for diagnostics).
	NativeFileName string `json:"native_file_name,omitempty"`

	// SourceLocale is the locale the source file writes numbers and dates
	// in, such as "fr-FR" (#124). Naming it migrates permissive values, such
	// as "12-A" in a Number field, with their original text instead of
	// refusing them; it also sets the date order when native_date_order is
	// empty.
	SourceLocale string `json:"source_locale,omitempty"`

	// ExternalFiles maps relative external container paths to base64-encoded
	// file contents when migrating externally stored container fields (#125).
	ExternalFiles map[string]string `json:"external_files,omitempty"`

	// externalContainerResolver is an optional local resolver set by CLI tools
	// (such as migration-preflight -external-containers). Never populated from
	// HTTP JSON input.
	externalContainerResolver externalContainerResolver
}

// SetExternalContainerDir configures a safe local-directory resolver for
// externally stored container references (#125). Intended for local CLI tools.
func (r *MigrationRequest) SetExternalContainerDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		r.externalContainerResolver = nil
		return
	}
	r.externalContainerResolver = NewDirectoryExternalContainerResolver(dir)
}

// AnalyzeAndConvertNative audits an explicitly selected local native input
// without a base64 copy or an HTTP request. It never opens a filesystem path.
func AnalyzeAndConvertNative(req MigrationRequest, native []byte) (*ConvertedSolution, error) {
	if len(native) == 0 {
		return nil, fmt.Errorf("native input is empty")
	}
	if strings.TrimSpace(req.FMP12Base64) != "" {
		return nil, fmt.Errorf("choose one native input")
	}
	req.nativeData = native
	return AnalyzeAndConvert(req)
}

// ConvertedSolution holds the ready-to-import File4Base SolutionBundle and
// any parsed table rows extracted from FMPXMLRESULT files.
type ConvertedSolution struct {
	Bundle    *schema.SolutionBundle
	TableRows map[string][]map[string]interface{}
	Report    *ConversionReport
	// Grants are the capabilities the file gave the privilege sets its
	// accounts map to, to grant to those roles once the design is in. They
	// only ever add to what the target database holds.
	Grants []CapabilityGrant
}

// CapabilityGrant is one capability to grant to one account role.
type CapabilityGrant struct {
	Role       string
	Capability string
	// Sets are the privilege sets that carry it in the file.
	Sets []string
}

// WithholdGrant records that a grant was not made, for the reason given:
// the role's report items about the grant give way to one that says so, so
// that the report never says both.
func (r *ConversionReport) WithholdGrant(g CapabilityGrant, reason string) {
	keep := func(items []ReportItem, count *int) []ReportItem {
		out := items[:0]
		for _, it := range items {
			if it.Domain == "privilege" && it.TargetName == g.Role {
				*count--
				continue
			}
			out = append(out, it)
		}
		return out
	}
	r.Translated = keep(r.Translated, &r.Counts.TranslatedCount)
	r.ManualActionRequired = keep(r.ManualActionRequired, &r.Counts.ManualActionCount)
	addReportItem(r, ReportItem{
		Category: CategoryManualActionRequired, Domain: "privilege", SourceName: strings.Join(g.Sets, ", "), TargetName: g.Role,
		Detail: fmt.Sprintf("Privilege set(s) %s carry %s (Access via FileMaker Data API), which was not granted to the role %q because %s; "+
			"grant it in Manage Security if all the accounts of the role may use the Data API", quoteList(g.Sets), g.Capability, g.Role, reason),
	})
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
	// Navigation & modes
	"go to layout":              "Go to Layout",
	"go to record/request/page": "Go to Record/Request/Page",
	"go to record":              "Go to Record/Request/Page",
	"go to field":               "Go to Field",
	"go to next field":          "Go to Next Field",
	"go to object":              "Go to Object",
	"go to related record":      "Go to Related Record",
	"go to portal row":          "Go to Portal Row",
	"enter browse mode":         "Enter Browse Mode",
	"enter find mode":           "Enter Find Mode",
	"enter preview mode":        "Enter Preview Mode",

	// Records & found set
	"new record/request":       "New Record/Request",
	"duplicate record/request": "Duplicate Record/Request",
	"delete record/request":    "Delete Record/Request",
	"delete all records":       "Delete All Records",
	"delete portal row":        "Delete Portal Row",
	"commit records/requests":  "Commit Records/Requests",
	"revert record/request":    "Revert Record/Request",
	"perform find":             "Perform Find",
	"show all records":         "Show All Records",
	"show omitted only":        "Show Omitted Only",
	"omit record":              "Omit Record",
	"omit multiple records":    "Omit Multiple Records",
	"constrain found set":      "Constrain Found Set",
	"extend found set":         "Extend Found Set",
	"modify last find":         "Modify Last Find",
	"sort records":             "Sort Records",
	"unsort records":           "Unsort Records",

	// Fields, editing & containers
	"set field":                "Set Field",
	"set field by name":        "Set Field By Name",
	"set next serial value":    "Set Next Serial Value",
	"relookup field contents":  "Relookup Field Contents",
	"insert calculated result": "Insert Calculated Result",
	"insert current date":      "Insert Current Date",
	"select all":               "Select All",
	"replace field contents":   "Replace Field Contents",
	"copy":                     "Copy",
	"paste":                    "Paste",
	"cut":                      "Cut",
	"clear":                    "Clear",
	"insert picture":           "Insert Picture",
	"insert file":              "Insert File",
	"insert from url":          "Insert from URL",
	"export field contents":    "Export Field Contents",

	// Control flow & scripts
	"if":                  "If",
	"else if":             "Else If",
	"else":                "Else",
	"end if":              "End If",
	"loop":                "Loop",
	"exit loop if":        "Exit Loop If",
	"end loop":            "End Loop",
	"set variable":        "Set Variable",
	"set error capture":   "Set Error Capture",
	"set error logging":   "Set Error Logging",
	"allow user abort":    "Allow User Abort",
	"perform script":      "Perform Script",
	"exit script":         "Exit Script",
	"halt script":         "Halt Script",
	"pause/resume script": "Pause/Resume Script",
	"comment":             "Comment",
	"#":                   "Comment",
	"# (comment)":         "Comment",

	// Windows & UI chrome
	"new window":           "New Window",
	"select window":        "Select Window",
	"close window":         "Close Window",
	"adjust window":        "Adjust Window",
	"move/resize window":   "Move/Resize Window",
	"freeze window":        "Freeze Window",
	"refresh window":       "Refresh Window",
	"refresh portal":       "Refresh Portal",
	"show/hide toolbars":   "Show/Hide Toolbars",
	"show/hide menubar":    "Show/Hide Menubar",
	"show/hide text ruler": "Show/Hide Text Ruler",
	"install menu set":     "Install Menu Set",
	"show custom dialog":   "Show Custom Dialog",
	"beep":                 "Beep",

	// Files, printing, mail & web
	"print setup":           "Print Setup",
	"print":                 "Print",
	"save records as pdf":   "Save Records as PDF",
	"save records as excel": "Save Records as Excel",
	"open url":              "Open URL",
	"send mail":             "Send Mail",
	"open file":             "Open File",
	"close file":            "Close File",
	"exit application":      "Exit Application",
	"re-login":              "Re-Login",

	// Record transfers (#96). An Import Records or Export Records step runs a
	// named transfer definition; until one is named it stops the script.
	"import records": "Import Records",
	"export records": "Export Records",
}

// transferScriptSteps are the steps that run a transfer definition (#96).
// A FileMaker report names a file and a mapping of its own, which File4Base
// cannot turn into a transfer definition on its own, so a converted step
// carries no target and the conversion report says one has to be named.
var transferScriptSteps = map[string]struct{}{
	"Import Records": {},
	"Export Records": {},
}

// needsTransferDefinition reports whether a converted step cannot run until a
// transfer definition is named in it.
func needsTransferDefinition(canonical string) bool {
	_, ok := transferScriptSteps[canonical]
	return ok
}

// catalogOnlyScriptSteps remain inspectable when the client cannot run them.
// All previously catalog-only steps (Sort Records, Omit Record, Show Omitted
// Only, Print, Insert from URL) now execute in the client runner (#133).
var catalogOnlyScriptSteps = map[string]string{}

// AnalyzeAndConvert parses a FileMaker DDR / Save-as-XML document (XML or HTML)
// and optional FMPXMLRESULT / .fmp12 table exports, returning a ConvertedSolution
// with its full ConversionReport. It never touches a database.
func AnalyzeAndConvert(req MigrationRequest) (*ConvertedSolution, error) {
	rawXML := strings.TrimSpace(req.DesignXML)
	if rawXML == "" {
		return nil, fmt.Errorf("%w: design_xml is required", ErrInvalidMigrationXML)
	}
	nativeOrder := strings.ToLower(strings.TrimSpace(req.NativeDateOrder))
	if nativeOrder != "" && nativeOrder != "iso" && nativeOrder != "dmy" && nativeOrder != "mdy" {
		return nil, fmt.Errorf("%w: native_date_order must be iso, dmy or mdy", ErrInvalidMigrationXML)
	}
	req.NativeDateOrder = nativeOrder
	locale, err := parseSourceLocale(req.SourceLocale)
	if err != nil {
		return nil, err
	}
	if locale != nil {
		req.SourceLocale = locale.Tag
		if req.NativeDateOrder == "" {
			req.NativeDateOrder = string(locale.DateOrder)
		}
	}
	if IsFMP12Binary("", []byte(rawXML)) {
		return nil, ErrBinaryFMP12
	}
	if isHTMLDDR(rawXML) {
		return analyzeAndConvertHTMLDDR(req)
	}

	rawXML, stripped := stripXMLControlChars(rawXML)
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
	if stripped > 0 {
		addReportItem(report, ReportItem{
			Category:   CategoryTranslated,
			Domain:     "design",
			SourceName: "design document",
			Detail: fmt.Sprintf("Removed %d control character(s) that XML 1.0 does not allow; "+
				"FileMaker writes them unescaped from text such as layout labels", stripped),
		})
	}
	// FMDeveloperTool 20+ / Claris 26 write <FMSaveAsXML>, which keeps a
	// table's fields in FieldsForTables and a script's steps in
	// StepsForScripts instead of nesting them. The document is adapted into
	// the shape the converter reads, and whatever could not be linked is
	// reported (#87); a document that turns out to carry no schema at all is
	// still refused by the empty-field check below rather than reported as a
	// successful migration of nothing.
	var saveAsXMLItems []ReportItem
	if report.SourceFormat == "filemaker_save_as_xml_native" {
		saveAsXMLItems = adaptSaveAsXML(root)
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
	tableIDByName := make(map[string]string)      // lower(tableName) -> tableID
	canonicalTableName := make(map[string]string) // lower(tableName) -> sql table name
	colIDByTableCol := make(map[string]string)    // lower(tableName) + "." + lower(colName) -> colID
	colTypeByTableCol := make(map[string]string)  // lower(tableName) + "." + lower(colName) -> FieldType
	tableIDBySourceID := make(map[string]string)
	tableSQLBySourceID := make(map[string]string)
	fieldNamesByTable := make(map[string]*fieldNameMap)

	// The .fmp12 record reader addresses rows by the numeric table and field
	// ids FileMaker assigns, which the DDR carries as id attributes (#91).
	fmpTableMeta := make(map[int]string)                // fmpTableID -> sqlTableName
	fmpTableRawNames := make(map[int]string)            // fmpTableID -> raw source table name
	fmpFieldMeta := make(map[int]map[int]htmlFieldMeta) // fmpTableID -> fmpFieldID -> meta

	baseTables := catalogDefinitions(root, []string{"BaseTableCatalog", "tables"}, "BaseTable", "Table", "table")
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
		if sourceID := firstAttr(bt, "id", "ID"); sourceID != "" {
			tableIDBySourceID[sourceID] = tID
			tableSQLBySourceID[sourceID] = sqlName
		}

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
				fmpTableRawNames[parsed] = rawName
				fmpFieldMeta[parsed] = make(map[int]htmlFieldMeta)
			}
		}

		fieldNodes := findDirectOrCatalogChildren(bt, "FieldCatalog", "fields", "Field", "field")
		seenCols := map[string]bool{"id": true}
		// Every name this table's fields can be referred to by — what the
		// source called them and what they ended up as — against which a
		// calculation's references are resolved once the whole table is known
		// (#111). File4Base's own primary key is referable as itself.
		fieldRenames := newFieldNameMap()
		fieldNamesByTable[sqlName] = fieldRenames
		fieldRenames.claim("id", "id", false)
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
			// The field's FileMaker id, by which another file that reads
			// this table names it when it could not read its name (#107).
			if sourceID := strings.TrimSpace(firstAttr(fn, "id", "ID")); schema.ValidSourceFieldID(sourceID) {
				col.SourceFieldID = sourceID
			}
			bTable.Columns = append(bTable.Columns, col)
			// What the source called the field is the authoritative name: a
			// formula written against this design used it. The imported name
			// is added only where no source field already claims it, so a
			// source field literally named like another field's imported
			// column still wins for its own name.
			fieldRenames.claim(fRawName, colName, true)
			fieldRenames.claim(colName, colName, false)
			colIDByTableCol[sqlName+"."+colName] = col.ID
			if sourceID := firstAttr(fn, "id", "ID"); sourceID != "" {
				colIDByTableCol[sqlName+".#"+sourceID] = col.ID
			}
			colTypeByTableCol[sqlName+"."+colName] = col.FieldType
			if fmpTableID > 0 {
				if raw := firstAttr(fn, "id", "ID"); raw != "" {
					if fieldID, err := strconv.Atoi(raw); err == nil && fieldID > 0 {
						srcKind, srcDataType := classifyXMLFieldSourceType(fn, col.FieldType)
						fmpFieldMeta[fmpTableID][fieldID] = htmlFieldMeta{
							TableID:        fmpTableID,
							FieldID:        fieldID,
							TableSQL:       sqlName,
							RawName:        fRawName,
							ColName:        colName,
							FieldType:      col.FieldType,
							SourceKind:     srcKind,
							SourceDataType: srcDataType,
							Repetitions:    max(col.MaxRepetitions, 1),
							Unstored:       srcKind == "CALCULATION" && unstoredCalculation(fn),
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
		advice := supportedDesignFormats
		if report.SourceFormat == "filemaker_save_as_xml_native" {
			// For this format the usual cause is one export option, not the
			// wrong product: without "Include details for analysis tools" the
			// document carries no schema detail to read (#87).
			advice = "Export the file again from FileMaker with \"Include details for analysis tools\" selected " +
				"(the root element then carries Has_DDR_INFO=\"True\"), or produce a Database Design Report instead. " +
				supportedDesignFormats
		}
		return nil, fmt.Errorf("%w: %d base table(s) were found but not one field, so this document's field catalog was not understood: %s",
			ErrInvalidMigrationXML, report.Counts.Tables, advice)
	}

	// 2. Extract Table Occurrences
	dataSources := readExternalDataSources(root)
	externalOccs := make(map[string]string)     // lower(occName) -> occID, of another file's table
	occIDByName := make(map[string]string)      // lower(occName) -> occID
	deletedOccurrences := make(map[string]bool) // lower(occName) of occurrences on deleted tables
	occTableByName := make(map[string]string)
	tablesWithOccurrence := make(map[string]bool) // sqlTableName -> the graph names an occurrence of it
	occNodes := catalogDefinitions(root, []string{"TableOccurrenceCatalog", "occurrences"}, "TableOccurrence", "occurrence")
	// A native DDR writes the relationships graph as
	// RelationshipGraph/TableList/Table: those <Table> elements are the
	// solution's table occurrences, aliases and all (#101).
	for _, graph := range designChildren(root, "RelationshipGraph", "relationshipGraph") {
		for _, list := range graph.Children {
			if list.Name != "TableList" && list.Name != "tableList" {
				continue
			}
			for _, tableNode := range list.Children {
				if tableNode.Name == "Table" || tableNode.Name == "table" {
					occNodes = append(occNodes, tableNode)
				}
			}
		}
	}
	for idx, on := range occNodes {
		occName := firstAttr(on, "name", "Name")
		if occName == "" {
			continue
		}
		if ref := findFirstChild(on, "FileReference"); ref != nil {
			// An occurrence of another file's table: migrated unbound, its
			// table resolved when its file reference is bound (#107). It is
			// never matched to a table of this file, whatever its source
			// table id: that id belongs to the other file.
			refName := strings.TrimSpace(firstAttr(ref, "name", "Name"))
			src, known := dataSources[strings.ToLower(refName)]
			if refName == "" || (known && !src.fileMaker) {
				addReportItem(report, ReportItem{
					Category: CategoryRejected, Domain: "occurrence", SourceName: occName,
					Detail: fmt.Sprintf("External table occurrence reads the data source %q, which is not a FileMaker file; File4Base does not migrate it", refName),
				})
				continue
			}
			table := ""
			if baseName := firstAttr(on, "baseTable", "base_table", "BaseTable"); baseName != "" {
				table = sanitizeIdentifier(baseName)
			}
			occID := uuid.NewString()
			occIDByName[strings.ToLower(occName)] = occID
			externalOccs[strings.ToLower(occName)] = occID
			bundle.TableOccurrences = append(bundle.TableOccurrences, schema.BundleOccurrence{
				ID:       occID,
				Name:     occName,
				XPos:     float64(80 + (idx%4)*260),
				YPos:     float64(80 + (idx/4)*180),
				External: &schema.BundleExternalOccurrence{FileReference: refName, Table: table},
			})
			addExternalFileReference(bundle, refName, src.paths)
			report.ExternalOccurrences = append(report.ExternalOccurrences, ExternalOccurrenceReport{
				Occurrence: occName, DataSource: refName, Paths: append([]string{}, src.paths...), Table: table,
			})
			addReportItem(report, ReportItem{
				Category: CategoryManualActionRequired, Domain: "occurrence", SourceName: occName, TargetName: occName,
				Detail: fmt.Sprintf("Table occurrence of a table of the file %q: migrate and install that file too, then bind the file reference %q to it and verify; reading its records from this solution is not available yet", refName, refName),
			})
			continue
		}
		baseName := firstAttr(on, "baseTable", "base_table", "BaseTable")
		if baseName == "" {
			if btRef := findFirstChild(on, "BaseTableReference"); btRef != nil {
				baseName = firstAttr(btRef, "name", "Name")
			}
		}
		if baseTableDeleted(on, baseName) {
			// FileMaker keeps an occurrence whose base table was deleted,
			// shown as <Table Missing>; it has no records to migrate.
			deletedOccurrences[strings.ToLower(occName)] = true
			addReportItem(report, ReportItem{
				Category: CategoryUnsupported, Domain: "occurrence", SourceName: occName,
				Detail: "Table occurrence is based on a table that was deleted in FileMaker; it was not migrated",
			})
			continue
		}
		tID := tableIDByName[strings.ToLower(baseName)]
		tSQL := canonicalTableName[strings.ToLower(baseName)]
		baseID := firstAttr(on, "baseTableId", "baseTableID", "base_table_id")
		if ref := findFirstChild(on, "BaseTableReference"); ref != nil && baseID == "" {
			baseID = firstAttr(ref, "id", "ID")
		}
		if baseID != "" {
			tID, tSQL = tableIDBySourceID[baseID], tableSQLBySourceID[baseID]
		}
		if baseID == "" && baseName == "" && tID == "" && len(bundle.Tables) > 0 {
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
		tablesWithOccurrence[tSQL] = true
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
		if tablesWithOccurrence[tbl.Name] {
			// The source graph already names an occurrence of this table,
			// under its own alias; a second, invented one would not be in the
			// solution being migrated (#101).
			continue
		}
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
	relNodes := catalogDefinitions(root, []string{"RelationshipCatalog", "relationships"}, "Relationship", "relationship")
	for _, graph := range designChildren(root, "RelationshipGraph", "relationshipGraph") {
		for _, list := range graph.Children {
			if nodeNamed(list, "RelationshipList", "relationshipList") {
				for _, rel := range list.Children {
					if nodeNamed(rel, "Relationship", "relationship") {
						relNodes = append(relNodes, rel)
					}
				}
			}
		}
	}
	for _, rn := range relNodes {
		rel, items := convertRelationshipNode(rn, occIDByName, occTableByName, colIDByTableCol, fieldNamesByTable, externalOccs)
		if rel == nil {
			if left, right := relationshipSides(rn); deletedOccurrences[strings.ToLower(left)] || deletedOccurrences[strings.ToLower(right)] {
				items = []ReportItem{{
					Category: CategoryUnsupported, Domain: "relationship", SourceName: firstAttr(rn, "name", "Name", "id"),
					Detail: fmt.Sprintf("Relationship %s - %s joins a table occurrence whose table was deleted in FileMaker; it was not migrated", left, right),
				}}
			}
		}
		for _, item := range items {
			addReportItem(report, item)
		}
		if rel != nil {
			bundle.Relationships = append(bundle.Relationships, *rel)
		}
	}
	report.Counts.Relationships = len(bundle.Relationships)

	// Only now are every table's fields, occurrence and relationship known,
	// so only now can a calculation be read with the names it was written
	// against, bound to the names the import gave them (#111), and its
	// related fields checked to be reachable.
	externalFields := externalFieldRefs(root, externalOccs)
	binding := newFormulaBinding(bundle, fieldNamesByTable, locale, externalFields)
	// Custom functions first, so every calculation that calls one reads.
	for _, it := range bindCustomFunctions(readXMLCustomFunctions(root), &binding, bundle) {
		addReportItem(report, it)
	}
	report.Counts.CustomFunctions = len(bundle.CustomFunctions)
	for i := range bundle.Tables {
		for _, it := range bindTableCalculations(&bundle.Tables[i], binding, colTypeByTableCol) {
			addReportItem(report, it)
		}
		for _, it := range bindTableAutoEnter(&bundle.Tables[i], binding) {
			addReportItem(report, it)
		}
		for _, it := range bindTableValidation(&bundle.Tables[i], binding) {
			addReportItem(report, it)
		}
	}

	// 4. Extract Value Lists. Only now is it known which calculations are
	// unstored, and a list cannot take its values from a field that cannot be
	// indexed.
	unindexable := map[string]string{}
	for _, t := range bundle.Tables {
		for _, c := range t.Columns {
			if why := schema.UnindexableField(c.FieldType, c.CalculationFormula); why != "" {
				unindexable[c.ID] = why
			}
		}
	}
	vlNodes := catalogDefinitions(root, []string{"ValueListCatalog", "value_lists"}, "ValueList", "value_list")
	for _, vn := range vlNodes {
		vl, items := convertValueListNode(vn, tableIDByName, canonicalTableName, occTableByName, occIDByName, colIDByTableCol, fieldNamesByTable, externalOccs, unindexable)
		for _, item := range items {
			addReportItem(report, item)
		}
		if vl != nil {
			bundle.ValueLists = append(bundle.ValueLists, *vl)
		}
	}
	report.Counts.ValueLists = len(bundle.ValueLists)

	// 5. Extract Layouts (their script triggers are kept, disabled)
	layoutNodes := catalogDefinitions(root, []string{"LayoutCatalog", "layouts"}, "Layout", "layout")
	layoutCtx := newLayoutObjectContext(root, bundle, binding, req.NativeDateOrder)
	for _, ln := range layoutNodes {
		lay, items := convertLayoutNode(ln, bundle.TableOccurrences, occIDByName, occTableByName, deletedOccurrences, layoutCtx)
		for _, it := range items {
			addReportItem(report, it)
		}
		if lay != nil {
			bundle.Layouts = append(bundle.Layouts, *lay)
		}
	}
	report.Counts.Layouts = len(bundle.Layouts)
	report.Counts.LayoutObjects = layoutCtx.LayoutObjectCounts()

	// 6. Extract Scripts & Steps
	scriptNodes := catalogDefinitions(root, []string{"ScriptCatalog", "scripts"}, "Script", "script")
	scripts := newScriptIndex()
	defaultContextTable := ""
	if len(bundle.Tables) > 0 {
		defaultContextTable = bundle.Tables[0].Name
	}
	for _, sn := range scriptNodes {
		// Skip <script> references inside <layout>
		if firstAttr(sn, "where") != "" && len(sn.Children) == 0 {
			continue
		}
		sc, items := convertScriptNode(sn, defaultContextTable, occTableByName)
		for _, it := range items {
			addReportItem(report, it)
		}
		if sc != nil {
			bundle.Scripts = append(bundle.Scripts, *sc)
			scripts.add(firstAttr(sn, "id", "ID"), sc)
		}
	}
	report.Counts.Scripts = len(bundle.Scripts)
	// Only now are the scripts known, which the layouts' triggers run.
	for _, it := range bindLayoutTriggers(bundle, scripts, binding, report.SolutionName) {
		addReportItem(report, it)
	}
	for _, it := range bindLayoutButtons(bundle, scripts, report.SolutionName) {
		addReportItem(report, it)
	}
	for _, it := range fileTriggerItem(readFileTriggers(root), report.SolutionName) {
		addReportItem(report, it)
	}
	collectFileReferences(bundle, report)
	for i := range bundle.FileReferences {
		// A reference a script names is given its data source's paths.
		if src, ok := dataSources[strings.ToLower(bundle.FileReferences[i].Name)]; ok && len(bundle.FileReferences[i].Paths) == 0 {
			bundle.FileReferences[i].Paths = src.paths
		}
	}
	if len(report.ExternalOccurrences) > 0 {
		fields := externalReportFields(bundle, externalFields)
		for i := range report.ExternalOccurrences {
			e := &report.ExternalOccurrences[i]
			e.Fields = fields[strings.ToLower(e.Occurrence)]
			if e.Fields == nil {
				e.Fields = []string{}
			}
			var ids []string
			for _, f := range e.Fields {
				if id, ok := schema.ExternalFieldSourceID(f); ok {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				// FileMaker could not open the other file when it wrote
				// this one, so it named these fields by their id only.
				addReportItem(report, ReportItem{
					Category: CategoryManualActionRequired, Domain: "occurrence", SourceName: e.Occurrence, TargetName: e.Occurrence,
					Detail: fmt.Sprintf("The document names %d field(s) of the file %q only by their FileMaker id (%s), as FileMaker does when that file is not reachable as it saves the design; "+
						"they are kept as #<id> and found by that id when the file reference %q is bound to the solution migrated from that file, whose table must have every one of them",
						len(ids), e.DataSource, strings.Join(ids, ", "), e.DataSource),
				})
			}
			if e.Paths == nil {
				e.Paths = []string{}
			}
		}
	}

	// 7. Extract Accounts & Privilege Sets (always disabled until password reset)
	accNodes := catalogDefinitions(root, []string{"AccountCatalog", "AccountsCatalog", "accounts"}, "Account", "account")
	setsByRole := map[string][]string{}
	for _, an := range accNodes {
		acc, items := convertAccountNode(an)
		for _, it := range items {
			addReportItem(report, it)
		}
		if acc != nil {
			bundle.Users = append(bundle.Users, *acc)
			setsByRole[acc.Role] = appendUnique(setsByRole[acc.Role], accountPrivilegeSet(an))
		}
	}
	report.Counts.Accounts = len(bundle.Users)
	grants, grantItems := fmrestGrants(root, setsByRole)
	for _, it := range grantItems {
		addReportItem(report, it)
	}

	// 8. Parse optional FMPXMLRESULT / XML data files and any attached .fmp12
	// binary. Both routes go through the same reader, so an attachment sent
	// with an XML design is read rather than ignored (#91).
	tableRows := make(map[string][]map[string]interface{})
	designFileName := extractDeclaredFileName(root)
	markSelfImports(bundle, designFileName)
	if err := populateDataRows(req, canonicalTableName, tableIDByName, colTypeByTableCol, bundleRepetitions(bundle),
		fmpTableMeta, fmpTableRawNames, fmpFieldMeta, designFileName, tableRows, report, fieldNamesByTable); err != nil {
		return nil, err
	}

	for _, it := range saveAsXMLItems {
		addReportItem(report, it)
	}

	return &ConvertedSolution{
		Bundle:    bundle,
		TableRows: tableRows,
		Report:    report,
		Grants:    grants,
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

	// A native DDR keeps a field's options in nested <Storage>, <Validation>
	// and <AutoEnter> elements rather than on the <Field> itself, so none of
	// them were read while the field was reported as preserved (#102).
	storageNode := findFirstChild(fn, "Storage", "storage")
	validationNode := findFirstChild(fn, "Validation", "validation")
	autoEnterNode := findFirstChild(fn, "AutoEnter", "autoEnter")

	// Repetitions: a repeating field keeps every one of its repetitions
	// (see the repetition package).
	reps := firstAttr(fn, "maxRepetition", "maxRepetitions", "repetitions")
	if reps == "" && storageNode != nil {
		reps = firstAttr(storageNode, "maxRepetition", "maxRepetitions", "repetitions", "maximumRepetitions")
	}
	if n, ok := parseRepetitions(reps); ok && n > 1 {
		col.MaxRepetitions = n
		items = append(items, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "field",
			SourceName: tableSQL + "." + rawFieldName,
			TargetName: tableSQL + "." + colName,
			Detail:     fmt.Sprintf("Repeating field with %d repetitions", n),
		})
	} else if reps != "" && !ok {
		items = append(items, ReportItem{
			Category:   CategoryManualActionRequired,
			Domain:     "field",
			SourceName: tableSQL + "." + rawFieldName,
			TargetName: tableSQL + "." + colName,
			Detail:     fmt.Sprintf("Repetitions %q could not be read; the field was imported with a single repetition", reps),
		})
	}
	isGlobal := parseBoolAttr(fn, "global", "globalStorage")
	if !isGlobal && storageNode != nil {
		isGlobal = parseBoolAttr(storageNode, "global", "globalStorage")
	}
	if isGlobal {
		items = append(items, ReportItem{
			Category:   CategoryTranslated,
			Domain:     "field",
			SourceName: tableSQL + "." + rawFieldName,
			TargetName: tableSQL + "." + colName,
			Detail:     "FileMaker global field converted to standard table column",
		})
	}

	if rules, ruleItems := convertFieldValidation(tableSQL, rawFieldName, colName, validationNode); rules != nil || len(ruleItems) > 0 {
		items = append(items, ruleItems...)
		if rules != nil {
			encoded, err := json.Marshal(rules)
			if err == nil {
				stored := string(encoded)
				col.ValidationRules = &stored
				if rules.NotEmpty && !rules.AllowOverride {
					col.IsNullable = false
				}
			}
		}
	}
	items = append(items, convertFieldAutoEnter(tableSQL, rawFieldName, colName, autoEnterNode, &col)...)

	// Determine calculation or summary formula if present
	calcText, calcContext := "", ""
	if calcNode := findFirstChild(fn, "Calculation", "calculation"); calcNode != nil {
		calcText = markNamelessFieldReferences(calcNode, calculationText(calcNode))
		// The occurrence the formula is evaluated from, which decides the
		// records its related fields reach.
		calcContext = firstAttr(calcNode, "table")
		if occ := findFirstChild(calcNode, "TableOccurrenceReference"); occ != nil {
			calcContext = firstAttr(occ, "name")
		}
	}
	if fieldKind == "CALCULATED" {
		fieldKind = "CALCULATION"
	}

	if fieldKind == "CALCULATION" || rawType == "CALCULATION" || (calcText != "" && rawType != "SUMMARY" && fieldKind != "SUMMARY") {
		resultType := normalizeCalcResultType(rawType)
		formula := formulaText(calcText)
		// A container result is a calculation like any other: its answer
		// refers to the container it comes from rather than copying it.
		if rawType == "BINARY" || rawType == "CONTAINER" || rawType == "BLOB" {
			resultType = "Container"
		}
		if formula != "" {
			// Kept as the source wrote it: it is read and bound to the
			// imported fields once every table and occurrence is known.
			spec := map[string]interface{}{"formula": formula, "result_type": resultType}
			if storageNode != nil && strings.EqualFold(firstAttr(storageNode, "storeCalculationResults"), "false") {
				spec["stored"] = false
			}
			if calcContext != "" {
				spec["context"] = calcContext
			}
			payload, _ := json.Marshal(spec)
			s := string(payload)
			col.FieldType = "CALCULATION"
			col.CalculationFormula = &s
			return col, items
		}
		// Nothing to compute: a stored column, reported.
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

// nestedOption reads a DDR field option, which FileMaker writes either as an
// attribute of the options element (notEmpty="True") or as a child element
// carrying a value (<NotEmpty value="True"/>). It returns the value and
// whether the option was present at all.
func nestedOption(parent *xmlNode, element string, attrs ...string) (string, bool) {
	if parent == nil {
		return "", false
	}
	if value := firstAttr(parent, attrs...); value != "" {
		return value, true
	}
	child := findFirstChild(parent, element, strings.ToLower(element[:1])+element[1:])
	if child == nil {
		return "", false
	}
	if value := firstAttr(child, "value", "Value"); value != "" {
		return value, true
	}
	if text := strings.TrimSpace(child.Text); text != "" {
		return text, true
	}
	return "True", true // a bare <NotEmpty/> is the option being set
}

func nestedFlag(parent *xmlNode, element string, attrs ...string) bool {
	value, ok := nestedOption(parent, element, attrs...)
	if !ok {
		return false
	}
	switch strings.ToLower(value) {
	case "true", "yes", "1":
		return true
	}
	return false
}

// convertFieldValidation turns a field's nested <Validation> element into
// File4Base validation rules, so a not-empty or unique field is still
// not-empty or unique after the migration, and reports every validation
// semantic File4Base cannot enforce (#102).
func convertFieldValidation(tableSQL, rawFieldName, colName string, vn *xmlNode) (*validation.Rules, []ReportItem) {
	if vn == nil {
		return nil, nil
	}
	source := tableSQL + "." + rawFieldName
	target := tableSQL + "." + colName
	var items []ReportItem
	rules := &validation.Rules{}
	kept := []string{}

	if nestedFlag(vn, "NotEmpty", "notEmpty", "not_empty") {
		rules.NotEmpty = true
		kept = append(kept, "not empty")
	}
	if nestedFlag(vn, "Unique", "unique") {
		rules.Unique = true
		kept = append(kept, "unique")
	}
	if nestedFlag(vn, "Existing", "existing", "existingValue") {
		rules.ExistingValue = true
		kept = append(kept, "member of existing values")
	}
	if value, ok := nestedOption(vn, "MaxLength", "maxLength", "max_length"); ok {
		if length, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && length > 0 {
			rules.MaxLengthEnabled = true
			rules.MaxLength = &length
			kept = append(kept, fmt.Sprintf("at most %d characters", length))
		}
	}
	if rangeNode := findFirstChild(vn, "Range", "range"); rangeNode != nil {
		from := firstAttr(rangeNode, "from", "From", "min", "start")
		to := firstAttr(rangeNode, "to", "To", "max", "end")
		if from != "" || to != "" {
			rules.RangeEnabled = true
			rules.RangeMin = from
			rules.RangeMax = to
			kept = append(kept, fmt.Sprintf("between %q and %q", from, to))
		}
	}
	if value, ok := nestedOption(vn, "StrictDataType", "strictDataType", "strictType"); ok {
		if strict := strictTypeFromDDR(value); strict != "" {
			rules.StrictTypeEnabled = true
			rules.StrictType = strict
			kept = append(kept, "strictly "+strict)
		} else {
			items = append(items, ReportItem{
				Category:   CategoryUnsupported,
				Domain:     "field",
				SourceName: source,
				TargetName: target,
				Detail:     fmt.Sprintf("Validation data type %q has no File4Base equivalent and was not migrated", value),
			})
		}
	}
	if message, ok := nestedOption(vn, "Message", "message"); ok && message != "True" && message != "False" {
		rules.CustomMessageEnabled = true
		rules.CustomMessage = message
	}
	// FileMaker validates during data entry unless the field is set to
	// validate always, which is the same distinction File4Base draws.
	if nestedFlag(vn, "AlwaysValidate", "alwaysValidate", "alwaysValidateCalculation") ||
		strings.EqualFold(firstAttr(vn, "type"), "Always") {
		rules.Timing = "always"
	} else {
		rules.Timing = "entry"
	}
	// "Allow user to override during data entry".
	rules.AllowOverride = strings.EqualFold(firstAttr(vn, "allowOverride"), "True")

	// Validated by calculation: kept as the source wrote it, and bound to
	// the imported names with the other formulas (bindTableValidation).
	formulaFlag := nestedFlag(vn, "ValidateCalculation", "validateCalculation")
	if formula, context, ok := validationFormula(vn); ok {
		rules.CalculationEnabled = true
		rules.Calculation = formula
		rules.CalculationContext = context
	} else if formulaFlag {
		items = append(items,
			ReportItem{
				Category:   CategoryUnsupported,
				Domain:     "field",
				SourceName: source,
				TargetName: target,
				Detail:     "Validation by a validation formula was not migrated: the source does not include the formula",
			},
			ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "field",
				SourceName: source,
				TargetName: target,
				Detail:     fmt.Sprintf("Re-create the validation by calculation on %s in Manage Database", target),
			})
	}

	if nestedFlag(vn, "ValueList", "valuelist") {
		items = append(items,
			ReportItem{
				Category:   CategoryUnsupported,
				Domain:     "field",
				SourceName: source,
				TargetName: target,
				Detail:     "Validation by membership of a value list is not enforced by File4Base and was not migrated",
			},
			ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "field",
				SourceName: source,
				TargetName: target,
				Detail:     fmt.Sprintf("Re-create the validation by membership of a value list on %s, or enforce it in a script", target),
			})
	}

	if len(kept) == 0 {
		if rules.CalculationEnabled {
			// Reported once the formula is bound.
			return rules, items
		}
		// A custom message with nothing to enforce is no rule at all.
		return nil, items
	}
	items = append(items, ReportItem{
		Category:   CategoryPreserved,
		Domain:     "field",
		SourceName: source,
		TargetName: target,
		Detail:     fmt.Sprintf("Validation migrated (%s)", strings.Join(kept, ", ")),
	})
	return rules, items
}

// validationFormula is the formula of a field's validation by calculation
// and the occurrence it is evaluated from, when the validation has one that
// is switched on: <Calculated enable="True"><Calculation> in a Save a Copy as
// XML, or a <Calculation> next to validateCalculation="True".
func validationFormula(vn *xmlNode) (formula, context string, ok bool) {
	calcNode := (*xmlNode)(nil)
	if calculated := findFirstChild(vn, "Calculated", "calculated"); calculated != nil {
		if !strings.EqualFold(firstAttr(calculated, "enable", "enabled"), "True") {
			return "", "", false
		}
		calcNode = findFirstChild(calculated, "Calculation", "calculation")
	} else if nestedFlag(vn, "ValidateCalculation", "validateCalculation") {
		calcNode = findFirstChild(vn, "Calculation", "calculation")
	}
	if calcNode == nil {
		return "", "", false
	}
	formula = formulaText(markNamelessFieldReferences(calcNode, calculationText(calcNode)))
	if formula == "" {
		return "", "", false
	}
	context = firstAttr(calcNode, "table")
	if occ := findFirstChild(calcNode, "TableOccurrenceReference"); occ != nil {
		context = firstAttr(occ, "name")
	}
	return formula, context, true
}

func strictTypeFromDDR(raw string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", "")) {
	case "numeric", "numericonly", "number":
		return "Numeric Only"
	case "date":
		return "Date"
	case "4digityear", "fourdigityear", "year":
		return "4-Digit Year"
	case "time", "timeofday":
		return "Time of Day"
	case "text", "textonly":
		return "Text Only"
	}
	return ""
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
	case "BINARY", "CONTAINER", "BLOB":
		return "CONTAINER"
	default:
		return "TEXT"
	}
}

func convertRelationshipNode(
	rn *xmlNode,
	occIDByName map[string]string,
	occTableByName map[string]string,
	colIDByTableCol map[string]string,
	fieldNamesByTable map[string]*fieldNameMap,
	externalOccs map[string]string,
) (*schema.BundleRelationship, []ReportItem) {
	relName := firstAttr(rn, "name", "Name", "id")
	leftOcc := firstAttr(rn, "left", "leftTable", "LeftTable")
	leftField := firstAttr(rn, "left_field", "leftField", "LeftField")
	rightOcc := firstAttr(rn, "right", "rightTable", "RightTable")
	rightField := firstAttr(rn, "right_field", "rightField", "RightField")
	op := firstAttr(rn, "operator", "Operator")

	// A native DDR names the two sides in <LeftTable> / <RightTable> child
	// elements, and carries its join in a <JoinPredicateList> whose
	// <JoinPredicate> elements are not direct children of the relationship
	// (#101).
	leftTableNode := findFirstChild(rn, "LeftTable", "leftTable")
	rightTableNode := findFirstChild(rn, "RightTable", "rightTable")
	if leftOcc == "" && leftTableNode != nil {
		leftOcc = firstAttr(leftTableNode, "name", "Name")
	}
	if rightOcc == "" && rightTableNode != nil {
		rightOcc = firstAttr(rightTableNode, "name", "Name")
	}

	// Every join predicate is a criterion of the relationship, and a record is
	// related when it meets all of them. A relationship written with
	// attributes only has the one they name. All criteria join the same two
	// occurrences, the relationship's own.
	type sourceCriterion struct {
		op, leftField, rightField string
		leftEnd, rightEnd         *xmlNode // <LeftField> / <RightField>, which may carry the field's source id
	}
	var criteria []sourceCriterion
	for i, jp := range findNodes(rn, "JoinPredicate", "Predicate") {
		lOcc, lField := joinPredicateEndpoint(jp, "LeftField", "leftField")
		rOcc, rField := joinPredicateEndpoint(jp, "RightField", "rightField")
		c := sourceCriterion{
			op: firstAttr(jp, "type", "operator"), leftField: lField, rightField: rField,
			leftEnd: findFirstChild(jp, "LeftField", "leftField"), rightEnd: findFirstChild(jp, "RightField", "rightField"),
		}
		if i == 0 {
			// The relationship's own attributes, where it has them, describe
			// its first criterion.
			if op != "" {
				c.op = op
			}
			if leftField != "" {
				c.leftField = leftField
			}
			if rightField != "" {
				c.rightField = rightField
			}
			if leftOcc == "" {
				leftOcc = lOcc
			}
			if rightOcc == "" {
				rightOcc = rOcc
			}
		}
		if lOcc != "" && rOcc != "" && !strings.EqualFold(leftOcc, rightOcc) &&
			strings.EqualFold(lOcc, rightOcc) && strings.EqualFold(rOcc, leftOcc) {
			// A criterion written from the other side: Right::b > Left::a is
			// Left::a < Right::b.
			c.leftField, c.rightField = c.rightField, c.leftField
			c.leftEnd, c.rightEnd = c.rightEnd, c.leftEnd
			if normalized, ok := normalizeRelOperator(c.op); ok {
				c.op = mirrorRelOperator(normalized)
			}
		}
		criteria = append(criteria, c)
	}
	if len(criteria) == 0 {
		criteria = []sourceCriterion{{op: op, leftField: leftField, rightField: rightField}}
	}

	if relName == "" {
		relName = fmt.Sprintf("%s_to_%s", leftOcc, rightOcc)
	}

	leftOccID := occIDByName[strings.ToLower(leftOcc)]
	rightOccID := occIDByName[strings.ToLower(rightOcc)]
	leftTbl := occTableByName[strings.ToLower(leftOcc)]
	rightTbl := occTableByName[strings.ToLower(rightOcc)]

	// resolveColumn finds the imported column of a criterion's field: by the
	// field's source id when the DDR gives one, otherwise by its name.
	resolveColumn := func(table, field string, end *xmlNode) string {
		name := sanitizeIdentifier(field)
		if names := fieldNamesByTable[table]; names != nil {
			if resolved, ok := names.resolve(field); ok {
				name = resolved
			}
		}
		columnID := colIDByTableCol[table+"."+name]
		if columnID == "" {
			columnID = colIDByTableCol[strings.ToLower(table)+"."+strings.ToLower(field)]
		}
		if end != nil {
			if fieldNode := findFirstChild(end, "Field", "field"); fieldNode != nil {
				if sourceID := firstAttr(fieldNode, "id", "ID"); sourceID != "" {
					columnID = colIDByTableCol[table+".#"+sourceID]
				}
			}
		}
		return columnID
	}

	// resolveSide is one side of a criterion, as the LeftColumn* of a
	// predicate: the imported column, or on an occurrence of another file's
	// table, the field's name as that file's migration gives it (#107).
	resolveSide := func(occurrence, table, field string, end *xmlNode) schema.BundlePredicate {
		if _, external := externalOccs[strings.ToLower(occurrence)]; external {
			if field == "" && end != nil {
				// A field of a file FileMaker could not open when it wrote
				// the document: named only by its id, resolved by that id
				// when the file reference is bound.
				if fieldNode := findFirstChild(end, "Field", "field", "FieldReference"); fieldNode != nil {
					field = referencedFieldName(fieldNode)
				}
			}
			if field == "" {
				return schema.BundlePredicate{}
			}
			return schema.BundlePredicate{LeftColumnName: externalFieldName(field)}
		}
		return schema.BundlePredicate{LeftColumnID: resolveColumn(table, field, end)}
	}

	var predicates []schema.BundlePredicate
	var resolved, unresolved, unsupported []string
	for _, c := range criteria {
		normOp, known := normalizeRelOperator(c.op)
		if !known {
			unsupported = append(unsupported, fmt.Sprintf("%s.%s %q %s.%s", leftOcc, c.leftField, c.op, rightOcc, c.rightField))
			continue
		}
		var left, right schema.BundlePredicate
		if leftOccID != "" && rightOccID != "" {
			left = resolveSide(leftOcc, leftTbl, c.leftField, c.leftEnd)
			right = resolveSide(rightOcc, rightTbl, c.rightField, c.rightEnd)
		}
		text := fmt.Sprintf("%s.%s %s %s.%s", leftOcc, firstNonEmpty(c.leftField, left.LeftColumnName), normOp,
			rightOcc, firstNonEmpty(c.rightField, right.LeftColumnName))
		if (left.LeftColumnID == "" && left.LeftColumnName == "") || (right.LeftColumnID == "" && right.LeftColumnName == "") {
			unresolved = append(unresolved, text)
			continue
		}
		resolved = append(resolved, text)
		predicates = append(predicates, schema.BundlePredicate{
			LeftColumnID: left.LeftColumnID, LeftColumnName: left.LeftColumnName, Operator: normOp,
			RightColumnID: right.LeftColumnID, RightColumnName: right.LeftColumnName,
		})
	}

	if len(unresolved) > 0 || len(unsupported) > 0 {
		// A relationship missing one of its criteria would relate — and
		// cascade deletes to — records the source never related, so it is
		// refused whole rather than migrated wider than it was. So is one
		// with an operator that is not known: reading it as = would relate
		// other records than the source did.
		var reasons []string
		if len(unsupported) > 0 {
			reasons = append(reasons, fmt.Sprintf("Unsupported join operator in %d of its %d criteria (%s)",
				len(unsupported), len(criteria), strings.Join(unsupported, ", ")))
		}
		if len(unresolved) > 0 {
			detail := fmt.Sprintf("Unresolvable join endpoints (%s)", strings.Join(unresolved, ", "))
			if len(criteria) > 1 {
				detail = fmt.Sprintf("Unresolvable join endpoints in %d of its %d criteria (%s)",
					len(unresolved), len(criteria), strings.Join(unresolved, ", "))
			}
			reasons = append(reasons, detail)
		}
		detail := strings.Join(reasons, "; ")
		return nil, []ReportItem{{
			Category:   CategoryRejected,
			Domain:     "relationship",
			SourceName: relName,
			Detail:     detail,
		}}
	}

	// A DDR and Save a Copy as XML record the options per side, as FileMaker
	// sets them: <LeftTable> and <RightTable> each carry those of the table
	// they describe, acting on that table's records (#101). The right side's
	// are the relationship's own attributes in a File4Base design document.
	rel := &schema.BundleRelationship{
		ID:                uuid.NewString(),
		Name:              relName,
		LeftOccurrenceID:  leftOccID,
		RightOccurrenceID: rightOccID,
		AllowCreation:     parseBoolAttr(rn, "allow_creation", "allowCreate", "rightAllowCreate"),
		CascadeDelete:     parseBoolAttr(rn, "cascade_delete", "cascadeDelete", "rightCascadeDelete"),
		SortRelated:       firstAttr(rn, "sort_related", "sortRelated"),
		LeftAllowCreation: parseBoolAttr(rn, "left_allow_creation", "leftAllowCreate"),
		LeftCascadeDelete: parseBoolAttr(rn, "left_cascade_delete", "leftCascadeDelete"),
		LeftSortRelated:   firstAttr(rn, "left_sort_related", "leftSortRelated"),
	}
	var sortNotes []ReportItem
	for _, side := range []struct {
		node               *xmlNode
		occurrence, table  string
		allowCreate, purge *bool
		sort               *string
	}{
		{leftTableNode, leftOcc, leftTbl, &rel.LeftAllowCreation, &rel.LeftCascadeDelete, &rel.LeftSortRelated},
		{rightTableNode, rightOcc, rightTbl, &rel.AllowCreation, &rel.CascadeDelete, &rel.SortRelated},
	} {
		if side.node == nil {
			continue
		}
		*side.allowCreate = *side.allowCreate || parseBoolAttr(side.node, "cascadeCreate", "allowCreate", "allow_creation")
		*side.purge = *side.purge || parseBoolAttr(side.node, "cascadeDelete", "cascade_delete")
		if *side.sort != "" {
			continue
		}
		_, external := externalOccs[strings.ToLower(side.occurrence)]
		table := side.table
		spec, notes := relationshipSideSort(side.node, side.occurrence, func(field string) string {
			if external {
				return externalFieldName(field)
			}
			name := sanitizeIdentifier(field)
			if names := fieldNamesByTable[table]; names != nil {
				if resolved, ok := names.resolve(field); ok {
					name = resolved
				}
			}
			if colIDByTableCol[table+"."+name] == "" {
				return ""
			}
			return name
		})
		*side.sort = firstNonEmpty(spec, firstAttr(side.node, "sortList", "sort_related", "sortRelated"))
		if len(notes) > 0 {
			sortNotes = append(sortNotes, ReportItem{
				Category:   CategoryTranslated,
				Domain:     "relationship",
				SourceName: relName,
				TargetName: relName,
				Detail:     fmt.Sprintf("Sort records of %s: %s", side.occurrence, strings.Join(notes, "; ")),
			})
		}
	}

	detail := strings.Join(resolved, " AND ") + relationshipOptionsDetail(leftOcc, rightOcc, rel)
	for _, side := range []string{leftOcc, rightOcc} {
		if _, external := externalOccs[strings.ToLower(side)]; external {
			detail += fmt.Sprintf("; %s is an occurrence of another file's table, whose fields are verified when its file reference is bound", side)
			break
		}
	}
	items := append([]ReportItem{{
		Category:   CategoryPreserved,
		Domain:     "relationship",
		SourceName: relName,
		TargetName: relName,
		Detail:     detail,
	}}, sortNotes...)

	rel.WithPredicates(predicates)
	return rel, items
}

// relationshipSideSort reads the "Sort records" of one side of a
// relationship — the order that side's records are read in from a record of
// the other side — as a sort order of the side's fields ("grouping,-order"):
// a DDR's <SortList>, or Save a Copy as XML's <SortSpecification>, under the
// <LeftTable> or <RightTable> of that side. column gives the field's name as
// migrated, or "" when it is not a field of the side's table.
//
// What cannot be kept is described in notes rather than dropped silently: a
// field of another occurrence, which a side's order cannot name, and a
// custom order by value list, which sorts ascending instead.
func relationshipSideSort(side *xmlNode, occurrence string, column func(field string) string) (string, []string) {
	holder := findFirstChild(side, "SortSpecification", "SortList", "sortList")
	if holder == nil || (hasAttr(holder, "value", "Value") && !parseBoolAttr(holder, "value", "Value")) {
		return "", nil
	}
	var levels, notes []string
	for _, level := range findNodes(holder, "Sort", "sort") {
		fields := findNodes(level, "Field", "FieldReference", "field")
		if len(fields) == 0 {
			continue
		}
		fieldNode := fields[0]
		field := referencedFieldName(fieldNode)
		owner := firstAttr(fieldNode, "table", "Table")
		if ref := findFirstChild(fieldNode, "TableOccurrenceReference"); owner == "" && ref != nil {
			owner = firstAttr(ref, "name", "Name")
		}
		if owner != "" && !strings.EqualFold(owner, occurrence) {
			notes = append(notes, fmt.Sprintf("%s::%s is a field of another occurrence, which is not kept", owner, field))
			continue
		}
		name := column(field)
		if name == "" {
			notes = append(notes, fmt.Sprintf("%s is not a migrated field of %s, which is not kept", field, occurrence))
			continue
		}
		switch order := strings.ToLower(firstAttr(level, "type", "Type", "order")); {
		case strings.HasPrefix(order, "desc"):
			name = "-" + name
		case order == "custom":
			notes = append(notes, fmt.Sprintf("%s is sorted by a value list in FileMaker, and ascending here", field))
		}
		levels = append(levels, name)
	}
	return strings.Join(levels, ","), notes
}

// relationshipOptionsDetail describes, for a report, the options set on each
// side of rel, which act on that side's records.
func relationshipOptionsDetail(leftOcc, rightOcc string, rel *schema.BundleRelationship) string {
	var parts []string
	for _, side := range []struct {
		name, other   string
		create, purge bool
		sort          string
	}{
		{leftOcc, rightOcc, rel.LeftAllowCreation, rel.LeftCascadeDelete, rel.LeftSortRelated},
		{rightOcc, leftOcc, rel.AllowCreation, rel.CascadeDelete, rel.SortRelated},
	} {
		var options []string
		if side.create {
			options = append(options, "records created from "+side.other)
		}
		if side.purge {
			options = append(options, "deleted with their "+side.other+" record")
		}
		if side.sort != "" {
			options = append(options, "sorted by "+side.sort)
		}
		if len(options) > 0 {
			parts = append(parts, side.name+": "+strings.Join(options, ", "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "; " + strings.Join(parts, "; ")
}

// joinPredicateEndpoint reads one side of a native DDR join predicate, whose
// field is a <Field> element inside <LeftField> / <RightField> naming both the
// occurrence and the field (#101).
func joinPredicateEndpoint(jp *xmlNode, names ...string) (string, string) {
	side := findFirstChild(jp, names...)
	if side == nil {
		return "", ""
	}
	if fieldNode := findFirstChild(side, "Field", "field"); fieldNode != nil {
		return firstAttr(fieldNode, "table", "occurrence", "Table"),
			firstAttr(fieldNode, "name", "Name")
	}
	return firstAttr(side, "table", "occurrence"), firstAttr(side, "name", "Name")
}

// normalizeRelOperator reads a join operator as FileMaker and its exports
// spell it: a sign, or a name such as Equal, NotEqual, LessThan,
// LessThanOrEqualTo or CartesianProduct, in any case and with or without
// spaces, underscores and hyphens. No operator at all is an equality, as a
// relationship described by attributes only has. Anything else is not
// guessed: ok is false.
func normalizeRelOperator(op string) (normalized string, ok bool) {
	key := strings.Map(func(r rune) rune {
		if r == ' ' || r == '_' || r == '-' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(op)))
	switch key {
	case "", "=", "==", "eq", "equal", "equals", "equalto":
		return "=", true
	case "≠", "!=", "<>", "ne", "neq", "notequal", "notequals", "notequalto":
		return "≠", true
	case "<", "lt", "less", "lessthan":
		return "<", true
	case "≤", "<=", "le", "lte", "lessorequal", "lessthanorequal", "lessthanorequalto", "lessthanequal", "lessthanequalto":
		return "≤", true
	case ">", "gt", "greater", "greaterthan":
		return ">", true
	case "≥", ">=", "ge", "gte", "greaterorequal", "greaterthanorequal", "greaterthanorequalto", "greaterthanequal", "greaterthanequalto":
		return "≥", true
	case "x", "×", "cartesian", "cartesianproduct":
		return "×", true
	}
	return "", false
}

// mirrorRelOperator is a comparison read from the other side: a < b is b > a.
func mirrorRelOperator(op string) string {
	switch op {
	case "<":
		return ">"
	case "≤":
		return "≥"
	case ">":
		return "<"
	case "≥":
		return "≤"
	}
	return op
}

func convertValueListNode(
	vn *xmlNode,
	tableIDByName map[string]string,
	canonicalTableName map[string]string,
	occTableByName map[string]string,
	occIDByName map[string]string,
	colIDByTableCol map[string]string,
	fieldNamesByTable map[string]*fieldNameMap,
	externalOccs map[string]string,
	unindexable map[string]string,
) (*schema.BundleValueList, []ReportItem) {
	name := firstAttr(vn, "name", "Name")
	if name == "" {
		return nil, []ReportItem{{
			Category:   CategoryRejected,
			Domain:     "value_list",
			SourceName: "(unnamed)",
			Detail:     "Value list without a name was skipped",
		}}
	}

	kind := strings.ToLower(firstAttr(vn, "kind", "source", "type"))
	if src := findFirstChild(vn, "Source"); src != nil && kind == "" {
		kind = strings.ToLower(firstAttr(src, "value"))
	}
	if kind == "external" {
		return nil, []ReportItem{{
			Category:   CategoryManualActionRequired,
			Domain:     "value_list",
			SourceName: name,
			Detail:     fmt.Sprintf("Value list %q uses a value list of another FileMaker file and was skipped; recreate it in Manage Database > Value Lists", name),
		}}
	}
	if kind == "field" || kind == "fromfield" || findFirstChild(vn, "Field") != nil {
		return convertFieldValueList(vn, name, tableIDByName, canonicalTableName, occTableByName, occIDByName, colIDByTableCol, fieldNamesByTable, externalOccs, unindexable)
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
			// FileMaker writes the whole list in one <Text>, one value per
			// line, with carriage returns.
			for _, tNode := range findNodes(cv, "Text", "text") {
				vals = append(vals, splitValueListLines(tNode.Text)...)
			}
			if len(vals) == 0 {
				vals = splitValueListLines(cv.Text)
			}
		}
	}

	// File4Base rejects a custom value list with no entries, so an empty one is
	// reported here instead of reaching the import and rolling the whole
	// solution back.
	if len(vals) == 0 {
		return nil, []ReportItem{{
			Category:   CategoryManualActionRequired,
			Domain:     "value_list",
			SourceName: name,
			Detail:     fmt.Sprintf("Custom value list %q carried no values and was skipped; recreate it in Manage Database > Value Lists", name),
		}}
	}

	return &schema.BundleValueList{
			ID:           uuid.NewString(),
			Name:         name,
			Kind:         "custom",
			CustomValues: strings.Join(vals, "\n"),
		}, []ReportItem{{
			Category:   CategoryPreserved,
			Domain:     "value_list",
			SourceName: name,
			TargetName: name,
			Detail:     fmt.Sprintf("Custom value list with %d entries", len(vals)),
		}}
}

// splitValueListLines splits custom values written one per line, with
// carriage returns (FileMaker) or line feeds, dropping blank lines.
func splitValueListLines(text string) []string {
	var vals []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			vals = append(vals, trimmed)
		}
	}
	return vals
}

// convertFieldValueList binds a value list that takes its values from a
// field, with the options FileMaker gives such a list: a second field of the
// same table shown beside the first or in its place, the field the values are
// sorted by, and including only the values related to the current record of a
// table occurrence.
//
// Save a Copy as XML nests the options in <Field>: <PrimaryField> and
// <SecondaryField> each hold a <FieldReference name> with its
// <TableOccurrenceReference name>, and <ShowRelated value> the occurrence the
// related values start from. A DDR writes the same elements in the value list
// itself, naming a field with <Field table name> and the occurrence with
// <Table name>. A design document written by hand may name the field with
// table and field attributes, on the list or on <Field>.
//
// Both write show="False" on the first field when only the second is shown,
// and sort="True" on the field the values are sorted by. An option whose
// field or occurrence was not migrated is reported rather than silently
// dropped. So is a list whose field cannot be indexed (unindexable, by column
// id): FileMaker refuses one, and File4Base would offer nothing from it.
func convertFieldValueList(
	vn *xmlNode,
	name string,
	tableIDByName map[string]string,
	canonicalTableName map[string]string,
	occTableByName map[string]string,
	occIDByName map[string]string,
	colIDByTableCol map[string]string,
	fieldNamesByTable map[string]*fieldNameMap,
	externalOccs map[string]string,
	unindexable map[string]string,
) (*schema.BundleValueList, []ReportItem) {
	tblName := firstAttr(vn, "table", "base_table")
	fldName := firstAttr(vn, "field")
	options := vn
	if fChild := findFirstChild(vn, "Field"); fChild != nil {
		if findFirstChild(fChild, "PrimaryField") != nil {
			options = fChild
		}
		tblName = firstNonEmpty(tblName, firstAttr(fChild, "table"))
		fldName = firstNonEmpty(fldName, firstAttr(fChild, "name"))
	}
	primary := findFirstChild(options, "PrimaryField")
	secondary := findFirstChild(options, "SecondaryField")
	occName := ""
	if primary != nil {
		if occ, fld := valueListFieldReference(primary); fld != "" {
			occName, fldName = occ, fld
		}
	}
	if occID, external := externalOccs[strings.ToLower(firstNonEmpty(occName, tblName))]; external && fldName != "" {
		// Values from a field of another file's table, named through the
		// occurrence; it lists nothing until another solution's records can be
		// read through a bound file reference (#107).
		source := firstNonEmpty(occName, tblName) + "::" + readableFieldRefs(fldName)
		items := []ReportItem{{
			Category: CategoryPreserved, Domain: "value_list", SourceName: name, TargetName: name,
			Detail: fmt.Sprintf("Dynamic field value list from %s, a field of another file's table; it lists no value until that file's records can be read from this solution", source),
		}}
		if rel := findFirstChild(options, "ShowRelated"); secondary != nil || (rel != nil && isTrue(firstAttr(rel, "value"))) {
			items = append(items, ReportItem{
				Category: CategoryUnsupported, Domain: "value_list", SourceName: name, TargetName: name,
				Detail: fmt.Sprintf("Value list %q lost its second field and related-values options, which a list of another file's table does not keep yet", name),
			})
		}
		return &schema.BundleValueList{
			ID: uuid.NewString(), Name: name, Kind: "field",
			SourceOccurrenceID: &occID, SourceColumnName: externalFieldName(fldName),
		}, items
	}
	tSQL := canonicalTableName[strings.ToLower(tblName)]
	if occName != "" || tSQL == "" {
		if t := occTableByName[strings.ToLower(firstNonEmpty(occName, tblName))]; t != "" {
			tSQL = t
		}
	}
	tID := tableIDByName[tSQL]
	columnID := func(field string) string {
		if id, ok := namelessFieldID(field); ok {
			return colIDByTableCol[tSQL+".#"+id]
		}
		column := sanitizeIdentifier(field)
		if names := fieldNamesByTable[tSQL]; names != nil {
			if resolved, ok := names.resolve(field); ok {
				column = resolved
			}
		}
		return colIDByTableCol[tSQL+"."+column]
	}
	cID := columnID(fldName)
	sourceOcc := firstNonEmpty(occName, tblName)
	source := sourceOcc + "::" + readableFieldRefs(fldName)
	if tID == "" || cID == "" {
		return nil, []ReportItem{{
			Category:   CategoryManualActionRequired,
			Domain:     "value_list",
			SourceName: name,
			Detail:     fmt.Sprintf("Value list %q takes its values from %s, which was not migrated; recreate it in Manage Database > Value Lists", name, source),
		}}
	}
	if why := unindexable[cID]; why != "" {
		return nil, []ReportItem{{
			Category:   CategoryManualActionRequired,
			Domain:     "value_list",
			SourceName: name,
			Detail: fmt.Sprintf("Value list %q takes its values from %s, which cannot be indexed, so the list was not migrated: %s; "+
				"then recreate it in Manage Database > Value Lists", name, source, why),
		}}
	}
	vl := &schema.BundleValueList{
		ID:             uuid.NewString(),
		Name:           name,
		Kind:           "field",
		SourceTableID:  &tID,
		SourceColumnID: &cID,
	}
	detail := fmt.Sprintf("Dynamic field value list from %s", source)
	var lost []string

	if secondary != nil {
		occ, fld := valueListFieldReference(secondary)
		second := firstNonEmpty(occ, sourceOcc) + "::" + fld
		secondID := ""
		// The second field is read on the same record as the first, so it
		// must come from the same occurrence: one of another occurrence of
		// the table (a self-join, say) names a related record's value.
		if fld != "" && (occ == "" || strings.EqualFold(occ, sourceOcc)) {
			secondID = columnID(fld)
		}
		if secondID == "" {
			lost = append(lost, fmt.Sprintf("its second field %s (it now shows the first field only)", second))
		} else if unindexable[secondID] != "" {
			lost = append(lost, fmt.Sprintf("its second field %s, which cannot be indexed (it now shows the first field only)", second))
		} else {
			vl.SecondColumnID = &secondID
			vl.ShowSecondOnly = primary != nil && strings.EqualFold(firstAttr(primary, "show"), "false")
			if isTrue(firstAttr(secondary, "sort")) {
				vl.SortBy = schema.ValueListSortSecond
			}
			detail += ", showing " + second
		}
	}

	if rel := findFirstChild(options, "ShowRelated"); rel != nil && isTrue(firstAttr(rel, "value")) {
		from := ""
		if ref := findFirstChild(rel, "TableOccurrenceReference", "Table"); ref != nil {
			from = firstAttr(ref, "name", "Name")
		}
		startID := occIDByName[strings.ToLower(from)]
		sourceID := ""
		if occTableByName[strings.ToLower(sourceOcc)] == tSQL {
			sourceID = occIDByName[strings.ToLower(sourceOcc)]
		}
		if startID == "" || sourceID == "" {
			lost = append(lost, fmt.Sprintf("including only the values related from %q (it now lists every record)", from))
		} else {
			vl.RelatedOccurrenceID, vl.SourceOccurrenceID = &startID, &sourceID
			detail += ", only the values related from " + from
		}
	}

	items := []ReportItem{{
		Category:   CategoryPreserved,
		Domain:     "value_list",
		SourceName: name,
		TargetName: name,
		Detail:     detail,
	}}
	if len(lost) > 0 {
		items = append(items, ReportItem{
			Category:   CategoryUnsupported,
			Domain:     "value_list",
			SourceName: name,
			TargetName: name,
			Detail:     fmt.Sprintf("Value list %q lost %s", name, strings.Join(lost, ", ")),
		})
	}
	return vl, items
}

// valueListFieldReference is the occurrence and the name of the field a
// <PrimaryField> or <SecondaryField> names, in either grammar; a field named
// only by its id is its marker "#<id>".
func valueListFieldReference(n *xmlNode) (occurrence, field string) {
	if ref := findFirstChild(n, "FieldReference"); ref != nil {
		if to := findFirstChild(ref, "TableOccurrenceReference"); to != nil {
			occurrence = firstAttr(to, "name")
		}
		return occurrence, referencedFieldName(ref)
	}
	if f := findFirstChild(n, "Field"); f != nil {
		return firstAttr(f, "table"), referencedFieldName(f)
	}
	return "", ""
}

func convertLayoutNode(
	ln *xmlNode,
	occurrences []schema.BundleOccurrence,
	occIDByName map[string]string,
	occTableByName map[string]string,
	deletedOccurrences map[string]bool,
	objects *layoutObjectContext,
) (*schema.BundleLayout, []ReportItem) {
	var items []ReportItem
	name := firstAttr(ln, "name", "Name")
	if name == "" || nativeFolderMarker(ln) || nativeSeparatorMarker(ln) {
		return nil, nil
	}
	occName := firstAttr(ln, "table_occurrence", "table", "tableOccurrence")
	if occName == "" {
		if tRef := findFirstChild(ln, "Table", "TableOccurrenceReference"); tRef != nil {
			occName = firstAttr(tRef, "name", "Name")
		}
	}
	if occName == deletedTableOccurrence || deletedOccurrences[strings.ToLower(occName)] {
		return nil, []ReportItem{{Category: CategoryUnsupported, Domain: "layout", SourceName: name,
			Detail: "Layout is based on a table occurrence that was deleted in FileMaker, so it shows no records there; it was not migrated"}}
	}
	occID := occIDByName[strings.ToLower(occName)]
	hasExplicitOccurrence := occName != "" || findFirstChild(ln, "Table", "TableOccurrenceReference") != nil
	if occID == "" && hasExplicitOccurrence {
		return nil, []ReportItem{{Category: CategoryRejected, Domain: "layout", SourceName: name,
			Detail: fmt.Sprintf("Layout references unresolved table occurrence %q; its records must not be shown from an unrelated table", occName)}}
	}
	if occID == "" && occName == "" {
		// The first occurrence of one of this file's tables: an occurrence
		// of another file's is never a default.
		for _, o := range occurrences {
			if o.External == nil {
				occID, occName = o.ID, o.Name
				break
			}
		}
	}

	// objectIDs maps the field node of each migrated object to its id, so a
	// trigger set on that object is attached to it.
	objectIDs := map[*xmlNode]string{}

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
	} else if objects != nil && isNativeLayout(ln) {
		items = append(items, convertNativeLayout(ln, objects, def, name, occName, occID, objectIDs)...)
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
			colName := sanitizeIdentifier(fName)
			// The occurrence the field is shown from is kept. A field of
			// another file's table is named as that file's migration names
			// it (or by its id marker), and is never bound to a column of
			// this file that happens to share its name.
			fieldOcc := firstAttr(fn, "table", "Table")
			if externalOcc, field := externalLayoutField(fn, occurrences); field != "" {
				fieldOcc, colName = externalOcc, field
				if fName == "" {
					fName = field
				}
			}
			if fName == "" {
				continue
			}
			binding := map[string]interface{}{
				"field_name":    colName,
				"control_style": "edit_box",
			}
			if fieldOcc != "" {
				binding["table_occurrence"] = fieldOcc
			}
			// How many repetitions of a repeating field the object shows.
			if n, ok := parseRepetitions(firstAttr(fn, "numOfReps", "repetitions")); ok {
				binding["repetitions_shown"] = n
			}
			objID := uuid.NewString()
			objectIDs[fn] = objID
			objs = append(objs, map[string]interface{}{
				"id":            objID,
				"type":          "field",
				"field_name":    colName,
				"label":         fName,
				"x":             24,
				"y":             64 + idx*44,
				"width":         240,
				"height":        32,
				"field_binding": binding,
			})
		}
		if len(objs) > 0 {
			def["objects"] = objs
		}
	}

	// Script triggers are kept, switched off, until the developer reviews
	// them: an imported script never runs because a layout was opened.
	quarantineDefinitionTriggers(def)
	if triggers := readLayoutTriggers(ln); len(triggers) > 0 {
		list, _ := def[schema.LayoutScriptTriggersKey].([]interface{})
		def[schema.LayoutScriptTriggersKey] = append(list, layoutTriggerEntries(triggers, objectIDs)...)
	}

	items = append(items, ReportItem{
		Category:   CategoryPreserved,
		Domain:     "layout",
		SourceName: name,
		TargetName: name,
		Detail:     layoutOccurrenceDetail(occName, occurrences, occID),
	})

	return &schema.BundleLayout{
		ID:                uuid.NewString(),
		Name:              name,
		TableOccurrenceID: occID,
		Definition:        def,
	}, items
}

// layoutOccurrenceDetail says which occurrence a layout shows, and when it is
// one of another file's tables, that its records are not read from here yet.
func layoutOccurrenceDetail(occName string, occurrences []schema.BundleOccurrence, occID string) string {
	for _, o := range occurrences {
		if o.ID == occID && o.External != nil {
			return fmt.Sprintf("Layout bound to table occurrence %q, of a table of the file %q; it shows no record until that file's records can be read from this solution",
				occName, o.External.FileReference)
		}
	}
	return fmt.Sprintf("Layout bound to table occurrence %q", occName)
}

// occTableByName maps a lower-cased table occurrence name to the File4Base
// table it shows, so a step naming an occurrence can say which table it
// writes.
func convertScriptNode(sn *xmlNode, defaultContextTable string, occTableByName map[string]string) (*schema.BundleScript, []ReportItem) {
	var items []ReportItem
	name := firstAttr(sn, "name", "Name")
	if name == "" || nativeFolderMarker(sn) || nativeSeparatorMarker(sn) {
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

		resolved := canonicalScriptStep(rawStepName, firstAttr(stNode, "id", "ID"))
		canonical, isSupported, isLocalized := resolved.Canonical, resolved.Supported, resolved.Localized
		if resolved.Conflict {
			items = append(items, ReportItem{
				Category:   CategoryManualActionRequired,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail: fmt.Sprintf("Step name %q disagrees with its instruction id %q; it was read by its name",
					rawStepName, firstAttr(stNode, "id", "ID")),
			})
		}

		params, structured, unmapped := decodeStepParams(stNode, canonical)
		if canonical == "Save Records as PDF" && pdfSecurityRequested(stNode) {
			params["pdf_security"] = true
		}
		// The instruction id and the name as the report wrote it are kept as
		// diagnostic metadata: a localized report names its steps in its own
		// language, and the id is the only language-free key the document
		// carries (#103).
		if stepID := firstAttr(stNode, "id", "ID"); stepID != "" {
			params["filemaker_step_id"] = stepID
		}
		if isLocalized {
			params["filemaker_step_name"] = rawStepName
			items = append(items, ReportItem{
				Category:   CategoryTranslated,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail:     fmt.Sprintf("Localized step name read as %q", canonical),
			})
		}
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
			for _, it := range stepOptionItems(canonical, params, occTableByName) {
				it.SourceName = fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1)
				it.TargetName = name
				items = append(items, it)
			}
			if _, done := params["migration_note"]; !done && needsTransferDefinition(canonical) {
				items = append(items, ReportItem{
					Category:   CategoryManualActionRequired,
					Domain:     "script",
					SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
					TargetName: name,
					Detail: fmt.Sprintf("Step %q runs a File4Base transfer definition, which names the "+
						"two solutions, the field mappings and the match keys. The FileMaker file and "+
						"mapping this step referred to were not converted into one, so the step stops "+
						"the script until a transfer is created and named in it", canonical),
				})
			}
			// An Import Records step that reads another FileMaker file
			// waits, disabled, for the transfer the migration review
			// creates (#191); confirming it enables the step again only
			// if FileMaker had it enabled.
			if params["awaiting_transfer"] == true {
				params["filemaker_step_enabled"] = enabled
				enabled = false
			}
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
				Category:   CategoryManualActionRequired,
				Domain:     "script",
				SourceName: fmt.Sprintf("%s > %s (step %d)", name, rawStepName, idx+1),
				TargetName: name,
				Detail:     fmt.Sprintf("Step %q is retained in Script Workspace for inspection; the client runner cannot execute it and stops at this step", canonical),
			})
			continue
		}

		// Unsupported FileMaker script step: preserve as a disabled step so the
		// script can still be inspected, carrying its original operands so
		// nothing the source declared is lost (#88).
		commentParams := map[string]interface{}{
			"text":                    fmt.Sprintf("[UNSUPPORTED FM STEP: %s]", rawStepName),
			"original_filemaker_step": rawStepName,
			"filemaker_step_name":     rawStepName,
			"filemaker_step_enabled":  enabled,
		}
		if stepID := firstAttr(stNode, "id", "ID"); stepID != "" {
			commentParams["filemaker_step_id"] = stepID
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
				Detail: fmt.Sprintf("Unsupported FileMaker script step %q (instruction id %q%s) converted to disabled comment step; supported report languages: %s",
					rawStepName, firstAttr(stNode, "id", "ID"), englishStepSuffix(resolved), strings.Join(SupportedDDRLocales, ", ")),
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
	displayCalcFallback := ""
	for _, child := range stNode.Children {
		switch child.Name {
		case "UUID", "OwnerID", "DDRREF", "StepText", "DisableStepCollapsed", "Animation":
			// Object identity and display-only presentation metadata are not
			// executable operands (#133).
			continue
		case "DisplayCalculation":
			// Presentational tokenized rendering of <Calculation>; keep text
			// only as a fallback when <Calculation> is absent (#133).
			if displayCalcFallback == "" {
				displayCalcFallback = displayCalculationText(child)
			}
			continue
		case "Options":
			if len(child.Attrs) == 0 && len(child.Children) == 0 && strings.TrimSpace(child.Text) == "" {
				continue
			}
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["options_state"] = v
			}
			params["options"] = decodeNodeValue(child)
		case "Option":
			if canonicalStep == "Open File" || canonicalStep == "Set Error Logging" {
				// Both steps write their one option as <Option state="…"/>:
				// Open hidden for Open File, On/Off for Set Error Logging.
				key := "hidden"
				if canonicalStep == "Set Error Logging" {
					key = "state"
				}
				if v, ok := xmlAttrBool(child, "state", "value"); ok {
					params[key] = v
				} else {
					flagUnmapped(child.Name)
				}
				continue
			}
			// Commit Records/Requests option (e.g. SkipDataEntryValidation).
			raw := strings.TrimSpace(firstAttr(child, "value", "Value", "state", "name"))
			if raw == "" {
				raw = strings.TrimSpace(child.Text)
			}
			lower := strings.ToLower(raw)
			if strings.Contains(lower, "skip") || strings.Contains(lower, "validation") || lower == "true" {
				params["skip_validation"] = true
			} else if raw != "" {
				params["option"] = raw
			}
		case "SkipDataEntryValidation":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["skip_validation"] = v
			} else {
				params["skip_validation"] = true
			}
		case "ESSForceCommit", "ForceCommit":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["force_commit"] = v
			} else {
				params["force_commit"] = true
			}
		case "NoInteract":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["dialog"] = !v
			} else {
				params["dialog"] = false
			}
		case "Set", "SetState", "State", "Capture", "Boolean":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["state"] = v
			} else if raw := strings.TrimSpace(firstAttr(child, "value", "state")); raw != "" {
				params["state"] = strings.ToLower(raw)
			}
		case "Exit":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["exit_after_last"] = v
			} else {
				params["exit_after_last"] = true
			}
		case "Restore":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["restore"] = v
			} else {
				params["restore"] = true
			}
		case "Pause":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["pause"] = v
			} else {
				params["pause"] = true
			}
		case "LayoutDestination":
			decodeLayoutDestination(child, params)
		case "RowPageLocation":
			decodeRowPageLocation(child, params)
		case "Repetition":
			if expr := calculationText(child); expr != "" {
				params["repetition"] = expr
			} else if v := firstAttr(child, "value", "Value"); v != "" {
				params["repetition"] = v
			}
		case "Query", "FindRequest", "FindRequestList":
			if reqs := decodeFindRequests(child); len(reqs) > 0 {
				params["find_requests"] = reqs
			}
		case "SortList", "SortSpecification":
			if sortOrder := decodeSortOrder(child); len(sortOrder) > 0 {
				params["sort_order"] = sortOrder
			}
		case "PrintSettings", "PrintSetup", "PageSetup", "Print":
			params["print_settings"] = decodeNodeValue(child)
		case "ImportOptions", "FieldMap":
			params["import_options"] = decodeNodeValue(child)
		case "ExportOptions":
			params["export_options"] = decodeNodeValue(child)
		case "PDFOptions", "SaveOptions":
			params["pdf_options"] = decodeNodeValue(child)
		case "UniversalPathList", "File":
			if txt := strings.TrimSpace(child.Text); txt != "" {
				params["path"] = txt
			} else if v := firstAttr(child, "value", "path", "name"); v != "" {
				params["path"] = v
			} else {
				params["path"] = decodeNodeValue(child)
			}
		case "Title":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["title"] = calculationText(calc)
				params["title_is_calculation"] = true
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["title"] = txt
			}
		case "Message":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["message"] = calculationText(calc)
				params["message_is_calculation"] = true
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["message"] = txt
			}
		case "Buttons":
			if btns := decodeDialogButtons(child); len(btns) > 0 {
				params["buttons"] = btns
			}
		case "InputFields":
			if inputs := decodeDialogInputFields(child); len(inputs) > 0 {
				params["input_fields"] = inputs
			}
		case "Window", "WindowName", "CurrentWindow":
			decodeWindowNode(child, params)
		case "NewWndStyles", "WindowStyle":
			if v := firstAttr(child, "Style", "style", "value", "name"); v != "" {
				params["window_style"] = v
			} else {
				params["window_style"] = decodeNodeValue(child)
			}
		case "Top", "Left", "Width", "Height":
			key := strings.ToLower(child.Name)
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params[key] = calculationText(calc)
			} else if v := firstAttr(child, "value"); v != "" {
				params[key] = v
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params[key] = txt
			}
		case "Adjust", "WindowAction", "Action":
			if v := firstAttr(child, "value", "Value", "name"); v != "" {
				params["window_action"] = v
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["window_action"] = txt
			}
		case "SelectAll", "Select":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["select"] = v
			} else {
				params["select"] = true
			}
		case "Target", "TargetField", "FieldByName", "TargetName":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["target_field"] = calculationText(calc)
				params["target_field_is_calculation"] = true
			} else if fn := findFirstChild(child, "Field", "FieldReference"); fn != nil {
				if fname := firstAttr(fn, "name", "Name"); fname != "" {
					if strings.HasPrefix(fname, "$") {
						params["target"] = fname
					} else {
						params["field"] = sanitizeIdentifier(fname)
						params["field_label"] = fname
					}
				}
			} else if v := firstAttr(child, "value", "name"); v != "" {
				params["target"] = v
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["target_field"] = txt
			}
		case "Object", "ObjectName":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["object_name"] = calculationText(calc)
				params["object_name_is_calculation"] = true
			} else if v := firstAttr(child, "value", "name"); v != "" {
				params["object_name"] = v
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["object_name"] = txt
			}
		case "RelatedTable", "Table":
			if name := firstAttr(child, "name", "Name"); name != "" {
				params["related_table"] = name
			}
		case "ShowRelated", "ShowOnlyRelated", "MatchCurrentRecordOnly":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params["show_only_related"] = v
			}
		case "Replace", "ReplaceType", "CurrentContents", "SerialNumbers":
			if v := firstAttr(child, "type", "value", "name"); v != "" {
				params["replace_type"] = strings.ToLower(v)
			} else {
				params["replace_type"] = strings.ToLower(child.Name)
			}
		case "URL":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["url"] = calculationText(calc)
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["url"] = txt
			}
		case "cURLOptions", "CURLOptions":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["curl_options"] = calculationText(calc)
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params["curl_options"] = txt
			}
		case "VerifySSLCertificates", "AutoEncode", "Encode":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params[strings.ToLower(child.Name)] = v
			}
		case "To", "CC", "BCC", "Subject", "Attachment", "Attachments", "Mail", "SendMail":
			key := strings.ToLower(child.Name)
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params[key] = calculationText(calc)
			} else if txt := strings.TrimSpace(child.Text); txt != "" {
				params[key] = txt
			} else {
				params[key] = decodeNodeValue(child)
			}
		case "MenuSet", "CustomMenuSet":
			if name := firstAttr(child, "name", "Name", "value"); name != "" {
				params["menu_set"] = name
			}
		case "ToolbarState", "ShowHide", "Lock", "IncludeEditRecordToolbar":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params[strings.ToLower(child.Name)] = v
			} else if raw := firstAttr(child, "value", "state"); raw != "" {
				params["state"] = strings.ToLower(raw)
			}
		case "FlushCache", "FlushCachedJoinResults", "FlushCachedExternalData", "FlushSQLData":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params[strings.ToLower(child.Name)] = v
			} else {
				params[strings.ToLower(child.Name)] = true
			}
		case "Duration", "Indefinitely":
			if calc := findFirstChild(child, "Calculation", "calculation"); calc != nil {
				params["duration_seconds"] = calculationText(calc)
			} else if v := firstAttr(child, "value", "state"); v != "" {
				params[strings.ToLower(child.Name)] = v
			}
		case "StoreOnlyAsReference", "ReferenceOnly", "Compression", "Display", "AutomaticallyOpenFile", "AutomaticallyOpen", "CreateFolders", "CreateEmail", "Append":
			if v, ok := xmlAttrBool(child, "state", "value"); ok {
				params[strings.ToLower(child.Name)] = v
			} else if raw := firstAttr(child, "value", "state"); raw != "" {
				params[strings.ToLower(child.Name)] = raw
			}
		case "Calculation", "calculation":
			if text := calculationText(child); text != "" && calcText == "" {
				calcText = text
			}
		case "Field", "field":
			fieldName := firstAttr(child, "name", "Name")
			if fieldName == "" && canonicalStep == "Insert Current Date" {
				// The DDR writes a variable target as element text,
				// <Field>$today</Field>. A target that cannot be read is
				// marked, so the step fails instead of writing into
				// whatever field happens to be active.
				if text := strings.TrimSpace(child.Text); strings.HasPrefix(text, "$") {
					params["name"] = text
				} else {
					params["target_unresolved"] = true
					flagUnmapped(child.Name)
				}
				break
			}
			if fieldName != "" {
				if strings.HasPrefix(fieldName, "$") {
					params["name"] = fieldName
					break
				}
				params["field"] = sanitizeIdentifier(fieldName)
				params["field_label"] = fieldName
				if tbl := firstAttr(child, "table", "Table"); tbl != "" {
					params["field_table"] = sanitizeIdentifier(tbl)
				}
				if rep := firstAttr(child, "repetition", "Repetition"); rep != "" && rep != "1" {
					params["repetition"] = rep
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
			} else if v := firstAttr(child, "value"); v != "" {
				params["name"] = v
			}
		case "Value", "value":
			if text := calculationText(child); text != "" {
				params["value"] = text
			}
		case "Text", "text":
			if canonicalStep == "Comment" {
				params["text"] = child.Text
			} else {
				flagUnmapped(child.Name)
			}
		case "params", "ParameterValues":
			if child.Name == "ParameterValues" && findFirstChild(child, "Parameter") != nil {
				native, unknown := decodeNativeParameters(child, canonicalStep)
				for k, v := range native {
					params[k] = v
				}
				for _, name := range unknown {
					flagUnmapped(name)
				}
				continue
			}
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
			} else {
				flagUnmapped(child.Name)
			}
		default:
			flagUnmapped(child.Name)
		}
	}

	if calcText == "" && displayCalcFallback != "" {
		calcText = displayCalcFallback
	}
	if calcText != "" {
		switch canonicalStep {
		case "Set Field", "Insert Calculated Result", "Replace Field Contents", "Set Next Serial Value":
			params["value"] = calcText
			params["value_is_calculation"] = true
		case "Set Field By Name":
			if _, ok := params["target_field"]; !ok {
				params["target_field"] = calcText
				params["target_field_is_calculation"] = true
			} else {
				params["value"] = calcText
				params["value_is_calculation"] = true
			}
		case "If", "Else If", "Exit Loop If":
			params["condition"] = calcText
		case "Set Variable":
			if _, ok := params["value"]; !ok {
				params["value"] = calcText
			}
		case "Open URL", "Insert from URL":
			if _, ok := params["url"]; !ok {
				params["url"] = calcText
			}
		case "Perform Script":
			params["script_parameter"] = calcText
		case "Exit Script":
			params["result"] = calcText
		case "Show Custom Dialog":
			if _, ok := params["message"]; !ok {
				params["message"] = calcText
			}
		case "Go to Layout":
			if _, ok := params["layout_calculation"]; !ok {
				params["layout_calculation"] = calcText
			}
		case "Go to Record/Request/Page", "Go to Portal Row":
			if _, ok := params["record_calculation"]; !ok {
				params["record_calculation"] = calcText
			}
		case "Go to Object", "Refresh Portal":
			if _, ok := params["object_name"]; !ok {
				params["object_name"] = calcText
				params["object_name_is_calculation"] = true
			}
		case "Set Error Logging":
			params["custom_debug_info"] = calcText
		case "Pause/Resume Script":
			if _, ok := params["duration_seconds"]; !ok {
				params["duration_seconds"] = calcText
			}
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

// redactedSecret replaces a secret a design document carries (an SMTP
// password of Send Mail, a PDF password of Save Records as PDF) wherever the
// migration keeps FileMaker's own options: such a value is never stored.
const redactedSecret = "[redacted]"

// isSecretName tells whether an element, attribute or option type names a
// password.
func isSecretName(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "password") || strings.Contains(l, "passwd")
}

// redactedNode is what is kept of an element that holds a secret: whether
// it is set (a True/False switch is not a secret), never its value.
func redactedNode(n *xmlNode) interface{} {
	value := map[string]interface{}{}
	for k, v := range n.Attrs {
		switch {
		case strings.EqualFold(k, "type"), strings.EqualFold(k, "name"):
			value[k] = v
		case strings.EqualFold(v, "true"), strings.EqualFold(v, "false"):
			value[k] = v
		case strings.TrimSpace(v) != "":
			value[k] = redactedSecret
		}
	}
	if strings.TrimSpace(n.Text) != "" || len(n.Children) > 0 {
		value["#text"] = redactedSecret
	}
	if len(value) == 0 {
		return nil
	}
	return value
}

func decodeNodeValue(n *xmlNode) interface{} {
	if isSecretName(n.Name) || isSecretName(firstAttr(n, "type")) {
		return redactedNode(n)
	}
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
		if isSecretName(k) && strings.TrimSpace(v) != "" {
			v = redactedSecret
		}
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
	// A Database Design Report puts the name and privilege set in attributes;
	// Save a Copy as XML writes <Authentication><AccountName> and a
	// <PrivilegeSetReference name=…> child instead.
	username := strings.TrimSpace(firstAttr(an, "name", "username", "AccountName", "Name"))
	if username == "" {
		if auth := findFirstChild(an, "Authentication"); auth != nil {
			if n := findFirstChild(auth, "AccountName"); n != nil {
				username = strings.TrimSpace(n.Text)
			}
		}
	}
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

	privSet := accountPrivilegeSet(an)
	role := roleForPrivilegeSet(privSet)

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

// accountPrivilegeSet is the name of the privilege set an account uses.
func accountPrivilegeSet(an *xmlNode) string {
	privSet := strings.TrimSpace(firstAttr(an, "privilegeSet", "role", "PrivilegeSet"))
	if privSet == "" {
		if ref := findFirstChild(an, "PrivilegeSetReference"); ref != nil {
			privSet = strings.TrimSpace(firstAttr(ref, "name"))
		}
	}
	return privSet
}

// roleForPrivilegeSet is the account role a FileMaker privilege set maps to.
func roleForPrivilegeSet(privSet string) string {
	switch strings.ToLower(privSet) {
	case "[full access]", "full access", "owner":
		return "owner"
	case "admin", "administrator":
		return "admin"
	}
	return "user"
}

// fmrestGrants carries FileMaker's fmrest extended privilege (Access via
// FileMaker Data API) over to the roles the file's accounts map to. Save a
// Copy as XML lists, in its ExtendedPrivilegesCatalog, the privilege sets
// each extended privilege is enabled for; setsByRole holds the privilege sets
// the migrated accounts use, by the role each maps to.
//
// File4Base grants it per role, which is coarser than FileMaker's per
// privilege set (#213): a role is granted fmrest when any of its privilege
// sets carries it, and the accounts of its other sets gain the Data API too,
// which the report says. Owners hold it whatever the file says.
func fmrestGrants(root *xmlNode, setsByRole map[string][]string) ([]CapabilityGrant, []ReportItem) {
	extended := catalogDefinitions(root, []string{"ExtendedPrivilegesCatalog"}, "ExtendedPrivilege")
	if len(extended) == 0 {
		return nil, fmrestUnknownItems(setsByRole)
	}
	withFMRest := map[string]bool{}
	for _, ep := range extended {
		if !strings.EqualFold(strings.TrimSpace(firstAttr(ep, "name")), schema.CapabilityFMRest) {
			continue
		}
		visitNodes(ep, func(n *xmlNode) {
			if nodeNamed(n, "PrivilegeSetReference") {
				withFMRest[strings.ToLower(strings.TrimSpace(firstAttr(n, "name")))] = true
			}
		})
	}

	var grants []CapabilityGrant
	var items []ReportItem
	for _, role := range []string{"owner", "admin", "user"} {
		var granting, without []string
		for _, set := range setsByRole[role] {
			if withFMRest[strings.ToLower(set)] {
				granting = append(granting, set)
			} else {
				without = append(without, set)
			}
		}
		if role == "owner" {
			// Owners hold every capability, as [Full Access] holds every
			// extended privilege; a set mapped to the role that does not
			// carry fmrest gains it all the same, which is reported.
			granting = nil
		}
		// One item per role: the grant, or what the role's accounts gain
		// without their set carrying fmrest, which is to review.
		if len(granting) > 0 {
			grants = append(grants, CapabilityGrant{Role: role, Capability: schema.CapabilityFMRest, Sets: granting})
		}
		granted := fmt.Sprintf("The fmrest extended privilege (Access via FileMaker Data API) of privilege set(s) %s is granted to the role %q as its fmrest privilege",
			quoteList(granting), role)
		switch {
		case len(without) == 0 && len(granting) > 0:
			items = append(items, ReportItem{
				Category: CategoryTranslated, Domain: "privilege", SourceName: strings.Join(granting, ", "), TargetName: role, Detail: granted,
			})
		case len(without) > 0 && len(granting) > 0:
			items = append(items, ReportItem{
				Category: CategoryManualActionRequired, Domain: "privilege", SourceName: strings.Join(append(append([]string(nil), granting...), without...), ", "), TargetName: role,
				Detail: fmt.Sprintf("%s. Privilege set(s) %s do not carry fmrest in FileMaker, but their accounts hold that role, so they gain it too: File4Base grants the Data API per role, not per privilege set (#213). "+
					"Review these accounts, or withhold fmrest from the role in Manage Security", granted, quoteList(without)),
			})
		case len(without) > 0 && role == "owner":
			items = append(items, ReportItem{
				Category: CategoryManualActionRequired, Domain: "privilege", SourceName: strings.Join(without, ", "), TargetName: role,
				Detail: fmt.Sprintf("Privilege set(s) %s do not carry fmrest in FileMaker, but their accounts hold the role %q, which does: File4Base grants the Data API per role, not per privilege set (#213). "+
					"Review these accounts: an owner always holds fmrest", quoteList(without), role),
			})
		}
	}
	return grants, items
}

// fmrestUnknownItems reports, for a document that does not say which
// privilege sets carry fmrest, that no account role was granted the Data API.
func fmrestUnknownItems(setsByRole map[string][]string) []ReportItem {
	var items []ReportItem
	for _, role := range []string{"admin", "user"} {
		if len(setsByRole[role]) == 0 {
			continue
		}
		items = append(items, ReportItem{
			Category: CategoryManualActionRequired, Domain: "privilege", SourceName: strings.Join(setsByRole[role], ", "), TargetName: role,
			Detail: fmt.Sprintf("The document does not list which privilege sets carry the fmrest extended privilege (Access via FileMaker Data API), so the role %q is not granted it; "+
				"grant fmrest to the role in Manage Security if accounts of privilege set(s) %s use the Data API", role, quoteList(setsByRole[role])),
		})
	}
	return items
}

func visitNodes(n *xmlNode, fn func(*xmlNode)) {
	for _, c := range n.Children {
		fn(c)
		visitNodes(c, fn)
	}
}

func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return strings.Join(quoted, ", ")
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return list
		}
	}
	return append(list, s)
}

// ─── Minimal XML Tree Helpers ──────────────────────────────────────────────

// stripXMLControlChars removes the characters XML 1.0 forbids (C0 controls
// other than tab, line feed and carriage return, and U+FFFE/U+FFFF).
// FileMaker copies them unescaped from stored text, for example an ETX
// pasted into a layout label, so its own export would otherwise be refused.
// It returns the input unchanged, without copying, when nothing is removed.
func stripXMLControlChars(s string) (string, int) {
	if strings.IndexFunc(s, notXMLChar) < 0 {
		return s, 0
	}
	removed := 0
	clean := strings.Map(func(r rune) rune {
		if notXMLChar(r) {
			removed++
			return -1
		}
		return r
	}, s)
	return clean, removed
}

func notXMLChar(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n' && r != '\r') || r == 0xFFFE || r == 0xFFFF
}

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
	"(<FMPReport> XML or the HTML report), a <FMSaveAsXML> or <FMPSaveAsXML> Save a Copy as XML document " +
	"(FileMaker Pro 20 / Claris 26, exported with \"Include details for analysis tools\"), and its own design XML"

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
	if s := extractDeclaredFileName(root); s != "" {
		return strings.TrimSuffix(s, ".fmp12")
	}
	return ""
}

func extractDeclaredFileName(root *xmlNode) string {
	if s := firstAttr(root, "solution", "name", "file", "File"); s != "" {
		return s
	}
	if f := findFirstChild(root, "File", "Database", "FMSaveAsXMLFile"); f != nil {
		if s := firstAttr(f, "name", "Name"); s != "" {
			return s
		}
	}
	return ""
}

// unstoredCalculation reports whether a calculation field's options say
// "Do not store calculation results".
func unstoredCalculation(fn *xmlNode) bool {
	storage := findFirstChild(fn, "Storage", "storage")
	return storage != nil && strings.EqualFold(firstAttr(storage, "storeCalculationResults"), "false")
}

func classifyXMLFieldSourceType(fn *xmlNode, fallbackFieldType string) (string, string) {
	rawType := strings.ToUpper(strings.TrimSpace(firstAttr(fn, "dataType", "type", "Type")))
	fieldKind := strings.ToUpper(strings.TrimSpace(firstAttr(fn, "fieldType", "kind")))
	if fieldKind == "CALCULATED" {
		fieldKind = "CALCULATION"
	}
	calcText := ""
	if calcNode := findFirstChild(fn, "Calculation", "calculation"); calcNode != nil {
		calcText = calculationText(calcNode)
	}
	kind := "NORMAL"
	if fieldKind == "SUMMARY" || rawType == "SUMMARY" {
		kind = "SUMMARY"
	} else if fieldKind == "CALCULATION" || rawType == "CALCULATION" || calcText != "" {
		kind = "CALCULATION"
	}
	dataType := rawType
	if dataType == "" {
		dataType = strings.ToUpper(strings.TrimSpace(fallbackFieldType))
	}
	switch dataType {
	case "BINARY", "BLOB", "CONTAINER":
		dataType = "CONTAINER"
	case "NUMERIC", "DECIMAL", "INTEGER", "FLOAT", "NUMBER":
		dataType = "NUMBER"
	case "DATETIME", "TIMESTAMP":
		dataType = "TIMESTAMP"
	case "CALCULATED":
		dataType = "CALCULATION"
	}
	return kind, dataType
}

// firstAttr answers the first of keys the element carries.
//
// Each key is looked for as written first, so a document that spells an
// attribute exactly as the reader expects keeps resolving in one map lookup
// and in the order the caller asked for. Only then is the whole attribute set
// compared case-insensitively: <FMSaveAsXML> writes datatype and fieldtype
// where a Database Design Report writes dataType and fieldType, and matching
// on the exact spelling alone read every field of such a document as an
// untyped one (#87).
func firstAttr(n *xmlNode, keys ...string) string {
	for _, k := range keys {
		if v, ok := n.Attrs[k]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	for _, k := range keys {
		for name, v := range n.Attrs {
			if strings.EqualFold(name, k) && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
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
// Permissive source Number text cannot be replaced by a guessed numeric value.
// In particular, a slash in text is not a division operator.
func coerceSourceValue(valStr, fieldType string) (interface{}, error) {
	if fieldType == "NUMBER" {
		literal := strings.TrimSpace(valStr)
		if literal == "" {
			return nil, nil
		}
		if !reSourceNumericLiteral.MatchString(literal) {
			return nil, errors.New("is not a canonical decimal; localized or permissive source Number text requires compatible representation")
		}
		return literal, nil
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
				return at.Format(time.RFC3339Nano), nil
			}
		}
		for _, layout := range []string{"15:04:05", "15:04"} {
			if at, err := time.Parse(layout, trimmed); err == nil {
				return timeOfDay(at), nil
			}
		}
		return nil, errors.New("is not a date and time")
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
		if value, err := coerceSourceValue(valStr, fieldType); err == nil {
			return value
		}
		return nil
	case "BOOLEAN":
		low := strings.ToLower(valStr)
		return low == "true" || low == "1" || low == "yes"
	default:
		return valStr
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// isTrue reads a FileMaker boolean attribute ("True"/"False").
func isTrue(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// baseTableDeleted reports an occurrence whose base table was deleted: the
// source reference is empty, or names FileMaker's placeholder.
func baseTableDeleted(on *xmlNode, baseName string) bool {
	if baseName == deletedBaseTable {
		return true
	}
	if baseName != "" || firstAttr(on, "baseTableId", "baseTableID", "base_table_id") != "" {
		return false
	}
	src := findFirstChild(on, "BaseTableSourceReference")
	return src != nil && findFirstChild(src, "BaseTableReference") == nil && len(src.Children) == 0
}

// relationshipSides names the two occurrences a relationship joins.
func relationshipSides(rn *xmlNode) (string, string) {
	left := firstAttr(rn, "left", "leftTable", "LeftTable")
	right := firstAttr(rn, "right", "rightTable", "RightTable")
	if n := findFirstChild(rn, "LeftTable", "leftTable"); left == "" && n != nil {
		left = firstAttr(n, "name", "Name")
	}
	if n := findFirstChild(rn, "RightTable", "rightTable"); right == "" && n != nil {
		right = firstAttr(n, "name", "Name")
	}
	return left, right
}
