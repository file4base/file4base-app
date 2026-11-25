package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/mail"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// Send Mail through SMTP (#133).
//
// Server administrators keep the SMTP profiles: a server-wide one, and one
// per solution that takes precedence over it. A profile never holds a
// password: it names an environment variable of the server (always
// FILE4BASE_SMTP_…) that holds it, so the password is in no API response, no
// control store row, no solution file and no log.
//
// Any signed-in account may send through its solution's profile, as any
// FileMaker user may run a script with Send Mail; the step's addresses,
// subject and message are evaluated by the client, and the server checks
// them (no line break in a header, at most mail.MaxRecipients recipients,
// attachments of at most mail.MaxAttachmentBytes) and counts what each
// account sends against the profile's hourly limit.

const (
	// maxMailRequestBytes bounds a send request: the attachments travel in
	// it, base64-encoded.
	maxMailRequestBytes = 32 << 20
	// maxMailBatch is the most messages one request sends (one per record
	// of a found set is sent in batches of at most this many).
	maxMailBatch = 100
)

// mailProfileView is a profile as the API shows it: never with a password,
// only with whether the variable it names is set on the server.
type mailProfileView struct {
	Solution string `json:"solution"`
	mail.Profile
	PasswordAvailable bool      `json:"password_available"`
	UpdatedBy         string    `json:"updated_by,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func profileView(rec control.MailProfileRecord) (mailProfileView, error) {
	var p mail.Profile
	if err := json.Unmarshal(rec.Profile, &p); err != nil {
		return mailProfileView{}, err
	}
	return mailProfileView{
		Solution:          rec.Database,
		Profile:           p,
		PasswordAvailable: p.PasswordAvailable(),
		UpdatedBy:         rec.UpdatedBy,
		UpdatedAt:         rec.UpdatedAt,
	}, nil
}

// mailLimiter counts the messages each account sent in the last hour.
type mailLimiter struct {
	mu   sync.Mutex
	sent map[string][]time.Time
}

// reserve takes n messages from the account's allowance, or says in how
// long it will have room for them.
func (l *mailLimiter) reserve(key string, n, perHour int, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sent == nil {
		l.sent = map[string][]time.Time{}
	}
	cutoff := now.Add(-time.Hour)
	times := l.sent[key]
	for len(times) > 0 && !times[0].After(cutoff) {
		times = times[1:]
	}
	if len(times)+n > perHour {
		l.sent[key] = times
		if n > perHour || len(times) == 0 {
			return false, time.Hour
		}
		// Room comes back as the oldest messages leave the window.
		idx := len(times) + n - perHour - 1
		return false, times[idx].Sub(cutoff)
	}
	for i := 0; i < n; i++ {
		times = append(times, now)
	}
	l.sent[key] = times
	return true, 0
}

// release gives back the messages that were reserved and not sent.
func (l *mailLimiter) release(key string, n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	times := l.sent[key]
	if n > len(times) {
		n = len(times)
	}
	l.sent[key] = times[:len(times)-n]
}

// registerMailAdminRoutes mounts the profile administration (server
// administrators only).
func (h *ServerAdminHandler) registerMailAdminRoutes(r chi.Router) {
	r.Get("/mail-profiles", h.ListMailProfiles)
	r.Put("/mail-profiles", h.PutMailProfile)
	r.Delete("/mail-profiles", h.DeleteMailProfile)
	r.Post("/mail-profiles/test", h.TestMailProfile)
}

// registerMailRoutes mounts what scripts use (any signed-in account).
func (h *ServerAdminHandler) registerMailRoutes(r chi.Router) {
	r.Route("/api/v1/mail", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/status", h.GetMailStatus)
		r.Post("/send", h.SendMail)
	})
}

// ListMailProfiles returns every SMTP profile, without secrets.
func (h *ServerAdminHandler) ListMailProfiles(w http.ResponseWriter, r *http.Request) {
	recs, err := h.controlStore.ListMailProfiles(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	views := make([]mailProfileView, 0, len(recs))
	for _, rec := range recs {
		v, err := profileView(rec)
		if err != nil {
			continue
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"profiles":            views,
		"password_env_prefix": mail.PasswordEnvPrefix,
	})
}

type putMailProfileRequest struct {
	Solution string `json:"solution"`
	mail.Profile
}

// PutMailProfile stores the server-wide profile (no solution) or the
// profile of one solution, named by its database.
func (h *ServerAdminHandler) PutMailProfile(w http.ResponseWriter, r *http.Request) {
	var req putMailProfileRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}
	database := strings.TrimSpace(req.Solution)
	if database != "" && !h.databaseExists(r.Context(), database) {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Unknown database", fmt.Sprintf("no database is named %q", database))
		return
	}
	profile, err := req.Profile.Normalize()
	if err != nil {
		telemetry.WriteValidationProblem(w, r, err.Error(), []telemetry.InvalidParam{{Name: "profile", Reason: err.Error()}})
		return
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	sess := currentSession(r)
	if err := h.controlStore.PutMailProfile(r.Context(), control.MailProfileRecord{
		Database: database, Profile: raw, UpdatedBy: sess.Username,
	}); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	h.recordAudit("mail", "Update SMTP Profile", sess.Username, sess.Database, mailScope(database), "success",
		fmt.Sprintf("SMTP profile set to %s (%s)", profile.Address(), profile.Security))
	rec, err := h.controlStore.GetMailProfile(r.Context(), database)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	view, _ := profileView(*rec)
	writeJSON(w, http.StatusOK, view)
}

// DeleteMailProfile removes a profile (?solution=, none for the
// server-wide one). The query does not say "database", which names the
// database a request runs against.
func (h *ServerAdminHandler) DeleteMailProfile(w http.ResponseWriter, r *http.Request) {
	database := strings.TrimSpace(r.URL.Query().Get("solution"))
	if err := h.controlStore.DeleteMailProfile(r.Context(), database); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	sess := currentSession(r)
	h.recordAudit("mail", "Remove SMTP Profile", sess.Username, sess.Database, mailScope(database), "success", "SMTP profile removed")
	w.WriteHeader(http.StatusNoContent)
}

// TestMailProfile sends a short message through a stored profile.
func (h *ServerAdminHandler) TestMailProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Solution string `json:"solution"`
		To       string `json:"to"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}
	database := strings.TrimSpace(req.Solution)
	rec, err := h.controlStore.GetMailProfile(r.Context(), database)
	if errors.Is(err, control.ErrMailProfileNotFound) {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "No SMTP profile", "no SMTP profile is stored for "+mailScope(database))
		return
	}
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	var profile mail.Profile
	if err := json.Unmarshal(rec.Profile, &profile); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	prepared, err := mail.Prepare(profile, mail.Message{
		To:      req.To,
		Subject: "File4Base SMTP test",
		Body:    "This message was sent by File4Base to test the SMTP profile of " + mailScope(database) + ".",
	}, time.Now())
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid message", err.Error())
		return
	}
	sess := currentSession(r)
	if _, err := h.mailSender.Send(r.Context(), profile, profile.Password(), []*mail.Prepared{prepared}); err != nil {
		h.recordAudit("mail", "Test SMTP Profile", sess.Username, sess.Database, mailScope(database), "failure",
			fmt.Sprintf("FileMaker error %d", mail.FileMakerError(err)))
		writeMailFailure(w, r, err, 0, 1)
		return
	}
	h.recordAudit("mail", "Test SMTP Profile", sess.Username, sess.Database, mailScope(database), "success", "test message sent")
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "sent"})
}

func mailScope(database string) string {
	if database == "" {
		return "the server"
	}
	return "the solution " + database
}

func (h *ServerAdminHandler) databaseExists(ctx context.Context, name string) bool {
	dbs, err := h.dbMgr.ListDatabases(ctx)
	if err != nil {
		return false
	}
	for _, d := range dbs {
		if strings.EqualFold(d, name) {
			return true
		}
	}
	return false
}

// sessionMailProfile is the profile the session's solution sends with: its
// own, otherwise the server-wide one. scope is "solution" or "server".
func (h *ServerAdminHandler) sessionMailProfile(ctx context.Context, database string) (*mail.Profile, string, error) {
	for _, candidate := range []struct{ name, scope string }{{database, "solution"}, {"", "server"}} {
		if candidate.scope == "solution" && strings.TrimSpace(candidate.name) == "" {
			continue
		}
		rec, err := h.controlStore.GetMailProfile(ctx, candidate.name)
		if errors.Is(err, control.ErrMailProfileNotFound) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		var p mail.Profile
		if err := json.Unmarshal(rec.Profile, &p); err != nil {
			return nil, "", err
		}
		return &p, candidate.scope, nil
	}
	return nil, "", nil
}

// GetMailStatus tells a script whether its solution can send through SMTP,
// and as whom.
func (h *ServerAdminHandler) GetMailStatus(w http.ResponseWriter, r *http.Request) {
	profile, scope, err := h.sessionMailProfile(r.Context(), currentSession(r).Database)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	if profile == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"smtp_configured": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"smtp_configured": true,
		"scope":           scope,
		"from_name":       profile.FromName,
		"from_address":    profile.FromAddress,
		"max_per_hour":    profile.MaxPerHour,
		"max_recipients":  mail.MaxRecipients,
		"max_attachment":  mail.MaxAttachmentBytes,
		"max_batch":       maxMailBatch,
	})
}

type sendMailMessage struct {
	To      string `json:"to"`
	CC      string `json:"cc"`
	BCC     string `json:"bcc"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

type sendMailRequest struct {
	FromName    string            `json:"from_name"`
	From        string            `json:"from"`
	ReplyTo     string            `json:"reply_to"`
	Attachments []mail.Attachment `json:"attachments"`
	Messages    []sendMailMessage `json:"messages"`
}

// SendMail sends the messages of one Send Mail step through the session's
// solution's profile.
func (h *ServerAdminHandler) SendMail(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	profile, _, err := h.sessionMailProfile(r.Context(), sess.Database)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	if profile == nil {
		telemetry.WriteProblem(w, r, http.StatusConflict, "No SMTP profile",
			"no SMTP profile is configured for this solution or for the server; a server administrator sets one in Server Tools")
		return
	}
	var req sendMailRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMailRequestBytes)).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Request too large",
				fmt.Sprintf("a send request may weigh at most %d bytes", maxMailRequestBytes))
			return
		}
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}
	if len(req.Messages) == 0 || len(req.Messages) > maxMailBatch {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid message",
			fmt.Sprintf("a send request has from 1 to %d messages", maxMailBatch))
		return
	}
	now := time.Now()
	prepared := make([]*mail.Prepared, 0, len(req.Messages))
	for i, m := range req.Messages {
		p, err := mail.Prepare(*profile, mail.Message{
			FromName: req.FromName, From: req.From, ReplyTo: req.ReplyTo,
			To: m.To, CC: m.CC, BCC: m.BCC, Subject: m.Subject, Body: m.Message,
			Attachments: req.Attachments,
		}, now)
		if err != nil {
			detail := err.Error()
			if len(req.Messages) > 1 {
				detail = fmt.Sprintf("message %d: %s", i+1, detail)
			}
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid message", detail)
			return
		}
		prepared = append(prepared, p)
	}
	key := strings.ToLower(sess.Database) + "\x00" + strings.ToLower(sess.Username)
	ok, wait := h.mailLimiter.reserve(key, len(prepared), profile.MaxPerHour, now)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		telemetry.WriteProblem(w, r, http.StatusTooManyRequests, "Too many messages",
			fmt.Sprintf("this account may send %d messages an hour through this profile", profile.MaxPerHour))
		return
	}
	sent, err := h.mailSender.Send(r.Context(), *profile, profile.Password(), prepared)
	h.mailLimiter.release(key, len(prepared)-sent)
	// The audit trail counts messages; it keeps no address, subject or text.
	if err != nil {
		h.recordAudit("mail", "Send Mail", sess.Username, sess.Database, profile.Address(), "failure",
			fmt.Sprintf("%d of %d message(s) sent; FileMaker error %d", sent, len(prepared), mail.FileMakerError(err)))
		writeMailFailure(w, r, err, sent, len(prepared))
		return
	}
	h.recordAudit("mail", "Send Mail", sess.Username, sess.Database, profile.Address(), "success",
		fmt.Sprintf("%d message(s) sent", sent))
	writeJSON(w, http.StatusOK, map[string]interface{}{"sent": sent})
}

// mailFailure is the problem of a send the SMTP server refused, with how
// many messages it took first and the FileMaker error code of the step.
type mailFailure struct {
	telemetry.ProblemDetails
	Sent           int `json:"sent"`
	FileMakerError int `json:"filemaker_error"`
}

func writeMailFailure(w http.ResponseWriter, r *http.Request, err error, sent, total int) {
	detail := err.Error()
	if total > 1 {
		detail = fmt.Sprintf("%d of %d messages were sent: %s", sent, total, detail)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(mailFailure{
		ProblemDetails: telemetry.ProblemDetails{
			Type:     "https://file4base.org/errors/smtp-failure",
			Title:    "SMTP failure",
			Status:   http.StatusBadGateway,
			Detail:   detail,
			Instance: r.URL.Path,
			TraceID:  telemetry.TraceIDFromContext(r.Context()),
		},
		Sent:           sent,
		FileMakerError: mail.FileMakerError(err),
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
