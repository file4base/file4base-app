package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// bootstrapReservation keeps the bootstrap database names from being taken
// by just anyone (#236). The first owner of FILE4BASE_ADMIN_DATABASE, or of
// the database of a FILE4BASE_SUPER_OWNER seed, becomes a server
// administrator, so creating, cloning or migrating into such a database is
// reserved to a super-owner, or to the operator, who proves it with the
// setup code the server prints in its log at startup (or reads from
// FILE4BASE_SETUP_CODE). A fresh install bootstraps with that code.
type bootstrapReservation struct {
	databases map[string]bool
	setupCode string // normalized; empty when nothing is reserved
}

// newBootstrapReservation reserves the admin database and the databases of
// the seeds ("database:username"; malformed entries are ignored, as by
// ConfigureSuperOwner). An empty setupCode is replaced by a random one.
func newBootstrapReservation(adminDatabase string, seeds []string, setupCode string) *bootstrapReservation {
	b := &bootstrapReservation{databases: map[string]bool{}}
	if db := strings.ToLower(strings.TrimSpace(adminDatabase)); db != "" {
		b.databases[db] = true
	}
	for _, raw := range seeds {
		db, user, ok := strings.Cut(raw, ":")
		db, user = strings.ToLower(strings.TrimSpace(db)), strings.TrimSpace(user)
		if ok && db != "" && user != "" {
			b.databases[db] = true
		}
	}
	if len(b.databases) == 0 {
		return b
	}
	b.setupCode = normalizeSetupCode(setupCode)
	if b.setupCode == "" {
		b.setupCode = randomSetupCode()
	}
	return b
}

// randomSetupCode returns 80 random bits as 16 base32 characters.
func randomSetupCode() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		panic("file4base: no randomness for the setup code: " + err.Error())
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
}

// normalizeSetupCode ignores case, spaces and dashes, so the grouped form
// printed in the log can be typed as is.
func normalizeSetupCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
}

// groupedSetupCode is the code as printed: XXXX-XXXX-XXXX-XXXX.
func groupedSetupCode(code string) string {
	var parts []string
	for len(code) > 4 {
		parts = append(parts, code[:4])
		code = code[4:]
	}
	return strings.Join(append(parts, code), "-")
}

func (b *bootstrapReservation) reserved(database string) bool {
	return b != nil && b.databases[strings.ToLower(strings.TrimSpace(database))]
}

func (b *bootstrapReservation) codeMatches(code string) bool {
	given := normalizeSetupCode(code)
	return b.setupCode != "" && given != "" &&
		subtle.ConstantTimeCompare([]byte(given), []byte(b.setupCode)) == 1
}

// logStartup prints the setup code when a reserved database does not exist
// yet, so that the operator can create it. A code given through
// FILE4BASE_SETUP_CODE is never printed.
func (b *bootstrapReservation) logStartup(dbMgr *dbal.MultiDatabaseManager, fromEnv bool) {
	if b == nil || len(b.databases) == 0 || dbMgr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var missing []string
	for db := range b.databases {
		if _, exists, err := dbMgr.LookupDatabase(ctx, db); err != nil || !exists {
			missing = append(missing, db)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	if fromEnv {
		log.Printf("Bootstrap: database(s) %s reserved for server administrators do not exist yet; create them with the FILE4BASE_SETUP_CODE setup code", strings.Join(missing, ", "))
		return
	}
	log.Printf("Bootstrap: database(s) %s reserved for server administrators do not exist yet. To create one without a super-owner, enter this setup code (valid until restart): %s", strings.Join(missing, ", "), groupedSetupCode(b.setupCode))
}

// allow reports whether the caller may create the database. A super-owner
// always may; with acceptsCode, so may whoever gives the setup code. It writes
// the 403 otherwise.
func (b *bootstrapReservation) allow(w http.ResponseWriter, r *http.Request, database, code string, acceptsCode bool) bool {
	if !b.reserved(database) {
		return true
	}
	if sess, ok := auth.FromContext(r.Context()); ok && sess.SuperOwner {
		return true
	}
	if acceptsCode && b.codeMatches(code) {
		log.Printf("Bootstrap: reserved database %q created with the setup code", strings.ToLower(strings.TrimSpace(database)))
		return true
	}
	detail := "database '" + database + "' is reserved: its owners become server administrators " +
		"(FILE4BASE_ADMIN_DATABASE / FILE4BASE_SUPER_OWNER), so only a super-owner can create it"
	switch {
	case acceptsCode && strings.TrimSpace(code) != "":
		detail = "the setup code is not valid; it is printed in the server log at startup"
	case acceptsCode:
		detail += ", or enter the setup code printed in the server log at startup"
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type":                telemetry.DefaultTypeForStatus(http.StatusForbidden),
		"title":               "Reserved Database",
		"status":              http.StatusForbidden,
		"detail":              detail,
		"instance":            r.URL.Path,
		"trace_id":            telemetry.TraceIDFromContext(r.Context()),
		"reserved_database":   strings.ToLower(strings.TrimSpace(database)),
		"setup_code_required": acceptsCode,
	})
	return false
}
