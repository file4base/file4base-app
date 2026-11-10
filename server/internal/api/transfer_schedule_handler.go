package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/go-chi/chi/v5"
)

// registerScheduleRoutes serves the transfer schedules of the solution the
// caller is signed in to (#108). Everything about a schedule is an
// administrator's: it runs with nobody signed in, as accounts an
// administrator named.
func (h *TransferHandler) registerScheduleRoutes(r chi.Router) {
	r.Route("/api/v1/transfer-schedules", func(r chi.Router) {
		r.Use(RequireSession, RequireAdmin)
		r.Get("/", h.ListSchedules)
		r.With(RequireDesignUnlocked).Post("/", h.CreateSchedule)
		r.Get("/{id}", h.GetSchedule)
		r.With(RequireDesignUnlocked).Put("/{id}", h.UpdateSchedule)
		r.With(RequireDesignUnlocked).Delete("/{id}", h.DeleteSchedule)
		r.Post("/{id}/run", h.RunScheduleNow)
		r.Get("/{id}/runs", h.ListScheduleRuns)
	})
}

// TransferScheduleRequest is the body that creates a schedule.
type TransferScheduleRequest struct {
	schema.TransferScheduleInput

	// PeerToken is a session of the transfer's other solution, opened by one
	// of its owners or administrators. Only they can name an account of that
	// solution for a schedule to run as.
	PeerToken string `json:"peer_token,omitempty"`
}

// scheduleRunView is a recorded run with its report as JSON rather than as
// a string of JSON.
type scheduleRunView struct {
	ID         string          `json:"id"`
	ScheduleID string          `json:"schedule_id"`
	Trigger    string          `json:"trigger"`
	StartedAt  string          `json:"started_at"`
	FinishedAt string          `json:"finished_at"`
	Status     string          `json:"status"`
	Error      string          `json:"error,omitempty"`
	Report     json.RawMessage `json:"report,omitempty"`
}

func viewOfRun(run schema.TransferScheduleRun) scheduleRunView {
	view := scheduleRunView{
		ID:         run.ID,
		ScheduleID: run.ScheduleID,
		Trigger:    run.Trigger,
		StartedAt:  run.StartedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		FinishedAt: run.FinishedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		Status:     run.Status,
		Error:      run.Error,
	}
	if run.Report != "" && json.Valid([]byte(run.Report)) {
		view.Report = json.RawMessage(run.Report)
	}
	return view
}

func (h *TransferHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := schemaService(r).ListTransferSchedules(r.Context())
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(schedules)
}

func (h *TransferHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	sched, err := schemaService(r).GetTransferSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sched)
}

// CreateSchedule stores a schedule after proving, now, everything a run will
// have to prove again every time: the transfer reaches this solution, the far
// solution's administrator vouches for the account named there, and both
// accounts hold what their end needs.
func (h *TransferHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var body TransferScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	def, err := resolveTransfer(r, body.Transfer)
	if err != nil {
		writeTransferError(w, r, err)
		return
	}

	local := currentSession(r)
	localDriver, _ := dbal.DriverFromContext(r.Context())
	sourceDB := def.Source.DatabaseOr(local.Database)
	destinationDB := def.Destination.DatabaseOr(local.Database)

	// A schedule lives in one end of its transfer. A schedule here for a
	// transfer between two other solutions would let this solution's
	// administrators act on two solutions they need not administer.
	if !strings.EqualFold(sourceDB, local.Database) && !strings.EqualFold(destinationDB, local.Database) {
		writeForbidden(w, r, "a transfer is scheduled in one of its own solutions; this one moves records "+
			"from "+sourceDB+" to "+destinationDB)
		return
	}

	drivers := map[string]dbal.DatabaseDriver{strings.ToLower(local.Database): localDriver}
	peerToken := strings.TrimSpace(body.PeerToken)
	if far := otherEnd(local.Database, sourceDB, destinationDB); far != "" {
		peer, ok := h.resolvePeer(w, r, peerToken, far, "other end")
		if !ok {
			return
		}
		if peer.session.Role != auth.RoleOwner && peer.session.Role != auth.RoleAdmin {
			writeForbidden(w, r, "only an owner or administrator of "+far+
				" can name an account of it for a schedule to run as")
			return
		}
		drivers[strings.ToLower(far)] = peer.driver
	} else if peerToken != "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Peer Token Not Needed",
			"both ends of this transfer are the solution this session signed in to, so it takes no peer_token")
		return
	}

	// The accounts are checked now as a run will check them, so a schedule
	// that could never run is refused rather than stored to fail at night.
	if !h.scheduleAccountAllowed(w, r, drivers[strings.ToLower(sourceDB)], sourceDB,
		body.SourceAccount, schema.CapabilityBulkExport, def.Source.Table, false) {
		return
	}
	if !h.scheduleAccountAllowed(w, r, drivers[strings.ToLower(destinationDB)], destinationDB,
		body.DestinationAccount, schema.CapabilityBulkImport, def.Destination.Table, true) {
		return
	}

	in := body.TransferScheduleInput
	in.Transfer = def.ID
	sched, err := schemaService(r).CreateTransferSchedule(r.Context(), in, local.Database, local.UserID)
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sched)
}

func (h *TransferHandler) scheduleAccountAllowed(
	w http.ResponseWriter, r *http.Request, driver dbal.DatabaseDriver, database, username,
	capability, tableName string, writing bool,
) bool {
	if strings.TrimSpace(username) == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Transfer Schedule",
			"a schedule names the account to run as in each solution")
		return false
	}
	account, err := transfer.ResolveAccount(r.Context(), driver, database, username)
	if err == nil {
		err = transfer.CheckEnd(r.Context(), driver, account, database, capability, tableName, writing)
	}
	switch {
	case err == nil:
		return true
	case errors.Is(err, transfer.ErrNotAuthorized):
		writeForbidden(w, r, strings.TrimPrefix(err.Error(), transfer.ErrNotAuthorized.Error()+": "))
	default:
		telemetry.WriteInternalError(w, r, err)
	}
	return false
}

// otherEnd is the end of a transfer that is not local, or "" when both are.
func otherEnd(local, sourceDB, destinationDB string) string {
	switch {
	case !strings.EqualFold(sourceDB, local):
		return sourceDB
	case !strings.EqualFold(destinationDB, local):
		return destinationDB
	}
	return ""
}

func (h *TransferHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	var patch schema.TransferSchedulePatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	sched, err := schemaService(r).UpdateTransferSchedule(r.Context(), chi.URLParam(r, "id"), patch)
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sched)
}

func (h *TransferHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := schemaService(r).DeleteTransferSchedule(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeScheduleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunScheduleNow runs a schedule at once, exactly as the scheduler would: as
// its accounts, checked again, and recorded in its history. 200 when the
// destination was written, 422 with the same run object when it was not.
func (h *TransferHandler) RunScheduleNow(w http.ResponseWriter, r *http.Request) {
	sched, err := schemaService(r).GetTransferSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	if h.dbMgr == nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Transfers Unavailable",
			"this server cannot reach the solutions of a schedule")
		return
	}
	run, err := transfer.RunSchedule(r.Context(), h.dbMgr, currentSession(r).Database, sched,
		schema.ScheduleTriggerManual)
	switch {
	case errors.Is(err, transfer.ErrScheduleRunning):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Schedule Running", err.Error())
		return
	case err != nil && run == nil:
		telemetry.WriteInternalError(w, r, err)
		return
	}
	status := http.StatusOK
	if run.Status != schema.ScheduleRunCommitted {
		status = http.StatusUnprocessableEntity
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(viewOfRun(*run))
}

func (h *TransferHandler) ListScheduleRuns(w http.ResponseWriter, r *http.Request) {
	svc := schemaService(r)
	if _, err := svc.GetTransferSchedule(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeScheduleError(w, r, err)
		return
	}
	runs, err := svc.ListTransferScheduleRuns(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScheduleError(w, r, err)
		return
	}
	views := make([]scheduleRunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, viewOfRun(run))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(views)
}

func writeScheduleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, schema.ErrTransferScheduleNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Transfer Schedule Not Found", err.Error())
	case errors.Is(err, schema.ErrTransferScheduleExists):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Transfer Schedule Already Exists", err.Error())
	case errors.Is(err, schema.ErrInvalidTransferSchedule):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Transfer Schedule", err.Error())
	default:
		writeTransferError(w, r, err)
	}
}
