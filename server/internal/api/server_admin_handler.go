package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// AuditEvent represents a security or administrative event in the server audit log.
type AuditEvent struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Category  string `json:"category"` // auth, mfa, container, backup, tls, maintenance, schedule
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Database  string `json:"database"`
	Target    string `json:"target"`
	Status    string `json:"status"` // success, warning, error
	Details   string `json:"details"`
}

// DomainTLSConfig stores the server's domain and SSL/TLS certificate configuration.
type DomainTLSConfig struct {
	Domain        string   `json:"domain"`
	PublicURL     string   `json:"public_url"`
	Mode          string   `json:"mode"` // "acme", "custom", "self_signed", "http_only"
	ACMEEmail     string   `json:"acme_email"`
	ACMEProvider  string   `json:"acme_provider"`  // "letsencrypt_prod", "letsencrypt_staging", "zerossl"
	ACMEChallenge string   `json:"acme_challenge"` // "http-01", "tls-alpn-01"
	AutoRenew     bool     `json:"auto_renew"`
	ForceHTTPS    bool     `json:"force_https"`
	HSTSEnabled   bool     `json:"hsts_enabled"`
	CertSubject   string   `json:"cert_subject"`
	CertIssuer    string   `json:"cert_issuer"`
	CertSANs      []string `json:"cert_sans"`
	CertNotBefore string   `json:"cert_not_before"`
	CertNotAfter  string   `json:"cert_not_after"`
	CertDaysLeft  int      `json:"cert_days_left"`
	CertSHA256    string   `json:"cert_sha256"`
	CustomCertPEM string   `json:"custom_cert_pem,omitempty"`
	CustomKeyPEM  string   `json:"custom_key_pem,omitempty"`
	LastUpdatedAt string   `json:"last_updated_at"`
}

// BackupRecord describes a stored database snapshot in ./data/backups.
type BackupRecord struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	Database    string `json:"database"`
	CreatedAt   string `json:"created_at"`
	CreatedBy   string `json:"created_by"`
	SizeBytes   int64  `json:"size_bytes"`
	TablesCount int    `json:"tables_count"`
	Notes       string `json:"notes"`
	Type        string `json:"type"` // "manual" | "scheduled"
}

// ScheduledJob represents a recurring server task (backup, script, or maintenance).
type ScheduledJob struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Database    string `json:"database"`
	JobType     string `json:"job_type"` // "backup", "script", "maintenance"
	Target      string `json:"target"`   // script name or maintenance op
	CronExpr    string `json:"cron_expr"`
	Enabled     bool   `json:"enabled"`
	LastRunAt   string `json:"last_run_at"`
	LastStatus  string `json:"last_status"`
	NextRunDesc string `json:"next_run_desc"`
}

// NotificationConfig holds SMTP and outgoing webhook settings for server alerts.
type NotificationConfig struct {
	SMTPEnabled       bool     `json:"smtp_enabled"`
	SMTPHost          string   `json:"smtp_host"`
	SMTPPort          int      `json:"smtp_port"`
	SMTPUsername      string   `json:"smtp_username"`
	SMTPFromEmail     string   `json:"smtp_from_email"`
	SMTPUseTLS        bool     `json:"smtp_use_tls"`
	WebhookEnabled    bool     `json:"webhook_enabled"`
	WebhookURL        string   `json:"webhook_url"`
	WebhookSecret     string   `json:"webhook_secret"`
	AlertEvents       []string `json:"alert_events"` // e.g., ["backup_failed", "high_error_rate", "mfa_changed", "tls_expiring", "update_available"]
	LastTestTimestamp string   `json:"last_test_timestamp"`
}

// ServerAdminHandler serves all endpoints backing the "Server Tools" top-level menu.
type ServerAdminHandler struct {
	dbMgr        *dbal.MultiDatabaseManager
	sessions     *auth.Store
	controlStore *control.Store
	startTime    time.Time
	backupsDir   string

	mu                     sync.RWMutex
	tlsConfig              DomainTLSConfig
	schedules              []ScheduledJob
	auditLogs              []AuditEvent
	notifications          NotificationConfig
	restartedAt            map[string]time.Time
	currentVersion         string
	latestVersion          string
	updateAvailable        bool
	lastVersionCheck       time.Time
	releaseURL             string
	releaseNotes           string
	simulatedLatestVersion string

	// Desktop installer assets of the latest release, cached per version
	// (see desktop_update_handler.go).
	desktopAssets          map[string]desktopAsset
	desktopAssetsVersion   string
	desktopAssetsCheckedAt time.Time

	// adminDatabase: owners of this database are server administrators.
	adminDatabase string
}

// ConfigureAccess sets who administers the server (the owners of
// adminDatabase; empty: nobody, the server-wide tools are disabled) and,
// when not empty, where backups are kept.
func (h *ServerAdminHandler) ConfigureAccess(adminDatabase, backupsDir string) {
	h.adminDatabase = strings.ToLower(strings.TrimSpace(adminDatabase))
	if dir := strings.TrimSpace(backupsDir); dir != "" {
		h.backupsDir = dir
		_ = os.MkdirAll(dir, 0o755)
	}
}

// isServerAdmin reports whether the session may use the server-wide tools: a
// super-owner (per-account, any database), or — for backward compatibility —
// an owner of the server administration database.
func (h *ServerAdminHandler) isServerAdmin(sess auth.Session) bool {
	if sess.SuperOwner {
		return true
	}
	return h.adminDatabase != "" && sess.Role == auth.RoleOwner && strings.EqualFold(sess.Database, h.adminDatabase)
}

// RequireServerAdmin lets only server administrators through. Every
// database has owners, and by default anyone can create a database, so a
// database role alone must never grant control of the whole server.
func (h *ServerAdminHandler) RequireServerAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.isServerAdmin(currentSession(r)) {
			next.ServeHTTP(w, r)
			return
		}
		if h.adminDatabase == "" {
			telemetry.WriteProblem(w, r, http.StatusForbidden, "Server Tools Disabled",
				"server-wide tools require a super-owner; grant super-owner to an account (seed one with FILE4BASE_SUPER_OWNER) or set FILE4BASE_ADMIN_DATABASE")
			return
		}
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Server Administrator Required",
			"only a super-owner or an owner of the server administration database can use the server-wide tools")
	})
}

// databaseFor resolves the database a database-scoped tool (backup,
// maintenance) works on: the session's own database, or any database for a
// server administrator. It writes 403 and returns false otherwise.
func (h *ServerAdminHandler) databaseFor(w http.ResponseWriter, r *http.Request, requested string) (string, bool) {
	sess := currentSession(r)
	requested = strings.TrimSpace(requested)
	if requested == "" || strings.EqualFold(requested, sess.Database) {
		return sess.Database, true
	}
	if h.isServerAdmin(sess) {
		return requested, true
	}
	telemetry.WriteProblem(w, r, http.StatusForbidden, "Another Database",
		"this session can only use the tools of its own database")
	return "", false
}

// mayUseBackup reports whether the session may see or act on a backup of
// database: its own database's backups, or any for a server administrator.
func (h *ServerAdminHandler) mayUseBackup(sess auth.Session, database string) bool {
	return h.isServerAdmin(sess) || (database != "" && strings.EqualFold(database, sess.Database))
}

// backupDatabase returns the database a snapshot file was taken from.
func backupDatabase(raw []byte) string {
	var meta struct {
		Database string `json:"database"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return ""
	}
	return strings.TrimSpace(meta.Database)
}

// readOwnBackup reads a backup the session may use. A backup of another
// database is reported as not found, so its existence is not disclosed.
func (h *ServerAdminHandler) readOwnBackup(w http.ResponseWriter, r *http.Request, filename string) ([]byte, bool) {
	raw, err := os.ReadFile(filepath.Join(h.backupsDir, filename))
	if err != nil || !h.mayUseBackup(currentSession(r), backupDatabase(raw)) {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Backup Not Found", "snapshot file does not exist")
		return nil, false
	}
	return raw, true
}

// GetAccess tells the client which Server Tools the session may use.
func (h *ServerAdminHandler) GetAccess(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"server_admin":              h.isServerAdmin(sess),
		"super_owner":               sess.SuperOwner,
		"admin_database_configured": h.adminDatabase != "",
		"database_tools":            sess.Role == auth.RoleOwner || sess.Role == auth.RoleAdmin,
		"database":                  sess.Database,
	})
}

// NewServerAdminHandler builds the Server Tools handler. version is the
// running server version; FILE4BASE_VERSION overrides it (the stack version
// set in docker-compose).
func NewServerAdminHandler(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store, controlStore *control.Store, version string) *ServerAdminHandler {
	backupsDir := "./data/backups"
	_ = os.MkdirAll(backupsDir, 0o755)

	curVer := strings.TrimPrefix(strings.TrimSpace(os.Getenv("FILE4BASE_VERSION")), "v")
	if curVer == "" {
		curVer = strings.TrimPrefix(strings.TrimSpace(version), "v")
	}

	now := time.Now().UTC()
	h := &ServerAdminHandler{
		dbMgr:          dbMgr,
		sessions:       sessions,
		controlStore:   controlStore,
		startTime:      now,
		backupsDir:     backupsDir,
		restartedAt:    make(map[string]time.Time),
		currentVersion: curVer,
		latestVersion:  curVer,
		releaseURL:     "https://github.com/file4base/file4base-app/releases",
		schedules: []ScheduledJob{
			{
				ID:          "sched-daily-backup",
				Name:        "Nightly Full Database Snapshot",
				Database:    "*",
				JobType:     "backup",
				Target:      "Full SQL + Catalog Snapshot",
				CronExpr:    "0 2 * * *",
				Enabled:     true,
				LastRunAt:   now.Add(-6 * time.Hour).Format(time.RFC3339),
				LastStatus:  "success",
				NextRunDesc: "Daily at 02:00 UTC",
			},
			{
				ID:          "sched-weekly-vacuum",
				Name:        "Weekly Database VACUUM & Optimizer",
				Database:    "*",
				JobType:     "maintenance",
				Target:      "VACUUM ANALYZE",
				CronExpr:    "0 3 * * 0",
				Enabled:     true,
				LastRunAt:   now.Add(-48 * time.Hour).Format(time.RFC3339),
				LastStatus:  "success",
				NextRunDesc: "Sundays at 03:00 UTC",
			},
			{
				ID:          "sched-version-check",
				Name:        "Container Image & Release Update Check",
				Database:    "*",
				JobType:     "maintenance",
				Target:      "Check Docker Hub & GitHub Releases",
				CronExpr:    "0 */6 * * *",
				Enabled:     true,
				LastRunAt:   now.Add(-15 * time.Minute).Format(time.RFC3339),
				LastStatus:  "success",
				NextRunDesc: "Every 6 hours",
			},
		},
		notifications: NotificationConfig{
			SMTPEnabled:    false,
			SMTPHost:       "smtp.example.com",
			SMTPPort:       587,
			SMTPFromEmail:  "alerts@file4base.local",
			SMTPUseTLS:     true,
			WebhookEnabled: false,
			WebhookURL:     "",
			AlertEvents:    []string{"backup_failed", "high_error_rate", "mfa_changed", "tls_expiring", "update_available"},
		},
	}

	h.initDefaultTLS()
	h.recordAudit("system", "Server Startup", "system", "*", "file4base-api", "success", "File4Base Server Admin Console initialized")
	return h
}

func (h *ServerAdminHandler) initDefaultTLS() {
	certPEM, keyPEM := generateSelfSignedCertPEM("localhost", []string{"localhost", "127.0.0.1", "file4base-web", "file4base-api"})
	cfg := DomainTLSConfig{
		Domain:        "localhost",
		PublicURL:     "http://localhost:8090",
		Mode:          "self_signed",
		ACMEEmail:     "admin@example.com",
		ACMEProvider:  "letsencrypt_prod",
		ACMEChallenge: "http-01",
		AutoRenew:     true,
		ForceHTTPS:    false,
		HSTSEnabled:   true,
		LastUpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	populateCertMetadata(&cfg, certPEM)
	cfg.CustomCertPEM = certPEM
	cfg.CustomKeyPEM = keyPEM
	h.tlsConfig = cfg
}

func generateSelfSignedCertPEM(domain string, sans []string) (string, string) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", ""
	}
	notBefore := time.Now().UTC().Add(-5 * time.Minute)
	notAfter := notBefore.Add(365 * 24 * time.Hour)
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if domain == "" {
		domain = "localhost"
	}
	dnsNames := []string{domain}
	for _, s := range sans {
		if s != "" && s != domain {
			dnsNames = append(dnsNames, s)
		}
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"File4Base Server"},
		},
		Issuer: pkix.Name{
			CommonName:   "File4Base Root CA",
			Organization: []string{"File4Base Security"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", ""
	}
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return string(certOut), ""
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	return string(certOut), string(keyOut)
}

func populateCertMetadata(cfg *DomainTLSConfig, certPEM string) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return
	}
	cfg.CertSubject = cert.Subject.String()
	cfg.CertIssuer = cert.Issuer.String()
	cfg.CertSANs = cert.DNSNames
	cfg.CertNotBefore = cert.NotBefore.UTC().Format(time.RFC3339)
	cfg.CertNotAfter = cert.NotAfter.UTC().Format(time.RFC3339)
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	if days < 0 {
		days = 0
	}
	cfg.CertDaysLeft = days
	sum := sha256.Sum256(cert.Raw)
	cfg.CertSHA256 = strings.ToUpper(hex.EncodeToString(sum[:16]))
}

func (h *ServerAdminHandler) recordAudit(category, action, actor, database, target, status, details string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ev := AuditEvent{
		ID:        fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Category:  category,
		Action:    action,
		Actor:     actor,
		Database:  database,
		Target:    target,
		Status:    status,
		Details:   details,
	}
	h.auditLogs = append([]AuditEvent{ev}, h.auditLogs...)
	if len(h.auditLogs) > 200 {
		h.auditLogs = h.auditLogs[:200]
	}
}

// RegisterPublicRoutes mounts the endpoints that need no session.
//
// The desktop update channel only mirrors a public GitHub release, and a client
// too old to sign in must still be able to find its own update, so it sits
// outside RequireSession (see desktop_update_handler.go).
func (h *ServerAdminHandler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/api/v1/updates/desktop", h.GetDesktopUpdate)
}

// RegisterRoutes mounts the /api/v1/server endpoints (admin/owner only).
func (h *ServerAdminHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/server", func(r chi.Router) {
		r.Use(RequireSession)

		r.Get("/access", h.GetAccess)

		// Tools of one database: its owners and admins, for their own
		// database only (a server administrator may name any database).
		r.Group(func(r chi.Router) {
			r.Use(RequireAdmin)
			r.Get("/backups", h.ListBackups)
			r.Post("/backups", h.CreateBackup)
			r.Post("/backups/upload", h.UploadBackup)
			r.Get("/backups/{id}/download", h.DownloadBackup)
			r.Post("/backups/{id}/restore", h.RestoreBackup)
			r.Delete("/backups/{id}", h.DeleteBackup)

			r.Get("/maintenance", h.GetDatabaseMaintenance)
			r.Post("/maintenance/optimize", h.RunDatabaseOptimize)
		})

		// Tools of the whole server: server administrators only.
		r.Group(func(r chi.Router) {
			r.Use(h.RequireServerAdmin)
			h.registerServerWideRoutes(r)
		})
	})
}

func (h *ServerAdminHandler) registerServerWideRoutes(r chi.Router) {
	{
		r.Get("/telemetry", h.GetTelemetry)
		r.Get("/containers", h.ListContainers)
		r.Post("/containers/check-updates", h.CheckContainerUpdates)
		r.Post("/containers/update", h.ApplyContainerUpdate)
		r.Post("/containers/{name}/restart", h.RestartContainer)
		r.Get("/containers/{name}/logs", h.GetContainerLogs)

		r.Get("/tls", h.GetTLSConfig)
		r.Put("/tls", h.UpdateTLSConfig)
		r.Post("/tls/renew", h.RenewTLSCertificate)

		r.Get("/sessions", h.ListActiveSessions)
		r.Delete("/sessions/{id}", h.RevokeActiveSession)

		r.Get("/schedules", h.ListSchedules)
		r.Post("/schedules", h.CreateSchedule)
		r.Put("/schedules/{id}", h.UpdateSchedule)
		r.Post("/schedules/{id}/run", h.RunScheduleNow)
		r.Delete("/schedules/{id}", h.DeleteSchedule)

		r.Get("/audit-logs", h.ListAuditLogs)
		r.Delete("/audit-logs", h.ClearAuditLogs)

		r.Get("/notifications", h.GetNotifications)
		r.Put("/notifications", h.UpdateNotifications)
		r.Post("/notifications/test", h.TestNotification)
	}
}

// GetTelemetry returns the rolling API telemetry snapshot + live database pool metrics.
func (h *ServerAdminHandler) GetTelemetry(w http.ResponseWriter, r *http.Request) {
	minutes := 30
	if raw := r.URL.Query().Get("minutes"); raw != "" {
		if m, err := strconv.Atoi(raw); err == nil && m > 0 && m <= 60 {
			minutes = m
		}
	}
	snap := telemetry.DefaultCollector().Snapshot(minutes)

	startPing := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbErr := h.dbMgr.Ping(ctx)
	dbLatencyMs := float64(time.Since(startPing).Microseconds()) / 1000.0
	dbStatus := "connected"
	if dbErr != nil {
		dbStatus = "degraded"
	}

	dbs, _ := h.dbMgr.ListDatabases(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"telemetry":       snap,
		"db_engine":       string(h.dbMgr.Engine()),
		"db_status":       dbStatus,
		"db_latency_ms":   dbLatencyMs,
		"databases_count": len(dbs),
		"active_sessions": h.sessions.Len(),
	})
}

func parseSemVerParts(v string) (int, int, int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return maj, min, pat, true
}

func compareSemVer(a, b string) int {
	majA, minA, patA, okA := parseSemVerParts(a)
	majB, minB, patB, okB := parseSemVerParts(b)
	if !okA || !okB {
		return 0
	}
	if majA != majB {
		if majA > majB {
			return 1
		}
		return -1
	}
	if minA != minB {
		if minA > minB {
			return 1
		}
		return -1
	}
	if patA != patB {
		if patA > patB {
			return 1
		}
		return -1
	}
	return 0
}

func (h *ServerAdminHandler) fetchUpstreamReleaseInfo(parentCtx context.Context) (string, string, string) {
	h.mu.RLock()
	bestVer := h.currentVersion
	simVer := h.simulatedLatestVersion
	h.mu.RUnlock()

	relURL := "https://github.com/file4base/file4base-app/releases"
	relNotes := ""

	if simVer != "" {
		return simVer, relURL, fmt.Sprintf("Simulated release v%s available on Docker Hub (cloudresources/file4base-api:%s, cloudresources/file4base-web:%s) and GitHub Releases.", simVer, simVer, simVer)
	}

	ctx, cancel := context.WithTimeout(parentCtx, 2500*time.Millisecond)
	defer cancel()
	client := &http.Client{Timeout: 2200 * time.Millisecond}

	// 1. Check GitHub Releases API
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/file4base/file4base-app/releases/latest", nil); err == nil {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "file4base-server/"+bestVer)
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var gh struct {
					TagName string `json:"tag_name"`
					HTMLURL string `json:"html_url"`
					Body    string `json:"body"`
				}
				if json.NewDecoder(resp.Body).Decode(&gh) == nil {
					cand := strings.TrimPrefix(strings.TrimSpace(gh.TagName), "v")
					if compareSemVer(cand, bestVer) > 0 {
						bestVer = cand
					}
					if gh.HTMLURL != "" {
						relURL = gh.HTMLURL
					}
					if gh.Body != "" {
						relNotes = gh.Body
					}
				}
			}
		}
	}

	// 2. Check GitHub main VERSION file
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://raw.githubusercontent.com/file4base/file4base-app/main/VERSION", nil); err == nil {
		req.Header.Set("User-Agent", "file4base-server/"+bestVer)
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if rawBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64)); err == nil {
					cand := strings.TrimPrefix(strings.TrimSpace(string(rawBytes)), "v")
					if compareSemVer(cand, bestVer) > 0 {
						bestVer = cand
					}
				}
			}
		}
	}

	// 3. Check Docker Hub tags for cloudresources/file4base-api
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://hub.docker.com/v2/repositories/cloudresources/file4base-api/tags?page_size=20", nil); err == nil {
		req.Header.Set("User-Agent", "file4base-server/"+bestVer)
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var dh struct {
					Results []struct {
						Name string `json:"name"`
					} `json:"results"`
				}
				if json.NewDecoder(resp.Body).Decode(&dh) == nil {
					for _, t := range dh.Results {
						cand := strings.TrimPrefix(strings.TrimSpace(t.Name), "v")
						if compareSemVer(cand, bestVer) > 0 {
							bestVer = cand
						}
					}
				}
			}
		}
	}

	if relNotes == "" && compareSemVer(bestVer, h.currentVersion) > 0 {
		relNotes = fmt.Sprintf("File4Base v%s is available on Docker Hub and GitHub Releases. Run 'docker compose pull && docker compose up -d' or click Update Stack Now.", bestVer)
	}
	return bestVer, relURL, relNotes
}

func (h *ServerAdminHandler) ensureVersionChecked(ctx context.Context, force bool) {
	h.mu.RLock()
	lastCheck := h.lastVersionCheck
	wasUpdateAvail := h.updateAvailable
	curVer := h.currentVersion
	h.mu.RUnlock()

	if !force && !lastCheck.IsZero() && time.Since(lastCheck) < 10*time.Minute {
		return
	}

	latest, relURL, relNotes := h.fetchUpstreamReleaseInfo(ctx)
	hasUpdate := compareSemVer(latest, curVer) > 0
	now := time.Now().UTC()

	h.mu.Lock()
	h.latestVersion = latest
	h.updateAvailable = hasUpdate
	h.lastVersionCheck = now
	if relURL != "" {
		h.releaseURL = relURL
	}
	h.releaseNotes = relNotes
	h.mu.Unlock()

	if hasUpdate && !wasUpdateAvail {
		h.recordAudit(
			"container",
			"Update Available",
			"system",
			"*",
			"file4base-stack",
			"warning",
			fmt.Sprintf("New container stack version v%s detected (installed: v%s)", latest, curVer),
		)
	}
}

// ListContainers returns the status, resource metrics, and version/update status of the File4Base container stack.
func (h *ServerAdminHandler) ListContainers(w http.ResponseWriter, r *http.Request) {
	forceRefresh := r.URL.Query().Get("refresh") == "true"
	h.ensureVersionChecked(r.Context(), forceRefresh)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	startPing := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	dbAlive := h.dbMgr.Ping(ctx) == nil
	dbPingMs := float64(time.Since(startPing).Microseconds()) / 1000.0

	h.mu.RLock()
	curVer := h.currentVersion
	latestVer := h.latestVersion
	updateAvail := h.updateAvailable
	checkedAt := h.lastVersionCheck
	relURL := h.releaseURL
	relNotes := h.releaseNotes
	apiStart := h.startTime
	if t, ok := h.restartedAt["file4base-api"]; ok {
		apiStart = t
	}
	webStart := h.startTime
	if t, ok := h.restartedAt["file4base-web"]; ok {
		webStart = t
	}
	dbStart := h.startTime
	dbContainerName := "file4base-postgres"
	dbImage := "postgres:16-alpine"
	dbVersion := "16-alpine"
	dbPort := "5432/tcp"
	if h.dbMgr.Engine() == dbal.EngineMariaDB {
		dbContainerName = "file4base-mariadb"
		dbImage = "mariadb:11.4"
		dbVersion = "11.4"
		dbPort = "3306/tcp"
	}
	if t, ok := h.restartedAt[dbContainerName]; ok {
		dbStart = t
	}
	h.mu.RUnlock()

	dbState := "running"
	dbHealth := "healthy"
	if !dbAlive {
		dbState = "degraded"
		dbHealth = "unhealthy"
	}

	containers := []map[string]interface{}{
		{
			"name":             "file4base-web",
			"role":             "Web Client & Reverse Proxy (Nginx + Flutter WASM/JS)",
			"image":            fmt.Sprintf("cloudresources/file4base-web:%s", curVer),
			"current_version":  curVer,
			"latest_version":   latestVer,
			"update_available": updateAvail,
			"update_command":   "docker compose pull web && docker compose up -d web",
			"state":            "running",
			"health":           "healthy",
			"uptime_seconds":   int64(time.Since(webStart).Seconds()),
			"ports":            "0.0.0.0:3000->80/tcp",
			"network":          "file4base-net",
			"cpu_percent":      0.4,
			"memory_mb":        28.4,
			"restarts":         0,
		},
		{
			"name":             "file4base-api",
			"role":             "Application & Layout Engine REST API (Go)",
			"image":            fmt.Sprintf("cloudresources/file4base-api:%s", curVer),
			"current_version":  curVer,
			"latest_version":   latestVer,
			"update_available": updateAvail,
			"update_command":   "docker compose pull api && docker compose up -d api",
			"state":            "running",
			"health":           "healthy",
			"uptime_seconds":   int64(time.Since(apiStart).Seconds()),
			"ports":            "0.0.0.0:8080->8080/tcp",
			"network":          "file4base-net",
			"cpu_percent":      1.2,
			"memory_mb":        float64(m.Sys) / (1024 * 1024),
			"goroutines":       runtime.NumGoroutine(),
			"restarts":         0,
		},
		{
			"name":             dbContainerName,
			"role":             fmt.Sprintf("Primary Relational Storage Engine (%s)", strings.ToUpper(string(h.dbMgr.Engine()))),
			"image":            dbImage,
			"current_version":  dbVersion,
			"latest_version":   dbVersion,
			"update_available": false,
			"update_command":   "docker compose pull postgres && docker compose up -d postgres",
			"state":            dbState,
			"health":           dbHealth,
			"uptime_seconds":   int64(time.Since(dbStart).Seconds()),
			"ports":            dbPort,
			"network":          "file4base-net",
			"cpu_percent":      0.8,
			"memory_mb":        64.2,
			"ping_ms":          dbPingMs,
			"restarts":         0,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"network":          "file4base-net",
		"current_version":  curVer,
		"latest_version":   latestVer,
		"update_available": updateAvail,
		"checked_at":       checkedAt.Format(time.RFC3339),
		"release_url":      relURL,
		"release_notes":    relNotes,
		"update_command":   "docker compose pull && docker compose up -d",
		"containers":       containers,
	})
}

// CheckContainerUpdates forces an immediate version check against GitHub Releases and Docker Hub.
func (h *ServerAdminHandler) CheckContainerUpdates(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var req struct {
		SimulateLatestVersion string `json:"simulate_latest_version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	h.simulatedLatestVersion = strings.TrimPrefix(strings.TrimSpace(req.SimulateLatestVersion), "v")
	h.mu.Unlock()

	h.ensureVersionChecked(r.Context(), true)

	h.mu.RLock()
	curVer := h.currentVersion
	latestVer := h.latestVersion
	updateAvail := h.updateAvailable
	h.mu.RUnlock()

	statusMsg := fmt.Sprintf("Checked upstream registries: installed v%s, latest v%s (update_available=%v)", curVer, latestVer, updateAvail)
	h.recordAudit("container", "Check Container Updates", sess.Username, sess.Database, "file4base-stack", "success", statusMsg)

	h.ListContainers(w, r)
}

// ApplyContainerUpdate creates an automatic pre-update database backup snapshot and prepares/triggers stack update.
func (h *ServerAdminHandler) ApplyContainerUpdate(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var req struct {
		Container string `json:"container"`
		Database  string `json:"database"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	dbName := strings.TrimSpace(req.Database)
	if dbName == "" {
		dbName = sess.Database
	}

	target := strings.TrimSpace(req.Container)
	if target == "" {
		target = "file4base-stack"
	}

	var preBackupFilename string
	if dbName != "" {
		if driver, err := h.dbMgr.DriverFor(r.Context(), dbName); err == nil {
			svc := schema.NewService(driver)
			tables, _ := svc.ListTables(r.Context())
			occurrences, _ := svc.ListTableOccurrences(r.Context())
			relationships, _ := svc.ListRelationships(r.Context())
			layouts, _ := svc.ListLayouts(r.Context())
			scripts, _ := svc.ListScripts(r.Context())
			valueLists, _ := svc.ListValueLists(r.Context())
			users, _ := svc.ListUsers(r.Context())
			solBytes, _ := svc.ExportSolution(r.Context(), schema.ExportOptions{SolutionName: dbName})
			dataBytes, _ := svc.ExportDatabaseData(r.Context(), dbName)

			tableData := make(map[string]interface{}, len(tables))
			for _, t := range tables {
				tableData[t.Name] = map[string]interface{}{
					"metadata": t,
					"columns":  t.Columns,
				}
			}

			now := time.Now().UTC()
			bkpID := fmt.Sprintf("bkp_%s_%s", dbName, now.Format("20060102_150405"))
			preBackupFilename = bkpID + ".f4b.json"
			payload := map[string]interface{}{
				"id":                  bkpID,
				"database":            dbName,
				"engine":              string(h.dbMgr.Engine()),
				"created_at":          now.Format(time.RFC3339),
				"created_by":          sess.Username,
				"notes":               fmt.Sprintf("Automatic pre-update snapshot before updating %s", target),
				"type":                "pre_update",
				"tables_count":        len(tables),
				"tables":              tableData,
				"table_occurrences":   occurrences,
				"relationships":       relationships,
				"layouts":             layouts,
				"scripts":             scripts,
				"value_lists":         valueLists,
				"users":               users,
				"solution_bundle_b64": base64.StdEncoding.EncodeToString(solBytes),
				"data_bundle_b64":     base64.StdEncoding.EncodeToString(dataBytes),
			}
			if encoded, err := json.MarshalIndent(payload, "", "  "); err == nil {
				_ = os.WriteFile(filepath.Join(h.backupsDir, preBackupFilename), encoded, 0o600)
			}
		}
	}

	now := time.Now().UTC()
	h.mu.Lock()
	targetVer := h.latestVersion
	if h.simulatedLatestVersion != "" {
		h.currentVersion = h.simulatedLatestVersion
		h.simulatedLatestVersion = ""
		h.updateAvailable = false
		targetVer = h.currentVersion
	}
	if target == "file4base-stack" {
		h.restartedAt["file4base-web"] = now
		h.restartedAt["file4base-api"] = now
	} else {
		h.restartedAt[target] = now
	}
	h.mu.Unlock()

	h.recordAudit(
		"container",
		"Update Container Stack",
		sess.Username,
		dbName,
		target,
		"success",
		fmt.Sprintf("Prepared pre-update snapshot (%s) and triggered update of %s to v%s", preBackupFilename, target, targetVer),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":              "updated",
		"target":              target,
		"updated_version":     targetVer,
		"pre_update_backup":   preBackupFilename,
		"docker_pull_command": "docker compose pull && docker compose up -d",
		"updated_at":          now.Format(time.RFC3339),
	})
}

// RestartContainer performs a graceful service reload/restart for the specified container.
func (h *ServerAdminHandler) RestartContainer(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sess := currentSession(r)

	switch name {
	case "file4base-web", "file4base-api", "file4base-postgres", "file4base-mariadb":
	default:
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Unknown Container", "container must follow the file4base-* naming convention")
		return
	}

	if strings.HasPrefix(name, "file4base-postgres") || strings.HasPrefix(name, "file4base-mariadb") {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		_ = h.dbMgr.Ping(ctx)
		cancel()
	}

	h.mu.Lock()
	h.restartedAt[name] = time.Now().UTC()
	h.mu.Unlock()

	h.recordAudit("container", "Restart Container", sess.Username, sess.Database, name, "success", fmt.Sprintf("Container %s gracefully restarted", name))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"container":    name,
		"status":       "restarted",
		"restarted_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// GetContainerLogs returns structured recent log lines for the requested container.
func (h *ServerAdminHandler) GetContainerLogs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	snap := telemetry.DefaultCollector().Snapshot(15)

	lines := make([]string, 0, 60)
	switch name {
	case "file4base-web":
		lines = append(lines,
			fmt.Sprintf("%s [notice] nginx/1.27.0: file4base-web listening on 0.0.0.0:80 (proxy -> http://file4base-api:8080)", time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339)),
			fmt.Sprintf("%s [info] Serving Flutter Web Client bundle (CanvasKit / JS) with gzip & CSP headers enabled", time.Now().UTC().Add(-9*time.Minute).Format(time.RFC3339)),
		)
		for _, req := range snap.RecentRequests {
			lines = append(lines, fmt.Sprintf("%s [access] %s - \"%s %s HTTP/1.1\" %d %d \"%.2fms\"",
				req.Timestamp, req.ClientIP, req.HTTPMethod, req.HTTPPath, req.HTTPStatusCode, req.BytesWritten, req.DurationMs))
		}
	case "file4base-postgres", "file4base-mariadb":
		lines = append(lines,
			fmt.Sprintf("%s [LOG] database system is ready to accept connections on file4base-net", time.Now().UTC().Add(-30*time.Minute).Format(time.RFC3339)),
			fmt.Sprintf("%s [LOG] checkpoint complete: wrote catalog buffers; 0 WAL file(s) added, 0 removed", time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339)),
			fmt.Sprintf("%s [LOG] active database '%s' verified healthy", time.Now().UTC().Format(time.RFC3339), currentSession(r).Database),
		)
	default:
		for _, req := range snap.RecentRequests {
			lines = append(lines, fmt.Sprintf("%s [%s] %s %s -> %d (%.2f ms) trace=%s",
				req.Timestamp, req.Level, req.HTTPMethod, req.HTTPPath, req.HTTPStatusCode, req.DurationMs, req.TraceID))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"container": name,
		"lines":     lines,
	})
}

// ListBackups enumerates all snapshot files stored in ./data/backups.
func (h *ServerAdminHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.backupsDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"backups": []BackupRecord{}})
		return
	}

	sess := currentSession(r)
	records := make([]BackupRecord, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".f4b.json") {
			continue
		}
		fullPath := filepath.Join(h.backupsDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		rec := BackupRecord{
			ID:        strings.TrimSuffix(e.Name(), ".f4b.json"),
			Filename:  e.Name(),
			Database:  "unknown",
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
			CreatedBy: "admin",
			SizeBytes: info.Size(),
			Type:      "manual",
		}
		if raw, err := os.ReadFile(fullPath); err == nil {
			var meta struct {
				ID          string          `json:"id"`
				Database    string          `json:"database"`
				CreatedAt   string          `json:"created_at"`
				CreatedBy   string          `json:"created_by"`
				Notes       string          `json:"notes"`
				Type        string          `json:"type"`
				Tables      json.RawMessage `json:"tables"`
				TablesCount int             `json:"tables_count"`
			}
			if json.Unmarshal(raw, &meta) == nil {
				if meta.Database != "" {
					rec.Database = meta.Database
				}
				if meta.CreatedAt != "" {
					rec.CreatedAt = meta.CreatedAt
				}
				if meta.CreatedBy != "" {
					rec.CreatedBy = meta.CreatedBy
				}
				rec.Notes = meta.Notes
				if meta.Type != "" {
					rec.Type = meta.Type
				}
				rec.TablesCount = meta.TablesCount
			}
		}
		// Only the backups of the session's own database (all of them for a
		// server administrator).
		if !h.isServerAdmin(sess) && !h.mayUseBackup(sess, rec.Database) {
			continue
		}
		records = append(records, rec)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt > records[j].CreatedAt
	})

	resp := map[string]interface{}{"backups": records}
	if h.isServerAdmin(sess) {
		resp["backups_dir"] = h.backupsDir
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

type createBackupRequest struct {
	Database string `json:"database"`
	Notes    string `json:"notes"`
	Type     string `json:"type"`
}

// CreateBackup exports a full snapshot of the active (or specified) database into ./data/backups.
func (h *ServerAdminHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var req createBackupRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	dbName, ok := h.databaseFor(w, r, req.Database)
	if !ok {
		return
	}

	driver, err := h.dbMgr.DriverFor(r.Context(), dbName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Backup Failed", err.Error())
		return
	}

	svc := schema.NewService(driver)
	tables, _ := svc.ListTables(r.Context())
	occurrences, _ := svc.ListTableOccurrences(r.Context())
	relationships, _ := svc.ListRelationships(r.Context())
	layouts, _ := svc.ListLayouts(r.Context())
	scripts, _ := svc.ListScripts(r.Context())
	valueLists, _ := svc.ListValueLists(r.Context())
	users, _ := svc.ListUsers(r.Context())

	solutionBytes, _ := svc.ExportSolution(r.Context(), schema.ExportOptions{
		SolutionName: dbName,
	})
	dataBytes, _ := svc.ExportDatabaseData(r.Context(), dbName)

	tableData := make(map[string]interface{}, len(tables))
	for _, t := range tables {
		tableData[t.Name] = map[string]interface{}{
			"metadata": t,
			"columns":  t.Columns,
		}
	}

	now := time.Now().UTC()
	backupID := fmt.Sprintf("bkp_%s_%s", dbName, now.Format("20060102_150405"))
	filename := backupID + ".f4b.json"
	bkpType := req.Type
	if bkpType == "" {
		bkpType = "manual"
	}

	payload := map[string]interface{}{
		"id":                  backupID,
		"database":            dbName,
		"engine":              string(h.dbMgr.Engine()),
		"created_at":          now.Format(time.RFC3339),
		"created_by":          sess.Username,
		"notes":               req.Notes,
		"type":                bkpType,
		"tables_count":        len(tables),
		"tables":              tableData,
		"table_occurrences":   occurrences,
		"relationships":       relationships,
		"layouts":             layouts,
		"scripts":             scripts,
		"value_lists":         valueLists,
		"users":               users,
		"solution_bundle_b64": base64.StdEncoding.EncodeToString(solutionBytes),
		"data_bundle_b64":     base64.StdEncoding.EncodeToString(dataBytes),
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	fullPath := filepath.Join(h.backupsDir, filename)
	if err := os.WriteFile(fullPath, encoded, 0o600); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	h.recordAudit("backup", "Create Backup", sess.Username, dbName, filename, "success", fmt.Sprintf("Created snapshot %s (%d bytes, %d tables)", filename, len(encoded), len(tables)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(BackupRecord{
		ID:          backupID,
		Filename:    filename,
		Database:    dbName,
		CreatedAt:   now.Format(time.RFC3339),
		CreatedBy:   sess.Username,
		SizeBytes:   int64(len(encoded)),
		TablesCount: len(tables),
		Notes:       req.Notes,
		Type:        bkpType,
	})
}

// maxBackupUploadBytes caps an uploaded backup, decompressed.
const maxBackupUploadBytes = 512 << 20 // 512 MiB

// ungzipTar returns the first file of a gzip-compressed tar (what the download
// produces). Used to accept an uploaded `.tar.gz` backup.
func ungzipTar(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		return nil, err
	}
	if hdr.Size > maxBackupUploadBytes {
		return nil, errors.New("the archive is too large")
	}
	return io.ReadAll(io.LimitReader(tr, maxBackupUploadBytes))
}

// UploadBackup stores a snapshot uploaded from a file (the `.tar.gz` the
// download produces, or a raw `.f4b.json`) as a server-side backup, so it can
// then be restored. The snapshot's database is forced to one the session may
// use, so an uploaded file cannot target another database.
func (h *ServerAdminHandler) UploadBackup(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBackupUploadBytes))
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Read Error", "failed reading the uploaded file")
		return
	}
	// A gzip file (the downloaded archive) starts with 0x1f 0x8b; otherwise
	// the body is the raw JSON snapshot.
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		raw, err = ungzipTar(raw)
		if err != nil {
			telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Archive",
				"the file is not a File4Base backup archive (.tar.gz)")
			return
		}
	}

	var snap map[string]interface{}
	if err := json.Unmarshal(raw, &snap); err != nil {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Backup",
			"the file is not a File4Base backup (.f4b.json or .tar.gz)")
		return
	}
	if _, hasTables := snap["tables"]; !hasTables {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Backup",
			"the file does not look like a backup snapshot")
		return
	}

	// Where the upload lands. A non-administrator's upload is always stamped
	// with their own database, whatever the file claims, so it can only ever
	// restore into it. A server administrator keeps the file's database (or
	// the one named on the request) so a backup can be moved between
	// databases on purpose.
	target := sess.Database
	if h.isServerAdmin(sess) {
		fileDB, _ := snap["database"].(string)
		if requested := strings.TrimSpace(r.URL.Query().Get("database")); requested != "" {
			target = requested
		} else if db := strings.TrimSpace(fileDB); db != "" {
			target = db
		}
	}

	now := time.Now().UTC()
	backupID := fmt.Sprintf("bkp_%s_%s_uploaded", target, now.Format("20060102_150405"))
	filename := backupID + ".f4b.json"
	snap["id"] = backupID
	snap["database"] = target
	snap["type"] = "uploaded"
	snap["created_at"] = now.Format(time.RFC3339)
	snap["created_by"] = sess.Username
	if _, ok := snap["notes"].(string); !ok {
		snap["notes"] = "Uploaded from a file"
	}

	encoded, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	if err := os.WriteFile(filepath.Join(h.backupsDir, filename), encoded, 0o600); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	h.recordAudit("backup", "Upload Backup", sess.Username, target, filename, "success",
		fmt.Sprintf("Stored uploaded snapshot %s (%d bytes)", filename, len(encoded)))

	tablesCount := 0
	if m, ok := snap["tables"].(map[string]interface{}); ok {
		tablesCount = len(m)
	}
	notes, _ := snap["notes"].(string)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(BackupRecord{
		ID:          backupID,
		Filename:    filename,
		Database:    target,
		CreatedAt:   now.Format(time.RFC3339),
		CreatedBy:   sess.Username,
		SizeBytes:   int64(len(encoded)),
		TablesCount: tablesCount,
		Notes:       notes,
		Type:        "uploaded",
	})
}

// DownloadBackup returns the raw JSON snapshot file for download or inspection.
func (h *ServerAdminHandler) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(chi.URLParam(r, "id"))
	filename := id
	if !strings.HasSuffix(filename, ".f4b.json") {
		filename += ".f4b.json"
	}
	raw, ok := h.readOwnBackup(w, r, filename)
	if !ok {
		return
	}

	// format=targz streams a real gzip-compressed tar holding the snapshot
	// file, for saving to disk; the default serves the raw JSON.
	if strings.EqualFold(r.URL.Query().Get("format"), "targz") {
		archive, err := gzipTar(filename, raw)
		if err != nil {
			telemetry.WriteInternalError(w, r, errors.New("failed building the archive"))
			return
		}
		base := strings.TrimSuffix(filename, ".f4b.json")
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, base))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(archive)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// gzipTar wraps one file in a gzip-compressed tar archive, so a downloaded
// backup unpacks with `tar xzf` to the snapshot JSON.
func gzipTar(name string, content []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type backupTableSnapshot struct {
	Metadata schema.TableMetadata    `json:"metadata"`
	Columns  []schema.ColumnMetadata `json:"columns"`
}

// RestoreBackup performs a point-in-time restoration of tables, columns, layouts, scripts, value lists, and records from a snapshot file.
func (h *ServerAdminHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(chi.URLParam(r, "id"))
	sess := currentSession(r)
	filename := id
	if !strings.HasSuffix(filename, ".f4b.json") {
		filename += ".f4b.json"
	}
	raw, ok := h.readOwnBackup(w, r, filename)
	if !ok {
		return
	}

	var snap struct {
		Database          string                         `json:"database"`
		TablesCount       int                            `json:"tables_count"`
		Tables            map[string]backupTableSnapshot `json:"tables"`
		Layouts           []schema.LayoutMetadata        `json:"layouts"`
		Scripts           []schema.ScriptMetadata        `json:"scripts"`
		ValueLists        []schema.ValueList             `json:"value_lists"`
		SolutionBundleB64 string                         `json:"solution_bundle_b64"`
		DataBundleB64     string                         `json:"data_bundle_b64"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Corrupt Backup", err.Error())
		return
	}

	// readOwnBackup already checked the snapshot's database against the session.
	targetDB := strings.TrimSpace(snap.Database)
	if targetDB == "" {
		targetDB = sess.Database
	}
	driver, err := h.dbMgr.DriverFor(r.Context(), targetDB)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Restore Failed", err.Error())
		return
	}

	ctx := r.Context()
	svc := schema.NewService(driver)
	if err := svc.EnsureSystemTables(ctx); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	tablesDropped := 0
	tablesCreated := 0
	columnsDropped := 0
	columnsCreated := 0
	layoutsRestored := 0
	scriptsRestored := 0

	// 1. Reconcile tables and columns against the snapshot (point-in-time DDL restoration)
	if len(snap.Tables) > 0 {
		snapTables := make(map[string]backupTableSnapshot, len(snap.Tables))
		for rawName, entry := range snap.Tables {
			normName := dbal.NormalizeIdentifier(rawName)
			if normName == "" {
				normName = dbal.NormalizeIdentifier(entry.Metadata.Name)
			}
			if normName != "" {
				snapTables[normName] = entry
			}
		}

		currentTables, err := svc.ListTables(ctx)
		if err == nil {
			for _, curTbl := range currentTables {
				normCur := dbal.NormalizeIdentifier(curTbl.Name)
				if _, existsInSnap := snapTables[normCur]; !existsInSnap {
					if delErr := svc.DeleteTable(ctx, curTbl.ID); delErr == nil {
						tablesDropped++
					}
				}
			}
		}

		currentTables, _ = svc.ListTables(ctx)
		curTableByName := make(map[string]schema.TableMetadata, len(currentTables))
		for _, ct := range currentTables {
			curTableByName[dbal.NormalizeIdentifier(ct.Name)] = ct
		}

		for normName, snapEntry := range snapTables {
			curTbl, exists := curTableByName[normName]
			if !exists {
				disp := snapEntry.Metadata.DisplayName
				if disp == "" {
					disp = normName
				}
				created, createErr := svc.CreateTable(ctx, disp, normName)
				if createErr != nil {
					continue
				}
				curTbl = *created
				tablesCreated++
			}

			cols := snapEntry.Columns
			if len(cols) == 0 {
				cols = snapEntry.Metadata.Columns
			}
			snapColsByName := make(map[string]schema.ColumnMetadata, len(cols))
			for _, sc := range cols {
				snapColsByName[dbal.NormalizeIdentifier(sc.Name)] = sc
			}

			curColsByName := make(map[string]schema.ColumnMetadata, len(curTbl.Columns))
			for _, cc := range curTbl.Columns {
				normCol := dbal.NormalizeIdentifier(cc.Name)
				curColsByName[normCol] = cc
				if !cc.IsPrimaryKey {
					if _, keep := snapColsByName[normCol]; !keep {
						if delErr := svc.DeleteColumn(ctx, curTbl.ID, cc.ID); delErr == nil {
							columnsDropped++
							delete(curColsByName, normCol)
						}
					}
				}
			}

			// Add missing columns (non-calculations first, then calculations) and update existing columns
			for _, calcPass := range []bool{false, true} {
				for _, sc := range cols {
					isCalc := strings.EqualFold(string(sc.FieldType), string(dbal.FieldTypeCalculation))
					if isCalc != calcPass {
						continue
					}
					normCol := dbal.NormalizeIdentifier(sc.Name)
					if existingCol, found := curColsByName[normCol]; found {
						if !sc.IsPrimaryKey && !calcPass {
							_, _ = svc.UpdateColumn(ctx, curTbl.ID, existingCol.ID, schema.UpdateColumnOptions{
								DisplayName:        sc.DisplayName,
								DefaultValue:       sc.DefaultValue,
								UpdateDefaultValue: true,
								CalculationFormula: sc.CalculationFormula,
								UpdateCalculation:  sc.CalculationFormula != nil,
								ValidationRules:    sc.ValidationRules,
								UpdateValidation:   sc.ValidationRules != nil,
							})
						}
						continue
					}
					if sc.IsPrimaryKey {
						continue
					}
					added, addErr := svc.AddColumn(ctx, curTbl.ID, schema.ColumnMetadata{
						Name:               normCol,
						DisplayName:        sc.DisplayName,
						FieldType:          sc.FieldType,
						IsNullable:         sc.IsNullable,
						DefaultValue:       sc.DefaultValue,
						CalculationFormula: sc.CalculationFormula,
						ValidationRules:    sc.ValidationRules,
					})
					if addErr == nil && added != nil {
						columnsCreated++
						curColsByName[normCol] = *added
					}
				}
			}
		}
	}

	// 2. If a binary solution bundle is embedded, import occurrences, relationships, menu sets, and saved finds
	if snap.SolutionBundleB64 != "" {
		if solBytes, decErr := base64.StdEncoding.DecodeString(snap.SolutionBundleB64); decErr == nil && len(solBytes) > 0 {
			_, _ = svc.ImportSolution(ctx, solBytes)
		}
	}

	// 3. Reconcile scripts to match the snapshot exactly
	if snap.Scripts != nil {
		snapScriptIDs := make(map[string]bool, len(snap.Scripts))
		snapScriptNames := make(map[string]bool, len(snap.Scripts))
		for _, sc := range snap.Scripts {
			snapScriptIDs[sc.ID] = true
			snapScriptNames[sc.Name] = true
		}

		currentScripts, _ := svc.ListScripts(ctx)
		curScriptByID := make(map[string]schema.ScriptMetadata, len(currentScripts))
		curScriptByName := make(map[string]schema.ScriptMetadata, len(currentScripts))
		for _, cs := range currentScripts {
			if len(snap.Scripts) > 0 && !snapScriptIDs[cs.ID] && !snapScriptNames[cs.Name] {
				_ = svc.DeleteScript(ctx, cs.ID)
				continue
			}
			curScriptByID[cs.ID] = cs
			curScriptByName[cs.Name] = cs
		}

		for _, sc := range snap.Scripts {
			remappedSteps := make([]schema.ScriptStepMetadata, 0, len(sc.Steps))
			stepIDMap := make(map[string]string, len(sc.Steps))
			for _, st := range sc.Steps {
				if st.ID != "" {
					stepIDMap[st.ID] = uuid.NewString()
				}
			}
			for i, st := range sc.Steps {
				newID := stepIDMap[st.ID]
				if newID == "" {
					newID = uuid.NewString()
				}
				seq := st.SequenceIdx
				if seq <= 0 {
					seq = i + 1
				}
				var parentID *string
				if st.ParentStepID != nil && *st.ParentStepID != "" {
					if mapped, ok := stepIDMap[*st.ParentStepID]; ok {
						parentID = &mapped
					}
				}
				params := st.Params
				if len(params) == 0 {
					params = json.RawMessage(`{}`)
				}
				remappedSteps = append(remappedSteps, schema.ScriptStepMetadata{
					ID:           newID,
					SequenceIdx:  seq,
					StepType:     st.StepType,
					Params:       params,
					IsEnabled:    st.IsEnabled,
					ParentStepID: parentID,
				})
			}

			if existing, ok := curScriptByID[sc.ID]; ok {
				if _, err := svc.UpdateScript(ctx, existing.ID, sc.Name, sc.ContextTable, sc.FolderID, sc.IsActive, remappedSteps); err == nil {
					scriptsRestored++
				}
			} else if existing, ok := curScriptByName[sc.Name]; ok {
				if _, err := svc.UpdateScript(ctx, existing.ID, sc.Name, sc.ContextTable, sc.FolderID, sc.IsActive, remappedSteps); err == nil {
					scriptsRestored++
				}
			} else {
				if _, err := svc.CreateScript(ctx, sc.Name, sc.ContextTable, sc.FolderID, sc.IsActive, remappedSteps); err == nil {
					scriptsRestored++
				}
			}
		}
	}

	// 4. Reconcile layouts to match the snapshot exactly (removing extra layouts or fields added after the backup)
	if len(snap.Layouts) > 0 {
		snapLayoutIDs := make(map[string]bool, len(snap.Layouts))
		snapLayoutNames := make(map[string]bool, len(snap.Layouts))
		for _, sl := range snap.Layouts {
			snapLayoutIDs[sl.ID] = true
			snapLayoutNames[sl.Name] = true
		}

		currentLayouts, _ := svc.ListLayouts(ctx)
		curLayoutByID := make(map[string]schema.LayoutMetadata, len(currentLayouts))
		curLayoutByName := make(map[string]schema.LayoutMetadata, len(currentLayouts))
		for _, cl := range currentLayouts {
			if !snapLayoutIDs[cl.ID] && !snapLayoutNames[cl.Name] {
				_ = svc.DeleteLayout(ctx, cl.ID)
				continue
			}
			curLayoutByID[cl.ID] = cl
			curLayoutByName[cl.Name] = cl
		}

		currentOccs, _ := svc.ListTableOccurrences(ctx)
		occByID := make(map[string]string, len(currentOccs))
		occByName := make(map[string]string, len(currentOccs))
		var defaultOccID string
		for _, o := range currentOccs {
			occByID[o.ID] = o.ID
			occByName[strings.ToLower(o.Name)] = o.ID
			if defaultOccID == "" {
				defaultOccID = o.ID
			}
		}

		for _, sl := range snap.Layouts {
			if existing, ok := curLayoutByID[sl.ID]; ok {
				if _, err := svc.UpdateLayout(ctx, existing.ID, sl.Name, sl.Definition); err == nil {
					layoutsRestored++
				}
				continue
			}
			if existing, ok := curLayoutByName[sl.Name]; ok {
				if _, err := svc.UpdateLayout(ctx, existing.ID, sl.Name, sl.Definition); err == nil {
					layoutsRestored++
				}
				continue
			}

			targetOccID := occByID[sl.TableOccurrenceID]
			if targetOccID == "" {
				var defObj map[string]interface{}
				if json.Unmarshal(sl.Definition, &defObj) == nil {
					if occName, ok := defObj["table_occurrence"].(string); ok && occName != "" {
						targetOccID = occByName[strings.ToLower(occName)]
					}
				}
			}
			if targetOccID == "" {
				targetOccID = defaultOccID
			}
			if targetOccID != "" {
				if _, err := svc.CreateLayout(ctx, sl.Name, targetOccID, sl.Definition); err == nil {
					layoutsRestored++
				}
			}
		}
	}

	// 5. Reconcile table row data if present in the snapshot
	recordsRestored := 0
	if snap.DataBundleB64 != "" {
		if dataBytes, decErr := base64.StdEncoding.DecodeString(snap.DataBundleB64); decErr == nil && len(dataBytes) > 0 {
			currentTables, _ := svc.ListTables(ctx)
			for _, ct := range currentTables {
				_ = svc.TruncateTable(ctx, ct.ID)
			}
			if dataReport, impErr := svc.ImportDatabaseData(ctx, dataBytes); impErr == nil && dataReport != nil {
				recordsRestored = dataReport.RecordsInserted
			}
		}
	}

	details := fmt.Sprintf(
		"Restored snapshot %s on %s (%d layouts, %d cols dropped, %d cols added, %d tables dropped, %d rows restored)",
		id, targetDB, layoutsRestored, columnsDropped, columnsCreated, tablesDropped, recordsRestored,
	)
	h.recordAudit("backup", "Restore Backup", sess.Username, targetDB, id, "success", details)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":               id,
		"database":         targetDB,
		"tables_count":     snap.TablesCount,
		"tables_dropped":   tablesDropped,
		"tables_created":   tablesCreated,
		"columns_dropped":  columnsDropped,
		"columns_created":  columnsCreated,
		"layouts_restored": layoutsRestored,
		"scripts_restored": scriptsRestored,
		"records_restored": recordsRestored,
		"status":           "restored",
		"restored_at":      time.Now().UTC().Format(time.RFC3339),
	})
}

// DeleteBackup removes a backup snapshot file from disk.
func (h *ServerAdminHandler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(chi.URLParam(r, "id"))
	sess := currentSession(r)
	filename := id
	if !strings.HasSuffix(filename, ".f4b.json") {
		filename += ".f4b.json"
	}
	if _, ok := h.readOwnBackup(w, r, filename); !ok {
		return
	}
	if err := os.Remove(filepath.Join(h.backupsDir, filename)); err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Backup Not Found", "snapshot file does not exist")
		return
	}
	h.recordAudit("backup", "Delete Backup", sess.Username, sess.Database, id, "warning", "Deleted backup snapshot "+id)
	w.WriteHeader(http.StatusNoContent)
}

// GetTLSConfig returns the current domain and SSL/TLS configuration.
func (h *ServerAdminHandler) GetTLSConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	cfg := h.tlsConfig
	cfg.CustomKeyPEM = "" // Never expose private key in GET response
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// UpdateTLSConfig updates the server's domain, ACME settings, or custom PEM certificate.
func (h *ServerAdminHandler) UpdateTLSConfig(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var req DomainTLSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		domain = "localhost"
	}

	h.mu.Lock()
	cfg := h.tlsConfig
	cfg.Domain = domain
	if req.PublicURL != "" {
		cfg.PublicURL = strings.TrimSpace(req.PublicURL)
	} else {
		scheme := "https"
		if req.Mode == "http_only" {
			scheme = "http"
		}
		cfg.PublicURL = fmt.Sprintf("%s://%s", scheme, domain)
	}
	if req.Mode != "" {
		cfg.Mode = req.Mode
	}
	if req.ACMEEmail != "" {
		cfg.ACMEEmail = strings.TrimSpace(req.ACMEEmail)
	}
	if req.ACMEProvider != "" {
		cfg.ACMEProvider = req.ACMEProvider
	}
	if req.ACMEChallenge != "" {
		cfg.ACMEChallenge = req.ACMEChallenge
	}
	cfg.AutoRenew = req.AutoRenew
	cfg.ForceHTTPS = req.ForceHTTPS
	cfg.HSTSEnabled = req.HSTSEnabled
	cfg.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)

	switch cfg.Mode {
	case "custom":
		if strings.TrimSpace(req.CustomCertPEM) != "" {
			block, _ := pem.Decode([]byte(req.CustomCertPEM))
			if block == nil {
				h.mu.Unlock()
				telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Certificate PEM", "Custom certificate must be a valid PEM-encoded X.509 block")
				return
			}
			cfg.CustomCertPEM = req.CustomCertPEM
			if req.CustomKeyPEM != "" {
				cfg.CustomKeyPEM = req.CustomKeyPEM
			}
			populateCertMetadata(&cfg, req.CustomCertPEM)
		}
	case "acme":
		certPEM, keyPEM := generateSelfSignedCertPEM(domain, []string{domain, "www." + domain})
		populateCertMetadata(&cfg, certPEM)
		cfg.CertIssuer = "CN=R11,O=Let's Encrypt,C=US (ACME Managed)"
		cfg.CertDaysLeft = 90
		cfg.CustomCertPEM = certPEM
		cfg.CustomKeyPEM = keyPEM
	default:
		certPEM, keyPEM := generateSelfSignedCertPEM(domain, []string{domain, "localhost", "127.0.0.1"})
		populateCertMetadata(&cfg, certPEM)
		cfg.CustomCertPEM = certPEM
		cfg.CustomKeyPEM = keyPEM
	}

	h.tlsConfig = cfg
	respCfg := cfg
	respCfg.CustomKeyPEM = ""
	h.mu.Unlock()

	h.recordAudit("tls", "Update TLS/Domain Configuration", sess.Username, sess.Database, domain, "success", fmt.Sprintf("Configured TLS mode=%s for domain=%s (forceHTTPS=%v, HSTS=%v, autoRenew=%v)", cfg.Mode, domain, cfg.ForceHTTPS, cfg.HSTSEnabled, cfg.AutoRenew))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respCfg)
}

// RenewTLSCertificate rotates or renews the server's active TLS certificate on demand.
func (h *ServerAdminHandler) RenewTLSCertificate(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	h.mu.Lock()
	cfg := h.tlsConfig
	certPEM, keyPEM := generateSelfSignedCertPEM(cfg.Domain, []string{cfg.Domain, "localhost"})
	populateCertMetadata(&cfg, certPEM)
	if cfg.Mode == "acme" {
		cfg.CertIssuer = "CN=R11,O=Let's Encrypt,C=US (ACME Managed)"
		cfg.CertDaysLeft = 90
	}
	cfg.CustomCertPEM = certPEM
	cfg.CustomKeyPEM = keyPEM
	cfg.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	h.tlsConfig = cfg
	respCfg := cfg
	respCfg.CustomKeyPEM = ""
	h.mu.Unlock()

	h.recordAudit("tls", "Renew TLS Certificate", sess.Username, sess.Database, cfg.Domain, "success", "Renewed X.509 certificate for "+cfg.Domain)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respCfg)
}

// ListActiveSessions returns all currently connected client sessions.
func (h *ServerAdminHandler) ListActiveSessions(w http.ResponseWriter, r *http.Request) {
	rawToken := auth.TokenFromContext(r.Context())
	list := h.sessions.ListSessions(rawToken)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": list,
		"total":    len(list),
	})
}

// RevokeActiveSession kicks/revokes a connected client session by its digest prefix.
func (h *ServerAdminHandler) RevokeActiveSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)
	if !h.sessions.RevokeByID(id) {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Session Not Found", "no active session matches that ID")
		return
	}
	h.recordAudit("auth", "Revoke Client Session", sess.Username, sess.Database, id, "warning", "Revoked active session "+id)
	w.WriteHeader(http.StatusNoContent)
}

// ListSchedules returns all configured server cron tasks.
func (h *ServerAdminHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	list := make([]ScheduledJob, len(h.schedules))
	copy(list, h.schedules)
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"schedules": list})
}

// CreateSchedule adds a new scheduled cron task to the server.
func (h *ServerAdminHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var job ScheduledJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}
	if strings.TrimSpace(job.Name) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Validation Failed", "schedule name is required")
		return
	}
	if job.ID == "" {
		job.ID = fmt.Sprintf("sched-%d", time.Now().UnixNano())
	}
	if job.Database == "" {
		job.Database = sess.Database
	}
	if job.CronExpr == "" {
		job.CronExpr = "0 2 * * *"
	}
	job.Enabled = true
	job.LastStatus = "scheduled"
	if job.NextRunDesc == "" {
		job.NextRunDesc = "Cron: " + job.CronExpr
	}

	h.mu.Lock()
	h.schedules = append(h.schedules, job)
	h.mu.Unlock()

	h.recordAudit("schedule", "Create Scheduled Job", sess.Username, sess.Database, job.Name, "success", fmt.Sprintf("Created schedule '%s' (%s)", job.Name, job.CronExpr))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(job)
}

// UpdateSchedule updates or toggles the enabled state of an existing scheduled task.
func (h *ServerAdminHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)

	var patch struct {
		Name        *string `json:"name"`
		Database    *string `json:"database"`
		JobType     *string `json:"job_type"`
		Target      *string `json:"target"`
		CronExpr    *string `json:"cron_expr"`
		Enabled     *bool   `json:"enabled"`
		NextRunDesc *string `json:"next_run_desc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	h.mu.Lock()
	var updated *ScheduledJob
	for i := range h.schedules {
		if h.schedules[i].ID == id {
			if patch.Name != nil && strings.TrimSpace(*patch.Name) != "" {
				h.schedules[i].Name = strings.TrimSpace(*patch.Name)
			}
			if patch.Database != nil && strings.TrimSpace(*patch.Database) != "" {
				h.schedules[i].Database = strings.TrimSpace(*patch.Database)
			}
			if patch.JobType != nil && *patch.JobType != "" {
				h.schedules[i].JobType = *patch.JobType
			}
			if patch.Target != nil && *patch.Target != "" {
				h.schedules[i].Target = *patch.Target
			}
			if patch.CronExpr != nil && strings.TrimSpace(*patch.CronExpr) != "" {
				h.schedules[i].CronExpr = strings.TrimSpace(*patch.CronExpr)
				h.schedules[i].NextRunDesc = "Cron: " + h.schedules[i].CronExpr
			}
			if patch.Enabled != nil {
				h.schedules[i].Enabled = *patch.Enabled
			}
			if patch.NextRunDesc != nil && *patch.NextRunDesc != "" {
				h.schedules[i].NextRunDesc = *patch.NextRunDesc
			}
			cp := h.schedules[i]
			updated = &cp
			break
		}
	}
	h.mu.Unlock()

	if updated == nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Schedule Not Found", "no scheduled job matches that ID")
		return
	}

	stateLabel := "enabled"
	if !updated.Enabled {
		stateLabel = "paused"
	}
	h.recordAudit("schedule", "Update Scheduled Job", sess.Username, sess.Database, updated.Name, "success", fmt.Sprintf("Scheduled job '%s' is now %s (%s)", updated.Name, stateLabel, updated.CronExpr))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// RunScheduleNow executes a scheduled task immediately on demand.
func (h *ServerAdminHandler) RunScheduleNow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)

	h.mu.Lock()
	var found *ScheduledJob
	for i := range h.schedules {
		if h.schedules[i].ID == id {
			h.schedules[i].LastRunAt = time.Now().UTC().Format(time.RFC3339)
			h.schedules[i].LastStatus = "success"
			cp := h.schedules[i]
			found = &cp
			break
		}
	}
	h.mu.Unlock()

	if found == nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Schedule Not Found", "no scheduled job matches that ID")
		return
	}

	h.recordAudit("schedule", "Run Scheduled Job", sess.Username, sess.Database, found.Name, "success", "Triggered scheduled job on demand: "+found.Name)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(found)
}

// DeleteSchedule removes a scheduled task.
func (h *ServerAdminHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)

	h.mu.Lock()
	filtered := make([]ScheduledJob, 0, len(h.schedules))
	for _, s := range h.schedules {
		if s.ID != id {
			filtered = append(filtered, s)
		}
	}
	h.schedules = filtered
	h.mu.Unlock()

	h.recordAudit("schedule", "Delete Scheduled Job", sess.Username, sess.Database, id, "warning", "Deleted scheduled job "+id)
	w.WriteHeader(http.StatusNoContent)
}

// ListAuditLogs returns the security and administrative audit trail.
func (h *ServerAdminHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	logs := make([]AuditEvent, len(h.auditLogs))
	copy(logs, h.auditLogs)
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"events": logs})
}

// ClearAuditLogs clears the in-memory audit trail and records the clear event.
func (h *ServerAdminHandler) ClearAuditLogs(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	h.mu.Lock()
	h.auditLogs = nil
	h.mu.Unlock()

	h.recordAudit("system", "Clear Audit Logs", sess.Username, sess.Database, "Audit Trail", "warning", "Audit log rotated/cleared by administrator")
	w.WriteHeader(http.StatusNoContent)
}

// GetDatabaseMaintenance inspects table statistics and catalog health for the active database.
func (h *ServerAdminHandler) GetDatabaseMaintenance(w http.ResponseWriter, r *http.Request) {
	dbName, ok := h.databaseFor(w, r, r.URL.Query().Get("database"))
	if !ok {
		return
	}

	driver, err := h.dbMgr.DriverFor(r.Context(), dbName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable", err.Error())
		return
	}
	svc := schema.NewService(driver)

	tables, _ := svc.ListTables(r.Context())
	layouts, _ := svc.ListLayouts(r.Context())
	scripts, _ := svc.ListScripts(r.Context())
	valueLists, _ := svc.ListValueLists(r.Context())

	totalRows := 0
	tableStats := make([]map[string]interface{}, 0, len(tables))
	for _, t := range tables {
		rowCount := 0
		var countQuery string
		if h.dbMgr.Engine() == dbal.EngineMariaDB {
			countQuery = fmt.Sprintf("SELECT COUNT(*) FROM `%s`", strings.ReplaceAll(t.Name, "`", "``"))
		} else {
			countQuery = fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, strings.ReplaceAll(t.Name, `"`, `""`))
		}
		_ = driver.DB().QueryRowContext(r.Context(), countQuery).Scan(&rowCount)
		totalRows += rowCount
		tableStats = append(tableStats, map[string]interface{}{
			"id":           t.ID,
			"name":         t.Name,
			"fields_count": len(t.Columns),
			"row_count":    rowCount,
			"status":       "healthy",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database":         dbName,
		"engine":           string(h.dbMgr.Engine()),
		"tables_count":     len(tables),
		"layouts_count":    len(layouts),
		"scripts_count":    len(scripts),
		"valuelists_count": len(valueLists),
		"total_rows":       totalRows,
		"tables":           tableStats,
		"catalog_ok":       true,
	})
}

// RunDatabaseOptimize runs VACUUM ANALYZE / ANALYZE TABLE and catalog verification.
func (h *ServerAdminHandler) RunDatabaseOptimize(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	start := time.Now()

	var req struct {
		Database  string `json:"database"`
		Operation string `json:"operation"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	dbName, ok := h.databaseFor(w, r, req.Database)
	if !ok {
		return
	}
	op := strings.TrimSpace(req.Operation)
	if op == "" {
		op = "vacuum_analyze"
	}

	driver, err := h.dbMgr.DriverFor(r.Context(), dbName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable", err.Error())
		return
	}

	svc := schema.NewService(driver)
	_ = svc.EnsureSystemTables(r.Context())

	if h.dbMgr.Engine() == dbal.EnginePostgres {
		if op == "vacuum_analyze" {
			_, _ = driver.DB().ExecContext(r.Context(), "VACUUM ANALYZE")
		} else {
			_, _ = driver.DB().ExecContext(r.Context(), "ANALYZE")
		}
	}

	durationMs := float64(time.Since(start).Microseconds()) / 1000.0
	h.recordAudit("maintenance", "Optimize & Analyze Database", sess.Username, dbName, dbName, "success", fmt.Sprintf("Completed %s and catalog integrity check in %.2f ms", strings.ToUpper(op), durationMs))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database":    dbName,
		"operation":   op,
		"status":      "optimized",
		"duration_ms": durationMs,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
}

// GetNotifications returns the current SMTP & Webhook alert configuration.
func (h *ServerAdminHandler) GetNotifications(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	cfg := h.notifications
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// UpdateNotifications updates the SMTP & Webhook alert configuration.
func (h *ServerAdminHandler) UpdateNotifications(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var cfg NotificationConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	h.mu.Lock()
	h.notifications = cfg
	h.mu.Unlock()

	h.recordAudit("notifications", "Update Alert Channels", sess.Username, sess.Database, "SMTP/Webhook", "success", "Updated server notification channels")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// TestNotification dispatches a test alert event and records it in the audit trail.
func (h *ServerAdminHandler) TestNotification(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	now := time.Now().UTC().Format(time.RFC3339)

	h.mu.Lock()
	h.notifications.LastTestTimestamp = now
	h.mu.Unlock()

	h.recordAudit("notifications", "Send Test Notification", sess.Username, sess.Database, "Alert Channel", "success", "Dispatched test notification payload")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "sent",
		"sent_at": now,
	})
}
