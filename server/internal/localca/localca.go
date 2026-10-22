// Package localca issues and persists the local certificate authority that
// signs the TLS certificate File4Base serves on localhost and on a LAN.
//
// The authority ("File4Base Local CA") and the leaf certificate it signs live
// on disk, inside the volume the API container already persists, so the same
// material survives restarts. A server administrator installs the CA
// certificate once in the trust store of each machine; after that every leaf
// this package reissues — on renewal, or when the configured domain changes —
// is trusted without reinstalling anything.
//
// The CA private key never leaves the directory: only the CA's public
// certificate is ever exported.
package localca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	caCertFile   = "ca.crt"
	caKeyFile    = "ca.key"
	leafCertFile = "server.crt"
	leafKeyFile  = "server.key"
	bundleFile   = "server-bundle.pem"

	// CAName is the common name of the authority an administrator installs.
	CAName = "File4Base Local CA"
	// CAOrganization labels the authority in the operating system trust store.
	CAOrganization = "File4Base"

	// caValidity keeps the installed trust usable for a decade.
	caValidity = 10 * 365 * 24 * time.Hour
	// leafValidity stays under the 398-day ceiling browsers apply to server
	// certificates, so a leaf is never rejected for being too long-lived.
	leafValidity = 397 * 24 * time.Hour
	// renewBefore reissues a leaf that is close to expiry on the next start.
	renewBefore = 30 * 24 * time.Hour
)

// Subject describes the names the served leaf certificate must cover.
type Subject struct {
	// Domain is the certificate's common name and first DNS name.
	Domain string
	// DNSNames are additional host names to cover.
	DNSNames []string
	// IPs are additional IP addresses to cover, as strings.
	IPs []string
}

// Material is the on-disk certificate material, as PEM plus the file paths a
// TLS listener or Nginx reads.
type Material struct {
	CACertPEM  string
	CertPEM    string
	KeyPEM     string
	BundlePEM  string
	CACertPath string
	CertPath   string
	KeyPath    string
	BundlePath string
}

// Store is the directory holding the authority and the leaf it signed.
type Store struct {
	dir string
}

// New returns the store rooted at dir. The directory is created on first use.
func New(dir string) *Store {
	return &Store{dir: strings.TrimSpace(dir)}
}

// DefaultDir is where the API keeps TLS material: FILE4BASE_TLS_DIR when set,
// otherwise ./data/tls (/app/data/tls in the container, a persisted volume).
func DefaultDir() string {
	if dir := strings.TrimSpace(os.Getenv("FILE4BASE_TLS_DIR")); dir != "" {
		return dir
	}
	return filepath.Join("data", "tls")
}

// Dir reports the directory the material is kept in.
func (s *Store) Dir() string { return s.dir }

// CACertPath is the authority's public certificate (the file an administrator
// installs in the operating system trust store).
func (s *Store) CACertPath() string { return filepath.Join(s.dir, caCertFile) }

// CertPath is the served leaf certificate.
func (s *Store) CertPath() string { return filepath.Join(s.dir, leafCertFile) }

// KeyPath is the served leaf private key.
func (s *Store) KeyPath() string { return filepath.Join(s.dir, leafKeyFile) }

// BundlePath is the leaf certificate followed by the authority, for a reverse
// proxy that wants the whole chain in one file.
func (s *Store) BundlePath() string { return filepath.Join(s.dir, bundleFile) }

func (s *Store) caKeyPath() string { return filepath.Join(s.dir, caKeyFile) }

// DefaultSubject covers the names the stack answers on: the configured domain,
// loopback, and the container names and network aliases of the compose stack.
func DefaultSubject(domain string, extraDNS, extraIPs []string) Subject {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = "localhost"
	}
	dns := []string{
		"localhost",
		"file4base-web",
		"file4base-api",
		"api",
		"server",
	}
	dns = append(dns, extraDNS...)
	ips := append([]string{"127.0.0.1", "::1"}, extraIPs...)
	return Subject{Domain: domain, DNSNames: dns, IPs: ips}
}

// EnvDomain is the domain the operator configured for the served certificate
// (FILE4BASE_TLS_DOMAIN), or "localhost".
func EnvDomain() string {
	if d := strings.TrimSpace(os.Getenv("FILE4BASE_TLS_DOMAIN")); d != "" {
		return d
	}
	return "localhost"
}

// EnvSANs are the extra host names and IP addresses the operator configured
// (FILE4BASE_TLS_SANS, comma-separated).
func EnvSANs() []string {
	out := []string{}
	for _, item := range strings.Split(os.Getenv("FILE4BASE_TLS_SANS"), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// SubjectFromEnv is the subject both the HTTPS listener and Server Tools use,
// so each agrees on the names the persisted leaf certificate must cover.
func SubjectFromEnv() Subject {
	return DefaultSubject(EnvDomain(), EnvSANs(), nil)
}

// names resolves the subject into the deduplicated DNS and IP SAN lists.
func (sub Subject) names() ([]string, []net.IP) {
	domain := strings.TrimSpace(sub.Domain)
	if domain == "" {
		domain = "localhost"
	}

	dnsNames := []string{}
	ips := []net.IP{}
	seenDNS := map[string]bool{}
	seenIP := map[string]bool{}

	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if ip := net.ParseIP(raw); ip != nil {
			if key := ip.String(); !seenIP[key] {
				seenIP[key] = true
				ips = append(ips, ip)
			}
			return
		}
		key := strings.ToLower(raw)
		if !seenDNS[key] {
			seenDNS[key] = true
			dnsNames = append(dnsNames, raw)
		}
	}

	// The domain comes first so it stays the certificate's primary name.
	add(domain)
	for _, n := range sub.DNSNames {
		add(n)
	}
	for _, n := range sub.IPs {
		add(n)
	}
	return dnsNames, ips
}

// Ensure returns usable material, generating only what is missing: the
// authority if it has not been created yet, and the leaf if it is absent,
// close to expiry, or no longer covers the requested names. An existing
// authority is never replaced, so installed trust keeps working.
func (s *Store) Ensure(sub Subject) (Material, error) {
	caCert, caKey, err := s.ensureCA()
	if err != nil {
		return Material{}, err
	}

	leaf, _, err := s.loadLeaf()
	if err == nil && leafIsUsable(leaf, caCert, sub) {
		return s.load()
	}
	return s.issueLeaf(caCert, caKey, sub)
}

// IssueLeaf rotates the leaf certificate, keeping the authority (and therefore
// the trust an administrator already installed) untouched.
func (s *Store) IssueLeaf(sub Subject) (Material, error) {
	caCert, caKey, err := s.ensureCA()
	if err != nil {
		return Material{}, err
	}
	return s.issueLeaf(caCert, caKey, sub)
}

// Load reads the stored material without generating anything.
func (s *Store) Load() (Material, error) { return s.load() }

// CACertPEM returns the authority's public certificate. The authority's
// private key is never returned by this package.
func (s *Store) CACertPEM() (string, error) {
	raw, err := os.ReadFile(s.CACertPath())
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Exists reports whether a complete set of material is already on disk.
func (s *Store) Exists() bool {
	for _, p := range []string{s.CACertPath(), s.caKeyPath(), s.CertPath(), s.KeyPath()} {
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			return false
		}
	}
	return true
}

func (s *Store) load() (Material, error) {
	caCertPEM, err := os.ReadFile(s.CACertPath())
	if err != nil {
		return Material{}, err
	}
	certPEM, err := os.ReadFile(s.CertPath())
	if err != nil {
		return Material{}, err
	}
	keyPEM, err := os.ReadFile(s.KeyPath())
	if err != nil {
		return Material{}, err
	}
	bundlePEM, err := os.ReadFile(s.BundlePath())
	if err != nil {
		bundlePEM = append(append([]byte{}, certPEM...), caCertPEM...)
	}
	return Material{
		CACertPEM:  string(caCertPEM),
		CertPEM:    string(certPEM),
		KeyPEM:     string(keyPEM),
		BundlePEM:  string(bundlePEM),
		CACertPath: s.CACertPath(),
		CertPath:   s.CertPath(),
		KeyPath:    s.KeyPath(),
		BundlePath: s.BundlePath(),
	}, nil
}

// ensureCA loads the authority, creating it only when it is not on disk yet.
func (s *Store) ensureCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	if s.dir == "" {
		return nil, nil, errors.New("localca: empty directory")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("localca: create %s: %w", s.dir, err)
	}

	cert, key, err := s.loadCA()
	if err == nil {
		return cert, key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	return s.createCA()
}

func (s *Store) loadCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(s.CACertPath())
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(s.caKeyPath())
	if err != nil {
		return nil, nil, err
	}
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: read CA certificate: %w", err)
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: read CA key: %w", err)
	}
	return cert, key, nil
}

func (s *Store) createCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: generate CA key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, err
	}
	notBefore := time.Now().UTC().Add(-5 * time.Minute)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   CAName,
			Organization: []string{CAOrganization},
		},
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: sign CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("localca: parse CA certificate: %w", err)
	}

	if err := writeFile(s.CACertPath(), encodeCert(der), 0o644); err != nil {
		return nil, nil, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	if err := writeFile(s.caKeyPath(), keyPEM, 0o600); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func (s *Store) loadLeaf() (*x509.Certificate, []byte, error) {
	certPEM, err := os.ReadFile(s.CertPath())
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(s.KeyPath()); err != nil {
		return nil, nil, err
	}
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, nil, err
	}
	return cert, certPEM, nil
}

func (s *Store) issueLeaf(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, sub Subject) (Material, error) {
	dnsNames, ips := sub.names()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Material{}, fmt.Errorf("localca: generate leaf key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return Material{}, err
	}
	notBefore := time.Now().UTC().Add(-5 * time.Minute)
	notAfter := notBefore.Add(leafValidity)
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   dnsNames[0],
			Organization: []string{CAOrganization},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return Material{}, fmt.Errorf("localca: sign leaf certificate: %w", err)
	}

	certPEM := encodeCert(der)
	keyPEM, err := encodeKey(key)
	if err != nil {
		return Material{}, err
	}
	caCertPEM := encodeCert(caCert.Raw)

	if err := writeFile(s.CertPath(), certPEM, 0o644); err != nil {
		return Material{}, err
	}
	if err := writeFile(s.KeyPath(), keyPEM, 0o600); err != nil {
		return Material{}, err
	}
	bundlePEM := append(append([]byte{}, certPEM...), caCertPEM...)
	if err := writeFile(s.BundlePath(), bundlePEM, 0o644); err != nil {
		return Material{}, err
	}

	return Material{
		CACertPEM:  string(caCertPEM),
		CertPEM:    string(certPEM),
		KeyPEM:     string(keyPEM),
		BundlePEM:  string(bundlePEM),
		CACertPath: s.CACertPath(),
		CertPath:   s.CertPath(),
		KeyPath:    s.KeyPath(),
		BundlePath: s.BundlePath(),
	}, nil
}

// leafIsUsable reports whether the stored leaf can keep serving: signed by the
// current authority, not near expiry, and covering exactly the wanted names.
func leafIsUsable(leaf, caCert *x509.Certificate, sub Subject) bool {
	if leaf == nil || caCert == nil {
		return false
	}
	if err := leaf.CheckSignatureFrom(caCert); err != nil {
		return false
	}
	now := time.Now().UTC()
	if now.Before(leaf.NotBefore) || now.Add(renewBefore).After(leaf.NotAfter) {
		return false
	}
	wantDNS, wantIPs := sub.names()
	if !sameStrings(wantDNS, leaf.DNSNames) {
		return false
	}
	have := make([]string, 0, len(leaf.IPAddresses))
	for _, ip := range leaf.IPAddresses {
		have = append(have, ip.String())
	}
	want := make([]string, 0, len(wantIPs))
	for _, ip := range wantIPs {
		want = append(want, ip.String())
	}
	return sameStrings(want, have)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	for i := range x {
		x[i] = strings.ToLower(x[i])
	}
	for i := range y {
		y[i] = strings.ToLower(y[i])
	}
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// SelfSignedPair builds a standalone self-signed certificate with no
// authority behind it. It backs the configurations that do not serve from the
// local CA; everything the stack actually serves goes through a Store.
func SelfSignedPair(domain string, sans []string) (string, string, error) {
	sub := Subject{Domain: domain, DNSNames: sans, IPs: []string{"127.0.0.1"}}
	dnsNames, ips := sub.names()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("localca: generate key: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return "", "", err
	}
	notBefore := time.Now().UTC().Add(-5 * time.Minute)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   dnsNames[0],
			Organization: []string{CAOrganization},
		},
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(leafValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("localca: sign certificate: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return "", "", err
	}
	return string(encodeCert(der)), string(keyPEM), nil
}

func newSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("localca: serial number: %w", err)
	}
	return serial, nil
}

func encodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("localca: marshal key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

func parseCertPEM(raw []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("localca: no CERTIFICATE block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKeyPEM(raw []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("localca: no PEM block")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// writeFile replaces path atomically with the given permissions.
func writeFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("localca: write %s: %w", path, err)
	}
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("localca: chmod %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("localca: replace %s: %w", path, err)
	}
	return nil
}
