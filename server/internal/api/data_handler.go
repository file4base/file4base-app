package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/extsource"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/go-chi/chi/v5"
)

// DataHandler serves record CRUD and Find Mode queries on the user tables of
// the database the caller is signed in to.
//
// Only tables registered in the system catalog are reachable; sys_* tables are
// not. Owners and admins have full access. For regular users the access level
// is derived from their layout permissions (see schema.Service.TableAccess).
type DataHandler struct{}

func NewDataHandler() *DataHandler {
	return &DataHandler{}
}

func (h *DataHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/data/{table}", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/", h.ListRows)
		r.Post("/", h.InsertRow)
		r.Post("/find", h.FindRows)
		r.Post("/summary", h.SummarizeRows)
		r.Post("/evaluate", h.EvaluateExpression)
		// Bringing records in or out in bulk is a capability an owner can
		// withhold from a role (#39), enforced here rather than in a client.
		r.With(RequireCapability(schema.CapabilityBulkImport)).Post("/import/preview", h.PreviewImport)
		r.With(RequireCapability(schema.CapabilityBulkImport)).Post("/import", h.ImportRows)
		r.With(RequireCapability(schema.CapabilityBulkExport)).Post("/export", h.ExportRows)
		r.Post("/related", h.ListRelatedRowsBatch)
		r.Post("/relookup", h.RelookupRows)
		r.Get("/serials/{field}", h.GetNextSerialValue)
		r.Put("/serials/{field}", h.SetNextSerialValue)
		r.Get("/{id}", h.GetRow)
		r.Post("/{id}/duplicate", h.DuplicateRow)
		r.Get("/{id}/related", h.ListRelatedRows)
		r.Post("/{id}/related", h.CreateRelatedRow)
		r.Put("/{id}", h.UpdateRow)
		r.Delete("/{id}", h.DeleteRow)
	})
}

// authorize resolves the data service for the request and enforces the
// caller's access level on the table. It writes the error response itself
// and returns ok=false when the request must not proceed.
func (h *DataHandler) authorize(w http.ResponseWriter, r *http.Request, table string, write bool) (*data.Service, bool) {
	return h.authorizeAction(w, r, table, write, "")
}

func (h *DataHandler) authorizeAction(w http.ResponseWriter, r *http.Request, table string, write bool, action string) (*data.Service, bool) {
	driver, _ := dbal.DriverFromContext(r.Context())
	sess := currentSession(r)

	if !sess.IsAdmin() {
		privileges, err := recordPrivileges(r.Context(), schema.NewService(driver), sess.UserID, table)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return nil, false
		}
		if refusal := privileges.refusal(write, action); refusal != "" {
			// The records of another solution are refused as the account
			// the session group holds there, which the message names, with
			// the file reference and the solution (#107).
			if external := externalOrigin(r); external != nil && write {
				if action == "" {
					action = data.ActionEdit
				}
				refusal = external.Wrap(&data.PermissionError{Account: sess.Username, Table: table, Action: action}).Error()
			}
			writeForbidden(w, r, refusal)
			return nil, false
		}
	}

	return callerDataService(r), true
}

// RecordPrivileges are what a session may do with the records of a table:
// what the data routes allow it, and what a client enables.
type RecordPrivileges struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
}

// allPrivileges are an administrator's.
var allPrivileges = RecordPrivileges{Read: true, Create: true, Edit: true, Delete: true}

// recordPrivileges works out a non-administrator's privileges on a table:
// the access level of the table, and, when it is read-write, the record
// privileges its explicit permission row grants.
func recordPrivileges(ctx context.Context, svc *schema.Service, userID, table string) (RecordPrivileges, error) {
	level, err := svc.TableAccess(ctx, userID, table)
	if err != nil {
		return RecordPrivileges{}, err
	}
	switch level {
	case schema.AccessNone:
		return RecordPrivileges{}, nil
	case schema.AccessReadWrite:
	default:
		return RecordPrivileges{Read: true}, nil
	}
	tp, err := svc.TablePermissionFor(ctx, userID, table)
	if err != nil || tp == nil {
		return allPrivileges, nil
	}
	return RecordPrivileges{Read: true, Create: tp.CanCreate, Edit: tp.CanEdit, Delete: tp.CanDelete}, nil
}

// sessionPrivileges are a session's privileges on a table of its solution.
func sessionPrivileges(ctx context.Context, driver dbal.DatabaseDriver, sess auth.Session, table string) (RecordPrivileges, error) {
	if sess.IsAdmin() {
		return allPrivileges, nil
	}
	return recordPrivileges(ctx, schema.NewService(driver), sess.UserID, table)
}

// allows reports whether the privileges cover a write action.
func (p RecordPrivileges) allows(action string) bool {
	switch action {
	case data.ActionCreate:
		return p.Create
	case data.ActionEdit:
		return p.Edit
	case data.ActionDelete:
		return p.Delete
	}
	return p.Create || p.Edit || p.Delete
}

// refusal says why a request is refused, or is empty when it may proceed.
// A write with no action needs the read-write access level only.
func (p RecordPrivileges) refusal(write bool, action string) string {
	switch {
	case !p.Read:
		return "you do not have access to this table"
	case write && !p.Create && !p.Edit && !p.Delete:
		return "you have read-only access to this table"
	case !write || action == "" || p.allows(action):
		return ""
	case action == data.ActionCreate:
		return "you do not have permission to create records in this table"
	case action == data.ActionEdit:
		return "you do not have permission to modify records in this table"
	}
	return "you do not have permission to delete records in this table"
}

// callerDataService is the data service of a request's database, reading
// related records as the caller: a calculation or a value list that reads a
// table they may not read gets <No Access>, not its data. A read through an
// occurrence of another solution's table goes to that solution, as the
// account the caller's session group holds there (#107).
func callerDataService(r *http.Request) *data.Service {
	driver, _ := dbal.DriverFromContext(r.Context())
	svc := dataServiceAs(driver, currentSession(r))
	if peers := peersOf(r); peers != nil {
		svc.WithPeers(peers)
	}
	return svc
}

// dataServiceAs is the data service of a solution reading as one of its
// sessions, with that session's access: what it may read, and, for the
// records of another solution a request of this one writes (#107), what it
// may create and delete.
func dataServiceAs(driver dbal.DatabaseDriver, sess auth.Session) *data.Service {
	dataSvc := data.NewService(driver).WithDatabase(sess.Database).WithAccount(sess.Username)
	if !sess.IsAdmin() {
		schemaSvc, userID, username := schema.NewService(driver), sess.UserID, sess.Username
		dataSvc.WithReadAccess(func(ctx context.Context, table string) (bool, error) {
			level, err := schemaSvc.TableAccess(ctx, userID, table)
			return level != schema.AccessNone, err
		})
		dataSvc.WithWriteAccess(func(ctx context.Context, table, action string) error {
			privileges, err := recordPrivileges(ctx, schemaSvc, userID, table)
			if err != nil {
				return err
			}
			if !privileges.allows(action) {
				return &data.PermissionError{Account: username, Table: table, Action: action}
			}
			return nil
		})
	}
	return dataSvc
}

// EvaluateExpressionBody is what the Data Viewer asks (#50).
type EvaluateExpressionBody struct {
	// Expressions are evaluated in the order they are given, each on its
	// own: one that does not parse does not stop the others, because a
	// viewer shows a list and a typo in one line must not blank the rest.
	Expressions []string `json:"expressions"`

	// Expression is the single-expression form, for a caller that watches one.
	Expression string `json:"expression"`

	// RecordID is the record to evaluate against. Without one the
	// expressions are evaluated against no record, so every field reads as
	// empty and a formula like UPPER("ab") still answers.
	RecordID string `json:"record_id"`

	// ResultType is how to read the answer: Text, Number, Date, Timestamp or
	// Boolean. Text when it is left out.
	ResultType string `json:"result_type"`

	// Variables are a script's $ and $$ variables, by name ("$total"), for
	// a script step evaluating a formula. The variables an expression sets
	// with Let come back in its result.
	Variables map[string]any `json:"variables,omitempty"`

	// Get answers the Get functions only the client knows, by selector
	// ("ScriptParameter", "LayoutName", …).
	Get map[string]any `json:"get,omitempty"`
}

// EvaluateExpression computes expressions against a record with the engine
// the calculation fields use, so the Data Viewer and the field agree (#50).
//
//	POST /api/v1/data/{table}/evaluate
//
// It only reads: evaluating a formula is all the engine can do, and the
// caller needs no more than read access to the table.
func (h *DataHandler) EvaluateExpression(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body EvaluateExpressionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	expressions := body.Expressions
	if len(expressions) == 0 && strings.TrimSpace(body.Expression) != "" {
		expressions = []string{body.Expression}
	}
	if len(expressions) == 0 {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Nothing to Evaluate",
			"give an expression to evaluate")
		return
	}
	if len(expressions) > maxWatchedExpressions {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Too Many Expressions",
			fmt.Sprintf("at most %d expressions are evaluated in one request", maxWatchedExpressions))
		return
	}

	results := make([]map[string]interface{}, 0, len(expressions))
	for _, expression := range expressions {
		evaluation, err := svc.EvaluateExpressionIn(r.Context(), table, body.RecordID, expression, body.ResultType,
			data.EvaluationEnv{Variables: body.Variables, Get: body.Get})
		if err != nil {
			// A record that is not there, or a table that is not reachable,
			// is about the request rather than about one expression.
			if errors.Is(err, data.ErrTableNotFound) || errors.Is(err, data.ErrRecordNotFound) {
				writeDataError(w, r, http.StatusNotFound, "Not Found", err)
				return
			}
			results = append(results, map[string]interface{}{
				"expression": expression,
				"error":      strings.TrimPrefix(err.Error(), "invalid expression: "),
			})
			continue
		}
		result := map[string]interface{}{
			"expression":     evaluation.Expression,
			"value":          evaluation.Value,
			"text":           evaluation.Text,
			"result_type":    evaluation.ResultType,
			"fields":         evaluation.Fields,
			"unknown_fields": evaluation.UnknownFields,
			"record_id":      evaluation.RecordID,
		}
		if len(evaluation.Variables) > 0 {
			result["variables"] = evaluation.Variables
		}
		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

// maxWatchedExpressions bounds one request, since each one reads the record
// again.
const maxWatchedExpressions = 50

// writeDataError maps data-layer errors to RFC 9457 responses.
func writeDataError(w http.ResponseWriter, r *http.Request, fallbackStatus int, title string, err error) {
	switch {
	// First: it wraps the error that stopped the delete, which says why but
	// not that another solution's records are already gone.
	case errors.Is(err, data.ErrPartialDelete):
		writePartialDelete(w, r, err)
	case errors.Is(err, data.ErrNoPermission):
		writeForbidden(w, r, err.Error())
	case errors.Is(err, data.ErrTableNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Table Not Found", err.Error())
	case errors.Is(err, data.ErrRecordNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Record Not Found", err.Error())
	case errors.Is(err, data.ErrInvalidExpression):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Expression", err.Error())
	case errors.Is(err, dataio.ErrSourceTooLarge):
		telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Source File Too Large", err.Error())
	case errors.Is(err, dataio.ErrInvalidSource):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Source File", err.Error())
	case errors.Is(err, data.ErrImport):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Import Failed", err.Error())
	case errors.Is(err, data.ErrInvalidSummary):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Summary Field", err.Error())
	case errors.Is(err, data.ErrExternalUnavailable):
		writeExternalUnavailable(w, r, err)
	case errors.Is(err, data.ErrRelationshipNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Relationship Not Found", err.Error())
	case errors.Is(err, data.ErrCreationNotAllowed):
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Creation Not Allowed", err.Error())
	case errors.Is(err, data.ErrNoMatchValue):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "No Match Value", err.Error())
	case errors.Is(err, data.ErrProhibitedModification):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Prohibited Modification", err.Error())
	case errors.Is(err, data.ErrNotSerial):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Not a Serial Number Field", err.Error())
	case errors.Is(err, data.ErrUnknownField):
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Unknown Field", err.Error())
	case errors.Is(err, data.ErrUnstoredField):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Unstored Calculation", err.Error())
	case errors.Is(err, data.ErrTooManyRelated):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Too Many Related Records", err.Error())
	case errors.Is(err, data.ErrFindTooBroad):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Find Too Broad", err.Error())
	case errors.Is(err, validation.ErrValidation):
		var ve *validation.Error
		errors.As(err, &ve)
		telemetry.WriteValidationProblem(w, r, ve.Message, []telemetry.InvalidParam{{Name: ve.Field, Reason: ve.Rule, Overridable: ve.Overridable}})
	case errors.Is(err, data.ErrInvalidValue):
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Value", err.Error())
	default:
		telemetry.WriteProblem(w, r, fallbackStatus, title, err.Error())
	}
}

// parseSortOrder reads the `sort` query parameter, which may be repeated or
// comma-separated and orders by each field in turn. A leading "-" sorts that
// field descending:
//
//	?sort=company&sort=-fee_paid
//	?sort=company,-fee_paid
//
// It supersedes sort_by/sort_asc, which stay for callers that only ever sort by
// one field.
func parseSortOrder(values []string) []data.SortField {
	var order []data.SortField
	for _, value := range values {
		order = append(order, data.ParseSortSpec(value)...)
	}
	return order
}

func (h *DataHandler) ListRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	sortBy := r.URL.Query().Get("sort_by")
	sortAsc := r.URL.Query().Get("sort_asc") != "false"

	rows, err := svc.ListRows(r.Context(), table, data.QueryOptions{
		Limit:   limit,
		Offset:  offset,
		SortBy:  sortBy,
		SortAsc: sortAsc,
		Sort:    parseSortOrder(r.URL.Query()["sort"]),
	})
	if err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}
	if err := svc.AttachSourceText(r.Context(), table, rows); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}
	if err := svc.AttachContainerMeta(r.Context(), table, rows); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (h *DataHandler) InsertRow(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorizeAction(w, r, table, true, "create")
	if !ok {
		return
	}

	record, err := decodeRecord(r)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	inserted, err := svc.InsertRow(entryContext(r), table, record)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Insert Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(inserted)
}

func (h *DataHandler) GetRow(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	id := chi.URLParam(r, "id")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	row, err := svc.GetRow(r.Context(), table, id)
	if err != nil {
		writeDataError(w, r, http.StatusNotFound, "Record Not Found", err)
		return
	}
	if err := svc.AttachSourceText(r.Context(), table, []map[string]interface{}{row}); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}
	if err := svc.AttachContainerMeta(r.Context(), table, []map[string]interface{}{row}); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(row)
}

func (h *DataHandler) UpdateRow(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	id := chi.URLParam(r, "id")
	svc, ok := h.authorizeAction(w, r, table, true, "edit")
	if !ok {
		return
	}

	updates, err := decodeRecord(r)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	updated, err := svc.UpdateRow(entryContext(r), table, id, updates)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Update Error", err)
		return
	}
	if err := svc.AttachSourceText(r.Context(), table, []map[string]interface{}{updated}); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}
	if err := svc.AttachContainerMeta(r.Context(), table, []map[string]interface{}{updated}); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *DataHandler) DeleteRow(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	id := chi.URLParam(r, "id")
	svc, ok := h.authorizeAction(w, r, table, true, "delete")
	if !ok {
		return
	}

	if err := svc.DeleteRow(r.Context(), table, id); err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Delete Error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type FindRequestBody struct {
	Requests []data.FindRequest `json:"requests"`
	Options  data.QueryOptions  `json:"options"`
}

func (h *DataHandler) FindRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body FindRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if !h.authorizeFindRelations(w, r, svc, table, body.Requests) {
		return
	}

	results, err := svc.ExecuteFind(r.Context(), table, body.Requests, body.Options)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Find Query Error", err)
		return
	}
	if err := svc.AttachSourceText(r.Context(), table, results); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}
	if err := svc.AttachContainerMeta(r.Context(), table, results); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

// ImportBody is what the import endpoints take: the file itself, base64 so it
// travels in JSON beside the settings, and how to read it.
type ImportBody struct {
	FileName string `json:"file_name"`

	// The file, base64 encoded (standard alphabet, padding optional).
	Content string `json:"content"`

	Format        string `json:"format"`
	HasHeader     bool   `json:"has_header"`
	Delimiter     string `json:"delimiter"`
	Sheet         string `json:"sheet"`
	RecordElement string `json:"record_element"`

	// DataSource reads the records from another SQL database instead of a
	// file (#47). The source is one an owner registered: the request names
	// it, never a host of its own.
	DataSource *ExternalSourceBody `json:"data_source,omitempty"`

	// Only used by the import itself, not by the preview.
	Options data.ImportOptions `json:"options"`
}

// ExternalSourceBody says what to read from a registered data source.
type ExternalSourceBody struct {
	ID    string `json:"id"`
	Table string `json:"table"`
	Query string `json:"query"`

	// Password is used for this request only: it is never stored and never
	// logged. Left out, the source's PasswordEnv is used.
	Password string `json:"password"`

	// Timeout in seconds for the read, clamped by the reader.
	TimeoutSeconds int `json:"timeout_seconds"`
}

// errSourceQueryOwnerOnly refuses a statement of the caller's own against a
// data source to anyone but an owner.
var errSourceQueryOwnerOnly = errors.New("only an owner can read a data source with a statement of their own; import a whole table instead")

// importSource reads what an import is to bring in: the file in the body, or
// the registered data source it names (#47).
func importSource(r *http.Request, body ImportBody) (*dataio.Table, error) {
	if body.DataSource != nil && strings.TrimSpace(body.DataSource.ID) != "" {
		return readExternalSource(r, body)
	}
	return parseSource(body)
}

// writeImportSourceError maps the reading failures of both kinds of source.
func writeImportSourceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, schema.ErrDataSourceNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Data Source Not Found", err.Error())
	case errors.Is(err, errSourceQueryOwnerOnly):
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Owner Only", err.Error())
	case errors.Is(err, extsource.ErrUnsafeStatement):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Unsafe Statement", err.Error())
	case errors.Is(err, extsource.ErrUnsupportedEngine):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Unsupported Engine", err.Error())
	case errors.Is(err, extsource.ErrConnection):
		telemetry.WriteProblem(w, r, http.StatusBadGateway, "Data Source Unreachable", err.Error())
	default:
		writeDataError(w, r, http.StatusUnprocessableEntity, "Invalid Source File", err)
	}
}

// readExternalSource reads the records out of a registered data source.
//
// The request names a source the owner registered, never a host of its own,
// so an import cannot be pointed at an arbitrary machine. The connection is
// opened for this read and closed again.
func readExternalSource(r *http.Request, body ImportBody) (*dataio.Table, error) {
	ref := body.DataSource
	// The source reads with the credentials its owner stored. Importing one
	// of its tables is what the bulk import capability grants; running a
	// statement of one's own with those credentials is the owner's call.
	if strings.TrimSpace(ref.Query) != "" && currentSession(r).Role != auth.RoleOwner {
		return nil, errSourceQueryOwnerOnly
	}
	source, err := schemaService(r).GetDataSource(r.Context(), strings.TrimSpace(ref.ID))
	if err != nil {
		return nil, err
	}

	password := source.Password(ref.Password)
	opts := extsource.ReadOptions{
		Table:   ref.Table,
		Query:   ref.Query,
		Limits:  dataio.DefaultLimits(),
		Timeout: time.Duration(ref.TimeoutSeconds) * time.Second,
	}
	return extsource.Read(r.Context(), source.Connection(), password, opts)
}

// parseSource decodes and reads the file an import body carries.
func parseSource(body ImportBody) (*dataio.Table, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.Content))
	if err != nil {
		// A client that left the padding off still gets read.
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(body.Content))
		if err != nil {
			return nil, fmt.Errorf("%w: the file could not be decoded: %v", dataio.ErrInvalidSource, err)
		}
	}

	format := dataio.Format(strings.ToLower(strings.TrimSpace(body.Format)))
	if format == "" {
		format = dataio.FormatFromName(body.FileName)
	}

	return dataio.Parse(raw, dataio.Options{
		Format:        format,
		HasHeader:     body.HasHeader,
		Delimiter:     body.Delimiter,
		Sheet:         body.Sheet,
		RecordElement: body.RecordElement,
	})
}

// PreviewImport reads a file and reports what is in it, without writing
// anything (#38).
//
//	POST /api/v1/data/{table}/import/preview
//
// It answers the columns the file has, the sheets a workbook holds, and the
// first rows, so the import dialog can show what is about to be brought in
// and match the columns to the fields.
func (h *DataHandler) PreviewImport(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	// Previewing writes nothing, but it reads a file into a table the caller
	// must be allowed to write to, so it asks for write access.
	if _, ok := h.authorize(w, r, table, true); !ok {
		return
	}

	var body ImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	source, err := importSource(r, body)
	if err != nil {
		writeImportSourceError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"columns":   source.Columns,
		"sheets":    source.Sheets,
		"rows":      source.Sample(20),
		"row_count": len(source.Rows),
		"truncated": source.Truncated,
	})
}

// ImportRows brings the records of a file into a table (#38).
//
//	POST /api/v1/data/{table}/import
//
// It is one transaction: the first row that cannot be read or stored takes the
// whole file with it, and the answer says which row it was.
func (h *DataHandler) ImportRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, true)
	if !ok {
		return
	}

	var body ImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	source, err := importSource(r, body)
	if err != nil {
		writeImportSourceError(w, r, err)
		return
	}

	report, err := svc.ImportRecords(r.Context(), table, source, body.Options)
	if err != nil {
		writeDataError(w, r, http.StatusUnprocessableEntity, "Import Failed", err)
		return
	}

	report.Rows = len(source.Rows)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"rows":      report.Rows,
		"added":     report.Added,
		"updated":   report.Updated,
		"skipped":   report.Skipped,
		"fields":    report.Fields,
		"truncated": source.Truncated,
	})
}

// ExportBody is what the export endpoint takes.
type ExportBody struct {
	Format string `json:"format"`

	// The sheet name of a workbook, and the stem of the suggested file name.
	Name string `json:"name"`

	data.ExportOptions
}

// ExportRows writes a found set out as CSV, tab-separated text or a workbook
// (#38).
//
//	POST /api/v1/data/{table}/export
//
// The body takes the same find requests a find does, so what is exported is
// what was found.
func (h *DataHandler) ExportRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body ExportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	format := dataio.Format(strings.ToLower(strings.TrimSpace(body.Format)))
	if format == "" {
		format = dataio.FormatCSV
	}
	if format == dataio.FormatXML {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Format Not Supported",
			"records are exported as csv, tsv or xlsx; XML is read but not written")
		return
	}

	source, err := svc.ExportRecords(r.Context(), table, body.ExportOptions)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Export Failed", err)
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = table
	}

	var out []byte
	switch format {
	case dataio.FormatXLSX:
		out, err = dataio.WriteXLSX(source, name)
	case dataio.FormatTSV:
		out, err = dataio.WriteDelimited(source, '\t', false)
	default:
		// With the byte order mark, so Excel opens it as UTF-8 and accented
		// names survive the round trip.
		out, err = dataio.WriteDelimited(source, ',', true)
	}
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", dataio.ContentType(format))
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name+dataio.Extension(format)))
	w.Header().Set("X-File4Base-Rows", fmt.Sprint(len(source.Rows)))
	_, _ = w.Write(out)
}

// SummarizeRows works out the figures of a report: what each summary field
// comes to over the found set, and over each group of it (#32).
//
//	POST /api/v1/data/{table}/summary
//
// The found set is given as find requests, exactly as `POST .../find` takes
// them, so a report totals the records the find returned. An empty `requests`
// summarizes every record of the table.
func (h *DataHandler) SummarizeRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body data.SummaryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if !h.authorizeFindRelations(w, r, svc, table, body.Requests) {
		return
	}

	result, err := svc.Summarize(r.Context(), table, body)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Summary Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ListRelatedRows returns the records of a related table that match the record
// in hand: what a portal shows, and what a related field on a layout reads its
// value from (#36).
//
//	GET /api/v1/data/{table}/{id}/related?relationship=<id>&occurrence=<id>
//
// `occurrence` names the side whose records are wanted. It may be left out
// unless the relationship joins a table to itself, where there is no other
// side to infer. `side` (`left` or `right`) names the end instead, which a
// relationship joining an occurrence to itself needs for its left end.
func (h *DataHandler) ListRelatedRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}
	h.listRelated(w, r, svc, data.LocalOrigin(table))
}

// listRelated answers the records related to the record {id} of from.
func (h *DataHandler) listRelated(w http.ResponseWriter, r *http.Request, svc *data.Service, from data.Origin) {
	relationship := r.URL.Query().Get("relationship")
	if strings.TrimSpace(relationship) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Missing Relationship",
			"the relationship to follow must be given as ?relationship=<id>")
		return
	}
	occurrence := r.URL.Query().Get("occurrence")
	end, ok := requestRelationEnd(w, r, r.URL.Query().Get("side"))
	if !ok {
		return
	}

	// The caller needs access to the table the records come from as well.
	if !h.authorizeRelatedFrom(w, r, svc, from, relationship, occurrence, end, false, "") {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	rows, err := svc.RelatedRowsFrom(r.Context(), from, chi.URLParam(r, "id"), relationship, occurrence, data.QueryOptions{
		Limit:  limit,
		Offset: offset,
		Sort:   parseSortOrder(r.URL.Query()["sort"]),
		End:    end,
	})
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

// RelatedBatchBody asks for the records related to several records at once.
type RelatedBatchBody struct {
	Relationship string           `json:"relationship"`
	Occurrence   string           `json:"occurrence"`
	Side         string           `json:"side,omitempty"`
	RecordIDs    []string         `json:"record_ids"`
	Limit        int              `json:"limit"`
	Offset       int              `json:"offset"`
	Sort         []data.SortField `json:"sort,omitempty"`
}

// ListRelatedRowsBatch returns, for each record named, the records related
// to it: what a list of records each showing a related field or a portal
// reads, in one request and without a statement per record.
//
//	POST /api/v1/data/{table}/related
//
// The answer is {"records": {"<record id>": [related records]}}, every record
// asked for included. Each record gets what GET .../{id}/related gives it.
func (h *DataHandler) ListRelatedRowsBatch(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}
	h.listRelatedBatch(w, r, svc, data.LocalOrigin(table))
}

func (h *DataHandler) listRelatedBatch(w http.ResponseWriter, r *http.Request, svc *data.Service, from data.Origin) {
	var body RelatedBatchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if strings.TrimSpace(body.Relationship) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Missing Relationship",
			"the relationship to follow must be given as \"relationship\"")
		return
	}
	if len(body.RecordIDs) > data.MaxRelatedBatch {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Batch Too Large", data.ErrBatchTooLarge.Error())
		return
	}
	end, ok := requestRelationEnd(w, r, body.Side)
	if !ok {
		return
	}
	if !h.authorizeRelatedFrom(w, r, svc, from, body.Relationship, body.Occurrence, end, false, "") {
		return
	}
	records, err := svc.RelatedRowsBatch(r.Context(), from, body.RecordIDs, body.Relationship, body.Occurrence,
		data.QueryOptions{Limit: body.Limit, Offset: body.Offset, Sort: body.Sort, End: end})
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"records": records})
}

// CreateRelatedRow creates a record in a related table and fills its match
// field, which is what typing into the last row of a portal does.
//
//	POST /api/v1/data/{table}/{id}/related?relationship=<id>&occurrence=<id>[&side=left|right]
//
// It is refused with 403 unless the relationship allows creation on the side
// created in. A related
// table of another solution is written there, as the account the session
// group holds in it, which needs its create privilege (#107).
func (h *DataHandler) CreateRelatedRow(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}
	h.createRelated(w, r, svc, data.LocalOrigin(table))
}

// createRelated creates a record related to the record {id} of from.
func (h *DataHandler) createRelated(w http.ResponseWriter, r *http.Request, svc *data.Service, from data.Origin) {
	relationship := r.URL.Query().Get("relationship")
	if strings.TrimSpace(relationship) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Missing Relationship",
			"the relationship to follow must be given as ?relationship=<id>")
		return
	}
	occurrence := r.URL.Query().Get("occurrence")
	end, ok := requestRelationEnd(w, r, r.URL.Query().Get("side"))
	if !ok {
		return
	}

	// The record is created in the related table, so that is where the
	// create privilege has to hold.
	if !h.authorizeRelatedFrom(w, r, svc, from, relationship, occurrence, end, true, data.ActionCreate) {
		return
	}

	record, err := decodeRecord(r)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	inserted, err := svc.CreateRelatedRowToward(r.Context(), from, chi.URLParam(r, "id"), relationship, occurrence, end, record)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Insert Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(inserted)
}

// authorizeFindRelations enforces the caller's access on every table a find
// reaches through a relationship (#46): a criterion on a related field reads
// the related table, so a user who cannot see that table cannot search it
// sideways either.
func (h *DataHandler) authorizeFindRelations(
	w http.ResponseWriter, r *http.Request, svc *data.Service, table string, requests []data.FindRequest,
) bool {
	if refuseRelatedCriteria(w, r, requests) {
		return false
	}
	seen := map[string]struct{}{}
	for _, req := range requests {
		for _, crit := range req.Criteria {
			relationship := strings.TrimSpace(crit.RelationshipID)
			if relationship == "" {
				continue
			}
			key := relationship + "\x00" + strings.TrimSpace(crit.Occurrence)
			if _, done := seen[key]; done {
				continue
			}
			seen[key] = struct{}{}
			if !h.authorizeRelated(w, r, svc, table, relationship, strings.TrimSpace(crit.Occurrence), false) {
				return false
			}
		}
	}
	return true
}

// authorizeRelated enforces the caller's access level on the table at the far
// end of a relationship. It writes the error response itself and returns false
// when the request must not proceed.
func (h *DataHandler) authorizeRelated(
	w http.ResponseWriter, r *http.Request, svc *data.Service,
	table, relationship, occurrence string, write bool,
) bool {
	return h.authorizeRelatedFrom(w, r, svc, data.LocalOrigin(table), relationship, occurrence, "", write, "")
}

// requestRelationEnd reads the end of a relationship a request names, and
// answers 400 itself when it is neither left nor right.
func requestRelationEnd(w http.ResponseWriter, r *http.Request, text string) (data.RelationEnd, bool) {
	end, err := data.ParseRelationEnd(text)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Side", err.Error())
		return "", false
	}
	return end, true
}

func (h *DataHandler) authorizeRelatedFrom(
	w http.ResponseWriter, r *http.Request, svc *data.Service,
	from data.Origin, relationship, occurrence string, end data.RelationEnd, write bool, action string,
) bool {
	rel, err := svc.Relationship(r.Context(), relationship)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return false
	}
	_, target, err := rel.SidesToward(from, occurrence, end)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return false
	}
	if target.External != nil {
		// The records are another solution's, read and written as the
		// account the session group holds there: the data service checks
		// that account's access, never this one's (#107).
		return true
	}
	_, ok := h.authorizeAction(w, r, target.TableName, write, action)
	return ok
}

// decodeRecord decodes a record body keeping every JSON number exactly as
// written: numbers are passed to the database as their decimal text, never
// through float64, so NUMBER fields do not lose precision (issue #18).
func decodeRecord(r *http.Request) (map[string]interface{}, error) {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var record map[string]interface{}
	if err := dec.Decode(&record); err != nil {
		return nil, err
	}
	for k, v := range record {
		if n, ok := v.(json.Number); ok {
			record[k] = n.String()
		}
	}
	return record, nil
}
