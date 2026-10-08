package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const AppVersion = "0.11.0"

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
}

// corsMiddleware answers CORS preflights and tags responses for the allowed
// origins. Sessions travel in the Authorization header (never in cookies), so
// a browser cannot attach a user's credentials to a cross-site request on its
// own; the allow-list still limits which web origins may call the API at all.
// allowed == ["*"] accepts any origin.
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
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With, X-Database-Name, traceparent, tracestate, X-Trace-ID")
				w.Header().Set("Access-Control-Expose-Headers", "Link, Content-Length, Content-Disposition, traceparent, X-Trace-ID, Deprecation, Sunset")
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

func main() {
	engineStr := os.Getenv("DB_ENGINE")
	if engineStr == "" {
		engineStr = "postgres"
	}

	engineType, err := dbal.ParseEngineType(engineStr)
	if err != nil {
		log.Fatalf("Invalid DB_ENGINE: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Println("Warning: DATABASE_URL is not set; falling back to the local development DSN with the default password. Set DATABASE_URL for any non-development deployment.")
		if engineType == dbal.EnginePostgres {
			dsn = "postgres://file4base:dev_password@localhost:5432/postgres?sslmode=disable"
		} else {
			dsn = "file4base:dev_password@tcp(localhost:3306)/mysql?parseTime=true"
		}
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

	// Session store: every signed-in client holds a bearer token bound to
	// exactly one database. There is no server-wide "active database".
	sessions := auth.NewStore(envDuration("SESSION_TTL", auth.DefaultSessionTTL))

	corsOrigins := []string{"*"}
	if raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS")); raw != "" {
		corsOrigins = strings.Split(raw, ",")
	}
	allowPublicCreate := envBool("ALLOW_PUBLIC_DATABASE_CREATION", true)
	if allowPublicCreate {
		log.Println("Notice: anyone who can reach this server may create new databases (ALLOW_PUBLIC_DATABASE_CREATION=true). Set it to false on shared or internet-facing servers.")
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
	r.Use(telemetry.Middleware("file4base-api", AppVersion))
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

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

	// API domain routes: public sign-in/database selector, and the protected
	// routes that run against the database of the caller's session.
	api.Mount(r, dbMgr, sessions, api.Options{
		AllowPublicDatabaseCreation: allowPublicCreate,
	})

	srv := &http.Server{
		Addr:              port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server ListenAndServe error: %v", err)
		}
	}()

	log.Println("Server is ready and listening for requests.")

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

	// 4. Close database pool connections
	dbMgr.Close()
	log.Println("Server and database connections exited cleanly.")
}
