package fmdapi

import (
	"net/http"
	"strconv"
	"strings"
)

const jsonContentType = "application/json; charset=utf-8"

// okMessages is the messages array of every successful reply.
var okMessages = []Object{{{"code", "0"}, {"message", "OK"}}}

// emptyObject is the "response" of every error reply.
var emptyObject = Object{}

// writeOK answers a successful call: the response, then the OK message.
// Every endpoint but productInfo also tells caches not to reuse the reply.
func writeOK(w http.ResponseWriter, response Object, noCache bool) {
	if noCache {
		w.Header().Set("Cache-Control", "no-cache")
	}
	writeBody(w, http.StatusOK, jsonContentType,
		Marshal(Object{{"response", response}, {"messages", okMessages}}))
}

// writeError answers an error: the message, then an empty response. The
// front end and the engine order a message's keys differently.
func writeError(w http.ResponseWriter, e *Error) {
	msg := Object{{"code", e.Code}, {"message", e.Message}}
	if e.Front {
		msg = Object{{"message", e.Message}, {"code", e.Code}}
	}
	writeBody(w, e.Status, jsonContentType,
		Marshal(Object{{"messages", []Object{msg}}, {"response", emptyObject}}))
}

// writeVersionError answers a version other than v1, v2 and vLatest.
func writeVersionError(w http.ResponseWriter) {
	writeBody(w, http.StatusBadRequest, jsonContentType, Marshal(Object{{"response", emptyObject}}))
}

// writeNotFound answers a request no route matches with Express's default
// page (finalhandler), naming the method and the resource as it does.
func writeNotFound(w http.ResponseWriter, method, resource string) {
	msg := escapeHTML("Cannot " + method + " " + encodeURL(resource))
	msg = strings.ReplaceAll(msg, "\n", "<br>")
	msg = strings.ReplaceAll(msg, "  ", " &nbsp;")
	page := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>Error</title>\n</head>\n<body>\n<pre>" +
		msg + "</pre>\n</body>\n</html>\n"
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeBody(w, http.StatusNotFound, "text/html; charset=utf-8", []byte(page))
}

func writeBody(w http.ResponseWriter, status int, contentType string, body []byte) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// escapeHTML is the escape-html package: & < > " and '.
func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}

// encodeURL is the encodeurl package as Express applies it to a request
// URL, which Node reads as Latin-1: characters outside the URL set are
// percent-encoded as UTF-8, and so is a % that starts no escape. Existing
// escapes are kept.
func encodeURL(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte(c)
		case c == '%':
			b.WriteString("%25")
		case urlSafe(c):
			b.WriteByte(c)
		case c < 0x80:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xF])
		default:
			// A Latin-1 character, as UTF-8.
			for _, u := range []byte(string(rune(c))) {
				b.WriteByte('%')
				b.WriteByte(hexDigits[u>>4])
				b.WriteByte(hexDigits[u&0xF])
			}
		}
	}
	return b.String()
}

// urlSafe is encodeurl's set of characters left as they are.
func urlSafe(c byte) bool {
	return c == 0x21 || (c >= 0x23 && c <= 0x3B) || c == 0x3D || (c >= 0x3F && c <= 0x5F) ||
		(c >= 0x61 && c <= 0x7A) || c == 0x7C || c == 0x7E
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
