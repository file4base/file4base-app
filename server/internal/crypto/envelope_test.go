package crypto

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt_RoundTripAndTamperDetection(t *testing.T) {
	key := DeriveKey("test-secret")
	plaintext := []byte(`{"database":"invoices","username":"admin"}`)

	sealed, err := Encrypt(key, plaintext)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(sealed, envelopeMagic))

	opened, err := Decrypt(key, sealed)
	require.NoError(t, err)
	assert.Equal(t, plaintext, opened)

	// Tamper with ciphertext byte
	sealed[len(sealed)-1] ^= 0xFF
	_, err = Decrypt(key, sealed)
	assert.ErrorIs(t, err, ErrInvalidEnvelope)
}

func TestMiddleware_EncryptsAndDecryptsRequestAndResponse(t *testing.T) {
	secret := "unit-test-channel-key"
	key := DeriveKey(secret)

	handler := Middleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"echo":` + string(body) + `}`))
	}))

	reqPlain := []byte(`"secret-solution-bundle"`)
	reqEncrypted, err := Encrypt(key, reqPlain)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/import", bytes.NewReader(reqEncrypted))
	req.Header.Set(HeaderEncrypted, CipherAES256GCM)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, CipherAES256GCM, rec.Header().Get(HeaderEncrypted))
	assert.Equal(t, "application/json", rec.Header().Get(HeaderOriginalContentType))

	respPlain, err := Decrypt(key, rec.Body.Bytes())
	require.NoError(t, err)
	assert.JSONEq(t, `{"echo":"secret-solution-bundle"}`, string(respPlain))
}

func TestMiddleware_RejectsOversizedEncryptedBody(t *testing.T) {
	orig := maxEncryptedRequestBytes
	maxEncryptedRequestBytes = 64 // tiny cap for the test
	defer func() { maxEncryptedRequestBytes = orig }()

	handlerRan := false
	handler := Middleware("unit-test-channel-key")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	// A body far larger than the cap, flagged as encrypted, must be rejected
	// before it is read into memory and before the handler runs.
	oversized := bytes.Repeat([]byte("A"), 4096)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/import", bytes.NewReader(oversized))
	req.Header.Set(HeaderEncrypted, CipherAES256GCM)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.False(t, handlerRan, "handler must not run when the encrypted body exceeds the limit")
}

func TestHandshakeAndSelfSignedTLS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/crypto/handshake", nil)
	rec := httptest.NewRecorder()
	HandshakeHandler("")(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, CipherAES256GCM, body["cipher"])
	assert.NotEmpty(t, body["key_fingerprint"])

	tlsCfg, err := SelfSignedTLSConfig()
	require.NoError(t, err)
	assert.Len(t, tlsCfg.Certificates, 1)
}
