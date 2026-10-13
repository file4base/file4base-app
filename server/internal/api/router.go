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

	schemaHandler := NewSchemaHandler()
	dataHandler := NewDataHandler()
	securityHandler := NewSecurityHandlerWithControl(dbMgr, sessions, controlStore)
	solutionHandler := NewSolutionHandler(dbMgr, sessions, SolutionHandlerOptions{
		AllowPublicDatabaseCreation: opts.AllowPublicDatabaseCreation,
		ControlStore:                controlStore,
	})

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
	})
}

