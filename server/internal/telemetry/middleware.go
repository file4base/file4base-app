package telemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (w *statusCapturingResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

// Hijack lets a caller take over the connection (needed by the WebSocket
// upgrade at /api/v1/realtime). Embedding http.ResponseWriter does not promote
// Hijack — the static type is the bare interface — so the wrapper must forward
// it explicitly, or the upgrade fails with 501 Not Implemented.
func (w *statusCapturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Flush forwards streaming flushes (e.g. Server-Sent Events) to the underlying
// writer when it supports them.
func (w *statusCapturingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// LogEntry defines the standard structured JSON log record.
type LogEntry struct {
	Timestamp      string  `json:"timestamp"`
	Level          string  `json:"level"`
	Message        string  `json:"message"`
	TraceID        string  `json:"trace_id,omitempty"`
	SpanID         string  `json:"span_id,omitempty"`
	ServiceName    string  `json:"service.name"`
	ServiceVersion string  `json:"service.version"`
	HTTPMethod     string  `json:"http.method"`
	HTTPPath       string  `json:"http.path"`
	HTTPStatusCode int     `json:"http.status_code"`
	BytesWritten   int64   `json:"http.bytes_written"`
	DurationMs     float64 `json:"duration_ms"`
	ClientIP       string  `json:"client_ip,omitempty"`
	UserAgent      string  `json:"user_agent,omitempty"`
}

// Middleware creates a high-performance OpenTelemetry and structured logging
// HTTP middleware. clientIP resolves the caller's address for the request log,
// honouring forwarding headers only from trusted proxies; when nil, the direct
// peer address (RemoteAddr) is logged.
func Middleware(serviceName, version string, clientIP func(*http.Request) string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			// 1. Extract or generate W3C TraceContext
			incomingTrace := r.Header.Get("traceparent")
			traceID, spanID, ok := ParseTraceparent(incomingTrace)
			if !ok {
				traceID = GenerateTraceID()
				spanID = GenerateSpanID()
			}

			// Injected into Context
			ctx := ContextWithTrace(r.Context(), traceID, spanID)
			r = r.WithContext(ctx)

			// 2. Set Response Headers
			traceparentHeader := fmt.Sprintf("00-%s-%s-01", traceID, spanID)
			w.Header().Set("traceparent", traceparentHeader)
			w.Header().Set("X-Trace-ID", traceID)

			// Security Headers (Defense-in-Depth)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// 3. Wrap response writer to capture code and size
			wrapped := &statusCapturingResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(startTime)
			durationMs := float64(duration.Microseconds()) / 1000.0

			// 4. Do not flood logs with healthy liveness probes
			if (r.URL.Path == "/healthz/liveness" || r.URL.Path == "/livez") && wrapped.statusCode == http.StatusOK {
				return
			}

			// 5. Determine log level
			level := "INFO"
			if wrapped.statusCode >= 500 {
				level = "ERROR"
			} else if wrapped.statusCode >= 400 {
				level = "WARN"
			}

			clientAddr := r.RemoteAddr
			if clientIP != nil {
				clientAddr = clientIP(r)
			}

			entry := LogEntry{
				Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
				Level:          level,
				Message:        fmt.Sprintf("HTTP %s %s -> %d", r.Method, r.URL.Path, wrapped.statusCode),
				TraceID:        traceID,
				SpanID:         spanID,
				ServiceName:    serviceName,
				ServiceVersion: version,
				HTTPMethod:     r.Method,
				HTTPPath:       r.URL.Path,
				HTTPStatusCode: wrapped.statusCode,
				BytesWritten:   wrapped.bytesWritten,
				DurationMs:     durationMs,
				ClientIP:       clientAddr,
				UserAgent:      r.UserAgent(),
			}

			DefaultCollector().Record(entry)

			jsonBytes, err := json.Marshal(entry)
			if err == nil {
				_, _ = fmt.Fprintln(os.Stdout, string(jsonBytes))
			}
		})
	}
}
