package api

import (
	"net"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/clientip"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/crypto"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/realtime"
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
	// SuperOwnerSeed bootstraps super-ownership (FILE4BASE_SUPER_OWNER). Each
	// entry is "database:username"; those accounts sign in as super-owners even
	// before a super_owner flag has been persisted for them, so the first
	// super-owner can exist without another one to grant it. A super-owner has
	// full, server-wide control regardless of AdminDatabase.
	SuperOwnerSeed []string
	// BackupsDir is where Server Tools keeps backup snapshots
	// (default ./data/backups).
	BackupsDir string
	// Version is the running server version (AppVersion), reported by
	// Server Tools > Containers and compared with the latest release.
	Version string
	// LoginThrottle rate-limits password checks (sign-in and credential-based
	// database drops). When nil, Mount installs a default one.
	LoginThrottle *auth.LoginThrottle
	// Hub pushes change notifications to connected clients. When nil, Mount
	// creates one; the server keeps it for its lifetime.
	Hub *realtime.Hub
	// TrustedProxies lists the reverse-proxy networks whose X-Forwarded-For /
	// X-Real-IP headers may be trusted when resolving a caller's address for
	// the audit log, session metadata and the login-throttle key
	// (FILE4BASE_TRUSTED_PROXIES). Empty means no proxy is trusted, so those
	// headers are ignored and the direct TCP peer address is used.
	TrustedProxies []*net.IPNet
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

	ipResolver := clientip.NewResolver(opts.TrustedProxies)

	authMW := NewAuthMiddleware(sessions, dbMgr)
	authMW.clientIP = ipResolver

	throttle := opts.LoginThrottle
	if throttle == nil {
		throttle = DefaultLoginThrottle()
	}

	schemaHandler := NewSchemaHandler()
	dataHandler := NewDataHandler()
	securityHandler := NewSecurityHandlerWithControl(dbMgr, sessions, controlStore)
	securityHandler.throttle = throttle
	securityHandler.clientIP = ipResolver
	securityHandler.ConfigureSuperOwner(opts.SuperOwnerSeed)
	securityHandler.ConfigureServerAdmin(opts.AdminDatabase)
	solutionHandler := NewSolutionHandler(dbMgr, sessions, SolutionHandlerOptions{
		AllowPublicDatabaseCreation: opts.AllowPublicDatabaseCreation,
		ControlStore:                controlStore,
	})
	solutionHandler.throttle = throttle
	solutionHandler.clientIP = ipResolver
	serverAdminHandler := NewServerAdminHandler(dbMgr, sessions, controlStore, opts.Version)
	serverAdminHandler.ConfigureAccess(opts.AdminDatabase, opts.BackupsDir)

	hub := opts.Hub
	if hub == nil {
		hub = realtime.NewHub()
	}
	realtimeHandler := NewRealtimeHandler(hub, sessions)

	r.Get("/api/v1/crypto/handshake", crypto.HandshakeHandler(opts.ChannelSecret))
	// The change-notification WebSocket authenticates with its first message,
	// so it sits outside the bearer-header auth middleware.
	r.Get("/api/v1/realtime", realtimeHandler.ServeWS)

	r.Group(func(r chi.Router) {
		r.Use(crypto.Middleware(opts.ChannelSecret))
		r.Use(authMW.Optional)
		securityHandler.RegisterPublicRoutes(r)
		solutionHandler.RegisterPublicRoutes(r)
		serverAdminHandler.RegisterPublicRoutes(r)
	})

	r.Group(func(r chi.Router) {
		r.Use(crypto.Middleware(opts.ChannelSecret))
		r.Use(authMW.Authenticate)
		r.Use(emitChangeEvents(hub))
		schemaHandler.RegisterRoutes(r)
		dataHandler.RegisterRoutes(r)
		securityHandler.RegisterRoutes(r)
		solutionHandler.RegisterRoutes(r)
		serverAdminHandler.RegisterRoutes(r)
	})
}
