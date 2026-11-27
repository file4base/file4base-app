package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

type backupTableSnapshot struct {
	Metadata schema.TableMetadata    `json:"metadata"`
	Columns  []schema.ColumnMetadata `json:"columns"`
}

// backupContent is what a restore reads of a backup file.
type backupContent struct {
	Database          string                         `json:"database"`
	TablesCount       int                            `json:"tables_count"`
	Tables            map[string]backupTableSnapshot `json:"tables"`
	Layouts           []schema.LayoutMetadata        `json:"layouts"`
	Scripts           []schema.ScriptMetadata        `json:"scripts"`
	SolutionBundleB64 string                         `json:"solution_bundle_b64"`
	DataBundleB64     string                         `json:"data_bundle_b64"`

	solution, records []byte
}

// readBackupContent reads and decodes a backup file before anything changes,
// so a damaged backup is refused rather than half restored.
func readBackupContent(path string) (*backupContent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c backupContent
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("the backup cannot be read: %w", err)
	}
	raw = nil
	if c.records, err = base64.StdEncoding.DecodeString(c.DataBundleB64); err != nil {
		return nil, fmt.Errorf("the records of the backup cannot be read: %w", err)
	}
	if c.solution, err = base64.StdEncoding.DecodeString(c.SolutionBundleB64); err != nil {
		return nil, fmt.Errorf("the design of the backup cannot be read: %w", err)
	}
	c.DataBundleB64, c.SolutionBundleB64 = "", ""
	return &c, nil
}

// restoreReport counts what a restore changed.
type restoreReport struct {
	TablesDropped   int
	TablesCreated   int
	ColumnsDropped  int
	ColumnsCreated  int
	LayoutsRestored int
	ScriptsRestored int
	RecordsRestored int
}

// lockRestore lets one restore of a database run at a time.
func (h *ServerAdminHandler) lockRestore(database string) (func(), bool) {
	key := strings.ToLower(database)
	h.restoreMu.Lock()
	defer h.restoreMu.Unlock()
	if h.restoring[key] {
		return nil, false
	}
	if h.restoring == nil {
		h.restoring = map[string]bool{}
	}
	h.restoring[key] = true
	return func() {
		h.restoreMu.Lock()
		delete(h.restoring, key)
		h.restoreMu.Unlock()
	}, true
}

// RestoreBackup puts a database back as a backup holds it: its tables and
// fields, layouts, scripts, occurrences, relationships, value lists, menus
// and records. Its accounts are left as they are.
//
// A restore either completes or leaves the database as it was (#203). The
// first step that fails stops it, and the error says which. A pre-restore
// backup is taken first; on PostgreSQL the restore then runs in one
// transaction, which a failure rolls back, while on MariaDB, which commits
// before every change of a table's structure, a failure is undone by
// restoring the pre-restore backup. A restore that completes keeps that
// backup, so it can be undone too.
func (h *ServerAdminHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(chi.URLParam(r, "id"))
	sess := currentSession(r)
	filename := id
	if !strings.HasSuffix(filename, ".f4b.json") {
		filename += ".f4b.json"
	}
	path, _, ok := h.ownBackup(w, r, filename)
	if !ok {
		return
	}
	content, err := readBackupContent(path)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Corrupt Backup", err.Error())
		return
	}

	// ownBackup already checked the backup's database against the session.
	targetDB := strings.TrimSpace(content.Database)
	if targetDB == "" {
		targetDB = sess.Database
	}
	unlock, ok := h.lockRestore(targetDB)
	if !ok {
		telemetry.WriteProblem(w, r, http.StatusConflict, "Restore In Progress",
			"a restore of "+targetDB+" is already running")
		return
	}
	defer unlock()
	// A restore runs to its end, or its rollback, even if the client leaves.
	ctx := context.WithoutCancel(r.Context())

	now := time.Now().UTC()
	pre, err := h.writeSnapshot(ctx, targetDB, h.newBackupID(targetDB, now, "pre_restore"), sess.Username,
		"Automatic backup before restoring "+id, "pre_restore", now)
	if err != nil {
		h.failRestore(w, r, sess.Username, targetDB, id,
			fmt.Errorf("the pre-restore backup could not be taken, so nothing was restored: %w", err))
		return
	}
	prePath := filepath.Join(h.backupsDir, pre.Filename)

	tablesCount := content.TablesCount
	report, err := h.restoreInto(ctx, targetDB, content)
	content = nil
	if err != nil {
		if h.dbMgr.Engine() == dbal.EnginePostgres {
			_ = os.Remove(prePath)
			h.failRestore(w, r, sess.Username, targetDB, id,
				fmt.Errorf("%w; the restore was rolled back, the database is unchanged", err))
			return
		}
		undo, rbErr := readBackupContent(prePath)
		if rbErr == nil {
			_, rbErr = h.restoreInto(ctx, targetDB, undo)
		}
		if rbErr != nil {
			h.failRestore(w, r, sess.Username, targetDB, id, fmt.Errorf(
				"%w; putting the database back as it was also failed (%v): restore the pre-restore backup %s",
				err, rbErr, pre.ID))
			return
		}
		_ = os.Remove(prePath)
		h.failRestore(w, r, sess.Username, targetDB, id,
			fmt.Errorf("%w; the database was put back as it was before the restore", err))
		return
	}

	details := fmt.Sprintf(
		"Restored snapshot %s on %s (%d layouts, %d cols dropped, %d cols added, %d tables dropped, %d rows restored; pre-restore backup %s)",
		id, targetDB, report.LayoutsRestored, report.ColumnsDropped, report.ColumnsCreated, report.TablesDropped, report.RecordsRestored, pre.ID,
	)
	h.recordAudit("backup", "Restore Backup", sess.Username, targetDB, id, "success", details)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                 id,
		"database":           targetDB,
		"tables_count":       tablesCount,
		"tables_dropped":     report.TablesDropped,
		"tables_created":     report.TablesCreated,
		"columns_dropped":    report.ColumnsDropped,
		"columns_created":    report.ColumnsCreated,
		"layouts_restored":   report.LayoutsRestored,
		"scripts_restored":   report.ScriptsRestored,
		"records_restored":   report.RecordsRestored,
		"pre_restore_backup": pre.ID,
		"status":             "restored",
		"restored_at":        time.Now().UTC().Format(time.RFC3339),
	})
}

// restoreInto restores content into database: in one transaction on
// PostgreSQL, directly on MariaDB.
func (h *ServerAdminHandler) restoreInto(ctx context.Context, database string, content *backupContent) (restoreReport, error) {
	defer h.forgetCaches(ctx, database)
	if h.dbMgr.Engine() != dbal.EnginePostgres {
		driver, err := h.dbMgr.DriverFor(ctx, database)
		if err != nil {
			return restoreReport{}, err
		}
		return restoreContent(ctx, schema.NewService(driver), content)
	}
	tx, err := h.dbMgr.OpenTransaction(ctx, database)
	if err != nil {
		return restoreReport{}, err
	}
	defer schema.ForgetPool(tx.DB())
	report, err := restoreContent(ctx, schema.NewService(tx), content)
	if err != nil {
		_ = tx.Rollback()
		return report, err
	}
	if err := tx.Commit(ctx); err != nil {
		return report, fmt.Errorf("the restore could not be committed: %w", err)
	}
	return report, nil
}

// forgetCaches drops what this process keeps of a database that was changed
// through another connection pool.
func (h *ServerAdminHandler) forgetCaches(ctx context.Context, database string) {
	if driver, err := h.dbMgr.DriverFor(ctx, database); err == nil {
		data.InvalidateLibrary(driver)
		data.ForgetCreationOrder(driver.DB())
	}
}

// restoreContent reconciles the database with the backup. The first error
// stops it.
func restoreContent(ctx context.Context, svc *schema.Service, snap *backupContent) (restoreReport, error) {
	var rep restoreReport
	if err := svc.EnsureSystemTables(ctx); err != nil {
		return rep, fmt.Errorf("the catalog could not be prepared: %w", err)
	}

	// 1. Tables and fields.
	if len(snap.Tables) > 0 {
		if err := restoreTables(ctx, svc, snap, &rep); err != nil {
			return rep, err
		}
	}

	// 2. Occurrences, relationships, value lists, menu sets, saved finds and
	// the layout and script menus, from the solution bundle.
	menusPlaced := false
	if len(snap.solution) > 0 {
		if _, err := svc.ImportSolution(ctx, snap.solution); err != nil {
			return rep, fmt.Errorf("the occurrences, relationships and menus could not be restored: %w", err)
		}
		menusPlaced = true
	}

	// 3. Scripts.
	if snap.Scripts != nil {
		if err := restoreScripts(ctx, svc, snap, menusPlaced, &rep); err != nil {
			return rep, err
		}
	}

	// 4. Layouts, removing those added after the backup.
	if len(snap.Layouts) > 0 {
		if err := restoreLayouts(ctx, svc, snap, &rep); err != nil {
			return rep, err
		}
	}

	// 5. Records.
	if len(snap.records) > 0 {
		currentTables, err := svc.ListTables(ctx)
		if err != nil {
			return rep, fmt.Errorf("the tables could not be listed: %w", err)
		}
		for _, ct := range currentTables {
			if err := svc.TruncateTable(ctx, ct.ID); err != nil {
				return rep, fmt.Errorf("table %s could not be emptied: %w", ct.Name, err)
			}
		}
		dataReport, err := svc.ImportDatabaseData(ctx, snap.records)
		if err != nil {
			return rep, fmt.Errorf("the records could not be restored: %w", err)
		}
		rep.RecordsRestored = dataReport.RecordsInserted
	}
	return rep, nil
}

func restoreTables(ctx context.Context, svc *schema.Service, snap *backupContent, rep *restoreReport) error {
	snapTables := make(map[string]backupTableSnapshot, len(snap.Tables))
	for rawName, entry := range snap.Tables {
		normName := dbal.NormalizeIdentifier(rawName)
		if normName == "" {
			normName = dbal.NormalizeIdentifier(entry.Metadata.Name)
		}
		if normName != "" {
			snapTables[normName] = entry
		}
	}

	currentTables, err := svc.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("the tables could not be listed: %w", err)
	}
	for _, curTbl := range currentTables {
		if _, keep := snapTables[dbal.NormalizeIdentifier(curTbl.Name)]; !keep {
			if err := svc.DeleteTable(ctx, curTbl.ID); err != nil {
				return fmt.Errorf("table %s, added after the backup, could not be removed: %w", curTbl.Name, err)
			}
			rep.TablesDropped++
		}
	}

	currentTables, err = svc.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("the tables could not be listed: %w", err)
	}
	curTableByName := make(map[string]schema.TableMetadata, len(currentTables))
	for _, ct := range currentTables {
		curTableByName[dbal.NormalizeIdentifier(ct.Name)] = ct
	}

	for normName, snapEntry := range snapTables {
		curTbl, exists := curTableByName[normName]
		if !exists {
			disp := snapEntry.Metadata.DisplayName
			if disp == "" {
				disp = normName
			}
			created, err := svc.CreateTable(ctx, disp, normName)
			if err != nil {
				return fmt.Errorf("table %s could not be created: %w", normName, err)
			}
			curTbl = *created
			rep.TablesCreated++
		}

		cols := snapEntry.Columns
		if len(cols) == 0 {
			cols = snapEntry.Metadata.Columns
		}
		snapColsByName := make(map[string]schema.ColumnMetadata, len(cols))
		for _, sc := range cols {
			snapColsByName[dbal.NormalizeIdentifier(sc.Name)] = sc
		}

		curColsByName := make(map[string]schema.ColumnMetadata, len(curTbl.Columns))
		for _, cc := range curTbl.Columns {
			normCol := dbal.NormalizeIdentifier(cc.Name)
			curColsByName[normCol] = cc
			if cc.IsPrimaryKey {
				continue
			}
			if _, keep := snapColsByName[normCol]; !keep {
				if err := svc.DeleteColumn(ctx, curTbl.ID, cc.ID); err != nil {
					return fmt.Errorf("field %s::%s, added after the backup, could not be removed: %w", normName, cc.Name, err)
				}
				rep.ColumnsDropped++
				delete(curColsByName, normCol)
			}
		}

		// Missing fields are added, non-calculations first, then
		// calculations; existing ones are updated.
		for _, calcPass := range []bool{false, true} {
			for _, sc := range cols {
				isCalc := strings.EqualFold(string(sc.FieldType), string(dbal.FieldTypeCalculation))
				if isCalc != calcPass {
					continue
				}
				normCol := dbal.NormalizeIdentifier(sc.Name)
				if existingCol, found := curColsByName[normCol]; found {
					if err := restoreExistingColumn(ctx, svc, curTbl.ID, existingCol, sc, calcPass); err != nil {
						return fmt.Errorf("field %s::%s could not be restored: %w", normName, sc.Name, err)
					}
					continue
				}
				if sc.IsPrimaryKey {
					continue
				}
				added, err := svc.AddColumn(ctx, curTbl.ID, schema.ColumnMetadata{
					Name:               normCol,
					DisplayName:        sc.DisplayName,
					FieldType:          sc.FieldType,
					IsNullable:         sc.IsNullable,
					DefaultValue:       sc.DefaultValue,
					CalculationFormula: sc.CalculationFormula,
					ValidationRules:    sc.ValidationRules,
					MaxRepetitions:     sc.MaxRepetitions,
					SourceFieldID:      sc.SourceFieldID,
					SourceType:         sc.SourceType,
				})
				if err == nil && added == nil {
					err = errors.New("it was not created")
				}
				if err != nil {
					return fmt.Errorf("field %s::%s could not be created: %w", normName, sc.Name, err)
				}
				rep.ColumnsCreated++
				curColsByName[normCol] = *added
			}
		}
	}
	return nil
}

// restoreExistingColumn puts back the options of a field the database has.
func restoreExistingColumn(ctx context.Context, svc *schema.Service, tableID string, existing, sc schema.ColumnMetadata, calcPass bool) error {
	if sc.IsPrimaryKey {
		return nil
	}
	if !calcPass {
		_, err := svc.UpdateColumn(ctx, tableID, existing.ID, schema.UpdateColumnOptions{
			DisplayName:        sc.DisplayName,
			DefaultValue:       sc.DefaultValue,
			UpdateDefaultValue: true,
			CalculationFormula: sc.CalculationFormula,
			UpdateCalculation:  sc.CalculationFormula != nil,
			ValidationRules:    sc.ValidationRules,
			UpdateValidation:   sc.ValidationRules != nil,
			MaxRepetitions:     sc.MaxRepetitions,
			UpdateRepetitions:  sc.MaxRepetitions > 0,
		})
		return err
	}
	if sc.MaxRepetitions <= 0 || sc.MaxRepetitions == existing.MaxRepetitions {
		return nil
	}
	// An existing calculation keeps its formula, but its maximum is restored
	// like any field's: the records restored later carry that many
	// repetitions.
	display := sc.DisplayName
	if display == "" {
		display = existing.DisplayName
	}
	_, err := svc.UpdateColumn(ctx, tableID, existing.ID, schema.UpdateColumnOptions{
		DisplayName:       display,
		MaxRepetitions:    sc.MaxRepetitions,
		UpdateRepetitions: true,
	})
	return err
}

func restoreScripts(ctx context.Context, svc *schema.Service, snap *backupContent, menusPlaced bool, rep *restoreReport) error {
	snapScriptIDs := make(map[string]bool, len(snap.Scripts))
	snapScriptNames := make(map[string]bool, len(snap.Scripts))
	for _, sc := range snap.Scripts {
		snapScriptIDs[sc.ID] = true
		snapScriptNames[sc.Name] = true
	}

	currentScripts, err := svc.ListScripts(ctx)
	if err != nil {
		return fmt.Errorf("the scripts could not be listed: %w", err)
	}
	curScriptByID := make(map[string]schema.ScriptMetadata, len(currentScripts))
	curScriptByName := make(map[string]schema.ScriptMetadata, len(currentScripts))
	for _, cs := range currentScripts {
		if len(snap.Scripts) > 0 && !snapScriptIDs[cs.ID] && !snapScriptNames[cs.Name] {
			if err := svc.DeleteScript(ctx, cs.ID); err != nil {
				return fmt.Errorf("script %q, added after the backup, could not be removed: %w", cs.Name, err)
			}
			continue
		}
		curScriptByID[cs.ID] = cs
		curScriptByName[cs.Name] = cs
	}

	for _, sc := range snap.Scripts {
		remappedSteps := make([]schema.ScriptStepMetadata, 0, len(sc.Steps))
		stepIDMap := make(map[string]string, len(sc.Steps))
		for _, st := range sc.Steps {
			if st.ID != "" {
				stepIDMap[st.ID] = uuid.NewString()
			}
		}
		for i, st := range sc.Steps {
			newID := stepIDMap[st.ID]
			if newID == "" {
				newID = uuid.NewString()
			}
			seq := st.SequenceIdx
			if seq <= 0 {
				seq = i + 1
			}
			var parentID *string
			if st.ParentStepID != nil && *st.ParentStepID != "" {
				if mapped, ok := stepIDMap[*st.ParentStepID]; ok {
					parentID = &mapped
				}
			}
			params := st.Params
			if len(params) == 0 {
				params = json.RawMessage(`{}`)
			}
			remappedSteps = append(remappedSteps, schema.ScriptStepMetadata{
				ID:           newID,
				SequenceIdx:  seq,
				StepType:     st.StepType,
				Params:       params,
				IsEnabled:    st.IsEnabled,
				ParentStepID: parentID,
			})
		}

		// The solution bundle placed the scripts in the menu's folders, under
		// ids of their own: the snapshot's folder ids name the backed-up
		// database's, so they are read only when no bundle placed them.
		folderOf := func(current *string) *string {
			if menusPlaced {
				return current
			}
			return sc.FolderID
		}
		var err error
		if existing, ok := curScriptByID[sc.ID]; ok {
			_, err = svc.UpdateScript(ctx, existing.ID, sc.Name, sc.ContextTable, folderOf(existing.FolderID), sc.IsActive, remappedSteps)
		} else if existing, ok := curScriptByName[sc.Name]; ok {
			_, err = svc.UpdateScript(ctx, existing.ID, sc.Name, sc.ContextTable, folderOf(existing.FolderID), sc.IsActive, remappedSteps)
		} else {
			_, err = svc.CreateScript(ctx, sc.Name, sc.ContextTable, folderOf(nil), sc.IsActive, remappedSteps)
		}
		if err != nil {
			return fmt.Errorf("script %q could not be restored: %w", sc.Name, err)
		}
		rep.ScriptsRestored++
	}
	return nil
}

func restoreLayouts(ctx context.Context, svc *schema.Service, snap *backupContent, rep *restoreReport) error {
	snapLayoutIDs := make(map[string]bool, len(snap.Layouts))
	snapLayoutNames := make(map[string]bool, len(snap.Layouts))
	for _, sl := range snap.Layouts {
		snapLayoutIDs[sl.ID] = true
		snapLayoutNames[sl.Name] = true
	}

	currentLayouts, err := svc.ListLayouts(ctx)
	if err != nil {
		return fmt.Errorf("the layouts could not be listed: %w", err)
	}
	curLayoutByID := make(map[string]schema.LayoutMetadata, len(currentLayouts))
	curLayoutByName := make(map[string]schema.LayoutMetadata, len(currentLayouts))
	for _, cl := range currentLayouts {
		if !snapLayoutIDs[cl.ID] && !snapLayoutNames[cl.Name] {
			if err := svc.DeleteLayout(ctx, cl.ID); err != nil {
				return fmt.Errorf("layout %q, added after the backup, could not be removed: %w", cl.Name, err)
			}
			continue
		}
		curLayoutByID[cl.ID] = cl
		curLayoutByName[cl.Name] = cl
	}

	currentOccs, err := svc.ListTableOccurrences(ctx)
	if err != nil {
		return fmt.Errorf("the table occurrences could not be listed: %w", err)
	}
	occByID := make(map[string]string, len(currentOccs))
	occByName := make(map[string]string, len(currentOccs))
	var defaultOccID string
	for _, o := range currentOccs {
		occByID[o.ID] = o.ID
		occByName[strings.ToLower(o.Name)] = o.ID
		if defaultOccID == "" {
			defaultOccID = o.ID
		}
	}

	for _, sl := range snap.Layouts {
		var err error
		if existing, ok := curLayoutByID[sl.ID]; ok {
			_, err = svc.UpdateLayout(ctx, existing.ID, sl.Name, sl.Definition)
		} else if existing, ok := curLayoutByName[sl.Name]; ok {
			_, err = svc.UpdateLayout(ctx, existing.ID, sl.Name, sl.Definition)
		} else {
			targetOccID := occByID[sl.TableOccurrenceID]
			if targetOccID == "" {
				var defObj map[string]interface{}
				if json.Unmarshal(sl.Definition, &defObj) == nil {
					if occName, ok := defObj["table_occurrence"].(string); ok && occName != "" {
						targetOccID = occByName[strings.ToLower(occName)]
					}
				}
			}
			if targetOccID == "" {
				targetOccID = defaultOccID
			}
			if targetOccID == "" {
				return fmt.Errorf("layout %q could not be restored: the database has no table occurrence to show it on", sl.Name)
			}
			_, err = svc.CreateLayout(ctx, sl.Name, targetOccID, sl.Definition)
		}
		if err != nil {
			return fmt.Errorf("layout %q could not be restored: %w", sl.Name, err)
		}
		rep.LayoutsRestored++
	}
	return nil
}
