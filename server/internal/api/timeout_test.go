package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestTimeoutGivesMigrationRoutesTheirOwnLimit(t *testing.T) {
	var remaining time.Duration
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		require.True(t, ok)
		remaining = time.Until(deadline)
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequestTimeout(time.Minute, MigrationTimeouts(45*time.Minute))(probe)

	for _, path := range MigrationRoutes {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
		require.Greater(t, remaining, 44*time.Minute, path)
	}
	for _, path := range []string{"/api/v1/data/visits/", "/api/v1/solutions/migrate/preflight/extra", "/api/v1/solutions/import"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
		require.LessOrEqual(t, remaining, time.Minute, path)
	}
}

func TestMigrationTimeoutsDefault(t *testing.T) {
	for _, limit := range MigrationTimeouts(0) {
		require.Equal(t, DefaultMigrationRequestTimeout, limit)
	}
}

func TestParseByteSize(t *testing.T) {
	valid := map[string]int64{
		"1073741824": 1 << 30,
		"512MiB":     512 << 20,
		"2 GiB":      2 << 30,
		"64kib":      64 << 10,
		"100b":       100,
	}
	for raw, want := range valid {
		got, err := ParseByteSize(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"", "0", "-5MiB", "2GB", "512MB", "1.5GiB", "GiB", "9223372036854775807GiB"} {
		_, err := ParseByteSize(raw)
		require.Error(t, err, raw)
	}
}
