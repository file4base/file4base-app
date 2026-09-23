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

func init() {
	dbal.RegisterDialect(dbal.EnginePostgres, func() dbal.Dialect { return postgres.New() })
	dbal.RegisterDialect(dbal.EngineMariaDB, func() dbal.Dialect { return mariadb.New() })
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

	// Attempt connection to database
	driver, err := dbal.Connect(dbal.DriverConfig{
		EngineType: engineType,
		DSN:        dsn,
	})
	var schemaSvc *schema.Service
	var dataSvc *data.Service
	if err != nil {

		log.Printf("Warning: Database driver could not connect at startup: %v", err)
	} else {
		defer driver.Close()
		schemaSvc = schema.NewService(driver)
		dataSvc = data.NewService(driver)
		ctxInit, cancelInit := context.WithTimeout(context.Background(), 10*time.Second)
		if err := schemaSvc.EnsureSystemTables(ctxInit); err != nil {
			log.Printf("Warning: Failed to ensure system tables: %v", err)
		} else {
			log.Println("System catalog (sys_*) tables initialized successfully.")
		}
		cancelInit()
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	if schemaSvc != nil {
		schemaHandler := api.NewSchemaHandler(schemaSvc)
		schemaHandler.RegisterRoutes(r)
	}
	if dataSvc != nil {
		dataHandler := api.NewDataHandler(dataSvc)
		dataHandler.RegisterRoutes(r)
	}



	// Health check endpoint
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		dbStatus := "disconnected"
		if driver != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := driver.Ping(ctx); err == nil {
				dbStatus = "connected"
			} else {
				dbStatus = "error: " + err.Error()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      status,
			"app":         "File4Base Server",
			"engine":      engineType,
			"database":    dbStatus,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
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
