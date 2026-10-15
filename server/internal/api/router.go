package api

import (
	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/crypto"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/go-chi/chi/v5"
)

// Options configures the API routes.
type Options struct {
	// AllowPublicDatabaseCreation lets callers without a session create new
	// databases. See SolutionHandlerOptions.
	AllowPublicDatabaseCreation bool
	// ControlStore persists project ↔ database mappings, options, and user
	// table/layout permissions in SQLite. When nil, an in-memory SQLite store
	// is created automatically.
	ControlStore *control.Store
	// ChannelSecret is the shared secret used to derive the AES-256-GCM key for
	// encrypted payload transfers between the Web Client and the API.
	ChannelSecret string
	// AdminDatabase names the database whose owners are the server
	// administrators (FILE4BASE_ADMIN_DATABASE). Only they reach the
	// server-wide Server Tools (containers, TLS, every session, audit log,
	// schedules, notifications, telemetry). Empty disables those tools; the
	// tools of one database (backups, maintenance) stay available to that
	// database's owners and admins, for their own database only.
	AdminDatabase string
	// BackupsDir is where Server Tools keeps backup snapshots
	// (default ./data/backups).
	BackupsDir string
	// Version is the running server version (AppVersion), reported by
	// Server Tools > Containers and compared with the latest release.
	Version string
	// LoginThrottle rate-limits password checks (sign-in and credential-based
	// database drops). When nil, Mount installs a default one.
	LoginThrottle *auth.LoginThrottle
}

// Mount registers every domain route of the API on r.
//
// Routes fall into two groups:
//   - public: sign-in and the database selector used before sign-in;
//   - protected: everything else, which requires a valid session and always
//     runs against the database that session signed in to.
func Mount(r chi.Router, dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store, opts Options) {
	controlStore := opts.ControlStore
	if controlStore == nil {
		controlStore, _ = control.NewStore(":memory:")
	}

	authMW := NewAuthMiddleware(sessions, dbMgr)

	throttle := opts.LoginThrottle
	if throttle == nil {
		throttle = DefaultLoginThrottle()
	}

	schemaHandler := NewSchemaHandler()
	dataHandler := NewDataHandler()
	securityHandler := NewSecurityHandlerWithControl(dbMgr, sessions, controlStore)
	securityHandler.throttle = throttle
	solutionHandler := NewSolutionHandler(dbMgr, sessions, SolutionHandlerOptions{
		AllowPublicDatabaseCreation: opts.AllowPublicDatabaseCreation,
		ControlStore:                controlStore,
	})
	solutionHandler.throttle = throttle
	serverAdminHandler := NewServerAdminHandler(dbMgr, sessions, controlStore, opts.Version)
	serverAdminHandler.ConfigureAccess(opts.AdminDatabase, opts.BackupsDir)

	r.Get("/api/v1/crypto/handshake", crypto.HandshakeHandler(opts.ChannelSecret))

	r.Group(func(r chi.Router) {
		r.Use(crypto.Middleware(opts.ChannelSecret))
		r.Use(authMW.Optional)
		securityHandler.RegisterPublicRoutes(r)
		solutionHandler.RegisterPublicRoutes(r)
	})

	r.Group(func(r chi.Router) {
		r.Use(crypto.Middleware(opts.ChannelSecret))
		r.Use(authMW.Authenticate)
		schemaHandler.RegisterRoutes(r)
		dataHandler.RegisterRoutes(r)
		securityHandler.RegisterRoutes(r)
		solutionHandler.RegisterRoutes(r)
		serverAdminHandler.RegisterRoutes(r)
	})
}
