package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestTimeout bounds the context of every request to def, except the
// request paths listed in long, which get their own limit. A FileMaker
// migration uploads and converts a native database copy of several hundred
// megabytes and then imports its records, which takes far longer than an
// ordinary request (#126); every other route keeps the short default.
//
// Paths are compared exactly with r.URL.Path.
func RequestTimeout(def time.Duration, long map[string]time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fallback := middleware.Timeout(def)(next)
		special := make(map[string]http.Handler, len(long))
		for path, limit := range long {
			special[path] = middleware.Timeout(limit)(next)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h, ok := special[r.URL.Path]; ok {
				h.ServeHTTP(w, r)
				return
			}
			fallback.ServeHTTP(w, r)
		})
	}
}

// ParseByteSize parses a positive byte count, optionally followed by a binary
// unit: "1073741824", "512MiB", "2 GiB", "64kib". Decimal units (MB, GB) are
// refused rather than guessed.
func ParseByteSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	units := []struct {
		suffix string
		shift  uint
	}{{"gib", 30}, {"mib", 20}, {"kib", 10}, {"b", 0}}
	shift := uint(0)
	lower := strings.ToLower(s)
	for _, u := range units {
		if strings.HasSuffix(lower, u.suffix) {
			s = strings.TrimSpace(s[:len(s)-len(u.suffix)])
			shift = u.shift
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("expected a positive byte count, optionally with a KiB, MiB or GiB suffix")
	}
	if n > math.MaxInt64>>shift {
		return 0, fmt.Errorf("size is too large")
	}
	return n << shift, nil
}

// MigrationTimeouts maps every migration route to limit, for RequestTimeout.
func MigrationTimeouts(limit time.Duration) map[string]time.Duration {
	if limit <= 0 {
		limit = DefaultMigrationRequestTimeout
	}
	out := make(map[string]time.Duration, len(MigrationRoutes))
	for _, path := range MigrationRoutes {
		out[path] = limit
	}
	return out
}
