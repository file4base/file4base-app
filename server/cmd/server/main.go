package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/clientip"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/localca"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const AppVersion = "0.22.0"

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
}

// corsMiddleware answers CORS preflights and tags responses for the allowed
// origins. Sessions travel in the Authorization header (never in cookies), so
// a browser cannot attach a user's credentials to a cross-site request on its
// own; the allow-list still limits which web origins may call the API at all.
// An empty list (the default) sends no CORS header, so only same-origin pages
// (the bundled Web Client, which reaches the API through its own Nginx) can
// read responses; allowed == ["*"] accepts any origin.
func corsMiddleware(allowed []string) func(http.Handler) http.Handler {
	allowAny := false
	origins := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "*" {
			allowAny = true
		}
		if o != "" {
			origins[strings.ToLower(o)] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			_, listed := origins[strings.ToLower(strings.TrimRight(origin, "/"))]

			switch {
			case allowAny:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case origin != "" && listed:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			if allowAny || listed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With, X-Database-Name, X-File4Base-Encrypted, X-File4Base-Entry, X-File4Base-Validation, Accept-Encryption, traceparent, tracestate, X-Trace-ID")
				w.Header().Set("Access-Control-Expose-Headers", "Link, Content-Length, Content-Disposition, X-File4Base-Encrypted, X-Original-Content-Type, traceparent, X-Trace-ID, Deprecation, Sunset")
				w.Header().Set("Access-Control-Max-Age", "300")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// publicDatabaseCreation reads ALLOW_PUBLIC_DATABASE_CREATION. It is off
// unless set: anyone who can reach the server would otherwise be able to
// create a database and become its owner (#202).
func publicDatabaseCreation() bool {
	return envBool("ALLOW_PUBLIC_DATABASE_CREATION", false)
}

// publicCreationNotice is the startup line that says who can create
// databases, and how a new server gets its first solution.
func publicCreationNotice(allow bool) string {
	if allow {
		return "Warning: ALLOW_PUBLIC_DATABASE_CREATION=true: anyone who can reach this server can create a database and become its owner. Use it only to create the first solution of a new server, then remove the setting and restart the API."
	}
	return "Database creation requires an owner session (ALLOW_PUBLIC_DATABASE_CREATION is off). A new server gets its first solution with ALLOW_PUBLIC_DATABASE_CREATION=true for one start; see README, \"First solution\"."
}

// corsNotices describes the CORS configuration at startup and flags entries
// a browser would never send as an Origin (a path, no scheme, ...).
func corsNotices(origins []string) []string {
	if len(origins) == 0 {
		return []string{"CORS: no cross-origin browser access (CORS_ALLOWED_ORIGINS is empty). The bundled Web Client calls the API on its own origin and needs none."}
	}
	var lines []string
	for _, o := range origins {
		if o == "*" {
			lines = append(lines, "Warning: CORS_ALLOWED_ORIGINS contains *: every web site may call this API from a browser. List the origins that need it instead (scheme://host:port).")
			continue
		}
		if !isWebOrigin(o) {
			lines = append(lines, fmt.Sprintf("Warning: CORS_ALLOWED_ORIGINS entry %q is not a web origin (scheme://host[:port], no path); no browser request will match it.", o))
		}
	}
	return append(lines, fmt.Sprintf("CORS: cross-origin browser access allowed for %s", strings.Join(origins, ", ")))
}

// isWebOrigin reports whether s has the form a browser sends in an Origin
// header: http or https, a host, an optional port, and nothing else.
func isWebOrigin(s string) bool {
	u, err := url.Parse(strings.TrimRight(s, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

// envBool reads a boolean environment variable, falling back to def when unset or invalid.
func envBool(name string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	val, err := strconv.ParseBool(raw)
	if err != nil {
		log.Printf("Warning: ignoring invalid boolean %s=%q", name, raw)
		return def
	}
	return val
}

// splitList splits a comma-separated environment value into trimmed,
// non-empty entries. An empty or blank value yields a nil slice.
func splitList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envDuration reads a duration environment variable (e.g. "12h", "30m").
func envDuration(name string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	val, err := time.ParseDuration(raw)
	if err != nil || val <= 0 {
		log.Printf("Warning: ignoring invalid duration %s=%q", name, raw)
		return def
	}
	return val
}

// envBytes reads a size environment variable: a byte count, optionally with
// a KiB, MiB or GiB suffix (e.g. "2GiB", "512MiB").
func envBytes(name string, def int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	val, err := api.ParseByteSize(raw)
	if err != nil {
		log.Printf("Warning: ignoring invalid size %s=%q: %v", name, raw, err)
		return def
	}
	return val
}

func main() {
	engineStr := os.Getenv("DB_ENGINE")
	if engineStr == "" {
		engineStr = "postgres"
	}

	engineType, err := dbal.ParseEngineType(engineStr)
	if err != nil {
		log.Fatalf("Invalid DB_ENGINE: %v", err)
	}

	// There is no built-in DSN: it would carry a known password (#202).
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		if engineType == dbal.EnginePostgres {
			log.Fatal("DATABASE_URL is not set. Set it to the engine's administrative database, e.g. postgres://file4base:<password>@localhost:5432/postgres?sslmode=disable (docker compose sets it from POSTGRES_PASSWORD in .env).")
		}
		log.Fatal("DATABASE_URL is not set. Set it to the engine's administrative database, e.g. file4base:<password>@tcp(localhost:3306)/mysql (docker compose sets it from MARIADB_PASSWORD in .env).")
	}

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = ":8080"
	}

	log.Printf("Starting File4Base Server v%s [Engine: %s, Port: %s]", AppVersion, engineType, port)

	// Initialize MultiDatabaseManager
	dbMgr, err := dbal.NewMultiDatabaseManager(engineType, dsn)
	if err != nil {
		log.Fatalf("Failed initializing multi-database manager: %v", err)
	}
	defer dbMgr.Close()

	// Initialize embedded SQLite Control Store for project-to-database mapping,
	// project options, and user table/layout permissions.
	controlDBPath := strings.TrimSpace(os.Getenv("CONTROL_DB_PATH"))
	if controlDBPath == "" {
		controlDBPath = "./data/file4base_control.db"
	}
	controlStore, err := control.NewStore(controlDBPath)
	if err != nil {
		log.Printf("Warning: failed to open SQLite control store at %s (%v); falling back to in-memory control store", controlDBPath, err)
		controlStore, err = control.NewStore(":memory:")
		if err != nil {
			log.Fatalf("Failed initializing fallback SQLite control store: %v", err)
		}
	} else {
		log.Printf("SQLite control store ready at %s", controlDBPath)
	}
	defer controlStore.Close()

	// Session store: every signed-in client holds a bearer token bound to
	// exactly one database. There is no server-wide "active database".
	sessions := auth.NewStore(envDuration("SESSION_TTL", auth.DefaultSessionTTL))

	// Secure defaults (#202): no cross-origin browser access unless origins
	// are listed, and no database creation without an owner session.
	corsOrigins := splitList(os.Getenv("CORS_ALLOWED_ORIGINS"))
	for _, line := range corsNotices(corsOrigins) {
		log.Println(line)
	}
	allowPublicCreate := publicDatabaseCreation()
	log.Println(publicCreationNotice(allowPublicCreate))
	if adminDB := strings.TrimSpace(os.Getenv("FILE4BASE_ADMIN_DATABASE")); adminDB != "" {
		log.Printf("Deprecated: FILE4BASE_ADMIN_DATABASE=%s makes the owners of one solution's database the server administrators. It keeps working for now; it will be replaced by server accounts (#185) and removed then.", adminDB)
	}

	// FileMaker migrations upload a native database copy of up to several
	// hundred megabytes and may run for minutes (#126).
	maxMigrationUpload := envBytes("MIGRATION_MAX_UPLOAD_BYTES", api.DefaultMaxMigrationUploadBytes)
	migrationTimeout := envDuration("MIGRATION_REQUEST_TIMEOUT", api.DefaultMigrationRequestTimeout)
	migrationUploadDir := strings.TrimSpace(os.Getenv("MIGRATION_UPLOAD_DIR"))

	// Reverse proxies whose X-Forwarded-For / X-Real-IP may be trusted when
	// recording a caller's address (audit log, session metadata, login
	// throttle). Empty means no proxy is trusted and the direct TCP peer is
	// used, so a client cannot forge the address attributed to it.
	trustedProxies, tpErr := clientip.ParseTrustedProxies(os.Getenv("FILE4BASE_TRUSTED_PROXIES"))
	if tpErr != nil {
		log.Printf("Warning: FILE4BASE_TRUSTED_PROXIES: %v", tpErr)
	}
	ipResolver := clientip.NewResolver(trustedProxies)
	if len(trustedProxies) > 0 {
		log.Printf("Trusting X-Forwarded-For / X-Real-IP from %d configured proxy network(s)", len(trustedProxies))
	}

	// Cloud-Native Lifecycle State Flags
	var isReady atomic.Bool
	var isStarted atomic.Bool
	isReady.Store(true)

	// Wait for the database engine to accept connections. The administrative
	// database is only used for server-level operations (listing, creating and
	// dropping databases); the sys_* catalog lives in each solution database.
	go func() {
		for i := 0; i < 60; i++ {
			ctxInit, cancelInit := context.WithTimeout(context.Background(), 2*time.Second)
			err := dbMgr.Ping(ctxInit)
			cancelInit()
			if err == nil {
				log.Printf("Database engine reachable through administrative database: %s", dbMgr.DefaultDatabase())
				isStarted.Store(true)
				return
			}
			time.Sleep(1 * time.Second)
		}
		log.Println("Warning: database engine did not become reachable during startup")
	}()

	r := chi.NewRouter()
	r.Use(corsMiddleware(corsOrigins))
	r.Use(telemetry.Middleware("file4base-api", AppVersion, ipResolver.ClientIP))
	// Note: chi's middleware.RealIP is deliberately not used. It overwrites
	// RemoteAddr from client-supplied X-Forwarded-For / X-Real-IP headers
	// unconditionally; the client address is instead resolved per request
	// through the trusted-proxy aware resolver (ipResolver / FILE4BASE_TRUSTED_PROXIES).
	r.Use(middleware.Recoverer)
	// Every request is bounded to 60 seconds, except the FileMaker migration
	// routes, which upload and import a whole database (#126).
	r.Use(api.RequestTimeout(60*time.Second, api.MigrationTimeouts(migrationTimeout)))

	// Cloud-Native Probes & Diagnostic Health (/healthz, /healthz/liveness, /healthz/readiness, etc.)
	healthHandler := api.NewHealthHandler(dbMgr, AppVersion, &isReady, &isStarted)
	healthHandler.RegisterRoutes(r)

	// Swagger UI & OpenAPI Specification (/swagger/, /swagger/openapi.json, /openapi.json, /docs)
	if swaggerHandler, err := api.NewSwaggerHandler(); err == nil {
		swaggerHandler.RegisterRoutes(r)
		log.Println("Swagger UI mounted at /swagger/ and OpenAPI spec at /swagger/openapi.json")
	} else {
		log.Printf("Warning: failed to initialize SwaggerHandler: %v", err)
	}

	// The payload-encryption channel falls back to a built-in default key when
	// no secret is configured. That default is public (it is in the source), so
	// it offers no confidentiality on its own — transport security then rests on
	// TLS. Warn so the fallback is never silent on a real deployment.
	channelSecret := strings.TrimSpace(os.Getenv("FILE4BASE_CHANNEL_SECRET"))
	if channelSecret == "" {
		log.Println("Warning: FILE4BASE_CHANNEL_SECRET is not set; the payload-encryption channel uses a built-in default key that provides no confidentiality on its own. Set FILE4BASE_CHANNEL_SECRET and serve over TLS on any non-local deployment.")
	}

	// API domain routes: public sign-in/database selector, and the protected
	// routes that run against the database of the caller's session.
	// The local certificate authority and the leaf it signs are persisted
	// here, shared by the HTTPS listener below, by Server Tools > TLS and by
	// the Web Client's Nginx (which mounts the same volume read-only).
	tlsStore := localca.New(localca.DefaultDir())

	api.Mount(r, dbMgr, sessions, api.Options{
		AllowPublicDatabaseCreation: allowPublicCreate,
		ControlStore:                controlStore,
		ChannelSecret:               channelSecret,
		Version:                     AppVersion,
		AdminDatabase:               os.Getenv("FILE4BASE_ADMIN_DATABASE"),
		SuperOwnerSeed:              splitList(os.Getenv("FILE4BASE_SUPER_OWNER")),
		TrustedProxies:              trustedProxies,
		TLSDir:                      tlsStore.Dir(),
		MaxMigrationUploadBytes:     maxMigrationUpload,
		MigrationUploadDir:          migrationUploadDir,
	})

	srv := &http.Server{
		Addr:              port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	tlsEnabled := envBool("TLS_ENABLED", false)
	tlsCert := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
	tlsKey := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))

	go func() {
		if tlsEnabled || (tlsCert != "" && tlsKey != "") {
			if tlsCert != "" && tlsKey != "" {
				log.Printf("Starting HTTPS listener with certificate %s", tlsCert)
				if err := srv.ListenAndServeTLS(tlsCert, tlsKey); err != nil && err != http.ErrServerClosed {
					log.Fatalf("Server ListenAndServeTLS error: %v", err)
				}
				return
			}
			// No explicit files: serve the persisted certificate the local
			// authority signed, the same one Server Tools reports and
			// exports. Only a first start on an empty volume creates it.
			mat, err := tlsStore.Ensure(localca.SubjectFromEnv())
			if err != nil {
				log.Fatalf("Failed preparing the File4Base Local CA in %s: %v", tlsStore.Dir(), err)
			}
			log.Printf("Starting HTTPS listener with the certificate issued by the File4Base Local CA (%s)", mat.CertPath)
			if err := srv.ListenAndServeTLS(mat.CertPath, mat.KeyPath); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Server ListenAndServeTLS error: %v", err)
			}
			return
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server ListenAndServe error: %v", err)
		}
	}()

	log.Println("Server is ready and listening for requests.")

	// Scheduled transfers (#108). Each due schedule is claimed in its
	// solution's catalog before it runs, so several server processes over the
	// same databases never run the same occurrence twice; one process can
	// still be told not to run any with FILE4BASE_TRANSFER_SCHEDULER=false.
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	schedulerDone := make(chan struct{})
	if envBool("FILE4BASE_TRANSFER_SCHEDULER", true) {
		scheduler := transfer.NewScheduler(dbMgr)
		go func() {
			defer close(schedulerDone)
			scheduler.Run(schedulerCtx)
		}()
		log.Printf("Transfer scheduler running (every %s)", scheduler.Interval)
	} else {
		close(schedulerDone)
		log.Println("Transfer scheduler disabled (FILE4BASE_TRANSFER_SCHEDULER=false): scheduled transfers do not run in this process")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	log.Printf("Received signal %v: initiating graceful shutdown...", sig)

	// 1. Immediately fail readiness probe so orchestrator / load balancer stops routing traffic
	isReady.Store(false)

	// 2. Short drain buffer for reverse-proxies (Nginx/Envoy) to discover unready state
	time.Sleep(1 * time.Second)

	// 3. Gracefully shutdown HTTP server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced shutdown with error: %v", err)
	}

	// 4. Stop starting scheduled transfers and let the running ones finish
	// their transaction, within the same shutdown deadline.
	stopScheduler()
	select {
	case <-schedulerDone:
	case <-ctx.Done():
		log.Println("Warning: shutdown deadline reached while a scheduled transfer was still running")
	}

	// 5. Close database pool connections
	dbMgr.Close()
	log.Println("Server and database connections exited cleanly.")
}
