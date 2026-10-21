package api_test

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tlsPanel is the part of the TLS configuration these tests assert on.
type tlsPanel struct {
	Mode        string   `json:"mode"`
	CertSource  string   `json:"cert_source"`
	CertIssuer  string   `json:"cert_issuer"`
	CertSubject string   `json:"cert_subject"`
	CertSANs    []string `json:"cert_sans"`
	CertSHA256  string   `json:"cert_sha256"`
	CAAvailable bool     `json:"ca_available"`
	CASubject   string   `json:"ca_subject"`
	CASHA256    string   `json:"ca_sha256"`
	CustomKey   string   `json:"custom_key_pem"`
}

func parseCert(t *testing.T, raw string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(raw))
	require.NotNil(t, block, "no PEM block in %q", raw)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}

func TestServerTools_TLSPanelDescribesTheCertificateTheServerReallyServes(t *testing.T) {
	s := newServerToolsHarness(t, true)

	var panel tlsPanel
	rec := s.do(http.MethodGet, "/api/v1/server/tls", s.adminToken, nil, &panel)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "local_ca", panel.CertSource)
	assert.True(t, panel.CAAvailable)
	assert.Contains(t, panel.CertIssuer, "File4Base Local CA")
	assert.Contains(t, panel.CASubject, "File4Base Local CA")
	assert.Contains(t, panel.CertSANs, "localhost")
	assert.Empty(t, panel.CustomKey, "a private key is never part of a response")

	// The exported authority is the one that signed what the panel describes.
	caRec := s.do(http.MethodGet, "/api/v1/server/tls/ca.crt", s.adminToken, nil, nil)
	require.Equal(t, http.StatusOK, caRec.Code, caRec.Body.String())
	ca := parseCert(t, caRec.Body.String())
	assert.True(t, ca.IsCA)
	assert.Equal(t, ca.Subject.String(), panel.CASubject)
}

func TestServerTools_CACertificateDownloadLeaksNoPrivateKey(t *testing.T) {
	s := newServerToolsHarness(t, true)

	rec := s.do(http.MethodGet, "/api/v1/server/tls/ca.crt", s.adminToken, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := rec.Body.String()
	assert.Equal(t, "application/x-x509-ca-cert", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="file4base-local-ca.crt"`)
	assert.Contains(t, body, "BEGIN CERTIFICATE")
	assert.NotContains(t, body, "PRIVATE KEY")
	assert.True(t, parseCert(t, body).IsCA)
}

func TestServerTools_CertificateChainDownloadIsTheLeafAndTheAuthority(t *testing.T) {
	s := newServerToolsHarness(t, true)

	rec := s.do(http.MethodGet, "/api/v1/server/tls/bundle.pem", s.adminToken, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "PRIVATE KEY")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="file4base-chain.pem"`)

	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(body)))
	leaf := parseCert(t, body)
	_, err := leaf.Verify(x509.VerifyOptions{DNSName: "localhost", Roots: roots})
	assert.NoError(t, err, "the exported chain must verify on its own")
}

func TestServerTools_CertificateDownloadsNeedTheServerAdministrator(t *testing.T) {
	s := newServerToolsHarness(t, true)

	for _, path := range []string{"/api/v1/server/tls/ca.crt", "/api/v1/server/tls/bundle.pem"} {
		rec := s.do(http.MethodGet, path, s.aliceToken, nil, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "owner of another database: %s", path)
		assert.NotContains(t, rec.Body.String(), "BEGIN CERTIFICATE", path)

		anon := s.do(http.MethodGet, path, "", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, anon.Code, "without a session: %s", path)
	}
}

func TestServerTools_RenewRotatesTheLeafAndKeepsTheInstalledAuthority(t *testing.T) {
	s := newServerToolsHarness(t, true)

	var before tlsPanel
	require.Equal(t, http.StatusOK, s.do(http.MethodGet, "/api/v1/server/tls", s.adminToken, nil, &before).Code)
	caBefore := s.do(http.MethodGet, "/api/v1/server/tls/ca.crt", s.adminToken, nil, nil).Body.String()

	var renewed tlsPanel
	rec := s.do(http.MethodPost, "/api/v1/server/tls/renew", s.adminToken, map[string]string{}, &renewed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.NotEqual(t, before.CertSHA256, renewed.CertSHA256, "the leaf must be reissued")
	assert.Equal(t, before.CASHA256, renewed.CASHA256, "the authority must survive a renewal")

	caAfter := s.do(http.MethodGet, "/api/v1/server/tls/ca.crt", s.adminToken, nil, nil).Body.String()
	assert.Equal(t, caBefore, caAfter, "trust installed from the exported authority keeps working")

	// The reissued leaf still chains to the same authority.
	chain := s.do(http.MethodGet, "/api/v1/server/tls/bundle.pem", s.adminToken, nil, nil).Body.String()
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(caAfter)))
	_, err := parseCert(t, chain).Verify(x509.VerifyOptions{DNSName: "localhost", Roots: roots})
	assert.NoError(t, err)
}

func TestServerTools_ChangingTheDomainReissuesTheLeafUnderTheSameAuthority(t *testing.T) {
	s := newServerToolsHarness(t, true)
	caBefore := s.do(http.MethodGet, "/api/v1/server/tls/ca.crt", s.adminToken, nil, nil).Body.String()

	var updated tlsPanel
	rec := s.do(http.MethodPut, "/api/v1/server/tls", s.adminToken, map[string]interface{}{
		"domain":     "files.example.lan",
		"mode":       "self_signed",
		"extra_sans": []string{"192.168.1.40"},
	}, &updated)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "local_ca", updated.CertSource)
	assert.Contains(t, updated.CertSANs, "files.example.lan")
	assert.Contains(t, updated.CertSANs, "localhost")
	assert.Empty(t, updated.CustomKey)

	caAfter := s.do(http.MethodGet, "/api/v1/server/tls/ca.crt", s.adminToken, nil, nil).Body.String()
	assert.Equal(t, caBefore, caAfter, "a new domain must not force administrators to reinstall the authority")

	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(caAfter)))
	chain := s.do(http.MethodGet, "/api/v1/server/tls/bundle.pem", s.adminToken, nil, nil).Body.String()
	_, err := parseCert(t, chain).Verify(x509.VerifyOptions{DNSName: "192.168.1.40", Roots: roots})
	assert.NoError(t, err, "the certificate must cover the configured LAN address")
}
