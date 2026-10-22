package localca_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/localca"
)

func parse(t *testing.T, certPEM string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	require.NotNil(t, block, "no PEM block")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}

func TestEnsure_CreatesAnAuthorityAndALeafItSigned(t *testing.T) {
	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	mat, err := store.Ensure(localca.DefaultSubject("", nil, nil))
	require.NoError(t, err)

	ca := parse(t, mat.CACertPEM)
	leaf := parse(t, mat.CertPEM)

	assert.True(t, ca.IsCA)
	assert.True(t, ca.BasicConstraintsValid)
	assert.NotZero(t, ca.KeyUsage&x509.KeyUsageCertSign, "the authority must be allowed to sign certificates")
	assert.Equal(t, localca.CAName, ca.Subject.CommonName)
	assert.Greater(t, ca.NotAfter.Year()-ca.NotBefore.Year(), 8, "the authority is long-lived so installed trust keeps working")

	assert.False(t, leaf.IsCA)
	require.NoError(t, leaf.CheckSignatureFrom(ca), "the leaf must be signed by the authority")
	assert.Equal(t, localca.CAName, leaf.Issuer.CommonName)
	assert.Subset(t, leaf.DNSNames, []string{"localhost", "file4base-web", "file4base-api", "api", "server"})

	ips := []string{}
	for _, ip := range leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	assert.Subset(t, ips, []string{"127.0.0.1", "::1"})

	// A browser rejects a server certificate valid for more than 398 days.
	assert.Less(t, leaf.NotAfter.Sub(leaf.NotBefore).Hours()/24, 398.0)

	// Every file is where the listener and Nginx expect it.
	for _, path := range []string{mat.CACertPath, mat.CertPath, mat.KeyPath, mat.BundlePath} {
		_, err := os.Stat(path)
		assert.NoError(t, err, path)
	}
	assert.Equal(t, strings.TrimSpace(mat.CertPEM)+"\n"+strings.TrimSpace(mat.CACertPEM)+"\n", strings.ReplaceAll(mat.BundlePEM, "\r\n", "\n"))
}

func TestEnsure_PrivateKeysAreNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes")
	}
	dir := filepath.Join(t.TempDir(), "tls")
	store := localca.New(dir)
	_, err := store.Ensure(localca.DefaultSubject("", nil, nil))
	require.NoError(t, err)

	for _, name := range []string{"ca.key", "server.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, name)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
	}
}

func TestEnsure_ReusesTheMaterialAlreadyOnDisk(t *testing.T) {
	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	sub := localca.DefaultSubject("", nil, nil)
	first, err := store.Ensure(sub)
	require.NoError(t, err)

	second, err := store.Ensure(sub)
	require.NoError(t, err)

	// Nothing is regenerated: the authority and the leaf are byte-identical,
	// so a restart never invalidates trust an administrator installed.
	assert.Equal(t, first.CACertPEM, second.CACertPEM)
	assert.Equal(t, first.CertPEM, second.CertPEM)
	assert.Equal(t, first.KeyPEM, second.KeyPEM)
}

func TestIssueLeaf_RotatesTheLeafAndKeepsTheAuthority(t *testing.T) {
	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	sub := localca.DefaultSubject("", nil, nil)
	first, err := store.Ensure(sub)
	require.NoError(t, err)

	renewed, err := store.IssueLeaf(sub)
	require.NoError(t, err)

	assert.Equal(t, first.CACertPEM, renewed.CACertPEM, "renewal must not touch the authority")
	assert.NotEqual(t, first.CertPEM, renewed.CertPEM, "renewal must issue a new leaf")
	assert.NotEqual(t, first.KeyPEM, renewed.KeyPEM)

	ca := parse(t, renewed.CACertPEM)
	assert.Equal(t, parse(t, first.CACertPEM).SerialNumber, ca.SerialNumber)
	require.NoError(t, parse(t, renewed.CertPEM).CheckSignatureFrom(ca))
}

func TestEnsure_ReissuesTheLeafWhenTheConfiguredNamesChange(t *testing.T) {
	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	first, err := store.Ensure(localca.DefaultSubject("", nil, nil))
	require.NoError(t, err)

	moved, err := store.Ensure(localca.DefaultSubject("files.example.lan", []string{"192.168.1.40"}, nil))
	require.NoError(t, err)

	assert.Equal(t, first.CACertPEM, moved.CACertPEM, "a new domain must not replace the authority")
	leaf := parse(t, moved.CertPEM)
	assert.Equal(t, "files.example.lan", leaf.Subject.CommonName)
	assert.Contains(t, leaf.DNSNames, "files.example.lan")
	assert.Contains(t, leaf.DNSNames, "localhost")
	ips := []string{}
	for _, ip := range leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	assert.Contains(t, ips, "192.168.1.40")
	require.NoError(t, leaf.CheckSignatureFrom(parse(t, moved.CACertPEM)))
}

func TestEnsure_LeafChainsToTheAuthorityForAVerifier(t *testing.T) {
	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	mat, err := store.Ensure(localca.DefaultSubject("files.example.lan", nil, nil))
	require.NoError(t, err)

	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(mat.CACertPEM)))
	_, err = parse(t, mat.CertPEM).Verify(x509.VerifyOptions{DNSName: "files.example.lan", Roots: roots})
	assert.NoError(t, err, "a client that installed the authority must accept the served certificate")
}

func TestCACertPEM_ExportsNoPrivateKey(t *testing.T) {
	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	_, err := store.Ensure(localca.DefaultSubject("", nil, nil))
	require.NoError(t, err)

	caPEM, err := store.CACertPEM()
	require.NoError(t, err)
	assert.Contains(t, caPEM, "BEGIN CERTIFICATE")
	assert.NotContains(t, caPEM, "PRIVATE KEY")
	assert.True(t, parse(t, caPEM).IsCA)
}

func TestSubjectFromEnv_UsesTheConfiguredDomainAndExtraNames(t *testing.T) {
	t.Setenv("FILE4BASE_TLS_DOMAIN", "files.example.lan")
	t.Setenv("FILE4BASE_TLS_SANS", " 10.0.0.9 , nas.example.lan ,")

	store := localca.New(filepath.Join(t.TempDir(), "tls"))
	mat, err := store.Ensure(localca.SubjectFromEnv())
	require.NoError(t, err)

	leaf := parse(t, mat.CertPEM)
	assert.Equal(t, "files.example.lan", leaf.Subject.CommonName)
	assert.Contains(t, leaf.DNSNames, "nas.example.lan")
	ips := []string{}
	for _, ip := range leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	assert.Contains(t, ips, "10.0.0.9")
}

func TestDefaultDir_FollowsTheEnvironment(t *testing.T) {
	t.Setenv("FILE4BASE_TLS_DIR", "/app/data/tls")
	assert.Equal(t, "/app/data/tls", localca.DefaultDir())
	t.Setenv("FILE4BASE_TLS_DIR", "")
	assert.Equal(t, filepath.Join("data", "tls"), localca.DefaultDir())
}
