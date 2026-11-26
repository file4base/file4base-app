package fmdapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// serverAccountIDPrefix marks the local mirror of a server account (#187).
// TODO(#211): server accounts are refused at the Data API until the account
// design note decides how they reach it.
const serverAccountIDPrefix = "srv-"

// dataAPIContainer is the client kind Server Tools lists Data API sessions
// under.
const dataAPIContainer = "fmi-data-api"

// fmDataSourceStrings are the string properties of an fmDataSource entry.
var fmDataSourceStrings = []string{"database", "username", "password", "oAuthRequestId", "oAuthIdentifier"}

// login opens a session: POST {version}/databases/{database}/sessions with
// the account in Basic credentials.
func (h *Handler) login(w http.ResponseWriter, req *request) {
	if err := checkLoginBody(req.body); err != nil {
		writeError(w, err)
		return
	}

	header, _ := authorization(req.Request)
	var basic bool
	var username, password string
	if strings.TrimSpace(header) == "" {
		if req.Header.Get("X-FM-Data-OAuth-Request-Id") == "" || req.Header.Get("X-FM-Data-OAuth-Identifier") == "" {
			writeError(w, errLoginCredentialsMissing)
			return
		}
		// OAuth sign-in is not served: the engine refuses it below.
	} else if s, creds := scheme(header); strings.EqualFold(s, "basic") {
		var ok bool
		username, password, ok = strings.Cut(wtf8(decodeUTF8Text(decodeBase64Node(creds))), ":")
		if !ok {
			writeError(w, errBasicInvalid)
			return
		}
		basic = true
	}

	// The engine opens the file first, then checks the account.
	ctx := req.Context()
	name := req.params["database"]
	alias, ok := h.cfg.alias(name)
	if !ok {
		writeError(w, engineError("802"))
		return
	}
	database, exists, err := h.deps.Databases.LookupDatabase(ctx, alias.Database)
	if err != nil {
		writeError(w, engineError("-1"))
		return
	}
	if !exists {
		writeError(w, engineError("802"))
		return
	}
	driver, err := h.deps.Databases.DriverFor(ctx, database)
	if err != nil {
		writeError(w, engineError("802"))
		return
	}
	svc := schema.NewService(driver)
	if !svc.HasSystemCatalog(ctx) {
		writeError(w, engineError("802"))
		return
	}
	if !basic {
		writeError(w, engineError("212"))
		return
	}

	// A locked-out account is refused before any work on the database.
	clientIP := h.deps.ClientIP(req.Request)
	key := auth.LoginThrottleKey(database, username, clientIP)
	if h.deps.Throttle != nil {
		if allowed, _ := h.deps.Throttle.Allow(key); !allowed {
			writeError(w, engineError("212"))
			return
		}
	}
	if err := svc.EnsureSystemTables(ctx); err != nil {
		writeError(w, engineError("-1"))
		return
	}
	user, err := svc.Authenticate(ctx, username, password)
	if err != nil {
		// As at the /api/v1 sign-in, a missing second factor does not count
		// against the account: the password was right. Data API sign-ins
		// cannot give one, so such an account is refused all the same.
		if h.deps.Throttle != nil && !errors.Is(err, schema.ErrMFARequired) {
			h.deps.Throttle.Fail(key)
		}
		writeError(w, engineError("212"))
		return
	}
	if h.deps.Throttle != nil {
		h.deps.Throttle.Succeed(key)
	}
	if strings.HasPrefix(user.ID, serverAccountIDPrefix) {
		writeError(w, engineError("212"))
		return
	}
	// FileMaker's fmrest extended privilege is the fmrest capability of the
	// account's role (#39): held by owners, and by other roles only once an
	// owner grants it. It is per role, not per privilege set as in FileMaker
	// (#211, #213).
	allowed, err := svc.RoleAllows(ctx, user.Role, schema.CapabilityFMRest)
	if err != nil {
		writeError(w, engineError("-1"))
		return
	}
	if !allowed {
		writeError(w, engineError("9"))
		return
	}

	token, _, err := h.deps.Sessions.CreateDataAPI(user.ID, user.Username, user.Role, database, name, user.Stamp)
	if err != nil {
		writeError(w, engineError("-1"))
		return
	}
	h.deps.Sessions.TouchMeta(token, clientIP, dataAPIContainer, strings.TrimSpace(req.UserAgent()))
	w.Header().Set("X-FM-Data-Access-Token", token)
	writeOK(w, Object{{"token", token}}, true)
}

// checkLoginBody validates a sign-in body as the request validator and the
// front end do. fmDataSource is checked but not acted on yet (P9): FileMaker
// Server itself never fails a sign-in over it.
func checkLoginBody(body *Value) *Error {
	if body == nil {
		return nil
	}
	if body.Kind != KindObject {
		return parameterError("Expected type object but found type " + body.SchemaType())
	}
	if ds, ok := body.Get("fmDataSource"); ok {
		if ds.Kind != KindArray {
			return schemaTypeError("fmDataSource", "array", ds)
		}
		for _, item := range ds.Elems {
			if item.Kind != KindObject {
				return schemaTypeError("fmDataSource", "object", item)
			}
			if _, ok := item.Get("database"); !ok {
				return parameterError("'fmDataSource': Missing required property: database")
			}
			for _, prop := range fmDataSourceStrings {
				if v, ok := item.Get(prop); ok && v.Kind != KindString {
					return schemaTypeError("fmDataSource", "string", v)
				}
			}
		}
	}
	return unknownParameters(body.Keys(), func(k string) bool { return k == "fmDataSource" })
}

// logout closes a session: DELETE {version}/databases/{database}/sessions/{token}.
// The token in the path is the credential; the database must be named as
// at sign-in.
func (h *Handler) logout(w http.ResponseWriter, req *request) {
	if !h.deps.Sessions.RevokeDataAPI(req.params["token"], req.params["database"]) {
		writeError(w, engineError("952"))
		return
	}
	writeOK(w, emptyObject, true)
}

// bearer authenticates a call by its Bearer token. renew extends the
// session's idle lifetime, as every call but validateSession does.
func (h *Handler) bearer(req *request, renew bool) (auth.Session, *Error) {
	header, present := authorization(req.Request)
	if !present {
		return auth.Session{}, errAuthorizationRequired
	}
	token, err := bearerToken(header)
	if err != nil {
		return auth.Session{}, err
	}
	return h.session(req, token, renew)
}

// bearerToken is the front end's reading of the Authorization header of a
// call that needs a session: Basic credentials are refused; the token is
// what follows "Bearer", or the whole header without that scheme.
func bearerToken(header string) (string, *Error) {
	s, token := scheme(header)
	switch {
	case strings.EqualFold(s, "basic"):
		return "", errBasicInvalid
	case !strings.EqualFold(s, "bearer"):
		token = strings.TrimSpace(header)
	}
	return token, nil
}

// session is the engine's check of a token. A call naming a database must
// name the one the session signed in to, exactly as it did.
func (h *Handler) session(req *request, token string, renew bool) (auth.Session, *Error) {
	invalid := engineError("952")
	sess, ok := h.deps.Sessions.DataAPI(token, false)
	if !ok {
		return auth.Session{}, invalid
	}
	if name, named := req.params["database"]; named && name != sess.DataAPIName {
		return auth.Session{}, invalid
	}

	// The account must still be in the state the session was opened with,
	// as for every File4Base session: a deleted, disabled, demoted or
	// re-passworded account loses its tokens at once.
	ctx := req.Context()
	driver, err := h.deps.Databases.DriverFor(ctx, sess.Database)
	if err != nil {
		return auth.Session{}, engineError("-1")
	}
	svc := schema.NewService(driver)
	stamp, err := svc.CurrentAccountStamp(ctx, sess.UserID)
	if err != nil && !errors.Is(err, schema.ErrAccountNotFound) {
		return auth.Session{}, engineError("-1")
	}
	if err != nil || stamp != sess.Stamp {
		h.deps.Sessions.Revoke(token)
		return auth.Session{}, invalid
	}
	// Nor may it have turned on two-step verification since: such an
	// account cannot sign in here, so it keeps no token either.
	mfa, err := svc.UserMFAEnabled(ctx, sess.UserID)
	if err != nil && !errors.Is(err, schema.ErrAccountNotFound) {
		return auth.Session{}, engineError("-1")
	}
	if err != nil || mfa {
		h.deps.Sessions.Revoke(token)
		return auth.Session{}, invalid
	}
	// So must its role's fmrest privilege, however it was withdrawn (in
	// Manage Security, which also ends these sessions, or by a solution
	// import), and whenever: also during the sign-in that read it.
	allowed, err := svc.RoleAllows(ctx, sess.Role, schema.CapabilityFMRest)
	if err != nil {
		return auth.Session{}, engineError("-1")
	}
	if !allowed {
		h.deps.Sessions.Revoke(token)
		return auth.Session{}, invalid
	}
	if renew {
		if sess, ok = h.deps.Sessions.DataAPI(token, true); !ok {
			return auth.Session{}, invalid
		}
		h.deps.Sessions.TouchMeta(token, h.deps.ClientIP(req.Request), dataAPIContainer, strings.TrimSpace(req.UserAgent()))
	}
	return sess, nil
}

// decodeBase64Node decodes base64 as Node's Buffer.from(s, "base64") does:
// both alphabets, characters outside them skipped, the end at the first =.
func decodeBase64Node(s string) []byte {
	var out []byte
	var acc uint32
	n := 0
loop:
	for i := 0; i < len(s); i++ {
		var v uint32
		switch c := s[i]; {
		case c >= 'A' && c <= 'Z':
			v = uint32(c - 'A')
		case c >= 'a' && c <= 'z':
			v = uint32(c-'a') + 26
		case c >= '0' && c <= '9':
			v = uint32(c-'0') + 52
		case c == '+' || c == '-':
			v = 62
		case c == '/' || c == '_':
			v = 63
		case c == '=':
			break loop
		default:
			continue
		}
		acc = acc<<6 | v
		if n++; n == 4 {
			out = append(out, byte(acc>>16), byte(acc>>8), byte(acc))
			acc, n = 0, 0
		}
	}
	switch n {
	case 2:
		out = append(out, byte(acc>>4))
	case 3:
		out = append(out, byte(acc>>10), byte(acc>>2))
	}
	return out
}
