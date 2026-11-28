package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// CustomFunctionsTable holds a solution's custom functions: what FileMaker
// calls Manage Custom Functions, a name, its parameters and a formula over
// them, which every formula of the solution may call.
const CustomFunctionsTable = "sys_custom_functions"

// ReadCustomFunctions reads the solution's custom functions, in name order.
// A database created before custom functions were stored has none.
func ReadCustomFunctions(ctx context.Context, driver dbal.DatabaseDriver) ([]calc.CustomFunction, error) {
	rows, err := driver.DB().QueryContext(ctx, `SELECT name, parameters, formula FROM `+CustomFunctionsTable+` ORDER BY name`)
	if dbal.IsUndefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed reading the custom functions: %w", err)
	}
	defer rows.Close()
	var out []calc.CustomFunction
	for rows.Next() {
		var fn calc.CustomFunction
		var params string
		if err := rows.Scan(&fn.Name, &params, &fn.Formula); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(params), &fn.Parameters); err != nil {
			return nil, fmt.Errorf("the parameters of the custom function %q cannot be read: %w", fn.Name, err)
		}
		out = append(out, fn)
	}
	return out, rows.Err()
}

// LoadLibrary is the solution's custom functions, ready for its formulas to
// call. A function whose formula cannot be read is left out of it, so the
// formulas that do not call it still work; one that does is reported as
// calling a function that does not exist.
//
// The library is read and compiled once per solution database and kept:
// every formula a request evaluates, and every request, reuses it. A change
// to the custom functions through this server replaces it at once
// (InvalidateLibrary); one written to the database by anything else is seen
// within libraryTTL.
func LoadLibrary(ctx context.Context, driver dbal.DatabaseDriver) (*calc.Library, error) {
	db := driver.DB()
	libraries.mu.Lock()
	entry := libraries.byDB[db]
	if entry == nil {
		entry = &libraryEntry{}
		libraries.byDB[db] = entry
	}
	if entry.ready && time.Since(entry.loaded) < libraryTTL {
		lib := entry.lib
		libraries.mu.Unlock()
		return lib, nil
	}
	generation := entry.generation
	libraries.mu.Unlock()

	lib, err := loadLibrary(ctx, driver)
	if err != nil {
		return nil, err
	}
	libraries.mu.Lock()
	// A change made while this one was read wins: the library read before
	// it is not kept.
	if entry.generation == generation {
		entry.lib, entry.loaded, entry.ready = lib, time.Now(), true
	}
	libraries.mu.Unlock()
	return lib, nil
}

// InvalidateLibrary drops the library kept for a solution database, after its
// custom functions changed: the next formula reads them again.
func InvalidateLibrary(driver dbal.DatabaseDriver) {
	libraries.mu.Lock()
	defer libraries.mu.Unlock()
	if entry := libraries.byDB[driver.DB()]; entry != nil {
		entry.generation++
		entry.lib, entry.ready = nil, false
	}
}

// libraryTTL bounds how long a library is kept without being read again,
// for custom functions changed other than through this server.
const libraryTTL = time.Minute

type libraryEntry struct {
	lib        *calc.Library
	loaded     time.Time
	ready      bool
	generation uint64
}

var libraries = struct {
	mu   sync.Mutex
	byDB map[*sql.DB]*libraryEntry
}{byDB: map[*sql.DB]*libraryEntry{}}

func loadLibrary(ctx context.Context, driver dbal.DatabaseDriver) (*calc.Library, error) {
	defs, err := ReadCustomFunctions(ctx, driver)
	if err != nil || len(defs) == 0 {
		return nil, err
	}
	lib, err := calc.NewLibrary(defs, calc.Options{})
	var broken *calc.LibraryError
	if err != nil && !errors.As(err, &broken) {
		return nil, err
	}
	return lib, nil
}

// ForgetPool drops what is kept for a connection pool that is closed for
// good (a dbal.TxDriver's): its custom function library and what is known of
// its tables' creation order columns.
func ForgetPool(db *sql.DB) {
	libraries.mu.Lock()
	delete(libraries.byDB, db)
	libraries.mu.Unlock()
	ForgetCreationOrder(db)
}
