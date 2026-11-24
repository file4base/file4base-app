package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/migration"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

const (
	// maxMigrationJSONBytes bounds a JSON migration request, and the JSON
	// "request" part of a multipart one. A native database copy sent as
	// base64 inside the JSON body counts against it; larger copies are sent
	// as a multipart "native" part instead (#126).
	maxMigrationJSONBytes = 64 << 20 // 64 MiB

	// DefaultMaxMigrationUploadBytes is the default ceiling of a multipart
	// migration request from a signed-in session (MIGRATION_MAX_UPLOAD_BYTES).
	DefaultMaxMigrationUploadBytes int64 = 2 << 30 // 2 GiB

	// DefaultMigrationRequestTimeout is the default time a migration
	// preflight or apply may take, upload included (MIGRATION_REQUEST_TIMEOUT).
	DefaultMigrationRequestTimeout = 30 * time.Minute
)

// MigrationRoutes are the request paths that receive a FileMaker migration
// upload. They are long-running: see RequestTimeout.
var MigrationRoutes = []string{
	"/api/v1/solutions/migrate/preflight",
	"/api/v1/solutions/migrate/apply",
}

// migrationInput is one decoded migration request: its JSON fields and, when
// the native database copy was uploaded as a multipart part, its bytes.
type migrationInput struct {
	req    migration.MigrationRequest
	native []byte
}

// hasAttachedData reports whether the request carries records to import, in
// any of the accepted forms.
func (in *migrationInput) hasAttachedData() bool {
	return len(in.native) > 0 || strings.TrimSpace(in.req.FMP12Base64) != "" || len(in.req.DataFiles) > 0
}

// convert runs the migration analysis over whichever native input was sent.
func (in *migrationInput) convert() (*migration.ConvertedSolution, error) {
	if len(in.native) > 0 {
		return migration.AnalyzeAndConvertNative(in.req, in.native)
	}
	return migration.AnalyzeAndConvert(in.req)
}

// maxMigrationUploadBytes is the ceiling of a multipart migration request for
// the caller. A caller without a session (only possible with
// ALLOW_PUBLIC_DATABASE_CREATION) keeps the JSON ceiling, so enabling public
// database creation never lets anonymous callers upload gigabytes.
func (h *SolutionHandler) maxMigrationUploadBytes(hasSession bool) int64 {
	limit := h.opts.MaxMigrationUploadBytes
	if limit <= 0 {
		limit = DefaultMaxMigrationUploadBytes
	}
	if !hasSession && limit > maxMigrationJSONBytes {
		limit = maxMigrationJSONBytes
	}
	return limit
}

// readMigrationInput decodes a migration request sent either as JSON (the
// original form, with an optional base64 "fmp12_base64") or as
// multipart/form-data with these parts (#126):
//
//	request  JSON object with the same fields as the JSON form (required)
//	native   the native .fmp12 database copy, raw bytes (optional)
//	design   the design XML/HTML document, raw (optional; else request.design_xml)
//
// It writes the problem response itself and returns false when the request
// cannot be used.
func (h *SolutionHandler) readMigrationInput(w http.ResponseWriter, r *http.Request, hasSession bool) (*migrationInput, bool) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		return h.readMultipartMigration(w, r, hasSession)
	}
	var in migrationInput
	body := http.MaxBytesReader(w, r.Body, maxMigrationJSONBytes)
	if err := json.NewDecoder(body).Decode(&in.req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Migration Request Too Large",
				fmt.Sprintf("a JSON migration request is limited to %d MiB; send the native database copy as the "+
					"\"native\" part of a multipart/form-data request instead of fmp12_base64", maxMigrationJSONBytes>>20))
			return nil, false
		}
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return nil, false
	}
	return &in, true
}

func (h *SolutionHandler) readMultipartMigration(w http.ResponseWriter, r *http.Request, hasSession bool) (*migrationInput, bool) {
	limit := h.maxMigrationUploadBytes(hasSession)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	reader, err := r.MultipartReader()
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Multipart Request", err.Error())
		return nil, false
	}

	tooLarge := func() {
		detail := fmt.Sprintf("a migration upload is limited to %d MiB", limit>>20)
		if !hasSession {
			detail += " without a session; sign in as an owner or admin to upload a larger native database copy"
		} else {
			detail += " (MIGRATION_MAX_UPLOAD_BYTES)"
		}
		telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Migration Upload Too Large", detail)
	}
	badPart := func(detail string) {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Multipart Request", detail)
	}
	failed := func(err error) {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			tooLarge()
			return
		}
		badPart("the upload could not be read: " + err.Error())
	}

	var (
		in                    migrationInput
		sawRequest, sawDesign bool
		sawNative             bool
		designXML             string
		nativeBytes           []byte
		nativeFileName        string
	)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			failed(err)
			return nil, false
		}
		name := part.FormName()
		switch name {
		case "request":
			if sawRequest {
				badPart("the \"request\" part appears more than once")
				return nil, false
			}
			sawRequest = true
			raw, err := io.ReadAll(io.LimitReader(part, maxMigrationJSONBytes+1))
			if err != nil {
				failed(err)
				return nil, false
			}
			if int64(len(raw)) > maxMigrationJSONBytes {
				telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Migration Request Too Large",
					fmt.Sprintf("the \"request\" part is limited to %d MiB; send the design as the \"design\" part "+
						"and the native database copy as the \"native\" part", maxMigrationJSONBytes>>20))
				return nil, false
			}
			if err := json.Unmarshal(raw, &in.req); err != nil {
				telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "the \"request\" part contains invalid JSON format")
				return nil, false
			}
		case "design":
			if sawDesign {
				badPart("the \"design\" part appears more than once")
				return nil, false
			}
			sawDesign = true
			raw, err := io.ReadAll(part)
			if err != nil {
				failed(err)
				return nil, false
			}
			designXML = string(raw)
		case "native":
			if sawNative {
				badPart("the \"native\" part appears more than once")
				return nil, false
			}
			sawNative = true
			if fn := strings.TrimSpace(part.FileName()); fn != "" {
				nativeFileName = filepath.Base(fn)
			}
			nativeBytes, err = spoolMigrationPart(part, h.opts.MigrationUploadDir)
			if err != nil {
				failed(err)
				return nil, false
			}
		default:
			badPart(fmt.Sprintf("unexpected part %q; a migration upload accepts the parts request, design and native", name))
			return nil, false
		}
		_ = part.Close()
	}

	if !sawRequest {
		badPart("the \"request\" part (the JSON migration request) is required")
		return nil, false
	}
	if sawDesign {
		if strings.TrimSpace(in.req.DesignXML) != "" {
			badPart("send the design either as the \"design\" part or as design_xml, not both")
			return nil, false
		}
		in.req.DesignXML = designXML
	}
	if sawNative {
		if len(nativeBytes) == 0 {
			badPart("the \"native\" part is empty")
			return nil, false
		}
		if strings.TrimSpace(in.req.FMP12Base64) != "" {
			badPart("send the native database copy either as the \"native\" part or as fmp12_base64, not both")
			return nil, false
		}
		in.native = nativeBytes
		if nativeFileName != "" && strings.TrimSpace(in.req.NativeFileName) == "" {
			in.req.NativeFileName = nativeFileName
		}
	}
	return &in, true
}

// spoolMigrationPart streams an uploaded native database copy to a private
// temporary file, then reads it back into one exactly-sized buffer and
// removes the file. The upload is never held as base64 or as a growing
// in-memory buffer, and nothing is allocated for it until it has been
// received completely and found to be within the request limit.
func spoolMigrationPart(part io.Reader, dir string) ([]byte, error) {
	file, err := os.CreateTemp(dir, "file4base-migration-*.upload")
	if err != nil {
		return nil, fmt.Errorf("no space for the upload: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	size, err := io.Copy(file, part)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(file, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// writeMigrationConversionError reports a failed migration analysis.
func writeMigrationConversionError(w http.ResponseWriter, r *http.Request, err error, title string) {
	switch {
	case errors.Is(err, migration.ErrBinaryFMP12):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Unsupported Binary .fmp12 Format", err.Error())
	case errors.Is(err, migration.ErrInvalidMigrationXML):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid FileMaker XML", err.Error())
	default:
		telemetry.WriteProblem(w, r, http.StatusBadRequest, title, err.Error())
	}
}
