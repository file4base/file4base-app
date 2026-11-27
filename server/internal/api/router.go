package api

import (
	"net"
	"net/http"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/clientip"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/crypto"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/fmdapi"
	"github.com/file4base/file4base-app/server/internal/mail"
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
	// SetupCode (FILE4BASE_SETUP_CODE) lets the operator create a database
	// that AdminDatabase or SuperOwnerSeed reserves without being a
	// super-owner (#236). Empty: a random code is printed in the log at
	// startup while such a database does not exist.
	SetupCode string
	// BackupsDir is where Server Tools keeps backup snapshots
	// (default ./data/backups).
	BackupsDir string
	// TLSDir persists the File4Base Local CA and the leaf certificate it
	// signs (default FILE4BASE_TLS_DIR, or ./data/tls). The HTTPS listener
	// serves that leaf, Server Tools reports it, and the authority's public
	// certificate is exported from there.
	TLSDir string
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
	// MaxMigrationUploadBytes and MigrationUploadDir configure multipart
	// FileMaker migration uploads. See SolutionHandlerOptions.
	MaxMigrationUploadBytes int64
	MigrationUploadDir      string
	// MailSender talks to the SMTP servers of Send Mail (#133); nil uses
	// the default one. Tests point it at an in-process server.
	MailSender *mail.Sender
	// DataAPI configures the FileMaker Data API at /fmi/data (#208). It is
	// mounted only when it serves at least one database.
	DataAPI *fmdapi.Config
	// Schedules runs the server schedules, backups and maintenance (#188,
	// #204), while its Context lasts. Without one they are stored and
	// listed but not run, and Run Now is refused.
	Schedules *ScheduleOptions
}

// nodeOnlyMethods are the methods Node's HTTP parser accepts that chi does
// not know (chi v5.5 knows the nine of net/http and QUERY). chi answers them
// with a bare 405 before any route; on /fmi/data, FileMaker Server's Express
// front end routes them like any other method, answering 1704 on a known
// path and its 404 page on an unknown one.
//
// They are not registered with chi.RegisterMethod: chi holds its methods in
// the bits of an int, and on a 32-bit build these overflow it, which panics
// at start-up. dataAPIMethods hands them to the Data API instead.
var nodeOnlyMethods = map[string]bool{
	"ACL": true, "BIND": true, "CHECKOUT": true, "COPY": true, "LINK": true, "LOCK": true,
	"M-SEARCH": true, "MERGE": true, "MKACTIVITY": true, "MKCALENDAR": true, "MKCOL": true,
	"MOVE": true, "NOTIFY": true, "PROPFIND": true, "PROPPATCH": true, "PURGE": true,
	"REBIND": true, "REPORT": true, "SEARCH": true, "SOURCE": true, "SUBSCRIBE": true,
	"UNBIND": true, "UNLINK": true, "UNLOCK": true, "UNSUBSCRIBE": true,
}

// dataAPIMethods sends a request for a /fmi/data path whose method chi
// cannot route, one of nodeOnlyMethods, straight to the Data API. Every
// other request, those for /api/v1 included, goes on to chi unchanged. It
// wraps the whole router rather than being one of its middlewares, which chi
// takes only before the first route, and the server registers its health
// routes before Mount; the server's own middlewares wrap it in turn.
func dataAPIMethods(dataAPI http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// chi routes the escaped path when there is one.
			path := r.URL.RawPath
			if path == "" {
				path = r.URL.Path
			}
			if nodeOnlyMethods[r.Method] && fmdapi.IsPath(path) {
				dataAPI.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Mount registers every domain route of the API on r, and returns the
// handler to serve: r, in front of which the Data API answers the methods
// chi does not know (dataAPIMethods) when it is on.
//
// Routes fall into two groups:
//   - public: sign-in and the database selector used before sign-in;
//   - protected: everything else, which requires a valid session and always
//     runs against the database that session signed in to.
func Mount(r chi.Router, dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store, opts Options) http.Handler {
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

	// The Data API, when it serves a database.
	var dataAPI *fmdapi.Handler
	if opts.DataAPI != nil && len(opts.DataAPI.Databases) > 0 {
		dataAPI = fmdapi.New(*opts.DataAPI, fmdapi.Deps{
			Databases: dbMgr,
			Sessions:  sessions,
			Throttle:  throttle,
			ClientIP:  ipResolver.ClientIP,
		})
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
		MaxMigrationUploadBytes:     opts.MaxMigrationUploadBytes,
		MigrationUploadDir:          opts.MigrationUploadDir,
	})
	solutionHandler.throttle = throttle
	solutionHandler.clientIP = ipResolver
	solutionHandler.bootstrap = newBootstrapReservation(opts.AdminDatabase, opts.SuperOwnerSeed, opts.SetupCode)
	solutionHandler.bootstrap.logStartup(dbMgr)
	serverAdminHandler := NewServerAdminHandler(dbMgr, sessions, controlStore, opts.Version)
	serverAdminHandler.ConfigureAccess(opts.AdminDatabase, opts.BackupsDir)
	serverAdminHandler.ConfigureTLS(opts.TLSDir)
	if opts.MailSender != nil {
		serverAdminHandler.mailSender = opts.MailSender
	}
	if opts.Schedules != nil && opts.Schedules.Context != nil {
		serverAdminHandler.StartSchedules(*opts.Schedules)
	}

	hub := opts.Hub
	if hub == nil {
		hub = realtime.NewHub()
	}
	realtimeHandler := NewRealtimeHandler(hub, sessions)
	// Transfers need the session store and the connection manager: the far
	// end of a cross-solution transfer is authorized by its own session
	// token, never by the session this request was made with (#96).
	transferHandler := NewTransferHandler(dbMgr, sessions)

	r.Get("/api/v1/crypto/handshake", crypto.HandshakeHandler(opts.ChannelSecret))
	// The change-notification WebSocket authenticates with its first message,
	// so it sits outside the bearer-header auth middleware.
	r.Get("/api/v1/realtime", realtimeHandler.ServeWS)

	// The FileMaker Data API has its own authentication and envelope, and
	// stays outside the payload-encryption channel.
	if dataAPI != nil {
		r.Handle("/fmi/data", dataAPI)
		r.Handle("/fmi/data/*", dataAPI)
	}

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
		dataHandler.RegisterOccurrenceRoutes(r)
		transferHandler.RegisterRoutes(r)
		NewFileReferenceHandler(dbMgr, sessions).RegisterRoutes(r)
		securityHandler.RegisterRoutes(r)
		solutionHandler.RegisterRoutes(r)
		serverAdminHandler.RegisterRoutes(r)
	})

	if dataAPI == nil {
		return r
	}
	return dataAPIMethods(dataAPI)(r)
}
