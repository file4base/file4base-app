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

// TransferHandler serves the transfer definitions of the solution the caller
// is signed in to, and runs them (#96).
//
// A transfer crosses two solutions, and a session reaches exactly the one
// database it signed in to. So running a transfer whose far end is another
// solution needs two authorizations: the session the request is made with,
// and a `peer_token` — a session token obtained by signing in to the other
// solution. The definition itself holds no credentials, and the server never
// derives authority for one solution from a session opened on another.
type TransferHandler struct {
	dbMgr    *dbal.MultiDatabaseManager
	sessions *auth.Store
}

func NewTransferHandler(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store) *TransferHandler {
	return &TransferHandler{dbMgr: dbMgr, sessions: sessions}
}

func (h *TransferHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/transfers", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/", h.ListTransfers)
		r.With(RequireAdmin, RequireDesignUnlocked).Post("/", h.CreateTransfer)
		r.Get("/{id}", h.GetTransfer)
		r.With(RequireAdmin, RequireDesignUnlocked).Put("/{id}", h.UpdateTransfer)
		r.With(RequireAdmin, RequireDesignUnlocked).Delete("/{id}", h.DeleteTransfer)
		r.Post("/{id}/run", h.RunTransfer)
	})
	h.registerScheduleRoutes(r)
}

// ListTransfers answers the solution's transfer definitions, by name.
func (h *TransferHandler) ListTransfers(w http.ResponseWriter, r *http.Request) {
	transfers, err := schemaService(r).ListTransfers(r.Context())
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(transfers)
}

// GetTransfer answers one definition, by id or by name.
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	def, err := resolveTransfer(r, chi.URLParam(r, "id"))
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(def)
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var in schema.TransferInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	def, err := schemaService(r).CreateTransfer(r.Context(), in, currentSession(r).UserID)
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(def)
}

func (h *TransferHandler) UpdateTransfer(w http.ResponseWriter, r *http.Request) {
	var in schema.TransferInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	current, err := resolveTransfer(r, chi.URLParam(r, "id"))
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	def, err := schemaService(r).UpdateTransfer(r.Context(), current.ID, in)
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(def)
}

func (h *TransferHandler) DeleteTransfer(w http.ResponseWriter, r *http.Request) {
	current, err := resolveTransfer(r, chi.URLParam(r, "id"))
	if err != nil {
		writeTransferError(w, r, err)
		return
	}
	if err := schemaService(r).DeleteTransfer(r.Context(), current.ID); err != nil {
		writeTransferError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TransferRunRequest is the body of a run.
type TransferRunRequest struct {
	// PeerToken is a session token for the end of the transfer that is not
	// this session's database. It is required whenever the two ends are
	// different solutions, and refused when they are the same one.
	PeerToken string `json:"peer_token,omitempty"`
}

// TransferRunResponse is what a run answers: the report, and, when the
// transfer was refused, why.
//
// The shape is the same whether the transfer committed or was refused, so a
// caller parses one object and reads `committed` to know what happened. The
// HTTP status says the same thing: 200 when the destination was written, 422
// when nothing was.
type TransferRunResponse struct {
	*transfer.Report
	Error string `json:"error,omitempty"`
}

// RunTransfer runs one definition.
func (h *TransferHandler) RunTransfer(w http.ResponseWriter, r *http.Request) {
	def, err := resolveTransfer(r, chi.URLParam(r, "id"))
	if err != nil {
		writeTransferError(w, r, err)
		return
	}

	var body TransferRunRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
				"Request body contains invalid JSON format")
			return
		}
	}

	ends, ok := h.authorizeEnds(w, r, def, strings.TrimSpace(body.PeerToken))
	if !ok {
		return
	}

	report, runErr := transfer.Run(r.Context(), def,
		ends.source.driver, ends.destination.driver,
		ends.source.database, ends.destination.database)

	response := TransferRunResponse{Report: report}
	status := http.StatusOK
	if runErr != nil {
		response.Error = runErr.Error()
		status = http.StatusUnprocessableEntity
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

// transferEnd is one authorized end of a run: the database, its driver, and
// the session that authorized it.
type transferEnd struct {
	database string
	driver   dbal.DatabaseDriver
	session  auth.Session
}

type transferEnds struct {
	source      transferEnd
	destination transferEnd
}

// authorizeEnds resolves and authorizes both ends of a transfer.
//
// The session the request was made with authorizes its own database. The
// other end, when it is another solution, is authorized by the peer token
// and by nothing else: there is no path by which one session's role reaches a
// second database. Each end is then checked for the capability and the table
// access the work needs — reading records out on the source, writing records
// in on the destination.
func (h *TransferHandler) authorizeEnds(
	w http.ResponseWriter, r *http.Request, def *schema.TransferDefinition, peerToken string,
) (transferEnds, bool) {
	var ends transferEnds

	local := currentSession(r)
	localDriver, _ := dbal.DriverFromContext(r.Context())
	localEnd := transferEnd{database: local.Database, driver: localDriver, session: local}

	sourceDB := def.Source.DatabaseOr(local.Database)
	destinationDB := def.Destination.DatabaseOr(local.Database)

	sameSolution := strings.EqualFold(sourceDB, destinationDB)
	switch {
	case sameSolution:
		if !strings.EqualFold(sourceDB, local.Database) {
			writeForbidden(w, r, "this transfer runs entirely inside the solution "+sourceDB+
				"; sign in to it and run the transfer there")
			return ends, false
		}
		if peerToken != "" {
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Peer Token Not Needed",
				"both ends of this transfer are the solution this session signed in to, "+
					"so it takes no peer_token")
			return ends, false
		}
		ends.source, ends.destination = localEnd, localEnd

	case strings.EqualFold(sourceDB, local.Database):
		peer, ok := h.resolvePeer(w, r, peerToken, destinationDB, "destination")
		if !ok {
			return ends, false
		}
		ends.source, ends.destination = localEnd, peer

	case strings.EqualFold(destinationDB, local.Database):
		peer, ok := h.resolvePeer(w, r, peerToken, sourceDB, "source")
		if !ok {
			return ends, false
		}
		ends.source, ends.destination = peer, localEnd

	default:
		// Neither end is this session's database. Running it would mean the
		// server acting on two solutions on the strength of a session opened
		// on a third, which is exactly what it must not do.
		writeForbidden(w, r,
			"this session is signed in to neither end of this transfer; sign in to the source "+
				"solution "+sourceDB+" or the destination solution "+destinationDB)
		return ends, false
	}

	if !h.endAllows(w, r, ends.source, schema.CapabilityBulkExport, def.Source.Table, false) {
		return ends, false
	}
	if !h.endAllows(w, r, ends.destination, schema.CapabilityBulkImport, def.Destination.Table, true) {
		return ends, false
	}
	return ends, true
}

// resolvePeer authorizes the far end of a transfer from its own session
// token. The token has to be a live session of that very database: a session
// of any other solution is refused, whatever role it holds.
func (h *TransferHandler) resolvePeer(
	w http.ResponseWriter, r *http.Request, token, database, side string,
) (transferEnd, bool) {
	var end transferEnd
	if token == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Peer Token Required",
			"the "+side+" of this transfer is the solution "+database+
				", so the request needs a peer_token: a session token obtained by signing in to it")
		return end, false
	}
	if h.sessions == nil || h.dbMgr == nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Transfers Unavailable",
			"this server cannot authorize the far end of a transfer")
		return end, false
	}

	sess, ok := h.sessions.Get(token)
	if !ok {
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Peer Token Rejected",
			"the peer_token is not a valid session; sign in to "+database+" again")
		return end, false
	}
	if !strings.EqualFold(sess.Database, database) {
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Peer Token Rejected",
			"the peer_token is a session of another solution; the "+side+
				" of this transfer is "+database)
		return end, false
	}

	driver, err := h.dbMgr.DriverFor(r.Context(), sess.Database)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable",
			"the solution "+database+" is not reachable")
		return end, false
	}

	// The peer account must still be in the state its session was opened
	// with, the same check every authenticated request makes. Without it a
	// revoked or demoted account could still write through a transfer.
	stamp, err := schema.NewService(driver).CurrentAccountStamp(r.Context(), sess.UserID)
	if err != nil || stamp != sess.Stamp {
		h.sessions.Revoke(token)
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Peer Token Rejected",
			"the account of the peer_token is no longer in the state it signed in with; "+
				"sign in to "+database+" again")
		return end, false
	}

	return transferEnd{database: sess.Database, driver: driver, session: sess}, true
}

// endAllows checks that one end's account may do the work the transfer asks
// of it: the capability its role holds, and its access to the table. It is
// the same rule a scheduled run applies to the accounts it runs as
// (transfer.CheckEnd), so a request and a schedule can never disagree about
// who may move what.
func (h *TransferHandler) endAllows(
	w http.ResponseWriter, r *http.Request, end transferEnd,
	capability, tableName string, writing bool,
) bool {
	err := transfer.CheckEnd(r.Context(), end.driver,
		transfer.Account{ID: end.session.UserID, Role: end.session.Role},
		end.database, capability, tableName, writing)
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

// resolveTransfer reads a definition by id, or by name when the path is a
// name rather than an id. A script step names the transfer it runs, because a
// name is what a developer writes and what survives a solution bundle being
// restored into another database with fresh ids.
func resolveTransfer(r *http.Request, idOrName string) (*schema.TransferDefinition, error) {
	svc := schemaService(r)
	idOrName = strings.TrimSpace(idOrName)
	def, err := svc.GetTransfer(r.Context(), idOrName)
	if err == nil {
		return def, nil
	}
	if !errors.Is(err, schema.ErrTransferNotFound) {
		return nil, err
	}
	return svc.GetTransferByName(r.Context(), idOrName)
}

func writeTransferError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, schema.ErrTransferNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Transfer Not Found", err.Error())
	case errors.Is(err, schema.ErrTransferExists):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Transfer Already Exists", err.Error())
	case errors.Is(err, schema.ErrInvalidTransfer):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Transfer", err.Error())
	default:
		writeSchemaError(w, r, err)
	}
}
