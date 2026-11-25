// Package mailtest is an SMTP server that lives in the test process: it
// listens on the loopback interface, keeps what it is sent in memory, and
// never relays anything.
package mailtest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Received is one message the server took.
type Received struct {
	From       string
	Recipients []string
	Data       string
	// Username and Password are what the client authenticated with.
	Username string
	Password string
	// TLS tells whether the message came over an encrypted connection.
	TLS bool
}

// Server is a fake SMTP server.
type Server struct {
	// Addr is the host:port to dial.
	Addr string
	// TLS, when set, is offered with STARTTLS.
	TLS *tls.Config
	// Implicit makes the server speak TLS from the first byte (port 465).
	Implicit bool
	// Mechanisms are the AUTH mechanisms offered ("PLAIN LOGIN" by
	// default; empty offers none).
	Mechanisms string
	// RejectRecipient is refused with 550.
	RejectRecipient string

	ln       net.Listener
	mu       sync.Mutex
	received []Received
	auths    []string
}

// Start starts a server; the test stops it.
func Start(t *testing.T, configure func(*Server)) *Server {
	t.Helper()
	s := &Server{Mechanisms: "PLAIN LOGIN"}
	if configure != nil {
		configure(s)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake SMTP server: %v", err)
	}
	if s.Implicit {
		ln = tls.NewListener(ln, s.TLS)
	}
	s.ln = ln
	s.Addr = ln.Addr().String()
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

// Received returns the messages taken so far.
func (s *Server) Received() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Received(nil), s.received...)
}

// Port is the port the server listens on.
func (s *Server) Port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_, encrypted := conn.(*tls.Conn)
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	readLine := func() (string, bool) {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", false
		}
		return strings.TrimRight(line, "\r\n"), true
	}
	var cur Received
	var user, pass string
	reply("220 fake.smtp.test ESMTP")
	for {
		line, ok := readLine()
		if !ok {
			return
		}
		verb := strings.ToUpper(strings.Fields(line + " x")[0])
		arg := strings.TrimSpace(line[min(len(line), len(verb)):])
		switch verb {
		case "EHLO", "HELO":
			lines := []string{"250-fake.smtp.test"}
			if s.TLS != nil && !encrypted && !s.Implicit {
				lines = append(lines, "250-STARTTLS")
			}
			if s.Mechanisms != "" {
				lines = append(lines, "250-AUTH "+s.Mechanisms)
			}
			lines = append(lines, "250 8BITMIME")
			for _, l := range lines {
				_, _ = w.WriteString(l + "\r\n")
			}
			_ = w.Flush()
		case "STARTTLS":
			if s.TLS == nil || encrypted {
				reply("454 TLS not available")
				continue
			}
			reply("220 go ahead")
			tc := tls.Server(conn, s.TLS)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, encrypted = tc, true
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
		case "AUTH":
			parts := strings.Fields(arg)
			if len(parts) == 0 {
				reply("501 mechanism needed")
				continue
			}
			switch strings.ToUpper(parts[0]) {
			case "PLAIN":
				payload := ""
				if len(parts) > 1 {
					payload = parts[1]
				} else {
					reply("334 ")
					payload, _ = readLine()
				}
				raw, _ := base64.StdEncoding.DecodeString(payload)
				fields := strings.Split(string(raw), "\x00")
				if len(fields) == 3 {
					user, pass = fields[1], fields[2]
				}
			case "LOGIN":
				reply("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
				u, _ := readLine()
				reply("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
				p, _ := readLine()
				ub, _ := base64.StdEncoding.DecodeString(u)
				pb, _ := base64.StdEncoding.DecodeString(p)
				user, pass = string(ub), string(pb)
			default:
				reply("504 unknown mechanism")
				continue
			}
			s.mu.Lock()
			s.auths = append(s.auths, user)
			s.mu.Unlock()
			reply("235 authenticated")
		case "MAIL":
			cur = Received{From: addressOf(arg), Username: user, Password: pass, TLS: encrypted}
			reply("250 ok")
		case "RCPT":
			to := addressOf(arg)
			if s.RejectRecipient != "" && strings.EqualFold(to, s.RejectRecipient) {
				reply("550 no such user")
				continue
			}
			cur.Recipients = append(cur.Recipients, to)
			reply("250 ok")
		case "DATA":
			reply("354 end with .")
			var data strings.Builder
			for {
				l, ok := readLine()
				if !ok {
					return
				}
				if l == "." {
					break
				}
				data.WriteString(strings.TrimPrefix(l, ".") + "\r\n")
			}
			cur.Data = data.String()
			s.mu.Lock()
			s.received = append(s.received, cur)
			s.mu.Unlock()
			cur = Received{}
			reply("250 queued")
		case "RSET":
			cur = Received{}
			reply("250 ok")
		case "NOOP":
			reply("250 ok")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

func addressOf(arg string) string {
	if i := strings.IndexByte(arg, '<'); i >= 0 {
		if j := strings.IndexByte(arg[i:], '>'); j > 0 {
			return arg[i+1 : i+j]
		}
	}
	return arg
}

// TLSPair makes a certificate for 127.0.0.1 and localhost, and returns the
// server's TLS configuration and a client configuration that trusts it.
func TLSPair(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake.smtp.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	server = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	client = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return server, client
}
