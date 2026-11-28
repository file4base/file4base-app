package fmdapi

import (
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf16"
)

// DefaultMaxBodyBytes bounds a JSON request body, decompressed.
const DefaultMaxBodyBytes = 10 << 20

// errBodyTooLarge carries the message body-parser gives an oversized body.
var errBodyTooLarge = errors.New("request entity too large")

// isJSONRequest reports whether body-parser would parse the request as
// JSON: it has a body and its media type is application/json, in any case
// and with any parameters.
func isJSONRequest(r *http.Request) bool {
	if r.ContentLength == 0 && len(r.TransferEncoding) == 0 {
		return false
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// readJSONBody reads and parses a JSON request body as FileMaker Server's
// front end does (body-parser in strict mode, then JSON.parse). A failure
// is the front end's 1630 reply carrying body-parser's message.
func readJSONBody(r *http.Request, limit int64) (Value, *Error) {
	// body-parser checks the charset first, then the content encoding.
	_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	charset := strings.ToLower(params["charset"])
	if charset == "" {
		charset = "utf-8"
	}
	if !strings.HasPrefix(charset, "utf-") {
		return Value{}, bodyError(`unsupported charset "` + strings.ToUpper(charset) + `"`)
	}
	var in io.Reader = r.Body
	switch enc := strings.ToLower(r.Header.Get("Content-Encoding")); enc {
	case "", "identity":
		if r.ContentLength > limit {
			return Value{}, bodyError(errBodyTooLarge.Error())
		}
	case "gzip":
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			return Value{}, bodyError(zlibMessage(err))
		}
		defer zr.Close()
		in = zr
	case "deflate":
		zr, err := zlib.NewReader(r.Body)
		if err != nil {
			return Value{}, bodyError(zlibMessage(err))
		}
		defer zr.Close()
		in = zr
	default:
		return Value{}, bodyError(`unsupported content encoding "` + enc + `"`)
	}
	decode := decoderFor(charset)
	if decode == nil {
		return Value{}, bodyError("specified encoding unsupported")
	}
	raw, err := io.ReadAll(io.LimitReader(in, limit+1))
	switch {
	case err != nil:
		return Value{}, bodyError(zlibMessage(err))
	case int64(len(raw)) > limit:
		return Value{}, bodyError(errBodyTooLarge.Error())
	}
	v, msg := parseBody(decode(raw))
	if msg != nil {
		return Value{}, bodyError(wtf8(msg))
	}
	return v, nil
}

// zlibMessage is the closest message to Node's zlib errors for a broken
// compressed body.
func zlibMessage(err error) string {
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected end of file"
	case errors.Is(err, gzip.ErrHeader), errors.Is(err, zlib.ErrHeader):
		return "incorrect header check"
	case errors.Is(err, gzip.ErrChecksum), errors.Is(err, zlib.ErrChecksum):
		return "incorrect data check"
	}
	var corrupt flate.CorruptInputError
	if errors.As(err, &corrupt) {
		return "invalid block type"
	}
	return "unexpected end of file"
}

func bodyError(msg string) *Error {
	return frontError(http.StatusOK, "1630", msg)
}

// parseBody is body-parser's JSON parse in strict mode: an empty body is
// an empty object, and the text must start with { or [. A text that does
// not is reported as JSON.parse reports it, with its characters masked and
// then put back into the message, as body-parser does.
func parseBody(text []uint16) (Value, []uint16) {
	if len(text) == 0 {
		return Value{Kind: KindObject, Members: []Member{}}, nil
	}
	first := 0
	for first < len(text) && tokenOf(int(text[first])) == tokWhitespace {
		first++
	}
	if first < len(text) && (text[first] == '{' || text[first] == '[') {
		return parseJSON(text)
	}
	var masked []uint16
	if first < len(text) {
		masked = append(masked, text[:first]...)
		for range text[first:] {
			masked = append(masked, '#')
		}
	}
	_, msg := parseJSON(masked)
	var out []uint16
	for i := 0; i < len(msg); {
		if msg[i] != '#' {
			out = append(out, msg[i])
			i++
			continue
		}
		run := i
		for i < len(msg) && msg[i] == '#' {
			i++
		}
		end := min(first+i-run, len(text))
		out = append(out, text[first:end]...)
	}
	return Value{}, out
}

// decoderFor returns the decoder iconv-lite gives a utf-* charset, nil when
// it has none File4Base reproduces. iconv-lite drops one leading byte order
// mark. UTF-7 and UTF-32 are refused as unsupported.
func decoderFor(charset string) func([]byte) []uint16 {
	canonical := strings.Map(func(r rune) rune {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			return r
		}
		return -1
	}, charset)
	switch canonical {
	case "utf8":
		return decodeUTF8
	case "utf16le":
		return func(b []byte) []uint16 { return stripBOM(decodeUTF16(b, false)) }
	case "utf16be":
		return func(b []byte) []uint16 { return stripBOM(decodeUTF16(b, true)) }
	case "utf16":
		return func(b []byte) []uint16 { return stripBOM(decodeUTF16(b, utf16IsBE(b))) }
	}
	return nil
}

// utf16IsBE is iconv-lite's guess of a UTF-16 body's byte order: its byte
// order mark, else whichever order makes more ASCII characters of the
// first 64 bytes, else little-endian.
func utf16IsBE(b []byte) bool {
	if len(b) >= 2 {
		switch {
		case b[0] == 0xFE && b[1] == 0xFF:
			return true
		case b[0] == 0xFF && b[1] == 0xFE:
			return false
		}
	}
	le, be := 0, 0
	for i := 0; i+1 < len(b) && i < 64; i += 2 {
		if b[i] == 0 && b[i+1] != 0 {
			be++
		}
		if b[i] != 0 && b[i+1] == 0 {
			le++
		}
	}
	return be > le
}

// decodeUTF16 reads code units; a trailing odd byte is dropped.
func decodeUTF16(b []byte, bigEndian bool) []uint16 {
	out := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			out = append(out, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			out = append(out, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return out
}

func stripBOM(u []uint16) []uint16 {
	if len(u) > 0 && u[0] == 0xFEFF {
		return u[1:]
	}
	return u
}

// decodeUTF8 decodes a body to UTF-16 as body-parser does: as
// decodeUTF8Text, one leading byte order mark dropped.
func decodeUTF8(b []byte) []uint16 {
	return stripBOM(decodeUTF8Text(b))
}

// decodeUTF8Text decodes UTF-8 to UTF-16 as Node's Buffer.toString does:
// each maximal invalid subsequence becomes U+FFFD (the WHATWG decoder).
func decodeUTF8Text(b []byte) []uint16 {
	out := make([]uint16, 0, len(b))
	var cp rune
	need, lower, upper := 0, byte(0x80), byte(0xBF)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if need == 0 {
			switch {
			case c <= 0x7F:
				out = append(out, uint16(c))
			case c >= 0xC2 && c <= 0xDF:
				need, cp = 1, rune(c&0x1F)
			case c >= 0xE0 && c <= 0xEF:
				if c == 0xE0 {
					lower = 0xA0
				} else if c == 0xED {
					upper = 0x9F
				}
				need, cp = 2, rune(c&0x0F)
			case c >= 0xF0 && c <= 0xF4:
				if c == 0xF0 {
					lower = 0x90
				} else if c == 0xF4 {
					upper = 0x8F
				}
				need, cp = 3, rune(c&0x07)
			default:
				out = append(out, 0xFFFD)
			}
			continue
		}
		if c < lower || c > upper {
			need, lower, upper = 0, 0x80, 0xBF
			out = append(out, 0xFFFD)
			i-- // the byte starts over
			continue
		}
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(c&0x3F)
		if need--; need == 0 {
			out = utf16.AppendRune(out, cp)
		}
	}
	if need > 0 {
		out = append(out, 0xFFFD)
	}
	return out
}
