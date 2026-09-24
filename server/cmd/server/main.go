package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/dbal/mariadb"
	"github.com/file4base/file4base-app/server/internal/dbal/postgres"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"


)

const AppVersion = "0.4.4"

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-Requested-With")
		w.Header().Set("Access-Control-Expose-Headers", "Link, Content-Length")
		w.Header().Set("Access-Control-Max-Age", "300")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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
		if engineType == dbal.EnginePostgres {
			dsn = "postgres://file4base:dev_password@localhost:5432/file4base_dev?sslmode=disable"
		} else {
			dsn = "file4base:dev_password@tcp(localhost:3306)/file4base_dev?parseTime=true"
		}
	}

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = ":8080"
	}

	log.Printf("Starting File4Base Server [Engine: %s, Port: %s]", engineType, port)

	// Initialize MultiDatabaseManager
	dbMgr, err := dbal.NewMultiDatabaseManager(engineType, dsn)
	if err != nil {
		log.Fatalf("Failed initializing multi-database manager: %v", err)
	}
	defer dbMgr.Close()

	schemaSvc := schema.NewService(dbMgr)
	dataSvc := data.NewService(dbMgr)

	// Connect and ensure system tables with retry loop for clean container startup
	go func() {
		for i := 0; i < 15; i++ {
			ctxInit, cancelInit := context.WithTimeout(context.Background(), 2*time.Second)
			if err := dbMgr.Ping(ctxInit); err == nil {
				if err := schemaSvc.EnsureSystemTables(ctxInit); err == nil {
					log.Printf("System catalog (sys_*) tables initialized on active database: %s", dbMgr.ActiveDatabase())
					cancelInit()
					return
				}
			}
			cancelInit()
			time.Sleep(1 * time.Second)
		}
	}()

	r := chi.NewRouter()
	r.Use(corsMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	schemaHandler := api.NewSchemaHandler(schemaSvc)
	schemaHandler.RegisterRoutes(r)

	dataHandler := api.NewDataHandler(dataSvc)
	dataHandler.RegisterRoutes(r)

	solutionHandler := api.NewSolutionHandler(dbMgr, schemaSvc)
	solutionHandler.RegisterRoutes(r)

	securityHandler := api.NewSecurityHandler(dbMgr, schemaSvc)
	securityHandler.RegisterRoutes(r)

	// Health check endpoint
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		dbStatus := "disconnected"
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := dbMgr.Ping(ctx); err == nil {
			dbStatus = "connected"
		} else {
			dbStatus = "error: " + err.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          status,
			"app":             "File4Base Server",
			"version":         AppVersion,
			"engine":          engineType,
			"database":        dbStatus,
			"active_database": dbMgr.ActiveDatabase(),
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		})
	})

	srv := &http.Server{
		Addr:    port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server ListenAndServe error: %v", err)
		}
	}()

	log.Println("Server is ready and listening for requests.")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited cleanly.")
}
