package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type contextKey string

const (
	traceIDKey contextKey = "telemetry_trace_id"
	spanIDKey  contextKey = "telemetry_span_id"
)

// GenerateTraceID generates a random 128-bit W3C-compliant trace ID (32 hex characters).
func GenerateTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000000000000000000000000001"
	}
	return hex.EncodeToString(b)
}

// GenerateSpanID generates a random 64-bit W3C-compliant span ID (16 hex characters).
func GenerateSpanID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000001"
	}
	return hex.EncodeToString(b)
}

// ParseTraceparent parses a standard W3C traceparent header:
// e.g. "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
func ParseTraceparent(header string) (traceID, spanID string, ok bool) {
	header = strings.TrimSpace(header)
	parts := strings.Split(header, "-")
	if len(parts) < 4 {
		return "", "", false
	}

	// Version must be valid hex and not "ff"
	version := parts[0]
	if len(version) != 2 || version == "ff" {
		return "", "", false
	}

	traceID = parts[1]
	spanID = parts[2]

	if len(traceID) != 32 || len(spanID) != 16 {
		return "", "", false
	}

	// Must be valid hex characters
	if _, err := hex.DecodeString(traceID); err != nil {
		return "", "", false
	}
	if _, err := hex.DecodeString(spanID); err != nil {
		return "", "", false
	}

	// All zeros is invalid in W3C spec
	if traceID == "00000000000000000000000000000000" || spanID == "0000000000000000" {
		return "", "", false
	}

	return traceID, spanID, true
}

// ContextWithTrace attaches trace_id and span_id to context.
func ContextWithTrace(ctx context.Context, traceID, spanID string) context.Context {
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	ctx = context.WithValue(ctx, spanIDKey, spanID)
	return ctx
}

// TraceIDFromContext returns the current trace_id or empty string.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(traceIDKey).(string); ok {
		return val
	}
	return ""
}

// SpanIDFromContext returns the current span_id or empty string.
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(spanIDKey).(string); ok {
		return val
	}
	return ""
}
