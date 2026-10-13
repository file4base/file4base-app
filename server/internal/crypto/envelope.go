package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// HeaderEncrypted indicates that the HTTP payload is encrypted with AES-256-GCM.
	HeaderEncrypted = "X-File4Base-Encrypted"
	// HeaderAcceptEncryption requests that the server encrypt the HTTP response body.
	HeaderAcceptEncryption = "Accept-Encryption"
	// HeaderOriginalContentType preserves the inner Content-Type when the response body is encrypted.
	HeaderOriginalContentType = "X-Original-Content-Type"

	// CipherAES256GCM is the identifier for the AES-256-GCM envelope.
	CipherAES256GCM = "aes-256-gcm"

	// DefaultChannelSecret is used when FILE4BASE_CHANNEL_SECRET is not set in the environment.
	DefaultChannelSecret = "file4base-default-channel-key-2026"

	nonceSize = 12
)

var (
	envelopeMagic = []byte{'F', '4', 'B', 'E', 0x01}
	// ErrInvalidEnvelope is returned when an encrypted payload is malformed or fails authentication.
	ErrInvalidEnvelope = errors.New("invalid or unauthenticated encrypted payload")
)

// DeriveKey derives a 256-bit AES key from a channel secret string.
func DeriveKey(secret string) [32]byte {
	s := strings.TrimSpace(secret)
	if s == "" {
		s = DefaultChannelSecret
	}
	return sha256.Sum256([]byte("file4base-channel-v1:" + s))
}

// KeyFingerprint returns a non-secret 16-hex-character identifier of the derived key.
func KeyFingerprint(key [32]byte) string {
	sum := sha256.Sum256(key[:])
	return hex.EncodeToString(sum[:8])
}

// Encrypt seals plaintext using AES-256-GCM with the "F4BE\x01" envelope header
// followed by a 12-byte random nonce and the authenticated ciphertext+tag.
func Encrypt(key [32]byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(envelopeMagic)+nonceSize+len(plaintext)+gcm.Overhead())
	out = append(out, envelopeMagic...)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, plaintext, envelopeMagic)
	return out, nil
}

// Decrypt opens an AES-256-GCM "F4BE\x01" envelope (raw binary or base64-encoded).
func Decrypt(key [32]byte, payload []byte) ([]byte, error) {
	raw := payload
	if !bytes.HasPrefix(raw, envelopeMagic) {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(payload)))
		if err != nil || !bytes.HasPrefix(decoded, envelopeMagic) {
			return nil, ErrInvalidEnvelope
		}
		raw = decoded
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	minLen := len(envelopeMagic) + nonceSize + gcm.Overhead()
	if len(raw) < minLen {
		return nil, ErrInvalidEnvelope
	}

	nonce := raw[len(envelopeMagic) : len(envelopeMagic)+nonceSize]
	ciphertext := raw[len(envelopeMagic)+nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, envelopeMagic)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return plaintext, nil
}

// HandshakeHandler returns a handler for GET /api/v1/crypto/handshake that
// reports the server's active payload cipher and key fingerprint.
func HandshakeHandler(secret string) http.HandlerFunc {
	key := DeriveKey(secret)
	fp := KeyFingerprint(key)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"cipher":          CipherAES256GCM,
			"key_fingerprint": fp,
			"tls":             r.TLS != nil,
		})
	}
}

// Middleware transparently decrypts incoming HTTP request bodies carrying
// X-File4Base-Encrypted: aes-256-gcm and encrypts outgoing HTTP responses when
// requested via X-File4Base-Encrypted or Accept-Encryption.
func Middleware(secret string) func(http.Handler) http.Handler {
	key := DeriveKey(secret)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCipher := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderEncrypted)))
			acceptCipher := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderAcceptEncryption)))

			if reqCipher == CipherAES256GCM && r.Body != nil {
				encryptedBody, err := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if err != nil {
					http.Error(w, `{"title":"Bad Request","detail":"failed reading encrypted request body"}`, http.StatusBadRequest)
					return
				}
				if len(encryptedBody) > 0 {
					plaintext, err := Decrypt(key, encryptedBody)
					if err != nil {
						w.Header().Set("Content-Type", "application/problem+json")
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"title":"Invalid Encrypted Payload","detail":"failed authenticating or decrypting request payload"}`))
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(plaintext))
					r.ContentLength = int64(len(plaintext))
					r.Header.Set("Content-Length", strconv.Itoa(len(plaintext)))
				} else {
					r.Body = io.NopCloser(bytes.NewReader(nil))
					r.ContentLength = 0
				}
			}

			wantEncryptedResp := reqCipher == CipherAES256GCM || acceptCipher == CipherAES256GCM
			if !wantEncryptedResp {
				next.ServeHTTP(w, r)
				return
			}

			rec := &responseBuffer{
				header: make(http.Header),
				status: http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			for k, vals := range rec.header {
				if strings.EqualFold(k, "Content-Length") {
					continue
				}
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}

			bodyBytes := rec.buf.Bytes()
			if len(bodyBytes) == 0 || rec.status == http.StatusNoContent {
				w.WriteHeader(rec.status)
				return
			}

			sealed, err := Encrypt(key, bodyBytes)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			if origCT := rec.header.Get("Content-Type"); origCT != "" {
				w.Header().Set(HeaderOriginalContentType, origCT)
			}
			w.Header().Set(HeaderEncrypted, CipherAES256GCM)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(len(sealed)))
			w.WriteHeader(rec.status)
			_, _ = w.Write(sealed)
		})
	}
}

type responseBuffer struct {
	header http.Header
	buf    bytes.Buffer
	status int
	wrote  bool
}

func (r *responseBuffer) Header() http.Header {
	return r.header
}

func (r *responseBuffer) WriteHeader(statusCode int) {
	if !r.wrote {
		r.status = statusCode
		r.wrote = true
	}
}

func (r *responseBuffer) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.buf.Write(b)
}

// SelfSignedTLSConfig generates an ephemeral ECDSA P-256 TLS certificate for
// encrypted HTTPS communication when no external certificate files are provided.
func SelfSignedTLSConfig() (*tls.Config, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"File4Base API Server"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "api", "server", "file4base-api"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}, nil
}
