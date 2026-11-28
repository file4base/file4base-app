package fmdapi

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Databases is what the Data API needs of the database manager.
type Databases interface {
	LookupDatabase(ctx context.Context, name string) (string, bool, error)
	DriverFor(ctx context.Context, name string) (dbal.DatabaseDriver, error)
}

// Deps are the services the Data API shares with the rest of the server.
type Deps struct {
	Databases Databases
	Sessions  *auth.Store
	// Throttle is the sign-in throttle of the /api/v1 sign-in, so that both
	// count the failures of an account together.
	Throttle *auth.LoginThrottle
	// ClientIP resolves the caller's address, trusting forwarding headers
	// only from trusted proxies.
	ClientIP func(*http.Request) string
}

// Handler serves /fmi/data.
type Handler struct {
	cfg  Config
	deps Deps
}

// New returns the Data API handler for cfg.
func New(cfg Config, deps Deps) *Handler {
	if cfg.ProductName == "" {
		cfg.ProductName = DefaultProductName
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if deps.ClientIP == nil {
		deps.ClientIP = remoteHost
	}
	return &Handler{cfg: cfg, deps: deps}
}

// request is a call that passed the front end's routing checks.
type request struct {
	*http.Request
	ep     endpoint
	params map[string]string
	// body is the parsed JSON body; nil when the request has none.
	body *Value
}

// ServeHTTP runs a request through the front end's checks in their order,
// then the endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w = headWriter{w}
	}
	// Express routes the path as sent; an absolute-form target (a proxy's
	// request line) is reduced to its path and query.
	target := r.RequestURI
	if !strings.HasPrefix(target, "/") {
		target = r.URL.RequestURI()
	}
	path, query, hasQuery := strings.Cut(target, "?")
	path = mergeSlashes(path)
	resource := path
	if hasQuery {
		resource += "?" + query
	}

	// body-parser runs before routing.
	var body *Value
	if isJSONRequest(r) {
		v, err := readJSONBody(r, h.cfg.MaxBodyBytes)
		if err != nil {
			writeError(w, err)
			return
		}
		body = &v
	}

	rt, raw, ok := matchRoute(path)
	if !ok {
		writeNotFound(w, r.Method, resource)
		return
	}
	params, perr := decodeParams(raw)
	if perr != nil {
		writeError(w, perr)
		return
	}
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	ep, ok := rt.methods[method]
	if !ok {
		writeError(w, errMethodNotAllowed)
		return
	}
	if !apiVersions[params["version"]] {
		writeVersionError(w)
		return
	}
	spec := endpointSpecs[ep]
	if spec.consumes != "" && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		// The request validator compares the media type as sent, before
		// any parameter.
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		if ct, _, _ = strings.Cut(ct, ";"); ct != spec.consumes {
			writeError(w, contentTypeError(ct, spec.consumes))
			return
		}
	}

	req := &request{Request: r, ep: ep, params: params, body: body}
	switch ep {
	case epProductInfo:
		h.productInfo(w)
	case epDatabases:
		h.databases(w, req)
	case epLogin:
		h.login(w, req)
	case epLogout:
		h.logout(w, req)
	case epLayouts:
		h.layouts(w, req)
	case epScripts:
		h.scripts(w, req)
	case epLayout:
		h.layout(w, req)
	case epMetadata:
		h.metadata(w, req)
	case epValidateSession:
		if _, err := h.bearer(req, false); err != nil {
			writeError(w, err)
			return
		}
		writeOK(w, Object{{"isSessionInUse", false}}, true)
	default:
		writeError(w, h.notServed(req, spec))
	}
}

// notServed answers an endpoint File4Base does not serve yet: the request
// goes through the checks the endpoint will make, in their order, then the
// engine says the command is unavailable.
func (h *Handler) notServed(req *request, spec endpointSpec) *Error {
	header, present := authorization(req.Request)
	if !present {
		return errAuthorizationRequired
	}
	if spec.body != nil {
		if err := spec.body(req.body); err != nil {
			return err
		}
	}
	token, err := bearerToken(header)
	if err != nil {
		return err
	}
	if spec.params != nil {
		if err := spec.params(req); err != nil {
			return err
		}
	}
	if _, err := h.session(req, token, true); err != nil {
		return err
	}
	return engineError("3")
}

// remoteHost is the peer address without its port.
func remoteHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// authorization returns the first Authorization header and whether the
// request has one at all.
func authorization(r *http.Request) (string, bool) {
	vals := r.Header.Values("Authorization")
	if len(vals) == 0 {
		return "", false
	}
	return vals[0], true
}

// scheme splits an Authorization header into its scheme and credentials.
func scheme(header string) (string, string) {
	s, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	return s, strings.TrimSpace(rest)
}

// headWriter answers a HEAD request with the headers of the GET reply.
type headWriter struct{ http.ResponseWriter }

func (headWriter) Write(b []byte) (int, error) { return len(b), nil }
