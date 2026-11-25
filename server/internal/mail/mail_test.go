package mail_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/mail"
	"github.com/file4base/file4base-app/server/internal/mail/mailtest"
)

func profileFor(s *mailtest.Server, security string) mail.Profile {
	return mail.Profile{
		Host:        "127.0.0.1",
		Port:        s.Port(),
		Security:    security,
		FromName:    "Orders",
		FromAddress: "orders@example.invalid",
	}
}

func prepare(t *testing.T, p mail.Profile, m mail.Message) *mail.Prepared {
	t.Helper()
	prepared, err := mail.Prepare(p, m, time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return prepared
}

func TestSendOverStartTLSWithAttachment(t *testing.T) {
	serverTLS, clientTLS := mailtest.TLSPair(t)
	fake := mailtest.Start(t, func(s *mailtest.Server) { s.TLS = serverTLS })
	t.Setenv("FILE4BASE_SMTP_TEST_PASSWORD", "pw-in-the-environment")

	p := profileFor(fake, mail.SecurityStartTLS)
	p.Username = "orders"
	p.PasswordEnv = "FILE4BASE_SMTP_TEST_PASSWORD"
	p, err := p.Normalize()
	require.NoError(t, err)
	require.True(t, p.PasswordAvailable())

	msg := prepare(t, p, mail.Message{
		To:      `"Doe, Jane" <jane@example.invalid>; bob@example.invalid`,
		BCC:     "audit@example.invalid",
		Subject: "Facture n° 12",
		Body:    "Bonjour\rLigne 2",
		Attachments: []mail.Attachment{
			{Name: "/tmp/File4Base/Temporary/invoice 12.pdf", Data: []byte("%PDF-1.4 fake")},
		},
	})
	sent, err := (&mail.Sender{TLSConfig: clientTLS, Timeout: 5 * time.Second}).
		Send(context.Background(), p, p.Password(), []*mail.Prepared{msg})
	require.NoError(t, err)
	assert.Equal(t, 1, sent)

	got := fake.Received()
	require.Len(t, got, 1)
	assert.True(t, got[0].TLS, "the message travels encrypted")
	assert.Equal(t, "orders", got[0].Username)
	assert.Equal(t, "pw-in-the-environment", got[0].Password)
	assert.Equal(t, "orders@example.invalid", got[0].From)
	assert.ElementsMatch(t, []string{"jane@example.invalid", "bob@example.invalid", "audit@example.invalid"}, got[0].Recipients)

	parsed, err := netmail.ReadMessage(strings.NewReader(got[0].Data))
	require.NoError(t, err)
	assert.Empty(t, parsed.Header.Get("Bcc"), "BCC addresses are envelope recipients only")
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Facture n° 12", subject)
	assert.Contains(t, parsed.Header.Get("To"), "jane@example.invalid")

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/mixed", mediaType)
	mr := multipart.NewReader(parsed.Body, params["boundary"])
	text, err := mr.NextPart()
	require.NoError(t, err)
	body, _ := io.ReadAll(text)
	assert.Equal(t, "Bonjour\r\nLigne 2", string(body), "FileMaker's ¶ becomes a line break")
	file, err := mr.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "invoice 12.pdf", file.FileName(), "no folder of the user's disk travels with the file")
	assert.Equal(t, "application/pdf", strings.Split(file.Header.Get("Content-Type"), ";")[0])
	data, _ := io.ReadAll(base64.NewDecoder(base64.StdEncoding, file))
	assert.Equal(t, "%PDF-1.4 fake", string(data))
}

func TestSendOverImplicitTLSWithLoginAndSeveralMessages(t *testing.T) {
	serverTLS, clientTLS := mailtest.TLSPair(t)
	fake := mailtest.Start(t, func(s *mailtest.Server) {
		s.TLS, s.Implicit, s.Mechanisms = serverTLS, true, "LOGIN"
	})
	t.Setenv("FILE4BASE_SMTP_LOGIN", "login-pw")
	p, err := mail.Profile{
		Host: "127.0.0.1", Port: fake.Port(), Security: mail.SecurityTLS,
		Username: "u", PasswordEnv: "FILE4BASE_SMTP_LOGIN", FromAddress: "a@example.invalid",
	}.Normalize()
	require.NoError(t, err)
	msgs := []*mail.Prepared{
		prepare(t, p, mail.Message{To: "one@example.invalid", Subject: "1"}),
		prepare(t, p, mail.Message{To: "two@example.invalid", Subject: "2"}),
	}
	sent, err := (&mail.Sender{TLSConfig: clientTLS, Timeout: 5 * time.Second}).Send(context.Background(), p, p.Password(), msgs)
	require.NoError(t, err)
	assert.Equal(t, 2, sent)
	got := fake.Received()
	require.Len(t, got, 2)
	assert.Equal(t, "login-pw", got[1].Password)
	assert.True(t, got[1].TLS)
}

func TestSendStopsAtTheFirstRefusal(t *testing.T) {
	fake := mailtest.Start(t, func(s *mailtest.Server) { s.RejectRecipient = "nobody@example.invalid" })
	p, err := profileFor(fake, mail.SecurityNone).Normalize()
	require.NoError(t, err)
	msgs := []*mail.Prepared{
		prepare(t, p, mail.Message{To: "one@example.invalid"}),
		prepare(t, p, mail.Message{To: "nobody@example.invalid"}),
		prepare(t, p, mail.Message{To: "three@example.invalid"}),
	}
	sent, err := (&mail.Sender{Timeout: 5 * time.Second}).Send(context.Background(), p, "", msgs)
	require.ErrorIs(t, err, mail.ErrSend)
	assert.Equal(t, 1, sent)
	assert.Contains(t, err.Error(), "nobody@example.invalid")
	assert.Len(t, fake.Received(), 1)
}

func TestStartTLSIsRequiredWhenTheProfileSaysSo(t *testing.T) {
	fake := mailtest.Start(t, nil) // offers no STARTTLS
	p, err := profileFor(fake, mail.SecurityStartTLS).Normalize()
	require.NoError(t, err)
	_, err = (&mail.Sender{Timeout: 5 * time.Second}).Send(context.Background(), p, "",
		[]*mail.Prepared{prepare(t, p, mail.Message{To: "x@example.invalid"})})
	require.ErrorIs(t, err, mail.ErrSend)
	assert.Contains(t, err.Error(), "STARTTLS")
	assert.Empty(t, fake.Received(), "nothing is said in clear")
}

func TestHeaderInjectionIsRefused(t *testing.T) {
	p := mail.Profile{Host: "127.0.0.1", FromAddress: "a@example.invalid"}
	cases := map[string]mail.Message{
		"subject":      {To: "x@example.invalid", Subject: "Hi\r\nBcc: victim@example.invalid"},
		"to":           {To: "x@example.invalid\nBcc: victim@example.invalid"},
		"cc":           {To: "x@example.invalid", CC: "y@example.invalid\r"},
		"from name":    {To: "x@example.invalid", FromName: "Boss\r\nX-Evil: 1"},
		"reply-to":     {To: "x@example.invalid", ReplyTo: "r@example.invalid\nTo: z@example.invalid"},
		"attachment":   {To: "x@example.invalid", Attachments: []mail.Attachment{{Name: "a.pdf\r\nX: y"}}},
		"no recipient": {Subject: "nobody"},
		"bad address":  {To: "not an address"},
	}
	for name, m := range cases {
		_, err := mail.Prepare(p, m, time.Now())
		assert.ErrorIs(t, err, mail.ErrInvalidMessage, name)
	}
	_, err := mail.Prepare(p, mail.Message{To: "x@example.invalid", Body: "a body may\r\nhave lines"}, time.Now())
	assert.NoError(t, err)
}

func TestLimitsOfOneMessage(t *testing.T) {
	p := mail.Profile{Host: "127.0.0.1", FromAddress: "a@example.invalid"}
	many := make([]string, mail.MaxRecipients+1)
	for i := range many {
		many[i] = "r@example.invalid"
	}
	_, err := mail.Prepare(p, mail.Message{To: strings.Join(many, ";")}, time.Now())
	assert.ErrorIs(t, err, mail.ErrInvalidMessage)
	_, err = mail.Prepare(p, mail.Message{To: "x@example.invalid", Attachments: []mail.Attachment{
		{Name: "big.bin", Data: make([]byte, mail.MaxAttachmentBytes+1)},
	}}, time.Now())
	assert.ErrorIs(t, err, mail.ErrInvalidMessage)
}

func TestSenderAddressOfAnotherDomainBecomesTheReplyTo(t *testing.T) {
	p := mail.Profile{Host: "127.0.0.1", FromName: "Orders", FromAddress: "orders@example.invalid"}
	read := func(m mail.Message) netmail.Header {
		prepared, err := mail.Prepare(p, m, time.Now())
		require.NoError(t, err)
		assert.Equal(t, "orders@example.invalid", prepared.Sender, "the envelope sender is the profile's")
		parsed, err := netmail.ReadMessage(strings.NewReader(string(prepared.Data)))
		require.NoError(t, err)
		return parsed.Header
	}
	same := read(mail.Message{To: "x@example.invalid", FromName: "Sales", From: "sales@example.invalid"})
	assert.Equal(t, `"Sales" <sales@example.invalid>`, same.Get("From"))
	assert.Empty(t, same.Get("Reply-To"))

	other := read(mail.Message{To: "x@example.invalid", From: "someone@elsewhere.invalid"})
	assert.Equal(t, `"Orders" <orders@example.invalid>`, other.Get("From"))
	assert.Contains(t, other.Get("Reply-To"), "someone@elsewhere.invalid")

	explicit := read(mail.Message{To: "x@example.invalid", From: "someone@elsewhere.invalid", ReplyTo: "desk@example.invalid"})
	assert.Equal(t, "<desk@example.invalid>", explicit.Get("Reply-To"))
}

func TestProfileValidation(t *testing.T) {
	ok, err := mail.Profile{Host: "smtp.example.invalid", FromAddress: "a@example.invalid"}.Normalize()
	require.NoError(t, err)
	assert.Equal(t, mail.SecurityStartTLS, ok.Security)
	assert.Equal(t, 587, ok.Port)
	assert.Equal(t, mail.DefaultMaxPerHour, ok.MaxPerHour)

	bad := []mail.Profile{
		{Host: "", FromAddress: "a@example.invalid"},
		{Host: "smtp.example.invalid", FromAddress: "nope"},
		{Host: "smtp.example.invalid", FromAddress: "a@example.invalid", Security: "ssl3"},
		// Any server variable would be handed to the SMTP server.
		{Host: "smtp.example.invalid", FromAddress: "a@example.invalid", Username: "u", PasswordEnv: "DATABASE_URL"},
		{Host: "smtp.example.invalid", FromAddress: "a@example.invalid", Username: "u", PasswordEnv: "FILE4BASE_SMTP_X;rm"},
		// The password would travel in clear.
		{Host: "smtp.example.invalid", FromAddress: "a@example.invalid", Security: mail.SecurityNone, Username: "u"},
		{Host: "smtp.example.invalid", FromAddress: "a@example.invalid", FromName: "x\r\nBcc: y"},
	}
	for i, p := range bad {
		_, err := p.Normalize()
		assert.True(t, errors.Is(err, mail.ErrInvalidProfile), "profile %d: %v", i, err)
	}
	_, err = mail.Profile{Host: "127.0.0.1", FromAddress: "a@example.invalid", Security: mail.SecurityNone, Username: "u"}.Normalize()
	assert.NoError(t, err, "a relay on the loopback interface may take a user name in clear")
}
