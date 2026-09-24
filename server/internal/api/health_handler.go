package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/go-chi/chi/v5"
)

// HealthHandler manages Cloud-Native lifecycle probes (Liveness, Readiness, Startup)
// and deep IETF-compliant diagnostic health checks.
type HealthHandler struct {
	dbMgr     *dbal.MultiDatabaseManager
	version   string
	isReady   *atomic.Bool
	isStarted *atomic.Bool
	startTime time.Time
}

// NewHealthHandler creates a configured HealthHandler instance.
func NewHealthHandler(dbMgr *dbal.MultiDatabaseManager, version string, isReady, isStarted *atomic.Bool) *HealthHandler {
	return &HealthHandler{
		dbMgr:     dbMgr,
		version:   version,
		isReady:   isReady,
		isStarted: isStarted,
		startTime: time.Now(),
	}
}

// RegisterRoutes registers the cloud-native health probes on the given router.
func (h *HealthHandler) RegisterRoutes(r chi.Router) {
	// Root diagnostic health check
	r.Get("/healthz", h.Diagnostic)

	// Liveness probes (process responsiveness, no external deps)
	r.Get("/healthz/liveness", h.Liveness)
	r.Get("/livez", h.Liveness)

	// Readiness probes (dependency ping, active drain status)
	r.Get("/healthz/readiness", h.Readiness)
	r.Get("/readyz", h.Readiness)

	// Startup probes (catalog initialization completion)
	r.Get("/healthz/startup", h.Startup)
	r.Get("/startupz", h.Startup)
}

// Liveness returns 200 OK immediately if the HTTP process is responsive.
// In accordance with cloud-native best practices, it NEVER checks external dependencies.
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// Readiness verifies that the instance is ready to receive and process traffic.
// Checks the DB connection pool and verifies that the instance is not in graceful shutdown.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !h.isReady.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"reason": "server is undergoing graceful shutdown",
		})
		return
	}

	if !h.isStarted.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_ready",
			"reason": "system catalog tables are still initializing",
		})
		return
	}

	startPing := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.dbMgr.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "not_ready",
			"database": "disconnected",
			"reason":   err.Error(),
		})
		return
	}

	latencyMs := float64(time.Since(startPing).Microseconds()) / 1000.0

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ready",
		"database":        "connected",
		"active_database": h.dbMgr.ActiveDatabase(),
		"latency_ms":      latencyMs,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	})
}

// Startup checks if startup tasks (system catalog initialization) are complete.
func (h *HealthHandler) Startup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.isStarted.Load() {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "started",
		})
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "starting",
	})
}

// Diagnostic provides deep IETF-compliant health check diagnostics.
func (h *HealthHandler) Diagnostic(w http.ResponseWriter, r *http.Request) {
	startPing := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "connected"
	overallStatus := "pass"
	dbErr := ""
	if err := h.dbMgr.Ping(ctx); err != nil {
		dbStatus = "disconnected"
		overallStatus = "fail"
		dbErr = err.Error()
	}
	latencyMs := float64(time.Since(startPing).Microseconds()) / 1000.0

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	w.Header().Set("Content-Type", "application/json")
	if overallStatus != "pass" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	uptimeSec := int(time.Since(h.startTime).Seconds())

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          overallStatus,
		"app":             "File4Base API",
		"version":         h.version,
		"engine":          "postgres",
		"database":        dbStatus,
		"active_database": h.dbMgr.ActiveDatabase(),
		"release_id":      fmt.Sprintf("file4base-api-v%s", h.version),
		"service_id":      "file4base-api",
		"description":     "File4Base API Engine Health",
		"uptime_seconds":  uptimeSec,
		"checks": map[string]interface{}{
			"database": map[string]interface{}{
				"status":          dbStatus,
				"active_database": h.dbMgr.ActiveDatabase(),
				"latency_ms":      latencyMs,
				"error":           dbErr,
			},
			"memory": map[string]interface{}{
				"alloc_mb":       fmt.Sprintf("%.1f MB", float64(m.Alloc)/(1024*1024)),
				"sys_mb":         fmt.Sprintf("%.1f MB", float64(m.Sys)/(1024*1024)),
				"num_goroutines": runtime.NumGoroutine(),
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
