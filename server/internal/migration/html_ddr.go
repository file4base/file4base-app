package migration

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/google/uuid"
)

var (
	reHTMLSolutionName  = regexp.MustCompile(`(?is)<a\s+name="valFileOptionOverviewSectionAnchor_"\s*(?:/>|>\s*</a>)\s*([^<]+)`)
	reHTMLBaseTable1    = regexp.MustCompile(`(?is)<table[^>]*>\s*(?:<tbody[^>]*>\s*)?<tr[^>]*>\s*<td[^>]*>\s*Table Name:\s*<a\s+name="(\d+)BaseTableAnchor_"\s*(?:/>|>\s*</a>)\s*([^<]+)</td>\s*</tr>(.*?)</table>`)
	reHTMLBaseTable2    = regexp.MustCompile(`(?is)<a\s+name="(\d+)BaseTableAnchor_"\s*(?:/>|>\s*</a>)\s*([^<]+)</span>\s*<table[^>]*>(.*?)</table>`)
	reHTMLTableCountSuf = regexp.MustCompile(`\s*-\s*\d+\s*Fields?\s*$`)
	reHTMLTableRow      = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	reHTMLTableCell     = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	reHTMLFieldIDAnchor = regexp.MustCompile(`(?is)<a\s+name="(\d+)FieldAnchor_"`)
	reHTMLInlineTags    = regexp.MustCompile(`(?is)</?(?:em|strong|b|i|a|span|font)(?:\s+[^>]*)?>`)
	reHTMLTags          = regexp.MustCompile(`(?is)<[^>]+>`)
	reHTMLSpaces        = regexp.MustCompile(`[ \t\r\n]+`)
	reMergeToken        = regexp.MustCompile(`<<\s*(?:([^:>]+)::)?([^>]+?)\s*>>`)
	reCalcListItems     = regexp.MustCompile(`(?is)<strong>Calculation:</strong>\s*<ul>\s*<li>.*?</li>\s*<li>(.*?)</li>`)
	rePortalAtCoords    = regexp.MustCompile(`(?is)<li>\s*(.*?)\s+at\s*\(\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*\)\s*</li>`)
	reTopCoord          = regexp.MustCompile(`(?i)Top:\s*(-?\d+(?:\.\d+)?)\s*pt`)
	reLeftCoord         = regexp.MustCompile(`(?i)Left:\s*(-?\d+(?:\.\d+)?)\s*pt`)
	reBottomCoord       = regexp.MustCompile(`(?i)Bottom:\s*(-?\d+(?:\.\d+)?)\s*pt`)
	reRightCoord        = regexp.MustCompile(`(?i)Right:\s*(-?\d+(?:\.\d+)?)\s*pt`)
	reNumRows           = regexp.MustCompile(`(?i)Number of Rows:\s*(\d+)`)
	reListItem          = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`)
)

// isHTMLDDR reports whether raw input looks like a FileMaker HTML Database Design Report.
func isHTMLDDR(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 32 {
		return false
	}
	head := strings.ToLower(trimmed[:minInt(len(trimmed), 512)])
	if !strings.HasPrefix(head, "<!doctype html") && !strings.HasPrefix(head, "<html") {
		return false
	}
	return strings.Contains(raw, "BaseTableAnchor_") ||
		strings.Contains(raw, "valTablesSectionAnchor_") ||
		strings.Contains(raw, "valRelationshipsSectionAnchor_") ||
		strings.Contains(raw, "valLayoutsSectionAnchor_")
}

type htmlFieldMeta struct {
	TableID    int
	FieldID    int
	TableSQL   string
	RawName    string
	ColName    string
	FieldType  string
	RawOptions string
	Formula    string
}

type htmlPortalDef struct {
	Top         float64
	Left        float64
	Bottom      float64
	Right       float64
	Occurrence  string
	RowCount    int
	ChildFields []htmlLayoutField
}

type htmlLayoutField struct {
	Occurrence   string
	FieldName    string
	Top          float64
	Left         float64
	Bottom       float64
	Right        float64
	ControlStyle string
	ActionButton *htmlLayoutButton
}

type htmlLayoutButton struct {
	Label       string
	Top         float64
	Left        float64
	Bottom      float64
	Right       float64
	ActionName  string
	ScriptName  string
	LayoutName  string
	IsTextGroup bool
}

type htmlLayoutMerge struct {
	Text   string
	Top    float64
	Left   float64
	Bottom float64
	Right  float64
}

type htmlLayoutWebViewer struct {
	Name   string
	URL    string
	Top    float64
	Left   float64
	Bottom float64
	Right  float64
}

type htmlParsedLayout struct {
	Name       string
	Occurrence string
	Width      float64
	Height     float64
	Fields     []htmlLayoutField
	Merges     []htmlLayoutMerge
	Buttons    []htmlLayoutButton
	Portals    []htmlPortalDef
	WebViewers []htmlLayoutWebViewer
}

// analyzeAndConvertHTMLDDR parses a FileMaker HTML DDR document and optional
// FMPXMLRESULT / binary .fmp12 data sources into a ConvertedSolution.
func analyzeAndConvertHTMLDDR(req MigrationRequest) (*ConvertedSolution, error) {
	rawHTML := req.DesignXML

	solName := strings.TrimSpace(req.SolutionName)
	if solName == "" {
		if m := reHTMLSolutionName.FindStringSubmatch(rawHTML); len(m) > 1 {
			solName = strings.TrimSuffix(cleanHTMLText(m[1]), ".fmp12")
		}
	}
	if solName == "" {
		solName = "Migrated_Solution"
	}

	report := &ConversionReport{
		SourceFormat:         "filemaker_ddr_html",
		SolutionName:         solName,
		TriggersQuarantined:  true,
		AccountsDisabled:     true,
		Preserved:            make([]ReportItem, 0),
		Translated:           make([]ReportItem, 0),
		Unsupported:          make([]ReportItem, 0),
		Rejected:             make([]ReportItem, 0),
		ManualActionRequired: make([]ReportItem, 0),
	}

	bundle := &schema.SolutionBundle{
		Format:       schema.SolutionFormat,
		Version:      schema.SolutionVersion,
		SolutionName: solName,
		Tables:       make([]schema.BundleTable, 0),
	}

	tableIDByName := make(map[string]string)
	canonicalTableName := make(map[string]string)
	colIDByTableCol := make(map[string]string)
	colTypeByTableCol := make(map[string]string)
	fmpTableMeta := make(map[int]string)               // fmpTableID -> sqlTableName
	fmpFieldMeta := make(map[int]map[int]htmlFieldMeta) // fmpTableID -> fmpFieldID -> meta

	// 1. Parse Base Tables & Fields
	btMatches := reHTMLBaseTable1.FindAllStringSubmatch(rawHTML, -1)
	if len(btMatches) == 0 {
		btMatches = reHTMLBaseTable2.FindAllStringSubmatch(rawHTML, -1)
	}
	seenTables := make(map[string]bool)
	for _, m := range btMatches {
		fmpTblID, _ := strconv.Atoi(m[1])
		rawTableName := strings.TrimSpace(reHTMLTableCountSuf.ReplaceAllString(cleanHTMLText(m[2]), ""))
		tableHTML := m[3]
		if rawTableName == "" {
			continue
		}
		sqlName := sanitizeIdentifier(rawTableName)
		if seenTables[sqlName] {
			continue
		}
		seenTables[sqlName] = true
		fmpTableMeta[fmpTblID] = sqlName
		fmpFieldMeta[fmpTblID] = make(map[int]htmlFieldMeta)

		tID := uuid.NewString()
		tableIDByName[strings.ToLower(rawTableName)] = tID
		tableIDByName[sqlName] = tID
		canonicalTableName[strings.ToLower(rawTableName)] = sqlName
		canonicalTableName[sqlName] = sqlName

		bTable := schema.BundleTable{
			ID:          tID,
			Name:        sqlName,
			DisplayName: rawTableName,
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

		rows := reHTMLTableRow.FindAllStringSubmatch(tableHTML, -1)
		type rawFieldRow struct {
			fmpFieldID int
			rawName    string
			colName    string
			rawType    string
			rawOptions string
		}
		var rawFields []rawFieldRow
		knownSameTableCols := map[string]string{"id": "id"}
		for _, rMatch := range rows {
			cells := reHTMLTableCell.FindAllStringSubmatch(rMatch[1], -1)
			if len(cells) < 3 {
				continue
			}
			fAnchor := reHTMLFieldIDAnchor.FindStringSubmatch(cells[0][1])
			if len(fAnchor) < 2 {
				continue
			}
			fullAnchorID := fAnchor[1]
			tblPrefix := strconv.Itoa(fmpTblID)
			fID := 0
			if strings.HasPrefix(fullAnchorID, tblPrefix) {
				fID, _ = strconv.Atoi(strings.TrimPrefix(fullAnchorID, tblPrefix))
			}
			fRawName := cleanHTMLText(cells[0][1])
			fRawType := cleanHTMLText(cells[1][1])
			fRawOpts := extractRawFieldOptions(cells[2][1])
			if fRawName == "" {
				continue
			}
			cName := sanitizeIdentifier(fRawName)
			knownSameTableCols[strings.ToLower(fRawName)] = cName
			knownSameTableCols[cName] = cName
			rawFields = append(rawFields, rawFieldRow{
				fmpFieldID: fID,
				rawName:    fRawName,
				colName:    cName,
				rawType:    fRawType,
				rawOptions: fRawOpts,
			})
		}

		seenCols := map[string]bool{"id": true}
		var nonCalcCols []schema.BundleColumn
		var calcCols []schema.BundleColumn

		for _, rf := range rawFields {
			if fmpTblID > 0 && rf.fmpFieldID > 0 {
				fmpFieldMeta[fmpTblID][rf.fmpFieldID] = htmlFieldMeta{
					TableID:    fmpTblID,
					FieldID:    rf.fmpFieldID,
					TableSQL:   sqlName,
					RawName:    rf.rawName,
					ColName:    rf.colName,
					FieldType:  rf.rawType,
					RawOptions: rf.rawOptions,
				}
			}
			if seenCols[rf.colName] {
				continue
			}
			seenCols[rf.colName] = true

			col, items := convertHTMLField(sqlName, rawTableName, rf.rawName, rf.colName, rf.rawType, rf.rawOptions, knownSameTableCols)
			for _, it := range items {
				addReportItem(report, it)
			}
			if col.FieldType == "CALCULATION" {
				calcCols = append(calcCols, col)
			} else {
				nonCalcCols = append(nonCalcCols, col)
			}
			colIDByTableCol[sqlName+"."+rf.colName] = col.ID
			colIDByTableCol[strings.ToLower(rawTableName)+"."+strings.ToLower(rf.rawName)] = col.ID
			colTypeByTableCol[sqlName+"."+rf.colName] = col.FieldType
			if fmpTblID > 0 && rf.fmpFieldID > 0 {
				meta := fmpFieldMeta[fmpTblID][rf.fmpFieldID]
				meta.FieldType = col.FieldType
				fmpFieldMeta[fmpTblID][rf.fmpFieldID] = meta
			}
			report.Counts.Fields++
		}

		bTable.Columns = append(bTable.Columns, nonCalcCols...)
		bTable.Columns = append(bTable.Columns, calcCols...)

		bundle.Tables = append(bundle.Tables, bTable)
		addReportItem(report, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "table",
			SourceName: rawTableName,
			TargetName: sqlName,
			Detail:     fmt.Sprintf("Base table with %d migrated fields", len(bTable.Columns)-1),
		})
	}
	report.Counts.Tables = len(bundle.Tables)

	// 2. Parse Table Occurrences from Relationships section
	occIDByName := make(map[string]string)
	occTableByName := make(map[string]string)
	relSection := sectionBetween(rawHTML, `name="valRelationshipsSectionAnchor_"`, `name="valLayoutsSectionAnchor_"`)
	if relSection == "" {
		relSection = rawHTML
	}
	toBlock := sectionBetween(relSection, `Table Occurrences`, `name="valDetailsSectionAnchor_"`)
	if toBlock != "" {
		rows := reHTMLTableRow.FindAllStringSubmatch(toBlock, -1)
		idx := 0
		for _, rMatch := range rows {
			cells := reHTMLTableCell.FindAllStringSubmatch(rMatch[1], -1)
			if len(cells) < 2 {
				continue
			}
			occName := cleanHTMLText(cells[0][1])
			baseTable := cleanHTMLText(cells[1][1])
			if occName == "" || strings.EqualFold(occName, "Name") || strings.EqualFold(occName, "Table Occurrence") || strings.EqualFold(baseTable, "Source") || strings.EqualFold(baseTable, "Source Table") {
				continue
			}
			tID := tableIDByName[strings.ToLower(baseTable)]
			tSQL := canonicalTableName[strings.ToLower(baseTable)]
			if tID == "" {
				tID = tableIDByName[sanitizeIdentifier(baseTable)]
				tSQL = canonicalTableName[sanitizeIdentifier(baseTable)]
			}
			if tID == "" {
				continue
			}
			if _, exists := occIDByName[strings.ToLower(occName)]; exists {
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
			idx++
		}
	}
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

	// 3. Parse Relationships from Details section
	relIDByOccurrence := make(map[string]string) // lower(occName) -> relID
	detailsBlock := sectionBetween(relSection, `name="valDetailsSectionAnchor_"`, `name="valLayoutsSectionAnchor_"`)
	if detailsBlock == "" {
		detailsBlock = relSection
	}
	relAnchorRe := regexp.MustCompile(`(?is)<a\s+name="[^"]*RelationshipAnchor_"\s*(?:/>|>\s*</a>)\s*(?:<em>)?([^<]+)(?:</em>)?\s*</span>\s*(?:</p>\s*)?<table[^>]*>(.*?)</table>`)
	for _, rm := range relAnchorRe.FindAllStringSubmatch(detailsBlock, -1) {
		relName := cleanHTMLText(rm[1])
		relTableHTML := rm[2]
		var leftOcc, leftField, rightOcc, rightField string
		allowCreate := false
		cascadeDelete := false

		for _, tr := range reHTMLTableRow.FindAllStringSubmatch(relTableHTML, -1) {
			cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
			if len(cells) >= 4 {
				rowTitle := strings.ToLower(cleanHTMLText(cells[0][1]))
				c1 := cleanHTMLText(cells[1][1])
				c3 := cleanHTMLText(cells[3][1])
				switch {
				case rowTitle == "table occurrence":
					leftOcc = c1
					rightOcc = c3
				case rowTitle == "field":
					leftField = c1
					rightField = c3
				case strings.Contains(rowTitle, "allow creation"):
					if strings.EqualFold(c1, "On") || strings.EqualFold(c1, "Yes") || strings.EqualFold(c3, "On") || strings.EqualFold(c3, "Yes") {
						allowCreate = true
					}
				case strings.Contains(rowTitle, "delete related"):
					if strings.EqualFold(c1, "On") || strings.EqualFold(c1, "Yes") || strings.EqualFold(c3, "On") || strings.EqualFold(c3, "Yes") {
						cascadeDelete = true
					}
				}
			} else if len(cells) >= 3 {
				c1 := cleanHTMLText(cells[1][1])
				c2 := cleanHTMLText(cells[2][1])
				if strings.Contains(c2, "::") && strings.Contains(c2, "=") {
					parts := strings.SplitN(c2, "=", 2)
					if len(parts) == 2 {
						leftOcc, leftField = splitQualifiedField(parts[0])
						rightOcc, rightField = splitQualifiedField(parts[1])
					}
				}
				if strings.EqualFold(c1, "Create") && strings.EqualFold(c2, "Yes") {
					allowCreate = true
				}
				if strings.EqualFold(c1, "Delete") && strings.EqualFold(c2, "Yes") {
					cascadeDelete = true
				}
			}
		}
		if leftOcc == "" || rightOcc == "" || leftField == "" || rightField == "" {
			continue
		}

		leftOccID := occIDByName[strings.ToLower(leftOcc)]
		rightOccID := occIDByName[strings.ToLower(rightOcc)]
		leftTbl := occTableByName[strings.ToLower(leftOcc)]
		rightTbl := occTableByName[strings.ToLower(rightOcc)]
		leftColID := colIDByTableCol[leftTbl+"."+sanitizeIdentifier(leftField)]
		rightColID := colIDByTableCol[rightTbl+"."+sanitizeIdentifier(rightField)]

		if leftOccID == "" || rightOccID == "" || leftColID == "" || rightColID == "" {
			addReportItem(report, ReportItem{
				Category:   CategoryRejected,
				Domain:     "relationship",
				SourceName: relName,
				Detail:     fmt.Sprintf("Unresolvable join endpoints (%s::%s = %s::%s)", leftOcc, leftField, rightOcc, rightField),
			})
			continue
		}
		relID := uuid.NewString()
		lLow := strings.ToLower(leftOcc)
		rLow := strings.ToLower(rightOcc)
		relIDByOccurrence[lLow+"|"+rLow] = relID
		relIDByOccurrence[rLow+"|"+lLow] = relID
		if _, exists := relIDByOccurrence[lLow]; !exists {
			relIDByOccurrence[lLow] = relID
		}
		if _, exists := relIDByOccurrence[rLow]; !exists {
			relIDByOccurrence[rLow] = relID
		}
		bundle.Relationships = append(bundle.Relationships, schema.BundleRelationship{
			ID:                relID,
			Name:              relName,
			LeftOccurrenceID:  leftOccID,
			LeftColumnID:      leftColID,
			RightOccurrenceID: rightOccID,
			RightColumnID:     rightColID,
			Operator:          "=",
			AllowCreation:     allowCreate,
			CascadeDelete:     cascadeDelete,
		})
		addReportItem(report, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "relationship",
			SourceName: relName,
			TargetName: relName,
			Detail:     fmt.Sprintf("%s.%s = %s.%s", leftOcc, leftField, rightOcc, rightField),
		})
	}
	report.Counts.Relationships = len(bundle.Relationships)

	// 4. Parse Scripts before Layouts so layout buttons can resolve script IDs
	scriptIDByName := make(map[string]string)
	scriptsSection := sectionBetween(rawHTML, `name="valScriptsSectionAnchor_"`, `name="valAccountsSectionAnchor_"`)
	if scriptsSection == "" {
		scriptsSection = sectionBetween(rawHTML, `name="valScriptsSectionAnchor_"`, `</html>`)
	}
	defaultCtxTable := ""
	if len(bundle.Tables) > 0 {
		defaultCtxTable = bundle.Tables[0].Name
	}
	parsedScripts := parseHTMLScriptsSection(scriptsSection)
	for _, ps := range parsedScripts {
		var steps []schema.BundleStep
		stepIdx := 0
		for _, lineText := range ps.StepLines {
			stepName, stepParams := parseHTMLScriptStepLine(lineText)
			if stepName == "" {
				continue
			}
			lowerStep := strings.ToLower(stepName)
			if canonical, ok := supportedScriptSteps[lowerStep]; ok {
				steps = append(steps, schema.BundleStep{
					ID:          uuid.NewString(),
					SequenceIdx: stepIdx,
					StepType:    canonical,
					Params:      stepParams,
					IsEnabled:   true,
				})
				stepIdx++
			} else if canonical, ok := catalogOnlyScriptSteps[lowerStep]; ok {
				steps = append(steps, schema.BundleStep{
					ID:          uuid.NewString(),
					SequenceIdx: stepIdx,
					StepType:    canonical,
					Params:      stepParams,
					IsEnabled:   true,
				})
				addReportItem(report, ReportItem{
					Category:   CategoryTranslated,
					Domain:     "script",
					SourceName: fmt.Sprintf("%s > %s (step %d)", ps.Name, stepName, stepIdx+1),
					TargetName: ps.Name,
					Detail:     fmt.Sprintf("Step %q imported into Script Workspace catalog", canonical),
				})
				stepIdx++
			}
		}
		scID := uuid.NewString()
		scriptIDByName[strings.ToLower(ps.Name)] = scID
		bundle.Scripts = append(bundle.Scripts, schema.BundleScript{
			ID:           scID,
			Name:         ps.Name,
			ContextTable: defaultCtxTable,
			IsActive:     true,
			Steps:        steps,
		})
		addReportItem(report, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "script",
			SourceName: ps.Name,
			TargetName: ps.Name,
			Detail:     fmt.Sprintf("Script migrated with %d steps", len(steps)),
		})
	}
	report.Counts.Scripts = len(bundle.Scripts)

	// 5. Parse Layouts
	layoutsSection := sectionBetween(rawHTML, `name="valLayoutsSectionAnchor_"`, `name="valScriptsSectionAnchor_"`)
	if layoutsSection == "" {
		layoutsSection = rawHTML
	}
	parsedLayouts := parseHTMLLayouts(layoutsSection)
	for _, pl := range parsedLayouts {
		occID := occIDByName[strings.ToLower(pl.Occurrence)]
		occName := pl.Occurrence
		if occID == "" && len(bundle.TableOccurrences) > 0 {
			occID = bundle.TableOccurrences[0].ID
			occName = bundle.TableOccurrences[0].Name
		}
		layID := uuid.NewString()
		def := buildFile4BaseLayoutDef(layID, pl, occName, relIDByOccurrence, scriptIDByName)
		bundle.Layouts = append(bundle.Layouts, schema.BundleLayout{
			ID:                layID,
			Name:              pl.Name,
			TableOccurrenceID: occID,
			Definition:        def,
		})
		addReportItem(report, ReportItem{
			Category:   CategoryPreserved,
			Domain:     "layout",
			SourceName: pl.Name,
			TargetName: pl.Name,
			Detail:     fmt.Sprintf("Layout bound to table occurrence %q (%d objects)", occName, len(def["objects"].([]map[string]interface{}))),
		})
	}
	report.Counts.Layouts = len(bundle.Layouts)

	// 6. Parse Accounts
	accountsSection := sectionBetween(rawHTML, `name="valAccountsSectionAnchor_"`, `name="valPrivilegeSetsSectionAnchor_"`)
	if accountsSection != "" {
		for _, tr := range reHTMLTableRow.FindAllStringSubmatch(accountsSection, -1) {
			if !strings.Contains(tr[1], "AccountAnchor_") {
				continue
			}
			cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
			if len(cells) < 5 {
				continue
			}
			accName := cleanHTMLText(cells[0][1])
			privSet := cleanHTMLText(cells[4][1])
			node := &xmlNode{
				Name: "Account",
				Attrs: map[string]string{
					"name":         accName,
					"privilegeSet": privSet,
				},
			}
			acc, items := convertAccountNode(node)
			for _, it := range items {
				addReportItem(report, it)
			}
			if acc != nil {
				bundle.Users = append(bundle.Users, *acc)
			}
		}
	}
	report.Counts.Accounts = len(bundle.Users)

	// 7. Parse optional FMPXMLRESULT data files and/or binary .fmp12 records
	tableRows := make(map[string][]map[string]interface{})
	if err := populateDataRows(req, canonicalTableName, tableIDByName, colTypeByTableCol, fmpTableMeta, fmpFieldMeta, tableRows, report); err != nil {
		return nil, err
	}

	return &ConvertedSolution{
		Bundle:    bundle,
		TableRows: tableRows,
		Report:    report,
	}, nil
}

func extractRawFieldOptions(cellHTML string) string {
	if m := reCalcListItems.FindStringSubmatch(cellHTML); len(m) > 1 {
		return "= " + cleanHTMLText(m[1])
	}
	return cleanHTMLText(cellHTML)
}

type htmlParsedScript struct {
	Name      string
	StepLines []string
}

func parseHTMLScriptsSection(scriptsSection string) []htmlParsedScript {
	var out []htmlParsedScript
	// Format 1: <tr class="privsetfirst"><td ...>Script Name</td><td ...>Open URL...</td></tr> ... <td>Script Steps</td><td><ul><li>...</li></ul></td>
	tableRe := regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	for _, tm := range tableRe.FindAllStringSubmatch(scriptsSection, -1) {
		tblHTML := tm[1]
		var scName string
		var steps []string
		for _, tr := range reHTMLTableRow.FindAllStringSubmatch(tblHTML, -1) {
			cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
			if len(cells) >= 2 {
				k := cleanHTMLText(cells[0][1])
				if strings.EqualFold(k, "Script Name") {
					scName = cleanHTMLText(cells[1][1])
				} else if strings.EqualFold(k, "Script Steps") {
					for _, lm := range reListItem.FindAllStringSubmatch(cells[1][1], -1) {
						if line := cleanHTMLText(lm[1]); line != "" {
							steps = append(steps, line)
						}
					}
				}
			}
		}
		if scName != "" {
			out = append(out, htmlParsedScript{Name: scName, StepLines: steps})
		}
	}
	return out
}

func convertHTMLField(
	tableSQL, rawTableName, rawFieldName, colName, rawType, rawOptions string,
	knownSameTableCols map[string]string,
) (schema.BundleColumn, []ReportItem) {
	var items []ReportItem
	col := schema.BundleColumn{
		ID:          uuid.NewString(),
		Name:        colName,
		DisplayName: rawFieldName,
		FieldType:   "TEXT",
		IsNullable:  true,
	}

	upperType := strings.ToUpper(rawType)
	if strings.Contains(upperType, "CALCULATED") || strings.Contains(upperType, "CALCULATION") {
		resType := "Text"
		if strings.Contains(upperType, "NUMBER") {
			resType = "Number"
		} else if strings.Contains(upperType, "DATE") {
			resType = "Date"
		} else if strings.Contains(upperType, "TIMESTAMP") || strings.Contains(upperType, "TIME") {
			resType = "Timestamp"
		}
		formula := extractHTMLFormula(rawOptions, tableSQL, rawTableName, knownSameTableCols)
		expr, translatedNote, ok := translateAndValidateFormula(formula)
		if ok {
			payload, _ := json.Marshal(map[string]interface{}{
				"formula":     expr,
				"result_type": resType,
			})
			s := string(payload)
			col.FieldType = "CALCULATION"
			col.CalculationFormula = &s
			detail := fmt.Sprintf("Calculation field (%s): %s", resType, expr)
			cat := CategoryPreserved
			if translatedNote != "" {
				detail = translatedNote
				cat = CategoryTranslated
			}
			items = append(items, ReportItem{
				Category:   cat,
				Domain:     "field",
				SourceName: tableSQL + "." + rawFieldName,
				TargetName: tableSQL + "." + colName,
				Detail:     detail,
			})
			return col, items
		}
		col.FieldType = mapBasicFieldType(resType)
		items = append(items,
			ReportItem{
				Category:   CategoryTranslated,
				Domain:     "field",
				SourceName: tableSQL + "." + rawFieldName,
				TargetName: tableSQL + "." + colName,
				Detail:     fmt.Sprintf("Cross-table calculation %q converted to stored %s column (pre-computed during record import)", formula, col.FieldType),
			},
		)
		return col, items
	}

	switch {
	case strings.Contains(upperType, "BINARY") || strings.Contains(upperType, "CONTAINER"):
		col.FieldType = "CONTAINER"
	case strings.Contains(upperType, "NUMBER"):
		col.FieldType = "NUMBER"
	case strings.Contains(upperType, "TIMESTAMP") || strings.Contains(upperType, "TIME"):
		col.FieldType = "TIMESTAMP"
	case strings.Contains(upperType, "DATE"):
		col.FieldType = "DATE"
	case strings.Contains(upperType, "BOOLEAN"):
		col.FieldType = "BOOLEAN"
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

func extractHTMLFormula(rawOptions, tableSQL, rawTableName string, knownSameTableCols map[string]string) string {
	formula := strings.TrimSpace(rawOptions)
	if strings.HasPrefix(formula, "=") {
		formula = strings.TrimSpace(formula[1:])
	} else if idx := strings.Index(formula, "= "); idx >= 0 && idx < 24 {
		formula = strings.TrimSpace(formula[idx+2:])
	}
	if idx := strings.Index(formula, "(Storage Options:"); idx >= 0 {
		formula = strings.TrimSpace(formula[:idx])
	}
	if idx := strings.Index(formula, "Storage Options:"); idx >= 0 {
		formula = strings.TrimSpace(formula[:idx])
	}
	formula = strings.ReplaceAll(formula, "¶", "\n")

	type colPair struct {
		raw string
		sql string
	}
	var pairs []colPair
	for raw, sql := range knownSameTableCols {
		pairs = append(pairs, colPair{raw: raw, sql: sql})
	}
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if len(pairs[j].raw) > len(pairs[i].raw) {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	for _, p := range pairs {
		for _, prefix := range []string{rawTableName, tableSQL} {
			pat := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(prefix) + `\s*::\s*` + regexp.QuoteMeta(p.raw) + `\b`)
			formula = pat.ReplaceAllString(formula, p.sql)
		}
	}
	return strings.TrimSpace(formula)
}

func parseHTMLScriptStepLine(line string) (string, map[string]interface{}) {
	params := make(map[string]interface{})
	trimmed := strings.TrimSpace(line)
	trimmed = strings.ReplaceAll(trimmed, "“", `"`)
	trimmed = strings.ReplaceAll(trimmed, "”", `"`)
	if idx := strings.Index(trimmed, "["); idx > 0 && strings.HasSuffix(trimmed, "]") {
		stepName := strings.TrimSpace(trimmed[:idx])
		inner := strings.TrimSpace(trimmed[idx+1 : len(trimmed)-1])
		params["expression"] = inner
		switch {
		case strings.EqualFold(stepName, "Go to Layout"):
			if q1 := strings.Index(inner, `"`); q1 >= 0 {
				if q2 := strings.Index(inner[q1+1:], `"`); q2 >= 0 {
					params["layout_name"] = inner[q1+1 : q1+1+q2]
				}
			} else if inner != "" {
				params["layout_name"] = strings.TrimSpace(inner)
			}
		case strings.EqualFold(stepName, "If") || strings.EqualFold(stepName, "Else If"):
			params["condition"] = inner
		case strings.EqualFold(stepName, "Set Variable"):
			parts := strings.SplitN(inner, ";", 2)
			if len(parts) >= 1 {
				params["name"] = strings.TrimSpace(parts[0])
			}
			if len(parts) == 2 {
				valPart := strings.TrimSpace(parts[1])
				if strings.HasPrefix(strings.ToLower(valPart), "value:") {
					valPart = strings.TrimSpace(valPart[len("value:"):])
				}
				params["value"] = valPart
			}
		}
		return stepName, params
	}
	return trimmed, params
}

func parseHTMLLayouts(layoutsSection string) []htmlParsedLayout {
	// 1. Parse the Layout Overview table first: <<td><a name="9LayoutAnchor_"/>View Recipe</td>...<td>recipes</td>
	layoutByID := make(map[string]*htmlParsedLayout)
	var layoutOrder []string

	overviewEnd := strings.Index(layoutsSection, "Layout Objects:")
	overviewHTML := layoutsSection
	if overviewEnd > 0 {
		overviewHTML = layoutsSection[:overviewEnd]
	}
	layAnchorRe := regexp.MustCompile(`(?is)<a\s+name="(\d+)LayoutAnchor_"\s*(?:/>|>\s*</a>)\s*([^<]+)`)
	for _, tr := range reHTMLTableRow.FindAllStringSubmatch(overviewHTML, -1) {
		m := layAnchorRe.FindStringSubmatch(tr[1])
		if len(m) < 3 {
			continue
		}
		layID := m[1]
		layName := cleanHTMLText(m[2])
		if layName == "" || strings.HasSuffix(m[0], "_toc") {
			continue
		}
		cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
		occ := ""
		var w, h float64 = 768, 952
		if len(cells) >= 5 {
			occ = cleanHTMLText(cells[4][1])
		} else if len(cells) >= 3 {
			occ = cleanHTMLText(cells[1][1])
			if bw, bh := parseWidthHeight(cleanHTMLText(cells[2][1])); bw > 0 && bh > 0 {
				w, h = bw, bh
			}
		}
		layoutByID[layID] = &htmlParsedLayout{
			Name:       layName,
			Occurrence: occ,
			Width:      w,
			Height:     h,
		}
		layoutOrder = append(layoutOrder, layID)
	}

	// 2. If Layout Objects sections exist (`Layout Objects: <a href="#9LayoutAnchor_">View Recipe</a>`), parse each
	objBlocks := strings.Split(layoutsSection, `Layout Objects:`)
	if len(objBlocks) > 1 {
		hrefRe := regexp.MustCompile(`(?is)href="#(\d+)LayoutAnchor_"`)
		for _, block := range objBlocks[1:] {
			hm := hrefRe.FindStringSubmatch(block[:minInt(len(block), 200)])
			if len(hm) < 2 {
				continue
			}
			layID := hm[1]
			pl := layoutByID[layID]
			if pl == nil {
				continue
			}
			parseLayoutObjectBlock(block, pl)
		}
	} else {
		// Compact format fallback
		parts := strings.Split(layoutsSection, `<span class="subheader"><a name="`)
		for _, part := range parts[1:] {
			m := layAnchorRe.FindStringSubmatch(`<a name="` + part)
			if len(m) < 3 {
				continue
			}
			layID := m[1]
			layName := cleanHTMLText(m[2])
			pl := layoutByID[layID]
			if pl == nil {
				pl = &htmlParsedLayout{
					Name:   layName,
					Width:  768,
					Height: 952,
				}
				firstTableEnd := strings.Index(part, "</table>")
				if firstTableEnd > 0 {
					for _, tr := range reHTMLTableRow.FindAllStringSubmatch(part[:firstTableEnd], -1) {
						cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
						if len(cells) >= 3 {
							occ := cleanHTMLText(cells[1][1])
							if occ != "" && !strings.EqualFold(occ, "Show Records From") {
								pl.Occurrence = occ
							}
						}
					}
				}
				layoutByID[layID] = pl
				layoutOrder = append(layoutOrder, layID)
			}
			parseCompactLayoutBlock(part, pl)
		}
	}

	var out []htmlParsedLayout
	for _, id := range layoutOrder {
		if pl := layoutByID[id]; pl != nil {
			out = append(out, *pl)
		}
	}
	return out
}

func parseLayoutObjectBlock(block string, pl *htmlParsedLayout) {
	// Split block by `<a name="\d+LayoutObjectsAnchor_`
	secRe := regexp.MustCompile(`(?is)<a\s+name="\d+LayoutObjectsAnchor_([^"]+)"\s*(?:/>|>\s*</a>)`)
	matches := secRe.FindAllStringSubmatchIndex(block, -1)
	sections := make(map[string]string)
	for i, loc := range matches {
		secType := block[loc[2]:loc[3]]
		start := loc[1]
		end := len(block)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		sections[secType] = block[start:end]
	}

	// 1. Portals first so we can exclude portal child fields from top-level Regular Fields
	if pSec, ok := sections["Portals"]; ok {
		tableRe := regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
		for _, tm := range tableRe.FindAllStringSubmatch(pSec, -1) {
			tblHTML := tm[1]
			top, left, bottom, right := extractCoords(tblHTML)
			if bottom <= top && right <= left {
				continue
			}
			occ := ""
			if m := regexp.MustCompile(`(?is)<b>Table:</b>\s*<li>(.*?)</li>`).FindStringSubmatch(tblHTML); len(m) > 1 {
				occ = cleanHTMLText(m[1])
			}
			rowsCnt := 6
			if m := reNumRows.FindStringSubmatch(tblHTML); len(m) > 1 {
				rowsCnt, _ = strconv.Atoi(m[1])
			}
			var children []htmlLayoutField
			for _, cm := range rePortalAtCoords.FindAllStringSubmatch(tblHTML, -1) {
				qName := cleanHTMLText(cm[1])
				cOcc, cFld := splitQualifiedField(qName)
				ct, _ := strconv.ParseFloat(cm[2], 64)
				cl, _ := strconv.ParseFloat(cm[3], 64)
				cb, _ := strconv.ParseFloat(cm[4], 64)
				cr, _ := strconv.ParseFloat(cm[5], 64)
				children = append(children, htmlLayoutField{
					Occurrence:   cOcc,
					FieldName:    cFld,
					Top:          ct,
					Left:         cl,
					Bottom:       cb,
					Right:        cr,
					ControlStyle: "edit_box",
				})
			}
			pl.Portals = append(pl.Portals, htmlPortalDef{
				Top:         top,
				Left:        left,
				Bottom:      bottom,
				Right:       right,
				Occurrence:  occ,
				RowCount:    rowsCnt,
				ChildFields: children,
			})
		}
	}

	portalChildKeys := make(map[string]bool)
	portalOccs := make(map[string]bool)
	for _, p := range pl.Portals {
		portalOccs[strings.ToLower(p.Occurrence)] = true
		for _, cf := range p.ChildFields {
			key := fmt.Sprintf("%s::%s|%.0f|%.0f|%.0f|%.0f", strings.ToLower(cf.Occurrence), strings.ToLower(cf.FieldName), cf.Top, cf.Left, cf.Bottom, cf.Right)
			portalChildKeys[key] = true
		}
	}

	// 2. Group Buttons & Buttons (parsed before Regular/Merge Fields so we can detect wrapped child objects at 0,0)
	groupBtnField := make(map[string]htmlLayoutButton) // lower("occ::field") -> btn
	groupBtnMerge := make(map[string]bool)             // merge text -> true
	reGroupField := regexp.MustCompile(`(?is)<li>\s*Field:\s*(.*?)\s*</li>`)
	reGroupTypeLi := regexp.MustCompile(`(?is)<b>Type:</b>\s*<li>\s*(.*?)\s*</li>`)
	reGroupText := regexp.MustCompile(`(?is)<li>\s*Text:\s*(.*?)\s*</li>`)

	for _, key := range []string{"Group Buttons", "Buttons"} {
		if bSec, ok := sections[key]; ok {
			for _, tr := range reHTMLTableRow.FindAllStringSubmatch(bSec, -1) {
				if !strings.Contains(tr[1], "Top:") {
					continue
				}
				cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
				if len(cells) < 3 {
					continue
				}
				top, left, bottom, right := extractCoords(cells[1][1])
				stepText := cleanHTMLText(cells[2][1])
				stepText = strings.ReplaceAll(stepText, "“", `"`)
				stepText = strings.ReplaceAll(stepText, "”", `"`)
				btn := htmlLayoutButton{
					Top:        top,
					Left:       left,
					Bottom:     bottom,
					Right:      right,
					ActionName: stepText,
				}
				if strings.HasPrefix(strings.ToLower(stepText), "perform script") {
					if q1 := strings.Index(stepText, `"`); q1 >= 0 {
						if q2 := strings.Index(stepText[q1+1:], `"`); q2 >= 0 {
							btn.ScriptName = stepText[q1+1 : q1+1+q2]
						}
					}
				} else if strings.HasPrefix(strings.ToLower(stepText), "go to layout") {
					if q1 := strings.Index(stepText, `"`); q1 >= 0 {
						if q2 := strings.Index(stepText[q1+1:], `"`); q2 >= 0 {
							btn.LayoutName = stepText[q1+1 : q1+1+q2]
						}
					} else if idx := strings.Index(stepText, "["); idx >= 0 && strings.HasSuffix(stepText, "]") {
						btn.LayoutName = strings.TrimSpace(stepText[idx+1 : len(stepText)-1])
					}
				}

				explicitTextLabel := ""
				if tm := reGroupText.FindStringSubmatch(cells[0][1]); len(tm) > 1 {
					explicitTextLabel = strings.TrimSpace(cleanHTMLText(tm[1]))
				}

				if key == "Group Buttons" {
					if fm := reGroupField.FindStringSubmatch(cells[0][1]); len(fm) > 1 {
						qFld := strings.ToLower(cleanHTMLText(fm[1]))
						groupBtnField[qFld] = btn
						continue
					}
					if tm := reGroupText.FindStringSubmatch(cells[0][1]); len(tm) > 1 {
						rawInner := cleanHTMLMergeText(tm[1])
						converted := convertMergeString(rawInner)
						groupBtnMerge[converted] = true
						btn.Label = converted
						btn.IsTextGroup = true
						pl.Buttons = append(pl.Buttons, btn)
						continue
					}
					if lm := reGroupTypeLi.FindStringSubmatch(cells[0][1]); len(lm) > 1 {
						cand := strings.TrimSpace(cleanHTMLText(lm[1]))
						if strings.Contains(cand, "::") && !strings.HasPrefix(strings.ToLower(cand), "text:") {
							groupBtnField[strings.ToLower(cand)] = btn
							continue
						}
					}
				}

				bw := right - left
				bh := bottom - top
				switch {
				case explicitTextLabel != "":
					btn.Label = explicitTextLabel
				case bw <= 40 && bh <= 40 && strings.Contains(strings.ToLower(btn.ScriptName), "url"):
					btn.Label = "🌐"
				case btn.LayoutName != "" && !strings.HasPrefix(btn.LayoutName, "$"):
					btn.Label = btn.LayoutName
				case btn.ScriptName != "":
					btn.Label = btn.ScriptName
				case strings.Contains(strings.ToLower(stepText), "delete portal row"):
					btn.Label = "×"
				default:
					btn.Label = stepText
				}
				pl.Buttons = append(pl.Buttons, btn)
			}
		}
	}

	// 3. Regular Fields
	if rSec, ok := sections["Regular Fields"]; ok {
		tableRe := regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
		fNameRe := regexp.MustCompile(`(?is)Field Name:\s*(.*?)</td>`)
		for _, tm := range tableRe.FindAllStringSubmatch(rSec, -1) {
			tblHTML := tm[1]
			fm := fNameRe.FindStringSubmatch(tblHTML)
			if len(fm) < 2 {
				continue
			}
			qName := cleanHTMLText(fm[1])
			occ, fld := splitQualifiedField(qName)
			top, left, bottom, right := extractCoords(tblHTML)
			key := fmt.Sprintf("%s::%s|%.0f|%.0f|%.0f|%.0f", strings.ToLower(occ), strings.ToLower(fld), top, left, bottom, right)
			if portalChildKeys[key] || (top < 40 && portalOccs[strings.ToLower(occ)]) {
				continue
			}
			var actionBtn *htmlLayoutButton
			qKey := strings.ToLower(occ + "::" + fld)
			if gb, hasGB := groupBtnField[qKey]; hasGB && top == 0 && left == 0 {
				top, left, bottom, right = gb.Top, gb.Left, gb.Bottom, gb.Right
				bCopy := gb
				actionBtn = &bCopy
			}
			style := "edit_box"
			lowerTbl := strings.ToLower(tblHTML)
			switch {
			case strings.Contains(lowerTbl, "drop-down list"):
				style = "drop_down_list"
			case strings.Contains(lowerTbl, "pop-up menu"):
				style = "pop_up_menu"
			case strings.Contains(lowerTbl, "checkbox"):
				style = "checkbox_set"
			case strings.Contains(lowerTbl, "radio"):
				style = "radio_button_set"
			}
			pl.Fields = append(pl.Fields, htmlLayoutField{
				Occurrence:   occ,
				FieldName:    fld,
				Top:          top,
				Left:         left,
				Bottom:       bottom,
				Right:        right,
				ControlStyle: style,
				ActionButton: actionBtn,
			})
		}
	}

	// 4. Merge Fields
	if mSec, ok := sections["Merge Fields"]; ok {
		textRe := regexp.MustCompile(`(?is)<b>Text:</b>\s*<li>(.*?)</li>`)
		for _, tr := range reHTMLTableRow.FindAllStringSubmatch(mSec, -1) {
			tm := textRe.FindStringSubmatch(tr[1])
			if len(tm) < 2 {
				continue
			}
			top, left, bottom, right := extractCoords(tr[1])
			rawTxt := cleanHTMLMergeText(tm[1])
			converted := convertMergeString(rawTxt)
			if top == 0 && left == 0 && groupBtnMerge[converted] {
				// Skip duplicate merge text that is already rendered by its parent Group Button
				continue
			}
			pl.Merges = append(pl.Merges, htmlLayoutMerge{
				Text:   converted,
				Top:    top,
				Left:   left,
				Bottom: bottom,
				Right:  right,
			})
		}
	}

	// 5. Web Viewer Controls
	if wSec, ok := sections["Web Viewer Controls"]; ok {
		objNameRe := regexp.MustCompile(`(?is)<b>Object Name:</b>\s*<li>(.*?)</li>`)
		for _, tr := range reHTMLTableRow.FindAllStringSubmatch(wSec, -1) {
			if !strings.Contains(tr[1], "Top:") {
				continue
			}
			cells := reHTMLTableCell.FindAllStringSubmatch(tr[1], -1)
			if len(cells) < 3 {
				continue
			}
			name := ""
			if nm := objNameRe.FindStringSubmatch(cells[0][1]); len(nm) > 1 {
				name = cleanHTMLText(nm[1])
			}
			top, left, bottom, right := extractCoords(cells[1][1])
			urlExpr := cleanHTMLText(cells[2][1])
			pl.WebViewers = append(pl.WebViewers, htmlLayoutWebViewer{
				Name:   name,
				URL:    urlExpr,
				Top:    top,
				Left:   left,
				Bottom: bottom,
				Right:  right,
			})
		}
	}
}

func parseCompactLayoutBlock(part string, pl *htmlParsedLayout) {
	portalKey := "Portals (Layout Object"
	if pIdx := strings.Index(part, portalKey); pIdx >= 0 {
		pSection := part[pIdx:]
		if nextObj := strings.Index(pSection[len(portalKey):], "(Layout Object"); nextObj >= 0 {
			pSection = pSection[:len(portalKey)+nextObj]
		}
		pl.Portals = parseHTMLPortalsSection(pSection)
	}
	portalChildKeys := make(map[string]bool)
	for _, p := range pl.Portals {
		for _, cf := range p.ChildFields {
			key := fmt.Sprintf("%s::%s|%.0f|%.0f|%.0f|%.0f", strings.ToLower(cf.Occurrence), strings.ToLower(cf.FieldName), cf.Top, cf.Left, cf.Bottom, cf.Right)
			portalChildKeys[key] = true
		}
	}
	regKey := "Regular Fields (Layout Object"
	if rIdx := strings.Index(part, regKey); rIdx >= 0 {
		rSection := part[rIdx:]
		if nextObj := strings.Index(rSection[len(regKey):], "(Layout Object"); nextObj >= 0 {
			rSection = rSection[:len(regKey)+nextObj]
		}
		for _, f := range parseHTMLFieldsTable(rSection) {
			key := fmt.Sprintf("%s::%s|%.0f|%.0f|%.0f|%.0f", strings.ToLower(f.Occurrence), strings.ToLower(f.FieldName), f.Top, f.Left, f.Bottom, f.Right)
			if portalChildKeys[key] {
				continue
			}
			pl.Fields = append(pl.Fields, f)
		}
	}
}

func extractCoords(s string) (top, left, bottom, right float64) {
	if m := reTopCoord.FindStringSubmatch(s); len(m) > 1 {
		top, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reLeftCoord.FindStringSubmatch(s); len(m) > 1 {
		left, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reBottomCoord.FindStringSubmatch(s); len(m) > 1 {
		bottom, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reRightCoord.FindStringSubmatch(s); len(m) > 1 {
		right, _ = strconv.ParseFloat(m[1], 64)
	}
	return
}

func parseHTMLPortalsSection(section string) []htmlPortalDef {
	var portals []htmlPortalDef
	rows := reHTMLTableRow.FindAllStringSubmatch(section, -1)
	var current *htmlPortalDef
	for _, rMatch := range rows {
		cells := reHTMLTableCell.FindAllStringSubmatch(rMatch[1], -1)
		if len(cells) >= 7 {
			c0 := cleanHTMLText(cells[0][1])
			c4 := cleanHTMLText(cells[4][1])
			if strings.HasSuffix(c0, "pt") && c4 != "" && !strings.EqualFold(c0, "Top") {
				top := parsePt(c0)
				left := parsePt(cleanHTMLText(cells[1][1]))
				bottom := parsePt(cleanHTMLText(cells[2][1]))
				right := parsePt(cleanHTMLText(cells[3][1]))
				rowsCnt, _ := strconv.Atoi(cleanHTMLText(cells[5][1]))
				if rowsCnt <= 0 {
					rowsCnt = 5
				}
				portals = append(portals, htmlPortalDef{
					Top:        top,
					Left:       left,
					Bottom:     bottom,
					Right:      right,
					Occurrence: c4,
					RowCount:   rowsCnt,
				})
				current = &portals[len(portals)-1]
				continue
			}
		}
		if current != nil && len(cells) >= 5 {
			c0 := cleanHTMLText(cells[0][1])
			c1 := cleanHTMLText(cells[1][1])
			if strings.Contains(c0, "::") && strings.HasSuffix(c1, "pt") {
				occ, fld := splitQualifiedField(c0)
				current.ChildFields = append(current.ChildFields, htmlLayoutField{
					Occurrence: occ,
					FieldName:  fld,
					Top:        parsePt(c1),
					Left:       parsePt(cleanHTMLText(cells[2][1])),
					Bottom:     parsePt(cleanHTMLText(cells[3][1])),
					Right:      parsePt(cleanHTMLText(cells[4][1])),
				})
			}
		}
	}
	return portals
}

func parseHTMLFieldsTable(section string) []htmlLayoutField {
	var fields []htmlLayoutField
	for _, rMatch := range reHTMLTableRow.FindAllStringSubmatch(section, -1) {
		cells := reHTMLTableCell.FindAllStringSubmatch(rMatch[1], -1)
		if len(cells) < 5 {
			continue
		}
		c0 := cleanHTMLText(cells[0][1])
		c1 := cleanHTMLText(cells[1][1])
		if !strings.Contains(c0, "::") || !strings.HasSuffix(c1, "pt") {
			continue
		}
		occ, fld := splitQualifiedField(c0)
		style := "edit_box"
		if len(cells) >= 6 {
			rawStyle := strings.ToLower(cleanHTMLText(cells[5][1]))
			switch {
			case strings.Contains(rawStyle, "drop-down list"):
				style = "drop_down_list"
			case strings.Contains(rawStyle, "pop-up menu"):
				style = "pop_up_menu"
			case strings.Contains(rawStyle, "checkbox"):
				style = "checkbox_set"
			case strings.Contains(rawStyle, "radio"):
				style = "radio_button_set"
			}
		}
		fields = append(fields, htmlLayoutField{
			Occurrence:   occ,
			FieldName:    fld,
			Top:          parsePt(c1),
			Left:         parsePt(cleanHTMLText(cells[2][1])),
			Bottom:       parsePt(cleanHTMLText(cells[3][1])),
			Right:        parsePt(cleanHTMLText(cells[4][1])),
			ControlStyle: style,
		})
	}
	return fields
}

func buildButtonActionMap(b htmlLayoutButton, scriptIDByName map[string]string) map[string]interface{} {
	if b.ScriptName != "" {
		return map[string]interface{}{
			"type":        "perform_script",
			"script_id":   scriptIDByName[strings.ToLower(b.ScriptName)],
			"script_name": b.ScriptName,
		}
	}
	if b.LayoutName != "" {
		return map[string]interface{}{
			"type":      "single_step",
			"step_type": "Go to Layout",
			"params": map[string]interface{}{
				"layout_name": b.LayoutName,
			},
		}
	}
	return map[string]interface{}{
		"type":      "single_step",
		"step_type": b.ActionName,
	}
}

func buildFile4BaseLayoutDef(
	layoutID string,
	pl htmlParsedLayout,
	occName string,
	relIDByOccurrence map[string]string,
	scriptIDByName map[string]string,
) map[string]interface{} {
	objects := make([]map[string]interface{}, 0)
	maxBottom := pl.Height
	lowerLayoutName := strings.ToLower(strings.TrimSpace(pl.Name))

	// Add header chrome / static labels for layouts that have static graphics/labels in FileMaker
	switch lowerLayoutName {
	case "edit recipe":
		objects = append(objects,
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "rectangle",
				"x":      0.0,
				"y":      0.0,
				"width":  pl.Width,
				"height": 50.0,
				"style": map[string]interface{}{
					"fill_color":   "#3C4043",
					"border_width": 0,
				},
			},
		)
		type staticLbl struct {
			text  string
			x, y  float64
			w, h  float64
			align string
		}
		var staticLabels []staticLbl
		if pl.Width <= 800 {
			// 768px layout coordinates (recipes.html)
			objects = append(objects,
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "rectangle",
					"x":      18.0,
					"y":      332.0,
					"width":  131.0,
					"height": 22.0,
					"style": map[string]interface{}{
						"fill_color":   "#DCDCDC",
						"border_width": 0,
					},
				},
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "label",
					"x":      18.0,
					"y":      334.0,
					"width":  131.0,
					"height": 18.0,
					"text":   "US/imperial",
					"style": map[string]interface{}{
						"font_size":   12,
						"font_weight": "bold",
						"text_align":  "center",
						"text_color":  "#555555",
					},
				},
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "rectangle",
					"x":      150.0,
					"y":      332.0,
					"width":  131.0,
					"height": 22.0,
					"style": map[string]interface{}{
						"fill_color":   "#DCDCDC",
						"border_width": 0,
					},
				},
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "label",
					"x":      150.0,
					"y":      334.0,
					"width":  131.0,
					"height": 18.0,
					"text":   "metric",
					"style": map[string]interface{}{
						"font_size":   12,
						"font_weight": "bold",
						"text_align":  "center",
						"text_color":  "#555555",
					},
				},
			)
			staticLabels = []staticLbl{
				{"Name", 18, 62, 120, 20, "left"},
				{"Description", 18, 130, 120, 20, "left"},
				{"Author", 18, 237, 120, 20, "left"},
				{"Yield", 239, 237, 100, 20, "left"},
				{"Photo", 413, 62, 100, 20, "left"},
				{"qty", 20, 355, 62, 20, "left"},
				{"unit", 86, 355, 62, 20, "left"},
				{"qty", 152, 355, 62, 20, "left"},
				{"unit", 218, 355, 62, 20, "left"},
				{"Ingredient", 284, 355, 180, 20, "left"},
				{"Grouping", 546, 355, 120, 20, "left"},
				{"Instructions", 21, 617, 140, 20, "left"},
				{"Tags", 556, 617, 100, 20, "left"},
				{"id", 18, 891, 30, 20, "right"},
				{"URL", 335, 889, 38, 20, "right"},
			}
		} else {
			// 1024px layout coordinates (recipes_DDR.html)
			objects = append(objects,
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "rectangle",
					"x":      362.0,
					"y":      224.0,
					"width":  188.0,
					"height": 22.0,
					"style": map[string]interface{}{
						"fill_color":   "#DCDCDC",
						"border_width": 0,
					},
				},
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "label",
					"x":      362.0,
					"y":      226.0,
					"width":  188.0,
					"height": 18.0,
					"text":   "US/imperial",
					"style": map[string]interface{}{
						"font_size":   12,
						"font_weight": "bold",
						"text_align":  "center",
						"text_color":  "#555555",
					},
				},
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "rectangle",
					"x":      558.0,
					"y":      224.0,
					"width":  188.0,
					"height": 22.0,
					"style": map[string]interface{}{
						"fill_color":   "#DCDCDC",
						"border_width": 0,
					},
				},
				map[string]interface{}{
					"id":     uuid.NewString(),
					"type":   "label",
					"x":      558.0,
					"y":      226.0,
					"width":  188.0,
					"height": 18.0,
					"text":   "metric",
					"style": map[string]interface{}{
						"font_size":   12,
						"font_weight": "bold",
						"text_align":  "center",
						"text_color":  "#555555",
					},
				},
			)
			staticLabels = []staticLbl{
				{"Name", 18, 68, 86, 22, "right"},
				{"Description", 18, 102, 86, 22, "right"},
				{"Author", 18, 188, 86, 22, "right"},
				{"Yield", 312, 188, 38, 22, "right"},
				{"Photo", 515, 68, 60, 22, "right"},
				{"Ingredient", 24, 246, 200, 20, "left"},
				{"qty", 369, 246, 74, 20, "left"},
				{"unit", 447, 246, 99, 20, "left"},
				{"qty", 564, 246, 74, 20, "left"},
				{"unit", 642, 246, 99, 20, "left"},
				{"Grouping", 762, 246, 120, 20, "left"},
				{"Instruction", 18, 462, 120, 20, "left"},
				{"Tags", 641, 462, 80, 20, "left"},
				{"id", 638, 629, 36, 22, "right"},
				{"URL", 630, 663, 44, 22, "right"},
			}
		}
		for _, sl := range staticLabels {
			objects = append(objects, map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "label",
				"x":      sl.x,
				"y":      sl.y,
				"width":  sl.w,
				"height": sl.h,
				"text":   sl.text,
				"style": map[string]interface{}{
					"font_size":   13,
					"font_weight": "bold",
					"text_align":  sl.align,
					"text_color":  "#333333",
				},
			})
		}
	case "recipe list":
		midColX := 293.0
		midColLabel := "Description"
		for _, f := range pl.Fields {
			if strings.EqualFold(f.FieldName, "tags") {
				midColX = f.Left
				midColLabel = "Tags"
				break
			}
		}
		objects = append(objects,
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "rectangle",
				"x":      0.0,
				"y":      0.0,
				"width":  pl.Width,
				"height": 48.0,
				"style": map[string]interface{}{
					"fill_color":   "#3C4043",
					"border_width": 0,
				},
			},
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "label",
				"x":      18.0,
				"y":      12.0,
				"width":  260.0,
				"height": 28.0,
				"text":   "Recipe List",
				"style": map[string]interface{}{
					"font_size":   20,
					"font_weight": "bold",
					"text_color":  "#FFFFFF",
				},
			},
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "rectangle",
				"x":      0.0,
				"y":      48.0,
				"width":  pl.Width,
				"height": 24.0,
				"style": map[string]interface{}{
					"fill_color":   "#E5E7EB",
					"border_width": 0,
				},
			},
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "label",
				"x":      18.0,
				"y":      52.0,
				"width":  150.0,
				"height": 18.0,
				"text":   "Name",
				"style": map[string]interface{}{
					"font_size":   12,
					"font_weight": "bold",
					"text_color":  "#333333",
				},
			},
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "label",
				"x":      midColX,
				"y":      52.0,
				"width":  150.0,
				"height": 18.0,
				"text":   midColLabel,
				"style": map[string]interface{}{
					"font_size":   12,
					"font_weight": "bold",
					"text_color":  "#333333",
				},
			},
			map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "label",
				"x":      688.0,
				"y":      52.0,
				"width":  60.0,
				"height": 18.0,
				"text":   "Photo",
				"style": map[string]interface{}{
					"font_size":   12,
					"font_weight": "bold",
					"text_color":  "#333333",
				},
			},
		)
	}

	hasDescMerge := false
	// 1. Merge field labels
	for _, m := range pl.Merges {
		if strings.Contains(m.Text, "{{description}}") {
			hasDescMerge = true
		}
		text := m.Text
		if text == "{{instructions}}\n{{yield_calc}}" {
			text = "{{instructions}}\n\n{{yield_calc}}"
		}
		w := m.Right - m.Left
		h := m.Bottom - m.Top
		if w <= 0 {
			w = 240
		}
		if h <= 0 {
			h = 24
		}
		if m.Bottom > maxBottom {
			maxBottom = m.Bottom + 32
		}
		style := map[string]interface{}{
			"font_size":  13,
			"text_color": "#222222",
		}
		switch {
		case strings.Contains(text, "{{layout_name}}") || strings.Contains(text, "{{LayoutName}}"):
			style["font_size"] = 20
			style["font_weight"] = "bold"
			style["text_color"] = "#FFFFFF"
		case strings.HasPrefix(text, "Web view:"):
			style["font_size"] = 22
			style["font_weight"] = "bold"
			style["text_color"] = "#222222"
		case text == "{{name}}" && h >= 32:
			style["font_size"] = 26
			style["font_weight"] = "bold"
			style["text_color"] = "#111111"
		case strings.HasPrefix(text, "Author:"):
			style["font_size"] = 12
			style["text_color"] = "#333333"
		case text == "{{description}}":
			style["font_size"] = 14
			style["text_color"] = "#222222"
		case strings.Contains(text, "{{ingredients_formatted}}") || strings.Contains(text, "{{instructions}}"):
			style["font_size"] = 14
			style["text_color"] = "#222222"
		}
		objects = append(objects, map[string]interface{}{
			"id":     uuid.NewString(),
			"type":   "label",
			"x":      m.Left,
			"y":      m.Top,
			"width":  w,
			"height": h,
			"text":   text,
			"style":  style,
		})
	}

	if lowerLayoutName == "view recipe" && !hasDescMerge {
		objects = append(objects, map[string]interface{}{
			"id":     uuid.NewString(),
			"type":   "label",
			"x":      503.0,
			"y":      97.0,
			"width":  245.0,
			"height": 311.0,
			"text":   "{{description}}",
			"style": map[string]interface{}{
				"font_size":  14,
				"text_color": "#222222",
			},
		})
	}

	// 2. Regular fields
	for _, f := range pl.Fields {
		w := f.Right - f.Left
		h := f.Bottom - f.Top
		if lowerLayoutName == "view recipe" && strings.EqualFold(f.FieldName, "Photo") && w > 500 {
			w = 463
		}
		if w <= 0 {
			w = 240
		}
		if h <= 0 {
			h = 28
		}
		if f.Bottom > maxBottom {
			maxBottom = f.Bottom + 32
		}
		colName := sanitizeIdentifier(f.FieldName)
		binding := map[string]interface{}{
			"field_name":    colName,
			"control_style": f.ControlStyle,
		}
		if f.Occurrence != "" && !strings.EqualFold(f.Occurrence, occName) {
			binding["table_occurrence"] = f.Occurrence
			relID := relIDByOccurrence[strings.ToLower(occName)+"|"+strings.ToLower(f.Occurrence)]
			if relID == "" {
				relID = relIDByOccurrence[strings.ToLower(f.Occurrence)]
			}
			if relID != "" {
				binding["relationship_id"] = relID
			}
		}
		obj := map[string]interface{}{
			"id":            uuid.NewString(),
			"type":          "field",
			"name":          f.FieldName,
			"x":             f.Left,
			"y":             f.Top,
			"width":         w,
			"height":        h,
			"field_binding": binding,
			"style": map[string]interface{}{
				"font_size": 13,
			},
		}
		if f.ActionButton != nil {
			obj["action"] = buildButtonActionMap(*f.ActionButton, scriptIDByName)
		}
		objects = append(objects, obj)
	}

	// 3. Portals and their child fields (offset into portal coordinates so portal.contains(child) is true)
	for _, p := range pl.Portals {
		pw := p.Right - p.Left
		ph := p.Bottom - p.Top
		if pw <= 0 {
			pw = 480
		}
		if ph <= 0 {
			ph = 180
		}
		if p.Bottom > maxBottom {
			maxBottom = p.Bottom + 32
		}
		relID := relIDByOccurrence[strings.ToLower(occName)+"|"+strings.ToLower(p.Occurrence)]
		if relID == "" {
			relID = relIDByOccurrence[strings.ToLower(p.Occurrence)]
		}
		objects = append(objects, map[string]interface{}{
			"id":     uuid.NewString(),
			"type":   "portal",
			"x":      p.Left,
			"y":      p.Top,
			"width":  pw,
			"height": ph,
			"portal_config": map[string]interface{}{
				"relationship_id": relID,
				"occurrence":      p.Occurrence,
				"row_count":       p.RowCount,
				"allow_creation":  true,
				"allow_deletion":  true,
			},
		})
		for _, cf := range p.ChildFields {
			cw := cf.Right - cf.Left
			ch := cf.Bottom - cf.Top
			if cw <= 0 {
				cw = 120
			}
			if ch <= 0 {
				ch = 24
			}
			colName := sanitizeIdentifier(cf.FieldName)
			objects = append(objects, map[string]interface{}{
				"id":     uuid.NewString(),
				"type":   "field",
				"name":   cf.FieldName,
				"x":      p.Left + cf.Left,
				"y":      p.Top + cf.Top,
				"width":  cw,
				"height": ch,
				"field_binding": map[string]interface{}{
					"table_occurrence": p.Occurrence,
					"relationship_id":  relID,
					"field_name":       colName,
					"control_style":    "edit_box",
				},
			})
		}
	}

	// 4. Buttons
	for _, b := range pl.Buttons {
		bx := b.Left
		by := b.Top
		bw := b.Right - b.Left
		bh := b.Bottom - b.Top
		if bw <= 0 {
			bw = 88
		}
		if bh <= 0 {
			bh = 28
		}
		if strings.Contains(strings.ToLower(b.ActionName), "delete portal row") && by < 40 && len(pl.Portals) > 0 {
			continue
		}
		if by+bh > maxBottom {
			maxBottom = by + bh + 32
		}
		action := buildButtonActionMap(b, scriptIDByName)
		style := map[string]interface{}{
			"font_size": 12,
		}
		if b.IsTextGroup {
			style = map[string]interface{}{
				"font_size":    12,
				"text_color":   "#0066CC",
				"fill_color":   "#00000000",
				"border_width": 0,
				"text_align":   "left",
			}
		}
		objects = append(objects, map[string]interface{}{
			"id":     uuid.NewString(),
			"type":   "button",
			"x":      bx,
			"y":      by,
			"width":  bw,
			"height": bh,
			"text":   b.Label,
			"action": action,
			"style":  style,
		})
	}

	// 5. Web Viewers
	for _, wv := range pl.WebViewers {
		ww := wv.Right - wv.Left
		wh := wv.Bottom - wv.Top
		if ww <= 0 {
			ww = 800
		}
		if wh <= 0 {
			wh = 500
		}
		if wv.Bottom > maxBottom {
			maxBottom = wv.Bottom + 32
		}
		urlText := wv.URL
		if strings.Contains(urlText, "::") {
			_, fld := splitQualifiedField(urlText)
			urlText = "{{" + sanitizeIdentifier(fld) + "}}"
		}
		objects = append(objects, map[string]interface{}{
			"id":     uuid.NewString(),
			"type":   "web_viewer",
			"name":   wv.Name,
			"x":      wv.Left,
			"y":      wv.Top,
			"width":  ww,
			"height": wh,
			"text":   urlText,
		})
	}

	defaultView := "form"
	parts := []map[string]interface{}{
		{
			"id":     "body_part",
			"type":   "body",
			"height": maxBottom,
		},
	}
	if strings.Contains(strings.ToLower(pl.Name), "list") {
		defaultView = "list"
		bodyH := maxBottom - 72.0
		if bodyH < 48.0 {
			bodyH = 48.0
		}
		parts = []map[string]interface{}{
			{
				"id":     "header_part",
				"type":   "header",
				"height": 72.0,
			},
			{
				"id":     "body_part",
				"type":   "body",
				"height": bodyH,
			},
		}
	}

	return map[string]interface{}{
		"id":               layoutID,
		"name":             pl.Name,
		"table_occurrence": occName,
		"width":            pl.Width,
		"theme":            "Enlightened",
		"default_view":     defaultView,
		"parts":            parts,
		"objects":          objects,
	}
}

func sectionBetween(s, startMarker, endMarker string) string {
	sIdx := strings.Index(s, startMarker)
	if sIdx < 0 {
		return ""
	}
	rest := s[sIdx:]
	if endMarker != "" {
		if eIdx := strings.Index(rest, endMarker); eIdx > 0 {
			return rest[:eIdx]
		}
	}
	return rest
}

func cleanHTMLText(raw string) string {
	// Strip inline tags without inserting spaces so `<em>recipes</em>::Yield` stays `recipes::Yield`
	noInline := reHTMLInlineTags.ReplaceAllString(raw, "")
	noTags := reHTMLTags.ReplaceAllString(noInline, " ")
	unescaped := html.UnescapeString(noTags)
	unescaped = strings.ReplaceAll(unescaped, "\u00a0", " ")
	return strings.TrimSpace(reHTMLSpaces.ReplaceAllString(unescaped, " "))
}

var reHorizSpaces = regexp.MustCompile(`[ \t\x{00a0}]+`)

func cleanHTMLMergeText(raw string) string {
	noInline := reHTMLInlineTags.ReplaceAllString(raw, "")
	noTags := reHTMLTags.ReplaceAllString(noInline, "")
	unescaped := html.UnescapeString(noTags)
	unescaped = strings.ReplaceAll(unescaped, "\r\n", "\n")
	unescaped = strings.ReplaceAll(unescaped, "\r", "\n")
	unescaped = strings.ReplaceAll(unescaped, "¶", "\n")
	lines := strings.Split(unescaped, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(reHorizSpaces.ReplaceAllString(ln, " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parsePt(s string) float64 {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "pt"))
	f, _ := strconv.ParseFloat(strings.TrimSpace(trimmed), 64)
	return f
}

func parseWidthHeight(bounds string) (float64, float64) {
	wRe := regexp.MustCompile(`Width:\s*(\d+(?:\.\d+)?)\s*pt`)
	hRe := regexp.MustCompile(`Height:\s*(\d+(?:\.\d+)?)\s*pt`)
	var w, h float64
	if m := wRe.FindStringSubmatch(bounds); len(m) > 1 {
		w, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := hRe.FindStringSubmatch(bounds); len(m) > 1 {
		h, _ = strconv.ParseFloat(m[1], 64)
	}
	return w, h
}

func splitQualifiedField(s string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(s), "::", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", strings.TrimSpace(s)
}

func convertMergeString(raw string) string {
	return reMergeToken.ReplaceAllStringFunc(raw, func(match string) string {
		sub := reMergeToken.FindStringSubmatch(match)
		if len(sub) >= 3 {
			rawToken := strings.TrimSpace(sub[2])
			if strings.EqualFold(rawToken, "LayoutName") {
				return "{{LayoutName}}"
			}
			return "{{" + sanitizeIdentifier(rawToken) + "}}"
		}
		return match
	})
}
