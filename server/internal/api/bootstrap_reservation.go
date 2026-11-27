package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
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
// setup code the server prints in its log (or reads from
// FILE4BASE_SETUP_CODE). A fresh install bootstraps with that code.
type bootstrapReservation struct {
	adminDatabase string
	// databases maps each reserved database to its seeded usernames.
	databases map[string][]string
	setupCode string // normalized; empty when nothing is reserved
	fromEnv   bool   // the code is FILE4BASE_SETUP_CODE: never print it
	printed   atomic.Bool
}

// minSetupCodeLength is the length of the random code; a shorter
// FILE4BASE_SETUP_CODE could be guessed through the public creation route.
const minSetupCodeLength = 16

// newBootstrapReservation reserves the admin database and the databases of
// the seeds ("database:username"; malformed entries are ignored, as by
// ConfigureSuperOwner). An empty or too short setupCode is replaced by a
// random one.
func newBootstrapReservation(adminDatabase string, seeds []string, setupCode string) *bootstrapReservation {
	b := &bootstrapReservation{databases: map[string][]string{}}
	if db := strings.ToLower(strings.TrimSpace(adminDatabase)); db != "" {
		b.adminDatabase = db
		b.databases[db] = nil
	}
	for _, raw := range seeds {
		db, user, ok := strings.Cut(raw, ":")
		db, user = strings.ToLower(strings.TrimSpace(db)), strings.ToLower(strings.TrimSpace(user))
		if ok && db != "" && user != "" {
			b.databases[db] = append(b.databases[db], user)
		}
	}
	if len(b.databases) == 0 {
		return b
	}
	b.setupCode = normalizeSetupCode(setupCode)
	switch {
	case len(b.setupCode) >= minSetupCodeLength:
		b.fromEnv = true
	case b.setupCode != "":
		log.Printf("Bootstrap: FILE4BASE_SETUP_CODE is ignored: it needs at least %d letters or digits; a random setup code is used instead", minSetupCodeLength)
		fallthrough
	default:
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
	if b == nil {
		return false
	}
	_, ok := b.databases[strings.ToLower(strings.TrimSpace(database))]
	return ok
}

func (b *bootstrapReservation) codeMatches(code string) bool {
	given := normalizeSetupCode(code)
	return b != nil && b.setupCode != "" && given != "" &&
		subtle.ConstantTimeCompare([]byte(given), []byte(b.setupCode)) == 1
}

// logStartup prints the setup code when a reserved database does not exist
// yet, so that the operator can create it.
func (b *bootstrapReservation) logStartup(dbMgr *dbal.MultiDatabaseManager) {
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
	b.printCode("database(s) " + strings.Join(missing, ", ") + " reserved for server administrators do not exist yet")
}

// printCode logs the setup code once (never a FILE4BASE_SETUP_CODE one).
func (b *bootstrapReservation) printCode(why string) {
	if b.fromEnv {
		log.Printf("Bootstrap: %s; create them with the FILE4BASE_SETUP_CODE setup code", why)
		b.printed.Store(true)
		return
	}
	if b.printed.Swap(true) {
		return
	}
	log.Printf("Bootstrap: %s. To create one without a super-owner, enter this setup code (valid until restart): %s", why, groupedSetupCode(b.setupCode))
}

// allow reports whether the caller may create the database with ownerUser as
// its first owner (ownerField names that request field). A super-owner always
// may; with acceptsCode, so may whoever gives the setup code, but then the
// first owner of a seeded database must be one of its seeded accounts, or
// nobody could create them later. It writes the problem otherwise.
func (b *bootstrapReservation) allow(w http.ResponseWriter, r *http.Request, database, ownerUser, ownerField, code string, acceptsCode bool) bool {
	if !b.reserved(database) {
		return true
	}
	database = strings.ToLower(strings.TrimSpace(database))
	if sess, ok := auth.FromContext(r.Context()); ok && sess.SuperOwner {
		return true
	}
	if acceptsCode && b.codeMatches(code) {
		seeded := b.databases[database]
		owner := strings.ToLower(strings.TrimSpace(ownerUser))
		if database != b.adminDatabase && len(seeded) > 0 && !slices.Contains(seeded, owner) {
			telemetry.WriteValidationProblem(w, r, "The first owner of a reserved database must be its super-owner", []telemetry.InvalidParam{
				{Name: ownerField, Reason: "FILE4BASE_SUPER_OWNER seeds " + strings.Join(seeded, ", ") + " for database '" + database + "'; create it with that owner"},
			})
			return false
		}
		log.Printf("Bootstrap: setup code accepted for reserved database %q", database)
		return true
	}
	if acceptsCode && !b.printed.Load() {
		b.printCode("database " + database + " reserved for server administrators was requested")
	}
	detail := "database '" + database + "' is reserved: its owners become server administrators " +
		"(FILE4BASE_ADMIN_DATABASE / FILE4BASE_SUPER_OWNER), so only a super-owner can create it"
	switch {
	case acceptsCode && strings.TrimSpace(code) != "":
		detail = "the setup code is not valid; it is printed in the server log"
	case acceptsCode:
		detail += ", or enter the setup code printed in the server log"
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
		"reserved_database":   database,
		"setup_code_required": acceptsCode,
	})
	return false
}
