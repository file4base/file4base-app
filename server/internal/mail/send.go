package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// ErrSend is returned when the SMTP server could not be reached or refused
// a message. Its text is the server's answer, never the password.
var ErrSend = errors.New("the SMTP server did not take the message")

// What went wrong, each also an ErrSend. FileMakerError maps them onto the
// error codes FileMaker's Send Mail reports.
var (
	errConnect          = errors.New("connection failed")
	errTLS              = errors.New("TLS failed")
	errStartTLSRequired = errors.New("STARTTLS not offered")
	errAuth             = errors.New("authentication failed")
	errAuthNotSupported = errors.New("authentication not supported")
)

// FileMakerError is the error FileMaker's Send Mail reports for err:
// 1501 authentication failed, 1502 connection refused, 1503 TLS error, 1504
// the server requires an encrypted connection, 1505 the authentication is
// not supported, 1506 the message could not be sent.
func FileMakerError(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errAuth):
		return 1501
	case errors.Is(err, errConnect):
		return 1502
	case errors.Is(err, errTLS):
		return 1503
	case errors.Is(err, errStartTLSRequired):
		return 1504
	case errors.Is(err, errAuthNotSupported):
		return 1505
	}
	return 1506
}

// Sender holds the SMTP conversation.
type Sender struct {
	// TLSConfig is the base TLS configuration (tests trust their own CA
	// with it); nil verifies the server against the system roots. The
	// server name is always the profile's host.
	TLSConfig *tls.Config
	// Timeout bounds the whole conversation; zero is one minute.
	Timeout time.Duration
	// Dial opens the TCP connection; nil uses a net.Dialer.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
}

func (s *Sender) tlsConfig(host string) *tls.Config {
	var cfg *tls.Config
	if s != nil && s.TLSConfig != nil {
		cfg = s.TLSConfig.Clone()
	} else {
		cfg = &tls.Config{}
	}
	cfg.ServerName = host
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	return cfg
}

// Send sends every message over one connection, in order, and reports how
// many the server took before the first one it refused.
func (s *Sender) Send(ctx context.Context, p Profile, password string, messages []*Prepared) (int, error) {
	timeout := time.Minute
	if s != nil && s.Timeout > 0 {
		timeout = s.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dial := (&net.Dialer{}).DialContext
	if s != nil && s.Dial != nil {
		dial = s.Dial
	}
	conn, err := dial(ctx, "tcp", p.Address())
	if err != nil {
		return 0, fmt.Errorf("%w: %w: cannot connect to %s: %v", ErrSend, errConnect, p.Address(), err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if p.Security == SecurityTLS {
		tc := tls.Client(conn, s.tlsConfig(p.Host))
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return 0, fmt.Errorf("%w: %w: TLS with %s failed: %v", ErrSend, errTLS, p.Address(), err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, p.Host)
	if err != nil {
		_ = conn.Close()
		return 0, fmt.Errorf("%w: %v", ErrSend, err)
	}
	defer c.Close()
	if err := c.Hello(heloName()); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrSend, err)
	}
	if p.Security == SecurityStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return 0, fmt.Errorf("%w: %w: %s does not offer STARTTLS, and the profile requires it", ErrSend, errStartTLSRequired, p.Address())
		}
		if err := c.StartTLS(s.tlsConfig(p.Host)); err != nil {
			return 0, fmt.Errorf("%w: %w: STARTTLS with %s failed: %v", ErrSend, errTLS, p.Address(), err)
		}
	}
	if p.Username != "" {
		auth, err := pickAuth(c, p, password)
		if err != nil {
			return 0, err
		}
		if err := c.Auth(auth); err != nil {
			return 0, fmt.Errorf("%w: %w: authentication as %s failed: %v", ErrSend, errAuth, p.Username, err)
		}
	}
	sent := 0
	for _, m := range messages {
		if err := sendOne(c, m); err != nil {
			return sent, err
		}
		sent++
	}
	_ = c.Quit()
	return sent, nil
}

func sendOne(c *smtp.Client, m *Prepared) error {
	if err := c.Mail(m.Sender); err != nil {
		return fmt.Errorf("%w: sender %s refused: %v", ErrSend, m.Sender, err)
	}
	for _, rcpt := range m.Recipients {
		if err := c.Rcpt(rcpt); err != nil {
			_ = c.Reset()
			return fmt.Errorf("%w: recipient %s refused: %v", ErrSend, rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		_ = c.Reset()
		return fmt.Errorf("%w: %v", ErrSend, err)
	}
	if _, err := w.Write(m.Data); err != nil {
		return fmt.Errorf("%w: %v", ErrSend, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrSend, err)
	}
	return nil
}

func pickAuth(c *smtp.Client, p Profile, password string) (smtp.Auth, error) {
	ok, mechs := c.Extension("AUTH")
	if !ok {
		return nil, fmt.Errorf("%w: %w: %s offers no authentication, and the profile has a user name", ErrSend, errAuthNotSupported, p.Address())
	}
	fields := strings.Fields(strings.ToUpper(mechs))
	has := func(name string) bool {
		for _, f := range fields {
			if f == name {
				return true
			}
		}
		return false
	}
	switch {
	case has("PLAIN"):
		return smtp.PlainAuth("", p.Username, password, p.Host), nil
	case has("LOGIN"):
		return &loginAuth{username: p.Username, password: password, host: p.Host}, nil
	}
	return nil, fmt.Errorf("%w: %w: %s offers neither PLAIN nor LOGIN authentication", ErrSend, errAuthNotSupported, p.Address())
}

// loginAuth is the LOGIN mechanism, which some servers offer instead of
// PLAIN. Like smtp.PlainAuth it refuses to send the password unencrypted to
// anything but the loopback interface.
type loginAuth struct {
	username, password, host string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLoopback(server.Name) {
		return "", nil, errors.New("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, errors.New("wrong host name")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected LOGIN challenge")
}

func heloName() string {
	if h, err := os.Hostname(); err == nil && h != "" && !strings.ContainsAny(h, " \r\n") {
		return h
	}
	return "localhost"
}
