package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The change-notification WebSocket at /api/v1/realtime upgrades the
// connection by hijacking it. The telemetry middleware wraps every response
// writer to record the status code; if that wrapper does not forward Hijack,
// the upgrade fails with 501 Not Implemented and real-time sync silently never
// connects. This guards that regression.
func TestMiddleware_ForwardsHijackForWebSocketUpgrade(t *testing.T) {
	gotHijacker := make(chan bool, 1)

	handler := Middleware("test", "0.0.0", nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		gotHijacker <- ok
		if !ok {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("Hijack() through telemetry middleware failed: %v", err)
			return
		}
		_ = conn.Close()
	}))

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// The handler hijacks and closes the connection, so the client sees an
	// error rather than a response; that is expected and irrelevant here.
	resp, err := http.Get(srv.URL) //nolint:bodyclose // connection is hijacked
	if err == nil {
		_ = resp.Body.Close()
	}

	select {
	case ok := <-gotHijacker:
		if !ok {
			t.Fatal("telemetry middleware's response writer does not implement http.Hijacker; the WebSocket upgrade at /api/v1/realtime would fail with 501 and real-time sync would never connect")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not invoked")
	}
}
