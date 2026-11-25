// Package mail sends the e-mail of the Send Mail script step through an SMTP
// server (#133).
//
// A Profile says how to reach the SMTP server. It holds no password: the
// password is read from an environment variable of the server that the
// profile names, and that name must start with PasswordEnvPrefix, so a
// profile can never be made to hand another server secret (a database URL,
// the channel secret) to an SMTP server of its author's choosing. The same
// rule keeps the password out of the control store, of the API responses
// and of the logs.
//
// Every value that ends up in a header — addresses, names, the subject, the
// file names of the attachments — is refused when it holds a line break, so
// a value computed from a field cannot add a header (Bcc:, a second To:) or
// a part of its own.
package mail

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Connection security of a profile.
const (
	// SecurityStartTLS connects in clear and upgrades with STARTTLS before
	// anything else is said; a server that does not offer it is refused.
	SecurityStartTLS = "starttls"
	// SecurityTLS connects over TLS from the first byte (port 465).
	SecurityTLS = "tls"
	// SecurityNone never encrypts. It is only accepted without a user name,
	// or for a relay on the loopback interface.
	SecurityNone = "none"
)

// PasswordEnvPrefix is how the environment variable holding a profile's
// password must be named.
const PasswordEnvPrefix = "FILE4BASE_SMTP_"

// DefaultMaxPerHour is how many messages one account may send in an hour
// when the profile does not say.
const DefaultMaxPerHour = 200

// Limits of one message.
const (
	// MaxRecipients is the most addresses (To, CC and BCC together) of one
	// message.
	MaxRecipients = 50
	// MaxAttachmentBytes is the most the attachments of one message may
	// weigh together, before encoding.
	MaxAttachmentBytes = 20 << 20
	// MaxBodyBytes is the most a subject and a message text may weigh.
	MaxBodyBytes = 1 << 20
)

// ErrInvalidProfile is returned for a profile that cannot be stored.
var ErrInvalidProfile = errors.New("invalid SMTP profile")

// ErrInvalidMessage is returned for a message that cannot be sent as given:
// an address that does not parse, a header value with a line break, too many
// recipients, attachments too large.
var ErrInvalidMessage = errors.New("invalid message")

// Profile is how File4Base reaches an SMTP server.
type Profile struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	Username string `json:"username,omitempty"`
	// PasswordEnv names the environment variable of the server that holds
	// the password. The password itself is never stored nor returned.
	PasswordEnv string `json:"password_env,omitempty"`
	FromName    string `json:"from_name,omitempty"`
	FromAddress string `json:"from_address"`
	ReplyTo     string `json:"reply_to,omitempty"`
	// MaxPerHour caps the messages one account sends in an hour.
	MaxPerHour int `json:"max_per_hour,omitempty"`
}

// Normalize trims the profile, fills its defaults and checks it.
func (p Profile) Normalize() (Profile, error) {
	p.Host = strings.TrimSpace(p.Host)
	p.Security = strings.ToLower(strings.TrimSpace(p.Security))
	p.Username = strings.TrimSpace(p.Username)
	p.PasswordEnv = strings.TrimSpace(p.PasswordEnv)
	p.FromName = strings.TrimSpace(p.FromName)
	p.FromAddress = strings.TrimSpace(p.FromAddress)
	p.ReplyTo = strings.TrimSpace(p.ReplyTo)
	if p.Security == "" {
		p.Security = SecurityStartTLS
	}
	if p.Port == 0 {
		switch p.Security {
		case SecurityTLS:
			p.Port = 465
		case SecurityNone:
			p.Port = 25
		default:
			p.Port = 587
		}
	}
	if p.MaxPerHour == 0 {
		p.MaxPerHour = DefaultMaxPerHour
	}
	bad := func(format string, args ...interface{}) (Profile, error) {
		return p, fmt.Errorf("%w: %s", ErrInvalidProfile, fmt.Sprintf(format, args...))
	}
	if p.Host == "" || strings.ContainsAny(p.Host, " /\\@\r\n") {
		return bad("the host %q is not a host name", p.Host)
	}
	if p.Port < 1 || p.Port > 65535 {
		return bad("the port %d is not a TCP port", p.Port)
	}
	switch p.Security {
	case SecurityStartTLS, SecurityTLS, SecurityNone:
	default:
		return bad("the security %q is none of starttls, tls and none", p.Security)
	}
	if p.Security == SecurityNone && p.Username != "" && !isLoopback(p.Host) {
		return bad("a user name over an unencrypted connection would send the password in clear: use starttls or tls")
	}
	if p.PasswordEnv != "" && (!strings.HasPrefix(p.PasswordEnv, PasswordEnvPrefix) || !environmentName(p.PasswordEnv)) {
		return bad("the password variable %q must be an environment variable name starting with %s", p.PasswordEnv, PasswordEnvPrefix)
	}
	if p.PasswordEnv != "" && p.Username == "" {
		return bad("a password variable needs a user name")
	}
	if strings.ContainsAny(p.Username, "\r\n\x00") {
		return bad("the user name holds a line break")
	}
	if _, err := ParseAddress(p.FromAddress); err != nil {
		return bad("the sender address: %v", err)
	}
	if err := checkHeaderValue("sender name", p.FromName); err != nil {
		return bad("%v", err)
	}
	if p.ReplyTo != "" {
		if _, err := ParseAddress(p.ReplyTo); err != nil {
			return bad("the reply-to address: %v", err)
		}
	}
	if p.MaxPerHour < 1 || p.MaxPerHour > 100000 {
		return bad("the hourly limit %d is out of range (1 to 100000)", p.MaxPerHour)
	}
	return p, nil
}

// Password reads the profile's password from the server environment.
func (p Profile) Password() string {
	if p.PasswordEnv == "" {
		return ""
	}
	return os.Getenv(p.PasswordEnv)
}

// PasswordAvailable reports whether the environment variable the profile
// names is set: an administrator can see that it is without seeing it.
func (p Profile) PasswordAvailable() bool {
	if p.PasswordEnv == "" {
		return false
	}
	_, ok := os.LookupEnv(p.PasswordEnv)
	return ok
}

// Address is the host and port to dial.
func (p Profile) Address() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func environmentName(s string) bool {
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return s != ""
}

// Attachment is one file of a message.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type,omitempty"`
	Data        []byte `json:"data"`
}

// Message is one e-mail as the Send Mail step words it: the address lists
// are FileMaker's, separated by semicolons or commas.
type Message struct {
	FromName string
	From     string
	ReplyTo  string
	To       string
	CC       string
	BCC      string
	Subject  string
	Body     string

	Attachments []Attachment
}

// Prepared is a message checked and ready for the SMTP conversation. Its
// MIME text is written only while it is sent (Write): the attachments,
// shared by every message of a batch, are never copied or encoded ahead,
// so a batch holds one copy of them whatever its size.
type Prepared struct {
	// Sender is the envelope sender: always the profile's address, the one
	// the SMTP account may send as.
	Sender     string
	Recipients []string

	head        []byte // header lines, without the Content-Type
	body        string // the message text, CRLF line breaks
	attachments []Attachment
}

// Bytes is the whole message, for tests and diagnostics.
func (p *Prepared) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Write writes the message, as sent, to w.
func (p *Prepared) Write(w io.Writer) error {
	bw := bufio.NewWriterSize(w, 32<<10)
	if _, err := bw.Write(p.head); err != nil {
		return err
	}
	header := func(name, value string) {
		bw.WriteString(name)
		bw.WriteString(": ")
		bw.WriteString(value)
		bw.WriteString("\r\n")
	}
	if len(p.attachments) == 0 {
		header("Content-Type", "text/plain; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		bw.WriteString("\r\n")
		if err := writeQuotedPrintable(bw, p.body); err != nil {
			return err
		}
		return bw.Flush()
	}
	mw := multipart.NewWriter(bw)
	header("Content-Type", "multipart/mixed; boundary="+strconv.Quote(mw.Boundary()))
	bw.WriteString("\r\n")
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	if err := writeQuotedPrintable(part, p.body); err != nil {
		return err
	}
	for _, a := range p.attachments {
		name := attachmentName(a.Name)
		ctype := strings.TrimSpace(a.ContentType)
		if ctype == "" {
			ctype = mime.TypeByExtension(path.Ext(name))
		}
		if _, _, err := mime.ParseMediaType(ctype); err != nil || ctype == "" {
			ctype = "application/octet-stream"
		}
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(baseType(ctype), map[string]string{"name": name})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return err
		}
		writeBase64Lines(part, a.Data)
	}
	if err := mw.Close(); err != nil {
		return err
	}
	return bw.Flush()
}

// checkHeaderValue refuses a value that would end its header line.
func checkHeaderValue(what, v string) error {
	if strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("the %s holds a line break, which would add to the headers of the message", what)
	}
	return nil
}

// ParseAddress reads one address, with or without a display name.
func ParseAddress(s string) (*mail.Address, error) {
	if err := checkHeaderValue("address", s); err != nil {
		return nil, err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("no address")
	}
	a, err := mail.ParseAddress(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not an e-mail address", s)
	}
	return a, nil
}

// ParseAddressList reads a FileMaker address list: addresses separated by
// semicolons or commas (a comma inside a quoted name does not separate).
func ParseAddressList(s string) ([]*mail.Address, error) {
	if err := checkHeaderValue("address list", s); err != nil {
		return nil, err
	}
	var out []*mail.Address
	for _, part := range splitAddresses(s) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		a, err := ParseAddress(part)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func splitAddresses(s string) []string {
	var parts []string
	var cur strings.Builder
	quoted, angle := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && quoted && i+1 < len(s):
			cur.WriteByte(c)
			i++
			c = s[i]
		case c == '"':
			quoted = !quoted
		case c == '<' && !quoted:
			angle = true
		case c == '>' && !quoted:
			angle = false
		case (c == ';' || c == ',') && !quoted && !angle:
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	return append(parts, cur.String())
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

// Prepare checks a message and writes it out for profile p.
//
// The sender's name and address and the reply-to address come from the
// message when it sets them (FileMaker's "via SMTP server" options), from the
// profile otherwise. A sender address of another domain than the profile's is
// not put in From — the SMTP account could not send as it, and a script must
// not be a way to send as anyone — but becomes the reply-to address, unless
// the message sets one.
func Prepare(p Profile, m Message, now time.Time) (*Prepared, error) {
	bad := func(format string, args ...interface{}) (*Prepared, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMessage, fmt.Sprintf(format, args...))
	}
	for what, v := range map[string]string{"sender name": m.FromName, "subject": m.Subject} {
		if err := checkHeaderValue(what, v); err != nil {
			return bad("%v", err)
		}
	}
	if len(m.Subject)+len(m.Body) > MaxBodyBytes {
		return bad("the subject and the message weigh more than %d bytes", MaxBodyBytes)
	}
	from := &mail.Address{Name: p.FromName, Address: p.FromAddress}
	if strings.TrimSpace(m.FromName) != "" {
		from.Name = strings.TrimSpace(m.FromName)
	}
	var replyTo *mail.Address
	if strings.TrimSpace(p.ReplyTo) != "" {
		a, err := ParseAddress(p.ReplyTo)
		if err != nil {
			return bad("the profile's reply-to address: %v", err)
		}
		replyTo = a
	}
	if strings.TrimSpace(m.From) != "" {
		a, err := ParseAddress(m.From)
		if err != nil {
			return bad("the sender address: %v", err)
		}
		if a.Name != "" && strings.TrimSpace(m.FromName) == "" {
			from.Name = a.Name
		}
		if domainOf(a.Address) == domainOf(p.FromAddress) {
			from.Address = a.Address
		} else {
			replyTo = &mail.Address{Name: from.Name, Address: a.Address}
		}
	}
	if strings.TrimSpace(m.ReplyTo) != "" {
		a, err := ParseAddress(m.ReplyTo)
		if err != nil {
			return bad("the reply-to address: %v", err)
		}
		replyTo = a
	}
	lists := map[string][]*mail.Address{}
	total := 0
	for _, f := range []struct{ name, raw string }{{"To", m.To}, {"Cc", m.CC}, {"Bcc", m.BCC}} {
		addrs, err := ParseAddressList(f.raw)
		if err != nil {
			return bad("%s: %v", f.name, err)
		}
		lists[f.name] = addrs
		total += len(addrs)
	}
	if total == 0 {
		return bad("the message has no recipient")
	}
	if total > MaxRecipients {
		return bad("the message has %d recipients, more than %d", total, MaxRecipients)
	}
	size := 0
	for _, a := range m.Attachments {
		if err := checkHeaderValue("attachment name", a.Name); err != nil {
			return bad("%v", err)
		}
		if err := checkHeaderValue("attachment type", a.ContentType); err != nil {
			return bad("%v", err)
		}
		size += len(a.Data)
	}
	if size > MaxAttachmentBytes {
		return bad("the attachments weigh %d bytes, more than %d", size, MaxAttachmentBytes)
	}

	var buf bytes.Buffer
	header := func(name, value string) {
		buf.WriteString(name)
		buf.WriteString(": ")
		buf.WriteString(value)
		buf.WriteString("\r\n")
	}
	joined := func(addrs []*mail.Address) string {
		s := make([]string, len(addrs))
		for i, a := range addrs {
			s[i] = a.String()
		}
		return strings.Join(s, ", ")
	}
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+randomID()+"@"+messageIDDomain(from.Address)+">")
	header("From", from.String())
	if len(lists["To"]) > 0 {
		header("To", joined(lists["To"]))
	}
	if len(lists["Cc"]) > 0 {
		header("Cc", joined(lists["Cc"]))
	}
	if replyTo != nil {
		header("Reply-To", replyTo.String())
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("MIME-Version", "1.0")
	header("X-Mailer", "File4Base")

	prepared := &Prepared{
		Sender:      p.FromAddress,
		head:        buf.Bytes(),
		body:        normalizeLineBreaks(m.Body),
		attachments: m.Attachments,
	}
	for _, name := range []string{"To", "Cc", "Bcc"} {
		for _, a := range lists[name] {
			prepared.Recipients = append(prepared.Recipients, a.Address)
		}
	}
	return prepared, nil
}

func baseType(ctype string) string {
	t, _, err := mime.ParseMediaType(ctype)
	if err != nil {
		return "application/octet-stream"
	}
	return t
}

// attachmentName keeps the file name of a path, so no folder of the user's
// disk travels with the message.
func attachmentName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" {
		return "attachment"
	}
	return name
}

func messageIDDomain(addr string) string {
	if d := domainOf(addr); d != "" {
		return d
	}
	return "file4base.local"
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// normalizeLineBreaks turns FileMaker's carriage returns (¶) and any mix of
// line endings into CRLF.
func normalizeLineBreaks(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

type writer interface {
	Write(p []byte) (int, error)
}

func writeQuotedPrintable(w writer, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(s)); err != nil {
		return err
	}
	return qp.Close()
}

// writeBase64Lines writes data in base64 lines of 76 characters, a line at a
// time: no encoded copy of the attachment is ever held.
func writeBase64Lines(w writer, data []byte) {
	var line [78]byte
	for len(data) > 0 {
		n := len(data)
		if n > 57 {
			n = 57
		}
		m := base64.StdEncoding.EncodedLen(n)
		base64.StdEncoding.Encode(line[:m], data[:n])
		line[m], line[m+1] = '\r', '\n'
		_, _ = w.Write(line[:m+2])
		data = data[n:]
	}
}
