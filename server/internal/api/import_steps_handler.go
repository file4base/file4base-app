package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/go-chi/chi/v5"
)

// Import Records steps migrated from FileMaker that import from another
// FileMaker file wait, disabled, for a transfer (#191). The migration review
// lists them, infers the source table each one read when one table alone
// fits, and creates the transfers a person confirms.

// ImportStepMapping is one target field a step writes or matches on.
type ImportStepMapping struct {
	Position    int    `json:"position"`
	TargetField string `json:"target_field"`
	Match       bool   `json:"match,omitempty"`
}

// ImportStepView is a step waiting for its transfer.
type ImportStepView struct {
	StepID     string `json:"step_id"`
	ScriptID   string `json:"script_id"`
	ScriptName string `json:"script_name"`
	StepIndex  int    `json:"step_index"`

	// SourceFile is the FileMaker file the step read, "" when its path is
	// computed as the script runs.
	SourceFile  string              `json:"source_file,omitempty"`
	TargetTable string              `json:"target_table"`
	Method      string              `json:"method"`
	AddRemain   bool                `json:"add_remaining,omitempty"`
	AutoEnter   []string            `json:"auto_enter_fields,omitempty"`
	Mappings    []ImportStepMapping `json:"mappings"`
	// MatchByName: FileMaker pairs the fields by matching names each time
	// the step runs, and so does its transfer.
	MatchByName bool `json:"match_by_name,omitempty"`

	// SuggestedDatabase is the solution the source file most likely became:
	// this one when the step imports from the file the design was saved
	// from, the solution a file reference of that name is mapped to, or the
	// only solution of that name. SuggestedBy says which.
	SuggestedDatabase string `json:"suggested_database,omitempty"`
	SuggestedBy       string `json:"suggested_by,omitempty"`

	// Problem says why the step cannot be given a transfer as it is.
	Problem string `json:"problem,omitempty"`
	// EnabledInFileMaker: confirming the transfer enables the step.
	EnabledInFileMaker bool `json:"enabled_in_filemaker"`
}

func (h *TransferHandler) registerImportStepRoutes(r chi.Router) {
	r.With(RequireAdmin).Get("/import-steps", h.ListImportSteps)
	r.With(RequireAdmin).Post("/import-steps/infer", h.InferImportSteps)
	r.With(RequireAdmin, RequireDesignUnlocked).Post("/import-steps/confirm", h.ConfirmImportSteps)
}

// ListImportSteps answers the steps waiting for a transfer.
func (h *TransferHandler) ListImportSteps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	svc := schemaService(r)
	steps, err := svc.ListAwaitingImportSteps(ctx)
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	refs, err := svc.ListFileReferences(ctx)
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	var databases []string
	if h.dbMgr != nil {
		databases, _ = h.dbMgr.ListDatabases(ctx)
	}
	local := currentSession(r).Database

	views := make([]ImportStepView, 0, len(steps))
	for _, st := range steps {
		step, _ := transfer.ParseImportStep(st.Params)
		v := ImportStepView{
			StepID: st.StepID, ScriptID: st.ScriptID, ScriptName: st.ScriptName, StepIndex: st.StepIndex,
			SourceFile: step.SourceFile, TargetTable: step.TargetTable, Method: step.Method,
			AddRemain: step.AddRemaining, Mappings: []ImportStepMapping{}, MatchByName: step.MatchByName,
			EnabledInFileMaker: st.EnabledInFileMaker,
		}
		for _, m := range step.Maps {
			v.Mappings = append(v.Mappings, ImportStepMapping{Position: m.Position, TargetField: m.Target.Name, Match: m.Match})
		}
		for _, f := range step.AutoEnterFields {
			v.AutoEnter = append(v.AutoEnter, f.Name)
		}
		if step.AutoEnterFields == nil && step.AutoEnter {
			v.AutoEnter = []string{"*"}
		}
		if step.PathIsCalculation {
			// The file is chosen as the script runs: the name saved is
			// only one of the files it may be, so none is suggested.
			v.SourceFile = ""
		} else {
			v.SuggestedDatabase, v.SuggestedBy = suggestSource(st.Params, step.SourceFile, local, refs, databases)
		}
		v.Problem = step.Problem()
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"steps": views})
}

// suggestSource names the solution a step's source file most likely became.
func suggestSource(
	params map[string]interface{}, file, local string, refs []schema.FileReference, databases []string,
) (string, string) {
	if source, _ := params["import_source"].(map[string]interface{}); source["this_file"] == true {
		return local, "this_file"
	}
	if file == "" {
		return "", ""
	}
	for _, ref := range refs {
		if ref.IsMapped() && strings.EqualFold(strings.TrimSpace(ref.Name), file) {
			return ref.Database, "file_reference"
		}
	}
	match := ""
	for _, db := range databases {
		if squashName(db) == squashName(file) {
			if match != "" {
				return "", ""
			}
			match = db
		}
	}
	if match != "" {
		return match, "database_name"
	}
	return "", ""
}

func squashName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ImportStepsInferRequest asks which table of a solution each step read.
type ImportStepsInferRequest struct {
	// SourceDatabase is the solution to look in; empty is this one.
	SourceDatabase string `json:"source_database"`
	// PeerToken signs in to SourceDatabase when it is another solution.
	PeerToken string   `json:"peer_token,omitempty"`
	StepIDs   []string `json:"step_ids"`
}

// ImportStepInference is what the source solution says about one step.
type ImportStepInference struct {
	StepID string `json:"step_id"`
	transfer.Inference
	Error string `json:"error,omitempty"`
}

// ImportStepsInferResponse answers the inference of each step, and the
// tables of the source solution a person may pick from.
type ImportStepsInferResponse struct {
	SourceDatabase string                `json:"source_database"`
	Tables         []string              `json:"tables"`
	Steps          []ImportStepInference `json:"steps"`
}

// sourceSolution is a solution steps may import from, as the account that
// reaches it may read it.
type sourceSolution struct {
	end      transferEnd
	database string // "" for this solution
	tables   map[string]transfer.PositionedTable
	names    []string
	// hidden is how many tables the account may not export from.
	hidden int
}

// openSource opens the solution steps import from: this one, or another one
// through its own session token, which is the only authority the server
// takes for it. Only the tables that account may export from are offered.
func (h *TransferHandler) openSource(
	w http.ResponseWriter, r *http.Request, database, token string,
) (*sourceSolution, bool) {
	local := currentSession(r)
	database = strings.TrimSpace(database)
	var src sourceSolution
	if database == "" || strings.EqualFold(database, local.Database) {
		driver, _ := dbal.DriverFromContext(r.Context())
		src.end = transferEnd{database: local.Database, driver: driver, session: local}
	} else {
		end, ok := resolvePeerSession(w, r, h.dbMgr, h.sessions, strings.TrimSpace(token), database, peerPurpose{
			what:           "the source of these Import Records steps",
			unavailable:    "Transfers Unavailable",
			unavailableWhy: "this server cannot authorize another solution",
		})
		if !ok {
			return nil, false
		}
		src.end, src.database = end, end.database
	}
	svc := schema.NewService(src.end.driver)
	tables, err := svc.ListTables(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return nil, false
	}
	account := transfer.Account{ID: src.end.session.UserID, Role: src.end.session.Role}
	src.tables = map[string]transfer.PositionedTable{}
	for _, t := range tables {
		err := transfer.CheckEnd(r.Context(), src.end.driver, account, src.end.database,
			schema.CapabilityBulkExport, t.Name, false)
		if errors.Is(err, transfer.ErrNotAuthorized) {
			src.hidden++
			continue
		}
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return nil, false
		}
		deleted, err := svc.DeletedSourceFieldIDs(r.Context(), t.Name)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return nil, false
		}
		src.tables[t.Name] = transfer.PositionTable(t, deleted...)
		src.names = append(src.names, t.Name)
	}
	return &src, true
}

// InferImportSteps infers the source table of each step in one solution.
func (h *TransferHandler) InferImportSteps(w http.ResponseWriter, r *http.Request) {
	var body ImportStepsInferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	src, ok := h.openSource(w, r, body.SourceDatabase, body.PeerToken)
	if !ok {
		return
	}
	ctx := r.Context()
	resp := ImportStepsInferResponse{SourceDatabase: src.database, Tables: src.names, Steps: []ImportStepInference{}}
	if resp.Tables == nil {
		resp.Tables = []string{}
	}
	for _, id := range body.StepIDs {
		out := ImportStepInference{StepID: id, Inference: transfer.Inference{Candidates: []transfer.SourceCandidate{}}}
		step, target, err := loadImportStep(ctx, schemaService(r), id)
		if err != nil {
			out.Error = err.Error()
			resp.Steps = append(resp.Steps, out)
			continue
		}
		exclude := ""
		if src.database == "" {
			exclude = step.TargetTable
		}
		positioned := make([]transfer.PositionedTable, 0, len(src.names))
		for _, name := range src.names {
			positioned = append(positioned, src.tables[name])
		}
		out.Inference = transfer.InferSource(step, target, positioned, exclude)
		if src.hidden > 0 {
			// Uniqueness is not claimed over tables nobody could weigh.
			out.TablesHidden = src.hidden
			out.Reason += fmt.Sprintf("; %d table(s) of the solution were left out, as this account may not "+
				"export from them", src.hidden)
		}
		resp.Steps = append(resp.Steps, out)
	}
	writeJSON(w, http.StatusOK, resp)
}

// loadImportStep reads a step waiting for its transfer and its target table.
func loadImportStep(
	ctx context.Context, svc *schema.Service, stepID string,
) (transfer.ImportStep, transfer.TargetColumns, error) {
	var target transfer.TargetColumns
	st, err := svc.AwaitingImportStep(ctx, stepID)
	if err != nil {
		return transfer.ImportStep{}, target, err
	}
	step, ok := transfer.ParseImportStep(st.Params)
	if !ok {
		return step, target, schema.ErrImportStepNotFound
	}
	table, found, err := svc.TableByName(ctx, step.TargetTable)
	if err != nil {
		return step, target, err
	}
	if !found {
		return step, target, errors.New("the target table " + step.TargetTable + " is not in this solution")
	}
	return step, transfer.NewTargetColumns(*table), nil
}

// ImportStepChoice is the source a person confirmed for one step.
type ImportStepChoice struct {
	StepID         string `json:"step_id"`
	SourceDatabase string `json:"source_database"`
	SourceTable    string `json:"source_table"`
}

// ImportStepsConfirmRequest creates the transfers of the confirmed steps.
type ImportStepsConfirmRequest struct {
	Items []ImportStepChoice `json:"items"`
	// PeerTokens sign in to the other solutions the items import from, by
	// database name.
	PeerTokens map[string]string `json:"peer_tokens,omitempty"`
}

// ImportStepOutcome is what confirming one step did.
type ImportStepOutcome struct {
	StepID   string `json:"step_id"`
	Transfer string `json:"transfer,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ConfirmImportSteps creates the transfer of each confirmed step and names it
// in the step, which is enabled again if FileMaker had it enabled. Each step
// is created on its own: one that cannot be is reported and leaves the
// others as they are.
func (h *TransferHandler) ConfirmImportSteps(w http.ResponseWriter, r *http.Request) {
	var body ImportStepsConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if len(body.Items) == 0 {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Nothing To Confirm", "items names no step")
		return
	}
	ctx := r.Context()
	svc := schemaService(r)
	local := currentSession(r).Database
	// Every source is opened, and its session checked, before any transfer
	// is created: a refusal then leaves every step as it was, rather than
	// hiding the ones already created.
	sourceKey := func(database string) string {
		key := strings.TrimSpace(database)
		if strings.EqualFold(key, local) {
			return ""
		}
		return key
	}
	sources := map[string]*sourceSolution{}
	for _, item := range body.Items {
		key := sourceKey(item.SourceDatabase)
		if _, opened := sources[key]; opened {
			continue
		}
		token := body.PeerTokens[key]
		if token == "" {
			for db, t := range body.PeerTokens {
				if strings.EqualFold(db, key) {
					token = t
				}
			}
		}
		src, ok := h.openSource(w, r, key, token)
		if !ok {
			return // the refusal says which solution and why
		}
		sources[key] = src
	}
	outcomes := make([]ImportStepOutcome, 0, len(body.Items))
	for _, item := range body.Items {
		src := sources[sourceKey(item.SourceDatabase)]
		out := ImportStepOutcome{StepID: item.StepID}
		def, err := h.confirmImportStep(ctx, svc, src, item, currentSession(r).UserID, local)
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Transfer = def.Name
		}
		outcomes = append(outcomes, out)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"steps": outcomes})
}

func (h *TransferHandler) confirmImportStep(
	ctx context.Context, svc *schema.Service, src *sourceSolution, item ImportStepChoice, createdBy, local string,
) (*schema.TransferDefinition, error) {
	step, target, err := loadImportStep(ctx, svc, item.StepID)
	if err != nil {
		return nil, err
	}
	table, ok := src.tables[strings.TrimSpace(item.SourceTable)]
	if !ok {
		return nil, errors.New("the source solution has no table " + item.SourceTable +
			" that the account signed in to it may export from")
	}
	if len(table.Fields) == 0 {
		return nil, errors.New("the table " + table.Name + " was not migrated from FileMaker, " +
			"so it has no field positions to map")
	}
	st, err := svc.AwaitingImportStep(ctx, item.StepID)
	if err != nil {
		return nil, err
	}
	shown := src.database
	if shown == "" {
		shown = local
	}
	name := transfer.TransferName(st.ScriptName, st.StepIndex, shown, table.Name, step.TargetTable)
	in, err := transfer.PlanTransfer(step, name, src.database, table, target)
	if err != nil {
		return nil, err
	}
	return svc.CreateImportStepTransfer(ctx, item.StepID, in, createdBy)
}
