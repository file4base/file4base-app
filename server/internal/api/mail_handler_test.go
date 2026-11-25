package api_test

import (
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/mail"
	"github.com/file4base/file4base-app/server/internal/mail/mailtest"
)

// The password of the SMTP account: it is in the server environment, and it
// must reach the SMTP server and nothing else.
const smtpTestSecret = "s3cret-never-in-a-response-7d2e"

func newMailHarness(t *testing.T) (*serverToolsHarness, *mailtest.Server) {
	t.Helper()
	s := &serverToolsHarness{adminDB: uniqueDB("mailadm"), aliceDB: uniqueDB("mailalice"), bobDB: uniqueDB("mailbob")}
	s.apiHarness = newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		AdminDatabase:               s.adminDB,
		BackupsDir:                  t.TempDir(),
		TLSDir:                      filepath.Join(t.TempDir(), "tls"),
		MailSender:                  &mail.Sender{Timeout: 5 * time.Second},
	})
	s.createDatabase(s.adminDB, "root", "root-pass-1")
	s.createDatabase(s.aliceDB, "alice", "alice-pass-1")
	s.createDatabase(s.bobDB, "bob", "bob-pass-1")
	s.adminToken = s.login(s.adminDB, "root", "root-pass-1")
	s.aliceToken = s.login(s.aliceDB, "alice", "alice-pass-1")
	s.bobToken = s.login(s.bobDB, "bob", "bob-pass-1")
	t.Setenv("FILE4BASE_SMTP_APITEST", smtpTestSecret)
	fake := mailtest.Start(t, func(f *mailtest.Server) { f.RejectRecipient = "bounce@example.invalid" })
	return s, fake
}

func (s *serverToolsHarness) putProfile(t *testing.T, fake *mailtest.Server, database string, extra map[string]interface{}) (int, string) {
	t.Helper()
	body := map[string]interface{}{
		"solution":     database,
		"host":         "127.0.0.1",
		"port":         fake.Port(),
		"security":     "none",
		"username":     "mailer",
		"password_env": "FILE4BASE_SMTP_APITEST",
		"from_name":    "Orders",
		"from_address": "orders@example.invalid",
	}
	for k, v := range extra {
		body[k] = v
	}
	rec := s.do(http.MethodPut, "/api/v1/server/mail-profiles", s.adminToken, body, nil)
	return rec.Code, rec.Body.String()
}

func TestMail_ProfilesAreAdministeredWithoutTheirSecret(t *testing.T) {
	s, fake := newMailHarness(t)

	// A database owner is not a server administrator.
	rec := s.do(http.MethodPut, "/api/v1/server/mail-profiles", s.aliceToken, map[string]interface{}{"host": "x"}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	code, body := s.putProfile(t, fake, "", nil)
	require.Equal(t, http.StatusOK, code, body)
	assert.Contains(t, body, `"password_env":"FILE4BASE_SMTP_APITEST"`)
	assert.Contains(t, body, `"password_available":true`)
	assert.NotContains(t, body, smtpTestSecret)

	// A profile cannot name a server variable that is not an SMTP password,
	// nor send a password in clear to a remote host.
	code, body = s.putProfile(t, fake, "", map[string]interface{}{"password_env": "DATABASE_URL"})
	assert.Equal(t, http.StatusUnprocessableEntity, code, body)
	code, body = s.putProfile(t, fake, "", map[string]interface{}{"host": "smtp.example.invalid"})
	assert.Equal(t, http.StatusUnprocessableEntity, code, body)
	code, _ = s.putProfile(t, fake, "no_such_database_here", nil)
	assert.Equal(t, http.StatusNotFound, code)

	for _, path := range []string{"/api/v1/server/mail-profiles", "/api/v1/server/audit-logs"} {
		rec = s.do(http.MethodGet, path, s.adminToken, nil, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Body.String(), smtpTestSecret, path)
	}

	// A test message goes out with the password from the environment.
	rec = s.do(http.MethodPost, "/api/v1/server/mail-profiles/test", s.adminToken, map[string]string{"to": "admin@example.invalid"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := fake.Received()
	require.Len(t, got, 1)
	assert.Equal(t, smtpTestSecret, got[0].Password)
	assert.NotContains(t, rec.Body.String(), smtpTestSecret)
}

func TestMail_ScriptsSendThroughTheirSolutionsProfile(t *testing.T) {
	s, fake := newMailHarness(t)

	var status map[string]interface{}
	s.do(http.MethodGet, "/api/v1/mail/status", s.aliceToken, nil, &status)
	assert.Equal(t, false, status["smtp_configured"])
	rec := s.do(http.MethodPost, "/api/v1/mail/send", s.aliceToken, map[string]interface{}{
		"messages": []map[string]string{{"to": "x@example.invalid"}},
	}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "no profile, no mail")

	code, body := s.putProfile(t, fake, "", nil)
	require.Equal(t, http.StatusOK, code, body)
	code, body = s.putProfile(t, fake, s.aliceDB, map[string]interface{}{"from_address": "alice@example.invalid", "max_per_hour": 3})
	require.Equal(t, http.StatusOK, code, body)

	s.do(http.MethodGet, "/api/v1/mail/status", s.aliceToken, nil, &status)
	assert.Equal(t, true, status["smtp_configured"])
	assert.Equal(t, "solution", status["scope"])
	assert.Equal(t, "alice@example.invalid", status["from_address"])
	s.do(http.MethodGet, "/api/v1/mail/status", s.bobToken, nil, &status)
	assert.Equal(t, "server", status["scope"], "a solution without its own profile uses the server's")

	// One message per record, with the step's attachment.
	pdf := []byte("%PDF-1.4 report")
	var sent struct {
		Sent int `json:"sent"`
	}
	rec = s.do(http.MethodPost, "/api/v1/mail/send", s.aliceToken, map[string]interface{}{
		"from_name": "Alice's desk",
		"attachments": []map[string]string{
			{"name": "report.pdf", "content_type": "application/pdf", "data": base64.StdEncoding.EncodeToString(pdf)},
		},
		"messages": []map[string]string{
			{"to": "one@example.invalid", "subject": "Record 1", "message": "Hello\rone"},
			{"to": "two@example.invalid", "cc": "boss@example.invalid", "subject": "Record 2"},
		},
	}, &sent)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, sent.Sent)
	assert.NotContains(t, rec.Body.String(), smtpTestSecret)
	got := fake.Received()
	require.Len(t, got, 2)
	assert.Equal(t, "alice@example.invalid", got[0].From)
	assert.Equal(t, []string{"two@example.invalid", "boss@example.invalid"}, got[1].Recipients)
	assert.Contains(t, got[0].Data, "Alice's desk")
	assert.Contains(t, got[0].Data, base64.StdEncoding.EncodeToString(pdf))

	// A computed subject or address cannot add a header.
	for _, m := range []map[string]string{
		{"to": "x@example.invalid", "subject": "Hi\r\nBcc: victim@example.invalid"},
		{"to": "x@example.invalid\nBcc: victim@example.invalid"},
	} {
		rec = s.do(http.MethodPost, "/api/v1/mail/send", s.aliceToken, map[string]interface{}{"messages": []map[string]string{m}}, nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "line break")
	}
	assert.Len(t, fake.Received(), 2, "nothing more was sent")

	// The SMTP server refusing a recipient is the step's error 1506, after
	// the messages it took.
	var failure struct {
		Sent           int `json:"sent"`
		FileMakerError int `json:"filemaker_error"`
	}
	rec = s.do(http.MethodPost, "/api/v1/mail/send", s.bobToken, map[string]interface{}{
		"messages": []map[string]string{{"to": "fine@example.invalid"}, {"to": "bounce@example.invalid"}},
	}, &failure)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, 1, failure.Sent)
	assert.Equal(t, 1506, failure.FileMakerError)

	// Alice's profile allows 3 messages an hour: she sent 2.
	rec = s.do(http.MethodPost, "/api/v1/mail/send", s.aliceToken, map[string]interface{}{
		"messages": []map[string]string{{"to": "a@example.invalid"}, {"to": "b@example.invalid"}},
	}, nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	rec = s.do(http.MethodPost, "/api/v1/mail/send", s.aliceToken, map[string]interface{}{
		"messages": []map[string]string{{"to": "a@example.invalid"}},
	}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Removing the solution's profile falls back on the server's.
	rec = s.do(http.MethodDelete, "/api/v1/server/mail-profiles?solution="+s.aliceDB, s.adminToken, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	s.do(http.MethodGet, "/api/v1/mail/status", s.aliceToken, nil, &status)
	assert.Equal(t, "server", status["scope"])

	rec = s.do(http.MethodGet, "/api/v1/server/audit-logs", s.adminToken, nil, nil)
	assert.False(t, strings.Contains(rec.Body.String(), "one@example.invalid"), "the audit trail keeps no address")
}
