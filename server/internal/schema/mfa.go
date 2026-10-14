package schema

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrMFARequired is returned by Authenticate when an account has Google
// Authenticator MFA enabled and no TOTP code was supplied with the login request.
var ErrMFARequired = errors.New("MFA verification code is required")

// ErrInvalidMFACode is returned when the supplied 6-digit TOTP code does not
// match the account's secret within the allowed ±1 time step window.
var ErrInvalidMFACode = errors.New("invalid MFA verification code")

// MFASetupInfo holds the provisioning parameters for Google Authenticator.
type MFASetupInfo struct {
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Issuer     string `json:"issuer"`
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
	MFAEnabled bool   `json:"mfa_enabled"`
}

// GenerateMFASecret generates a 160-bit random secret encoded as uppercase
// Base32 without padding, suitable for Google Authenticator (RFC 6238).
func GenerateMFASecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed generating MFA secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// BuildOTPAuthURI builds the standard otpauth://totp/ URI recognized by
// Google Authenticator and compatible TOTP apps.
func BuildOTPAuthURI(issuer, username, secret string) string {
	cleanIssuer := strings.TrimSpace(issuer)
	if cleanIssuer == "" {
		cleanIssuer = "File4Base"
	}
	label := url.PathEscape(fmt.Sprintf("%s:%s", cleanIssuer, strings.TrimSpace(username)))
	q := url.Values{}
	q.Set("secret", strings.ToUpper(strings.TrimSpace(secret)))
	q.Set("issuer", cleanIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode())
}

// ComputeTOTPCode calculates the 6-digit RFC 6238 TOTP code for a Base32
// secret at the given Unix timestamp.
func ComputeTOTPCode(secret string, at time.Time) (string, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	cleaned = strings.TrimRight(cleaned, "=")
	if cleaned == "" {
		return "", errors.New("empty MFA secret")
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid base32 MFA secret: %w", err)
	}

	counter := uint64(at.Unix() / 30)
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)

	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counterBytes[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	binCode := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]&0xff) << 16) |
		(uint32(sum[offset+2]&0xff) << 8) |
		uint32(sum[offset+3]&0xff)
	otp := binCode % 1000000
	return fmt.Sprintf("%06d", otp), nil
}

// VerifyTOTP validates a 6-digit TOTP code against the Base32 secret allowing
// ±1 30-second step for clock drift.
func VerifyTOTP(secret, code string, now time.Time) bool {
	trimmedCode := strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(trimmedCode) != 6 {
		return false
	}
	for _, offsetSec := range []int64{0, -30, 30} {
		expected, err := ComputeTOTPCode(secret, now.Add(time.Duration(offsetSec)*time.Second))
		if err == nil && hmac.Equal([]byte(expected), []byte(trimmedCode)) {
			return true
		}
	}
	return false
}

// SetupUserMFA generates (or retrieves) a TOTP secret for the user so it can
// be scanned with Google Authenticator before enabling MFA.
func (s *Service) SetupUserMFA(ctx context.Context, userID, databaseName string) (*MFASetupInfo, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	secret, err := GenerateMFASecret()
	if err != nil {
		return nil, err
	}

	issuer := "File4Base"
	if strings.TrimSpace(databaseName) != "" {
		issuer = fmt.Sprintf("File4Base (%s)", strings.TrimSpace(databaseName))
	}

	// If MFA is not currently enabled, persist the pending secret so EnableUserMFA
	// can verify against it even if the caller omits the secret in the request body.
	if !u.MFAEnabled {
		q := `UPDATE sys_users SET mfa_secret = $1, updated_at = $2 WHERE id = $3`
		if s.driver.Dialect().Engine() != dbal.EnginePostgres {
			q = `UPDATE sys_users SET mfa_secret = ?, updated_at = ? WHERE id = ?`
		}
		_, _ = s.driver.DB().ExecContext(ctx, q, secret, time.Now().UTC(), userID)
	}

	return &MFASetupInfo{
		UserID:     u.ID,
		Username:   u.Username,
		Issuer:     issuer,
		Secret:     secret,
		OTPAuthURI: BuildOTPAuthURI(issuer, u.Username, secret),
		MFAEnabled: u.MFAEnabled,
	}, nil
}

// EnableUserMFA verifies a 6-digit Google Authenticator code and activates MFA
// on the user account.
func (s *Service) EnableUserMFA(ctx context.Context, userID, secret, code string) error {
	if _, err := s.GetUser(ctx, userID); err != nil {
		return err
	}

	effectiveSecret := strings.TrimSpace(secret)
	if effectiveSecret == "" {
		qSec := `SELECT COALESCE(mfa_secret, '') FROM sys_users WHERE id = $1`
		if s.driver.Dialect().Engine() != dbal.EnginePostgres {
			qSec = `SELECT COALESCE(mfa_secret, '') FROM sys_users WHERE id = ?`
		}
		_ = s.driver.DB().QueryRowContext(ctx, qSec, userID).Scan(&effectiveSecret)
	}
	if effectiveSecret == "" {
		return errors.New("MFA secret has not been initialized; run MFA setup first")
	}

	if !VerifyTOTP(effectiveSecret, code, time.Now().UTC()) {
		return ErrInvalidMFACode
	}

	q := `UPDATE sys_users SET mfa_enabled = TRUE, mfa_secret = $1, updated_at = $2 WHERE id = $3`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `UPDATE sys_users SET mfa_enabled = TRUE, mfa_secret = ?, updated_at = ? WHERE id = ?`
	}
	_, err := s.driver.DB().ExecContext(ctx, q, effectiveSecret, time.Now().UTC(), userID)
	return err
}

// DisableUserMFA turns off MFA and clears the stored TOTP secret for the user.
func (s *Service) DisableUserMFA(ctx context.Context, userID string) error {
	if _, err := s.GetUser(ctx, userID); err != nil {
		return err
	}
	q := `UPDATE sys_users SET mfa_enabled = FALSE, mfa_secret = '', updated_at = $1 WHERE id = $2`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `UPDATE sys_users SET mfa_enabled = FALSE, mfa_secret = '', updated_at = ? WHERE id = ?`
	}
	_, err := s.driver.DB().ExecContext(ctx, q, time.Now().UTC(), userID)
	return err
}
